package store

import (
	"testing"
	"time"
)

// aliveByToken builds a liveness predicate over the (pid, proc_start) pairs of
// the drains it should treat as alive. A row whose pair is absent reads dead —
// modelling both a dead PID and a reused PID (same pid, different token).
func aliveByToken(alive ...Drain) func(Drain) bool {
	type key struct {
		pid       int
		procStart string
	}
	live := map[key]bool{}
	for _, d := range alive {
		live[key{d.PID, d.ProcStart}] = true
	}
	return func(d Drain) bool { return live[key{d.PID, d.ProcStart}] }
}

func TestReconcileCrashedTransitionsDeadPID(t *testing.T) {
	s := openTestStore(t)
	d, err := s.StartDrain(Drain{Repo: "r", SetID: "s", RuntimePath: "/rt", PID: 100, ProcStart: "t1", StartedAt: time.Now()}, allAlive(false))
	if err != nil {
		t.Fatalf("StartDrain: %v", err)
	}

	now := time.Now().UTC().Truncate(time.Second)
	n, err := s.ReconcileCrashed(aliveByToken(), now) // nothing alive → dead PID
	if err != nil {
		t.Fatalf("ReconcileCrashed: %v", err)
	}
	if n != 1 {
		t.Fatalf("reconciled %d, want 1", n)
	}
	// The running row is now an explicit crashed terminal, not a missing record.
	if live, _ := s.LiveDrainByRuntimePath("/rt", allAlive(true)); live != nil {
		t.Fatalf("expected no live drain after crash, got %+v", live)
	}
	term, err := s.LatestTerminalByRuntimePath("/rt")
	if err != nil {
		t.Fatalf("LatestTerminalByRuntimePath: %v", err)
	}
	if term == nil || term.ID != d.ID {
		t.Fatalf("expected crashed terminal for drain %d, got %+v", d.ID, term)
	}
	if term.State != StateCrashed {
		t.Fatalf("state = %q, want crashed", term.State)
	}
	if !term.FinishedAt.Equal(now) {
		t.Fatalf("finished_at = %v, want %v", term.FinishedAt, now)
	}
}

func TestReconcileCrashedLeavesLiveDrain(t *testing.T) {
	s := openTestStore(t)
	d, err := s.StartDrain(Drain{Repo: "r", SetID: "s", RuntimePath: "/rt", PID: 100, ProcStart: "t1", StartedAt: time.Now()}, allAlive(false))
	if err != nil {
		t.Fatalf("StartDrain: %v", err)
	}

	n, err := s.ReconcileCrashed(aliveByToken(d), time.Now().UTC())
	if err != nil {
		t.Fatalf("ReconcileCrashed: %v", err)
	}
	if n != 0 {
		t.Fatalf("reconciled %d, want 0 (drain is live)", n)
	}
	live, err := s.LiveDrainByRuntimePath("/rt", allAlive(true))
	if err != nil {
		t.Fatalf("LiveDrainByRuntimePath: %v", err)
	}
	if live == nil || live.ID != d.ID || live.State != StateRunning {
		t.Fatalf("live drain wrongly transitioned: %+v", live)
	}
}

func TestReconcileCrashedDetectsReusedPID(t *testing.T) {
	s := openTestStore(t)
	d, err := s.StartDrain(Drain{Repo: "r", SetID: "s", RuntimePath: "/rt", PID: 100, ProcStart: "t1", StartedAt: time.Now()}, allAlive(false))
	if err != nil {
		t.Fatalf("StartDrain: %v", err)
	}

	// PID 100 is alive, but it now belongs to a different process (start token
	// "t2"), not this drain (token "t1"). The pair-keyed predicate reads dead.
	reused := Drain{PID: 100, ProcStart: "t2"}
	n, err := s.ReconcileCrashed(aliveByToken(reused), time.Now().UTC())
	if err != nil {
		t.Fatalf("ReconcileCrashed: %v", err)
	}
	if n != 1 {
		t.Fatalf("reconciled %d, want 1 (PID reused)", n)
	}
	term, _ := s.LatestTerminalByRuntimePath("/rt")
	if term == nil || term.ID != d.ID || term.State != StateCrashed {
		t.Fatalf("expected crashed terminal for reused PID, got %+v", term)
	}
}

func TestReconcileCrashedNilPredicateIsNoOp(t *testing.T) {
	s := openTestStore(t)
	if _, err := s.StartDrain(Drain{Repo: "r", SetID: "s", RuntimePath: "/rt", PID: 100, ProcStart: "t1", StartedAt: time.Now()}, allAlive(false)); err != nil {
		t.Fatalf("StartDrain: %v", err)
	}
	n, err := s.ReconcileCrashed(nil, time.Now().UTC())
	if err != nil {
		t.Fatalf("ReconcileCrashed: %v", err)
	}
	if n != 0 {
		t.Fatalf("reconciled %d, want 0 for nil predicate", n)
	}
	if term, _ := s.LatestTerminalByRuntimePath("/rt"); term != nil {
		t.Fatalf("nil predicate must touch nothing, got %+v", term)
	}
}
