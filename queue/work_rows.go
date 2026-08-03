package queue

import (
	"fmt"

	"github.com/glebglazov/pop/tasks"
)

// The transitional bridge between the Work seam and queue's row model. A
// Container carries the composed status cell its own kind wrote, so once the
// render paths read Containers these three go away with the IsMap branches they
// exist to keep working.

// dashboardStatusCellText composes a row's plain, un-styled STATUS cell: the Map
// tally for a Map row (ADR-0130), else the Task-set kind's composition — label
// plus the verified-at, auto-drain, orphaned, parked and config-error suffixes in
// that fixed order. Column width-fitting measures this plain form, so no ANSI
// leaks into column math.
func dashboardStatusCellText(row DashboardRow) string {
	if row.IsMap {
		return fmt.Sprintf("WAYFINDING · %d open / %d frontier", row.MapOpen, row.MapFrontier)
	}
	return tasks.WorkRowStatusCell(row)
}

// dashboardStatusLabelText is a row's display label: WAYFINDING for a Map row,
// else the Task-set label with the READY→IN PROGRESS refinement applied.
func dashboardStatusLabelText(row DashboardRow) string {
	if row.IsMap {
		return "WAYFINDING"
	}
	return tasks.WorkRowStatusLabel(row)
}

// sortDashboardRows applies the shared Queue surface order to a set of dashboard
// rows. The comparator — the ADR-0121 membership tiers, status bands,
// intra-project status order and SetID tiebreak — is the Task-set kind's own
// (ADR-0121); the snapshot builder reaches it through that kind's Less, and this
// is the queue-side seam the static status render keys on.
func sortDashboardRows(rows []DashboardRow) {
	tasks.SortWorkRows(rows)
}
