package dashboard

import (
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/glebglazov/pop/tasks/drain"
	"github.com/glebglazov/pop/ui"

	tea "charm.land/bubbletea/v2"
	"github.com/glebglazov/pop/config"
	"github.com/glebglazov/pop/tasks"
	"github.com/glebglazov/pop/work"
	"github.com/glebglazov/pop/work/ref"
)

// TestCopyMenuOpensOverATaskSetRow covers `y` on a task-set row: it opens the
// copy menu rather than copying the name outright, `n` inside it copies the bare
// set identifier through the injected clipboard helper, and the confirmation lands
// on the hint line (ADR-0236 decision 6).
func TestCopyMenuOpensOverATaskSetRow(t *testing.T) {
	row := DashboardRow{
		Project: "pop", CursorKey: "pop\x00my-set",
		RawStatus: tasks.StatusReady, ID: "my-set", DefPath: "/repo/tasks", StatePath: "/repo/state.json",
	}
	m := newQueueDashboard(&drain.Deps{}, &config.Config{}, DashboardSnapshot{Containers: []DashboardRow{row}})
	m.width, m.height = 120, 24

	var captured string
	callCount := 0
	m.copyFunc = func(s string) error {
		callCount++
		captured = s
		return nil
	}

	updated, cmd := m.update(tea.KeyPressMsg{Code: 'y', Text: "y"})
	got := updated.(QueueDashboard)
	if cmd != nil {
		t.Fatal("opening the copy menu should not schedule a command")
	}
	if got.menu == nil || got.menu.copy == nil {
		t.Fatalf("y did not open the copy menu: %+v", got.menu)
	}
	if callCount != 0 {
		t.Fatalf("y wrote the clipboard %d times before anything was chosen", callCount)
	}
	// An unbound set offers its name and its own folder, and no worktree path.
	if keys := copyMenuKeys(got); !reflect.DeepEqual(keys, []string{"n", "y"}) {
		t.Fatalf("unbound copy menu keys = %v, want name and set folder", keys)
	}

	updated, cmd = got.update(tea.KeyPressMsg{Code: 'n', Text: "n"})
	got = updated.(QueueDashboard)
	if cmd != nil {
		t.Fatal("copying a name should not schedule a command")
	}
	if got.menu != nil {
		t.Fatal("choosing an entry must close the copy menu")
	}
	if callCount != 1 || captured != "my-set" {
		t.Fatalf("copyFunc called %d times with %q, want my-set", callCount, captured)
	}
	if got.flash.Text() != "copied my-set" {
		t.Fatalf("flash = %q, want copied confirmation", got.flash.Text())
	}
}

// TestCopyMenuYYCopiesTheSetsOwnFolder is the new capability: `y` `y` puts the
// set-definition folder on the clipboard, which nothing could copy before, and a
// bound set offers its worktree beside it on `w`.
func TestCopyMenuYYCopiesTheSetsOwnFolder(t *testing.T) {
	row := DashboardRow{
		Project: "pop", CursorKey: "pop\x00my-set",
		RawStatus: tasks.StatusReady, ID: "my-set", DefPath: "/repo/tasks", StatePath: "/repo/state.json",
		Bound: true, RuntimePath: "/repo/worktrees/my-set",
	}
	m := newQueueDashboard(&drain.Deps{}, &config.Config{}, DashboardSnapshot{Containers: []DashboardRow{row}})
	m.width, m.height = 120, 24
	var captured string
	m.copyFunc = func(s string) error { captured = s; return nil }

	updated, _ := m.update(tea.KeyPressMsg{Code: 'y', Text: "y"})
	got := updated.(QueueDashboard)
	if keys := copyMenuKeys(got); !reflect.DeepEqual(keys, []string{"n", "y", "w"}) {
		t.Fatalf("bound copy menu keys = %v, want name, set folder and worktree", keys)
	}
	// The menu block names the menu and the row it is about to copy from.
	if view := ui.StripANSI(got.View().Content); !strings.Contains(view, "copy · my-set") {
		t.Fatalf("copy menu block missing its rule:\n%s", view)
	}

	got = pressKeys(t, got, "y")
	if captured != "/repo/tasks/my-set" {
		t.Fatalf("y y copied %q, want the set's own definition folder", captured)
	}
	if got.menu != nil {
		t.Fatal("choosing an entry must close the copy menu")
	}

	// And `w` is the worktree it is bound to, the payload the old `a` `p` had.
	got = pressKeys(t, got, "y", "w")
	if captured != "/repo/worktrees/my-set" {
		t.Fatalf("y w copied %q, want the bound worktree path", captured)
	}
}

// TestCopyMenuOnAMapRow covers the Map's copy menu: its name and its map folder.
// The folder resolves against the store the row's definition path names, so a row
// carrying none reports the refusal rather than copying a bare id.
func TestCopyMenuOnAMapRow(t *testing.T) {
	row := DashboardRow{
		Project: "pop", Kind: ref.KindMap, CursorKey: "pop\x00map\x00demo-map",
		ID: "demo-map", MapOpen: 1, MapFrontier: 1,
	}
	m := newQueueDashboard(&drain.Deps{}, &config.Config{}, DashboardSnapshot{Containers: []DashboardRow{row}})
	m.width, m.height = 120, 24

	var captured string
	m.copyFunc = func(s string) error { captured = s; return nil }

	updated, _ := m.update(tea.KeyPressMsg{Code: 'y', Text: "y"})
	got := updated.(QueueDashboard)
	if keys := copyMenuKeys(got); !reflect.DeepEqual(keys, []string{"n", "y"}) {
		t.Fatalf("map copy menu keys = %v, want name and map folder", keys)
	}
	updated, _ = got.update(tea.KeyPressMsg{Code: 'n', Text: "n"})
	got = updated.(QueueDashboard)
	if captured != "demo-map" {
		t.Fatalf("copyFunc captured %q, want demo-map", captured)
	}
	if got.flash.Text() != "copied demo-map" {
		t.Fatalf("flash = %q, want copied confirmation", got.flash.Text())
	}
}

// TestCopyMenuEscReturnsToTheRowList pins the way out: the menu opened from the
// row list, so esc closes to it and nothing was copied.
func TestCopyMenuEscReturnsToTheRowList(t *testing.T) {
	row := DashboardRow{
		Project: "pop", CursorKey: "pop\x00esc-set",
		RawStatus: tasks.StatusReady, ID: "esc-set", DefPath: "/repo/tasks",
	}
	m := newQueueDashboard(&drain.Deps{}, &config.Config{}, DashboardSnapshot{Containers: []DashboardRow{row}})
	m.width, m.height = 120, 24
	calls := 0
	m.copyFunc = func(string) error { calls++; return nil }

	updated, _ := m.update(tea.KeyPressMsg{Code: 'y', Text: "y"})
	updated, _ = updated.(QueueDashboard).update(tea.KeyPressMsg{Code: tea.KeyEscape})
	got := updated.(QueueDashboard)
	if got.menu != nil {
		t.Fatal("esc did not close the copy menu")
	}
	if calls != 0 {
		t.Fatalf("esc copied %d times, want none", calls)
	}
}

// TestActionMenuHasNoCopyEntry pins decision 6's other half: the Run menu lost
// both copies on every wired kind, so no verb it offers is one the copy menu owns.
func TestActionMenuHasNoCopyEntry(t *testing.T) {
	rows := []DashboardRow{
		{Project: "pop", ID: "2026-07-01-set", RawStatus: tasks.StatusReady, Bound: true, RuntimePath: "/repo/wt", DefPath: "/repo/tasks"},
		{Project: "pop", Kind: ref.KindMap, ID: "2026-07-02-chart", MapOpen: 1, MapFrontier: 1},
		{Project: "pop", Kind: ref.KindRoutine, ID: "alpha", RoutineLastReport: "/data/report.md"},
	}
	for _, row := range rows {
		kinds := testKinds()
		if row.Kind == ref.KindRoutine {
			kinds = testRoutineKinds()
		}
		copies := kinds.copyActionsFor(row)
		if len(copies) == 0 {
			t.Fatalf("%s offers nothing to copy", row.Kind)
		}
		for _, item := range dashboardMenuItems(kinds, row) {
			if item.verb == work.VerbCopy || item.verb == work.VerbCopyName {
				t.Fatalf("%s run menu still offers %s", row.Kind, item.verb)
			}
			for _, copyAction := range copies {
				if item.verb == copyAction.Verb {
					t.Fatalf("%s run menu still offers the copy verb %s", row.Kind, item.verb)
				}
			}
		}
	}
}

// TestQueueDashboardCopyErrorSurfaces confirms a failing clipboard write is
// surfaced in the status line rather than crashing the dashboard.
func TestQueueDashboardCopyErrorSurfaces(t *testing.T) {
	row := DashboardRow{
		Project: "pop", CursorKey: "pop\x00fail-set",
		RawStatus: tasks.StatusReady, ID: "fail-set",
	}
	m := newQueueDashboard(&drain.Deps{}, &config.Config{}, DashboardSnapshot{Containers: []DashboardRow{row}})
	m.copyFunc = func(string) error { return errors.New("boom") }

	updated, _ := m.update(tea.KeyPressMsg{Code: 'y', Text: "y"})
	updated, cmd := updated.(QueueDashboard).update(tea.KeyPressMsg{Code: 'n', Text: "n"})
	if cmd != nil {
		t.Fatal("y n should not schedule a command on copy failure")
	}
	got := updated.(QueueDashboard)
	if got.flash.Text() != "copy failed: boom" {
		t.Fatalf("flash = %q, want copy failed message", got.flash.Text())
	}
}

// TestQueueDashboardCopyHintAdvertisesY confirms the main hint bar advertises the
// copy menu as the menu it now is.
func TestQueueDashboardCopyHintAdvertisesY(t *testing.T) {
	row := DashboardRow{
		Project: "pop", CursorKey: "pop\x00hint-set",
		RawStatus: tasks.StatusReady, ID: "hint-set",
	}
	m := newQueueDashboard(&drain.Deps{}, &config.Config{}, DashboardSnapshot{Containers: []DashboardRow{row}})
	m.width, m.height = 120, 24
	hint := m.mainHint()
	if !strings.Contains(hint, "y copy ▸") {
		t.Fatalf("mainHint = %q, want the copy menu advertised", hint)
	}
}

// copyMenuKeys is the open copy menu's entry keys, which is what a menu keyed for
// the fingers has to be pinned on.
func copyMenuKeys(m QueueDashboard) []string {
	if m.menu == nil || m.menu.copy == nil {
		return nil
	}
	var keys []string
	for _, action := range m.menu.copy.list.Items() {
		keys = append(keys, action.Key)
	}
	return keys
}

// detailCopyModel builds a QueueDashboard with a loaded task-set detail view.
func detailCopyModel(setID string, task tasks.Task) QueueDashboard {
	row := DashboardRow{ID: setID, DefPath: "/def"}
	manifest := &tasks.Manifest{Valid: true, Tasks: []tasks.Task{task}}
	m := newQueueDashboard(&drain.Deps{}, &config.Config{}, DashboardSnapshot{Containers: []DashboardRow{row}})
	m.width, m.height = 120, 24
	dv := newTaskDetailView(row, manifest, nil)
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

	updated, cmd := m.update(tea.KeyPressMsg{Code: 'y', Text: "y"})
	if cmd != nil {
		t.Fatal("y should not schedule a command")
	}
	got := updated.(QueueDashboard)
	if captured != "my-set/01-a.md" {
		t.Fatalf("copyFunc captured %q, want my-set/01-a.md", captured)
	}
	if got.detail.flash.Text() != "copied my-set/01-a.md" {
		t.Fatalf("flash = %q, want copied confirmation", got.detail.flash.Text())
	}
}

// TestQueueDashboardCopyDetailTaskViaMenu confirms copy name is reachable from
// the task run menu in the detail view.
func TestQueueDashboardCopyDetailTaskViaMenu(t *testing.T) {
	task := tasks.Task{ID: "01-a", File: "01-a.md", Status: "open"}
	m := detailCopyModel("set-menu", task)

	if !menuHasItemKey(m.kinds.itemActionsFor(m.detail.row, m.detail.row.Items[0]), "y") {
		t.Fatal("task item menu missing copy name bound to y")
	}

	var captured string
	m.copyFunc = func(s string) error { captured = s; return nil }

	updated, _ := m.update(tea.KeyPressMsg{Code: 'r', Text: "r"})
	got := updated.(QueueDashboard)
	updated, cmd := got.update(tea.KeyPressMsg{Code: 'y', Text: "y"})
	if cmd != nil {
		t.Fatal("y in task menu should not schedule a command")
	}
	got = updated.(QueueDashboard)
	if got.itemMenu != nil {
		t.Fatal("y in task menu should close the menu")
	}
	if captured != "set-menu/01-a.md" {
		t.Fatalf("copyFunc captured %q, want set-menu/01-a.md", captured)
	}
	if got.detail.flash.Text() != "copied set-menu/01-a.md" {
		t.Fatalf("flash = %q, want copied confirmation", got.detail.flash.Text())
	}
}

func TestQueueDashboardCopyDetailTaskPath(t *testing.T) {
	task := tasks.Task{ID: "01-a", File: "/repo/tasks/my-set/01-a.md", Status: "open"}
	m := detailCopyModel("my-set", task)

	if !menuHasItemKey(m.kinds.itemActionsFor(m.detail.row, m.detail.row.Items[0]), "p") {
		t.Fatal("task item menu missing copy path bound to p")
	}
	var captured string
	m.copyFunc = func(s string) error { captured = s; return nil }

	updated, cmd := m.update(tea.KeyPressMsg{Code: 'p', Text: "p"})
	if cmd == nil {
		t.Fatal("p did not dispatch through the kind command seam")
	}
	updated, _ = updated.(QueueDashboard).update(cmd())
	got := updated.(QueueDashboard)
	if captured != task.File {
		t.Fatalf("copyFunc captured %q, want %q", captured, task.File)
	}
	if got.detail.flash.Text() != "copied "+task.File {
		t.Fatalf("flash = %q, want copied confirmation", got.detail.flash.Text())
	}

	m = detailCopyModel("my-set", task)
	m.copyFunc = func(s string) error { captured = s; return nil }
	updated, _ = m.update(tea.KeyPressMsg{Code: 'r', Text: "r"})
	got = updated.(QueueDashboard)
	updated, cmd = got.update(tea.KeyPressMsg{Code: 'p', Text: "p"})
	if cmd == nil {
		t.Fatal("p in task menu did not dispatch through the kind command seam")
	}
	updated, _ = updated.(QueueDashboard).update(cmd())
	got = updated.(QueueDashboard)
	if captured != task.File || got.detail.flash.Text() != "copied "+task.File {
		t.Fatalf("task menu copy path = %q, flash = %q", captured, got.detail.flash.Text())
	}
}

// TestQueueDashboardCopyPeekTask covers the `y` verb inside the Document peek.
func TestQueueDashboardCopyPeekTask(t *testing.T) {
	task := tasks.Task{ID: "02-b", File: "02-b.md", Status: "open"}
	m := detailCopyModel("set-peek", task)
	m.detail.peek = &documentPeek{itemID: "02-b", text: "body\n"}

	var captured string
	m.copyFunc = func(s string) error { captured = s; return nil }

	updated, cmd := m.update(tea.KeyPressMsg{Code: 'y', Text: "y"})
	if cmd != nil {
		t.Fatal("y should not schedule a command")
	}
	got := updated.(QueueDashboard)
	if captured != "set-peek/02-b.md" {
		t.Fatalf("copyFunc captured %q, want set-peek/02-b.md", captured)
	}
	if got.detail.peek.flash.Text() != "copied set-peek/02-b.md" {
		t.Fatalf("peek flash = %q, want copied confirmation", got.detail.peek.flash.Text())
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
	m.detail.peek = &documentPeek{itemID: "02-b", text: "body\n"}

	var captured string
	m.copyFunc = func(s string) error { captured = s; return nil }

	updated, _ := m.update(tea.KeyPressMsg{Code: 'r', Text: "r"})
	got := updated.(QueueDashboard)
	updated, cmd := got.update(tea.KeyPressMsg{Code: 'y', Text: "y"})
	if cmd != nil {
		t.Fatal("y in peek menu should not schedule a command")
	}
	got = updated.(QueueDashboard)
	if captured != "set-peek-menu/02-b.md" {
		t.Fatalf("copyFunc captured %q, want set-peek-menu/02-b.md", captured)
	}
	if got.detail.peek.flash.Text() != "copied set-peek-menu/02-b.md" {
		t.Fatalf("peek flash = %q, want copied confirmation", got.detail.peek.flash.Text())
	}
}

func TestQueueDashboardCopyPeekTaskPath(t *testing.T) {
	task := tasks.Task{ID: "02-b", File: "/repo/tasks/set-peek/02-b.md", Status: "open"}
	m := detailCopyModel("set-peek", task)
	m.detail.peek = &documentPeek{itemID: "02-b", text: "body\n"}

	var captured string
	m.copyFunc = func(s string) error { captured = s; return nil }
	updated, cmd := m.update(tea.KeyPressMsg{Code: 'p', Text: "p"})
	if cmd == nil {
		t.Fatal("p did not dispatch through the kind command seam")
	}
	updated, _ = updated.(QueueDashboard).update(cmd())
	got := updated.(QueueDashboard)
	if captured != task.File {
		t.Fatalf("copyFunc captured %q, want %q", captured, task.File)
	}
	if got.detail.peek.flash.Text() != "copied "+task.File || got.detail.flash.Text() != "" {
		t.Fatalf("flash landed on wrong surface: peek=%q detail=%q", got.detail.peek.flash.Text(), got.detail.flash.Text())
	}
}

// TestQueueDashboardCopyMapDetailTicket covers the `y` verb on a Map detail
// ticket list: the bare ticket id is copied.
func TestQueueDashboardCopyMapDetailTicket(t *testing.T) {
	m, _ := newMapDetailDashboard(t)
	got := openMapDetail(t, m)

	var captured string
	got.copyFunc = func(s string) error { captured = s; return nil }

	updated, cmd := got.update(tea.KeyPressMsg{Code: 'y', Text: "y"})
	if cmd != nil {
		t.Fatal("y should not schedule a command")
	}
	result := updated.(QueueDashboard)
	if captured != "01" {
		t.Fatalf("copyFunc captured %q, want bare ticket id 01", captured)
	}
	if result.detail.flash.Text() != "copied 01" {
		t.Fatalf("flash = %q, want copied confirmation", result.detail.flash.Text())
	}
}

// TestQueueDashboardCopyMapPeekTicket covers the `y` verb inside a Map ticket
// text peek: the bare ticket id is copied.
func TestQueueDashboardCopyMapPeekTicket(t *testing.T) {
	m, _ := newMapDetailDashboard(t)
	got := openMapDetail(t, m)
	got.detail.peek = &documentPeek{itemID: "01", text: "ticket body\n"}

	var captured string
	got.copyFunc = func(s string) error { captured = s; return nil }

	updated, cmd := got.update(tea.KeyPressMsg{Code: 'y', Text: "y"})
	if cmd != nil {
		t.Fatal("y should not schedule a command")
	}
	result := updated.(QueueDashboard)
	if captured != "01" {
		t.Fatalf("copyFunc captured %q, want bare ticket id 01", captured)
	}
	if result.detail.peek.flash.Text() != "copied 01" {
		t.Fatalf("peek flash = %q, want copied confirmation", result.detail.peek.flash.Text())
	}
}

func menuHasItemKey(actions []work.Action, key string) bool {
	for _, action := range actions {
		if action.Key == key {
			return true
		}
	}
	return false
}
