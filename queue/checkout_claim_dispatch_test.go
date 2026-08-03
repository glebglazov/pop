package queue

import (
	"strings"
	"testing"
	"time"

	"github.com/glebglazov/pop/store"
	"github.com/glebglazov/pop/tasks"
	"github.com/glebglazov/pop/work/ref"
)

// claimForMap builds a checkoutClaimFunc from a candidate-set → claim map, the
// shape selectReadySets consults per Ready candidate (ADR-0135 slice 05).
func claimForMap(claims map[string]*store.CheckoutClaim) checkoutClaimFunc {
	return func(setID string) *store.CheckoutClaim { return claims[setID] }
}

// TestSelectReadySetDefersBehindOtherSetQuotaWaiter: a Ready set whose bound
// checkout carries another set's live quota-waiter claim defers, and the
// deferral surfaces that owner's reset instant (feeding the earliest-eligible
// display) rather than burning a spawn BeginDrain would only refuse.
func TestSelectReadySetDefersBehindOtherSetQuotaWaiter(t *testing.T) {
	resetAt := time.Date(2026, 7, 23, 14, 0, 0, 0, time.UTC)
	refresh := &tasks.RefreshResult{Rows: []tasks.Row{
		{ID: "set-b", Status: tasks.StatusReady, AutoDrain: true, Priority: 100, RegIndex: 0},
	}}
	recoveryWaiters := map[string]tasks.RecoveryWaiter{
		"set-a": {SetID: "set-a", Preset: "codex", ResetAt: resetAt, RuntimePath: "/rt"},
	}
	claims := claimForMap(map[string]*store.CheckoutClaim{
		"set-b": {Holder: ref.WorkRef{Kind: ref.KindTaskSet, ContainerID: "set-a"}, Reason: store.ClaimQuotaWaiter},
	})

	ids, deferral, ok := selectReadySets(refresh, nil, recoveryWaiters, claims)
	if ok || len(ids) != 0 {
		t.Fatalf("set-b should defer behind set-a's quota waiter, got ids=%v ok=%v", ids, ok)
	}
	if deferral.Reason != DeferQuotaRecovery || deferral.SetID != "set-b" || !deferral.Until.Equal(resetAt) {
		t.Fatalf("deferral = %+v, want DeferQuotaRecovery set-b until %s", deferral, resetAt)
	}
}

// TestSelectReadySetDefersBehindOtherSetFailedGate: a Ready set whose bound
// checkout carries another set's dirty Failed-gate claim defers with a reason
// naming the holding set.
func TestSelectReadySetDefersBehindOtherSetFailedGate(t *testing.T) {
	refresh := &tasks.RefreshResult{Rows: []tasks.Row{
		{ID: "set-b", Status: tasks.StatusReady, AutoDrain: true, Priority: 100, RegIndex: 0},
	}}
	claims := claimForMap(map[string]*store.CheckoutClaim{
		"set-b": {Holder: ref.WorkRef{Kind: ref.KindTaskSet, ContainerID: "set-a"}, Reason: store.ClaimFailedGate},
	})

	ids, deferral, ok := selectReadySets(refresh, nil, nil, claims)
	if ok || len(ids) != 0 {
		t.Fatalf("set-b should defer behind set-a's failed-gate claim, got ids=%v ok=%v", ids, ok)
	}
	if deferral.Reason != DeferCheckoutClaim || deferral.SetID != "set-b" {
		t.Fatalf("deferral = %+v, want DeferCheckoutClaim set-b", deferral)
	}
	msg := deferral.Message()
	if !strings.Contains(msg, "set-a") || !strings.Contains(msg, "failed gate") {
		t.Fatalf("deferral message %q should name holding set-a and the failed gate", msg)
	}
}

// TestSelectReadySetDispatchesPastNonClaimingGateHold: a non-claiming gate hold
// (HITL, verify-fail, clean Failed gate) yields no Checkout claim, so a Ready
// set on that checkout dispatches normally (ADR-0135).
func TestSelectReadySetDispatchesPastNonClaimingGateHold(t *testing.T) {
	refresh := &tasks.RefreshResult{Rows: []tasks.Row{
		{ID: "set-b", Status: tasks.StatusReady, AutoDrain: true, Priority: 100, RegIndex: 0},
	}}
	// claimFor returns nil for set-b: a non-claiming hold contributes no claim.
	claims := claimForMap(map[string]*store.CheckoutClaim{})

	ids, _, ok := selectReadySets(refresh, nil, nil, claims)
	if !ok || len(ids) != 1 || ids[0] != "set-b" {
		t.Fatalf("set-b should dispatch past a non-claiming hold, got ids=%v ok=%v", ids, ok)
	}
}

// TestSelectReadySetOwnClaimStillDefers: a set's own quota waiter keeps
// deferring it (set-scoped path), and a same-set claim on its own checkout does
// not spuriously dispatch it — no duplicate spawn (ADR-0135).
func TestSelectReadySetOwnClaimStillDefers(t *testing.T) {
	resetAt := time.Date(2026, 7, 23, 15, 0, 0, 0, time.UTC)
	refresh := &tasks.RefreshResult{Rows: []tasks.Row{
		{ID: "set-a", Status: tasks.StatusReady, AutoDrain: true, Priority: 100, RegIndex: 0},
	}}
	recoveryWaiters := map[string]tasks.RecoveryWaiter{
		"set-a": {SetID: "set-a", Preset: "codex", ResetAt: resetAt, RuntimePath: "/rt"},
	}
	// The claim on set-a's checkout is set-a's own waiter; the checkout-scoped arm
	// must not treat an own claim as another set's.
	claims := claimForMap(map[string]*store.CheckoutClaim{
		"set-a": {Holder: ref.WorkRef{Kind: ref.KindTaskSet, ContainerID: "set-a"}, Reason: store.ClaimQuotaWaiter},
	})

	ids, deferral, ok := selectReadySets(refresh, nil, recoveryWaiters, claims)
	if ok || len(ids) != 0 {
		t.Fatalf("set-a should keep deferring on its own waiter, got ids=%v ok=%v", ids, ok)
	}
	if deferral.Reason != DeferQuotaRecovery || deferral.SetID != "set-a" || !deferral.Until.Equal(resetAt) {
		t.Fatalf("deferral = %+v, want DeferQuotaRecovery set-a until %s", deferral, resetAt)
	}
}

// TestUnplacedSetNotClaimDeferredBehindTrunkDrain drives the ADR-0147 deadlock:
// a live drain claims the trunk while an unbound (unplaced) Ready set would
// historically resolve its claim target to that same trunk and defer forever.
// Binding-to-runtime-path now returns no checkout for the unplaced set, so the
// claim gate does not defer it on account of the trunk's claim.
func TestUnplacedSetNotClaimDeferredBehindTrunkDrain(t *testing.T) {
	const trunk = "/repo/main"
	td := queueTestTasksDeps(t, true)
	td.ProcessAlive = func(pid int) bool { return pid == 4242 }
	td.ProcessStartToken = func(pid int) (string, bool) {
		if pid == 4242 {
			return "live-tok", true
		}
		return "", false
	}
	d := &Deps{Tasks: td}

	s, ok, err := td.Store(true)
	if err != nil || !ok {
		t.Fatalf("Store: ok=%v err=%v", ok, err)
	}
	if _, err := s.StartDrain(store.Drain{
		Repo:        "/repo/.git",
		SetID:       "trunk-drain",
		RuntimePath: trunk,
		PID:         4242,
		ProcStart:   "live-tok",
		StartedAt:   time.Now().UTC(),
	}); err != nil {
		t.Fatalf("StartDrain on trunk: %v", err)
	}

	// No binding for the unplaced set — RuntimeForSet must not fall back to trunk.
	claimFor := d.checkoutClaimLookup(nil, "repo-key")
	if claim := claimFor("unplaced"); claim != nil {
		t.Fatalf("unplaced claim = %+v, want nil (no checkout to claim-gate)", claim)
	}
	// Bound set on the trunk still sees the live drain claim.
	bound := map[string]WorktreeBinding{
		setScopedKey("repo-key", "bound-on-trunk"): {RuntimePath: trunk},
	}
	boundClaim := d.checkoutClaimLookup(bound, "repo-key")
	if claim := boundClaim("bound-on-trunk"); claim == nil || claim.Reason != store.ClaimRunningDrain || claim.Holder.ContainerID != "trunk-drain" {
		t.Fatalf("bound-on-trunk claim = %+v, want trunk-drain running claim", claim)
	}

	refresh := &tasks.RefreshResult{Rows: []tasks.Row{
		{ID: "unplaced", Status: tasks.StatusReady, AutoDrain: true, Priority: 100, RegIndex: 0},
	}}
	ids, deferral, okSel := selectReadySets(refresh, nil, nil, claimFor)
	if !okSel || len(ids) != 1 || ids[0] != "unplaced" {
		t.Fatalf("unplaced should dispatch past trunk claim, got ids=%v ok=%v deferral=%+v", ids, okSel, deferral)
	}
	if deferral.Deferred() {
		t.Fatalf("unplaced must not be claim-deferred, got %+v", deferral)
	}
}

// TestCheckoutClaimDeferralRendersReason: the claim deferral surfaces through
// the run/status view's blocked bucket with the holding set named and the
// checkout_claim kind (criterion 5).
func TestCheckoutClaimDeferralRendersReason(t *testing.T) {
	idle := IdleProject{
		Project:   "pop",
		RepoLabel: "pop",
		Deferral: SpawnDeferral{
			Reason: DeferCheckoutClaim,
			SetID:  "set-b",
			Claim:  &store.CheckoutClaim{Holder: ref.WorkRef{Kind: ref.KindTaskSet, ContainerID: "set-a"}, Reason: store.ClaimFailedGate},
		},
	}
	item := blockedItemFromIdle(idle)
	if item.Kind != "checkout_claim" || item.SetID != "set-b" {
		t.Fatalf("blocked item = %+v, want kind checkout_claim set-b", item)
	}
	if !strings.Contains(item.Reason, "set-a") || !strings.Contains(item.Reason, "failed gate") {
		t.Fatalf("blocked item reason %q should name holding set-a and the failed gate", item.Reason)
	}
}
