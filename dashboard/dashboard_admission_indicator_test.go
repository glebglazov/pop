package dashboard

import (
	"strings"
	"testing"

	"github.com/glebglazov/pop/tasks"
)

// TestDashboardQueuedMarkerSitsBesideTheStatus covers the Admission indicator
// (ADR-0239): a set with a command queued for its checkout carries its own
// marker beside the derived status, in both row layouts and under every status —
// including the ones that already say a human is needed. Waiting is an execution
// fact, so it never displaces the status a set is in.
func TestDashboardQueuedMarkerSitsBesideTheStatus(t *testing.T) {
	// Column order: PROJECT, TASK SET, STATUS, WORKTREE, indicator — STATUS is
	// given ample width so nothing is truncated at render.
	statusW := []int{10, 10, 80, 10, 10}

	for _, status := range []tasks.TaskSetStatus{tasks.StatusReady, tasks.StatusNeedsVerify, tasks.StatusAwaitingApproval, tasks.StatusFailed} {
		row := DashboardRow{RawStatus: status, QueuedCommand: true}
		label := tasks.WorkRowStatusLabel(row)

		// The plain cell is what column math measures: the marker trails the status
		// there, and the styled lines carry it beside a status only ANSI separates.
		plain := dashboardStatusCellText(testKinds(), row)
		if !strings.HasPrefix(plain, label) || !strings.Contains(plain, " · "+tasks.QueuedMark) {
			t.Fatalf("status %s plain cell = %q, want %s kept with a %q marker beside it", status, plain, label, tasks.QueuedMark)
		}
		single := dashboardTableLine(dashboardRowValues(testKinds(), row, livePaneCache{}), statusW)
		if !strings.Contains(single, "· "+tasks.QueuedMark) || !strings.Contains(single, label) {
			t.Fatalf("status %s single-line render missing the queued marker beside the status:\n%s", status, single)
		}
		twoLine := dashboardTwoLineRowLine2(testKinds(), row, []int{10, 10, 10, 10})
		if !strings.Contains(twoLine, "· "+tasks.QueuedMark) || !strings.Contains(twoLine, label) {
			t.Fatalf("status %s two-line render missing the queued marker beside the status:\n%s", status, twoLine)
		}
	}

	// The live-drain marker is the READY→IN PROGRESS refinement of the label
	// (ADR-0111): a queued command rides beside it rather than in place of it, so
	// a drained set someone is queued behind shows both facts at once.
	live := DashboardRow{RawStatus: tasks.StatusReady, LiveDrain: true, QueuedCommand: true}
	cell := dashboardStatusCellText(testKinds(), live)
	if cell != "IN PROGRESS · "+tasks.QueuedMark {
		t.Fatalf("live-drained queued cell = %q, want %q", cell, "IN PROGRESS · "+tasks.QueuedMark)
	}

	// Nothing queued, nothing said.
	idle := dashboardStatusCellText(testKinds(), DashboardRow{RawStatus: tasks.StatusReady})
	if strings.Contains(idle, tasks.QueuedMark) {
		t.Fatalf("unqueued row carries the marker: %q", idle)
	}
}
