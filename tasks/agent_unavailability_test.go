package tasks

import (
	"strings"
	"testing"
	"time"
)

func TestQuotaPauseUnavailabilityIsTimeHealing(t *testing.T) {
	resetAt := time.Date(2026, 7, 30, 12, 0, 0, 0, time.UTC)
	u := NewQuotaPauseUnavailability("claude", "weekly limit", resetAt)
	if u.Kind != UnavailabilityQuotaPause {
		t.Fatalf("kind = %q, want %q", u.Kind, UnavailabilityQuotaPause)
	}
	th, ok := u.TimeHealing()
	if !ok {
		t.Fatal("quota pause must be time-healing")
	}
	if !th.ResetAt.Equal(resetAt) {
		t.Fatalf("ResetAt = %v, want %v", th.ResetAt, resetAt)
	}
}

func TestHumanHealingCannotReportTimeHealing(t *testing.T) {
	u := NewAuthFailureUnavailability("cursor", "Authentication required")
	if u.Kind != UnavailabilityAuthFailure {
		t.Fatalf("kind = %q, want %q", u.Kind, UnavailabilityAuthFailure)
	}
	if _, ok := u.TimeHealing(); ok {
		t.Fatal("human-healing must not report TimeHealing — recovery wait accepts TimeHealingRecovery only")
	}
}

func TestMissingBinaryUnavailabilityIsHumanHealing(t *testing.T) {
	u := NewMissingBinaryUnavailability("cursor", "binary not found on PATH")
	if u.Kind != UnavailabilityMissingBinary {
		t.Fatalf("kind = %q, want %q", u.Kind, UnavailabilityMissingBinary)
	}
	if _, ok := u.TimeHealing(); ok {
		t.Fatal("missing binary must be human-healing")
	}
}

func TestFormatHumanHealingExhaustionMessage(t *testing.T) {
	msg := formatHumanHealingExhaustionMessage([]AgentUnavailability{
		NewAuthFailureUnavailability("cursor", "Error: Authentication required"),
		NewMissingBinaryUnavailability("claude", "binary not found on PATH"),
	})
	if !strings.Contains(msg, "cursor: \"Error: Authentication required\"") {
		t.Fatalf("missing cursor diagnostic: %q", msg)
	}
	if !strings.Contains(msg, "claude: \"binary not found on PATH\"") {
		t.Fatalf("missing claude diagnostic: %q", msg)
	}
}

func TestResolveAgentFallbackUnavailablePrefersTimeHealing(t *testing.T) {
	sel := &Selection{TaskSetID: "demo", TaskID: "01-a"}
	resetEarly := time.Date(2026, 7, 30, 10, 0, 0, 0, time.UTC)
	resetLate := time.Date(2026, 7, 30, 12, 0, 0, 0, time.UTC)
	results := []*RunTaskResult{
		unavailabilityResult(sel, NewAuthFailureUnavailability("cursor", "logged out")),
		unavailabilityResult(sel, NewQuotaPauseUnavailability("codex", "usage limit", resetLate)),
		unavailabilityResult(sel, NewQuotaPauseUnavailability("claude", "weekly limit", resetEarly)),
	}
	got := resolveAgentFallbackUnavailable(sel, results)
	if got == nil || got.Unavailability == nil {
		t.Fatal("expected quota pause result")
	}
	if got.Unavailability.Preset != "claude" {
		t.Fatalf("preset = %q, want claude", got.Unavailability.Preset)
	}
	if len(got.UnavailablePresets) != 0 {
		t.Fatalf("UnavailablePresets = %#v, want nil for mixed list", got.UnavailablePresets)
	}
}

func TestResolveAgentFallbackUnavailableCollectsHumanHealing(t *testing.T) {
	sel := &Selection{TaskSetID: "demo", TaskID: "01-a"}
	results := []*RunTaskResult{
		unavailabilityResult(sel, NewAuthFailureUnavailability("cursor", "logged out")),
		unavailabilityResult(sel, NewMissingBinaryUnavailability("claude", "binary not found on PATH")),
	}
	got := resolveAgentFallbackUnavailable(sel, results)
	if got == nil || len(got.UnavailablePresets) != 2 {
		t.Fatalf("UnavailablePresets = %#v, want 2 entries", got)
	}
	if _, ok := got.Unavailability.TimeHealing(); ok {
		t.Fatal("all-human-healing exhaustion must not report time-healing")
	}
}
