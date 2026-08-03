package dashboardshell

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/glebglazov/pop/config"
	"github.com/glebglazov/pop/dashboard"
	"github.com/glebglazov/pop/tasks"
	"github.com/glebglazov/pop/tasks/drain"
	"github.com/glebglazov/pop/work"
	"github.com/glebglazov/pop/work/ref"
)

// pageKind is a wired Work kind standing in for one page's real adapter: it loads
// the containers the test declares and answers the seam's questions from them, so
// the shell's paging is exercised through production wiring without touching a
// data dir.
type pageKind struct {
	id         work.KindID
	containers []work.Container
	columns    []string
	noun       string
}

func (k *pageKind) ID() work.KindID                                     { return k.id }
func (k *pageKind) Load() ([]work.Container, error)                     { return k.containers, nil }
func (k *pageKind) Less(a, b work.Container) bool                       { return a.ID < b.ID }
func (k *pageKind) Columns() []string                                   { return k.columns }
func (k *pageKind) Actions(work.Container) []work.Action                { return nil }
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
	return &drain.Deps{
		Kinds: func(*config.Config) []work.Kind {
			return []work.Kind{&pageKind{id: ref.KindTaskSet, containers: setRows(), columns: []string{"PROJECT", "TASK SET", "STATUS", "WORKTREE", ""}, noun: "task set"}}
		},
		RoutineKinds: func(*config.Config) []work.Kind {
			return []work.Kind{&pageKind{id: ref.KindRoutine, containers: routineRows(), columns: []string{"ROUTINE", "DIRECTORY", "SCHEDULE", "LAST RUN", "STATUS"}, noun: "routine"}}
		},
	}
}

func newTestShell(t *testing.T, start Page) Shell {
	t.Helper()
	s, err := newShell(start, testDeps(), &config.Config{})
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
	if !strings.Contains(wp.View().Content, "Queue ·") {
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
	if !strings.Contains(view, "Queue ·") || !strings.Contains(view, "TASK SET") {
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

func TestShellTogglePreservesEachPagesCursorAndFilter(t *testing.T) {
	s := newTestShell(t, PageWork)
	for i := 0; i < 2; i++ {
		s.Update(tea.KeyPressMsg{Code: 'j', Text: "j"})
	}
	if got := s.PageDashboard(PageWork).ListCursor(); got != 2 {
		t.Fatalf("work cursor = %d, want 2", got)
	}
	s.Update(tea.KeyPressMsg{Code: '/', Text: "/"})
	if !s.PageDashboard(PageWork).FilterActive() {
		t.Fatal("expected the work page filter to engage")
	}

	s = pressV(t, s)
	s.Update(tea.KeyPressMsg{Code: 'j', Text: "j"})
	if got := s.PageDashboard(PageRoutines).ListCursor(); got != 1 {
		t.Fatalf("routine cursor = %d, want 1", got)
	}

	s = pressV(t, s)
	if got := s.PageDashboard(PageWork).ListCursor(); got != 2 {
		t.Fatalf("restored work cursor = %d, want 2", got)
	}
	if !s.PageDashboard(PageWork).FilterActive() {
		t.Fatal("expected the work page filter to survive the switch")
	}
	if got := s.PageDashboard(PageRoutines).ListCursor(); got != 1 {
		t.Fatalf("restored routine cursor = %d, want 1", got)
	}
}

func TestShellHelpNamesThePageTheToggleLeadsTo(t *testing.T) {
	s := newTestShell(t, PageWork)
	s.Update(tea.KeyPressMsg{Code: 'h', Mod: tea.ModCtrl})
	view := s.View().Content
	if !strings.Contains(view, "Help · Queue") || !strings.Contains(view, "routines view") {
		t.Fatalf("work page help missing the toggle:\n%s", view)
	}

	s = newTestShell(t, PageRoutines)
	s.Update(tea.KeyPressMsg{Code: 'h', Mod: tea.ModCtrl})
	view = s.View().Content
	if !strings.Contains(view, "Help · Routines") || !strings.Contains(view, "queue view") {
		t.Fatalf("routine page help missing the toggle:\n%s", view)
	}
}

func TestShellVIgnoredWhileAPageOwnsTheKeyboard(t *testing.T) {
	s := newTestShell(t, PageWork)
	if _, cmd := s.Update(tea.KeyPressMsg{Code: 'l', Text: "l"}); cmd != nil {
		t.Fatal("entering the detail reads the container in hand, not a fresh load")
	}

	updated, cmd := s.Update(tea.KeyPressMsg{Code: 'v', Text: "v"})
	if cmd != nil {
		t.Fatal("v should not toggle out of a detail view")
	}
	if updated.(Shell).ActivePage() != PageWork {
		t.Fatal("should stay on the work page while its detail is open")
	}
}
