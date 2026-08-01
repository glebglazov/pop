package tasks

import (
	"fmt"
	"strings"
	"time"
)

// AgentProceedKind names why an Agent cannot carry on. It is descriptive only —
// it decides human-facing wording, never a policy branch; Scope and Recovery
// decide those.
type AgentProceedKind string

const (
	// ProceedQuotaPause is the time-healing kind (ADR-0153).
	ProceedQuotaPause AgentProceedKind = "quota_pause"
	// ProceedAuthFailure is a human-healing kind: a logged-out CLI (ADR-0153).
	ProceedAuthFailure AgentProceedKind = "auth_failure"
	// ProceedMissingBinary is a human-healing kind: the preset's binary is
	// absent from PATH (ADR-0153).
	ProceedMissingBinary AgentProceedKind = "missing_binary"
	// ProceedPlanGate is a human-healing kind: the agent reported that the
	// resolved model is not on this account's plan at all (ADR-0164). Human-healing
	// because only a plan upgrade or a different model clears it — and, unlike a
	// quota pause, no cooldown is worth recording: the gate is deterministic per
	// account+model, so the next attempt re-probes once and falls through again.
	ProceedPlanGate AgentProceedKind = "plan_gate"
)

// AgentProceedScope is how much of an Agent a verdict condemns (ADR-0168).
// Dispatch reads this rather than the kind, so a new kind lands without editing
// the orchestrator.
type AgentProceedScope string

const (
	// ProceedScopePreset condemns the whole entry in the Agent fallback list:
	// one adapter, one CLI, one login can run nothing. It abandons the remaining
	// Task retry cap for that preset and hands the turn to the next preset; it is
	// the only scope that reaches the preset cooldown store and the Agent quota
	// recovery wait.
	ProceedScopePreset AgentProceedScope = "preset"
	// ProceedScopeModel condemns only the `--model` token the Effort ladder tier
	// resolved: the CLI is healthy and this one model is not.
	ProceedScopeModel AgentProceedScope = "model"
)

// AgentProceedRecovery is what would make the condemned scope usable again
// (ADR-0153, ADR-0168).
type AgentProceedRecovery string

const (
	// ProceedRecoveryTime heals on its own; the verdict may carry a reset instant
	// and it is the only recovery the quota recovery wait accepts.
	ProceedRecoveryTime AgentProceedRecovery = "time"
	// ProceedRecoveryHuman needs an operator — a login, a plan change. Polling
	// cannot resolve it, so it must never enter the recovery wait.
	ProceedRecoveryHuman AgentProceedRecovery = "human"
	// ProceedRecoveryPermanent never heals for this account and scope, so nothing
	// is worth waiting for and no expiry is worth recording.
	ProceedRecoveryPermanent AgentProceedRecovery = "permanent"
)

// AgentProceedVerdict is the shared answer every Agent adapter gives to "can you
// carry on?" (ADR-0168). A nil verdict means yes; a present one says at what
// Scope the agent is stopped, what would heal it, when it resets if the adapter
// parsed an instant, and whether the attempt is charged to the Task retry cap.
//
// It subsumes ADR-0153's separate unavailability report: that report is the
// Scope=Preset case, not a sibling type, so the dispatch rule is stated once.
type AgentProceedVerdict struct {
	Kind     AgentProceedKind
	Scope    AgentProceedScope
	Recovery AgentProceedRecovery
	// ResetAt is present only when a reset instant is known — the adapter parsed
	// one, or the executor derived one for a quota pause. Zero otherwise, and
	// always zero for a recovery that no instant can heal.
	ResetAt time.Time
	// ConsumesAttempt reports whether this verdict charges the Task retry cap.
	// Every Scope=Preset verdict abandons the rest of the cap instead of spending
	// from it, so it is false throughout; ADR-0168's Effort model skip is what
	// makes the field vary.
	ConsumesAttempt bool
	Reason          string
	Preset          string
	// Model names the model the verdict is about, for a verdict that condemns one
	// model rather than the whole preset (a Plan gate). Empty otherwise.
	Model string
}

// TimeHealingRecovery witnesses that a verdict heals with time, carrying the
// instant to wait for. Only TimeHealing produces one, and Agent quota recovery
// wait takes it by value — so a human-healing or permanent verdict cannot reach
// that wait (ADR-0153).
type TimeHealingRecovery struct {
	ResetAt time.Time
}

// NewQuotaPauseVerdict builds the sole time-healing kind.
func NewQuotaPauseVerdict(preset, reason string, resetAt time.Time) AgentProceedVerdict {
	return AgentProceedVerdict{
		Kind:     ProceedQuotaPause,
		Scope:    ProceedScopePreset,
		Recovery: ProceedRecoveryTime,
		ResetAt:  resetAt,
		Reason:   reason,
		Preset:   preset,
	}
}

// DetectedQuotaPause is the adapter-side form of a quota pause: kind and reason
// only. Preset and ResetAt are filled by the executor after detection.
func DetectedQuotaPause(reason string) *AgentProceedVerdict {
	v := NewQuotaPauseVerdict("", reason, time.Time{})
	return &v
}

// NewAuthFailureVerdict builds a human-healing auth-failure verdict.
func NewAuthFailureVerdict(preset, reason string) AgentProceedVerdict {
	return AgentProceedVerdict{
		Kind:     ProceedAuthFailure,
		Scope:    ProceedScopePreset,
		Recovery: ProceedRecoveryHuman,
		Reason:   reason,
		Preset:   preset,
	}
}

// DetectedAuthFailure is the adapter-side form of an auth failure: kind and
// reason only. Preset is filled by the executor after detection.
func DetectedAuthFailure(reason string) *AgentProceedVerdict {
	v := NewAuthFailureVerdict("", reason)
	return &v
}

// NewMissingBinaryVerdict builds a human-healing missing-binary verdict.
func NewMissingBinaryVerdict(preset, reason string) AgentProceedVerdict {
	return AgentProceedVerdict{
		Kind:     ProceedMissingBinary,
		Scope:    ProceedScopePreset,
		Recovery: ProceedRecoveryHuman,
		Reason:   reason,
		Preset:   preset,
	}
}

// NewPlanGateVerdict builds a human-healing plan-gate verdict against one model.
// model is what the diagnostic or the invocation names as gated; it only ever
// reaches human-facing messaging, never a policy decision.
func NewPlanGateVerdict(preset, model, reason string) AgentProceedVerdict {
	return AgentProceedVerdict{
		Kind:     ProceedPlanGate,
		Scope:    ProceedScopePreset,
		Recovery: ProceedRecoveryHuman,
		Reason:   reason,
		Preset:   preset,
		Model:    model,
	}
}

// DetectedPlanGate is the adapter-side form of a plan gate: kind, reason, and the
// model the provider's own diagnostic named. Preset — and the model pop pinned,
// when it pinned one — are filled by the executor after detection.
func DetectedPlanGate(model, reason string) *AgentProceedVerdict {
	v := NewPlanGateVerdict("", model, reason)
	return &v
}

// stampDetectedVerdict completes an adapter-detected verdict with what only the
// caller knows: which preset produced it and, for a Plan gate, the model pop
// pinned for the invocation. The pinned alias outranks the wire name the provider
// used, because the alias is what the human would change to clear the gate; an
// invocation that pins no model leaves the adapter's name in place.
func stampDetectedVerdict(v AgentProceedVerdict, preset, pinnedModel string) AgentProceedVerdict {
	v = v.WithPreset(preset)
	if v.Kind == ProceedPlanGate && pinnedModel != "" {
		v.Model = pinnedModel
	}
	return v
}

// fallThroughMessage is the dim line printed when the orchestrator walks past a
// stopped agent. role is the caller's word for what it is walking past ("Agent"
// on implement, "Verifier agent" on verify). Kind picks the wording, so a kind
// nobody has taught a phrase to still names the preset rather than printing
// nothing.
func (v AgentProceedVerdict) fallThroughMessage(role string) string {
	switch v.Kind {
	case ProceedQuotaPause:
		return fmt.Sprintf("%s %s quota-paused; trying next", role, v.Preset)
	case ProceedAuthFailure:
		return fmt.Sprintf("%s %s unauthenticated; trying next", role, v.Preset)
	case ProceedMissingBinary:
		return fmt.Sprintf("%s %s unavailable (binary not found); trying next", role, v.Preset)
	case ProceedPlanGate:
		model := v.Model
		if model == "" {
			model = "its resolved model"
		}
		return fmt.Sprintf("%s %s plan-gated on %s; trying next", role, v.Preset, model)
	default:
		return fmt.Sprintf("%s %s unavailable; trying next", role, v.Preset)
	}
}

// TimeHealing reports the time-healing witness when this verdict heals with
// time. Human-healing and permanent verdicts return false — the gate that keeps
// them out of recovery wait.
func (v AgentProceedVerdict) TimeHealing() (TimeHealingRecovery, bool) {
	if v.Recovery != ProceedRecoveryTime {
		return TimeHealingRecovery{}, false
	}
	return TimeHealingRecovery{ResetAt: v.ResetAt}, true
}

// WithPreset returns a copy with Preset set.
func (v AgentProceedVerdict) WithPreset(preset string) AgentProceedVerdict {
	v.Preset = preset
	return v
}

// WithResetAt returns a copy carrying resetAt. A recovery no instant can heal is
// left unchanged.
func (v AgentProceedVerdict) WithResetAt(resetAt time.Time) AgentProceedVerdict {
	if _, ok := v.TimeHealing(); ok {
		v.ResetAt = resetAt
	}
	return v
}

// formatHumanHealingExhaustionMessage names each exhausted preset and quotes the
// provider diagnostic for an all-human-healing agent fallback exhaustion (ADR-0153).
func formatHumanHealingExhaustionMessage(presets []AgentProceedVerdict) string {
	if len(presets) == 0 {
		return "all configured agents unavailable"
	}
	var b strings.Builder
	b.WriteString("all configured agents unavailable:")
	for _, v := range presets {
		fmt.Fprintf(&b, "\n  %s: %q", v.Preset, v.Reason)
	}
	return b.String()
}
