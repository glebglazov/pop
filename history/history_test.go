package history

import (
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/glebglazov/pop/internal/deps"
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

func TestDefaultHistoryPathWith(t *testing.T) {
	tests := []struct {
		name     string
		xdgData  string
		userHome string
		expected string
	}{
		{
			name:     "uses XDG_DATA_HOME when set",
			xdgData:  "/custom/data",
			userHome: "/home/user",
			expected: "/custom/data/pop/history.json",
		},
		{
			name:     "falls back to ~/.local/share when XDG not set",
			xdgData:  "",
			userHome: "/home/user",
			expected: "/home/user/.local/share/pop/history.json",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d := &Deps{
				FS: &deps.MockFileSystem{
					GetenvFunc: func(key string) string {
						if key == "XDG_DATA_HOME" {
							return tt.xdgData
						}
						return ""
					},
					UserHomeDirFunc: func() (string, error) {
						return tt.userHome, nil
					},
				},
			}

			result := DefaultHistoryPathWith(d)

			if result != tt.expected {
				t.Errorf("DefaultHistoryPathWith() = %q, want %q", result, tt.expected)
			}
		})
	}
}

func TestLoadWith(t *testing.T) {
	tests := []struct {
		name        string
		fileContent string
		fileErr     error
		wantEntries int
		wantErr     bool
	}{
		{
			name:        "loads existing history",
			fileContent: `{"entries":[{"path":"/project1","last_access":"2024-01-01T00:00:00Z"}]}`,
			wantEntries: 1,
		},
		{
			name:        "returns empty history when file not found",
			fileErr:     os.ErrNotExist,
			wantEntries: 0,
		},
		{
			name:        "returns empty history on parse error",
			fileContent: "invalid json",
			wantEntries: 0,
		},
		{
			name:    "returns error on read error",
			fileErr: os.ErrPermission,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d := &Deps{
				FS: &deps.MockFileSystem{
					ReadFileFunc: func(path string) ([]byte, error) {
						if tt.fileErr != nil {
							return nil, tt.fileErr
						}
						return []byte(tt.fileContent), nil
					},
				},
			}

			h, err := LoadWith(d, "/test/history.json")

			if (err != nil) != tt.wantErr {
				t.Errorf("LoadWith() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if !tt.wantErr && len(h.Entries) != tt.wantEntries {
				t.Errorf("got %d entries, want %d", len(h.Entries), tt.wantEntries)
			}
		})
	}
}

func TestSaveWith(t *testing.T) {
	var savedData []byte
	var savedPath string

	d := &Deps{
		FS: &deps.MockFileSystem{
			MkdirAllFunc: func(path string, perm os.FileMode) error {
				return nil
			},
			WriteFileFunc: func(path string, data []byte, perm os.FileMode) error {
				savedPath = path
				savedData = data
				return nil
			},
		},
	}

	h := &History{
		path: "/test/dir/history.json",
		Entries: []Entry{
			{Path: "/project1", LastAccess: time.Now()},
		},
	}

	err := h.SaveWith(d)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if savedPath != "/test/dir/history.json" {
		t.Errorf("saved to wrong path: %s", savedPath)
	}

	if !strings.Contains(string(savedData), "/project1") {
		t.Error("saved data doesn't contain expected content")
	}
}

// Note: Symlink resolution is now done at config expansion time (the source),
// so history functions work with canonical paths only. Tests verify direct path matching.

func TestRecord(t *testing.T) {
	t.Run("adds new entry", func(t *testing.T) {
		h := &History{}
		h.Record("/home/user/project-a")

		if len(h.Entries) != 1 {
			t.Fatalf("got %d entries, want 1", len(h.Entries))
		}
		if h.Entries[0].Path != "/home/user/project-a" {
			t.Errorf("path = %q, want %q", h.Entries[0].Path, "/home/user/project-a")
		}
		if h.Entries[0].LastAccess.IsZero() {
			t.Error("LastAccess is zero, want non-zero")
		}
	})

	t.Run("updates existing entry", func(t *testing.T) {
		h := &History{
			Entries: []Entry{
				{Path: "/home/user/project-a", LastAccess: time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)},
				{Path: "/home/user/project-b", LastAccess: time.Date(2020, 1, 2, 0, 0, 0, 0, time.UTC)},
			},
		}
		h.Record("/home/user/project-a")

		if len(h.Entries) != 2 {
			t.Fatalf("got %d entries, want 2 (should not duplicate)", len(h.Entries))
		}
		if h.Entries[0].LastAccess.Year() == 2020 {
			t.Error("LastAccess was not updated")
		}
	})

	t.Run("preserves other entries", func(t *testing.T) {
		original := time.Date(2020, 1, 2, 0, 0, 0, 0, time.UTC)
		h := &History{
			Entries: []Entry{
				{Path: "/home/user/project-a"},
				{Path: "/home/user/project-b", LastAccess: original},
			},
		}
		h.Record("/home/user/project-a")

		if h.Entries[1].LastAccess != original {
			t.Error("other entry's LastAccess was modified")
		}
	})
}

func TestRemoveWith(t *testing.T) {
	tests := []struct {
		name     string
		entries  []Entry
		remove   string
		expected []string
	}{
		{
			name: "removes existing entry",
			entries: []Entry{
				{Path: "/a"},
				{Path: "/b"},
				{Path: "/c"},
			},
			remove:   "/b",
			expected: []string{"/a", "/c"},
		},
		{
			name: "no-op for missing entry",
			entries: []Entry{
				{Path: "/a"},
				{Path: "/b"},
			},
			remove:   "/x",
			expected: []string{"/a", "/b"},
		},
		{
			name:     "empty history",
			entries:  nil,
			remove:   "/a",
			expected: nil,
		},
		{
			name: "removes only first match",
			entries: []Entry{
				{Path: "/a"},
				{Path: "/b"},
				{Path: "/a"},
			},
			remove:   "/a",
			expected: []string{"/b", "/a"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := &History{Entries: tt.entries}
			h.RemoveWith(DefaultDeps(), tt.remove)

			if len(h.Entries) != len(tt.expected) {
				t.Fatalf("got %d entries, want %d", len(h.Entries), len(tt.expected))
			}
			for i, exp := range tt.expected {
				if h.Entries[i].Path != exp {
					t.Errorf("entry[%d].Path = %q, want %q", i, h.Entries[i].Path, exp)
				}
			}
		})
	}
}

func TestDedupeEntriesBy(t *testing.T) {
	t.Run("merges entries with same canonical path keeping latest timestamp", func(t *testing.T) {
		older := time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)
		newer := time.Date(2025, 6, 15, 0, 0, 0, 0, time.UTC)

		h := &History{
			Entries: []Entry{
				{Path: "/symlink/project", LastAccess: older},
				{Path: "/real/project", LastAccess: newer},
			},
		}

		// Both paths resolve to /real/project
		h.dedupeEntriesBy(func(path string) (string, error) {
			return "/real/project", nil
		})

		if len(h.Entries) != 1 {
			t.Fatalf("got %d entries, want 1", len(h.Entries))
		}
		if h.Entries[0].Path != "/real/project" {
			t.Errorf("path = %q, want /real/project", h.Entries[0].Path)
		}
		if h.Entries[0].LastAccess != newer {
			t.Errorf("LastAccess = %v, want %v (newer)", h.Entries[0].LastAccess, newer)
		}
	})

	t.Run("keeps entries with distinct canonical paths", func(t *testing.T) {
		h := &History{
			Entries: []Entry{
				{Path: "/project-a"},
				{Path: "/project-b"},
			},
		}

		h.dedupeEntriesBy(func(path string) (string, error) {
			return path, nil // identity — no symlinks
		})

		if len(h.Entries) != 2 {
			t.Fatalf("got %d entries, want 2", len(h.Entries))
		}
	})

	t.Run("uses original path on eval error", func(t *testing.T) {
		h := &History{
			Entries: []Entry{
				{Path: "/broken-link"},
				{Path: "/working"},
			},
		}

		h.dedupeEntriesBy(func(path string) (string, error) {
			if path == "/broken-link" {
				return "", fmt.Errorf("no such file")
			}
			return path, nil
		})

		if len(h.Entries) != 2 {
			t.Fatalf("got %d entries, want 2", len(h.Entries))
		}
	})
}

func TestTmuxSessionActivityWith(t *testing.T) {
	tests := []struct {
		name       string
		tmuxOutput string
		tmuxErr    error
		expected   map[string]int64
	}{
		{
			name:       "parses session activity",
			tmuxOutput: "session1\t1234567890\nsession2\t1234567891",
			expected: map[string]int64{
				"session1": 1234567890,
				"session2": 1234567891,
			},
		},
		{
			name:       "preserves spaces in session names",
			tmuxOutput: "rails (work)\t1234567890\nrails (mixed)\t1234567891",
			expected: map[string]int64{
				"rails (work)":  1234567890,
				"rails (mixed)": 1234567891,
			},
		},
		{
			name:     "returns empty map on error",
			tmuxErr:  fmt.Errorf("tmux error"),
			expected: map[string]int64{},
		},
		{
			name:       "handles empty output",
			tmuxOutput: "",
			expected:   map[string]int64{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d := &Deps{
				Tmux: &deps.MockTmux{
					ListSessionsFunc: func() (string, error) {
						return tt.tmuxOutput, tt.tmuxErr
					},
				},
			}

			result := TmuxSessionActivityWith(d)

			if len(result) != len(tt.expected) {
				t.Errorf("got %d sessions, want %d", len(result), len(tt.expected))
				return
			}

			for k, v := range tt.expected {
				if result[k] != v {
					t.Errorf("session %q activity = %d, want %d", k, result[k], v)
				}
			}
		})
	}
}
