package dashboard

import (
	"slices"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/glebglazov/pop/config"
	"github.com/glebglazov/pop/tasks"
	"github.com/glebglazov/pop/tasks/drain"
)

// `a` is retired and silent: it named the set of everything, and the set of
// everything is what the Status, Copy and Mute menus split up, leaving `r` as
// the only opener for what remains, the Run menu (ADR-0236 decisions 2 and 3).
func TestDashboardAOpensNothing(t *testing.T) {
	m := newQueueDashboard(&drain.Deps{}, &config.Config{}, DashboardSnapshot{Containers: []DashboardRow{
		{Project: "pop", CursorKey: "pop\x00set-a", RawStatus: tasks.StatusReady, ID: "set-a", RuntimePath: "/repo/wt-a"},
	}})
	m.width = 120
	m.height = 20

	updated, _ := m.Update(tea.KeyPressMsg{Code: 'a', Text: "a"})
	got := updated.(QueueDashboard)
	if got.menu != nil {
		t.Fatal("a opened a menu")
	}
	if got.flash.Text() != "" {
		t.Fatalf("a produced a message: %q", got.flash.Text())
	}

	updated, _ = got.Update(tea.KeyPressMsg{Code: 'r', Text: "r"})
	if got := updated.(QueueDashboard); got.menu == nil {
		t.Fatal("r did not open the run menu")
	}
}

// A menu does not survive the verb it fires: an in-place write closes it just
// as a handoff does. Archive is the case, and it is reached with `s` `x` now
// that it lives in the Status menu alone (ADR-0236 decision 4).
func TestDashboardMenuClosesOnInPlaceVerb(t *testing.T) {
	d := &drain.Deps{
		SetArchived: func(defPath, setID string, on bool) error { return nil },
	}
	m := newQueueDashboard(d, &config.Config{}, DashboardSnapshot{Containers: []DashboardRow{
		{Project: "pop", CursorKey: "pop\x00one", RawStatus: tasks.StatusDone, ID: "one", DefPath: "/tasks", RuntimePath: "/wt", Bound: true},
	}})
	m.width = 120
	m.height = 24

	updated, _ := m.Update(tea.KeyPressMsg{Code: 's', Text: "s"})
	got := updated.(QueueDashboard)
	updated, cmd := got.Update(tea.KeyPressMsg{Code: 'x', Text: "x"})
	if got := updated.(QueueDashboard); got.menu != nil {
		t.Fatal("archive left the status menu open")
	}
	if cmd == nil {
		t.Fatal("archive should dispatch")
	}
}

// J/K move the live row list while the menu keeps the target and items it had
// when it opened. This is navigation, not the retired pinned-menu re-filtering.
func TestDashboardMenuRowCursorKeysKeepItsOriginalTarget(t *testing.T) {
	m := newQueueDashboard(&drain.Deps{}, &config.Config{}, DashboardSnapshot{Containers: []DashboardRow{
		{Project: "pop", CursorKey: "pop\x00plain", ID: "plain", RuntimePath: "/wt"},
		{Project: "pop", CursorKey: "pop\x00bound", ID: "bound", Bound: true, RuntimePath: "/wt"},
	}})
	m.width = 120
	m.height = 24

	updated, _ := m.Update(tea.KeyPressMsg{Code: 'r', Text: "r"})
	got := updated.(QueueDashboard)
	menuCursor := got.menu.list.Cursor()
	menuKeysBefore := menuKeys(got.menu)
	updated, _ = got.Update(tea.KeyPressMsg{Code: 'J', Text: "J"})
	got = updated.(QueueDashboard)
	if got.ListCursor() != 1 {
		t.Fatalf("J moved the row cursor to %d, want 1 with a menu open", got.ListCursor())
	}
	if got.menu == nil || got.menu.row.ID != "plain" {
		t.Fatal("J changed the open menu's original target")
	}
	if got.menu.list.Cursor() != menuCursor || !slices.Equal(menuKeys(got.menu), menuKeysBefore) {
		t.Fatal("J re-filtered or navigated the open menu")
	}

	updated, _ = got.Update(tea.KeyPressMsg{Code: 'K', Text: "K"})
	if got := updated.(QueueDashboard); got.ListCursor() != 0 {
		t.Fatalf("K moved the row cursor to %d, want 0 with a menu open", got.ListCursor())
	}
}

func menuKeys(menu *dashboardMenu) []string {
	if menu == nil {
		return nil
	}
	var keys []string
	for _, item := range menu.list.Items() {
		keys = append(keys, item.key)
	}
	return keys
}
