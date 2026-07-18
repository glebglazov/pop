package binding

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/glebglazov/pop/tasks"
)

// legacyBinding mirrors one bindings.json record so a test can hand-write the
// retired file in its real on-disk shape (NUL-separated scoped keys included).
type legacyBinding struct {
	RuntimePath string `json:"runtime_path"`
	Branch      string `json:"branch,omitempty"`
	Project     string `json:"project,omitempty"`
	Provisioned bool   `json:"provisioned,omitempty"`
}

func writeLegacyBindingsFile(t *testing.T, d *tasks.Deps, bindings map[string]legacyBinding) {
	t.Helper()
	payload, err := json.Marshal(map[string]any{"bindings": bindings})
	if err != nil {
		t.Fatalf("marshal legacy bindings: %v", err)
	}
	path := LegacyBindingsPath(d)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir data dir: %v", err)
	}
	if err := os.WriteFile(path, payload, 0o644); err != nil {
		t.Fatalf("write legacy file: %v", err)
	}
}

// TestMigrateLegacyBindingsFile verifies a surviving bindings.json is folded
// into the store with every binding's provisioned bit preserved, and the file
// is retired afterwards (the data must not be lost).
func TestMigrateLegacyBindingsFile(t *testing.T) {
	d := bindingTestDeps(t)
	managedKey := "repo-abc\x00set-managed"
	adoptedKey := "repo-abc\x00set-adopted"
	writeLegacyBindingsFile(t, d, map[string]legacyBinding{
		managedKey: {RuntimePath: "/wt/managed", Branch: "pop/set-managed/x", Project: "proj", Provisioned: true},
		adoptedKey: {RuntimePath: "/wt/adopted", Branch: "feature", Project: "proj"},
	})

	store, err := Load(d)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if len(store.Bindings) != 2 {
		t.Fatalf("bindings = %+v, want 2", store.Bindings)
	}
	managed, ok := store.Get(managedKey)
	if !ok || !managed.Provisioned || managed.RuntimePath != "/wt/managed" {
		t.Fatalf("managed binding = %+v, want provisioned /wt/managed", managed)
	}
	adopted, ok := store.Get(adoptedKey)
	if !ok || adopted.Provisioned || adopted.RuntimePath != "/wt/adopted" {
		t.Fatalf("adopted binding = %+v, want adopted /wt/adopted", adopted)
	}

	// The file is retired once its contents are safely in the store.
	if _, err := os.Stat(LegacyBindingsPath(d)); !os.IsNotExist(err) {
		t.Fatalf("legacy bindings file still present: stat err = %v", err)
	}

	// A second load is a no-op that still returns the migrated bindings.
	again, err := Load(d)
	if err != nil || len(again.Bindings) != 2 {
		t.Fatalf("reload bindings = %+v err = %v, want 2 entries", again, err)
	}
}

// TestMigrateLegacyBindingsFileStoreWins verifies a binding already present in
// the store is not clobbered by a stale entry left in bindings.json.
func TestMigrateLegacyBindingsFileStoreWins(t *testing.T) {
	d := bindingTestDeps(t)
	key := "repo-abc\x00set-1"
	store := &Store{}
	store.Put(key, Binding{RuntimePath: "/wt/current", Provisioned: true})
	if err := Save(d, store); err != nil {
		t.Fatalf("seed store: %v", err)
	}
	writeLegacyBindingsFile(t, d, map[string]legacyBinding{
		key: {RuntimePath: "/wt/stale"},
	})

	loaded, err := Load(d)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	got, ok := loaded.Get(key)
	if !ok || got.RuntimePath != "/wt/current" || !got.Provisioned {
		t.Fatalf("binding = %+v, want store value /wt/current provisioned", got)
	}
}

// TestBindingStoreRoundTrip exercises the store-backed put/delete/replace path
// the binding façade rides on.
func TestBindingStoreRoundTrip(t *testing.T) {
	d := bindingTestDeps(t)
	key := "repo-xyz\x00set-9"

	store := &Store{}
	store.Put(key, Binding{RuntimePath: "/wt/9", Branch: "b", Project: "p", Provisioned: true})
	if err := Save(d, store); err != nil {
		t.Fatalf("put: %v", err)
	}
	loaded, err := Load(d)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if got, _ := loaded.Get(key); got.RuntimePath != "/wt/9" {
		t.Fatalf("after put binding = %+v", got)
	}

	if err := Save(d, &Store{Bindings: map[string]Binding{key: {RuntimePath: "/wt/9b"}}}); err != nil {
		t.Fatalf("save: %v", err)
	}
	loaded, err = Load(d)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if got, _ := loaded.Get(key); len(loaded.Bindings) != 1 || got.RuntimePath != "/wt/9b" || got.Provisioned {
		t.Fatalf("after replace bindings = %+v", loaded.Bindings)
	}

	loaded.Delete(key)
	if err := Save(d, loaded); err != nil {
		t.Fatalf("save after delete: %v", err)
	}
	final, err := Load(d)
	if err != nil || len(final.Bindings) != 0 {
		t.Fatalf("after delete bindings = %+v err = %v, want empty", final.Bindings, err)
	}
}
