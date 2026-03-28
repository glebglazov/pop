package cmd

import (
	"testing"

	"github.com/glebglazov/pop/config"
	"github.com/glebglazov/pop/ui"
)

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
		if result[2].Name != "abc %3 (zsh)" {
			t.Errorf("pane[2] name: expected %q, got %q", "abc %3 (zsh)", result[2].Name)
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
		if result[1].Name != "myproject %9" {
			t.Errorf("pane[1] name: expected %q, got %q", "myproject %9", result[1].Name)
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

	t.Run("default criteria: status, last_visit_at, alphabetical", func(t *testing.T) {
		p := panes()
		lastVisited := map[string]int64{"%1": 100, "%2": 200, "%3": 300}
		sortDashboardPanes(p, lastVisited, config.DefaultSortCriteria)

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

	t.Run("last_visit_at only", func(t *testing.T) {
		p := panes()
		lastVisited := map[string]int64{"%1": 300, "%2": 100, "%3": 200}
		sortDashboardPanes(p, lastVisited, []string{config.SortByLastVisitAt})

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

	t.Run("same status, different visit times", func(t *testing.T) {
		p := []ui.AttentionPane{
			{PaneID: "%1", Session: "a", Status: ui.AttentionWorking},
			{PaneID: "%2", Session: "b", Status: ui.AttentionWorking},
			{PaneID: "%3", Session: "c", Status: ui.AttentionWorking},
		}
		lastVisited := map[string]int64{"%1": 300, "%2": 100, "%3": 200}
		sortDashboardPanes(p, lastVisited, config.DefaultSortCriteria)

		// All same status, so last_visit_at breaks tie: %2(100), %3(200), %1(300)
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
		sortDashboardPanes(p, nil, config.DefaultSortCriteria)

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
