package store

import (
	"testing"
	"time"
)

// elapsedWaiter builds a recovery waiter on /rt whose cooldown has already
// elapsed (reset one hour in the past), so acquisition attempts reach the
// blocker checks rather than the pre-cooldown early return.
func elapsedWaiter(setID string, priority int, registered time.Time) RecoveryWaiter {
	return RecoveryWaiter{
		SetID:        setID,
		Preset:       "sonnet",
		ResetAt:      time.Now().Add(-time.Hour).UTC(),
		RuntimePath:  "/rt",
		Priority:     priority,
		RegisteredAt: registered.UTC(),
	}
}

func TestTryAcquireRecoveryTurnAcquiresWhenClear(t *testing.T) {
	s := openTestStore(t)
	w := elapsedWaiter("set-a", 0, time.Now())
	if err := s.PutRecoveryWaiter(w); err != nil {
		t.Fatalf("PutRecoveryWaiter: %v", err)
	}

	acquired, block, err := s.TryAcquireRecoveryTurn(w, time.Now().UTC())
	if err != nil {
		t.Fatalf("TryAcquireRecoveryTurn: %v", err)
	}
	if !acquired {
		t.Fatal("expected turn acquired on a clear path")
	}
	if block != nil {
		t.Fatalf("expected nil block when acquired, got %+v", block)
	}
}

func TestTryAcquireRecoveryTurnNoBlockBeforeCooldown(t *testing.T) {
	s := openTestStore(t)
	w := elapsedWaiter("set-a", 0, time.Now())
	w.ResetAt = time.Now().Add(time.Hour).UTC() // cooldown not yet elapsed
	if err := s.PutRecoveryWaiter(w); err != nil {
		t.Fatalf("PutRecoveryWaiter: %v", err)
	}

	acquired, block, err := s.TryAcquireRecoveryTurn(w, time.Now().UTC())
	if err != nil {
		t.Fatalf("TryAcquireRecoveryTurn: %v", err)
	}
	if acquired {
		t.Fatal("expected no acquisition before cooldown elapses")
	}
	if block != nil {
		t.Fatalf("expected nil block before cooldown, got %+v", block)
	}
}

// TestTryAcquireRecoveryTurnAcquiresPastNonClaimingGateHold pins ADR-0135: a
// non-claiming gate hold (claim=0 — HITL, approval, verify-fail, or a clean
// Failed gate) is quiescence occupancy only and does not defer a quota-recovered
// waiter. The waiter resumes past an open human-wait menu on its checkout.
func TestTryAcquireRecoveryTurnAcquiresPastNonClaimingGateHold(t *testing.T) {
	for _, gate := range []string{"hitl", "approval", "verify-fail", "clean-failed-gate"} {
		t.Run(gate, func(t *testing.T) {
			s := openTestStore(t) // default liveness: every owner is alive
			if err := s.PutCheckoutGateHold(CheckoutGateHold{
				RuntimePath: "/rt", SetID: "set-gate", PID: 10, ProcStart: "t1",
				Claim: false, RegisteredAt: time.Now(),
			}); err != nil {
				t.Fatalf("PutCheckoutGateHold: %v", err)
			}
			w := elapsedWaiter("set-a", 0, time.Now())
			if err := s.PutRecoveryWaiter(w); err != nil {
				t.Fatalf("PutRecoveryWaiter: %v", err)
			}

			acquired, block, err := s.TryAcquireRecoveryTurn(w, time.Now().UTC())
			if err != nil {
				t.Fatalf("TryAcquireRecoveryTurn: %v", err)
			}
			if !acquired {
				t.Fatalf("expected acquisition past a non-claiming gate hold, got block %+v", block)
			}
			if block != nil {
				t.Fatalf("expected nil block when acquired, got %+v", block)
			}
		})
	}
}

// TestTryAcquireRecoveryTurnBlockedByClaimBearingGateHold pins that a live
// claim-bearing Failed-gate hold (claim=1, a dirty tree under review) of another
// set defers the turn and surfaces as a claim block naming that set and kind.
func TestTryAcquireRecoveryTurnBlockedByClaimBearingGateHold(t *testing.T) {
	s := openTestStore(t) // default liveness: every owner is alive
	if err := s.PutCheckoutGateHold(CheckoutGateHold{
		RuntimePath: "/rt", SetID: "set-gate", PID: 10, ProcStart: "t1",
		Claim: true, RegisteredAt: time.Now(),
	}); err != nil {
		t.Fatalf("PutCheckoutGateHold: %v", err)
	}
	w := elapsedWaiter("set-a", 0, time.Now())
	if err := s.PutRecoveryWaiter(w); err != nil {
		t.Fatalf("PutRecoveryWaiter: %v", err)
	}

	acquired, block, err := s.TryAcquireRecoveryTurn(w, time.Now().UTC())
	if err != nil {
		t.Fatalf("TryAcquireRecoveryTurn: %v", err)
	}
	if acquired {
		t.Fatal("expected denial while a claim-bearing gate hold is parked")
	}
	if block == nil || block.Kind != RecoveryBlockClaimed || block.SetID != "set-gate" {
		t.Fatalf("want claimed block for set-gate, got %+v", block)
	}
	if block.Claim == nil || block.Claim.Kind != ClaimFailedGate {
		t.Fatalf("want failed_gate claim, got %+v", block.Claim)
	}
}

// TestTryAcquireRecoveryTurnOwnGateHoldDoesNotSelfBlock pins that the acquiring
// waiter's own claim-bearing gate hold is excluded from the claim check, so a set
// never blocks itself.
func TestTryAcquireRecoveryTurnOwnGateHoldDoesNotSelfBlock(t *testing.T) {
	s := openTestStore(t) // default liveness: every owner is alive
	w := elapsedWaiter("set-a", 0, time.Now())
	if err := s.PutCheckoutGateHold(CheckoutGateHold{
		RuntimePath: "/rt", SetID: "set-a", PID: 10, ProcStart: "t1",
		Claim: true, RegisteredAt: time.Now(),
	}); err != nil {
		t.Fatalf("PutCheckoutGateHold: %v", err)
	}
	if err := s.PutRecoveryWaiter(w); err != nil {
		t.Fatalf("PutRecoveryWaiter: %v", err)
	}

	acquired, block, err := s.TryAcquireRecoveryTurn(w, time.Now().UTC())
	if err != nil {
		t.Fatalf("TryAcquireRecoveryTurn: %v", err)
	}
	if !acquired {
		t.Fatalf("expected acquisition past own gate hold, got block %+v", block)
	}
}

func TestTryAcquireRecoveryTurnBlockedByLiveDrain(t *testing.T) {
	s := openTestStore(t) // default liveness: every owner is alive
	if _, err := s.StartDrain(Drain{
		Repo: "r", SetID: "set-drain", RuntimePath: "/rt", PID: 20, StartedAt: time.Now(),
	}); err != nil {
		t.Fatalf("StartDrain: %v", err)
	}
	w := elapsedWaiter("set-a", 0, time.Now())
	if err := s.PutRecoveryWaiter(w); err != nil {
		t.Fatalf("PutRecoveryWaiter: %v", err)
	}

	acquired, block, err := s.TryAcquireRecoveryTurn(w, time.Now().UTC())
	if err != nil {
		t.Fatalf("TryAcquireRecoveryTurn: %v", err)
	}
	if acquired {
		t.Fatal("expected denial while a live drain runs on the path")
	}
	if block == nil || block.Kind != RecoveryBlockClaimed || block.SetID != "set-drain" {
		t.Fatalf("want claimed block for set-drain, got %+v", block)
	}
	if block.Claim == nil || block.Claim.Kind != ClaimRunningDrain {
		t.Fatalf("want running_drain claim, got %+v", block.Claim)
	}
}

func TestTryAcquireRecoveryTurnBlockedByHeldTurn(t *testing.T) {
	s := openTestStore(t)
	// First waiter acquires the turn, leaving a recovery_turns row on the path.
	first := elapsedWaiter("set-first", 0, time.Now().Add(-2*time.Minute))
	if err := s.PutRecoveryWaiter(first); err != nil {
		t.Fatalf("PutRecoveryWaiter first: %v", err)
	}
	acquired, _, err := s.TryAcquireRecoveryTurn(first, time.Now().UTC())
	if err != nil {
		t.Fatalf("TryAcquireRecoveryTurn first: %v", err)
	}
	if !acquired {
		t.Fatal("first waiter should acquire the turn")
	}

	// Second waiter now finds the turn already held.
	second := elapsedWaiter("set-second", 0, time.Now().Add(-1*time.Minute))
	if err := s.PutRecoveryWaiter(second); err != nil {
		t.Fatalf("PutRecoveryWaiter second: %v", err)
	}
	acquired, block, err := s.TryAcquireRecoveryTurn(second, time.Now().UTC())
	if err != nil {
		t.Fatalf("TryAcquireRecoveryTurn second: %v", err)
	}
	if acquired {
		t.Fatal("expected denial while the turn is held by another set")
	}
	if block == nil || block.Kind != RecoveryBlockTurnHeld || block.SetID != "set-first" {
		t.Fatalf("want turn_held block for set-first, got %+v", block)
	}
}

func TestTryAcquireRecoveryTurnBlockedBehindWaiter(t *testing.T) {
	s := openTestStore(t)
	// Higher-priority waiter is first in the ordering.
	ahead := elapsedWaiter("set-ahead", 10, time.Now().Add(-2*time.Minute))
	behind := elapsedWaiter("set-behind", 0, time.Now().Add(-1*time.Minute))
	if err := s.PutRecoveryWaiter(ahead); err != nil {
		t.Fatalf("PutRecoveryWaiter ahead: %v", err)
	}
	if err := s.PutRecoveryWaiter(behind); err != nil {
		t.Fatalf("PutRecoveryWaiter behind: %v", err)
	}

	acquired, block, err := s.TryAcquireRecoveryTurn(behind, time.Now().UTC())
	if err != nil {
		t.Fatalf("TryAcquireRecoveryTurn behind: %v", err)
	}
	if acquired {
		t.Fatal("expected denial while a higher-priority waiter is first")
	}
	if block == nil || block.Kind != RecoveryBlockBehindWaiter || block.SetID != "set-ahead" {
		t.Fatalf("want behind_waiter block for set-ahead, got %+v", block)
	}
}
