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

	t.Run("identify reports daemon identity", func(t *testing.T) {
		resp := handler(monitor.Request{Cmd: "identify"})
		if !resp.OK {
			t.Fatalf("identify returned error: %q", resp.Error)
		}
		if resp.PID != os.Getpid() {
			t.Errorf("PID = %d, want %d", resp.PID, os.Getpid())
		}
		if resp.ExeMod == 0 {
			t.Error("ExeMod = 0, want the running binary's mtime")
		}
	})
	// "shutdown" is intentionally not exercised here: it SIGTERMs the current
	// process, which would kill the test runner. Its routing is covered by the
	// dispatch switch; the signal path is verified manually.
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

func TestHandleSetStatus_IgnoresSourceBeforeStateWork(t *testing.T) {
	statePath := setupEmptyState(t)
	configDir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", configDir)
	popDir := filepath.Join(configDir, "pop")
	if err := os.MkdirAll(popDir, 0755); err != nil {
		t.Fatal(err)
	}
	configBody := "[pane_monitoring]\nignore_status_from = [\"tmux-global\"]\n"
	if err := os.WriteFile(filepath.Join(popDir, "config.toml"), []byte(configBody), 0644); err != nil {
		t.Fatal(err)
	}

	tmuxCalled := false
	tmux := &deps.MockTmux{
		CommandFunc: func(args ...string) (string, error) {
			tmuxCalled = true
			return "", nil
		},
	}

	resp := handleSetStatus(tmux, statePath, monitor.Request{
		Cmd:    "set-status",
		PaneID: "%1",
		Status: "working",
		Source: "tmux-global",
	})
	if !resp.OK {
		t.Fatalf("unexpected error: %s", resp.Error)
	}
	if tmuxCalled {
		t.Error("expected no tmux calls when source is ignored")
	}
	state := loadStateFromPath(t, statePath)
	if len(state.Panes) != 0 {
		t.Errorf("expected empty state, got %d panes", len(state.Panes))
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
			he.Following != de.Following {
			t.Errorf("pane %s: handler=%+v direct=%+v", id, he, de)
		}
	}
}

func TestSetStatus_HandlerAndDirectDelegation(t *testing.T) {
	initial := map[string]*monitor.PaneEntry{
		"%1": {PaneID: "%1", Session: "test", Status: monitor.StatusWorking},
	}
	handlerPath, _ := seedMonitorStateDir(t, clonePaneMap(initial))
	directPath, dataHome := seedMonitorStateDir(t, clonePaneMap(initial))
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	req := monitor.Request{Cmd: "set-status", PaneID: "%1", Status: "clear"}
	tmux := &deps.MockTmux{}

	resp := handleSetStatus(tmux, handlerPath, req)
	if !resp.OK {
		t.Fatalf("handler error: %s", resp.Error)
	}

	t.Setenv("XDG_DATA_HOME", dataHome)
	if err := runPaneSetStatusDirect(tmux, &config.Config{}, "%1", "clear", "", false, ""); err != nil {
		t.Fatalf("direct error: %v", err)
	}

	assertPaneStatesMatch(t, loadStateFromPath(t, handlerPath), loadStateFromPath(t, directPath))
}

func TestSetFollowing_HandlerAndDirectDelegation(t *testing.T) {
	followTrue := true
	initial := map[string]*monitor.PaneEntry{
		"%3": {PaneID: "%3", Session: "proj-b", Status: monitor.StatusWorking},
	}
	handlerPath, _ := seedMonitorStateDir(t, clonePaneMap(initial))
	directPath, dataHome := seedMonitorStateDir(t, clonePaneMap(initial))

	req := monitor.Request{
		Cmd:       "set-following",
		PaneID:    "%3",
		Following: &followTrue,
	}
	tmux := &deps.MockTmux{}

	resp := handleSetFollowing(tmux, handlerPath, req)
	if !resp.OK {
		t.Fatalf("handler error: %s", resp.Error)
	}

	t.Setenv("XDG_DATA_HOME", dataHome)
	if err := runPaneSetFollowDirect(tmux, "%3", true); err != nil {
		t.Fatalf("direct error: %v", err)
	}

	assertPaneStatesMatch(t, loadStateFromPath(t, handlerPath), loadStateFromPath(t, directPath))
}

func TestVisit_HandlerAndDirectDelegation(t *testing.T) {
	before := time.Now().Add(-1 * time.Hour)
	initial := map[string]*monitor.PaneEntry{
		"%3": {
			PaneID:       "%3",
			Session:      "proj-z",
			Status:       monitor.StatusUnread,
			LastActiveAt: before,
		},
	}
	handlerPath, _ := seedMonitorStateDir(t, clonePaneMap(initial))
	directPath, dataHome := seedMonitorStateDir(t, clonePaneMap(initial))

	req := monitor.Request{Cmd: "visit", PaneID: "%3"}

	resp := handleVisit(handlerPath, req)
	if !resp.OK {
		t.Fatalf("handler error: %s", resp.Error)
	}

	t.Setenv("XDG_DATA_HOME", dataHome)
	if err := runPaneVisitDirect("%3"); err != nil {
		t.Fatalf("direct error: %v", err)
	}

	assertPaneStatesMatch(t, loadStateFromPath(t, handlerPath), loadStateFromPath(t, directPath))
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
