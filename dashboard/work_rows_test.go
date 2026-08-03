package dashboard

import (
	"strings"
	"testing"

	"github.com/glebglazov/pop/tasks"
	"github.com/glebglazov/pop/work/ref"
)

// TestKindSummariesComposeTheHeader pins the header the dashboard now prints: it
// is each kind's own phrases over its own rows, in kind precedence order
// (ADR-0173). For a page of task sets that is the string the hand-rolled summary
// produced; for a mixed page the same phrases appear with the Map count grouped
// behind the Task-set counts rather than interleaved between them — the ordering
// consequence of counting per kind rather than over one row list.
func TestKindSummariesComposeTheHeader(t *testing.T) {
	setRows := []DashboardRow{
		{Project: "pop", ID: "2026-07-01-a", RawStatus: tasks.StatusReady},
		{Project: "pop", ID: "2026-07-02-b", RawStatus: tasks.StatusBlocked, LiveDrain: true},
		{Project: "pop", ID: "2026-07-03-c", RawStatus: tasks.StatusBlocked, AutoDrain: true},
	}
	mapRows := []DashboardRow{
		{Project: "pop", Kind: ref.KindMap, ID: "2026-07-04-chart", MapOpen: 2, MapFrontier: 1},
	}

	if got, want := dashboardSummary(testKinds(), setRows), "3 task sets · 1 ready · 1 running · 1 auto-drain"; got != want {
		t.Fatalf("task-set header = %q, want %q", got, want)
	}

	mixed := dashboardSummary(testKinds(), append(setRows, mapRows...))
	want := "3 task sets · 1 ready · 1 running · 1 auto-drain · 1 map"
	if mixed != want {
		t.Fatalf("mixed header = %q, want %q", mixed, want)
	}
	// The header narrows with the fuzzy filter: it counts the rows on screen, not
	// the rows the snapshot was built from.
	if got, want := dashboardSummary(testKinds(), mapRows), "1 map"; got != want {
		t.Fatalf("map-only header = %q, want %q", got, want)
	}
}

// TestStatusCellComesFromTheOwningKind pins the seam every render path reads
// through: a row's STATUS cell is composed by its own kind, so no surface
// branches on what a row is, and the styled form is the plain one plus ANSI.
func TestStatusCellComesFromTheOwningKind(t *testing.T) {
	set := DashboardRow{Project: "pop", ID: "2026-07-01-a", RawStatus: tasks.StatusReady, AutoDrain: true}
	if got, want := dashboardStatusCellText(testKinds(), set), "READY · auto-drain"; got != want {
		t.Fatalf("task-set cell = %q, want %q", got, want)
	}
	wfMap := DashboardRow{Project: "pop", Kind: ref.KindMap, ID: "2026-07-04-chart", MapOpen: 2, MapFrontier: 1}
	if got, want := dashboardStatusCellText(testKinds(), wfMap), "WAYFINDING · 2 open / 1 frontier"; got != want {
		t.Fatalf("map cell = %q, want %q", got, want)
	}
	for _, row := range []DashboardRow{set, wfMap} {
		plain := dashboardStatusCellText(testKinds(), row)
		styled := dashboardStatusCellStyled(testKinds(), row)
		if strings.Contains(plain, "\x1b[") {
			t.Fatalf("plain cell carries ANSI: %q", plain)
		}
		if stripped := stripCellANSI(styled); stripped != plain {
			t.Fatalf("styled cell strips to %q, want %q", stripped, plain)
		}
	}
}

// stripCellANSI removes SGR sequences so a styled cell can be compared with the
// plain one it must reproduce.
func stripCellANSI(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] == '\x1b' {
			for i < len(s) && s[i] != 'm' {
				i++
			}
			continue
		}
		b.WriteByte(s[i])
	}
	return b.String()
}
