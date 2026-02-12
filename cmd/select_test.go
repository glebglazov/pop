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
			result := ui.LastNSegments(tt.path, tt.n)
			if result != tt.expected {
				t.Errorf("LastNSegments(%q, %d) = %q, want %q", tt.path, tt.n, result, tt.expected)
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

func TestBuildSessionAwareItems(t *testing.T) {
	now := time.Now()

	t.Run("standalone sessions detected correctly", func(t *testing.T) {
		baseItems := []ui.Item{
			{Name: "app", Path: "/app"},
			{Name: "api", Path: "/api"},
		}
		// Sessions: app matches project, api matches project, scratch and notes are standalone
		sessionActivity := map[string]int64{
			"app":     now.Unix(),
			"api":     now.Unix(),
			"scratch": now.Unix(),
			"notes":   now.Unix(),
		}
		hist := &history.History{}

		result := buildSessionAwareItemsWith(baseItems, hist, sessionActivity)

		// Should have 4 items: 2 projects + 2 standalone
		if len(result) != 4 {
			t.Fatalf("got %d items, want 4", len(result))
		}

		standalone := 0
		for _, item := range result {
			if isStandaloneSession(item) {
				standalone++
			}
		}
		if standalone != 2 {
			t.Errorf("got %d standalone sessions, want 2", standalone)
		}
	})

	t.Run("icon assignment", func(t *testing.T) {
		baseItems := []ui.Item{
			{Name: "app", Path: "/app"},
			{Name: "idle", Path: "/idle"},
		}
		sessionActivity := map[string]int64{
			"app":     now.Unix(),
			"scratch": now.Unix(),
		}
		hist := &history.History{}

		result := buildSessionAwareItemsWith(baseItems, hist, sessionActivity)

		iconByPath := make(map[string]string)
		for _, item := range result {
			iconByPath[item.Path] = item.Icon
		}

		if iconByPath["/app"] != iconDirSession {
			t.Errorf("project with session: Icon = %q, want %q", iconByPath["/app"], iconDirSession)
		}
		if iconByPath["/idle"] != "" {
			t.Errorf("project without session: Icon = %q, want empty", iconByPath["/idle"])
		}
		if iconByPath[tmuxSessionPathPrefix+"scratch"] != iconStandaloneSession {
			t.Errorf("standalone session: Icon = %q, want %q", iconByPath[tmuxSessionPathPrefix+"scratch"], iconStandaloneSession)
		}
	})

	t.Run("no sessions means no icons and no standalone items", func(t *testing.T) {
		baseItems := []ui.Item{
			{Name: "app", Path: "/app"},
			{Name: "api", Path: "/api"},
		}
		sessionActivity := map[string]int64{}
		hist := &history.History{}

		result := buildSessionAwareItemsWith(baseItems, hist, sessionActivity)

		if len(result) != 2 {
			t.Fatalf("got %d items, want 2", len(result))
		}
		for _, item := range result {
			if item.Icon != "" {
				t.Errorf("item %q has Icon %q, want empty", item.Name, item.Icon)
			}
		}
	})

	t.Run("sanitized name matching", func(t *testing.T) {
		// Project name "my.app" sanitizes to "my_app"
		baseItems := []ui.Item{
			{Name: "my.app", Path: "/my.app"},
		}
		// Session name "my_app" should match the sanitized project name
		sessionActivity := map[string]int64{
			"my_app": now.Unix(),
		}
		hist := &history.History{}

		result := buildSessionAwareItemsWith(baseItems, hist, sessionActivity)

		if len(result) != 1 {
			t.Fatalf("got %d items, want 1 (session should match project)", len(result))
		}
		if result[0].Icon != iconDirSession {
			t.Errorf("project with matching sanitized session: Icon = %q, want %q", result[0].Icon, iconDirSession)
		}
	})
}

func TestSortByUnifiedRecency(t *testing.T) {
	t.Run("mixed items sort correctly", func(t *testing.T) {
		items := []ui.Item{
			{Name: "no-history", Path: "/no-history"},
			{Name: "old-project", Path: "/old-project"},
			{Name: "recent-session", Path: "tmux:recent-session"},
		}
		hist := &history.History{
			Entries: []history.Entry{
				{Path: "/old-project", LastAccess: time.Unix(1000, 0)},
			},
		}
		sessionActivity := map[string]int64{
			"recent-session": 2000,
		}

		result := sortByUnifiedRecency(items, hist, sessionActivity)

		// Expected: no-history first (alphabetical, no timestamp), old-project (ts=1000), recent-session (ts=2000)
		expected := []string{"/no-history", "/old-project", "tmux:recent-session"}
		for i, want := range expected {
			if result[i].Path != want {
				t.Errorf("result[%d].Path = %q, want %q", i, result[i].Path, want)
			}
		}
	})

	t.Run("sessions interleave with projects by timestamp", func(t *testing.T) {
		items := []ui.Item{
			{Name: "proj-old", Path: "/proj-old"},
			{Name: "session-mid", Path: "tmux:session-mid"},
			{Name: "proj-new", Path: "/proj-new"},
		}
		hist := &history.History{
			Entries: []history.Entry{
				{Path: "/proj-old", LastAccess: time.Unix(1000, 0)},
				{Path: "/proj-new", LastAccess: time.Unix(3000, 0)},
			},
		}
		sessionActivity := map[string]int64{
			"session-mid": 2000,
		}

		result := sortByUnifiedRecency(items, hist, sessionActivity)

		expected := []string{"/proj-old", "tmux:session-mid", "/proj-new"}
		for i, want := range expected {
			if result[i].Path != want {
				t.Errorf("result[%d].Path = %q, want %q", i, result[i].Path, want)
			}
		}
	})

	t.Run("multiple sessions sort by activity", func(t *testing.T) {
		items := []ui.Item{
			{Name: "older", Path: "tmux:older"},
			{Name: "newer", Path: "tmux:newer"},
			{Name: "middle", Path: "tmux:middle"},
		}
		hist := &history.History{}
		sessionActivity := map[string]int64{
			"older":  1000,
			"middle": 2000,
			"newer":  3000,
		}

		result := sortByUnifiedRecency(items, hist, sessionActivity)

		expected := []string{"tmux:older", "tmux:middle", "tmux:newer"}
		for i, want := range expected {
			if result[i].Path != want {
				t.Errorf("result[%d].Path = %q, want %q", i, result[i].Path, want)
			}
		}
	})
}

func TestSortBaseItemsByHistory(t *testing.T) {
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

		result := sortBaseItemsByHistory(items, hist)

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

		result := sortBaseItemsByHistory(items, hist)

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

		result := sortBaseItemsByHistory(items, hist)

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
