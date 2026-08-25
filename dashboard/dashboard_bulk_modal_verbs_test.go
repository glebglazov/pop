package dashboard

import (
	"slices"
	"strings"
	"testing"

	"github.com/glebglazov/pop/tasks/setkind"
	"github.com/glebglazov/pop/ui"
	"github.com/glebglazov/pop/wayfinder"
	"github.com/glebglazov/pop/work"
	"github.com/glebglazov/pop/work/ref"
)

// The verbs the dashboard intercepts before they reach a kind (ADR-0215 decision
// 5). Mute, unmute and abandon go plural because their input is shared — one
// duration, none at all, or a confirmation — so one answer stands for the whole
// Selection. Everything else the surface intercepts resolves a checkout, a
// worktree or a session per row, and refuses out loud instead.

// mapDashboard is a page of Map-shaped rows, whose status vocabulary is the one
// that carries abandon.
func mapDashboard(t *testing.T, ids ...string) (QueueDashboard, *bulkKind) {
	t.Helper()
	actions, status := mapVerbs()
	k := &bulkKind{id: ref.KindMap, ids: ids, actions: actions, status: status, log: &bulkLog{}}
	return bulkDashboard(t, k), k
}

// markAll marks every row on the page, which is what the tests below act over.
func markAll(t *testing.T, m QueueDashboard, n int) QueueDashboard {
	t.Helper()
	for range n {
		m = bulkPress(t, m, selKeyTab())
	}
	return m
}

// Unmute takes no input at all, so one `u` answers for the whole Selection: the
// confirmation is the only question, and every marked row is cleared through the
// same work.Muter seam the single row uses. It is reached from the Mute menu,
// beside the windows that set a mute (ADR-0236 decision 5).
func TestWorkBulkUnmuteClearsEverySelectedRow(t *testing.T) {
	actions, status := setVerbs()
	k := &bulkKind{
		id: ref.KindTaskSet, ids: []string{"set-a", "set-b", "set-c"},
		actions: actions, status: status, muted: mutedUntilExample, log: &bulkLog{},
	}
	m := markAll(t, bulkDashboard(t, k), 3)

	m = bulkPress(t, m, selKeyRune('m'))
	m = bulkPress(t, m, selKeyRune('u'))

	if m.bulkPrompt == nil {
		t.Fatal("a bulk unmute ran without a confirmation")
	}
	if got := ui.StripANSI(m.mainHint()); got != ui.StripANSI(ui.ConfirmPrompt("unmute 3 rows")) {
		t.Fatalf("hint line = %q, want one question naming the verb and the whole set", got)
	}
	if len(k.log.performed) != 0 {
		t.Fatalf("the prompt unmuted before it was answered: %v", k.log.performed)
	}

	m = bulkPress(t, m, selKeyRune('y'))

	want := []string{"set-a/unmute", "set-b/unmute", "set-c/unmute"}
	if !slices.Equal(k.log.performed, want) {
		t.Fatalf("unmuted %v, want every marked row (%v)", k.log.performed, want)
	}
	if m.selection.Active() {
		t.Fatal("a successful unmute left the marks behind")
	}
	if got := m.flash.Text(); got != "unmute 3 rows" {
		t.Fatalf("flash = %q, want the run's own report", got)
	}
}

// Abandon is a confirmation and nothing else, so the set answers it once: the
// bulk prompt *is* the modal, and every marked Map is written behind it.
func TestWorkBulkAbandonConfirmsOnceAndAbandonsEveryRow(t *testing.T) {
	m, k := mapDashboard(t, "map-a", "map-b", "map-c")
	m = markAll(t, m, 3)

	m = bulkPress(t, m, selKeyRune('s'))
	m = bulkPress(t, m, selKeyRune('a'))

	if m.bulkPrompt == nil {
		t.Fatal("a bulk abandon ran without a confirmation")
	}
	if len(m.bulkPrompt.rows) != 3 {
		t.Fatalf("the question named %d rows, want the whole Selection", len(m.bulkPrompt.rows))
	}
	if got := ui.StripANSI(m.mainHint()); got != ui.StripANSI(ui.ConfirmPrompt("abandon 3 rows")) {
		t.Fatalf("hint line = %q, want one question for the whole set", got)
	}

	m = bulkPress(t, m, selKeyRune('y'))

	want := []string{"map-a/abandon", "map-b/abandon", "map-c/abandon"}
	if !slices.Equal(k.log.performed, want) {
		t.Fatalf("abandoned %v, want every marked row (%v)", k.log.performed, want)
	}
	if m.selection.Active() {
		t.Fatal("a successful abandon left the marks behind")
	}
}

// One failing row does not abort an abandon and leaves exactly itself marked:
// the plural modals reuse the bulk loop, its collapse and its flash rather than
// growing reporting of their own.
func TestWorkBulkAbandonReusesTheFailureCollapse(t *testing.T) {
	m, k := mapDashboard(t, "map-a", "map-b")
	k.fail = map[string]string{"map-b": "map.md is read-only"}
	m = markAll(t, m, 2)

	m = bulkPress(t, m, selKeyRune('s'))
	m = bulkPress(t, m, selKeyRune('a'))
	m = bulkPress(t, m, selKeyRune('y'))

	if len(k.log.performed) != 2 {
		t.Fatalf("performed %v, want the failure not to abort the run", k.log.performed)
	}
	if got := m.flash.Text(); got != "abandon 1 row · 1 failed: map-b: map.md is read-only" {
		t.Fatalf("flash = %q, want the one reason when exactly one row failed", got)
	}
	if m.selection.Len() != 1 || !m.selection.Has("pop\x00map-b") {
		t.Fatalf("Selection = %d marks, want the failed row alone", m.selection.Len())
	}
}

// The plural modals count their target set: the Mute menu's rule says how many
// rows the window it is about to pick will land on, and one confirmation
// follows — not one per row.
func TestWorkBulkMuteAsksOnceForTheWholeSet(t *testing.T) {
	m, k := setDashboard(t, "set-a", "set-b", "set-c")
	m = markAll(t, m, 3)

	m = bulkPress(t, m, selKeyRune('m'))

	lines := ui.StripANSI(strings.Join(dashboardMuteMenuLines(m.menu.mute, m.menu.target(), 120), "\n"))
	if !strings.Contains(lines, "mute · 3 selected") {
		t.Fatalf("Mute menu = %q, want one menu naming all three rows on its rule", lines)
	}

	m = bulkPress(t, m, selKeyRune('3'))

	if m.menu != nil {
		t.Fatal("the Mute menu reopened for a second row")
	}
	if got := ui.StripANSI(m.mainHint()); got != ui.StripANSI(ui.ConfirmPrompt("mute 3 rows")) {
		t.Fatalf("hint line = %q, want one question for all three rows", got)
	}

	m = bulkPress(t, m, selKeyRune('y'))

	if len(k.log.muted) != 3 {
		t.Fatalf("muted %v, want the one window on all three rows", k.log.muted)
	}
	if m.bulkPrompt != nil {
		t.Fatal("a second question followed the answer")
	}
}

// Every verb the surface intercepts for a per-row modal refuses in selection
// mode, and names itself while doing it. dispatchVerb is the gate they all pass,
// so this is where the whole audit is exercised.
func TestWorkSelectionRefusesEveryInterceptedSingularVerb(t *testing.T) {
	for _, tc := range []struct {
		verb work.Verb
		want string
	}{
		{setkind.VerbDrain, "drain"},
		{setkind.VerbVerify, "verify"},
		{setkind.VerbBind, "bind worktree"},
		{setkind.VerbUnbind, "unbind worktree"},
		{setkind.VerbFold, "fold"},
		{setkind.VerbUnpark, "unpark"},
		{setkind.VerbAutoDrain, "auto-drain"},
		{setkind.VerbAssist, "assist"},
		{wayfinder.VerbWork, "work frontier ticket and go"},
		{wayfinder.VerbWorkHere, "work frontier ticket"},
		{wayfinder.VerbFanOut, "fan out frontier and go"},
		{wayfinder.VerbFanOutHere, "fan out frontier"},
		{wayfinder.VerbAssist, "assist the map and go"},
	} {
		t.Run(string(tc.verb), func(t *testing.T) {
			rows := selRows("set-a", "set-b")
			rows[0].Bound, rows[0].Parked = true, true
			m := selDashboard(rows)
			m = markRow(t, m, "set-a")

			updated, cmd := m.dispatchVerb(tc.verb, rows[0])
			got := updated.(QueueDashboard)

			if cmd != nil {
				t.Fatal("a refused verb scheduled work anyway")
			}
			if !strings.Contains(got.flash.Text(), tc.want+" acts on one row") {
				t.Fatalf("flash = %q, want a refusal naming the verb", got.flash.Text())
			}
			if !strings.Contains(got.flash.Text(), "shift+tab clears the 1 selected") {
				t.Fatalf("flash = %q, want the way out of the mode", got.flash.Text())
			}
			if got.bind != nil || got.abandon != nil || got.drainPick != nil {
				t.Fatal("a refused verb opened its modal anyway")
			}
			if !got.selection.Active() {
				t.Fatal("a refusal dropped the Selection")
			}
		})
	}
}

// The flat `I` refuses by the verb it would have run, not by the key: over rows
// that all read `I` as drain, "drain acts on one row" is the sentence that tells
// the human what did not happen.
func TestWorkSelectionRefusalNamesTheRowsOwnVerb(t *testing.T) {
	m, k := setDashboard(t, "set-a", "set-b")
	m = markAll(t, m, 2)

	m = bulkPress(t, m, selKeyRune('I'))

	if !strings.Contains(m.flash.Text(), "drain acts on one row") {
		t.Fatalf("flash = %q, want the refusal to name the row's own verb", m.flash.Text())
	}
	if len(k.log.performed) != 0 {
		t.Fatalf("a refused flat key ran anyway: %v", k.log.performed)
	}
}

// With no Selection open every one of these verbs behaves exactly as it did: the
// modal opens on the cursored row, and the guards that were always there answer
// the rest.
func TestWorkSingleRowInterceptedVerbsAreUnchanged(t *testing.T) {
	rows := selRows("set-a")
	rows[0].Bound = true
	m := selDashboard(rows)

	updated, cmd := m.dispatchVerb(setkind.VerbBind, rows[0])
	bound := updated.(QueueDashboard)
	if bound.bind == nil || bound.bind.row.ID != "set-a" || cmd == nil {
		t.Fatalf("bind modal = %+v, want it open on the cursored row with its load scheduled", bound.bind)
	}
	if bound.flash.Text() != "" {
		t.Fatalf("flash = %q, want no refusal without a Selection", bound.flash.Text())
	}

	updated, _ = m.dispatchVerb(setkind.VerbUnbind, rows[0])
	unbound := updated.(QueueDashboard)
	if unbound.abandon == nil || unbound.abandon.row.ID != "set-a" {
		t.Fatalf("abandon modal = %+v, want the unbind confirmation on the cursored row", unbound.abandon)
	}

	updated, _ = m.dispatchVerb(setkind.VerbUnpark, rows[0])
	if got := updated.(QueueDashboard).flash.Text(); got != "task set is not parked" {
		t.Fatalf("flash = %q, want unpark's own eligibility answer", got)
	}
}
