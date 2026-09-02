package tasks

import "github.com/glebglazov/pop/store"

// resolveSetVerifyMark returns the set's Verification mark via the read-side
// resolution — the same answer every surface uses. Human Remediate (ADR-0255)
// asks this, not the raw verdict rows, before writing anything.
func resolveSetVerifyMark(d *Deps, m *Manifest, repo, setID, workSHA string) VerifyMark {
	var current, latestPass *store.VerifyVerdict
	if s, ok, err := openDrainStoreIfExists(d); err == nil && ok && repo != "" && setID != "" {
		if v, err := s.GetVerifyVerdict(repo, setID, workSHA); err == nil {
			current = v
		}
		if v, err := s.GetLatestPassVerifyVerdict(repo, setID); err == nil {
			latestPass = v
		}
	}
	return ResolveVerifiedStatus(m, workSHA, current, latestPass).Mark
}

// verifyMarkLabel is the Verification mark as named in a refusal: the stored
// vocabulary for a present mark, and "none" when the mark is absent.
func verifyMarkLabel(mark VerifyMark) string {
	if mark == VerifyMarkNone {
		return "none"
	}
	return string(mark)
}

// ResolveVerifiedStatus is the single read-side Verified status resolution
// (CONTEXT.md): it layers a set's Verify verdicts onto its manifest-derived
// status and reports the ADR-0096 immunization SHA to surface. Every surface
// that gates status on a verdict — `pop tasks status`, the Work dashboard,
// `pop work status`/daemon scan, and the pre-approval Drain phase — routes
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
// It returns the resolved status together with the verification mark and the
// immunization SHA behind it. Callers already hold the verdicts they pass in, so
// the gating verdict is not echoed back.
func ResolveVerifiedStatus(m *Manifest, workSHA string, currentAtSHA, latestPass *store.VerifyVerdict) VerifiedResolution {
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

	res := VerifiedResolution{Status: DeriveStatusWithVerdict(m, true, current, pass)}
	if !TerminalStatus(DeriveStatus(m)) {
		// Nothing is finished, so there is nothing to have judged: no mark.
		return res
	}

	// The mark is derived from the verdicts alone — the same rule whether or not the
	// set is human-completed. What human completion changes is only whether that
	// answer is also allowed to be the status (see DeriveStatusWithVerdict).
	switch {
	case current != nil && *current == VerdictPass:
		res.Mark = VerifyMarkVerified
		res.VerifiedAtSHA = ShortSHA(currentAtSHA.WorkSHA)
		res.Drifted = currentAtSHA.WorkSHA != workSHA
	case current != nil:
		res.Mark = VerifyMarkFailed
	case pass != nil && *pass == VerdictPass:
		res.Mark = VerifyMarkVerified
		res.VerifiedAtSHA = ShortSHA(latestPass.WorkSHA)
		res.Drifted = latestPass.WorkSHA != workSHA
	default:
		res.Mark = VerifyMarkUnverified
	}
	return res
}

// VerifiedResolution is what the read-side resolution answers for one terminal
// set: the status every surface displays, plus the verification outcome riding
// beside it. The two are separate fields because they are separate facts — a
// human-completed set reads DONE with an unverified or verify-failed mark — and
// they are resolved together here so no surface re-derives either.
type VerifiedResolution struct {
	Status TaskSetStatus
	// Mark is the verification outcome, blank when the set is not terminal.
	Mark VerifyMark
	// VerifiedAtSHA is the short SHA of the episode's PASS (empty unless Mark is
	// VerifyMarkVerified), and Drifted reports runtime HEAD having moved past it.
	VerifiedAtSHA string
	Drifted       bool
}
