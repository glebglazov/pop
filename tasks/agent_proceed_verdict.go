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
	// ProceedSpendCap is the time-healing kind for a provider refusing the
	// account because somebody capped its spending: the CLI is healthy, the
	// session is valid, and no allowance is exhausted — a human set a ceiling
	// and the account has reached it. It is its own flavour rather than a quota
	// pause because a quota pause names the moment it lifts and a spend cap
	// names nothing at all, so pop dates it itself (ADR-0231).
	ProceedSpendCap AgentProceedKind = "spend_cap"
	// ProceedAuthFailure is a human-healing kind: a logged-out CLI (ADR-0153).
	ProceedAuthFailure AgentProceedKind = "auth_failure"
	// ProceedMissingBinary is a human-healing kind: the preset's binary is
	// absent from PATH (ADR-0153).
	ProceedMissingBinary AgentProceedKind = "missing_binary"
	// ProceedModelRefused is the kind for a provider refusing one model to this
	// account while the CLI itself stays healthy — kimi's subscription 401
	// (`does not have access to …`), and the same shape for a broker's spent
	// per-vendor allowance. It is model-scoped when the Effort ladder resolved
	// the model, and escalates to preset scope once the tier has no entry left
	// (ADR-0168).
	ProceedModelRefused AgentProceedKind = "model_refused"
	// ProceedTierExhausted is the preset-scoped kind for an Effort tier whose
	// every entry is already recorded as an Effort model skip, so this run never
	// spawned the preset at all (ADR-0168).
	ProceedTierExhausted AgentProceedKind = "tier_exhausted"
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
	// A Scope=Preset verdict abandons the rest of the cap instead of spending
	// from it, and an Effort model skip restarts the attempt on the tier's next
	// entry rather than spending a try, so it is false throughout: nothing about
	// the Task failed in either case.
	ConsumesAttempt bool
	Reason          string
	Preset          string
	// Model names the model the verdict is about, for a verdict that condemns one
	// model rather than the whole preset. Empty otherwise.
	Model string
	// WindowClass names which allowance a quota refusal exhausted, as the
	// refusal's own channel stated it — the typed field where the capture
	// carried one, the marker sentence where it did not (ADR-0234). Empty for
	// every other flavour, and for a refusal that named no window.
	WindowClass AgentQuotaWindowClass
}

// TimeHealingRecovery witnesses that a verdict heals with time, carrying the
// instant to wait for. Only TimeHealing produces one, and Agent quota recovery
// wait takes it by value — so a human-healing or permanent verdict cannot reach
// that wait (ADR-0153).
type TimeHealingRecovery struct {
	ResetAt time.Time
}

// spendCapCooldown is how long pop puts a spend-capped preset aside. It is
// pop's own number, not a figure any provider reported: a spend cap names no
// moment it will lift, so there is nothing to parse and something has to be
// invented for the preset to rejoin the walk on its own.
//
// An hour is the deliberate guess. A cap raised over lunch costs almost nothing
// — the preset is back within the hour — and a cap nobody ever raises costs one
// refused invocation an hour instead of needing a human to notice a stopped
// drain. Hitting the cap again simply starts another hour (ADR-0231).
const spendCapCooldown = time.Hour

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

// NewSpendCapVerdict builds a spend-cap verdict: preset-scoped, because one
// capped account can run no model through this CLI, and time-healing, because
// the invented hour is what brings it back.
func NewSpendCapVerdict(preset, reason string, resetAt time.Time) AgentProceedVerdict {
	return AgentProceedVerdict{
		Kind:     ProceedSpendCap,
		Scope:    ProceedScopePreset,
		Recovery: ProceedRecoveryTime,
		ResetAt:  resetAt,
		Reason:   reason,
		Preset:   preset,
	}
}

// DetectedSpendCap is the adapter-side form of a spend cap: kind and the
// provider's own sentence. Preset and ResetAt are filled by the executor after
// detection, the reset from spendCapCooldown rather than from this reason.
func DetectedSpendCap(reason string) *AgentProceedVerdict {
	v := NewSpendCapVerdict("", reason, time.Time{})
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

// NewModelRefusedVerdict builds a model-scoped verdict against one model: the
// CLI is healthy and this token is not runnable on this account. recovery says
// what would bring it back — Permanent for a subscription that simply lacks the
// model, Time for an allowance that refills. model is what the diagnostic or the
// invocation names as refused; it names the Effort model skip to record and the
// wording the human reads, never a policy branch.
func NewModelRefusedVerdict(preset, model, reason string, recovery AgentProceedRecovery) AgentProceedVerdict {
	return AgentProceedVerdict{
		Kind:     ProceedModelRefused,
		Scope:    ProceedScopeModel,
		Recovery: recovery,
		Reason:   reason,
		Preset:   preset,
		Model:    model,
	}
}

// DetectedPermanentModelRefusal is the adapter-side form of a model refusal that
// never heals on its own: kind, reason, and the model the provider's own
// diagnostic named. Preset — and the model pop pinned, when it pinned one — are
// filled by the executor after detection.
func DetectedPermanentModelRefusal(model, reason string) *AgentProceedVerdict {
	v := NewModelRefusedVerdict("", model, reason, ProceedRecoveryPermanent)
	return &v
}

// NewTierExhaustedVerdict builds the preset-scoped stop for an Effort tier whose
// every entry is already recorded as skipped, so the preset was never spawned.
// Human-healing: only widening the tier or waiting out the skips brings it back,
// and the recovery wait polls for neither.
func NewTierExhaustedVerdict(preset, reason string) AgentProceedVerdict {
	return AgentProceedVerdict{
		Kind:     ProceedTierExhausted,
		Scope:    ProceedScopePreset,
		Recovery: ProceedRecoveryHuman,
		Reason:   reason,
		Preset:   preset,
	}
}

// escalateToPreset returns a model-scoped verdict as the preset-scoped stop it
// becomes when its Effort tier has no entry left to walk to: same cause, same
// recovery, but condemning the whole preset so Agent fallback advances (ADR-0168).
func (v AgentProceedVerdict) escalateToPreset() AgentProceedVerdict {
	v.Scope = ProceedScopePreset
	return v
}

// stampDetectedVerdict completes an adapter-detected verdict with what only the
// caller knows: which preset produced it and, for a model-scoped verdict, the
// model pop pinned for the invocation. The pinned alias outranks the wire name
// the provider used, because the alias is the Effort ladder entry to skip and
// what the human would change; an invocation that pins no model leaves the
// adapter's name in place.
func stampDetectedVerdict(v AgentProceedVerdict, preset, pinnedModel string) AgentProceedVerdict {
	v = v.WithPreset(preset)
	if v.Scope == ProceedScopeModel && pinnedModel != "" {
		v.Model = pinnedModel
	}
	return v
}

// fallThroughMessage is the dim line printed when the orchestrator walks past a
// stopped agent — past the model, for a model-scoped verdict, and past the whole
// preset otherwise. role is the caller's word for what it is walking past
// ("Agent" on implement, "Verifier agent" on verify). Scope picks which list is
// advancing and Kind picks the wording, so a kind nobody has taught a phrase to
// still names the preset rather than printing nothing.
func (v AgentProceedVerdict) fallThroughMessage(role string) string {
	if v.Scope == ProceedScopeModel {
		return fmt.Sprintf("%s %s cannot run %s; trying the next model in its effort tier", role, v.Preset, v.modelOrPlaceholder())
	}
	switch v.Kind {
	case ProceedQuotaPause:
		return fmt.Sprintf("%s %s quota-paused; trying next", role, v.Preset)
	case ProceedSpendCap:
		return fmt.Sprintf("%s %s stopped by a spend cap; cooling %s — pop's own wait, since a cap names no reset of its own; trying next", role, v.Preset, formatDuration(spendCapCooldown))
	case ProceedAuthFailure:
		return fmt.Sprintf("%s %s unauthenticated; trying next", role, v.Preset)
	case ProceedMissingBinary:
		return fmt.Sprintf("%s %s unavailable (binary not found); trying next", role, v.Preset)
	case ProceedModelRefused:
		return fmt.Sprintf("%s %s cannot run %s and has no effort tier entry left; trying next", role, v.Preset, v.modelOrPlaceholder())
	case ProceedTierExhausted:
		return fmt.Sprintf("%s %s has no runnable model left in its effort tier; trying next", role, v.Preset)
	default:
		return fmt.Sprintf("%s %s unavailable; trying next", role, v.Preset)
	}
}

// effortModelSkipMessage is the dim line printed when the orchestrator walks
// past one Effort tier entry onto the next inside the same preset: it names the
// model skipped and the model taking over, so the human reads the substitution
// rather than a gap. A walk that cannot name its successor falls back to the
// generic model-scoped phrasing.
func (v AgentProceedVerdict) effortModelSkipMessage(role, next string) string {
	next = strings.TrimSpace(next)
	if next == "" {
		return v.fallThroughMessage(role)
	}
	return fmt.Sprintf("%s %s cannot run %s; trying %s instead", role, v.Preset, v.modelOrPlaceholder(), next)
}

// modelOrPlaceholder names the model a verdict is about, standing in a phrase
// for the invocation that pinned none.
func (v AgentProceedVerdict) modelOrPlaceholder() string {
	if v.Model == "" {
		return "its resolved model"
	}
	return v.Model
}

// resolveProceedResetAt fills in the instant a time-healing verdict waits for.
// Every flavour but one is dated by the provider: the preset's adapter parses
// the reset out of the diagnostic. A spend cap is dated by pop, because the
// refusal names no reset, so the adapter must not be asked — and must not
// overwrite the hour with the zero instant it would answer (ADR-0231).
//
// A verdict that already carries an instant keeps it. This seam is handed a
// reason and nothing else, so an adapter that dated the refusal from its whole
// capture — claude's rate-limit epoch (ADR-0233) — knows something re-deriving
// from the sentence cannot recover.
func resolveProceedResetAt(v AgentProceedVerdict, now time.Time) AgentProceedVerdict {
	if _, ok := v.TimeHealing(); !ok {
		return v
	}
	if v.Kind == ProceedSpendCap {
		return v.WithResetAt(now.Add(spendCapCooldown))
	}
	if !v.ResetAt.IsZero() {
		return v
	}
	return v.WithResetAt(agentQuotaResetAt(v.Preset, v.Reason, now))
}

// pausesUntilReset reports the flavours that park a drain until a moment
// passes, which is what the QuotaPaused result fields mirror for callers that
// never see the verdict itself: a quota pause and a spend cap alike leave the
// task Open, record the preset as cooling, and enter Agent quota recovery wait.
func (v AgentProceedVerdict) pausesUntilReset() bool {
	return v.Kind == ProceedQuotaPause || v.Kind == ProceedSpendCap
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

// WithWindowClass returns a copy naming the Quota window class the refusal
// exhausted.
func (v AgentProceedVerdict) WithWindowClass(class AgentQuotaWindowClass) AgentProceedVerdict {
	v.WindowClass = class
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
