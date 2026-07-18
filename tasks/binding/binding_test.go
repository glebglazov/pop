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

	all, err := AllBindings(d)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if len(all) != 2 {
		t.Fatalf("bindings = %+v, want 2", all)
	}
	managed, ok, err := Lookup(d, managedKey)
	if err != nil || !ok || !managed.Provisioned || managed.RuntimePath != "/wt/managed" {
		t.Fatalf("managed binding = %+v ok=%v err=%v, want provisioned /wt/managed", managed, ok, err)
	}
	adopted, ok, err := Lookup(d, adoptedKey)
	if err != nil || !ok || adopted.Provisioned || adopted.RuntimePath != "/wt/adopted" {
		t.Fatalf("adopted binding = %+v ok=%v err=%v, want adopted /wt/adopted", adopted, ok, err)
	}

	// The file is retired once its contents are safely in the store.
	if _, err := os.Stat(LegacyBindingsPath(d)); !os.IsNotExist(err) {
		t.Fatalf("legacy bindings file still present: stat err = %v", err)
	}

	// A second load is a no-op that still returns the migrated bindings.
	again, err := AllBindings(d)
	if err != nil || len(again) != 2 {
		t.Fatalf("reload bindings = %+v err = %v, want 2 entries", again, err)
	}
}

// TestMigrateLegacyBindingsFileStoreWins verifies a binding already present in
// the store is not clobbered by a stale entry left in bindings.json.
func TestMigrateLegacyBindingsFileStoreWins(t *testing.T) {
	d := bindingTestDeps(t)
	key := "repo-abc\x00set-1"
	if err := Put(d, key, Binding{RuntimePath: "/wt/current", Provisioned: true}); err != nil {
		t.Fatalf("seed store: %v", err)
	}
	writeLegacyBindingsFile(t, d, map[string]legacyBinding{
		key: {RuntimePath: "/wt/stale"},
	})

	got, ok, err := Lookup(d, key)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if !ok || got.RuntimePath != "/wt/current" || !got.Provisioned {
		t.Fatalf("binding = %+v, want store value /wt/current provisioned", got)
	}
}

// TestBindingStoreRoundTrip exercises the keyed store put/upsert/delete path the
// binding accessors ride on directly (ADR-0118).
func TestBindingStoreRoundTrip(t *testing.T) {
	d := bindingTestDeps(t)
	key := "repo-xyz\x00set-9"

	if err := Put(d, key, Binding{RuntimePath: "/wt/9", Branch: "b", Project: "p", Provisioned: true}); err != nil {
		t.Fatalf("put: %v", err)
	}
	got, ok, err := Lookup(d, key)
	if err != nil || !ok || got.RuntimePath != "/wt/9" {
		t.Fatalf("after put binding = %+v ok=%v err=%v", got, ok, err)
	}

	// Put upserts: re-putting the same key overwrites the row in place.
	if err := Put(d, key, Binding{RuntimePath: "/wt/9b"}); err != nil {
		t.Fatalf("upsert: %v", err)
	}
	all, err := AllBindings(d)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	got, _, _ = Lookup(d, key)
	if len(all) != 1 || got.RuntimePath != "/wt/9b" || got.Provisioned {
		t.Fatalf("after upsert bindings = %+v", all)
	}

	if err := Delete(d, key); err != nil {
		t.Fatalf("delete: %v", err)
	}
	final, err := AllBindings(d)
	if err != nil || len(final) != 0 {
		t.Fatalf("after delete bindings = %+v err = %v, want empty", final, err)
	}
}
