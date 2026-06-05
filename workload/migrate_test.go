package workload

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/glebglazov/pop/internal/deps"
)

// recordingGit reports a fixed common directory and toplevel while recording every
// git invocation so tests can assert no configuration or ignore probing occurred.
type recordingGit struct {
	commonDir string
	toplevel  string
	mu        sync.Mutex
	calls     [][]string
}

func (g *recordingGit) Command(args ...string) (string, error) {
	return g.CommandInDir("", args...)
}

func (g *recordingGit) CommandInDir(dir string, args ...string) (string, error) {
	g.mu.Lock()
	g.calls = append(g.calls, append([]string{}, args...))
	g.mu.Unlock()
	if len(args) >= 2 && args[0] == "rev-parse" {
		switch args[1] {
		case "--git-common-dir":
			return g.commonDir, nil
		case "--show-toplevel":
			return g.toplevel, nil
		}
	}
	return "", nil
}

// migrateEnv wires a worktree, data home, and recording git for migration tests.
type migrateEnv struct {
	root     string
	worktree string
	dataHome string
	git      *recordingGit
	deps     *Deps
}

func newMigrateEnv(t *testing.T) *migrateEnv {
	t.Helper()
	root := t.TempDir()
	worktree := filepath.Join(root, "repo")
	commonDir := filepath.Join(worktree, ".git")
	dataHome := filepath.Join(root, "data")
	if err := os.MkdirAll(commonDir, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("XDG_DATA_HOME", dataHome)

	git := &recordingGit{commonDir: commonDir, toplevel: worktree}
	d := &Deps{
		FS:     deps.NewRealFileSystem(),
		Git:    git,
		Runner: RealCommandRunner{},
	}
	return &migrateEnv{root: root, worktree: worktree, dataHome: dataHome, git: git, deps: d}
}

// writeLegacySet creates a legacy thoughts/issues/<id> set with a manifest, a markdown,
// and a progress record.
func (e *migrateEnv) writeLegacySet(t *testing.T, id string) {
	t.Helper()
	dir := filepath.Join(e.worktree, "thoughts", "issues", id)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	files := map[string]string{
		"index.json":   `{"version":1}`,
		"01-thing.md":  "# Thing\n",
		"progress.txt": "done 01-thing.md\n",
	}
	for name, body := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

func (e *migrateEnv) legacyKey(t *testing.T) string {
	t.Helper()
	key, err := NormalizeProjectPathWith(e.deps, e.worktree)
	if err != nil {
		t.Fatal(err)
	}
	return key
}

func (e *migrateEnv) storageKey(t *testing.T) string {
	t.Helper()
	id, err := ResolveRepositoryIdentity(e.deps, e.worktree)
	if err != nil {
		t.Fatal(err)
	}
	key, err := CanonicalDefinitionPathWith(e.deps, id.IssuesDir)
	if err != nil {
		t.Fatal(err)
	}
	return key
}

func (e *migrateEnv) seedState(t *testing.T, key string, sets []RegisteredIssueSet) {
	t.Helper()
	statePath := DefaultStatePathWith(e.deps)
	state := &GlobalState{
		Version:   StateVersion,
		Workloads: map[string]*WorkloadEntry{key: {IssueSets: sets}},
	}
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(statePath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(statePath, data, 0o644); err != nil {
		t.Fatal(err)
	}
}

func (e *migrateEnv) loadState(t *testing.T) *GlobalState {
	t.Helper()
	state, err := LoadGlobalStateWith(e.deps, DefaultStatePathWith(e.deps))
	if err != nil {
		t.Fatal(err)
	}
	return state
}

func TestMigrateMovesSetsAndRekeysState(t *testing.T) {
	e := newMigrateEnv(t)
	e.writeLegacySet(t, "set-a")
	e.writeLegacySet(t, "set-b")
	legacyKey := e.legacyKey(t)
	e.seedState(t, legacyKey, []RegisteredIssueSet{
		{ID: "set-a", Priority: 5},
		{ID: "set-b", Priority: 2},
	})

	result, err := Migrate(e.deps, e.worktree)
	if err != nil {
		t.Fatal(err)
	}

	if strings.Join(result.Migrated, ",") != "set-a,set-b" {
		t.Fatalf("migrated = %v, want [set-a set-b]", result.Migrated)
	}
	if len(result.Skipped) != 0 {
		t.Fatalf("skipped = %v, want none", result.Skipped)
	}

	// Files moved intact into storage.
	for _, name := range []string{"index.json", "01-thing.md", "progress.txt"} {
		moved := filepath.Join(result.StorageDir, "set-a", name)
		if _, err := os.Stat(moved); err != nil {
			t.Fatalf("expected %s in storage: %v", name, err)
		}
	}
	// Legacy directory is gone.
	if _, err := os.Stat(filepath.Join(e.worktree, "thoughts", "issues", "set-a")); !os.IsNotExist(err) {
		t.Fatalf("legacy set-a still present: %v", err)
	}

	// State rekeyed to storage path, priorities and order preserved.
	state := e.loadState(t)
	if _, ok := state.Workloads[legacyKey]; ok {
		t.Fatal("legacy state key still present after full migration")
	}
	entry := state.Workloads[e.storageKey(t)]
	if entry == nil {
		t.Fatal("storage state key missing")
	}
	if len(entry.IssueSets) != 2 {
		t.Fatalf("storage entry sets = %v", entry.IssueSets)
	}
	if entry.IssueSets[0].ID != "set-a" || entry.IssueSets[0].Priority != 5 {
		t.Fatalf("set-a registration not preserved: %+v", entry.IssueSets[0])
	}
	if entry.IssueSets[1].ID != "set-b" || entry.IssueSets[1].Priority != 2 {
		t.Fatalf("set-b registration not preserved: %+v", entry.IssueSets[1])
	}
}

func TestMigrateCollisionSkips(t *testing.T) {
	e := newMigrateEnv(t)
	e.writeLegacySet(t, "set-a")
	e.writeLegacySet(t, "set-collide")

	// Pre-create the colliding set in storage with distinct content.
	id, err := ResolveRepositoryIdentity(e.deps, e.worktree)
	if err != nil {
		t.Fatal(err)
	}
	if err := EnsureStorage(e.deps, id); err != nil {
		t.Fatal(err)
	}
	existing := filepath.Join(id.IssuesDir, "set-collide")
	if err := os.MkdirAll(existing, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(existing, "index.json"), []byte(`{"storage":true}`), 0o644); err != nil {
		t.Fatal(err)
	}

	result, err := Migrate(e.deps, e.worktree)
	if err != nil {
		t.Fatal(err)
	}

	if strings.Join(result.Migrated, ",") != "set-a" {
		t.Fatalf("migrated = %v, want [set-a]", result.Migrated)
	}
	if strings.Join(result.Skipped, ",") != "set-collide" {
		t.Fatalf("skipped = %v, want [set-collide]", result.Skipped)
	}

	// Storage copy of the colliding set is untouched (not merged/overwritten).
	data, err := os.ReadFile(filepath.Join(existing, "index.json"))
	if err != nil || !strings.Contains(string(data), "storage") {
		t.Fatalf("storage set-collide overwritten: %q err=%v", data, err)
	}
	// Skipped set remains in the legacy tree.
	if _, err := os.Stat(filepath.Join(e.worktree, "thoughts", "issues", "set-collide")); err != nil {
		t.Fatalf("skipped legacy set removed: %v", err)
	}
	// thoughts/ retained because it still holds the skipped set.
	if result.ThoughtsRemoved {
		t.Fatal("thoughts removed despite remaining skipped set")
	}
	if _, err := os.Stat(filepath.Join(e.worktree, "thoughts")); err != nil {
		t.Fatalf("thoughts wrongly removed: %v", err)
	}
}

func TestMigrateRemovesEmptyThoughts(t *testing.T) {
	e := newMigrateEnv(t)
	e.writeLegacySet(t, "set-a")

	result, err := Migrate(e.deps, e.worktree)
	if err != nil {
		t.Fatal(err)
	}
	if !result.ThoughtsRemoved {
		t.Fatal("expected thoughts/ removed when left empty")
	}
	if _, err := os.Stat(filepath.Join(e.worktree, "thoughts")); !os.IsNotExist(err) {
		t.Fatalf("thoughts still present: %v", err)
	}
}

func TestMigrateKeepsNonEmptyThoughts(t *testing.T) {
	e := newMigrateEnv(t)
	e.writeLegacySet(t, "set-a")
	// A sibling note under thoughts/ must keep the directory alive.
	if err := os.WriteFile(filepath.Join(e.worktree, "thoughts", "notes.md"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	result, err := Migrate(e.deps, e.worktree)
	if err != nil {
		t.Fatal(err)
	}
	if result.ThoughtsRemoved {
		t.Fatal("thoughts removed despite sibling content")
	}
	if _, err := os.Stat(filepath.Join(e.worktree, "thoughts", "notes.md")); err != nil {
		t.Fatalf("sibling note lost: %v", err)
	}
	// The emptied issues subdir is pruned even though thoughts/ survives.
	if _, err := os.Stat(filepath.Join(e.worktree, "thoughts", "issues")); !os.IsNotExist(err) {
		t.Fatalf("empty issues dir not pruned: %v", err)
	}
}

func TestMigrateNoOp(t *testing.T) {
	e := newMigrateEnv(t)

	result, err := Migrate(e.deps, e.worktree)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Migrated) != 0 || len(result.Skipped) != 0 {
		t.Fatalf("expected clean no-op, got %+v", result)
	}
	if result.ThoughtsRemoved {
		t.Fatal("nothing to remove, but reported removal")
	}

	// A second run is equally a clean no-op.
	result, err = Migrate(e.deps, e.worktree)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Migrated) != 0 || len(result.Skipped) != 0 {
		t.Fatalf("second run not a no-op: %+v", result)
	}
}

func TestMigrateNeverTouchesGitConfigOrIgnore(t *testing.T) {
	e := newMigrateEnv(t)
	e.writeLegacySet(t, "set-a")

	if _, err := Migrate(e.deps, e.worktree); err != nil {
		t.Fatal(err)
	}

	for _, call := range e.git.calls {
		joined := strings.Join(call, " ")
		for _, banned := range []string{"config", "check-ignore", "ignore", "exclude"} {
			if strings.Contains(joined, banned) {
				t.Fatalf("migration ran forbidden git command: %v", call)
			}
		}
	}
}
