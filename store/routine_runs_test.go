package store

import (
	"testing"
	"time"
)

func TestStartRoutineRunRefusesConcurrentLiveRun(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/pop.db"
	s, err := Open(path, allAlive(true))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = s.Close() }()

	alive := func(RoutineRun) bool { return true }
	first, err := s.StartRoutineRun(RoutineRun{
		RoutineID: "daily",
		FiredAt:   time.Now().UTC(),
		PID:       100,
	}, alive)
	if err != nil {
		t.Fatal(err)
	}
	if first.Outcome != RoutineRunRunning {
		t.Fatalf("outcome = %q", first.Outcome)
	}

	_, err = s.StartRoutineRun(RoutineRun{
		RoutineID: "daily",
		FiredAt:   time.Now().UTC(),
		PID:       200,
	}, alive)
	if err == nil {
		t.Fatal("expected ErrRoutineRunInProgress")
	}
	if err != ErrRoutineRunInProgress {
		t.Fatalf("err = %v", err)
	}
}

func TestStartRoutineRunAllowsAfterDeadProcess(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/pop.db"
	s, err := Open(path, allAlive(true))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = s.Close() }()

	alive := func(RoutineRun) bool { return false }
	if _, err := s.StartRoutineRun(RoutineRun{
		RoutineID: "daily",
		FiredAt:   time.Now().UTC(),
		PID:       100,
	}, alive); err != nil {
		t.Fatal(err)
	}
	if _, err := s.StartRoutineRun(RoutineRun{
		RoutineID: "daily",
		FiredAt:   time.Now().UTC(),
		PID:       200,
	}, alive); err != nil {
		t.Fatalf("expected new run after dead process, got %v", err)
	}
}

func TestFinishRoutineRunPersistsOutcome(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/pop.db"
	s, err := Open(path, allAlive(true))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = s.Close() }()

	run, err := s.StartRoutineRun(RoutineRun{
		RoutineID: "daily",
		FiredAt:   time.Now().UTC(),
		PID:       100,
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	finishedAt := time.Now().UTC()
	if err := s.FinishRoutineRun(run.ID, RoutineRunFailed, "", "agent exited with status 1", finishedAt); err != nil {
		t.Fatal(err)
	}
	row, err := s.LastRoutineRun("daily")
	if err != nil {
		t.Fatal(err)
	}
	if row.Outcome != RoutineRunFailed || row.FailReason == "" {
		t.Fatalf("row = %+v", row)
	}
}
