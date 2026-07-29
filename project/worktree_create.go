package project

import (
	"errors"
	"fmt"
	"path/filepath"
	"strings"
)

// ErrBranchCheckedOutElsewhere is the sentinel wrapped into the refusal
// AddWorktreeNamedWith returns when the requested name reuses a local branch
// that is already checked out in another worktree (most commonly: accepting
// the create flow's suggested name, unchanged, for a base ref that is also
// the branch checked out in the worktree the picker was opened from). Git
// itself refuses this with a bare "fatal: '<branch>' is already used by
// worktree at '<path>'"; this sentinel lets callers surface a reason instead
// of that raw message.
var ErrBranchCheckedOutElsewhere = errors.New("branch already checked out in another worktree")

// Branch is a git ref offered in the worktree-create branch picker. Ref is the
// git-facing name — a local short name ("main", "feature/x") or a remote short
// name ("origin/main"). IsRemote marks refs under refs/remotes so the create
// flow knows to strip the remote prefix and spin up a local tracking branch.
type Branch struct {
	Ref      string
	IsRemote bool
}

// ListBranches returns the repo's branches for the create picker.
// Uses default dependencies.
func ListBranches(ctx *RepoContext) ([]Branch, error) {
	return ListBranchesWith(defaultDeps, ctx)
}

// ListBranchesWith lists local + remote branches with main/master first and
// remote HEAD symrefs (origin/HEAD) excluded, using provided dependencies.
func ListBranchesWith(d *Deps, ctx *RepoContext) ([]Branch, error) {
	output, err := d.Git.CommandInDir(ctx.GitRoot, "for-each-ref", "--format=%(refname)", "refs/heads", "refs/remotes")
	if err != nil {
		return nil, err
	}
	return parseBranches(output), nil
}

func parseBranches(output string) []Branch {
	var locals, remotes []Branch
	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimSpace(line)
		switch {
		case strings.HasPrefix(line, "refs/heads/"):
			locals = append(locals, Branch{Ref: strings.TrimPrefix(line, "refs/heads/")})
		case strings.HasPrefix(line, "refs/remotes/"):
			name := strings.TrimPrefix(line, "refs/remotes/")
			// Skip the remote's default-branch symref (e.g. origin/HEAD).
			if strings.HasSuffix(name, "/HEAD") {
				continue
			}
			remotes = append(remotes, Branch{Ref: name, IsRemote: true})
		}
	}
	return append(orderMainFirst(locals), remotes...)
}

// orderMainFirst moves main then master to the front, preserving the order of
// the remaining local branches.
func orderMainFirst(locals []Branch) []Branch {
	var front, rest []Branch
	var hasMain, hasMaster bool
	for _, b := range locals {
		switch b.Ref {
		case "main":
			hasMain = true
		case "master":
			hasMaster = true
		default:
			rest = append(rest, b)
		}
	}
	if hasMain {
		front = append(front, Branch{Ref: "main"})
	}
	if hasMaster {
		front = append(front, Branch{Ref: "master"})
	}
	return append(front, rest...)
}

// DeriveWorktreeName maps a selected branch ref to the branch to check out and
// the worktree directory name. Remote refs strip the remote prefix (origin/x →
// x); the directory name replaces "/" with "-".
func DeriveWorktreeName(ref string, isRemote bool) (branch, dir string) {
	branch = ref
	if isRemote {
		if idx := strings.Index(ref, "/"); idx >= 0 {
			branch = ref[idx+1:]
		}
	}
	dir = strings.ReplaceAll(branch, "/", "-")
	return branch, dir
}

// WorktreePath returns the checkout path for a new worktree directory name:
// <git_root>/<dir> for bare repos, sibling <repo_name>-<dir> for non-bare.
func WorktreePath(ctx *RepoContext, dir string) string {
	if ctx.IsBare {
		return filepath.Join(ctx.GitRoot, dir)
	}
	return filepath.Join(filepath.Dir(ctx.GitRoot), ctx.RepoName+"-"+dir)
}

// LocalBranchExistsWith reports whether refs/heads/<name> exists.
func LocalBranchExistsWith(d *Deps, ctx *RepoContext, name string) bool {
	_, err := d.Git.CommandInDir(ctx.GitRoot, "show-ref", "--verify", "--quiet", "refs/heads/"+name)
	return err == nil
}

// AddWorktree creates a worktree for the selected branch and returns its path.
// Uses default dependencies.
func AddWorktree(ctx *RepoContext, selection Branch) (string, error) {
	return AddWorktreeWith(defaultDeps, ctx, selection)
}

// AddWorktreeWith runs `git worktree add` using the branch-derived directory name.
func AddWorktreeWith(d *Deps, ctx *RepoContext, selection Branch) (string, error) {
	_, dir := DeriveWorktreeName(selection.Ref, selection.IsRemote)
	return AddWorktreeNamedWith(d, ctx, selection, dir)
}

// AddWorktreeNamed creates a worktree for the selected branch at the given
// directory name and returns its path. Uses default dependencies.
func AddWorktreeNamed(ctx *RepoContext, selection Branch, dir string) (string, error) {
	return AddWorktreeNamedWith(defaultDeps, ctx, selection, dir)
}

// AddWorktreeNamedWith runs `git worktree add`, porting the retired
// tmux-create-worktree logic faithfully (ADR-0076): the human-typed name IS the
// new branch name (and the directory name); the picked ref (selection) is only
// the fork start-point. A local branch matching the typed name is reused;
// otherwise a new branch is created with `-b <name> <path> <selection.Ref>` (a
// remote selection thus becomes a local tracking branch). Returns the new
// worktree path.
func AddWorktreeNamedWith(d *Deps, ctx *RepoContext, selection Branch, dir string) (string, error) {
	path := WorktreePath(ctx, dir)

	var err error
	if LocalBranchExistsWith(d, ctx, dir) {
		if elsewhere, checkErr := branchCheckedOutPathWith(d, ctx, dir); checkErr == nil && elsewhere != "" {
			return "", fmt.Errorf("%w: %q is checked out at %s; type a different name to fork a new branch", ErrBranchCheckedOutElsewhere, dir, elsewhere)
		}
		_, err = d.Git.CommandInDir(ctx.GitRoot, "worktree", "add", path, dir)
	} else {
		_, err = d.Git.CommandInDir(ctx.GitRoot, "worktree", "add", "-b", dir, path, selection.Ref)
	}
	if err != nil {
		return "", err
	}
	return path, nil
}

// branchCheckedOutPathWith returns the path of the repo's existing worktree
// that has branch checked out, or "" if none does. A check-list failure is
// not itself fatal here — the caller falls through to the git command that
// would have run anyway, so a transient listing error never blocks a create
// that might otherwise succeed.
func branchCheckedOutPathWith(d *Deps, ctx *RepoContext, branch string) (string, error) {
	worktrees, err := ListWorktreesWith(d, ctx)
	if err != nil {
		return "", err
	}
	for _, wt := range worktrees {
		if wt.Branch == branch {
			return wt.Path, nil
		}
	}
	return "", nil
}
