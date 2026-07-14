package tasks

import (
	"reflect"
	"strings"
	"testing"
)

// seedIntentState writes a state file beside defPath holding the given
// registered sets under the canonical definition path, and returns the
// canonical path and the derived state path.
func seedIntentState(t *testing.T, d *Deps, defPath string, sets []RegisteredTaskSet) (canon, statePath string) {
	t.Helper()
	var err error
	canon, err = CanonicalDefinitionPathWith(d, defPath)
	if err != nil {
		t.Fatal(err)
	}
	statePath = StatePathFor(canon)
	seed := &GlobalState{
		Version: StateVersion,
		Tasks:   map[string]*TaskEntry{canon: {TaskSets: sets}},
		path:    statePath,
	}
	if err := seed.SaveWith(d); err != nil {
		t.Fatal(err)
	}
	return canon, statePath
}

func TestSetTaskSetPriorityReturnsPriorAndPersists(t *testing.T) {
	root := t.TempDir()
	t.Setenv("XDG_DATA_HOME", root)
	d := DefaultDeps()
	defPath := root + "/tasks"
	canon, statePath := seedIntentState(t, d, defPath, []RegisteredTaskSet{{ID: "demo", Priority: 3}})

	old, err := SetTaskSetPriority(d, defPath, "demo", 7)
	if err != nil {
		t.Fatal(err)
	}
	if old != 3 {
		t.Fatalf("old priority = %d, want 3", old)
	}

	state, err := LoadGlobalStateWith(d, statePath)
	if err != nil {
		t.Fatal(err)
	}
	if got := state.Tasks[canon].TaskSets[0].Priority; got != 7 {
		t.Fatalf("persisted priority = %d, want 7", got)
	}
}

func TestSetTaskSetPriorityUnknownID(t *testing.T) {
	root := t.TempDir()
	t.Setenv("XDG_DATA_HOME", root)
	d := DefaultDeps()
	defPath := root + "/tasks"
	seedIntentState(t, d, defPath, []RegisteredTaskSet{{ID: "demo", Priority: 3}})

	_, err := SetTaskSetPriority(d, defPath, "ghost", 7)
	if err == nil {
		t.Fatal("expected unknown-id error")
	}
	if !strings.Contains(err.Error(), `unknown task set "ghost"`) {
		t.Fatalf("err = %v", err)
	}
}

func TestSetTaskSetPriorityNotRegistered(t *testing.T) {
	root := t.TempDir()
	t.Setenv("XDG_DATA_HOME", root)
	d := DefaultDeps()
	defPath := root + "/tasks"
	// No state seeded: the definition path has no registered sets at all.

	_, err := SetTaskSetPriority(d, defPath, "demo", 7)
	if err == nil {
		t.Fatal("expected not-registered error")
	}
	if !strings.Contains(err.Error(), "no registered task sets") {
		t.Fatalf("err = %v", err)
	}
}

func TestSetTaskSetArchivedBatchAllOrNothing(t *testing.T) {
	root := t.TempDir()
	t.Setenv("XDG_DATA_HOME", root)
	d := DefaultDeps()
	defPath := root + "/tasks"
	canon, statePath := seedIntentState(t, d, defPath, []RegisteredTaskSet{
		{ID: "one"},
		{ID: "two"},
	})

	if err := SetTaskSetArchived(d, defPath, []string{"one", "two"}, true); err != nil {
		t.Fatal(err)
	}
	state, err := LoadGlobalStateWith(d, statePath)
	if err != nil {
		t.Fatal(err)
	}
	for _, set := range state.Tasks[canon].TaskSets {
		if !set.Archived {
			t.Fatalf("set %q not archived: %#v", set.ID, set)
		}
	}
}

func TestSetTaskSetArchivedUnknownIDFailsWholeBatch(t *testing.T) {
	root := t.TempDir()
	t.Setenv("XDG_DATA_HOME", root)
	d := DefaultDeps()
	defPath := root + "/tasks"
	canon, _ := seedIntentState(t, d, defPath, []RegisteredTaskSet{
		{ID: "one"},
		{ID: "two"},
	})
	before := registeredTaskSetsFor(t, d, canon)

	err := SetTaskSetArchived(d, defPath, []string{"one", "ghost"}, true)
	if err == nil {
		t.Fatal("expected unknown-id error")
	}
	if !strings.Contains(err.Error(), `unknown task set "ghost"`) {
		t.Fatalf("err = %v", err)
	}
	after := registeredTaskSetsFor(t, d, canon)
	if !reflect.DeepEqual(after, before) {
		t.Fatalf("batch wrote despite failure:\nbefore=%#v\nafter=%#v", before, after)
	}
}

func TestSetTaskSetArchivedEmptyWritesNothing(t *testing.T) {
	root := t.TempDir()
	t.Setenv("XDG_DATA_HOME", root)
	d := DefaultDeps()
	defPath := root + "/tasks"
	canon, _ := seedIntentState(t, d, defPath, []RegisteredTaskSet{{ID: "one"}})
	before := registeredTaskSetsFor(t, d, canon)

	if err := SetTaskSetArchived(d, defPath, nil, true); err != nil {
		t.Fatal(err)
	}
	after := registeredTaskSetsFor(t, d, canon)
	if !reflect.DeepEqual(after, before) {
		t.Fatalf("empty batch wrote:\nbefore=%#v\nafter=%#v", before, after)
	}
}

func TestSetTaskSetArchivedNotRegistered(t *testing.T) {
	root := t.TempDir()
	t.Setenv("XDG_DATA_HOME", root)
	d := DefaultDeps()
	defPath := root + "/tasks"

	err := SetTaskSetArchived(d, defPath, []string{"demo"}, true)
	if err == nil {
		t.Fatal("expected not-registered error")
	}
	if !strings.Contains(err.Error(), "no registered task sets") {
		t.Fatalf("err = %v", err)
	}
}

func TestToggleTaskSetAutoDrainFlipsAndPersists(t *testing.T) {
	root := t.TempDir()
	t.Setenv("XDG_DATA_HOME", root)
	d := DefaultDeps()
	defPath := root + "/tasks"
	canon, statePath := seedIntentState(t, d, defPath, []RegisteredTaskSet{{ID: "demo", AutoDrain: false}})

	next, err := ToggleTaskSetAutoDrain(d, defPath, "demo")
	if err != nil {
		t.Fatal(err)
	}
	if !next {
		t.Fatalf("first toggle = %v, want true", next)
	}
	state, err := LoadGlobalStateWith(d, statePath)
	if err != nil {
		t.Fatal(err)
	}
	if !state.Tasks[canon].TaskSets[0].AutoDrain {
		t.Fatalf("auto_drain not persisted: %#v", state.Tasks[canon].TaskSets[0])
	}

	next, err = ToggleTaskSetAutoDrain(d, defPath, "demo")
	if err != nil {
		t.Fatal(err)
	}
	if next {
		t.Fatalf("second toggle = %v, want false", next)
	}
}

func TestToggleTaskSetAutoDrainUnknownID(t *testing.T) {
	root := t.TempDir()
	t.Setenv("XDG_DATA_HOME", root)
	d := DefaultDeps()
	defPath := root + "/tasks"
	seedIntentState(t, d, defPath, []RegisteredTaskSet{{ID: "demo"}})

	_, err := ToggleTaskSetAutoDrain(d, defPath, "ghost")
	if err == nil {
		t.Fatal("expected unknown-id error")
	}
	if !strings.Contains(err.Error(), `unknown task set "ghost"`) {
		t.Fatalf("err = %v", err)
	}
}

func TestToggleTaskSetAutoDrainNotRegistered(t *testing.T) {
	root := t.TempDir()
	t.Setenv("XDG_DATA_HOME", root)
	d := DefaultDeps()
	defPath := root + "/tasks"

	_, err := ToggleTaskSetAutoDrain(d, defPath, "demo")
	if err == nil {
		t.Fatal("expected not-registered error")
	}
	if !strings.Contains(err.Error(), "no registered task sets") {
		t.Fatalf("err = %v", err)
	}
}

func TestSetTaskSetAutoDrainSetsAndPersists(t *testing.T) {
	root := t.TempDir()
	t.Setenv("XDG_DATA_HOME", root)
	d := DefaultDeps()
	defPath := root + "/tasks"
	canon, statePath := seedIntentState(t, d, defPath, []RegisteredTaskSet{{ID: "demo", AutoDrain: false}})

	changed, err := SetTaskSetAutoDrain(d, defPath, "demo", true)
	if err != nil {
		t.Fatal(err)
	}
	if !changed {
		t.Fatal("expected change when enabling auto-drain")
	}

	state, err := LoadGlobalStateWith(d, statePath)
	if err != nil {
		t.Fatal(err)
	}
	if !state.Tasks[canon].TaskSets[0].AutoDrain {
		t.Fatalf("auto_drain not persisted: %#v", state.Tasks[canon].TaskSets[0])
	}
}

func TestSetTaskSetAutoDrainIdempotentNoOp(t *testing.T) {
	root := t.TempDir()
	t.Setenv("XDG_DATA_HOME", root)
	d := DefaultDeps()
	defPath := root + "/tasks"
	canon, statePath := seedIntentState(t, d, defPath, []RegisteredTaskSet{{ID: "demo", AutoDrain: true}})
	before := registeredTaskSetsFor(t, d, canon)

	changed, err := SetTaskSetAutoDrain(d, defPath, "demo", true)
	if err != nil {
		t.Fatal(err)
	}
	if changed {
		t.Fatal("setting auto-drain to its current value should be a no-op")
	}
	after := registeredTaskSetsFor(t, d, canon)
	if !reflect.DeepEqual(after, before) {
		t.Fatalf("no-op wrote state:\nbefore=%#v\nafter=%#v", before, after)
	}

	changed, err = SetTaskSetAutoDrain(d, defPath, "demo", false)
	if err != nil {
		t.Fatal(err)
	}
	if !changed {
		t.Fatal("expected change when clearing auto-drain")
	}

	changed, err = SetTaskSetAutoDrain(d, defPath, "demo", false)
	if err != nil {
		t.Fatal(err)
	}
	if changed {
		t.Fatal("repeat clear should be a no-op")
	}
	state, err := LoadGlobalStateWith(d, statePath)
	if err != nil {
		t.Fatal(err)
	}
	if state.Tasks[canon].TaskSets[0].AutoDrain {
		t.Fatalf("auto_drain should stay cleared: %#v", state.Tasks[canon].TaskSets[0])
	}
}

func TestSetTaskSetAutoDrainUnknownID(t *testing.T) {
	root := t.TempDir()
	t.Setenv("XDG_DATA_HOME", root)
	d := DefaultDeps()
	defPath := root + "/tasks"
	seedIntentState(t, d, defPath, []RegisteredTaskSet{{ID: "demo"}})

	_, err := SetTaskSetAutoDrain(d, defPath, "ghost", false)
	if err == nil {
		t.Fatal("expected unknown-id error")
	}
	if !strings.Contains(err.Error(), `unknown task set "ghost"`) {
		t.Fatalf("err = %v", err)
	}
}
