package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/glebglazov/pop/config"
	"github.com/glebglazov/pop/history"
	"github.com/glebglazov/pop/internal/deps"
	"github.com/glebglazov/pop/project"
	"github.com/glebglazov/pop/ui"
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

// --- Worktree create-path Workbench shaping (ADR-0075/0076) ---

// shapeSpy records which branch of shapeWorktreeSession ran. Each closure sets
// its flag, so a test can assert exactly one path fired.
type shapeSpy struct {
	resolveCalled  bool
	promptCalled   bool
	createdTmpl    string
	createdSession string
	createdPath    string
	historyPath    string
	attached       string
	flatCalled     bool
}

// newShapeDeps builds worktreeShapeDeps whose behavior is driven by the given
// pick_on_create toggle, resolved set, and prompt result. All side effects are
// captured in the returned spy.
func newShapeDeps(pickOn bool, workbenches []config.SessionTemplate, promptName string, promptConfirmed bool) (*worktreeShapeDeps, *shapeSpy) {
	spy := &shapeSpy{}
	d := &worktreeShapeDeps{
		LoadConfig:   func() (*config.Config, error) { return &config.Config{}, nil },
		PickOnCreate: func(cfg *config.Config) bool { return pickOn },
		ResolveWorkbenches: func(cfg *config.Config, path string) []config.SessionTemplate {
			spy.resolveCalled = true
			return workbenches
		},
		ResolvePreferredWorkbench: func(cfg *config.Config, path string) (string, []string) {
			return "", nil
		},
		PromptWorkbench: func(wbs []config.SessionTemplate) (string, bool, error) {
			spy.promptCalled = true
			return promptName, promptConfirmed, nil
		},
		FindWorkbench: findSessionTemplate,
		CreateSession: func(tmpl config.SessionTemplate, sessionName, path string) error {
			spy.createdTmpl = tmpl.Name
			spy.createdSession = sessionName
			spy.createdPath = path
			return nil
		},
		SessionName:   func(path string) string { return "sess-" + path },
		RecordHistory: func(path string) { spy.historyPath = path },
		Attach:        func(sessionName string) error { spy.attached = sessionName; return nil },
		Flat: func(ctx *project.RepoContext, item *ui.Item) error {
			spy.flatCalled = true
			return nil
		},
	}
	return d, spy
}

func TestShapeWorktreeSession_PickAWorkbench(t *testing.T) {
	wbs := []config.SessionTemplate{{Name: "gs-dev"}, {Name: "minimal"}}
	d, spy := newShapeDeps(true, wbs, "gs-dev", true)

	if err := shapeWorktreeSession(d, &project.RepoContext{}, "/repo/feature"); err != nil {
		t.Fatalf("shapeWorktreeSession: %v", err)
	}

	if !spy.promptCalled {
		t.Error("expected the Workbench prompt to be shown")
	}
	if spy.createdTmpl != "gs-dev" {
		t.Errorf("CreateSession tmpl = %q, want gs-dev", spy.createdTmpl)
	}
	if spy.createdSession != "sess-/repo/feature" || spy.createdPath != "/repo/feature" {
		t.Errorf("CreateSession(session=%q, path=%q), want sess-/repo/feature and /repo/feature", spy.createdSession, spy.createdPath)
	}
	if spy.historyPath != "/repo/feature" {
		t.Errorf("RecordHistory path = %q, want /repo/feature", spy.historyPath)
	}
	if spy.attached != "sess-/repo/feature" {
		t.Errorf("Attach target = %q, want sess-/repo/feature", spy.attached)
	}
	if spy.flatCalled {
		t.Error("flat session must not run when a Workbench is chosen")
	}
}

// TestShapeWorktreeSession_PreferredAutoApplies asserts a resolved preferred
// workbench (ADR-0078) auto-applies silently, building the session and attaching
// without a prompt, whether or not pick_on_create is on.
func TestShapeWorktreeSession_PreferredAutoApplies(t *testing.T) {
	for _, pickOn := range []bool{false, true} {
		name := "pick_on_create_off"
		if pickOn {
			name = "pick_on_create_on"
		}
		t.Run(name, func(t *testing.T) {
			wbs := []config.SessionTemplate{{Name: "gs-dev"}, {Name: "minimal"}}
			d, spy := newShapeDeps(pickOn, wbs, "gs-dev", true)
			d.ResolvePreferredWorkbench = func(cfg *config.Config, path string) (string, []string) {
				return "gs-dev", nil
			}

			if err := shapeWorktreeSession(d, &project.RepoContext{}, "/repo/feature"); err != nil {
				t.Fatalf("shapeWorktreeSession: %v", err)
			}

			if spy.promptCalled {
				t.Error("prompt must be suppressed when a preferred workbench resolves")
			}
			if spy.createdTmpl != "gs-dev" {
				t.Errorf("CreateSession tmpl = %q, want gs-dev", spy.createdTmpl)
			}
			if spy.historyPath != "/repo/feature" {
				t.Errorf("RecordHistory path = %q, want /repo/feature", spy.historyPath)
			}
			if spy.attached != "sess-/repo/feature" {
				t.Errorf("Attach target = %q, want sess-/repo/feature", spy.attached)
			}
			if spy.flatCalled {
				t.Error("flat session must not run when a preferred workbench resolves")
			}
		})
	}
}

// TestShapeWorktreeSession_StalePreferredFallsThrough asserts a stale preferred
// workbench (empty name + warning) never blocks: with pick_on_create off it
// falls through to today's flat session.
func TestShapeWorktreeSession_StalePreferredFallsThrough(t *testing.T) {
	wbs := []config.SessionTemplate{{Name: "gs-dev"}}
	d, spy := newShapeDeps(false, wbs, "gs-dev", true)
	d.ResolvePreferredWorkbench = func(cfg *config.Config, path string) (string, []string) {
		return "", []string{"preferred workbench \"ghost\" does not resolve; ignoring"}
	}

	if err := shapeWorktreeSession(d, &project.RepoContext{}, "/repo/feature"); err != nil {
		t.Fatalf("shapeWorktreeSession: %v", err)
	}

	if spy.createdTmpl != "" {
		t.Errorf("CreateSession must not run for a stale preferred workbench, got %q", spy.createdTmpl)
	}
	if !spy.flatCalled {
		t.Error("expected the flat session fall-through for a stale preferred workbench")
	}
}

func TestShapeWorktreeSession_NoWorkbenchFallsThrough(t *testing.T) {
	wbs := []config.SessionTemplate{{Name: "gs-dev"}}
	// The "no workbench" sentinel: confirmed choice, empty name.
	d, spy := newShapeDeps(true, wbs, "", true)

	if err := shapeWorktreeSession(d, &project.RepoContext{}, "/repo/feature"); err != nil {
		t.Fatalf("shapeWorktreeSession: %v", err)
	}

	if !spy.promptCalled {
		t.Error("expected the Workbench prompt to be shown")
	}
	if spy.createdTmpl != "" {
		t.Errorf("CreateSession must not run for the no-workbench sentinel, got %q", spy.createdTmpl)
	}
	if !spy.flatCalled {
		t.Error("expected the flat session fall-through for the no-workbench choice")
	}
}

func TestShapeWorktreeSession_EscFallsThrough(t *testing.T) {
	wbs := []config.SessionTemplate{{Name: "gs-dev"}}
	// Esc: not confirmed. The worktree already exists, so fall through to flat.
	d, spy := newShapeDeps(true, wbs, "", false)

	if err := shapeWorktreeSession(d, &project.RepoContext{}, "/repo/feature"); err != nil {
		t.Fatalf("shapeWorktreeSession: %v", err)
	}

	if spy.createdTmpl != "" {
		t.Error("CreateSession must not run when the prompt is cancelled")
	}
	if !spy.flatCalled {
		t.Error("expected the flat session fall-through when the prompt is cancelled")
	}
}

func TestShapeWorktreeSession_ToggleOffSkipsPrompt(t *testing.T) {
	wbs := []config.SessionTemplate{{Name: "gs-dev"}}
	d, spy := newShapeDeps(false, wbs, "gs-dev", true)

	if err := shapeWorktreeSession(d, &project.RepoContext{}, "/repo/feature"); err != nil {
		t.Fatalf("shapeWorktreeSession: %v", err)
	}

	if spy.resolveCalled {
		t.Error("ResolveWorkbenches must not be consulted when pick_on_create is off")
	}
	if spy.promptCalled {
		t.Error("Workbench prompt must not be shown when pick_on_create is off")
	}
	if !spy.flatCalled {
		t.Error("expected the flat session when pick_on_create is off")
	}
}

func TestShapeWorktreeSession_EmptySetSkipsPrompt(t *testing.T) {
	d, spy := newShapeDeps(true, nil, "", true)

	if err := shapeWorktreeSession(d, &project.RepoContext{}, "/repo/feature"); err != nil {
		t.Fatalf("shapeWorktreeSession: %v", err)
	}

	if !spy.resolveCalled {
		t.Error("expected ResolveWorkbenches to be consulted when pick_on_create is on")
	}
	if spy.promptCalled {
		t.Error("prompt must be skipped when no Workbenches resolve")
	}
	if !spy.flatCalled {
		t.Error("expected the flat session when the resolved Workbench set is empty")
	}
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
