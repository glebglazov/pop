package cmd

import (
	"encoding/json"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
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

// --- set-status dispatch ---

func startMonitorTestServer(t *testing.T, handler monitor.RequestHandler) string {
	t.Helper()
	ln, err := monitor.ListenAndServe("127.0.0.1:0", handler)
	if err != nil {
		t.Fatalf("ListenAndServe: %v", err)
	}
	t.Cleanup(func() { ln.Close() })
	return ln.Addr().String()
}

func tcpServerEnabledCfg() *config.Config {
	return &config.Config{
		PaneMonitoring: &config.PaneMonitoringConfig{TCPServer: true},
	}
}

func TestRunPaneSetStatusWith_IgnoresConfiguredSource(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_DATA_HOME", dir)

	cfg := &config.Config{
		PaneMonitoring: &config.PaneMonitoringConfig{
			IgnoreStatusFrom: []string{"tmux-global"},
			TCPServer:        true,
		},
	}

	tmuxCalled := false
	tmux := &deps.MockTmux{
		CommandFunc: func(args ...string) (string, error) {
			tmuxCalled = true
			return "", nil
		},
	}

	if err := runPaneSetStatusWith(tmux, cfg, "tmux-global", false, "", []string{"%1", "working"}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if tmuxCalled {
		t.Error("expected no tmux calls when source is ignored")
	}

	statePath := filepath.Join(dir, "pop", "monitor.json")
	if _, err := os.Stat(statePath); err == nil {
		state := loadState(t, statePath)
		if len(state.Panes) != 0 {
			t.Errorf("expected empty state, got %d panes", len(state.Panes))
		}
	}
}

func TestRunPaneSetStatusWith_SocketSuccessSkipsDirect(t *testing.T) {
	var handlerCalled bool
	addr := startMonitorTestServer(t, func(req monitor.Request) monitor.Response {
		handlerCalled = true
		return monitor.Response{OK: true}
	})
	t.Setenv("POP_MONITOR_ADDR", addr)

	dir := t.TempDir()
	t.Setenv("XDG_DATA_HOME", dir)

	directWouldCallTmux := false
	tmux := &deps.MockTmux{
		CommandFunc: func(args ...string) (string, error) {
			directWouldCallTmux = true
			return "", fmt.Errorf("direct path should not run")
		},
	}

	if err := runPaneSetStatusWith(tmux, tcpServerEnabledCfg(), "", false, "", []string{"%7", "working"}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !handlerCalled {
		t.Error("expected daemon handler to receive request")
	}
	if directWouldCallTmux {
		t.Error("expected socket success without direct fallback")
	}

	statePath := filepath.Join(dir, "pop", "monitor.json")
	if _, err := os.Stat(statePath); err == nil {
		state := loadState(t, statePath)
		if len(state.Panes) != 0 {
			t.Errorf("expected no local state write on socket path, got %d panes", len(state.Panes))
		}
	}
}

func TestRunPaneSetStatusWith_SocketFailureFallsBackAndStartsDaemon(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := ln.Addr().String()
	ln.Close()

	t.Setenv("POP_MONITOR_ADDR", addr)

	dir := t.TempDir()
	t.Setenv("XDG_DATA_HOME", dir)

	daemonStarted := make(chan struct{}, 1)
	oldHook := paneOnSocketSendFailed
	paneOnSocketSendFailed = func() { daemonStarted <- struct{}{} }
	t.Cleanup(func() { paneOnSocketSendFailed = oldHook })

	tmux := newPaneInfoMockTmux(map[string]string{"%7": "sess\tcmd"})
	if err := runPaneSetStatusWith(tmux, tcpServerEnabledCfg(), "", false, "", []string{"%7", "working"}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	select {
	case <-daemonStarted:
	case <-time.After(time.Second):
		t.Fatal("expected daemon startup hook after socket failure")
	}

	state := loadState(t, filepath.Join(dir, "pop", "monitor.json"))
	entry, ok := state.Panes["%7"]
	if !ok {
		t.Fatal("expected direct fallback to register pane")
	}
	if entry.Status != monitor.StatusWorking {
		t.Errorf("status = %q, want %q", entry.Status, monitor.StatusWorking)
	}
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
