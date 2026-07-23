package tasks

import (
	"bytes"
	"strings"
	"testing"
	"time"
)

// TestBeginDrainSameSetWaiterDoesNotBlockResume is the resume-ordering
// regression (ADR-0135): ParkAndWaitForQuotaRecovery re-starts a parked set's
// drain (ensureDrain → BeginDrain) *before* deregistering that set's recovery
// waiter. The set's own live waiter must therefore never self-block its resume.
func TestBeginDrainSameSetWaiterDoesNotBlockResume(t *testing.T) {
	d, repo := drainTestRepo(t)

	if _, err := RegisterRecoveryWaiter(d, RecoveryWaiter{
		SetID:       "demo",
		Preset:      "claude",
		ResetAt:     time.Now().Add(-time.Hour),
		RuntimePath: repo,
	}); err != nil {
		t.Fatalf("RegisterRecoveryWaiter: %v", err)
	}

	// The waiter is still registered (deregistration follows the resume start).
	h, err := BeginDrain(d, repo, "demo", &bytes.Buffer{})
	if err != nil {
		t.Fatalf("same-set resume BeginDrain refused by its own waiter: %v", err)
	}
	_ = h.Cancel()
}

// TestBeginDrainTwoSetsOneCheckout is the observed 2026-07-22 bug: set B must
// not be admitted into a checkout while set A's quota-recovery waiter is live on
// it, and must be admitted once A deregisters (ADR-0135).
func TestBeginDrainTwoSetsOneCheckout(t *testing.T) {
	d, repo := drainTestRepo(t)

	if _, err := RegisterRecoveryWaiter(d, RecoveryWaiter{
		SetID:       "set-a",
		Preset:      "claude",
		ResetAt:     time.Now().Add(-time.Hour),
		RuntimePath: repo,
	}); err != nil {
		t.Fatalf("RegisterRecoveryWaiter set-a: %v", err)
	}

	// Set B's admission is refused while set A's waiter claims the checkout.
	_, err := BeginDrain(d, repo, "set-b", &bytes.Buffer{})
	assertExitCode(t, err, ExitOperational)
	if err == nil || !strings.Contains(err.Error(), "claimed by set set-a") {
		t.Fatalf("err = %v, want a claim refusal naming set-a", err)
	}

	// After set A deregisters, set B is admitted.
	if err := DeregisterRecoveryWaiter(d, "set-a"); err != nil {
		t.Fatalf("DeregisterRecoveryWaiter set-a: %v", err)
	}
	h, err := BeginDrain(d, repo, "set-b", &bytes.Buffer{})
	if err != nil {
		t.Fatalf("set-b not admitted after set-a deregistered: %v", err)
	}
	_ = h.Cancel()
}
