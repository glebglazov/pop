package store

import (
	"path/filepath"
	"testing"
	"time"
)

// aliveByToken builds a liveness predicate over the (pid, proc_start) pairs of
// the drains it should treat as alive. A row whose pair is absent reads dead —
// modelling both a dead PID and a reused PID (same pid, different token).
func aliveByToken(alive ...Drain) Liveness {
	type key struct {
		pid       int
		procStart string
	}
	live := map[key]bool{}
	for _, d := range alive {
		live[key{d.PID, d.ProcStart}] = true
	}
	return func(pid int, procStart string) bool { return live[key{pid, procStart}] }
}

func TestReconcileCrashedTransitionsDeadPID(t *testing.T) {
	s := openTestStore(t, aliveByToken()) // nothing alive → dead PID
	d, err := s.StartDrain(Drain{Repo: "r", SetID: "s", RuntimePath: "/rt", PID: 100, ProcStart: "t1", StartedAt: time.Now()})
	if err != nil {
		t.Fatalf("StartDrain: %v", err)
	}

	now := time.Now().UTC().Truncate(time.Second)
	n, err := s.ReconcileCrashed(now)
	if err != nil {
		t.Fatalf("ReconcileCrashed: %v", err)
	}
	if n != 1 {
		t.Fatalf("reconciled %d, want 1", n)
	}
	// The running row is now an explicit crashed terminal, not a missing record.
	if live, _ := s.LiveDrainByRuntimePath("/rt"); live != nil {
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
	s := openTestStore(t, aliveByToken(Drain{PID: 100, ProcStart: "t1"}))
	d, err := s.StartDrain(Drain{Repo: "r", SetID: "s", RuntimePath: "/rt", PID: 100, ProcStart: "t1", StartedAt: time.Now()})
	if err != nil {
		t.Fatalf("StartDrain: %v", err)
	}

	n, err := s.ReconcileCrashed(time.Now().UTC())
	if err != nil {
		t.Fatalf("ReconcileCrashed: %v", err)
	}
	if n != 0 {
		t.Fatalf("reconciled %d, want 0 (drain is live)", n)
	}
	live, err := s.LiveDrainByRuntimePath("/rt")
	if err != nil {
		t.Fatalf("LiveDrainByRuntimePath: %v", err)
	}
	if live == nil || live.ID != d.ID || live.State != StateRunning {
		t.Fatalf("live drain wrongly transitioned: %+v", live)
	}
}

func TestReconcileCrashedDetectsReusedPID(t *testing.T) {
	// PID 100 is alive, but it now belongs to a different process (start token
	// "t2"), not this drain (token "t1"). The pair-keyed policy reads dead.
	s := openTestStore(t, aliveByToken(Drain{PID: 100, ProcStart: "t2"}))
	d, err := s.StartDrain(Drain{Repo: "r", SetID: "s", RuntimePath: "/rt", PID: 100, ProcStart: "t1", StartedAt: time.Now()})
	if err != nil {
		t.Fatalf("StartDrain: %v", err)
	}

	n, err := s.ReconcileCrashed(time.Now().UTC())
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

// TestOpenRejectsNilLiveness locks the construction-time contract: a store cannot
// be opened without a liveness policy, so crash healing can never be silently
// disabled (ADR-0118).
func TestOpenRejectsNilLiveness(t *testing.T) {
	if _, err := Open(filepath.Join(t.TempDir(), "pop.db"), nil); err == nil {
		t.Fatal("Open with nil liveness must fail")
	}
}
