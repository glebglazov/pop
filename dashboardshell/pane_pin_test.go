package dashboardshell

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/glebglazov/pop/config"
	tmuxmod "github.com/glebglazov/pop/internal/tmux"
	"github.com/glebglazov/pop/internal/tmux/tmuxtest"
	"github.com/glebglazov/pop/tasks/drain"
	"github.com/glebglazov/pop/ui"
	"github.com/glebglazov/pop/work"
	"github.com/glebglazov/pop/work/ref"
)

// attributingKind is a page kind that claims the pane tagged for one of its rows —
// the seam a real kind implements, on the fake the shell tests already page
// through.
type attributingKind struct {
	*pageKind
	claims string
}

func (k *attributingKind) AttributePane(facts work.PaneFacts) (work.Attribution, bool) {
	if facts.Set != k.claims {
		return work.Attribution{}, false
	}
	for _, c := range k.containers {
		if c.ID == k.claims {
			return work.AttributeOne(work.AttributedContainer{
				Ref:       ref.WorkRef{Kind: k.id, ContainerID: c.ID},
				CursorKey: c.CursorKey,
				Label:     "task set " + c.ID,
			}), true
		}
	}
	return work.Attribution{}, false
}

// taggedPaneDeps builds deps whose page-A kind claims setID, in a pane tagged for
// it, and returns the fake so a test can count what the launch asked tmux.
func taggedPaneDeps(setID string) (*drain.Deps, *tmuxtest.Fake) {
	f := &tmuxtest.Fake{
		CurrentPaneID:      "%9",
		CurrentSessionName: "work",
		PaneTagValues:      map[string]map[tmuxmod.PaneTag]string{"%9": {tmuxmod.TagSet: setID}},
	}
	d := countedDeps(nil, nil, nil)
	d.Tmux = f
	d.Kinds = func(*drain.Deps, *config.Config) []work.Kind {
		return []work.Kind{&attributingKind{
			pageKind: &pageKind{id: ref.KindTaskSet, containers: setRows(),
				columns: []string{"PROJECT", "TASK SET", "STATUS", "WORKTREE", ""}, noun: "task set"},
			claims: setID,
		}}
	}
	return d, f
}

// The launch reads the pane once and the entry page opens with the row it named
// pinned to the top and marked — with the cursor left where it always rests. Paging
// away and back is not a launch, so the facts are never asked for again.
func TestEntryOnPageAPinsTheLaunchingPanesRow(t *testing.T) {
	d, f := taggedPaneDeps("set-g")
	s := newShellWith(t, PageWork, d)

	// set-g sorts last of the three rows the fake page loads, so seeing it above
	// set-a is the whole of the pin.
	assertPinnedFirst(t, s, "set-g", "set-a")
	if got := s.PageDashboard(PageWork).ListCursor(); got != 0 {
		t.Fatalf("cursor = %d, want the untouched first row: the pin moves rows, not the cursor", got)
	}
	if f.CurrentPaneFactsCalls != 1 {
		t.Fatalf("read the pane %d times at launch, want one round-trip", f.CurrentPaneFactsCalls)
	}

	s = pressV(t, s)
	s = pressV(t, s)
	if f.CurrentPaneFactsCalls != 1 {
		t.Fatalf("read the pane %d times after paging, want the launch's one", f.CurrentPaneFactsCalls)
	}
	assertPinnedFirst(t, s, "set-g", "set-a")
}

// assertPinnedFirst reads the rendered page the way the human does: the pinned row
// above the row that outranks it under the ordinary sort, carrying the pin mark.
func assertPinnedFirst(t *testing.T, s Shell, pinned, below string) {
	t.Helper()
	view := ui.StripANSI(s.View().Content)
	at, under := strings.Index(view, pinned), strings.Index(view, below)
	if at < 0 || under < 0 {
		t.Fatalf("view names %q at %d and %q at %d:\n%s", pinned, at, below, under, view)
	}
	if at > under {
		t.Fatalf("%q renders below %q, want it pinned above:\n%s", pinned, below, view)
	}
	line := view[at:]
	if end := strings.IndexByte(line, '\n'); end >= 0 {
		line = line[:end]
	}
	if start := strings.LastIndexByte(view[:at], '\n'); start >= 0 {
		line = view[start+1:][:len(line)+at-start-1]
	}
	if !strings.Contains(line, "\u25b8") {
		t.Fatalf("pinned row = %q, want the pin mark in its prefix column", line)
	}
}

// The pane is read for whichever page the dashboard opens on: the pin is computed
// per page, so the entry page decides nothing about whether the facts are worth
// having. Decision 5 survives as it always was — the launch opens the page it was
// asked for and never follows an answer across the toggle.
func TestPaneFactsAreReadForEitherEntryPage(t *testing.T) {
	d, _ := taggedPaneDeps("set-g")

	if facts := launchPaneFacts(d); facts.Set != "set-g" {
		t.Fatalf("facts = %+v, want the pane's set tag — the fixture is not arranging a pane", facts)
	}
}

// routineAttributingKind is page B's half of the same seam: it claims the pane
// tagged for one of its Routines, which is the one rung the real Routine kind
// answers.
type routineAttributingKind struct {
	*pageKind
	claims string
}

func (k *routineAttributingKind) AttributePane(facts work.PaneFacts) (work.Attribution, bool) {
	if facts.Routine != k.claims {
		return work.Attribution{}, false
	}
	for _, c := range k.containers {
		if c.ID == k.claims {
			return work.AttributeOne(work.AttributedContainer{
				Ref:       ref.WorkRef{Kind: k.id, ContainerID: c.ID},
				CursorKey: c.CursorKey,
				Label:     "routine " + c.ID,
			}), true
		}
	}
	return work.Attribution{}, false
}

// routinePaneDeps builds deps in a pane tagged for a routine fire, with page B's
// kind claiming it and page A's claiming nothing of the sort.
func routinePaneDeps(routineID string) (*drain.Deps, *tmuxtest.Fake) {
	f := &tmuxtest.Fake{
		CurrentPaneID:      "%11",
		CurrentSessionName: "routines",
		PaneTagValues:      map[string]map[tmuxmod.PaneTag]string{"%11": {tmuxmod.TagRoutine: routineID}},
	}
	d, _ := taggedPaneDeps("set-g")
	d.Tmux = f
	d.RoutineKinds = func(*drain.Deps, *config.Config) []work.Kind {
		return []work.Kind{&routineAttributingKind{
			pageKind: &pageKind{id: ref.KindRoutine, containers: routineRows(),
				columns: []string{"ROUTINE", "DIRECTORY", "SCHEDULE", "LAST RUN", "STATUS"}, noun: "routine"},
			claims: routineID,
		}}
	}
	return d, f
}

// A pane pop opened to fire a Routine pins that Routine's row on page B, whether
// the human entered on page A and toggled over or came straight in through
// `pop routine dashboard`. Page A, which lists no Routines, is left exactly as it
// always looks.
func TestRoutinePanePinsOnPageBFromEitherEntry(t *testing.T) {
	// "hourly" sorts after "daily", so seeing it first is the whole of the pin.
	for _, tc := range []struct {
		name  string
		entry Page
	}{
		{name: "entered on page A and toggled", entry: PageWork},
		{name: "opened straight onto page B", entry: PageRoutines},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d, f := routinePaneDeps("hourly")
			s := newShellWith(t, tc.entry, d)
			if tc.entry == PageWork {
				assertNothingPinned(t, s)
				s = pressV(t, s)
			}

			assertPinnedFirst(t, s, "hourly", "daily")
			if got := s.PageDashboard(PageRoutines).ListCursor(); got != 0 {
				t.Fatalf("cursor = %d, want the untouched first row", got)
			}
			if f.CurrentPaneFactsCalls != 1 {
				t.Fatalf("read the pane %d times, want the launch's one round-trip", f.CurrentPaneFactsCalls)
			}
		})
	}
}

// A routine-attributed pane pins nothing on the page that lists no Routines: the
// answer belongs to a row on the other page, and page A does not follow it.
func assertNothingPinned(t *testing.T, s Shell) {
	t.Helper()
	view := ui.StripANSI(s.View().Content)
	assertNoRowIsMarked(t, view)
	if at, under := strings.Index(view, "set-a"), strings.Index(view, "set-g"); at < 0 || at > under {
		t.Fatalf("rows are not in their untouched order:\n%s", view)
	}
}

// A pinned row the human searches away is simply gone: the search is the active
// view on page B, and a launch does not widen it (ADR-0209 decisions 7 and 8).
func TestFilteringAwayAPinnedRoutineRowIsSilent(t *testing.T) {
	d, _ := routinePaneDeps("hourly")
	s := newShellWith(t, PageRoutines, d)
	assertPinnedFirst(t, s, "hourly", "daily")

	for _, key := range []tea.KeyPressMsg{
		{Code: '/', Text: "/"},
		{Code: 'd', Text: "d"},
		{Code: 'a', Text: "a"},
	} {
		updated, _ := s.Update(key)
		s = updated.(Shell)
	}

	view := ui.StripANSI(s.View().Content)
	if strings.Contains(view, "hourly") {
		t.Fatalf("the filtered-away pinned row still renders:\n%s", view)
	}
	assertNoRowIsMarked(t, view)
}

// assertNoRowIsMarked reads the pin out of the table body alone. The frame's hint
// line wears the same mark on every menu opener, so a whole-view scan answers a
// question about the footer rather than about the rows.
func assertNoRowIsMarked(t *testing.T, view string) {
	t.Helper()
	for _, row := range tableRows(view) {
		if strings.Contains(row, "▸") {
			t.Fatalf("row %q carries the pin mark, want none:\n%s", row, view)
		}
	}
}

// tableRows returns the rendered rows: the lines under the header's dashed rule,
// up to the blank line that ends the table.
func tableRows(view string) []string {
	lines := strings.Split(view, "\n")
	start := -1
	for i, line := range lines {
		if strings.Contains(line, "---") {
			start = i + 1
			break
		}
	}
	if start < 0 {
		return nil
	}
	var rows []string
	for _, line := range lines[start:] {
		if strings.TrimSpace(line) == "" {
			break
		}
		rows = append(rows, line)
	}
	return rows
}
