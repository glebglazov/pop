package config

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/BurntSushi/toml"
	"github.com/glebglazov/pop/internal/deps"
)

func runtimeTestDeps(t *testing.T) (*Deps, string) {
	t.Helper()
	root := t.TempDir()
	dataDir := filepath.Join(root, "data")
	d := &Deps{FS: &deps.MockFileSystem{
		GetenvFunc: func(key string) string {
			if key == "XDG_DATA_HOME" {
				return dataDir
			}
			return ""
		},
		UserHomeDirFunc: func() (string, error) { return filepath.Join(root, "home"), nil },
		ReadFileFunc:    os.ReadFile,
		WriteFileFunc:   os.WriteFile,
		MkdirAllFunc:    os.MkdirAll,
		RenameFunc:      os.Rename,
		RemoveAllFunc:   os.RemoveAll,
		StatFunc:        os.Stat,
	}}
	return d, filepath.Join(dataDir, "pop", "config.runtime.toml")
}

func writeRuntimeFile(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestRemoveRuntimeIntegrationSkill_FromDefaults(t *testing.T) {
	d, runtimePath := runtimeTestDeps(t)

	if err := RemoveRuntimeIntegrationSkillsWith(d, IntegrationSkillPane); err != nil {
		t.Fatalf("RemoveRuntimeIntegrationSkillsWith() error: %v", err)
	}

	data, err := os.ReadFile(runtimePath)
	if err != nil {
		t.Fatalf("runtime file missing: %v", err)
	}
	if !strings.Contains(string(data), `skills = ["tasks"]`) {
		t.Fatalf("runtime file = %q, want skills = [\"tasks\"]", string(data))
	}
}

func TestRemoveRuntimeIntegrationSkill_FromExistingRuntime(t *testing.T) {
	d, runtimePath := runtimeTestDeps(t)
	writeRuntimeFile(t, runtimePath, `
[integrations]
skills = ["tasks", "pane"]

[future]
enabled = true
`)

	if err := RemoveRuntimeIntegrationSkillsWith(d, IntegrationSkillPane); err != nil {
		t.Fatalf("RemoveRuntimeIntegrationSkillsWith() error: %v", err)
	}

	data, err := os.ReadFile(runtimePath)
	if err != nil {
		t.Fatalf("runtime file missing: %v", err)
	}
	body := string(data)
	if !strings.Contains(body, `skills = ["tasks"]`) {
		t.Fatalf("runtime skills not updated: %q", body)
	}
	if !strings.Contains(body, "[future]") {
		t.Fatalf("unrelated runtime keys lost: %q", body)
	}
}

func TestRemoveRuntimeIntegrationSkills_RemovesBoth(t *testing.T) {
	d, runtimePath := runtimeTestDeps(t)

	if err := RemoveRuntimeIntegrationSkillsWith(d, IntegrationSkillPane, IntegrationSkillTasks); err != nil {
		t.Fatalf("RemoveRuntimeIntegrationSkillsWith() error: %v", err)
	}
	data, err := os.ReadFile(runtimePath)
	if err != nil {
		t.Fatalf("runtime file missing: %v", err)
	}
	if !strings.Contains(string(data), "skills = []") {
		t.Fatalf("expected empty skills array in runtime file, got %q", string(data))
	}
}

func TestClearRuntimeIntegrations_DeletesSection(t *testing.T) {
	d, runtimePath := runtimeTestDeps(t)
	writeRuntimeFile(t, runtimePath, `
[integrations]
skills = ["tasks"]

[future]
enabled = true
`)

	if err := ClearRuntimeIntegrationsWith(d); err != nil {
		t.Fatalf("ClearRuntimeIntegrationsWith() error: %v", err)
	}

	data, err := os.ReadFile(runtimePath)
	if err != nil {
		t.Fatalf("runtime file missing: %v", err)
	}
	body := string(data)
	if strings.Contains(body, "[integrations]") {
		t.Fatalf("integrations section should be cleared: %q", body)
	}
	if !strings.Contains(body, "[future]") {
		t.Fatalf("unrelated runtime keys lost: %q", body)
	}
}

func TestClearRuntimeIntegrations_DeletesEmptyFile(t *testing.T) {
	d, runtimePath := runtimeTestDeps(t)
	writeRuntimeFile(t, runtimePath, `
[integrations]
skills = ["tasks"]
`)

	if err := ClearRuntimeIntegrationsWith(d); err != nil {
		t.Fatalf("ClearRuntimeIntegrationsWith() error: %v", err)
	}
	if _, err := os.Stat(runtimePath); !os.IsNotExist(err) {
		t.Fatal("expected runtime file deleted when empty")
	}
}

func TestBareIntegrateClearThenUserConfigWins(t *testing.T) {
	root := t.TempDir()
	dataDir := filepath.Join(root, "data")
	configDir := filepath.Join(root, "config")
	userPath := filepath.Join(configDir, "config.toml")
	runtimePath := filepath.Join(dataDir, "pop", "config.runtime.toml")

	writeRuntimeFile(t, runtimePath, `
[integrations]
skills = ["tasks"]
`)
	writeConfigFile(t, userPath, `
projects = [{ path = "/main" }]

[integrations]
skills = ["tasks"]
`)

	d := &Deps{FS: &deps.MockFileSystem{
		GetenvFunc: func(key string) string {
			if key == "XDG_DATA_HOME" {
				return dataDir
			}
			return ""
		},
		UserHomeDirFunc: func() (string, error) { return filepath.Join(root, "home"), nil },
		ReadFileFunc:    os.ReadFile,
		WriteFileFunc:   os.WriteFile,
		MkdirAllFunc:    os.MkdirAll,
		RenameFunc:      os.Rename,
		RemoveAllFunc:   os.RemoveAll,
		StatFunc:        os.Stat,
	}}

	if err := ClearRuntimeIntegrationsWith(d); err != nil {
		t.Fatalf("ClearRuntimeIntegrationsWith() error: %v", err)
	}

	cfg, err := LoadWith(d, userPath)
	if err != nil {
		t.Fatalf("LoadWith() error: %v", err)
	}
	skills, err := cfg.IntegrationsSkills()
	if err != nil {
		t.Fatalf("IntegrationsSkills() error: %v", err)
	}
	want := []string{"tasks"}
	if !reflect.DeepEqual(skills, want) {
		t.Fatalf("IntegrationsSkills() = %#v, want %#v (user config wins after bare clear)", skills, want)
	}
}

func TestRuntimeIntegrationsSkills_ReadsOnDisk(t *testing.T) {
	d, runtimePath := runtimeTestDeps(t)
	writeRuntimeFile(t, runtimePath, `
[integrations]
skills = ["tasks"]
`)

	skills, ok, err := RuntimeIntegrationsSkillsWith(d)
	if err != nil {
		t.Fatalf("RuntimeIntegrationsSkillsWith() error: %v", err)
	}
	if !ok {
		t.Fatal("RuntimeIntegrationsSkillsWith() ok = false, want true")
	}
	if !reflect.DeepEqual(skills, []string{"tasks"}) {
		t.Fatalf("RuntimeIntegrationsSkillsWith() = %#v, want [tasks]", skills)
	}
}

func TestRuntimeWriteIsAtomic(t *testing.T) {
	d, runtimePath := runtimeTestDeps(t)
	var renameTarget string
	origRename := d.FS.(*deps.MockFileSystem).RenameFunc
	d.FS.(*deps.MockFileSystem).RenameFunc = func(oldpath, newpath string) error {
		renameTarget = newpath
		if origRename != nil {
			return origRename(oldpath, newpath)
		}
		return os.Rename(oldpath, newpath)
	}

	if err := RemoveRuntimeIntegrationSkillsWith(d, IntegrationSkillPane); err != nil {
		t.Fatalf("RemoveRuntimeIntegrationSkillsWith() error: %v", err)
	}
	if renameTarget != runtimePath {
		t.Fatalf("atomic rename target = %q, want %q", renameTarget, runtimePath)
	}
}

func TestRuntimeDocumentRoundTripPreservesExtraKeys(t *testing.T) {
	d, runtimePath := runtimeTestDeps(t)
	writeRuntimeFile(t, runtimePath, `
[integrations]
skills = ["tasks", "pane"]

[future]
enabled = true
count = 2
`)

	if err := RemoveRuntimeIntegrationSkillsWith(d, IntegrationSkillPane); err != nil {
		t.Fatal(err)
	}

	var doc map[string]any
	if _, err := toml.DecodeFile(runtimePath, &doc); err != nil {
		t.Fatal(err)
	}
	future, ok := doc["future"].(map[string]any)
	if !ok {
		t.Fatalf("future section missing from %#v", doc)
	}
	if future["enabled"] != true || future["count"] != int64(2) {
		t.Fatalf("future section corrupted: %#v", future)
	}
}
