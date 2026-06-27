package tasks

import (
	"os"
	"path/filepath"
	"testing"
)

func TestToggleAutoDrainRoundTripStateOnly(t *testing.T) {
	root := filepath.Join(t.TempDir(), "tasks")
	setupManifest(t, root, "demo", []Task{
		{ID: "01-a", File: "01-a.md", Title: "A", Type: "AFK", Status: "open"},
	})
	statePath := StatePathFor(root)
	if _, err := RegisterWith(DefaultDeps(), root, statePath); err != nil {
		t.Fatal(err)
	}

	manifestPath := filepath.Join(root, "demo", "index.json")
	beforeManifest, err := os.ReadFile(manifestPath)
	if err != nil {
		t.Fatal(err)
	}

	on, err := ToggleAutoDrainWith(DefaultDeps(), root, statePath, "demo")
	if err != nil {
		t.Fatal(err)
	}
	if !on.AutoDrain {
		t.Fatalf("first toggle AutoDrain = false, want true")
	}
	state, err := LoadGlobalState(statePath)
	if err != nil {
		t.Fatal(err)
	}
	canon, _ := CanonicalDefinitionPath(root)
	if !state.Tasks[canon].TaskSets[0].AutoDrain {
		t.Fatalf("state did not persist auto_drain=true: %#v", state.Tasks[canon].TaskSets[0])
	}
	refresh, err := RefreshWith(DefaultDeps(), root, statePath)
	if err != nil {
		t.Fatal(err)
	}
	if len(refresh.Rows) != 1 || !refresh.Rows[0].AutoDrain {
		t.Fatalf("refresh rows did not round-trip auto_drain: %#v", refresh.Rows)
	}
	if _, err := os.Stat(filepath.Join(root, "demo", "progress.txt")); !os.IsNotExist(err) {
		t.Fatalf("toggle wrote progress.txt: %v", err)
	}
	afterManifest, err := os.ReadFile(manifestPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(afterManifest) != string(beforeManifest) {
		t.Fatalf("toggle mutated manifest:\nbefore=%s\nafter=%s", beforeManifest, afterManifest)
	}

	off, err := ToggleAutoDrainWith(DefaultDeps(), root, statePath, "demo")
	if err != nil {
		t.Fatal(err)
	}
	if off.AutoDrain {
		t.Fatalf("second toggle AutoDrain = true, want false")
	}
}

func TestAutoDrainAndArchiveAreIndependent(t *testing.T) {
	root := filepath.Join(t.TempDir(), "tasks")
	setupManifest(t, root, "demo", []Task{
		{ID: "01-a", File: "01-a.md", Title: "A", Type: "AFK", Status: "open"},
	})
	statePath := StatePathFor(root)
	if _, err := RegisterWith(DefaultDeps(), root, statePath); err != nil {
		t.Fatal(err)
	}
	if _, err := ToggleAutoDrainWith(DefaultDeps(), root, statePath, "demo"); err != nil {
		t.Fatal(err)
	}
	if _, err := ArchiveTaskSetWith(DefaultDeps(), nil, nil, ResolveInput{DefinitionOverride: root, CWD: root}, "demo"); err != nil {
		t.Fatal(err)
	}
	state, err := LoadGlobalState(statePath)
	if err != nil {
		t.Fatal(err)
	}
	canon, _ := CanonicalDefinitionPath(root)
	got := state.Tasks[canon].TaskSets[0]
	if !got.AutoDrain || !got.Archived {
		t.Fatalf("archive changed auto-drain independence: %#v", got)
	}
	if _, err := ToggleAutoDrainWith(DefaultDeps(), root, statePath, "demo"); err != nil {
		t.Fatal(err)
	}
	state, err = LoadGlobalState(statePath)
	if err != nil {
		t.Fatal(err)
	}
	got = state.Tasks[canon].TaskSets[0]
	if got.AutoDrain || !got.Archived {
		t.Fatalf("auto-drain toggle changed archive independence: %#v", got)
	}
}
