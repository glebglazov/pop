package completion

import (
	"testing"

	"github.com/glebglazov/pop/internal/deps"
	"github.com/glebglazov/pop/tasks"
	"github.com/glebglazov/pop/tasks/binding"
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
	// Replace the whole set: clear any prior rows, then write the given ones.
	existing, err := binding.AllBindings(td)
	if err != nil {
		t.Fatal(err)
	}
	for key := range existing {
		if err := binding.Delete(td, key); err != nil {
			t.Fatal(err)
		}
	}
	for key, b := range bindings {
		if err := binding.Put(td, key, b); err != nil {
			t.Fatal(err)
		}
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

// TestBindWorktreeSetIDs verifies the candidate list is the bound set IDs,
// deduplicated and sorted across repos.
func TestBindWorktreeSetIDs(t *testing.T) {
	td := testDeps(t)
	seedBindings(t, td, map[string]binding.Binding{
		"alpha\x00set-a": {RuntimePath: "/wt/a", Project: "alpha"},
		"beta\x00set-b":  {RuntimePath: "/wt/b", Project: "beta"},
	})

	ids, err := BindWorktreeSetIDs(td)
	if err != nil {
		t.Fatal(err)
	}
	if len(ids) != 2 || ids[0] != "set-a" || ids[1] != "set-b" {
		t.Fatalf("ids = %#v, want [set-a set-b]", ids)
	}
}
