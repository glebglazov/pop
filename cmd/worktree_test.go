package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/glebglazov/pop/history"
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

func TestRemoveFromHistoryWith(t *testing.T) {
	histJSON := `{"entries":[
		{"path":"/repo/feature","last_access":"2026-06-01T10:00:00Z"},
		{"path":"/repo/main","last_access":"2026-06-02T10:00:00Z"}
	]}`

	t.Run("removes deleted worktree entry and saves", func(t *testing.T) {
		var written []byte
		d := &history.Deps{
			FS: &deps.MockFileSystem{
				ReadFileFunc: func(path string) ([]byte, error) { return []byte(histJSON), nil },
				WriteFileFunc: func(path string, data []byte, perm os.FileMode) error {
					written = data
					return nil
				},
			},
		}

		removeFromHistoryWith(d, "/mock/history.json", "/repo/feature")

		if written == nil {
			t.Fatal("history was not saved")
		}
		var saved history.History
		if err := json.Unmarshal(written, &saved); err != nil {
			t.Fatal(err)
		}
		if len(saved.Entries) != 1 || saved.Entries[0].Path != "/repo/main" {
			t.Errorf("saved entries = %+v, want only /repo/main", saved.Entries)
		}
	})

	t.Run("load failure skips save", func(t *testing.T) {
		var saveCalled bool
		d := &history.Deps{
			FS: &deps.MockFileSystem{
				ReadFileFunc: func(path string) ([]byte, error) { return nil, os.ErrPermission },
				WriteFileFunc: func(path string, data []byte, perm os.FileMode) error {
					saveCalled = true
					return nil
				},
			},
		}

		removeFromHistoryWith(d, "/mock/history.json", "/repo/feature")

		if saveCalled {
			t.Error("history saved despite load failure")
		}
	})

	t.Run("missing entry still saves without change", func(t *testing.T) {
		var written []byte
		d := &history.Deps{
			FS: &deps.MockFileSystem{
				ReadFileFunc: func(path string) ([]byte, error) { return []byte(histJSON), nil },
				WriteFileFunc: func(path string, data []byte, perm os.FileMode) error {
					written = data
					return nil
				},
			},
		}

		removeFromHistoryWith(d, "/mock/history.json", "/repo/unknown")

		var saved history.History
		if err := json.Unmarshal(written, &saved); err != nil {
			t.Fatal(err)
		}
		if len(saved.Entries) != 2 {
			t.Errorf("saved %d entries, want 2 untouched", len(saved.Entries))
		}
	})
}

func TestWorktreeHelpHasNoPhantomCreateBinding(t *testing.T) {
	// ctrl-n is cursor-down in the picker; a create binding never shipped.
	// Guard against the stale help line returning.
	if strings.Contains(worktreeDashboardCmd.Long, "ctrl-n") {
		t.Error("worktree dashboard help advertises ctrl-n, which is not a create binding")
	}
}

// TestBuildWorktreeItemsTasksNoGitCalls guards against reintroducing the
// per-worktree git-call storm (commit 59d4af8, fixed in 417eaeb). Session
// names must be derived from the already-known RepoContext, not by calling
// project.SessionName(path) — which spawns 2-3 git subprocesses per worktree —
// inside the build loop. Building items for many worktrees must cost zero git
// calls regardless of count.
func TestBuildWorktreeItemsTasksNoGitCalls(t *testing.T) {
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
			t.Errorf("IsBare=%v: buildWorktreeItems taskd %d git calls for %d worktrees, want 0 (per-item git derivation regressed)", ctx.IsBare, *gitCalls, len(worktrees))
		}
	}
}
