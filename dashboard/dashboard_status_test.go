package dashboard

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/glebglazov/pop/config"
	"github.com/glebglazov/pop/internal/queuetest"
	"github.com/glebglazov/pop/tasks"
	"github.com/glebglazov/pop/tasks/drain"
	"github.com/glebglazov/pop/tasks/setkind"
	"github.com/glebglazov/pop/wayfinder"
	"github.com/glebglazov/pop/work"
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

	// A Map carries the same two keys: `S` for its own assist session (ADR-0184),
	// and `s` for its own status submenu (ADR-0186) — the opener is shared, the
	// items behind it are not.
	mapKeys := dashboardMenuItems(testKinds(), DashboardRow{Kind: ref.KindMap, ID: "map-1"})
	var mapAssist, mapStatus bool
	for _, item := range mapKeys {
		if item.key == "s" {
			if item.verb != work.VerbStatus {
				t.Fatalf("map s = %q, want the shared status opener", item.verb)
			}
			mapStatus = true
		}
		if item.key == "S" {
			if item.verb != wayfinder.VerbAssist {
				t.Fatalf("map S = %q, want the Map's own assist verb", item.verb)
			}
			mapAssist = true
		}
	}
	if !mapAssist {
		t.Fatalf("map row should offer assist on S: %v", mapKeys)
	}
	if !mapStatus {
		t.Fatalf("map row should offer a status submenu on s: %v", mapKeys)
	}
}

// The Task set's submenu is the one it has always had — same keys, same labels,
// same order — now answered by the kind rather than by a list in the dashboard.
func TestDashboardStatusSubmenuItems(t *testing.T) {
	want := []string{"c complete", "o open", "s skip", "x archive", "u unarchive"}
	var got []string
	for _, item := range testKinds().statusActionsFor(DashboardRow{ID: "demo"}) {
		got = append(got, item.Key+" "+item.Label)
	}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("status submenu = %v, want %v", got, want)
	}
}

// The Map's submenu is its own vocabulary on the same opener, and `arrive` is
// absent from it: arrival ends the Map, kills its session and prints a report, so
// it stays a command-line ceremony (ADR-0186).
func TestMapStatusSubmenuItems(t *testing.T) {
	row := DashboardRow{Kind: ref.KindMap, ID: "map-1"}
	want := []string{"o open (reopen)", "a abandon", "x archive", "u unarchive"}
	var got []string
	for _, item := range testKinds().statusActionsFor(row) {
		got = append(got, item.Key+" "+item.Label)
		if item.Verb == "arrive" || strings.Contains(item.Label, "arrive") {
			t.Fatalf("arrive is offered in the map status submenu: %+v", item)
		}
	}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("map status submenu = %v, want %v", got, want)
	}
}

// A kind with no status to write offers no submenu and no opener, so `s` is not a
// key that opens an empty overlay.
func TestRoutineOffersNoStatusSubmenu(t *testing.T) {
	row := DashboardRow{Kind: ref.KindRoutine, ID: "nightly"}
	if items := testRoutineKinds().statusActionsFor(row); len(items) != 0 {
		t.Fatalf("routine status actions = %+v, want none", items)
	}
	for _, item := range dashboardMenuItems(testRoutineKinds(), row) {
		if item.verb == work.VerbStatus {
			t.Fatalf("routine row offers the status opener: %+v", item)
		}
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

// A refused status write is surfaced on the sticky action-error line, the way
// every other refused row verb is.
func TestDashboardStatusVerbErrorIsSurfaced(t *testing.T) {
	td := queuetest.DataDeps(t)
	row := DashboardRow{
		ID:        "demo",
		CursorKey: "pop\x00demo",
	}
	m := newQueueDashboard(&drain.Deps{Tasks: td}, &config.Config{}, DashboardSnapshot{Containers: []DashboardRow{row}})

	updated, _ := m.Update(dashboardKindVerbMsg{row: row, verb: setkind.VerbComplete, err: errors.New("refused")})
	got := updated.(QueueDashboard)
	if got.actionErr == nil {
		t.Fatal("expected action error surfaced")
	}
}

func TestDashboardStatusSubmenuHelp(t *testing.T) {
	m := newQueueDashboard(nil, nil, DashboardSnapshot{})
	m.menu = newDashboardMenu(testKinds(), DashboardRow{ID: "set"}, false)
	m.menu.status = newDashboardStatusMenu(testKinds(), DashboardRow{ID: "set"})
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
	m.menu.status = newDashboardStatusMenu(testKinds(), row)

	updated, cmd := m.Update(tea.KeyPressMsg{Code: 'c', Text: "c"})
	got := updated.(QueueDashboard)
	if got.menu != nil {
		t.Fatal("dispatch should close menus")
	}
	if cmd == nil {
		t.Fatal("complete dispatch returned no command")
	}
	msg := cmd()
	statusMsg, ok := msg.(dashboardKindVerbMsg)
	if !ok {
		t.Fatalf("msg = %T, want dashboardKindVerbMsg (in-process, not ExecProcess)", msg)
	}
	if statusMsg.err != nil {
		t.Fatalf("complete err = %v", statusMsg.err)
	}
	if statusMsg.verb != setkind.VerbComplete {
		t.Fatalf("verb = %q, want complete", statusMsg.verb)
	}
	if statusMsg.item != nil {
		t.Fatalf("status submenu complete named an item (%+v); it writes the whole set", statusMsg.item)
	}

	updated, reloadCmd := got.Update(statusMsg)
	got = updated.(QueueDashboard)
	if reloadCmd == nil {
		t.Fatal("expected reload after in-process status write")
	}
	if got.flash.Text() == "" || !strings.Contains(got.flash.Text(), "complete") {
		t.Fatalf("flash = %q, want complete confirmation", got.flash.Text())
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

// Both halves of the archive flag are written in-process from the submenu, each
// through the kind's own Perform — no subprocess, no TUI suspend (ADR-0158).
func TestDashboardStatusArchivePairInProcess(t *testing.T) {
	type write struct {
		defPath, setID string
		archived       bool
	}
	var writes []write
	d := &drain.Deps{
		SetArchived: func(defPath, setID string, archived bool) error {
			writes = append(writes, write{defPath, setID, archived})
			return nil
		},
		Tasks: queuetest.DataDeps(t),
	}
	row := DashboardRow{ID: "demo", DefPath: "/repo/tasks", CursorKey: "pop\x00demo"}
	for _, key := range []rune{'x', 'u'} {
		m := newQueueDashboard(d, &config.Config{}, DashboardSnapshot{Containers: []DashboardRow{row}})
		m.menu = newDashboardMenu(m.kinds, row, false)
		m.menu.status = newDashboardStatusMenu(m.kinds, row)
		_, cmd := m.Update(tea.KeyPressMsg{Code: key, Text: string(key)})
		if cmd == nil {
			t.Fatalf("%c dispatched no command", key)
		}
		msg, ok := cmd().(dashboardKindVerbMsg)
		if !ok {
			t.Fatalf("%c produced %T, want dashboardKindVerbMsg", key, cmd())
		}
		if msg.err != nil {
			t.Fatalf("%c err = %v", key, msg.err)
		}
	}
	want := []write{{"/repo/tasks", "demo", true}, {"/repo/tasks", "demo", false}}
	if fmt.Sprint(writes) != fmt.Sprint(want) {
		t.Fatalf("archive writes = %v, want %v", writes, want)
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
