package supervisor

import (
	"bytes"
	"github.com/glebglazov/pop/config"
	"github.com/glebglazov/pop/routine"
	"strings"
	"testing"
	"time"

	"github.com/glebglazov/pop/internal/queuetest"
	"github.com/glebglazov/pop/work/ref"
)

// TestSupervisorDrivesRoutinesThroughTheAdvanceSeam pins the wiring the slice
// exists for: a supervisor tick — not a routine-only pass beside it — reconciles
// and fires Routines, and it does so through the advance seam rather than an
// inline pipeline of its own.
func TestSupervisorDrivesRoutinesThroughTheAdvanceSeam(t *testing.T) {
	now := time.Date(2026, 7, 18, 12, 0, 0, 0, time.UTC)
	qd, rd, home := routineTickDeps(t, now)
	if _, err := routine.AddWith(rd, "nightly", "every 1h", home); err != nil {
		t.Fatal(err)
	}
	if _, err := routine.ResumeWith(rd, "nightly"); err != nil {
		t.Fatal(err)
	}
	recordRoutineRun(t, "nightly", "", now.Add(-2*time.Hour))

	advanced := false
	for _, adv := range advancers(qd, &config.Config{}) {
		if advancerKindID(adv) == ref.KindRoutine {
			advanced = true
		}
	}
	if !advanced {
		t.Fatal("the supervisor's advancer list carries no routine kind")
	}

	var out bytes.Buffer
	tick(qd, &out, newRunOutputState())

	rt := qd.Tmux.(*queuetest.RecordingTmux)
	if _, ok := extractRoutineSpawnCommand(rt, "nightly"); !ok {
		t.Fatalf("a tick must fire a due routine, commands=%v", rt.Commands)
	}
	if !strings.Contains(out.String(), "work: routine nightly: spawned fire") {
		t.Fatalf("tick output missing the routine's fire line:\n%s", out.String())
	}
}

// TestRoutineCandidateIsContainerLevelAndOccupiesNoCheckout pins the two facts
// the seam needs from a Routine candidate: its ref names the container and not a
// run — a candidate exists before a run does, and the run id is minted by the
// fire — and it claims no checkout, so a read-only fire is never queued behind a
// drain.
func TestRoutineCandidateIsContainerLevelAndOccupiesNoCheckout(t *testing.T) {
	now := time.Date(2026, 7, 18, 12, 0, 0, 0, time.UTC)
	qd, rd, home := routineTickDeps(t, now)
	if _, err := routine.AddWith(rd, "audit", "every 1h", home); err != nil {
		t.Fatal(err)
	}
	if _, err := routine.ResumeWith(rd, "audit"); err != nil {
		t.Fatal(err)
	}
	recordRoutineRun(t, "audit", "", now.Add(-2*time.Hour))

	candidates := routineCandidates(t, qd)
	if len(candidates) != 1 {
		t.Fatalf("candidates = %+v, want the one due routine", candidates)
	}
	got := candidates[0]
	if got.Ref != (ref.WorkRef{Kind: ref.KindRoutine, ContainerID: "audit"}) {
		t.Fatalf("candidate ref = %s, want routine:audit", got.Ref)
	}
	if got.Ref.IsItem() {
		t.Fatalf("candidate ref %s names an item; the run id is minted by the fire", got.Ref)
	}
	if got.Checkout != "" {
		t.Fatalf("candidate checkout = %q, want none: a fire occupies no checkout", got.Checkout)
	}
	if reason := newCheckoutOccupancy(qd).refusal(got); reason != "" {
		t.Fatalf("occupancy refused a routine: %q", reason)
	}
}
