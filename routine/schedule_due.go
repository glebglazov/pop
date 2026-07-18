package routine

import (
	"time"

	"github.com/glebglazov/pop/store"
)

// SkipReasonOverlap is recorded when a due fire is skipped because the previous
// run of the same routine is still live.
const SkipReasonOverlap = "previous run still live"

// IsDue reports whether a routine should fire at now given its schedule and the
// instant of its most recent non-skipped fire. A zero lastFired means the
// routine has never fired and is due immediately. When multiple schedule slots
// were missed, only one fire is due per evaluation — catch-up fires once, not
// once per missed slot.
func IsDue(sched Schedule, lastFired, now time.Time) bool {
	if lastFired.IsZero() {
		return true
	}
	next := sched.NextAfter(lastFired)
	return !next.After(now)
}

// LastFireTime returns the fired_at instant of the routine's most recent
// non-skipped run from the execution-state store.
func LastFireTime(s *store.Store, routineID string) (time.Time, error) {
	return s.LastRoutineFireTime(routineID)
}
