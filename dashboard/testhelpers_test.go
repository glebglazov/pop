package dashboard

import (
	"github.com/glebglazov/pop/tasks/drain"
	"strings"

	"github.com/glebglazov/pop/tasks"
	"github.com/glebglazov/pop/tasks/setkind"
)

// detailRowWithTasks returns row carrying what the Task-set kind would have
// built from manifest: the Work items the detail view lists, the malformed
// verdict, and the progress headline. The detail view reads the container, so a
// test that wants tasks on screen puts them there — through the kind's own
// projection, never a second one.
func detailRowWithTasks(row DashboardRow, manifest *tasks.Manifest, taskRow *tasks.Row) DashboardRow {
	row.Items = setkind.ItemsFromManifest(manifest)
	if manifest != nil && !manifest.Valid {
		row.Broken, row.BrokenReason = true, strings.Join(manifest.Errors, "; ")
	}
	if taskRow != nil {
		row.Headline = taskRow.Progress
	}
	return row
}

// newTaskDetailView opens a detail view over row as the Task-set kind would have
// loaded it.
func newTaskDetailView(row DashboardRow, manifest *tasks.Manifest, taskRow *tasks.Row) *detailView {
	return newDetailView(detailRowWithTasks(row, manifest, taskRow))
}

// testKinds is the wiring list a test renders and dispatches through: the same
// real adapters `queue.Deps.WorkKinds` hands the snapshot builder, so a test row
// gets the cells and the verbs its own kind composes.
func testKinds() workKinds {
	return newWorkKinds((&drain.Deps{}).WorkKinds(nil))
}

// testRoutineKinds is the Routine page's wiring list, for a test that asks what
// that kind offers over one of its own rows.
func testRoutineKinds() workKinds {
	return newWorkKinds((&drain.Deps{}).RoutinePageKinds(nil))
}
