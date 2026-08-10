package work

import "time"

// Mute: a human's timed "not now" on one Work container (ADR-0200). The window
// is a date the human picks off a surface-owned list, so what crosses the seam
// is an instant and one bit saying whether that instant is secret — never a
// duration and never a menu entry. Two things live here: the optional seam a
// mutable kind implements, and the one rendering of a mute every read surface
// shows, so the secret cannot leak by one surface formatting it differently.

// Muter is the seam a kind implements when its containers can be muted. Like
// Advancer it is deliberately not part of Kind and is obtained by type
// assertion: two of the three kinds have no mute, and a Kind.Mutable() member
// would make every kind answer a question most of them answer by silence.
//
// It exists because neither half of the gesture fits the verb seam. A kind
// cannot ask the surface to open a modal, so the window list is the surface's;
// and a verb id carries no payload, so the chosen instant has nowhere to ride on
// Perform. Widening Outcome for one feature was the alternative and was
// rejected.
type Muter interface {
	// Mute records the container's resurfacing instant, secret marking the random
	// default window. It succeeds on a busy container — muting is scheduling, and
	// "no time for this right now" is a coherent thing to say about a set that is
	// mid-drain — and reports what is still live there rather than refusing.
	Mute(c Container, until time.Time, secret bool) (Outcome, error)
	// Unmute clears the mute and nothing else. It does not restore the Auto-drain
	// consent muting destroyed: that bit is an explicit human instruction, and a
	// view gesture has no business handing it back unasked.
	Unmute(c Container) (Outcome, error)
}

// muteSecretMark stands in for a random window's instant. It discloses that a
// secret exists rather than concealing that there is one — a row that simply
// said `muted` would leave the human wondering whether pop had lost the date.
const muteSecretMark = "[?]"

// muteDateFormat is the day-and-date form a dated mute reads back as, the same
// words the submenu offered plus the hour in full. The hour is read off the
// instant rather than written as a constant here: it is invariant because of how
// the windows are derived, and a surface that asserted it would keep saying
// 09:00 the day that derivation changed.
const muteDateFormat = "Mon 2 Jan, 15:04 UTC"

// MuteSuffix is a muted container's phrase for a status cell: `unmuted on Fri 14
// Aug, 09:00 UTC`, or `unmuted on [?]` for the random window. It is empty when
// nothing mutes the container, so a caller appends it unconditionally.
//
// Every read surface goes through this one function — the status cell, the
// detail view and `pop work status` alike — because the secret binds them all
// equally, and a second formatter is how a secret leaks.
func MuteSuffix(c Container) string {
	if c.MutedUntil.IsZero() {
		return ""
	}
	if c.MuteSecret {
		return "unmuted on " + muteSecretMark
	}
	return "unmuted on " + c.MutedUntil.UTC().Format(muteDateFormat)
}
