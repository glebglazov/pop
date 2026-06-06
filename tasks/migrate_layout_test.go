package tasks

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// oldLayoutEnv extends migrateEnv with helpers that build a pre-rename storage
// layout (workloads/<repo>-<hash>/tasks, task-keyed manifests, global
// workloads-state.json) for the repository the env resolves to.
type oldLayoutEnv struct {
	*migrateEnv
	oldStorageDir string
	oldTasksDir   string
}

func newOldLayoutEnv(t *testing.T) *oldLayoutEnv {
	t.Helper()
	e := newMigrateEnv(t)
	id, err := ResolveRepositoryIdentity(e.deps, e.worktree)
	if err != nil {
		t.Fatal(err)
	}
	oldStorageDir := filepath.Join(e.dataHome, "pop", legacyStorageParent, filepath.Base(id.StorageDir))
	return &oldLayoutEnv{
		migrateEnv:    e,
		oldStorageDir: oldStorageDir,
		oldTasksDir:   filepath.Join(oldStorageDir, legacyTasksSubdir),
	}
}

// writeOldSet creates a valid task-keyed set under the old tasks directory.
func (e *oldLayoutEnv) writeOldSet(t *testing.T, id string) {
	t.Helper()
	dir := filepath.Join(e.oldTasksDir, id)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	files := map[string]string{
		"index.json":   `{"issues":[{"id":"01-thing","file":"01-thing.md","title":"T","type":"AFK","status":"open","blocked_by":[]}]}`,
		"01-thing.md":  "# Thing\n\n## Acceptance criteria\n\n- [ ] do it\n",
		"progress.txt": "done 01-thing.md\n",
	}
	for name, body := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

func (e *oldLayoutEnv) writeOldMarker(t *testing.T) {
	t.Helper()
	if err := os.MkdirAll(e.oldStorageDir, 0o755); err != nil {
		t.Fatal(err)
	}
	marker := RepoMarker{RepositoryPath: e.git.commonDir}
	data, err := json.MarshalIndent(marker, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(e.oldStorageDir, repoMarkerFile), data, 0o644); err != nil {
		t.Fatal(err)
	}
}

// seedOldGlobalState writes the legacy global state entry keyed by the old tasks
// directory, the way the pre-rename layout recorded registrations.
func (e *oldLayoutEnv) seedOldGlobalState(t *testing.T, sets []RegisteredTaskSet) {
	t.Helper()
	key, err := CanonicalDefinitionPathWith(e.deps, e.oldTasksDir)
	if err != nil {
		t.Fatal(err)
	}
	e.seedState(t, key, sets)
}

func TestMigrateStorageLayoutMovesEverything(t *testing.T) {
	e := newOldLayoutEnv(t)
	e.writeOldSet(t, "set-a")
	e.writeOldSet(t, "set-b")
	e.writeOldMarker(t)
	e.seedOldGlobalState(t, []RegisteredTaskSet{
		{ID: "set-a", Priority: 5},
		{ID: "set-b", Priority: 2},
	})

	id, err := ResolveRepositoryIdentity(e.deps, e.worktree)
	if err != nil {
		t.Fatal(err)
	}

	mig, err := MigrateStorageLayout(e.deps, id.TasksDir)
	if err != nil {
		t.Fatal(err)
	}
	if mig == nil {
		t.Fatal("expected a migration summary")
	}
	if strings.Join(mig.MovedSets, ",") != "set-a,set-b" {
		t.Fatalf("moved sets = %v", mig.MovedSets)
	}

	// Old layout is gone.
	if _, err := os.Stat(e.oldStorageDir); !os.IsNotExist(err) {
		t.Fatalf("old storage dir still present: %v", err)
	}

	// Set artifacts moved into tasks/, progress and markdown intact.
	for _, name := range []string{"index.json", "01-thing.md", "progress.txt"} {
		if _, err := os.Stat(filepath.Join(id.TasksDir, "set-a", name)); err != nil {
			t.Fatalf("expected %s under tasks/: %v", name, err)
		}
	}
	// repo.json moved alongside.
	if _, err := os.Stat(filepath.Join(id.StorageDir, repoMarkerFile)); err != nil {
		t.Fatalf("repo.json not preserved: %v", err)
	}

	// Manifest rewritten to the "tasks" key.
	data, err := os.ReadFile(filepath.Join(id.TasksDir, "set-a", "index.json"))
	if err != nil {
		t.Fatal(err)
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatal(err)
	}
	if _, ok := raw["tasks"]; !ok {
		t.Fatalf("manifest missing tasks key: %s", data)
	}
	if _, ok := raw["issues"]; ok {
		t.Fatalf("manifest still has tasks key: %s", data)
	}

	// State rekeyed into the per-repository state.json, priority and order preserved.
	newKey, err := CanonicalDefinitionPathWith(e.deps, id.TasksDir)
	if err != nil {
		t.Fatal(err)
	}
	repoState, err := LoadGlobalStateWith(e.deps, StatePathFor(id.TasksDir))
	if err != nil {
		t.Fatal(err)
	}
	entry := repoState.Tasks[newKey]
	if entry == nil || len(entry.TaskSets) != 2 {
		t.Fatalf("per-repo state entry = %#v", entry)
	}
	if entry.TaskSets[0].ID != "set-a" || entry.TaskSets[0].Priority != 5 {
		t.Fatalf("set-a registration lost: %+v", entry.TaskSets[0])
	}
	if entry.TaskSets[1].ID != "set-b" || entry.TaskSets[1].Priority != 2 {
		t.Fatalf("set-b registration lost: %+v", entry.TaskSets[1])
	}

	// Legacy global key drained.
	oldKey, err := CanonicalDefinitionPathWith(e.deps, e.oldTasksDir)
	if err != nil {
		t.Fatal(err)
	}
	global := e.loadGlobalState(t)
	if _, ok := global.Tasks[oldKey]; ok {
		t.Fatal("legacy global state key still present after migration")
	}
}

func TestMigrateStorageLayoutIdempotent(t *testing.T) {
	e := newOldLayoutEnv(t)
	e.writeOldSet(t, "set-a")
	e.writeOldMarker(t)
	e.seedOldGlobalState(t, []RegisteredTaskSet{{ID: "set-a", Priority: 3}})

	id, err := ResolveRepositoryIdentity(e.deps, e.worktree)
	if err != nil {
		t.Fatal(err)
	}

	if _, err := MigrateStorageLayout(e.deps, id.TasksDir); err != nil {
		t.Fatal(err)
	}

	// A second migration is a clean no-op: no summary, no further moves.
	mig, err := MigrateStorageLayout(e.deps, id.TasksDir)
	if err != nil {
		t.Fatal(err)
	}
	if mig != nil {
		t.Fatalf("second migration reported moves: %#v", mig)
	}
	if _, err := os.Stat(e.oldStorageDir); !os.IsNotExist(err) {
		t.Fatalf("old storage dir reappeared: %v", err)
	}
}

func TestMigrateStorageLayoutViaRefreshRendersSets(t *testing.T) {
	e := newOldLayoutEnv(t)
	e.writeOldSet(t, "set-a")
	e.writeOldMarker(t)
	e.seedOldGlobalState(t, []RegisteredTaskSet{{ID: "set-a", Priority: 7}})

	id, err := ResolveRepositoryIdentity(e.deps, e.worktree)
	if err != nil {
		t.Fatal(err)
	}
	// Production resolves the definition path canonically (resolveDefinitionPath);
	// mirror that so the state path matches what migration writes.
	defPath, err := CanonicalDefinitionPathWith(e.deps, id.TasksDir)
	if err != nil {
		t.Fatal(err)
	}

	// First command touch auto-migrates and renders the migrated set.
	result, err := RefreshWith(e.deps, defPath, StatePathFor(defPath))
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Rows) != 1 || result.Rows[0].ID != "set-a" {
		t.Fatalf("rows = %#v", result.Rows)
	}
	// No fresh registration occurred — the set was already registered pre-migration.
	if len(result.NewRegistrations) != 0 {
		t.Fatalf("unexpected new registrations: %v", result.NewRegistrations)
	}
	if result.Rows[0].Priority != 7 {
		t.Fatalf("priority not preserved through migration: %d", result.Rows[0].Priority)
	}
}

func TestMigrateStorageLayoutNoOldLayout(t *testing.T) {
	e := newOldLayoutEnv(t)
	id, err := ResolveRepositoryIdentity(e.deps, e.worktree)
	if err != nil {
		t.Fatal(err)
	}
	mig, err := MigrateStorageLayout(e.deps, id.TasksDir)
	if err != nil {
		t.Fatal(err)
	}
	if mig != nil {
		t.Fatalf("migration ran with no old layout: %#v", mig)
	}
}
