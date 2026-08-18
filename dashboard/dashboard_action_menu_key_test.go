package dashboard

import (
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/glebglazov/pop/config"
	"github.com/glebglazov/pop/tasks"
	"github.com/glebglazov/pop/tasks/drain"
)

// `A` is unbound (ADR-0224 decision 6): sweeping one verb over many rows is the
// Selection's job, so the uppercase key opens nothing and `a` is the only opener.
func TestDashboardUppercaseAOpensNothing(t *testing.T) {
	m := newQueueDashboard(&drain.Deps{}, &config.Config{}, DashboardSnapshot{Containers: []DashboardRow{
		{Project: "pop", CursorKey: "pop\x00set-a", RawStatus: tasks.StatusReady, ID: "set-a", RuntimePath: "/repo/wt-a"},
	}})
	m.width = 120
	m.height = 20

	updated, _ := m.Update(tea.KeyPressMsg{Code: 'A', Text: "A"})
	if got := updated.(QueueDashboard); got.menu != nil {
		t.Fatal("A opened an action menu")
	}

	updated, _ = m.Update(tea.KeyPressMsg{Code: 'a', Text: "a"})
	if got := updated.(QueueDashboard); got.menu == nil {
		t.Fatal("a did not open the action menu")
	}
}

// The menu does not survive the verb it fires: an in-place write closes it just
// as a handoff does.
func TestDashboardActionMenuClosesOnInPlaceVerb(t *testing.T) {
	d := &drain.Deps{
		SetArchived: func(defPath, setID string, on bool) error { return nil },
	}
	m := newQueueDashboard(d, &config.Config{}, DashboardSnapshot{Containers: []DashboardRow{
		{Project: "pop", CursorKey: "pop\x00one", RawStatus: tasks.StatusDone, ID: "one", DefPath: "/tasks", RuntimePath: "/wt", Bound: true},
	}})
	m.width = 120
	m.height = 24

	updated, _ := m.Update(tea.KeyPressMsg{Code: 'a', Text: "a"})
	got := updated.(QueueDashboard)
	updated, cmd := got.Update(tea.KeyPressMsg{Code: 'x', Text: "x"})
	if got := updated.(QueueDashboard); got.menu != nil {
		t.Fatal("archive left the action menu open")
	}
	if cmd == nil {
		t.Fatal("archive should dispatch")
	}
}

// J/K keep moving the row cursor with no menu open, and do nothing to a menu
// that is open.
func TestDashboardMenuIgnoresRowCursorKeys(t *testing.T) {
	m := newQueueDashboard(&drain.Deps{}, &config.Config{}, DashboardSnapshot{Containers: []DashboardRow{
		{Project: "pop", CursorKey: "pop\x00plain", ID: "plain", RuntimePath: "/wt"},
		{Project: "pop", CursorKey: "pop\x00bound", ID: "bound", Bound: true, RuntimePath: "/wt"},
	}})
	m.width = 120
	m.height = 24

	updated, _ := m.Update(tea.KeyPressMsg{Code: 'a', Text: "a"})
	got := updated.(QueueDashboard)
	updated, _ = got.Update(tea.KeyPressMsg{Code: 'J', Text: "J"})
	got = updated.(QueueDashboard)
	if got.ListCursor() != 0 {
		t.Fatalf("J moved the row cursor to %d with a menu open", got.ListCursor())
	}
	if got.menu == nil || got.menu.row.ID != "plain" {
		t.Fatal("J re-filtered the open menu")
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
