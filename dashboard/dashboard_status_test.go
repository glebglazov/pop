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

// The Run menu offers neither the status opener nor an archive of its own:
// both live in the Status menu, which `s` opens from the row list (ADR-0236
// decisions 1 and 4). `A` assist is untouched, on a task set and on a Map alike.
func TestRunMenuKeepsAssistAndLosesStatusAndArchive(t *testing.T) {
	for _, row := range []DashboardRow{
		{ID: "demo", RawStatus: tasks.StatusReady, Bound: true},
		{Kind: ref.KindMap, ID: "map-1"},
	} {
		keys := make(map[string]work.Verb)
		labels := make(map[string]string)
		for _, item := range dashboardMenuItems(testKinds(), row) {
			keys[item.key] = item.verb
			labels[item.key] = item.label
		}
		if _, ok := keys["s"]; ok {
			t.Fatalf("%s row still offers a status entry in the run menu: %q", row.ID, labels["s"])
		}
		if _, ok := keys["x"]; ok {
			t.Fatalf("%s row still offers archive beside the status opener: %q", row.ID, labels["x"])
		}
		if keys["A"] == "" {
			t.Fatalf("%s row lost its assist key A", row.ID)
		}
	}
	if got := dashboardMenuItems(testKinds(), DashboardRow{Kind: ref.KindMap, ID: "map-1"}); !mapAssistOffered(got) {
		t.Fatalf("map row should offer its own assist verb on A: %v", got)
	}
}

// mapAssistOffered reports whether `A` is the Map's own assist session rather
// than a Task set's — the opener is shared, the sessions behind the keys are not.
func mapAssistOffered(items []dashboardMenuItem) bool {
	for _, item := range items {
		if item.key == "A" && item.verb == wayfinder.VerbAssist {
			return true
		}
	}
	return false
}

// `s` opens the Status menu straight from the row list, over a Task set and over
// a Map, with each kind's own verbs on the keys they have always had.
func TestStatusMenuOpensFromTheRowList(t *testing.T) {
	for _, tc := range []struct {
		name string
		row  DashboardRow
		want []string
	}{
		{"task set", DashboardRow{Project: "pop", CursorKey: "pop\x00demo", ID: "demo", RawStatus: tasks.StatusReady}, []string{"c complete", "o open", "s skip", "x archive", "u unarchive"}},
		{"map", DashboardRow{Project: "pop", CursorKey: "pop\x00map-1", Kind: ref.KindMap, ID: "map-1"}, []string{"o open (reopen)", "a abandon", "x archive", "u unarchive"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := newQueueDashboard(&drain.Deps{}, &config.Config{}, DashboardSnapshot{Containers: []DashboardRow{tc.row}})
			m.width, m.height = 120, 24

			updated, _ := m.Update(tea.KeyPressMsg{Code: 's', Text: "s"})
			got := updated.(QueueDashboard)
			if got.menu == nil || got.menu.status == nil {
				t.Fatal("s on the row list did not open the status menu")
			}
			var items []string
			for _, action := range got.menu.status.list.Items() {
				items = append(items, action.Key+" "+action.Label)
			}
			if strings.Join(items, ",") != strings.Join(tc.want, ",") {
				t.Fatalf("status menu = %v, want %v", items, tc.want)
			}
		})
	}
}

// A Routine has no status to write, and at top level the absence can no longer
// explain itself by a missing line in a list, so `s` says so (ADR-0236
// decision 7).
func TestStatusKeyOnARoutineFlashes(t *testing.T) {
	row := DashboardRow{Project: "pop", CursorKey: "pop\x00nightly", Kind: ref.KindRoutine, ID: "nightly"}
	m := newQueueDashboard(&drain.Deps{}, &config.Config{}, DashboardSnapshot{Containers: []DashboardRow{row}})
	m.kinds = testRoutineKinds()
	m.width, m.height = 120, 24

	updated, _ := m.Update(tea.KeyPressMsg{Code: 's', Text: "s"})
	got := updated.(QueueDashboard)
	if got.menu != nil {
		t.Fatal("s opened a menu over a kind with no status vocabulary")
	}
	if want := "a Routine has no status to write"; got.flash.Text() != want {
		t.Fatalf("flash = %q, want %q", got.flash.Text(), want)
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

// A kind with no status to write offers no status verbs at all, so `s` never
// opens an empty overlay over one.
func TestRoutineOffersNoStatusVerbs(t *testing.T) {
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

// The Status menu has nothing underneath it, so one esc returns to the row list
// rather than uncovering a menu the operator never opened.
func TestDashboardStatusMenuEscReturnsToTheRowList(t *testing.T) {
	row := DashboardRow{ID: "demo"}
	m := newQueueDashboard(nil, nil, DashboardSnapshot{Containers: []DashboardRow{row}})

	updated, _ := m.Update(tea.KeyPressMsg{Code: 's', Text: "s"})
	got := updated.(QueueDashboard)
	if got.menu == nil || got.menu.status == nil {
		t.Fatal("s should open the status menu")
	}

	updated, _ = got.Update(tea.KeyPressMsg{Code: tea.KeyEscape, Text: "esc"})
	got = updated.(QueueDashboard)
	if got.menu != nil {
		t.Fatal("esc from the status menu should leave no overlay open")
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

func TestDashboardStatusMenuHelp(t *testing.T) {
	row := DashboardRow{ID: "set"}
	m := newQueueDashboard(nil, nil, DashboardSnapshot{Containers: []DashboardRow{row}})
	m.menu = &dashboardMenu{row: row, status: newDashboardStatusMenu(testKinds(), row)}
	found := map[string]bool{}
	for _, e := range m.helpEntries() {
		found[e.Key] = true
	}
	for _, key := range []string{"c", "o", "s", "x", "u", "esc"} {
		if !found[key] {
			t.Errorf("status menu help missing %q", key)
		}
	}

	// With no menu open the overlay names the opener, which is the only place the
	// operator can now learn the key.
	m.menu = nil
	found = map[string]bool{}
	for _, e := range m.helpEntries() {
		found[e.Key] = true
	}
	if !found["s"] {
		t.Error("main help missing the status menu opener s")
	}
	if !strings.Contains(m.mainHint(), "s status") {
		t.Errorf("footer does not advertise the status opener: %q", m.mainHint())
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
	m.menu = &dashboardMenu{row: row, status: newDashboardStatusMenu(testKinds(), row)}

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
		m.menu = &dashboardMenu{row: row, status: newDashboardStatusMenu(m.kinds, row)}
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
