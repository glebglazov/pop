package cmd

import (
	"bytes"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/BurntSushi/toml"
	"github.com/glebglazov/pop/config"
	"github.com/glebglazov/pop/internal/deps"
)

func integrateRuntimePath(t *testing.T) string {
	t.Helper()
	if xdg := os.Getenv("XDG_DATA_HOME"); xdg != "" {
		return filepath.Join(xdg, "pop", "config.runtime.toml")
	}
	t.Fatal("XDG_DATA_HOME must be set")
	return ""
}

func writeIntegrateRuntimeFile(t *testing.T, body string) {
	t.Helper()
	path := integrateRuntimePath(t)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func readIntegrateRuntimeSkills(t *testing.T) []string {
	t.Helper()
	path := integrateRuntimePath(t)
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		t.Fatal(err)
	}
	var doc map[string]any
	if _, err := toml.Decode(string(data), &doc); err != nil {
		t.Fatal(err)
	}
	integrations, _ := doc["integrations"].(map[string]any)
	if integrations == nil {
		return nil
	}
	raw, _ := integrations["skills"].([]any)
	skills := make([]string, 0, len(raw))
	for _, item := range raw {
		if s, ok := item.(string); ok {
			skills = append(skills, s)
		}
	}
	return skills
}

func TestIntegrateRuntimeConfig_NoPaneSkill_WritesRuntimeAndRemovesArtifacts(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	fs := newFakeFS()
	home := "/h"

	seedFileComponent(t, fs, home, ComponentPaneSkill, "claude")
	link := claudePaneLink(home)

	optOuts := map[ComponentID]bool{ComponentPaneSkill: true}
	if err := applyIntegrateRuntimeConfig(false, optOuts); err != nil {
		t.Fatalf("applyIntegrateRuntimeConfig: %v", err)
	}

	skills := readIntegrateRuntimeSkills(t)
	want := []string{"tasks"}
	if !reflect.DeepEqual(skills, want) {
		t.Fatalf("runtime skills = %#v, want %#v", skills, want)
	}

	var out bytes.Buffer
	d := fakeDeps(home, fs, &out)
	if err := runIntegrateComponents(d, "claude", []ComponentID{ComponentStatusWiring}, false, false, optOuts, false, false); err != nil {
		t.Fatalf("runIntegrateComponents: %v", err)
	}
	if _, ok := fs.symlinks[link]; ok {
		t.Fatalf("pane-skill symlink still present at %s", link)
	}
	if !strings.Contains(out.String(), "removed (opted out)") {
		t.Fatalf("expected removed outcome, got %q", out.String())
	}
}

func TestIntegrateRuntimeConfig_NoTaskSkills_WritesRuntimeAndRemovesArtifacts(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	fs := newFakeFS()
	home := "/h"

	seedFileComponent(t, fs, home, ComponentTaskSkills, "claude")
	linksBefore := len(fs.symlinks)

	optOuts := map[ComponentID]bool{ComponentTaskSkills: true}
	if err := applyIntegrateRuntimeConfig(false, optOuts); err != nil {
		t.Fatalf("applyIntegrateRuntimeConfig: %v", err)
	}

	skills := readIntegrateRuntimeSkills(t)
	want := []string{"pane"}
	if !reflect.DeepEqual(skills, want) {
		t.Fatalf("runtime skills = %#v, want %#v", skills, want)
	}

	var out bytes.Buffer
	d := fakeDeps(home, fs, &out)
	if err := runIntegrateComponents(d, "claude", []ComponentID{ComponentStatusWiring}, false, false, optOuts, false, false); err != nil {
		t.Fatalf("runIntegrateComponents: %v", err)
	}
	if len(fs.symlinks) >= linksBefore {
		t.Fatalf("expected task-skills symlinks removed, count still %d", len(fs.symlinks))
	}
	if !strings.Contains(out.String(), "removed (opted out)") {
		t.Fatalf("expected removed outcome, got %q", out.String())
	}
}

func TestIntegrateRuntimeConfig_BareIntegrateClearsRuntime(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	writeIntegrateRuntimeFile(t, `
[integrations]
skills = ["tasks"]
`)

	if err := applyIntegrateRuntimeConfig(true, nil); err != nil {
		t.Fatalf("applyIntegrateRuntimeConfig: %v", err)
	}
	if _, err := os.Stat(integrateRuntimePath(t)); !os.IsNotExist(err) {
		data, _ := os.ReadFile(integrateRuntimePath(t))
		t.Fatalf("expected runtime file removed after bare clear, got %q", string(data))
	}
}

func TestIntegrateRuntimeConfig_UserConfigWinsAfterBareClear(t *testing.T) {
	root := t.TempDir()
	dataDir := filepath.Join(root, "data")
	configDir := filepath.Join(root, "config")
	userPath := filepath.Join(configDir, "config.toml")
	runtimePath := filepath.Join(dataDir, "pop", "config.runtime.toml")

	t.Setenv("XDG_DATA_HOME", dataDir)
	if err := os.MkdirAll(filepath.Dir(runtimePath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(runtimePath, []byte(`
[integrations]
skills = ["pane"]
`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(configDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(userPath, []byte(`
projects = [{ path = "/main" }]

[integrations]
skills = ["tasks"]
`), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := applyIntegrateRuntimeConfig(true, nil); err != nil {
		t.Fatalf("applyIntegrateRuntimeConfig: %v", err)
	}

	d := &config.Deps{FS: &deps.MockFileSystem{
		GetenvFunc: func(key string) string {
			if key == "XDG_DATA_HOME" {
				return dataDir
			}
			return ""
		},
		UserHomeDirFunc: func() (string, error) { return filepath.Join(root, "home"), nil },
		ReadFileFunc:    os.ReadFile,
		StatFunc:        os.Stat,
	}}
	cfg, err := config.LoadWith(d, userPath)
	if err != nil {
		t.Fatalf("LoadWith: %v", err)
	}
	skills, err := cfg.IntegrationsSkills()
	if err != nil {
		t.Fatalf("IntegrationsSkills: %v", err)
	}
	want := []string{"tasks"}
	if !reflect.DeepEqual(skills, want) {
		t.Fatalf("IntegrationsSkills() = %#v, want %#v", skills, want)
	}
}

func TestIntegrateRuntimeConfig_VariadicNoFlagsOncePerInvocation(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())

	optOuts := map[ComponentID]bool{
		ComponentPaneSkill:  true,
		ComponentTaskSkills: true,
	}
	if err := applyIntegrateRuntimeConfig(false, optOuts); err != nil {
		t.Fatalf("first applyIntegrateRuntimeConfig: %v", err)
	}
	data, err := os.ReadFile(integrateRuntimePath(t))
	if err != nil {
		t.Fatalf("runtime file missing: %v", err)
	}
	if !strings.Contains(string(data), "skills = []") {
		t.Fatalf("expected empty skills array when both opted out, got %q", string(data))
	}

	// Second call is a no-op on an already-empty runtime skills list.
	if err := applyIntegrateRuntimeConfig(false, optOuts); err != nil {
		t.Fatalf("second applyIntegrateRuntimeConfig: %v", err)
	}
}

func TestIntegrateRuntimeConfig_NoPaneSkillFromExistingRuntime(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	writeIntegrateRuntimeFile(t, `
[integrations]
skills = ["tasks", "pane"]

[future]
enabled = true
`)

	optOuts := map[ComponentID]bool{ComponentPaneSkill: true}
	if err := applyIntegrateRuntimeConfig(false, optOuts); err != nil {
		t.Fatalf("applyIntegrateRuntimeConfig: %v", err)
	}

	data, err := os.ReadFile(integrateRuntimePath(t))
	if err != nil {
		t.Fatal(err)
	}
	body := string(data)
	if !strings.Contains(body, `skills = ["tasks"]`) {
		t.Fatalf("runtime skills not updated: %q", body)
	}
	if !strings.Contains(body, "[future]") {
		t.Fatalf("unrelated runtime keys lost: %q", body)
	}
}
