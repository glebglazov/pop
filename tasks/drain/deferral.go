package drain

import (
	"fmt"
	"time"

	"github.com/glebglazov/pop/store"
)

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
	// DeferCheckoutClaim is a checkout-scoped deferral (ADR-0135): a Ready set's
	// bound checkout carries *another* set's live Checkout claim over which quota
	// recovery does not already speak — a dirty Failed-gate hold (or a running
	// drain sharing the checkout). Claim names the holder and claim reason; a
	// quota-waiter claim is instead reported as DeferQuotaRecovery so its reset
	// instant feeds the earliest-eligible display.
	DeferCheckoutClaim
	// DeferAdmissionQueue is the queue-scoped sibling of DeferCheckoutClaim
	// (ADR-0239): nothing holds the Ready set's bound checkout, but a human
	// command is queued for it and waiting for the next window. The daemon stands
	// off rather than taking the window the waiter has been queuing for — without
	// this the daemon can jump a human repeatedly, and the queue's ordering
	// guarantee is only nominal. Claim names the waiting set.
	DeferAdmissionQueue
)

// SpawnDeferral is the single readiness-side representation of "Ready but not
// spawning" (ADR-0106): a reason species, the set it concerns, and an optional
// until-instant (zero for the indefinite Parked species).
type SpawnDeferral struct {
	Reason DeferralReason
	SetID  string
	Until  time.Time
	// Claim carries the other set's Checkout claim when Reason is
	// DeferCheckoutClaim — the holder and claim reason the message names. Nil
	// for every other species.
	Claim *store.CheckoutClaim
}

// Deferred reports whether this value carries a real deferral.
func (d SpawnDeferral) Deferred() bool { return d.Reason != DeferNone }

// Message is the human-readable decision reason for this deferral. For a
// DeferCheckoutClaim it names the holding set and its claim reason; every other
// species defers to the reason-species wording.
func (d SpawnDeferral) Message() string {
	if d.Claim == nil {
		return d.Reason.Message()
	}
	switch d.Reason {
	case DeferCheckoutClaim:
		return fmt.Sprintf("checkout claimed by set %s (%s)", d.Claim.Holder.ContainerID, d.Claim.Reason.Phrase())
	case DeferAdmissionQueue:
		return fmt.Sprintf("checkout awaited by set %s (%s)", d.Claim.Holder.ContainerID, d.Claim.Reason.Phrase())
	}
	return d.Reason.Message()
}

// DeferralForClaim is the one mapping from a Checkout claim to the deferral
// species that reports it, so the Task-set adapter's display and the
// supervisor's cross-kind backstop name the same claim the same way. A queued
// command is the queue species; every other claim reason is the claim species.
func DeferralForClaim(setID string, claim *store.CheckoutClaim) SpawnDeferral {
	reason := DeferCheckoutClaim
	if claim != nil && claim.Reason == store.ClaimQueuedCommand {
		reason = DeferAdmissionQueue
	}
	return SpawnDeferral{Reason: reason, SetID: setID, Claim: claim}
}

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
	case DeferCheckoutClaim:
		return "checkout claimed by another set"
	case DeferAdmissionQueue:
		return "checkout awaited by a queued command"
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
	case DeferCheckoutClaim:
		return "checkout_claim"
	case DeferAdmissionQueue:
		return "admission_queue"
	default:
		return ""
	}
}
