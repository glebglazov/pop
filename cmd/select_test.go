package cmd

import (
	"testing"
	"time"

	"github.com/glebglazov/pop/history"
	"github.com/glebglazov/pop/ui"
)

func TestLastNSegments(t *testing.T) {
	tests := []struct {
		name     string
		path     string
		n        int
		expected string
	}{
		{
			name:     "single segment (n=1)",
			path:     "/a/b/c/d",
			n:        1,
			expected: "d",
		},
		{
			name:     "two segments",
			path:     "/a/b/c/d",
			n:        2,
			expected: "c/d",
		},
		{
			name:     "three segments",
			path:     "/a/b/c/d",
			n:        3,
			expected: "b/c/d",
		},
		{
			name:     "n=0 returns basename",
			path:     "/a/b/c",
			n:        0,
			expected: "c",
		},
		{
			name:     "n exceeds path depth",
			path:     "/a/b",
			n:        5,
			expected: "a/b",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := lastNSegments(tt.path, tt.n)
			if result != tt.expected {
				t.Errorf("lastNSegments(%q, %d) = %q, want %q", tt.path, tt.n, result, tt.expected)
			}
		})
	}
}

func TestSanitizeSessionName(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "simple name unchanged",
			input:    "myproject",
			expected: "myproject",
		},
		{
			name:     "with slash unchanged",
			input:    "project/worktree",
			expected: "project/worktree",
		},
		{
			name:     "dots replaced with underscores",
			input:    "my.project",
			expected: "my_project",
		},
		{
			name:     "colons replaced with underscores",
			input:    "project:v1",
			expected: "project_v1",
		},
		{
			name:     "multiple dots and colons",
			input:    "my.project:v1.2.3",
			expected: "my_project_v1_2_3",
		},
		{
			name:     "worktree with dots",
			input:    "annual_calendar/feature.1",
			expected: "annual_calendar/feature_1",
		},
		{
			name:     "empty string",
			input:    "",
			expected: "",
		},
		{
			name:     "only special chars",
			input:    "...::",
			expected: "_____",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := sanitizeSessionName(tt.input)
			if result != tt.expected {
				t.Errorf("sanitizeSessionName(%q) = %q, want %q", tt.input, result, tt.expected)
			}
		})
	}
}

func TestSortItemsByHistory(t *testing.T) {
	now := time.Now()

	t.Run("no duplicates after resort changes order", func(t *testing.T) {
		// Items currently sorted: abc (oldest), sss (middle), ddd (newest)
		items := []ui.Item{
			{Name: "abc", Path: "/abc"},
			{Name: "sss", Path: "/sss"},
			{Name: "ddd", Path: "/ddd"},
		}

		// History: abc and sss have entries, ddd was just removed
		// This means ddd moves from end (had history) to front (no history)
		hist := &history.History{
			Entries: []history.Entry{
				{Path: "/abc", LastAccess: now.Add(-2 * time.Hour)},
				{Path: "/sss", LastAccess: now.Add(-1 * time.Hour)},
			},
		}

		result := sortItemsByHistory(items, hist)

		// Expected: ddd (no history), abc (oldest), sss (newer)
		expected := []string{"/ddd", "/abc", "/sss"}
		if len(result) != len(expected) {
			t.Fatalf("got %d items, want %d", len(result), len(expected))
		}
		for i, want := range expected {
			if result[i].Path != want {
				t.Errorf("result[%d].Path = %q, want %q", i, result[i].Path, want)
			}
		}

		// Verify no duplicates
		seen := make(map[string]bool)
		for _, item := range result {
			if seen[item.Path] {
				t.Errorf("duplicate item: %q", item.Path)
			}
			seen[item.Path] = true
		}
	})

	t.Run("preserves item context through resort", func(t *testing.T) {
		items := []ui.Item{
			{Name: "proj/wt1", Path: "/proj/wt1", Context: "proj"},
			{Name: "other", Path: "/other", Context: "other"},
		}

		hist := &history.History{
			Entries: []history.Entry{
				{Path: "/proj/wt1", LastAccess: now.Add(-1 * time.Hour)},
			},
		}

		result := sortItemsByHistory(items, hist)

		// "other" has no history -> goes first, "proj/wt1" has history -> goes second
		if result[0].Path != "/other" || result[0].Context != "other" {
			t.Errorf("result[0] = %+v, want Path=/other Context=other", result[0])
		}
		if result[1].Path != "/proj/wt1" || result[1].Context != "proj" {
			t.Errorf("result[1] = %+v, want Path=/proj/wt1 Context=proj", result[1])
		}
	})

	t.Run("no duplicates with many items and large reorder", func(t *testing.T) {
		// 5 items all with history, remove the middle one
		items := []ui.Item{
			{Name: "aaa", Path: "/aaa"},
			{Name: "bbb", Path: "/bbb"},
			{Name: "ccc", Path: "/ccc"},
			{Name: "ddd", Path: "/ddd"},
			{Name: "eee", Path: "/eee"},
		}

		// ccc removed from history -> moves to no-history group at front
		hist := &history.History{
			Entries: []history.Entry{
				{Path: "/aaa", LastAccess: now.Add(-4 * time.Hour)},
				{Path: "/bbb", LastAccess: now.Add(-3 * time.Hour)},
				{Path: "/ddd", LastAccess: now.Add(-1 * time.Hour)},
				{Path: "/eee", LastAccess: now},
			},
		}

		result := sortItemsByHistory(items, hist)

		if len(result) != 5 {
			t.Fatalf("got %d items, want 5", len(result))
		}

		seen := make(map[string]bool)
		for _, item := range result {
			if seen[item.Path] {
				t.Errorf("duplicate item: %q", item.Path)
			}
			seen[item.Path] = true
		}

		// ccc should be first (no history)
		if result[0].Path != "/ccc" {
			t.Errorf("result[0].Path = %q, want /ccc", result[0].Path)
		}
	})
}
