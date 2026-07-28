package binding

import (
	"strings"

	"github.com/glebglazov/pop/config"
	"github.com/glebglazov/pop/tasks"
)

// RefreshCommitlessManagedBranch fast-forwards a provisioned managed worktree
// onto the current Trunk worktree HEAD when its branch carries no commits of
// its own (ADR-0147). It is a best-effort pre-drain refresh: dirty worktrees,
// branches that already have their own commits, and situations where a
// fast-forward is not possible are left untouched without error.
func RefreshCommitlessManagedBranch(td *tasks.Deps, cfg *config.Config, checkoutPath string, b Binding) error {
	if td == nil {
		return nil
	}
	if !b.Provisioned {
		return nil
	}
	runtimePath := strings.TrimSpace(b.RuntimePath)
	if runtimePath == "" {
		return nil
	}
	checkoutPath = strings.TrimSpace(checkoutPath)
	if checkoutPath == "" {
		return nil
	}

	trunkPath, bare, err := ResolveTrunkPath(td, cfg, checkoutPath)
	if err != nil || bare || strings.TrimSpace(trunkPath) == "" {
		return nil
	}

	dirty, err := worktreeIsDirty(td, runtimePath)
	if err != nil || dirty {
		return nil
	}

	branch := strings.TrimSpace(b.Branch)
	if branch == "" {
		branch = CurrentBranch(td, runtimePath)
	}
	if branch == "" {
		return nil
	}

	trunkBranch := CurrentBranch(td, trunkPath)
	if trunkBranch == "" {
		return nil
	}

	own, err := branchHasCommitsNotInTrunk(td, trunkPath, branch, trunkBranch)
	if err != nil || own {
		return nil
	}

	if _, err := td.Git.CommandInDir(runtimePath, "merge", "--ff-only", trunkBranch); err != nil {
		return nil
	}
	return nil
}

func worktreeIsDirty(td *tasks.Deps, path string) (bool, error) {
	out, err := td.Git.CommandInDir(path, "status", "--porcelain")
	if err != nil {
		return false, err
	}
	return strings.TrimSpace(out) != "", nil
}

// branchHasCommitsNotInTrunk reports whether branch has commits reachable from
// it that are not reachable from the trunk branch — i.e. work the managed
// branch carries on its own.
func branchHasCommitsNotInTrunk(td *tasks.Deps, trunkPath, branch, trunkBranch string) (bool, error) {
	out, err := td.Git.CommandInDir(trunkPath, "rev-list", branch, "--not", trunkBranch)
	if err != nil {
		return false, err
	}
	return strings.TrimSpace(out) != "", nil
}
