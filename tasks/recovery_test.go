package tasks

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/glebglazov/pop/store"
)

func TestRegisterRecoveryWaiter(t *testing.T) {
	d, _ := drainTestRepo(t)
	
	waiter := RecoveryWaiter{
		SetID:       "demo",
		Preset:      "claude",
		ResetAt:     time.Now().Add(time.Hour),
		RuntimePath: "/test/path",
	}
	
	registered, err := RegisterRecoveryWaiter(d, waiter)
	if err != nil {
		t.Fatalf("RegisterRecoveryWaiter failed: %v", err)
	}
	
	if registered.SetID != waiter.SetID {
		t.Errorf("SetID = %q, want %q", registered.SetID, waiter.SetID)
	}
	if registered.RegisteredAt.IsZero() {
		t.Error("RegisteredAt should be set")
	}
	
	// Verify it was persisted
	stored, err := GetRecoveryWaiter(d, "demo")
	if err != nil {
		t.Fatalf("GetRecoveryWaiter failed: %v", err)
	}
	if stored == nil {
		t.Fatal("waiter not found after registration")
	}
	if stored.Preset != "claude" {
		t.Errorf("stored preset = %q, want claude", stored.Preset)
	}
}

func TestDeregisterRecoveryWaiter(t *testing.T) {
	d, _ := drainTestRepo(t)
	
	waiter := RecoveryWaiter{
		SetID:       "demo",
		Preset:      "claude",
		ResetAt:     time.Now().Add(time.Hour),
		RuntimePath: "/test/path",
	}
	
	_, err := RegisterRecoveryWaiter(d, waiter)
	if err != nil {
		t.Fatalf("RegisterRecoveryWaiter failed: %v", err)
	}
	
	err = DeregisterRecoveryWaiter(d, "demo")
	if err != nil {
		t.Fatalf("DeregisterRecoveryWaiter failed: %v", err)
	}
	
	stored, err := GetRecoveryWaiter(d, "demo")
	if err != nil {
		t.Fatalf("GetRecoveryWaiter failed: %v", err)
	}
	if stored != nil {
		t.Error("waiter should be nil after deregistration")
	}
}

func TestGetRecoveryWaiter_NotFound(t *testing.T) {
	d, _ := drainTestRepo(t)
	
	stored, err := GetRecoveryWaiter(d, "nonexistent")
	if err != nil {
		t.Fatalf("GetRecoveryWaiter failed: %v", err)
	}
	if stored != nil {
		t.Error("expected nil for nonexistent waiter")
	}
}

func TestAcquireRecoveryTurn_PresetAgnosticDifferentCheckouts(t *testing.T) {
	d, _ := drainTestRepo(t)

	// Register two waiters with different presets on different checkouts.
	waiter1 := RecoveryWaiter{
		SetID:       "demo1",
		Preset:      "claude",
		ResetAt:     time.Now().Add(-time.Hour),
		RuntimePath: "/test/path1",
	}
	waiter2 := RecoveryWaiter{
		SetID:       "demo2",
		Preset:      "codex",
		ResetAt:     time.Now().Add(-time.Hour),
		RuntimePath: "/test/path2",
	}

	_, err := RegisterRecoveryWaiter(d, waiter1)
	if err != nil {
		t.Fatalf("RegisterRecoveryWaiter failed: %v", err)
	}
	_, err = RegisterRecoveryWaiter(d, waiter2)
	if err != nil {
		t.Fatalf("RegisterRecoveryWaiter failed: %v", err)
	}

	acquired1, _, err := acquireRecoveryTurn(d, &waiter1)
	if err != nil {
		t.Fatalf("acquireRecoveryTurn failed: %v", err)
	}
	if !acquired1 {
		t.Error("waiter1 should acquire turn on its checkout")
	}

	acquired2, _, err := acquireRecoveryTurn(d, &waiter2)
	if err != nil {
		t.Fatalf("acquireRecoveryTurn failed: %v", err)
	}
	if !acquired2 {
		t.Error("waiter2 should acquire turn on a different checkout")
	}
}

func TestAcquireRecoveryTurn_PresetAgnosticOnePerCheckout(t *testing.T) {
	d, repo := drainTestRepo(t)
	resetAt := time.Now().Add(-time.Hour)

	waiter1 := RecoveryWaiter{
		SetID:       "claude-set",
		Preset:      "claude",
		ResetAt:     resetAt,
		RuntimePath: repo,
		Priority:    5,
	}
	waiter2 := RecoveryWaiter{
		SetID:       "codex-set",
		Preset:      "codex",
		ResetAt:     resetAt,
		RuntimePath: repo,
		Priority:    5,
	}

	_, err := RegisterRecoveryWaiter(d, waiter1)
	if err != nil {
		t.Fatalf("RegisterRecoveryWaiter waiter1: %v", err)
	}
	_, err = RegisterRecoveryWaiter(d, waiter2)
	if err != nil {
		t.Fatalf("RegisterRecoveryWaiter waiter2: %v", err)
	}

	acquired1, _, err := acquireRecoveryTurn(d, &waiter1)
	if err != nil {
		t.Fatalf("acquireRecoveryTurn waiter1: %v", err)
	}
	acquired2, _, err := acquireRecoveryTurn(d, &waiter2)
	if err != nil {
		t.Fatalf("acquireRecoveryTurn waiter2: %v", err)
	}
	if !acquired1 {
		t.Fatal("first waiter on checkout should acquire turn")
	}
	if acquired2 {
		t.Fatal("second waiter on same checkout must not acquire while turn is held")
	}

	if err := ReleaseRecoveryTurn(d, repo); err != nil {
		t.Fatalf("ReleaseRecoveryTurn: %v", err)
	}
	if err := DeregisterRecoveryWaiter(d, waiter1.SetID); err != nil {
		t.Fatalf("DeregisterRecoveryWaiter waiter1: %v", err)
	}

	acquired2, _, err = acquireRecoveryTurn(d, &waiter2)
	if err != nil {
		t.Fatalf("acquireRecoveryTurn waiter2 after release: %v", err)
	}
	if !acquired2 {
		t.Fatal("second waiter should acquire after turn is released")
	}
}

func TestAcquireRecoveryTurn_PrioritySameCheckout(t *testing.T) {
	d, repo := drainTestRepo(t)
	resetAt := time.Now().Add(-time.Hour)
	now := time.Now().UTC()

	low := RecoveryWaiter{
		SetID:        "low",
		Preset:       "claude",
		ResetAt:      resetAt,
		RuntimePath:  repo,
		Priority:     0,
		RegisteredAt: now.Add(-2 * time.Hour),
	}
	high := RecoveryWaiter{
		SetID:        "high",
		Preset:       "claude",
		ResetAt:      resetAt,
		RuntimePath:  repo,
		Priority:     10,
		RegisteredAt: now.Add(-1 * time.Hour),
	}

	if _, err := RegisterRecoveryWaiter(d, low); err != nil {
		t.Fatalf("RegisterRecoveryWaiter low: %v", err)
	}
	if _, err := RegisterRecoveryWaiter(d, high); err != nil {
		t.Fatalf("RegisterRecoveryWaiter high: %v", err)
	}

	acquiredLow, _, err := acquireRecoveryTurn(d, &low)
	if err != nil {
		t.Fatalf("acquireRecoveryTurn low: %v", err)
	}
	acquiredHigh, _, err := acquireRecoveryTurn(d, &high)
	if err != nil {
		t.Fatalf("acquireRecoveryTurn high: %v", err)
	}
	if acquiredLow {
		t.Fatal("lower-priority waiter must not acquire before higher-priority waiter")
	}
	if !acquiredHigh {
		t.Fatal("higher-priority waiter should acquire first")
	}
}

func TestAcquireRecoveryTurn_FIFOEqualPriority(t *testing.T) {
	d, repo := drainTestRepo(t)
	resetAt := time.Now().Add(-time.Hour)
	now := time.Now().UTC()

	first := RecoveryWaiter{
		SetID:        "first",
		Preset:       "claude",
		ResetAt:      resetAt,
		RuntimePath:  repo,
		Priority:     5,
		RegisteredAt: now.Add(-2 * time.Hour),
	}
	second := RecoveryWaiter{
		SetID:        "second",
		Preset:       "claude",
		ResetAt:      resetAt,
		RuntimePath:  repo,
		Priority:     5,
		RegisteredAt: now.Add(-1 * time.Hour),
	}

	if _, err := RegisterRecoveryWaiter(d, first); err != nil {
		t.Fatalf("RegisterRecoveryWaiter first: %v", err)
	}
	if _, err := RegisterRecoveryWaiter(d, second); err != nil {
		t.Fatalf("RegisterRecoveryWaiter second: %v", err)
	}

	acquiredSecond, _, err := acquireRecoveryTurn(d, &second)
	if err != nil {
		t.Fatalf("acquireRecoveryTurn second: %v", err)
	}
	acquiredFirst, _, err := acquireRecoveryTurn(d, &first)
	if err != nil {
		t.Fatalf("acquireRecoveryTurn first: %v", err)
	}
	if acquiredSecond {
		t.Fatal("later-registered waiter must not jump ahead at equal priority")
	}
	if !acquiredFirst {
		t.Fatal("earlier-registered waiter should acquire at equal priority")
	}
}

func TestAcquireRecoveryTurn_DifferentCheckoutsParallel(t *testing.T) {
	d, repo := drainTestRepo(t)
	path2 := filepath.Join(filepath.Dir(repo), "worktree-b")
	if err := os.MkdirAll(path2, 0o755); err != nil {
		t.Fatal(err)
	}
	resetAt := time.Now().Add(-time.Hour)

	waiterA := RecoveryWaiter{
		SetID:       "set-a",
		Preset:      "claude",
		ResetAt:     resetAt,
		RuntimePath: repo,
		Priority:    0,
	}
	waiterB := RecoveryWaiter{
		SetID:       "set-b",
		Preset:      "claude",
		ResetAt:     resetAt,
		RuntimePath: path2,
		Priority:    100,
	}

	if _, err := RegisterRecoveryWaiter(d, waiterA); err != nil {
		t.Fatalf("RegisterRecoveryWaiter set-a: %v", err)
	}
	if _, err := RegisterRecoveryWaiter(d, waiterB); err != nil {
		t.Fatalf("RegisterRecoveryWaiter set-b: %v", err)
	}

	acquiredA, _, err := acquireRecoveryTurn(d, &waiterA)
	if err != nil {
		t.Fatalf("acquireRecoveryTurn set-a: %v", err)
	}
	acquiredB, _, err := acquireRecoveryTurn(d, &waiterB)
	if err != nil {
		t.Fatalf("acquireRecoveryTurn set-b: %v", err)
	}
	if !acquiredA || !acquiredB {
		t.Fatalf("both checkouts should acquire independently: a=%v b=%v", acquiredA, acquiredB)
	}
}

// TestAcquireRecoveryTurn_BlockedByClaimBearingGateHold pins ADR-0135: a live
// claim-bearing Failed-gate hold (a dirty tree under review) of another set
// defers a quota-recovered waiter's turn; releasing it lets the waiter acquire.
func TestAcquireRecoveryTurn_BlockedByClaimBearingGateHold(t *testing.T) {
	d, repo := drainTestRepo(t)

	waiter := RecoveryWaiter{
		SetID:       "set-b",
		Preset:      "claude",
		ResetAt:     time.Now().Add(-time.Hour),
		RuntimePath: repo,
	}
	_, err := RegisterRecoveryWaiter(d, waiter)
	if err != nil {
		t.Fatalf("RegisterRecoveryWaiter failed: %v", err)
	}

	if err := RegisterCheckoutGateHold(d, "set-a", repo, true); err != nil {
		t.Fatalf("RegisterCheckoutGateHold failed: %v", err)
	}

	acquired, block, err := acquireRecoveryTurn(d, &waiter)
	if err != nil {
		t.Fatalf("acquireRecoveryTurn failed: %v", err)
	}
	if acquired {
		t.Fatal("recovery turn must not be acquired while a claim-bearing gate hold is active")
	}
	if block == nil || block.Kind != store.RecoveryBlockClaimed || block.Claim == nil ||
		block.Claim.Kind != store.ClaimFailedGate || block.Claim.SetID != "set-a" {
		t.Fatalf("want failed_gate claim block for set-a, got %+v", block)
	}

	if err := ReleaseCheckoutGateHold(d, "set-a", repo); err != nil {
		t.Fatalf("ReleaseCheckoutGateHold failed: %v", err)
	}

	acquired, _, err = acquireRecoveryTurn(d, &waiter)
	if err != nil {
		t.Fatalf("acquireRecoveryTurn after release failed: %v", err)
	}
	if !acquired {
		t.Fatal("recovery turn should be acquired after gate hold is released")
	}
	_ = ReleaseRecoveryTurn(d, repo)
}

// TestAcquireRecoveryTurn_AcquiresPastNonClaimingGateHold pins ADR-0135: a
// non-claiming gate hold (HITL/approval/verify-fail/clean Failed gate) of another
// set is quiescence occupancy only and does not defer a quota-recovered waiter.
func TestAcquireRecoveryTurn_AcquiresPastNonClaimingGateHold(t *testing.T) {
	d, repo := drainTestRepo(t)

	waiter := RecoveryWaiter{
		SetID:       "set-b",
		Preset:      "claude",
		ResetAt:     time.Now().Add(-time.Hour),
		RuntimePath: repo,
	}
	if _, err := RegisterRecoveryWaiter(d, waiter); err != nil {
		t.Fatalf("RegisterRecoveryWaiter failed: %v", err)
	}
	if err := RegisterCheckoutGateHold(d, "set-a", repo, false); err != nil {
		t.Fatalf("RegisterCheckoutGateHold failed: %v", err)
	}

	acquired, block, err := acquireRecoveryTurn(d, &waiter)
	if err != nil {
		t.Fatalf("acquireRecoveryTurn failed: %v", err)
	}
	if !acquired {
		t.Fatalf("recovery turn should be acquired past a non-claiming gate hold, got block %+v", block)
	}
	_ = ReleaseRecoveryTurn(d, repo)
}

// TestReconcileSweepsDeadGateHoldUnblocksRecoveryTurn exercises the full path:
// a claim-bearing gate hold registered by a process that then dies must not block
// a recovery turn on that checkout forever — the reconcile pass sweeps the
// dead-owner hold, after which the waiting set acquires its turn.
func TestReconcileSweepsDeadGateHoldUnblocksRecoveryTurn(t *testing.T) {
	d, repo := drainTestRepo(t)

	// The recovery waiter is owned by a live process (waiter-token).
	d.ProcessStartToken = func(int) (string, bool) { return "waiter-token", true }
	waiter := RecoveryWaiter{
		SetID:       "set-b",
		Preset:      "claude",
		ResetAt:     time.Now().Add(-time.Hour),
		RuntimePath: repo,
	}
	if _, err := RegisterRecoveryWaiter(d, waiter); err != nil {
		t.Fatalf("RegisterRecoveryWaiter: %v", err)
	}

	// A claim-bearing gate hold owned by a *different* process (gate-token) blocks
	// acquisition.
	d.ProcessStartToken = func(int) (string, bool) { return "gate-token", true }
	if err := RegisterCheckoutGateHold(d, "set-a", repo, true); err != nil {
		t.Fatalf("RegisterCheckoutGateHold: %v", err)
	}
	acquired, _, err := acquireRecoveryTurn(d, &waiter)
	if err != nil {
		t.Fatalf("acquireRecoveryTurn (blocked): %v", err)
	}
	if acquired {
		t.Fatal("recovery turn must not be acquired while a gate hold is active")
	}

	// The gate owner dies while the waiter's process lives on: the live process's
	// start token is the waiter's, so the reconcile pass sweeps the gate hold
	// (gate-token no longer matches) but leaves the still-live waiter. The store's
	// liveness policy is fixed at open (ADR-0118) but consults d's process seam
	// live on each call, so pointing the token at the survivor models this.
	d.ProcessStartToken = func(int) (string, bool) { return "waiter-token", true }
	if _, err := ReconcileDrains(d); err != nil {
		t.Fatalf("ReconcileDrains: %v", err)
	}
	if hold, _ := GetCheckoutGateHold(d, repo); hold != nil {
		t.Fatalf("dead-owner gate hold survived reconcile: %+v", hold)
	}
	if w, _ := GetRecoveryWaiter(d, "set-b"); w == nil {
		t.Fatal("live-owner waiter was wrongly swept alongside the dead gate hold")
	}

	// With the orphan swept, the waiting set acquires its recovery turn.
	acquired, _, err = acquireRecoveryTurn(d, &waiter)
	if err != nil {
		t.Fatalf("acquireRecoveryTurn (after sweep): %v", err)
	}
	if !acquired {
		t.Fatal("recovery turn should be acquired after the dead gate hold is swept")
	}
	_ = ReleaseRecoveryTurn(d, repo)
}

// TestReconcileLeavesLiveGateHold guards the survival case: a gate hold whose
// owner is still alive must never be swept.
func TestReconcileLeavesLiveGateHold(t *testing.T) {
	d, repo := drainTestRepo(t)
	d.ProcessStartToken = func(int) (string, bool) { return "gate-token", true }

	if err := RegisterCheckoutGateHold(d, "set-a", repo, false); err != nil {
		t.Fatalf("RegisterCheckoutGateHold: %v", err)
	}
	// d.ProcessAlive reports the current process (the hold owner) alive.
	if _, err := ReconcileDrains(d); err != nil {
		t.Fatalf("ReconcileDrains: %v", err)
	}
	hold, err := GetCheckoutGateHold(d, repo)
	if err != nil {
		t.Fatalf("GetCheckoutGateHold: %v", err)
	}
	if hold == nil {
		t.Fatal("live gate hold was wrongly swept by reconcile")
	}
	if hold.PID != os.Getpid() || hold.ProcStart != "gate-token" {
		t.Fatalf("gate hold owner identity not persisted: %+v", hold)
	}
}

func TestRegisterCheckoutGateHold(t *testing.T) {
	d, repo := drainTestRepo(t)

	if err := RegisterCheckoutGateHold(d, "set-a", repo, false); err != nil {
		t.Fatalf("RegisterCheckoutGateHold failed: %v", err)
	}

	hold, err := GetCheckoutGateHold(d, repo)
	if err != nil {
		t.Fatalf("GetCheckoutGateHold failed: %v", err)
	}
	if hold == nil {
		t.Fatal("gate hold not found after registration")
	}
	if hold.SetID != "set-a" {
		t.Errorf("SetID = %q, want set-a", hold.SetID)
	}
	if hold.RuntimePath != repo {
		t.Errorf("RuntimePath = %q, want %q", hold.RuntimePath, repo)
	}
	if hold.RegisteredAt.IsZero() {
		t.Error("RegisteredAt should be set")
	}

	if err := ReleaseCheckoutGateHold(d, "set-a", repo); err != nil {
		t.Fatalf("ReleaseCheckoutGateHold failed: %v", err)
	}
	hold, err = GetCheckoutGateHold(d, repo)
	if err != nil {
		t.Fatalf("GetCheckoutGateHold after release failed: %v", err)
	}
	if hold != nil {
		t.Error("gate hold should be nil after release")
	}
}

// TestReconcileSweepsDeadRecoveryWaiter exercises the queue-deferral chokepoint:
// a recovery waiter registered by a process that then dies must not linger and
// defer its set in the Queue forever (ADR-0135). The reconcile pass sweeps the
// dead-owner waiter, after which the queue's waiter reader (AllRecoveryWaiters)
// no longer sees it.
func TestReconcileSweepsDeadRecoveryWaiter(t *testing.T) {
	d, repo := drainTestRepo(t)
	d.ProcessStartToken = func(int) (string, bool) { return "waiter-token", true }

	waiter := RecoveryWaiter{
		SetID:       "set-a",
		Preset:      "claude",
		ResetAt:     time.Now().Add(-time.Hour),
		RuntimePath: repo,
	}
	if _, err := RegisterRecoveryWaiter(d, waiter); err != nil {
		t.Fatalf("RegisterRecoveryWaiter: %v", err)
	}

	// The registering process dies. The reconcile pass (run by whoever next
	// reads) sweeps the dead-owner waiter using the same PID+start-token liveness
	// as drains. The store's liveness policy is fixed at open (ADR-0118) but
	// consults d's process seam live on each call, so flipping the seam models
	// the owner dying.
	d.ProcessAlive = func(int) bool { return false }
	if _, err := ReconcileDrains(d); err != nil {
		t.Fatalf("ReconcileDrains: %v", err)
	}

	// The queue-deferral reader observes the post-sweep view.
	waiters, err := AllRecoveryWaiters(d)
	if err != nil {
		t.Fatalf("AllRecoveryWaiters: %v", err)
	}
	if _, ok := waiters["set-a"]; ok {
		t.Fatalf("dead-owner waiter survived reconcile: %+v", waiters["set-a"])
	}
	if got, _ := GetRecoveryWaiter(d, "set-a"); got != nil {
		t.Fatalf("dead-owner waiter row survived reconcile: %+v", got)
	}
}

// TestReconcileLeavesLiveRecoveryWaiter guards the survival case: a recovery
// waiter whose owner is still alive must never be swept, and its owner identity
// (PID + start token) round-trips through the store.
func TestReconcileLeavesLiveRecoveryWaiter(t *testing.T) {
	d, repo := drainTestRepo(t)
	d.ProcessStartToken = func(int) (string, bool) { return "waiter-token", true }

	waiter := RecoveryWaiter{
		SetID:       "set-a",
		Preset:      "claude",
		ResetAt:     time.Now().Add(-time.Hour),
		RuntimePath: repo,
	}
	if _, err := RegisterRecoveryWaiter(d, waiter); err != nil {
		t.Fatalf("RegisterRecoveryWaiter: %v", err)
	}

	// d.ProcessAlive reports the current process (the waiter owner) alive.
	if _, err := ReconcileDrains(d); err != nil {
		t.Fatalf("ReconcileDrains: %v", err)
	}
	got, err := GetRecoveryWaiter(d, "set-a")
	if err != nil {
		t.Fatalf("GetRecoveryWaiter: %v", err)
	}
	if got == nil {
		t.Fatal("live recovery waiter was wrongly swept by reconcile")
	}
	if got.PID != os.Getpid() || got.ProcStart != "waiter-token" {
		t.Fatalf("waiter owner identity not persisted: %+v", got)
	}
}

func TestAcquireRecoveryTurn_BeforeReset(t *testing.T) {
	d, _ := drainTestRepo(t)
	
	waiter := RecoveryWaiter{
		SetID:       "demo",
		Preset:      "claude",
		ResetAt:     time.Now().Add(time.Hour), // In the future
		RuntimePath: "/test/path",
	}
	
	_, err := RegisterRecoveryWaiter(d, waiter)
	if err != nil {
		t.Fatalf("RegisterRecoveryWaiter failed: %v", err)
	}
	
	acquired, _, err := acquireRecoveryTurn(d, &waiter)
	if err != nil {
		t.Fatalf("acquireRecoveryTurn failed: %v", err)
	}
	if acquired {
		t.Error("should not acquire turn before reset time")
	}
}

func TestRecoveryWaiterRoundTrip(t *testing.T) {
	d, _ := drainTestRepo(t)
	
	now := time.Now().Truncate(time.Millisecond)
	waiter := RecoveryWaiter{
		SetID:       "demo",
		Preset:      "claude",
		ResetAt:     now.Add(time.Hour),
		RuntimePath: "/test/path",
	}
	
	_, err := RegisterRecoveryWaiter(d, waiter)
	if err != nil {
		t.Fatalf("RegisterRecoveryWaiter failed: %v", err)
	}
	
	stored, err := GetRecoveryWaiter(d, "demo")
	if err != nil {
		t.Fatalf("GetRecoveryWaiter failed: %v", err)
	}
	
	if stored.SetID != waiter.SetID {
		t.Errorf("SetID = %q, want %q", stored.SetID, waiter.SetID)
	}
	if stored.Preset != waiter.Preset {
		t.Errorf("Preset = %q, want %q", stored.Preset, waiter.Preset)
	}
	if !stored.ResetAt.Equal(waiter.ResetAt) {
		t.Errorf("ResetAt = %v, want %v", stored.ResetAt, waiter.ResetAt)
	}
	if stored.RuntimePath != waiter.RuntimePath {
		t.Errorf("RuntimePath = %q, want %q", stored.RuntimePath, waiter.RuntimePath)
	}
}

