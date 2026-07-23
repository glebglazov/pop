package store

import (
	"errors"
	"testing"
	"time"
)

// aliveHoldsByToken builds a gate-hold liveness predicate over the (pid,
// proc_start) pairs it should treat as alive. A pair that is absent reads dead —
// modelling both a dead PID and a reused PID (same pid, different token).
func aliveHoldsByToken(alive ...CheckoutGateHold) Liveness {
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
	s := openTestStore(t, aliveHoldsByToken()) // nothing alive → dead owner
	if err := s.PutCheckoutGateHold(CheckoutGateHold{
		RuntimePath: "/rt", SetID: "s", PID: 100, ProcStart: "t1", RegisteredAt: time.Now(),
	}); err != nil {
		t.Fatalf("PutCheckoutGateHold: %v", err)
	}

	n, err := s.ReconcileGateHolds()
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
	live := CheckoutGateHold{RuntimePath: "/rt", SetID: "s", PID: 100, ProcStart: "t1", RegisteredAt: time.Now()}
	s := openTestStore(t, aliveHoldsByToken(live))
	if err := s.PutCheckoutGateHold(live); err != nil {
		t.Fatalf("PutCheckoutGateHold: %v", err)
	}

	n, err := s.ReconcileGateHolds()
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
	// PID 100 is alive again but belongs to a different process (start token t2):
	// a reused PID must not be mistaken for the original gate owner.
	reused := CheckoutGateHold{PID: 100, ProcStart: "t2"}
	s := openTestStore(t, aliveHoldsByToken(reused))
	if err := s.PutCheckoutGateHold(CheckoutGateHold{
		RuntimePath: "/rt", SetID: "s", PID: 100, ProcStart: "t1", RegisteredAt: time.Now(),
	}); err != nil {
		t.Fatalf("PutCheckoutGateHold: %v", err)
	}

	n, err := s.ReconcileGateHolds()
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

func TestPutCheckoutGateHoldRefusesLiveForeignOwner(t *testing.T) {
	// Only set-a's owner (pid 100 / t1) is alive; set-b's owner (pid 200 / t2) is not.
	s := openTestStore(t, aliveHoldsByToken(CheckoutGateHold{PID: 100, ProcStart: "t1"}))
	if err := s.PutCheckoutGateHold(CheckoutGateHold{
		RuntimePath: "/rt", SetID: "set-a", PID: 100, ProcStart: "t1",
	}); err != nil {
		t.Fatalf("PutCheckoutGateHold (set-a): %v", err)
	}

	// set-b tries to take over a hold whose owner is still live — no steal.
	err := s.PutCheckoutGateHold(CheckoutGateHold{
		RuntimePath: "/rt", SetID: "set-b", PID: 200, ProcStart: "t2",
	})
	if !errors.Is(err, ErrGateHoldHeld) {
		t.Fatalf("err = %v, want ErrGateHoldHeld", err)
	}
	got, err := s.GetCheckoutGateHold("/rt")
	if err != nil {
		t.Fatalf("GetCheckoutGateHold: %v", err)
	}
	if got == nil || got.SetID != "set-a" {
		t.Fatalf("hold = %+v, want set-a's still held", got)
	}
}

func TestPutCheckoutGateHoldReplacesDeadForeignOwner(t *testing.T) {
	// set-b (pid 200 / t2) is the only live owner; set-a's owner is dead.
	s := openTestStore(t, aliveHoldsByToken(CheckoutGateHold{PID: 200, ProcStart: "t2"}))
	if err := s.PutCheckoutGateHold(CheckoutGateHold{
		RuntimePath: "/rt", SetID: "set-a", PID: 100, ProcStart: "t1",
	}); err != nil {
		t.Fatalf("PutCheckoutGateHold (set-a): %v", err)
	}

	// set-a's owner is dead, so set-b may replace it.
	if err := s.PutCheckoutGateHold(CheckoutGateHold{
		RuntimePath: "/rt", SetID: "set-b", PID: 200, ProcStart: "t2",
	}); err != nil {
		t.Fatalf("PutCheckoutGateHold (set-b) over dead owner: %v", err)
	}
	got, err := s.GetCheckoutGateHold("/rt")
	if err != nil {
		t.Fatalf("GetCheckoutGateHold: %v", err)
	}
	if got == nil || got.SetID != "set-b" {
		t.Fatalf("hold = %+v, want set-b after replacing dead owner", got)
	}
}

func TestPutCheckoutGateHoldRefreshesOwnHold(t *testing.T) {
	live := CheckoutGateHold{PID: 100, ProcStart: "t1"}
	s := openTestStore(t, aliveHoldsByToken(live))
	if err := s.PutCheckoutGateHold(CheckoutGateHold{
		RuntimePath: "/rt", SetID: "set-a", PID: 100, ProcStart: "t1", Claim: false,
	}); err != nil {
		t.Fatalf("PutCheckoutGateHold: %v", err)
	}
	// The same set re-registers, now claim-bearing: its own live hold is refreshed,
	// never refused.
	if err := s.PutCheckoutGateHold(CheckoutGateHold{
		RuntimePath: "/rt", SetID: "set-a", PID: 100, ProcStart: "t1", Claim: true,
	}); err != nil {
		t.Fatalf("PutCheckoutGateHold (refresh): %v", err)
	}
	got, err := s.GetCheckoutGateHold("/rt")
	if err != nil {
		t.Fatalf("GetCheckoutGateHold: %v", err)
	}
	if got == nil || !got.Claim {
		t.Fatalf("hold = %+v, want refreshed claim-bearing hold", got)
	}
}

func TestDeleteCheckoutGateHoldOwnerChecked(t *testing.T) {
	s := openTestStore(t, aliveHoldsByToken(CheckoutGateHold{PID: 100, ProcStart: "t1"}))
	if err := s.PutCheckoutGateHold(CheckoutGateHold{
		RuntimePath: "/rt", SetID: "set-a", PID: 100, ProcStart: "t1",
	}); err != nil {
		t.Fatalf("PutCheckoutGateHold: %v", err)
	}

	// A different set's release must not remove set-a's hold.
	if err := s.DeleteCheckoutGateHold("/rt", "set-b"); err != nil {
		t.Fatalf("DeleteCheckoutGateHold (foreign): %v", err)
	}
	if got, _ := s.GetCheckoutGateHold("/rt"); got == nil {
		t.Fatal("foreign-set release wrongly removed set-a's hold")
	}

	// The owner's own release removes it.
	if err := s.DeleteCheckoutGateHold("/rt", "set-a"); err != nil {
		t.Fatalf("DeleteCheckoutGateHold (owner): %v", err)
	}
	if got, _ := s.GetCheckoutGateHold("/rt"); got != nil {
		t.Fatalf("owner release did not remove the hold: %+v", got)
	}
}
