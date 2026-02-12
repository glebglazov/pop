package cmd

import (
	"testing"

	"github.com/glebglazov/pop/project"
)

func TestBuildWorktreeItems(t *testing.T) {
	ctx := &project.RepoContext{
		RepoName: "myrepo",
		IsBare:   true,
	}

	t.Run("worktree with active session gets icon", func(t *testing.T) {
		worktrees := []project.Worktree{
			{Name: "feature", Path: "/repo/feature", Branch: "feature-branch"},
		}
		// Session name for bare repo: "myrepo/feature"
		sessionActivity := map[string]int64{
			"myrepo/feature": 1000,
		}

		items := buildWorktreeItems(worktrees, ctx, sessionActivity)

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

		items := buildWorktreeItems(worktrees, ctx, sessionActivity)

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
			"myrepo/active": 1000,
		}

		items := buildWorktreeItems(worktrees, ctx, sessionActivity)

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

	t.Run("non-bare repo session name", func(t *testing.T) {
		nonBareCtx := &project.RepoContext{
			RepoName: "myrepo",
			IsBare:   false,
		}
		worktrees := []project.Worktree{
			{Name: "feature", Path: "/repo/feature", Branch: "feature-branch"},
		}
		// Non-bare: session name is just the worktree name
		sessionActivity := map[string]int64{
			"feature": 1000,
		}

		items := buildWorktreeItems(worktrees, nonBareCtx, sessionActivity)

		if items[0].Icon != iconDirSession {
			t.Errorf("Icon = %q, want %q", items[0].Icon, iconDirSession)
		}
	})
}
