package queue

import (
	"errors"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/glebglazov/pop/config"
	"github.com/glebglazov/pop/tasks"
	"github.com/glebglazov/pop/work/ref"
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
		Project: "pop", Kind: ref.KindMap, CursorKey: "pop\x00map\x00demo-map",
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
		if !menuHasKey(newDashboardMenu(testKinds(), row, false), "y") {
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
			Project: "pop", Kind: ref.KindMap, CursorKey: "pop\x00map\x00map-menu",
			SetRef: SetRef{SetID: "map-menu"}, MapOpen: 1, MapFrontier: 1,
		}
		m := newQueueDashboard(&Deps{}, &config.Config{}, DashboardSnapshot{Rows: []DashboardRow{row}})
		m.width, m.height = 120, 24
		items := dashboardMenuItems(testKinds(), row)
		if last := items[len(items)-1]; last.label != "copy name" || last.key != "y" {
			t.Fatalf("map menu items = %v, want copy name last on y", items)
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

// detailCopyModel builds a QueueDashboard with a loaded task-set detail view.
func detailCopyModel(setID string, task tasks.Task) QueueDashboard {
	row := DashboardRow{SetRef: SetRef{SetID: setID, DefPath: "/def"}}
	manifest := &tasks.Manifest{Valid: true, Tasks: []tasks.Task{task}}
	m := newQueueDashboard(&Deps{}, &config.Config{}, DashboardSnapshot{Rows: []DashboardRow{row}})
	m.width, m.height = 120, 24
	dv := newDetailView(row)
	dv.syncManifest(manifest, nil)
	m.detail = dv
	return m
}

// TestQueueDashboardCopyDetailTask covers the `y` verb in the task-set detail
// view: the cursored task's <task-set>/<file>.md reference is copied.
func TestQueueDashboardCopyDetailTask(t *testing.T) {
	task := tasks.Task{ID: "01-a", File: "01-a.md", Status: "open"}
	m := detailCopyModel("my-set", task)

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
	if captured != "my-set/01-a.md" {
		t.Fatalf("copyFunc captured %q, want my-set/01-a.md", captured)
	}
	if got.detail.statusMsg != "copied my-set/01-a.md" {
		t.Fatalf("statusMsg = %q, want copied confirmation", got.detail.statusMsg)
	}
}

// TestQueueDashboardCopyDetailTaskViaMenu confirms copy name is reachable from
// the task action menu in the detail view.
func TestQueueDashboardCopyDetailTaskViaMenu(t *testing.T) {
	task := tasks.Task{ID: "01-a", File: "01-a.md", Status: "open"}
	m := detailCopyModel("set-menu", task)

	items := taskMenuItems(task)
	if !menuHasTaskKey(items, "y") {
		t.Fatal("task menu missing copy name bound to y")
	}

	var captured string
	m.copyFunc = func(s string) error { captured = s; return nil }

	updated, _ := m.Update(tea.KeyPressMsg{Code: 'a', Text: "a"})
	got := updated.(QueueDashboard)
	updated, cmd := got.Update(tea.KeyPressMsg{Code: 'y', Text: "y"})
	if cmd != nil {
		t.Fatal("y in task menu should not schedule a command")
	}
	got = updated.(QueueDashboard)
	if got.taskMenu != nil {
		t.Fatal("y in task menu should close the menu")
	}
	if captured != "set-menu/01-a.md" {
		t.Fatalf("copyFunc captured %q, want set-menu/01-a.md", captured)
	}
	if got.detail.statusMsg != "copied set-menu/01-a.md" {
		t.Fatalf("statusMsg = %q, want copied confirmation", got.detail.statusMsg)
	}
}

// TestQueueDashboardCopyPeekTask covers the `y` verb inside the task text peek.
func TestQueueDashboardCopyPeekTask(t *testing.T) {
	task := tasks.Task{ID: "02-b", File: "02-b.md", Status: "open"}
	m := detailCopyModel("set-peek", task)
	m.detail.peek = &taskTextPeek{taskID: "02-b", text: "body\n"}

	var captured string
	m.copyFunc = func(s string) error { captured = s; return nil }

	updated, cmd := m.Update(tea.KeyPressMsg{Code: 'y', Text: "y"})
	if cmd != nil {
		t.Fatal("y should not schedule a command")
	}
	got := updated.(QueueDashboard)
	if captured != "set-peek/02-b.md" {
		t.Fatalf("copyFunc captured %q, want set-peek/02-b.md", captured)
	}
	if got.detail.peek.statusMsg != "copied set-peek/02-b.md" {
		t.Fatalf("peek statusMsg = %q, want copied confirmation", got.detail.peek.statusMsg)
	}
	view := got.View().Content
	if !strings.Contains(view, "copied set-peek/02-b.md") {
		t.Fatalf("peek view missing status line:\n%s", view)
	}
}

// TestQueueDashboardCopyPeekTaskViaMenu confirms copy name from the peek menu.
func TestQueueDashboardCopyPeekTaskViaMenu(t *testing.T) {
	task := tasks.Task{ID: "02-b", File: "02-b.md", Status: "failed"}
	m := detailCopyModel("set-peek-menu", task)
	m.detail.peek = &taskTextPeek{taskID: "02-b", text: "body\n"}

	var captured string
	m.copyFunc = func(s string) error { captured = s; return nil }

	updated, _ := m.Update(tea.KeyPressMsg{Code: 'a', Text: "a"})
	got := updated.(QueueDashboard)
	updated, cmd := got.Update(tea.KeyPressMsg{Code: 'y', Text: "y"})
	if cmd != nil {
		t.Fatal("y in peek menu should not schedule a command")
	}
	got = updated.(QueueDashboard)
	if captured != "set-peek-menu/02-b.md" {
		t.Fatalf("copyFunc captured %q, want set-peek-menu/02-b.md", captured)
	}
	if got.detail.peek.statusMsg != "copied set-peek-menu/02-b.md" {
		t.Fatalf("peek statusMsg = %q, want copied confirmation", got.detail.peek.statusMsg)
	}
}

// TestQueueDashboardCopyMapDetailTicket covers the `y` verb on a Map detail
// ticket list: the bare ticket id is copied.
func TestQueueDashboardCopyMapDetailTicket(t *testing.T) {
	m, _ := newMapDetailDashboard(t)
	got := loadMapDetail(t, m)

	var captured string
	got.copyFunc = func(s string) error { captured = s; return nil }

	updated, cmd := got.Update(tea.KeyPressMsg{Code: 'y', Text: "y"})
	if cmd != nil {
		t.Fatal("y should not schedule a command")
	}
	result := updated.(QueueDashboard)
	if captured != "01" {
		t.Fatalf("copyFunc captured %q, want bare ticket id 01", captured)
	}
	if result.detail.statusMsg != "copied 01" {
		t.Fatalf("statusMsg = %q, want copied confirmation", result.detail.statusMsg)
	}
}

// TestQueueDashboardCopyMapPeekTicket covers the `y` verb inside a Map ticket
// text peek: the bare ticket id is copied.
func TestQueueDashboardCopyMapPeekTicket(t *testing.T) {
	m, _ := newMapDetailDashboard(t)
	got := loadMapDetail(t, m)
	got.detail.peek = &taskTextPeek{taskID: "01-frontier", text: "ticket body\n"}

	var captured string
	got.copyFunc = func(s string) error { captured = s; return nil }

	updated, cmd := got.Update(tea.KeyPressMsg{Code: 'y', Text: "y"})
	if cmd != nil {
		t.Fatal("y should not schedule a command")
	}
	result := updated.(QueueDashboard)
	if captured != "01" {
		t.Fatalf("copyFunc captured %q, want bare ticket id 01", captured)
	}
	if result.detail.peek.statusMsg != "copied 01" {
		t.Fatalf("peek statusMsg = %q, want copied confirmation", result.detail.peek.statusMsg)
	}
}

func menuHasTaskKey(items []taskMenuItem, key string) bool {
	for _, item := range items {
		if item.key == key {
			return true
		}
	}
	return false
}
