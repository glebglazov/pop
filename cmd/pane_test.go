package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"

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

		err := runPaneSetStatusWith(activeTmux, cfg, []string{"%1", "needs_attention"})
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

		err := runPaneSetStatusWith(activeTmux, cfg, []string{"%1", "needs_attention"})
		if err != nil {
			t.Fatal(err)
		}

		state := loadState(t, statePath)
		if state.Panes["%1"].Status != monitor.StatusRead {
			t.Errorf("got %q, want %q", state.Panes["%1"].Status, monitor.StatusRead)
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

		err := runPaneSetStatusWith(inactiveTmux, cfg, []string{"%1", "needs_attention"})
		if err != nil {
			t.Fatal(err)
		}

		state := loadState(t, statePath)
		if state.Panes["%1"].Status != monitor.StatusNeedsAttention {
			t.Errorf("got %q, want %q", state.Panes["%1"].Status, monitor.StatusNeedsAttention)
		}
	})
}
