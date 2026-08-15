package setkind

import (
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/glebglazov/pop/repogroup"
	"github.com/glebglazov/pop/tasks"
	"github.com/glebglazov/pop/work"
	"github.com/glebglazov/pop/work/ref"
)

// The Task-set kind's answer to Pane work attribution (ADR-0201): the strongest
// rung of the ladder, because a pane carrying @pop_set / @pop_verify / @pop_fold
// / @pop_assist is a pane pop opened for that set — not a pane that happens to
// sit near it.

// paneIndex maps a set id to the attributed container its row would carry. It is
// recorded from every row a pass read, including the rows the view preset
// dropped: a pane tagged for a hidden set has to be answered for by name, and a
// lookup afterwards would mean scanning the machine a second time.
type paneIndex map[string]work.AttributedContainer

// boundSet is one candidate for the ladder's weakest rung: a set whose Worktree
// binding points somewhere, so a pane standing in that checkout might be about
// it. The binding path is kept as recorded and canonicalised only if a launch
// actually asks — an ordinary rebuild must pay nothing for a question nobody
// asked.
type boundSet struct {
	setID string
	// repoCommonDir identifies the repository the set's drain history is keyed
	// under, which is where its recency comes from.
	repoCommonDir string
	// runtimePath is the bound checkout, exactly as the binding spells it: the
	// checkout claim is keyed by the same string.
	runtimePath string
	// sortRow is the projection the last-resort tiebreak compares under the active
	// sort. It carries only what the comparator reads, and deliberately not
	// Orphaned: every candidate is bound to the checkout the pane is standing in,
	// so none of them can be pointing at a missing one.
	sortRow work.Container
}

// recordPanes indexes one group's rows for attribution. It runs before the
// preset filter by construction — it reads the refresh, not the containers the
// filter produced.
func (k *Kind) recordPanes(g repogroup.Group, refresh *tasks.RefreshResult, snap *snapshot) {
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
		k.panes[row.ID] = work.AttributedContainer{
			Ref:       ref.WorkRef{Kind: ref.KindTaskSet, ContainerID: row.ID},
			CursorKey: setCursorKey(g, row.ID),
			Label:     "task set " + row.ID,
		}
		bnd, hasBinding := snap.bindingFor(g.RepoKey, row.ID)
		if !hasBinding || strings.TrimSpace(bnd.RuntimePath) == "" {
			continue
		}
		k.bound = append(k.bound, boundSet{
			setID:         row.ID,
			repoCommonDir: g.RepoCommonDir,
			runtimePath:   bnd.RuntimePath,
			sortRow: work.Container{
				Kind:      ref.KindTaskSet,
				ID:        row.ID,
				Project:   g.ProjectName,
				RawStatus: row.Status,
				AutoDrain: row.AutoDrain,
				Started:   row.Started,
				Bound:     true,
				LiveDrain: liveDrain(snap, g.RepoKey, row.ID, bnd.RuntimePath),
			},
		})
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
		if c, ok := k.panes[id]; ok {
			return work.AttributeOne(c), true
		}
	}
	return work.Attribution{}, false
}

// AttributePaneNeighbourhood answers the ladder's two weakest rungs, in order: a
// live Drain at the pane's directory, which names its set outright, and otherwise
// the checkout the pane sits in plus the sets bound to it. The second is the rung
// that fires for the ordinary editor shell the human opened themselves — which is
// where they are standing when they want a seeded cursor at all (ADR-0201
// decision 1).
//
// Directories are compared canonically and by containment, so a pane deep inside
// a checkout still resolves to it; when checkouts nest, the deepest one containing
// the pane wins, because that is the checkout the pane is actually in.
func (k *Kind) AttributePaneNeighbourhood(facts work.PaneFacts) (work.Attribution, bool) {
	dir := k.d.canonPath(facts.Directory)
	if dir == "" {
		return work.Attribution{}, false
	}
	if att, ok := k.attributeLiveDrainAt(dir); ok {
		return att, true
	}
	return k.attributeBoundCheckoutAt(dir)
}

// attributeLiveDrainAt names the set of the live Drain whose checkout contains the
// pane. A drain is unambiguous where a binding is not: it is one process working
// one set in one checkout right now.
func (k *Kind) attributeLiveDrainAt(dir string) (work.Attribution, bool) {
	deepest, setID := "", ""
	for _, dr := range k.live {
		path := k.d.canonPath(dr.RuntimePath)
		if !pathUnder(dir, path) || len(path) <= len(deepest) {
			continue
		}
		deepest, setID = path, dr.SetID
	}
	if setID == "" {
		return work.Attribution{}, false
	}
	c, ok := k.panes[setID]
	if !ok {
		return work.Attribution{}, false
	}
	return work.AttributeOne(c), true
}

// attributeBoundCheckoutAt resolves the pane's directory to the bound checkout
// containing it and then to the sets bound there. One checkout can hold several,
// and every one of them is an answer: the pane really is standing in all of their
// work, so all of them are attributed and only their order is decided
// (ADR-0209 decision 2).
func (k *Kind) attributeBoundCheckoutAt(dir string) (work.Attribution, bool) {
	checkout := ""
	for _, cand := range k.bound {
		path := k.d.canonPath(cand.runtimePath)
		if pathUnder(dir, path) && len(path) > len(checkout) {
			checkout = path
		}
	}
	if checkout == "" {
		return work.Attribution{}, false
	}
	var cands []boundSet
	for _, cand := range k.bound {
		if k.d.canonPath(cand.runtimePath) == checkout {
			cands = append(cands, cand)
		}
	}
	if len(cands) == 0 {
		return work.Attribution{}, false
	}
	att := work.Attribution{}
	for _, cand := range k.rankBoundCandidates(cands) {
		if c, ok := k.panes[cand.setID]; ok {
			att.Containers = append(att.Containers, c)
		}
	}
	return att, len(att.Containers) > 0
}

// rankBoundCandidates orders the sets bound to one checkout, leader first, by the
// sub-ladder of ADR-0201 decision 2 — demoted by ADR-0209 from picking a winner to
// picking who leads: the checkout claim holder while something is live there, then
// the sets that drained, most recent first, then the never-drained ones in the
// order the active sort already puts them in.
func (k *Kind) rankBoundCandidates(cands []boundSet) []boundSet {
	claim := k.claimHolder(cands)
	drained := k.drainStarts(cands)
	ranked := make([]boundSet, len(cands))
	copy(ranked, cands)
	sort.SliceStable(ranked, func(i, j int) bool {
		a, b := ranked[i], ranked[j]
		if held := a.setID == claim; held != (b.setID == claim) {
			return held
		}
		atA, hasA := drained[a.setID]
		atB, hasB := drained[b.setID]
		if hasA != hasB {
			return hasA
		}
		if hasA && !atA.Equal(atB) {
			return atA.After(atB)
		}
		return tasks.WorkRowLess(a.sortRow, b.sortRow, k.d.ViewPreset.Sort)
	})
	return ranked
}

// claimHolder returns the id of the candidate holding the live Checkout claim on
// the shared checkout. The claim is keyed by the runtime path as each binding
// spells it, so every distinct spelling is asked until one answers; a claim held by
// something that is not one of these candidates ranks nothing. A claim that cannot
// be read ranks nothing either — the rung below is a better answer than an error
// about row order.
func (k *Kind) claimHolder(cands []boundSet) string {
	bound := map[string]bool{}
	for _, cand := range cands {
		bound[cand.setID] = true
	}
	asked := map[string]bool{}
	for _, cand := range cands {
		if asked[cand.runtimePath] {
			continue
		}
		asked[cand.runtimePath] = true
		claim, err := tasks.ReadCheckoutClaim(k.d.Tasks, cand.runtimePath)
		if err != nil || claim == nil {
			continue
		}
		if claim.Holder.Kind != ref.KindTaskSet {
			continue
		}
		if bound[claim.Holder.ContainerID] {
			return claim.Holder.ContainerID
		}
	}
	return ""
}

// drainStarts reads each candidate's last Drain start, which is the only per-set
// recency pop records. A set missing from the map never drained, and ranks below
// every set that did.
func (k *Kind) drainStarts(cands []boundSet) map[string]time.Time {
	starts := map[string]map[string]time.Time{}
	out := map[string]time.Time{}
	for _, cand := range cands {
		byRepo, read := starts[cand.repoCommonDir]
		if !read {
			var err error
			byRepo, err = tasks.LatestDrainStarts(k.d.Tasks, cand.repoCommonDir)
			if err != nil {
				byRepo = nil
			}
			starts[cand.repoCommonDir] = byRepo
		}
		if at, drained := byRepo[cand.setID]; drained {
			out[cand.setID] = at
		}
	}
	return out
}

// canonPath canonicalizes a directory for containment comparison, falling back to
// a cleaned absolute path when it cannot be resolved — a pane's directory may have
// been deleted under it, and the fallback still compares two strings the same way.
func (d *Deps) canonPath(path string) string {
	path = strings.TrimSpace(path)
	if path == "" {
		return ""
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		abs = path
	}
	if d != nil && d.Tasks != nil && d.Tasks.FS != nil {
		if resolved, err := d.Tasks.FS.EvalSymlinks(abs); err == nil {
			return resolved
		}
	}
	return filepath.Clean(abs)
}

// pathUnder reports whether path is root or lives beneath it. Both are expected
// canonical.
func pathUnder(path, root string) bool {
	if root == "" {
		return false
	}
	return path == root || strings.HasPrefix(path, root+string(filepath.Separator))
}

// setCursorKey is a task-set row's stable cursor identity: the project label and
// the set id, which is the pair that is unique across a page.
func setCursorKey(g repogroup.Group, setID string) string {
	return g.ProjectName + "\x00" + setID
}
