package binding

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/glebglazov/pop/tasks"
)

// foldScratchState is where a fold stopped, as git alone tells it. Nothing about a
// fold's progress is written outside git (ADR-0229), so a killed process leaves the
// same four signals any other interruption does — the scratch ref's existence, a
// rebase-in-progress directory, the branch's reach over the scratch ref, and trunk's
// containment of it — and those signals decide what happens next.
type foldScratchState int

const (
	// foldScratchAbsent: no scratch ref. No fold is in flight, or the last one
	// finished and cleaned up after itself.
	foldScratchAbsent foldScratchState = iota
	// foldScratchParked: a fold rebase is in progress on the scratch ref. The fold is
	// mid-rewrite with a resolution possibly half-done in the working tree, so it is
	// resumed through the Fold conflict prompt rather than started again.
	foldScratchParked
	// foldScratchResidue: the scratch ref's work is accounted for — either the branch
	// already reaches it, or trunk does — so it is the leftover of a fold that died
	// after the fast-forward. Deleting it loses nothing.
	foldScratchResidue
	// foldScratchAmbiguous: a scratch-named ref carrying commits neither the branch
	// nor trunk holds. Fold refuses it by name: it cannot say where that ref came
	// from, and guessing means deleting work.
	foldScratchAmbiguous
)

// parkedFold is a fold rebase left in progress in a checkout: the scratch ref git is
// rewriting, and the branch that ref was cut from. branch is empty when the mapping
// back is not unique — the caller then has to learn the branch some other way (a
// set's binding records it).
type parkedFold struct {
	scratch string
	branch  string
}

// readParkedFold reports the fold rebase parked in a checkout, if any. A rebase in
// progress is only *this* fold's when git is rewriting a Fold scratch ref: a human's
// own rebase is not a parked fold, and the dirty refusal is the honest answer for it.
// A head-name git will not give up is read as a parked fold anyway — fold's own
// rebase is by far the likeliest one to find in a checkout it is being asked to
// fold, and the conflict prompt says more about it than "dirty" ever could.
func readParkedFold(td *tasks.Deps, path string) (parkedFold, bool) {
	if !rebaseInProgress(td, path) {
		return parkedFold{}, false
	}
	rebasing := rebasingBranch(td, path)
	if rebasing == "" {
		return parkedFold{}, true
	}
	if !strings.HasPrefix(rebasing, foldScratchPrefix) {
		return parkedFold{}, false
	}
	return parkedFold{scratch: rebasing, branch: foldScratchRealBranch(td, path, rebasing)}, true
}

// rebasingBranch is the short name of the branch git is rebasing in a checkout.
// During a rebase HEAD is detached, so the branch cannot be read from HEAD; git
// records it in the rebase state directory instead, which is also why a rebase of a
// detached HEAD answers with something that is no branch name at all.
func rebasingBranch(td *tasks.Deps, path string) string {
	for _, name := range []string{"rebase-merge/head-name", "rebase-apply/head-name"} {
		p, err := td.Git.CommandInDir(path, "rev-parse", "--git-path", name)
		if err != nil {
			continue
		}
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		if !filepath.IsAbs(p) {
			p = filepath.Join(path, p)
		}
		data, err := os.ReadFile(p)
		if err != nil {
			continue
		}
		return strings.TrimPrefix(strings.TrimSpace(string(data)), "refs/heads/")
	}
	return ""
}

// foldScratchRealBranch maps a Fold scratch ref back to the branch it was cut from.
// Flattening `/` to `-` is not reversible, so the answer is looked up among the
// branches that exist rather than computed: exactly one match is an answer, and
// none or several is no answer at all.
func foldScratchRealBranch(td *tasks.Deps, path, scratch string) string {
	out, err := td.Git.CommandInDir(path, "for-each-ref", "--format=%(refname:short)", "refs/heads")
	if err != nil {
		return ""
	}
	found := ""
	for _, line := range strings.Split(out, "\n") {
		branch := strings.TrimSpace(line)
		if branch == "" || foldScratchBranch(branch) != scratch {
			continue
		}
		if found != "" {
			return ""
		}
		found = branch
	}
	return found
}

// resolveFoldBranch names the branch a fold is being asked to land, for a checkout
// standing anywhere a stopped fold could have left it: on the branch itself, detached
// mid-rebase, or on the scratch ref of a fold that died after landing and before
// putting the branch back. declared wins when the caller already knows the branch — a
// set's binding records the branch it was bound at. An empty answer with no error is
// a checkout that is detached for reasons of its own, which the caller refuses in its
// own words.
func resolveFoldBranch(td *tasks.Deps, path, declared string, parked parkedFold) (string, error) {
	branch := strings.TrimSpace(declared)
	if branch == "" {
		branch = CurrentBranch(td, path)
	}
	if branch == "" {
		branch = parked.branch
	}
	if !strings.HasPrefix(branch, foldScratchPrefix) {
		return branch, nil
	}
	// Folding a scratch ref would land the fold's own scaffolding and leave the real
	// branch behind; the fold to finish is the one that ref was cut from.
	cutFrom := foldScratchRealBranch(td, path, branch)
	if cutFrom == "" {
		return "", fmt.Errorf("fold refused: %s is standing on the fold scratch branch %s and no branch it could have been cut from remains; check %s back out, then fold again", path, branch, branch)
	}
	return cutFrom, nil
}

// classifyFoldScratch decides what a scratch ref found at preflight means.
func classifyFoldScratch(td *tasks.Deps, parked bool, plan foldCheckoutPlan, scratch string) foldScratchState {
	if parked {
		return foldScratchParked
	}
	if !localBranchExists(td, plan.path, scratch) {
		return foldScratchAbsent
	}
	if refContains(td, plan.path, plan.branch, scratch) {
		return foldScratchResidue
	}
	if refContains(td, plan.trunkPath, plan.trunkBranch, scratch) {
		return foldScratchResidue
	}
	return foldScratchAmbiguous
}

// discardFoldScratchResidue removes the scratch ref a fold left behind after its
// work had already landed. The checkout may still be standing on that ref — going
// back to the branch is precisely the step such a fold did not reach — and the
// delete is forced, because `-d` measures against HEAD and residue whose only home
// is trunk was never merged into the branch.
func discardFoldScratchResidue(td *tasks.Deps, plan foldCheckoutPlan, scratch string) error {
	if CurrentBranch(td, plan.path) == scratch {
		if _, err := td.Git.CommandInDir(plan.path, "checkout", plan.branch); err != nil {
			return fmt.Errorf("fold refused: check %s back out over the fold scratch branch %s: %w", plan.branch, scratch, err)
		}
	}
	if _, err := td.Git.CommandInDir(plan.path, "branch", "-D", scratch); err != nil {
		return fmt.Errorf("fold refused: delete the fold scratch branch %s left by an earlier fold: %w", scratch, err)
	}
	return nil
}

// refuseAmbiguousFoldScratch names the ref and stops. Everything else fold does to a
// scratch branch is safe because fold created it; this one it cannot vouch for.
func refuseAmbiguousFoldScratch(scratch, branch string) error {
	return fmt.Errorf("fold refused: %s exists with commits neither %s nor trunk holds; no fold is in progress, so pop cannot tell what it is — inspect it, then delete or rename it and fold again", scratch, branch)
}

func localBranchExists(td *tasks.Deps, path, branch string) bool {
	_, err := td.Git.CommandInDir(path, "rev-parse", "--verify", "--quiet", "refs/heads/"+branch)
	return err == nil
}

// refContains reports whether container reaches every commit on ref. git answers
// with an exit status and the git seam surfaces only an error, so anything but
// success reads as "does not contain" — which keeps a genuine git problem from
// being read as an accounted-for ref.
func refContains(td *tasks.Deps, path, container, ref string) bool {
	_, err := td.Git.CommandInDir(path, "merge-base", "--is-ancestor", ref, container)
	return err == nil
}
