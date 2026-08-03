package tasks

import (
	"sort"

	"github.com/glebglazov/pop/work"
)

// The Task-set kind's half of the Work row derivation. These read this kind's
// status vocabulary — the Done-inclusion filter, the ADR-0121 membership tiers,
// status bands and intra-project status order, and the STATUS-cell composition —
// so they live with the kind that owns those statuses rather than in the `work`
// seam, which names no kind's statuses. The Work-kind adapter in tasks/setkind
// orders its containers through WorkRowLess, so the seam's ordering and the
// legacy comparator are one comparator, not two.

// ShowRow is the shared Done-inclusion row filter (ADR-0121). Every read surface
// hides DONE sets by default and reveals them under `--include-done`; nothing
// else is filtered here.
func ShowRow(row Row, includeDone bool) bool {
	return includeDone || row.Status != StatusDone
}

// Membership tiers float rows above the whole status scheme (ADR-0121). A row
// takes the first tier it qualifies for, so an orphaned + auto-drain set sorts
// under the auto-drain tier (auto-drain is checked before orphaned).
const (
	tierRunning   = iota // live drain holds the checkout (Picked-up)
	tierAutoDrain        // auto-drain enabled
	tierOrphaned         // Worktree binding points at a missing checkout
	tierRest             // everything else
)

// sortTier returns a row's membership tier (see the tier* constants). The order
// of these checks encodes the precedence: a row that is both orphaned and
// auto-drain qualifies for the auto-drain tier first.
func sortTier(r work.Row) int {
	switch {
	case r.LiveDrain:
		return tierRunning
	case r.AutoDrain:
		return tierAutoDrain
	case r.Orphaned:
		return tierOrphaned
	default:
		return tierRest
	}
}

// Queue surface status bands (ADR-0121). A row's band is keyed on its DISPLAYED
// label, not its raw status: an IN PROGRESS row (a started or live-drained READY
// set) sorts in the IN PROGRESS band even though its raw status is READY. The
// IN PROGRESS and READY bands float running/ready work across projects; every
// other status reads per-project in bandRest.
const (
	bandInProgress = iota // displayed label "IN PROGRESS"
	bandReady             // displayed label "READY"
	bandRest              // every other displayed status
)

// statusBand returns a row's status band, keyed on its displayed label so the
// READY→IN PROGRESS refinement (WorkRowStatusLabel) lands in the IN PROGRESS band
// rather than the READY band.
func statusBand(r work.Row) int {
	switch WorkRowStatusLabel(r) {
	case "IN PROGRESS":
		return bandInProgress
	case string(StatusReady):
		return bandReady
	default:
		return bandRest
	}
}

// statusOrder is the explicit intra-project ordering for the bandRest band
// (ADR-0121): the "needs-you" statuses first, then the problem bucket, then the
// shelved/terminal statuses, then the structural defects. MISSING and MALFORMED
// share the last rank.
func statusOrder(s TaskSetStatus) int {
	switch s {
	case StatusAwaitingApproval:
		return 0
	case StatusNeedsVerify:
		return 1
	case StatusVerifyFailed:
		return 2
	case StatusFailed:
		return 3
	case StatusBlocked:
		return 4
	case StatusDeferred:
		return 5
	case StatusDone:
		return 6
	case StatusMissing, StatusMalformed:
		return 7
	default:
		return 8
	}
}

// WorkRowLess is the shared Queue surface comparator (ADR-0121), the single
// source of the total order both `pop work dashboard` and `pop queue status`
// read. Rows float by membership tier (live-drain → auto-drain → orphaned), then
// fall through to the status scheme: the IN PROGRESS and READY bands read
// cross-project (Project asc, then SetID desc), and every remaining status reads
// per-project (Project asc, then the explicit status order, then SetID desc).
// Bands key on the displayed label, so a started or live-drained READY set sorts
// as IN PROGRESS even though its raw status is READY. The membership tiers float
// above the whole status scheme — an auto-drain BLOCKED set outranks a plain
// IN PROGRESS set — and fall through to the same band/status/SetID tiebreak
// within a tier.
func WorkRowLess(a, b work.Row) bool {
	if ta, tb := sortTier(a), sortTier(b); ta != tb {
		return ta < tb
	}
	ba, bb := statusBand(a), statusBand(b)
	if ba != bb {
		return ba < bb
	}
	if a.Project != b.Project {
		return a.Project < b.Project
	}
	// The explicit status order breaks ties only within bandRest; the IN PROGRESS
	// and READY bands are single-status, so they go straight to the SetID tiebreak
	// after project name.
	if ba == bandRest {
		if ra, rb := statusOrder(a.RawStatus), statusOrder(b.RawStatus); ra != rb {
			return ra < rb
		}
	}
	return a.SetID > b.SetID
}

// SortWorkRows applies the shared Queue surface order (WorkRowLess) to a set of
// Work rows.
func SortWorkRows(rows []work.Row) {
	sort.SliceStable(rows, func(i, j int) bool {
		return WorkRowLess(rows[i], rows[j])
	})
}

// WorkRowStatusLabel reproduces StatusLabel from a Work row's live fields,
// extending the READY refinement with the live-drain trigger (ADR-0111): a READY
// set shows "IN PROGRESS" when it is started (≥1 done) OR held by a live drain;
// every other row shows its raw status. The refinement is READY-only — a live
// drain coinciding with a non-READY status leaves that label untouched (needs-you
// outranks liveness). It reads RawStatus/Started/LiveDrain so the label is
// recomposed on each render pass rather than baked in at row-build time.
func WorkRowStatusLabel(row work.Row) string {
	if row.RawStatus == StatusReady && (row.Started || row.LiveDrain) {
		return "IN PROGRESS"
	}
	return StatusLabel(Row{Status: row.RawStatus, Started: row.Started})
}

// WorkRowStatusCell composes a Task-set row's STATUS cell from its live fields —
// the single source of truth every render path and the header count read
// (ADR-0108). It returns the plain, un-styled text: the display label followed by
// the verified-at, auto-drain, orphaned, parked, and config-error suffixes in that
// fixed order. Column width-fitting measures this plain form, so no ANSI leaks
// into column math; the queue-side styled wrapper layers styling for the rendered
// output.
func WorkRowStatusCell(row work.Row) string {
	label := WorkRowStatusLabel(row)
	badge := DeriveVerifiedAtBadge(Row{
		Status:            row.RawStatus,
		VerifiedAtSHA:     row.VerifiedAtSHA,
		VerifiedAtDrifted: row.VerifiedAtDrifted,
	})
	if text := VerifiedAtBadgeText(badge); text != "" {
		label += " · " + text
	}
	if work.AutoDrainWaiting(row) {
		label += " · auto-drain"
	}
	if row.Orphaned {
		label += " · orphaned"
	}
	// Parked and config-error relocated off the DRAIN string onto the STATUS cell
	// (ADR-0111). Both are uncoloured plain text; they trail the auto-drain/
	// orphaned suffixes in a fixed order.
	if row.Parked {
		label += " · parked"
	}
	if row.ConfigError != "" {
		label += " · config error: " + row.ConfigError
	}
	return label
}
