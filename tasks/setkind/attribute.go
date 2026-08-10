package setkind

import (
	"github.com/glebglazov/pop/repogroup"
	"github.com/glebglazov/pop/tasks"
	"github.com/glebglazov/pop/work"
	"github.com/glebglazov/pop/work/ref"
)

// The Task-set kind's answer to Pane work attribution (ADR-0201): the strongest
// rung of the ladder, because a pane carrying @pop_set / @pop_verify / @pop_fold
// / @pop_assist is a pane pop opened for that set — not a pane that happens to
// sit near it.

// paneIndex maps a set id to the attribution its row would carry. It is recorded
// from every row a pass read, including the rows the view preset dropped: a pane
// tagged for a hidden set has to be reported by name, and a lookup afterwards
// would mean scanning the machine a second time.
type paneIndex map[string]work.Attribution

// recordPanes indexes one group's rows for attribution. It runs before the
// preset filter by construction — it reads the refresh, not the containers the
// filter produced.
func (k *Kind) recordPanes(g repogroup.Group, refresh *tasks.RefreshResult) {
	if refresh == nil {
		return
	}
	if k.panes == nil {
		k.panes = paneIndex{}
	}
	for _, row := range refresh.Rows {
		if _, seen := k.panes[row.ID]; seen {
			// Two repository groups can hold a set of the same name. The pane tag
			// carries only the id, so the first group wins — the same rule the cursor
			// key's own collision has always had.
			continue
		}
		k.panes[row.ID] = work.Attribution{
			Ref:       ref.WorkRef{Kind: ref.KindTaskSet, ContainerID: row.ID},
			CursorKey: setCursorKey(g, row.ID),
			Label:     "task set " + row.ID,
		}
	}
}

// AttributePane answers the ladder's first rung. The four tags are one question —
// drain, verify, fold and assist are activities on the same set, and no pane
// carries two — so the first one set answers. A tag naming a set this pass never
// saw answers nothing: the set was deleted under a live pane, and claiming a row
// that does not exist would report a phantom.
func (k *Kind) AttributePane(facts work.PaneFacts) (work.Attribution, bool) {
	for _, id := range []string{facts.Set, facts.Verify, facts.Fold, facts.Assist} {
		if id == "" {
			continue
		}
		if att, ok := k.panes[id]; ok {
			return att, true
		}
	}
	return work.Attribution{}, false
}

// setCursorKey is a task-set row's stable cursor identity: the project label and
// the set id, which is the pair that is unique across a page.
func setCursorKey(g repogroup.Group, setID string) string {
	return g.ProjectName + "\x00" + setID
}
