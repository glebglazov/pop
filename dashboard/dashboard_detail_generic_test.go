package dashboard

import (
	"fmt"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/glebglazov/pop/config"
	"github.com/glebglazov/pop/tasks"
	"github.com/glebglazov/pop/tasks/drain"
	"github.com/glebglazov/pop/tasks/setkind"
	"github.com/glebglazov/pop/ui"
	"github.com/glebglazov/pop/wayfinder"
	"github.com/glebglazov/pop/work"
	"github.com/glebglazov/pop/work/ref"
)

// The detail view is generic over Work kinds: it renders whatever sections and
// items the container carries and asks the owning kind what verbs one item has.
// These tests drive that path from a kind rather than from a task set, so the
// genericity is pinned by something other than the Task-set tests it has to keep
// rendering identically.

// itemVerbKind is a kind whose item verbs can change between asks — the fact a
// menu built on open must exhibit — and which carries prose sections.
type itemVerbKind struct {
	asked int
	extra *work.Action
}

func (k *itemVerbKind) ID() work.KindID                 { return ref.KindTaskSet }
func (k *itemVerbKind) Load() ([]work.Container, error) { return nil, nil }
func (k *itemVerbKind) Less(a, b work.Container) bool   { return a.ID < b.ID }
func (k *itemVerbKind) StatusCell(work.Container) []work.StatusSegment {
	return []work.StatusSegment{{Text: "READY", Tone: work.ToneLabel}}
}
func (k *itemVerbKind) Actions(work.Container) []work.Action       { return nil }
func (k *itemVerbKind) StatusActions(work.Container) []work.Action { return nil }
func (k *itemVerbKind) CopyActions(work.Container) []work.Action   { return nil }

func (k *itemVerbKind) ItemActions(work.Container, work.Item) []work.Action {
	k.asked++
	actions := []work.Action{{Verb: work.VerbCopyName, Key: "y", Label: "copy name"}}
	if k.extra != nil {
		actions = append(actions, *k.extra)
	}
	return actions
}

func (k *itemVerbKind) Perform(c work.Container, item *work.Item, verb work.Verb) (work.Outcome, error) {
	if verb == work.VerbCopyName && item != nil {
		return work.Outcome{Kind: work.OutcomeMessage, Clipboard: c.ID + "#" + item.ID}, nil
	}
	return work.Outcome{}, work.UnknownVerb(k.ID(), verb)
}
func (k *itemVerbKind) Summary([]work.Container) []string { return nil }
func (k *itemVerbKind) Columns() []string                 { return nil }

func genericDetailRow() DashboardRow {
	return DashboardRow{
		Project: "pop", CursorKey: "pop\x00thing", ID: "thing", Kind: ref.KindTaskSet,
		Headline: "2 of 3",
		DetailSections: []work.Section{
			{Title: "Destination", Body: "Somewhere worth going"},
			{Title: "Decisions so far", Body: "One settled\nTwo settled"},
		},
		Items: []work.Item{
			{ID: "01", Title: "First", Type: "AFK", Status: "open"},
			{ID: "02", Title: "Second", Type: "AFK", Status: "open", Blocked: true, BlockedBy: []string{"01"}},
		},
	}
}

func genericDetailDashboard(kind work.Kind) QueueDashboard {
	d := &drain.Deps{Kinds: func(*drain.Deps, *config.Config) []work.Kind { return []work.Kind{kind} }}
	m := newQueueDashboard(d, &config.Config{}, DashboardSnapshot{Containers: []DashboardRow{genericDetailRow()}})
	m.width, m.height = 120, 24
	return m
}

// TestDetailSectionsRenderAboveTheItemList covers the kind-authored prose: every
// section renders, in order, above the item table, and the container's headline
// rides its header.
func TestDetailSectionsRenderAboveTheItemList(t *testing.T) {
	m := genericDetailDashboard(&itemVerbKind{})
	updated, _ := m.Update(tea.KeyPressMsg{Code: 'l', Text: "l"})
	view := updated.(QueueDashboard).View().Content
	lines := strings.Split(view, "\n")

	idx := func(want string) int { return dashboardTestLineIndex(lines, want) }
	for _, want := range []string{"Destination", "Somewhere worth going", "Decisions so far", "One settled", "Two settled"} {
		if idx(want) < 0 {
			t.Fatalf("detail missing section text %q:\n%s", want, view)
		}
	}
	if !strings.Contains(view, "2 of 3") {
		t.Fatalf("detail header missing the container headline:\n%s", view)
	}
	order := []string{"Destination", "Somewhere worth going", "Decisions so far", "One settled", "STATUS", "First"}
	for i := 1; i < len(order); i++ {
		if idx(order[i-1]) >= idx(order[i]) {
			t.Fatalf("%q should render above %q:\n%s", order[i-1], order[i], view)
		}
	}
}

// TestDetailWithSectionsClampsToBodyHeight covers the budget the sections share
// with the item list: a short terminal cuts the prose (marking the cut) rather
// than overflowing or squeezing the list away.
func TestDetailWithSectionsClampsToBodyHeight(t *testing.T) {
	row := genericDetailRow()
	row.DetailSections = []work.Section{{Title: "Destination", Body: strings.Repeat("a long thought\n", 30)}}
	d := &drain.Deps{Kinds: func(*drain.Deps, *config.Config) []work.Kind { return []work.Kind{&itemVerbKind{}} }}
	m := newQueueDashboard(d, &config.Config{}, DashboardSnapshot{Containers: []DashboardRow{row}})
	m.width, m.height = 120, 12
	updated, _ := m.Update(tea.KeyPressMsg{Code: 'l', Text: "l"})
	got := updated.(QueueDashboard)

	view := got.viewDetail()
	lines := strings.Split(view, "\n")
	if len(lines) != m.height {
		t.Fatalf("detail line count = %d, want %d (clamped to body height):\n%s", len(lines), m.height, view)
	}
	if !strings.Contains(view, "…") {
		t.Fatalf("elided sections should be marked:\n%s", view)
	}
	if !strings.Contains(view, "First") {
		t.Fatalf("the item list must survive the prose:\n%s", view)
	}
}

// TestItemMenuIsBuiltOnOpenFromTheKind pins the same laziness the container menu
// has: no kind is asked for item verbs while the detail is only listing items,
// the owning kind is asked when the menu opens, and a verb that became eligible
// since the container was built shows up without a rebuild.
func TestItemMenuIsBuiltOnOpenFromTheKind(t *testing.T) {
	kind := &itemVerbKind{}
	m := genericDetailDashboard(kind)
	updated, _ := m.Update(tea.KeyPressMsg{Code: 'l', Text: "l"})
	got := updated.(QueueDashboard)
	got.View()
	if kind.asked != 0 {
		t.Fatalf("kind asked for item verbs %d times while only listing items, want 0", kind.asked)
	}

	updated, _ = got.Update(tea.KeyPressMsg{Code: 'r', Text: "r"})
	got = updated.(QueueDashboard)
	if got.itemMenu == nil || len(got.itemMenu.list.Items()) != 1 {
		t.Fatalf("item menu = %+v, want the kind's single verb", got.itemMenu)
	}
	if got.itemMenu.item.ID != "01" {
		t.Fatalf("menu opened over %q, want the cursored item 01", got.itemMenu.item.ID)
	}
	if kind.asked == 0 {
		t.Fatal("opening the item menu did not ask the kind for verbs")
	}

	kind.extra = &work.Action{Verb: work.Verb("late"), Key: "L", Label: "late verb"}
	updated, _ = got.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	updated, _ = updated.(QueueDashboard).Update(tea.KeyPressMsg{Code: 'r', Text: "r"})
	got = updated.(QueueDashboard)
	if keys := itemMenuKeys(got.itemMenu); len(keys) != 2 || keys[1] != "L" {
		t.Fatalf("item menu keys = %v, want the verb the kind added since the build", keys)
	}
}

// TestDetailItemMenuIsBottomChrome pins the detail half of ADR-0224 decision 4:
// a kind-authored item menu uses the same reserved Block as the table menu. It
// names its item, ends above the hint line, takes exactly its own height from the
// list, and gives that height back when it closes.
func TestDetailItemMenuIsBottomChrome(t *testing.T) {
	row := genericDetailRow()
	row.DetailSections = nil
	row.Items = make([]work.Item, 20)
	for i := range row.Items {
		row.Items[i] = work.Item{
			ID:     fmt.Sprintf("item-%02d", i),
			Title:  fmt.Sprintf("Item %02d", i),
			Type:   "AFK",
			Status: "open",
		}
	}
	d := &drain.Deps{Kinds: func(*drain.Deps, *config.Config) []work.Kind {
		return []work.Kind{&itemVerbKind{}}
	}}
	m := newQueueDashboard(d, &config.Config{}, DashboardSnapshot{Containers: []DashboardRow{row}})
	m.width, m.height = 120, 18
	updated, _ := m.Update(tea.KeyPressMsg{Code: 'l', Text: "l"})
	m = updated.(QueueDashboard)
	m.detail.list.SetCursor(len(row.Items) - 1)

	before := m.View().Content
	beforeRows := append([]string(nil), m.detail.list.VisibleRows()...)
	updated, _ = m.Update(tea.KeyPressMsg{Code: 'r', Text: "r"})
	m = updated.(QueueDashboard)
	if m.itemMenu == nil {
		t.Fatal("r did not open the detail run menu")
	}

	during := m.View().Content
	duringRows := append([]string(nil), m.detail.list.VisibleRows()...)
	block := itemMenuLines(m.itemMenu, m.width)
	lines := strings.Split(during, "\n")
	ruleAt := dashboardTestLineIndex(lines, "run · item-19")
	if ruleAt < 0 {
		t.Fatalf("item menu rule does not name its target:\n%s", ui.StripANSI(during))
	}
	if want := len(lines) - 1 - len(block); ruleAt != want {
		t.Fatalf("item menu rule at line %d, want %d so its block ends above the hint line:\n%s", ruleAt, want, ui.StripANSI(during))
	}
	if got, want := len(beforeRows)-len(duringRows), len(block); got != want {
		t.Fatalf("detail body lost %d rows, want the menu block's %d lines", got, want)
	}
	if !strings.Contains(ui.StripANSI(duringRows[len(duringRows)-1]), "item-19") {
		t.Fatalf("cursored item left the body after the menu opened: %v", duringRows)
	}

	updated, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	m = updated.(QueueDashboard)
	if m.itemMenu != nil {
		t.Fatal("esc did not close the detail item menu")
	}
	after := m.View().Content
	afterRows := m.detail.list.VisibleRows()
	if len(afterRows) != len(beforeRows) {
		t.Fatalf("detail body restored to %d rows, want %d", len(afterRows), len(beforeRows))
	}
	if got, want := len(strings.Split(after, "\n")), len(strings.Split(before, "\n")); got != want {
		t.Fatalf("restored detail has %d screen rows, want %d", got, want)
	}
}

// TestDocumentPeekItemMenuIsBottomChrome covers the nested detail state that
// has no ui.List: its menu still uses the shared Frame Block, and its document
// window yields and restores exactly the Block's height.
func TestDocumentPeekItemMenuIsBottomChrome(t *testing.T) {
	m := genericDetailDashboard(&itemVerbKind{})
	m.height = 14
	updated, _ := m.Update(tea.KeyPressMsg{Code: 'l', Text: "l"})
	m = updated.(QueueDashboard)
	var text strings.Builder
	for i := 0; i < 20; i++ {
		fmt.Fprintf(&text, "line-%02d\n", i)
	}
	m.detail.peek = &documentPeek{
		itemID: "01",
		title:  "Document · 01",
		path:   "/tmp/01.txt",
		text:   text.String(),
	}

	before := m.View().Content
	beforeVisible := strings.Count(before, "line-")
	updated, _ = m.Update(tea.KeyPressMsg{Code: 'r', Text: "r"})
	m = updated.(QueueDashboard)
	if m.itemMenu == nil || !m.itemMenu.inPeek {
		t.Fatal("r did not open the Document peek run menu")
	}

	during := m.View().Content
	block := itemMenuLines(m.itemMenu, m.width)
	lines := strings.Split(during, "\n")
	ruleAt := dashboardTestLineIndex(lines, "run · 01")
	if want := len(lines) - 1 - len(block); ruleAt != want {
		t.Fatalf("Document peek menu rule at line %d, want %d above the hint line:\n%s", ruleAt, want, ui.StripANSI(during))
	}
	if got := beforeVisible - strings.Count(during, "line-"); got != len(block) {
		t.Fatalf("Document peek body lost %d rows, want the menu block's %d lines", got, len(block))
	}

	updated, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	m = updated.(QueueDashboard)
	after := m.View().Content
	if got := strings.Count(after, "line-"); got != beforeVisible {
		t.Fatalf("Document peek body restored to %d rows, want %d", got, beforeVisible)
	}
}

// TestItemCopyNamePayloadComesFromTheKind pins that the clipboard reference is
// the kind's answer rather than a shape the dashboard assumes.
func TestItemCopyNamePayloadComesFromTheKind(t *testing.T) {
	m := genericDetailDashboard(&itemVerbKind{})
	updated, _ := m.update(tea.KeyPressMsg{Code: 'l', Text: "l"})
	got := updated.(QueueDashboard)
	var captured string
	got.copyFunc = func(s string) error { captured = s; return nil }

	updated, cmd := got.update(tea.KeyPressMsg{Code: 'y', Text: "y"})
	if cmd != nil {
		t.Fatal("y should not schedule a command")
	}
	if captured != "thing#01" {
		t.Fatalf("copied %q, want the kind's own reference thing#01", captured)
	}
	if msg := updated.(QueueDashboard).detail.flash.Text(); msg != "copied thing#01" {
		t.Fatalf("flash = %q, want the copy confirmation", msg)
	}
}

// TestItemMenuVerbsAreTheOwningKindsOwn pins that the two real kinds fill the
// item menu with their own vocabulary: a task's status writes and a drain.Decision
// ticket's grilling handoff, sharing only copy-name.
func TestItemMenuVerbsAreTheOwningKindsOwn(t *testing.T) {
	kinds := testKinds()
	set := DashboardRow{ID: "2026-07-01-a", RawStatus: tasks.StatusReady}
	wfMap := DashboardRow{ID: "2026-07-02-chart", Kind: ref.KindMap}

	task := work.Item{ID: "01-a", Status: string(tasks.TaskOpen), File: "/repo/tasks/2026-07-01-a/01-a.md"}
	verbs := func(row DashboardRow, item work.Item) []work.Verb {
		var out []work.Verb
		for _, a := range kinds.itemActionsFor(row, item) {
			out = append(out, a.Verb)
		}
		return out
	}
	if got := verbs(set, task); !containsVerb(got, setkind.VerbComplete) || !containsVerb(got, work.VerbCopyName) {
		t.Fatalf("task item verbs = %v, want complete and copy-name", got)
	}
	frontier := work.Item{ID: "01", Status: "open"}
	if got := verbs(wfMap, frontier); !containsVerb(got, wayfinder.VerbWork) || containsVerb(got, setkind.VerbComplete) {
		t.Fatalf("ticket verbs = %v, want the Map's work verb and none of the Task-set's", got)
	}
	blocked := work.Item{ID: "02", Status: "open", Blocked: true, BlockedBy: []string{"01"}}
	if got := verbs(wfMap, blocked); containsVerb(got, wayfinder.VerbWork) {
		t.Fatalf("blocked ticket verbs = %v, want no work verb", got)
	}
}

func containsVerb(verbs []work.Verb, want work.Verb) bool {
	for _, v := range verbs {
		if v == want {
			return true
		}
	}
	return false
}

// TestDetailFollowsTheRebuiltContainer covers the detail's one data path: the
// periodic rebuild that feeds the table also feeds the open detail, so an item
// status that moved on disk shows up without a second loader.
func TestDetailFollowsTheRebuiltContainer(t *testing.T) {
	m := genericDetailDashboard(&itemVerbKind{})
	updated, _ := m.Update(tea.KeyPressMsg{Code: 'l', Text: "l"})
	got := updated.(QueueDashboard)

	refreshed := genericDetailRow()
	refreshed.Items[0].Status = "done"
	refreshed.Items[0].StatusLabel = ""
	updated, _ = got.Update(dashboardRowsMsg{snap: DashboardSnapshot{Containers: []DashboardRow{refreshed}}})
	got = updated.(QueueDashboard)

	item, ok := got.detail.list.Selected()
	if !ok || item.ID != "01" || item.Status != "done" {
		t.Fatalf("detail item after rebuild = %+v, want 01 done with the cursor still on it", item)
	}
	if !strings.Contains(got.View().Content, "done") {
		t.Fatalf("rebuilt status not rendered:\n%s", got.View().Content)
	}
}
