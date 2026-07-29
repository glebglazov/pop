package binding

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/glebglazov/pop/config"
	"github.com/glebglazov/pop/project"
	"github.com/glebglazov/pop/tasks"
)

const unbindConfirmPrompt = "Abandon binding for %s? This forgets the association; the checkout and branch are kept. Task statuses are unchanged. [y/N]: "

const managedWorktreeDeletePrompt = "delete managed worktree at %s? [y/N]: "

// BindWorktreeOptions controls bind-worktree behaviour.
type BindWorktreeOptions struct {
	Force bool
	Yes   bool
	In    io.Reader
	// ProjectName, when non-empty, is used verbatim as the binding's Project
	// label and skips DetectProject entirely. Callers that already resolved the
	// name fork-free (the dashboard, ADR-0060) supply it; cwd-based callers
	// leave it empty to fall back to DetectProject.
	ProjectName string
	// Managed switches from adopting checkoutPath to provisioning a managed
	// worktree from the Trunk and recording a provisioned binding (the same
	// seam as `register --managed`, ADR-0147). checkoutPath is still resolved to
	// the repository identity; TrunkPath, when non-empty, is the fork base
	// (already resolved and optionally persisted by the caller).
	Managed bool
	TrunkPath string
}

// BindWorktreeResult describes the outcome of adopting an existing checkout.
type BindWorktreeResult struct {
	SetID       string
	RuntimePath string
	Branch      string
	Replaced    bool
}

// UnbindWorktreeOptions controls confirmation for unbind-worktree.
type UnbindWorktreeOptions struct {
	Yes bool
	In  io.Reader
}

// UnbindWorktreeResult describes the outcome of releasing a worktree binding.
type UnbindWorktreeResult struct {
	SetID string
	Noop  bool
}

// LifecycleHooks injects runtime lock reads and queue-owned side effects.
type LifecycleHooks struct {
	ReadLock func(runtimePath string) *tasks.RuntimeLockStatus
	// NeedsConfirm is optional; when nil, unbind never prompts.
	NeedsConfirm func(setID string, b Binding) (bool, error)
	// AfterUnbind runs after the binding is deleted from the shared store.
	AfterUnbind func(key, setID string, b Binding, branch string) error
	// ResolveTeardownBase returns the git working tree used to remove a managed
	// checkout. When nil, ResolveTrunkPath is used.
	ResolveTeardownBase func(b Binding) (string, error)
}

func readRuntimeLock(hooks LifecycleHooks, runtimePath string) *tasks.RuntimeLockStatus {
	if hooks.ReadLock != nil {
		return hooks.ReadLock(runtimePath)
	}
	return nil
}

// lockedByAnotherSet reports whether a live runtime lock is attributable to a
// set other than setID. N sets can share one checkout (ADR-0115/0116), so an
// unrelated set's drain must not block retargeting or releasing setID's binding
// — only setID's own live lock refuses the verb. A locked status with no
// attributable owner is treated as setID's own hold (the conservative default),
// keeping the "currently executing" refusal.
func lockedByAnotherSet(lock *tasks.RuntimeLockStatus, setID string) bool {
	return lock != nil && lock.Locked &&
		lock.Metadata != nil && lock.Metadata.SetID != "" &&
		lock.Metadata.SetID != setID
}

// BindWorktree creates an adopted (Provisioned=false) binding for (repo
// identity, setID) pointing to checkoutPath. Run from inside the checkout;
// pass os.Getwd() as checkoutPath. It refuses to re-point a set already bound
// elsewhere unless opts.Force is true, and always refuses while the set holds
// a live Runtime execution lock.
func BindWorktree(td *tasks.Deps, pd *project.Deps, cfg *config.Config, setID, checkoutPath string, opts BindWorktreeOptions, hooks LifecycleHooks, out io.Writer) (BindWorktreeResult, error) {
	setID = strings.TrimSpace(setID)
	if setID == "" {
		return BindWorktreeResult{}, fmt.Errorf("set id is required")
	}
	checkoutPath = strings.TrimSpace(checkoutPath)
	if checkoutPath == "" {
		return BindWorktreeResult{}, fmt.Errorf("checkout path is required")
	}
	if out == nil {
		out = io.Discard
	}
	if td == nil {
		td = tasks.DefaultDeps()
	}
	if pd == nil {
		pd = project.DefaultDeps()
	}

	if opts.Managed {
		return bindWorktreeManaged(td, pd, cfg, setID, checkoutPath, opts, hooks, out)
	}

	branch, err := resolveRuntimeBranch(td, checkoutPath)
	if err != nil {
		return BindWorktreeResult{}, fmt.Errorf("resolve branch in %s: %w", checkoutPath, err)
	}

	id, err := tasks.ResolveRepositoryIdentity(td, checkoutPath)
	if err != nil {
		return BindWorktreeResult{}, fmt.Errorf("resolve repository identity: %w", err)
	}
	key := Key(id, setID)

	existing, ok, err := Lookup(td, key)
	if err != nil {
		return BindWorktreeResult{}, err
	}

	var replaced bool
	if ok {
		lock := readRuntimeLock(hooks, existing.RuntimePath)
		if lock != nil && lock.Locked && !lockedByAnotherSet(lock, setID) {
			return BindWorktreeResult{}, fmt.Errorf("refusing bind-worktree: %s is currently executing", setID)
		}
		existingCanon, _ := canonicalCheckoutPath(td, existing.RuntimePath)
		newCanon, _ := canonicalCheckoutPath(td, checkoutPath)
		if existingCanon != newCanon {
			if !opts.Force {
				return BindWorktreeResult{}, fmt.Errorf("%s is already bound to %s; use --force to re-point", setID, existing.RuntimePath)
			}
			replaced = true
			if err := maybeTeardownReboundCheckout(td, pd, cfg, existing, key, opts.Yes, opts.In, out,
				"non-interactive bind-worktree requires --yes to delete managed worktree when rebinding", hooks); err != nil {
				return BindWorktreeResult{}, err
			}
		}
	}

	// When the caller already knows the project name (dashboard rows carry it
	// pre-resolved, ADR-0060), use it directly and skip the DetectProject
	// fan-out that forks `git rev-parse` once per configured project.
	proj := opts.ProjectName
	if proj == "" {
		proj = DetectProject(pd, td, cfg, id)
	}

	if err := Put(td, key, Adopt(checkoutPath, branch, proj)); err != nil {
		return BindWorktreeResult{}, err
	}
	fmt.Fprintf(out, "Bound %s to %s (branch %s)\n", setID, checkoutPath, branch)
	return BindWorktreeResult{SetID: setID, RuntimePath: checkoutPath, Branch: branch, Replaced: replaced}, nil
}

// bindWorktreeManaged provisions a managed worktree for setID from the Trunk
// (opts.Managed) and records a provisioned binding before returning — the same
// eager seam as `register --managed` (ADR-0147). It resolves checkoutPath only
// to the repository identity; any of the repo's checkouts is fine. A set already
// bound elsewhere refuses without opts.Force; with --force the old binding is
// dropped forget-only before provisioning the new checkout.
func bindWorktreeManaged(td *tasks.Deps, pd *project.Deps, cfg *config.Config, setID, checkoutPath string, opts BindWorktreeOptions, hooks LifecycleHooks, out io.Writer) (BindWorktreeResult, error) {
	id, err := tasks.ResolveRepositoryIdentity(td, checkoutPath)
	if err != nil {
		return BindWorktreeResult{}, fmt.Errorf("resolve repository identity: %w", err)
	}
	key := Key(id, setID)

	existing, ok, err := Lookup(td, key)
	if err != nil {
		return BindWorktreeResult{}, err
	}

	var replaced bool
	if ok {
		lock := readRuntimeLock(hooks, existing.RuntimePath)
		if lock != nil && lock.Locked && !lockedByAnotherSet(lock, setID) {
			return BindWorktreeResult{}, fmt.Errorf("refusing bind-worktree: %s is currently executing", setID)
		}
		if !opts.Force {
			return BindWorktreeResult{}, fmt.Errorf("%s is already bound to %s; use --force to re-point to a managed worktree", setID, existing.RuntimePath)
		}
		if err := maybeTeardownReboundCheckout(td, pd, cfg, existing, key, opts.Yes, opts.In, out,
			"non-interactive bind-worktree requires --yes to delete managed worktree when rebinding", hooks); err != nil {
			return BindWorktreeResult{}, err
		}
		if err := Delete(td, key); err != nil {
			return BindWorktreeResult{}, err
		}
		replaced = true
	}

	b, err := ProvisionManagedBinding(ProvisionManagedBindingRequest{
		TD:           td,
		PD:           pd,
		Config:       cfg,
		TrunkPath:    opts.TrunkPath,
		CheckoutPath: checkoutPath,
		SetID:        setID,
	})
	if err != nil {
		return BindWorktreeResult{}, err
	}
	fmt.Fprintf(out, "Provisioned managed worktree for %s at %s (branch %s)\n", setID, b.RuntimePath, b.Branch)
	return BindWorktreeResult{SetID: setID, RuntimePath: b.RuntimePath, Branch: b.Branch, Replaced: replaced}, nil
}

// UnbindWorktree releases a set's worktree binding without integrating.
func UnbindWorktree(td *tasks.Deps, pd *project.Deps, cfg *config.Config, setID string, opts UnbindWorktreeOptions, hooks LifecycleHooks, out io.Writer) (UnbindWorktreeResult, error) {
	setID = strings.TrimSpace(setID)
	if setID == "" {
		return UnbindWorktreeResult{}, fmt.Errorf("set id is required")
	}
	if td == nil {
		td = tasks.DefaultDeps()
	}
	if pd == nil {
		pd = project.DefaultDeps()
	}

	key, b, ok, err := FindBySetID(td, setID)
	if err != nil {
		return UnbindWorktreeResult{}, err
	}
	if !ok {
		if out == nil {
			out = io.Discard
		}
		fmt.Fprintf(out, "%s has no worktree binding to unbind\n", setID)
		return UnbindWorktreeResult{SetID: setID, Noop: true}, nil
	}
	return unbindResolvedBinding(td, pd, cfg, key, b, setID, opts, hooks, out)
}

// UnbindBindingKey releases the binding stored at bindingKey. When bindingKey
// is empty, behaviour matches UnbindWorktree.
func UnbindBindingKey(td *tasks.Deps, pd *project.Deps, cfg *config.Config, bindingKey, setID string, opts UnbindWorktreeOptions, hooks LifecycleHooks, out io.Writer) (UnbindWorktreeResult, error) {
	setID = strings.TrimSpace(setID)
	if setID == "" {
		return UnbindWorktreeResult{}, fmt.Errorf("set id is required")
	}
	bindingKey = strings.TrimSpace(bindingKey)
	if bindingKey == "" {
		return UnbindWorktree(td, pd, cfg, setID, opts, hooks, out)
	}
	if td == nil {
		td = tasks.DefaultDeps()
	}
	if pd == nil {
		pd = project.DefaultDeps()
	}

	b, ok, err := Lookup(td, bindingKey)
	if err != nil {
		return UnbindWorktreeResult{}, err
	}
	if !ok {
		if out == nil {
			out = io.Discard
		}
		fmt.Fprintf(out, "%s has no worktree binding to unbind\n", setID)
		return UnbindWorktreeResult{SetID: setID, Noop: true}, nil
	}
	return unbindResolvedBinding(td, pd, cfg, bindingKey, b, setID, opts, hooks, out)
}

func unbindResolvedBinding(td *tasks.Deps, pd *project.Deps, cfg *config.Config, key string, wt Binding, setID string, opts UnbindWorktreeOptions, hooks LifecycleHooks, out io.Writer) (UnbindWorktreeResult, error) {
	if out == nil {
		out = io.Discard
	}
	if opts.In == nil {
		opts.In = os.Stdin
	}

	lock := readRuntimeLock(hooks, wt.RuntimePath)
	if lock != nil && lock.Locked && !lockedByAnotherSet(lock, setID) {
		return UnbindWorktreeResult{}, fmt.Errorf("%s is currently executing; refusing unbind", setID)
	}

	if hooks.NeedsConfirm != nil {
		needsConfirm, err := hooks.NeedsConfirm(setID, wt)
		if err != nil {
			return UnbindWorktreeResult{}, err
		}
		if needsConfirm {
			prompt := fmt.Sprintf(unbindConfirmPrompt, setID)
			confirmed, err := confirmUnbind(opts.In, out, opts.Yes, prompt)
			if err != nil {
				return UnbindWorktreeResult{}, err
			}
			if !confirmed {
				fmt.Fprintf(out, "Unbind cancelled for %s\n", setID)
				return UnbindWorktreeResult{SetID: setID, Noop: true}, nil
			}
		}
	}

	branch := strings.TrimSpace(wt.Branch)

	if err := Delete(td, key); err != nil {
		return UnbindWorktreeResult{}, err
	}
	if hooks.AfterUnbind != nil {
		if err := hooks.AfterUnbind(key, setID, wt, branch); err != nil {
			return UnbindWorktreeResult{}, err
		}
	}
	fmt.Fprintf(out, "Unbound %s (checkout retained at %s)\n", setID, wt.RuntimePath)
	return UnbindWorktreeResult{SetID: setID}, nil
}

// TeardownAndReleaseManagedBinding removes a managed binding's checkout and
// branch, then forgets the binding association.
func TeardownAndReleaseManagedBinding(td *tasks.Deps, pd *project.Deps, cfg *config.Config, key string, b Binding, hooks LifecycleHooks) error {
	if err := TeardownManagedWorktree(td, pd, cfg, b, hooks); err != nil {
		return err
	}
	return Delete(td, key)
}

// ConfirmManagedWorktreeDelete prompts to delete a managed worktree before
// archive or rebind. yes skips the prompt; a declined answer returns (false, nil).
func ConfirmManagedWorktreeDelete(in io.Reader, out io.Writer, yes bool, runtimePath string) (bool, error) {
	return confirmManagedWorktreeDelete(in, out, yes, runtimePath, "non-interactive archive requires --yes")
}

func confirmManagedWorktreeDelete(in io.Reader, out io.Writer, yes bool, runtimePath, nonInteractiveErr string) (bool, error) {
	prompt := fmt.Sprintf(managedWorktreeDeletePrompt, runtimePath)
	return confirmYesNo(in, out, yes, prompt, nonInteractiveErr)
}

// TeardownManagedWorktree removes a managed binding's checkout and branch.
// It must only be called for provisioned bindings; adopted checkouts are never
// torn down.
func TeardownManagedWorktree(td *tasks.Deps, pd *project.Deps, cfg *config.Config, b Binding, hooks LifecycleHooks) error {
	if td == nil {
		td = tasks.DefaultDeps()
	}
	if pd == nil {
		pd = project.DefaultDeps()
	}
	workingPath, err := resolveTeardownWorkingPath(td, pd, cfg, b, hooks)
	if err != nil {
		return err
	}
	branch := strings.TrimSpace(b.Branch)
	if branch == "" {
		branch, err = resolveRuntimeBranch(td, b.RuntimePath)
		if err != nil {
			return err
		}
	}
	return TeardownWorktree(td, workingPath, b.RuntimePath, branch, true)
}

func resolveRuntimeBranch(td *tasks.Deps, runtimePath string) (string, error) {
	branch, err := td.Git.CommandInDir(runtimePath, "branch", "--show-current")
	if err != nil {
		return "", fmt.Errorf("resolve set branch: %w", err)
	}
	branch = strings.TrimSpace(branch)
	if branch == "" {
		return "", fmt.Errorf("resolve set branch: worktree %s is detached", runtimePath)
	}
	return branch, nil
}

func resolveTeardownWorkingPath(td *tasks.Deps, pd *project.Deps, cfg *config.Config, b Binding, hooks LifecycleHooks) (string, error) {
	if hooks.ResolveTeardownBase != nil {
		return hooks.ResolveTeardownBase(b)
	}
	path, bare, err := ResolveTrunkPath(td, cfg, b.RuntimePath)
	if err != nil {
		return "", err
	}
	if !bare && strings.TrimSpace(path) != "" {
		return path, nil
	}
	if b.Project != "" && cfg != nil && pd != nil {
		projects, err := tasks.ListPickerProjectsWith(pd, cfg)
		if err != nil {
			return "", err
		}
		var matches []string
		for _, p := range projects {
			if p.Name != b.Project {
				continue
			}
			matches = append(matches, p.Path)
		}
		switch len(matches) {
		case 1:
			return matches[0], nil
		case 0:
			return "", fmt.Errorf("project %q for binding is not configured", b.Project)
		default:
			return "", fmt.Errorf("project %q for binding is ambiguous", b.Project)
		}
	}
	return b.RuntimePath, nil
}

func confirmUnbind(in io.Reader, out io.Writer, yes bool, prompt string) (bool, error) {
	if prompt == "" {
		prompt = unbindConfirmPrompt
	}
	return confirmYesNo(in, out, yes, prompt, "non-interactive abandon requires --yes")
}

func confirmYesNo(in io.Reader, out io.Writer, yes bool, prompt, nonInteractiveErr string) (bool, error) {
	if yes {
		return true, nil
	}
	if _, ok := in.(tasks.NonInteractiveReader); ok {
		return false, fmt.Errorf("%s", nonInteractiveErr)
	}
	if in == nil {
		in = os.Stdin
	}
	if in == os.Stdin {
		if f, ok := in.(*os.File); ok {
			info, err := f.Stat()
			if err != nil || info.Mode()&os.ModeCharDevice == 0 {
				return false, fmt.Errorf("%s", nonInteractiveErr)
			}
		}
	}
	fmt.Fprintf(out, "%s", prompt)
	answer, err := asLineReader(in).br.ReadString('\n')
	if err != nil && err != io.EOF {
		return false, fmt.Errorf("read confirmation: %w", err)
	}
	answer = strings.ToLower(strings.TrimSpace(answer))
	return answer == "y" || answer == "yes", nil
}

func asLineReader(in io.Reader) *lineReader {
	if lr, ok := in.(*lineReader); ok {
		return lr
	}
	return &lineReader{br: bufio.NewReader(in)}
}

// lineReader buffers confirmation input so multiple prompts can share one
// underlying reader without bufio read-ahead consuming later answers.
type lineReader struct {
	br *bufio.Reader
}

func (lr *lineReader) Read(p []byte) (int, error) {
	return lr.br.Read(p)
}
