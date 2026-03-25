package ui

import (
	"strings"
	"testing"
)

// containsSubstring checks if s contains substr, stripping ANSI codes first.
func containsSubstring(s, substr string) bool {
	s = StripANSI(s)
	return strings.Contains(s, substr)
}

func TestAdjustScroll(t *testing.T) {
	tests := []struct {
		name      string
		cursor    int
		scroll    int
		height    int
		itemCount int
		margin    int
		expected  int
	}{
		{
			name:      "no adjustment needed",
			cursor:    5,
			scroll:    3,
			height:    10,
			itemCount: 20,
			margin:    0,
			expected:  3,
		},
		{
			name:      "cursor above visible area scrolls up",
			cursor:    2,
			scroll:    5,
			height:    10,
			itemCount: 20,
			margin:    0,
			expected:  2,
		},
		{
			name:      "cursor below visible area scrolls down",
			cursor:    15,
			scroll:    3,
			height:    10,
			itemCount: 20,
			margin:    0,
			expected:  6,
		},
		{
			name:      "margin keeps extra lines above cursor",
			cursor:    5,
			scroll:    5,
			height:    10,
			itemCount: 20,
			margin:    3,
			expected:  2,
		},
		{
			name:      "zero items returns zero",
			cursor:    0,
			scroll:    0,
			height:    10,
			itemCount: 0,
			margin:    0,
			expected:  0,
		},
		{
			name:      "height exceeds item count clamps visible",
			cursor:    2,
			scroll:    0,
			height:    20,
			itemCount: 5,
			margin:    0,
			expected:  0,
		},
		{
			name:      "scroll clamped to max",
			cursor:    18,
			scroll:    0,
			height:    10,
			itemCount: 20,
			margin:    0,
			expected:  9,
		},
		{
			name:      "margin larger than visible is reduced",
			cursor:    0,
			scroll:    0,
			height:    3,
			itemCount: 5,
			margin:    10,
			expected:  0,
		},
		{
			name:      "scroll never negative",
			cursor:    0,
			scroll:    5,
			height:    10,
			itemCount: 20,
			margin:    0,
			expected:  0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := adjustScroll(tt.cursor, tt.scroll, tt.height, tt.itemCount, tt.margin)
			if result != tt.expected {
				t.Errorf("adjustScroll(%d, %d, %d, %d, %d) = %d, want %d",
					tt.cursor, tt.scroll, tt.height, tt.itemCount, tt.margin, result, tt.expected)
			}
		})
	}
}

func TestFuzzyMatch(t *testing.T) {
	candidates := []string{"apple", "application", "banana", "grape", "pineapple"}

	t.Run("matches relevant candidates", func(t *testing.T) {
		result := fuzzyMatch("app", candidates)
		if len(result) == 0 {
			t.Fatal("expected matches, got none")
		}
		// Best match should be last (highest score)
		found := false
		for _, r := range result {
			if r == "apple" || r == "application" || r == "pineapple" {
				found = true
			}
		}
		if !found {
			t.Errorf("expected apple/application/pineapple in results, got %v", result)
		}
	})

	t.Run("no matches returns empty", func(t *testing.T) {
		result := fuzzyMatch("xyz", candidates)
		if len(result) != 0 {
			t.Errorf("expected no matches, got %v", result)
		}
	})

	t.Run("case insensitive", func(t *testing.T) {
		result := fuzzyMatch("APP", candidates)
		if len(result) == 0 {
			t.Fatal("expected case-insensitive matches, got none")
		}
	})

	t.Run("empty query returns empty", func(t *testing.T) {
		result := fuzzyMatch("", candidates)
		if len(result) != 0 {
			t.Errorf("expected no matches for empty query, got %v", result)
		}
	})
}

func TestLastNSegments(t *testing.T) {
	tests := []struct {
		name  string
		path  string
		n     int
		want  string
	}{
		{name: "depth 1", path: "/a/b/c/d", n: 1, want: "d"},
		{name: "depth 2", path: "/a/b/c/d", n: 2, want: "c/d"},
		{name: "depth 3", path: "/a/b/c/d", n: 3, want: "b/c/d"},
		{name: "depth exceeds segments", path: "/a/b", n: 5, want: "a/b"},
		{name: "depth 0 defaults to base", path: "/a/b/c", n: 0, want: "c"},
		{name: "depth negative defaults to base", path: "/a/b/c", n: -1, want: "c"},
		{name: "single segment", path: "/foo", n: 1, want: "foo"},
		{name: "single segment depth 2", path: "/foo", n: 2, want: "foo"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := LastNSegments(tt.path, tt.n)
			if got != tt.want {
				t.Errorf("LastNSegments(%q, %d) = %q, want %q", tt.path, tt.n, got, tt.want)
			}
		})
	}
}
