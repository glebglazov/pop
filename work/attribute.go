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
	// RepoCommonDir is the git common directory of the repository the pane is
	// standing in, empty outside one. It is the repository's identity everywhere
	// in pop — the key Task storage is filed under — and it is true of a sibling
	// worktree by construction, which is what lets the weakest pass answer a
	// shell no binding mentions (ADR-0241 decision 4).
	//
	// It is git-derived rather than tmux-derived, which is why it is here and not
	// on internal/tmux's own facts, and it is resolved once at launch: the pane's
	// directory is read once and never re-read, so the answer cannot change while
	// the dashboard rebuilds over it (decisions 5 and 6).
	RepoCommonDir string
}

// Empty reports facts read from no pane at all — the answer outside tmux, where
// attribution is silent rather than mistaken.
func (f PaneFacts) Empty() bool { return f == PaneFacts{} }

// Attribution names the Work containers a pane belongs to.
//
// It is a list, not one container, because a pane standing in a checkout several
// Task sets are bound to belongs to all of them: the surface lifts every one
// rather than choosing (ADR-0209 decision 2). Nothing is chosen, so nothing is
// explained — an attribution carries no message.
type Attribution struct {
	// Containers is every container the pane is attributed to, most likely first.
	// A kind that had to rank candidates ranks them here; the leader is the one a
	// surface reaches for when it can only use one.
	Containers []AttributedContainer
}

// AttributedContainer is one container a pane is attributed to.
type AttributedContainer struct {
	// Ref is the container's identity, the same one every other Work surface
	// names it by.
	Ref ref.WorkRef
	// CursorKey is the row handle a TUI addresses the row by. Row order is not
	// stable across rebuilds, so the key is the only valid handle — an index is
	// not.
	CursorKey string
	// Label is the kind's own phrase for the container ("task set 04-foo"), for
	// the one line a surface prints when the row it names cannot be shown.
	Label string
}

// AttributeOne is the answer of a rung that names exactly one container, which is
// every rung above the bound-checkout one.
func AttributeOne(c AttributedContainer) Attribution {
	return Attribution{Containers: []AttributedContainer{c}}
}

// Leading is the container a surface uses when it can only use one: the head of
// the ranking the answering kind gave.
func (a Attribution) Leading() (AttributedContainer, bool) {
	if len(a.Containers) == 0 {
		return AttributedContainer{}, false
	}
	return a.Containers[0], true
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
// A kind answering here answers with every candidate it has, ranked: locality is
// the rung where one directory can mean several containers, and none of them is
// wrong (ADR-0209 decision 2).
type PaneNeighbourhoodAttributor interface {
	AttributePaneNeighbourhood(PaneFacts) (Attribution, bool)
}

// PaneRepositoryAttributor is the weakest half of the seam, and the only one whose
// question is not about a checkout: a kind answers it with every container of its
// own that lives in the repository the pane is standing in, bound to a checkout or
// not (ADR-0241 decision 1). It is a third method rather than a third rung inside
// AttributePaneNeighbourhood so that first-hit precedence survives the addition —
// a checkout answer from any kind must outrank a repository answer from any kind,
// which a branch inside one method cannot express.
//
// Its answer is plural by construction, and plural across kinds too: see the merge
// in AttributePane.
type PaneRepositoryAttributor interface {
	AttributePaneRepository(PaneFacts) (Attribution, bool)
}

// AttributePane walks the wired kinds and returns the attribution they make, or
// nil when the pane belongs to nothing here.
//
// It walks them three times, which is the ladder (ADR-0201 decision 1, ADR-0241
// decision 1): tags, then neighbourhood, then repository. The first pass asks
// every kind what pane pop opened this is: a pane pop opened for a Task set is
// that set's whatever session it sits in, so the Task-set kind is asked before the
// kind that has only a session stamp to go on. Only once no kind recognises the
// pane as its own does the second pass ask which bound checkout the pane is
// standing in, in the same kind order, and only once that is silent too does the
// third ask which repository it is standing in. A kind not wired onto this page is
// asked in none of the passes, which is how a Routine pane leaves a page-A cursor
// alone.
func AttributePane(kinds []Kind, facts PaneFacts) *Attribution {
	if facts.Empty() {
		return nil
	}
	ordered := kindsInPrecedence(kinds)
	for _, k := range ordered {
		if attributor, ok := k.(PaneAttributor); ok {
			if att, hit := attributor.AttributePane(facts); hit && len(att.Containers) > 0 {
				return &att
			}
		}
	}
	for _, k := range ordered {
		if attributor, ok := k.(PaneNeighbourhoodAttributor); ok {
			if att, hit := attributor.AttributePaneNeighbourhood(facts); hit && len(att.Containers) > 0 {
				return &att
			}
		}
	}
	return attributeRepository(ordered, facts)
}

// attributeRepository is the ladder's third pass, and the one place this walk is
// not first-hit (ADR-0241 decision 2). Every kind holding work in the pane's
// repository contributes, and the answers concatenate in kind precedence order —
// each kind's own ranking kept inside its block. The pass means *the work of mine
// that lives here*, which is plural, where a tag means *this pane is that work*,
// which is one container; first-hit here would silently decide that a repository
// with a Task set has no Maps.
func attributeRepository(ordered []Kind, facts PaneFacts) *Attribution {
	merged := Attribution{}
	for _, k := range ordered {
		attributor, ok := k.(PaneRepositoryAttributor)
		if !ok {
			continue
		}
		if att, hit := attributor.AttributePaneRepository(facts); hit {
			merged.Containers = append(merged.Containers, att.Containers...)
		}
	}
	if len(merged.Containers) == 0 {
		return nil
	}
	return &merged
}
