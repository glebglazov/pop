package routine

import (
	"time"

	"github.com/glebglazov/pop/store"
)

// SkipReasonOverlap is recorded when a due fire is skipped because the previous
// run of the same routine is still live.
const SkipReasonOverlap = "previous run still live"

// SkipReasonChanged is recorded when a due fire is skipped because the routine's
// run-affecting inputs drifted since its last run (ADR-0128). The same dispatch
// pauses the routine with reason `changed`, so exactly one such row is written
// per drift: every later tick stops at the pause bit and never reaches the
// fingerprint check.
const SkipReasonChanged = "run-affecting inputs drifted"

// IsDue reports whether a routine should fire at now given its schedule and the
// instant of its most recent non-skipped fire. A zero lastFired means the
// routine has never fired and is never due: the first fire is a human act
// (pop routine fire or the refinement gate's fire verb) that anchors the
// schedule (ADR-0124). Once anchored, when multiple schedule slots were missed
// only one fire is due per evaluation — catch-up fires once, not once per
// missed slot.
func IsDue(sched Schedule, lastFired, now time.Time) bool {
	if lastFired.IsZero() {
		return false
	}
	next := sched.NextAfter(lastFired)
	return !next.After(now)
}

// LastFireTime returns the fired_at instant of the routine's most recent
// non-skipped run from the execution-state store.
func LastFireTime(s *store.Store, routineID string) (time.Time, error) {
	return s.LastRoutineFireTime(routineID)
}
