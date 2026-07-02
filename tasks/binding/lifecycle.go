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

const foregroundManagedRebindPrompt = "rebind to current and DELETE managed worktree %s? [y/N]: "

// BindWorktreeOptions controls bind-worktree behaviour.
type BindWorktreeOptions struct {
	Force bool
	// ProjectName, when non-empty, is used verbatim as the binding's Project
	// label and skips DetectProject entirely. Callers that already resolved the
	// name fork-free (the dashboard, ADR-0060) supply it; cwd-based callers
	// leave it empty to fall back to DetectProject.
	ProjectName string
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

	branch, err := resolveRuntimeBranch(td, checkoutPath)
	if err != nil {
		return BindWorktreeResult{}, fmt.Errorf("resolve branch in %s: %w", checkoutPath, err)
	}

	id, err := tasks.ResolveRepositoryIdentity(td, checkoutPath)
	if err != nil {
		return BindWorktreeResult{}, fmt.Errorf("resolve repository identity: %w", err)
	}
	key := Key(id, setID)

	store, err := Load(td)
	if err != nil {
		return BindWorktreeResult{}, err
	}

	var replaced bool
	if existing, ok := store.Get(key); ok {
		lock := readRuntimeLock(hooks, existing.RuntimePath)
		if lock != nil && lock.Locked {
			if lock.Metadata != nil && lock.Metadata.SetID != "" && lock.Metadata.SetID != setID {
				return BindWorktreeResult{}, fmt.Errorf("refusing bind-worktree: %s runtime checkout is locked for another set (%s)", setID, lock.Metadata.SetID)
			}
			return BindWorktreeResult{}, fmt.Errorf("refusing bind-worktree: %s is currently executing", setID)
		}
		existingCanon, _ := canonicalCheckoutPath(td, existing.RuntimePath)
		newCanon, _ := canonicalCheckoutPath(td, checkoutPath)
		if existingCanon != newCanon {
			if !opts.Force {
				return BindWorktreeResult{}, fmt.Errorf("%s is already bound to %s; use --force to re-point", setID, existing.RuntimePath)
			}
			replaced = true
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

	store, err := Load(td)
	if err != nil {
		return UnbindWorktreeResult{}, err
	}
	b, ok := store.Get(bindingKey)
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
	if lock != nil && lock.Locked {
		if lock.Metadata != nil && lock.Metadata.SetID != "" && lock.Metadata.SetID != setID {
			return UnbindWorktreeResult{}, fmt.Errorf("%s runtime checkout is locked for another set (%s); refusing unbind", setID, lock.Metadata.SetID)
		}
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
// archive. yes skips the prompt; a declined answer returns (false, nil).
func ConfirmManagedWorktreeDelete(in io.Reader, out io.Writer, yes bool, runtimePath string) (bool, error) {
	prompt := fmt.Sprintf(managedWorktreeDeletePrompt, runtimePath)
	return confirmYesNo(in, out, yes, prompt, "non-interactive archive requires --yes")
}

// ConfirmForegroundManagedRebind prompts before tearing down an idle managed
// binding so a foreground implement can rebind the set to the current checkout.
// yes skips the prompt; a declined answer returns (false, nil).
func ConfirmForegroundManagedRebind(in io.Reader, out io.Writer, yes bool, runtimePath string) (bool, error) {
	prompt := fmt.Sprintf(foregroundManagedRebindPrompt, runtimePath)
	return confirmYesNo(in, out, yes, prompt, "non-interactive implement requires --yes to delete managed worktree when rebinding")
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
	answer, err := bufio.NewReader(in).ReadString('\n')
	if err != nil && err != io.EOF {
		return false, fmt.Errorf("read confirmation: %w", err)
	}
	answer = strings.ToLower(strings.TrimSpace(answer))
	return answer == "y" || answer == "yes", nil
}
