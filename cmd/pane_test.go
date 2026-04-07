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

func TestRunPaneSetStatusWith_IdleAutoRegistersWithSeededLastVisited(t *testing.T) {
	// `set-status idle` on an untracked pane must auto-register it (so
	// agent integrations like opencode/pi that eagerly call setStatus("idle")
	// on plugin load can make their pane appear in the dashboard right
	// away) AND seed LastVisited to "now" so the new entry sorts to the
	// bottom of the idle group (closest to the cursor) instead of the top.
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
	if err := runPaneSetStatusWith(tmux, cfg, "", []string{"%9", "idle"}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	after := time.Now()

	state := loadState(t, statePath)
	entry, ok := state.Panes["%9"]
	if !ok {
		t.Fatal("expected %9 to be auto-registered after idle")
	}
	if entry.Status != monitor.StatusIdle {
		t.Errorf("status = %q, want %q", entry.Status, monitor.StatusIdle)
	}
	if entry.LastVisited.Before(before) || entry.LastVisited.After(after) {
		t.Errorf("LastVisited = %v, want between %v and %v", entry.LastVisited, before, after)
	}
}

func TestRunPaneSetStatusWith_ReadIsAliasForIdle(t *testing.T) {
	// "read" is the deprecated CLI alias for "idle". Calling
	// `set-status read` must behave identically to `set-status idle` —
	// auto-register and persist as monitor.StatusIdle, never as a
	// freestanding "read" status.
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

	if err := runPaneSetStatusWith(tmux, cfg, "", []string{"%5", "read"}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	state := loadState(t, statePath)
	entry, ok := state.Panes["%5"]
	if !ok {
		t.Fatal("expected %5 to be auto-registered after read alias")
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
