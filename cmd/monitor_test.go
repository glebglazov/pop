package cmd

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/glebglazov/pop/internal/deps"
	"github.com/glebglazov/pop/monitor"
)

func TestBinaryNewerThanPIDWith(t *testing.T) {
	now := time.Now()
	older := now.Add(-1 * time.Hour)
	newer := now.Add(1 * time.Hour)

	tests := []struct {
		name     string
		exeMod   time.Time
		exeErr   error
		pidMod   time.Time
		pidErr   error
		expected bool
	}{
		{
			name:     "binary newer than PID",
			exeMod:   newer,
			pidMod:   older,
			expected: true,
		},
		{
			name:     "binary older than PID",
			exeMod:   older,
			pidMod:   newer,
			expected: false,
		},
		{
			name:     "same time",
			exeMod:   now,
			pidMod:   now,
			expected: false,
		},
		{
			name:     "exe stat error returns true",
			exeErr:   os.ErrNotExist,
			pidMod:   now,
			expected: true,
		},
		{
			name:     "pid stat error returns true",
			exeMod:   now,
			pidErr:   os.ErrNotExist,
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fs := &deps.MockFileSystem{
				StatFunc: func(path string) (os.FileInfo, error) {
					switch path {
					case "/usr/local/bin/pop":
						if tt.exeErr != nil {
							return nil, tt.exeErr
						}
						return &deps.MockFileInfo{ModTimeVal: tt.exeMod}, nil
					case "/tmp/monitor.pid":
						if tt.pidErr != nil {
							return nil, tt.pidErr
						}
						return &deps.MockFileInfo{ModTimeVal: tt.pidMod}, nil
					default:
						return nil, os.ErrNotExist
					}
				},
			}

			result := binaryNewerThanPIDWith(fs, "/usr/local/bin/pop", "/tmp/monitor.pid")
			if result != tt.expected {
				t.Errorf("binaryNewerThanPIDWith() = %v, want %v", result, tt.expected)
			}
		})
	}
}

func TestRunPaneMonitorStatusWith(t *testing.T) {
	t.Run("shows running daemon with panes", func(t *testing.T) {
		panes := map[string]*monitor.PaneEntry{
			"%1": {
				PaneID:    "%1",
				Session:   "project-a",
				Status:    monitor.StatusWorking,
				UpdatedAt: time.Date(2026, 3, 25, 14, 30, 0, 0, time.UTC),
			},
		}
		stateData, _ := json.Marshal(&monitor.State{Panes: panes})
		pid := fmt.Sprintf("%d", os.Getpid())

		d := &monitor.Deps{
			FS: &deps.MockFileSystem{
				GetenvFunc: func(key string) string {
					if key == "XDG_DATA_HOME" {
						return "/mock/data"
					}
					return ""
				},
				ReadFileFunc: func(path string) ([]byte, error) {
					switch path {
					case "/mock/data/pop/monitor.pid":
						return []byte(pid), nil
					case "/mock/data/pop/monitor.json":
						return stateData, nil
					default:
						return nil, os.ErrNotExist
					}
				},
			},
		}

		var buf bytes.Buffer
		err := runPaneMonitorStatusWith(d, &buf)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		output := buf.String()
		if !strings.Contains(output, "Daemon: running") {
			t.Errorf("output missing 'Daemon: running': %s", output)
		}
		if !strings.Contains(output, "%1") {
			t.Errorf("output missing pane ID: %s", output)
		}
		if !strings.Contains(output, "project-a") {
			t.Errorf("output missing session name: %s", output)
		}
		if !strings.Contains(output, "working") {
			t.Errorf("output missing status: %s", output)
		}
	})

	t.Run("shows stopped daemon", func(t *testing.T) {
		d := &monitor.Deps{
			FS: &deps.MockFileSystem{
				GetenvFunc: func(key string) string {
					if key == "XDG_DATA_HOME" {
						return "/mock/data"
					}
					return ""
				},
				ReadFileFunc: func(path string) ([]byte, error) {
					return nil, os.ErrNotExist
				},
			},
		}

		var buf bytes.Buffer
		err := runPaneMonitorStatusWith(d, &buf)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		output := buf.String()
		if !strings.Contains(output, "Daemon: stopped") {
			t.Errorf("output missing 'Daemon: stopped': %s", output)
		}
		if !strings.Contains(output, "No monitored panes") {
			t.Errorf("output missing 'No monitored panes': %s", output)
		}
	})
}

func TestTmuxPaneSessionWith(t *testing.T) {
	tmux := &deps.MockTmux{
		CommandFunc: func(args ...string) (string, error) {
			if args[0] == "display-message" {
				return "project-a", nil
			}
			return "", nil
		},
	}

	session, err := tmuxPaneSessionWith(tmux, "%1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if session != "project-a" {
		t.Errorf("got %q, want %q", session, "project-a")
	}
}

func TestTmuxPaneInfoWith(t *testing.T) {
	t.Run("parses tab-separated session and command", func(t *testing.T) {
		tmux := &deps.MockTmux{
			CommandFunc: func(args ...string) (string, error) {
				if args[0] == "display-message" {
					return "project-a\topencode", nil
				}
				return "", nil
			},
		}
		session, cmdName, err := tmuxPaneInfoWith(tmux, "%1")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if session != "project-a" {
			t.Errorf("session = %q, want %q", session, "project-a")
		}
		if cmdName != "opencode" {
			t.Errorf("cmdName = %q, want %q", cmdName, "opencode")
		}
	})

	t.Run("propagates tmux errors", func(t *testing.T) {
		tmux := &deps.MockTmux{
			CommandFunc: func(args ...string) (string, error) {
				return "", fmt.Errorf("pane not found")
			},
		}
		_, _, err := tmuxPaneInfoWith(tmux, "%nope")
		if err == nil {
			t.Error("expected error, got nil")
		}
	})

	t.Run("errors on malformed output", func(t *testing.T) {
		tmux := &deps.MockTmux{
			CommandFunc: func(args ...string) (string, error) {
				return "no-tab-here", nil
			},
		}
		_, _, err := tmuxPaneInfoWith(tmux, "%1")
		if err == nil {
			t.Error("expected error on malformed output, got nil")
		}
	})
}

func TestIsPlainShellCommand(t *testing.T) {
	tests := []struct {
		cmd  string
		want bool
	}{
		// Shells — should be skipped.
		{"zsh", true},
		{"bash", true},
		{"fish", true},
		{"sh", true},
		{"dash", true},
		{"ksh", true},
		{"tcsh", true},
		{"csh", true},
		// Login-shell marker.
		{"-zsh", true},
		{"-bash", true},
		{"-fish", true},
		// Agents / non-shells — should NOT be treated as shells.
		{"opencode", false},
		{"claude", false},
		{"pi", false},
		{"node", false},
		{"bun", false},
		{"python", false},
		{"vim", false},
		{"nvim", false},
		{"less", false},
		{"", false},
	}
	for _, tt := range tests {
		t.Run(tt.cmd, func(t *testing.T) {
			if got := isPlainShellCommand(tt.cmd); got != tt.want {
				t.Errorf("isPlainShellCommand(%q) = %v, want %v", tt.cmd, got, tt.want)
			}
		})
	}
}

func TestIsActiveTmuxPaneWith(t *testing.T) {
	tests := []struct {
		name     string
		output   string
		err      error
		expected bool
	}{
		{
			name:     "active pane",
			output:   "1 1 1",
			expected: true,
		},
		{
			name:     "inactive pane",
			output:   "0 1 1",
			expected: false,
		},
		{
			name:     "detached session",
			output:   "1 1 0",
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
			result := isActiveTmuxPaneWith(tmux, "%1")
			if result != tt.expected {
				t.Errorf("got %v, want %v", result, tt.expected)
			}
		})
	}
}

func TestBuildMonitorHandler_DispatchesByCmd(t *testing.T) {
	dir := t.TempDir()
	statePath := dir + "/monitor.json"
	emptyState := &monitor.State{Panes: map[string]*monitor.PaneEntry{}}
	data, _ := json.Marshal(emptyState)
	if err := os.WriteFile(statePath, data, 0644); err != nil {
		t.Fatal(err)
	}

	handler := buildMonitorHandler(&deps.MockTmux{}, statePath)

	t.Run("unknown command returns error", func(t *testing.T) {
		resp := handler(monitor.Request{Cmd: "definitely-not-a-real-command", PaneID: "%1"})
		if resp.OK {
			t.Error("expected OK=false for unknown command")
		}
		if !strings.Contains(resp.Error, "unknown command") {
			t.Errorf("error = %q, want containing 'unknown command'", resp.Error)
		}
	})

	t.Run("empty cmd is treated as set-status (backward compat)", func(t *testing.T) {
		// Empty cmd should not produce "unknown command".
		resp := handler(monitor.Request{Cmd: "", PaneID: ""})
		if !resp.OK {
			t.Errorf("expected OK=true for empty pane_id (set-status no-op), got error %q", resp.Error)
		}
	})
}

func TestHandleSetFollowing(t *testing.T) {
	t.Run("rejects request without pane_id", func(t *testing.T) {
		statePath := setupEmptyState(t)
		follow := true
		resp := handleSetFollowing(&deps.MockTmux{}, statePath, monitor.Request{
			Cmd:       "set-following",
			Following: &follow,
		})
		if resp.OK {
			t.Error("expected OK=false")
		}
		if !strings.Contains(resp.Error, "pane_id") {
			t.Errorf("error = %q, want containing 'pane_id'", resp.Error)
		}
	})

	t.Run("rejects request with nil following", func(t *testing.T) {
		statePath := setupEmptyState(t)
		resp := handleSetFollowing(&deps.MockTmux{}, statePath, monitor.Request{
			Cmd:    "set-following",
			PaneID: "%1",
		})
		if resp.OK {
			t.Error("expected OK=false")
		}
		if !strings.Contains(resp.Error, "following") {
			t.Errorf("error = %q, want containing 'following'", resp.Error)
		}
	})

	t.Run("auto-registers untracked pane on follow=true", func(t *testing.T) {
		statePath := setupEmptyState(t)
		tmux := newPaneInfoMockTmux(map[string]string{
			"%8": "proj-x\tclaude",
		})
		follow := true
		resp := handleSetFollowing(tmux, statePath, monitor.Request{
			Cmd:       "set-following",
			PaneID:    "%8",
			Following: &follow,
		})
		if !resp.OK {
			t.Fatalf("unexpected error: %s", resp.Error)
		}
		state := loadStateFromPath(t, statePath)
		entry, ok := state.Panes["%8"]
		if !ok {
			t.Fatalf("expected %%8 to be auto-registered")
		}
		if !entry.Following {
			t.Error("expected Following = true")
		}
		if entry.Status != monitor.StatusIdle {
			t.Errorf("status = %q, want idle", entry.Status)
		}
		if entry.Session != "proj-x" {
			t.Errorf("session = %q, want proj-x", entry.Session)
		}
	})

	t.Run("unfollow on untracked pane is a no-op", func(t *testing.T) {
		statePath := setupEmptyState(t)
		// Tmux must not be called.
		tmux := &deps.MockTmux{
			CommandFunc: func(args ...string) (string, error) {
				t.Errorf("unexpected tmux call: %v", args)
				return "", nil
			},
		}
		follow := false
		resp := handleSetFollowing(tmux, statePath, monitor.Request{
			Cmd:       "set-following",
			PaneID:    "%9",
			Following: &follow,
		})
		if !resp.OK {
			t.Errorf("expected OK=true, got error %q", resp.Error)
		}
		state := loadStateFromPath(t, statePath)
		if len(state.Panes) != 0 {
			t.Errorf("expected empty state, got %d entries", len(state.Panes))
		}
	})

	t.Run("unfollow clears note on tracked pane", func(t *testing.T) {
		dir := t.TempDir()
		statePath := dir + "/monitor.json"
		seed := &monitor.State{
			Panes: map[string]*monitor.PaneEntry{
				"%2": {
					PaneID:    "%2",
					Session:   "proj-y",
					Status:    monitor.StatusIdle,
					Following: true,
					Note:      "remember to check this",
				},
			},
		}
		data, _ := json.Marshal(seed)
		if err := os.WriteFile(statePath, data, 0644); err != nil {
			t.Fatal(err)
		}

		follow := false
		resp := handleSetFollowing(&deps.MockTmux{}, statePath, monitor.Request{
			Cmd:       "set-following",
			PaneID:    "%2",
			Following: &follow,
		})
		if !resp.OK {
			t.Fatalf("unexpected error: %s", resp.Error)
		}
		state := loadStateFromPath(t, statePath)
		entry := state.Panes["%2"]
		if entry.Following {
			t.Error("expected Following = false")
		}
		if entry.Note != "" {
			t.Errorf("note = %q, want cleared", entry.Note)
		}
	})
}

func setupEmptyState(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	statePath := dir + "/monitor.json"
	emptyState := &monitor.State{Panes: map[string]*monitor.PaneEntry{}}
	data, _ := json.Marshal(emptyState)
	if err := os.WriteFile(statePath, data, 0644); err != nil {
		t.Fatal(err)
	}
	return statePath
}

func loadStateFromPath(t *testing.T, path string) *monitor.State {
	t.Helper()
	state, err := monitor.Load(path)
	if err != nil {
		t.Fatalf("load state: %v", err)
	}
	return state
}

func TestUninstallTmuxAutoReadHooksWith(t *testing.T) {
	var removedHooks []string
	tmux := &deps.MockTmux{
		CommandFunc: func(args ...string) (string, error) {
			if args[0] == "show-hooks" {
				return "after-select-pane[0] run-shell \"pop pane set-status #{pane_id} read\"\n" +
					"after-select-pane[1] run-shell \"echo other hook\"\n" +
					"session-window-changed[0] run-shell \"pop monitor check\"\n", nil
			}
			if args[0] == "set-hook" && args[1] == "-gu" {
				removedHooks = append(removedHooks, args[2])
			}
			return "", nil
		},
	}

	uninstallTmuxAutoReadHooksWith(tmux)

	if len(removedHooks) != 2 {
		t.Fatalf("removed %d hooks, want 2", len(removedHooks))
	}
	// Should remove the pop hooks but not "echo other hook"
	if removedHooks[0] != "after-select-pane[0]" && removedHooks[0] != "session-window-changed[0]" {
		t.Errorf("unexpected removed hook: %q", removedHooks[0])
	}
}

