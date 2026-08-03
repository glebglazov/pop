package dashboard

import (
	"fmt"
	"github.com/glebglazov/pop/tasks/drain"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/glebglazov/pop/config"
	"github.com/glebglazov/pop/internal/tmux/tmuxtest"
	"github.com/glebglazov/pop/routine"
	"github.com/glebglazov/pop/work"
	"github.com/glebglazov/pop/work/ref"
)

// The dashboard's side of a kind's own verbs: the menu is whatever the row's kind
// offers, dispatch hands the verb back to that kind, and the outcome it returns is
// carried out here. The Routine kind is the one that exercises all four outcomes,
// so it drives these — but nothing in the code path under test names it.

// firedRoutineContainer is a Routine row with a run behind it: a report to copy at
// the row level and one run item carrying that report's path.
func firedRoutineContainer() work.Container {
	c := routineContainer("delta", routine.TierHere)
	c.RoutineLastReport = "/data/pop/routines/delta/runs/2026-07-18T16-00-00Z.md"
	c.Items = []work.Item{{
		ID:     "2026-07-18T16:00:00Z",
		Title:  "2026-07-18 16:00",
		Status: "ok",
		File:   c.RoutineLastReport,
	}}
	return c
}

// pressKeys feeds one single-character key at a time, applying every command the
// model returns, so a verb that answers through a tea.Cmd lands the way it does
// live.
func pressKeys(t *testing.T, m QueueDashboard, keys ...string) QueueDashboard {
	t.Helper()
	for _, key := range keys {
		updated, cmd := m.Update(tea.KeyPressMsg{Code: rune(key[0]), Text: key})
		m = updated.(QueueDashboard)
		for cmd != nil {
			msg := cmd()
			if msg == nil {
				break
			}
			updated, cmd = m.Update(msg)
			m = updated.(QueueDashboard)
		}
	}
	return m
}

// TestRoutineVerbsComeFromTheKindAndDispatchToIt drives one Routine row from the
// keypress that opens its menu to the clipboard the verb wrote: every key in the
// menu is the Routine kind's own, and the verb behind it runs through that kind's
// Perform with no dashboard case of its own.
func TestRoutineVerbsComeFromTheKindAndDispatchToIt(t *testing.T) {
	container := firedRoutineContainer()
	d := routinePageDeps([]work.Container{container})
	m := openPage(t, d, PageRoutines)
	var copied string
	m.copyFunc = func(payload string) error { copied = payload; return nil }

	m = pressKeys(t, m, "a")
	if m.menu == nil {
		t.Fatal("`a` did not open the action menu")
	}
	kind := routine.NewKind(nil)
	var want []string
	for _, action := range kind.Actions(container) {
		want = append(want, action.Key)
	}
	if got := menuKeys(m.menu); fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("menu keys = %v, want the Routine kind's own %v", got, want)
	}

	m = pressKeys(t, m, "c")
	if copied != container.RoutineLastReport {
		t.Fatalf("clipboard = %q, want the newest run's report %q", copied, container.RoutineLastReport)
	}
	if m.statusMsg != "copied report path" {
		t.Fatalf("status = %q, want the kind's own confirmation", m.statusMsg)
	}
	if m.menu != nil {
		t.Fatal("a one-shot menu must close behind the verb")
	}
}

// TestRoutineVerbBehavesTheSameOnEitherPage pins the page symmetry: a Routine row
// listed on page A and the same row on page B run the same verb through the same
// kind and produce the same result, because paging is a display concern and verbs
// hang off the kind rather than off the page.
func TestRoutineVerbBehavesTheSameOnEitherPage(t *testing.T) {
	container := firedRoutineContainer()
	kinds := func(*config.Config) []work.Kind {
		return []work.Kind{&fixedRoutineKind{Kind: routine.NewKind(nil), containers: []work.Container{container}}}
	}
	d := &drain.Deps{Kinds: kinds, RoutineKinds: kinds}

	type result struct {
		keys    []string
		copied  string
		status  string
		detail  bool
		details int
	}
	var results []result
	for _, page := range []Page{PageWork, PageRoutines} {
		m := openPage(t, d, page)
		var copied string
		m.copyFunc = func(payload string) error { copied = payload; return nil }
		m = pressKeys(t, m, "a")
		got := result{keys: menuKeys(m.menu)}
		m = pressKeys(t, m, "c")
		got.copied, got.status = copied, m.statusMsg
		// The runs verb opens the same generic detail from either page.
		m = pressKeys(t, m, "a", "l")
		got.detail = m.detail != nil
		if m.detail != nil {
			got.details = len(m.detail.row.Items)
		}
		results = append(results, got)
	}
	if fmt.Sprint(results[0]) != fmt.Sprint(results[1]) {
		t.Fatalf("page A result %+v differs from page B %+v", results[0], results[1])
	}
	if results[0].copied != container.RoutineLastReport || !results[0].detail || results[0].details != 1 {
		t.Fatalf("verbs did not act: %+v", results[0])
	}
}

// TestRoutineRunItemVerbsComeFromTheKind pins the item half: a run's verbs are the
// kind's ItemActions, asked when the item menu opens, and the copy lands on the
// detail's own status line.
func TestRoutineRunItemVerbsComeFromTheKind(t *testing.T) {
	container := firedRoutineContainer()
	d := routinePageDeps([]work.Container{container})
	m := openPage(t, d, PageRoutines)
	var copied string
	m.copyFunc = func(payload string) error { copied = payload; return nil }

	m = pressKeys(t, m, "l") // open the detail over the row
	if m.detail == nil {
		t.Fatal("`l` did not open the detail view")
	}
	m = pressKeys(t, m, "a") // item actions over the cursored run
	if m.itemMenu == nil {
		t.Fatal("`a` did not open the item action menu")
	}
	var want []string
	for _, action := range routine.NewKind(nil).ItemActions(container, container.Items[0]) {
		want = append(want, action.Key)
	}
	var got []string
	for _, action := range m.itemMenu.list.Items() {
		got = append(got, action.Key)
	}
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("item menu keys = %v, want the kind's %v", got, want)
	}

	m = pressKeys(t, m, "c")
	if copied != container.Items[0].File {
		t.Fatalf("clipboard = %q, want the run's report %q", copied, container.Items[0].File)
	}
	if m.detail == nil || m.detail.statusMsg != "copied report path" {
		t.Fatalf("detail status = %q, want the kind's confirmation", m.detail.statusMsg)
	}
}

// outcomeKind answers one arranged outcome for its one verb, so the dashboard's
// interpretation of each outcome kind can be driven on its own.
type outcomeKind struct {
	*routine.Kind
	outcome work.Outcome
	err     error
}

func (k *outcomeKind) Actions(work.Container) []work.Action {
	return []work.Action{{Verb: "arranged", Key: "z", Label: "arranged"}}
}

func (k *outcomeKind) Perform(work.Container, *work.Item, work.Verb) (work.Outcome, error) {
	return k.outcome, k.err
}

// TestKindVerbOutcomesAreCarriedOutGenerically pins the four things a performed
// verb can ask of the surface — a message, a refresh, a detail view, a pane
// handoff — plus a refusal, none of which needs a case per kind.
func TestKindVerbOutcomesAreCarriedOutGenerically(t *testing.T) {
	container := firedRoutineContainer()

	open := func(t *testing.T, k *outcomeKind) (QueueDashboard, *string) {
		t.Helper()
		kinds := func(*config.Config) []work.Kind { return []work.Kind{k} }
		d := &drain.Deps{Kinds: kinds, RoutineKinds: kinds, Tmux: &tmuxtest.Fake{Inside: true}}
		m := openPage(t, d, PageRoutines)
		copied := new(string)
		m.copyFunc = func(payload string) error { *copied = payload; return nil }
		return m, copied
	}
	fixed := func(outcome work.Outcome, err error) *outcomeKind {
		return &outcomeKind{Kind: routine.NewKind(nil), outcome: outcome, err: err}
	}

	t.Run("message with a clipboard payload", func(t *testing.T) {
		k := fixed(work.Outcome{Kind: work.OutcomeMessage, Clipboard: "payload", Message: "copied a thing"}, nil)
		m, copied := open(t, k)
		m = pressKeys(t, m, "a", "z")
		if *copied != "payload" || m.statusMsg != "copied a thing" {
			t.Fatalf("clipboard = %q, status = %q", *copied, m.statusMsg)
		}
	})

	t.Run("refresh reloads the page", func(t *testing.T) {
		k := fixed(work.Outcome{Kind: work.OutcomeRefresh, Message: "paused delta"}, nil)
		m, _ := open(t, k)
		updated, cmd := m.Update(tea.KeyPressMsg{Code: 'a', Text: "a"})
		m = updated.(QueueDashboard)
		updated, cmd = m.Update(tea.KeyPressMsg{Code: 'z', Text: "z"})
		m = updated.(QueueDashboard)
		msg := cmd()
		updated, cmd = m.Update(msg)
		m = updated.(QueueDashboard)
		if m.statusMsg != "paused delta" {
			t.Fatalf("status = %q", m.statusMsg)
		}
		if cmd == nil {
			t.Fatal("a refresh outcome must reload the page")
		}
		if _, ok := cmd().(dashboardRowsMsg); !ok {
			t.Fatalf("reload cmd produced %T, want a rows message", cmd())
		}
	})

	t.Run("detail opens the container's items", func(t *testing.T) {
		k := fixed(work.Outcome{Kind: work.OutcomeDetail}, nil)
		m, _ := open(t, k)
		m = pressKeys(t, m, "a", "z")
		if m.detail == nil || m.detail.row.ID != container.ID || len(m.detail.row.Items) != 1 {
			t.Fatalf("detail = %+v, want the row's own items", m.detail)
		}
	})

	t.Run("handoff focuses the pane and quits", func(t *testing.T) {
		k := fixed(work.Outcome{
			Kind:    work.OutcomeHandoff,
			Handoff: work.Handoff{Kind: work.HandoffTmux, Target: "%77"},
		}, nil)
		m, _ := open(t, k)
		m = pressKeys(t, m, "a", "z")
		fake := m.d.Tmux.(*tmuxtest.Fake)
		if len(fake.Selected) == 0 || fake.Selected[0] != "%77" {
			t.Fatalf("selected panes = %v, want the handoff's own", fake.Selected)
		}
		if len(fake.Switched) == 0 {
			t.Fatal("a handoff must switch the client to the pane it named")
		}
	})

	t.Run("a refusal is sticky on the action line", func(t *testing.T) {
		k := fixed(work.Outcome{}, fmt.Errorf("routine %q has no directory", "delta"))
		m, _ := open(t, k)
		m = pressKeys(t, m, "a", "z")
		if m.actionErr == nil {
			t.Fatal("a refused verb must surface")
		}
		if m.statusMsg != "" {
			t.Fatalf("status = %q, want the refusal on the action line alone", m.statusMsg)
		}
	})
}

// Load is the arranged row set every outcome case runs over.
func (k *outcomeKind) Load() ([]work.Container, error) {
	return []work.Container{firedRoutineContainer()}, nil
}

func (k *outcomeKind) ID() work.KindID { return ref.KindRoutine }
