package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

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

// newPaneInfoMockTmux builds a MockTmux that responds to the tmux
// display-message calls made by auto-registration in runPaneSetStatusWith.
// paneInfo maps pane ID → "session\tpane_current_command"; unknown panes
// return an error (matching tmux's behavior for non-existent panes).
func newPaneInfoMockTmux(paneInfo map[string]string) *deps.MockTmux {
	return &deps.MockTmux{
		CommandFunc: func(args ...string) (string, error) {
			if len(args) >= 5 && args[0] == "display-message" && args[1] == "-t" {
				paneID := args[2]
				// Format string argument comes after -p.
				format := args[4]
				info, ok := paneInfo[paneID]
				if !ok {
					return "", fmt.Errorf("pane not found: %s", paneID)
				}
				switch format {
				case "#{session_name}\t#{pane_current_command}":
					return info, nil
				case "#{session_name}":
					// Caller only wants the session; strip the command.
					parts := strings.SplitN(info, "\t", 2)
					return parts[0], nil
				case "#{pane_active} #{window_active} #{session_attached}":
					// Inactive by default — no dismiss downgrade.
					return "0 0 0", nil
				}
			}
			return "", nil
		},
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

// --- follow / unfollow ---

func TestResolvePaneArg(t *testing.T) {
	t.Run("returns pane_id verbatim when prefixed with %", func(t *testing.T) {
		// Should not call into tmux at all.
		tmux := &deps.MockTmux{
			CommandFunc: func(args ...string) (string, error) {
				t.Errorf("unexpected tmux call for raw pane_id: %v", args)
				return "", nil
			},
		}
		got, err := resolvePaneArg(tmux, "%42")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got != "%42" {
			t.Errorf("got %q, want %%42", got)
		}
	})

	t.Run("resolves name via findPane in current session", func(t *testing.T) {
		tmux := &deps.MockTmux{
			CommandFunc: func(args ...string) (string, error) {
				// resolveSessionWith → currentTmuxSessionWith calls
				// `display-message -p #S` to get the current session.
				if args[0] == "display-message" && len(args) >= 3 && args[1] == "-p" && args[2] == "#S" {
					return "session-x", nil
				}
				if args[0] == "list-panes" {
					return "myagent|%5\nother|%6", nil
				}
				return "", nil
			},
		}
		got, err := resolvePaneArg(tmux, "myagent")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got != "%5" {
			t.Errorf("got %q, want %%5", got)
		}
	})
}

func TestRunPaneSetFollowDirect_AutoRegistersOnFollow(t *testing.T) {
	// follow on an untracked pane auto-registers it as clear+following.
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

	tmux := newPaneInfoMockTmux(map[string]string{
		"%7": "proj-a\topencode",
	})

	before := time.Now()
	if err := runPaneSetFollowDirect(tmux, "%7", true); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	after := time.Now()

	state := loadState(t, statePath)
	entry, ok := state.Panes["%7"]
	if !ok {
		t.Fatalf("expected %%7 to be auto-registered")
	}
	if !entry.Following {
		t.Error("expected Following = true")
	}
	if entry.Status != monitor.StatusClear {
		t.Errorf("auto-registered status = %q, want clear", entry.Status)
	}
	if entry.Session != "proj-a" {
		t.Errorf("session = %q, want proj-a", entry.Session)
	}
	if entry.LastActiveAt.Before(before) || entry.LastActiveAt.After(after) {
		t.Errorf("LastActiveAt = %v, want between %v and %v", entry.LastActiveAt, before, after)
	}
}

func TestRunPaneSetFollowDirect_UnfollowOnUntrackedIsNoop(t *testing.T) {
	// Unfollowing an untracked pane is a no-op — no auto-registration,
	// no error. Absence already implies "not followed".
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

	// Tmux must NOT be queried — that would mean we tried to register.
	tmux := &deps.MockTmux{
		CommandFunc: func(args ...string) (string, error) {
			t.Errorf("unexpected tmux call on unfollow no-op: %v", args)
			return "", nil
		},
	}

	if err := runPaneSetFollowDirect(tmux, "%7", false); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	state := loadState(t, statePath)
	if len(state.Panes) != 0 {
		t.Errorf("expected empty state, got %d entries", len(state.Panes))
	}
}

func TestRunPaneSetFollowDirect_TogglesTrackedPane(t *testing.T) {
	// follow then unfollow on a tracked pane flips the flag without
	// touching status; unfollowing also clears any note.
	dir := t.TempDir()
	t.Setenv("XDG_DATA_HOME", dir)
	stateDir := filepath.Join(dir, "pop")
	if err := os.MkdirAll(stateDir, 0755); err != nil {
		t.Fatal(err)
	}
	statePath := filepath.Join(stateDir, "monitor.json")
	seed := &monitor.State{
		Panes: map[string]*monitor.PaneEntry{
			"%3": {
				PaneID:  "%3",
				Session: "proj-b",
				Status:  monitor.StatusWorking,
				Note:    "watch the deploy",
			},
		},
	}
	data, _ := json.Marshal(seed)
	if err := os.WriteFile(statePath, data, 0644); err != nil {
		t.Fatal(err)
	}

	// No tmux calls expected for already-tracked panes.
	tmux := &deps.MockTmux{
		CommandFunc: func(args ...string) (string, error) {
			t.Errorf("unexpected tmux call for tracked pane: %v", args)
			return "", nil
		},
	}

	// follow=true
	if err := runPaneSetFollowDirect(tmux, "%3", true); err != nil {
		t.Fatalf("follow: unexpected error: %v", err)
	}
	state := loadState(t, statePath)
	entry := state.Panes["%3"]
	if !entry.Following {
		t.Error("after follow: expected Following = true")
	}
	if entry.Status != monitor.StatusWorking {
		t.Errorf("after follow: status changed to %q, want unchanged 'working'", entry.Status)
	}
	if entry.Note != "watch the deploy" {
		t.Errorf("after follow: note = %q, want unchanged", entry.Note)
	}

	// follow=false
	if err := runPaneSetFollowDirect(tmux, "%3", false); err != nil {
		t.Fatalf("unfollow: unexpected error: %v", err)
	}
	state = loadState(t, statePath)
	entry = state.Panes["%3"]
	if entry.Following {
		t.Error("after unfollow: expected Following = false")
	}
	if entry.Note != "" {
		t.Errorf("after unfollow: note = %q, want cleared", entry.Note)
	}
}

func TestRunPaneSetFollowDirect_NoOpWhenAlreadyAtTargetState(t *testing.T) {
	// If Following already matches the target, the entry is not rewritten
	// (UpdatedAt should not bump). Verifies the early-return path.
	dir := t.TempDir()
	t.Setenv("XDG_DATA_HOME", dir)
	stateDir := filepath.Join(dir, "pop")
	if err := os.MkdirAll(stateDir, 0755); err != nil {
		t.Fatal(err)
	}
	statePath := filepath.Join(stateDir, "monitor.json")
	originalUpdatedAt := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	seed := &monitor.State{
		Panes: map[string]*monitor.PaneEntry{
			"%4": {
				PaneID:    "%4",
				Session:   "proj-c",
				Status:    monitor.StatusClear,
				Following: true,
				UpdatedAt: originalUpdatedAt,
			},
		},
	}
	data, _ := json.Marshal(seed)
	if err := os.WriteFile(statePath, data, 0644); err != nil {
		t.Fatal(err)
	}

	tmux := &deps.MockTmux{}
	if err := runPaneSetFollowDirect(tmux, "%4", true); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	state := loadState(t, statePath)
	if !state.Panes["%4"].UpdatedAt.Equal(originalUpdatedAt) {
		t.Errorf("UpdatedAt was bumped on no-op follow: %v != %v",
			state.Panes["%4"].UpdatedAt, originalUpdatedAt)
	}
}
