package work

import "strings"

// A container's STATUS cell is composed by the kind that owns it — the fields
// behind its suffixes are that kind's own, so nothing else could compose it —
// and consumed by surfaces that must both measure it (plain) and paint it
// (styled). Segments are how one composition serves both: the kind says what
// each token means, the surface decides what that looks like, and ADR-0143's
// boundary holds because no colour is ever named here.

// StatusTone is the semantic weight of one STATUS-cell segment. It names what
// the token means, never how it is painted: a plain renderer ignores it, and a
// TUI maps it to a style.
type StatusTone int

const (
	// ToneLabel marks the kind's own status label — the token a TUI colours by
	// its status bucket.
	ToneLabel StatusTone = iota
	// TonePlain marks an uncoloured suffix.
	TonePlain
	// ToneGood, ToneWarn and ToneBad are the three attention levels a kind-local
	// badge can carry. The Task-set verified-at badge (ADR-0156) is the only one
	// today: at HEAD, drifted, unverified.
	ToneGood
	ToneWarn
	ToneBad
)

// StatusSegment is one token of a container's composed STATUS cell.
type StatusSegment struct {
	Text string
	Tone StatusTone
}

// StatusCellText joins segments into the plain, un-styled cell — the form column
// width math measures, so it must never carry ANSI. Empty segments are dropped
// rather than rendered as an empty token between two separators.
func StatusCellText(segments []StatusSegment) string {
	parts := make([]string, 0, len(segments))
	for _, s := range segments {
		if s.Text == "" {
			continue
		}
		parts = append(parts, s.Text)
	}
	return strings.Join(parts, " · ")
}
