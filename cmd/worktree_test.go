package cmd

import (
	"fmt"
	"os"
	"testing"

	"github.com/glebglazov/pop/internal/deps"
	"github.com/glebglazov/pop/project"
)

// countingGitDeps swaps project's package-global dependencies for ones whose
// git calls are counted, and returns the live counter plus a restore func.
// findBareRoot is short-circuited (Stat always misses) so the only way to ring
// the counter is an actual git subprocess — exactly the "heavy call" we guard.
func countingGitDeps(t *testing.T) (gitCalls *int, restore func()) {
	t.Helper()
	n := 0
	count := func(...string) (string, error) { n++; return "", nil }
	d := &project.Deps{
		Git: &deps.MockGit{
			CommandFunc:      func(args ...string) (string, error) { return count(args...) },
			CommandInDirFunc: func(dir string, args ...string) (string, error) { return count(args...) },
		},
		FS: &deps.MockFileSystem{
			StatFunc:  func(string) (os.FileInfo, error) { return nil, os.ErrNotExist },
			GetwdFunc: func() (string, error) { return "/tmp", nil },
		},
	}
	return &n, project.SetDefaultDeps(d)
}

func TestBuildWorktreeItems(t *testing.T) {
	t.Run("worktree with active session gets icon", func(t *testing.T) {
		worktrees := []project.Worktree{
			{Name: "feature", Path: "/repo/feature", Branch: "feature-branch"},
		}
		sessionActivity := map[string]int64{
			project.SessionName("/repo/feature"): 1000,
		}

		items := buildWorktreeItems(&project.RepoContext{IsBare: false}, worktrees, sessionActivity)

		if len(items) != 1 {
			t.Fatalf("got %d items, want 1", len(items))
		}
		if items[0].Icon != iconDirSession {
			t.Errorf("Icon = %q, want %q", items[0].Icon, iconDirSession)
		}
		if items[0].Context != "feature-branch" {
			t.Errorf("Context = %q, want %q", items[0].Context, "feature-branch")
		}
	})

	t.Run("worktree without session has no icon", func(t *testing.T) {
		worktrees := []project.Worktree{
			{Name: "feature", Path: "/repo/feature", Branch: "feature-branch"},
		}
		sessionActivity := map[string]int64{}

		items := buildWorktreeItems(&project.RepoContext{IsBare: false}, worktrees, sessionActivity)

		if items[0].Icon != "" {
			t.Errorf("Icon = %q, want empty", items[0].Icon)
		}
	})

	t.Run("mixed session and no-session worktrees", func(t *testing.T) {
		worktrees := []project.Worktree{
			{Name: "active", Path: "/repo/active", Branch: "main"},
			{Name: "idle", Path: "/repo/idle", Branch: "dev"},
		}
		sessionActivity := map[string]int64{
			project.SessionName("/repo/active"): 1000,
		}

		items := buildWorktreeItems(&project.RepoContext{IsBare: false}, worktrees, sessionActivity)

		if len(items) != 2 {
			t.Fatalf("got %d items, want 2", len(items))
		}
		if items[0].Icon != iconDirSession {
			t.Errorf("active worktree: Icon = %q, want %q", items[0].Icon, iconDirSession)
		}
		if items[1].Icon != "" {
			t.Errorf("idle worktree: Icon = %q, want empty", items[1].Icon)
		}
	})

	t.Run("session icon matches SessionName for path", func(t *testing.T) {
		worktrees := []project.Worktree{
			{Name: "feature", Path: "/repo/feature", Branch: "feature-branch"},
		}
		sessionActivity := map[string]int64{
			project.SessionName("/repo/feature"): 1000,
		}

		items := buildWorktreeItems(&project.RepoContext{IsBare: false}, worktrees, sessionActivity)

		if items[0].Icon != iconDirSession {
			t.Errorf("Icon = %q, want %q", items[0].Icon, iconDirSession)
		}
	})
}

// TestBuildWorktreeItemsIssuesNoGitCalls guards against reintroducing the
// per-worktree git-call storm (commit 59d4af8, fixed in 417eaeb). Session
// names must be derived from the already-known RepoContext, not by calling
// project.SessionName(path) — which spawns 2-3 git subprocesses per worktree —
// inside the build loop. Building items for many worktrees must cost zero git
// calls regardless of count.
func TestBuildWorktreeItemsIssuesNoGitCalls(t *testing.T) {
	for _, ctx := range []*project.RepoContext{
		{IsBare: true, RepoName: "myrepo"},
		{IsBare: false},
	} {
		worktrees := make([]project.Worktree, 20)
		for i := range worktrees {
			name := fmt.Sprintf("wt-%d", i)
			worktrees[i] = project.Worktree{Name: name, Path: "/repo/" + name, Branch: name}
		}

		gitCalls, restore := countingGitDeps(t)
		buildWorktreeItems(ctx, worktrees, map[string]int64{})
		restore()

		if *gitCalls != 0 {
			t.Errorf("IsBare=%v: buildWorktreeItems issued %d git calls for %d worktrees, want 0 (per-item git derivation regressed)", ctx.IsBare, *gitCalls, len(worktrees))
		}
	}
}
