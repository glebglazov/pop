package cmd

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/glebglazov/pop/config"
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
		if entry.Status != monitor.StatusClear {
			t.Errorf("status = %q, want clear", entry.Status)
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
					Status:    monitor.StatusClear,
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

func TestHandleVisit(t *testing.T) {
	t.Run("rejects request without pane_id", func(t *testing.T) {
		statePath := setupEmptyState(t)
		resp := handleVisit(statePath, monitor.Request{Cmd: "visit"})
		if resp.OK {
			t.Error("expected OK=false")
		}
		if !strings.Contains(resp.Error, "pane_id") {
			t.Errorf("error = %q, want containing 'pane_id'", resp.Error)
		}
	})

	t.Run("no-op on untracked pane", func(t *testing.T) {
		statePath := setupEmptyState(t)
		// Tmux must not be called.
		resp := handleVisit(statePath, monitor.Request{
			Cmd:    "visit",
			PaneID: "%99",
		})
		if !resp.OK {
			t.Errorf("expected OK=true, got error %q", resp.Error)
		}
		state := loadStateFromPath(t, statePath)
		if len(state.Panes) != 0 {
			t.Errorf("expected empty state, got %d entries", len(state.Panes))
		}
	})

	t.Run("updates LastActiveAt on tracked pane", func(t *testing.T) {
		dir := t.TempDir()
		statePath := dir + "/monitor.json"
		before := time.Now().Add(-1 * time.Hour)
		seed := &monitor.State{
			Panes: map[string]*monitor.PaneEntry{
				"%3": {
					PaneID:       "%3",
					Session:      "proj-z",
					Status:       monitor.StatusClear,
					LastActiveAt: before,
				},
			},
		}
		data, _ := json.Marshal(seed)
		if err := os.WriteFile(statePath, data, 0644); err != nil {
			t.Fatal(err)
		}

		resp := handleVisit(statePath, monitor.Request{
			Cmd:    "visit",
			PaneID: "%3",
		})
		if !resp.OK {
			t.Fatalf("unexpected error: %s", resp.Error)
		}
		state := loadStateFromPath(t, statePath)
		entry := state.Panes["%3"]
		if !entry.LastActiveAt.After(before) {
			t.Errorf("LastActiveAt = %v, want after %v", entry.LastActiveAt, before)
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

func seedMonitorStateDir(t *testing.T, panes map[string]*monitor.PaneEntry) (statePath, dataHome string) {
	t.Helper()
	dir := t.TempDir()
	dataHome = dir
	stateDir := filepath.Join(dir, "pop")
	if err := os.MkdirAll(stateDir, 0755); err != nil {
		t.Fatal(err)
	}
	statePath = filepath.Join(stateDir, "monitor.json")
	if panes == nil {
		panes = map[string]*monitor.PaneEntry{}
	}
	data, _ := json.Marshal(&monitor.State{Panes: panes})
	if err := os.WriteFile(statePath, data, 0644); err != nil {
		t.Fatal(err)
	}
	return statePath, dataHome
}

func parityMockTmux(active bool) *deps.MockTmux {
	activeOut := "0 0 0"
	if active {
		activeOut = "1 1 1"
	}
	return &deps.MockTmux{
		CommandFunc: func(args ...string) (string, error) {
			if len(args) >= 5 && args[0] == "display-message" {
				switch args[4] {
				case "#{session_name}\t#{pane_current_command}":
					return "proj-x\tclaude", nil
				case "#{pane_active} #{window_active} #{session_attached}":
					return activeOut, nil
				}
			}
			return "", nil
		},
	}
}

func assertPaneStatesMatch(t *testing.T, handlerState, directState *monitor.State) {
	t.Helper()
	if len(handlerState.Panes) != len(directState.Panes) {
		t.Fatalf("pane count: handler=%d direct=%d", len(handlerState.Panes), len(directState.Panes))
	}
	for id, he := range handlerState.Panes {
		de, ok := directState.Panes[id]
		if !ok {
			t.Fatalf("pane %s in handler state but missing from direct state", id)
		}
		if he.Status != de.Status || he.Session != de.Session || he.Label != de.Label ||
			he.Following != de.Following || he.Note != de.Note {
			t.Errorf("pane %s: handler=%+v direct=%+v", id, he, de)
		}
	}
}

func TestSetStatus_HandlerAndDirectParity(t *testing.T) {
	tests := []struct {
		name    string
		initial map[string]*monitor.PaneEntry
		req     monitor.Request
		cfg     *config.Config
		tmux    *deps.MockTmux
	}{
		{
			name:    "auto-registers untracked pane",
			initial: map[string]*monitor.PaneEntry{},
			req: monitor.Request{
				Cmd:    "set-status",
				PaneID: "%7",
				Status: "working",
			},
			cfg:  &config.Config{},
			tmux: parityMockTmux(false),
		},
		{
			name: "updates tracked pane to clear",
			initial: map[string]*monitor.PaneEntry{
				"%1": {PaneID: "%1", Session: "test", Status: monitor.StatusWorking},
			},
			req: monitor.Request{
				Cmd:    "set-status",
				PaneID: "%1",
				Status: "clear",
			},
			cfg:  &config.Config{},
			tmux: parityMockTmux(false),
		},
		{
			name: "dismiss unread on active pane when policy enabled",
			initial: map[string]*monitor.PaneEntry{
				"%1": {PaneID: "%1", Session: "test", Status: monitor.StatusWorking},
			},
			req: monitor.Request{
				Cmd:    "set-status",
				PaneID: "%1",
				Status: "unread",
			},
			cfg: &config.Config{
				PaneMonitoring: &config.PaneMonitoringConfig{
					DismissUnreadInActivePane: true,
				},
			},
			tmux: parityMockTmux(true),
		},
		{
			name:    "no-register leaves untracked pane untouched",
			initial: map[string]*monitor.PaneEntry{},
			req: monitor.Request{
				Cmd:        "set-status",
				PaneID:     "%9",
				Status:     "clear",
				NoRegister: true,
			},
			cfg:  &config.Config{},
			tmux: parityMockTmux(false),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			handlerPath, _ := seedMonitorStateDir(t, clonePaneMap(tt.initial))
			directPath, dataHome := seedMonitorStateDir(t, clonePaneMap(tt.initial))

			if tt.cfg != nil && tt.cfg.PaneMonitoring != nil && tt.cfg.PaneMonitoring.DismissUnreadInActivePane {
				configDir := t.TempDir()
				t.Setenv("XDG_CONFIG_HOME", configDir)
				popDir := filepath.Join(configDir, "pop")
				if err := os.MkdirAll(popDir, 0755); err != nil {
					t.Fatal(err)
				}
				configBody := "[pane_monitoring]\ndismiss_unread_in_active_pane = true\n"
				if err := os.WriteFile(filepath.Join(popDir, "config.toml"), []byte(configBody), 0644); err != nil {
					t.Fatal(err)
				}
			} else {
				t.Setenv("XDG_CONFIG_HOME", t.TempDir())
			}

			resp := handleSetStatus(tt.tmux, handlerPath, tt.req)
			if !resp.OK {
				t.Fatalf("handler error: %s", resp.Error)
			}

			t.Setenv("XDG_DATA_HOME", dataHome)
			if err := runPaneSetStatusDirect(tt.tmux, tt.cfg, tt.req.PaneID, tt.req.Status, tt.req.Source, tt.req.NoRegister, tt.req.Label); err != nil {
				t.Fatalf("direct error: %v", err)
			}

			handlerState := loadStateFromPath(t, handlerPath)
			directState := loadStateFromPath(t, directPath)
			assertPaneStatesMatch(t, handlerState, directState)
		})
	}
}

func clonePaneMap(in map[string]*monitor.PaneEntry) map[string]*monitor.PaneEntry {
	if in == nil {
		return map[string]*monitor.PaneEntry{}
	}
	out := make(map[string]*monitor.PaneEntry, len(in))
	for id, e := range in {
		copied := *e
		out[id] = &copied
	}
	return out
}

func TestUninstallTmuxAutoClearHooksWith(t *testing.T) {
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

	uninstallTmuxAutoClearHooksWith(tmux)

	if len(removedHooks) != 2 {
		t.Fatalf("removed %d hooks, want 2", len(removedHooks))
	}
	// Should remove the pop hooks but not "echo other hook"
	if removedHooks[0] != "after-select-pane[0]" && removedHooks[0] != "session-window-changed[0]" {
		t.Errorf("unexpected removed hook: %q", removedHooks[0])
	}
}
