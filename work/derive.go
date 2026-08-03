package work

// The derivation that survives here is the part that reads no status vocabulary:
// the auto-drain marker predicate, the live indicator, and the destination-column
// label. The Done-inclusion filter, the ADR-0121 tiers/bands/status order and the
// STATUS-cell composition all moved kind-side with the Task-set adapter — they
// key on that kind's statuses, and `work` no longer names them.

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
