package queue

import "time"

// DeferralReason names why a Ready set is present but not being spawned. Its
// three species are the read-side unification (ADR-0106) of two independent
// pause mechanisms that stay structurally separate: crash backoff / park
// (derived from Drain history, queue-owned, no live process — ADR-0055) and
// quota-recovery waiters (registered by a live quota-paused process, owned by
// implement — ADR-0100). Readers — dispatch decisions, dashboard rows, run
// output — consume this single vocabulary; it is the shape the future global
// scheduler inherits.
type DeferralReason int

const (
	// DeferNone is the zero value: no deferral (the set is spawnable, or there is
	// no ready set at all).
	DeferNone DeferralReason = iota
	// DeferCrashBackoff is a timed backoff after an abnormal Drain exit; Until is
	// the instant the set next becomes spawnable.
	DeferCrashBackoff
	// DeferParked is an indefinite park after repeated abnormal Drain exits; it is
	// cleared only by a human unpark, so Until is zero.
	DeferParked
	// DeferQuotaRecovery is a process-owned wait for an agent's quota to recover;
	// Until is the reset instant reported by the live waiter.
	DeferQuotaRecovery
)

// SpawnDeferral is the single readiness-side representation of "Ready but not
// spawning" (ADR-0106): a reason species, the set it concerns, and an optional
// until-instant (zero for the indefinite Parked species).
type SpawnDeferral struct {
	Reason DeferralReason
	SetID  string
	Until  time.Time
}

// Deferred reports whether this value carries a real deferral.
func (d SpawnDeferral) Deferred() bool { return d.Reason != DeferNone }

// Message is the human-readable decision reason for the deferral species. It is
// the single source of the wording that dispatch decisions and dashboard/run
// output render; call sites no longer hand-write these strings.
func (r DeferralReason) Message() string {
	switch r {
	case DeferCrashBackoff:
		return "set backed off after abnormal drain exit"
	case DeferParked:
		return "set parked after repeated abnormal drain exits"
	case DeferQuotaRecovery:
		return "set waiting for quota recovery"
	default:
		return ""
	}
}

// Kind is the run-view kind slug for the deferral species, matching the
// BlockedItem.Kind values the renderers switch on.
func (r DeferralReason) Kind() string {
	switch r {
	case DeferCrashBackoff:
		return "crash_backoff"
	case DeferParked:
		return "parked"
	case DeferQuotaRecovery:
		return "recovery_wait"
	default:
		return ""
	}
}
