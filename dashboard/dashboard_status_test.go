package dashboard

import (
	"errors"
	"github.com/glebglazov/pop/internal/queuetest"
	"github.com/glebglazov/pop/tasks/drain"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/glebglazov/pop/config"
	"github.com/glebglazov/pop/tasks"
	"github.com/glebglazov/pop/work/ref"
)

func TestDashboardActionMenuStatusAndAssistKeys(t *testing.T) {
	row := DashboardRow{ID: "demo", RawStatus: tasks.StatusReady, Bound: true}
	keys := make(map[string]string)
	for _, item := range dashboardMenuItems(testKinds(), row) {
		keys[item.key] = item.label
	}
	if keys["s"] != "status ▸" {
		t.Fatalf("status submenu key = %q, want status ▸", keys["s"])
	}
	if keys["S"] != "assist" {
		t.Fatalf("assist key = %q, want assist on S", keys["S"])
	}
	if _, ok := keys["x"]; !ok {
		t.Fatal("top-level archive shortcut x missing")
	}

	mapKeys := dashboardMenuItems(testKinds(), DashboardRow{Kind: ref.KindMap, ID: "map-1"})
	for _, item := range mapKeys {
		if item.key == "s" || item.key == "S" {
			t.Fatalf("map row should not offer status or assist: %v", mapKeys)
		}
	}
}

func TestDashboardStatusSubmenuItems(t *testing.T) {
	want := []string{"c", "o", "s", "x", "u"}
	var got []string
	for _, item := range dashboardStatusMenuItems() {
		got = append(got, item.key)
	}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("status submenu keys = %v, want %v", got, want)
	}
}

func TestDashboardStatusSubmenuEscNavigation(t *testing.T) {
	row := DashboardRow{ID: "demo"}
	m := newQueueDashboard(nil, nil, DashboardSnapshot{Containers: []DashboardRow{row}})
	m.menu = newDashboardMenu(testKinds(), row, false)

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

func TestDashboardStatusMsgReloadsOnError(t *testing.T) {
	td := queuetest.DataDeps(t)
	row := DashboardRow{
		ID:        "demo",
		CursorKey: "pop\x00demo",
	}
	m := newQueueDashboard(&drain.Deps{Tasks: td}, &config.Config{}, DashboardSnapshot{Containers: []DashboardRow{row}})

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
	m.menu = newDashboardMenu(testKinds(), DashboardRow{ID: "set"}, false)
	m.menu.status = newDashboardStatusMenu(DashboardRow{})
	entries := m.helpEntries()
	found := map[string]bool{}
	for _, e := range entries {
		found[e.Key] = true
	}
	for _, key := range []string{"c", "o", "s", "x", "u", "esc"} {
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

func TestDashboardStatusSubmenuDispatchInProcess(t *testing.T) {
	repo, setID, _ := queuetest.SetupSpawnRepo(t, "status-complete", []queuetest.SpawnTask{
		{ID: "01-a", File: "01-a.md", Title: "A", Type: "AFK", Status: "open"},
		{ID: "02-b", File: "02-b.md", Title: "B", Type: "AFK", Status: "open"},
	})
	d, cfg, row, _ := dashboardLaunchFixture(t, repo, setID)
	row.ProjectPath = repo
	m := newQueueDashboard(d, cfg, DashboardSnapshot{Containers: []DashboardRow{row}})
	m.menu = newDashboardMenu(testKinds(), row, false)
	m.menu.status = newDashboardStatusMenu(row)

	updated, cmd := m.Update(tea.KeyPressMsg{Code: 'c', Text: "c"})
	got := updated.(QueueDashboard)
	if got.menu != nil {
		t.Fatal("dispatch should close menus")
	}
	if cmd == nil {
		t.Fatal("complete dispatch returned no command")
	}
	msg := cmd()
	statusMsg, ok := msg.(dashboardStatusMsg)
	if !ok {
		t.Fatalf("msg = %T, want dashboardStatusMsg (in-process, not ExecProcess)", msg)
	}
	if statusMsg.err != nil {
		t.Fatalf("complete err = %v", statusMsg.err)
	}
	if statusMsg.verb != "complete" {
		t.Fatalf("verb = %q, want complete", statusMsg.verb)
	}

	updated, reloadCmd := got.Update(statusMsg)
	got = updated.(QueueDashboard)
	if reloadCmd == nil {
		t.Fatal("expected reload after in-process status write")
	}
	if got.statusMsg == "" || !strings.Contains(got.statusMsg, "complete") {
		t.Fatalf("statusMsg = %q, want complete confirmation", got.statusMsg)
	}

	result, err := tasks.RefreshWith(d.Tasks, row.DefPath, row.StatePath)
	if err != nil {
		t.Fatal(err)
	}
	manifest := result.Manifests[setID]
	if manifest == nil {
		t.Fatal("missing manifest after complete")
	}
	for _, task := range manifest.Tasks {
		if task.Status != tasks.TaskDone {
			t.Fatalf("task %s status = %s, want done after in-process complete-all", task.ID, task.Status)
		}
	}
}

func TestDashboardStatusArchiveInProcess(t *testing.T) {
	var archivedDef, archivedSet string
	d := &drain.Deps{
		ArchiveSet: func(defPath, setID string) error {
			archivedDef, archivedSet = defPath, setID
			return nil
		},
		Tasks: queuetest.DataDeps(t),
	}
	row := DashboardRow{ID: "demo", DefPath: "/repo/tasks"}
	if err := applyDashboardStatusVerb(d, row, "archive"); err != nil {
		t.Fatalf("archive: %v", err)
	}
	if archivedDef != "/repo/tasks" || archivedSet != "demo" {
		t.Fatalf("archive target = (%q, %q), want (/repo/tasks, demo)", archivedDef, archivedSet)
	}
}

func TestQueueDashboardHasNoExecProcess(t *testing.T) {
	entries, err := filepath.Glob("dashboard*.go")
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range entries {
		if strings.HasSuffix(name, "_test.go") {
			continue
		}
		body, err := os.ReadFile(name)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(body), "tea.ExecProcess") {
			t.Fatalf("%s still references tea.ExecProcess", name)
		}
	}
}
