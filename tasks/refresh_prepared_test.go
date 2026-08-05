package tasks

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/glebglazov/pop/internal/deps"
)

// TestPrepareRefreshesMigratesBeforeAnyRead pins the hoist ADR-0189 asks for: the
// storage-layout migration is a write, and it is finished by the time
// PrepareRefreshes returns — before the caller fans anything out. The assertions
// run between the preparation and the refresh, which is the only place the
// ordering is observable.
func TestPrepareRefreshesMigratesBeforeAnyRead(t *testing.T) {
	e := newOldLayoutEnv(t)
	e.writeOldSet(t, "set-a")
	e.writeOldMarker(t)
	e.seedOldGlobalState(t, []RegisteredTaskSet{{ID: "set-a", Priority: 7}})

	id, err := ResolveRepositoryIdentity(e.deps, e.worktree)
	if err != nil {
		t.Fatal(err)
	}

	prepared, err := PrepareRefreshes(e.deps, []string{id.TasksDir})
	if err != nil {
		t.Fatalf("PrepareRefreshes: %v", err)
	}
	if _, err := os.Stat(e.oldStorageDir); !os.IsNotExist(err) {
		t.Fatalf("legacy storage still present after preparation: %v", err)
	}
	if _, err := os.Stat(id.TasksDir); err != nil {
		t.Fatalf("migrated tasks directory missing after preparation: %v", err)
	}

	result, err := prepared[0].Refresh(e.deps)
	if err != nil {
		t.Fatalf("Refresh: %v", err)
	}
	if len(result.Rows) != 1 || result.Rows[0].ID != "set-a" {
		t.Fatalf("rows = %#v, want the migrated set", result.Rows)
	}
	if result.Rows[0].Priority != 7 {
		t.Fatalf("priority = %d, want 7 preserved through migration", result.Rows[0].Priority)
	}
}

// TestPreparedRefreshReadsNoStore pins the other half of the hoist: a prepared
// refresh answers from the registration the preparation read, so nothing it does
// speaks to pop's single store connection. The proof is to take the store away
// between the two — a refresh that still lists the registered set cannot have
// gone looking for one.
func TestPreparedRefreshReadsNoStore(t *testing.T) {
	dataHome := t.TempDir()
	current := dataHome
	d := &Deps{
		FS:  movableDataHomeFS(&current),
		Git: &deps.MockGit{},
	}
	t.Cleanup(func() { _ = d.CloseStore() })

	defPath := filepath.Join(t.TempDir(), "tasks")
	writePreparedSet(t, defPath, "set-a")
	if _, err := RegisterWith(d, defPath, StatePathFor(defPath)); err != nil {
		t.Fatalf("RegisterWith: %v", err)
	}

	prepared, err := PrepareRefreshes(d, []string{defPath})
	if err != nil {
		t.Fatalf("PrepareRefreshes: %v", err)
	}

	// Drop the store beneath the deps: the cached handle goes, and the data dir
	// the next open would resolve holds no database at all.
	if err := d.CloseStore(); err != nil {
		t.Fatalf("CloseStore: %v", err)
	}
	current = t.TempDir()

	result, err := prepared[0].Refresh(d)
	if err != nil {
		t.Fatalf("Refresh: %v", err)
	}
	if len(result.Rows) != 1 || result.Rows[0].ID != "set-a" {
		t.Fatalf("rows = %#v, want the set the preparation registered", result.Rows)
	}

	// The control: with the store gone, a refresh that does read it sees no
	// registration at all. Without this the assertion above would pass on a
	// machine where the store was never needed.
	unprepared, err := RefreshWith(d, defPath, StatePathFor(defPath))
	if err != nil {
		t.Fatalf("RefreshWith: %v", err)
	}
	if len(unprepared.Rows) != 0 {
		t.Fatalf("store-reading refresh returned %#v with the store removed — the fixture is not cutting it off", unprepared.Rows)
	}
}

// movableDataHomeFS is a real filesystem whose XDG_DATA_HOME answer a test can
// move mid-run, which is how the store is taken away between two reads.
func movableDataHomeFS(dataHome *string) deps.FileSystem {
	real := deps.NewRealFileSystem()
	return &deps.MockFileSystem{
		GetenvFunc: func(key string) string {
			if key == "XDG_DATA_HOME" {
				return *dataHome
			}
			return ""
		},
		GetwdFunc:        real.Getwd,
		UserHomeDirFunc:  func() (string, error) { return *dataHome, nil },
		StatFunc:         real.Stat,
		ReadDirFunc:      real.ReadDir,
		ReadFileFunc:     real.ReadFile,
		WriteFileFunc:    real.WriteFile,
		MkdirAllFunc:     real.MkdirAll,
		RenameFunc:       real.Rename,
		RemoveAllFunc:    real.RemoveAll,
		DirFSFunc:        real.DirFS,
		EvalSymlinksFunc: real.EvalSymlinks,
	}
}

// writePreparedSet writes one valid task set under defPath.
func writePreparedSet(t *testing.T, defPath, setID string) {
	t.Helper()
	dir := filepath.Join(defPath, setID)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	files := map[string]string{
		"index.json":  `{"tasks":[{"id":"01-thing","file":"01-thing.md","title":"T","type":"AFK","status":"open","blocked_by":[]}]}`,
		"01-thing.md": "# Thing\n\n## Acceptance criteria\n\n- [ ] do it\n",
	}
	for name, body := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}
