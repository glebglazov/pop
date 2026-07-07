package tasks

import "github.com/glebglazov/pop/store"

// ResolveVerifiedStatus is the single read-side Verified status resolution
// (CONTEXT.md): it layers a set's Verify verdicts onto its manifest-derived
// status and reports the ADR-0096 immunization SHA to surface. Every surface
// that gates status on a verdict — `pop tasks status`, the Queue dashboard,
// `pop queue status`/daemon scan, and the pre-approval Drain phase — routes
// through here, so the gate rule and the immunization-SHA surfacing live in one
// place.
//
// It assumes Agent verification is enabled: every caller checks that gate before
// reaching this function, so the enable flag is not part of the interface.
// DeriveStatusWithVerdict remains the pure inner core (verification on/off,
// verdict enums only); this function wraps it, converting the two stored verdict
// slots and computing the surfaced SHA.
//
// currentAtSHA is the verdict recorded at the set's current work SHA (nil when
// absent or stale); latestPass is the most recent PASS verdict for the set
// regardless of SHA (nil when the set has never passed). These are exactly what
// the two store getters return, so callers pass them straight through. It is
// read-only and side-effect free — deciding whether to *run* the Verifier on a
// cache miss belongs to the Drain phase, not here.
//
// It returns the resolved status and, when a terminal set is immunized by an
// older PASS whose SHA differs from the current work SHA, the short SHA of that
// PASS (empty otherwise). Callers already hold the verdicts they pass in, so the
// gating verdict is not echoed back.
func ResolveVerifiedStatus(m *Manifest, workSHA string, currentAtSHA, latestPass *store.VerifyVerdict) (TaskSetStatus, string) {
	var current *Verdict
	if currentAtSHA != nil {
		vv := Verdict(currentAtSHA.Verdict)
		current = &vv
	}
	var pass *Verdict
	if latestPass != nil {
		vv := Verdict(latestPass.Verdict)
		pass = &vv
	}

	status := DeriveStatusWithVerdict(m, true, current, pass)

	// ADR-0096: when the terminal status stands only because an older PASS
	// immunizes it (no verdict at HEAD, HEAD has moved past that PASS), surface
	// the SHA the set was verified at.
	if status == StatusDone || status == StatusAwaitingApproval {
		if currentAtSHA == nil && latestPass != nil &&
			latestPass.Verdict == string(VerdictPass) && latestPass.WorkSHA != workSHA {
			return status, ShortSHA(latestPass.WorkSHA)
		}
	}
	return status, ""
}
