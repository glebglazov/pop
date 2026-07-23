package store

import (
	"errors"
	"testing"
	"time"
)

func putWaiter(t *testing.T, s *Store, setID, path string, pid int, procStart string) {
	t.Helper()
	if err := s.PutRecoveryWaiter(RecoveryWaiter{
		SetID:        setID,
		Preset:       "claude",
		ResetAt:      time.Now().Add(-time.Hour).UTC(),
		RuntimePath:  path,
		PID:          pid,
		ProcStart:    procStart,
		RegisteredAt: time.Now().UTC(),
	}); err != nil {
		t.Fatalf("PutRecoveryWaiter: %v", err)
	}
}

func putGateHold(t *testing.T, s *Store, setID, path string, pid int, procStart string, claim bool) {
	t.Helper()
	if err := s.PutCheckoutGateHold(CheckoutGateHold{
		SetID:        setID,
		RuntimePath:  path,
		PID:          pid,
		ProcStart:    procStart,
		Claim:        claim,
		RegisteredAt: time.Now().UTC(),
	}); err != nil {
		t.Fatalf("PutCheckoutGateHold: %v", err)
	}
}

func TestReadCheckoutClaimFailedGate(t *testing.T) {
	s := openTestStore(t, aliveByToken(Drain{PID: 100, ProcStart: "t1"}))
	putGateHold(t, s, "set-a", "/rt", 100, "t1", true)
	claim, err := s.ReadCheckoutClaim("/rt")
	if err != nil {
		t.Fatalf("ReadCheckoutClaim: %v", err)
	}
	if claim == nil || claim.Kind != ClaimFailedGate || claim.SetID != "set-a" {
		t.Fatalf("claim = %+v, want failed-gate claim by set-a", claim)
	}
	if claim.Reason() != "failed gate, uncommitted changes" {
		t.Fatalf("reason = %q, want %q", claim.Reason(), "failed gate, uncommitted changes")
	}
}

func TestReadCheckoutClaimNonClaimingGateHoldUnclaimed(t *testing.T) {
	// A non-claiming hold (HITL / verify-fail / clean Failed gate) contributes
	// quiescence occupancy but never a checkout claim.
	s := openTestStore(t, aliveByToken(Drain{PID: 100, ProcStart: "t1"}))
	putGateHold(t, s, "set-a", "/rt", 100, "t1", false)
	claim, err := s.ReadCheckoutClaim("/rt")
	if err != nil {
		t.Fatalf("ReadCheckoutClaim: %v", err)
	}
	if claim != nil {
		t.Fatalf("claim = %+v, want nil (non-claiming hold does not claim)", claim)
	}
}

func TestReadCheckoutClaimDeadGateHoldUnclaimed(t *testing.T) {
	// A claim-bearing hold whose owner is dead does not claim (swept by reconcile,
	// filtered by the read regardless).
	s := openTestStore(t, aliveByToken()) // nothing alive
	putGateHold(t, s, "set-a", "/rt", 100, "t1", true)
	claim, err := s.ReadCheckoutClaim("/rt")
	if err != nil {
		t.Fatalf("ReadCheckoutClaim: %v", err)
	}
	if claim != nil {
		t.Fatalf("claim = %+v, want nil (dead-owner gate hold does not claim)", claim)
	}
}

func TestStartDrainRefusesOtherSetClaimingGateHold(t *testing.T) {
	s := openTestStore(t, aliveByToken(Drain{PID: 100, ProcStart: "t1"}))
	putGateHold(t, s, "set-a", "/rt", 100, "t1", true)

	_, err := s.StartDrain(Drain{Repo: "repo", SetID: "set-b", RuntimePath: "/rt", PID: 2, ProcStart: "t2", StartedAt: time.Now()})
	if !errors.Is(err, ErrCheckoutClaimed) {
		t.Fatalf("err = %v, want ErrCheckoutClaimed", err)
	}
	var claimed *CheckoutClaimedError
	if !errors.As(err, &claimed) {
		t.Fatalf("err = %v, want *CheckoutClaimedError", err)
	}
	if claimed.Claim.SetID != "set-a" || claimed.Claim.Kind != ClaimFailedGate {
		t.Fatalf("claim = %+v, want failed-gate claim by set-a", claimed.Claim)
	}
}

func TestStartDrainAdmittedAlongsideNonClaimingGateHold(t *testing.T) {
	// A non-claiming gate hold (a human at a HITL / clean Failed gate) must not
	// block another set's admission — queue liveness over gate-time safety.
	s := openTestStore(t, aliveByToken(Drain{PID: 100, ProcStart: "t1"}))
	putGateHold(t, s, "set-a", "/rt", 100, "t1", false)

	d, err := s.StartDrain(Drain{Repo: "repo", SetID: "set-b", RuntimePath: "/rt", PID: 2, ProcStart: "t2", StartedAt: time.Now()})
	if err != nil {
		t.Fatalf("non-claiming gate hold wrongly blocked admission: %v", err)
	}
	if d.ID == 0 || d.State != StateRunning {
		t.Fatalf("drain not started alongside a non-claiming hold: %+v", d)
	}
}

func TestStartDrainSameSetClaimingGateHoldDoesNotBlockReacquire(t *testing.T) {
	// A gate-launched, checkout-mutating action (e.g. reverify) re-acquires the
	// drain while the set's own claim-bearing hold is still registered; its own
	// hold must not self-block.
	s := openTestStore(t, aliveByToken(Drain{PID: 100, ProcStart: "t1"}))
	putGateHold(t, s, "set-a", "/rt", 100, "t1", true)

	d, err := s.StartDrain(Drain{Repo: "repo", SetID: "set-a", RuntimePath: "/rt", PID: 3, ProcStart: "t3", StartedAt: time.Now()})
	if err != nil {
		t.Fatalf("same-set re-acquire refused by own gate hold: %v", err)
	}
	if d.ID == 0 || d.State != StateRunning {
		t.Fatalf("re-acquire drain not started: %+v", d)
	}
}

func TestStartDrainDeadClaimingGateHoldDoesNotBlock(t *testing.T) {
	s := openTestStore(t, aliveByToken()) // nothing alive
	putGateHold(t, s, "set-a", "/rt", 100, "t1", true)

	d, err := s.StartDrain(Drain{Repo: "repo", SetID: "set-b", RuntimePath: "/rt", PID: 2, ProcStart: "t2", StartedAt: time.Now()})
	if err != nil {
		t.Fatalf("dead-owner gate hold wrongly blocked admission: %v", err)
	}
	if d.ID == 0 || d.State != StateRunning {
		t.Fatalf("drain not started over dead-owner gate hold: %+v", d)
	}
}

func TestReadCheckoutClaimNoneWhenIdle(t *testing.T) {
	s := openTestStore(t)
	claim, err := s.ReadCheckoutClaim("/rt")
	if err != nil {
		t.Fatalf("ReadCheckoutClaim: %v", err)
	}
	if claim != nil {
		t.Fatalf("claim = %+v, want nil on an idle checkout", claim)
	}
}

func TestReadCheckoutClaimRunningDrain(t *testing.T) {
	s := openTestStore(t)
	if _, err := s.StartDrain(Drain{Repo: "repo", SetID: "set-a", RuntimePath: "/rt", PID: 1, StartedAt: time.Now()}); err != nil {
		t.Fatalf("StartDrain: %v", err)
	}
	claim, err := s.ReadCheckoutClaim("/rt")
	if err != nil {
		t.Fatalf("ReadCheckoutClaim: %v", err)
	}
	if claim == nil || claim.Kind != ClaimRunningDrain || claim.SetID != "set-a" {
		t.Fatalf("claim = %+v, want running-drain claim by set-a", claim)
	}
}

func TestReadCheckoutClaimQuotaWaiter(t *testing.T) {
	s := openTestStore(t, aliveByToken(Drain{PID: 100, ProcStart: "t1"}))
	putWaiter(t, s, "set-a", "/rt", 100, "t1")
	claim, err := s.ReadCheckoutClaim("/rt")
	if err != nil {
		t.Fatalf("ReadCheckoutClaim: %v", err)
	}
	if claim == nil || claim.Kind != ClaimQuotaWaiter || claim.SetID != "set-a" {
		t.Fatalf("claim = %+v, want quota-waiter claim by set-a", claim)
	}
}

func TestReadCheckoutClaimDeadWaiterUnclaimed(t *testing.T) {
	// Owner reads dead → the waiter does not claim the checkout.
	s := openTestStore(t, aliveByToken())
	putWaiter(t, s, "set-a", "/rt", 100, "t1")
	claim, err := s.ReadCheckoutClaim("/rt")
	if err != nil {
		t.Fatalf("ReadCheckoutClaim: %v", err)
	}
	if claim != nil {
		t.Fatalf("claim = %+v, want nil (dead-owner waiter does not claim)", claim)
	}
}

func TestStartDrainRefusesOtherSetWaiter(t *testing.T) {
	s := openTestStore(t, aliveByToken(Drain{PID: 100, ProcStart: "t1"}))
	putWaiter(t, s, "set-a", "/rt", 100, "t1")

	// Set B tries to drain the checkout set A's live waiter is parked on.
	_, err := s.StartDrain(Drain{Repo: "repo", SetID: "set-b", RuntimePath: "/rt", PID: 2, ProcStart: "t2", StartedAt: time.Now()})
	if !errors.Is(err, ErrCheckoutClaimed) {
		t.Fatalf("err = %v, want ErrCheckoutClaimed", err)
	}
	if errors.Is(err, ErrDrainInProgress) {
		t.Fatalf("claim refusal must be distinguishable from ErrDrainInProgress: %v", err)
	}
	var claimed *CheckoutClaimedError
	if !errors.As(err, &claimed) {
		t.Fatalf("err = %v, want *CheckoutClaimedError", err)
	}
	if claimed.Claim.SetID != "set-a" || claimed.Claim.Kind != ClaimQuotaWaiter {
		t.Fatalf("claim = %+v, want quota-waiter claim by set-a", claimed.Claim)
	}
}

func TestStartDrainSameSetWaiterDoesNotBlockResume(t *testing.T) {
	// A quota-parked set resumes by re-starting its drain past its own still-
	// registered waiter (deregistration happens after the resume start today).
	s := openTestStore(t, aliveByToken(Drain{PID: 100, ProcStart: "t1"}))
	putWaiter(t, s, "set-a", "/rt", 100, "t1")

	d, err := s.StartDrain(Drain{Repo: "repo", SetID: "set-a", RuntimePath: "/rt", PID: 3, ProcStart: "t3", StartedAt: time.Now()})
	if err != nil {
		t.Fatalf("same-set resume StartDrain refused: %v", err)
	}
	if d.ID == 0 || d.State != StateRunning {
		t.Fatalf("resume drain not started: %+v", d)
	}
}

func TestStartDrainDeadWaiterDoesNotBlock(t *testing.T) {
	// The waiter's owner reads dead (slice 01 would sweep it); it must not block
	// admission even before the sweep runs.
	s := openTestStore(t, aliveByToken()) // nothing alive
	putWaiter(t, s, "set-a", "/rt", 100, "t1")

	d, err := s.StartDrain(Drain{Repo: "repo", SetID: "set-b", RuntimePath: "/rt", PID: 2, ProcStart: "t2", StartedAt: time.Now()})
	if err != nil {
		t.Fatalf("dead-owner waiter wrongly blocked admission: %v", err)
	}
	if d.ID == 0 || d.State != StateRunning {
		t.Fatalf("drain not started over dead-owner waiter: %+v", d)
	}
}

func TestStartDrainAdmittedAfterWaiterDeregisters(t *testing.T) {
	s := openTestStore(t, aliveByToken(Drain{PID: 100, ProcStart: "t1"}))
	putWaiter(t, s, "set-a", "/rt", 100, "t1")

	if _, err := s.StartDrain(Drain{Repo: "repo", SetID: "set-b", RuntimePath: "/rt", PID: 2, ProcStart: "t2", StartedAt: time.Now()}); !errors.Is(err, ErrCheckoutClaimed) {
		t.Fatalf("err = %v, want ErrCheckoutClaimed while set-a's waiter is live", err)
	}

	if err := s.DeleteRecoveryWaiter("set-a"); err != nil {
		t.Fatalf("DeleteRecoveryWaiter: %v", err)
	}
	d, err := s.StartDrain(Drain{Repo: "repo", SetID: "set-b", RuntimePath: "/rt", PID: 2, ProcStart: "t2", StartedAt: time.Now()})
	if err != nil {
		t.Fatalf("StartDrain after deregister: %v", err)
	}
	if d.ID == 0 || d.State != StateRunning {
		t.Fatalf("set-b not admitted after set-a deregistered: %+v", d)
	}
}
