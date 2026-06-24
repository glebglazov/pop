package tasks

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/glebglazov/pop/internal/deps"
)

func bindingsStoreDeps(t *testing.T) *Deps {
	t.Helper()
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	return &Deps{FS: deps.NewRealFileSystem()}
}

// legacyBinding mirrors one bindings.json record so a test can hand-write the
// retired file in its real on-disk shape (NUL-separated scoped keys included).
type legacyBinding struct {
	RuntimePath string `json:"runtime_path"`
	Branch      string `json:"branch,omitempty"`
	Project     string `json:"project,omitempty"`
	Provisioned bool   `json:"provisioned,omitempty"`
}

func writeLegacyBindingsFile(t *testing.T, d *Deps, bindings map[string]legacyBinding) {
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
	d := bindingsStoreDeps(t)
	managedKey := "repo-abc\x00set-managed"
	adoptedKey := "repo-abc\x00set-adopted"
	writeLegacyBindingsFile(t, d, map[string]legacyBinding{
		managedKey: {RuntimePath: "/wt/managed", Branch: "pop/set-managed/x", Project: "proj", Provisioned: true},
		adoptedKey: {RuntimePath: "/wt/adopted", Branch: "feature", Project: "proj"},
	})

	entries, err := LoadBindingEntries(d)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("entries = %+v, want 2", entries)
	}
	managed, ok := entries[managedKey]
	if !ok || !managed.Provisioned || managed.RuntimePath != "/wt/managed" {
		t.Fatalf("managed binding = %+v, want provisioned /wt/managed", managed)
	}
	adopted, ok := entries[adoptedKey]
	if !ok || adopted.Provisioned || adopted.RuntimePath != "/wt/adopted" {
		t.Fatalf("adopted binding = %+v, want adopted /wt/adopted", adopted)
	}

	// The file is retired once its contents are safely in the store.
	if _, err := os.Stat(LegacyBindingsPath(d)); !os.IsNotExist(err) {
		t.Fatalf("legacy bindings file still present: stat err = %v", err)
	}

	// A second load is a no-op that still returns the migrated bindings.
	again, err := LoadBindingEntries(d)
	if err != nil || len(again) != 2 {
		t.Fatalf("reload entries = %+v err = %v, want 2 entries", again, err)
	}
}

// TestMigrateLegacyBindingsFileStoreWins verifies a binding already present in
// the store is not clobbered by a stale entry left in bindings.json.
func TestMigrateLegacyBindingsFileStoreWins(t *testing.T) {
	d := bindingsStoreDeps(t)
	key := "repo-abc\x00set-1"
	if err := PutBindingEntry(d, key, BindingEntry{RuntimePath: "/wt/current", Provisioned: true}); err != nil {
		t.Fatalf("seed store: %v", err)
	}
	writeLegacyBindingsFile(t, d, map[string]legacyBinding{
		key: {RuntimePath: "/wt/stale"},
	})

	entries, err := LoadBindingEntries(d)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	got := entries[key]
	if got.RuntimePath != "/wt/current" || !got.Provisioned {
		t.Fatalf("binding = %+v, want store value /wt/current provisioned", got)
	}
}

// TestBindingEntriesRoundTrip exercises the store-backed put/delete/replace path
// the binding façade rides on.
func TestBindingEntriesRoundTrip(t *testing.T) {
	d := bindingsStoreDeps(t)
	key := "repo-xyz\x00set-9"

	if err := PutBindingEntry(d, key, BindingEntry{RuntimePath: "/wt/9", Branch: "b", Project: "p", Provisioned: true}); err != nil {
		t.Fatalf("put: %v", err)
	}
	entries, err := LoadBindingEntries(d)
	if err != nil || entries[key].RuntimePath != "/wt/9" {
		t.Fatalf("after put entries = %+v err = %v", entries, err)
	}

	if err := SaveBindingEntries(d, map[string]BindingEntry{key: {RuntimePath: "/wt/9b"}}); err != nil {
		t.Fatalf("save: %v", err)
	}
	entries, err = LoadBindingEntries(d)
	if err != nil || len(entries) != 1 || entries[key].RuntimePath != "/wt/9b" || entries[key].Provisioned {
		t.Fatalf("after replace entries = %+v err = %v", entries, err)
	}

	if err := DeleteBindingEntry(d, key); err != nil {
		t.Fatalf("delete: %v", err)
	}
	entries, err = LoadBindingEntries(d)
	if err != nil || len(entries) != 0 {
		t.Fatalf("after delete entries = %+v err = %v, want empty", entries, err)
	}
}
