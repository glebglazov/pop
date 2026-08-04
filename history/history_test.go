package history

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/glebglazov/pop/internal/deps"
	"github.com/glebglazov/pop/internal/tmux"
	"github.com/glebglazov/pop/internal/tmux/tmuxtest"
	"github.com/glebglazov/pop/project"
	"github.com/glebglazov/pop/tasks"
)

// storeDeps builds history deps rooted at dataHome, so the rows land in a
// throwaway pop.db instead of the developer's real machine store (the tasks
// package panics on the latter). Each call gets its own store handle, which is
// what lets a test drive two independent recorders against one database.
func storeDeps(t *testing.T, dataHome string) *Deps {
	t.Helper()
	real := deps.NewRealFileSystem()
	fs := &deps.MockFileSystem{
		GetenvFunc: func(key string) string {
			if key == "XDG_DATA_HOME" {
				return dataHome
			}
			return ""
		},
		UserHomeDirFunc:  func() (string, error) { return dataHome, nil },
		StatFunc:         real.Stat,
		ReadDirFunc:      real.ReadDir,
		ReadFileFunc:     real.ReadFile,
		WriteFileFunc:    real.WriteFile,
		MkdirAllFunc:     real.MkdirAll,
		RenameFunc:       real.Rename,
		RemoveAllFunc:    real.RemoveAll,
		DirFSFunc:        real.DirFS,
		EvalSymlinksFunc: real.EvalSymlinks,
	}
	td := &tasks.Deps{FS: fs, Git: deps.NewRealGit(), Runner: tasks.RealCommandRunner{}}
	t.Cleanup(func() { _ = td.CloseStore() })
	return &Deps{FS: fs, Tmux: &tmuxtest.Fake{}, Tasks: td}
}

// writeLegacyFile plants a pre-store history.json under dataHome.
func writeLegacyFile(t *testing.T, dataHome, contents string) string {
	t.Helper()
	path := filepath.Join(dataHome, "pop", legacyHistoryFile)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(contents), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func entryPaths(h *History) []string {
	out := make([]string, 0, len(h.Entries))
	for _, e := range h.Entries {
		out = append(out, e.Path)
	}
	return out
}

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

func TestLegacyHistoryPathWith(t *testing.T) {
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

			result := LegacyHistoryPathWith(d)

			if result != tt.expected {
				t.Errorf("LegacyHistoryPathWith() = %q, want %q", result, tt.expected)
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

			h, err := LoadWith(d)

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

// TestLoadWithFoldsLegacyFile pins the whole storage move end to end: a
// surviving history.json becomes rows, the file is not deleted, and — the part
// the fold marker exists for — an entry the human removes afterwards is not
// resurrected from the file that still holds it.
func TestLoadWithFoldsLegacyFile(t *testing.T) {
	t.Parallel()
	dataHome := t.TempDir()
	legacy := writeLegacyFile(t, dataHome, `{"entries":[
		{"path":"/repo/main","last_access":"2026-06-01T10:00:00Z"},
		{"path":"/repo/feature","last_access":"2026-06-02T10:00:00Z"}
	]}`)
	d := storeDeps(t, dataHome)

	h, err := LoadWith(d)
	if err != nil {
		t.Fatalf("LoadWith: %v", err)
	}
	if got := entryPaths(h); len(got) != 2 {
		t.Fatalf("entries after fold = %v, want both legacy paths", got)
	}
	// The migrated timestamps are what the dashboard's sort key and `pop pane
	// status` read, so they must survive the move, not just the paths.
	for _, e := range h.Entries {
		want := "2026-06-01T10:00:00Z"
		if e.Path == "/repo/feature" {
			want = "2026-06-02T10:00:00Z"
		}
		if got := e.LastAccess.UTC().Format(time.RFC3339); got != want {
			t.Errorf("%s last access = %s, want %s", e.Path, got, want)
		}
	}
	if _, err := os.Stat(legacy); err != nil {
		t.Errorf("legacy file was not left on disk: %v", err)
	}

	if err := h.Remove("/repo/feature"); err != nil {
		t.Fatalf("Remove: %v", err)
	}
	reloaded, err := LoadWith(d)
	if err != nil {
		t.Fatalf("LoadWith after remove: %v", err)
	}
	if got := entryPaths(reloaded); len(got) != 1 || got[0] != "/repo/main" {
		t.Errorf("entries after remove = %v, want only /repo/main — the fold re-ran", got)
	}
	if _, err := os.Stat(legacy); err != nil {
		t.Errorf("legacy file was removed by a later load: %v", err)
	}
}

func TestRecordPersistsAcrossLoads(t *testing.T) {
	t.Parallel()
	dataHome := t.TempDir()

	h, err := LoadWith(storeDeps(t, dataHome))
	if err != nil {
		t.Fatalf("LoadWith: %v", err)
	}
	if err := h.Record("/repo/feature"); err != nil {
		t.Fatalf("Record: %v", err)
	}

	// A second Deps is a second store handle against the same database — the
	// shape another pop process has.
	reloaded, err := LoadWith(storeDeps(t, dataHome))
	if err != nil {
		t.Fatalf("LoadWith (second handle): %v", err)
	}
	if got := entryPaths(reloaded); len(got) != 1 || got[0] != "/repo/feature" {
		t.Fatalf("entries = %v, want [/repo/feature]", got)
	}
	if reloaded.Entries[0].LastAccess.IsZero() {
		t.Error("recorded entry came back with a zero timestamp")
	}
}

// TestConcurrentRecordersDoNotLoseWrites is the reason History moved into the
// store: the whole-file rewrite it replaces took no lock, so the last writer to
// finish erased every landing recorded while it held its in-memory copy.
func TestConcurrentRecordersDoNotLoseWrites(t *testing.T) {
	t.Parallel()
	dataHome := t.TempDir()
	const perRecorder = 12

	// Two recorders, each with its own store handle, each loaded before either
	// writes — so both start from the same empty snapshot.
	first, err := LoadWith(storeDeps(t, dataHome))
	if err != nil {
		t.Fatalf("LoadWith (first recorder): %v", err)
	}
	second, err := LoadWith(storeDeps(t, dataHome))
	if err != nil {
		t.Fatalf("LoadWith (second recorder): %v", err)
	}

	var wg sync.WaitGroup
	errs := make(chan error, 2*perRecorder)
	record := func(h *History, prefix string) {
		defer wg.Done()
		for i := 0; i < perRecorder; i++ {
			if err := h.Record(fmt.Sprintf("%s/%d", prefix, i)); err != nil {
				errs <- err
			}
		}
	}
	wg.Add(2)
	go record(first, "/first")
	go record(second, "/second")
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Errorf("Record: %v", err)
	}

	reloaded, err := LoadWith(storeDeps(t, dataHome))
	if err != nil {
		t.Fatalf("LoadWith after concurrent recording: %v", err)
	}
	got := make(map[string]bool, len(reloaded.Entries))
	for _, e := range reloaded.Entries {
		got[e.Path] = true
	}
	for i := 0; i < perRecorder; i++ {
		for _, prefix := range []string{"/first", "/second"} {
			path := fmt.Sprintf("%s/%d", prefix, i)
			if !got[path] {
				t.Errorf("%s is missing — a concurrent write was lost", path)
			}
		}
	}
	if len(got) != 2*perRecorder {
		t.Errorf("got %d entries, want %d", len(got), 2*perRecorder)
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

func TestRemove(t *testing.T) {
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
			if err := h.Remove(tt.remove); err != nil {
				t.Fatalf("Remove: %v", err)
			}

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
		name     string
		sessions []tmux.SessionActivity
		tmuxErr  error
		expected map[string]int64
	}{
		{
			name: "maps sessions to activity",
			sessions: []tmux.SessionActivity{
				{Name: "session1", Activity: 1234567890},
				{Name: "session2", Activity: 1234567891},
			},
			expected: map[string]int64{
				"session1": 1234567890,
				"session2": 1234567891,
			},
		},
		{
			name: "preserves spaces in session names",
			sessions: []tmux.SessionActivity{
				{Name: "rails (work)", Activity: 1234567890},
				{Name: "rails (mixed)", Activity: 1234567891},
			},
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
			name:     "handles no sessions",
			sessions: nil,
			expected: map[string]int64{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fake := &tmuxtest.Fake{SessionList: tt.sessions}
			if tt.tmuxErr != nil {
				fake.SessionsFunc = func() ([]tmux.SessionActivity, error) {
					return nil, tt.tmuxErr
				}
			}
			d := &Deps{Tmux: fake}

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
