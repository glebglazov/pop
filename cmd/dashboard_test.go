package cmd

import (
	"testing"
	"time"

	"github.com/glebglazov/pop/history"
	"github.com/glebglazov/pop/ui"
)

func TestPathBase(t *testing.T) {
	tests := []struct {
		name     string
		path     string
		expected string
	}{
		{
			name:     "regular path",
			path:     "/home/user/project",
			expected: "project",
		},
		{
			name:     "tmux prefix path without slash",
			path:     "tmux:scratch",
			expected: "tmux:scratch",
		},
		{
			name:     "tmux prefix path with slash",
			path:     "tmux:project/worktree",
			expected: "worktree",
		},
		{
			name:     "no slashes",
			path:     "standalone",
			expected: "standalone",
		},
		{
			name:     "trailing slash stripped externally",
			path:     "/a/b/c",
			expected: "c",
		},
		{
			name:     "single segment with leading slash",
			path:     "/root",
			expected: "root",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := pathBase(tt.path)
			if result != tt.expected {
				t.Errorf("pathBase(%q) = %q, want %q", tt.path, result, tt.expected)
			}
		})
	}
}

func TestPositionCurrentPane(t *testing.T) {
	t.Run("untracked pane is injected at end", func(t *testing.T) {
		// Scenario: monitored panes are pop (working) and vcdr (needs-attention),
		// current pane is "abc" which is not monitored.
		// After normal sort: pop (working=1), vcdr (needs_attention=2)
		// Expected: pop, vcdr, abc (under cursor at end)
		panes := []ui.AttentionPane{
			{PaneID: "%1", Session: "pop", Name: "pop %1", Status: ui.AttentionWorking},
			{PaneID: "%2", Session: "vcdr", Name: "vcdr %2", Status: ui.AttentionNeedsAttention},
		}
		paneCommands := map[string]string{"%3": "zsh"}

		result := positionCurrentPane(panes, "%3", "abc", paneCommands)

		if len(result) != 3 {
			t.Fatalf("expected 3 panes, got %d", len(result))
		}
		// Original order preserved for first two
		if result[0].PaneID != "%1" {
			t.Errorf("pane[0]: expected %%1 (pop), got %s", result[0].PaneID)
		}
		if result[1].PaneID != "%2" {
			t.Errorf("pane[1]: expected %%2 (vcdr), got %s", result[1].PaneID)
		}
		// Current pane injected at end
		if result[2].PaneID != "%3" {
			t.Errorf("pane[2]: expected %%3 (abc), got %s", result[2].PaneID)
		}
		if result[2].Session != "abc" {
			t.Errorf("pane[2] session: expected abc, got %s", result[2].Session)
		}
		if result[2].Status != ui.AttentionIdle {
			t.Errorf("pane[2] status: expected idle, got %d", result[2].Status)
		}
		if result[2].Name != "abc %3 (zsh)" {
			t.Errorf("pane[2] name: expected %q, got %q", "abc %3 (zsh)", result[2].Name)
		}
	})

	t.Run("tracked pane is moved to end", func(t *testing.T) {
		// Scenario: monitored panes are pop (working) and vcdr (needs-attention),
		// current pane is pop's pane (%1).
		// After normal sort: pop (working=1), vcdr (needs_attention=2)
		// Expected: vcdr, pop (under cursor at end)
		panes := []ui.AttentionPane{
			{PaneID: "%1", Session: "pop", Name: "pop %1", Status: ui.AttentionWorking},
			{PaneID: "%2", Session: "vcdr", Name: "vcdr %2", Status: ui.AttentionNeedsAttention},
		}

		result := positionCurrentPane(panes, "%1", "pop", nil)

		if len(result) != 2 {
			t.Fatalf("expected 2 panes, got %d", len(result))
		}
		if result[0].PaneID != "%2" {
			t.Errorf("pane[0]: expected %%2 (vcdr), got %s", result[0].PaneID)
		}
		if result[1].PaneID != "%1" {
			t.Errorf("pane[1]: expected %%1 (pop), got %s", result[1].PaneID)
		}
	})

	t.Run("pane already at end is not moved", func(t *testing.T) {
		panes := []ui.AttentionPane{
			{PaneID: "%1", Session: "pop", Name: "pop %1", Status: ui.AttentionWorking},
			{PaneID: "%2", Session: "vcdr", Name: "vcdr %2", Status: ui.AttentionNeedsAttention},
		}

		result := positionCurrentPane(panes, "%2", "vcdr", nil)

		if len(result) != 2 {
			t.Fatalf("expected 2 panes, got %d", len(result))
		}
		if result[0].PaneID != "%1" {
			t.Errorf("pane[0]: expected %%1, got %s", result[0].PaneID)
		}
		if result[1].PaneID != "%2" {
			t.Errorf("pane[1]: expected %%2, got %s", result[1].PaneID)
		}
	})

	t.Run("untracked pane without command", func(t *testing.T) {
		panes := []ui.AttentionPane{
			{PaneID: "%1", Session: "pop", Name: "pop %1", Status: ui.AttentionWorking},
		}

		result := positionCurrentPane(panes, "%9", "myproject", nil)

		if len(result) != 2 {
			t.Fatalf("expected 2 panes, got %d", len(result))
		}
		if result[1].Name != "myproject %9" {
			t.Errorf("pane[1] name: expected %q, got %q", "myproject %9", result[1].Name)
		}
	})

	t.Run("empty list with untracked pane", func(t *testing.T) {
		result := positionCurrentPane(nil, "%5", "abc", nil)

		if len(result) != 1 {
			t.Fatalf("expected 1 pane, got %d", len(result))
		}
		if result[0].PaneID != "%5" {
			t.Errorf("pane[0]: expected %%5, got %s", result[0].PaneID)
		}
		if result[0].Session != "abc" {
			t.Errorf("pane[0] session: expected abc, got %s", result[0].Session)
		}
	})
}

func TestSessionAccessTime(t *testing.T) {
	now := time.Now()
	hist := &history.History{
		Entries: []history.Entry{
			{Path: "/home/user/my.project", LastAccess: now.Add(-2 * time.Hour)},
			{Path: "/home/user/game_server", LastAccess: now.Add(-1 * time.Hour)},
		},
	}

	tests := []struct {
		name     string
		session  string
		hist     *history.History
		expected int64
	}{
		{
			name:     "exact match via sanitized base",
			session:  "my_project", // sanitize turns . into _
			hist:     hist,
			expected: now.Add(-2 * time.Hour).Unix(),
		},
		{
			name:     "exact match without sanitization",
			session:  "game_server",
			hist:     hist,
			expected: now.Add(-1 * time.Hour).Unix(),
		},
		{
			name:     "no match returns zero",
			session:  "nonexistent",
			hist:     hist,
			expected: 0,
		},
		{
			name:     "nil history returns zero",
			session:  "anything",
			hist:     nil,
			expected: 0,
		},
		{
			name:     "worktree session partial match",
			session:  "repo/game_server",
			hist:     hist,
			expected: now.Add(-1 * time.Hour).Unix(),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := sessionAccessTime(tt.session, tt.hist, nil)
			if result != tt.expected {
				t.Errorf("sessionAccessTime(%q) = %d, want %d", tt.session, result, tt.expected)
			}
		})
	}
}
