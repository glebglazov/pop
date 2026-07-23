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
