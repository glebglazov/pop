package routine

import (
	"strings"
	"testing"

	"github.com/glebglazov/pop/store"
)

// TestDashboardIdleStatusSurfacesOutcome pins the STATUS precedence for an idle
// (not-live) Routine: pause wins over outcome, a succeeded/failed last run maps
// to ok/failed, and a never-fired Routine stays plain idle.
func TestDashboardIdleStatusSurfacesOutcome(t *testing.T) {
	cases := []struct {
		name        string
		m           Manifest
		lastOutcome string
		want        string
	}{
		{"succeeded", Manifest{}, store.RoutineRunSucceeded, "ok"},
		{"failed", Manifest{}, store.RoutineRunFailed, "failed"},
		{"never fired", Manifest{}, "", "idle"},
		{"skipped falls back to idle", Manifest{}, store.RoutineRunSkipped, "idle"},
		{"pause wins over ok", Manifest{Paused: true, PauseReason: PauseReasonManual}, store.RoutineRunSucceeded, "paused"},
		{"pause wins over failed", Manifest{Paused: true, PauseReason: PauseReasonFailure}, store.RoutineRunFailed, "paused (failed)"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := dashboardIdleStatus(c.m, c.lastOutcome); got != c.want {
				t.Fatalf("dashboardIdleStatus(%+v, %q) = %q, want %q", c.m, c.lastOutcome, got, c.want)
			}
		})
	}
}

// TestRunDetailLineFailReason confirms the runs detail view renders the fail
// reason for failed runs, the skip reason for skipped runs, and neither for a
// clean succeeded run.
func TestRunDetailLineFailReason(t *testing.T) {
	failed := runDetailLine(store.RoutineRun{
		Outcome:    store.RoutineRunFailed,
		FailReason: "missing ROUTINE_COMPLETE sentinel",
	})
	if !strings.Contains(failed, "failed (missing ROUTINE_COMPLETE sentinel)") {
		t.Fatalf("failed line missing reason: %q", failed)
	}

	skipped := runDetailLine(store.RoutineRun{
		Outcome:    store.RoutineRunSkipped,
		SkipReason: "checkout busy",
	})
	if !strings.Contains(skipped, "skipped (checkout busy)") {
		t.Fatalf("skipped line missing reason: %q", skipped)
	}

	ok := runDetailLine(store.RoutineRun{Outcome: store.RoutineRunSucceeded})
	if strings.Contains(ok, "(") {
		t.Fatalf("succeeded line should carry no reason: %q", ok)
	}

	// A failed run without a recorded reason renders the bare outcome.
	bare := runDetailLine(store.RoutineRun{Outcome: store.RoutineRunFailed})
	if strings.Contains(bare, "(") {
		t.Fatalf("reasonless failed line should carry no parens: %q", bare)
	}
}
