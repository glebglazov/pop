package project

import (
	"testing"
)

func TestDisambiguateNames(t *testing.T) {
	tests := []struct {
		name     string
		items    []ExpandedProject
		expected []string // expected Name fields after disambiguation
	}{
		{
			name: "no duplicates - no changes",
			items: []ExpandedProject{
				{Name: "alpha", Path: "/a/b/alpha"},
				{Name: "beta", Path: "/x/y/beta"},
			},
			expected: []string{"alpha", "beta"},
		},
		{
			name: "two items, differ at first parent",
			items: []ExpandedProject{
				{Name: "d", Path: "/a/b/c/d"},
				{Name: "d", Path: "/x/y/z/d"},
			},
			expected: []string{"d (c)", "d (z)"},
		},
		{
			name: "two items, same immediate parent, differ at second level",
			items: []ExpandedProject{
				{Name: "d", Path: "/a/b/c/d"},
				{Name: "d", Path: "/x/y/c/d"},
			},
			expected: []string{"d (b)", "d (y)"},
		},
		{
			name: "three items, all differ at first parent",
			items: []ExpandedProject{
				{Name: "app", Path: "/work/frontend/app"},
				{Name: "app", Path: "/work/backend/app"},
				{Name: "app", Path: "/work/mobile/app"},
			},
			expected: []string{"app (frontend)", "app (backend)", "app (mobile)"},
		},
		{
			name: "three items, one unique at level 0, others at level 1",
			items: []ExpandedProject{
				{Name: "d", Path: "/a/b/c/d"},
				{Name: "d", Path: "/a/b/e/d"},
				{Name: "d", Path: "/a/x/c/d"},
			},
			expected: []string{"d (b)", "d (e)", "d (x)"},
		},
		{
			name: "three items, one resolved early, others need deeper",
			items: []ExpandedProject{
				{Name: "d", Path: "/a/b/c/d"},
				{Name: "d", Path: "/a/x/c/d"},
				{Name: "d", Path: "/a/b/e/d"},
			},
			expected: []string{"d (b)", "d (x)", "d (e)"},
		},
		{
			name: "four items, no single level disambiguates all, compound fallback",
			items: []ExpandedProject{
				{Name: "d", Path: "/a/c/d"},
				{Name: "d", Path: "/b/c/d"},
				{Name: "d", Path: "/a/e/d"},
				{Name: "d", Path: "/b/e/d"},
			},
			expected: []string{"d (a/c)", "d (b/c)", "d (a/e)", "d (b/e)"},
		},
		{
			name: "worktree names with slashes",
			items: []ExpandedProject{
				{Name: "proj/main", Path: "/a/b/proj/main"},
				{Name: "proj/main", Path: "/x/y/proj/main"},
			},
			expected: []string{"proj/main (b)", "proj/main (y)"},
		},
		{
			name: "mixed duplicates and unique",
			items: []ExpandedProject{
				{Name: "app", Path: "/work/frontend/app"},
				{Name: "lib", Path: "/work/shared/lib"},
				{Name: "app", Path: "/personal/projects/app"},
			},
			expected: []string{"app (frontend)", "lib", "app (projects)"},
		},
		{
			name: "single item - no changes",
			items: []ExpandedProject{
				{Name: "solo", Path: "/only/one/solo"},
			},
			expected: []string{"solo"},
		},
		{
			name: "empty list",
			items: []ExpandedProject{},
			expected: []string{},
		},
		{
			name: "multi-segment glob names, collision across patterns",
			items: []ExpandedProject{
				{Name: "work/app", Path: "/Dev/work/app"},
				{Name: "work/app", Path: "/Other/work/app"},
			},
			expected: []string{"work/app (Dev)", "work/app (Other)"},
		},
		{
			name: "multi-segment glob names, no collision",
			items: []ExpandedProject{
				{Name: "work/app", Path: "/Dev/work/app"},
				{Name: "personal/app", Path: "/Dev/personal/app"},
			},
			expected: []string{"work/app", "personal/app"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			DisambiguateNames(tt.items, "first_unique_segment")

			if len(tt.items) != len(tt.expected) {
				t.Fatalf("expected %d items, got %d", len(tt.expected), len(tt.items))
			}

			for i, item := range tt.items {
				if item.Name != tt.expected[i] {
					t.Errorf("item %d: expected Name=%q, got %q", i, tt.expected[i], item.Name)
				}
			}
		})
	}
}

func TestParentDir(t *testing.T) {
	tests := []struct {
		path     string
		name     string
		expected string
	}{
		{"/a/b/c/d", "d", "/a/b/c"},
		{"/a/b/proj/main", "proj/main", "/a/b"},
		{"/a/b/c/proj/feat/sub", "proj/feat/sub", "/a/b/c"},
	}

	for _, tt := range tests {
		got := parentDir(tt.path, tt.name)
		if got != tt.expected {
			t.Errorf("parentDir(%q, %q) = %q, want %q", tt.path, tt.name, got, tt.expected)
		}
	}
}

func TestDisambiguateNamesFullPath(t *testing.T) {
	tests := []struct {
		name     string
		items    []ExpandedProject
		expected []string
	}{
		{
			name: "two items, differ at first parent",
			items: []ExpandedProject{
				{Name: "d", Path: "/a/b/c/d"},
				{Name: "d", Path: "/x/y/z/d"},
			},
			expected: []string{"c/d", "z/d"},
		},
		{
			name: "three items, all unique at first parent",
			items: []ExpandedProject{
				{Name: "app", Path: "/work/frontend/app"},
				{Name: "app", Path: "/work/backend/app"},
				{Name: "app", Path: "/work/mobile/app"},
			},
			expected: []string{"frontend/app", "backend/app", "mobile/app"},
		},
		{
			name: "three items, need two levels - all expand to same depth",
			items: []ExpandedProject{
				{Name: "d", Path: "/a/b/c/d"},
				{Name: "d", Path: "/a/b/e/d"},
				{Name: "d", Path: "/a/x/c/d"},
			},
			expected: []string{"b/c/d", "b/e/d", "x/c/d"},
		},
		{
			name: "four items needing two levels",
			items: []ExpandedProject{
				{Name: "d", Path: "/a/c/d"},
				{Name: "d", Path: "/b/c/d"},
				{Name: "d", Path: "/a/e/d"},
				{Name: "d", Path: "/b/e/d"},
			},
			expected: []string{"a/c/d", "b/c/d", "a/e/d", "b/e/d"},
		},
		{
			name: "no duplicates - no changes",
			items: []ExpandedProject{
				{Name: "alpha", Path: "/a/b/alpha"},
				{Name: "beta", Path: "/x/y/beta"},
			},
			expected: []string{"alpha", "beta"},
		},
		{
			name: "mixed duplicates and unique",
			items: []ExpandedProject{
				{Name: "app", Path: "/work/frontend/app"},
				{Name: "lib", Path: "/work/shared/lib"},
				{Name: "app", Path: "/personal/projects/app"},
			},
			expected: []string{"frontend/app", "lib", "projects/app"},
		},
		{
			name: "multi-segment glob names with full_path",
			items: []ExpandedProject{
				{Name: "work/app", Path: "/Dev/work/app"},
				{Name: "work/app", Path: "/Other/work/app"},
			},
			expected: []string{"Dev/work/app", "Other/work/app"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			DisambiguateNames(tt.items, "full_path")

			if len(tt.items) != len(tt.expected) {
				t.Fatalf("expected %d items, got %d", len(tt.expected), len(tt.items))
			}

			for i, item := range tt.items {
				if item.Name != tt.expected[i] {
					t.Errorf("item %d: expected Name=%q, got %q", i, tt.expected[i], item.Name)
				}
			}
		})
	}
}

func TestSplitParentSegments(t *testing.T) {
	tests := []struct {
		dir      string
		expected []string
	}{
		{"/a/b/c", []string{"c", "b", "a"}},
		{"/x", []string{"x"}},
		{"/", nil},
	}

	for _, tt := range tests {
		got := splitParentSegments(tt.dir)
		if len(got) != len(tt.expected) {
			t.Errorf("splitParentSegments(%q) = %v, want %v", tt.dir, got, tt.expected)
			continue
		}
		for i := range got {
			if got[i] != tt.expected[i] {
				t.Errorf("splitParentSegments(%q)[%d] = %q, want %q", tt.dir, i, got[i], tt.expected[i])
			}
		}
	}
}
