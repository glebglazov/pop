package tasks

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRegisteredTaskSetArchivedDefaultsFalseOnExistingState(t *testing.T) {
	root := t.TempDir()
	statePath := StatePathFor(root)
	raw := `{"version":1,"workloads":{"/tmp/tasks":{"issue_sets":[{"id":"demo","priority":3}]}}}`
	if err := os.WriteFile(statePath, []byte(raw), 0o644); err != nil {
		t.Fatal(err)
	}

	state, err := LoadGlobalState(statePath)
	if err != nil {
		t.Fatal(err)
	}
	got := state.Tasks["/tmp/tasks"].TaskSets[0]
	if got.Archived {
		t.Fatalf("archived default = true, want false")
	}
}

func TestArchiveRoundTripStateOnlyAndStatusViews(t *testing.T) {
	root := t.TempDir()
	setupManifest(t, root, "active", []Task{
		{ID: "01-a", File: "01-a.md", Title: "A", Type: "AFK", Status: "open"},
	})
	setupManifest(t, root, "filed", []Task{
		{ID: "01-a", File: "01-a.md", Title: "A", Type: "AFK", Status: "done"},
	})
	statePath := StatePathFor(root)
	if _, err := RefreshWith(DefaultDeps(), root, statePath); err != nil {
		t.Fatal(err)
	}

	manifestPath := filepath.Join(root, "filed", "index.json")
	beforeManifest, err := os.ReadFile(manifestPath)
	if err != nil {
		t.Fatal(err)
	}

	if _, err := ArchiveTaskSetWith(DefaultDeps(), nil, nil, ResolveInput{DefinitionOverride: root, CWD: root}, "filed"); err != nil {
		t.Fatal(err)
	}

	state, err := LoadGlobalState(statePath)
	if err != nil {
		t.Fatal(err)
	}
	canon, _ := CanonicalDefinitionPath(root)
	idx := state.RegisteredIDs(canon)["filed"]
	if !state.Tasks[canon].TaskSets[idx].Archived {
		t.Fatalf("filed was not archived: %#v", state.Tasks[canon].TaskSets)
	}
	if _, err := os.Stat(filepath.Join(root, "filed", "progress.txt")); !os.IsNotExist(err) {
		t.Fatalf("archive wrote progress.txt: %v", err)
	}
	afterManifest, err := os.ReadFile(manifestPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(afterManifest) != string(beforeManifest) {
		t.Fatalf("archive mutated manifest:\nbefore=%s\nafter=%s", beforeManifest, afterManifest)
	}

	active, err := RefreshWith(DefaultDeps(), root, statePath)
	if err != nil {
		t.Fatal(err)
	}
	if len(active.Rows) != 1 || active.Rows[0].ID != "active" {
		t.Fatalf("default rows = %#v", active.Rows)
	}
	if active.ArchivedCount != 1 {
		t.Fatalf("archived count = %d", active.ArchivedCount)
	}
	var activeOut bytes.Buffer
	Render(&activeOut, active)
	if !strings.Contains(activeOut.String(), "1 Archived Task set hidden") || !strings.Contains(activeOut.String(), "pop tasks status --archived") {
		t.Fatalf("missing archive footer:\n%s", activeOut.String())
	}

	archived, err := RefreshArchivedWith(DefaultDeps(), root, statePath)
	if err != nil {
		t.Fatal(err)
	}
	if len(archived.Rows) != 1 || archived.Rows[0].ID != "filed" {
		t.Fatalf("archived rows = %#v", archived.Rows)
	}
	var archivedOut bytes.Buffer
	Render(&archivedOut, archived)
	if strings.Contains(archivedOut.String(), "hidden") {
		t.Fatalf("archived view should not print hidden footer:\n%s", archivedOut.String())
	}

	if _, err := UnarchiveTaskSetWith(DefaultDeps(), nil, nil, ResolveInput{DefinitionOverride: root, CWD: root}, "filed"); err != nil {
		t.Fatal(err)
	}
	unarchived, err := RefreshWith(DefaultDeps(), root, statePath)
	if err != nil {
		t.Fatal(err)
	}
	if len(unarchived.Rows) != 2 {
		t.Fatalf("rows after unarchive = %#v", unarchived.Rows)
	}
	var unarchivedOut bytes.Buffer
	Render(&unarchivedOut, unarchived)
	if strings.Contains(unarchivedOut.String(), "pop tasks status --archived") {
		t.Fatalf("footer present with zero archives:\n%s", unarchivedOut.String())
	}
}

func TestArchiveResolvesMissingRegisteredSet(t *testing.T) {
	root := t.TempDir()
	statePath := StatePathFor(root)
	canon, err := CanonicalDefinitionPath(root)
	if err != nil {
		t.Fatal(err)
	}
	state := &GlobalState{
		Version: StateVersion,
		Tasks:   map[string]*TaskEntry{canon: {TaskSets: []RegisteredTaskSet{{ID: "missing", Priority: 0}}}},
		path:    statePath,
	}
	if err := state.Save(); err != nil {
		t.Fatal(err)
	}

	if _, err := ArchiveTaskSetWith(DefaultDeps(), nil, nil, ResolveInput{DefinitionOverride: root, CWD: root}, "missing"); err != nil {
		t.Fatal(err)
	}
	archived, err := RefreshArchivedWith(DefaultDeps(), root, statePath)
	if err != nil {
		t.Fatal(err)
	}
	if len(archived.Rows) != 1 || archived.Rows[0].Status != StatusMissing {
		t.Fatalf("archived missing rows = %#v", archived.Rows)
	}
}

func TestArchivedSetIsNotAutoSelected(t *testing.T) {
	root := t.TempDir()
	setupManifest(t, root, "filed-high", []Task{
		{ID: "01-a", File: "01-a.md", Title: "A", Type: "AFK", Status: "open"},
	})
	setupManifest(t, root, "active-low", []Task{
		{ID: "01-a", File: "01-a.md", Title: "A", Type: "AFK", Status: "open"},
	})
	statePath := StatePathFor(root)
	canon, err := CanonicalDefinitionPath(root)
	if err != nil {
		t.Fatal(err)
	}
	state := &GlobalState{
		Version: StateVersion,
		Tasks: map[string]*TaskEntry{canon: {TaskSets: []RegisteredTaskSet{
			{ID: "filed-high", Priority: 100, Archived: true},
			{ID: "active-low", Priority: 0},
		}}},
		path: statePath,
	}
	if err := state.Save(); err != nil {
		t.Fatal(err)
	}

	refresh, err := RefreshWith(DefaultDeps(), root, statePath)
	if err != nil {
		t.Fatal(err)
	}
	if len(refresh.Rows) != 1 || refresh.Rows[0].ID != "active-low" || !refresh.Rows[0].AutoPick {
		t.Fatalf("rows = %#v", refresh.Rows)
	}
	selected, _, err := SelectTaskSet(refresh, "")
	if err != nil {
		t.Fatal(err)
	}
	if selected != "active-low" {
		t.Fatalf("selected = %q", selected)
	}
}
