package tasks

import "github.com/glebglazov/pop/work"

// VerifyMark is the verification outcome for a terminal Task set, carried beside
// its status rather than inside it. Work completion and verification are two
// independent facts: whether the work is finished (the manifest, or a human's
// explicit completion) and whether a Verifier has judged it. The status answers
// the first, this mark the second — so a human's "this is done" can never be
// overwritten by the absence, or the content, of an agent's opinion.
//
// It is an alias of work.VerifyMark so a Work container can carry the field
// without `work` importing this package, exactly as TaskSetStatus is. The
// vocabulary itself is the Task-set kind's own; ResolveVerifiedStatus is the one
// place that derives it.
type VerifyMark = work.VerifyMark

const (
	// VerifyMarkNone is the absent mark: verification is disabled, the set is not
	// terminal, or it is unplaced so there is no HEAD to gate on.
	VerifyMarkNone VerifyMark = ""
	// VerifyMarkUnverified is a terminal set with no PASS verdict in its episode —
	// finished and nobody checked. On a set that reached terminal on its own this
	// is also the status (NEEDS-VERIFY); on a human-completed set it is only the
	// mark, and it is what keeps the Verifier scheduled for it.
	VerifyMarkUnverified VerifyMark = "unverified"
	// VerifyMarkVerified is a terminal set cleared by a PASS verdict, at the
	// current work SHA or at an older immunizing one (ADR-0096).
	VerifyMarkVerified VerifyMark = "verified"
	// VerifyMarkFailed is a terminal set with a non-PASS verdict at the current
	// work SHA. On a self-completed set this is also the status (VERIFY-FAILED); on
	// a human-completed one the finding is information, not a veto, so it stays a
	// mark beside DONE.
	VerifyMarkFailed VerifyMark = "verify-failed"
)
