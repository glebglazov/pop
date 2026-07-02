package cmd

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/glebglazov/pop/config"
	"github.com/glebglazov/pop/history"
	"github.com/glebglazov/pop/internal/deps"
	"github.com/glebglazov/pop/project"
	"github.com/glebglazov/pop/ui"
)

// testItem creates a ui.Item with SessionName pre-computed using the same
// fast approximation the dashboard uses (project.FastSessionName).
func testItem(name, path string) ui.Item {
	return ui.Item{Name: name, Path: path, SessionName: project.FastSessionName(path)}
}

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
			testItem("app", "/app"),
			testItem("api", "/api"),
		}
		// Sessions: app matches project, api matches project, scratch and notes are standalone
		sessionActivity := map[string]int64{
			project.SessionName("/app"): now.Unix(),
			project.SessionName("/api"): now.Unix(),
			"scratch":                   now.Unix(),
			"notes":                     now.Unix(),
		}
		hist := &history.History{}

		result := buildSessionAwareItemsWith(baseItems, hist, sessionActivity, nil, nil)

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
			testItem("app", "/app"),
			testItem("idle", "/idle"),
		}
		sessionActivity := map[string]int64{
			project.SessionName("/app"): now.Unix(),
			"scratch":                   now.Unix(),
		}
		hist := &history.History{}

		result := buildSessionAwareItemsWith(baseItems, hist, sessionActivity, nil, nil)

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
			testItem("app", "/app"),
			testItem("api", "/api"),
		}
		sessionActivity := map[string]int64{}
		hist := &history.History{}

		result := buildSessionAwareItemsWith(baseItems, hist, sessionActivity, nil, nil)

		if len(result) != 2 {
			t.Fatalf("got %d items, want 2", len(result))
		}
		for _, item := range result {
			if item.Icon != "" {
				t.Errorf("item %q has Icon %q, want empty", item.Name, item.Icon)
			}
		}
	})

	t.Run("excluded session names not shown as standalone", func(t *testing.T) {
		// Simulate exclude_current_dir: "app" was removed from baseItems
		// but its tmux session still exists
		baseItems := []ui.Item{
			testItem("api", "/api"),
		}
		sessionActivity := map[string]int64{
			project.SessionName("/app"): now.Unix(), // session for excluded project
			project.SessionName("/api"): now.Unix(),
		}
		excludedSessionNames := map[string]bool{
			project.SessionName("/app"): true,
		}
		hist := &history.History{}

		result := buildSessionAwareItemsWith(baseItems, hist, sessionActivity, excludedSessionNames, nil)

		// Should have only 1 item: "api" with dir session icon
		// "app" should NOT appear as standalone
		if len(result) != 1 {
			t.Fatalf("got %d items, want 1", len(result))
		}
		if result[0].Name != "api" {
			t.Errorf("result[0].Name = %q, want %q", result[0].Name, "api")
		}
		if result[0].Icon != iconDirSession {
			t.Errorf("result[0].Icon = %q, want %q", result[0].Icon, iconDirSession)
		}
	})

	t.Run("sanitized name matching", func(t *testing.T) {
		// Project name "my.app" sanitizes to "my_app"
		baseItems := []ui.Item{
			testItem("my.app", "/my.app"),
		}
		// Session name "my_app" should match SessionName(path), not the display label
		sessionActivity := map[string]int64{
			project.SessionName("/my.app"): now.Unix(),
		}
		hist := &history.History{}

		result := buildSessionAwareItemsWith(baseItems, hist, sessionActivity, nil, nil)

		if len(result) != 1 {
			t.Fatalf("got %d items, want 1 (session should match project)", len(result))
		}
		if result[0].Icon != iconDirSession {
			t.Errorf("project with matching sanitized session: Icon = %q, want %q", result[0].Icon, iconDirSession)
		}
	})

	t.Run("session icon matches SessionName for top-level project", func(t *testing.T) {
		// Display label differs from session name (e.g. display_depth > 1)
		baseItems := []ui.Item{
			testItem("user/myapp", "/home/user/myapp"),
		}
		sessionActivity := map[string]int64{
			project.SessionName("/home/user/myapp"): now.Unix(),
		}
		hist := &history.History{}

		result := buildSessionAwareItemsWith(baseItems, hist, sessionActivity, nil, nil)

		if len(result) != 1 {
			t.Fatalf("got %d items, want 1", len(result))
		}
		if result[0].Icon != iconDirSession {
			t.Errorf("Icon = %q, want %q (should match SessionName(path), not display label %q)",
				result[0].Icon, iconDirSession, baseItems[0].Name)
		}
	})

	t.Run("session icon matches SessionName for bare-repo worktree", func(t *testing.T) {
		// Picker shows display_depth label; session name is repo/worktree
		baseItems := []ui.Item{
			testItem("projects/myrepo/feature", "/projects/myrepo/feature"),
		}
		sessionActivity := map[string]int64{
			project.SessionName("/projects/myrepo/feature"): now.Unix(),
		}
		hist := &history.History{}

		result := buildSessionAwareItemsWith(baseItems, hist, sessionActivity, nil, nil)

		if len(result) != 1 {
			t.Fatalf("got %d items, want 1", len(result))
		}
		if result[0].Icon != iconDirSession {
			t.Errorf("Icon = %q, want %q (should match SessionName(path), not display label %q)",
				result[0].Icon, iconDirSession, baseItems[0].Name)
		}
	})
}

func TestBuildSessionAwareItems_AttentionIndicator(t *testing.T) {
	now := time.Now()

	t.Run("attention overrides session icon for project", func(t *testing.T) {
		baseItems := []ui.Item{
			testItem("app", "/app"),
			testItem("api", "/api"),
		}
		sessionActivity := map[string]int64{
			project.SessionName("/app"): now.Unix(),
			project.SessionName("/api"): now.Unix(),
		}
		attentionSessions := map[string]bool{
			project.SessionName("/app"): true,
		}
		hist := &history.History{}

		result := buildSessionAwareItemsWith(baseItems, hist, sessionActivity, nil, attentionSessions)

		iconByPath := make(map[string]string)
		for _, item := range result {
			iconByPath[item.Path] = item.Icon
		}

		if iconByPath["/app"] != iconAttention {
			t.Errorf("project with attention: Icon = %q, want %q", iconByPath["/app"], iconAttention)
		}
		if iconByPath["/api"] != iconDirSession {
			t.Errorf("project without attention: Icon = %q, want %q", iconByPath["/api"], iconDirSession)
		}
	})

	t.Run("attention overrides standalone session icon", func(t *testing.T) {
		baseItems := []ui.Item{}
		sessionActivity := map[string]int64{
			"scratch": now.Unix(),
			"notes":   now.Unix(),
		}
		attentionSessions := map[string]bool{
			"scratch": true,
		}
		hist := &history.History{}

		result := buildSessionAwareItemsWith(baseItems, hist, sessionActivity, nil, attentionSessions)

		iconByPath := make(map[string]string)
		for _, item := range result {
			iconByPath[item.Path] = item.Icon
		}

		if iconByPath[tmuxSessionPathPrefix+"scratch"] != iconAttention {
			t.Errorf("standalone with attention: Icon = %q, want %q", iconByPath[tmuxSessionPathPrefix+"scratch"], iconAttention)
		}
		if iconByPath[tmuxSessionPathPrefix+"notes"] != iconStandaloneSession {
			t.Errorf("standalone without attention: Icon = %q, want %q", iconByPath[tmuxSessionPathPrefix+"notes"], iconStandaloneSession)
		}
	})

	t.Run("nil attention sessions does not affect icons", func(t *testing.T) {
		baseItems := []ui.Item{
			testItem("app", "/app"),
		}
		sessionActivity := map[string]int64{
			project.SessionName("/app"): now.Unix(),
		}
		hist := &history.History{}

		result := buildSessionAwareItemsWith(baseItems, hist, sessionActivity, nil, nil)

		if result[0].Icon != iconDirSession {
			t.Errorf("nil attention: Icon = %q, want %q", result[0].Icon, iconDirSession)
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

func TestOpenTmuxSessionWith(t *testing.T) {
	t.Run("top-level project uses SessionName from path", func(t *testing.T) {
		t.Setenv("TMUX", "1")

		var sessionUsed string
		tmux := &deps.MockTmux{
			HasSessionFunc: func(name string) bool { return false },
			NewSessionFunc: func(name, dir string) error {
				sessionUsed = name
				return nil
			},
		}

		ti := testItem("user/myapp", "/home/user/myapp")
		item := &ti
		if err := openTmuxSessionWith(tmux, item); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		want := project.SessionName("/home/user/myapp")
		if sessionUsed != want {
			t.Errorf("session name = %q, want %q (from SessionName(path), not display label)", sessionUsed, want)
		}
	})

	t.Run("bare-repo worktree uses SessionName from path", func(t *testing.T) {
		t.Setenv("TMUX", "1")

		var sessionUsed string
		tmux := &deps.MockTmux{
			HasSessionFunc: func(name string) bool { return false },
			NewSessionFunc: func(name, dir string) error {
				sessionUsed = name
				return nil
			},
		}

		ti := testItem("projects/myrepo/feature", "/projects/myrepo/feature")
		item := &ti
		if err := openTmuxSessionWith(tmux, item); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		want := project.SessionName("/projects/myrepo/feature")
		if sessionUsed != want {
			t.Errorf("session name = %q, want %q (from SessionName(path), not display label)", sessionUsed, want)
		}
	})
}

func TestOpenTmuxWindowWith(t *testing.T) {
	t.Run("selects existing window", func(t *testing.T) {
		var selectedWindow string
		tmux := &deps.MockTmux{
			CommandFunc: func(args ...string) (string, error) {
				switch args[0] {
				case "display-message":
					return "mysession", nil
				case "list-windows":
					return "main\nmyproject\nlogs", nil
				case "select-window":
					selectedWindow = args[2]
					return "", nil
				}
				return "", nil
			},
		}

		item := &ui.Item{Name: "myproject", Path: "/home/user/myproject"}
		err := openTmuxWindowWith(tmux, item)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if selectedWindow != "mysession:myproject" {
			t.Errorf("selected window = %q, want %q", selectedWindow, "mysession:myproject")
		}
	})

	t.Run("creates new window when not found", func(t *testing.T) {
		var newWindowName, newWindowDir string
		tmux := &deps.MockTmux{
			CommandFunc: func(args ...string) (string, error) {
				switch args[0] {
				case "display-message":
					return "mysession", nil
				case "list-windows":
					return "main\nlogs", nil // no "myproject"
				case "new-window":
					for i, a := range args {
						if a == "-n" && i+1 < len(args) {
							newWindowName = args[i+1]
						}
						if a == "-c" && i+1 < len(args) {
							newWindowDir = args[i+1]
						}
					}
					return "", nil
				}
				return "", nil
			},
		}

		item := &ui.Item{Name: "myproject", Path: "/home/user/myproject"}
		err := openTmuxWindowWith(tmux, item)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if newWindowName != "myproject" {
			t.Errorf("window name = %q, want %q", newWindowName, "myproject")
		}
		if newWindowDir != "/home/user/myproject" {
			t.Errorf("window dir = %q, want %q", newWindowDir, "/home/user/myproject")
		}
	})

	t.Run("sanitizes window name with dots", func(t *testing.T) {
		var selectedWindow string
		tmux := &deps.MockTmux{
			CommandFunc: func(args ...string) (string, error) {
				switch args[0] {
				case "display-message":
					return "mysession", nil
				case "list-windows":
					return "my_project", nil // sanitized name exists
				case "select-window":
					selectedWindow = args[2]
					return "", nil
				}
				return "", nil
			},
		}

		item := &ui.Item{Name: "my.project", Path: "/home/user/my.project"}
		err := openTmuxWindowWith(tmux, item)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		// Name should be sanitized: dots → underscores
		if selectedWindow != "mysession:my_project" {
			t.Errorf("selected window = %q, want %q", selectedWindow, "mysession:my_project")
		}
	})
}

// --- expandProjectsWith tests --------------------------------------------

// mockProject describes one project-path entry for buildExpandDeps.
type mockProject struct {
	path        string
	hasWorktree bool     // if true, the path is treated as a bare repo via a .bare dir
	worktrees   []string // worktree dir names under path (only when hasWorktree)
	readDirErr  error    // if non-nil, ReadDir on path fails (only when hasWorktree)
	statPanic   bool     // if true, Stat on path/.bare panics
}

// buildExpandDeps constructs a project.Deps backed by MockFileSystem that
// satisfies HasWorktreesWith + ListWorktreesForPathWith for the given mocks.
func buildExpandDeps(projects []mockProject) *project.Deps {
	statMap := make(map[string]os.FileInfo)
	readDirMap := make(map[string][]os.DirEntry)
	readDirErrs := make(map[string]error)
	panicStatPaths := make(map[string]bool)

	for _, mp := range projects {
		if mp.statPanic {
			panicStatPaths[filepath.Join(mp.path, ".bare")] = true
			continue
		}
		if mp.hasWorktree {
			statMap[filepath.Join(mp.path, ".bare")] = deps.MockFileInfo{NameVal: ".bare", IsDirVal: true}
			if mp.readDirErr != nil {
				readDirErrs[mp.path] = mp.readDirErr
				continue
			}
			var entries []os.DirEntry
			for _, wt := range mp.worktrees {
				entries = append(entries, deps.MockDirEntry{NameVal: wt, IsDirVal: true})
				// Each worktree must have a .git *file* (not dir) to be recognised.
				statMap[filepath.Join(mp.path, wt, ".git")] = deps.MockFileInfo{NameVal: ".git", IsDirVal: false}
			}
			readDirMap[mp.path] = entries
		}
		// Regular projects: no statMap entry → HasWorktreesWith returns false
		// and the goroutine treats the path as a plain directory.
	}

	return &project.Deps{
		Git: &deps.MockGit{},
		FS: &deps.MockFileSystem{
			StatFunc: func(path string) (os.FileInfo, error) {
				if panicStatPaths[path] {
					panic("intentional test panic on " + path)
				}
				if info, ok := statMap[path]; ok {
					return info, nil
				}
				return nil, os.ErrNotExist
			},
			ReadDirFunc: func(path string) ([]os.DirEntry, error) {
				if err, ok := readDirErrs[path]; ok {
					return nil, err
				}
				if entries, ok := readDirMap[path]; ok {
					return entries, nil
				}
				return nil, os.ErrNotExist
			},
		},
	}
}

// expandedNames returns the Name field of every ExpandedProject, sorted for
// deterministic comparison (goroutine ordering within expandProjectsWith is
// preserved per path but multiple test projects may interleave).
func expandedNames(projects []project.ExpandedProject) []string {
	out := make([]string, len(projects))
	for i, p := range projects {
		out[i] = p.Name
	}
	sort.Strings(out)
	return out
}

func TestExpandProjectsWith_AllRegularSucceeds(t *testing.T) {
	paths := []config.ExpandedPath{
		{Path: "/home/user/proj-a", DisplayDepth: 1},
		{Path: "/home/user/proj-b", DisplayDepth: 1},
		{Path: "/home/user/proj-c", DisplayDepth: 1},
	}
	d := buildExpandDeps(nil) // none are bare — default path returns ErrNotExist

	expanded, failed := expandProjectsWith(d, paths)

	if len(failed) != 0 {
		t.Errorf("expected no failures, got %v", failed)
	}
	got := expandedNames(expanded)
	want := []string{"proj-a", "proj-b", "proj-c"}
	if !equalStrings(got, want) {
		t.Errorf("expanded names = %v, want %v", got, want)
	}
}

func TestExpandProjectsWith_BareRepoExpandsWorktrees(t *testing.T) {
	paths := []config.ExpandedPath{
		{Path: "/home/user/bare-proj", DisplayDepth: 1},
	}
	d := buildExpandDeps([]mockProject{
		{
			path:        "/home/user/bare-proj",
			hasWorktree: true,
			worktrees:   []string{"feature-x", "main"},
		},
	})

	expanded, failed := expandProjectsWith(d, paths)

	if len(failed) != 0 {
		t.Errorf("expected no failures, got %v", failed)
	}
	got := expandedNames(expanded)
	want := []string{"bare-proj/feature-x", "bare-proj/main"}
	if !equalStrings(got, want) {
		t.Errorf("expanded names = %v, want %v", got, want)
	}
	// All entries should be flagged as worktrees
	for _, p := range expanded {
		if !p.IsWorktree {
			t.Errorf("expected IsWorktree=true for %q", p.Name)
		}
	}
}

func TestExpandProjectsWith_PartialFailureKeepsGoodProjects(t *testing.T) {
	paths := []config.ExpandedPath{
		{Path: "/home/user/good-a", DisplayDepth: 1},
		{Path: "/home/user/broken-bare", DisplayDepth: 1},
		{Path: "/home/user/good-b", DisplayDepth: 1},
	}
	d := buildExpandDeps([]mockProject{
		{
			path:        "/home/user/broken-bare",
			hasWorktree: true,
			readDirErr:  errors.New("permission denied"),
		},
	})

	expanded, failed := expandProjectsWith(d, paths)

	// Good projects survive
	got := expandedNames(expanded)
	want := []string{"good-a", "good-b"}
	if !equalStrings(got, want) {
		t.Errorf("expanded names = %v, want %v", got, want)
	}

	// Broken project is reported by its base name
	if len(failed) != 1 || failed[0] != "broken-bare" {
		t.Errorf("failed = %v, want [broken-bare]", failed)
	}
}

func TestExpandProjectsWith_AllFailedReturnsEmpty(t *testing.T) {
	paths := []config.ExpandedPath{
		{Path: "/home/user/broken-1", DisplayDepth: 1},
		{Path: "/home/user/broken-2", DisplayDepth: 1},
	}
	d := buildExpandDeps([]mockProject{
		{path: "/home/user/broken-1", hasWorktree: true, readDirErr: errors.New("io error")},
		{path: "/home/user/broken-2", hasWorktree: true, readDirErr: errors.New("io error")},
	})

	expanded, failed := expandProjectsWith(d, paths)

	if len(expanded) != 0 {
		t.Errorf("expected zero expanded projects, got %d", len(expanded))
	}
	if len(failed) != 2 {
		t.Errorf("expected 2 failures, got %v", failed)
	}
}

func TestExpandProjectsWith_PanicIsCapturedAsFailure(t *testing.T) {
	paths := []config.ExpandedPath{
		{Path: "/home/user/exploding", DisplayDepth: 1},
		{Path: "/home/user/fine", DisplayDepth: 1},
	}
	d := buildExpandDeps([]mockProject{
		{path: "/home/user/exploding", statPanic: true},
	})

	// Must not crash the test process — recover inside the goroutine catches it.
	expanded, failed := expandProjectsWith(d, paths)

	// The non-panicking project still expands successfully
	got := expandedNames(expanded)
	want := []string{"fine"}
	if !equalStrings(got, want) {
		t.Errorf("expanded names = %v, want %v", got, want)
	}

	// The panicking project is reported as a failure
	if len(failed) != 1 || failed[0] != "exploding" {
		t.Errorf("failed = %v, want [exploding]", failed)
	}
}

func TestExpandProjectsWith_EmptyInput(t *testing.T) {
	d := buildExpandDeps(nil)
	expanded, failed := expandProjectsWith(d, nil)
	if len(expanded) != 0 {
		t.Errorf("expected zero expanded, got %d", len(expanded))
	}
	if len(failed) != 0 {
		t.Errorf("expected zero failed, got %v", failed)
	}
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// scriptedPicker returns a RunPicker function that calls each fn in sequence
// on successive picker iterations. Each fn receives the actual items passed
// to the picker so results can reference items[N] directly. When the sequence
// is exhausted, the function returns ActionCancel to terminate loops cleanly.
// Modeled on mockPickDirSequence in configure_test.go.
func scriptedPicker(fns ...func(items []ui.Item) ui.Result) func(items []ui.Item, opts ...ui.PickerOption) (ui.Result, error) {
	i := 0
	return func(items []ui.Item, opts ...ui.PickerOption) (ui.Result, error) {
		if i >= len(fns) {
			return ui.Result{Action: ui.ActionCancel}, nil
		}
		fn := fns[i]
		i++
		return fn(items), nil
	}
}

// testProjectDeps returns a ProjectDeps with no-op defaults safe for tests.
// Callers should override only the fields their test cares about.
//
// History and cache paths are sandboxed via t.Setenv so tests do not touch
// the user's real history, config, or cache files. LoadConfig returns a
// config pointing at a fresh t.TempDir, which cfg.ExpandProjects resolves
// to exactly one item (not a bare repo, no worktrees) — enough for the
// picker loop to reach its first iteration.
func testProjectDeps(t *testing.T) *ProjectDeps {
	t.Helper()

	// Sandbox XDG_* paths for defense in depth — any code that touches
	// history.DefaultHistoryPath, monitor state, or glob cache will be
	// redirected to a throwaway location.
	xdg := t.TempDir()
	t.Setenv("XDG_DATA_HOME", xdg)
	t.Setenv("XDG_CACHE_HOME", filepath.Join(xdg, "cache"))
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(xdg, "config"))

	// Default project directory — a real tmpdir so cfg.ExpandProjects
	// produces exactly one entry. Tests that need more items can override
	// LoadConfig.
	projectDir := t.TempDir()

	return &ProjectDeps{
		Tmux: &deps.MockTmux{},
		Project: &project.Deps{
			Git: &deps.MockGit{},
			FS:  &deps.MockFileSystem{},
		},

		LoadConfig: func() (*config.Config, error) {
			return &config.Config{
				Projects: []config.ProjectEntry{{Path: projectDir}},
			}, nil
		},
		LoadHistory: func() (*history.History, error) {
			// Bind to a sandbox path so any hist.Save() writes to the tmpdir.
			return history.Load(filepath.Join(xdg, "pop", "history.json"))
		},

		RunPicker: func(items []ui.Item, opts ...ui.PickerOption) (ui.Result, error) {
			return ui.Result{Action: ui.ActionCancel}, nil
		},

		SessionActivity:   func() map[string]int64 { return nil },
		AttentionSessions: func() map[string]bool { return nil },

		OpenSession:              func(tmux deps.Tmux, item *ui.Item) error { return nil },
		OpenSessionWithWorkbench: func(tmux deps.Tmux, item *ui.Item, workbenchName string) error { return nil },
		OpenWindow:               func(tmux deps.Tmux, item *ui.Item) error { return nil },
		KillSession:              func(tmux deps.Tmux, name string) {},
		SendCDToPane:             func(tmux deps.Tmux, paneID, path string) error { return nil },
		SwitchToTarget:           func(tmux deps.Tmux, target string) error { return nil },
		SwitchAndZoom:            func(tmux deps.Tmux, target string) error { return nil },
		RunCustomCommand:         func(command string, item *ui.Item) {},
		EnsureSystemState:        func() []string { return nil },
		RunConfigure:             func() error { return nil },

		ResolveWorkbenches:        func(cfg *config.Config, path string) []config.Workbench { return nil },
		ResolvePreferredWorkbench: func(cfg *config.Config, path string) (string, []string) { return "", nil },

		InTmux:         func() bool { return false },
		CurrentSession: func(tmux deps.Tmux) string { return "" },
	}
}

func TestRunProject_ActionConfirmRecordsHistory(t *testing.T) {
	var openedItem *ui.Item
	var hist *history.History

	d := testProjectDeps(t)
	// Capture the history object the loop sees, so we can inspect entries
	// after RunProject returns.
	origLoadHistory := d.LoadHistory
	d.LoadHistory = func() (*history.History, error) {
		h, err := origLoadHistory()
		hist = h
		return h, err
	}
	d.RunPicker = scriptedPicker(func(items []ui.Item) ui.Result {
		return ui.Result{
			Action:      ui.ActionConfirm,
			Selected:    &items[0],
			CursorIndex: 0,
		}
	})
	d.OpenSession = func(tmux deps.Tmux, item *ui.Item) error {
		openedItem = item
		return nil
	}

	if err := RunProject(d); err != nil {
		t.Fatalf("RunProject: %v", err)
	}

	if openedItem == nil {
		t.Fatal("expected OpenSession to be called")
	}
	if hist == nil {
		t.Fatal("LoadHistory was not called")
	}
	if len(hist.Entries) != 1 {
		t.Fatalf("expected 1 history entry, got %d", len(hist.Entries))
	}
	// The path recorded in history must match the path passed to OpenSession.
	// Both should be the canonical form produced by config.ExpandProjects
	// (which resolves symlinks — on macOS, /var/folders/... becomes
	// /private/var/folders/...), so asserting they agree is the
	// load-bearing invariant without hardcoding a canonical form.
	if hist.Entries[0].Path != openedItem.Path {
		t.Errorf("history recorded %q but OpenSession opened %q — paths disagree",
			hist.Entries[0].Path, openedItem.Path)
	}
}

func TestRunProject_ActionKillSessionContinuesLoop(t *testing.T) {
	var killedNames []string
	var pickerCalls int
	var selectedPath string

	d := testProjectDeps(t)
	d.RunPicker = func(items []ui.Item, opts ...ui.PickerOption) (ui.Result, error) {
		pickerCalls++
		switch pickerCalls {
		case 1:
			selectedPath = items[0].Path
			return ui.Result{
				Action:      ui.ActionKillSession,
				Selected:    &items[0],
				CursorIndex: 7,
			}, nil
		case 2:
			return ui.Result{Action: ui.ActionCancel}, nil
		default:
			t.Fatalf("picker called %d times, expected at most 2", pickerCalls)
			return ui.Result{}, nil
		}
	}
	d.KillSession = func(tmux deps.Tmux, name string) {
		killedNames = append(killedNames, name)
	}

	if err := RunProject(d); err != nil {
		t.Fatalf("RunProject: %v", err)
	}

	if pickerCalls != 2 {
		t.Errorf("picker called %d times, want 2 (kill → cancel)", pickerCalls)
	}
	if len(killedNames) != 1 {
		t.Fatalf("expected 1 kill, got %d: %v", len(killedNames), killedNames)
	}
	if killedNames[0] != project.SessionName(selectedPath) {
		t.Errorf("killed session %q, want SessionName(%q) = %q",
			killedNames[0], selectedPath, project.SessionName(selectedPath))
	}
}

func TestRunProject_ActionCancelExitsCleanly(t *testing.T) {
	var pickerCalls int
	openCalled := false

	d := testProjectDeps(t)
	d.RunPicker = func(items []ui.Item, opts ...ui.PickerOption) (ui.Result, error) {
		pickerCalls++
		return ui.Result{Action: ui.ActionCancel}, nil
	}
	d.OpenSession = func(tmux deps.Tmux, item *ui.Item) error {
		openCalled = true
		return nil
	}

	if err := RunProject(d); err != nil {
		t.Fatalf("RunProject on ActionCancel: unexpected error %v", err)
	}

	if pickerCalls != 1 {
		t.Errorf("picker called %d times, want 1", pickerCalls)
	}
	if openCalled {
		t.Error("OpenSession called on cancel path — expected no-op")
	}
}

// TestRunProject_UpdateNoticeKillSwitch verifies the [updates] kill switch:
// with notice_enabled = false the UpdateNotice seam is never invoked (so no
// background update fetch is scheduled), and it is invoked when enabled.
func TestRunProject_UpdateNoticeKillSwitch(t *testing.T) {
	disabled := false
	enabled := true

	tests := []struct {
		name       string
		updates    *config.UpdatesConfig
		wantCalled bool
	}{
		{name: "absent section defaults to enabled", updates: nil, wantCalled: true},
		{name: "explicit true enabled", updates: &config.UpdatesConfig{NoticeEnabled: &enabled}, wantCalled: true},
		{name: "explicit false disabled", updates: &config.UpdatesConfig{NoticeEnabled: &disabled}, wantCalled: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			projectDir := t.TempDir()
			noticeCalled := false

			d := testProjectDeps(t)
			d.LoadConfig = func() (*config.Config, error) {
				return &config.Config{
					Projects: []config.ProjectEntry{{Path: projectDir}},
					Updates:  tt.updates,
				}, nil
			}
			d.UpdateNotice = func() string {
				noticeCalled = true
				return "1.2.3"
			}
			d.RunPicker = func(items []ui.Item, opts ...ui.PickerOption) (ui.Result, error) {
				return ui.Result{Action: ui.ActionCancel}, nil
			}

			if err := RunProject(d); err != nil {
				t.Fatalf("RunProject: unexpected error %v", err)
			}
			if noticeCalled != tt.wantCalled {
				t.Errorf("UpdateNotice called = %v, want %v", noticeCalled, tt.wantCalled)
			}
		})
	}
}

// TestRunProject_NoGitCallsDuringPickerOpen asserts that opening the project
// picker does not invoke git commands. Regression guard for the session-module
// change that called project.SessionName (which runs git) inside hot loops
// (icon matching, exclusion filtering, etc.), causing 200+ subprocess
// invocations on every open.
func TestRunProject_NoGitCallsDuringPickerOpen(t *testing.T) {
	gitCalls := 0
	d := testProjectDeps(t)
	d.Project.Git = &deps.MockGit{
		CommandInDirFunc: func(dir string, args ...string) (string, error) {
			gitCalls++
			return "", fmt.Errorf("unexpected git call: %v in %s", args, dir)
		},
	}
	d.RunPicker = func(items []ui.Item, opts ...ui.PickerOption) (ui.Result, error) {
		return ui.Result{Action: ui.ActionCancel}, nil
	}

	if err := RunProject(d); err != nil {
		t.Fatalf("RunProject: %v", err)
	}

	if gitCalls > 0 {
		t.Errorf("picker open triggered %d git call(s); expected 0 — SessionName should be pre-computed during expansion", gitCalls)
	}
}

// TestRunProject_StaleEffortKeyRendersWithBanner proves the ADR 0054 spine
// end-to-end: a config whose only problem is a stale [effort] key — which the
// project dashboard never consumes — must still render the project list (Load
// no longer aborts), and the resulting finding must reach the picker's warning
// banner through the cfg.Warnings path.
func TestRunProject_StaleEffortKeyRendersWithBanner(t *testing.T) {
	projectDir := t.TempDir()
	configPath := filepath.Join(t.TempDir(), "config.toml")
	body := fmt.Sprintf(`
projects = [{ path = %q }]

[effort.opencode]
extreme = [{ model = "opencode/claude-opus-4-8" }]
`, projectDir)
	if err := os.WriteFile(configPath, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}

	d := testProjectDeps(t)
	d.LoadConfig = func() (*config.Config, error) { return config.Load(configPath) }

	var capturedItems []ui.Item
	var capturedOpts []ui.PickerOption
	d.RunPicker = func(items []ui.Item, opts ...ui.PickerOption) (ui.Result, error) {
		capturedItems = items
		capturedOpts = opts
		return ui.Result{Action: ui.ActionCancel}, nil
	}

	if err := RunProject(d); err != nil {
		t.Fatalf("RunProject aborted on a stale [effort] key the dashboard never consumes: %v", err)
	}
	if len(capturedItems) == 0 {
		t.Fatal("expected the project list to render at least one item")
	}

	// Reconstruct the picker the loop built and assert the finding lands in the
	// rendered warning banner.
	view := ui.NewPicker(capturedItems, capturedOpts...).View().Content
	if !strings.Contains(view, "unknown tier") {
		t.Errorf("expected the effort finding in the picker warning banner, got view:\n%s", view)
	}
}

// TestRunProject_ExecutionRenameRendersWithBanner asserts that the deliberate
// queue_base→trunk migration tripwire — fatal to the queue/drain commands that
// resolve execution config — is invisible to the project dashboard's capability
// (it never resolves repo config), so the list still renders. The finding still
// surfaces in the non-blocking warning banner (ADR 0054).
func TestRunProject_ExecutionRenameRendersWithBanner(t *testing.T) {
	projectDir := t.TempDir()
	configPath := filepath.Join(t.TempDir(), "config.toml")
	body := fmt.Sprintf(`
projects = [{ path = %q }]

[repo."/some/repo"]
queue_base = true
`, projectDir)
	if err := os.WriteFile(configPath, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}

	d := testProjectDeps(t)
	d.LoadConfig = func() (*config.Config, error) { return config.Load(configPath) }

	var capturedItems []ui.Item
	var capturedOpts []ui.PickerOption
	d.RunPicker = func(items []ui.Item, opts ...ui.PickerOption) (ui.Result, error) {
		capturedItems = items
		capturedOpts = opts
		return ui.Result{Action: ui.ActionCancel}, nil
	}

	if err := RunProject(d); err != nil {
		t.Fatalf("RunProject aborted on an execution-config rename it never consumes: %v", err)
	}
	if len(capturedItems) == 0 {
		t.Fatal("expected the project list to render at least one item")
	}

	view := ui.NewPicker(capturedItems, capturedOpts...).View().Content
	if !strings.Contains(view, "queue_base was renamed to trunk") {
		t.Errorf("expected the execution-rename finding in the picker warning banner, got view:\n%s", view)
	}
}

// TestRunProject_InvalidDisplayDepthRendersWithBanner asserts that a wrong-typed
// display_depth (a non-essential value in a consumed section) degrades to the
// default depth plus a warning: the picker still renders and the finding lands
// in the banner (ADR 0054).
func TestRunProject_InvalidDisplayDepthRendersWithBanner(t *testing.T) {
	projectDir := t.TempDir()
	configPath := filepath.Join(t.TempDir(), "config.toml")
	body := fmt.Sprintf("projects = [{ path = %q, display_depth = \"two\" }]\n", projectDir)
	if err := os.WriteFile(configPath, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}

	d := testProjectDeps(t)
	d.LoadConfig = func() (*config.Config, error) { return config.Load(configPath) }

	var capturedItems []ui.Item
	var capturedOpts []ui.PickerOption
	d.RunPicker = func(items []ui.Item, opts ...ui.PickerOption) (ui.Result, error) {
		capturedItems = items
		capturedOpts = opts
		return ui.Result{Action: ui.ActionCancel}, nil
	}

	if err := RunProject(d); err != nil {
		t.Fatalf("RunProject aborted on a non-essential bad display_depth: %v", err)
	}
	if len(capturedItems) == 0 {
		t.Fatal("expected the project list to render at least one item")
	}

	view := ui.NewPicker(capturedItems, capturedOpts...).View().Content
	if !strings.Contains(view, "non-integer display_depth") {
		t.Errorf("expected the display_depth finding in the picker warning banner, got view:\n%s", view)
	}
}

// TestRunProject_MalformedGlobRendersResolvedAndWarns asserts that one malformed
// glob alongside one good entry renders the directories that resolved and warns
// about the malformed pattern instead of aborting (ADR 0054).
func TestRunProject_MalformedGlobRendersResolvedAndWarns(t *testing.T) {
	base := t.TempDir()
	if err := os.Mkdir(filepath.Join(base, "repo"), 0o755); err != nil {
		t.Fatal(err)
	}
	configPath := filepath.Join(t.TempDir(), "config.toml")
	body := fmt.Sprintf("projects = [{ path = %q }, { path = %q }]\n",
		filepath.Join(base, "[a-")+"/*", filepath.Join(base, "*"))
	if err := os.WriteFile(configPath, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}

	d := testProjectDeps(t)
	d.LoadConfig = func() (*config.Config, error) { return config.Load(configPath) }

	var capturedItems []ui.Item
	var capturedOpts []ui.PickerOption
	d.RunPicker = func(items []ui.Item, opts ...ui.PickerOption) (ui.Result, error) {
		capturedItems = items
		capturedOpts = opts
		return ui.Result{Action: ui.ActionCancel}, nil
	}

	if err := RunProject(d); err != nil {
		t.Fatalf("RunProject aborted despite a partially-resolving config: %v", err)
	}
	var rendered bool
	for _, it := range capturedItems {
		if filepath.Base(it.Path) == "repo" {
			rendered = true
		}
	}
	if !rendered {
		t.Fatalf("expected the good entry to render a repo item, got %+v", capturedItems)
	}

	view := ui.NewPicker(capturedItems, capturedOpts...).View().Content
	if !strings.Contains(view, "not a valid glob pattern") {
		t.Errorf("expected the malformed-glob warning in the picker banner, got view:\n%s", view)
	}
}

// TestRunProject_ZeroUsableDirectoriesAborts asserts that a projects table that
// yields no usable directories keeps the existing clean hard-fail — there is
// nothing to switch to (ADR 0054).
func TestRunProject_ZeroUsableDirectoriesAborts(t *testing.T) {
	base := t.TempDir()
	configPath := filepath.Join(t.TempDir(), "config.toml")
	// A single malformed glob resolves to nothing.
	body := fmt.Sprintf("projects = [{ path = %q }]\n", filepath.Join(base, "[a-")+"/*")
	if err := os.WriteFile(configPath, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}

	d := testProjectDeps(t)
	d.LoadConfig = func() (*config.Config, error) { return config.Load(configPath) }
	d.RunConfigure = func() error { t.Fatal("RunConfigure should not run for an existing config"); return nil }

	err := RunProject(d)
	if err == nil {
		t.Fatal("expected RunProject to hard-fail when no directories resolve")
	}
	if !strings.Contains(err.Error(), "no projects found") {
		t.Errorf("error = %v, want a clear no-projects-found message", err)
	}
}

// TestRunProject_UnparseableTOMLAborts asserts that unparseable TOML (class A)
// still hard-fails the dashboard with a clear message (ADR 0054).
func TestRunProject_UnparseableTOMLAborts(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(configPath, []byte("this is = not valid = toml\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	d := testProjectDeps(t)
	d.LoadConfig = func() (*config.Config, error) { return config.Load(configPath) }
	d.RunConfigure = func() error { t.Fatal("RunConfigure should not run for unparseable TOML"); return nil }

	err := RunProject(d)
	if err == nil {
		t.Fatal("expected RunProject to hard-fail on unparseable TOML")
	}
	if !strings.Contains(err.Error(), "failed to load config") {
		t.Errorf("error = %v, want a clear config-load failure message", err)
	}
}

// --- Picker create-path Workbench selection (ADR-0075) ---

// projectDepsForWorkbenchPrompt builds a ProjectDeps with [workbench]
// pick_on_create = true and a fixed resolved-Workbench set, so the create-path
// prompt can be exercised without touching .pop.toml or the global library.
func projectDepsForWorkbenchPrompt(t *testing.T, workbenches []config.Workbench) *ProjectDeps {
	t.Helper()
	d := testProjectDeps(t)
	orig := d.LoadConfig
	d.LoadConfig = func() (*config.Config, error) {
		cfg, err := orig()
		if err != nil {
			return nil, err
		}
		cfg.WorkbenchOpts = &config.WorkbenchOptions{PickOnCreate: true}
		return cfg, nil
	}
	d.ResolveWorkbenches = func(cfg *config.Config, path string) []config.Workbench {
		return workbenches
	}
	return d
}

// TestRunProject_WorkbenchPickDisabledNoPrompt asserts the default (toggle off)
// path is byte-for-byte today's behavior: one picker, flat session, and the
// Workbench machinery is never consulted.
func TestRunProject_WorkbenchPickDisabledNoPrompt(t *testing.T) {
	d := testProjectDeps(t) // pick_on_create defaults false
	resolveCalled := false
	d.ResolveWorkbenches = func(cfg *config.Config, path string) []config.Workbench {
		resolveCalled = true
		return []config.Workbench{{Name: "dev"}}
	}
	openFlat := false
	d.OpenSession = func(tmux deps.Tmux, item *ui.Item) error { openFlat = true; return nil }
	openWB := false
	d.OpenSessionWithWorkbench = func(tmux deps.Tmux, item *ui.Item, name string) error { openWB = true; return nil }

	calls := 0
	d.RunPicker = func(items []ui.Item, opts ...ui.PickerOption) (ui.Result, error) {
		calls++
		return ui.Result{Action: ui.ActionConfirm, Selected: &items[0]}, nil
	}

	if err := RunProject(d); err != nil {
		t.Fatalf("RunProject: %v", err)
	}
	if !openFlat {
		t.Error("expected flat OpenSession when pick_on_create is off")
	}
	if openWB {
		t.Error("OpenSessionWithWorkbench must not run when pick_on_create is off")
	}
	if resolveCalled {
		t.Error("ResolveWorkbenches must not be consulted when pick_on_create is off")
	}
	if calls != 1 {
		t.Errorf("picker shown %d times, want 1 (no Workbench prompt)", calls)
	}
}

// TestRunProject_WorkbenchPickEmptySetSkipsPrompt asserts that with the toggle on
// but no Workbenches resolving for the project, the prompt is skipped entirely
// and a flat session is created.
func TestRunProject_WorkbenchPickEmptySetSkipsPrompt(t *testing.T) {
	d := projectDepsForWorkbenchPrompt(t, nil) // empty resolved set
	openFlat := false
	d.OpenSession = func(tmux deps.Tmux, item *ui.Item) error { openFlat = true; return nil }
	openWB := false
	d.OpenSessionWithWorkbench = func(tmux deps.Tmux, item *ui.Item, name string) error { openWB = true; return nil }

	calls := 0
	d.RunPicker = func(items []ui.Item, opts ...ui.PickerOption) (ui.Result, error) {
		calls++
		return ui.Result{Action: ui.ActionConfirm, Selected: &items[0]}, nil
	}

	if err := RunProject(d); err != nil {
		t.Fatalf("RunProject: %v", err)
	}
	if !openFlat {
		t.Error("expected flat OpenSession when no Workbenches resolve")
	}
	if openWB {
		t.Error("OpenSessionWithWorkbench must not run with an empty Workbench set")
	}
	if calls != 1 {
		t.Errorf("picker shown %d times, want 1 (empty set => no prompt)", calls)
	}
}

// TestRunProject_WorkbenchPickSelectsWorkbench asserts that with the toggle on
// and ≥1 Workbench, the second (quick-search) list shows "no workbench" first and
// selecting a real Workbench routes to OpenSessionWithWorkbench (not the flat path).
func TestRunProject_WorkbenchPickSelectsWorkbench(t *testing.T) {
	d := projectDepsForWorkbenchPrompt(t, []config.Workbench{{Name: "gs-dev"}, {Name: "minimal"}})

	openFlat := false
	d.OpenSession = func(tmux deps.Tmux, item *ui.Item) error { openFlat = true; return nil }
	var gotName string
	openWB := false
	d.OpenSessionWithWorkbench = func(tmux deps.Tmux, item *ui.Item, name string) error {
		openWB = true
		gotName = name
		return nil
	}

	var wbItems []ui.Item
	calls := 0
	d.RunPicker = func(items []ui.Item, opts ...ui.PickerOption) (ui.Result, error) {
		calls++
		if calls == 1 {
			// Project picker: choose the (only) project.
			return ui.Result{Action: ui.ActionConfirm, Selected: &items[0]}, nil
		}
		// Workbench quick-search list: select the first real Workbench (gs-dev).
		wbItems = items
		return ui.Result{Action: ui.ActionConfirm, Selected: &items[1]}, nil
	}

	if err := RunProject(d); err != nil {
		t.Fatalf("RunProject: %v", err)
	}
	if len(wbItems) != 3 {
		t.Fatalf("Workbench list had %d items, want 3 (no workbench + 2)", len(wbItems))
	}
	if wbItems[0].Name != noWorkbenchLabel {
		t.Errorf("Workbench list first entry = %q, want %q", wbItems[0].Name, noWorkbenchLabel)
	}
	if wbItems[0].Path != "" {
		t.Errorf("no-workbench entry Path = %q, want empty sentinel", wbItems[0].Path)
	}
	if !openWB {
		t.Fatal("expected OpenSessionWithWorkbench to run")
	}
	if gotName != "gs-dev" {
		t.Errorf("OpenSessionWithWorkbench name = %q, want %q", gotName, "gs-dev")
	}
	if openFlat {
		t.Error("flat OpenSession must not run when a Workbench was chosen")
	}
}

// TestRunProject_WorkbenchPickNoWorkbenchYieldsFlat asserts that choosing the
// "no workbench" entry creates today's flat session.
func TestRunProject_WorkbenchPickNoWorkbenchYieldsFlat(t *testing.T) {
	d := projectDepsForWorkbenchPrompt(t, []config.Workbench{{Name: "gs-dev"}})

	openFlat := false
	d.OpenSession = func(tmux deps.Tmux, item *ui.Item) error { openFlat = true; return nil }
	openWB := false
	d.OpenSessionWithWorkbench = func(tmux deps.Tmux, item *ui.Item, name string) error { openWB = true; return nil }

	calls := 0
	d.RunPicker = func(items []ui.Item, opts ...ui.PickerOption) (ui.Result, error) {
		calls++
		if calls == 1 {
			return ui.Result{Action: ui.ActionConfirm, Selected: &items[0]}, nil
		}
		// Pick the preselected first entry: "no workbench".
		return ui.Result{Action: ui.ActionConfirm, Selected: &items[0]}, nil
	}

	if err := RunProject(d); err != nil {
		t.Fatalf("RunProject: %v", err)
	}
	if !openFlat {
		t.Error("expected flat OpenSession for the no-workbench choice")
	}
	if openWB {
		t.Error("OpenSessionWithWorkbench must not run for the no-workbench choice")
	}
}

// TestRunProject_PreferredWorkbenchAutoApplies asserts that a resolved preferred
// workbench (ADR-0078) auto-applies silently and suppresses the prompt whether
// pick_on_create is off or on — one picker, straight to OpenSessionWithWorkbench.
func TestRunProject_PreferredWorkbenchAutoApplies(t *testing.T) {
	for _, pickOn := range []bool{false, true} {
		name := "pick_on_create_off"
		if pickOn {
			name = "pick_on_create_on"
		}
		t.Run(name, func(t *testing.T) {
			d := testProjectDeps(t)
			if pickOn {
				orig := d.LoadConfig
				d.LoadConfig = func() (*config.Config, error) {
					cfg, err := orig()
					if err != nil {
						return nil, err
					}
					cfg.WorkbenchOpts = &config.WorkbenchOptions{PickOnCreate: true}
					return cfg, nil
				}
			}
			d.ResolvePreferredWorkbench = func(cfg *config.Config, path string) (string, []string) {
				return "gs-dev", nil
			}

			openFlat := false
			d.OpenSession = func(tmux deps.Tmux, item *ui.Item) error { openFlat = true; return nil }
			var gotName string
			openWB := false
			d.OpenSessionWithWorkbench = func(tmux deps.Tmux, item *ui.Item, n string) error {
				openWB = true
				gotName = n
				return nil
			}

			calls := 0
			d.RunPicker = func(items []ui.Item, opts ...ui.PickerOption) (ui.Result, error) {
				calls++
				return ui.Result{Action: ui.ActionConfirm, Selected: &items[0]}, nil
			}

			if err := RunProject(d); err != nil {
				t.Fatalf("RunProject: %v", err)
			}
			if !openWB {
				t.Fatal("expected preferred workbench to auto-apply via OpenSessionWithWorkbench")
			}
			if gotName != "gs-dev" {
				t.Errorf("auto-applied workbench = %q, want %q", gotName, "gs-dev")
			}
			if openFlat {
				t.Error("flat OpenSession must not run when a preferred workbench resolves")
			}
			if calls != 1 {
				t.Errorf("picker shown %d times, want 1 (no prompt when preferred resolves)", calls)
			}
		})
	}
}

// TestRunProject_StalePreferredFallsThrough asserts that a preferred workbench
// that does not resolve (empty name + warning) never blocks the open: with
// pick_on_create off it falls through to today's flat session.
func TestRunProject_StalePreferredFallsThrough(t *testing.T) {
	d := testProjectDeps(t) // pick_on_create defaults false
	d.ResolvePreferredWorkbench = func(cfg *config.Config, path string) (string, []string) {
		return "", []string{"preferred workbench \"ghost\" does not resolve; ignoring"}
	}
	openFlat := false
	d.OpenSession = func(tmux deps.Tmux, item *ui.Item) error { openFlat = true; return nil }
	openWB := false
	d.OpenSessionWithWorkbench = func(tmux deps.Tmux, item *ui.Item, n string) error { openWB = true; return nil }

	d.RunPicker = func(items []ui.Item, opts ...ui.PickerOption) (ui.Result, error) {
		return ui.Result{Action: ui.ActionConfirm, Selected: &items[0]}, nil
	}

	if err := RunProject(d); err != nil {
		t.Fatalf("RunProject: %v", err)
	}
	if !openFlat {
		t.Error("expected flat OpenSession when the preferred workbench is stale")
	}
	if openWB {
		t.Error("OpenSessionWithWorkbench must not run for a stale preferred workbench")
	}
}

// TestRunProject_WorkbenchPickEscReturnsToProjectPicker asserts that Esc in the
// Workbench list creates nothing and returns to the project picker.
func TestRunProject_WorkbenchPickEscReturnsToProjectPicker(t *testing.T) {
	d := projectDepsForWorkbenchPrompt(t, []config.Workbench{{Name: "gs-dev"}})

	openFlat := false
	d.OpenSession = func(tmux deps.Tmux, item *ui.Item) error { openFlat = true; return nil }
	openWB := false
	d.OpenSessionWithWorkbench = func(tmux deps.Tmux, item *ui.Item, name string) error { openWB = true; return nil }

	calls := 0
	d.RunPicker = func(items []ui.Item, opts ...ui.PickerOption) (ui.Result, error) {
		calls++
		switch calls {
		case 1:
			// Project picker: choose the project.
			return ui.Result{Action: ui.ActionConfirm, Selected: &items[0]}, nil
		case 2:
			// Workbench list: Esc.
			return ui.Result{Action: ui.ActionCancel}, nil
		default:
			// Back at the project picker: quit cleanly.
			return ui.Result{Action: ui.ActionCancel}, nil
		}
	}

	if err := RunProject(d); err != nil {
		t.Fatalf("RunProject: %v", err)
	}
	if openFlat || openWB {
		t.Error("Esc in the Workbench list must create nothing")
	}
	if calls != 3 {
		t.Errorf("picker shown %d times, want 3 (project, workbench-esc, project-cancel)", calls)
	}
}

// TestRunProject_WorkbenchPickLiveSessionNoPrompt asserts the prompt fires only
// on selections that create a session: a project whose session is already live
// skips the prompt and reattaches via the flat path.
func TestRunProject_WorkbenchPickLiveSessionNoPrompt(t *testing.T) {
	d := projectDepsForWorkbenchPrompt(t, []config.Workbench{{Name: "gs-dev"}})
	d.Tmux = &deps.MockTmux{HasSessionFunc: func(name string) bool { return true }}

	resolveCalled := false
	d.ResolveWorkbenches = func(cfg *config.Config, path string) []config.Workbench {
		resolveCalled = true
		return []config.Workbench{{Name: "gs-dev"}}
	}
	openFlat := false
	d.OpenSession = func(tmux deps.Tmux, item *ui.Item) error { openFlat = true; return nil }
	openWB := false
	d.OpenSessionWithWorkbench = func(tmux deps.Tmux, item *ui.Item, name string) error { openWB = true; return nil }

	calls := 0
	d.RunPicker = func(items []ui.Item, opts ...ui.PickerOption) (ui.Result, error) {
		calls++
		return ui.Result{Action: ui.ActionConfirm, Selected: &items[0]}, nil
	}

	if err := RunProject(d); err != nil {
		t.Fatalf("RunProject: %v", err)
	}
	if !openFlat {
		t.Error("expected flat OpenSession (reattach) for a live session")
	}
	if openWB {
		t.Error("OpenSessionWithWorkbench must not run when the session is already live")
	}
	if resolveCalled {
		t.Error("ResolveWorkbenches must not be consulted when the session is already live")
	}
	if calls != 1 {
		t.Errorf("picker shown %d times, want 1 (no prompt for a live session)", calls)
	}
}

// TestRunProject_WorkbenchPickOpenWindowUnaffected asserts that the
// open-in-new-window action never triggers the Workbench prompt, even with the
// toggle on.
func TestRunProject_WorkbenchPickOpenWindowUnaffected(t *testing.T) {
	d := projectDepsForWorkbenchPrompt(t, []config.Workbench{{Name: "gs-dev"}})

	resolveCalled := false
	d.ResolveWorkbenches = func(cfg *config.Config, path string) []config.Workbench {
		resolveCalled = true
		return []config.Workbench{{Name: "gs-dev"}}
	}
	openWindow := false
	d.OpenWindow = func(tmux deps.Tmux, item *ui.Item) error { openWindow = true; return nil }
	openWB := false
	d.OpenSessionWithWorkbench = func(tmux deps.Tmux, item *ui.Item, name string) error { openWB = true; return nil }

	calls := 0
	d.RunPicker = func(items []ui.Item, opts ...ui.PickerOption) (ui.Result, error) {
		calls++
		return ui.Result{Action: ui.ActionOpenWindow, Selected: &items[0]}, nil
	}

	if err := RunProject(d); err != nil {
		t.Fatalf("RunProject: %v", err)
	}
	if !openWindow {
		t.Error("expected OpenWindow for the open-in-new-window action")
	}
	if openWB || resolveCalled {
		t.Error("open-in-new-window must not trigger the Workbench prompt")
	}
	if calls != 1 {
		t.Errorf("picker shown %d times, want 1", calls)
	}
}

// TestRunProject_WorkbenchPickStandaloneUnaffected asserts that selecting a
// standalone session switches to it without any Workbench prompt.
func TestRunProject_WorkbenchPickStandaloneUnaffected(t *testing.T) {
	d := projectDepsForWorkbenchPrompt(t, []config.Workbench{{Name: "gs-dev"}})

	resolveCalled := false
	d.ResolveWorkbenches = func(cfg *config.Config, path string) []config.Workbench {
		resolveCalled = true
		return []config.Workbench{{Name: "gs-dev"}}
	}
	var switched string
	d.SwitchToTarget = func(tmux deps.Tmux, target string) error { switched = target; return nil }
	openWB := false
	d.OpenSessionWithWorkbench = func(tmux deps.Tmux, item *ui.Item, name string) error { openWB = true; return nil }

	d.RunPicker = func(items []ui.Item, opts ...ui.PickerOption) (ui.Result, error) {
		return ui.Result{
			Action:   ui.ActionConfirm,
			Selected: &ui.Item{Name: "scratch", Path: tmuxSessionPathPrefix + "scratch"},
		}, nil
	}

	if err := RunProject(d); err != nil {
		t.Fatalf("RunProject: %v", err)
	}
	if switched != "scratch" {
		t.Errorf("SwitchToTarget target = %q, want %q", switched, "scratch")
	}
	if openWB || resolveCalled {
		t.Error("standalone selection must not trigger the Workbench prompt")
	}
}
