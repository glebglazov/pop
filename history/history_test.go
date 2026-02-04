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

func TestSortByRecencyWith_SymlinkResolution(t *testing.T) {
	now := time.Now()

	tests := []struct {
		name         string
		entries      []Entry
		projects     []project.Project
		symlinkMap   map[string]string // symlink path -> resolved path
		expected     []string          // expected order of project names
	}{
		{
			name: "matches history via symlink resolution",
			entries: []Entry{
				{Path: "/real/path/old", LastAccess: now.Add(-2 * time.Hour)},
				{Path: "/real/path/recent", LastAccess: now},
			},
			projects: []project.Project{
				{Name: "recent", Path: "/symlink/recent"},
				{Name: "old", Path: "/symlink/old"},
				{Name: "unvisited", Path: "/other"},
			},
			symlinkMap: map[string]string{
				"/symlink/recent": "/real/path/recent",
				"/symlink/old":    "/real/path/old",
			},
			expected: []string{"unvisited", "old", "recent"},
		},
		{
			name: "direct path match takes precedence",
			entries: []Entry{
				{Path: "/direct/path", LastAccess: now},
			},
			projects: []project.Project{
				{Name: "direct", Path: "/direct/path"},
				{Name: "other", Path: "/other"},
			},
			symlinkMap: map[string]string{}, // no symlinks
			expected:   []string{"other", "direct"},
		},
		{
			name: "mixed symlink and direct matches",
			entries: []Entry{
				{Path: "/real/a", LastAccess: now.Add(-2 * time.Hour)},
				{Path: "/direct/b", LastAccess: now.Add(-1 * time.Hour)},
				{Path: "/real/c", LastAccess: now},
			},
			projects: []project.Project{
				{Name: "c", Path: "/symlink/c"},
				{Name: "b", Path: "/direct/b"},
				{Name: "a", Path: "/symlink/a"},
			},
			symlinkMap: map[string]string{
				"/symlink/a": "/real/a",
				"/symlink/c": "/real/c",
			},
			expected: []string{"a", "b", "c"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d := &Deps{
				FS: &deps.MockFileSystem{
					EvalSymlinksFunc: func(path string) (string, error) {
						if resolved, ok := tt.symlinkMap[path]; ok {
							return resolved, nil
						}
						return path, nil
					},
				},
			}

			h := &History{Entries: tt.entries}
			result := h.SortByRecencyWith(d, tt.projects)

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

func TestRemoveWith_SymlinkResolution(t *testing.T) {
	now := time.Now()

	tests := []struct {
		name           string
		entries        []Entry
		removePath     string
		symlinkMap     map[string]string
		expectedPaths  []string // remaining paths after removal
	}{
		{
			name: "removes by direct path",
			entries: []Entry{
				{Path: "/path/a", LastAccess: now},
				{Path: "/path/b", LastAccess: now},
			},
			removePath:    "/path/a",
			symlinkMap:    map[string]string{},
			expectedPaths: []string{"/path/b"},
		},
		{
			name: "removes by resolved symlink path",
			entries: []Entry{
				{Path: "/real/path", LastAccess: now},
				{Path: "/other", LastAccess: now},
			},
			removePath: "/symlink/path",
			symlinkMap: map[string]string{
				"/symlink/path": "/real/path",
			},
			expectedPaths: []string{"/other"},
		},
		{
			name: "no match - nothing removed",
			entries: []Entry{
				{Path: "/path/a", LastAccess: now},
			},
			removePath: "/nonexistent",
			symlinkMap: map[string]string{
				"/nonexistent": "/also/nonexistent",
			},
			expectedPaths: []string{"/path/a"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d := &Deps{
				FS: &deps.MockFileSystem{
					EvalSymlinksFunc: func(path string) (string, error) {
						if resolved, ok := tt.symlinkMap[path]; ok {
							return resolved, nil
						}
						return path, nil
					},
				},
			}

			h := &History{Entries: tt.entries}
			h.RemoveWith(d, tt.removePath)

			if len(h.Entries) != len(tt.expectedPaths) {
				t.Errorf("expected %d entries, got %d", len(tt.expectedPaths), len(h.Entries))
				return
			}

			for i, expected := range tt.expectedPaths {
				if h.Entries[i].Path != expected {
					t.Errorf("entry %d: expected path %q, got %q", i, expected, h.Entries[i].Path)
				}
			}
		})
	}
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
			tmuxOutput: "session1 1234567890\nsession2 1234567891",
			expected: map[string]int64{
				"session1": 1234567890,
				"session2": 1234567891,
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
