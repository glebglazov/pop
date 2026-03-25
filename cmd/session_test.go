package cmd

import (
	"testing"
	"time"

	"github.com/glebglazov/pop/history"
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
