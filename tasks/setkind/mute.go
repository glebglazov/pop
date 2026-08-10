package setkind

import (
	"fmt"
	"strings"
	"time"

	"github.com/glebglazov/pop/store"
	"github.com/glebglazov/pop/tasks"
	"github.com/glebglazov/pop/work"
)

// The Task-set kind's half of the Mute seam (ADR-0200). The surface derives the
// window and hands over the instant; everything a mute *does* to a Task set is
// here.

// Mute records the human's window on the set and clears its Auto-drain consent
// — mute's whole reach into supervision, made as a write at mute time rather
// than as a gate the daemon consults (decision 2).
//
// It never reaches a running process. Like Archive it can neither cancel a
// Drain, retire a Checkout gate hold nor answer an open gate prompt; unlike
// Archive it does not refuse on live occupancy, because "no time for this right
// now" is a coherent thing to say about a set that is mid-drain, and refusing
// would disable mute in the exact moment a noisy set is what the human wants out
// of their view. So it succeeds and reports what is still live there instead.
func (k *Kind) Mute(c work.Container, until time.Time, secret bool) (work.Outcome, error) {
	if c.DefPath == "" {
		return work.Outcome{}, fmt.Errorf("setkind: %s carries no definition path to mute against", c.ID)
	}
	if err := tasks.MuteTaskSet(k.d.Tasks, c.DefPath, c.ID, until, secret); err != nil {
		return work.Outcome{}, err
	}
	message := fmt.Sprintf("muted %s — %s", c.ID, muteWindowPhrase(until, secret))
	if live := k.liveOccupancy(c); live != "" {
		message += ", " + live
	}
	return work.Outcome{Kind: work.OutcomeRefresh, Message: message}, nil
}

// Unmute clears the set's mute. It does not restore the Auto-drain bit and does
// not mention that one was cleared: the human turns standing consent back on
// themselves, whenever they mean it, muted or not.
func (k *Kind) Unmute(c work.Container) (work.Outcome, error) {
	if err := tasks.UnmuteTaskSet(k.d.Tasks, c.ID); err != nil {
		return work.Outcome{}, err
	}
	return work.Outcome{Kind: work.OutcomeRefresh, Message: "unmuted " + c.ID}, nil
}

// muteWindowPhrase words the mute the way the row will read afterwards, with the
// random window's instant withheld: the message is a read surface too, and a
// secret that survives the status cell but not the confirmation line is no
// secret (decision 6).
func muteWindowPhrase(until time.Time, secret bool) string {
	return work.MuteSuffix(work.Container{MutedUntil: until, MuteSecret: secret})
}

// liveOccupancy names what still holds the set's checkout, so a human who mutes
// a busy set learns that muting did not stop it. Empty when nothing live claims
// the checkout — and empty, too, when the claim cannot be read: a mute that
// already succeeded must not report an error about something it never touched.
func (k *Kind) liveOccupancy(c work.Container) string {
	path := strings.TrimSpace(c.RuntimePath)
	if path == "" {
		path = strings.TrimSpace(c.Checkout)
	}
	claim, err := tasks.ReadCheckoutClaim(k.d.Tasks, path)
	if err != nil || claim == nil {
		return ""
	}
	return liveClaimPhrase(*claim)
}

// liveClaimPhrase renders one live Checkout claim as the trailing half of the
// mute's report: what is running, and the pid that would have to be dealt with
// to stop it.
func liveClaimPhrase(claim store.CheckoutClaim) string {
	phrase := claim.Reason.Phrase() + " still live"
	if claim.PID > 0 {
		phrase += fmt.Sprintf(", pid %d", claim.PID)
	}
	return phrase
}
