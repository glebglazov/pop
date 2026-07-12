package store

import (
	"testing"
	"time"
)

// aliveHoldsByToken builds a gate-hold liveness predicate over the (pid,
// proc_start) pairs it should treat as alive. A pair that is absent reads dead —
// modelling both a dead PID and a reused PID (same pid, different token).
func aliveHoldsByToken(alive ...CheckoutGateHold) func(pid int, procStart string) bool {
	type key struct {
		pid       int
		procStart string
	}
	live := map[key]bool{}
	for _, h := range alive {
		live[key{h.PID, h.ProcStart}] = true
	}
	return func(pid int, procStart string) bool { return live[key{pid, procStart}] }
}

func TestReconcileGateHoldsSweepsDeadOwner(t *testing.T) {
	s := openTestStore(t)
	if err := s.PutCheckoutGateHold(CheckoutGateHold{
		RuntimePath: "/rt", SetID: "s", PID: 100, ProcStart: "t1", RegisteredAt: time.Now(),
	}); err != nil {
		t.Fatalf("PutCheckoutGateHold: %v", err)
	}

	n, err := s.ReconcileGateHolds(aliveHoldsByToken()) // nothing alive → dead owner
	if err != nil {
		t.Fatalf("ReconcileGateHolds: %v", err)
	}
	if n != 1 {
		t.Fatalf("swept %d, want 1", n)
	}
	got, err := s.GetCheckoutGateHold("/rt")
	if err != nil {
		t.Fatalf("GetCheckoutGateHold: %v", err)
	}
	if got != nil {
		t.Fatalf("dead-owner hold survived sweep: %+v", got)
	}
}

func TestReconcileGateHoldsLeavesLiveOwner(t *testing.T) {
	s := openTestStore(t)
	live := CheckoutGateHold{RuntimePath: "/rt", SetID: "s", PID: 100, ProcStart: "t1", RegisteredAt: time.Now()}
	if err := s.PutCheckoutGateHold(live); err != nil {
		t.Fatalf("PutCheckoutGateHold: %v", err)
	}

	n, err := s.ReconcileGateHolds(aliveHoldsByToken(live))
	if err != nil {
		t.Fatalf("ReconcileGateHolds: %v", err)
	}
	if n != 0 {
		t.Fatalf("swept %d, want 0 (owner is live)", n)
	}
	got, err := s.GetCheckoutGateHold("/rt")
	if err != nil {
		t.Fatalf("GetCheckoutGateHold: %v", err)
	}
	if got == nil {
		t.Fatal("live-owner hold was wrongly swept")
	}
	if got.PID != 100 || got.ProcStart != "t1" {
		t.Fatalf("owner identity not round-tripped: %+v", got)
	}
}

func TestReconcileGateHoldsSweepsReusedPID(t *testing.T) {
	s := openTestStore(t)
	if err := s.PutCheckoutGateHold(CheckoutGateHold{
		RuntimePath: "/rt", SetID: "s", PID: 100, ProcStart: "t1", RegisteredAt: time.Now(),
	}); err != nil {
		t.Fatalf("PutCheckoutGateHold: %v", err)
	}

	// PID 100 is alive again but belongs to a different process (start token t2):
	// a reused PID must not be mistaken for the original gate owner.
	reused := CheckoutGateHold{PID: 100, ProcStart: "t2"}
	n, err := s.ReconcileGateHolds(aliveHoldsByToken(reused))
	if err != nil {
		t.Fatalf("ReconcileGateHolds: %v", err)
	}
	if n != 1 {
		t.Fatalf("swept %d, want 1 (PID reused)", n)
	}
	got, err := s.GetCheckoutGateHold("/rt")
	if err != nil {
		t.Fatalf("GetCheckoutGateHold: %v", err)
	}
	if got != nil {
		t.Fatalf("reused-PID hold survived sweep: %+v", got)
	}
}

func TestReconcileGateHoldsNilPredicateIsNoOp(t *testing.T) {
	s := openTestStore(t)
	if err := s.PutCheckoutGateHold(CheckoutGateHold{
		RuntimePath: "/rt", SetID: "s", PID: 100, ProcStart: "t1", RegisteredAt: time.Now(),
	}); err != nil {
		t.Fatalf("PutCheckoutGateHold: %v", err)
	}
	n, err := s.ReconcileGateHolds(nil)
	if err != nil {
		t.Fatalf("ReconcileGateHolds: %v", err)
	}
	if n != 0 {
		t.Fatalf("swept %d, want 0", n)
	}
	if got, _ := s.GetCheckoutGateHold("/rt"); got == nil {
		t.Fatal("nil predicate wrongly swept a hold")
	}
}
