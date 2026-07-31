package tasks

import (
	"fmt"
	"strings"
	"time"
)

// AgentUnavailabilityKind identifies which flavour of Agent unavailability this is.
type AgentUnavailabilityKind string

const (
	// UnavailabilityQuotaPause is the time-healing kind (ADR-0153).
	UnavailabilityQuotaPause AgentUnavailabilityKind = "quota_pause"
	// UnavailabilityAuthFailure is a human-healing kind: a logged-out CLI (ADR-0153).
	UnavailabilityAuthFailure AgentUnavailabilityKind = "auth_failure"
	// UnavailabilityMissingBinary is a human-healing kind: the preset's binary is
	// absent from PATH (ADR-0153).
	UnavailabilityMissingBinary AgentUnavailabilityKind = "missing_binary"
	// UnavailabilityPlanGate is a human-healing kind: the agent reported that the
	// resolved model is not on this account's plan at all (ADR-0151). Human-healing
	// because only a plan upgrade or a different model clears it — and, unlike a
	// quota pause, no cooldown is worth recording: the gate is deterministic per
	// account+model, so the next attempt re-probes once and falls through again.
	UnavailabilityPlanGate AgentUnavailabilityKind = "plan_gate"
)

// AgentUnavailabilityRecovery is what would make an unavailable Agent preset
// usable again. The closed set is TimeHealingRecovery and HumanHealingRecovery —
// a human-healing recovery cannot be passed to the quota recovery wait because
// that entry point takes TimeHealingRecovery by value.
type AgentUnavailabilityRecovery interface {
	agentUnavailabilityRecovery()
}

// TimeHealingRecovery carries a reset instant and drives Agent quota recovery
// wait. It is the only recovery the wait entry point accepts.
type TimeHealingRecovery struct {
	ResetAt time.Time
}

func (TimeHealingRecovery) agentUnavailabilityRecovery() {}

// HumanHealingRecovery carries no instant; polling cannot resolve it. Present so
// later slices can add human-healing kinds without touching the recovery wait.
type HumanHealingRecovery struct{}

func (HumanHealingRecovery) agentUnavailabilityRecovery() {}

// AgentUnavailability is the verdict that this Agent preset cannot do the work
// at all, as distinct from an attempt that ran and failed (ADR-0153). Every kind
// abandons the remaining Task retry cap for that preset and hands the turn to
// the next preset in the Agent fallback list; kinds differ only in Recovery.
type AgentUnavailability struct {
	Kind     AgentUnavailabilityKind
	Recovery AgentUnavailabilityRecovery
	Reason   string
	Preset   string
	// Model names the model the verdict is about, for a kind that gates one model
	// rather than the whole preset (a Plan gate). Empty for every other kind.
	Model string
}

// NewQuotaPauseUnavailability builds the sole time-healing kind.
func NewQuotaPauseUnavailability(preset, reason string, resetAt time.Time) AgentUnavailability {
	return AgentUnavailability{
		Kind:     UnavailabilityQuotaPause,
		Recovery: TimeHealingRecovery{ResetAt: resetAt},
		Reason:   reason,
		Preset:   preset,
	}
}

// DetectedQuotaPause is the adapter-side form of a quota pause: kind and reason
// only. Preset and ResetAt are filled by the executor after detection.
func DetectedQuotaPause(reason string) *AgentUnavailability {
	u := NewQuotaPauseUnavailability("", reason, time.Time{})
	return &u
}

// NewAuthFailureUnavailability builds a human-healing auth-failure verdict.
func NewAuthFailureUnavailability(preset, reason string) AgentUnavailability {
	return AgentUnavailability{
		Kind:     UnavailabilityAuthFailure,
		Recovery: HumanHealingRecovery{},
		Reason:   reason,
		Preset:   preset,
	}
}

// DetectedAuthFailure is the adapter-side form of an auth failure: kind and
// reason only. Preset is filled by the executor after detection.
func DetectedAuthFailure(reason string) *AgentUnavailability {
	u := NewAuthFailureUnavailability("", reason)
	return &u
}

// NewMissingBinaryUnavailability builds a human-healing missing-binary verdict.
func NewMissingBinaryUnavailability(preset, reason string) AgentUnavailability {
	return AgentUnavailability{
		Kind:     UnavailabilityMissingBinary,
		Recovery: HumanHealingRecovery{},
		Reason:   reason,
		Preset:   preset,
	}
}

// NewPlanGateUnavailability builds a human-healing plan-gate verdict against one
// model. model is what the diagnostic or the invocation names as gated; it only
// ever reaches human-facing messaging, never a policy decision.
func NewPlanGateUnavailability(preset, model, reason string) AgentUnavailability {
	return AgentUnavailability{
		Kind:     UnavailabilityPlanGate,
		Recovery: HumanHealingRecovery{},
		Reason:   reason,
		Preset:   preset,
		Model:    model,
	}
}

// DetectedPlanGate is the adapter-side form of a plan gate: kind, reason, and the
// model the provider's own diagnostic named. Preset — and the model pop pinned,
// when it pinned one — are filled by the executor after detection.
func DetectedPlanGate(model, reason string) *AgentUnavailability {
	u := NewPlanGateUnavailability("", model, reason)
	return &u
}

// stampDetectedUnavailability completes an adapter-detected verdict with what only
// the caller knows: which preset produced it and, for a Plan gate, the model pop
// pinned for the invocation. The pinned alias outranks the wire name the provider
// used, because the alias is what the human would change to clear the gate; an
// invocation that pins no model leaves the adapter's name in place.
func stampDetectedUnavailability(u AgentUnavailability, preset, pinnedModel string) AgentUnavailability {
	u = u.WithPreset(preset)
	if u.Kind == UnavailabilityPlanGate && pinnedModel != "" {
		u.Model = pinnedModel
	}
	return u
}

// formatPlanGateFallThrough names the gated preset and the model it was gated on,
// mirroring the quota-pause fall-through wording. role is the caller's word for
// the agent it is walking past ("Agent" on implement, "Verifier agent" on verify).
func formatPlanGateFallThrough(role string, u AgentUnavailability) string {
	model := u.Model
	if model == "" {
		model = "its resolved model"
	}
	return fmt.Sprintf("%s %s plan-gated on %s; trying next", role, u.Preset, model)
}

// TimeHealing reports the time-healing recovery when present. A human-healing
// verdict returns false — the gate that keeps it out of recovery wait.
func (u AgentUnavailability) TimeHealing() (TimeHealingRecovery, bool) {
	if u.Recovery == nil {
		return TimeHealingRecovery{}, false
	}
	th, ok := u.Recovery.(TimeHealingRecovery)
	return th, ok
}

// WithPreset returns a copy with Preset set.
func (u AgentUnavailability) WithPreset(preset string) AgentUnavailability {
	u.Preset = preset
	return u
}

// WithResetAt returns a copy whose time-healing recovery carries resetAt.
// Non-time-healing recoveries are left unchanged.
func (u AgentUnavailability) WithResetAt(resetAt time.Time) AgentUnavailability {
	if _, ok := u.TimeHealing(); ok {
		u.Recovery = TimeHealingRecovery{ResetAt: resetAt}
	}
	return u
}

// formatHumanHealingExhaustionMessage names each exhausted preset and quotes the
// provider diagnostic for an all-human-healing agent fallback exhaustion (ADR-0153).
func formatHumanHealingExhaustionMessage(presets []AgentUnavailability) string {
	if len(presets) == 0 {
		return "all configured agents unavailable"
	}
	var b strings.Builder
	b.WriteString("all configured agents unavailable:")
	for _, u := range presets {
		fmt.Fprintf(&b, "\n  %s: %q", u.Preset, u.Reason)
	}
	return b.String()
}
