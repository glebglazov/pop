package work

import (
	"github.com/glebglazov/pop/work/ref"
)

// Pane work attribution: which Work container the pane a read surface was
// launched in belongs to. Pop knows a great deal about which pane it opened for
// which work and never used to ask the question — every other derivation runs
// the other way, from a known container to its pane (ADR-0201).

// PaneFacts is what the launching pane can show about itself. It is plain data,
// read once at launch and carried, because the ladder over it runs kind-side
// during a snapshot build where no kind may reach back to tmux.
//
// The tag fields are pop's own vocabulary — the activity a pane was opened for —
// not tmux's: which option key each one is spelled with stays in `internal/tmux`.
type PaneFacts struct {
	// PaneID, Session and Directory are the pane's own coordinates.
	PaneID, Session, Directory string
	// Set, Verify, Fold and Assist carry the container id of the activity pop
	// opened the pane for. The first three are a Task set's; Assist is either
	// kind's, because a Map's assist pane is tagged the same way, keyed by the
	// container id either way.
	Set, Verify, Fold, Assist string
	// Ticket is the Decision ticket a Grilling pane is deciding.
	Ticket string
	// Routine is the routine a fire pane is running.
	Routine string
	// WorkKind and WorkID are the Work stamp on the pane's session: a kind's wire
	// name and the container id it hosts. Only a Map stamps one today.
	WorkKind, WorkID string
}

// Empty reports facts read from no pane at all — the answer outside tmux, where
// attribution is silent rather than mistaken.
func (f PaneFacts) Empty() bool { return f == PaneFacts{} }

// Attribution names the Work container a pane belongs to.
type Attribution struct {
	// Ref is the container's identity, the same one every other Work surface
	// names it by.
	Ref ref.WorkRef
	// CursorKey is the row handle a TUI places its cursor with. Row order is not
	// stable across rebuilds, so the key is the only valid handle — an index is
	// not.
	CursorKey string
	// Label is the kind's own phrase for the container ("task set 04-foo"), for
	// the one line a surface prints when the row it names cannot be shown.
	Label string
	// Note is what the surface must say even when the cursor lands: the kind had
	// more than one candidate and chose this one, so the choice is named along
	// with how many there were and why. Empty for an unambiguous attribution,
	// which is the silent case — placing a cursor is not an action, but a
	// plausible near miss that says nothing looks like a bug (ADR-0201 decision
	// 2).
	Note string
}

// PaneAttributor is the optional seam a kind implements when a pane may belong to
// one of its containers. It is obtained by type assertion the way Advancer is, so
// a kind pop never opens a pane for answers nothing and is never asked again.
//
// The builder calls it after that kind's own Load, which is the point of asking
// during a snapshot build: a kind answers from rows it is already holding,
// including the rows its view preset dropped. Without those, a pane belonging to
// a hidden container would be indistinguishable from a pane belonging to nothing,
// and the surface could not say why the cursor did not move (ADR-0201 decision 6).
type PaneAttributor interface {
	AttributePane(PaneFacts) (Attribution, bool)
}

// PaneNeighbourhoodAttributor is the weak half of the same seam: a kind answers it
// when a pane pop never opened may still *sit* somewhere its work lives — a
// directory work is running or bound in. It is a second method rather than more
// rungs inside AttributePane because the two halves mean different things, and the
// ladder interleaves kinds across the boundary: a session stamped for a Map beats
// any kind's mere locality, however deep the checkout the pane is standing in
// (ADR-0201 decision 1).
//
// A kind answering here owes the surface a Note whenever it had to choose between
// candidates.
type PaneNeighbourhoodAttributor interface {
	AttributePaneNeighbourhood(PaneFacts) (Attribution, bool)
}

// AttributePane walks the wired kinds and returns the first attribution one of
// them makes, or nil when the pane belongs to nothing here.
//
// It walks them twice, which is the ladder (ADR-0201 decision 1). The first pass
// asks every kind what pane pop opened this is: a pane pop opened for a Task set
// is that set's whatever session it sits in, so the Task-set kind is asked before
// the kind that has only a session stamp to go on. Only once no kind recognises
// the pane as its own does the second pass ask where the pane is standing, in the
// same kind order. A kind not wired onto this page is asked in neither pass, which
// is how a Routine pane leaves a page-A cursor alone.
func AttributePane(kinds []Kind, facts PaneFacts) *Attribution {
	if facts.Empty() {
		return nil
	}
	ordered := kindsInPrecedence(kinds)
	for _, k := range ordered {
		if attributor, ok := k.(PaneAttributor); ok {
			if att, hit := attributor.AttributePane(facts); hit {
				return &att
			}
		}
	}
	for _, k := range ordered {
		if attributor, ok := k.(PaneNeighbourhoodAttributor); ok {
			if att, hit := attributor.AttributePaneNeighbourhood(facts); hit {
				return &att
			}
		}
	}
	return nil
}
