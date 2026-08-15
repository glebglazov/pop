package wayfinder

import (
	"github.com/glebglazov/pop/repogroup"
	"github.com/glebglazov/pop/work"
	"github.com/glebglazov/pop/work/ref"
)

// The Map kind's answer to Pane work attribution (ADR-0201): two rungs, because a
// Map is the one kind that opens both tagged panes and a session of its own. A
// Grilling pane names the ticket it is deciding; the session those panes live in
// names the Map.

// mapPaneIndex is what one load remembers for attribution: every Map it read,
// including the ones the view preset dropped, plus which Maps hold a ticket of a
// given id. Ticket ids are per-Map, so the same "01" exists in many of them and
// the index has to keep the ambiguity rather than resolve it early.
type mapPaneIndex struct {
	byMap    map[string]work.AttributedContainer
	byTicket map[string][]string
}

// recordPanes indexes one Map for attribution, before the view preset has had a
// say about whether its row renders.
func (idx *mapPaneIndex) recordPanes(g repogroup.Group, m Map) {
	if idx.byMap == nil {
		idx.byMap = map[string]work.AttributedContainer{}
		idx.byTicket = map[string][]string{}
	}
	if _, seen := idx.byMap[m.ID]; seen {
		return
	}
	idx.byMap[m.ID] = work.AttributedContainer{
		Ref:       ref.WorkRef{Kind: ref.KindMap, ContainerID: m.ID},
		CursorKey: mapCursorKey(g, m.ID),
		Label:     "map " + m.ID,
	}
	for _, t := range m.Tickets {
		idx.byTicket[t.ID] = append(idx.byTicket[t.ID], m.ID)
	}
}

// forTicket resolves a ticket id to its Map. One holder is the answer; several
// are broken by the session the pane sits in, which is the Map its Grilling panes
// belong to by construction.
func (idx *mapPaneIndex) forTicket(ticketID, session string) (work.AttributedContainer, bool) {
	holders := idx.byTicket[ticketID]
	if len(holders) == 0 {
		return work.AttributedContainer{}, false
	}
	if len(holders) > 1 {
		if from := MapIDFromSession(session); from != "" {
			for _, id := range holders {
				if id == from {
					return idx.byMap[id], true
				}
			}
		}
	}
	c, ok := idx.byMap[holders[0]]
	return c, ok
}

// AttributePane answers the Map's two rungs, strongest first.
//
// The tag rung means "this pane is one pop opened for this Map": a Grilling pane
// carries the ticket, and the Map's assist pane carries the Map itself under the
// same @pop_assist tag a Task set's assist pane uses (ADR-0184).
//
// The session rung is weaker only in that it speaks for a whole session rather
// than one pane — a bare shell in a Map's session still belongs to that Map, which
// is exactly the case the human is in when they open the dashboard from it. The
// stamp is the authority; the session name answers for a session created before
// pop stamped one.
func (k *MapKind) AttributePane(facts work.PaneFacts) (work.Attribution, bool) {
	if facts.Ticket != "" {
		if c, ok := k.panes.forTicket(facts.Ticket, facts.Session); ok {
			return work.AttributeOne(c), true
		}
	}
	if facts.Assist != "" {
		if c, ok := k.panes.byMap[facts.Assist]; ok {
			return work.AttributeOne(c), true
		}
	}
	if facts.WorkKind == string(ref.KindMap) && facts.WorkID != "" {
		if c, ok := k.panes.byMap[facts.WorkID]; ok {
			return work.AttributeOne(c), true
		}
	}
	if id := MapIDFromSession(facts.Session); id != "" {
		if c, ok := k.panes.byMap[id]; ok {
			return work.AttributeOne(c), true
		}
	}
	return work.Attribution{}, false
}

// mapCursorKey is a Map row's stable cursor identity. The literal "map" segment
// keeps it apart from a task set of the same name in the same project.
func mapCursorKey(g repogroup.Group, mapID string) string {
	return g.ProjectName + "\x00map\x00" + mapID
}
