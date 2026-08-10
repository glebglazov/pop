package wayfinder

import (
	"fmt"
	"time"

	"github.com/glebglazov/pop/work"
)

// The Map kind's half of the Mute seam (ADR-0200 decision 7). A Map carries no
// auto-drain bit and is not driven by the daemon, so mute reaches nothing beyond
// the registry row it shares with every other kind — it is purely a view fact,
// and nothing else about a Map changes when one is written.

// Mute records the human's window on the Map. It is the same store write
// ArchiveMap makes against the same registration row, so a Map muted from the
// dashboard and one muted by any future command-line route would be the same
// act with the same refusals — an unregistered Map's write comes back as
// store.ErrWorkContainerUnregistered.
func (k *MapKind) Mute(c work.Container, until time.Time, secret bool) (work.Outcome, error) {
	s, err := openWorkRegistry(k.d.Wayfinder)
	if err != nil {
		return work.Outcome{}, err
	}
	if err := s.MuteWorkContainer(MapRef(c.ID), until, secret); err != nil {
		return work.Outcome{}, err
	}
	message := fmt.Sprintf("muted %s — %s", c.ID, work.MuteSuffix(work.Container{MutedUntil: until, MuteSecret: secret}))
	return work.Outcome{Kind: work.OutcomeRefresh, Message: message}, nil
}

// Unmute clears the Map's mute. There is no Auto-drain bit to leave alone here —
// the whole reason mute reaches nothing beyond the registry row for a Map — so
// it is a plain clear with no consequence to report.
func (k *MapKind) Unmute(c work.Container) (work.Outcome, error) {
	s, err := openWorkRegistry(k.d.Wayfinder)
	if err != nil {
		return work.Outcome{}, err
	}
	if err := s.UnmuteWorkContainer(MapRef(c.ID)); err != nil {
		return work.Outcome{}, err
	}
	return work.Outcome{Kind: work.OutcomeRefresh, Message: "unmuted " + c.ID}, nil
}

// liveMapMute is a Map's whole of how a Mute expires, mirroring the Task-set
// kind's liveMute (ADR-0200 decision 1): one comparison on the load that renders
// the row. A window that has passed answers the zero instant — the same answer a
// Map that was never muted gives — so the row simply comes back on the next
// rebuild, with nothing written, no sweeper and nothing to reconcile.
func liveMapMute(m Map, now time.Time) (time.Time, bool) {
	if m.MutedUntil.After(now) {
		return m.MutedUntil, m.MuteSecret
	}
	return time.Time{}, false
}
