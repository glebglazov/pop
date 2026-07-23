package store

import (
	"testing"
	"time"
)

// aliveWaitersByToken builds a recovery-waiter liveness predicate over the (pid,
// proc_start) pairs it should treat as alive. A pair that is absent reads dead —
// modelling both a dead PID and a reused PID (same pid, different token).
func aliveWaitersByToken(alive ...RecoveryWaiter) Liveness {
	type key struct {
		pid       int
		procStart string
	}
	live := map[key]bool{}
	for _, w := range alive {
		live[key{w.PID, w.ProcStart}] = true
	}
	return func(pid int, procStart string) bool { return live[key{pid, procStart}] }
}

func waiterFixture(setID string, pid int, procStart string) RecoveryWaiter {
	return RecoveryWaiter{
		SetID:        setID,
		Preset:       "claude",
		ResetAt:      time.Now().Add(-time.Hour).UTC(),
		RuntimePath:  "/rt",
		Priority:     0,
		PID:          pid,
		ProcStart:    procStart,
		RegisteredAt: time.Now().UTC(),
	}
}

func TestReconcileRecoveryWaitersSweepsDeadOwner(t *testing.T) {
	s := openTestStore(t, aliveWaitersByToken()) // nothing alive → dead owner
	if err := s.PutRecoveryWaiter(waiterFixture("s", 100, "t1")); err != nil {
		t.Fatalf("PutRecoveryWaiter: %v", err)
	}

	n, err := s.ReconcileRecoveryWaiters()
	if err != nil {
		t.Fatalf("ReconcileRecoveryWaiters: %v", err)
	}
	if n != 1 {
		t.Fatalf("swept %d, want 1", n)
	}
	got, err := s.GetRecoveryWaiter("s")
	if err != nil {
		t.Fatalf("GetRecoveryWaiter: %v", err)
	}
	if got != nil {
		t.Fatalf("dead-owner waiter survived sweep: %+v", got)
	}
}

func TestReconcileRecoveryWaitersLeavesLiveOwner(t *testing.T) {
	live := waiterFixture("s", 100, "t1")
	s := openTestStore(t, aliveWaitersByToken(live))
	if err := s.PutRecoveryWaiter(live); err != nil {
		t.Fatalf("PutRecoveryWaiter: %v", err)
	}

	n, err := s.ReconcileRecoveryWaiters()
	if err != nil {
		t.Fatalf("ReconcileRecoveryWaiters: %v", err)
	}
	if n != 0 {
		t.Fatalf("swept %d, want 0 (owner is live)", n)
	}
	got, err := s.GetRecoveryWaiter("s")
	if err != nil {
		t.Fatalf("GetRecoveryWaiter: %v", err)
	}
	if got == nil {
		t.Fatal("live-owner waiter was wrongly swept")
	}
	if got.PID != 100 || got.ProcStart != "t1" {
		t.Fatalf("owner identity not round-tripped: %+v", got)
	}
}

func TestReconcileRecoveryWaitersSweepsReusedPID(t *testing.T) {
	// PID 100 is alive again but belongs to a different process (start token t2):
	// a reused PID must not be mistaken for the original waiter owner.
	reused := RecoveryWaiter{PID: 100, ProcStart: "t2"}
	s := openTestStore(t, aliveWaitersByToken(reused))
	if err := s.PutRecoveryWaiter(waiterFixture("s", 100, "t1")); err != nil {
		t.Fatalf("PutRecoveryWaiter: %v", err)
	}

	n, err := s.ReconcileRecoveryWaiters()
	if err != nil {
		t.Fatalf("ReconcileRecoveryWaiters: %v", err)
	}
	if n != 1 {
		t.Fatalf("swept %d, want 1 (PID reused)", n)
	}
	got, err := s.GetRecoveryWaiter("s")
	if err != nil {
		t.Fatalf("GetRecoveryWaiter: %v", err)
	}
	if got != nil {
		t.Fatalf("reused-PID waiter survived sweep: %+v", got)
	}
}
