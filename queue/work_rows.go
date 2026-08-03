package queue

import (
	"github.com/glebglazov/pop/tasks"
	"github.com/glebglazov/pop/work"
	"github.com/glebglazov/pop/work/ref"
)

// The queue-side end of the Work seam: how a read surface reaches the kind that
// owns a row. Every cell a row shows and every verb it offers is composed by
// that kind (ADR-0173), so the dashboard, the action menu and the static status
// table all resolve the kind first and ask it — none of them branches on what a
// row is.

// workKinds resolves a row's Work kind. It is built from the same wiring list
// the snapshot builder consumes, so a caller that injects a kind sees its cells
// and its verbs, not just its rows.
type workKinds struct {
	byID map[work.KindID]work.Kind
}

// newWorkKinds indexes a wiring list by kind id.
func newWorkKinds(kinds []work.Kind) workKinds {
	byID := make(map[work.KindID]work.Kind, len(kinds))
	for _, k := range kinds {
		if k != nil {
			byID[k.ID()] = k
		}
	}
	return workKinds{byID: byID}
}

// kindFor returns the kind that owns row. A row that names no kind came from a
// builder that predates the seam, and every one of those built task sets — the
// default keeps them rendering until the last of them is gone.
func (w workKinds) kindFor(row DashboardRow) work.Kind {
	id := row.Kind
	if id == "" {
		id = ref.KindTaskSet
	}
	return w.byID[id]
}

// statusSegments is the row's STATUS cell as the owning kind composed it. A row
// whose kind is not wired shows no status rather than a guess.
func (w workKinds) statusSegments(row DashboardRow) []work.StatusSegment {
	k := w.kindFor(row)
	if k == nil {
		return nil
	}
	return k.StatusCell(row)
}

// actionsFor is the verb list the kind offers over row right now. It is called
// when a menu opens over one row, never per row at build time, so eligibility is
// as fresh as the keypress that asked (ADR-0173).
func (w workKinds) actionsFor(row DashboardRow) []work.Action {
	k := w.kindFor(row)
	if k == nil {
		return nil
	}
	return k.Actions(row)
}

// offers reports whether the kind currently offers verb over row.
func (w workKinds) offers(row DashboardRow, verb work.Verb) bool {
	for _, a := range w.actionsFor(row) {
		if a.Verb == verb {
			return true
		}
	}
	return false
}

// summary composes the header phrases for a page of rows: each kind's own
// Summary over its own rows, in kind precedence order (ADR-0173). Counting per
// kind rather than over one row list is what groups the Map tally behind the
// Task-set tallies instead of interleaving it.
func (w workKinds) summary(rows []DashboardRow) []string {
	var phrases []string
	for _, id := range ref.Kinds() {
		k := w.byID[id]
		if k == nil {
			continue
		}
		var own []work.Container
		for _, row := range rows {
			if w.kindFor(row) == k {
				own = append(own, row)
			}
		}
		if len(own) == 0 {
			continue
		}
		phrases = append(phrases, k.Summary(own)...)
	}
	return phrases
}

// mapRow reports whether a row is a Wayfinder Map. It is the whole of what the
// dashboard still asks a row about its kind, and only where the question is
// genuinely about the kind rather than about a verb: the flat wayfinding
// shortcut (a Map with an empty frontier must still answer, so the key cannot
// hang off the verb being offered), the activity cluster, and the detail frame
// slice 14 replaces.
func mapRow(row DashboardRow) bool {
	return row.Kind == ref.KindMap
}

// dashboardStatusCellText is a row's plain, un-styled STATUS cell — the kind's
// segments joined. Column width-fitting measures this form, so no ANSI ever
// leaks into column math.
func dashboardStatusCellText(kinds workKinds, row DashboardRow) string {
	return work.StatusCellText(kinds.statusSegments(row))
}

// sortDashboardRows applies the shared Queue surface order to a set of dashboard
// rows. The comparator — the ADR-0121 membership tiers, status bands,
// intra-project status order and SetID tiebreak — is the Task-set kind's own
// (ADR-0121); the snapshot builder reaches it through that kind's Less, and this
// is the queue-side seam the static status render keys on.
func sortDashboardRows(rows []DashboardRow) {
	tasks.SortWorkRows(rows)
}
