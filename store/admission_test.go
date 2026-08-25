package store

import (
	"testing"
	"time"
)

// queueOn registers a waiter for setID on /rt and returns it with its assigned
// place in the line.
func queueOn(t *testing.T, s *Store, setID string, pid int) AdmissionWaiter {
	t.Helper()
	w, err := s.RegisterAdmissionWaiter(AdmissionWaiter{
		RuntimePath:  "/rt",
		Repo:         "repo",
		SetID:        setID,
		PID:          pid,
		ProcStart:    "tok",
		RegisteredAt: time.Now().UTC(),
	})
	if err != nil {
		t.Fatalf("RegisterAdmissionWaiter(%s): %v", setID, err)
	}
	return w
}

func admissionDrain(setID string) Drain {
	return Drain{Repo: "repo", SetID: setID, RuntimePath: "/rt", PID: 4242, ProcStart: "tok", StartedAt: time.Now().UTC()}
}

// Three commands ask in turn for one checkout while a drain holds it. When it
// finishes they must go in the order they asked and in no other — that is the
// whole promise of the queue, and it is deliberately blind to which set anyone
// would otherwise rank first.
func TestAdmissionIsGrantedInRegistrationOrder(t *testing.T) {
	s := openTestStore(t)
	holder, err := s.StartDrain(Drain{Repo: "repo", SetID: "holder", RuntimePath: "/rt", PID: 1, StartedAt: time.Now()})
	if err != nil {
		t.Fatalf("holder StartDrain: %v", err)
	}

	first := queueOn(t, s, "set-a", 11)
	second := queueOn(t, s, "set-b", 12)
	third := queueOn(t, s, "set-c", 13)

	// While the holder drains, nobody is admitted — not even the first in line.
	if _, block, err := s.TryAdmitDrain(admissionDrain("set-a"), first.ID); err != nil {
		t.Fatalf("TryAdmitDrain: %v", err)
	} else if block == nil || block.Kind != AdmissionBlockCheckoutClaimed {
		t.Fatalf("expected a checkout-claim block while the holder drains, got %+v", block)
	}

	if err := s.FinishDrain(holder.ID, DrainEnding{State: StateFinished}, time.Now()); err != nil {
		t.Fatalf("FinishDrain: %v", err)
	}

	// With the checkout free the two behind the head are still held by the queue,
	// and each names the set that asked before it.
	for _, behind := range []struct {
		waiter AdmissionWaiter
		set    string
		ahead  string
	}{
		{second, "set-b", "set-a"},
		{third, "set-c", "set-a"},
	} {
		_, block, err := s.TryAdmitDrain(admissionDrain(behind.set), behind.waiter.ID)
		if err != nil {
			t.Fatalf("TryAdmitDrain(%s): %v", behind.set, err)
		}
		if block == nil || block.Kind != AdmissionBlockBehindWaiter {
			t.Fatalf("%s: expected a queue-position block, got %+v", behind.set, block)
		}
		if block.AheadSetID != behind.ahead {
			t.Fatalf("%s: blocked behind %q, want %q", behind.set, block.AheadSetID, behind.ahead)
		}
	}

	granted, block, err := s.TryAdmitDrain(admissionDrain("set-a"), first.ID)
	if err != nil || block != nil {
		t.Fatalf("head of the queue must be admitted: block=%+v err=%v", block, err)
	}
	// The grant consumed the registration: a granted waiter no longer holds a
	// place, so the queue moves up rather than blocking on a ghost.
	line, err := s.AdmissionWaitersOn("/rt")
	if err != nil {
		t.Fatalf("AdmissionWaitersOn: %v", err)
	}
	if len(line) != 2 || line[0].SetID != "set-b" {
		t.Fatalf("after the grant the queue is %+v, want set-b then set-c", line)
	}

	if err := s.FinishDrain(granted.ID, DrainEnding{State: StateFinished}, time.Now()); err != nil {
		t.Fatalf("FinishDrain: %v", err)
	}
	// Second asked second, so second goes second — third still waits behind it.
	if _, block, err := s.TryAdmitDrain(admissionDrain("set-c"), third.ID); err != nil {
		t.Fatalf("TryAdmitDrain(set-c): %v", err)
	} else if block == nil || block.AheadSetID != "set-b" {
		t.Fatalf("set-c must still be behind set-b, got %+v", block)
	}
	if _, block, err := s.TryAdmitDrain(admissionDrain("set-b"), second.ID); err != nil || block != nil {
		t.Fatalf("set-b must be admitted second: block=%+v err=%v", block, err)
	}
}

// A grant takes the checkout and the set in one act. When either is held the
// waiter comes away with nothing at all, so it can never be one link of a
// deadlock cycle while it waits.
func TestAdmissionGrantsTheWholeLockSetOrNothing(t *testing.T) {
	s := openTestStore(t)
	// The set is draining in a different checkout: the tree the waiter wants is
	// free, but its set is not.
	if _, err := s.StartDrain(Drain{Repo: "repo", SetID: "demo", RuntimePath: "/elsewhere", PID: 1, StartedAt: time.Now()}); err != nil {
		t.Fatalf("elsewhere StartDrain: %v", err)
	}
	waiter := queueOn(t, s, "demo", 11)

	_, block, err := s.TryAdmitDrain(admissionDrain("demo"), waiter.ID)
	if err != nil {
		t.Fatalf("TryAdmitDrain: %v", err)
	}
	if block == nil || block.Kind != AdmissionBlockSetClaimed {
		t.Fatalf("expected a set-claim block, got %+v", block)
	}
	if block.Set == nil || block.Set.RuntimePath != "/elsewhere" {
		t.Fatalf("the block must name where the set is draining, got %+v", block.Set)
	}
	// Nothing was taken: no running row appeared on the checkout it wanted.
	live, err := s.LiveDrainByRuntimePath("/rt")
	if err != nil {
		t.Fatalf("LiveDrainByRuntimePath: %v", err)
	}
	if live != nil {
		t.Fatalf("a refused grant left a partial claim on /rt: %+v", live)
	}
	// And the waiter kept its place rather than losing it to the failed attempt.
	if line, err := s.AdmissionWaitersOn("/rt"); err != nil || len(line) != 1 {
		t.Fatalf("waiter must keep its place after a refused grant: %+v (%v)", line, err)
	}
}

// A waiter whose process died must not stall the strict-FIFO queue behind it:
// the grant steps over it, and the reconcile pass removes the row.
func TestDeadWaiterNeitherBlocksNorSurvivesReconcile(t *testing.T) {
	live := map[int]bool{11: false, 12: true}
	s := openTestStore(t, func(pid int, _ string) bool { return live[pid] })

	dead := queueOn(t, s, "set-a", 11)
	behind := queueOn(t, s, "set-b", 12)

	if _, block, err := s.TryAdmitDrain(admissionDrain("set-b"), behind.ID); err != nil || block != nil {
		t.Fatalf("a dead head must not block the queue: block=%+v err=%v", block, err)
	}

	swept, err := s.ReconcileAdmissionWaiters()
	if err != nil {
		t.Fatalf("ReconcileAdmissionWaiters: %v", err)
	}
	if swept != 1 {
		t.Fatalf("swept %d waiters, want the one dead owner", swept)
	}
	line, err := s.AdmissionWaitersOn("/rt")
	if err != nil {
		t.Fatalf("AdmissionWaitersOn: %v", err)
	}
	for _, w := range line {
		if w.ID == dead.ID {
			t.Fatalf("the dead owner's waiter survived the sweep: %+v", line)
		}
	}
}

// A set parked at a Failed gate over a dirty tree holds the checkout without a
// running drain. A waiter must hear that as the gate — the thing a human can go
// and answer — rather than as a bare busy signal.
func TestAdmissionBlockNamesAGateHold(t *testing.T) {
	s := openTestStore(t)
	if err := s.PutCheckoutGateHold(CheckoutGateHold{
		RuntimePath: "/rt", SetID: "parked", PID: 77, ProcStart: "tok", Claim: true,
		RegisteredAt: time.Now().UTC(),
	}); err != nil {
		t.Fatalf("PutCheckoutGateHold: %v", err)
	}
	waiter := queueOn(t, s, "demo", 11)

	_, block, err := s.TryAdmitDrain(admissionDrain("demo"), waiter.ID)
	if err != nil {
		t.Fatalf("TryAdmitDrain: %v", err)
	}
	if block == nil || block.Checkout == nil {
		t.Fatalf("expected a checkout-claim block, got %+v", block)
	}
	if block.Checkout.Reason != ClaimFailedGate || block.Checkout.Holder.ContainerID != "parked" || block.Checkout.PID != 77 {
		t.Fatalf("the block must name the parked set, its reason and its PID: %+v", block.Checkout)
	}
}
