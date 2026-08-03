package tasks

import "github.com/glebglazov/pop/work"

// VerifiedAtBadgeState is the four-state (plus absent) Verified-at SHA badge
// surfaced wherever pop shows a Task set's status (ADR-0156).
type VerifiedAtBadgeState uint8

const (
	VerifiedAtAbsent VerifiedAtBadgeState = iota
	VerifiedAtAtHead     // green: PASS at current runtime HEAD
	VerifiedAtDrifted    // yellow: PASS at an older SHA, HEAD has moved
	VerifiedAtUnverified // red: verification enabled, no PASS in the episode
	VerifiedAtFailed     // red: a non-PASS verdict at HEAD on a set the status no longer demotes
)

// VerifiedAtBadge is the derived badge for one status row. Call DeriveVerifiedAtBadge
// from renderers and prompts — one rule for colour and text.
type VerifiedAtBadge struct {
	State VerifiedAtBadgeState
	SHA   string // short SHA when State is AtHead or Drifted
}

// DeriveVerifiedAtBadge derives the Verified-at SHA badge from a set's status row.
// VerifyMark, VerifiedAtSHA and VerifiedAtDrifted are populated by
// ApplyVerifyVerdicts when Agent verification is enabled; when it is disabled
// rows carry no mark, so the badge stays absent.
//
// A row whose status *is* the verification outcome (NEEDS-VERIFY, VERIFY-FAILED)
// gets the plain red unverified badge: the status already says it, and a badge
// repeating it in different words would read as two facts. Every other terminal
// row shows the mark riding beside its status — which is where a human-completed
// set's verification outcome lives.
func DeriveVerifiedAtBadge(row Row) VerifiedAtBadge {
	switch row.Status {
	case StatusNeedsVerify, StatusVerifyFailed:
		return VerifiedAtBadge{State: VerifiedAtUnverified}
	}
	if !TerminalStatus(row.Status) {
		return VerifiedAtBadge{State: VerifiedAtAbsent}
	}
	switch row.VerifyMark {
	case VerifyMarkVerified:
		if row.VerifiedAtSHA != "" {
			state := VerifiedAtAtHead
			if row.VerifiedAtDrifted {
				state = VerifiedAtDrifted
			}
			return VerifiedAtBadge{State: state, SHA: row.VerifiedAtSHA}
		}
	case VerifyMarkUnverified:
		return VerifiedAtBadge{State: VerifiedAtUnverified}
	case VerifyMarkFailed:
		return VerifiedAtBadge{State: VerifiedAtFailed}
	}
	return VerifiedAtBadge{State: VerifiedAtAbsent}
}

// VerifiedAtBadgeFor derives the badge from a Work container — the same rule the
// status table reads, so the dashboard and `pop work status` never re-derive it
// from the container's cells themselves.
func VerifiedAtBadgeFor(c work.Container) VerifiedAtBadge {
	return DeriveVerifiedAtBadge(Row{
		Status:            c.RawStatus,
		VerifyMark:        c.VerifyMark,
		VerifiedAtSHA:     c.VerifiedAtSHA,
		VerifiedAtDrifted: c.VerifiedAtDrifted,
	})
}

// VerifiedAtBadgeText returns the plain badge label (no ANSI). Empty when absent.
func VerifiedAtBadgeText(badge VerifiedAtBadge) string {
	switch badge.State {
	case VerifiedAtAtHead, VerifiedAtDrifted:
		return "verified @ " + badge.SHA
	case VerifiedAtUnverified:
		return "unverified"
	case VerifiedAtFailed:
		return "verify-failed"
	default:
		return ""
	}
}
