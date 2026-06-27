package config

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/glebglazov/pop/internal/deps"
)

func writeConfigFile(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func testLoadWithDataHome(t *testing.T, userBody string) (*Config, string) {
	t.Helper()
	root := t.TempDir()
	dataDir := filepath.Join(root, "data")
	configDir := filepath.Join(root, "config")
	userPath := filepath.Join(configDir, "config.toml")
	writeConfigFile(t, userPath, userBody)

	d := &Deps{FS: &deps.MockFileSystem{
		GetenvFunc: func(key string) string {
			switch key {
			case "XDG_DATA_HOME":
				return dataDir
			default:
				return ""
			}
		},
		UserHomeDirFunc: func() (string, error) { return filepath.Join(root, "home"), nil },
		ReadFileFunc:    os.ReadFile,
		StatFunc:        os.Stat,
	}}

	cfg, err := LoadWith(d, userPath)
	if err != nil {
		t.Fatalf("LoadWith() error: %v", err)
	}
	return cfg, userPath
}

func TestLoadIntegrationsDefaultsWithoutRuntimeOrUserSection(t *testing.T) {
	cfg, _ := testLoadWithDataHome(t, `projects = [{ path = "/main" }]`)

	skills, err := cfg.IntegrationsSkills()
	if err != nil {
		t.Fatalf("IntegrationsSkills() error: %v", err)
	}
	want := []string{"tasks", "pane"}
	if !reflect.DeepEqual(skills, want) {
		t.Fatalf("IntegrationsSkills() = %#v, want %#v", skills, want)
	}
}

func TestLoadIntegrationsUserOverridesDefaults(t *testing.T) {
	cfg, _ := testLoadWithDataHome(t, `
projects = [{ path = "/main" }]

[integrations]
skills = ["tasks"]
`)

	skills, err := cfg.IntegrationsSkills()
	if err != nil {
		t.Fatalf("IntegrationsSkills() error: %v", err)
	}
	want := []string{"tasks"}
	if !reflect.DeepEqual(skills, want) {
		t.Fatalf("IntegrationsSkills() = %#v, want %#v", skills, want)
	}
}

func TestLoadIntegrationsThreeLayerPrecedence(t *testing.T) {
	root := t.TempDir()
	dataDir := filepath.Join(root, "data")
	configDir := filepath.Join(root, "config")
	userPath := filepath.Join(configDir, "config.toml")
	runtimePath := filepath.Join(dataDir, "pop", "config.runtime.toml")

	writeConfigFile(t, runtimePath, `
[integrations]
skills = ["pane"]
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
		StatFunc:        os.Stat,
	}}

	cfg, err := LoadWith(d, userPath)
	if err != nil {
		t.Fatalf("LoadWith() error: %v", err)
	}
	skills, err := cfg.IntegrationsSkills()
	if err != nil {
		t.Fatalf("IntegrationsSkills() error: %v", err)
	}
	if !reflect.DeepEqual(skills, []string{"tasks"}) {
		t.Fatalf("IntegrationsSkills() = %#v, want [tasks] (user wins over runtime and defaults)", skills)
	}
}

func TestLoadIntegrationsRuntimeBetweenDefaultsAndUser(t *testing.T) {
	root := t.TempDir()
	dataDir := filepath.Join(root, "data")
	userPath := filepath.Join(root, "config.toml")
	runtimePath := filepath.Join(dataDir, "pop", "config.runtime.toml")

	writeConfigFile(t, runtimePath, `
[integrations]
skills = ["pane"]
`)
	writeConfigFile(t, userPath, `projects = [{ path = "/main" }]`)

	d := &Deps{FS: &deps.MockFileSystem{
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

	cfg, err := LoadWith(d, userPath)
	if err != nil {
		t.Fatalf("LoadWith() error: %v", err)
	}
	skills, err := cfg.IntegrationsSkills()
	if err != nil {
		t.Fatalf("IntegrationsSkills() error: %v", err)
	}
	if !reflect.DeepEqual(skills, []string{"pane"}) {
		t.Fatalf("IntegrationsSkills() = %#v, want [pane] (runtime overrides defaults)", skills)
	}
}

func TestLoadIntegrationsInvalidAliasInUserConfig(t *testing.T) {
	cfg, path := testLoadWithDataHome(t, `
[integrations]
skills = ["bogus"]
`)

	if len(cfg.Findings) != 1 {
		t.Fatalf("expected 1 finding, got %d: %+v", len(cfg.Findings), cfg.Findings)
	}
	f := cfg.Findings[0]
	if f.Path != "integrations.skills[0]" {
		t.Errorf("finding path = %q, want integrations.skills[0]", f.Path)
	}
	if !strings.Contains(f.Message, path) ||
		!strings.Contains(f.Message, `unknown integration skill alias "bogus"`) ||
		!strings.Contains(f.Message, "valid aliases: pane, tasks") {
		t.Errorf("finding message = %q, want alias diagnostic", f.Message)
	}
	if _, err := cfg.IntegrationsSkills(); err == nil {
		t.Fatal("IntegrationsSkills() = nil error, want blocking finding")
	}
}

func TestLoadIntegrationsInvalidAliasInRuntimeConfig(t *testing.T) {
	root := t.TempDir()
	dataDir := filepath.Join(root, "data")
	userPath := filepath.Join(root, "config.toml")
	runtimePath := filepath.Join(dataDir, "pop", "config.runtime.toml")

	writeConfigFile(t, runtimePath, `
[integrations]
skills = ["nope"]
`)
	writeConfigFile(t, userPath, `projects = [{ path = "/main" }]`)

	d := &Deps{FS: &deps.MockFileSystem{
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

	cfg, err := LoadWith(d, userPath)
	if err != nil {
		t.Fatalf("LoadWith() error: %v", err)
	}
	if len(cfg.Findings) != 1 {
		t.Fatalf("expected 1 finding, got %d: %+v", len(cfg.Findings), cfg.Findings)
	}
	if !strings.Contains(cfg.Findings[0].Message, runtimePath) {
		t.Errorf("finding should name runtime file, got %q", cfg.Findings[0].Message)
	}
	if _, err := cfg.IntegrationsSkills(); err == nil {
		t.Fatal("IntegrationsSkills() = nil error, want blocking finding")
	}
}

func TestDefaultRuntimeConfigPathWith(t *testing.T) {
	d := &Deps{FS: &deps.MockFileSystem{
		GetenvFunc: func(key string) string {
			if key == "XDG_DATA_HOME" {
				return "/custom/data"
			}
			return ""
		},
	}}
	got := DefaultRuntimeConfigPathWith(d)
	want := "/custom/data/pop/config.runtime.toml"
	if got != want {
		t.Fatalf("DefaultRuntimeConfigPathWith() = %q, want %q", got, want)
	}
}
