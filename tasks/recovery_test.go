package tasks

import (
	"testing"
	"time"
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

func TestAcquireRecoveryTurn_PresetAgnostic(t *testing.T) {
	d, _ := drainTestRepo(t)
	
	// Register two waiters with different presets
	waiter1 := RecoveryWaiter{
		SetID:       "demo1",
		Preset:      "claude",
		ResetAt:     time.Now().Add(-time.Hour), // Already past
		RuntimePath: "/test/path1",
	}
	waiter2 := RecoveryWaiter{
		SetID:       "demo2",
		Preset:      "codex",
		ResetAt:     time.Now().Add(-time.Hour), // Already past
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
	
	// Both should be able to acquire a turn (preset-agnostic)
	acquired1, err := acquireRecoveryTurn(d, &waiter1)
	if err != nil {
		t.Fatalf("acquireRecoveryTurn failed: %v", err)
	}
	if !acquired1 {
		t.Error("waiter1 should acquire turn")
	}
	
	acquired2, err := acquireRecoveryTurn(d, &waiter2)
	if err != nil {
		t.Fatalf("acquireRecoveryTurn failed: %v", err)
	}
	if !acquired2 {
		t.Error("waiter2 should acquire turn")
	}
}

func TestAcquireRecoveryTurn_BlockedByCheckoutGateHold(t *testing.T) {
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

	if err := RegisterCheckoutGateHold(d, "set-a", repo); err != nil {
		t.Fatalf("RegisterCheckoutGateHold failed: %v", err)
	}

	acquired, err := acquireRecoveryTurn(d, &waiter)
	if err != nil {
		t.Fatalf("acquireRecoveryTurn failed: %v", err)
	}
	if acquired {
		t.Fatal("recovery turn must not be acquired while checkout gate hold is active")
	}

	if err := ReleaseCheckoutGateHold(d, repo); err != nil {
		t.Fatalf("ReleaseCheckoutGateHold failed: %v", err)
	}

	acquired, err = acquireRecoveryTurn(d, &waiter)
	if err != nil {
		t.Fatalf("acquireRecoveryTurn after release failed: %v", err)
	}
	if !acquired {
		t.Fatal("recovery turn should be acquired after gate hold is released")
	}
}

func TestRegisterCheckoutGateHold(t *testing.T) {
	d, repo := drainTestRepo(t)

	if err := RegisterCheckoutGateHold(d, "set-a", repo); err != nil {
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

	if err := ReleaseCheckoutGateHold(d, repo); err != nil {
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
	
	acquired, err := acquireRecoveryTurn(d, &waiter)
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

