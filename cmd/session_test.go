package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/glebglazov/pop/history"
	"github.com/glebglazov/pop/internal/deps"
	"github.com/glebglazov/pop/internal/tmux/tmuxtest"
	"github.com/glebglazov/pop/monitor"
	"github.com/glebglazov/pop/ui"
)

func TestIsStandaloneSession(t *testing.T) {
	t.Parallel()
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
	t.Parallel()
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
	t.Parallel()
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
	t.Parallel()
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
	t.Parallel()
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
	t.Parallel()
	t.Run("returns sessions needing attention", func(t *testing.T) {
		d := mockMonitorDeps(map[string]*monitor.PaneEntry{
			"%1": {PaneID: "%1", Session: "proj-a", Status: monitor.StatusUnread},
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

func TestCurrentTmuxSessionWith(t *testing.T) {
	t.Parallel()
	t.Run("returns session name", func(t *testing.T) {
		mod := &tmuxtest.Fake{CurrentSessionName: "my-session"}
		result := currentTmuxSessionWith(mod)
		if result != "my-session" {
			t.Errorf("got %q, want %q", result, "my-session")
		}
	})

	t.Run("returns empty on error", func(t *testing.T) {
		mod := &tmuxtest.Fake{CurrentSessionErr: fmt.Errorf("not in tmux")}
		result := currentTmuxSessionWith(mod)
		if result != "" {
			t.Errorf("got %q, want empty", result)
		}
	})
}

func TestTmuxPaneCommandsWith(t *testing.T) {
	t.Parallel()
	mod := &tmuxtest.Fake{
		PaneCommandMap: map[string]string{"%1": "zsh", "%2": "node", "%3": "vim"},
	}

	result := tmuxPaneCommandsWith(mod)
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
	t.Parallel()
	t.Run("returns pane content", func(t *testing.T) {
		mod := &tmuxtest.Fake{
			PreviewContent: map[string]string{"%5": "line 1\nline 2\nline 3"},
		}
		result := capturePanePreviewWith(mod, "%5")
		if result != "line 1\nline 2\nline 3" {
			t.Errorf("got %q", result)
		}
	})

	t.Run("returns empty on error", func(t *testing.T) {
		mod := &tmuxtest.Fake{
			CapturePreviewFunc: func(string) (string, error) {
				return "", fmt.Errorf("pane not found")
			},
		}
		result := capturePanePreviewWith(mod, "%99")
		if result != "" {
			t.Errorf("got %q, want empty", result)
		}
	})
}

func TestKillTmuxSessionByNameWith(t *testing.T) {
	t.Parallel()
	mod := &tmuxtest.Fake{Live: map[string]string{"my-session": "/proj"}}

	killTmuxSessionByNameWith(mod, "my-session")
	if len(mod.Killed) != 1 || mod.Killed[0] != "my-session" {
		t.Errorf("killed = %v, want [my-session]", mod.Killed)
	}
	if _, ok := mod.Live["my-session"]; ok {
		t.Error("session still live after kill")
	}
}

func TestSwitchToTmuxTargetWith_InTmux(t *testing.T) {
	t.Parallel()
	mod := &tmuxtest.Fake{Inside: true}

	err := switchToTmuxTargetWith(mod, "target-session")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(mod.Switched) != 1 || mod.Switched[0] != "target-session" {
		t.Errorf("switched = %v, want [target-session]", mod.Switched)
	}
}

func TestSwitchToTmuxTargetAndZoomWith_InTmux(t *testing.T) {
	t.Parallel()
	t.Run("switches and zooms an unzoomed window", func(t *testing.T) {
		mod := &tmuxtest.Fake{Inside: true}

		if err := switchToTmuxTargetAndZoomWith(mod, "%5"); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(mod.Switched) != 1 || mod.Switched[0] != "%5" {
			t.Errorf("switched = %v, want [%%5]", mod.Switched)
		}
		if !mod.Zoomed["%5"] {
			t.Error("target window not zoomed")
		}
	})

	t.Run("leaves an already-zoomed window maximized", func(t *testing.T) {
		mod := &tmuxtest.Fake{Inside: true, Zoomed: map[string]bool{"%5": true}}

		if err := switchToTmuxTargetAndZoomWith(mod, "%5"); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(mod.Switched) != 1 || mod.Switched[0] != "%5" {
			t.Errorf("switched = %v, want [%%5]", mod.Switched)
		}
		// zoom-only-if-not-zoomed: an already-zoomed window is never toggled
		// back to a split layout.
		if !mod.Zoomed["%5"] {
			t.Error("already-zoomed window was toggled off")
		}
	})
}
