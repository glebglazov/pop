package history

import (
	"testing"
	"time"

	"github.com/glebglazov/pop/project"
)

func TestSortByRecency(t *testing.T) {
	now := time.Now()

	tests := []struct {
		name     string
		entries  []Entry
		projects []project.Project
		expected []string // expected order of project names
	}{
		{
			name:    "no history - alphabetical order",
			entries: nil,
			projects: []project.Project{
				{Name: "zebra", Path: "/zebra"},
				{Name: "alpha", Path: "/alpha"},
				{Name: "mike", Path: "/mike"},
			},
			expected: []string{"alpha", "mike", "zebra"},
		},
		{
			name: "all have history - oldest first, most recent last",
			entries: []Entry{
				{Path: "/old", LastAccess: now.Add(-3 * time.Hour)},
				{Path: "/medium", LastAccess: now.Add(-1 * time.Hour)},
				{Path: "/recent", LastAccess: now},
			},
			projects: []project.Project{
				{Name: "recent", Path: "/recent"},
				{Name: "old", Path: "/old"},
				{Name: "medium", Path: "/medium"},
			},
			expected: []string{"old", "medium", "recent"},
		},
		{
			name: "mixed - no history first (alphabetical), then by recency",
			entries: []Entry{
				{Path: "/accessed", LastAccess: now.Add(-1 * time.Hour)},
				{Path: "/recent", LastAccess: now},
			},
			projects: []project.Project{
				{Name: "recent", Path: "/recent"},
				{Name: "never", Path: "/never"},
				{Name: "accessed", Path: "/accessed"},
				{Name: "also-never", Path: "/also-never"},
			},
			expected: []string{"also-never", "never", "accessed", "recent"},
		},
		{
			name: "worktree paths - sorted by individual access time",
			entries: []Entry{
				{Path: "/project/main", LastAccess: now.Add(-2 * time.Hour)},
				{Path: "/project/feature", LastAccess: now},
				{Path: "/other/main", LastAccess: now.Add(-1 * time.Hour)},
			},
			projects: []project.Project{
				{Name: "project/feature", Path: "/project/feature"},
				{Name: "project/main", Path: "/project/main"},
				{Name: "other/main", Path: "/other/main"},
			},
			expected: []string{"project/main", "other/main", "project/feature"},
		},
		{
			name:     "empty projects list",
			entries:  []Entry{{Path: "/something", LastAccess: now}},
			projects: []project.Project{},
			expected: []string{},
		},
		{
			name: "single project with history",
			entries: []Entry{
				{Path: "/only", LastAccess: now},
			},
			projects: []project.Project{
				{Name: "only", Path: "/only"},
			},
			expected: []string{"only"},
		},
		{
			name:    "single project without history",
			entries: nil,
			projects: []project.Project{
				{Name: "only", Path: "/only"},
			},
			expected: []string{"only"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := &History{Entries: tt.entries}
			result := h.SortByRecency(tt.projects)

			if len(result) != len(tt.expected) {
				t.Errorf("expected %d projects, got %d", len(tt.expected), len(result))
				return
			}

			for i, p := range result {
				if p.Name != tt.expected[i] {
					t.Errorf("position %d: expected %q, got %q", i, tt.expected[i], p.Name)
				}
			}
		})
	}
}

func TestSortByRecency_StableSort(t *testing.T) {
	// Projects without history should maintain relative alphabetical order
	h := &History{}
	projects := []project.Project{
		{Name: "charlie", Path: "/charlie"},
		{Name: "alpha", Path: "/alpha"},
		{Name: "bravo", Path: "/bravo"},
	}

	result := h.SortByRecency(projects)

	expected := []string{"alpha", "bravo", "charlie"}
	for i, p := range result {
		if p.Name != expected[i] {
			t.Errorf("position %d: expected %q, got %q", i, expected[i], p.Name)
		}
	}
}

func TestSortByRecency_DoesNotMutateOriginal(t *testing.T) {
	now := time.Now()
	h := &History{
		Entries: []Entry{
			{Path: "/b", LastAccess: now},
			{Path: "/a", LastAccess: now.Add(-1 * time.Hour)},
		},
	}

	original := []project.Project{
		{Name: "b", Path: "/b"},
		{Name: "a", Path: "/a"},
	}

	// Store original order
	originalOrder := make([]string, len(original))
	for i, p := range original {
		originalOrder[i] = p.Name
	}

	_ = h.SortByRecency(original)

	// Check original wasn't mutated
	for i, p := range original {
		if p.Name != originalOrder[i] {
			t.Errorf("original was mutated: position %d changed from %q to %q",
				i, originalOrder[i], p.Name)
		}
	}
}
