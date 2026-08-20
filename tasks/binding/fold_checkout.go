package binding

import (
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/glebglazov/pop/config"
	"github.com/glebglazov/pop/tasks"
)

// FoldCheckoutResult describes a successful checkout-addressed fold.
type FoldCheckoutResult struct {
	RuntimePath string
	Branch      string
	TrunkPath   string
}

// FoldCheckout folds one checkout: it rebases that checkout's branch onto the
// Trunk worktree's branch and then advances trunk by fast-forward only
// (ADR-0229). This is fold's primitive — it owns trunk resolution, every
// path-level refusal, the rebase, the Fold conflict prompt and the
// fast-forward, and knows nothing of Task sets. `Fold` is the set-addressed
// specialization: it resolves a set to its checkout, adds the promises a set
// owes, and delegates the git work here.
//
// It never pushes and never fetches. A Fold conflict prompt "retry" aborts the
// in-flight rebase and restarts from preflight.
func FoldCheckout(td *tasks.Deps, cfg *config.Config, path string, opts FoldOptions, out io.Writer) (FoldCheckoutResult, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return FoldCheckoutResult{}, fmt.Errorf("checkout path is required")
	}
	if td == nil {
		td = tasks.DefaultDeps()
	}
	if out == nil {
		out = io.Discard
	}
	if opts.In == nil {
		opts.In = os.Stdin
	}

	for {
		plan, err := preflightFoldCheckout(td, cfg, foldCheckoutRequest{path: path})
		if err != nil {
			return FoldCheckoutResult{}, err
		}
		if err := foldRebaseAndFastForward(td, cfg, opts, out, plan.rebaseContext(nil)); err != nil {
			if errors.Is(err, tasks.ErrFoldRetry) {
				continue
			}
			return FoldCheckoutResult{}, err
		}
		fmt.Fprintf(out, "Folded %s: trunk fast-forwarded onto %s\n", plan.subject, plan.branch)
		return FoldCheckoutResult{
			RuntimePath: plan.path,
			Branch:      plan.branch,
			TrunkPath:   plan.trunkPath,
		}, nil
	}
}

// foldCheckoutRequest addresses a fold at one checkout.
type foldCheckoutRequest struct {
	path string
	// setID names the Task set that addressed this fold, when one did: it makes
	// the refusals blame the set and hands the conflict prompt its set context.
	setID string
	// branch pre-empts reading the checkout's current branch, as a set's binding
	// records the branch it was bound at.
	branch string
	// gate runs once the checkout has resolved to something other than trunk and
	// before any cleanliness check — where the set-addressed fold's status gate
	// has always sat, so a set's refusals keep their precedence.
	gate func() error
}

// foldCheckoutPlan is a checkout whose fold has passed every path-level
// refusal: both ends resolved to a clean, unclaimed, attached checkout.
type foldCheckoutPlan struct {
	// subject names this fold in its refusals and in its success line: the set
	// id when a set addressed it, otherwise the checkout's own path.
	subject     string
	setID       string
	path        string
	trunkPath   string
	branch      string
	trunkBranch string
}

// rebaseContext hands the git work what it needs. manifest is the addressing
// set's manifest, which the Fold conflict prompt reads, and nil when no set
// addressed this fold.
func (p foldCheckoutPlan) rebaseContext(manifest *tasks.Manifest) foldRebaseContext {
	return foldRebaseContext{
		setID:       p.setID,
		manifest:    manifest,
		setPath:     p.path,
		trunkPath:   p.trunkPath,
		setBranch:   p.branch,
		trunkBranch: p.trunkBranch,
	}
}

// foldCheckoutLabel names the folding end of a fold in its refusals. A set-addressed
// fold has always called it the set worktree; a bare checkout answers for itself.
func foldCheckoutLabel(setID string) string {
	if strings.TrimSpace(setID) == "" {
		return "checkout"
	}
	return "set worktree"
}

func preflightFoldCheckout(td *tasks.Deps, cfg *config.Config, req foldCheckoutRequest) (foldCheckoutPlan, error) {
	path := strings.TrimSpace(req.path)
	if path == "" {
		return foldCheckoutPlan{}, fmt.Errorf("fold refused: no checkout to fold")
	}
	if td == nil {
		td = tasks.DefaultDeps()
	}
	setID := strings.TrimSpace(req.setID)
	subject := setID
	if subject == "" {
		subject = path
	}
	label := foldCheckoutLabel(setID)

	trunkPath, bare, err := ResolveTrunkPath(td, cfg, path)
	if err != nil {
		return foldCheckoutPlan{}, fmt.Errorf("fold refused: resolve trunk: %w", err)
	}
	if bare || strings.TrimSpace(trunkPath) == "" {
		return foldCheckoutPlan{}, fmt.Errorf("fold refused: repository has no resolvable Trunk worktree")
	}

	if same, err := sameCheckout(td, path, trunkPath); err != nil {
		return foldCheckoutPlan{}, err
	} else if same {
		if setID != "" {
			return foldCheckoutPlan{}, fmt.Errorf("fold refused: %s is bound to the Trunk worktree itself; nothing to fold", subject)
		}
		return foldCheckoutPlan{}, fmt.Errorf("fold refused: %s is the Trunk worktree itself; nothing to fold", subject)
	}

	if req.gate != nil {
		if err := req.gate(); err != nil {
			return foldCheckoutPlan{}, err
		}
	}

	// A fold rebase left in progress makes the folding checkout dirty; re-entering
	// Fold must route to the Fold conflict prompt instead of the dirty refusal.
	parkedRebase := rebaseInProgress(td, path)
	if dirty, err := worktreeIsDirty(td, path); err != nil {
		return foldCheckoutPlan{}, fmt.Errorf("fold refused: check %s: %w", label, err)
	} else if dirty && !parkedRebase {
		return foldCheckoutPlan{}, fmt.Errorf("fold refused: %s is dirty (%s)", label, path)
	}
	if dirty, err := worktreeIsDirty(td, trunkPath); err != nil {
		return foldCheckoutPlan{}, fmt.Errorf("fold refused: check trunk: %w", err)
	} else if dirty {
		return foldCheckoutPlan{}, fmt.Errorf("fold refused: Trunk worktree is dirty (%s)", trunkPath)
	}

	if err := refuseLiveClaim(td, label, path); err != nil {
		return foldCheckoutPlan{}, err
	}
	if err := refuseLiveClaim(td, "Trunk worktree", trunkPath); err != nil {
		return foldCheckoutPlan{}, err
	}

	branch := strings.TrimSpace(req.branch)
	if branch == "" {
		branch = CurrentBranch(td, path)
	}
	if branch == "" {
		return foldCheckoutPlan{}, fmt.Errorf("fold refused: %s %s is detached", label, path)
	}
	trunkBranch := CurrentBranch(td, trunkPath)
	if trunkBranch == "" {
		return foldCheckoutPlan{}, fmt.Errorf("fold refused: Trunk worktree %s is detached", trunkPath)
	}

	return foldCheckoutPlan{
		subject:     subject,
		setID:       setID,
		path:        path,
		trunkPath:   trunkPath,
		branch:      branch,
		trunkBranch: trunkBranch,
	}, nil
}
