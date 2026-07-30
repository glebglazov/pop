package tasks

// VerifiedAtBadgeState is the three-state (plus absent) Verified-at SHA badge
// surfaced wherever pop shows a Task set's status (ADR-0156).
type VerifiedAtBadgeState uint8

const (
	VerifiedAtAbsent VerifiedAtBadgeState = iota
	VerifiedAtAtHead     // green: PASS at current runtime HEAD
	VerifiedAtDrifted    // yellow: PASS at an older SHA, HEAD has moved
	VerifiedAtUnverified // red: verification enabled, no PASS in the episode
)

// VerifiedAtBadge is the derived badge for one status row. Call DeriveVerifiedAtBadge
// from renderers and prompts — one rule for colour and text.
type VerifiedAtBadge struct {
	State VerifiedAtBadgeState
	SHA   string // short SHA when State is AtHead or Drifted
}

// DeriveVerifiedAtBadge derives the Verified-at SHA badge from a set's status row.
// VerifiedAtSHA and VerifiedAtDrifted are populated by ApplyVerifyVerdicts when
// Agent verification is enabled; NEEDS-VERIFY and VERIFY-FAILED rows surface red
// unverified from status alone. When verification is disabled rows never carry
// verify-gated statuses, so the badge stays absent.
func DeriveVerifiedAtBadge(row Row) VerifiedAtBadge {
	switch row.Status {
	case StatusNeedsVerify, StatusVerifyFailed:
		return VerifiedAtBadge{State: VerifiedAtUnverified}
	case StatusDone, StatusAwaitingApproval:
		if row.VerifiedAtSHA != "" {
			state := VerifiedAtAtHead
			if row.VerifiedAtDrifted {
				state = VerifiedAtDrifted
			}
			return VerifiedAtBadge{State: state, SHA: row.VerifiedAtSHA}
		}
	}
	return VerifiedAtBadge{State: VerifiedAtAbsent}
}

// VerifiedAtBadgeText returns the plain badge label (no ANSI). Empty when absent.
func VerifiedAtBadgeText(badge VerifiedAtBadge) string {
	switch badge.State {
	case VerifiedAtAtHead, VerifiedAtDrifted:
		return "verified @ " + badge.SHA
	case VerifiedAtUnverified:
		return "unverified"
	default:
		return ""
	}
}
