package routine

import (
	"testing"
	"time"
)

func TestIsDueNeverFiredIsNeverDue(t *testing.T) {
	// A routine with zero non-skipped runs is anchored by a human's manual
	// first fire, not by the daemon (ADR-0124). It must never be due,
	// whatever its schedule form.
	now := time.Date(2026, 7, 18, 12, 0, 0, 0, time.UTC)
	for _, raw := range []string{"every 6h", "daily at 10:00"} {
		sched, err := ParseSchedule(raw)
		if err != nil {
			t.Fatalf("parse %q: %v", raw, err)
		}
		if IsDue(sched, time.Time{}, now) {
			t.Fatalf("never-fired routine (%q) must not be due", raw)
		}
	}
}

func TestIsDueEverySchedule(t *testing.T) {
	sched, err := ParseSchedule("every 1h")
	if err != nil {
		t.Fatal(err)
	}
	last := time.Date(2026, 7, 18, 10, 0, 0, 0, time.UTC)

	if IsDue(sched, last, last.Add(30*time.Minute)) {
		t.Fatal("should not be due before interval elapses")
	}
	if !IsDue(sched, last, last.Add(time.Hour)) {
		t.Fatal("should be due when interval elapsed")
	}
}

func TestIsDueDailySchedule(t *testing.T) {
	loc := time.FixedZone("test", 0)
	sched, err := ParseSchedule("daily at 10:00")
	if err != nil {
		t.Fatal(err)
	}
	last := time.Date(2026, 7, 18, 10, 0, 0, 0, loc)

	if IsDue(sched, last, time.Date(2026, 7, 18, 9, 30, 0, 0, loc)) {
		t.Fatal("should not be due before next slot")
	}
	if !IsDue(sched, last, time.Date(2026, 7, 19, 10, 0, 0, 0, loc)) {
		t.Fatal("should be due on next day's slot")
	}
}

func TestIsDueCatchUpOnceNotPerMissedSlot(t *testing.T) {
	sched, err := ParseSchedule("every 1h")
	if err != nil {
		t.Fatal(err)
	}
	last := time.Date(2026, 7, 18, 10, 0, 0, 0, time.UTC)
	now := time.Date(2026, 7, 18, 13, 30, 0, 0, time.UTC) // three missed hourly slots

	if !IsDue(sched, last, now) {
		t.Fatal("missed slots should still evaluate as due once")
	}
	// After one fire at now, the next due is one interval later — not two more catch-ups.
	if IsDue(sched, now, now) {
		t.Fatal("immediately after firing, should not be due again")
	}
	if IsDue(sched, now, now.Add(30*time.Minute)) {
		t.Fatal("half interval after fire should not be due")
	}
	if !IsDue(sched, now, now.Add(time.Hour)) {
		t.Fatal("next interval after fire should be due")
	}
}
