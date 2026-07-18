package store

import (
	"testing"
	"time"
)

func TestListRoutineRunsNewestFirst(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/pop.db"
	s, err := Open(path, allAlive(true))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = s.Close() }()

	firstAt := time.Date(2026, 7, 18, 10, 0, 0, 0, time.UTC)
	secondAt := time.Date(2026, 7, 18, 16, 0, 0, 0, time.UTC)
	run1, err := s.StartRoutineRun(RoutineRun{
		RoutineID: "daily",
		FiredAt:   firstAt,
		PID:       100,
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.FinishRoutineRun(run1.ID, RoutineRunSucceeded, "/first.md", "", firstAt); err != nil {
		t.Fatal(err)
	}
	run2, err := s.StartRoutineRun(RoutineRun{
		RoutineID: "daily",
		FiredAt:   secondAt,
		PID:       101,
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.FinishRoutineRun(run2.ID, RoutineRunFailed, "/second.md", "failed", secondAt); err != nil {
		t.Fatal(err)
	}

	runs, err := s.ListRoutineRuns("daily")
	if err != nil {
		t.Fatal(err)
	}
	if len(runs) != 2 {
		t.Fatalf("len = %d", len(runs))
	}
	if runs[0].ID != run2.ID || runs[1].ID != run1.ID {
		t.Fatalf("order = [%d, %d], want [%d, %d]", runs[0].ID, runs[1].ID, run2.ID, run1.ID)
	}
}
