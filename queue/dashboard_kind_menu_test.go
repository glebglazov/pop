package queue

import (
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/glebglazov/pop/config"
	"github.com/glebglazov/pop/tasks"
	"github.com/glebglazov/pop/tasks/setkind"
	"github.com/glebglazov/pop/wayfinder"
	"github.com/glebglazov/pop/work"
	"github.com/glebglazov/pop/work/ref"
)

// countingKind is a Task-set-shaped kind that records how often the dashboard
// asks it for verbs and can change its answer between asks — the two facts a
// lazily built menu must exhibit.
type countingKind struct {
	asked   int
	extra   *work.Action
	skipped []work.ModelSkip
}

func (k *countingKind) ID() work.KindID                 { return ref.KindTaskSet }
func (k *countingKind) Load() ([]work.Container, error) { return nil, nil }
func (k *countingKind) Less(a, b work.Container) bool   { return a.ID < b.ID }
func (k *countingKind) StatusCell(c work.Container) []work.StatusSegment {
	return []work.StatusSegment{{Text: "READY", Tone: work.ToneLabel}}
}

func (k *countingKind) Actions(c work.Container) []work.Action {
	k.asked++
	actions := []work.Action{{Verb: work.VerbCopyName, Key: "y", Label: "copy name"}}
	if k.extra != nil {
		actions = append(actions, *k.extra)
	}
	return actions
}

func (k *countingKind) ItemActions(work.Container, work.Item) []work.Action { return nil }
func (k *countingKind) Perform(work.Container, *work.Item, work.Verb) (work.Outcome, error) {
	return work.Outcome{}, nil
}
func (k *countingKind) Summary(containers []work.Container) []string { return nil }
func (k *countingKind) ModelSkips() ([]work.ModelSkip, error)        { return k.skipped, nil }

// TestActionMenuIsBuiltOnOpenFromTheKind pins the laziness the seam bought: the
// dashboard asks no kind for verbs while it is only showing rows, asks the one
// kind that owns the cursored row when the menu opens, and gets whatever that
// kind says at that moment — eligibility that moved since the snapshot is
// reflected without a rebuild.
func TestActionMenuIsBuiltOnOpenFromTheKind(t *testing.T) {
	kind := &countingKind{}
	d := &Deps{Kinds: func(*config.Config) []work.Kind { return []work.Kind{kind} }}
	row := DashboardRow{Project: "pop", CursorKey: "pop\x00set", Kind: ref.KindTaskSet, ID: "set", SetRef: SetRef{SetID: "set"}}
	m := newQueueDashboard(d, &config.Config{}, DashboardSnapshot{Rows: []DashboardRow{row}})
	m.width, m.height = 120, 24
	m.View()
	if kind.asked != 0 {
		t.Fatalf("kind asked for verbs %d times while only rendering rows, want 0", kind.asked)
	}

	updated, _ := m.Update(tea.KeyPressMsg{Code: 'a', Text: "a"})
	got := updated.(QueueDashboard)
	if got.menu == nil || len(got.menu.list.Items()) != 1 {
		t.Fatalf("menu = %+v, want the kind's single verb", got.menu)
	}
	if kind.asked == 0 {
		t.Fatal("opening the menu did not ask the kind for verbs")
	}

	// The set became eligible for a verb after the snapshot was built. Re-opening
	// the menu shows it, with no reload in between.
	kind.extra = &work.Action{Verb: work.Verb("late"), Key: "L", Label: "late verb"}
	updated, _ = got.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	updated, _ = updated.(QueueDashboard).Update(tea.KeyPressMsg{Code: 'a', Text: "a"})
	got = updated.(QueueDashboard)
	items := got.menu.list.Items()
	if len(items) != 2 || items[1].key != "L" || items[1].verb != work.Verb("late") {
		t.Fatalf("menu items = %+v, want the verb the kind added since the build", items)
	}
}

// TestSharedVerbsKeepOneKeyOnEveryKind pins the two verbs the seam declares
// shared: copy-name and shell come back on the same key from every kind, while
// everything else in a menu is that kind's own.
func TestSharedVerbsKeepOneKeyOnEveryKind(t *testing.T) {
	set := DashboardRow{Project: "pop", SetRef: SetRef{SetID: "2026-07-01-a", RawStatus: tasks.StatusReady, RuntimePath: "/repo/wt"}}
	wfMap := DashboardRow{Project: "pop", Kind: ref.KindMap, SetRef: SetRef{SetID: "2026-07-02-chart"}, MapOpen: 1, MapFrontier: 1}

	keyOf := func(row DashboardRow, verb work.Verb) string {
		for _, item := range dashboardMenuItems(testKinds(), row) {
			if item.verb == verb {
				return item.key
			}
		}
		return ""
	}
	for _, row := range []DashboardRow{set, wfMap} {
		if got := keyOf(row, work.VerbCopyName); got != "y" {
			t.Fatalf("copy-name key on %s = %q, want y", row.Kind, got)
		}
		if got := keyOf(row, work.VerbShell); got != "O" {
			t.Fatalf("shell key on %s = %q, want O", row.Kind, got)
		}
	}
	// Kind-local verbs are the kind's own: the Map's frontier verb is on the same
	// key as the Task-set drain and neither kind offers the other's.
	if got := keyOf(wfMap, wayfinder.VerbWork); got != "I" {
		t.Fatalf("map frontier verb key = %q, want I", got)
	}
	if got := keyOf(set, setkind.VerbDrain); got != "I" {
		t.Fatalf("task-set drain key = %q, want I", got)
	}
	if keyOf(set, wayfinder.VerbWork) != "" || keyOf(wfMap, setkind.VerbDrain) != "" {
		t.Fatal("a kind-local verb leaked across kinds")
	}
}

// TestModalTaskSetVerbsDispatchByVerbID pins the deferral ADR-0173 took: the
// Task-set verbs that drive a modal the dashboard owns still work, dispatched
// here by verb id rather than by kind.
func TestModalTaskSetVerbsDispatchByVerbID(t *testing.T) {
	row := DashboardRow{
		Project: "pop", CursorKey: "pop\x00set",
		SetRef: SetRef{SetID: "set", RawStatus: tasks.StatusReady, Bound: true, DefPath: "/repo/tasks"},
	}
	m := newQueueDashboard(&Deps{}, &config.Config{}, DashboardSnapshot{Rows: []DashboardRow{row}})
	m.width, m.height = 120, 24

	cases := []struct {
		verb  work.Verb
		check func(QueueDashboard) bool
	}{
		{setkind.VerbBind, func(m QueueDashboard) bool { return m.bind != nil }},
		{setkind.VerbUnbind, func(m QueueDashboard) bool { return m.abandon != nil }},
		{setkind.VerbStatus, func(m QueueDashboard) bool { return m.menu != nil && m.menu.status != nil }},
	}
	for _, c := range cases {
		t.Run(string(c.verb), func(t *testing.T) {
			opened, _ := m.Update(tea.KeyPressMsg{Code: 'a', Text: "a"})
			withMenu := opened.(QueueDashboard)
			key := ""
			for _, item := range withMenu.menu.list.Items() {
				if item.verb == c.verb {
					key = item.key
				}
			}
			if key == "" {
				t.Fatalf("menu does not offer %q: %+v", c.verb, withMenu.menu.list.Items())
			}
			updated, _ := withMenu.Update(tea.KeyPressMsg{Code: []rune(key)[0], Text: key})
			if !c.check(updated.(QueueDashboard)) {
				t.Fatalf("%q did not open its dashboard-owned modal", c.verb)
			}
		})
	}
}
