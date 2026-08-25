package dashboard

import (
	"slices"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/glebglazov/pop/config"
	"github.com/glebglazov/pop/tasks"
	"github.com/glebglazov/pop/tasks/drain"
	"github.com/glebglazov/pop/ui"
	"github.com/glebglazov/pop/work"
	"github.com/glebglazov/pop/work/ref"
)

// The Work dashboard's Selection (ADR-0224): tab marks a row, the marks move into
// a region at the foot of the list, and while any row is marked the surface is in
// selection mode — every verb refuses out loud and nothing but navigation acts.
// Every test here drives the keys, because the mode is only worth anything if the
// keyboard behaves.

func selKeyTab() tea.KeyPressMsg      { return tea.KeyPressMsg{Code: tea.KeyTab} }
func selKeyShiftTab() tea.KeyPressMsg { return tea.KeyPressMsg{Code: tea.KeyTab, Mod: tea.ModShift} }
func selKeyEsc() tea.KeyPressMsg      { return tea.KeyPressMsg{Code: tea.KeyEscape, Text: "esc"} }
func selKeyRune(r rune) tea.KeyPressMsg {
	return tea.KeyPressMsg{Code: r, Text: string(r)}
}

// selPress feeds one key and returns the model it produced, dropping the command:
// no key in this file schedules work whose result the assertions read.
func selPress(t *testing.T, m QueueDashboard, msg tea.KeyMsg) QueueDashboard {
	t.Helper()
	updated, _ := m.Update(msg)
	got, ok := updated.(QueueDashboard)
	if !ok {
		t.Fatalf("key %v took the model out of the dashboard", msg)
	}
	return got
}

// selRows is a page of plain task-set rows, one per id, in the order given.
func selRows(ids ...string) []DashboardRow {
	rows := make([]DashboardRow, 0, len(ids))
	for _, id := range ids {
		rows = append(rows, TestDashboardRow("pop", id, DashboardRow{
			RawStatus: tasks.StatusReady, Status: "READY",
			DefPath: "/repo/tasks", StatePath: "/repo/state.json",
		}))
	}
	return rows
}

// selDashboard is a model over those rows, sized for a terminal that shows them
// all.
func selDashboard(rows []DashboardRow) QueueDashboard {
	m := newQueueDashboard(&drain.Deps{}, &config.Config{}, DashboardSnapshot{Containers: rows})
	m.width, m.height = 120, 40
	m.cols.width = m.width
	m.cols.refit()
	m.resizeMainList()
	return m
}

// selIDs is the list in render order — where the rows sit, which is the whole of
// what a mark does to one.
func selIDs(m QueueDashboard) []string {
	ids := make([]string, 0, m.list.Len())
	for _, row := range m.list.Items() {
		ids = append(ids, row.ID)
	}
	return ids
}

// selCursorID is the row the cursor is on.
func selCursorID(t *testing.T, m QueueDashboard) string {
	t.Helper()
	row, ok := m.list.Selected()
	if !ok {
		t.Fatal("the cursor is on no row")
	}
	return row.ID
}

func TestWorkListTopScrollEdgeRidesTheTableRule(t *testing.T) {
	m := selDashboard(selRows("a", "b", "c", "d", "e", "f", "g", "h"))
	m.list.Resize(3)
	m.list.SetCursor(5)

	body := ui.StripANSI(m.mainBody())
	lines := strings.Split(body, "\n")
	if !strings.Contains(lines[m.dashboardChromeLines()-1], "↑ 3") {
		t.Fatalf("table rule has no top Scroll edge:\n%s", body)
	}
	if strings.Contains(ui.StripANSI(strings.Join(m.list.VisibleRows(), "\n")), "↑") {
		t.Fatal("Work List spent a separate row for the top Scroll edge")
	}
}

func TestWorkSelectionDividerCarriesBothScrollEdges(t *testing.T) {
	m := selDashboard(selRows("a", "b", "c", "d", "e", "f", "g", "h", "i", "j"))
	for _, id := range []string{"g", "h", "i", "j"} {
		m = markRow(t, m, id)
	}
	m.list.Resize(6)
	m.list.SetCursor(2)
	m.list.SetCursor(9)

	body := ui.StripANSI(strings.Join(m.list.VisibleRows(), "\n"))
	if !strings.Contains(body, "↓ ") || !strings.Contains(body, "↑ 2") {
		t.Fatalf("Selection divider does not carry both Scroll edges:\n%s", body)
	}
	if strings.Contains(body, "more selected") {
		t.Fatalf("old overflow grammar still renders:\n%s", body)
	}
}

// markRow walks the cursor onto a row by id and marks it, which is the only way a
// human ever makes a Selection.
func markRow(t *testing.T, m QueueDashboard, id string) QueueDashboard {
	t.Helper()
	target := slices.Index(selIDs(m), id)
	if target < 0 {
		t.Fatalf("row %q is not on the list: %v", id, selIDs(m))
	}
	for m.list.Cursor() != target {
		step := 'j'
		if m.list.Cursor() > target {
			step = 'k'
		}
		m = selPress(t, m, selKeyRune(step))
	}
	return selPress(t, m, selKeyTab())
}

func TestWorkSelectionTabMovesTheRowIntoTheFootRegion(t *testing.T) {
	m := selDashboard(selRows("set-a", "set-b", "set-c"))

	m = markRow(t, m, "set-b")

	if got := selIDs(m); !slices.Equal(got, []string{"set-a", "set-c", "set-b"}) {
		t.Fatalf("rows = %v, want the marked row moved to the foot", got)
	}
	if got := m.list.RegionCount(); got != 1 {
		t.Fatalf("region holds %d rows, want the one marked row", got)
	}
	if got := selCursorID(t, m); got != "set-c" {
		t.Fatalf("cursor on %q, want the row that followed the marked one", got)
	}
	if !m.selection.Has("pop\x00set-b") {
		t.Fatal("the marked row is not in the Selection")
	}

	// A second mark joins the first, and the region reads in the list's own order
	// rather than in the order the marks were made.
	m = markRow(t, m, "set-a")
	if got := selIDs(m); !slices.Equal(got, []string{"set-c", "set-a", "set-b"}) {
		t.Fatalf("rows = %v, want the region sorted as the list is, not by mark order", got)
	}
	if got := m.list.RegionCount(); got != 2 {
		t.Fatalf("region holds %d rows, want 2", got)
	}

	// Unmarking sends the row back to its own place rather than to the head of the
	// rest, and every row is on the list exactly once throughout.
	m = markRow(t, m, "set-b")
	if got := selIDs(m); !slices.Equal(got, []string{"set-b", "set-c", "set-a"}) {
		t.Fatalf("rows = %v after unmarking set-b, want it back in its sorted place", got)
	}
	if got := m.list.RegionCount(); got != 1 {
		t.Fatalf("region holds %d rows after an unmark, want 1", got)
	}
}

func TestWorkSelectionShiftTabClearsTheWholeSelection(t *testing.T) {
	m := selDashboard(selRows("set-a", "set-b", "set-c"))
	m = markRow(t, m, "set-b")
	m = markRow(t, m, "set-c")

	m = selPress(t, m, selKeyShiftTab())

	if m.selection.Active() {
		t.Fatalf("%d marks survived shift+tab", m.selection.Len())
	}
	if got := m.list.RegionCount(); got != 0 {
		t.Fatalf("region holds %d rows after the clear, want none", got)
	}
	if got := selIDs(m); !slices.Equal(got, []string{"set-a", "set-b", "set-c"}) {
		t.Fatalf("rows = %v, want the list back in its own order", got)
	}
	if got := m.modeWord(); got != "" {
		t.Fatalf("mode word = %q after the clear, want none", got)
	}
}

func TestWorkSelectionJumpsStopAtTheFootRegion(t *testing.T) {
	unmarked := selDashboard(selRows("set-a", "set-b", "set-c", "set-d", "set-e"))
	unmarked = selPress(t, unmarked, selKeyRune('G'))
	if got := unmarked.list.Cursor(); got != 4 {
		t.Fatalf("G without a Selection put the cursor at %d, want 4", got)
	}
	unmarked = selPress(t, unmarked, selKeyRune('g'))
	unmarked = selPress(t, unmarked, selKeyRune('g'))
	if got := unmarked.list.Cursor(); got != 0 {
		t.Fatalf("gg without a Selection put the cursor at %d, want 0", got)
	}

	m := selDashboard(selRows("set-a", "set-b", "set-c", "set-d", "set-e"))
	m = markRow(t, m, "set-d")
	m = markRow(t, m, "set-e")
	m.list.SetCursor(0)

	m = selPress(t, m, selKeyRune('G'))
	if got := m.list.Cursor(); got != 2 {
		t.Fatalf("G from the ordinary rows put the cursor at %d, want 2", got)
	}
	m = selPress(t, m, selKeyRune('G'))
	if got := m.list.Cursor(); got != 4 {
		t.Fatalf("second G put the cursor at %d, want the last marked row 4", got)
	}

	m = selPress(t, m, selKeyRune('g'))
	m = selPress(t, m, selKeyRune('g'))
	if got := m.list.Cursor(); got != 3 {
		t.Fatalf("gg from the foot region put the cursor at %d, want the first marked row 3", got)
	}
	m = selPress(t, m, selKeyRune('g'))
	m = selPress(t, m, selKeyRune('g'))
	if got := m.list.Cursor(); got != 0 {
		t.Fatalf("second gg put the cursor at %d, want the first row", got)
	}
}

// The mode is visible or it is not a mode: the word on the bottom line and the
// counted separator above the marked rows are the whole of what says a verb
// will refuse. Both are the shared primitive's own words (ADR-0215 decision 3).
func TestWorkSelectionRendersTheModeWordAndCountedSeparator(t *testing.T) {
	m := selDashboard(selRows("set-a", "set-b", "set-c"))
	if strings.Contains(ui.StripANSI(m.View().Content), ui.SelectionMode) {
		t.Fatal("the mode word shows with nothing marked")
	}

	m = markRow(t, m, "set-b")
	m = markRow(t, m, "set-c")

	view := ui.StripANSI(m.View().Content)
	if !strings.Contains(view, ui.SelectionMode) {
		t.Fatalf("the bottom line does not carry %s:\n%s", ui.SelectionMode, view)
	}
	if want := ui.StripANSI(ui.SelectionSeparator(2, m.width)); !strings.Contains(view, want) {
		t.Fatalf("the separator %q is not on screen:\n%s", want, view)
	}
	// The word is padded on both sides, so the hints never butt against it.
	if want := "  " + ui.SelectionMode + "  r run"; !strings.Contains(view, want) {
		t.Fatalf("want %q on the bottom line, evenly spaced:\n%s", want, view)
	}

	// The separator sits under every ordinary row and above the marked rows.
	rows := ui.StripANSI(strings.Join(m.list.VisibleRows(), "\n"))
	sep := strings.Index(rows, ui.StripANSI(ui.SelectionSeparator(2, m.width)))
	if sep < 0 {
		t.Fatalf("no separator in the list body:\n%s", rows)
	}
	if before, after := rows[:sep], rows[sep:]; !strings.Contains(before, "set-a") ||
		!strings.Contains(after, "set-b") || !strings.Contains(after, "set-c") {
		t.Fatalf("the marked rows are not the block below the separator:\n%s", rows)
	}
	visible := m.list.VisibleRows()
	separatorRow := -1
	ordinaryRow := -1
	for i, line := range visible {
		plain := ui.StripANSI(line)
		if strings.Contains(plain, "2 selected") {
			separatorRow = i
		}
		if strings.Contains(plain, "set-a") {
			ordinaryRow = i
		}
	}
	if want := len(visible) - 3; separatorRow != want {
		t.Fatalf("separator row = %d, want the fixed foot row %d", separatorRow, want)
	}
	for i := ordinaryRow + 1; i < separatorRow; i++ {
		if visible[i] != "" {
			t.Fatalf("row %d between the ordinary list and foot = %q, want blank", i, visible[i])
		}
	}

	// A refusal cannot hide the mode: the flash takes the rest of the line.
	m = selPress(t, m, selKeyRune('l'))
	view = ui.StripANSI(m.View().Content)
	if !strings.Contains(view, ui.SelectionMode) || !strings.Contains(view, "acts on one row") {
		t.Fatalf("want both the mode word and the refusal on the bottom line:\n%s", view)
	}
}

// The pane pin keeps its column and its place: the pinned block is above the
// region, a marked row that is also attributed keeps its `▸`, and a pin still
// yields to the narrowings a mark is exempt from (ADR-0209 decision 7 stands for
// pop's own inference).
func TestWorkSelectionLeavesThePanePinAtTheHead(t *testing.T) {
	rows := selRows("set-a", "set-b", "set-c")
	rows[0].Pinned = true
	rows[1].Pinned = true
	// The snapshot builder has already lifted the pinned rows to the top.
	m := selDashboard(rows)

	m = markRow(t, m, "set-b")
	m = markRow(t, m, "set-c")

	if got := selIDs(m); !slices.Equal(got, []string{"set-a", "set-b", "set-c"}) {
		t.Fatalf("rows = %v, want the pinned ordinary row above the marked rows", got)
	}
	m.list.Resize(m.list.Len() + 2)
	body := m.list.VisibleRows()
	if got := ui.StripANSI(body[0]); !strings.HasPrefix(got, " ▸") && !strings.HasPrefix(got, "█▸") {
		t.Fatalf("the pane pin is not at the head of the list: %q", got)
	}
	pinned := -1
	for i, line := range body {
		if strings.Contains(ui.StripANSI(line), "set-a") {
			pinned = i
		}
	}
	if pinned < 0 {
		t.Fatalf("the unmarked pinned row is not on screen:\n%s", strings.Join(body, "\n"))
	}
	if got := ui.StripANSI(body[pinned]); !strings.HasPrefix(got, " ▸") && !strings.HasPrefix(got, "█▸") {
		t.Fatalf("the pin at the head is not marked: %q", got)
	}
}

// A mark outranks the query and a pin does not: the marked row stays on screen
// through a search that excludes it, the pinned row does not, and neither is on
// the list twice.
func TestWorkSelectionOutranksTheSearchAndThePinDoesNot(t *testing.T) {
	rows := selRows("set-a", "set-b", "other-c")
	rows[0].Pinned = true
	m := selDashboard(rows)

	m = markRow(t, m, "set-b")
	m = selPress(t, m, selKeyRune('/'))
	for _, r := range "other" {
		m = selPress(t, m, selKeyRune(r))
	}
	m = selPress(t, m, tea.KeyPressMsg{Code: tea.KeyEnter})

	if got := selIDs(m); !slices.Equal(got, []string{"other-c", "set-b"}) {
		t.Fatalf("rows = %v, want the marked row kept and the pinned row filtered away", got)
	}
	if got := m.list.RegionCount(); got != 1 {
		t.Fatalf("region holds %d rows under a search, want the marked one", got)
	}

	// Clearing the mark hands the row back to the query.
	m = selPress(t, m, selKeyShiftTab())
	if got := selIDs(m); !slices.Equal(got, []string{"other-c"}) {
		t.Fatalf("rows = %v once unmarked, want the query alone to decide", got)
	}
}

// presetKind is a wired Work kind that answers the active Work view preset the
// way a real one does: every container it holds under `all`, and only the ones
// the fixture calls visible under anything else. A preset is the one narrowing a
// surface cannot undo by itself, so this is what a mark has to survive.
type presetKind struct {
	d *drain.Deps
	f *presetFixture
}

// presetFixture is the machine behind the kind: which containers exist at all,
// and which of them the active preset selects.
type presetFixture struct {
	rows   []work.Container
	hidden map[string]bool
}

func (f *presetFixture) drop(id string) {
	f.rows = slices.DeleteFunc(f.rows, func(c work.Container) bool { return c.ID == id })
}

func (k *presetKind) ID() work.KindID { return ref.KindTaskSet }

func (k *presetKind) Load() ([]work.Container, error) {
	if k.d != nil && k.d.ViewPreset.Name == "all" {
		return slices.Clone(k.f.rows), nil
	}
	var out []work.Container
	for _, row := range k.f.rows {
		if !k.f.hidden[row.ID] {
			out = append(out, row)
		}
	}
	return out, nil
}

func (k *presetKind) Less(a, b work.Container) bool { return a.ID < b.ID }
func (k *presetKind) Columns() []string {
	return []string{"PROJECT", "TASK SET", "STATUS", "WORKTREE", ""}
}
func (k *presetKind) Actions(work.Container) []work.Action                { return nil }
func (k *presetKind) StatusActions(work.Container) []work.Action          { return nil }
func (k *presetKind) CopyActions(work.Container) []work.Action            { return nil }
func (k *presetKind) TypeWords() []string                                 { return nil }
func (k *presetKind) ItemActions(work.Container, work.Item) []work.Action { return nil }

func (k *presetKind) StatusCell(c work.Container) []work.StatusSegment {
	return []work.StatusSegment{{Text: c.Status, Tone: work.ToneLabel}}
}

func (k *presetKind) Perform(work.Container, *work.Item, work.Verb) (work.Outcome, error) {
	return work.Outcome{}, nil
}

func (k *presetKind) Summary(containers []work.Container) []string {
	return []string{work.CountPhrase(len(containers), "task set", "task sets")}
}

// presetDashboard opens page A over the fixture, through the wiring a launch
// uses, with an active preset that is not `all`.
func presetDashboard(t *testing.T, f *presetFixture) QueueDashboard {
	t.Helper()
	d := &drain.Deps{Kinds: func(d *drain.Deps, _ *config.Config) []work.Kind {
		return []work.Kind{&presetKind{d: d, f: f}}
	}}
	d.ViewPreset, _ = config.ShippedWorkViewPreset("active")
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

// selPoll is one poll: the model's own reload command and the message it
// produces, which is the only path a running dashboard takes to new rows.
func selPoll(t *testing.T, m QueueDashboard) QueueDashboard {
	t.Helper()
	msg, ok := m.reload()().(dashboardRowsMsg)
	if !ok {
		t.Fatal("reload did not produce a rows message")
	}
	if msg.err != nil {
		t.Fatalf("reload: %v", msg.err)
	}
	updated, _ := m.Update(msg)
	return updated.(QueueDashboard)
}

func TestWorkSelectionOutranksThePreset(t *testing.T) {
	f := &presetFixture{rows: selRows("set-a", "set-b", "set-c"), hidden: map[string]bool{}}
	m := presetDashboard(t, f)

	m = markRow(t, m, "set-b")
	// The preset now hides the marked row, exactly as picking another entry in the
	// filter menu would.
	f.hidden["set-b"] = true
	m = selPoll(t, m)

	if got := selIDs(m); !slices.Equal(got, []string{"set-a", "set-c", "set-b"}) {
		t.Fatalf("rows = %v, want the marked row kept in the region", got)
	}
	if !m.selection.Has("pop\x00set-b") {
		t.Fatal("the preset took the mark with it")
	}
	if got := m.list.RegionCount(); got != 1 {
		t.Fatalf("region holds %d rows, want the marked one", got)
	}
	for _, row := range m.allRows {
		if row.ID == "set-b" {
			t.Fatal("the fixture is not exercising the exemption: the preset still selects set-b")
		}
	}

	// Clearing the mark hands the row back to the preset.
	m = selPress(t, m, selKeyShiftTab())
	if got := selIDs(m); !slices.Equal(got, []string{"set-a", "set-c"}) {
		t.Fatalf("rows = %v once unmarked, want the preset alone to decide", got)
	}
}

func TestWorkSelectionDropsAVanishedContainerSilently(t *testing.T) {
	f := &presetFixture{rows: selRows("set-a", "set-b", "set-c"), hidden: map[string]bool{}}
	m := presetDashboard(t, f)

	m = markRow(t, m, "set-b")
	m = markRow(t, m, "set-c")
	f.drop("set-b")
	m = selPoll(t, m)

	if m.selection.Has("pop\x00set-b") {
		t.Fatal("a container that left the snapshot kept its mark")
	}
	if got := selIDs(m); !slices.Equal(got, []string{"set-a", "set-c"}) {
		t.Fatalf("rows = %v, want the gone row gone and set-c still marked", got)
	}
	if m.flash.Text() != "" {
		t.Fatalf("status = %q, want silence: a row that no longer exists cannot be a target", m.flash.Text())
	}
	if got := m.list.RegionCount(); got != 1 {
		t.Fatalf("region holds %d rows, want the one surviving mark", got)
	}
}

func TestWorkSelectionSurvivesThePollRebuild(t *testing.T) {
	m := selDashboard(selRows("set-a", "set-b", "set-c"))
	m = markRow(t, m, "set-b")

	// The poll replaces the row slice wholesale, and with reordered, freshly built
	// rows: the mark rides the cursor key, never a row identity or an index.
	rebuilt := selRows("set-c", "set-b", "set-a")
	rebuilt[1].Status = "DRAINING"
	updated, _ := m.Update(dashboardRowsMsg{snap: DashboardSnapshot{Containers: rebuilt}})
	m = updated.(QueueDashboard)

	if !m.selection.Has("pop\x00set-b") {
		t.Fatal("the rebuild dropped the mark")
	}
	if got := selIDs(m); !slices.Equal(got, []string{"set-c", "set-a", "set-b"}) {
		t.Fatalf("rows = %v, want the marked row still in the region", got)
	}
	if got := m.list.Items()[m.list.Len()-1].Status; got != "DRAINING" {
		t.Fatalf("region row status = %q, want the freshly built row rather than a kept copy", got)
	}
}

// The region is a rendering limit, never a narrowing: it takes a third of the
// viewport and says how many members it left out, and the separator keeps
// counting all of them.
func TestWorkSelectionRegionCapsAtAThirdOfTheViewport(t *testing.T) {
	m := selDashboard(selRows("set-a", "set-b", "set-c", "set-d", "set-e", "set-f"))
	for _, id := range []string{"set-a", "set-b", "set-c", "set-d"} {
		m = markRow(t, m, id)
	}
	m.list.Resize(6)

	body := ui.StripANSI(strings.Join(m.list.VisibleRows(), "\n"))
	if !strings.Contains(body, "↓ 2") {
		t.Fatalf("want the closing Scroll edge in:\n%s", body)
	}
	if strings.Contains(body, "more selected") {
		t.Fatalf("old Selection overflow line still renders:\n%s", body)
	}
	if want := ui.StripANSI(ui.SelectionSeparator(4, m.width)); !strings.Contains(body, want) {
		t.Fatalf("want the separator to count every mark (%q) in:\n%s", want, body)
	}
	if m.selection.Len() != 4 {
		t.Fatalf("the cap changed the Selection: %d marks", m.selection.Len())
	}
}

// The Selection divider is a rule that spans the list's width on the Work
// dashboard's list view and on the menu-overlay renderer — the same primitive,
// including when the terminal is too narrow for the full label.
func TestWorkSelectionSeparatorIsARule(t *testing.T) {
	t.Run("the list view draws a full-width rule", func(t *testing.T) {
		m := selDashboard(selRows("set-a", "set-b", "set-c"))
		m = markRow(t, m, "set-b")
		m = markRow(t, m, "set-c")

		want := ui.StripANSI(ui.SelectionSeparator(2, m.width))
		body := ui.StripANSI(strings.Join(m.list.VisibleRows(), "\n"))
		if !strings.Contains(body, want) {
			t.Fatalf("want rule %q in:\n%s", want, body)
		}
		if !strings.HasPrefix(want, "───") || !strings.Contains(want, "2 selected") {
			t.Fatalf("rule shape = %q", want)
		}
	})

	t.Run("a narrow list truncates the rule without wrapping", func(t *testing.T) {
		m := selDashboard(selRows("set-a", "set-b", "set-c"))
		m.width = 12
		m.cols.width = m.width
		m.cols.refit()
		m.resizeMainList()
		m = markRow(t, m, "set-b")
		m = markRow(t, m, "set-c")

		want := ui.StripANSI(ui.SelectionSeparator(2, 12))
		if got := len([]rune(want)); got != 12 {
			t.Fatalf("narrow rule width = %d, want 12: %q", got, want)
		}
		body := ui.StripANSI(strings.Join(m.list.VisibleRows(), "\n"))
		if !strings.Contains(body, want) {
			t.Fatalf("want truncated rule %q in:\n%s", want, body)
		}
		if strings.Contains(want, "\n") {
			t.Fatalf("rule wrapped: %q", want)
		}
	})

	t.Run("the menu overlay draws the same rule", func(t *testing.T) {
		m := selDashboard(selRows("set-a", "set-b", "set-c"))
		m = markRow(t, m, "set-b")
		m = markRow(t, m, "set-c")
		// The copy menu is the one a Selection of task sets can open: copy-name is
		// the only plural verb a set has, and it lives there now (ADR-0236
		// decision 6).
		m = selPress(t, m, selKeyRune('y'))
		if m.menu == nil {
			t.Fatal("`y` did not open the copy menu")
		}

		want := ui.StripANSI(ui.SelectionSeparator(2, m.width))
		view := ui.StripANSI(m.View().Content)
		if !strings.Contains(view, want) {
			t.Fatalf("want rule %q in the menu overlay:\n%s", want, view)
		}

		m.width = 12
		m.cols.width = m.width
		narrow := ui.StripANSI(ui.SelectionSeparator(2, 12))
		view = ui.StripANSI(m.View().Content)
		if !strings.Contains(view, narrow) {
			t.Fatalf("want truncated rule %q in the narrow menu overlay:\n%s", narrow, view)
		}
	})
}

// The cursor never starts in the region and no rebuild puts it there — but j and k
// walk in, which is how a row gets unmarked.
func TestWorkSelectionCursorStaysOutOfTheRegionUntilWalkedIn(t *testing.T) {
	last := selDashboard(selRows("set-a", "set-b", "set-c"))
	last = markRow(t, last, "set-c")
	if got := selCursorID(t, last); got != "set-b" {
		t.Fatalf("cursor on %q after marking the last row, want the last ordinary row", got)
	}
	if last.list.Cursor() >= last.list.Len()-last.list.RegionCount() {
		t.Fatalf("cursor = %d, inside a region of %d rows", last.list.Cursor(), last.list.RegionCount())
	}

	m := selDashboard(selRows("set-a", "set-b", "set-c"))
	m = markRow(t, m, "set-b")
	if got := selCursorID(t, m); got != "set-c" {
		t.Fatalf("cursor on %q after the mark, want the next ordinary row", got)
	}

	// The cursored row leaves on the next poll, so the cursor has to fall back
	// somewhere — and the region is not it.
	updated, _ := m.Update(dashboardRowsMsg{snap: DashboardSnapshot{Containers: selRows("set-a", "set-b")}})
	m = updated.(QueueDashboard)
	if got := selCursorID(t, m); got != "set-a" {
		t.Fatalf("cursor fell back onto %q, want the first ordinary row", got)
	}

	m = selPress(t, m, selKeyRune('j'))
	if got := selCursorID(t, m); got != "set-b" {
		t.Fatalf("j landed on %q, want the marked row: j/k walk into the region", got)
	}
	m = selPress(t, m, selKeyTab())
	if m.selection.Active() {
		t.Fatal("tab in the region did not unmark the row")
	}
}

// A verb no kind declared plural says so in selection mode rather than quietly
// acting on the cursored row. The keys that went plural — `a` and `y` — have
// their own tests in dashboard_bulk_verbs_test.go.
func TestWorkSelectionRefusesEverySingularVerb(t *testing.T) {
	for _, tc := range []struct {
		name string
		key  tea.KeyPressMsg
	}{
		{"detail", selKeyRune('l')},
		{"detail via enter", tea.KeyPressMsg{Code: tea.KeyEnter}},
		{"row's own I", selKeyRune('I')},
		{"open worktree", tea.KeyPressMsg{Code: 'g', Mod: tea.ModCtrl}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rows := selRows("set-a", "set-b")
			rows[1].Checkout = "/repo/worktree"
			m := selDashboard(rows)
			var copied int
			m.copyFunc = func(string) error { copied++; return nil }
			m = markRow(t, m, "set-a")

			m = selPress(t, m, tc.key)

			if !strings.Contains(m.flash.Text(), "acts on one row") {
				t.Fatalf("flash = %q, want a refusal naming the mode", m.flash.Text())
			}
			if !strings.Contains(m.flash.Text(), "shift+tab") {
				t.Fatalf("flash = %q, want the way out of the mode", m.flash.Text())
			}
			if m.detail != nil || m.menu != nil || copied != 0 || m.openCheckout != "" {
				t.Fatal("a refused verb acted anyway")
			}
			if !m.selection.Active() {
				t.Fatal("a refusal dropped the Selection")
			}
		})
	}
}

// Navigation is never gated: the mode changes what a verb does, not what the
// keyboard reaches.
func TestWorkSelectionKeepsNavigationLive(t *testing.T) {
	m := selDashboard(selRows("set-a", "set-b", "set-c"))
	m = markRow(t, m, "set-a")

	m = selPress(t, m, selKeyRune('j'))
	if got := selCursorID(t, m); got != "set-c" {
		t.Fatalf("j landed on %q, want the next row", got)
	}

	m = selPress(t, m, selKeyRune('/'))
	if !m.searchTyping {
		t.Fatal("the search did not open in selection mode")
	}
	m = selPress(t, m, tea.KeyPressMsg{Code: tea.KeyEscape})
	if !m.selection.Active() {
		t.Fatal("leaving the search dropped the Selection")
	}

	m = selPress(t, m, selKeyRune('f'))
	if m.filter == nil {
		t.Fatal("the preset list did not open in selection mode")
	}
	m = selPress(t, m, tea.KeyPressMsg{Code: tea.KeyEscape})

	if !m.ViewToggleAllowed() {
		t.Fatal("the page toggle is gated by the mode")
	}

	m = selPress(t, m, tea.KeyPressMsg{Code: 'h', Mod: tea.ModCtrl})
	if !m.showHelp {
		t.Fatal("help did not open in selection mode")
	}
}

// The detail view is one container's items, and item-level bulk is out of scope:
// tab there is a key that does nothing at all (ADR-0215 consequences).
func TestWorkSelectionTabIsInertInTheDetailView(t *testing.T) {
	m := selDashboard(selRows("set-a", "set-b"))
	m = selPress(t, m, selKeyRune('l'))
	if m.detail == nil {
		t.Fatal("the detail view did not open")
	}

	m = selPress(t, m, selKeyTab())

	if m.selection.Active() {
		t.Fatal("tab marked a row from inside the detail view")
	}
	if m.detail == nil {
		t.Fatal("tab closed the detail view")
	}
	if m.flash.Text() != "" {
		t.Fatalf("status = %q, want an inert key to say nothing", m.flash.Text())
	}
}
