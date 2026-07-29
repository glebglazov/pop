package queue

import (
	"errors"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/glebglazov/pop/config"
	"github.com/glebglazov/pop/tasks"
)

// TestQueueDashboardCopyTaskSetRow covers the `y` verb on a task-set row: the
// bare set identifier is copied through the injected clipboard helper and
// confirmed on the status line.
func TestQueueDashboardCopyTaskSetRow(t *testing.T) {
	row := DashboardRow{
		Project: "pop", CursorKey: "pop\x00my-set",
		SetRef: SetRef{RawStatus: tasks.StatusReady, SetID: "my-set", DefPath: "/repo/tasks", StatePath: "/repo/state.json"},
	}
	m := newQueueDashboard(&Deps{}, &config.Config{}, DashboardSnapshot{Rows: []DashboardRow{row}})
	m.width, m.height = 120, 24

	var captured string
	callCount := 0
	m.copyFunc = func(s string) error {
		callCount++
		captured = s
		return nil
	}

	updated, cmd := m.Update(tea.KeyPressMsg{Code: 'y', Text: "y"})
	if cmd != nil {
		t.Fatal("y should not schedule a command")
	}
	got := updated.(QueueDashboard)
	if callCount != 1 || captured != "my-set" {
		t.Fatalf("copyFunc called %d times with %q, want my-set", callCount, captured)
	}
	if got.statusMsg != "copied my-set" {
		t.Fatalf("statusMsg = %q, want copied confirmation", got.statusMsg)
	}
}

// TestQueueDashboardCopyMapRow covers the `y` verb on a Wayfinder Map row: the
// map id is copied and confirmed.
func TestQueueDashboardCopyMapRow(t *testing.T) {
	row := DashboardRow{
		Project: "pop", IsMap: true, CursorKey: "pop\x00map\x00demo-map",
		SetRef: SetRef{SetID: "demo-map"}, MapOpen: 1, MapFrontier: 1,
	}
	m := newQueueDashboard(&Deps{}, &config.Config{}, DashboardSnapshot{Rows: []DashboardRow{row}})
	m.width, m.height = 120, 24

	var captured string
	m.copyFunc = func(s string) error {
		captured = s
		return nil
	}

	updated, cmd := m.Update(tea.KeyPressMsg{Code: 'y', Text: "y"})
	if cmd != nil {
		t.Fatal("y should not schedule a command")
	}
	got := updated.(QueueDashboard)
	if captured != "demo-map" {
		t.Fatalf("copyFunc captured %q, want demo-map", captured)
	}
	if got.statusMsg != "copied demo-map" {
		t.Fatalf("statusMsg = %q, want copied confirmation", got.statusMsg)
	}
}

// TestQueueDashboardCopyViaMenu confirms copy name is reachable from the action
// menu on both row kinds and is bound to y.
func TestQueueDashboardCopyViaMenu(t *testing.T) {
	t.Run("task set row", func(t *testing.T) {
		row := DashboardRow{
			Project: "pop", CursorKey: "pop\x00set-menu",
			SetRef: SetRef{RawStatus: tasks.StatusReady, SetID: "set-menu"},
		}
		m := newQueueDashboard(&Deps{}, &config.Config{}, DashboardSnapshot{Rows: []DashboardRow{row}})
		m.width, m.height = 120, 24
		if !menuHasKey(newDashboardMenu(row), "y") {
			t.Fatal("task-set menu missing copy name bound to y")
		}

		var captured string
		m.copyFunc = func(s string) error { captured = s; return nil }

		updated, _ := m.Update(tea.KeyPressMsg{Code: 'a', Text: "a"})
		got := updated.(QueueDashboard)
		updated, cmd := got.Update(tea.KeyPressMsg{Code: 'y', Text: "y"})
		if cmd != nil {
			t.Fatal("y in menu should not schedule a command")
		}
		got = updated.(QueueDashboard)
		if got.menu != nil {
			t.Fatal("y in menu should close the menu")
		}
		if captured != "set-menu" {
			t.Fatalf("copyFunc captured %q, want set-menu", captured)
		}
		if got.statusMsg != "copied set-menu" {
			t.Fatalf("statusMsg = %q, want copied confirmation", got.statusMsg)
		}
	})

	t.Run("map row", func(t *testing.T) {
		row := DashboardRow{
			Project: "pop", IsMap: true, CursorKey: "pop\x00map\x00map-menu",
			SetRef: SetRef{SetID: "map-menu"}, MapOpen: 1, MapFrontier: 1,
		}
		m := newQueueDashboard(&Deps{}, &config.Config{}, DashboardSnapshot{Rows: []DashboardRow{row}})
		m.width, m.height = 120, 24
		items := dashboardMenuItems(row)
		if len(items) != 1 || items[0].label != "copy name" || items[0].key != "y" {
			t.Fatalf("map menu items = %v, want single copy name on y", items)
		}

		var captured string
		m.copyFunc = func(s string) error { captured = s; return nil }

		updated, _ := m.Update(tea.KeyPressMsg{Code: 'a', Text: "a"})
		got := updated.(QueueDashboard)
		if got.menu == nil {
			t.Fatal("a on map row did not open action menu")
		}
		updated, _ = got.Update(tea.KeyPressMsg{Code: 'y', Text: "y"})
		got = updated.(QueueDashboard)
		if captured != "map-menu" {
			t.Fatalf("copyFunc captured %q, want map-menu", captured)
		}
		if got.statusMsg != "copied map-menu" {
			t.Fatalf("statusMsg = %q, want copied confirmation", got.statusMsg)
		}
	})
}

// TestQueueDashboardCopyErrorSurfaces confirms a failing clipboard write is
// surfaced in the status line rather than crashing the dashboard.
func TestQueueDashboardCopyErrorSurfaces(t *testing.T) {
	row := DashboardRow{
		Project: "pop", CursorKey: "pop\x00fail-set",
		SetRef: SetRef{RawStatus: tasks.StatusReady, SetID: "fail-set"},
	}
	m := newQueueDashboard(&Deps{}, &config.Config{}, DashboardSnapshot{Rows: []DashboardRow{row}})
	m.copyFunc = func(string) error { return errors.New("boom") }

	updated, cmd := m.Update(tea.KeyPressMsg{Code: 'y', Text: "y"})
	if cmd != nil {
		t.Fatal("y should not schedule a command on copy failure")
	}
	got := updated.(QueueDashboard)
	if got.statusMsg != "copy failed: boom" {
		t.Fatalf("statusMsg = %q, want copy failed message", got.statusMsg)
	}
}

// TestQueueDashboardCopyHintAdvertisesY confirms the main hint bar advertises
// the copy-name key.
func TestQueueDashboardCopyHintAdvertisesY(t *testing.T) {
	row := DashboardRow{
		Project: "pop", CursorKey: "pop\x00hint-set",
		SetRef: SetRef{RawStatus: tasks.StatusReady, SetID: "hint-set"},
	}
	m := newQueueDashboard(&Deps{}, &config.Config{}, DashboardSnapshot{Rows: []DashboardRow{row}})
	m.width, m.height = 120, 24
	hint := m.mainHint()
	if !strings.Contains(hint, "y copy name") {
		t.Fatalf("mainHint = %q, want y copy name advertised", hint)
	}
}
