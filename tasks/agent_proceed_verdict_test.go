package tasks

import (
	"strings"
	"testing"
	"time"
)

func TestQuotaPauseVerdictIsTimeHealing(t *testing.T) {
	resetAt := time.Date(2026, 7, 30, 12, 0, 0, 0, time.UTC)
	v := NewQuotaPauseVerdict("claude", "weekly limit", resetAt)
	if v.Kind != ProceedQuotaPause {
		t.Fatalf("kind = %q, want %q", v.Kind, ProceedQuotaPause)
	}
	if v.Recovery != ProceedRecoveryTime {
		t.Fatalf("recovery = %q, want %q", v.Recovery, ProceedRecoveryTime)
	}
	th, ok := v.TimeHealing()
	if !ok {
		t.Fatal("quota pause must be time-healing")
	}
	if !th.ResetAt.Equal(resetAt) {
		t.Fatalf("ResetAt = %v, want %v", th.ResetAt, resetAt)
	}
}

func TestHumanHealingCannotReportTimeHealing(t *testing.T) {
	v := NewAuthFailureVerdict("cursor", "Authentication required")
	if v.Kind != ProceedAuthFailure {
		t.Fatalf("kind = %q, want %q", v.Kind, ProceedAuthFailure)
	}
	if _, ok := v.TimeHealing(); ok {
		t.Fatal("human-healing must not report TimeHealing — recovery wait accepts TimeHealingRecovery only")
	}
}

func TestMissingBinaryVerdictIsHumanHealing(t *testing.T) {
	v := NewMissingBinaryVerdict("cursor", "binary not found on PATH")
	if v.Kind != ProceedMissingBinary {
		t.Fatalf("kind = %q, want %q", v.Kind, ProceedMissingBinary)
	}
	if _, ok := v.TimeHealing(); ok {
		t.Fatal("missing binary must be human-healing")
	}
}

// TestWholeCLIVerdictsArePresetScopedAndFree pins ADR-0168's dispatch axis: a
// verdict about the CLI itself condemns the preset, and none of them charge the
// Task retry cap.
func TestWholeCLIVerdictsArePresetScopedAndFree(t *testing.T) {
	for _, v := range []AgentProceedVerdict{
		NewQuotaPauseVerdict("claude", "weekly limit", time.Now()),
		NewAuthFailureVerdict("cursor", "Authentication required"),
		NewMissingBinaryVerdict("codex", "binary not found on PATH"),
		NewTierExhaustedVerdict("kimi", "every effort tier model is skipped: k3"),
		*DetectedQuotaPause("usage limit"),
		*DetectedAuthFailure("Error: Authentication required."),
	} {
		if v.Scope != ProceedScopePreset {
			t.Fatalf("kind %q: scope = %q, want %q", v.Kind, v.Scope, ProceedScopePreset)
		}
		if v.ConsumesAttempt {
			t.Fatalf("kind %q: ConsumesAttempt = true, want false — a stopped preset abandons the retry cap", v.Kind)
		}
	}
}

// A model refusal condemns one token, and escalates to the whole preset only
// once its tier has nothing left to walk to — never charging the retry cap on
// either side of that line.
func TestModelRefusalIsModelScopedUntilItEscalates(t *testing.T) {
	for _, v := range []AgentProceedVerdict{
		NewModelRefusedVerdict("kimi", "k3", "does not have access to k3", ProceedRecoveryPermanent),
		*DetectedPermanentModelRefusal("k3", "does not have access to k3"),
	} {
		if v.Scope != ProceedScopeModel {
			t.Fatalf("scope = %q, want %q", v.Scope, ProceedScopeModel)
		}
		if v.ConsumesAttempt {
			t.Fatal("an Effort model skip must not charge the retry cap")
		}
		escalated := v.escalateToPreset()
		if escalated.Scope != ProceedScopePreset {
			t.Fatalf("escalated scope = %q, want %q", escalated.Scope, ProceedScopePreset)
		}
		if escalated.Recovery != v.Recovery || escalated.Reason != v.Reason || escalated.Model != v.Model {
			t.Fatalf("escalation lost the cause: %#v", escalated)
		}
		if escalated.ConsumesAttempt {
			t.Fatal("escalation must not start charging the retry cap")
		}
	}
}

// TestFallThroughMessageNamesEveryKind is the dispatch counterpart: the drain
// decides to print from Scope, and the wording comes from the verdict itself —
// including for a kind nobody has taught a phrase to.
func TestFallThroughMessageNamesEveryKind(t *testing.T) {
	for _, tt := range []struct {
		verdict AgentProceedVerdict
		role    string
		want    string
	}{
		{NewQuotaPauseVerdict("claude", "weekly limit", time.Time{}), "Agent", "Agent claude quota-paused; trying next"},
		{NewAuthFailureVerdict("cursor", "logged out"), "Agent", "Agent cursor unauthenticated; trying next"},
		{NewMissingBinaryVerdict("codex", "absent"), "Verifier agent", "Verifier agent codex unavailable (binary not found); trying next"},
		{NewModelRefusedVerdict("kimi", "k3", "gated", ProceedRecoveryPermanent), "Agent", "Agent kimi cannot run k3; trying the next model in its effort tier"},
		{NewModelRefusedVerdict("kimi", "k3", "gated", ProceedRecoveryPermanent).escalateToPreset(), "Agent", "Agent kimi cannot run k3 and has no effort tier entry left; trying next"},
		{NewModelRefusedVerdict("kimi", "", "gated", ProceedRecoveryPermanent).escalateToPreset(), "Verifier agent", "Verifier agent kimi cannot run its resolved model and has no effort tier entry left; trying next"},
		{NewTierExhaustedVerdict("kimi", "every effort tier model is skipped: k3"), "Agent", "Agent kimi has no runnable model left in its effort tier; trying next"},
		{AgentProceedVerdict{Preset: "future"}, "Agent", "Agent future unavailable; trying next"},
	} {
		if got := tt.verdict.fallThroughMessage(tt.role); got != tt.want {
			t.Fatalf("fallThroughMessage = %q, want %q", got, tt.want)
		}
	}
}

func TestFormatHumanHealingExhaustionMessage(t *testing.T) {
	msg := formatHumanHealingExhaustionMessage([]AgentProceedVerdict{
		NewAuthFailureVerdict("cursor", "Error: Authentication required"),
		NewMissingBinaryVerdict("claude", "binary not found on PATH"),
	})
	if !strings.Contains(msg, "cursor: \"Error: Authentication required\"") {
		t.Fatalf("missing cursor diagnostic: %q", msg)
	}
	if !strings.Contains(msg, "claude: \"binary not found on PATH\"") {
		t.Fatalf("missing claude diagnostic: %q", msg)
	}
}

func TestResolveAgentFallbackVerdictPrefersTimeHealing(t *testing.T) {
	sel := &Selection{TaskSetID: "demo", TaskID: "01-a"}
	resetEarly := time.Date(2026, 7, 30, 10, 0, 0, 0, time.UTC)
	resetLate := time.Date(2026, 7, 30, 12, 0, 0, 0, time.UTC)
	results := []*RunTaskResult{
		proceedVerdictResult(sel, NewAuthFailureVerdict("cursor", "logged out")),
		proceedVerdictResult(sel, NewQuotaPauseVerdict("codex", "usage limit", resetLate)),
		proceedVerdictResult(sel, NewQuotaPauseVerdict("claude", "weekly limit", resetEarly)),
	}
	got := resolveAgentFallbackVerdict(sel, results)
	if got == nil || got.ProceedVerdict == nil {
		t.Fatal("expected quota pause result")
	}
	if got.ProceedVerdict.Preset != "claude" {
		t.Fatalf("preset = %q, want claude", got.ProceedVerdict.Preset)
	}
	if len(got.UnavailablePresets) != 0 {
		t.Fatalf("UnavailablePresets = %#v, want nil for mixed list", got.UnavailablePresets)
	}
}

func TestResolveAgentFallbackVerdictCollectsHumanHealing(t *testing.T) {
	sel := &Selection{TaskSetID: "demo", TaskID: "01-a"}
	results := []*RunTaskResult{
		proceedVerdictResult(sel, NewAuthFailureVerdict("cursor", "logged out")),
		proceedVerdictResult(sel, NewMissingBinaryVerdict("claude", "binary not found on PATH")),
	}
	got := resolveAgentFallbackVerdict(sel, results)
	if got == nil || len(got.UnavailablePresets) != 2 {
		t.Fatalf("UnavailablePresets = %#v, want 2 entries", got)
	}
	if _, ok := got.ProceedVerdict.TimeHealing(); ok {
		t.Fatal("all-human-healing exhaustion must not report time-healing")
	}
}
