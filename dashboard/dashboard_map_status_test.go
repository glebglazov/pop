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

// The Map's Status menu opens on the same key the Task set's does — `s` from the
// row list — and offers the Map's own four verbs, each dispatched to the Map kind
// rather than to any dashboard-side status code.
func TestMapRowStatusMenuDispatchesTheMapsOwnVerbs(t *testing.T) {
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

		updated, _ := m.Update(tea.KeyPressMsg{Code: 's', Text: "s"})
		got := updated.(QueueDashboard)
		if got.menu == nil || got.menu.status == nil {
			t.Fatalf("s on a map row did not open the status menu")
		}
		// The menu names the Map's vocabulary on screen, not a task set's.
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

// The filter menu is a single-select numbered preset list (ADR-0197): exactly
// one preset is active, digits activate by position, and a fresh model resets
// to the default — the show-archived / show-done toggles are gone.
func TestDashboardFilterMenuSingleSelectPresets(t *testing.T) {
	m := filterMenuTestModel()
	if got := m.activeViewPreset().Name; got != "active" {
		t.Fatalf("launch preset = %q, want active", got)
	}

	updated, _ := m.Update(tea.KeyPressMsg{Code: 'f', Text: "f"})
	m = updated.(QueueDashboard)
	view := m.View().Content
	for _, want := range []string{"active", "unfolded", "all"} {
		if !strings.Contains(view, want) {
			t.Fatalf("filter menu missing %q:\n%s", want, view)
		}
	}
	if strings.Contains(view, "show done") || strings.Contains(view, "show archived") {
		t.Fatalf("retired toggles still present:\n%s", view)
	}

	updated, cmd := m.Update(tea.KeyPressMsg{Code: '2', Text: "2"})
	m = updated.(QueueDashboard)
	if m.d.ViewPreset.Name != "unfolded" {
		t.Fatalf("digit 2 preset = %q, want unfolded", m.d.ViewPreset.Name)
	}
	if cmd == nil {
		t.Fatal("selecting a preset must trigger a rebuild")
	}
	if m.filter == nil {
		t.Fatal("selection should leave the filter menu open")
	}
	if !strings.Contains(m.View().Content, "[x] unfolded") {
		t.Fatalf("active mark not on unfolded:\n%s", m.View().Content)
	}
	if strings.Count(m.View().Content, "[x]") != 1 {
		t.Fatalf("exactly one preset must be marked:\n%s", m.View().Content)
	}

	// Nothing is persisted: the preset lives on the model's deps for the session.
	if fresh := filterMenuTestModel(); fresh.activeViewPreset().Name != "active" {
		t.Fatal("a fresh dashboard inherited a non-default preset")
	}
}
