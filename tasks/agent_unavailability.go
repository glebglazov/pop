package tasks

import "time"

// AgentUnavailabilityKind identifies which flavour of Agent unavailability this is.
type AgentUnavailabilityKind string

const (
	// UnavailabilityQuotaPause is the sole time-healing kind (ADR-0153).
	UnavailabilityQuotaPause AgentUnavailabilityKind = "quota_pause"
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
