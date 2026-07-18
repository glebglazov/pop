package tasks

import (
	"github.com/glebglazov/pop/store"
)

// AllRoutineRuns returns every routine run row in the execution-state store,
// oldest first. A missing store yields nil, nil.
func AllRoutineRuns(d *Deps) ([]store.RoutineRun, error) {
	s, ok, err := openDrainStoreIfExists(d)
	if err != nil || !ok {
		return nil, err
	}
	defer func() { _ = s.Close() }()
	return s.ListAllRoutineRuns()
}
