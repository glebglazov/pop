package dashboard

import (
	"strings"
	"testing"

	"github.com/glebglazov/pop/internal/queuetest"
	"github.com/glebglazov/pop/routine"
	"github.com/glebglazov/pop/tasks"
	"github.com/glebglazov/pop/tasks/drain"
	"github.com/glebglazov/pop/work"
	"github.com/glebglazov/pop/work/ref"
)

// statusTables builds the two tables the command hands the render: page A's rows
// as the caller states them, page B's built the way the command builds them —
// work.BuildSnapshot over the Routine kind, so the rows arrive in the kind's own
// order rather than the test's.
func statusTables(t *testing.T, sets []DashboardRow, routines []work.Container) StatusTables {
	t.Helper()
	kinds := routinePageDeps(routines).RoutinePageKinds(nil)
	snap, err := work.BuildSnapshot(kinds)
	if err != nil {
		t.Fatalf("BuildSnapshot(routines): %v", err)
	}
	return StatusTables{
		TaskSets: StatusTable{Kinds: (&drain.Deps{}).WorkKinds(nil), Rows: sets},
		Routines: StatusTable{Kinds: kinds, Rows: snap.Containers},
	}
}

// TestRenderStatusPrintsTaskSetsThenRoutinesOmittingMaps pins the static status
// surface's shape: two sequential tables, each under its own kind's column
// headers, with Map rows dropped. Status reports what the daemon can advance, and
// a Map never advances unattended — the one place where "status renders the
// dashboard's table" deliberately does not hold, even though page A builds Maps
// from the same row builder.
func TestRenderStatusPrintsTaskSetsThenRoutinesOmittingMaps(t *testing.T) {
	sets := []DashboardRow{
		{Kind: ref.KindTaskSet, Project: "pop", ID: "2026-01-01-rdy", RawStatus: tasks.StatusReady, DestKind: work.DestNeedsBind},
		{Kind: ref.KindMap, Project: "pop", ID: "2026-01-02-map", Status: "ACTIVE"},
	}
	var out strings.Builder
	RenderStatus(&out, drain.StatusSnapshot{Tasks: queuetest.DataDeps(t)}, statusTables(t, sets, routinePageContainers()))
	text := out.String()

	setsAt := strings.Index(text, statusTaskSetsCaption)
	routinesAt := strings.Index(text, statusRoutinesCaption)
	if setsAt < 0 || routinesAt < 0 {
		t.Fatalf("status missing a table caption:\n%s", text)
	}
	if setsAt > routinesAt {
		t.Fatalf("Routines table printed before Task sets:\n%s", text)
	}
	// Each table carries its own kind's headers, and each header sits inside its
	// own table rather than above both.
	for _, want := range []string{"PROJECT", "TASK SET"} {
		if at := strings.Index(text, want); at < setsAt || at > routinesAt {
			t.Fatalf("Task-set header %q not in the Task sets table:\n%s", want, text)
		}
	}
	for _, want := range []string{"ROUTINE", "DIRECTORY", "SCHEDULE", "LAST RUN"} {
		if at := strings.Index(text, want); at < routinesAt {
			t.Fatalf("Routine header %q not in the Routines table:\n%s", want, text)
		}
	}
	if strings.Contains(text, "2026-01-02-map") {
		t.Fatalf("status printed a Map row:\n%s", text)
	}
	if !strings.Contains(text, "2026-01-01-rdy") {
		t.Fatalf("status missing its task set:\n%s", text)
	}
	// The Routines table is the Routine kind's own: every Routine there is, in its
	// relevance order, with its own status vocabulary.
	prev := routinesAt
	for _, id := range []string{"project:demo", "zeta-here", "mid-project", "aaa-elsewhere"} {
		at := strings.Index(text, id)
		if at < prev {
			t.Fatalf("Routines table order breaks the kind's order at %q:\n%s", id, text)
		}
		prev = at
	}
	if !strings.Contains(text, "paused (changed)") {
		t.Fatalf("Routines table missing the kind's own status word:\n%s", text)
	}
	if strings.Contains(text, "\x1b[") {
		t.Fatalf("status must be plain text (no ANSI):\n%q", text)
	}
}

// TestRenderStatusEmptyTablesSayWhichIsEmpty checks each table still announces
// itself when it has no rows, in its own page's words.
func TestRenderStatusEmptyTablesSayWhichIsEmpty(t *testing.T) {
	var out strings.Builder
	RenderStatus(&out, drain.StatusSnapshot{Tasks: queuetest.DataDeps(t)}, statusTables(t, nil, nil))
	text := out.String()
	for _, want := range []string{
		statusTaskSetsCaption,
		workPage().empty,
		statusRoutinesCaption,
		routine.EmptyListHint,
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("empty status missing %q:\n%s", want, text)
		}
	}
}

// TestRenderStatusMapOnlyTaskSetTableIsEmpty checks the Map omission is a drop of
// rows, not a filter that leaves a half-rendered table: a page A holding only
// Maps prints the empty line, never a header over nothing.
func TestRenderStatusMapOnlyTaskSetTableIsEmpty(t *testing.T) {
	sets := []DashboardRow{{Kind: ref.KindMap, Project: "pop", ID: "2026-01-02-map", Status: "ACTIVE"}}
	var out strings.Builder
	RenderStatus(&out, drain.StatusSnapshot{Tasks: queuetest.DataDeps(t)}, statusTables(t, sets, nil))
	text := out.String()
	if !strings.Contains(text, workPage().empty) {
		t.Fatalf("Map-only status missing the empty line:\n%s", text)
	}
	if strings.Contains(text, "TASK SET") {
		t.Fatalf("Map-only status rendered a Task-set header:\n%s", text)
	}
}

// TestStatusHeadlineDerivesFromTheScanNotThePageRows pins the invariant the
// verdict narrowing owes (ADR-0189): a verdict-derived aggregate may only be
// computed over rows whose verdicts were resolved. The page's rows are narrowed —
// a hidden terminal row carries no verdict — so the headline must not be a
// roll-up of them. It is the daemon scan's own snapshot, which resolves every set,
// and it therefore reads the same whatever the page happens to be rendering.
func TestStatusHeadlineDerivesFromTheScanNotThePageRows(t *testing.T) {
	snap := drain.StatusSnapshot{
		Tasks: queuetest.DataDeps(t),
		Idle: []drain.IdleProject{{
			Project:               "pop",
			RepoLabel:             "pop",
			AwaitingApprovalSetID: "2026-01-01-awaiting",
		}},
	}
	// Page A once with the terminal row hidden by the row filter (the ordinary
	// open), once with it revealed and resolved. Only the tables may differ.
	hidden := []DashboardRow{
		{Kind: ref.KindTaskSet, Project: "pop", ID: "2026-01-02-rdy", RawStatus: tasks.StatusReady, DestKind: work.DestNeedsBind},
	}
	revealed := append([]DashboardRow(nil), hidden...)
	revealed = append(revealed, DashboardRow{
		Kind: ref.KindTaskSet, Project: "pop", ID: "2026-01-01-awaiting",
		RawStatus: tasks.StatusAwaitingApproval, VerifyMark: tasks.VerifyMarkVerified, DestKind: work.DestNeedsBind,
	})

	headline := func(rows []DashboardRow) string {
		var out strings.Builder
		RenderStatus(&out, snap, statusTables(t, rows, nil))
		text := out.String()
		at := strings.Index(text, statusTaskSetsCaption)
		if at < 0 {
			t.Fatalf("status missing the task-set caption:\n%s", text)
		}
		return text[:at]
	}

	got := headline(hidden)
	if !strings.Contains(got, "1 awaiting approval") {
		t.Fatalf("headline lost the scan's awaiting-approval tally:\n%s", got)
	}
	if other := headline(revealed); other != got {
		t.Fatalf("headline changed with the page's rows:\n%q\nvs\n%q", got, other)
	}
}
