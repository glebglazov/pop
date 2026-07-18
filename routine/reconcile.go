package routine

import (
	"fmt"
	"os"

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
	defer func() { _ = s.Close() }()
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

func openExecutionStoreIfExists(d *Deps) (*store.Store, bool, error) {
	path := executionStorePath(d)
	guardTestStorePath(path)
	if _, err := d.FS.Stat(path); err != nil {
		if os.IsNotExist(err) {
			return nil, false, nil
		}
		return nil, false, fmt.Errorf("stat execution-state store: %w", err)
	}
	s, err := openExecutionStore(d)
	if err != nil {
		return nil, false, err
	}
	return s, true, nil
}
