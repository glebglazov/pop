package tasks

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/glebglazov/pop/work"
)

// exploreMarkLine opens every surface's Exploration line, so an assertion about
// the mark cannot be answered by the word turning up in a path.
const exploreMarkLine = "🧭 "

// exploredWorkSHA is the tree the seeded report records — the one
// seedExplorationReport stamps.
const exploredWorkSHA = "abc123abc123"

// seedFixtureExplorationReport files a report for the fixture's registered set
// through the same writer the Explore pass uses.
func seedFixtureExplorationReport(t *testing.T, env *runTaskSetFixture) string {
	t.Helper()
	return seedExplorationReport(t, env.deps(), exploreParkRows(t, env).Manifests["demo"])
}

// exploreMarkRow refreshes the fixture and returns the row every read surface
// renders — the one place the mark is resolved.
func exploreMarkRow(t *testing.T, env *runTaskSetFixture) Row {
	t.Helper()
	row := findRow(exploreParkRows(t, env), "demo")
	if row == nil {
		t.Fatal("no row for demo")
	}
	return *row
}

// exploreMarkSurfaces renders the four surfaces that must agree about one row:
// the `pop tasks status` table, its per-set detail, and the STATUS cell the
// dashboard row and `pop work status` share.
func exploreMarkSurfaces(t *testing.T, env *runTaskSetFixture, row Row) (table, detail, cell string) {
	t.Helper()
	var overview, perSet strings.Builder
	Render(&overview, exploreParkRows(t, env))
	m := exploreParkRows(t, env).Manifests["demo"]
	RenderTaskSetDetail(env.deps(), nil, &perSet, "demo", &row, m)
	return overview.String(), perSet.String(),
		WorkRowStatusCell(work.Container{RawStatus: row.Status, ExploreMark: row.Explore.Mark})
}

// TestTheExploreMarkSaysWhetherADeclaredSetWasExplored drives the whole read
// side of one declared set through its three answers — explored, waiting for a
// pass, and a pass that gave up — asserting every surface renders the one
// resolution and none of them carries the report's prose.
func TestTheExploreMarkSaysWhetherADeclaredSetWasExplored(t *testing.T) {
	env := setupDrainExploreFixture(t, map[string]any{"explore": true})
	path := seedFixtureExplorationReport(t, env)

	row := exploreMarkRow(t, env)
	if row.Explore.Mark != ExploreMarkExplored {
		t.Fatalf("mark = %q, want %q", row.Explore.Mark, ExploreMarkExplored)
	}
	// The pointer is the report: where it is and which tree it describes.
	if row.Explore.Pointer.Path != path || row.Explore.Pointer.WorkSHA != ShortSHA(exploredWorkSHA) {
		t.Fatalf("pointer = %+v, want %s at %s", row.Explore.Pointer, path, ShortSHA(exploredWorkSHA))
	}
	table, detail, cell := exploreMarkSurfaces(t, env, row)
	if !strings.Contains(table, "Explored") {
		t.Fatalf("status table lost the mark:\n%s", table)
	}
	if !strings.Contains(cell, string(ExploreMarkExplored)) {
		t.Fatalf("STATUS cell = %q, want the explored mark", cell)
	}
	for _, want := range []string{"Explored", path, ShortSHA(exploredWorkSHA)} {
		if !strings.Contains(detail, want) {
			t.Fatalf("per-set detail missing %q:\n%s", want, detail)
		}
	}
	for name, text := range map[string]string{"table": table, "detail": detail} {
		if strings.Contains(text, explorationProse) {
			t.Fatalf("%s inlined the report body:\n%s", name, text)
		}
	}

	// No report yet, and no pass has answered: the mark says the map is missing
	// and the reason says nobody has looked for it.
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	row = exploreMarkRow(t, env)
	if row.Explore.Mark != ExploreMarkUnexplored || row.Explore.Reason != UnexploredNotRun {
		t.Fatalf("mark/reason = %q/%q, want %q/%q", row.Explore.Mark, row.Explore.Reason, ExploreMarkUnexplored, UnexploredNotRun)
	}
	table, detail, cell = exploreMarkSurfaces(t, env, row)
	for name, text := range map[string]string{"table": table, "detail": detail} {
		if !strings.Contains(text, "no pass has run yet") {
			t.Fatalf("%s lost the reason beside the mark:\n%s", name, text)
		}
	}
	if !strings.Contains(cell, string(ExploreMarkUnexplored)) {
		t.Fatalf("STATUS cell = %q, want the unexplored mark", cell)
	}

	// A run the machinery stopped is not the pass answering, so the reason does
	// not move: a later drain will find that condition gone.
	writeExploreRunMeta(t, filepath.Join(env.tasksDir, "demo"), streamOutcomeInterrupted, time.Now().Add(-time.Hour))
	if got := exploreMarkRow(t, env).Explore.Reason; got != UnexploredNotRun {
		t.Fatalf("reason after an interrupted run = %q, want %q", got, UnexploredNotRun)
	}

	// A pass that reached its own ending with no report: the reason changes, the
	// set parks, and the mark leaves the STATUS cell — the status word is now the
	// same fact, and saying it twice reads as two.
	writeExploreRunMeta(t, filepath.Join(env.tasksDir, "demo"), streamOutcomeCompleted, time.Now())
	row = exploreMarkRow(t, env)
	if row.Status != StatusExploreFailed || row.Explore.Reason != UnexploredPassFailed {
		t.Fatalf("status/reason = %s/%q, want %s/%q", row.Status, row.Explore.Reason, StatusExploreFailed, UnexploredPassFailed)
	}
	table, detail, cell = exploreMarkSurfaces(t, env, row)
	for name, text := range map[string]string{"table": table, "detail": detail} {
		if !strings.Contains(text, "the pass gave up") {
			t.Fatalf("%s lost the park's reason:\n%s", name, text)
		}
	}
	if strings.Contains(cell, string(ExploreMarkUnexplored)) {
		t.Fatalf("STATUS cell = %q, want no mark beside %s", cell, StatusExploreFailed)
	}
}

// TestASetThatNeverAskedToBeExploredCarriesNoMark pins the opt-in: exploration
// is a thing a set asks for, so a set that did not ask says nothing about it
// anywhere — not even after a human's hand pass left it a report.
func TestASetThatNeverAskedToBeExploredCarriesNoMark(t *testing.T) {
	env := setupDrainExploreFixture(t, nil)
	path := seedFixtureExplorationReport(t, env)

	row := exploreMarkRow(t, env)
	if row.Explore.Mark != ExploreMarkNone || row.Explore.Reason != UnexploredReasonNone {
		t.Fatalf("mark/reason = %q/%q, want both absent", row.Explore.Mark, row.Explore.Reason)
	}
	table, detail, cell := exploreMarkSurfaces(t, env, row)
	for name, text := range map[string]string{"table": table, "cell": cell} {
		for _, mark := range []string{"explored", "Explored", "Not explored"} {
			if strings.Contains(text, mark) {
				t.Fatalf("%s carried %q for a set that never asked:\n%s", name, mark, text)
			}
		}
	}
	// The report a human asked for by hand is still theirs to find: the pointer
	// is a fact about the set, and only the mark is owed to a declaration.
	if !strings.Contains(detail, path) {
		t.Fatalf("per-set detail hid the hand run's report:\n%s", detail)
	}
	if strings.Contains(detail, "Not explored") || strings.Contains(detail, exploreMarkLine+"Explored") {
		t.Fatalf("per-set detail marked a set that never asked:\n%s", detail)
	}
}

// TestSignOffGateNamesTheExplorationReport confirms the human deciding on a set
// is told where the map its builders were handed is, beside the refine report
// the gate already names — and that a set with no report adds no line.
func TestSignOffGateNamesTheExplorationReport(t *testing.T) {
	d, m := hitlFixture(t)
	before, _ := hitlGateOutput(t, d, m, "0\n")
	if strings.Contains(before, ExplorationFileName) {
		t.Fatalf("gate named a report the set has not got:\n%s", before)
	}

	path := seedExplorationReport(t, d, m)

	after, _ := hitlGateOutput(t, d, m, "0\n")
	for _, want := range []string{path, ShortSHA(exploredWorkSHA)} {
		if !strings.Contains(after, want) {
			t.Fatalf("gate missing %q:\n%s", want, after)
		}
	}
	if strings.Contains(after, explorationProse) {
		t.Fatalf("gate printed the report body:\n%s", after)
	}
}
