package tasks

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"github.com/glebglazov/pop/store"
)

// TestReverifyAtGateAcquiresAndReleasesClaim: a gate-launched re-verify holds
// the Runtime execution lock (a running-Drain claim) for the duration of the
// Verifier run and releases it on return to the menu (ADR-0135). The Verifier
// touches the checkout, so — unlike an assist session or the runtime shell — it
// must re-acquire the claim the gate park released.
func TestReverifyAtGateAcquiresAndReleasesClaim(t *testing.T) {
	d, repo := drainTestRepo(t)

	var claimDuringRun *store.CheckoutClaim
	rv := &reverifyGateContext{
		cfg: verifyEnabledConfig(),
		runVerifier: func(string) (string, error) {
			claimDuringRun, _ = ReadCheckoutClaim(d, repo)
			return "VERDICT: PASS\n", nil
		},
	}
	m := &Manifest{Stem: "demo", Dir: repo, Valid: true}

	if err := reverifyAtGate(d, rv, &bytes.Buffer{}, repo, repo, "demo", m); err != nil {
		t.Fatalf("reverifyAtGate: %v", err)
	}

	if claimDuringRun == nil {
		t.Fatalf("re-verify ran without holding a Checkout claim")
	}
	if claimDuringRun.Kind != store.ClaimRunningDrain || claimDuringRun.SetID != "demo" {
		t.Fatalf("claim during run = %#v, want running-drain claim for demo", claimDuringRun)
	}

	// The claim is released on return so the gate menu (and the next set) is not
	// left blocked.
	after, err := ReadCheckoutClaim(d, repo)
	if err != nil {
		t.Fatalf("ReadCheckoutClaim after re-verify: %v", err)
	}
	if after != nil {
		t.Fatalf("claim not released after re-verify: %#v", after)
	}
}

// TestReverifyAtGateRefusedByOtherSetClaim: when another set's claim is live on
// the checkout while a human sits at the gate, the re-verify's BeginDrain
// refuses with the claim reason and never touches the checkout (the Verifier is
// not invoked). The error surfaces to the menu, which prints it and stays usable
// so the human can retry after the claimant finishes (ADR-0135).
func TestReverifyAtGateRefusedByOtherSetClaim(t *testing.T) {
	d, repo := drainTestRepo(t)

	// Another set holds a live quota-recovery waiter on the same checkout.
	if _, err := RegisterRecoveryWaiter(d, RecoveryWaiter{
		SetID:       "set-other",
		Preset:      "claude",
		ResetAt:     time.Now().Add(-time.Hour),
		RuntimePath: repo,
	}); err != nil {
		t.Fatalf("RegisterRecoveryWaiter: %v", err)
	}

	verifierRan := false
	rv := &reverifyGateContext{
		cfg: verifyEnabledConfig(),
		runVerifier: func(string) (string, error) {
			verifierRan = true
			return "VERDICT: PASS\n", nil
		},
	}
	m := &Manifest{Stem: "demo", Dir: repo, Valid: true}

	err := reverifyAtGate(d, rv, &bytes.Buffer{}, repo, repo, "demo", m)
	assertExitCode(t, err, ExitOperational)
	if err == nil || !strings.Contains(err.Error(), "claimed by set set-other") {
		t.Fatalf("err = %v, want a claim refusal naming set-other", err)
	}
	if verifierRan {
		t.Fatalf("re-verify touched the checkout despite another set's claim")
	}

	// The refused re-verify left no drain row of its own, so the checkout's only
	// live claim is still the other set's waiter — the menu remains usable.
	claim, err := ReadCheckoutClaim(d, repo)
	if err != nil {
		t.Fatalf("ReadCheckoutClaim: %v", err)
	}
	if claim == nil || claim.SetID != "set-other" || claim.Kind != store.ClaimQuotaWaiter {
		t.Fatalf("claim = %#v, want the surviving set-other quota waiter", claim)
	}
}
