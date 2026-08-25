package store

import (
	"testing"
	"time"
)

// guessedCooldown is the row a refusal pop had to guess about writes: an expiry
// dated from the window class's ceiling, no stated instant, and the first ask
// one interval out.
func guessedCooldown(preset string, now time.Time, ceiling, firstProbe time.Duration) AgentCooldown {
	return AgentCooldown{
		Preset:         preset,
		ExhaustedUntil: now.Add(ceiling),
		NextProbeAt:    now.Add(firstProbe),
	}
}

func TestQuotaProbeClaimIsWonByOneCheckoutPerInterval(t *testing.T) {
	t.Parallel()
	s := openTestStore(t)
	now := time.Date(2026, 8, 25, 19, 58, 0, 0, time.UTC)
	if _, err := s.RecordAgentQuotaCooldown(guessedCooldown("claude", now, 5*time.Hour, 10*time.Minute), now); err != nil {
		t.Fatalf("RecordAgentQuotaCooldown: %v", err)
	}

	due := now.Add(10 * time.Minute)
	first, err := s.ClaimQuotaProbe("claude", due, time.Minute)
	if err != nil {
		t.Fatalf("first ClaimQuotaProbe: %v", err)
	}
	if first == nil {
		t.Fatal("the ask came due and nobody held it, so the claim should have been won")
	}
	second, err := s.ClaimQuotaProbe("claude", due, time.Minute)
	if err != nil {
		t.Fatalf("second ClaimQuotaProbe: %v", err)
	}
	if second != nil {
		t.Fatal("two checkouts both won the same ask")
	}

	// Refused: the question is free again, but not until the next one is due.
	if err := s.ScheduleNextQuotaProbe("claude", due.Add(10*time.Minute)); err != nil {
		t.Fatalf("ScheduleNextQuotaProbe: %v", err)
	}
	if early, err := s.ClaimQuotaProbe("claude", due.Add(time.Minute), time.Minute); err != nil || early != nil {
		t.Fatalf("claim before the next ask is due = (%v, %v), want (nil, nil)", early, err)
	}
	if next, err := s.ClaimQuotaProbe("claude", due.Add(10*time.Minute), time.Minute); err != nil || next == nil {
		t.Fatalf("claim once the next ask is due = (%v, %v), want a row", next, err)
	}
}

// A prober that dies holds the ask only for the life of its lease: the next
// process wins it by the lease elapsing, with nothing sweeping anything.
func TestQuotaProbeClaimOfADeadProberLapses(t *testing.T) {
	t.Parallel()
	s := openTestStore(t)
	now := time.Date(2026, 8, 25, 19, 58, 0, 0, time.UTC)
	if _, err := s.RecordAgentQuotaCooldown(guessedCooldown("claude", now, 5*time.Hour, 0), now); err != nil {
		t.Fatalf("RecordAgentQuotaCooldown: %v", err)
	}
	lease := time.Minute
	if claimed, err := s.ClaimQuotaProbe("claude", now, lease); err != nil || claimed == nil {
		t.Fatalf("first claim = (%v, %v), want a row", claimed, err)
	}
	// The owner is gone; it never scheduled the next ask or released the lease.
	if mid, err := s.ClaimQuotaProbe("claude", now.Add(lease/2), lease); err != nil || mid != nil {
		t.Fatalf("claim inside a live lease = (%v, %v), want (nil, nil)", mid, err)
	}
	after, err := s.ClaimQuotaProbe("claude", now.Add(lease+time.Second), lease)
	if err != nil {
		t.Fatalf("claim after the lease: %v", err)
	}
	if after == nil {
		t.Fatal("a dead prober's claim should lapse on its own within the lease")
	}
}

func TestQuotaProbeClaimRefusesAStatedCooldown(t *testing.T) {
	t.Parallel()
	s := openTestStore(t)
	now := time.Date(2026, 8, 25, 19, 58, 0, 0, time.UTC)
	stated := now.Add(2 * time.Hour)
	if _, err := s.RecordAgentQuotaCooldown(AgentCooldown{
		Preset:         "claude",
		ExhaustedUntil: stated,
		StatedUntil:    stated,
		Class:          "five_hour",
	}, now); err != nil {
		t.Fatalf("RecordAgentQuotaCooldown: %v", err)
	}
	claimed, err := s.ClaimQuotaProbe("claude", now.Add(time.Hour), time.Minute)
	if err != nil {
		t.Fatalf("ClaimQuotaProbe: %v", err)
	}
	if claimed != nil {
		t.Fatal("an instant the provider stated is waited to, never probed")
	}
}

// An ask answered yes retires the cooldown and the parks that were queued behind
// it together: a park left sleeping on the ceiling would sit out a window the
// preset has already outlived.
func TestClearAgentQuotaCooldownReleasesEveryParkedWaiter(t *testing.T) {
	t.Parallel()
	s := openTestStore(t)
	now := time.Date(2026, 8, 25, 19, 58, 0, 0, time.UTC)
	ceiling := now.Add(5 * time.Hour)
	if _, err := s.RecordAgentQuotaCooldown(guessedCooldown("claude", now, 5*time.Hour, 10*time.Minute), now); err != nil {
		t.Fatalf("RecordAgentQuotaCooldown: %v", err)
	}
	for _, w := range []RecoveryWaiter{
		{SetID: "set-a", Preset: "claude", ResetAt: ceiling, RuntimePath: "/wt/a", RegisteredAt: now},
		{SetID: "set-b", Preset: "claude", ResetAt: ceiling, RuntimePath: "/wt/b", RegisteredAt: now},
		{SetID: "set-c", Preset: "codex", ResetAt: ceiling, RuntimePath: "/wt/c", RegisteredAt: now},
	} {
		if err := s.PutRecoveryWaiter(w); err != nil {
			t.Fatalf("PutRecoveryWaiter %s: %v", w.SetID, err)
		}
	}

	open := now.Add(time.Hour)
	if err := s.ClearAgentQuotaCooldown("claude", open); err != nil {
		t.Fatalf("ClearAgentQuotaCooldown: %v", err)
	}
	if row, err := s.GetAgentCooldown("claude"); err != nil || row != nil {
		t.Fatalf("cooldown after a yes = (%v, %v), want it gone", row, err)
	}
	waiters, err := s.AllRecoveryWaiters()
	if err != nil {
		t.Fatalf("AllRecoveryWaiters: %v", err)
	}
	for _, w := range waiters {
		want := open
		if w.Preset == "codex" {
			want = ceiling
		}
		if !w.ResetAt.Equal(want) {
			t.Errorf("waiter %s reset_at = %s, want %s", w.SetID, w.ResetAt, want)
		}
	}
}
