package store

import (
	"path/filepath"
	"testing"
	"time"
)

func TestAgentModelCooldownSurvivesRestart(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "pop.db")
	now := time.Date(2026, 6, 15, 12, 0, 0, 0, time.UTC)
	until := now.Add(30 * time.Minute)

	s, err := Open(path, func(int, string) bool { return true })
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if err := s.PutAgentModelCooldown("cursor", "claude-opus-5-thinking-high", until, time.Time{}); err != nil {
		t.Fatalf("PutAgentModelCooldown: %v", err)
	}
	if err := s.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	reopened, err := Open(path, func(int, string) bool { return true })
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer func() { _ = reopened.Close() }()

	active, err := reopened.ActiveAgentModelCooldowns(now)
	if err != nil {
		t.Fatalf("ActiveAgentModelCooldowns: %v", err)
	}
	if len(active) != 1 {
		t.Fatalf("active = %#v, want 1 entry", active)
	}
	got := active[0]
	if got.Preset != "cursor" || got.Model != "claude-opus-5-thinking-high" || !got.Until.Equal(until) {
		t.Fatalf("entry = %#v, want preset=cursor model=claude-opus-5-thinking-high until=%s", got, until)
	}
}

func TestAgentModelCooldownPermanentNeverExpires(t *testing.T) {
	t.Parallel()
	s := openTestStore(t)
	now := time.Date(2026, 6, 15, 12, 0, 0, 0, time.UTC)

	if err := s.PutAgentModelCooldown("kimi", "k2.7-code-highspeed", time.Time{}, time.Time{}); err != nil {
		t.Fatalf("PutAgentModelCooldown: %v", err)
	}

	far := now.AddDate(50, 0, 0)
	active, err := s.ActiveAgentModelCooldowns(far)
	if err != nil {
		t.Fatalf("ActiveAgentModelCooldowns: %v", err)
	}
	if len(active) != 1 {
		t.Fatalf("active = %#v, want 1 permanent entry", active)
	}
	if !active[0].Until.IsZero() {
		t.Fatalf("Until = %s, want zero (never expires)", active[0].Until)
	}
}

func TestAgentModelCooldownReadsFilterToInForce(t *testing.T) {
	t.Parallel()
	s := openTestStore(t)
	now := time.Date(2026, 6, 15, 12, 0, 0, 0, time.UTC)

	if err := s.PutAgentModelCooldown("cursor", "composer-2.5", now.Add(time.Hour), time.Time{}); err != nil {
		t.Fatalf("put active: %v", err)
	}
	if err := s.PutAgentModelCooldown("cursor", "gpt-5.6", now.Add(-time.Minute), time.Time{}); err != nil {
		t.Fatalf("put expired: %v", err)
	}

	active, err := s.ActiveAgentModelCooldowns(now)
	if err != nil {
		t.Fatalf("ActiveAgentModelCooldowns: %v", err)
	}
	if len(active) != 1 || active[0].Model != "composer-2.5" {
		t.Fatalf("active = %#v, want only composer-2.5", active)
	}

	// Advancing past the remaining entry's expiry drops it too.
	later, err := s.ActiveAgentModelCooldowns(now.Add(2 * time.Hour))
	if err != nil {
		t.Fatalf("ActiveAgentModelCooldowns later: %v", err)
	}
	if len(later) != 0 {
		t.Fatalf("later active = %#v, want none", later)
	}
}

func TestAgentModelCooldownLatestWriteWins(t *testing.T) {
	t.Parallel()
	s := openTestStore(t)
	now := time.Date(2026, 6, 15, 12, 0, 0, 0, time.UTC)

	if err := s.PutAgentModelCooldown("cursor", "composer-2.5", now.Add(time.Hour), time.Time{}); err != nil {
		t.Fatalf("first put: %v", err)
	}
	if err := s.PutAgentModelCooldown("cursor", "composer-2.5", now.Add(2*time.Hour), time.Time{}); err != nil {
		t.Fatalf("second put: %v", err)
	}

	active, err := s.ActiveAgentModelCooldowns(now)
	if err != nil {
		t.Fatalf("ActiveAgentModelCooldowns: %v", err)
	}
	if len(active) != 1 || !active[0].Until.Equal(now.Add(2*time.Hour)) {
		t.Fatalf("active = %#v, want single entry at +2h", active)
	}
}

func TestAgentModelCooldownDoesNotAffectPresetCooldownReads(t *testing.T) {
	t.Parallel()
	s := openTestStore(t)
	now := time.Date(2026, 6, 15, 12, 0, 0, 0, time.UTC)

	if err := s.PutAgentCooldown("cursor", now.Add(time.Hour)); err != nil {
		t.Fatalf("PutAgentCooldown: %v", err)
	}
	if err := s.PutAgentModelCooldown("cursor", "composer-2.5", now.Add(time.Hour), time.Time{}); err != nil {
		t.Fatalf("PutAgentModelCooldown: %v", err)
	}
	if err := s.PutAgentModelCooldown("claude", "opus", time.Time{}, time.Time{}); err != nil {
		t.Fatalf("PutAgentModelCooldown permanent: %v", err)
	}

	presets, err := s.AllAgentCooldowns()
	if err != nil {
		t.Fatalf("AllAgentCooldowns: %v", err)
	}
	if len(presets) != 1 {
		t.Fatalf("presets = %#v, want exactly the one preset row, no model rows leaking in", presets)
	}
	until, ok := presets["cursor"]
	if !ok || !until.Equal(now.Add(time.Hour)) {
		t.Fatalf("presets[cursor] = %v, ok=%v, want %s", until, ok, now.Add(time.Hour))
	}
}
