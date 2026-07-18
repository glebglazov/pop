package store

import (
	"testing"
	"time"
)

func TestLastRoutineFireTimeIgnoresSkipped(t *testing.T) {
	s := openTestStore(t)
	now := time.Date(2026, 7, 18, 10, 0, 0, 0, time.UTC)
	if _, err := s.InsertSkippedRoutineRun(RoutineRun{
		RoutineID:  "daily",
		FiredAt:    now,
		SkipReason: "previous run still live",
	}); err != nil {
		t.Fatal(err)
	}
	got, err := s.LastRoutineFireTime("daily")
	if err != nil {
		t.Fatal(err)
	}
	if !got.IsZero() {
		t.Fatalf("last fire = %v, want zero when only skipped rows exist", got)
	}

	fired := now.Add(time.Hour)
	if _, err := s.StartRoutineRun(RoutineRun{
		RoutineID: "daily",
		FiredAt:   fired,
		PID:       100,
	}, nil); err != nil {
		t.Fatal(err)
	}
	if err := s.FinishRoutineRun(1, RoutineRunSucceeded, "", "", fired); err != nil {
		t.Fatal(err)
	}
	got, err = s.LastRoutineFireTime("daily")
	if err != nil {
		t.Fatal(err)
	}
	if !got.Equal(fired) {
		t.Fatalf("last fire = %v, want %v", got, fired)
	}
}

func TestReconcileCrashedRoutineRunsTransitionsDeadPID(t *testing.T) {
	s := openTestStore(t)
	now := time.Date(2026, 7, 18, 12, 0, 0, 0, time.UTC)
	if _, err := s.StartRoutineRun(RoutineRun{
		RoutineID: "daily",
		FiredAt:   now,
		PID:       100,
		ProcStart: "dead",
	}, func(RoutineRun) bool { return false }); err != nil {
		t.Fatal(err)
	}
	n, err := s.ReconcileCrashedRoutineRuns(func(RoutineRun) bool { return false }, now)
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("reconciled %d rows, want 1", n)
	}
	row, err := s.LastRoutineRun("daily")
	if err != nil {
		t.Fatal(err)
	}
	if row.Outcome != RoutineRunFailed {
		t.Fatalf("outcome = %q", row.Outcome)
	}
}

func TestLiveRoutineRunReturnsAliveRow(t *testing.T) {
	s := openTestStore(t)
	now := time.Now().UTC()
	if _, err := s.StartRoutineRun(RoutineRun{
		RoutineID: "daily",
		FiredAt:   now,
		PID:       100,
		ProcStart: "live",
	}, func(RoutineRun) bool { return true }); err != nil {
		t.Fatal(err)
	}
	live, err := s.LiveRoutineRun("daily", func(RoutineRun) bool { return true })
	if err != nil {
		t.Fatal(err)
	}
	if live == nil || live.Outcome != RoutineRunRunning {
		t.Fatalf("live = %+v", live)
	}
}
