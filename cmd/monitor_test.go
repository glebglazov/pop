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

