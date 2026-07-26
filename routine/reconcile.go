package routine

import (
	"github.com/glebglazov/pop/store"
	"github.com/glebglazov/pop/tasks"
)

// ReconcileRunsWith heals running routine rows whose owning process is no longer
// alive, mirroring the drain reconcile pass. It returns the number of rows
// transitioned to failed.
func ReconcileRunsWith(d *Deps) (int, error) {
	s, ok, err := openExecutionStoreIfExists(d)
	if err != nil || !ok {
		return 0, err
	}
	return s.ReconcileCrashedRoutineRuns(func(run store.RoutineRun) bool {
		return routineProcessAlive(d, run.PID, run.ProcStart)
	}, nowUTC(d))
}

func routineProcessAlive(d *Deps, pid int, procStart string) bool {
	if d.ProcessAlive != nil {
		return d.ProcessAlive(pid, procStart)
	}
	if d.Tasks != nil {
		return tasks.ProcessLiveWithToken(d.Tasks, pid, procStart)
	}
	return processAlivePID(pid)
}

// openExecutionStoreIfExists borrows the process-cached store handle through the
// tasks accessor in if-exists mode (ADR-0140): a machine without a store yields
// (nil, false, nil) without materialising an empty database, so pure readers
// never create one as a side effect. The borrowed handle is shared for process
// life and must not be closed.
func openExecutionStoreIfExists(d *Deps) (*store.Store, bool, error) {
	return d.taskDeps().Store(false)
}
