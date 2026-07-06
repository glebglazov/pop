package queue

import (
	"github.com/glebglazov/pop/tasks"
)

// loadRecoveryWaiters reads active quota-recovery waiters from pop.db. A read
// error degrades to nil so dispatch never blocks on a transient store problem.
func loadRecoveryWaiters(d *Deps) map[string]tasks.RecoveryWaiter {
	if d == nil || d.Tasks == nil {
		return nil
	}
	waiters, err := tasks.AllRecoveryWaiters(d.Tasks)
	if err != nil {
		return nil
	}
	return waiters
}
