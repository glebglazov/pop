package completion

import (
	"testing"

	"github.com/glebglazov/pop/internal/deps"
	"github.com/glebglazov/pop/tasks"
	"github.com/glebglazov/pop/tasks/binding"
	"github.com/glebglazov/pop/tasks/integration"
)

func testDeps(t *testing.T) *tasks.Deps {
	t.Helper()
	dir := t.TempDir()
	real := deps.NewRealFileSystem()
	d := tasks.DefaultDeps()
	d.FS = &deps.MockFileSystem{
		GetenvFunc: func(key string) string {
			if key == "XDG_DATA_HOME" {
				return dir
			}
			return ""
		},
		ReadFileFunc:  real.ReadFile,
		WriteFileFunc: real.WriteFile,
		MkdirAllFunc:  real.MkdirAll,
		RenameFunc:    real.Rename,
		RemoveAllFunc: real.RemoveAll,
	}
	return d
}

func seedBindings(t *testing.T, td *tasks.Deps, bindings map[string]binding.Binding) {
	t.Helper()
	store := &binding.Store{}
	for key, b := range bindings {
		store.Put(key, b)
	}
	if err := binding.Save(td, store); err != nil {
		t.Fatal(err)
	}
}

func seedMergeability(t *testing.T, td *tasks.Deps, records map[string]integration.Record) {
	t.Helper()
	store := &integration.Store{}
	for key, rec := range records {
		store.Put(key, rec)
	}
	if err := integration.Save(td, store); err != nil {
		t.Fatal(err)
	}
}

func TestIntegrationSetIDsEmpty(t *testing.T) {
	ids, err := IntegrationSetIDs(testDeps(t))
	if err != nil {
		t.Fatal(err)
	}
	if len(ids) != 0 {
		t.Fatalf("ids = %#v, want empty", ids)
	}
}

func TestIntegrationSetIDsDedupesAndSorts(t *testing.T) {
	td := testDeps(t)
	seedMergeability(t, td, map[string]integration.Record{
		"b\x00set-b":  {Project: "beta", SetID: "set-b", Status: "conflicts"},
		"a\x00set-a":  {Project: "alpha", SetID: "set-a", Status: "clean"},
		"a2\x00set-a": {Project: "alpha2", SetID: "set-a", Status: "clean"},
	})

	ids, err := IntegrationSetIDs(td)
	if err != nil {
		t.Fatal(err)
	}
	if len(ids) != 2 || ids[0] != "set-a" || ids[1] != "set-b" {
		t.Fatalf("ids = %#v, want [set-a set-b]", ids)
	}
}

func TestAbandonSetIDsEmpty(t *testing.T) {
	ids, err := AbandonSetIDs(testDeps(t))
	if err != nil {
		t.Fatal(err)
	}
	if len(ids) != 0 {
		t.Fatalf("ids = %#v, want empty", ids)
	}
}

func TestAbandonSetIDsDedupesAndSorts(t *testing.T) {
	td := testDeps(t)
	seedBindings(t, td, map[string]binding.Binding{
		"beta\x00set-b":   {RuntimePath: "/wt/b", Project: "beta"},
		"alpha\x00set-a":  {RuntimePath: "/wt/a", Project: "alpha"},
		"alpha2\x00set-a": {RuntimePath: "/wt/a2", Project: "alpha2"},
	})

	ids, err := AbandonSetIDs(td)
	if err != nil {
		t.Fatal(err)
	}
	if len(ids) != 2 || ids[0] != "set-a" || ids[1] != "set-b" {
		t.Fatalf("ids = %#v, want [set-a set-b]", ids)
	}
}

// TestBindWorktreeSetIDsUnion verifies the one function carrying real logic:
// the candidate list is the union of bound sets and integration-eligible sets.
func TestBindWorktreeSetIDsUnion(t *testing.T) {
	td := testDeps(t)
	seedBindings(t, td, map[string]binding.Binding{
		"alpha\x00set-a": {RuntimePath: "/wt/a", Project: "alpha"},
	})
	seedMergeability(t, td, map[string]integration.Record{
		"beta\x00set-b": {Project: "beta", SetID: "set-b", Status: "clean"},
	})

	ids, err := BindWorktreeSetIDs(td)
	if err != nil {
		t.Fatal(err)
	}
	if len(ids) != 2 || ids[0] != "set-a" || ids[1] != "set-b" {
		t.Fatalf("ids = %#v, want union [set-a set-b]", ids)
	}
}
