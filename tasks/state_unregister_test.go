package tasks

import (
	"testing"
)

func TestRemoveRegisteredTaskSetsDropsOnlyNamedSets(t *testing.T) {
	t.Parallel()
	d := newTestDeps(t)
	raw := t.TempDir()
	defPath, err := CanonicalDefinitionPathWith(d, raw)
	if err != nil {
		t.Fatal(err)
	}
	statePath := StatePathFor(defPath)

	if err := UpdateGlobalStateWith(d, statePath, func(state *GlobalState) error {
		entry := state.Entry(defPath)
		entry.TaskSets = []RegisteredTaskSet{{ID: "keep"}, {ID: "drop"}}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if err := RemoveRegisteredTaskSets(d, defPath, []string{"drop"}); err != nil {
		t.Fatal(err)
	}
	loaded, err := LoadGlobalStateWith(d, statePath)
	if err != nil {
		t.Fatal(err)
	}
	entry := loaded.Tasks[defPath]
	if len(entry.TaskSets) != 1 || entry.TaskSets[0].ID != "keep" {
		t.Fatalf("task sets = %+v, want only keep", entry.TaskSets)
	}
}
