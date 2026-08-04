package dashboard

import (
	"fmt"

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

// columns is the column header row a page reads under: the page's primary kind's
// own, because that kind authors the cells beneath them (ADR-0173). A page whose
// primary kind is not wired shows no header rather than a guess, the same rule
// kindFor follows for cells.
func (w workKinds) columns(primary work.KindID) []string {
	k := w.byID[primary]
	if k == nil {
		return nil
	}
	return k.Columns()
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

// statusActionsFor is the status-submenu verbs the kind offers over row. Asked
// when the submenu opens, like actionsFor: the vocabulary is the kind's and the
// surface only lays it out (ADR-0186).
func (w workKinds) statusActionsFor(row DashboardRow) []work.Action {
	k := w.kindFor(row)
	if k == nil {
		return nil
	}
	return k.StatusActions(row)
}

// itemActionsFor is the verb list the kind offers over one of row's items right
// now. Like actionsFor it is asked when the menu opens, never carried on the
// item: a task completed in another pane must not still offer "complete".
func (w workKinds) itemActionsFor(row DashboardRow, item work.Item) []work.Action {
	k := w.kindFor(row)
	if k == nil {
		return nil
	}
	return k.ItemActions(row, item)
}

// itemCopyPayload is the reference the kind writes to the clipboard for one of
// its items. The kind answers rather than the surface guessing: a task set names
// a task by its paste-ready `<set>/<file>.md` target, a Map names a ticket by its
// bare id, and neither form is derivable from the item alone.
func (w workKinds) itemCopyPayload(row DashboardRow, item work.Item) (string, error) {
	k := w.kindFor(row)
	if k == nil {
		return "", fmt.Errorf("no Work kind wired for %s", row.ID)
	}
	outcome, err := k.Perform(row, &item, work.VerbCopyName)
	if err != nil {
		return "", err
	}
	return outcome.Clipboard, nil
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
// hang off the verb being offered) and the activity cluster. The detail view no
// longer asks at all — it is generic over containers and their items.
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
