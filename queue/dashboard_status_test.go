package queue

import (
	"errors"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/glebglazov/pop/config"
	"github.com/glebglazov/pop/tasks"
)

func TestDashboardActionMenuStatusAndAssistKeys(t *testing.T) {
	row := DashboardRow{SetRef: SetRef{SetID: "demo", RawStatus: tasks.StatusReady, Bound: true}}
	keys := make(map[string]string)
	for _, item := range dashboardMenuItems(row) {
		keys[item.key] = item.label
	}
	if keys["s"] != "status ▸" {
		t.Fatalf("status submenu key = %q, want status ▸", keys["s"])
	}
	if keys["S"] != "assist" {
		t.Fatalf("assist key = %q, want assist on S", keys["S"])
	}
	if _, ok := keys["A"]; !ok {
		t.Fatal("top-level archive shortcut A missing")
	}

	mapKeys := dashboardMenuItems(DashboardRow{IsMap: true, SetRef: SetRef{SetID: "map-1"}})
	for _, item := range mapKeys {
		if item.key == "s" || item.key == "S" {
			t.Fatalf("map row should not offer status or assist: %v", mapKeys)
		}
	}
}

func TestDashboardStatusSubmenuItems(t *testing.T) {
	want := []string{"c", "o", "k", "A", "u"}
	var got []string
	for _, item := range dashboardStatusMenuItems() {
		got = append(got, item.key)
	}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("status submenu keys = %v, want %v", got, want)
	}
}

func TestDashboardStatusSubmenuEscNavigation(t *testing.T) {
	row := DashboardRow{SetRef: SetRef{SetID: "demo"}}
	m := newQueueDashboard(nil, nil, DashboardSnapshot{Rows: []DashboardRow{row}})
	m.menu = newDashboardMenu(row)

	updated, _ := m.Update(tea.KeyPressMsg{Code: 's', Text: "s"})
	got := updated.(QueueDashboard)
	if got.menu == nil || got.menu.status == nil {
		t.Fatal("s should open status submenu")
	}

	updated, _ = got.Update(tea.KeyPressMsg{Code: tea.KeyEscape, Text: "esc"})
	got = updated.(QueueDashboard)
	if got.menu == nil {
		t.Fatal("esc from submenu should return to action menu")
	}
	if got.menu.status != nil {
		t.Fatal("esc from submenu should close status submenu only")
	}

	updated, _ = got.Update(tea.KeyPressMsg{Code: tea.KeyEscape, Text: "esc"})
	got = updated.(QueueDashboard)
	if got.menu != nil {
		t.Fatal("esc from action menu should close menu")
	}
}

func TestDashboardStatusExecCommand(t *testing.T) {
	row := DashboardRow{
		SetRef: SetRef{
			SetID:       "my-set",
			ProjectPath: "/repo",
		},
	}
	cmd := statusExecCommand(row, "complete")
	if len(cmd.Args) < 2 || filepath.Base(cmd.Args[0]) != "pop" {
		t.Fatalf("cmd = %v, want pop tasks complete", cmd.Args)
	}
	wantArgs := []string{"tasks", "complete", "my-set"}
	if strings.Join(cmd.Args[1:], " ") != strings.Join(wantArgs, " ") {
		t.Fatalf("args = %v, want %v", cmd.Args[1:], wantArgs)
	}
	if cmd.Dir != "/repo" {
		t.Fatalf("dir = %q, want /repo", cmd.Dir)
	}
}

func TestDashboardStatusMsgReloadsOnError(t *testing.T) {
	td := queueDataDeps(t)
	row := DashboardRow{
		SetRef:    SetRef{SetID: "demo"},
		CursorKey: "pop\x00demo",
	}
	m := newQueueDashboard(&Deps{Tasks: td}, &config.Config{}, DashboardSnapshot{Rows: []DashboardRow{row}})

	updated, cmd := m.Update(dashboardStatusMsg{setID: "demo", verb: "complete", err: errors.New("refused")})
	got := updated.(QueueDashboard)
	if got.actionErr == nil {
		t.Fatal("expected action error surfaced")
	}
	if cmd == nil {
		t.Fatal("status msg should trigger reload even on error")
	}
	if _, ok := cmd().(dashboardRowsMsg); !ok {
		t.Fatalf("reload cmd type = %T", cmd())
	}
}

func TestDashboardStatusSubmenuHelp(t *testing.T) {
	m := newQueueDashboard(nil, nil, DashboardSnapshot{})
	m.menu = &dashboardMenu{status: newDashboardStatusMenu(DashboardRow{})}
	entries := m.helpEntries()
	found := map[string]bool{}
	for _, e := range entries {
		found[e.Key] = true
	}
	for _, key := range []string{"c", "o", "k", "A", "u", "esc"} {
		if !found[key] {
			t.Errorf("status submenu help missing %q", key)
		}
	}

	m.menu.status = nil
	entries = m.helpEntries()
	found = map[string]bool{}
	for _, e := range entries {
		found[e.Key] = true
	}
	if !found["s"] {
		t.Error("action menu help missing status submenu key s")
	}
	if !found["S"] {
		t.Error("action menu help missing assist key S")
	}
}

func TestDashboardStatusSubmenuDispatch(t *testing.T) {
	row := DashboardRow{SetRef: SetRef{SetID: "demo", ProjectPath: "/repo"}}
	m := newQueueDashboard(nil, nil, DashboardSnapshot{Rows: []DashboardRow{row}})
	m.menu = newDashboardMenu(row)
	m.menu.status = newDashboardStatusMenu(row)

	updated, cmd := m.Update(tea.KeyPressMsg{Code: 'c', Text: "c"})
	got := updated.(QueueDashboard)
	if got.menu != nil {
		t.Fatal("dispatch should close menus")
	}
	if cmd == nil {
		t.Fatal("complete dispatch returned no command")
	}
	// ExecProcess commands are opaque in unit tests; verify the msg handler path
	// separately. Simulate child exit.
	updated, reloadCmd := got.Update(dashboardStatusMsg{setID: "demo", verb: "complete"})
	got = updated.(QueueDashboard)
	if reloadCmd == nil {
		t.Fatal("expected reload after status child exit")
	}
}
