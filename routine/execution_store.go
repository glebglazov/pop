package routine

import (
	"github.com/glebglazov/pop/store"
)

// openExecutionStore borrows the process-cached execution-state store handle
// through the tasks accessor in create-if-missing mode (ADR-0140): the data
// directory and database file are materialised on first use. The handle is
// shared for process life — long-lived surfaces (the Routine dashboard) hold it
// per ADR-0118's model — so borrowers never close it. The test-isolation guard
// and store path derivation live inside the accessor, not here.
func openExecutionStore(d *Deps) (*store.Store, error) {
	s, _, err := d.taskDeps().Store(true)
	return s, err
}
