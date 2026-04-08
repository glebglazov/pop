package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/glebglazov/pop/config"
	"github.com/glebglazov/pop/internal/deps"
	"github.com/glebglazov/pop/monitor"
)

func TestFindPaneWith(t *testing.T) {
	tmux := &deps.MockTmux{
		CommandFunc: func(args ...string) (string, error) {
			if args[0] == "list-panes" {
				return "server|%5\ndb|%6\nlogs|%7", nil
			}
			return "", nil
		},
	}

	t.Run("finds existing pane", func(t *testing.T) {
		paneID, err := findPaneWith(tmux, "project", "db")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if paneID != "%6" {
			t.Errorf("got %q, want %%6", paneID)
		}
	})

	t.Run("returns error for missing pane", func(t *testing.T) {
		_, err := findPaneWith(tmux, "project", "nonexistent")
		if err == nil {
			t.Error("expected error for missing pane")
		}
	})
}

func TestHasAgentWindowWith(t *testing.T) {
	tests := []struct {
		name     string
		output   string
		expected bool
	}{
		{
			name:     "agent window exists",
			output:   "main\nagent\nlogs",
			expected: true,
		},
		{
			name:     "no agent window",
			output:   "main\nlogs",
			expected: false,
		},
		{
			name:     "empty output",
			output:   "",
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmux := &deps.MockTmux{
				CommandFunc: func(args ...string) (string, error) {
					return tt.output, nil
				},
			}
			result := hasAgentWindowWith(tmux, "project")
			if result != tt.expected {
				t.Errorf("got %v, want %v", result, tt.expected)
			}
		})
	}
}

func TestIsPaneDeadWith(t *testing.T) {
	tests := []struct {
		name     string
		output   string
		err      error
		expected bool
	}{
		{
			name:     "dead pane",
			output:   "1",
			expected: true,
		},
		{
			name:     "alive pane",
			output:   "0",
			expected: false,
		},
		{
			name:     "error returns false",
			err:      fmt.Errorf("pane not found"),
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmux := &deps.MockTmux{
				CommandFunc: func(args ...string) (string, error) {
					return tt.output, tt.err
				},
			}
			result := isPaneDeadWith(tmux, "%5")
			if result != tt.expected {
				t.Errorf("got %v, want %v", result, tt.expected)
			}
		})
	}
}

func TestResolveSessionWith_WithProject(t *testing.T) {
	// Save and restore paneProject
	oldProject := paneProject
	defer func() { paneProject = oldProject }()
	paneProject = "/home/user/my.project"

	var createdSession string
	tmux := &deps.MockTmux{
		CommandFunc: func(args ...string) (string, error) {
			if args[0] == "has-session" {
				return "", fmt.Errorf("no such session") // session doesn't exist
			}
			if args[0] == "new-session" {
				createdSession = args[2] // "-ds", name
				return "", nil
			}
			return "", nil
		},
	}

	session, err := resolveSessionWith(tmux)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if session != "my_project" { // dots sanitized to underscores
		t.Errorf("got %q, want %q", session, "my_project")
	}
	if createdSession != "my_project" {
		t.Errorf("created session %q, want %q", createdSession, "my_project")
	}
}

func TestResolveSessionWith_ExistingSession(t *testing.T) {
	oldProject := paneProject
	defer func() { paneProject = oldProject }()
	paneProject = "/home/user/project"

	tmux := &deps.MockTmux{
		CommandFunc: func(args ...string) (string, error) {
			if args[0] == "has-session" {
				return "", nil // session exists
			}
			return "", nil
		},
	}

	session, err := resolveSessionWith(tmux)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if session != "project" {
		t.Errorf("got %q, want %q", session, "project")
	}
}

func TestResolveSessionWith_NoProjectNotInTmux(t *testing.T) {
	oldProject := paneProject
	defer func() { paneProject = oldProject }()
	paneProject = ""

	tmux := &deps.MockTmux{
		CommandFunc: func(args ...string) (string, error) {
			return "", fmt.Errorf("not in tmux")
		},
	}

	_, err := resolveSessionWith(tmux)
	if err == nil {
		t.Error("expected error when not in tmux and no --project")
	}
}

func setupStateFile(t *testing.T, paneID string, status monitor.PaneStatus) string {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("XDG_DATA_HOME", dir)

	stateDir := filepath.Join(dir, "pop")
	if err := os.MkdirAll(stateDir, 0755); err != nil {
		t.Fatal(err)
	}

	state := &monitor.State{
		Panes: map[string]*monitor.PaneEntry{
			paneID: {PaneID: paneID, Session: "test", Status: status},
		},
	}
	data, _ := json.Marshal(state)
	if err := os.WriteFile(filepath.Join(stateDir, "monitor.json"), data, 0644); err != nil {
		t.Fatal(err)
	}
	return filepath.Join(stateDir, "monitor.json")
}

func loadState(t *testing.T, path string) *monitor.State {
	t.Helper()
	state, err := monitor.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	return state
}

func TestRunPaneCreateWith(t *testing.T) {
	// Save and restore paneProject (used by resolveSessionWith)
	oldProject := paneProject
	defer func() { paneProject = oldProject }()
	paneProject = "/home/user/project"

	t.Run("returns existing alive pane", func(t *testing.T) {
		var cmds []string
		tmux := &deps.MockTmux{
			CommandFunc: func(args ...string) (string, error) {
				cmds = append(cmds, args[0])
				switch args[0] {
				case "has-session":
					return "", nil
				case "list-panes":
					return "mypane|%5", nil
				case "display-message":
					return "0", nil // not dead
				}
				return "", nil
			},
		}

		err := runPaneCreateWith(tmux, "mypane", "echo hi")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		// Should NOT have created a window or split
		for _, c := range cmds {
			if c == "new-window" || c == "split-window" {
				t.Errorf("should not call %s for alive pane", c)
			}
		}
	})

	t.Run("kills dead pane and recreates with new-window", func(t *testing.T) {
		var killed, created bool
		tmux := &deps.MockTmux{
			CommandFunc: func(args ...string) (string, error) {
				switch args[0] {
				case "has-session":
					return "", nil
				case "list-panes":
					if killed {
						return "", fmt.Errorf("no agent window")
					}
					return "mypane|%5", nil
				case "display-message":
					return "1", nil // dead pane
				case "kill-pane":
					killed = true
					return "", nil
				case "list-windows":
					return "main", nil // no agent window
				case "new-window":
					created = true
					return "%10", nil
				case "select-pane":
					return "", nil
				}
				return "", nil
			},
		}

		err := runPaneCreateWith(tmux, "mypane", "echo hi")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !killed {
			t.Error("expected dead pane to be killed")
		}
		if !created {
			t.Error("expected new-window to be called")
		}
	})

	t.Run("uses split-window when agent window exists", func(t *testing.T) {
		var splitCalled, tiledCalled bool
		tmux := &deps.MockTmux{
			CommandFunc: func(args ...string) (string, error) {
				switch args[0] {
				case "has-session":
					return "", nil
				case "list-panes":
					return "", fmt.Errorf("no agent window") // pane not found
				case "list-windows":
					return "main\nagent", nil // agent window exists
				case "split-window":
					splitCalled = true
					return "%10", nil
				case "select-layout":
					tiledCalled = true
					return "", nil
				case "select-pane":
					return "", nil
				}
				return "", nil
			},
		}

		err := runPaneCreateWith(tmux, "mypane", "echo hi")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !splitCalled {
			t.Error("expected split-window to be called")
		}
		if !tiledCalled {
			t.Error("expected select-layout tiled to be called")
		}
	})

	t.Run("uses new-window when no agent window", func(t *testing.T) {
		var newWindowCalled bool
		tmux := &deps.MockTmux{
			CommandFunc: func(args ...string) (string, error) {
				switch args[0] {
				case "has-session":
					return "", nil
				case "list-panes":
					return "", fmt.Errorf("no pane") // not found
				case "list-windows":
					return "main", nil // no agent window
				case "new-window":
					newWindowCalled = true
					return "%10", nil
				case "select-pane":
					return "", nil
				}
				return "", nil
			},
		}

		err := runPaneCreateWith(tmux, "mypane", "echo hi")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !newWindowCalled {
			t.Error("expected new-window to be called")
		}
	})
}

func TestRunPaneSetStatusWith_IdleDoesNotAutoRegister(t *testing.T) {
	// `set-status idle` on an untracked pane must be a no-op: the pane is
	// NOT added to the monitor state and therefore does NOT appear on the
	// dashboard.
	//
	// This is critical because:
	//   1. The tmux-global auto-read hook (see cmd/monitor.go) fires
	//      `pop pane set-status --source tmux-global <pane> read` on every
	//      pane navigation. If idle auto-registered, every pane the user
	//      ever visits would pollute the dashboard as an idle entry.
	//   2. Agent extensions (opencode, pi) eagerly send idle on plugin
	//      load as housekeeping to clear stale "working" from crashed
	//      runs. Their top-of-file contract explicitly promises that this
	//      cannot register new panes.
	//
	// Only "agentic" statuses (working / needs_attention) — which only
	// agent integrations ever send — register a new pane.
	dir := t.TempDir()
	t.Setenv("XDG_DATA_HOME", dir)
	stateDir := filepath.Join(dir, "pop")
	if err := os.MkdirAll(stateDir, 0755); err != nil {
		t.Fatal(err)
	}
	statePath := filepath.Join(stateDir, "monitor.json")
	emptyState := &monitor.State{Panes: map[string]*monitor.PaneEntry{}}
	data, _ := json.Marshal(emptyState)
	if err := os.WriteFile(statePath, data, 0644); err != nil {
		t.Fatal(err)
	}

	tmux := &deps.MockTmux{
		CommandFunc: func(args ...string) (string, error) {
			return "test-session", nil
		},
	}
	cfg := &config.Config{}

	if err := runPaneSetStatusWith(tmux, cfg, "", []string{"%9", "idle"}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	state := loadState(t, statePath)
	if _, ok := state.Panes["%9"]; ok {
		t.Fatalf("expected %%9 to NOT be auto-registered on idle, but it is: %+v", state.Panes["%9"])
	}
	if len(state.Panes) != 0 {
		t.Errorf("expected empty state, got %d pane(s)", len(state.Panes))
	}
}

func TestRunPaneSetStatusWith_TmuxGlobalHookDoesNotPolluteDashboard(t *testing.T) {
	// Simulates the exact scenario that was polluting the dashboard:
	// the tmux-global auto-read hook (installed by `pop pane monitor-start`)
	// fires `pop pane set-status --source tmux-global <pane> read` on every
	// pane-select/window-change/client-session-change. Running this on a
	// handful of random pane IDs must NOT add any entries to the monitor
	// state — the dashboard should stay empty until an actual agent
	// extension claims one of those panes.
	dir := t.TempDir()
	t.Setenv("XDG_DATA_HOME", dir)
	stateDir := filepath.Join(dir, "pop")
	if err := os.MkdirAll(stateDir, 0755); err != nil {
		t.Fatal(err)
	}
	statePath := filepath.Join(stateDir, "monitor.json")
	emptyState := &monitor.State{Panes: map[string]*monitor.PaneEntry{}}
	data, _ := json.Marshal(emptyState)
	if err := os.WriteFile(statePath, data, 0644); err != nil {
		t.Fatal(err)
	}

	tmux := &deps.MockTmux{
		CommandFunc: func(args ...string) (string, error) {
			return "some-session", nil
		},
	}
	cfg := &config.Config{}

	// Simulate user navigating through a bunch of panes. The hook fires
	// "read" (normalized to idle) with --source tmux-global for each one.
	for _, paneID := range []string{"%1", "%2", "%3", "%4", "%42"} {
		if err := runPaneSetStatusWith(tmux, cfg, "tmux-global", []string{paneID, "read"}); err != nil {
			t.Fatalf("unexpected error for %s: %v", paneID, err)
		}
	}

	state := loadState(t, statePath)
	if len(state.Panes) != 0 {
		t.Fatalf("expected dashboard state to stay empty after tmux-global hooks fired, got %d pane(s): %+v", len(state.Panes), state.Panes)
	}

	// Now simulate an agent extension claiming %2 with a "working" status.
	// That single pane — and only that one — should appear in the state.
	if err := runPaneSetStatusWith(tmux, cfg, "", []string{"%2", "working"}); err != nil {
		t.Fatalf("unexpected error claiming %%2: %v", err)
	}

	state = loadState(t, statePath)
	if len(state.Panes) != 1 {
		t.Fatalf("expected exactly 1 registered pane, got %d: %+v", len(state.Panes), state.Panes)
	}
	entry, ok := state.Panes["%2"]
	if !ok {
		t.Fatalf("expected %%2 to be the registered pane, got: %+v", state.Panes)
	}
	if entry.Status != monitor.StatusWorking {
		t.Errorf("status = %q, want %q", entry.Status, monitor.StatusWorking)
	}

	// A subsequent tmux-global idle on %2 (already registered) should
	// update LastVisited but keep the pane in state — this is the
	// legitimate auto-read path.
	before := time.Now()
	if err := runPaneSetStatusWith(tmux, cfg, "tmux-global", []string{"%2", "read"}); err != nil {
		t.Fatalf("unexpected error on auto-read: %v", err)
	}
	after := time.Now()

	state = loadState(t, statePath)
	entry, ok = state.Panes["%2"]
	if !ok {
		t.Fatal("expected %2 to remain registered after auto-read")
	}
	if entry.Status != monitor.StatusIdle {
		t.Errorf("status after auto-read = %q, want %q", entry.Status, monitor.StatusIdle)
	}
	if entry.LastVisited.Before(before) || entry.LastVisited.After(after) {
		t.Errorf("LastVisited = %v, want between %v and %v", entry.LastVisited, before, after)
	}
}

func TestRunPaneSetStatusWith_ReadIsAliasForIdle(t *testing.T) {
	// "read" is the deprecated CLI alias for "idle". Calling
	// `set-status read` must behave identically to `set-status idle`:
	//   - on an untracked pane: no-op (does NOT auto-register)
	//   - on an already-tracked pane: updates status to idle and
	//     refreshes LastVisited
	dir := t.TempDir()
	t.Setenv("XDG_DATA_HOME", dir)
	stateDir := filepath.Join(dir, "pop")
	if err := os.MkdirAll(stateDir, 0755); err != nil {
		t.Fatal(err)
	}
	statePath := filepath.Join(stateDir, "monitor.json")
	// Pre-register %5 as working to prove that "read" still updates
	// tracked panes. Pre-register nothing for %6 to prove that "read"
	// does NOT register new ones.
	seedState := &monitor.State{
		Panes: map[string]*monitor.PaneEntry{
			"%5": {PaneID: "%5", Session: "test-session", Status: monitor.StatusWorking},
		},
	}
	data, _ := json.Marshal(seedState)
	if err := os.WriteFile(statePath, data, 0644); err != nil {
		t.Fatal(err)
	}

	tmux := &deps.MockTmux{
		CommandFunc: func(args ...string) (string, error) {
			return "test-session", nil
		},
	}
	cfg := &config.Config{}

	// Untracked pane: "read" must be a no-op.
	if err := runPaneSetStatusWith(tmux, cfg, "", []string{"%6", "read"}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	state := loadState(t, statePath)
	if _, ok := state.Panes["%6"]; ok {
		t.Fatal("expected %6 to remain untracked after read alias")
	}

	// Tracked pane: "read" must transition it to idle.
	if err := runPaneSetStatusWith(tmux, cfg, "", []string{"%5", "read"}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	state = loadState(t, statePath)
	entry, ok := state.Panes["%5"]
	if !ok {
		t.Fatal("expected %5 to remain tracked")
	}
	if entry.Status != monitor.StatusIdle {
		t.Errorf("status = %q, want %q (read should be normalized to idle)", entry.Status, monitor.StatusIdle)
	}
}

func TestRunPaneSetStatusWith_AutoRegisterSeedsLastVisited(t *testing.T) {
	// On first registration, LastVisited must be seeded to "now" so the
	// new pane sorts to the bottom of its status group in the dashboard
	// (closest to the cursor). Without this, the zero-value LastVisited
	// would sort the pane to the top of its group (farthest from the
	// cursor) under the ascending sort in sortDashboardPanes.
	dir := t.TempDir()
	t.Setenv("XDG_DATA_HOME", dir)
	stateDir := filepath.Join(dir, "pop")
	if err := os.MkdirAll(stateDir, 0755); err != nil {
		t.Fatal(err)
	}
	statePath := filepath.Join(stateDir, "monitor.json")
	emptyState := &monitor.State{Panes: map[string]*monitor.PaneEntry{}}
	data, _ := json.Marshal(emptyState)
	if err := os.WriteFile(statePath, data, 0644); err != nil {
		t.Fatal(err)
	}

	tmux := &deps.MockTmux{
		CommandFunc: func(args ...string) (string, error) {
			return "test-session", nil
		},
	}
	cfg := &config.Config{}

	before := time.Now()
	if err := runPaneSetStatusWith(tmux, cfg, "", []string{"%7", "working"}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	after := time.Now()

	state := loadState(t, statePath)
	entry, ok := state.Panes["%7"]
	if !ok {
		t.Fatal("expected %7 to be auto-registered")
	}
	if entry.LastVisited.IsZero() {
		t.Errorf("expected LastVisited to be seeded on auto-register, got zero value")
	}
	if entry.LastVisited.Before(before) || entry.LastVisited.After(after) {
		t.Errorf("LastVisited = %v, want between %v and %v", entry.LastVisited, before, after)
	}
}

func TestRunPaneSetStatusWith_IdleUpdatesRegisteredPane(t *testing.T) {
	// The flip side of IdleDoesNotAutoRegister: a pane that IS already
	// tracked should still be transitioned to idle. This is what lets the
	// extensions clear a stale "working" status (e.g. left over from a
	// crashed previous run) by calling setStatus("idle") on plugin load.
	statePath := setupStateFile(t, "%1", monitor.StatusWorking)

	tmux := &deps.MockTmux{
		CommandFunc: func(args ...string) (string, error) {
			return "test", nil
		},
	}
	cfg := &config.Config{}

	if err := runPaneSetStatusWith(tmux, cfg, "", []string{"%1", "idle"}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	state := loadState(t, statePath)
	entry, ok := state.Panes["%1"]
	if !ok {
		t.Fatal("expected %1 to remain in state")
	}
	if entry.Status != monitor.StatusIdle {
		t.Errorf("got status %q, want %q", entry.Status, monitor.StatusIdle)
	}
}

func TestRunPaneSetStatusWith_DismissAttentionInActivePane(t *testing.T) {
	activeTmux := &deps.MockTmux{
		CommandFunc: func(args ...string) (string, error) {
			// isActiveTmuxPaneWith checks display-message
			if args[0] == "display-message" {
				return "1 1 1", nil
			}
			return "", nil
		},
	}

	t.Run("default config does not downgrade on active pane", func(t *testing.T) {
		statePath := setupStateFile(t, "%1", monitor.StatusWorking)
		cfg := &config.Config{}

		err := runPaneSetStatusWith(activeTmux, cfg, "", []string{"%1", "needs_attention"})
		if err != nil {
			t.Fatal(err)
		}

		state := loadState(t, statePath)
		if state.Panes["%1"].Status != monitor.StatusNeedsAttention {
			t.Errorf("got %q, want %q", state.Panes["%1"].Status, monitor.StatusNeedsAttention)
		}
	})

	t.Run("dismiss_attention_in_active_pane downgrades to read", func(t *testing.T) {
		statePath := setupStateFile(t, "%1", monitor.StatusWorking)
		cfg := &config.Config{
			PaneMonitoring: &config.PaneMonitoringConfig{
				DismissAttentionInActivePane: true,
			},
		}

		err := runPaneSetStatusWith(activeTmux, cfg, "", []string{"%1", "needs_attention"})
		if err != nil {
			t.Fatal(err)
		}

		state := loadState(t, statePath)
		if state.Panes["%1"].Status != monitor.StatusIdle {
			t.Errorf("got %q, want %q", state.Panes["%1"].Status, monitor.StatusIdle)
		}
	})

	t.Run("dismiss_attention_in_active_pane no effect on inactive pane", func(t *testing.T) {
		inactiveTmux := &deps.MockTmux{
			CommandFunc: func(args ...string) (string, error) {
				if args[0] == "display-message" {
					return "0 1 1", nil // pane not active
				}
				return "", nil
			},
		}
		statePath := setupStateFile(t, "%1", monitor.StatusWorking)
		cfg := &config.Config{
			PaneMonitoring: &config.PaneMonitoringConfig{
				DismissAttentionInActivePane: true,
			},
		}

		err := runPaneSetStatusWith(inactiveTmux, cfg, "", []string{"%1", "needs_attention"})
		if err != nil {
			t.Fatal(err)
		}

		state := loadState(t, statePath)
		if state.Panes["%1"].Status != monitor.StatusNeedsAttention {
			t.Errorf("got %q, want %q", state.Panes["%1"].Status, monitor.StatusNeedsAttention)
		}
	})
}
