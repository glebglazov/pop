package tasks

import (
	"sort"

	"github.com/glebglazov/pop/config"
	"github.com/glebglazov/pop/work"
)

// The Task-set kind's half of the Work row derivation. These read this kind's
// status vocabulary — the Work view preset filter (ADR-0197), the ADR-0121
// membership tiers, status bands and intra-project status order, and the
// STATUS-cell composition — so they live with the kind that owns those statuses
// rather than in the `work` seam, which names no kind's statuses. The Work-kind
// adapter in tasks/setkind orders its containers through WorkRowLess, so the
// seam's ordering and the legacy comparator are one comparator, not two.

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
func sortTier(r work.Container) int {
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
func statusBand(r work.Container) int {
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

// WorkRowLess is the shared Queue surface comparator (ADR-0121 / ADR-0197), the
// single source of the total order both `pop work dashboard` and `pop work
// status` read. Rows float by membership tier (live-drain → auto-drain →
// orphaned) under every preset. Below the tiers, an empty sortMode keeps the
// ADR-0121 status scheme; a preset's created_desc / created_asc replaces that
// scheme only (ADR-0197 decision 6).
func WorkRowLess(a, b work.Container, sortMode string) bool {
	if ta, tb := sortTier(a), sortTier(b); ta != tb {
		return ta < tb
	}
	switch sortMode {
	case config.PresetSortCreatedDesc, config.PresetSortCreatedAsc:
		return createdSortLess(a, b, sortMode)
	default:
		return statusSchemeLess(a, b)
	}
}

// statusSchemeLess is the ADR-0121 status scheme under the membership tiers:
// IN PROGRESS and READY bands read cross-project (Project asc, then ID desc);
// every remaining status reads per-project (Project asc, then the explicit
// status order, then ID desc). Bands key on the displayed label, so a started
// or live-drained READY set sorts as IN PROGRESS even though its raw status is
// READY.
func statusSchemeLess(a, b work.Container) bool {
	ba, bb := statusBand(a), statusBand(b)
	if ba != bb {
		return ba < bb
	}
	if a.Project != b.Project {
		return a.Project < b.Project
	}
	// The explicit status order breaks ties only within bandRest; the IN PROGRESS
	// and READY bands are single-status, so they go straight to the ID tiebreak
	// after project name.
	if ba == bandRest {
		if ra, rb := statusOrder(a.RawStatus), statusOrder(b.RawStatus); ra != rb {
			return ra < rb
		}
	}
	return a.ID > b.ID
}

// createdSortLess orders by identifier date prefix under the membership tiers.
// Ids with no parseable date always sort after every dated id (both directions),
// then by ID descending among themselves — a defined position rather than an
// arbitrary one (ADR-0197). Equal dates break on ID descending.
func createdSortLess(a, b work.Container, sortMode string) bool {
	ta, oka := IDCreatedAt(a.ID)
	tb, okb := IDCreatedAt(b.ID)
	switch {
	case !oka && !okb:
		return a.ID > b.ID
	case !oka:
		return false
	case !okb:
		return true
	}
	if !ta.Equal(tb) {
		if sortMode == config.PresetSortCreatedAsc {
			return ta.Before(tb)
		}
		return ta.After(tb)
	}
	return a.ID > b.ID
}

// SortWorkRows applies the shared Queue surface order (WorkRowLess) to a set of
// Work rows. sortMode is the active preset's sort (empty = status scheme).
func SortWorkRows(rows []work.Container, sortMode string) {
	sort.SliceStable(rows, func(i, j int) bool {
		return WorkRowLess(rows[i], rows[j], sortMode)
	})
}

// WorkRowStatusLabel reproduces StatusLabel from a Work row's live fields,
// extending the READY refinement with the live-drain trigger (ADR-0111): a READY
// set shows "IN PROGRESS" when it is started (≥1 done) OR held by a live drain;
// every other row shows its raw status. The refinement is READY-only — a live
// drain coinciding with a non-READY status leaves that label untouched (needs-you
// outranks liveness). It reads RawStatus/Started/LiveDrain so the label is
// recomposed on each render pass rather than baked in at row-build time.
func WorkRowStatusLabel(row work.Container) string {
	if row.RawStatus == StatusReady && (row.Started || row.LiveDrain) {
		return "IN PROGRESS"
	}
	return StatusLabel(Row{Status: row.RawStatus, Started: row.Started})
}

// WorkRowStatusCell composes a Task-set row's STATUS cell from its live fields —
// the single source of truth every render path and the header count read
// (ADR-0108). It returns the plain, un-styled text: the display label followed by
// the verified-at, unfolded, auto-drain, orphaned, parked, and config-error
// suffixes in that fixed order. Column width-fitting measures this plain form, so
// no ANSI leaks into column math; the queue-side styled wrapper layers styling for
// the rendered output.
func WorkRowStatusCell(row work.Container) string {
	return work.StatusCellText(WorkRowStatusSegments(row))
}

// WorkRowStatusSegments is that same composition as tone-tagged tokens — the
// form a styled surface needs, since it must paint the label by status bucket
// and the verified-at badge by verdict age while leaving every other suffix
// plain. Plain and styled therefore differ only by ANSI: both walk this one
// sequence.
func WorkRowStatusSegments(row work.Container) []work.StatusSegment {
	segments := []work.StatusSegment{{Text: WorkRowStatusLabel(row), Tone: work.ToneLabel}}
	badge := VerifiedAtBadgeFor(row)
	if text := VerifiedAtBadgeText(badge); text != "" {
		segments = append(segments, work.StatusSegment{Text: text, Tone: verifiedAtTone(badge.State)})
	}
	// Unfolded rides beside the status the way the Verification mark does
	// (ADR-0197): derived from Bound plus FoldEligibleStatus, never persisted.
	if Unfolded(row.Bound, row.RawStatus) {
		segments = append(segments, work.StatusSegment{Text: UnfoldedMark, Tone: work.TonePlain})
	}
	if work.AutoDrainWaiting(row) {
		segments = append(segments, work.StatusSegment{Text: "auto-drain", Tone: work.TonePlain})
	}
	if row.Orphaned {
		segments = append(segments, work.StatusSegment{Text: "orphaned", Tone: work.TonePlain})
	}
	// Parked and config-error relocated off the DRAIN string onto the STATUS cell
	// (ADR-0111). Both are uncoloured plain text; they trail the auto-drain/
	// orphaned suffixes in a fixed order.
	if row.Parked {
		segments = append(segments, work.StatusSegment{Text: "parked", Tone: work.TonePlain})
	}
	if row.ConfigError != "" {
		segments = append(segments, work.StatusSegment{Text: "config error: " + row.ConfigError, Tone: work.TonePlain})
	}
	// Last, and only ever present with the show-archived view on (ADR-0186): it is
	// what tells the operator this row is in the filing cabinet rather than the
	// table, which is the difference between pressing archive and unarchive.
	if row.Archived {
		segments = append(segments, work.StatusSegment{Text: "archived", Tone: work.TonePlain})
	}
	return segments
}

// verifiedAtTone maps the verified-at states onto the seam's attention tones
// (ADR-0156): a PASS at HEAD is good, a drifted one warns, an unverified or
// verify-failed terminal set is bad.
func verifiedAtTone(state VerifiedAtBadgeState) work.StatusTone {
	switch state {
	case VerifiedAtAtHead:
		return work.ToneGood
	case VerifiedAtDrifted:
		return work.ToneWarn
	case VerifiedAtUnverified, VerifiedAtFailed:
		return work.ToneBad
	default:
		return work.TonePlain
	}
}
