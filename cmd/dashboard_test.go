package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
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
// surfaces ONLY panes whose foreground process is NOT a plain shell. Agent
// panes (opencode, claude, pi, ...) appear immediately — even when their
// very first status update is idle — while plain zsh/fish/bash panes the
// user merely navigates through stay out of the dashboard.
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

	// Mock tmux knows each pane's session and foreground command.
	tmux := &deps.MockTmux{
		CommandFunc: func(args ...string) (string, error) {
			if len(args) >= 5 && args[0] == "display-message" && args[1] == "-t" {
				paneID := args[2]
				format := args[4]
				paneInfo := map[string]string{
					"%10": "proj-a\tzsh",      // plain shell
					"%11": "proj-a\tfish",     // plain shell
					"%12": "proj-b\tbash",     // plain shell
					"%13": "proj-b\t-zsh",     // login shell
					"%20": "proj-c\topencode", // agent (housekeeping idle)
					"%7":  "proj-d\tclaude",   // agent (claim via working)
					"%8":  "proj-e\tpi",       // agent (claim via unread)
				}
				info, ok := paneInfo[paneID]
				if !ok {
					return "", os.ErrNotExist
				}
				switch format {
				case "#{session_name}\t#{pane_current_command}":
					return info, nil
				case "#{session_name}":
					parts := strings.SplitN(info, "\t", 2)
					return parts[0], nil
				case "#{pane_active} #{window_active} #{session_attached}":
					return "0 0 0", nil
				}
			}
			// list-panes for tmuxPaneCommands in dashboard build
			if len(args) >= 1 && args[0] == "list-panes" {
				return "", nil
			}
			return "", nil
		},
	}
	cfg := &config.Config{}

	// 1. User navigates through 4 plain-shell panes. The tmux-global
	//    auto-read hook fires read/idle for each. None must register.
	for _, paneID := range []string{"%10", "%11", "%12", "%13"} {
		if err := runPaneSetStatusWith(tmux, cfg, "tmux-global", true, []string{paneID, "read"}); err != nil {
			t.Fatalf("tmux-global hook for %s: %v", paneID, err)
		}
	}

	// 2. opencode plugin eagerly sends idle on load for its pane (%20).
	//    Because %20 is running opencode (not a shell), it MUST register
	//    right away as idle — this is the change the user asked for.
	if err := runPaneSetStatusWith(tmux, cfg, "", false, []string{"%20", "idle"}); err != nil {
		t.Fatalf("opencode housekeeping idle: %v", err)
	}

	// 3. Real agent claims: claude fires working on %7, pi fires
	//    unread on %8. Both register.
	if err := runPaneSetStatusWith(tmux, cfg, "", false, []string{"%7", "working"}); err != nil {
		t.Fatalf("claude working: %v", err)
	}
	if err := runPaneSetStatusWith(tmux, cfg, "", false, []string{"%8", "unread"}); err != nil {
		t.Fatalf("pi unread: %v", err)
	}

	// Build the dashboard exactly as runDashboard would.
	panes := buildDashboardPanesWithCurrentPane("", "", config.DefaultSortCriteria)

	if len(panes) != 3 {
		t.Fatalf("expected 3 agentic panes on dashboard, got %d: %+v", len(panes), panes)
	}

	got := map[string]ui.AttentionStatus{}
	for _, p := range panes {
		got[p.PaneID] = p.Status
	}
	if s, ok := got["%20"]; !ok || s != ui.AttentionIdle {
		t.Errorf("%%20 (opencode): got status %v (present=%v), want AttentionIdle", s, ok)
	}
	if s, ok := got["%7"]; !ok || s != ui.AttentionWorking {
		t.Errorf("%%7 (claude): got status %v (present=%v), want AttentionWorking", s, ok)
	}
	if s, ok := got["%8"]; !ok || s != ui.AttentionUnread {
		t.Errorf("%%8 (pi): got status %v (present=%v), want AttentionUnread", s, ok)
	}

	// Plain shell panes must be absent from the dashboard.
	for _, paneID := range []string{"%10", "%11", "%12", "%13"} {
		if _, ok := got[paneID]; ok {
			t.Errorf("plain shell pane %s leaked onto dashboard", paneID)
		}
	}
}

func TestPositionCurrentPane(t *testing.T) {
	t.Run("untracked pane is injected at end", func(t *testing.T) {
		// Scenario: monitored panes are pop (working) and vcdr (unread),
		// current pane is "abc" which is not monitored.
		// After normal sort: pop (working=1), vcdr (unread=2)
		// Expected: pop, vcdr, abc (under cursor at end)
		panes := []ui.AttentionPane{
			{PaneID: "%1", Session: "pop", Name: "pop %1", Status: ui.AttentionWorking},
			{PaneID: "%2", Session: "vcdr", Name: "vcdr %2", Status: ui.AttentionUnread},
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
		// Scenario: monitored panes are pop (working) and vcdr (unread),
		// current pane is pop's pane (%1).
		// After normal sort: pop (working=1), vcdr (unread=2)
		// Expected: vcdr, pop (under cursor at end)
		panes := []ui.AttentionPane{
			{PaneID: "%1", Session: "pop", Name: "pop %1", Status: ui.AttentionWorking},
			{PaneID: "%2", Session: "vcdr", Name: "vcdr %2", Status: ui.AttentionUnread},
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
			{PaneID: "%2", Session: "vcdr", Name: "vcdr %2", Status: ui.AttentionUnread},
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
			{PaneID: "%2", Session: "beta", Status: ui.AttentionUnread},
			{PaneID: "%3", Session: "gamma", Status: ui.AttentionIdle},
		}
	}

	t.Run("default criteria: status, pane_last_visit_at, alphabetical", func(t *testing.T) {
		p := panes()
		lastVisited := map[string]int64{"%1": 100, "%2": 200, "%3": 300}
		sortDashboardPanes(p, lastVisited, nil, config.DefaultSortCriteria)

		// Status groups: idle(%3)=0, working(%1)=1, unread(%2)=2
		if p[0].PaneID != "%3" {
			t.Errorf("pane[0]: expected %%3 (idle), got %s", p[0].PaneID)
		}
		if p[1].PaneID != "%1" {
			t.Errorf("pane[1]: expected %%1 (working), got %s", p[1].PaneID)
		}
		if p[2].PaneID != "%2" {
			t.Errorf("pane[2]: expected %%2 (unread), got %s", p[2].PaneID)
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

// TestHandleDashboardSwitch verifies the contract between the two switch
// actions and the monitor state:
//
//   - Normal switch (dismissUnread=true) MUST flip an unread pane to idle
//     and MUST stamp LastVisited, matching the long-standing Enter behavior.
//   - Peek (dismissUnread=false) MUST leave the monitor state completely
//     untouched — no status flip, no LastVisited update — so the pane
//     continues to appear as unread in the dashboard even after the user
//     has glanced at its content.
//
// Both actions record the session in pop's history for recency tracking in
// `pop select` (a separate store from monitor state).
func TestHandleDashboardSwitch(t *testing.T) {
	setup := func(t *testing.T) (statePath string, initialVisited time.Time) {
		t.Helper()
		dir := t.TempDir()
		t.Setenv("XDG_DATA_HOME", dir)
		t.Setenv("HOME", dir) // ensures history.DefaultHistoryPath stays inside tmp
		stateDir := filepath.Join(dir, "pop")
		if err := os.MkdirAll(stateDir, 0755); err != nil {
			t.Fatal(err)
		}
		statePath = filepath.Join(stateDir, "monitor.json")

		// Write a fake PID file pointing at the current test process so
		// IsDaemonRunningWith (which sends signal 0 to the PID) considers
		// the daemon alive. Without this, dismissUnreadPane silently
		// no-ops and the test can't verify status mutation.
		pidPath := filepath.Join(stateDir, "monitor.pid")
		if err := os.WriteFile(pidPath, []byte(fmt.Sprintf("%d", os.Getpid())), 0644); err != nil {
			t.Fatal(err)
		}

		initialVisited = time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC)
		state := &monitor.State{Panes: map[string]*monitor.PaneEntry{
			"%1": {
				PaneID:      "%1",
				Session:     "proj",
				Status:      monitor.StatusUnread,
				LastVisited: initialVisited,
			},
		}}
		data, _ := json.Marshal(state)
		if err := os.WriteFile(statePath, data, 0644); err != nil {
			t.Fatal(err)
		}
		return statePath, initialVisited
	}

	loadPane := func(t *testing.T, statePath, paneID string) *monitor.PaneEntry {
		t.Helper()
		state, err := monitor.Load(statePath)
		if err != nil {
			t.Fatalf("monitor.Load: %v", err)
		}
		entry, ok := state.Panes[paneID]
		if !ok {
			t.Fatalf("pane %s missing from state", paneID)
		}
		return entry
	}

	t.Run("normal switch dismisses unread and stamps LastVisited", func(t *testing.T) {
		statePath, initialVisited := setup(t)
		result := ui.Result{
			Selected: &ui.Item{Name: "proj (%1)", Path: "%1", Context: "proj"},
			Action:   ui.ActionSwitchToPane,
		}

		got := handleDashboardSwitch(result, true)
		if got != "%1" {
			t.Errorf("return = %q, want %q", got, "%1")
		}

		entry := loadPane(t, statePath, "%1")
		if entry.Status != monitor.StatusIdle {
			t.Errorf("status = %q, want %q (dismiss should have flipped it)", entry.Status, monitor.StatusIdle)
		}
		if !entry.LastVisited.After(initialVisited) {
			t.Errorf("LastVisited = %v, want something after %v", entry.LastVisited, initialVisited)
		}
	})

	t.Run("peek leaves monitor state untouched", func(t *testing.T) {
		statePath, initialVisited := setup(t)
		result := ui.Result{
			Selected: &ui.Item{Name: "proj (%1)", Path: "%1", Context: "proj"},
			Action:   ui.ActionSwitchToPaneKeepUnread,
		}

		got := handleDashboardSwitch(result, false)
		if got != "%1" {
			t.Errorf("return = %q, want %q", got, "%1")
		}

		entry := loadPane(t, statePath, "%1")
		if entry.Status != monitor.StatusUnread {
			t.Errorf("status = %q, want %q (peek must not flip status)", entry.Status, monitor.StatusUnread)
		}
		if !entry.LastVisited.Equal(initialVisited) {
			t.Errorf("LastVisited = %v, want %v (peek must not update LastVisited)", entry.LastVisited, initialVisited)
		}
	})

	t.Run("nil selected returns empty string", func(t *testing.T) {
		setup(t)
		result := ui.Result{Selected: nil, Action: ui.ActionSwitchToPane}
		if got := handleDashboardSwitch(result, true); got != "" {
			t.Errorf("got %q, want empty string", got)
		}
	})
}
