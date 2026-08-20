package binding

import (
	"errors"
	"fmt"
	"io"
	"os/exec"
	"strings"
	"time"

	"github.com/glebglazov/pop/config"
	"github.com/glebglazov/pop/tasks"
)

// foldRebaseContext is what the fold's git work needs: both ends of the fold and
// the branch on each. manifest is the addressing set's manifest, which the Fold
// conflict prompt reads, and nil when no set addressed the fold.
type foldRebaseContext struct {
	setID       string
	manifest    *tasks.Manifest
	setPath     string
	trunkPath   string
	setBranch   string
	trunkBranch string
}

// foldScratchBranch names the Fold scratch branch for a branch: `pop/fold/<branch>`
// with `/` flattened to `-`. The flattening is git's own constraint — it cannot
// hold `pop/fold/a` and `pop/fold/a/b` at once, one being a file where the other
// needs a directory. The name carries no timestamp and no uniquifying suffix on
// purpose: a re-run must compute the same ref, which is what lets a stopped fold be
// read back out of git rather than out of a journal (ADR-0229).
func foldScratchBranch(branch string) string {
	return foldScratchPrefix + strings.ReplaceAll(strings.TrimSpace(branch), "/", "-")
}

// foldScratchPrefix is what marks a ref as fold's own to rewrite and to delete —
// recovery reads it off a parked rebase to know the rebase in flight is a fold's.
const foldScratchPrefix = "pop/fold/"

// foldRebaseAndFastForward does the fold's git work on a Fold scratch branch, so
// the branch it was asked to fold is rewritten by nothing (ADR-0229): the scratch
// branch is created at that branch's pre-fold tip, rebased onto trunk, and landed
// in trunk by fast-forward; only then does the real branch move — once, by a forced
// ref update, to a tip trunk already carries. If trunk moves between the rebase and
// the fast-forward, the scratch branch is reset to the recorded tip and the rebase
// redone once; a second move refuses.
func foldRebaseAndFastForward(td *tasks.Deps, cfg *config.Config, opts FoldOptions, out io.Writer, ctx foldRebaseContext) error {
	scratch := foldScratchBranch(ctx.setBranch)
	// The pre-fold tip is read from the branch, never journalled: the branch does not
	// move until the work has landed, so a re-entered fold reads the same commit the
	// first attempt did.
	tip, err := revParseRef(td, ctx.setPath, ctx.setBranch)
	if err != nil {
		return fmt.Errorf("fold refused: read tip of %s: %w", ctx.setBranch, err)
	}

	const maxAttempts = 2
	for attempt := 0; attempt < maxAttempts; attempt++ {
		trunkBefore, err := revParseHEAD(td, ctx.trunkPath)
		if err != nil {
			return fmt.Errorf("fold refused: read trunk HEAD: %w", err)
		}

		if err := rebaseScratchOntoTrunk(td, cfg, opts, out, ctx, scratch, tip); err != nil {
			return err
		}

		trunkAfterRebase, err := revParseHEAD(td, ctx.trunkPath)
		if err != nil {
			return refuseAndAbandon(td, ctx, scratch,
				fmt.Errorf("fold refused: read trunk HEAD: %w", err))
		}
		if trunkAfterRebase != trunkBefore {
			if attempt+1 < maxAttempts {
				continue
			}
			return refuseAndAbandon(td, ctx, scratch, errTrunkMovedDuringFold)
		}

		// Conflict resolution can run for an hour after preflight looked, so trunk
		// answers for itself once more immediately before the one irreversible act.
		if err := refuseTrunkUnfitToLand(td, ctx.trunkPath); err != nil {
			return refuseAndAbandon(td, ctx, scratch, err)
		}

		foldedTip, err := revParseRef(td, ctx.setPath, scratch)
		if err != nil {
			return refuseAndAbandon(td, ctx, scratch,
				fmt.Errorf("fold refused: read folded tip %s: %w", scratch, err))
		}
		if err := fastForwardTrunk(td, ctx.trunkPath, scratch); err != nil {
			state, recoveryErr := classifyFailedFastForward(td, ctx, scratch, foldedTip, trunkBefore)
			if recoveryErr != nil {
				return fmt.Errorf("fold stopped after git could not report whether the failed fast-forward landed; the fold scratch branch %s is preserved so trunk is not unwound — retry when git can read the repository: %w", scratch, recoveryErr)
			}
			switch state {
			case failedFastForwardLanded:
				return landFoldedBranch(td, ctx, scratch)
			case failedFastForwardTrunkMoved:
				if attempt+1 < maxAttempts {
					continue
				}
				return refuseAndAbandon(td, ctx, scratch, errTrunkMovedDuringFold)
			case failedFastForwardNotLanded:
				return refuseAndAbandon(td, ctx, scratch,
					fmt.Errorf("fold refused: could not fast-forward trunk onto %s: %w", ctx.setBranch, err))
			}
		}
		return landFoldedBranch(td, ctx, scratch)
	}
	return errTrunkMovedDuringFold
}

type failedFastForwardState int

const (
	failedFastForwardNotLanded failedFastForwardState = iota
	failedFastForwardLanded
	failedFastForwardTrunkMoved
)

// classifyFailedFastForward locates a failed merge command on Fold's irreversible
// boundary. Git can update trunk and then report a hook failure, so the command's
// error is not evidence that the fold stayed before the boundary. The refs decide:
// an unchanged trunk did not land, while a changed trunk that reaches the folded
// tip did. A different changed trunk is the existing concurrent-move case.
func classifyFailedFastForward(td *tasks.Deps, ctx foldRebaseContext, scratch, foldedTip, trunkBefore string) (failedFastForwardState, error) {
	var lastErr error
	for attempt := 0; attempt < foldPostLandingAttempts; attempt++ {
		if attempt > 0 {
			time.Sleep(foldPostLandingRetryDelay)
		}
		trunkNow, err := revParseHEAD(td, ctx.trunkPath)
		if err != nil {
			lastErr = fmt.Errorf("read trunk HEAD: %w", err)
			continue
		}
		if trunkNow == trunkBefore {
			return failedFastForwardNotLanded, nil
		}
		if trunkNow == foldedTip {
			return failedFastForwardLanded, nil
		}
		contains, err := refContainsKnown(td, ctx.trunkPath, ctx.trunkBranch, scratch)
		if err != nil {
			lastErr = fmt.Errorf("check whether trunk contains %s: %w", scratch, err)
			continue
		}
		if contains {
			return failedFastForwardLanded, nil
		}
		return failedFastForwardTrunkMoved, nil
	}
	return failedFastForwardNotLanded, lastErr
}

// refContainsKnown is refContains with an error-bearing third state. Git uses exit
// status 1 for the ordinary "not an ancestor" answer; every other failure means
// Fold cannot safely choose a side of the landing boundary.
func refContainsKnown(td *tasks.Deps, path, container, ref string) (bool, error) {
	_, err := td.Git.CommandInDir(path, "merge-base", "--is-ancestor", ref, container)
	if err == nil {
		return true, nil
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) && exitErr.ExitCode() == 1 {
		return false, nil
	}
	return false, err
}

var errTrunkMovedDuringFold = fmt.Errorf("fold refused: Trunk worktree moved during fold; redo once already attempted — resolve manually and retry")

// rebaseScratchOntoTrunk stands the folding checkout on the Fold scratch branch at
// the real branch's pre-fold tip and rebases that instead. A rebase already in
// progress is a parked fold — the scratch branch is checked out mid-rewrite, so
// re-creating it would throw away a resolution in flight — and goes straight to the
// Fold conflict prompt (ADR-0156).
func rebaseScratchOntoTrunk(td *tasks.Deps, cfg *config.Config, opts FoldOptions, out io.Writer, ctx foldRebaseContext, scratch, tip string) error {
	if !rebaseInProgress(td, ctx.setPath) {
		// `-B` is create-or-reset, which makes a first attempt and a redo after trunk
		// moved the same act; standing on the scratch branch is also what later lets the
		// real branch be forced, since git refuses to force a branch that is checked out.
		if _, err := td.Git.CommandInDir(ctx.setPath, "checkout", "-B", scratch, tip); err != nil {
			return fmt.Errorf("fold refused: create fold scratch branch %s: %w", scratch, err)
		}
		_, err := td.Git.CommandInDir(ctx.setPath, "rebase", ctx.trunkBranch)
		if err == nil {
			return nil
		}
		if !rebaseInProgress(td, ctx.setPath) {
			return refuseAndAbandon(td, ctx, scratch,
				fmt.Errorf("fold refused: rebase set branch onto trunk failed (trunk unchanged): %w", err))
		}
	}
	err := tasks.HandleFoldConflict(td, cfg, tasks.FoldConflictContext{
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
	if errors.Is(err, tasks.ErrFoldRetry) || errors.Is(err, tasks.ErrFoldAbandon) {
		// Both aborted the rebase and left the checkout standing on the scratch branch,
		// and neither may leave that ref behind: retry starts the verb again from
		// preflight, abandon stops for good. Only "exit" parks.
		abandonFoldScratch(td, ctx, scratch)
	}
	return err
}

func fastForwardTrunk(td *tasks.Deps, trunkPath, branch string) error {
	_, err := td.Git.CommandInDir(trunkPath, "merge", "--ff-only", branch)
	return err
}

// refuseTrunkUnfitToLand is trunk's half of preflight, asked again on the edge of
// the fast-forward. It refuses in preflight's own words, because a trunk that went
// dirty or got claimed mid-fold is the same situation preflight already names.
func refuseTrunkUnfitToLand(td *tasks.Deps, trunkPath string) error {
	if err := refuseDirtyTrunk(td, trunkPath); err != nil {
		return err
	}
	return refuseLiveClaim(td, "Trunk worktree", trunkPath)
}

// landFoldedBranch runs after trunk already carries the work, so it is past the
// Fold boundary: the real branch force-moves to the folded tip — its first and only
// move — the checkout returns to it, and the scratch branch goes. These are local
// ref updates on landed work, so each is retried a bounded number of times and a
// failure that outlasts that is reported for exactly what it is. Trunk is never
// unwound; running fold again converges on the same end state (ADR-0229).
func landFoldedBranch(td *tasks.Deps, ctx foldRebaseContext, scratch string) error {
	if !refContains(td, ctx.setPath, ctx.setBranch, scratch) {
		if err := retryAfterLanding(func() error {
			_, err := td.Git.CommandInDir(ctx.setPath, "branch", "-f", ctx.setBranch, scratch)
			return err
		}); err != nil {
			return fmt.Errorf("fold landed in trunk: trunk holds the work, %s does not — could not move it onto the folded tip %s after %d attempts; trunk stays as it is, so folding again finishes the job: %w",
				ctx.setBranch, scratch, foldPostLandingAttempts, err)
		}
	}
	if CurrentBranch(td, ctx.setPath) != ctx.setBranch {
		if err := retryAfterLanding(func() error {
			_, err := td.Git.CommandInDir(ctx.setPath, "checkout", ctx.setBranch)
			return err
		}); err != nil {
			return fmt.Errorf("fold landed in trunk and %s holds the work, but %s is still standing on the fold scratch branch %s after %d attempts: %w",
				ctx.setBranch, ctx.setPath, scratch, foldPostLandingAttempts, err)
		}
	}
	if err := retryAfterLanding(func() error {
		_, err := td.Git.CommandInDir(ctx.setPath, "branch", "-d", scratch)
		return err
	}); err != nil {
		return fmt.Errorf("fold landed in trunk and %s holds the work; only the fold scratch branch %s is left over — it could not be deleted after %d attempts: %w",
			ctx.setBranch, scratch, foldPostLandingAttempts, err)
	}
	return nil
}

// foldPostLandingAttempts bounds the inline retry of the ref updates that follow the
// fast-forward, and foldPostLandingRetryDelay spaces them. They are local ref
// writes on work that has already landed, so the only failure worth waiting out is a
// transient one — another process holding the index or a ref lock — and a couple of
// tries a moment apart either clears it or proves it is not transient.
const (
	foldPostLandingAttempts   = 3
	foldPostLandingRetryDelay = 100 * time.Millisecond
)

func retryAfterLanding(step func() error) error {
	var err error
	for attempt := 0; attempt < foldPostLandingAttempts; attempt++ {
		if attempt > 0 {
			time.Sleep(foldPostLandingRetryDelay)
		}
		if err = step(); err == nil {
			return nil
		}
	}
	return err
}

// refuseAndAbandon rolls the fold's refs back to what it found and returns the
// refusal that stopped it. Every exit before the fast-forward can do this: nothing
// but the scratch ref was ever rewritten, so there is no restore step — the fold
// simply did not happen (ADR-0229).
func refuseAndAbandon(td *tasks.Deps, ctx foldRebaseContext, scratch string, refusal error) error {
	abandonFoldScratch(td, ctx, scratch)
	return refusal
}

// abandonFoldScratch puts the folding checkout back on its own branch and deletes
// the scratch branch. A rebase still in progress is left exactly as it stands: that
// is a parked fold, and the resolution in it is worth more than a tidy ref list.
// Best-effort throughout — it runs on a path that is already refusing, and the
// refusal is the more useful thing to report.
func abandonFoldScratch(td *tasks.Deps, ctx foldRebaseContext, scratch string) {
	if rebaseInProgress(td, ctx.setPath) {
		return
	}
	if CurrentBranch(td, ctx.setPath) != scratch {
		return
	}
	if _, err := td.Git.CommandInDir(ctx.setPath, "checkout", ctx.setBranch); err != nil {
		return
	}
	// `-D`, not `-d`: a scratch branch abandoned before the fast-forward carries
	// rebased copies that nothing has merged.
	_, _ = td.Git.CommandInDir(ctx.setPath, "branch", "-D", scratch)
}
