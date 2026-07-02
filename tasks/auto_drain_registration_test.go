package tasks

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/glebglazov/pop/config"
)

func TestDiscoverySeedsAutoDrainFromManifest(t *testing.T) {
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
	if len(result.NewRegistrations) != 1 || result.NewRegistrations[0] != "auto-set (auto-drain)" {
		t.Fatalf("new regs = %v", result.NewRegistrations)
	}
	if len(result.Rows) != 1 || !result.Rows[0].AutoDrain {
		t.Fatalf("rows = %#v", result.Rows)
	}

	state, err := LoadGlobalState(statePath)
	if err != nil {
		t.Fatal(err)
	}
	entry := state.Tasks[result.DefinitionPath]
	if entry == nil || len(entry.TaskSets) != 1 || !entry.TaskSets[0].AutoDrain {
		t.Fatalf("state auto_drain = %#v", entry)
	}

	var buf bytes.Buffer
	Render(&buf, result)
	if !strings.Contains(buf.String(), "auto-set (auto-drain)") {
		t.Fatalf("registration line missing suffix:\n%s", buf.String())
	}
}

func TestDiscoverySeedsAutoDrainOffWhenAbsent(t *testing.T) {
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

func TestDiscoverySeedsAutoDrainOffWhenFalse(t *testing.T) {
	root := t.TempDir()
	taskDir := filepath.Join(root, "false-set")
	writeTaskMD(t, taskDir, "01-a.md", "## Acceptance criteria\n\n- [ ] ok\n")
	writeManifestWithSetKeys(t, taskDir, []Task{
		{ID: "01-a", File: "01-a.md", Title: "A", Type: "AFK", Status: "open"},
	}, map[string]any{"auto_drain": false})
	statePath := filepath.Join(root, "state.json")

	result, err := RegisterWith(DefaultDeps(), root, statePath)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.NewRegistrations) != 1 || result.NewRegistrations[0] != "false-set" {
		t.Fatalf("new regs = %v", result.NewRegistrations)
	}
	if len(result.Rows) != 1 || result.Rows[0].AutoDrain {
		t.Fatalf("rows = %#v", result.Rows)
	}
}

func TestAutoDrainNoResyncAfterDashboardToggle(t *testing.T) {
	root := filepath.Join(t.TempDir(), "tasks")
	taskDir := filepath.Join(root, "toggle-set")
	writeTaskMD(t, taskDir, "01-a.md", "## Acceptance criteria\n\n- [ ] ok\n")
	writeManifestWithSetKeys(t, taskDir, []Task{
		{ID: "01-a", File: "01-a.md", Title: "A", Type: "AFK", Status: "open"},
	}, map[string]any{"auto_drain": true})
	statePath := StatePathFor(root)

	if _, err := RegisterWith(DefaultDeps(), root, statePath); err != nil {
		t.Fatal(err)
	}
	if _, err := ToggleAutoDrainWith(DefaultDeps(), root, statePath, "toggle-set"); err != nil {
		t.Fatal(err)
	}

	result, err := RefreshWith(DefaultDeps(), root, statePath)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.NewRegistrations) != 0 {
		t.Fatalf("unexpected re-registration: %v", result.NewRegistrations)
	}
	if len(result.Rows) != 1 || result.Rows[0].AutoDrain {
		t.Fatalf("rows = %#v", result.Rows)
	}
}

func TestAutoDrainNoResyncAfterManifestEdit(t *testing.T) {
	root := t.TempDir()
	setupManifest(t, root, "edit-set", []Task{
		{ID: "01-a", File: "01-a.md", Title: "A", Type: "AFK", Status: "open"},
	})
	statePath := filepath.Join(root, "state.json")

	if _, err := RegisterWith(DefaultDeps(), root, statePath); err != nil {
		t.Fatal(err)
	}

	taskDir := filepath.Join(root, "edit-set")
	writeManifestWithSetKeys(t, taskDir, []Task{
		{ID: "01-a", File: "01-a.md", Title: "A", Type: "AFK", Status: "open"},
	}, map[string]any{"auto_drain": true})

	result, err := RefreshWith(DefaultDeps(), root, statePath)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.NewRegistrations) != 0 {
		t.Fatalf("unexpected re-registration: %v", result.NewRegistrations)
	}
	if len(result.Rows) != 1 || result.Rows[0].AutoDrain {
		t.Fatalf("rows = %#v, want auto_drain off after manifest edit", result.Rows)
	}
}

func TestImportSeedsAutoDrainFromManifest(t *testing.T) {
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
	if imported.TaskSetID != setID {
		t.Fatalf("imported id = %q, want %q", imported.TaskSetID, setID)
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
	if entry == nil || len(entry.TaskSets) != 1 || !entry.TaskSets[0].AutoDrain {
		t.Fatalf("import registration = %#v", entry)
	}

	result, err := RefreshWith(dst.deps, dst.tasksDir, statePath)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.NewRegistrations) != 0 {
		t.Fatalf("unexpected re-registration after import refresh: %v", result.NewRegistrations)
	}
	if len(result.Rows) != 1 || !result.Rows[0].AutoDrain {
		t.Fatalf("refresh rows = %#v", result.Rows)
	}
}
