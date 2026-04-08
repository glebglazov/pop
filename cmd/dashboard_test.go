package cmd

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/glebglazov/pop/config"
	"github.com/glebglazov/pop/history"
	"github.com/glebglazov/pop/internal/deps"
	"github.com/glebglazov/pop/monitor"
	"github.com/glebglazov/pop/ui"
)

// TestBuildDashboardPanes_OnlyAgenticPanes is the end-to-end guard against
// the "all tmux panes show as idle on dashboard" regression. It drives state
// through the same code path the real tmux hooks and agent extensions use
// (runPaneSetStatusWith) and then asserts that buildDashboardPanesWithCurrentPane
// surfaces ONLY panes that have been actively claimed by an agent — never
// the panes the user has merely navigated to.
func TestBuildDashboardPanes_OnlyAgenticPanes(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_DATA_HOME", dir)
	stateDir := filepath.Join(dir, "pop")
	if err := os.MkdirAll(stateDir, 0755); err != nil {
		t.Fatal(err)
	}
	statePath := filepath.Join(stateDir, "monitor.json")
	emptyState := &monitor.State{Panes: map[string]*monitor.PaneEntry{}}
	data, _ := json.Marshal(emptyState)
	if err := os.WriteFile(statePath, data, 0644); err != nil {
		t.Fatal(err)
	}

	tmux := &deps.MockTmux{
		CommandFunc: func(args ...string) (string, error) {
			return "some-session", nil
		},
	}
	cfg := &config.Config{}

	// 1. Simulate the tmux-global auto-read hook firing as the user
	//    navigates through a handful of panes. None of these are
	//    agentic — they must NOT appear on the dashboard.
	for _, paneID := range []string{"%10", "%11", "%12", "%13"} {
		if err := runPaneSetStatusWith(tmux, cfg, "tmux-global", []string{paneID, "read"}); err != nil {
			t.Fatalf("tmux-global hook for %s: %v", paneID, err)
		}
	}

	// 2. Simulate an opencode plugin eagerly sending idle on plugin
	//    load for its own pane (%20). This is the "housekeeping" idle
	//    the extension top-of-file contract documents — it must also
	//    not register the pane.
	if err := runPaneSetStatusWith(tmux, cfg, "", []string{"%20", "idle"}); err != nil {
		t.Fatalf("opencode housekeeping idle: %v", err)
	}

	// 3. Simulate a real agent claim: claude hook fires working on its
	//    pane (%7). Pi extension fires needs_attention on its pane (%8).
	//    Both must register and appear on the dashboard.
	if err := runPaneSetStatusWith(tmux, cfg, "", []string{"%7", "working"}); err != nil {
		t.Fatalf("claude working: %v", err)
	}
	if err := runPaneSetStatusWith(tmux, cfg, "", []string{"%8", "needs_attention"}); err != nil {
		t.Fatalf("pi needs_attention: %v", err)
	}

	// Build the dashboard exactly as runDashboard would.
	panes := buildDashboardPanesWithCurrentPane("", "", config.DefaultSortCriteria)

	if len(panes) != 2 {
		t.Fatalf("expected 2 agentic panes on dashboard, got %d: %+v", len(panes), panes)
	}

	got := map[string]ui.AttentionStatus{}
	for _, p := range panes {
		got[p.PaneID] = p.Status
	}
	if s, ok := got["%7"]; !ok || s != ui.AttentionWorking {
		t.Errorf("%%7: got status %v (present=%v), want AttentionWorking", s, ok)
	}
	if s, ok := got["%8"]; !ok || s != ui.AttentionNeedsAttention {
		t.Errorf("%%8: got status %v (present=%v), want AttentionNeedsAttention", s, ok)
	}

	// Extra belt-and-braces: the non-agentic panes must be absent.
	for _, paneID := range []string{"%10", "%11", "%12", "%13", "%20"} {
		if _, ok := got[paneID]; ok {
			t.Errorf("non-agentic pane %s leaked onto dashboard", paneID)
		}
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
		if result[2].Name != "abc (%3, zsh)" {
			t.Errorf("pane[2] name: expected %q, got %q", "abc (%3, zsh)", result[2].Name)
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
		if result[1].Name != "myproject (%9)" {
			t.Errorf("pane[1] name: expected %q, got %q", "myproject (%9)", result[1].Name)
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

func TestSortDashboardPanes(t *testing.T) {
	panes := func() []ui.AttentionPane {
		return []ui.AttentionPane{
			{PaneID: "%1", Session: "alpha", Status: ui.AttentionWorking},
			{PaneID: "%2", Session: "beta", Status: ui.AttentionNeedsAttention},
			{PaneID: "%3", Session: "gamma", Status: ui.AttentionIdle},
		}
	}

	t.Run("default criteria: status, pane_last_visit_at, alphabetical", func(t *testing.T) {
		p := panes()
		lastVisited := map[string]int64{"%1": 100, "%2": 200, "%3": 300}
		sortDashboardPanes(p, lastVisited, nil, config.DefaultSortCriteria)

		// Status groups: idle(%3)=0, working(%1)=1, needs_attention(%2)=2
		if p[0].PaneID != "%3" {
			t.Errorf("pane[0]: expected %%3 (idle), got %s", p[0].PaneID)
		}
		if p[1].PaneID != "%1" {
			t.Errorf("pane[1]: expected %%1 (working), got %s", p[1].PaneID)
		}
		if p[2].PaneID != "%2" {
			t.Errorf("pane[2]: expected %%2 (needs_attention), got %s", p[2].PaneID)
		}
	})

	t.Run("pane_last_visit_at only", func(t *testing.T) {
		p := panes()
		lastVisited := map[string]int64{"%1": 300, "%2": 100, "%3": 200}
		sortDashboardPanes(p, lastVisited, nil, []string{config.SortByPaneLastVisitAt})

		// Ascending by visit time: %2(100), %3(200), %1(300)
		if p[0].PaneID != "%2" {
			t.Errorf("pane[0]: expected %%2 (oldest visit), got %s", p[0].PaneID)
		}
		if p[1].PaneID != "%3" {
			t.Errorf("pane[1]: expected %%3, got %s", p[1].PaneID)
		}
		if p[2].PaneID != "%1" {
			t.Errorf("pane[2]: expected %%1 (newest visit), got %s", p[2].PaneID)
		}
	})

	t.Run("session_last_visit_at only", func(t *testing.T) {
		p := panes()
		sessionActivity := map[string]int64{"alpha": 300, "beta": 100, "gamma": 200}
		sortDashboardPanes(p, nil, sessionActivity, []string{config.SortBySessionLastVisitAt})

		// Ascending by session activity: beta(100), gamma(200), alpha(300)
		if p[0].Session != "beta" {
			t.Errorf("pane[0]: expected beta (oldest), got %s", p[0].Session)
		}
		if p[1].Session != "gamma" {
			t.Errorf("pane[1]: expected gamma, got %s", p[1].Session)
		}
		if p[2].Session != "alpha" {
			t.Errorf("pane[2]: expected alpha (newest), got %s", p[2].Session)
		}
	})

	t.Run("same status, different pane visit times", func(t *testing.T) {
		p := []ui.AttentionPane{
			{PaneID: "%1", Session: "a", Status: ui.AttentionWorking},
			{PaneID: "%2", Session: "b", Status: ui.AttentionWorking},
			{PaneID: "%3", Session: "c", Status: ui.AttentionWorking},
		}
		lastVisited := map[string]int64{"%1": 300, "%2": 100, "%3": 200}
		sortDashboardPanes(p, lastVisited, nil, config.DefaultSortCriteria)

		// All same status, so pane_last_visit_at breaks tie: %2(100), %3(200), %1(300)
		if p[0].PaneID != "%2" {
			t.Errorf("pane[0]: expected %%2, got %s", p[0].PaneID)
		}
		if p[1].PaneID != "%3" {
			t.Errorf("pane[1]: expected %%3, got %s", p[1].PaneID)
		}
		if p[2].PaneID != "%1" {
			t.Errorf("pane[2]: expected %%1, got %s", p[2].PaneID)
		}
	})

	t.Run("alphabetical fallback when no visit data", func(t *testing.T) {
		p := []ui.AttentionPane{
			{PaneID: "%1", Session: "gamma", Status: ui.AttentionIdle},
			{PaneID: "%2", Session: "alpha", Status: ui.AttentionIdle},
			{PaneID: "%3", Session: "beta", Status: ui.AttentionIdle},
		}
		sortDashboardPanes(p, nil, nil, config.DefaultSortCriteria)

		if p[0].Session != "alpha" {
			t.Errorf("pane[0]: expected alpha, got %s", p[0].Session)
		}
		if p[1].Session != "beta" {
			t.Errorf("pane[1]: expected beta, got %s", p[1].Session)
		}
		if p[2].Session != "gamma" {
			t.Errorf("pane[2]: expected gamma, got %s", p[2].Session)
		}
	})
}

func TestPathBase(t *testing.T) {
	tests := []struct {
		name     string
		path     string
		expected string
	}{
		{"simple path", "/home/user/project", "project"},
		{"no slash", "project", "project"},
		{"trailing slash path", "/a/b/c", "c"},
		{"single segment with slash", "/project", "project"},
		{"root", "/", ""},
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

func TestSessionAccessTime(t *testing.T) {
	now := time.Now()

	t.Run("nil history returns 0", func(t *testing.T) {
		result := sessionAccessTime("myproject", nil)
		if result != 0 {
			t.Errorf("expected 0, got %d", result)
		}
	})

	t.Run("exact match on sanitized base name", func(t *testing.T) {
		hist := &history.History{
			Entries: []history.Entry{
				{Path: "/home/user/myproject", LastAccess: now},
			},
		}
		result := sessionAccessTime("myproject", hist)
		if result != now.Unix() {
			t.Errorf("expected %d, got %d", now.Unix(), result)
		}
	})

	t.Run("exact match with dots sanitized", func(t *testing.T) {
		// sanitizeSessionName replaces . with _
		hist := &history.History{
			Entries: []history.Entry{
				{Path: "/home/user/my.project", LastAccess: now},
			},
		}
		// Session name has dots sanitized to underscores
		result := sessionAccessTime("my_project", hist)
		if result != now.Unix() {
			t.Errorf("expected %d, got %d", now.Unix(), result)
		}
	})

	t.Run("partial match on last component of session", func(t *testing.T) {
		// Session is "work/myproject", last component is "myproject"
		hist := &history.History{
			Entries: []history.Entry{
				{Path: "/home/user/myproject", LastAccess: now},
			},
		}
		result := sessionAccessTime("work/myproject", hist)
		if result != now.Unix() {
			t.Errorf("expected %d (partial match), got %d", now.Unix(), result)
		}
	})

	t.Run("exact match takes priority over partial", func(t *testing.T) {
		earlier := now.Add(-1 * time.Hour)
		// sanitizeSessionName(pathBase(path)) must equal the full session name for an exact match.
		// For session "work/myproject", an exact match would need a path whose
		// sanitized base is "work/myproject" — which can't happen since pathBase strips parents.
		// So test exact-vs-partial with a simple session name instead:
		// Entries are scanned in order; exact match on first entry returns immediately.
		hist := &history.History{
			Entries: []history.Entry{
				{Path: "/other/myproject", LastAccess: earlier}, // exact match (pathBase = "myproject")
				{Path: "/home/myproject", LastAccess: now},      // also exact match, but won't be reached
			},
		}
		result := sessionAccessTime("myproject", hist)
		// Should return the first exact match
		if result != earlier.Unix() {
			t.Errorf("expected %d (first exact match), got %d", earlier.Unix(), result)
		}
	})

	t.Run("partial match used when no exact match", func(t *testing.T) {
		// Session is "work/myproject" — no path will produce this as sanitizedBase.
		// But lastComponent is "myproject", so partial matching kicks in.
		earlier := now.Add(-1 * time.Hour)
		hist := &history.History{
			Entries: []history.Entry{
				{Path: "/other/different", LastAccess: now},       // no match
				{Path: "/home/myproject", LastAccess: earlier},    // partial match on lastComponent
			},
		}
		result := sessionAccessTime("work/myproject", hist)
		if result != earlier.Unix() {
			t.Errorf("expected %d (partial match), got %d", earlier.Unix(), result)
		}
	})

	t.Run("no match returns 0", func(t *testing.T) {
		hist := &history.History{
			Entries: []history.Entry{
				{Path: "/home/user/other", LastAccess: now},
			},
		}
		result := sessionAccessTime("myproject", hist)
		if result != 0 {
			t.Errorf("expected 0, got %d", result)
		}
	})
}
