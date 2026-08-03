package queue

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/glebglazov/pop/config"
	"github.com/glebglazov/pop/routine"
	"github.com/glebglazov/pop/tasks"
	"github.com/glebglazov/pop/work"
	"github.com/glebglazov/pop/work/ref"
)

// fixedRoutineKind is the real Routine kind with its data-dir walk replaced by a
// fixed set of containers. Ordering, the column headers, the STATUS cell and the
// summary phrases are all the production adapter's — only where the containers
// come from is the test's — so page B is exercised against real behaviour without
// a Routine on disk.
type fixedRoutineKind struct {
	*routine.Kind
	containers []work.Container
}

func (k *fixedRoutineKind) Load() ([]work.Container, error) { return k.containers, nil }

func routineContainer(id string, tier int) work.Container {
	return work.Container{
		Kind:             ref.KindRoutine,
		ID:               id,
		CursorKey:        "routine\x00" + id,
		Status:           "idle",
		RoutineDirectory: "/repo/" + id,
		RoutineSchedule:  "every 6h",
		RoutineLastRun:   "never",
		RoutineTier:      tier,
	}
}

// routinePageContainers is one of every membership a page of Routines mixes: a
// Project routine and an authored Routine in the checkout the reader stands in, a
// Routine in another checkout of the same project, and one bound elsewhere — in
// deliberately wrong order, so the page's own sort is what puts them right.
func routinePageContainers() []work.Container {
	elsewhere := routineContainer("aaa-elsewhere", routine.TierElsewhere)
	elsewhere.RoutinePaused = true
	elsewhere.Status = "paused (changed)"
	project := routineContainer("project:demo", routine.TierHere)
	project.Badge = "◆"
	project.RoutineSchedule = "manual"
	return []work.Container{
		elsewhere,
		routineContainer("mid-project", routine.TierProject),
		routineContainer("zeta-here", routine.TierHere),
		project,
	}
}

func routinePageDeps(containers []work.Container) *Deps {
	return &Deps{RoutineKinds: func(*config.Config) []work.Kind {
		return []work.Kind{&fixedRoutineKind{Kind: routine.NewKind(nil), containers: containers}}
	}}
}

// openPage builds and sizes one page of the dashboard the way the entry layer
// does: the page's own snapshot, then the model over it.
func openPage(t *testing.T, d *Deps, page Page) QueueDashboard {
	t.Helper()
	cfg := &config.Config{}
	snap, err := BuildPageSnapshot(d, cfg, page)
	if err != nil {
		t.Fatalf("BuildPageSnapshot(%v): %v", page, err)
	}
	m := NewDashboardOn(d, cfg, snap, page)
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 140, Height: 30})
	return updated.(QueueDashboard)
}

// bodyRows returns the rendered table's row lines: everything after the summary,
// the blank line, the column header and the separator.
func bodyRows(t *testing.T, view string) []string {
	t.Helper()
	var lines []string
	for _, line := range strings.Split(strings.TrimRight(view, "\n"), "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		lines = append(lines, line)
	}
	return lines
}

// TestRoutinePageListsEveryRoutineInRelevanceOrder pins page B's whole contract:
// no filtering, ordered by relevance tier then id, Project routines at the top —
// a deliberate change from the Routine TUI, which appended them last.
func TestRoutinePageListsEveryRoutineInRelevanceOrder(t *testing.T) {
	m := openPage(t, routinePageDeps(routinePageContainers()), PageRoutines)

	var got []string
	for _, row := range m.snap.Containers {
		got = append(got, row.ID)
	}
	want := []string{"project:demo", "zeta-here", "mid-project", "aaa-elsewhere"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("routine page order = %v, want %v", got, want)
	}
}

func TestRoutinePageHeaderAndSummaryComeFromTheRoutineKind(t *testing.T) {
	m := openPage(t, routinePageDeps(routinePageContainers()), PageRoutines)

	if got, want := m.page.headers(m.kinds), routine.NewKind(nil).Columns(); strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("routine page headers = %v, want the Routine kind's %v", got, want)
	}
	// One routine sits in the reader's own checkout beside the Project routine, and
	// one is paused: "here" is the tier-1 tally, not a count of everything.
	if got, want := m.pageHeader(), "Routines · 4 routines · 2 here · 1 paused"; got != want {
		t.Fatalf("routine page header = %q, want %q", got, want)
	}
	if strings.Contains(m.pageHeader(), "task set") {
		t.Fatalf("routine page header must count only its own page: %q", m.pageHeader())
	}
}

// TestRoutinePageRendersRowsWithNoTierChrome pins that tier boundaries are
// ordering only: one line per Routine, no separator between tiers and no badge
// naming a tier. The Directory cell already carries where a Routine belongs.
func TestRoutinePageRendersRowsWithNoTierChrome(t *testing.T) {
	containers := routinePageContainers()
	m := openPage(t, routinePageDeps(containers), PageRoutines)
	view := m.View().Content

	lines := bodyRows(t, view)
	// summary + header + separator + one line per row + the footer hint.
	if want := len(containers) + 4; len(lines) != want {
		t.Fatalf("routine page rendered %d non-blank lines, want %d (no tier chrome):\n%s", len(lines), want, view)
	}
	for _, term := range []string{"tier", "here:", "elsewhere:", "── "} {
		for _, line := range lines[3 : 3+len(containers)] {
			if strings.Contains(line, term) {
				t.Fatalf("row line %q carries tier chrome %q", line, term)
			}
		}
	}
	if !strings.Contains(view, "ROUTINE") || !strings.Contains(view, "LAST RUN") {
		t.Fatalf("routine page missing its column header:\n%s", view)
	}
	if !strings.Contains(view, "/repo/mid-project") {
		t.Fatalf("routine page missing the directory cell:\n%s", view)
	}
	if !strings.Contains(view, "v queue") {
		t.Fatalf("routine page footer should offer the page toggle:\n%s", view)
	}
}

// declaredColumnsKind heads a page with columns of its own, so the header can be
// shown to come from the page's primary kind rather than from anything the
// dashboard restates.
type declaredColumnsKind struct {
	work.Kind
	columns []string
}

func (k *declaredColumnsKind) ID() work.KindID                 { return ref.KindTaskSet }
func (k *declaredColumnsKind) Columns() []string               { return k.columns }
func (k *declaredColumnsKind) Load() ([]work.Container, error) { return nil, nil }
func (k *declaredColumnsKind) Less(a, b work.Container) bool   { return a.ID < b.ID }
func (k *declaredColumnsKind) Summary([]work.Container) []string {
	return []string{"1 task set"}
}

func (k *declaredColumnsKind) StatusCell(c work.Container) []work.StatusSegment {
	return []work.StatusSegment{{Text: c.Status, Tone: work.ToneLabel}}
}

func TestWorkPageHeaderComesFromItsPrimaryKind(t *testing.T) {
	headers := []string{"WHERE", "WHAT", "HOW", "WHITHER", ""}
	d := &Deps{Kinds: func(*config.Config) []work.Kind {
		return []work.Kind{&declaredColumnsKind{columns: headers}}
	}}
	snap := DashboardSnapshot{Containers: []DashboardRow{
		TestDashboardRow("alpha", "set-a", DashboardRow{RawStatus: tasks.StatusReady, Status: "READY"}),
	}}
	m := NewDashboardOn(d, &config.Config{}, snap, PageWork)
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 140, Height: 30})
	view := updated.(QueueDashboard).View().Content
	for _, h := range headers[:4] {
		if !strings.Contains(view, h) {
			t.Fatalf("work page header missing %q — headers must come from the primary kind:\n%s", h, view)
		}
	}
}

// TestPagePollsStayOnTheirOwnPage pins the tagging that keeps two models of one
// type apart: a page ignores the other page's tick and the other page's rows, so
// a reload in flight when the operator switches cannot land in the wrong table.
func TestPagePollsStayOnTheirOwnPage(t *testing.T) {
	m := openPage(t, routinePageDeps(routinePageContainers()), PageRoutines)

	if _, cmd := m.Update(dashboardTickMsg{page: PageWork}); cmd != nil {
		t.Fatal("the routine page should drop the work page's tick")
	}
	if _, cmd := m.Update(dashboardTickMsg{page: PageRoutines}); cmd == nil {
		t.Fatal("the routine page should answer its own tick with a reload")
	}

	foreign := DashboardSnapshot{Containers: []DashboardRow{
		TestDashboardRow("alpha", "set-a", DashboardRow{RawStatus: tasks.StatusReady}),
	}}
	updated, _ := m.Update(dashboardRowsMsg{page: PageWork, snap: foreign})
	if got := len(updated.(QueueDashboard).snap.Containers); got != 4 {
		t.Fatalf("routine page took the work page's rows: %d containers", got)
	}
}
