package binding

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/glebglazov/pop/config"
	"github.com/glebglazov/pop/project"
	"github.com/glebglazov/pop/tasks"
)

// FoldOptions controls confirmation for managed-worktree teardown after a
// successful fold. Yes skips only that confirmation (ADR-0148).
type FoldOptions struct {
	Yes bool
	In  io.Reader
	// ConfirmCheckoutFold asks the checkout primitive to confirm the landing.
	// The Task-set specialization has its own confirmations and leaves it false.
	ConfirmCheckoutFold bool
	// AgentPreset / AgentCmd select the attended fold-conflict assistance
	// adapter when a rebase conflict offers an interactive session.
	AgentPreset string
	AgentCmd    string
}

func confirmCheckoutFold(in io.Reader, out io.Writer, yes bool, path string) (bool, error) {
	prompt := fmt.Sprintf("Fold worktree %s into trunk? [y/N]: ", path)
	return confirmYesNo(in, out, yes, prompt, "non-interactive worktree fold requires --yes")
}

// FoldResult describes a successful fold.
type FoldResult struct {
	SetID       string
	RuntimePath string
	Branch      string
	TrunkPath   string
	TornDown    bool
}

// Fold folds the checkout a finished Task set is bound to, and releases that
// binding (ADR-0148, ADR-0156, ADR-0229). It is the set-addressed
// specialization of FoldCheckout: it resolves the set to a checkout, adds the
// promises a *set* owes — status eligibility, the Awaiting-approval sign-off,
// binding release, reference-counted teardown — and leaves every git act,
// including the retry loop and the conflict prompt, to the primitive. It never
// pushes, never fetches, and never archives the set. A Fold conflict prompt
// "retry" restarts the primitive from preflight, which is why the sign-off is
// asked per attempt rather than once around the fold.
func Fold(td *tasks.Deps, pd *project.Deps, cfg *config.Config, setID string, opts FoldOptions, hooks LifecycleHooks, out io.Writer) (FoldResult, error) {
	setID = strings.TrimSpace(setID)
	if setID == "" {
		return FoldResult{}, fmt.Errorf("set id is required")
	}
	if td == nil {
		td = tasks.DefaultDeps()
	}
	if pd == nil {
		pd = project.DefaultDeps()
	}
	if out == nil {
		out = io.Discard
	}
	if opts.In == nil {
		opts.In = os.Stdin
	}

	key, b, err := resolveFoldBinding(td, setID)
	if err != nil {
		return FoldResult{}, err
	}

	// The primitive owns the attempt loop, so both hooks below may run more than
	// once: status is re-read by the gate on every attempt, and the sign-off is
	// re-asked with it.
	var status foldSetStatus
	req := foldSetRequest(td, cfg, setID, b, &status)
	req.afterPreflight = func() (*tasks.Manifest, error) {
		if status.status == tasks.StatusAwaitingApproval {
			confirmed, err := confirmFoldSignOff(opts.In, out, opts.Yes, status.openHITL)
			if err != nil {
				return nil, err
			}
			if !confirmed {
				return nil, fmt.Errorf("fold cancelled")
			}
		}
		return loadFoldManifest(td, setID, b.RuntimePath), nil
	}
	req.afterLanding = func() error {
		return completeFoldSetTail(td, setID, b.RuntimePath, key, status.status)
	}

	res, err := foldCheckout(td, cfg, req, opts, out)
	if err != nil {
		return FoldResult{}, err
	}
	fmt.Fprintf(out, "Released worktree binding for %s\n", setID)

	tornDown, err := maybeTeardownAfterFold(td, pd, cfg, b, opts.Yes, opts.In, out, hooks)
	if err != nil {
		return FoldResult{}, err
	}

	return FoldResult{
		SetID:       setID,
		RuntimePath: res.RuntimePath,
		Branch:      res.Branch,
		TrunkPath:   res.TrunkPath,
		TornDown:    tornDown,
	}, nil
}

// completeFoldSetTail is everything a set-addressed fold owes once its work is in
// trunk: the Awaiting-approval sign-off and the release of the binding. It runs
// while the Fold scratch branch still stands, so a failure here leaves the ref that
// tells the next fold the work landed and the set was never finished — which is what
// makes a re-run converge instead of meeting the already-contained refusal
// (ADR-0229). Re-entry is safe: a completed sign-off has no open HITL task left to
// complete, and the set's status is re-read before it is asked for again.
func completeFoldSetTail(td *tasks.Deps, setID, runtimePath, key string, status tasks.TaskSetStatus) error {
	if status == tasks.StatusAwaitingApproval {
		manifest := loadFoldManifest(td, setID, runtimePath)
		if manifest == nil || !manifest.Valid {
			return fmt.Errorf("fold: reload manifest for sign-off: task set %s is malformed or missing", setID)
		}
		if err := tasks.CompleteFoldSignOff(td, manifest, runtimePath); err != nil {
			return fmt.Errorf("fold: complete sign-off: %w", err)
		}
	}
	if err := Delete(td, key); err != nil {
		return fmt.Errorf("fold: release binding: %w", err)
	}
	return nil
}

// PreflightFold applies the same precondition checks as Fold without rebasing
// or releasing the binding. Dashboard and CLI surfaces use it to refuse early
// with the same messages Fold would return. It is asked on the way into a fold,
// never to paint a row, which is why it may still reconcile one thing it finds:
// a Fold scratch branch left by a fold that died after its work landed is
// left intact here when trunk already holds it. The actual Fold consumes that
// Git-owned signal after it enters the post-landing path (ADR-0229).
func PreflightFold(td *tasks.Deps, cfg *config.Config, setID string) error {
	if td == nil {
		td = tasks.DefaultDeps()
	}
	setID = strings.TrimSpace(setID)
	_, b, err := resolveFoldBinding(td, setID)
	if err != nil {
		return err
	}
	var status foldSetStatus
	_, err = preflightFoldCheckout(td, cfg, foldSetRequest(td, cfg, setID, b, &status))
	return err
}

// resolveFoldBinding names the checkout a set's fold acts on. It is the one
// refusal that precedes every path-level check, because without a binding there is
// no checkout to ask anything about.
func resolveFoldBinding(td *tasks.Deps, setID string) (string, Binding, error) {
	setID = strings.TrimSpace(setID)
	if setID == "" {
		return "", Binding{}, fmt.Errorf("set id is required")
	}
	key, b, ok, err := FindBySetID(td, setID)
	if err != nil {
		return "", Binding{}, err
	}
	if !ok || strings.TrimSpace(b.RuntimePath) == "" {
		return "", Binding{}, fmt.Errorf("fold refused: %s has no worktree binding", setID)
	}
	return key, b, nil
}

// foldSetRequest addresses the primitive at the checkout a set is bound to. The
// status gate rides in as the primitive's gate so it keeps refusing where it always
// has: after the checkout resolves to something other than trunk, before either end
// is checked for cleanliness. status is written through so the caller can read the
// status each attempt saw.
func foldSetRequest(td *tasks.Deps, cfg *config.Config, setID string, b Binding, status *foldSetStatus) foldCheckoutRequest {
	runtimePath := strings.TrimSpace(b.RuntimePath)
	return foldCheckoutRequest{
		path:   runtimePath,
		setID:  setID,
		branch: strings.TrimSpace(b.Branch),
		gate: func() error {
			resolved, err := resolveFoldSetStatus(td, cfg, setID, runtimePath)
			if err != nil {
				return err
			}
			*status = resolved
			return nil
		},
	}
}

func sameCheckout(td *tasks.Deps, a, b string) (bool, error) {
	ca, err := canonicalCheckoutPath(td, a)
	if err != nil {
		return false, err
	}
	cb, err := canonicalCheckoutPath(td, b)
	if err != nil {
		return false, err
	}
	return ca == cb, nil
}

type foldSetStatus struct {
	status   tasks.TaskSetStatus
	openHITL []tasks.Task
}

// foldSetRowStatus reads one set's status the way every fold path needs it —
// refreshed from disk with verify verdicts applied — and hands back the manifest a
// sign-off would write through. Its errors are the ones no fold can read past: the
// set's storage, never the set's state, which is the caller's to judge.
func foldSetRowStatus(td *tasks.Deps, cfg *config.Config, setID, runtimePath string) (tasks.TaskSetStatus, *tasks.Manifest, error) {
	id, err := tasks.ResolveRepositoryIdentity(td, runtimePath)
	if err != nil {
		return "", nil, fmt.Errorf("fold refused: resolve repository: %w", err)
	}
	defPath, err := tasks.CanonicalDefinitionPathWith(td, id.TasksDir)
	if err != nil {
		return "", nil, fmt.Errorf("fold refused: resolve task storage: %w", err)
	}
	refresh, err := tasks.RefreshWith(td, defPath, tasks.StatePathFor(defPath))
	if err != nil {
		return "", nil, fmt.Errorf("fold refused: refresh status: %w", err)
	}
	tasks.ApplyVerifyVerdicts(td, refresh, cfg, runtimePath)
	row := tasks.FindRow(refresh, setID)
	if row == nil {
		return "", nil, fmt.Errorf("fold refused: task set %s is not registered", setID)
	}
	return row.Status, refresh.Manifests[setID], nil
}

// resolveFoldSetStatus is the set-addressed fold's status gate: everything outside
// DONE and Awaiting-approval refuses here, because a set-addressed fold is the act
// of finishing that set and an unfinished set has nothing to finish. The
// checkout-addressed fold reads the same status and asks instead (ADR-0233).
func resolveFoldSetStatus(td *tasks.Deps, cfg *config.Config, setID, runtimePath string) (foldSetStatus, error) {
	status, m, err := foldSetRowStatus(td, cfg, setID, runtimePath)
	if err != nil {
		return foldSetStatus{}, err
	}
	switch status {
	case tasks.StatusDone:
		return foldSetStatus{status: tasks.StatusDone}, nil
	case tasks.StatusAwaitingApproval:
		if m == nil || !m.Valid {
			return foldSetStatus{}, fmt.Errorf("fold refused: task set %s is malformed", setID)
		}
		return foldSetStatus{status: tasks.StatusAwaitingApproval, openHITL: tasks.OpenHITLTasks(m)}, nil
	case tasks.StatusNeedsVerify:
		return foldSetStatus{}, fmt.Errorf("fold refused: %s is NEEDS-VERIFY; verify or accept first", setID)
	default:
		return foldSetStatus{}, fmt.Errorf("fold refused: %s is %s (must be DONE)", setID, status)
	}
}

func confirmFoldSignOff(in io.Reader, out io.Writer, yes bool, hitl []tasks.Task) (bool, error) {
	if len(hitl) == 0 {
		return true, nil
	}
	prompt := tasks.FormatFoldSignOffConfirmation(hitl) + "\nProceed? [y/N]: "
	return confirmYesNo(in, out, yes, prompt, "non-interactive fold of an AWAITING-APPROVAL set requires --yes")
}

// refuseDirtyTrunk is the refusal trunk earns for uncommitted work. It is a
// function rather than an inline check because the fold asks it twice — once in
// preflight and once on the edge of the fast-forward — and both must say it the
// same way.
func refuseDirtyTrunk(td *tasks.Deps, trunkPath string) error {
	dirty, err := worktreeIsDirty(td, trunkPath)
	if err != nil {
		return fmt.Errorf("fold refused: check trunk: %w", err)
	}
	if dirty {
		return fmt.Errorf("fold refused: Trunk worktree is dirty (%s)", trunkPath)
	}
	return nil
}

func refuseLiveClaim(td *tasks.Deps, label, path string) error {
	claim, err := tasks.ReadCheckoutClaim(td, path)
	if err != nil {
		return fmt.Errorf("fold refused: read claim on %s: %w", label, err)
	}
	if claim == nil {
		return nil
	}
	reason := claim.Reason.Phrase()
	holder := strings.TrimSpace(claim.Holder.ContainerID)
	if holder != "" {
		return fmt.Errorf("fold refused: %s has a live claim (%s, held by %s)", label, reason, holder)
	}
	return fmt.Errorf("fold refused: %s has a live claim (%s)", label, reason)
}

func loadFoldManifest(td *tasks.Deps, setID, runtimePath string) *tasks.Manifest {
	id, err := tasks.ResolveRepositoryIdentity(td, runtimePath)
	if err != nil {
		return nil
	}
	defPath, err := tasks.CanonicalDefinitionPathWith(td, id.TasksDir)
	if err != nil {
		return nil
	}
	disc, err := tasks.DiscoverWith(td, defPath)
	if err != nil {
		return nil
	}
	manifestPath, ok := disc.Manifests[setID]
	if !ok {
		return nil
	}
	return tasks.LoadManifest(td, setID, manifestPath)
}

func rebaseInProgress(td *tasks.Deps, path string) bool {
	return rebaseStateDirPresent(td, path)
}

// rebaseStateDirPresent reports whether git has a rebase-merge or rebase-apply
// directory in the checkout. REBASE_HEAD alone is not reliable: git may leave
// that ref after a successful rebase.
func rebaseStateDirPresent(td *tasks.Deps, path string) bool {
	for _, name := range []string{"rebase-merge", "rebase-apply"} {
		out, err := td.Git.CommandInDir(path, "rev-parse", "--git-path", name)
		if err != nil {
			continue
		}
		p := strings.TrimSpace(out)
		if p == "" {
			continue
		}
		if !filepath.IsAbs(p) {
			p = filepath.Join(path, p)
		}
		if st, err := os.Stat(p); err == nil && st.IsDir() {
			return true
		}
	}
	return false
}

func revParseHEAD(td *tasks.Deps, path string) (string, error) {
	return revParseRef(td, path, "HEAD")
}

func revParseRef(td *tasks.Deps, path, ref string) (string, error) {
	out, err := td.Git.CommandInDir(path, "rev-parse", ref)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(out), nil
}

func maybeTeardownAfterFold(td *tasks.Deps, pd *project.Deps, cfg *config.Config, b Binding, yes bool, in io.Reader, out io.Writer, hooks LifecycleHooks) (bool, error) {
	// Binding already released; count live referents without an exclude key.
	should, err := shouldOfferManagedCheckoutTeardown(td, b.RuntimePath, nil)
	if err != nil {
		return false, err
	}
	if !should {
		return false, nil
	}
	confirmed, err := confirmManagedWorktreeDelete(in, out, yes, b.RuntimePath,
		"non-interactive fold requires --yes to delete managed worktree")
	if err != nil {
		return false, err
	}
	if !confirmed {
		fmt.Fprintf(out, "Kept managed worktree at %s\n", b.RuntimePath)
		return false, nil
	}
	if err := TeardownManagedWorktree(td, pd, cfg, b, hooks); err != nil {
		return false, err
	}
	fmt.Fprintf(out, "Removed managed worktree at %s\n", b.RuntimePath)
	return true, nil
}
