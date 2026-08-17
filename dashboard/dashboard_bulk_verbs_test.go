package dashboard

import (
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/glebglazov/pop/config"
	"github.com/glebglazov/pop/tasks/drain"
	"github.com/glebglazov/pop/ui"
	"github.com/glebglazov/pop/work"
	"github.com/glebglazov/pop/work/ref"
)

// The Work dashboard's plural verbs (ADR-0215 decisions 5, 6 and 7): what the
// menus offer over a Selection, what a bulk run writes, and what is left marked
// afterwards. Every test drives the keys, because a mode is only worth anything
// if the keyboard behaves.

// bulkLog records what the kinds under test were actually asked to do, which is
// the only way to tell "performed every row" from "reported five".
type bulkLog struct {
	performed []string
	muted     []string
}

func (l *bulkLog) note(id string, verb work.Verb) {
	l.performed = append(l.performed, fmt.Sprintf("%s/%s", id, verb))
}

// bulkKind is a wired Work kind whose verbs, capabilities and failures the test
// dictates. It is a whole kind rather than a stub over one method because the
// intersection and the loop both go through the seam, and a fake that answered
// only Perform would leave the menus untested.
type bulkKind struct {
	id      work.KindID
	ids     []string
	actions []work.Action
	status  []work.Action
	// fail names the containers whose Perform refuses, and with what.
	fail map[string]string
	log  *bulkLog
}

func (k *bulkKind) ID() work.KindID { return k.id }

func (k *bulkKind) Load() ([]work.Container, error) {
	rows := make([]work.Container, 0, len(k.ids))
	for _, id := range k.ids {
		rows = append(rows, work.Container{
			Kind: k.id, ID: id, Project: "pop", Status: "READY",
			CursorKey: "pop\x00" + id,
		})
	}
	return rows, nil
}

func (k *bulkKind) Less(a, b work.Container) bool { return a.ID < b.ID }
func (k *bulkKind) Columns() []string {
	return []string{"PROJECT", "TASK SET", "STATUS", "WORKTREE", ""}
}
func (k *bulkKind) Actions(work.Container) []work.Action       { return slices.Clone(k.actions) }
func (k *bulkKind) StatusActions(work.Container) []work.Action { return slices.Clone(k.status) }
func (k *bulkKind) ItemActions(work.Container, work.Item) []work.Action {
	return nil
}
func (k *bulkKind) StatusCell(c work.Container) []work.StatusSegment {
	return []work.StatusSegment{{Text: c.Status, Tone: work.ToneLabel}}
}
func (k *bulkKind) Summary(containers []work.Container) []string {
	return []string{work.CountPhrase(len(containers), "row", "rows")}
}

func (k *bulkKind) Perform(c work.Container, item *work.Item, verb work.Verb) (work.Outcome, error) {
	if item != nil {
		return work.Outcome{}, fmt.Errorf("a bulk run must name no item")
	}
	k.log.note(c.ID, verb)
	if msg, ok := k.fail[c.ID]; ok {
		return work.Outcome{}, fmt.Errorf("%s", msg)
	}
	if verb == work.VerbCopyName {
		return work.Outcome{Kind: work.OutcomeMessage, Clipboard: c.ID, Message: "copied " + c.ID}, nil
	}
	return work.Outcome{Kind: work.OutcomeRefresh, Message: string(verb) + " " + c.ID}, nil
}

func (k *bulkKind) Mute(c work.Container, until time.Time, secret bool) (work.Outcome, error) {
	k.log.muted = append(k.log.muted, fmt.Sprintf("%s@%s", c.ID, until.Format(time.RFC3339)))
	return work.Outcome{Kind: work.OutcomeRefresh, Message: "muted " + c.ID}, nil
}

func (k *bulkKind) Unmute(c work.Container) (work.Outcome, error) {
	k.log.note(c.ID, work.VerbUnmute)
	return work.Outcome{Kind: work.OutcomeRefresh, Message: "unmuted " + c.ID}, nil
}

// setVerbs is a Task-set-shaped verb list: one handoff verb that stays singular,
// the shared plural openers, and an archive only this kind offers.
func setVerbs() ([]work.Action, []work.Action) {
	return []work.Action{
			{Verb: work.Verb("drain"), Key: "I", Label: "drain"},
			{Verb: work.VerbShell, Key: "O", Label: "shell"},
			{Verb: work.VerbMute, Key: "m", Label: "mute ▸", Modes: work.Plural},
			{Verb: work.VerbStatus, Key: "s", Label: "status ▸", Modes: work.Plural},
			{Verb: work.Verb("archive"), Key: "x", Label: "archive", Modes: work.Plural},
			{Verb: work.VerbCopyName, Key: "y", Label: "copy name", Modes: work.Plural},
		}, []work.Action{
			{Verb: work.Verb("complete"), Key: "c", Label: "complete", Modes: work.Plural},
			{Verb: work.Verb("open"), Key: "o", Label: "open", Modes: work.Plural},
		}
}

// mapVerbs is a Map-shaped verb list: it shares the two openers and copy-name
// with a task set and nothing else, and its status vocabulary is disjoint.
func mapVerbs() ([]work.Action, []work.Action) {
	return []work.Action{
			{Verb: work.Verb("work"), Key: "I", Label: "work frontier ticket and go"},
			{Verb: work.VerbStatus, Key: "s", Label: "status ▸", Modes: work.Plural},
			{Verb: work.VerbCopyName, Key: "y", Label: "copy name", Modes: work.Plural},
		}, []work.Action{
			{Verb: work.Verb("abandon"), Key: "a", Label: "abandon", Modes: work.Plural},
		}
}

// bulkDashboard opens page A over the wired kinds, through the wiring a launch
// uses, so the menus ask the kinds the same way production does.
func bulkDashboard(t *testing.T, kinds ...work.Kind) QueueDashboard {
	t.Helper()
	d := &drain.Deps{Kinds: func(*drain.Deps, *config.Config) []work.Kind { return kinds }}
	d.ViewPreset, _ = config.ShippedWorkViewPreset("all")
	cfg := &config.Config{}
	snap, err := BuildPageSnapshot(d, cfg, PageWork, work.PaneFacts{})
	if err != nil {
		t.Fatalf("BuildPageSnapshot: %v", err)
	}
	m := NewDashboardOn(d, cfg, snap, PageWork)
	m.width, m.height = 120, 40
	m.cols.width = m.width
	m.cols.refit()
	m.resizeMainList()
	return m
}

// setDashboard is the common case: one kind holding the named rows.
func setDashboard(t *testing.T, ids ...string) (QueueDashboard, *bulkKind) {
	t.Helper()
	actions, status := setVerbs()
	k := &bulkKind{id: ref.KindTaskSet, ids: ids, actions: actions, status: status, log: &bulkLog{}}
	return bulkDashboard(t, k), k
}

// bulkPress feeds one key and runs whatever command it produced back into the
// model, which is how a bulk verb's own results arrive: the loop runs in a
// tea.Cmd, exactly as one Perform does.
func bulkPress(t *testing.T, m QueueDashboard, msg tea.KeyMsg) QueueDashboard {
	t.Helper()
	updated, cmd := m.Update(msg)
	got, ok := updated.(QueueDashboard)
	if !ok {
		t.Fatalf("key %v took the model out of the dashboard", msg)
	}
	if cmd == nil {
		return got
	}
	produced := cmd()
	bulk, ok := produced.(dashboardBulkVerbMsg)
	if !ok {
		return got
	}
	updated, _ = got.Update(bulk)
	return updated.(QueueDashboard)
}

// menuLabels is what the open menu is offering, submenu included.
func menuLabels(m QueueDashboard) []string {
	var out []string
	if m.menu == nil {
		return nil
	}
	if m.menu.status != nil {
		for _, action := range m.menu.status.list.Items() {
			out = append(out, action.Label)
		}
		return out
	}
	for _, item := range m.menu.list.Items() {
		out = append(out, item.label)
	}
	return out
}

// TestWorkBulkMenuListsOnlyWhatEveryRowOffers drives the intersection: over a
// Selection holding one row of each kind the menu shows the verbs both kinds
// offer and both declare plural — never a union, because pressing `archive` on
// two rows and silently affecting one is the failure the rule exists to prevent.
func TestWorkBulkMenuListsOnlyWhatEveryRowOffers(t *testing.T) {
	setActions, setStatus := setVerbs()
	mapActions, mapStatus := mapVerbs()
	log := &bulkLog{}
	sets := &bulkKind{id: ref.KindTaskSet, ids: []string{"set-a"}, actions: setActions, status: setStatus, log: log}
	maps := &bulkKind{id: ref.KindMap, ids: []string{"map-a"}, actions: mapActions, status: mapStatus, log: log}
	m := bulkDashboard(t, sets, maps)

	m = bulkPress(t, m, selKeyTab())
	m = bulkPress(t, m, selKeyTab())
	if m.selection.Len() != 2 {
		t.Fatalf("marked %d rows, want both", m.selection.Len())
	}

	m = bulkPress(t, m, selKeyRune('a'))
	if m.menu == nil || !m.menu.plural {
		t.Fatal("`a` did not open the plural action menu")
	}
	want := []string{"status ▸ (2 rows)", "copy name (2 rows)"}
	if got := menuLabels(m); !slices.Equal(got, want) {
		t.Fatalf("menu = %v, want %v — the intersection, each item naming its count", got, want)
	}
	// mute is plural but only one kind offers it; archive is offered by one kind
	// only; shell is offered by one and is singular anyway.
	for _, absent := range []work.Verb{work.VerbMute, work.Verb("archive"), work.VerbShell, work.Verb("drain")} {
		for _, item := range m.menu.list.Items() {
			if item.verb == absent {
				t.Fatalf("%s reached a menu over rows that do not all offer it plurally", absent)
			}
		}
	}
	// The title carries the count too, for the reader who never looks at an item.
	lines := ui.StripANSI(strings.Join(dashboardMenuLines(m.menu, 200, livePaneCache{}), "\n"))
	if !strings.Contains(lines, "actions (2 rows)") {
		t.Fatalf("menu title does not name its targets:\n%s", lines)
	}
}

// An empty intersection is not an empty menu: nothing opens and the surface says
// why, which is the only answer that tells the human what to change.
func TestWorkBulkMenuRefusesWhenTheRowsShareNothing(t *testing.T) {
	log := &bulkLog{}
	left := &bulkKind{id: ref.KindTaskSet, ids: []string{"set-a"}, log: log,
		actions: []work.Action{{Verb: work.Verb("archive"), Key: "x", Label: "archive", Modes: work.Plural}}}
	right := &bulkKind{id: ref.KindMap, ids: []string{"map-a"}, log: log,
		actions: []work.Action{{Verb: work.Verb("abandon"), Key: "a", Label: "abandon", Modes: work.Plural}}}
	m := bulkDashboard(t, left, right)

	m = bulkPress(t, m, selKeyTab())
	m = bulkPress(t, m, selKeyTab())
	m = bulkPress(t, m, selKeyRune('a'))

	if m.menu != nil {
		t.Fatal("an empty intersection opened a menu")
	}
	if got := m.flash.Text(); got != "no verb applies to all 2 rows" {
		t.Fatalf("flash = %q, want a reason naming the count", got)
	}
	if !m.selection.Active() {
		t.Fatal("a refusal dropped the Selection")
	}
}

// The status submenu intersects by the same rule, and says so when two kinds'
// status vocabularies share no word.
func TestWorkBulkStatusSubmenuIntersects(t *testing.T) {
	m, _ := setDashboard(t, "set-a", "set-b")
	m = bulkPress(t, m, selKeyTab())
	m = bulkPress(t, m, selKeyTab())
	m = bulkPress(t, m, selKeyRune('a'))
	m = bulkPress(t, m, selKeyRune('s'))

	if m.menu == nil || m.menu.status == nil {
		t.Fatal("`s` did not open the status submenu over the Selection")
	}
	want := []string{"complete (2 rows)", "open (2 rows)"}
	if got := menuLabels(m); !slices.Equal(got, want) {
		t.Fatalf("submenu = %v, want %v", got, want)
	}
	lines := ui.StripANSI(strings.Join(dashboardMenuLines(m.menu, 200, livePaneCache{}), "\n"))
	if !strings.Contains(lines, "status (2 rows)") {
		t.Fatalf("submenu title does not name its targets:\n%s", lines)
	}

	// A Map and a task set both offer the opener and neither shares a status word
	// with the other, which is exactly the case the intersection exists for.
	setActions, setStatus := setVerbs()
	mapActions, mapStatus := mapVerbs()
	log := &bulkLog{}
	mixed := bulkDashboard(t,
		&bulkKind{id: ref.KindTaskSet, ids: []string{"set-a"}, actions: setActions, status: setStatus, log: log},
		&bulkKind{id: ref.KindMap, ids: []string{"map-a"}, actions: mapActions, status: mapStatus, log: log},
	)
	mixed = bulkPress(t, mixed, selKeyTab())
	mixed = bulkPress(t, mixed, selKeyTab())
	mixed = bulkPress(t, mixed, selKeyRune('a'))
	mixed = bulkPress(t, mixed, selKeyRune('s'))

	if mixed.menu != nil {
		t.Fatal("an empty status intersection opened a submenu")
	}
	if got := mixed.flash.Text(); got != "no status verb applies to all 2 rows" {
		t.Fatalf("flash = %q, want a reason naming the count", got)
	}
	if len(log.performed) != 0 {
		t.Fatalf("a refused submenu wrote something: %v", log.performed)
	}
}

// The headline: mark rows, choose a status verb, confirm, and every marked row is
// written — each through its own container's Perform, with no item and no batch
// method anywhere on the seam.
func TestWorkBulkStatusVerbWritesEverySelectedRow(t *testing.T) {
	m, k := setDashboard(t, "set-a", "set-b", "set-c")
	for range 3 {
		m = bulkPress(t, m, selKeyTab())
	}
	m = bulkPress(t, m, selKeyRune('a'))
	m = bulkPress(t, m, selKeyRune('s'))
	m = bulkPress(t, m, selKeyRune('c'))

	// Nothing is written until the question on the hint line is answered.
	if m.bulkPrompt == nil {
		t.Fatal("a bulk write ran without a confirmation")
	}
	if len(k.log.performed) != 0 {
		t.Fatalf("the prompt wrote before it was answered: %v", k.log.performed)
	}
	if got := ui.StripANSI(m.mainHint()); got != ui.StripANSI(ui.ConfirmPrompt("complete 3 rows")) {
		t.Fatalf("hint line = %q, want the y/N question naming the verb and the count", got)
	}
	if m.menu != nil {
		t.Fatal("the menu survived a bulk verb it dispatched")
	}

	m = bulkPress(t, m, selKeyRune('y'))

	want := []string{"set-a/complete", "set-b/complete", "set-c/complete"}
	if !slices.Equal(k.log.performed, want) {
		t.Fatalf("performed %v, want %v — one Perform per container", k.log.performed, want)
	}
	if m.selection.Active() {
		t.Fatalf("a fully successful run left %d rows marked", m.selection.Len())
	}
	if got := m.flash.Text(); got != "complete 3 rows" {
		t.Fatalf("flash = %q, want the run's own report", got)
	}
	if m.bulkPrompt != nil {
		t.Fatal("the prompt outlived its answer")
	}
}

// Answering `n` writes nothing and keeps the whole Selection: nothing happened,
// so nothing is consumed.
func TestWorkBulkPromptDeclinedKeepsTheSelection(t *testing.T) {
	m, k := setDashboard(t, "set-a", "set-b")
	m = bulkPress(t, m, selKeyTab())
	m = bulkPress(t, m, selKeyTab())
	m = bulkPress(t, m, selKeyRune('a'))
	m = bulkPress(t, m, selKeyRune('x'))
	if m.bulkPrompt == nil {
		t.Fatal("archive over a Selection asked nothing")
	}

	m = bulkPress(t, m, selKeyRune('n'))

	if m.bulkPrompt != nil {
		t.Fatal("`n` left the prompt open")
	}
	if len(k.log.performed) != 0 {
		t.Fatalf("`n` wrote %v", k.log.performed)
	}
	if m.selection.Len() != 2 {
		t.Fatalf("%d rows still marked, want the whole Selection", m.selection.Len())
	}
	// Esc backs out the same way.
	m = bulkPress(t, m, selKeyRune('a'))
	m = bulkPress(t, m, selKeyRune('x'))
	m = bulkPress(t, m, tea.KeyPressMsg{Code: tea.KeyEscape})
	if m.bulkPrompt != nil || len(k.log.performed) != 0 || m.selection.Len() != 2 {
		t.Fatal("esc did not back out of the prompt cleanly")
	}
}

// One row failing does not abort the run, and what is left marked afterwards is
// exactly what failed — so the retry needs no re-marking.
func TestWorkBulkFailureLeavesExactlyTheFailedRowsMarked(t *testing.T) {
	m, k := setDashboard(t, "set-a", "set-b", "set-c")
	k.fail = map[string]string{"set-b": "manifest is locked"}
	for range 3 {
		m = bulkPress(t, m, selKeyTab())
	}
	m = bulkPress(t, m, selKeyRune('a'))
	m = bulkPress(t, m, selKeyRune('x'))
	m = bulkPress(t, m, selKeyRune('y'))

	want := []string{"set-a/archive", "set-b/archive", "set-c/archive"}
	if !slices.Equal(k.log.performed, want) {
		t.Fatalf("performed %v, want %v — a failure must not abort the loop", k.log.performed, want)
	}
	if got := m.flash.Text(); got != "archive 2 rows · 1 failed: set-b: manifest is locked" {
		t.Fatalf("flash = %q, want the one reason when exactly one row failed", got)
	}
	if m.selection.Len() != 1 || !m.selection.Has("pop\x00set-b") {
		t.Fatalf("Selection = %d marks, want the failed row alone", m.selection.Len())
	}
	if got := m.list.RegionCount(); got != 1 {
		t.Fatalf("region holds %d rows, want the failed row still lifted", got)
	}
}

// Several failures flash a bare count: five stacked reasons are unreadable, and
// the collapse surfaces them one at a time as the set shrinks.
func TestWorkBulkSeveralFailuresFlashABareCount(t *testing.T) {
	m, k := setDashboard(t, "set-a", "set-b", "set-c")
	k.fail = map[string]string{"set-a": "locked", "set-b": "gone"}
	for range 3 {
		m = bulkPress(t, m, selKeyTab())
	}
	m = bulkPress(t, m, selKeyRune('a'))
	m = bulkPress(t, m, selKeyRune('x'))
	m = bulkPress(t, m, selKeyRune('y'))

	if got := m.flash.Text(); got != "archive 1 row · 2 failed" {
		t.Fatalf("flash = %q, want a bare count when several rows failed", got)
	}
	if m.selection.Len() != 2 {
		t.Fatalf("Selection = %d marks, want both failures", m.selection.Len())
	}
	// Every row was still performed, the failures included.
	if len(k.log.performed) != 3 {
		t.Fatalf("performed %v, want all three rows", k.log.performed)
	}
}

// `y` over a Selection copies every marked name, newline-joined in the region's
// order. It asks nothing first: copying writes no container, and a mistaken copy
// costs one keypress.
func TestWorkBulkCopyNameJoinsEverySelectedName(t *testing.T) {
	m, _ := setDashboard(t, "set-a", "set-b", "set-c")
	var copied []string
	m.copyFunc = func(payload string) error { copied = append(copied, payload); return nil }
	m = bulkPress(t, m, selKeyTab())
	m = bulkPress(t, m, selKeyRune('j'))
	m = bulkPress(t, m, selKeyTab()) // marks set-c, so the marks are made out of order

	m = bulkPress(t, m, selKeyRune('y'))

	if m.bulkPrompt != nil {
		t.Fatal("a copy asked for confirmation")
	}
	if len(copied) != 1 || copied[0] != "set-a\nset-c" {
		t.Fatalf("clipboard = %q, want both names newline-joined in the region's order", copied)
	}
	if got := m.flash.Text(); got != "copied 2 names" {
		t.Fatalf("flash = %q, want the copy's own report", got)
	}
	if m.selection.Active() {
		t.Fatal("a successful copy left the marks behind")
	}
}

// Single-row behaviour is untouched: the same verb from the same menu writes at
// once, with no count in its label and no question on the hint line.
func TestWorkSingleRowVerbIsUnchanged(t *testing.T) {
	m, k := setDashboard(t, "set-a", "set-b")

	m = bulkPress(t, m, selKeyRune('a'))
	if m.menu == nil || m.menu.plural {
		t.Fatal("`a` over no Selection opened a plural menu")
	}
	if got := menuLabels(m); slices.ContainsFunc(got, func(l string) bool { return strings.Contains(l, "rows") }) {
		t.Fatalf("menu = %v, want the singular labels unchanged", got)
	}

	m = bulkPress(t, m, selKeyRune('x'))

	if m.bulkPrompt != nil {
		t.Fatal("a single-row verb asked for confirmation")
	}
	if !slices.Equal(k.log.performed, []string{"set-a/archive"}) {
		t.Fatalf("performed %v, want the cursored row alone, at once", k.log.performed)
	}
}

// A verb the rows offer but no kind declared plural refuses out loud from inside
// the plural menu: a key that goes silently inert is indistinguishable from a bug.
func TestWorkBulkMenuRefusesASingularVerbsHotkey(t *testing.T) {
	m, k := setDashboard(t, "set-a", "set-b")
	m = bulkPress(t, m, selKeyTab())
	m = bulkPress(t, m, selKeyTab())
	m = bulkPress(t, m, selKeyRune('a'))

	m = bulkPress(t, m, selKeyRune('I'))

	if !strings.Contains(m.flash.Text(), "drain acts on one row") {
		t.Fatalf("flash = %q, want a refusal naming the verb", m.flash.Text())
	}
	if !strings.Contains(m.flash.Text(), "shift+tab clears the 2 selected") {
		t.Fatalf("flash = %q, want the way out of the mode", m.flash.Text())
	}
	if len(k.log.performed) != 0 {
		t.Fatalf("a refused verb ran anyway: %v", k.log.performed)
	}
	if m.menu == nil || m.bulkPrompt != nil {
		t.Fatal("a refusal disturbed the menu")
	}
}

// Mute is the one plural verb whose modal takes input: the window is picked
// once, in the surface's own submenu, and answers identically for every marked
// row (ADR-0215 decision 5).
func TestWorkBulkMuteAppliesOneWindowToEveryRow(t *testing.T) {
	m, k := setDashboard(t, "set-a", "set-b")
	m = bulkPress(t, m, selKeyTab())
	m = bulkPress(t, m, selKeyTab())
	m = bulkPress(t, m, selKeyRune('a'))
	m = bulkPress(t, m, selKeyRune('m'))
	if m.menu == nil || m.menu.mute == nil {
		t.Fatal("`m` did not open the mute submenu over the Selection")
	}
	window := m.menu.mute.list.Items()[1]

	m = bulkPress(t, m, selKeyRune('2'))
	if m.bulkPrompt == nil {
		t.Fatal("a bulk mute ran without a confirmation")
	}
	if got := ui.StripANSI(m.mainHint()); got != ui.StripANSI(ui.ConfirmPrompt("mute 2 rows")) {
		t.Fatalf("hint line = %q, want the mute's own question", got)
	}

	m = bulkPress(t, m, selKeyRune('y'))

	want := []string{
		"set-a@" + window.Until.Format(time.RFC3339),
		"set-b@" + window.Until.Format(time.RFC3339),
	}
	if !slices.Equal(k.log.muted, want) {
		t.Fatalf("muted %v, want one window on every marked row (%v)", k.log.muted, want)
	}
	if m.selection.Active() {
		t.Fatal("a successful mute left the marks behind")
	}
}

// A menu is opened *by* the Selection, so it cannot be what hides it: the
// counted separator stays under the marked rows and the mode word stays at the
// left of the bottom line, ahead of the menu's own hints. The menu view composes
// its own body and footer rather than going through List and Frame, which is why
// both signals are pinned here as well as in the no-menu twin.
func selectionSignals(t *testing.T, m QueueDashboard, marked int) (view, separator string) {
	t.Helper()
	view = ui.StripANSI(m.View().Content)
	separator = ui.StripANSI(ui.SelectionSeparator(marked, m.width))
	if !strings.Contains(view, separator) {
		t.Fatalf("the separator %q is not on screen:\n%s", separator, view)
	}
	if !strings.Contains(view, ui.SelectionMode) {
		t.Fatalf("the bottom line does not carry %s:\n%s", ui.SelectionMode, view)
	}
	return view, separator
}

func TestWorkBulkMenuKeepsTheSelectionSignals(t *testing.T) {
	m, _ := setDashboard(t, "set-a", "set-b", "set-c", "set-d")
	m = bulkPress(t, m, selKeyTab())
	m = bulkPress(t, m, selKeyTab())
	m = bulkPress(t, m, selKeyRune('a'))
	if m.menu == nil || !m.menu.plural {
		t.Fatal("`a` did not open the plural action menu")
	}

	view, separator := selectionSignals(t, m, 2)

	// The divider sits directly below the marked rows: they are above it, every
	// other row below it, and the menu is below it too — it belongs to the cursor,
	// which is on the rest of the list.
	cut := strings.Index(view, separator)
	before, after := view[:cut], view[cut:]
	if !strings.Contains(before, "set-a") || !strings.Contains(before, "set-b") {
		t.Fatalf("the marked rows are not above the separator:\n%s", view)
	}
	if !strings.Contains(after, "set-c") || !strings.Contains(after, "set-d") {
		t.Fatalf("the unmarked rows are not below the separator:\n%s", view)
	}
	if !strings.Contains(after, "actions (2 rows)") {
		t.Fatalf("the menu moved above the separator:\n%s", view)
	}

	// The mode word leads the bottom line and the menu's hints follow it, spaced.
	lines := strings.Split(strings.TrimRight(view, "\n"), "\n")
	bottom := lines[len(lines)-1]
	if want := ui.SelectionMode + "  j/k move"; !strings.HasPrefix(strings.TrimLeft(bottom, " "), want) {
		t.Fatalf("bottom line = %q, want it to start %q", bottom, want)
	}
}

// Both submenus render through the same overlay path, and a submenu is where the
// write actually happens — the signals matter most there.
func TestWorkBulkSubmenusKeepTheSelectionSignals(t *testing.T) {
	for _, tc := range []struct{ name, key, title string }{
		{"status", "s", "status"},
		{"mute", "m", "mute"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m, _ := setDashboard(t, "set-a", "set-b", "set-c")
			m = bulkPress(t, m, selKeyTab())
			m = bulkPress(t, m, selKeyTab())
			m = bulkPress(t, m, selKeyRune('a'))
			m = bulkPress(t, m, selKeyRune(rune(tc.key[0])))
			if m.menu == nil || !m.menu.nested() {
				t.Fatalf("`%s` did not open the %s submenu", tc.key, tc.name)
			}
			view, _ := selectionSignals(t, m, 2)
			if !strings.Contains(view, tc.title) {
				t.Fatalf("the %s submenu is not on screen:\n%s", tc.name, view)
			}
		})
	}
}

// The two-line table is a second row loop with its own menu placement, so it
// carries the divider separately.
func TestWorkBulkMenuKeepsTheSelectionSignalsInTwoLineMode(t *testing.T) {
	m, _ := setDashboard(t, "set-a", "set-b", "set-c")
	// Below the width threshold and above the height floor is two-line mode.
	m.width = dashboardTwoLineWidthThreshold - 20
	m.cols.width = m.width
	m.cols.refit()
	m.resizeMainList()
	if !m.page.twoLine(m.snap.Containers, m.width, m.height) {
		t.Fatal("the page is not in two-line mode")
	}

	m = bulkPress(t, m, selKeyTab())
	m = bulkPress(t, m, selKeyRune('a'))
	if m.menu == nil || !m.menu.plural {
		t.Fatal("`a` did not open the plural action menu")
	}

	view, separator := selectionSignals(t, m, 1)
	cut := strings.Index(view, separator)
	if !strings.Contains(view[:cut], "set-a") || !strings.Contains(view[cut:], "set-b") {
		t.Fatalf("the divider does not sit between the marked row and the rest:\n%s", view)
	}
}

// With nothing marked the menu view is exactly what it always was: no divider
// and no mode word, because there is no mode.
func TestWorkMenuWithoutASelectionRendersNoSelectionSignals(t *testing.T) {
	m, _ := setDashboard(t, "set-a", "set-b")
	m = bulkPress(t, m, selKeyRune('a'))
	if m.menu == nil || m.menu.plural {
		t.Fatal("`a` did not open the singular action menu")
	}
	view := ui.StripANSI(m.View().Content)
	if strings.Contains(view, "selected") || strings.Contains(view, ui.SelectionMode) {
		t.Fatalf("a menu with nothing marked shows a Selection signal:\n%s", view)
	}
}
