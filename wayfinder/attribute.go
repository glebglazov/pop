package wayfinder

import (
	"path/filepath"
	"sort"
	"strings"

	"github.com/glebglazov/pop/repogroup"
	"github.com/glebglazov/pop/work"
	"github.com/glebglazov/pop/work/ref"
)

// The Map kind's answer to Pane work attribution (ADR-0201): two rungs of the tag
// pass, because a Map is the one kind that opens both tagged panes and a session
// of its own, plus the repository pass (ADR-0241). A Grilling pane names the
// ticket it is deciding; the session those panes live in names the Map; and a
// shell that is merely standing in the repository the Map lives in gets it from
// the weakest pass, where before it got nothing at all.

// mapPaneIndex is what one load remembers for attribution: every Map it read,
// including the ones the view preset dropped, plus which Maps hold a ticket of a
// given id. Ticket ids are per-Map, so the same "01" exists in many of them and
// the index has to keep the ambiguity rather than resolve it early.
type mapPaneIndex struct {
	byMap    map[string]work.AttributedContainer
	byTicket map[string][]string
	// inRepo is the same load's candidates for the repository pass, in the order
	// the Maps were read.
	inRepo []repoMap
}

// repoMap is one candidate for the ladder's weakest pass: a live Map of one
// repository. A Map is Trunk-rooted and owns no checkout, so the repository is the
// whole of its locality — and it is the repository whose Task storage the Map was
// read from, which is the same one the Trunk resolves into and which the group
// already carries as a git common directory (ADR-0241 decisions 3 and 4). Nothing
// is re-derived here: a Map is filed under a repository, and that is the answer.
type repoMap struct {
	mapID string
	// repoCommonDir is the whole of the match — the pane carries the same identity,
	// so any worktree of the repository resolves here, which is the blind spot the
	// pass exists to close.
	repoCommonDir string
	// sortRow is the projection the pass orders its answer under, read by the Map
	// kind's own comparator and by nothing else: Less compares the project label
	// and then the id, so those two fields are the whole projection.
	sortRow work.Container
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
	if liveForLocality(m) {
		idx.inRepo = append(idx.inRepo, repoMap{
			mapID:         m.ID,
			repoCommonDir: g.RepoCommonDir,
			sortRow:       work.Container{Kind: ref.KindMap, ID: m.ID, Project: g.ProjectName},
		})
	}
}

// liveForLocality reports whether a Map is work that *lives* in its repository —
// the question the repository pass asks, which is not the question the tag pass
// asks. A pane pop opened for an abandoned or BROKEN Map still belongs to it and
// is answered for by name (ADR-0201 decision 6); a shell that merely stands in the
// repository is not standing in work that has been abandoned or cannot be read. An
// archived Map stays a candidate: archiving hides a row, and which rows the answer
// can lift is the preset's business alone (ADR-0209 decision 7).
func liveForLocality(m Map) bool {
	if m.Broken {
		return false
	}
	return m.Status == MapActive || m.Status == MapArrived
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

// AttributePaneRepository answers the ladder's weakest pass with every live Map of
// the repository the pane is standing in (ADR-0241 decision 3). It is the only
// locality a Map has: a Map is Trunk-rooted, so before this pass an editor shell in
// the repository had the same blind spot for a live Map that it had for an unbound
// task set — you are standing in the work and the dashboard does not know it.
//
// The pass is reached only when the tag and neighbourhood passes are silent, so a
// pane inside a Map's own session is answered by the stamp as it always was; and
// its answer is merged with the Task-set kind's rather than beaten by it, which is
// what keeps a repository holding both from silently having no Maps (decision 2).
func (k *MapKind) AttributePaneRepository(facts work.PaneFacts) (work.Attribution, bool) {
	repo := k.canonPath(facts.RepoCommonDir)
	if repo == "" {
		return work.Attribution{}, false
	}
	// One canonicalisation per repository rather than per Map: every Map of a group
	// carries the one string, and a machine has far fewer repositories than Maps.
	canon := map[string]string{}
	var cands []repoMap
	for _, cand := range k.panes.inRepo {
		resolved, seen := canon[cand.repoCommonDir]
		if !seen {
			resolved = k.canonPath(cand.repoCommonDir)
			canon[cand.repoCommonDir] = resolved
		}
		if resolved == repo {
			cands = append(cands, cand)
		}
	}
	if len(cands) == 0 {
		return work.Attribution{}, false
	}
	// The kind's own comparator, which is what the page already ordered these rows
	// by: the pass adds no order of its own.
	sort.SliceStable(cands, func(i, j int) bool { return k.Less(cands[i].sortRow, cands[j].sortRow) })
	att := work.Attribution{}
	for _, cand := range cands {
		if c, ok := k.panes.byMap[cand.mapID]; ok {
			att.Containers = append(att.Containers, c)
		}
	}
	return att, len(att.Containers) > 0
}

// canonPath canonicalizes a path for comparison, falling back to a cleaned
// absolute path when it cannot be resolved: a repository may have been moved under
// a live pane, and the fallback still compares two strings the same way.
func (k *MapKind) canonPath(path string) string {
	path = strings.TrimSpace(path)
	if path == "" {
		return ""
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		abs = path
	}
	if wd := k.d.Wayfinder; wd != nil && wd.FS != nil {
		if resolved, err := wd.FS.EvalSymlinks(abs); err == nil {
			return resolved
		}
	}
	return filepath.Clean(abs)
}

// mapCursorKey is a Map row's stable cursor identity. The literal "map" segment
// keeps it apart from a task set of the same name in the same project.
func mapCursorKey(g repogroup.Group, mapID string) string {
	return g.ProjectName + "\x00map\x00" + mapID
}
