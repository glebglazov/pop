package queue

import (
	"strings"
	"testing"

	"github.com/glebglazov/pop/tasks"
	"github.com/glebglazov/pop/tasks/setkind"
	"github.com/glebglazov/pop/wayfinder"
	"github.com/glebglazov/pop/work"
	"github.com/glebglazov/pop/work/ref"
)

// TestKindSummariesReproduceTodaysHeader ties the seam's per-kind phrases to the
// header the dashboard composes today (ADR-0173): for a page of task sets the two
// are the same string, and for a mixed page they are the same phrases with the
// Maps grouped behind the Task-set counts instead of interleaved — the ordering
// consequence of counting per kind rather than over one row list.
func TestKindSummariesReproduceTodaysHeader(t *testing.T) {
	setRows := []DashboardRow{
		{Project: "pop", SetRef: SetRef{SetID: "2026-07-01-a", RawStatus: tasks.StatusReady}},
		{Project: "pop", SetRef: SetRef{SetID: "2026-07-02-b", RawStatus: tasks.StatusBlocked, LiveDrain: true}},
		{Project: "pop", SetRef: SetRef{SetID: "2026-07-03-c", RawStatus: tasks.StatusBlocked, AutoDrain: true}},
	}
	mapRows := []DashboardRow{
		{Project: "pop", IsMap: true, SetRef: SetRef{SetID: "2026-07-04-chart"}, MapOpen: 2, MapFrontier: 1},
	}

	sets := setkind.New(nil)
	maps := wayfinder.NewMapKind(nil)
	containersOf := func(kind work.KindID, rows []DashboardRow) []work.Container {
		var out []work.Container
		for _, row := range rows {
			out = append(out, work.Container{Kind: kind, ID: row.SetID, Row: row})
		}
		return out
	}

	setPhrases := sets.Summary(containersOf(ref.KindTaskSet, setRows))
	if got, want := strings.Join(setPhrases, " · "), dashboardSummary(setRows); got != want {
		t.Fatalf("task-set header = %q, want today's %q", got, want)
	}

	mixed := append(setPhrases, maps.Summary(containersOf(ref.KindMap, mapRows))...)
	got := strings.Join(mixed, " · ")
	want := "3 task sets · 1 ready · 1 running · 1 auto-drain · 1 map"
	if got != want {
		t.Fatalf("mixed header = %q, want %q", got, want)
	}
	// Same phrases as today, only their order differs: the Map count trails its
	// kind's block rather than sitting between the set count and the tallies.
	for _, phrase := range strings.Split(dashboardSummary(append(setRows, mapRows...)), " · ") {
		if !strings.Contains(got, phrase) {
			t.Fatalf("phrase %q from today's header missing from %q", phrase, got)
		}
	}
}
