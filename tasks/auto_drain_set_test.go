package tasks

import (
	"path/filepath"
	"strings"
	"testing"
)

func autoDrainState(t *testing.T, root, id string) RegisteredTaskSet {
	t.Helper()
	statePath := StatePathFor(root)
	state, err := LoadGlobalState(statePath)
	if err != nil {
		t.Fatal(err)
	}
	canon, _ := CanonicalDefinitionPath(root)
	for _, set := range state.Tasks[canon].TaskSets {
		if set.ID == id {
			return set
		}
	}
	t.Fatalf("task set %q not registered: %#v", id, state.Tasks[canon])
	return RegisteredTaskSet{}
}

func TestSetAutoDrainEnablesAndIsIdempotent(t *testing.T) {
	root := filepath.Join(t.TempDir(), "tasks")
	setupManifest(t, root, "demo", []Task{
		{ID: "01-a", File: "01-a.md", Title: "A", Type: "AFK", Status: "open"},
	})
	statePath := StatePathFor(root)
	if _, err := RegisterWith(DefaultDeps(), root, statePath); err != nil {
		t.Fatal(err)
	}

	res, err := SetAutoDrainWith(DefaultDeps(), nil, nil, ResolveInput{DefinitionOverride: root, CWD: root}, "demo", true)
	if err != nil {
		t.Fatal(err)
	}
	if !res.AutoDrain || res.TaskSetID != "demo" {
		t.Fatalf("first enable = %#v, want AutoDrain=true id=demo", res)
	}
	if !autoDrainState(t, root, "demo").AutoDrain {
		t.Fatalf("state did not persist auto_drain=true")
	}
	if res.Refresh == nil || len(res.Refresh.Rows) != 1 || !res.Refresh.Rows[0].AutoDrain {
		t.Fatalf("refresh did not reflect auto_drain=true: %#v", res.Refresh)
	}

	// Idempotent: a second enable leaves the bit set (does not flip).
	res, err = SetAutoDrainWith(DefaultDeps(), nil, nil, ResolveInput{DefinitionOverride: root, CWD: root}, "demo", true)
	if err != nil {
		t.Fatal(err)
	}
	if !res.AutoDrain {
		t.Fatalf("re-enable flipped the bit: %#v", res)
	}
	if !autoDrainState(t, root, "demo").AutoDrain {
		t.Fatalf("re-enable did not keep auto_drain=true")
	}
}

func TestSetAutoDrainOffDisablesAndIsIdempotent(t *testing.T) {
	root := filepath.Join(t.TempDir(), "tasks")
	setupManifest(t, root, "demo", []Task{
		{ID: "01-a", File: "01-a.md", Title: "A", Type: "AFK", Status: "open"},
	})
	statePath := StatePathFor(root)
	if _, err := RegisterWith(DefaultDeps(), root, statePath); err != nil {
		t.Fatal(err)
	}
	if _, err := SetAutoDrainWith(DefaultDeps(), nil, nil, ResolveInput{DefinitionOverride: root, CWD: root}, "demo", true); err != nil {
		t.Fatal(err)
	}

	res, err := SetAutoDrainWith(DefaultDeps(), nil, nil, ResolveInput{DefinitionOverride: root, CWD: root}, "demo", false)
	if err != nil {
		t.Fatal(err)
	}
	if res.AutoDrain {
		t.Fatalf("--off did not disable: %#v", res)
	}
	if autoDrainState(t, root, "demo").AutoDrain {
		t.Fatalf("state did not persist auto_drain=false")
	}

	// Idempotent off.
	res, err = SetAutoDrainWith(DefaultDeps(), nil, nil, ResolveInput{DefinitionOverride: root, CWD: root}, "demo", false)
	if err != nil {
		t.Fatal(err)
	}
	if res.AutoDrain {
		t.Fatalf("re-disable flipped the bit: %#v", res)
	}
}

func TestSetAutoDrainUnknownIDListsCandidates(t *testing.T) {
	root := filepath.Join(t.TempDir(), "tasks")
	setupManifest(t, root, "demo", []Task{
		{ID: "01-a", File: "01-a.md", Title: "A", Type: "AFK", Status: "open"},
	})
	statePath := StatePathFor(root)
	if _, err := RegisterWith(DefaultDeps(), root, statePath); err != nil {
		t.Fatal(err)
	}

	_, err := SetAutoDrainWith(DefaultDeps(), nil, nil, ResolveInput{DefinitionOverride: root, CWD: root}, "nope", true)
	if err == nil {
		t.Fatal("expected unknown-id error, got nil")
	}
	if !strings.Contains(err.Error(), "demo") {
		t.Fatalf("error did not list valid ids: %v", err)
	}
}

func TestSetAutoDrainRejectsFileReference(t *testing.T) {
	root := filepath.Join(t.TempDir(), "tasks")
	setupManifest(t, root, "demo", []Task{
		{ID: "01-a", File: "01-a.md", Title: "A", Type: "AFK", Status: "open"},
	})
	statePath := StatePathFor(root)
	if _, err := RegisterWith(DefaultDeps(), root, statePath); err != nil {
		t.Fatal(err)
	}

	if _, err := SetAutoDrainWith(DefaultDeps(), nil, nil, ResolveInput{DefinitionOverride: root, CWD: root}, "demo/01-a.md", true); err == nil {
		t.Fatal("expected file-reference rejection, got nil")
	}
	if autoDrainState(t, root, "demo").AutoDrain {
		t.Fatalf("file-reference form should not have mutated the set")
	}
}

func TestSetAutoDrainRejectsArchivedSet(t *testing.T) {
	root := filepath.Join(t.TempDir(), "tasks")
	setupManifest(t, root, "demo", []Task{
		{ID: "01-a", File: "01-a.md", Title: "A", Type: "AFK", Status: "open"},
	})
	statePath := StatePathFor(root)
	if _, err := RegisterWith(DefaultDeps(), root, statePath); err != nil {
		t.Fatal(err)
	}
	if _, err := ArchiveTaskSetWith(DefaultDeps(), nil, nil, ResolveInput{DefinitionOverride: root, CWD: root}, "demo"); err != nil {
		t.Fatal(err)
	}

	_, err := SetAutoDrainWith(DefaultDeps(), nil, nil, ResolveInput{DefinitionOverride: root, CWD: root}, "demo", true)
	if err == nil {
		t.Fatal("expected archived-set rejection, got nil")
	}
	if !strings.Contains(err.Error(), "unarchive") {
		t.Fatalf("archived rejection should point at unarchive: %v", err)
	}
	if autoDrainState(t, root, "demo").AutoDrain {
		t.Fatalf("archived set should not have been marked auto-drain")
	}
}
