package routine

import (
	"os"
	"path/filepath"
	"testing"
)

// A routine operation borrows the Execution-state store through tasks.Deps
// (ADR-0140), so a later tasks operation in the same process must reuse that
// very handle rather than open a second connection.
func TestRoutineAndTasksShareOneStoreHandle(t *testing.T) {
	root := t.TempDir()
	dataHome := filepath.Join(root, "data")
	home := filepath.Join(root, "home")
	if err := os.MkdirAll(home, 0o755); err != nil {
		t.Fatal(err)
	}
	d := routineDeps(t, dataHome)
	if _, err := AddWith(d, "shared", "every 6h", home); err != nil {
		t.Fatal(err)
	}

	// Routine borrow, create-if-missing mode: materialises the process-cached handle.
	routineHandle, err := openExecutionStore(d)
	if err != nil {
		t.Fatal(err)
	}

	// A later tasks operation, if-exists mode: finds the store the routine op
	// created and returns the identical cached handle.
	tasksHandle, ok, err := d.Tasks.Store(false)
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Fatal("tasks accessor should find the store the routine op created")
	}
	if routineHandle != tasksHandle {
		t.Fatal("routine and tasks operations must share a single store handle")
	}
}
