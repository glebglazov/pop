package tmux

import (
	"os"
	"strings"
)

// PaneFacts is everything about the pane a command was launched in that pop can
// attribute work from: the pane's own coordinates, each @pop_* tag it carries,
// and the Work stamp on the session it sits in. Every field comes out of one
// display-message round-trip, because a caller reads this once at launch and
// carries it — a ladder that re-asked tmux per rung would fork per question
// (ADR-0201).
type PaneFacts struct {
	PaneID    string
	Session   string
	Directory string
	// Tags are the pane's @pop_* values keyed by the tag enum, omitting the tags
	// it does not carry. A pane with none is an ordinary shell.
	Tags map[PaneTag]string
	// WorkKind and WorkID are the Work stamp on the pane's session, empty when
	// the session hosts no Work container. tmux resolves a session option through
	// a pane's own format context, which is what lets the stamp ride the same
	// round-trip as the pane's own tags instead of costing a second one.
	WorkKind string
	WorkID   string
}

// Tag returns one tag's value, empty when the pane does not carry it.
func (f PaneFacts) Tag(tag PaneTag) string { return f.Tags[tag] }

// paneFactTags is every tag pop writes on a pane. All of them are read: which
// ones mean something is the consuming ladder's judgment, not this module's.
var paneFactTags = []PaneTag{TagSet, TagVerify, TagFold, TagAssist, TagTicket, TagRoutine}

// CurrentPaneFacts reads the caller's own pane in one round-trip.
//
// Outside the configured tmux server it answers zero facts and no error. That is
// not a convenience: display-message would happily answer from a running server
// anyway, reporting whichever pane some attached client is sitting in, and a
// command typed at a plain terminal belongs to no pane at all. $TMUX_PANE is the
// target when tmux exported one, so the answer is about the pane the process runs
// in rather than the one the client has focused.
func (t *realTmux) CurrentPaneFacts() (PaneFacts, error) {
	if !t.InTmux() {
		return PaneFacts{}, nil
	}
	fields := []string{"#{pane_id}", "#{session_name}", "#{pane_current_path}"}
	for _, tag := range paneFactTags {
		fields = append(fields, "#{"+tag.option()+"}")
	}
	fields = append(fields, "#{"+optWorkKind+"}", "#{"+optWorkID+"}")

	args := []string{"display-message"}
	if pane := strings.TrimSpace(os.Getenv("TMUX_PANE")); pane != "" {
		args = append(args, "-t", pane)
	}
	args = append(args, "-p", strings.Join(fields, "\t"))

	out, err := t.run.output(args...)
	if err != nil {
		if absentServer(err) {
			return PaneFacts{}, nil
		}
		return PaneFacts{}, err
	}
	return parsePaneFacts(out), nil
}

// parsePaneFacts turns the format line into typed facts. A short line — an older
// tmux that dropped a field, a pane that vanished mid-read — yields the fields it
// did carry rather than an error: attribution is an affordance, and half a pane's
// facts still attribute it correctly or not at all.
func parsePaneFacts(out string) PaneFacts {
	parts := strings.Split(strings.TrimRight(out, "\n"), "\t")
	at := func(i int) string {
		if i < len(parts) {
			return strings.TrimSpace(parts[i])
		}
		return ""
	}
	facts := PaneFacts{PaneID: at(0), Session: at(1), Directory: at(2)}
	for i, tag := range paneFactTags {
		if v := at(3 + i); v != "" {
			if facts.Tags == nil {
				facts.Tags = map[PaneTag]string{}
			}
			facts.Tags[tag] = v
		}
	}
	facts.WorkKind = at(3 + len(paneFactTags))
	facts.WorkID = at(4 + len(paneFactTags))
	return facts
}
