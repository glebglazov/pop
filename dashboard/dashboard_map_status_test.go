package dashboard

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/glebglazov/pop/config"
	"github.com/glebglazov/pop/internal/queuetest"
	"github.com/glebglazov/pop/tasks/drain"
	"github.com/glebglazov/pop/wayfinder"
	"github.com/glebglazov/pop/work/ref"
)

// mapStatusRow is a Map row whose repository does not exist, which is all these
// tests need: the point is which verb the surface dispatches and where its result
// lands, not what the writer does with it (the writers are pinned kind-side).
func mapStatusRow() DashboardRow {
	return TestDashboardRow("pop", "2026-08-01-chart", DashboardRow{
		Kind:     ref.KindMap,
		ID:       "2026-08-01-chart",
		DefPath:  "/repo/tasks",
		Checkout: "/repo/main",
	})
}

// The Map's status submenu opens on the same key the Task set's does and offers
// the Map's own four verbs, each dispatched to the Map kind rather than to any
// dashboard-side status code.
func TestMapRowStatusSubmenuDispatchesTheMapsOwnVerbs(t *testing.T) {
	row := mapStatusRow()
	for _, tc := range []struct {
		key  rune
		verb string
	}{
		{'o', string(wayfinder.VerbReopen)},
		{'a', string(wayfinder.VerbAbandon)},
		{'x', string(wayfinder.VerbArchive)},
		{'u', string(wayfinder.VerbUnarchive)},
	} {
		m := newQueueDashboard(&drain.Deps{Tasks: queuetest.DataDeps(t)}, &config.Config{}, DashboardSnapshot{Containers: []DashboardRow{row}})
		m.width, m.height = 120, 20

		updated, _ := m.Update(tea.KeyPressMsg{Code: 'a', Text: "a"})
		got := updated.(QueueDashboard)
		updated, _ = got.Update(tea.KeyPressMsg{Code: 's', Text: "s"})
		got = updated.(QueueDashboard)
		if got.menu == nil || got.menu.status == nil {
			t.Fatalf("s on a map row did not open the status submenu")
		}
		// The submenu names the Map's vocabulary on screen, not a task set's.
		if view := got.View().Content; !strings.Contains(view, "abandon") || strings.Contains(view, "skip") {
			t.Fatalf("map status submenu view = \n%s", view)
		}

		updated, cmd := got.Update(tea.KeyPressMsg{Code: tc.key, Text: string(tc.key)})
		got = updated.(QueueDashboard)
		if got.menu != nil {
			t.Fatalf("%c did not close the menus", tc.key)
		}
		if cmd == nil {
			t.Fatalf("%c dispatched no command", tc.key)
		}
		msg, ok := cmd().(dashboardKindVerbMsg)
		if !ok {
			t.Fatalf("%c produced %T, want the kind-verb message every row verb uses", tc.key, cmd())
		}
		if string(msg.verb) != tc.verb {
			t.Fatalf("%c dispatched %q, want %q", tc.key, msg.verb, tc.verb)
		}
	}
}

// A refused status write — archiving a Map that was never registered is the
// standing case — lands on the dashboard's sticky action-error line with the
// writer's own corrective, rather than disappearing.
func TestMapRowStatusRefusalSurfacesTheCorrective(t *testing.T) {
	row := mapStatusRow()
	m := newQueueDashboard(&drain.Deps{Tasks: queuetest.DataDeps(t)}, &config.Config{}, DashboardSnapshot{Containers: []DashboardRow{row}})
	m.width, m.height = 120, 20

	cmd := m.performKindVerb(row, wayfinder.VerbArchive)
	msg, ok := cmd().(dashboardKindVerbMsg)
	if !ok {
		t.Fatalf("archive produced %T", cmd())
	}
	if msg.err == nil {
		t.Fatal("archiving a map with no readable storage succeeded")
	}
	updated, _ := m.Update(msg)
	got := updated.(QueueDashboard)
	if got.actionErr == nil {
		t.Fatal("refused status write was swallowed")
	}
	if view := got.View().Content; !strings.Contains(view, got.actionErr.Error()) {
		t.Fatalf("view does not show the refusal:\n%s", view)
	}
}

// The show-archived toggle sits beside show done and behaves like it: off at
// launch, flipped by its own letter, rebuilding the view, and carried nowhere —
// a fresh model starts off again.
func TestDashboardFilterMenuShowArchivedToggle(t *testing.T) {
	m := filterMenuTestModel()
	if m.filterToggleOn(filterToggleShowArchived) {
		t.Fatal("show archived must start off")
	}

	updated, _ := m.Update(tea.KeyPressMsg{Code: 'f', Text: "f"})
	m = updated.(QueueDashboard)
	view := m.View().Content
	for _, want := range []string{"show done", "show archived"} {
		if !strings.Contains(view, want) {
			t.Fatalf("filter menu missing %q:\n%s", want, view)
		}
	}

	updated, cmd := m.Update(tea.KeyPressMsg{Code: 'a', Text: "a"})
	m = updated.(QueueDashboard)
	if !m.filterToggleOn(filterToggleShowArchived) {
		t.Fatal("toggling show archived did not engage an archived-admitting preset")
	}
	if m.filterToggleOn(filterToggleShowDone) {
		t.Fatal("show archived flipped the show-done filter too")
	}
	if cmd == nil {
		t.Fatal("toggling show archived must trigger a rebuild")
	}
	if m.filter == nil {
		t.Fatal("toggle should leave the filter menu open")
	}
	if !strings.Contains(m.View().Content, "[x] show archived") {
		t.Fatalf("checkbox not checked after toggle-on:\n%s", m.View().Content)
	}

	updated, _ = m.Update(tea.KeyPressMsg{Code: 'a', Text: "a"})
	m = updated.(QueueDashboard)
	if m.filterToggleOn(filterToggleShowArchived) {
		t.Fatal("second press did not turn show archived back off")
	}

	// Nothing is persisted: the preset lives on the model's deps for the session.
	if fresh := filterMenuTestModel(); fresh.filterToggleOn(filterToggleShowArchived) {
		t.Fatal("a fresh dashboard inherited a show-archived state")
	}
}
