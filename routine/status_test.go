package routine

import (
	"strings"
	"testing"

	"github.com/glebglazov/pop/store"
)

// TestIdleStatusSurfacesOutcome pins the STATUS precedence for an idle
// (not-live) Routine: pause wins over outcome, a succeeded/failed last run maps
// to ok/failed, and a never-fired Routine stays plain idle.
func TestIdleStatusSurfacesOutcome(t *testing.T) {
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
			if got := idleStatus(c.m, c.lastOutcome); got != c.want {
				t.Fatalf("idleStatus(%+v, %q) = %q, want %q", c.m, c.lastOutcome, got, c.want)
			}
		})
	}
}

// TestRunStatusLabelCarriesReason confirms a run item's displayed status carries
// the fail reason for failed runs, the skip reason for skipped runs, and nothing
// for a clean succeeded run.
func TestRunStatusLabelCarriesReason(t *testing.T) {
	failed := runStatusLabel(store.RoutineRun{
		Outcome:    store.RoutineRunFailed,
		FailReason: "missing ROUTINE_COMPLETE sentinel",
	})
	if failed != "failed (missing ROUTINE_COMPLETE sentinel)" {
		t.Fatalf("failed label = %q, want the reason in parens", failed)
	}

	skipped := runStatusLabel(store.RoutineRun{
		Outcome:    store.RoutineRunSkipped,
		SkipReason: "checkout busy",
	})
	if skipped != "skipped (checkout busy)" {
		t.Fatalf("skipped label = %q, want the reason in parens", skipped)
	}

	// A clean run and a reasonless failed run both render as the bare outcome, so
	// the item list shows the status word alone.
	for _, run := range []store.RoutineRun{
		{Outcome: store.RoutineRunSucceeded},
		{Outcome: store.RoutineRunFailed},
	} {
		if label := runStatusLabel(run); strings.Contains(label, "(") {
			t.Fatalf("label for %+v = %q, want no parens", run, label)
		}
	}
}
