package tasks

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/glebglazov/pop/config"
)

// TestRegisterIgnoresAutoDrainManifestKey asserts the retired auto_drain key no
// longer seeds Task state (ADR-0115): the set registers successfully with
// auto-drain off, and no "(auto-drain)" suffix is shown.
func TestRegisterIgnoresAutoDrainManifestKey(t *testing.T) {
	root := t.TempDir()
	taskDir := filepath.Join(root, "auto-set")
	writeTaskMD(t, taskDir, "01-a.md", "## Acceptance criteria\n\n- [ ] ok\n")
	writeManifestWithSetKeys(t, taskDir, []Task{
		{ID: "01-a", File: "01-a.md", Title: "A", Type: "AFK", Status: "open"},
	}, map[string]any{"auto_drain": true})
	statePath := filepath.Join(root, "state.json")

	result, err := RegisterWith(DefaultDeps(), root, statePath)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.NewRegistrations) != 1 || result.NewRegistrations[0] != "auto-set" {
		t.Fatalf("new regs = %v, want [auto-set] with no auto-drain suffix", result.NewRegistrations)
	}
	if len(result.Rows) != 1 || result.Rows[0].AutoDrain {
		t.Fatalf("rows = %#v, want auto-drain off (key ignored)", result.Rows)
	}
	if result.Rows[0].Status == StatusMalformed {
		t.Fatalf("row MALFORMED for a legacy auto_drain key: %#v", result.Rows[0])
	}

	state, err := LoadGlobalState(statePath)
	if err != nil {
		t.Fatal(err)
	}
	entry := state.Tasks[result.DefinitionPath]
	if entry == nil || len(entry.TaskSets) != 1 || entry.TaskSets[0].AutoDrain {
		t.Fatalf("state auto_drain = %#v, want not seeded", entry)
	}
}

// TestRegisterMalformedAutoDrainNeverMalformed asserts a non-boolean auto_drain
// value is ignored, not treated as a MALFORMED manifest (ADR-0115).
func TestRegisterMalformedAutoDrainNeverMalformed(t *testing.T) {
	root := t.TempDir()
	taskDir := filepath.Join(root, "bad-set")
	writeTaskMD(t, taskDir, "01-a.md", "## Acceptance criteria\n\n- [ ] ok\n")
	writeManifestWithSetKeys(t, taskDir, []Task{
		{ID: "01-a", File: "01-a.md", Title: "A", Type: "AFK", Status: "open"},
	}, map[string]any{"auto_drain": "yes"})
	statePath := filepath.Join(root, "state.json")

	result, err := RegisterWith(DefaultDeps(), root, statePath)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Rows) != 1 || result.Rows[0].Status == StatusMalformed {
		t.Fatalf("rows = %#v, want a non-MALFORMED registration", result.Rows)
	}
	if result.Rows[0].AutoDrain {
		t.Fatalf("rows = %#v, want auto-drain off (key ignored)", result.Rows)
	}
}

// TestRegisterAutoDrainOffWhenAbsent asserts a set with no auto_drain key
// registers with auto-drain off.
func TestRegisterAutoDrainOffWhenAbsent(t *testing.T) {
	root := t.TempDir()
	setupManifest(t, root, "plain-set", []Task{
		{ID: "01-a", File: "01-a.md", Title: "A", Type: "AFK", Status: "open"},
	})
	statePath := filepath.Join(root, "state.json")

	result, err := RegisterWith(DefaultDeps(), root, statePath)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.NewRegistrations) != 1 || result.NewRegistrations[0] != "plain-set" {
		t.Fatalf("new regs = %v", result.NewRegistrations)
	}
	if len(result.Rows) != 1 || result.Rows[0].AutoDrain {
		t.Fatalf("rows = %#v", result.Rows)
	}
}

// TestAutoDrainToggleSurvivesManifestKey asserts auto-drain is store-authoritative:
// a dashboard toggle stands, and a manifest still carrying auto_drain never
// resyncs it on a later refresh (the key is ignored, ADR-0115).
func TestAutoDrainToggleSurvivesManifestKey(t *testing.T) {
	root := filepath.Join(t.TempDir(), "tasks")
	taskDir := filepath.Join(root, "toggle-set")
	writeTaskMD(t, taskDir, "01-a.md", "## Acceptance criteria\n\n- [ ] ok\n")
	writeManifestWithSetKeys(t, taskDir, []Task{
		{ID: "01-a", File: "01-a.md", Title: "A", Type: "AFK", Status: "open"},
	}, map[string]any{"auto_drain": false})
	statePath := StatePathFor(root)

	if _, err := RegisterWith(DefaultDeps(), root, statePath); err != nil {
		t.Fatal(err)
	}
	// Toggle on via the CLI seam (registered off, since the manifest key is ignored).
	if _, err := ToggleAutoDrainWith(DefaultDeps(), root, statePath, "toggle-set"); err != nil {
		t.Fatal(err)
	}

	// Rewrite the manifest with a conflicting auto_drain value; a refresh must not
	// resync auto-drain from it.
	writeManifestWithSetKeys(t, taskDir, []Task{
		{ID: "01-a", File: "01-a.md", Title: "A", Type: "AFK", Status: "open"},
	}, map[string]any{"auto_drain": false})

	result, err := RefreshWith(DefaultDeps(), root, statePath)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.NewRegistrations) != 0 {
		t.Fatalf("unexpected re-registration: %v", result.NewRegistrations)
	}
	if len(result.Rows) != 1 || !result.Rows[0].AutoDrain {
		t.Fatalf("rows = %#v, want auto-drain still on (store-authoritative)", result.Rows)
	}
}

// TestImportIgnoresAutoDrainManifestKey asserts importing a set whose manifest
// carries auto_drain does not seed auto-drain (ADR-0115).
func TestImportIgnoresAutoDrainManifestKey(t *testing.T) {
	src := newTransferEnv(t)
	const setID = "2026-06-01-import-drain"
	src.writeSet(t, setID, func(dir string) {
		manifest := `{"tasks":[{"id":"01-a","file":"01-a.md","title":"A","type":"AFK","status":"open"}],"auto_drain":true}`
		if err := os.WriteFile(filepath.Join(dir, "index.json"), []byte(manifest), 0o644); err != nil {
			t.Fatal(err)
		}
	})

	exported, err := ExportWith(src.deps, projectDefaultDeps(), config.Load, ExportOptions{
		ResolveInput: src.resolveInput(),
		TaskSetIDs:   []string{setID},
		OutputPath:   filepath.Join(src.root, setID+".tar.gz"),
	})
	if err != nil {
		t.Fatal(err)
	}

	dst := newTransferEnv(t)
	imported, err := ImportWith(dst.deps, projectDefaultDeps(), config.Load, ImportOptions{
		ResolveInput: dst.resolveInput(),
		ArchivePath:  exported.Path,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(imported.Sets) != 1 || imported.Sets[0].TaskSetID != setID {
		t.Fatalf("imported = %#v, want single set %q", imported.Sets, setID)
	}

	statePath := StatePathFor(dst.tasksDir)
	canonDef, err := CanonicalDefinitionPathWith(dst.deps, dst.tasksDir)
	if err != nil {
		t.Fatal(err)
	}
	state, err := LoadGlobalStateWith(dst.deps, statePath)
	if err != nil {
		t.Fatal(err)
	}
	entry := state.Tasks[canonDef]
	if entry == nil || len(entry.TaskSets) != 1 || entry.TaskSets[0].AutoDrain {
		t.Fatalf("import registration = %#v, want auto-drain not seeded", entry)
	}
}
