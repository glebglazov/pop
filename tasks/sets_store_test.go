package tasks

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/glebglazov/pop/internal/deps"
)

// writeLegacyStateFile hand-writes a retired per-repository state.json at
// statePath, in its real on-disk shape, so a test can exercise the one-time fold
// into the store on first read. The fold keys off the definition path stored
// inside the file.
func writeLegacyStateFile(t *testing.T, statePath string, tasks map[string]*TaskEntry) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(statePath), 0o755); err != nil {
		t.Fatal(err)
	}
	payload, err := json.MarshalIndent(&GlobalState{Version: StateVersion, Tasks: tasks}, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(statePath, payload, 0o644); err != nil {
		t.Fatal(err)
	}
}

// TestMigrateLegacyStateFilePreservesBitsAndOrder verifies the fold preserves
// every registration's priority, archived, and auto-drain bit exactly and keeps
// registration order, then retires the file (the data must not be lost).
func TestMigrateLegacyStateFilePreservesBitsAndOrder(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	d := &Deps{FS: deps.NewRealFileSystem()}
	defPath := filepath.Join(t.TempDir(), "alpha", "tasks")
	statePath := StatePathFor(defPath)
	writeLegacyStateFile(t, statePath, map[string]*TaskEntry{
		defPath: {TaskSets: []RegisteredTaskSet{
			{ID: "first", Priority: 5, Archived: false, AutoDrain: true},
			{ID: "second", Priority: 0, Archived: true, AutoDrain: false},
			{ID: "third", Priority: 9, Archived: false, AutoDrain: false},
		}},
	})

	state, err := LoadGlobalStateWith(d, statePath)
	if err != nil {
		t.Fatal(err)
	}
	want := []RegisteredTaskSet{
		{ID: "first", Priority: 5, AutoDrain: true},
		{ID: "second", Priority: 0, Archived: true},
		{ID: "third", Priority: 9},
	}
	if got := state.Tasks[defPath].TaskSets; !reflect.DeepEqual(got, want) {
		t.Fatalf("folded registration = %#v, want %#v", got, want)
	}

	if _, err := os.Stat(statePath); !os.IsNotExist(err) {
		t.Fatalf("legacy state.json still present: stat err = %v", err)
	}

	// A second load is a no-op that still returns the migrated registration.
	again, err := LoadGlobalStateWith(d, StatePathFor(defPath))
	if err != nil {
		t.Fatal(err)
	}
	if got := again.Tasks[defPath].TaskSets; !reflect.DeepEqual(got, want) {
		t.Fatalf("reload registration = %#v, want %#v", got, want)
	}
}

// TestMigrateLegacyStateFileStoreWins verifies a (def, set) already in the store
// is not clobbered by a stale entry left in a surviving state.json.
func TestMigrateLegacyStateFileStoreWins(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	d := &Deps{FS: deps.NewRealFileSystem()}
	defPath := filepath.Join(t.TempDir(), "beta", "tasks")
	statePath := StatePathFor(defPath)

	if err := UpdateGlobalStateWith(d, statePath, func(s *GlobalState) error {
		s.Entry(defPath).TaskSets = []RegisteredTaskSet{{ID: "shared", Priority: 7, AutoDrain: true}}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	writeLegacyStateFile(t, statePath, map[string]*TaskEntry{
		defPath: {TaskSets: []RegisteredTaskSet{{ID: "shared", Priority: 1, AutoDrain: false}}},
	})

	state, err := LoadGlobalStateWith(d, statePath)
	if err != nil {
		t.Fatal(err)
	}
	got := state.Tasks[defPath].TaskSets
	if len(got) != 1 || got[0].Priority != 7 || !got[0].AutoDrain {
		t.Fatalf("store value not preserved over stale file: %#v", got)
	}
}

// TestRegisterWritesIntoStoreTable verifies explicit registration writes a
// newly-seen set's registration (auto-drain seeded from the manifest) into the
// sets table, read back from the store directly.
func TestRegisterWritesIntoStoreTable(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	root := t.TempDir()
	taskDir := filepath.Join(root, "tbl-set")
	writeTaskMD(t, taskDir, "01-a.md", "## Acceptance criteria\n\n- [ ] ok\n")
	writeManifestWithSetKeys(t, taskDir, []Task{
		{ID: "01-a", File: "01-a.md", Title: "A", Type: "AFK", Status: "open"},
	}, map[string]any{"auto_drain": true})

	d := DefaultDeps()
	result, err := RegisterWith(d, root, StatePathFor(root))
	if err != nil {
		t.Fatal(err)
	}
	canon := result.DefinitionPath

	s, err := openDrainStore(d)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = s.Close() }()
	all, err := s.AllSets()
	if err != nil {
		t.Fatal(err)
	}
	regs := all[canon]
	if len(regs) != 1 || regs[0].SetID != "tbl-set" || !regs[0].AutoDrain {
		t.Fatalf("sets table = %#v, want one auto-drain tbl-set under %s", all, canon)
	}
}
