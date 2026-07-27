package work

import (
	"fmt"
	"sort"

	"github.com/glebglazov/pop/tasks"
)

// ShowRow is the shared Done-inclusion row filter (ADR-0121). Every read surface
// hides DONE sets by default and reveals them under `--include-done`; nothing
// else is filtered here.
func ShowRow(row tasks.Row, includeDone bool) bool {
	return includeDone || row.Status != tasks.StatusDone
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
func sortTier(r Row) int {
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
// READY→IN PROGRESS refinement (StatusLabel) lands in the IN PROGRESS band
// rather than the READY band.
func statusBand(r Row) int {
	switch StatusLabel(r) {
	case "IN PROGRESS":
		return bandInProgress
	case string(tasks.StatusReady):
		return bandReady
	default:
		return bandRest
	}
}

// statusOrder is the explicit intra-project ordering for the bandRest band
// (ADR-0121): the "needs-you" statuses first, then the problem bucket, then the
// shelved/terminal statuses, then the structural defects. MISSING and MALFORMED
// share the last rank.
func statusOrder(s tasks.TaskSetStatus) int {
	switch s {
	case tasks.StatusAwaitingApproval:
		return 0
	case tasks.StatusNeedsVerify:
		return 1
	case tasks.StatusVerifyFailed:
		return 2
	case tasks.StatusFailed:
		return 3
	case tasks.StatusBlocked:
		return 4
	case tasks.StatusDeferred:
		return 5
	case tasks.StatusDone:
		return 6
	case tasks.StatusMissing, tasks.StatusMalformed:
		return 7
	default:
		return 8
	}
}

// rowLess is the shared Queue surface comparator (ADR-0121), the single source
// of the total order both `pop work dashboard` and `pop queue status` read. Rows
// float by membership tier (live-drain → auto-drain → orphaned), then fall
// through to the status scheme: the IN PROGRESS and READY bands read
// cross-project (Project asc, then SetID desc), and every remaining status reads
// per-project (Project asc, then the explicit status order, then SetID desc).
// Bands key on the displayed label, so a started or live-drained READY set sorts
// as IN PROGRESS even though its raw status is READY. The membership tiers float
// above the whole status scheme — an auto-drain BLOCKED set outranks a plain
// IN PROGRESS set — and fall through to the same band/status/SetID tiebreak
// within a tier.
func rowLess(a, b Row) bool {
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

// SortRows applies the shared Queue surface order (rowLess) to a dashboard
// build's rows.
func SortRows(rows []Row) {
	sort.SliceStable(rows, func(i, j int) bool {
		return rowLess(rows[i], rows[j])
	})
}

// StatusLabel reproduces tasks.StatusLabel from a dashboard row's live fields,
// extending the READY refinement with the live-drain trigger (ADR-0111): a READY
// set shows "IN PROGRESS" when it is started (≥1 done) OR held by a live drain;
// every other row shows its raw status. The refinement is READY-only — a live
// drain coinciding with a non-READY status leaves that label untouched (needs-you
// outranks liveness). It reads RawStatus/Started/LiveDrain so the label is
// recomposed on each render pass rather than baked in at row-build time. Map rows
// always show "WAYFINDING" (ADR-0130).
func StatusLabel(row Row) string {
	if row.IsMap {
		return "WAYFINDING"
	}
	if row.RawStatus == tasks.StatusReady && (row.Started || row.LiveDrain) {
		return "IN PROGRESS"
	}
	return tasks.StatusLabel(tasks.Row{Status: row.RawStatus, Started: row.Started})
}

// StatusCell composes a row's STATUS cell from its live fields — the single
// source of truth every render path and the header count read (ADR-0108). It
// returns the plain, un-styled text: the display label followed by the
// verified-at, auto-drain, orphaned, parked, and config-error suffixes in that
// fixed order. Column width-fitting measures this plain form, so no ANSI leaks
// into column math; the queue-side styled wrapper layers styling for the
// rendered output. Map rows render `WAYFINDING · N open / M frontier` and skip
// the set-only suffixes (ADR-0130).
func StatusCell(row Row) string {
	if row.IsMap {
		return fmt.Sprintf("WAYFINDING · %d open / %d frontier", row.MapOpen, row.MapFrontier)
	}
	label := StatusLabel(row)
	if row.VerifiedAtSHA != "" {
		label += " · verified @ " + row.VerifiedAtSHA
	}
	if AutoDrainWaiting(row) {
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

// AutoDrainWaiting reports whether a set's auto-drain consent should surface as
// "waiting to be picked up" — the single predicate the per-row marker and the
// header tally both read (ADR-0108). A consented set counts and shows the marker
// only while it is not Picked-up; once a live drain holds the checkout
// (row.LiveDrain) the IN-PROGRESS refinement already signals the activity, so the
// marker is silenced and the set drops out of the "still needs picking up" count.
// The persisted consent bit is untouched — this is display-only.
func AutoDrainWaiting(row Row) bool {
	return row.AutoDrain && !row.LiveDrain
}

// LiveDrainGlyph is the stable, single-frame stand-in for the live-drain
// indicator, used for column-width math: a spinner frame whose display width
// matches every animated frame so the layout never shifts as the spinner advances
// (ADR-0111). It mirrors ui.SpinnerFrames[0]; the animated, coloured frame is a
// queue-side render concern (work imports no lipgloss/ui).
const LiveDrainGlyph = "⠋"

// LiveIndicator returns the plain (un-styled) trailing indicator cell: the
// fixed-width live-drain glyph when a live drain holds the checkout, blank
// otherwise. Returning the fixed-width stand-in keeps ANSI out of column math and
// the measured width constant across frames; the animated, coloured variant is a
// queue-side wrapper.
func LiveIndicator(row Row) string {
	if !row.LiveDrain {
		return ""
	}
	return LiveDrainGlyph
}

// WorktreeLabel returns the plain WORKTREE destination cell (ADR-0070/0072): a
// bound set shows its branch plainly; an unbound set with a managed directive
// shows `[managed wt]`; an unbound set with no directive shows `needs bind`; a
// Done set still holding a managed binding shows `[managed wt <branch>]`. It is
// the un-styled composition; the coloured wrapper stays queue-side.
func WorktreeLabel(kind DestKind, label string) string {
	switch kind {
	case DestManagedDirective:
		return DestLabelManagedWt
	case DestNeedsBind:
		return DestLabelNeedsBind
	case DestDoneManagedBound:
		return "[managed wt " + label + "]"
	default:
		return label
	}
}
