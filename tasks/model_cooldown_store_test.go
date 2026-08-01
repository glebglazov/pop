package tasks

import (
	"testing"
	"time"
)

func TestUpdateAgentModelCooldownUsesResetAtWhenParsed(t *testing.T) {
	t.Parallel()
	d := newTestDeps(t)
	resetAt := time.Now().Add(45 * time.Minute)

	if err := updateAgentModelCooldown(d, "cursor", "claude-opus-5-thinking-high", resetAt, false); err != nil {
		t.Fatalf("updateAgentModelCooldown: %v", err)
	}

	active, err := ActiveAgentModelCooldownsWith(d, time.Now())
	if err != nil {
		t.Fatalf("ActiveAgentModelCooldownsWith: %v", err)
	}
	if len(active) != 1 {
		t.Fatalf("active = %#v, want 1 entry", active)
	}
	want := agentQuotaCooldownUntil(resetAt, time.Now(), defaultAgentQuotaRetryAfter)
	if diff := active[0].Until.Sub(want); diff < -time.Minute || diff > time.Minute {
		t.Fatalf("Until = %s, want close to %s (via agentQuotaCooldownUntil)", active[0].Until, want)
	}
}

func TestUpdateAgentModelCooldownDefaultsToOneHourWithoutResetAt(t *testing.T) {
	t.Parallel()
	d := newTestDeps(t)
	before := time.Now()

	if err := updateAgentModelCooldown(d, "kimi", "k2.7-code-highspeed", time.Time{}, false); err != nil {
		t.Fatalf("updateAgentModelCooldown: %v", err)
	}

	active, err := ActiveAgentModelCooldownsWith(d, time.Now())
	if err != nil {
		t.Fatalf("ActiveAgentModelCooldownsWith: %v", err)
	}
	if len(active) != 1 {
		t.Fatalf("active = %#v, want 1 entry", active)
	}
	got := active[0].Until
	if got.Before(before.Add(defaultAgentQuotaRetryAfter)) || got.After(time.Now().Add(defaultAgentQuotaRetryAfter+time.Minute)) {
		t.Fatalf("Until = %s, want ~%s from now (one hour default)", got, defaultAgentQuotaRetryAfter)
	}
}

func TestUpdateAgentModelCooldownPermanentNeverExpires(t *testing.T) {
	t.Parallel()
	d := newTestDeps(t)

	if err := updateAgentModelCooldown(d, "kimi", "subscription-gated-model", time.Time{}, true); err != nil {
		t.Fatalf("updateAgentModelCooldown: %v", err)
	}

	farFuture := time.Now().AddDate(10, 0, 0)
	active, err := ActiveAgentModelCooldownsWith(d, farFuture)
	if err != nil {
		t.Fatalf("ActiveAgentModelCooldownsWith: %v", err)
	}
	if len(active) != 1 || !active[0].Until.IsZero() {
		t.Fatalf("active = %#v, want one permanent (zero Until) entry", active)
	}
}

func TestActiveAgentModelCooldownsWithNoStoreReturnsNoRows(t *testing.T) {
	t.Parallel()
	d := newTestDeps(t)

	active, err := ActiveAgentModelCooldownsWith(d, time.Now())
	if err != nil {
		t.Fatalf("ActiveAgentModelCooldownsWith: %v", err)
	}
	if len(active) != 0 {
		t.Fatalf("active = %#v, want none (no store ever created)", active)
	}
}

func TestUpdateAgentModelCooldownEmptyPresetOrModelIsNoop(t *testing.T) {
	t.Parallel()
	d := newTestDeps(t)

	if err := updateAgentModelCooldown(d, "", "some-model", time.Now(), false); err != nil {
		t.Fatalf("updateAgentModelCooldown empty preset: %v", err)
	}
	if err := updateAgentModelCooldown(d, "cursor", "", time.Now(), false); err != nil {
		t.Fatalf("updateAgentModelCooldown empty model: %v", err)
	}

	active, err := ActiveAgentModelCooldownsWith(d, time.Now())
	if err != nil {
		t.Fatalf("ActiveAgentModelCooldownsWith: %v", err)
	}
	if len(active) != 0 {
		t.Fatalf("active = %#v, want none written", active)
	}
}
