package dashboardshell

import (
	"errors"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/glebglazov/pop/config"
	"github.com/glebglazov/pop/dashboard"
	"github.com/glebglazov/pop/tasks"
	"github.com/glebglazov/pop/tasks/drain"
	"github.com/glebglazov/pop/ui"
	"github.com/glebglazov/pop/work"
	"github.com/glebglazov/pop/work/ref"
)

// pageKind is a wired Work kind standing in for one page's real adapter: it loads
// the containers the test declares and answers the seam's questions from them, so
// the shell's paging is exercised through production wiring without touching a
// data dir. loads counts every scan of it and loadErr fails them, so a test can
// pin which pages a shell paid for and what an unbuildable page does to startup.
type pageKind struct {
	id         work.KindID
	containers []work.Container
	columns    []string
	noun       string
	loads      *int
	loadErr    error
}

type artifactPageKind struct{ *pageKind }

func (*artifactPageKind) Artifacts(work.Container) ([]work.Artifact, error) {
	return []work.Artifact{{
		Type: "spec", Name: "spec.md", Path: "/tasks/set-a/spec.md",
		At: time.Date(2026, 8, 17, 12, 0, 0, 0, time.UTC),
	}}, nil
}

func (*artifactPageKind) ArtifactActions(work.Container, work.Artifact) []work.Action {
	return nil
}

func (k *artifactPageKind) PerformArtifact(work.Container, work.Artifact, work.Verb) (work.Outcome, error) {
	return work.Outcome{}, work.UnknownVerb(k.ID(), "artifact")
}

func (k *pageKind) ID() work.KindID { return k.id }

func (k *pageKind) Load() ([]work.Container, error) {
	if k.loads != nil {
		*k.loads++
	}
	if k.loadErr != nil {
		return nil, k.loadErr
	}
	return k.containers, nil
}
func (k *pageKind) Less(a, b work.Container) bool                       { return a.ID < b.ID }
func (k *pageKind) Columns() []string                                   { return k.columns }
func (k *pageKind) Actions(work.Container) []work.Action                { return nil }
func (k *pageKind) StatusActions(work.Container) []work.Action          { return nil }
func (k *pageKind) CopyActions(work.Container) []work.Action            { return nil }
func (k *pageKind) TypeWords() []string                                 { return nil }
func (k *pageKind) ItemActions(work.Container, work.Item) []work.Action { return nil }

func (k *pageKind) StatusCell(c work.Container) []work.StatusSegment {
	return []work.StatusSegment{{Text: c.Status, Tone: work.ToneLabel}}
}

func (k *pageKind) Perform(work.Container, *work.Item, work.Verb) (work.Outcome, error) {
	return work.Outcome{}, nil
}

func (k *pageKind) Summary(containers []work.Container) []string {
	return []string{work.CountPhrase(len(containers), k.noun, k.noun+"s")}
}

func setRows() []work.Container {
	return []work.Container{
		dashboard.TestDashboardRow("alpha", "set-a", dashboard.DashboardRow{RawStatus: tasks.StatusReady, Status: "READY", DefPath: "/a/tasks", StatePath: "/a/state.json"}),
		dashboard.TestDashboardRow("beta", "set-b", dashboard.DashboardRow{RawStatus: tasks.StatusReady, Status: "READY", DefPath: "/b/tasks", StatePath: "/b/state.json"}),
		dashboard.TestDashboardRow("gamma", "set-g", dashboard.DashboardRow{RawStatus: tasks.StatusReady, Status: "READY", DefPath: "/g/tasks", StatePath: "/g/state.json"}),
	}
}

func routineRows() []work.Container {
	return []work.Container{
		{Kind: ref.KindRoutine, ID: "daily", CursorKey: "routine\x00daily", Status: "idle",
			RoutineDirectory: "/home/daily", RoutineSchedule: "daily at 10:00", RoutineLastRun: "never"},
		{Kind: ref.KindRoutine, ID: "hourly", CursorKey: "routine\x00hourly", Status: "idle",
			RoutineDirectory: "/home/hourly", RoutineSchedule: "every 6h", RoutineLastRun: "never"},
	}
}

func testDeps() *drain.Deps {
	return countedDeps(nil, nil, nil)
}

// countedDeps wires both pages' kinds, optionally counting each page's scans and
// failing the routine page's.
func countedDeps(workLoads, routineLoads *int, routineErr error) *drain.Deps {
	return &drain.Deps{
		Kinds: func(*drain.Deps, *config.Config) []work.Kind {
			return []work.Kind{&pageKind{id: ref.KindTaskSet, containers: setRows(), columns: []string{"PROJECT", "TASK SET", "STATUS", "WORKTREE", ""}, noun: "task set", loads: workLoads}}
		},
		RoutineKinds: func(*drain.Deps, *config.Config) []work.Kind {
			return []work.Kind{&pageKind{id: ref.KindRoutine, containers: routineRows(), columns: []string{"ROUTINE", "DIRECTORY", "SCHEDULE", "LAST RUN", "STATUS"}, noun: "routine", loads: routineLoads, loadErr: routineErr}}
		},
	}
}

func newTestShell(t *testing.T, start Page) Shell {
	t.Helper()
	return newShellWith(t, start, testDeps())
}

func newShellWith(t *testing.T, start Page, d *drain.Deps) Shell {
	t.Helper()
	s, err := newShell(start, d, &config.Config{}, "")
	if err != nil {
		t.Fatalf("newShell: %v", err)
	}
	updated, _ := s.Update(tea.WindowSizeMsg{Width: 120, Height: 24})
	return updated.(Shell)
}

func pressV(t *testing.T, s Shell) Shell {
	t.Helper()
	updated, _ := s.Update(tea.KeyPressMsg{Code: 'v', Text: "v"})
	return updated.(Shell)
}

func TestShellOpensOnTheEntryPage(t *testing.T) {
	wp := newTestShell(t, PageWork)
	if wp.ActivePage() != PageWork {
		t.Fatalf("active page = %v, want work", wp.ActivePage())
	}
	if !strings.Contains(wp.View().Content, "Work ·") {
		t.Fatalf("expected the work page header, got:\n%s", wp.View().Content)
	}

	rp := newTestShell(t, PageRoutines)
	if rp.ActivePage() != PageRoutines {
		t.Fatalf("active page = %v, want routines", rp.ActivePage())
	}
	if !strings.Contains(rp.View().Content, "Routines ·") {
		t.Fatalf("expected the routine page header, got:\n%s", rp.View().Content)
	}
}

// TestShellVTogglesPagesFromEitherSide pins the toggle as a page switch rather
// than a one-way trip: it works from page A and from page B, and each page brings
// its own columns and its own rows with it.
func TestShellVTogglesPagesFromEitherSide(t *testing.T) {
	s := newTestShell(t, PageWork)
	s = pressV(t, s)
	if s.ActivePage() != PageRoutines {
		t.Fatalf("after v active = %v, want routines", s.ActivePage())
	}
	view := s.View().Content
	if !strings.Contains(view, "Routines ·") || !strings.Contains(view, "DIRECTORY") {
		t.Fatalf("expected the routine page, got:\n%s", view)
	}
	if strings.Contains(view, "set-a") {
		t.Fatalf("routine page must not list task sets:\n%s", view)
	}

	s = pressV(t, s)
	if s.ActivePage() != PageWork {
		t.Fatalf("after second v active = %v, want work", s.ActivePage())
	}
	view = s.View().Content
	if !strings.Contains(view, "Work ·") || !strings.Contains(view, "TASK SET") {
		t.Fatalf("expected the work page, got:\n%s", view)
	}
	if strings.Contains(view, "daily") {
		t.Fatalf("work page must not list routines:\n%s", view)
	}

	// The toggle is available from page B without re-entering through page A.
	s = newTestShell(t, PageRoutines)
	if s = pressV(t, s); s.ActivePage() != PageWork {
		t.Fatalf("v from the routine page = %v, want work", s.ActivePage())
	}
}

func TestShellTogglePreservesEachPagesCursorAndSearch(t *testing.T) {
	s := newTestShell(t, PageWork)
	for i := 0; i < 2; i++ {
		s.Update(tea.KeyPressMsg{Code: 'j', Text: "j"})
	}
	if got := s.PageDashboard(PageWork).ListCursor(); got != 2 {
		t.Fatalf("work cursor = %d, want 2", got)
	}
	// A search every row matches, applied with Enter: the term is the page's to
	// keep, and the cursor has no reason to move.
	s.Update(tea.KeyPressMsg{Code: '/', Text: "/"})
	for _, ch := range "set" {
		s.Update(tea.KeyPressMsg{Code: ch, Text: string(ch)})
	}
	s.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if got := s.PageDashboard(PageWork).SearchTerm(); got != "set" {
		t.Fatalf("work search term = %q, want 'set'", got)
	}

	s = pressV(t, s)
	s.Update(tea.KeyPressMsg{Code: 'j', Text: "j"})
	if got := s.PageDashboard(PageRoutines).ListCursor(); got != 1 {
		t.Fatalf("routine cursor = %d, want 1", got)
	}
	if got := s.PageDashboard(PageRoutines).SearchTerm(); got != "" {
		t.Fatalf("routine search term = %q: the search is per-page", got)
	}

	s = pressV(t, s)
	if got := s.PageDashboard(PageWork).ListCursor(); got != 2 {
		t.Fatalf("restored work cursor = %d, want 2", got)
	}
	if got := s.PageDashboard(PageWork).SearchTerm(); got != "set" {
		t.Fatalf("work search term = %q, want it to survive the switch", got)
	}
	if got := s.PageDashboard(PageRoutines).ListCursor(); got != 1 {
		t.Fatalf("restored routine cursor = %d, want 1", got)
	}
}

// TestShellVIsTypedIntoASearchNotAPageToggle is the shell's half of ADR-0213:
// the page toggle is the host key a text entry mode most needs suppressed,
// because `v` is a letter in a project name.
func TestShellVIsTypedIntoASearchNotAPageToggle(t *testing.T) {
	s := newTestShell(t, PageWork)
	s.Update(tea.KeyPressMsg{Code: '/', Text: "/"})
	s = pressV(t, s)
	if s.ActivePage() != PageWork {
		t.Fatalf("v paged the shell to %v while a search was being typed", s.ActivePage())
	}
	if _, built := s.pages[PageRoutines]; built {
		t.Fatal("v built the other page while a search was being typed")
	}

	s.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if got := s.PageDashboard(PageWork).SearchTerm(); got != "v" {
		t.Fatalf("search term = %q, want the typed 'v'", got)
	}

	// With the typing over, the toggle is the toggle again.
	if s = pressV(t, s); s.ActivePage() != PageRoutines {
		t.Fatalf("v after applying = %v, want the routine page", s.ActivePage())
	}
}

// TestShellPagesHoldIndependentSelections is the shell's half of ADR-0246: the
// two pages are two models, so each holds its own marks — and the toggle, which
// keeps a page's cursor and its search, keeps its Selection too.
func TestShellPagesHoldIndependentSelections(t *testing.T) {
	pressTab := func(s Shell) Shell {
		updated, _ := s.Update(tea.KeyPressMsg{Code: tea.KeyTab})
		return updated.(Shell)
	}
	selected := func(t *testing.T, s Shell, want string) {
		t.Helper()
		view := s.View().Content
		if want == "" {
			if strings.Contains(view, ui.SelectionMode) {
				t.Fatalf("page %v is in selection mode with nothing marked:\n%s", s.ActivePage(), view)
			}
			return
		}
		if !strings.Contains(view, ui.SelectionMode) || !strings.Contains(view, want) {
			t.Fatalf("page %v does not show %q marked:\n%s", s.ActivePage(), want, view)
		}
	}

	s := newTestShell(t, PageWork)
	s = pressTab(pressTab(s))
	selected(t, s, "2 selected")

	s = pressV(t, s)
	selected(t, s, "")
	s = pressTab(s)
	selected(t, s, "1 selected")

	s = pressV(t, s)
	selected(t, s, "2 selected")
	s = pressV(t, s)
	selected(t, s, "1 selected")
}

func TestShellHelpNamesThePageTheToggleLeadsTo(t *testing.T) {
	s := newTestShell(t, PageWork)
	s.Update(tea.KeyPressMsg{Code: 'h', Mod: tea.ModCtrl})
	view := s.View().Content
	if !strings.Contains(view, "Help · Work") || !strings.Contains(view, "routines view") {
		t.Fatalf("work page help missing the toggle:\n%s", view)
	}

	s = newTestShell(t, PageRoutines)
	s.Update(tea.KeyPressMsg{Code: 'h', Mod: tea.ModCtrl})
	view = s.View().Content
	if !strings.Contains(view, "Help · Routines") || !strings.Contains(view, "work view") {
		t.Fatalf("routine page help missing the toggle:\n%s", view)
	}
}

// TestShellOpenScansOnlyThePageItOpens is the point of the lazy page (ADR-0189):
// the open pays for one project scan, and the page not opened is not scanned at
// all until the operator asks for it — at which moment it lands on rows, without
// waiting for a poll.
func TestShellOpenScansOnlyThePageItOpens(t *testing.T) {
	var workLoads, routineLoads int
	s := newShellWith(t, PageWork, countedDeps(&workLoads, &routineLoads, nil))
	if workLoads != 1 || routineLoads != 0 {
		t.Fatalf("opening the work page scanned work=%d routines=%d, want 1 and 0", workLoads, routineLoads)
	}

	s = pressV(t, s)
	if routineLoads != 1 {
		t.Fatalf("the switch scanned routines %d times, want 1", routineLoads)
	}
	if workLoads != 1 {
		t.Fatalf("the switch rescanned the work page (%d scans), want 1", workLoads)
	}
	view := s.View().Content
	for _, want := range []string{"daily", "hourly"} {
		if !strings.Contains(view, want) {
			t.Fatalf("expected %q listed right after the switch, got:\n%s", want, view)
		}
	}
	s.Update(tea.KeyPressMsg{Code: 'j', Text: "j"})
	if got := s.PageDashboard(PageRoutines).ListCursor(); got != 1 {
		t.Fatalf("routine cursor after j = %d, want 1 — the fresh page must be sized and filled", got)
	}

	// Switching back reuses the model built at open rather than scanning again.
	if s = pressV(t, s); workLoads != 1 {
		t.Fatalf("switching back scanned work %d times, want 1", workLoads)
	}

	var openedOnRoutines int
	newShellWith(t, PageRoutines, countedDeps(&openedOnRoutines, nil, nil))
	if openedOnRoutines != 0 {
		t.Fatalf("opening the routine page scanned the work page %d times, want 0", openedOnRoutines)
	}
}

// TestShellUnbuildablePageDoesNotAbortTheOpen: a page that will not build used to
// take startup down with it. Built lazily it is that page's own error chrome, so
// the operator still reads their Task sets.
func TestShellUnbuildablePageDoesNotAbortTheOpen(t *testing.T) {
	s := newShellWith(t, PageWork, countedDeps(nil, nil, errors.New("routine store unreadable")))
	if !strings.Contains(s.View().Content, "set-a") {
		t.Fatalf("the work page must open with its rows:\n%s", s.View().Content)
	}

	s = pressV(t, s)
	if s.ActivePage() != PageRoutines {
		t.Fatalf("after v active = %v, want routines", s.ActivePage())
	}
	if view := s.View().Content; !strings.Contains(view, "routine store unreadable") {
		t.Fatalf("expected the build error as the routine page's chrome, got:\n%s", view)
	}
}

func TestShellVIgnoredWhileAPageOwnsTheKeyboard(t *testing.T) {
	d := countedDeps(nil, nil, nil)
	d.Kinds = func(*drain.Deps, *config.Config) []work.Kind {
		return []work.Kind{&artifactPageKind{pageKind: &pageKind{
			id: ref.KindTaskSet, containers: setRows(), columns: []string{"PROJECT", "TASK SET", "STATUS", "WORKTREE", ""}, noun: "task set",
		}}}
	}
	s := newShellWith(t, PageWork, d)
	if _, cmd := s.Update(tea.KeyPressMsg{Code: 'l', Text: "l"}); cmd != nil {
		t.Fatal("entering the detail reads the container in hand, not a fresh load")
	}

	updated, cmd := s.Update(tea.KeyPressMsg{Code: 'v', Text: "v"})
	if cmd != nil {
		t.Fatal("v should not toggle out of a detail view")
	}
	s = updated.(Shell)
	if s.ActivePage() != PageWork {
		t.Fatal("should stay on the work page while its detail is open")
	}
	if view := s.View().Content; !strings.Contains(view, "FILENAME") || !strings.Contains(view, "spec.md") {
		t.Fatalf("the shell withheld the page toggle but did not pass v to the detail's Artifact view:\n%s", view)
	}
}
