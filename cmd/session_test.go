package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/glebglazov/pop/history"
	"github.com/glebglazov/pop/internal/deps"
	"github.com/glebglazov/pop/monitor"
	"github.com/glebglazov/pop/ui"
)

func TestIsStandaloneSession(t *testing.T) {
	tests := []struct {
		name     string
		item     ui.Item
		expected bool
	}{
		{
			name:     "standalone session",
			item:     ui.Item{Path: "tmux:scratch"},
			expected: true,
		},
		{
			name:     "standalone session with slashes in name",
			item:     ui.Item{Path: "tmux:my/session"},
			expected: true,
		},
		{
			name:     "directory project",
			item:     ui.Item{Path: "/home/user/project"},
			expected: false,
		},
		{
			name:     "empty path",
			item:     ui.Item{Path: ""},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := isStandaloneSession(tt.item)
			if result != tt.expected {
				t.Errorf("isStandaloneSession(%q) = %v, want %v", tt.item.Path, result, tt.expected)
			}
		})
	}
}

func TestStandaloneSessionName(t *testing.T) {
	tests := []struct {
		name     string
		item     ui.Item
		expected string
	}{
		{
			name:     "strips tmux prefix",
			item:     ui.Item{Path: "tmux:scratch"},
			expected: "scratch",
		},
		{
			name:     "preserves slashes",
			item:     ui.Item{Path: "tmux:my/session"},
			expected: "my/session",
		},
		{
			name:     "no prefix returns full path",
			item:     ui.Item{Path: "/home/user/project"},
			expected: "/home/user/project",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := standaloneSessionName(tt.item)
			if result != tt.expected {
				t.Errorf("standaloneSessionName(%q) = %q, want %q", tt.item.Path, result, tt.expected)
			}
		})
	}
}

func TestSessionHistoryPath(t *testing.T) {
	hist := &history.History{
		Entries: []history.Entry{
			{Path: "/home/user/my.project", LastAccess: time.Now()},
			{Path: "/home/user/game_server", LastAccess: time.Now()},
			{Path: "/home/user/other", LastAccess: time.Now()},
		},
	}

	tests := []struct {
		name        string
		sessionName string
		expected    string
	}{
		{
			name:        "exact match via sanitized base",
			sessionName: "my_project", // sanitize turns . into _
			expected:    "/home/user/my.project",
		},
		{
			name:        "exact match without sanitization needed",
			sessionName: "other",
			expected:    "/home/user/other",
		},
		{
			name:        "worktree session partial match on last component",
			sessionName: "game_server/worktrees-and-stuff",
			expected:    "tmux:game_server/worktrees-and-stuff",
		},
		{
			name:        "no match falls back to tmux prefix",
			sessionName: "nonexistent",
			expected:    "tmux:nonexistent",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := sessionHistoryPath(tt.sessionName, hist)
			if result != tt.expected {
				t.Errorf("sessionHistoryPath(%q) = %q, want %q", tt.sessionName, result, tt.expected)
			}
		})
	}
}

func TestSessionHistoryPath_PartialMatch(t *testing.T) {
	// When session name is "repo/worktree-name", the last component "worktree-name"
	// should match a history entry whose sanitized base is "worktree-name"
	hist := &history.History{
		Entries: []history.Entry{
			{Path: "/home/user/worktree-name", LastAccess: time.Now()},
		},
	}

	result := sessionHistoryPath("repo/worktree-name", hist)
	if result != "/home/user/worktree-name" {
		t.Errorf("got %q, want %q", result, "/home/user/worktree-name")
	}
}

// mockMonitorDeps creates a monitor.Deps with a filesystem that simulates
// the daemon running (PID file contains current process PID) and a state file
// with the given panes. If panes is nil, the state file won't exist.
func mockMonitorDeps(panes map[string]*monitor.PaneEntry) *monitor.Deps {
	pid := fmt.Sprintf("%d", os.Getpid())
	stateData, _ := json.Marshal(&monitor.State{Panes: panes})

	var savedData []byte

	return &monitor.Deps{
		FS: &deps.MockFileSystem{
			GetenvFunc: func(key string) string {
				if key == "XDG_DATA_HOME" {
					return "/mock/data"
				}
				return ""
			},
			UserHomeDirFunc: func() (string, error) {
				return "/mock/home", nil
			},
			ReadFileFunc: func(path string) ([]byte, error) {
				switch path {
				case "/mock/data/pop/monitor.pid":
					return []byte(pid), nil
				case "/mock/data/pop/monitor.json":
					if savedData != nil {
						return savedData, nil
					}
					if panes == nil {
						return nil, os.ErrNotExist
					}
					return stateData, nil
				default:
					return nil, os.ErrNotExist
				}
			},
			MkdirAllFunc: func(path string, perm os.FileMode) error {
				return nil
			},
			WriteFileFunc: func(path string, data []byte, perm os.FileMode) error {
				savedData = data
				return nil
			},
		},
	}
}

// mockMonitorDepsNotRunning creates a monitor.Deps where the daemon is not running
func mockMonitorDepsNotRunning() *monitor.Deps {
	return &monitor.Deps{
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
}

func TestLoadMonitorStateWith(t *testing.T) {
	t.Run("returns state when daemon running", func(t *testing.T) {
		d := mockMonitorDeps(map[string]*monitor.PaneEntry{
			"%1": {PaneID: "%1", Session: "proj", Status: monitor.StatusWorking},
		})

		state := loadMonitorStateWith(d)
		if state == nil {
			t.Fatal("expected non-nil state")
		}
		if len(state.Panes) != 1 {
			t.Errorf("got %d panes, want 1", len(state.Panes))
		}
	})

	t.Run("returns nil when daemon not running", func(t *testing.T) {
		d := mockMonitorDepsNotRunning()

		state := loadMonitorStateWith(d)
		if state != nil {
			t.Error("expected nil state when daemon not running")
		}
	})
}

func TestMonitorAttentionSessionsWith(t *testing.T) {
	t.Run("returns sessions needing attention", func(t *testing.T) {
		d := mockMonitorDeps(map[string]*monitor.PaneEntry{
			"%1": {PaneID: "%1", Session: "proj-a", Status: monitor.StatusNeedsAttention},
			"%2": {PaneID: "%2", Session: "proj-b", Status: monitor.StatusWorking},
		})

		result := monitorAttentionSessionsWith(d)
		if len(result) != 1 {
			t.Fatalf("got %d sessions, want 1", len(result))
		}
		if !result["proj-a"] {
			t.Error("expected proj-a to need attention")
		}
	})

	t.Run("returns nil when daemon not running", func(t *testing.T) {
		d := mockMonitorDepsNotRunning()

		result := monitorAttentionSessionsWith(d)
		if result != nil {
			t.Error("expected nil when daemon not running")
		}
	})
}

func TestMarkPaneReadWith(t *testing.T) {
	t.Run("marks pane as read", func(t *testing.T) {
		d := mockMonitorDeps(map[string]*monitor.PaneEntry{
			"%1": {PaneID: "%1", Session: "proj", Status: monitor.StatusNeedsAttention},
			"%2": {PaneID: "%2", Session: "proj", Status: monitor.StatusWorking},
		})

		markPaneReadWith(d, "%1")

		// Reload state to verify the write
		state := loadMonitorStateWith(d)
		if state == nil {
			t.Fatal("expected non-nil state after mark read")
		}
		entry, ok := state.Panes["%1"]
		if !ok {
			t.Fatal("pane %1 not found after mark read")
		}
		if entry.Status != monitor.StatusRead {
			t.Errorf("status = %q, want %q", entry.Status, monitor.StatusRead)
		}
	})

	t.Run("no-op for unknown pane", func(t *testing.T) {
		d := mockMonitorDeps(map[string]*monitor.PaneEntry{
			"%1": {PaneID: "%1", Session: "proj", Status: monitor.StatusWorking},
		})

		// Should not panic or error
		markPaneReadWith(d, "%99")
	})

	t.Run("no-op when daemon not running", func(t *testing.T) {
		d := mockMonitorDepsNotRunning()
		// Should not panic
		markPaneReadWith(d, "%1")
	})
}

func TestCurrentTmuxSessionWith(t *testing.T) {
	t.Run("returns session name", func(t *testing.T) {
		tmux := &deps.MockTmux{
			CommandFunc: func(args ...string) (string, error) {
				if args[0] == "display-message" {
					return "my-session", nil
				}
				return "", nil
			},
		}
		result := currentTmuxSessionWith(tmux)
		if result != "my-session" {
			t.Errorf("got %q, want %q", result, "my-session")
		}
	})

	t.Run("returns empty on error", func(t *testing.T) {
		tmux := &deps.MockTmux{
			CommandFunc: func(args ...string) (string, error) {
				return "", fmt.Errorf("not in tmux")
			},
		}
		result := currentTmuxSessionWith(tmux)
		if result != "" {
			t.Errorf("got %q, want empty", result)
		}
	})
}

func TestTmuxPaneCommandsWith(t *testing.T) {
	tmux := &deps.MockTmux{
		CommandFunc: func(args ...string) (string, error) {
			if args[0] == "list-panes" {
				return "%1 zsh\n%2 node\n%3 vim", nil
			}
			return "", nil
		},
	}

	result := tmuxPaneCommandsWith(tmux)
	if len(result) != 3 {
		t.Fatalf("got %d entries, want 3", len(result))
	}
	if result["%1"] != "zsh" {
		t.Errorf("%%1 = %q, want %q", result["%1"], "zsh")
	}
	if result["%2"] != "node" {
		t.Errorf("%%2 = %q, want %q", result["%2"], "node")
	}
}

func TestCapturePanePreviewWith(t *testing.T) {
	t.Run("returns pane content", func(t *testing.T) {
		tmux := &deps.MockTmux{
			CommandFunc: func(args ...string) (string, error) {
				if args[0] == "capture-pane" {
					return "line 1\nline 2\nline 3", nil
				}
				return "", nil
			},
		}
		result := capturePanePreviewWith(tmux, "%5")
		if result != "line 1\nline 2\nline 3" {
			t.Errorf("got %q", result)
		}
	})

	t.Run("returns empty on error", func(t *testing.T) {
		tmux := &deps.MockTmux{
			CommandFunc: func(args ...string) (string, error) {
				return "", fmt.Errorf("pane not found")
			},
		}
		result := capturePanePreviewWith(tmux, "%99")
		if result != "" {
			t.Errorf("got %q, want empty", result)
		}
	})
}

func TestKillTmuxSessionByNameWith(t *testing.T) {
	var killedSession string
	tmux := &deps.MockTmux{
		CommandFunc: func(args ...string) (string, error) {
			if args[0] == "kill-session" {
				killedSession = args[2] // "-t", sessionName
				return "", nil
			}
			return "", nil
		},
	}

	killTmuxSessionByNameWith(tmux, "my-session")
	if killedSession != "my-session" {
		t.Errorf("killed %q, want %q", killedSession, "my-session")
	}
}

func TestSwitchToTmuxTargetWith_InTmux(t *testing.T) {
	// Set TMUX env to simulate being inside tmux
	t.Setenv("TMUX", "/tmp/tmux-1000/default,12345,0")

	var switchedTo string
	tmux := &deps.MockTmux{
		CommandFunc: func(args ...string) (string, error) {
			if args[0] == "switch-client" {
				switchedTo = args[2]
				return "", nil
			}
			return "", nil
		},
	}

	err := switchToTmuxTargetWith(tmux, "target-session")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if switchedTo != "target-session" {
		t.Errorf("switched to %q, want %q", switchedTo, "target-session")
	}
}

func TestSwitchToTmuxTargetAndZoomWith_InTmux(t *testing.T) {
	t.Setenv("TMUX", "/tmp/tmux-1000/default,12345,0")

	var gotArgs []string
	tmux := &deps.MockTmux{
		CommandFunc: func(args ...string) (string, error) {
			gotArgs = args
			return "", nil
		},
	}

	err := switchToTmuxTargetAndZoomWith(tmux, "%5")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Single tmux invocation: switch-client -t %5 ; if-shell ... resize-pane -Z
	expected := []string{
		"switch-client", "-t", "%5", ";",
		"if-shell", "-F", "#{!=:#{window_zoomed_flag},1}",
		"resize-pane -Z",
	}
	if len(gotArgs) != len(expected) {
		t.Fatalf("expected args %v, got %v", expected, gotArgs)
	}
	for i := range expected {
		if gotArgs[i] != expected[i] {
			t.Errorf("arg[%d]: got %q, want %q", i, gotArgs[i], expected[i])
		}
	}
}

func TestUnmonitorPaneWith(t *testing.T) {
	t.Run("removes pane from state", func(t *testing.T) {
		d := mockMonitorDeps(map[string]*monitor.PaneEntry{
			"%1": {PaneID: "%1", Session: "proj", Status: monitor.StatusNeedsAttention},
			"%2": {PaneID: "%2", Session: "proj", Status: monitor.StatusWorking},
		})

		unmonitorPaneWith(d, "%1")

		// Reload state to verify the write
		state := loadMonitorStateWith(d)
		if state == nil {
			t.Fatal("expected non-nil state after unmonitor")
		}
		if _, ok := state.Panes["%1"]; ok {
			t.Error("pane %1 should have been removed")
		}
		if _, ok := state.Panes["%2"]; !ok {
			t.Error("pane %2 should still exist")
		}
	})

	t.Run("no-op when daemon not running", func(t *testing.T) {
		d := mockMonitorDepsNotRunning()
		// Should not panic
		unmonitorPaneWith(d, "%1")
	})
}
