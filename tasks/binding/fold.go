package binding

import (
	"fmt"
	"io"
	"os"
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
	// AgentPreset / AgentCmd select the attended fold-conflict assistance
	// adapter when a merge conflict offers an interactive session.
	AgentPreset string
	AgentCmd    string
}

// FoldResult describes a successful fold.
type FoldResult struct {
	SetID       string
	RuntimePath string
	Branch      string
	TrunkPath   string
	TornDown    bool
}

// Fold merges a finished Task set's branch back into the Trunk worktree and
// releases its checkout (ADR-0148). It merges trunk into the set's branch
// inside the set's own checkout, then advances trunk by fast-forward only.
// On success it releases the Worktree binding and applies reference-counted
// teardown. It never pushes and never archives the set.
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

	key, b, ok, err := FindBySetID(td, setID)
	if err != nil {
		return FoldResult{}, err
	}
	if !ok {
		return FoldResult{}, fmt.Errorf("fold refused: %s has no worktree binding", setID)
	}
	runtimePath := strings.TrimSpace(b.RuntimePath)
	if runtimePath == "" {
		return FoldResult{}, fmt.Errorf("fold refused: %s has no worktree binding", setID)
	}

	trunkPath, bare, err := ResolveTrunkPath(td, cfg, runtimePath)
	if err != nil {
		return FoldResult{}, fmt.Errorf("fold refused: resolve trunk: %w", err)
	}
	if bare || strings.TrimSpace(trunkPath) == "" {
		return FoldResult{}, fmt.Errorf("fold refused: repository has no resolvable Trunk worktree")
	}

	if same, err := sameCheckout(td, runtimePath, trunkPath); err != nil {
		return FoldResult{}, err
	} else if same {
		return FoldResult{}, fmt.Errorf("fold refused: %s is bound to the Trunk worktree itself; nothing to fold", setID)
	}

	if err := requireSetDone(td, cfg, setID, runtimePath); err != nil {
		return FoldResult{}, err
	}

	if dirty, err := worktreeIsDirty(td, runtimePath); err != nil {
		return FoldResult{}, fmt.Errorf("fold refused: check set worktree: %w", err)
	} else if dirty {
		return FoldResult{}, fmt.Errorf("fold refused: set worktree is dirty (%s)", runtimePath)
	}
	if dirty, err := worktreeIsDirty(td, trunkPath); err != nil {
		return FoldResult{}, fmt.Errorf("fold refused: check trunk: %w", err)
	} else if dirty {
		return FoldResult{}, fmt.Errorf("fold refused: Trunk worktree is dirty (%s)", trunkPath)
	}

	if err := refuseLiveClaim(td, "set worktree", runtimePath); err != nil {
		return FoldResult{}, err
	}
	if err := refuseLiveClaim(td, "Trunk worktree", trunkPath); err != nil {
		return FoldResult{}, err
	}

	branch := strings.TrimSpace(b.Branch)
	if branch == "" {
		branch = CurrentBranch(td, runtimePath)
	}
	if branch == "" {
		return FoldResult{}, fmt.Errorf("fold refused: set worktree %s is detached", runtimePath)
	}
	trunkBranch := CurrentBranch(td, trunkPath)
	if trunkBranch == "" {
		return FoldResult{}, fmt.Errorf("fold refused: Trunk worktree %s is detached", trunkPath)
	}

	manifest := loadFoldManifest(td, setID, runtimePath)
	if err := foldMergeAndFastForward(td, cfg, opts, out, foldMergeContext{
		setID:       setID,
		manifest:    manifest,
		setPath:     runtimePath,
		trunkPath:   trunkPath,
		setBranch:   branch,
		trunkBranch: trunkBranch,
	}); err != nil {
		return FoldResult{}, err
	}

	fmt.Fprintf(out, "Folded %s: trunk fast-forwarded onto %s\n", setID, branch)

	if err := Delete(td, key); err != nil {
		return FoldResult{}, fmt.Errorf("fold: release binding: %w", err)
	}
	fmt.Fprintf(out, "Released worktree binding for %s\n", setID)

	tornDown, err := maybeTeardownAfterFold(td, pd, cfg, b, opts.Yes, opts.In, out, hooks)
	if err != nil {
		return FoldResult{}, err
	}

	return FoldResult{
		SetID:       setID,
		RuntimePath: runtimePath,
		Branch:      branch,
		TrunkPath:   trunkPath,
		TornDown:    tornDown,
	}, nil
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

func requireSetDone(td *tasks.Deps, cfg *config.Config, setID, runtimePath string) error {
	id, err := tasks.ResolveRepositoryIdentity(td, runtimePath)
	if err != nil {
		return fmt.Errorf("fold refused: resolve repository: %w", err)
	}
	defPath, err := tasks.CanonicalDefinitionPathWith(td, id.TasksDir)
	if err != nil {
		return fmt.Errorf("fold refused: resolve task storage: %w", err)
	}
	refresh, err := tasks.RefreshWith(td, defPath, tasks.StatePathFor(defPath))
	if err != nil {
		return fmt.Errorf("fold refused: refresh status: %w", err)
	}
	tasks.ApplyVerifyVerdicts(td, refresh, cfg, runtimePath)
	row := tasks.FindRow(refresh, setID)
	if row == nil {
		return fmt.Errorf("fold refused: task set %s is not registered", setID)
	}
	switch row.Status {
	case tasks.StatusDone:
		return nil
	case tasks.StatusNeedsVerify:
		return fmt.Errorf("fold refused: %s is NEEDS-VERIFY; verify or accept first", setID)
	default:
		return fmt.Errorf("fold refused: %s is %s (must be DONE)", setID, row.Status)
	}
}

func refuseLiveClaim(td *tasks.Deps, label, path string) error {
	claim, err := tasks.ReadCheckoutClaim(td, path)
	if err != nil {
		return fmt.Errorf("fold refused: read claim on %s: %w", label, err)
	}
	if claim == nil {
		return nil
	}
	reason := claim.Reason()
	if reason == "" {
		reason = string(claim.Kind)
	}
	holder := strings.TrimSpace(claim.SetID)
	if holder != "" {
		return fmt.Errorf("fold refused: %s has a live claim (%s, held by %s)", label, reason, holder)
	}
	return fmt.Errorf("fold refused: %s has a live claim (%s)", label, reason)
}

type foldMergeContext struct {
	setID       string
	manifest    *tasks.Manifest
	setPath     string
	trunkPath   string
	setBranch   string
	trunkBranch string
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

// foldMergeAndFastForward merges trunk into the set branch inside the set
// checkout, then fast-forwards trunk onto that branch. If trunk moves between
// the merge and the fast-forward, it redoes the merge once and then refuses.
func foldMergeAndFastForward(td *tasks.Deps, cfg *config.Config, opts FoldOptions, out io.Writer, ctx foldMergeContext) error {
	const maxAttempts = 2
	for attempt := 0; attempt < maxAttempts; attempt++ {
		trunkBefore, err := revParseHEAD(td, ctx.trunkPath)
		if err != nil {
			return fmt.Errorf("fold refused: read trunk HEAD: %w", err)
		}

		if err := mergeTrunkIntoSet(td, cfg, opts, out, ctx); err != nil {
			return err
		}

		trunkAfterMerge, err := revParseHEAD(td, ctx.trunkPath)
		if err != nil {
			return fmt.Errorf("fold refused: read trunk HEAD: %w", err)
		}
		if trunkAfterMerge != trunkBefore {
			if attempt+1 < maxAttempts {
				continue
			}
			return errTrunkMovedDuringFold
		}

		if err := fastForwardTrunk(td, ctx.trunkPath, ctx.setBranch); err != nil {
			trunkNow, readErr := revParseHEAD(td, ctx.trunkPath)
			if readErr != nil {
				return fmt.Errorf("fold refused: read trunk HEAD: %w", readErr)
			}
			if trunkNow != trunkBefore {
				if attempt+1 < maxAttempts {
					continue
				}
				return errTrunkMovedDuringFold
			}
			return fmt.Errorf("fold refused: could not fast-forward trunk onto %s: %w", ctx.setBranch, err)
		}
		return nil
	}
	return errTrunkMovedDuringFold
}

var errTrunkMovedDuringFold = fmt.Errorf("fold refused: Trunk worktree moved during fold; redo once already attempted — resolve manually and retry")

func mergeTrunkIntoSet(td *tasks.Deps, cfg *config.Config, opts FoldOptions, out io.Writer, ctx foldMergeContext) error {
	msg := fmt.Sprintf("pop fold: bring trunk (%s) into set branch", ctx.trunkBranch)
	_, err := td.Git.CommandInDir(ctx.setPath, "merge", "--no-edit", "-m", msg, ctx.trunkBranch)
	if err == nil {
		return nil
	}
	if !mergeInProgress(td, ctx.setPath) {
		return fmt.Errorf("fold refused: merge trunk into set branch failed (trunk unchanged): %w", err)
	}
	return tasks.HandleFoldMergeConflict(td, cfg, tasks.FoldConflictContext{
		SetID:       ctx.setID,
		Manifest:    ctx.manifest,
		RuntimePath: ctx.setPath,
		SetBranch:   ctx.setBranch,
		TrunkBranch: ctx.trunkBranch,
		TrunkPath:   ctx.trunkPath,
	}, tasks.FoldConflictAssistanceOptions{
		AgentPreset: opts.AgentPreset,
		AgentCmd:    opts.AgentCmd,
		In:          opts.In,
		Out:         out,
	})
}

func fastForwardTrunk(td *tasks.Deps, trunkPath, setBranch string) error {
	_, err := td.Git.CommandInDir(trunkPath, "merge", "--ff-only", setBranch)
	return err
}

func mergeInProgress(td *tasks.Deps, path string) bool {
	_, err := td.Git.CommandInDir(path, "rev-parse", "-q", "--verify", "MERGE_HEAD")
	return err == nil
}

func revParseHEAD(td *tasks.Deps, path string) (string, error) {
	out, err := td.Git.CommandInDir(path, "rev-parse", "HEAD")
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
