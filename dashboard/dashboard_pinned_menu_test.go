package dashboard

import (
	"github.com/glebglazov/pop/internal/queuetest"
	"github.com/glebglazov/pop/tasks/drain"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/glebglazov/pop/config"
	"github.com/glebglazov/pop/tasks"
)

func TestDashboardPinnedActionMenuOpen(t *testing.T) {
	rows := []DashboardRow{
		{Project: "pop", CursorKey: "pop\x00set-a", RawStatus: tasks.StatusReady, ID: "set-a", RuntimePath: "/repo/wt-a"},
	}
	m := newQueueDashboard(&drain.Deps{}, &config.Config{}, DashboardSnapshot{Containers: rows})
	m.width = 120
	m.height = 20

	// `A` opens pinned; `a` stays one-shot.
	updated, _ := m.Update(tea.KeyPressMsg{Code: 'A', Text: "A"})
	got := updated.(QueueDashboard)
	if got.menu == nil || !got.menu.pinned {
		t.Fatal("A did not open the pinned action menu")
	}

	updated, _ = got.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	got = updated.(QueueDashboard)

	updated, _ = got.Update(tea.KeyPressMsg{Code: 'a', Text: "a"})
	got = updated.(QueueDashboard)
	if got.menu == nil || got.menu.pinned {
		t.Fatal("a should open a one-shot action menu")
	}
}

func TestDashboardPinnedActionMenuRowCursorAndRefilter(t *testing.T) {
	rows := []DashboardRow{
		{Project: "pop", CursorKey: "pop\x00plain", ID: "plain", RuntimePath: "/wt"},
		{Project: "pop", CursorKey: "pop\x00bound", ID: "bound", Bound: true, RuntimePath: "/wt"},
		{Project: "pop", CursorKey: "pop\x00parked", ID: "parked", Parked: true, RuntimePath: "/wt"},
	}
	m := newQueueDashboard(&drain.Deps{}, &config.Config{}, DashboardSnapshot{Containers: rows})
	m.width = 120
	m.height = 24

	updated, _ := m.Update(tea.KeyPressMsg{Code: 'A', Text: "A"})
	got := updated.(QueueDashboard)
	if got.ListCursor() != 0 {
		t.Fatalf("cursor = %d, want 0", got.ListCursor())
	}
	if contains(menuKeys(got.menu), "u") {
		t.Fatalf("plain row should not offer unbind: %v", got.menu.list.Items())
	}

	updated, _ = got.Update(tea.KeyPressMsg{Code: 'J', Text: "J"})
	got = updated.(QueueDashboard)
	if got.ListCursor() != 1 {
		t.Fatalf("J cursor = %d, want 1", got.ListCursor())
	}
	if got.menu.row.ID != "bound" {
		t.Fatalf("menu row = %q, want bound", got.menu.row.ID)
	}
	if !contains(menuKeys(got.menu), "u") {
		t.Fatalf("bound row should offer unbind after J: %v", got.menu.list.Items())
	}

	updated, _ = got.Update(tea.KeyPressMsg{Code: 'J', Text: "J"})
	got = updated.(QueueDashboard)
	if got.ListCursor() != 2 {
		t.Fatalf("second J cursor = %d, want 2", got.ListCursor())
	}
	if !contains(menuKeys(got.menu), "r") {
		t.Fatalf("parked row should offer unpark after second J: %v", got.menu.list.Items())
	}

	updated, _ = got.Update(tea.KeyPressMsg{Code: 'K', Text: "K"})
	got = updated.(QueueDashboard)
	if got.ListCursor() != 1 || got.menu.row.ID != "bound" {
		t.Fatalf("K did not move back to bound row: cursor=%d row=%q", got.ListCursor(), got.menu.row.ID)
	}
}

func TestDashboardPinnedActionMenuInPlaceVerbStaysOpen(t *testing.T) {
	var archived []string
	d := &drain.Deps{
		ArchiveSet: func(defPath, setID string) error {
			archived = append(archived, setID)
			return nil
		},
	}
	rows := []DashboardRow{
		{Project: "pop", CursorKey: "pop\x00one", RawStatus: tasks.StatusDone, ID: "one", DefPath: "/tasks", RuntimePath: "/wt", Bound: true},
		{Project: "pop", CursorKey: "pop\x00two", RawStatus: tasks.StatusDone, ID: "two", DefPath: "/tasks", RuntimePath: "/wt", Bound: true},
	}
	m := newQueueDashboard(d, &config.Config{}, DashboardSnapshot{Containers: rows})
	m.width = 120
	m.height = 24

	updated, _ := m.Update(tea.KeyPressMsg{Code: 'A', Text: "A"})
	got := updated.(QueueDashboard)

	updated, cmd := got.Update(tea.KeyPressMsg{Code: 'x', Text: "x"})
	got = updated.(QueueDashboard)
	if got.menu == nil || !got.menu.pinned {
		t.Fatal("archive from pinned menu should leave it open")
	}
	if cmd == nil {
		t.Fatal("archive should dispatch")
	}
	updated, _ = got.Update(cmd().(dashboardArchiveMsg))
	got = updated.(QueueDashboard)
	if len(archived) != 1 || archived[0] != "one" {
		t.Fatalf("archived = %v, want [one]", archived)
	}

	updated, _ = got.Update(tea.KeyPressMsg{Code: 'J', Text: "J"})
	got = updated.(QueueDashboard)
	updated, cmd = got.Update(tea.KeyPressMsg{Code: 'x', Text: "x"})
	got = updated.(QueueDashboard)
	if got.menu == nil || !got.menu.pinned {
		t.Fatal("second archive from pinned menu should leave it open")
	}
	if cmd == nil {
		t.Fatal("second archive should dispatch")
	}
	updated, _ = got.Update(cmd().(dashboardArchiveMsg))
	got = updated.(QueueDashboard)
	if len(archived) != 2 || archived[1] != "two" {
		t.Fatalf("archived = %v, want [one two]", archived)
	}
}

func TestDashboardPinnedActionMenuHandoffQuits(t *testing.T) {
	repo, setID, _ := queuetest.SetupSpawnRepo(t, "pinned-handoff", []queuetest.SpawnTask{
		{ID: "01-a", File: "01-a.md", Title: "A", Type: "AFK", Status: "done"},
	})
	d, cfg, row, rt := dashboardLaunchFixture(t, repo, setID)
	row.RuntimePath = repo
	row.ProjectPath = repo
	rt.Fake.Inside = true

	m := newQueueDashboard(d, cfg, DashboardSnapshot{Containers: []DashboardRow{row}})
	m.width = 120
	m.height = 24

	updated, _ := m.Update(tea.KeyPressMsg{Code: 'A', Text: "A"})
	got := updated.(QueueDashboard)
	updated, cmd := got.Update(tea.KeyPressMsg{Code: 'S', Text: "S"})
	got = updated.(QueueDashboard)
	if got.menu != nil {
		t.Fatal("handoff from pinned menu should close the menu")
	}
	if cmd == nil {
		t.Fatal("assist handoff should dispatch")
	}
	handoff, ok := cmd().(dashboardHandoffMsg)
	if !ok {
		t.Fatalf("cmd = %T, want dashboardHandoffMsg", cmd())
	}
	if !handoff.quit {
		t.Fatalf("handoff = %+v, want quit", handoff)
	}
	_, quitCmd := got.Update(handoff)
	if quitCmd == nil {
		t.Fatal("successful handoff must quit the dashboard")
	}
	if _, ok := quitCmd().(tea.QuitMsg); !ok {
		t.Fatalf("quit cmd = %T, want tea.QuitMsg", quitCmd())
	}
}

func TestDashboardPinnedActionMenuEscapeCloses(t *testing.T) {
	m := newQueueDashboard(&drain.Deps{}, &config.Config{}, DashboardSnapshot{Containers: []DashboardRow{
		{Project: "pop", CursorKey: "pop\x00set", ID: "set", RuntimePath: "/wt"},
	}})
	updated, _ := m.Update(tea.KeyPressMsg{Code: 'A', Text: "A"})
	got := updated.(QueueDashboard)
	updated, cmd := got.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	got = updated.(QueueDashboard)
	if got.menu != nil {
		t.Fatal("esc should close the pinned action menu")
	}
	if cmd != nil {
		t.Fatal("closing pinned menu should not quit")
	}
}

func TestDashboardPinnedActionMenuHelp(t *testing.T) {
	m := newQueueDashboard(nil, nil, DashboardSnapshot{})
	m.menu = &dashboardMenu{pinned: true}
	entries := m.helpEntries()
	found := map[string]bool{}
	for _, e := range entries {
		found[e.Key] = true
	}
	if !found["J/K"] {
		t.Error("pinned action menu help missing J/K row cursor keys")
	}

	m = newQueueDashboard(nil, nil, DashboardSnapshot{})
	entries = m.helpEntries()
	found = map[string]bool{}
	for _, e := range entries {
		found[e.Key] = true
	}
	if !found["A"] {
		t.Error("main list help missing A pinned action menu key")
	}
}

func TestDashboardPinnedActionMenuFooterHint(t *testing.T) {
	m := newQueueDashboard(&drain.Deps{}, &config.Config{}, DashboardSnapshot{Containers: []DashboardRow{
		{Project: "pop", CursorKey: "pop\x00set", ID: "set", RuntimePath: "/wt"},
	}})
	m.width = 120
	m.height = 20
	m.menu = newDashboardMenu(testKinds(), m.snap.Containers[0], true)
	view := m.View().Content
	if !strings.Contains(view, "J/K row") {
		t.Fatalf("pinned menu footer missing J/K row hint:\n%s", view)
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
