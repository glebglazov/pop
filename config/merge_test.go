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

// TestLoadIntegrationsTwoLayerPrecedence pins what is left of ADR-0065's merge
// now that the runtime tier is gone: the user's own list beats the embedded
// defaults, and nothing pop wrote sits between them.
func TestLoadIntegrationsTwoLayerPrecedence(t *testing.T) {
	root := t.TempDir()
	dataDir := filepath.Join(root, "data")
	userPath := filepath.Join(root, "config", "config.toml")

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
		t.Fatalf("IntegrationsSkills() = %#v, want [tasks] (the user's list over the defaults)", skills)
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

// TestConfigSchemaTagsAreLegal walks the whole Config type tree with the
// slice-01 drift check, so any merge:/include: tag naming an unknown kind, a
// malformed list-by-key, or a kind the field's Go type cannot support fails the
// test run instead of silently misbehaving when the overlay walker runs at load
// time — and so does a field with no overridability answer, which would
// otherwise inherit "overridable" in silence (ADR-0212 decision 4).
func TestConfigSchemaTagsAreLegal(t *testing.T) {
	if problems := checkSchemaTags(reflect.TypeOf(Config{})); len(problems) != 0 {
		t.Fatalf("Config has illegal schema tags:\n%s", strings.Join(problems, "\n"))
	}
}

// TestRepoScopeConfigSchemaTagsAreLegal runs the slice-01 drift check over the
// shared repo-scope schema (ADR-0083), which the repo-scope enumerator (slice
// 04) merges same-type via the walker. It catches a merge: tag naming an unknown
// kind, a malformed list-by-key, or a kind the field's Go type cannot support —
// e.g. workbenches' list-by-key=name key field must exist on Workbench.
func TestRepoScopeConfigSchemaTagsAreLegal(t *testing.T) {
	if problems := checkSchemaTags(reflect.TypeOf(RepoScopeConfig{})); len(problems) != 0 {
		t.Fatalf("RepoScopeConfig has illegal schema tags:\n%s", strings.Join(problems, "\n"))
	}
}
