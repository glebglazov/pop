package config

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/glebglazov/pop/internal/deps"
)

func strPtr(s string) *string {
	return &s
}

func TestDefaultConfigPathWith(t *testing.T) {
	tests := []struct {
		name     string
		xdgHome  string
		userHome string
		expected string
	}{
		{
			name:     "uses XDG_CONFIG_HOME when set",
			xdgHome:  "/custom/config",
			userHome: "/home/user",
			expected: "/custom/config/pop/config.toml",
		},
		{
			name:     "falls back to ~/.config when XDG not set",
			xdgHome:  "",
			userHome: "/home/user",
			expected: "/home/user/.config/pop/config.toml",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d := &Deps{
				FS: &deps.MockFileSystem{
					GetenvFunc: func(key string) string {
						if key == "XDG_CONFIG_HOME" {
							return tt.xdgHome
						}
						return ""
					},
					UserHomeDirFunc: func() (string, error) {
						return tt.userHome, nil
					},
				},
			}

			result := DefaultConfigPathWith(d)

			if result != tt.expected {
				t.Errorf("DefaultConfigPathWith() = %q, want %q", result, tt.expected)
			}
		})
	}
}

func TestTaskAgentOutput(t *testing.T) {
	tests := []struct {
		name  string
		cfg   *Config
		agent string
		want  string
	}{
		{name: "nil config", want: "auto"},
		{name: "missing section", cfg: &Config{}, want: "auto"},
		{name: "empty value", cfg: &Config{Task: &TasksConfig{}}, want: "auto"},
		{
			name:  "configured text",
			cfg:   &Config{Task: &TasksConfig{Presets: map[string]TaskAgentConfig{"claude": {Output: "text"}}}},
			agent: "claude",
			want:  "text",
		},
		{
			name:  "other agent remains auto",
			cfg:   &Config{Task: &TasksConfig{Presets: map[string]TaskAgentConfig{"claude": {Output: "text"}}}},
			agent: "cursor",
			want:  "auto",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.cfg.TaskAgentOutput(tt.agent); got != tt.want {
				t.Fatalf("got %q, want %q", got, tt.want)
			}
		})
	}
}

func TestLoadRoutinesAgents(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(configPath, []byte(`
[routines]
agents = ["codex", "claude"]
`), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Routines == nil {
		t.Fatal("expected [routines] section to parse")
	}
	want := []string{"codex", "claude"}
	if !reflect.DeepEqual(cfg.Routines.Agents, want) {
		t.Fatalf("routines agents = %#v, want %#v", cfg.Routines.Agents, want)
	}
}

func TestLoadTasksImplementAgents(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(configPath, []byte(`
[tasks.implement]
agents = ["claude --model opus4.8", "codex"]
`), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Task == nil || cfg.Task.Implement == nil {
		t.Fatal("expected [tasks.implement] section to parse")
	}
	want := []string{"claude --model opus4.8", "codex"}
	if !reflect.DeepEqual(cfg.Task.Implement.Agents, want) {
		t.Fatalf("implement agents = %#v, want %#v", cfg.Task.Implement.Agents, want)
	}
}

func TestLoadWorkloadVerifyEnabled(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(configPath, []byte(`
[tasks.verify]
enabled = true
max_remediation_depth = 2
`), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Task == nil || cfg.Task.Verify == nil {
		t.Fatal("expected [tasks.verify] section to parse")
	}
	if !cfg.Task.Verify.Enabled {
		t.Fatal("expected [tasks.verify] enabled = true to load as enabled")
	}
	if cfg.Task.Verify.MaxRemediationDepth == nil || *cfg.Task.Verify.MaxRemediationDepth != 2 {
		t.Fatalf("expected max_remediation_depth = 2, got %v", cfg.Task.Verify.MaxRemediationDepth)
	}
}

func TestLoadTasksVerifyAgentsEffort(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(configPath, []byte(`
[tasks.verify]
enabled = true
agents = ["codex", "claude"]
effort = "standard"
`), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Task == nil || cfg.Task.Verify == nil {
		t.Fatal("expected [tasks.verify] section to parse")
	}
	want := []string{"codex", "claude"}
	if !reflect.DeepEqual(cfg.Task.Verify.Agents, want) {
		t.Fatalf("verify agents = %#v, want %#v", cfg.Task.Verify.Agents, want)
	}
	if cfg.Task.Verify.Effort != "standard" {
		t.Fatalf("verify effort = %q, want standard", cfg.Task.Verify.Effort)
	}
}

func TestWorkbenchPickOnCreate(t *testing.T) {
	// Defaults to false: nil receiver, nil section, and an empty section.
	var nilCfg *Config
	if nilCfg.WorkbenchPickOnCreate() {
		t.Error("nil config: WorkbenchPickOnCreate() = true, want false")
	}
	if (&Config{}).WorkbenchPickOnCreate() {
		t.Error("absent [workbench]: WorkbenchPickOnCreate() = true, want false")
	}
	if (&Config{WorkbenchOpts: &WorkbenchOptions{}}).WorkbenchPickOnCreate() {
		t.Error("[workbench] without pick_on_create: = true, want false")
	}
	if !(&Config{WorkbenchOpts: &WorkbenchOptions{PickOnCreate: true}}).WorkbenchPickOnCreate() {
		t.Error("pick_on_create=true: = false, want true")
	}
}

func TestLoadWorkbenchPickOnCreate(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(configPath, []byte(`
[workbench]
pick_on_create = true
`), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.WorkbenchPickOnCreate() {
		t.Fatal("expected [workbench] pick_on_create = true to load as enabled")
	}
}

func TestWorkbenchOrder(t *testing.T) {
	// Defaults to nil: nil receiver, nil section, and an empty section.
	var nilCfg *Config
	if nilCfg.WorkbenchOrder() != nil {
		t.Error("nil config: WorkbenchOrder() != nil")
	}
	if (&Config{}).WorkbenchOrder() != nil {
		t.Error("absent [workbench]: WorkbenchOrder() != nil")
	}
	if (&Config{WorkbenchOpts: &WorkbenchOptions{}}).WorkbenchOrder() != nil {
		t.Error("[workbench] without order: WorkbenchOrder() != nil")
	}
	got := (&Config{WorkbenchOpts: &WorkbenchOptions{Order: []string{"minimal", "<empty>"}}}).WorkbenchOrder()
	if len(got) != 2 || got[0] != "minimal" || got[1] != "<empty>" {
		t.Errorf("WorkbenchOrder() = %v, want [minimal <empty>]", got)
	}
}

func TestLoadWorkbenchOrder(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(configPath, []byte(`
[workbench]
order = ["minimal", "<reset>"]
`), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(configPath)
	if err != nil {
		t.Fatal(err)
	}
	got := cfg.WorkbenchOrder()
	if len(got) != 2 || got[0] != "minimal" || got[1] != "<reset>" {
		t.Fatalf("WorkbenchOrder() = %v, want [minimal <reset>]", got)
	}
}

func TestLoadTaskAgentOutput(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(configPath, []byte(`
[tasks.presets.claude]
output = "text"

[tasks.presets.cursor]
output = "auto"
`), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if got := cfg.TaskAgentOutput("claude"); got != "text" {
		t.Fatalf("claude output = %q, want text", got)
	}
	if got := cfg.TaskAgentOutput("cursor"); got != "auto" {
		t.Fatalf("cursor output = %q, want auto", got)
	}
}

func TestLoadWorkbenches(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(configPath, []byte(`
[[workbenches]]
name = "dev"

[[workbenches.windows]]
name = "work"

[workbenches.windows.layout]
name = "server"
command = "go test ./..."
`), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Workbenches) != 1 {
		t.Fatalf("got %d workbenches, want 1", len(cfg.Workbenches))
	}
	tmpl := cfg.Workbenches[0]
	if tmpl.Name != "dev" {
		t.Fatalf("template name = %q, want dev", tmpl.Name)
	}
	if len(tmpl.Windows) != 1 {
		t.Fatalf("got %d windows, want 1", len(tmpl.Windows))
	}
	window := tmpl.Windows[0]
	if window.Name != "work" {
		t.Fatalf("window name = %q, want work", window.Name)
	}
	if window.Layout == nil {
		t.Fatal("layout spec did not parse")
	}
	if window.Layout.Name != "server" || window.Layout.Command != "go test ./..." {
		t.Fatalf("layout spec = %#v, want name server and command go test ./...", *window.Layout)
	}
}

func TestLoadWorkbenchesMissingWindowName(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(configPath, []byte(`
[[workbenches]]
name = "bad"

[[workbenches.windows]]
# missing name

[workbenches.windows.layout]
name = "server"
command = "go test ./..."

[[workbenches]]
name = "good"

[[workbenches.windows]]
name = "work"

[workbenches.windows.layout]
name = "server"
command = "go test ./..."
`), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Workbenches) != 1 {
		t.Fatalf("got %d workbenches, want 1", len(cfg.Workbenches))
	}
	if cfg.Workbenches[0].Name != "good" {
		t.Fatalf("remaining template = %q, want good", cfg.Workbenches[0].Name)
	}

	foundFinding := false
	for _, f := range cfg.Findings {
		if strings.Contains(f.Message, "bad") && strings.Contains(f.Message, "missing") {
			foundFinding = true
			break
		}
	}
	if !foundFinding {
		t.Fatalf("expected missing-window-name finding, got %#v", cfg.Findings)
	}

	foundWarning := false
	for _, w := range cfg.Warnings {
		if strings.Contains(w, "bad") && strings.Contains(w, "missing") {
			foundWarning = true
			break
		}
	}
	if !foundWarning {
		t.Fatalf("expected missing-window-name warning, got %#v", cfg.Warnings)
	}
}

func TestLoadWorkbenchesDuplicateWindowName(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(configPath, []byte(`
[[workbenches]]
name = "bad"

[[workbenches.windows]]
name = "work"

[workbenches.windows.layout]
name = "server"
command = "go test ./..."

[[workbenches.windows]]
name = "work"

[workbenches.windows.layout]
name = "shell"
command = "bash"

[[workbenches]]
name = "good"

[[workbenches.windows]]
name = "review"

[workbenches.windows.layout]
name = "server"
command = "go test ./..."
`), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Workbenches) != 1 {
		t.Fatalf("got %d workbenches, want 1", len(cfg.Workbenches))
	}
	if cfg.Workbenches[0].Name != "good" {
		t.Fatalf("remaining template = %q, want good", cfg.Workbenches[0].Name)
	}

	foundFinding := false
	for _, f := range cfg.Findings {
		if strings.Contains(f.Message, "bad") && strings.Contains(f.Message, "duplicate") {
			foundFinding = true
			break
		}
	}
	if !foundFinding {
		t.Fatalf("expected duplicate-window-name finding, got %#v", cfg.Findings)
	}
}

func TestLoadWorkbenchesDuplicatePaneNameIsReapplyUnsafe(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(configPath, []byte(`
[[workbenches]]
name = "unsafe"

[[workbenches.windows]]
name = "dev"

[workbenches.windows.layout]
children = "rows"

[[workbenches.windows.layout.panes]]
name = "shell"
command = "bash"

[[workbenches.windows.layout.panes]]
name = "shell"
command = "zsh"
`), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(configPath)
	if err != nil {
		t.Fatal(err)
	}

	// Duplicate pane names are non-fatal: the template still loads (unlike a
	// duplicate window name, which excludes it).
	if len(cfg.Workbenches) != 1 || cfg.Workbenches[0].Name != "unsafe" {
		t.Fatalf("template should still load despite duplicate pane name, got %#v", cfg.Workbenches)
	}

	foundFinding := false
	for _, f := range cfg.Findings {
		if strings.Contains(f.Message, "duplicate pane name") && strings.Contains(f.Message, "reapply-unsafe") {
			foundFinding = true
			break
		}
	}
	if !foundFinding {
		t.Fatalf("expected reapply-unsafe duplicate-pane-name finding, got %#v", cfg.Findings)
	}
}

// TestLoadSessionTemplatesKeyIsUnrecognized asserts that the retired
// session_templates alias (ADR-0082) no longer loads as Workbenches: it is an
// ordinary unrecognized key now, not a rename nudge, so no data is loaded and
// no deprecation finding is recorded.
func TestLoadSessionTemplatesKeyIsUnrecognized(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(configPath, []byte(`
[[session_templates]]
name = "dev"

[[session_templates.windows]]
name = "work"

[session_templates.windows.layout]
name = "server"
command = "go test ./..."
`), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Workbenches) != 0 {
		t.Fatalf("got %d workbenches, want 0 (session_templates no longer loads)", len(cfg.Workbenches))
	}
	for _, f := range cfg.Findings {
		if strings.Contains(f.Path, "deprecated.session_templates") || strings.Contains(f.Message, "rename to") {
			t.Fatalf("unexpected deprecation-rename finding for retired session_templates key: %s", f.Message)
		}
	}
}

func TestLoadWorkbenchesIgnoresStaleSessionTemplatesKey(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(configPath, []byte(`
[[workbenches]]
name = "new"

[[workbenches.windows]]
name = "main"

[workbenches.windows.layout]
name = "editor"
command = "vim"

[[session_templates]]
name = "old"

[[session_templates.windows]]
name = "main"

[session_templates.windows.layout]
name = "editor"
command = "vim"
`), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(configPath)
	if err != nil {
		t.Fatal(err)
	}
	// Only the canonical key loads; the retired alias is ignored entirely.
	if len(cfg.Workbenches) != 1 {
		t.Fatalf("got %d workbenches, want 1", len(cfg.Workbenches))
	}
	if cfg.Workbenches[0].Name != "new" {
		t.Fatalf("template = %q, want new (session_templates ignored)", cfg.Workbenches[0].Name)
	}
}

func TestResolveWorkbenchesWith(t *testing.T) {
	t.Run("global [[workbenches]] resolves", func(t *testing.T) {
		tmpDir := t.TempDir()
		configPath := filepath.Join(tmpDir, "config.toml")
		if err := os.WriteFile(configPath, []byte(`
[[workbenches]]
name = "dev"
windows = [{name = "main", layout = {name = "editor", command = "vim"}}]
`), 0644); err != nil {
			t.Fatal(err)
		}

		cfg, err := Load(configPath)
		if err != nil {
			t.Fatalf("Load failed: %v", err)
		}

		d := &Deps{
			FS: &deps.MockFileSystem{
				StatFunc:         func(path string) (os.FileInfo, error) { return nil, os.ErrNotExist },
				ReadFileFunc:     func(path string) ([]byte, error) { return nil, os.ErrNotExist },
				EvalSymlinksFunc: func(path string) (string, error) { return path, nil },
				UserHomeDirFunc:  func() (string, error) { return tmpDir, nil },
			},
		}

		templates, warnings := cfg.ResolveWorkbenchesWith(d, tmpDir)
		if len(warnings) != 0 {
			t.Errorf("expected no warnings, got %v", warnings)
		}
		if len(templates) != 1 || templates[0].Name != "dev" {
			t.Fatalf("expected template 'dev', got %+v", templates)
		}
	})

	t.Run(".pop.toml [[workbenches]] resolves", func(t *testing.T) {
		tmpDir := t.TempDir()
		configPath := filepath.Join(tmpDir, "config.toml")
		if err := os.WriteFile(configPath, []byte(""), 0644); err != nil {
			t.Fatal(err)
		}

		popTomlPath := filepath.Join(tmpDir, ".pop.toml")
		if err := os.WriteFile(popTomlPath, []byte(`
[[workbenches]]
name = "repo-wb"
windows = [{name = "main", layout = {name = "editor", command = "vim"}}]
`), 0644); err != nil {
			t.Fatal(err)
		}

		cfg, err := Load(configPath)
		if err != nil {
			t.Fatalf("Load failed: %v", err)
		}

		d := &Deps{
			FS: &deps.MockFileSystem{
				StatFunc: func(path string) (os.FileInfo, error) { return nil, os.ErrNotExist },
				ReadFileFunc: func(path string) ([]byte, error) {
					if path == popTomlPath {
						return os.ReadFile(popTomlPath)
					}
					return nil, os.ErrNotExist
				},
				EvalSymlinksFunc: func(path string) (string, error) { return path, nil },
				UserHomeDirFunc:  func() (string, error) { return tmpDir, nil },
			},
		}

		templates, warnings := cfg.ResolveWorkbenchesWith(d, tmpDir)
		if len(warnings) != 0 {
			t.Errorf("expected no warnings, got %v", warnings)
		}
		if len(templates) != 1 || templates[0].Name != "repo-wb" {
			t.Fatalf("expected template 'repo-wb', got %+v", templates)
		}
	})

	t.Run("[repo] [[workbenches]] resolves", func(t *testing.T) {
		tmpDir := t.TempDir()
		configPath := filepath.Join(tmpDir, "config.toml")
		if err := os.WriteFile(configPath, []byte(fmt.Sprintf(`
[repo."%s"]
workbenches = [
  {name = "override", windows = [{name = "main", layout = {name = "editor", command = "vim"}}]}
]
`, tmpDir)), 0644); err != nil {
			t.Fatal(err)
		}

		cfg, err := Load(configPath)
		if err != nil {
			t.Fatalf("Load failed: %v", err)
		}

		d := &Deps{
			FS: &deps.MockFileSystem{
				StatFunc:         func(path string) (os.FileInfo, error) { return nil, os.ErrNotExist },
				ReadFileFunc:     func(path string) ([]byte, error) { return nil, os.ErrNotExist },
				EvalSymlinksFunc: func(path string) (string, error) { return path, nil },
				UserHomeDirFunc:  func() (string, error) { return tmpDir, nil },
			},
		}

		templates, warnings := cfg.ResolveWorkbenchesWith(d, tmpDir)
		if len(warnings) != 0 {
			t.Errorf("expected no warnings, got %v", warnings)
		}
		if len(templates) != 1 || templates[0].Name != "override" {
			t.Fatalf("expected template 'override', got %+v", templates)
		}
	})
}

func TestLoadEffortConfig(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(configPath, []byte(`
[effort.opencode]
heavy = [{ model = "opencode/claude-opus-4-8", reasoning = "turbo" }, { model = "opencode/kimi-k2.6" }]
standard = [{ model = "opencode/claude-sonnet-4-6", reasoning = "medium" }]
light = [{ model = "opencode/kimi-k2.6" }]
`), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(configPath)
	if err != nil {
		t.Fatal(err)
	}
	got := cfg.Effort["opencode"].Heavy
	want := []EffortModel{
		{Model: "opencode/claude-opus-4-8", Reasoning: "turbo"},
		{Model: "opencode/kimi-k2.6"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("opencode heavy effort = %#v, want %#v", got, want)
	}
}

// TestLoadEffortConfigUnknownTierYieldsFinding asserts that an unknown [effort]
// tier no longer aborts Load (ADR 0054): the load succeeds, the problem is
// recorded as a finding keyed to its config path, mirrored into the warning
// banner, and surfaced as the error of the effort getter for a consuming caller.
func TestLoadEffortConfigUnknownTierYieldsFinding(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(configPath, []byte(`
[effort.opencode]
extreme = [{ model = "opencode/claude-opus-4-8" }]
`), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(configPath)
	if err != nil {
		t.Fatalf("Load returned a fatal error for a stale effort tier: %v", err)
	}
	if len(cfg.Findings) != 1 {
		t.Fatalf("expected exactly 1 finding, got %d: %+v", len(cfg.Findings), cfg.Findings)
	}
	f := cfg.Findings[0]
	if f.Path != "effort.opencode.extreme" {
		t.Errorf("finding path = %q, want effort.opencode.extreme", f.Path)
	}
	if !strings.Contains(f.Message, `[effort.opencode] unknown tier "extreme"`) ||
		!strings.Contains(f.Message, "valid tiers: heavy, standard, light") {
		t.Errorf("finding message = %q, want unknown-tier diagnostic", f.Message)
	}
	if !containsSubstring(cfg.Warnings, "unknown tier") {
		t.Errorf("expected the effort finding mirrored into Warnings, got: %v", cfg.Warnings)
	}
	// A caller that consumes effort sees the finding as the getter's error.
	if _, err := cfg.EffortFor("opencode"); err == nil {
		t.Error("EffortFor returned nil error despite a blocking effort finding")
	} else if !strings.Contains(err.Error(), `unknown tier "extreme"`) {
		t.Errorf("EffortFor error = %v, want the unknown-tier finding", err)
	}
}

// TestLoadEffortConfigUnknownTierEntryKeyYieldsFinding mirrors the tier case for
// an unknown key inside an otherwise-valid tier.
func TestLoadEffortConfigUnknownTierEntryKeyYieldsFinding(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(configPath, []byte(`
[effort.opencode]
heavy = [{ model = "opencode/claude-opus-4-8", reasoning = "high", temperature = "low" }]
`), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(configPath)
	if err != nil {
		t.Fatalf("Load returned a fatal error for a stale effort entry key: %v", err)
	}
	if len(cfg.Findings) != 1 {
		t.Fatalf("expected exactly 1 finding, got %d: %+v", len(cfg.Findings), cfg.Findings)
	}
	f := cfg.Findings[0]
	if f.Path != "effort.opencode.heavy.temperature" {
		t.Errorf("finding path = %q, want effort.opencode.heavy.temperature", f.Path)
	}
	if !strings.Contains(f.Message, `[effort.opencode] tier "heavy" entry has unknown key "temperature"`) ||
		!strings.Contains(f.Message, "valid entry keys: model, reasoning") {
		t.Errorf("finding message = %q, want unknown-entry-key diagnostic", f.Message)
	}
	if _, err := cfg.EffortFor("opencode"); err == nil {
		t.Error("EffortFor returned nil error despite a blocking effort finding")
	}
}

// TestEffortForReturnsLadderWhenClean asserts the getter returns the configured
// ladder and a nil error when there is no blocking effort finding.
func TestEffortForReturnsLadderWhenClean(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(configPath, []byte(`
[effort.opencode]
heavy = [{ model = "opencode/claude-opus-4-8" }]
`), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(configPath)
	if err != nil {
		t.Fatal(err)
	}
	ladder, err := cfg.EffortFor("opencode")
	if err != nil {
		t.Fatalf("EffortFor returned error for a clean config: %v", err)
	}
	if len(ladder.Heavy) != 1 || ladder.Heavy[0].Model != "opencode/claude-opus-4-8" {
		t.Errorf("EffortFor ladder = %#v, want the configured heavy tier", ladder)
	}
	// An unconfigured agent is the zero ladder with a nil error, not a finding.
	if _, err := cfg.EffortFor("missing"); err != nil {
		t.Errorf("EffortFor(missing) = %v, want nil error", err)
	}
}

// TestLoadSyntaxErrorIsFatal asserts that unparseable TOML (class A) still hard
// fails Load — only semantic problems degrade to findings (ADR 0054).
func TestLoadSyntaxErrorIsFatal(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(configPath, []byte("this is = not valid = toml\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(configPath); err == nil {
		t.Fatal("Load accepted unparseable TOML; want a fatal error")
	}
}

// TestLoadInvalidDisplayDepthYieldsFinding asserts that a wrong-typed
// display_depth no longer aborts Load (ADR 0054): the load succeeds, every
// other entry survives, the bad entry falls back to the default depth, and the
// problem is recorded as a finding mirrored into the warning banner.
func TestLoadInvalidDisplayDepthYieldsFinding(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(configPath, []byte(`
projects = [
  { path = "~/bad", display_depth = "two" },
  { path = "~/good", display_depth = 2 },
]
`), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(configPath)
	if err != nil {
		t.Fatalf("Load returned a fatal error for a wrong-typed display_depth: %v", err)
	}
	if len(cfg.Projects) != 2 {
		t.Fatalf("expected both entries to survive decode, got %d: %+v", len(cfg.Projects), cfg.Projects)
	}
	if len(cfg.Findings) != 1 {
		t.Fatalf("expected exactly 1 finding, got %d: %+v", len(cfg.Findings), cfg.Findings)
	}
	f := cfg.Findings[0]
	if f.Path != "projects[].display_depth" {
		t.Errorf("finding path = %q, want projects[].display_depth", f.Path)
	}
	if !strings.Contains(f.Message, "non-integer display_depth") || !strings.Contains(f.Message, configPath) {
		t.Errorf("finding message = %q, want a file-qualified non-integer diagnostic", f.Message)
	}
	if !containsSubstring(cfg.Warnings, "non-integer display_depth") {
		t.Errorf("expected the display_depth finding mirrored into Warnings, got: %v", cfg.Warnings)
	}

	// The bad entry falls back to the default depth and surfaces the finding as
	// the getter's error; the good entry is clean.
	if d, err := cfg.Projects[0].GetDisplayDepth(); d != 1 || err == nil {
		t.Errorf("bad entry GetDisplayDepth() = %d, %v; want 1 with a finding error", d, err)
	}
	if d, err := cfg.Projects[1].GetDisplayDepth(); d != 2 || err != nil {
		t.Errorf("good entry GetDisplayDepth() = %d, %v; want 2, nil", d, err)
	}
}

// TestProjectEntriesSeverityBoundary asserts the projects-list getter's
// severity boundary (ADR 0054): a non-essential per-entry finding (display_depth)
// does not make the list fatal, while a blocking finding on the projects
// section's essentials does surface as the getter's error.
func TestProjectEntriesSeverityBoundary(t *testing.T) {
	// A display_depth finding is non-essential: ProjectEntries stays clean.
	cfg := &Config{
		Projects: []ProjectEntry{{Path: "/x"}},
		Findings: []Finding{{Path: "projects[].display_depth", Message: "bad depth"}},
	}
	if entries, err := cfg.ProjectEntries(); err != nil {
		t.Errorf("ProjectEntries() = %v; want nil error for a non-essential display_depth finding", err)
	} else if len(entries) != 1 {
		t.Errorf("ProjectEntries() returned %d entries, want 1", len(entries))
	}

	// A blocking finding scoped to the projects section is fatal at the getter.
	cfg2 := &Config{
		Projects: []ProjectEntry{{Path: "/x"}},
		Findings: []Finding{{Path: "projects", Message: "projects must be an array of tables"}},
	}
	if _, err := cfg2.ProjectEntries(); err == nil {
		t.Error("ProjectEntries() = nil error; want the blocking projects finding")
	} else if !strings.Contains(err.Error(), "array of tables") {
		t.Errorf("ProjectEntries() error = %v, want the projects finding", err)
	}
}

// TestExpandProjectsMalformedGlobWarnsAndPartiallyResolves asserts that a
// malformed glob degrades to a warning instead of aborting expansion: the good
// entry still resolves and the bad pattern is named in the warnings (ADR 0054).
func TestExpandProjectsMalformedGlobWarnsAndPartiallyResolves(t *testing.T) {
	t.Setenv("XDG_CACHE_HOME", t.TempDir())

	base := t.TempDir()
	child := filepath.Join(base, "repo")
	if err := os.Mkdir(child, 0o755); err != nil {
		t.Fatal(err)
	}

	cfg := &Config{
		Projects: []ProjectEntry{
			{Path: filepath.Join(base, "[a-") + "/*"}, // malformed glob
			{Path: filepath.Join(base, "*")},          // good glob → resolves child
		},
	}
	paths, err := cfg.ExpandProjects()
	if err != nil {
		t.Fatalf("ExpandProjects returned a fatal error despite a partially-resolving config: %v", err)
	}
	// The base tmpdir may be a symlink (macOS /var → /private/var), so the
	// resolved match path differs from `child`; compare by basename.
	var found bool
	for _, p := range paths {
		if filepath.Base(p.Path) == "repo" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected the good entry to resolve a %q dir; got %+v", child, paths)
	}
	if !containsSubstring(cfg.Warnings, "not a valid glob pattern") {
		t.Errorf("expected a warning naming the malformed glob, got: %v", cfg.Warnings)
	}
}

// containsSubstring reports whether any element of ss contains sub.
func containsSubstring(ss []string, sub string) bool {
	for _, s := range ss {
		if strings.Contains(s, sub) {
			return true
		}
	}
	return false
}

func TestResolveCommitConfigOverrides(t *testing.T) {
	tests := []struct {
		name    string
		cfg     *Config
		want    []string
		wantErr string
	}{
		{
			name: "nil config",
			cfg:  nil,
			want: nil,
		},
		{
			name: "no task section",
			cfg:  &Config{},
			want: nil,
		},
		{
			name: "no git sub-table",
			cfg:  &Config{Task: &TasksConfig{}},
			want: nil,
		},
		{
			name: "empty overrides",
			cfg:  &Config{Task: &TasksConfig{Git: &TaskGitConfig{CommitConfigOverrides: []string{}}}},
			want: nil,
		},
		{
			name: "valid entries including empty value",
			cfg:  &Config{Task: &TasksConfig{Git: &TaskGitConfig{CommitConfigOverrides: []string{"commit.gpgsign=false", "user.signingkey="}}}},
			want: []string{"commit.gpgsign=false", "user.signingkey="},
		},
		{
			name:    "missing equals",
			cfg:     &Config{Task: &TasksConfig{Git: &TaskGitConfig{CommitConfigOverrides: []string{"commit.gpgsign"}}}},
			wantErr: "[tasks.git] commit_config_overrides[0]:",
		},
		{
			name:    "empty key",
			cfg:     &Config{Task: &TasksConfig{Git: &TaskGitConfig{CommitConfigOverrides: []string{"=value"}}}},
			wantErr: "[tasks.git] commit_config_overrides[0]:",
		},
		{
			name:    "bad entry reports its index",
			cfg:     &Config{Task: &TasksConfig{Git: &TaskGitConfig{CommitConfigOverrides: []string{"commit.gpgsign=false", "oops"}}}},
			wantErr: "[tasks.git] commit_config_overrides[1]:",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := tt.cfg.ResolveCommitConfigOverrides()
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("err = %v, want containing %q", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("got %v, want %v", got, tt.want)
			}
		})
	}
}

// TestLoadCommitConfigOverridesDoesNotBreakGlobalLoad asserts that a malformed
// override entry is tolerated by global config Load (lazy validation): the
// dashboard/picker still opens; only the drain path surfaces the error.
func TestLoadCommitConfigOverridesDoesNotBreakGlobalLoad(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(configPath, []byte(`
[tasks.git]
commit_config_overrides = ["this-is-not-valid"]
`), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(configPath)
	if err != nil {
		t.Fatalf("global Load must tolerate a malformed override entry, got: %v", err)
	}
	// The malformed entry only surfaces when the drain lazily resolves it.
	if _, err := cfg.ResolveCommitConfigOverrides(); err == nil {
		t.Fatal("expected ResolveCommitConfigOverrides to reject the malformed entry")
	}
}

func TestLoadCommitConfigOverrides(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(configPath, []byte(`
[tasks.git]
commit_config_overrides = ["commit.gpgsign=false"]
`), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(configPath)
	if err != nil {
		t.Fatal(err)
	}
	got, err := cfg.ResolveCommitConfigOverrides()
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, []string{"commit.gpgsign=false"}) {
		t.Fatalf("got %v, want [commit.gpgsign=false]", got)
	}
}

func TestLoadQueueConfig(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(configPath, []byte(`
[queue]
agents = ["claude --model opus4.8", "codex", "opencode"]
poll_interval = "30s"
agent_quota_retry_after = "2h"
crash_retry_delays = ["10s", "1m", "5m"]
`), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Queue == nil {
		t.Fatal("expected [queue] section to parse")
	}
	if len(cfg.Warnings) != 1 {
		t.Fatalf("expected 1 warning for deprecated [queue].agents, got %d: %v", len(cfg.Warnings), cfg.Warnings)
	}
	if !strings.Contains(cfg.Warnings[0], "[queue] agents") || !strings.Contains(cfg.Warnings[0], "[tasks.implement].agents") {
		t.Fatalf("warning = %q, want [queue] agents ignored with pointer to [tasks.implement].agents", cfg.Warnings[0])
	}
	resolved, err := cfg.ResolveQueue()
	if err != nil {
		t.Fatal(err)
	}
	if resolved.PollInterval != 30*time.Second {
		t.Fatalf("poll interval = %s, want 30s", resolved.PollInterval)
	}
	if resolved.AgentQuotaRetryAfter != 2*time.Hour {
		t.Fatalf("quota retry = %s, want 2h", resolved.AgentQuotaRetryAfter)
	}
	if want := []time.Duration{10 * time.Second, time.Minute, 5 * time.Minute}; !reflect.DeepEqual(resolved.CrashRetryDelays, want) {
		t.Fatalf("crash retry delays = %#v, want %#v", resolved.CrashRetryDelays, want)
	}
}

func TestLoadRepoConfigDirectives(t *testing.T) {
	tests := []struct {
		name    string
		body    *string
		wantErr string
	}{
		{name: "absent"},
		{name: "worktree_ready causes error", body: strPtr("worktree_ready = true\n"), wantErr: "worktree_ready was removed"},
		{name: "execution_base causes error", body: strPtr("execution_base = true\n"), wantErr: "execution_base was renamed to trunk"},
		{name: "queue_base causes error", body: strPtr("queue_base = true\n"), wantErr: "queue_base was renamed to trunk"},
		{name: "malformed", body: strPtr("trunk =\n"), wantErr: ".pop.toml"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root := t.TempDir()
			if tt.body != nil {
				if err := os.WriteFile(filepath.Join(root, ".pop.toml"), []byte(*tt.body), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			got, err := LoadRepoConfig(root)
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("err = %v, want containing %q", err, tt.wantErr)
				}
				if got.Trunk {
					t.Fatalf("malformed config must degrade to zero repo config, got %+v", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}

// TestPopTOMLScopeLegality asserts that .pop.toml accepts only shared repo-scope
// keys (ADR-0083): a global-only key or the [repo]-only trunk is ignored but
// surfaces a non-fatal finding, while valid repo-scope keys still load
// (ADR-0054 degrade-not-abort).
func TestPopTOMLScopeLegality(t *testing.T) {
	loadBody := func(t *testing.T, body string) RepoConfig {
		t.Helper()
		root := t.TempDir()
		if err := os.WriteFile(filepath.Join(root, ".pop.toml"), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
		cfg, err := LoadRepoConfig(root)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		return cfg
	}

	findingFor := func(cfg RepoConfig, needle string) bool {
		for _, f := range cfg.Findings {
			if strings.Contains(f.Message, needle) {
				return true
			}
		}
		return false
	}

	t.Run("global-only key rejected, non-fatal", func(t *testing.T) {
		cfg := loadBody(t, "projects = [\"~/Dev/*\"]\n")
		if len(cfg.Findings) != 1 {
			t.Fatalf("want 1 finding, got %d: %+v", len(cfg.Findings), cfg.Findings)
		}
		if !findingFor(cfg, "projects") {
			t.Errorf("finding should name the offending key: %+v", cfg.Findings)
		}
	})

	t.Run("global-only table key rejected", func(t *testing.T) {
		cfg := loadBody(t, "[queue]\nmax_open = 3\n")
		if !findingFor(cfg, "queue") {
			t.Errorf("global-only [queue] table should produce a finding: %+v", cfg.Findings)
		}
	})

	t.Run("valid repo key accepted with no finding", func(t *testing.T) {
		cfg := loadBody(t, "preferred_workbench = \"gs-dev\"\n")
		if len(cfg.Findings) != 0 {
			t.Fatalf("valid repo key must not warn, got: %+v", cfg.Findings)
		}
		if cfg.PreferredWorkbench != "gs-dev" {
			t.Errorf("preferred_workbench = %q, want gs-dev", cfg.PreferredWorkbench)
		}
	})

	t.Run("trunk rejected as repo-only", func(t *testing.T) {
		cfg := loadBody(t, "trunk = true\n")
		if !findingFor(cfg, "trunk is only valid in a global") {
			t.Fatalf("trunk should produce a [repo]-only finding: %+v", cfg.Findings)
		}
		if cfg.Trunk {
			t.Error("trunk in .pop.toml must not be honored")
		}
	})

	t.Run("mixed file partially loads", func(t *testing.T) {
		cfg := loadBody(t, "preferred_workbench = \"gs-dev\"\nprojects = [\"~/Dev/*\"]\ntrunk = true\n")
		// Valid key still loads.
		if cfg.PreferredWorkbench != "gs-dev" {
			t.Errorf("valid key lost in mixed file: preferred_workbench = %q", cfg.PreferredWorkbench)
		}
		// Both illegal keys warned; neither aborts.
		if !findingFor(cfg, "projects") || !findingFor(cfg, "trunk is only valid in a global") {
			t.Errorf("both illegal keys should warn, got: %+v", cfg.Findings)
		}
	})

	// Criterion: adding a repo-scope key to the shared schema makes it accepted
	// in .pop.toml with no change to validation code — the legal set is generated.
	t.Run("legal set derived from shared schema", func(t *testing.T) {
		legal := repoScopeLegalKeys()
		if !legal["workbenches"] || !legal["preferred_workbench"] {
			t.Fatalf("legal set missing shared schema keys: %+v", legal)
		}
		if legal["trunk"] {
			t.Error("trunk must not be repo-scope-legal (it is [repo]-only)")
		}
		if legal["projects"] {
			t.Error("global-only projects must not be repo-scope-legal")
		}
	})
}

func TestPopTOMLPresenceDoesNotRegisterProject(t *testing.T) {
	root := t.TempDir()
	registered := filepath.Join(root, "registered")
	unregistered := filepath.Join(root, "unregistered")
	if err := os.MkdirAll(registered, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(unregistered, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(unregistered, ".pop.toml"), []byte("# pop repo config\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	real := deps.NewRealFileSystem()
	d := &Deps{FS: &deps.MockFileSystem{
		GetenvFunc: func(key string) string {
			if key == "XDG_DATA_HOME" {
				return filepath.Join(root, "data")
			}
			return ""
		},
		UserHomeDirFunc:  real.UserHomeDir,
		StatFunc:         real.Stat,
		ReadDirFunc:      real.ReadDir,
		ReadFileFunc:     real.ReadFile,
		WriteFileFunc:    real.WriteFile,
		MkdirAllFunc:     real.MkdirAll,
		RenameFunc:       real.Rename,
		RemoveAllFunc:    real.RemoveAll,
		DirFSFunc:        real.DirFS,
		EvalSymlinksFunc: real.EvalSymlinks,
	}}
	cfg := &Config{Projects: []ProjectEntry{{Path: registered}}}
	wantPath, err := real.EvalSymlinks(registered)
	if err != nil {
		t.Fatal(err)
	}

	projects, err := cfg.ExpandProjectsWith(d)
	if err != nil {
		t.Fatal(err)
	}
	if len(projects) != 1 || projects[0].Path != wantPath {
		t.Fatalf("projects = %+v, want only %s", projects, wantPath)
	}
}

func TestResolveSkillsPrefix(t *testing.T) {
	empty := ""
	custom := "my-"
	tests := []struct {
		name string
		cfg  *Config
		want string
	}{
		{name: "nil config", cfg: nil, want: DefaultSkillsPrefix},
		{name: "missing section", cfg: &Config{}, want: DefaultSkillsPrefix},
		{name: "section without key", cfg: &Config{Integrations: &IntegrationsConfig{}}, want: DefaultSkillsPrefix},
		{name: "explicit empty", cfg: &Config{Integrations: &IntegrationsConfig{SkillsPrefix: &empty}}, want: ""},
		{name: "explicit custom", cfg: &Config{Integrations: &IntegrationsConfig{SkillsPrefix: &custom}}, want: "my-"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.cfg.ResolveSkillsPrefix(); got != tt.want {
				t.Fatalf("ResolveSkillsPrefix() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestLoadSkillsPrefixFromTOML(t *testing.T) {
	t.Run("absent defaults to pop-", func(t *testing.T) {
		configPath := filepath.Join(t.TempDir(), "config.toml")
		if err := os.WriteFile(configPath, []byte("exclude_current_session = true\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		cfg, err := Load(configPath)
		if err != nil {
			t.Fatal(err)
		}
		if got := cfg.ResolveSkillsPrefix(); got != DefaultSkillsPrefix {
			t.Fatalf("ResolveSkillsPrefix() = %q, want %q", got, DefaultSkillsPrefix)
		}
	})

	t.Run("explicit empty installs bare", func(t *testing.T) {
		configPath := filepath.Join(t.TempDir(), "config.toml")
		if err := os.WriteFile(configPath, []byte("[integrations]\nskills_prefix = \"\"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		cfg, err := Load(configPath)
		if err != nil {
			t.Fatal(err)
		}
		if got := cfg.ResolveSkillsPrefix(); got != "" {
			t.Fatalf("ResolveSkillsPrefix() = %q, want %q", got, "")
		}
	})
}

func TestResolveQueueDefaults(t *testing.T) {
	tests := []struct {
		name string
		cfg  *Config
	}{
		{name: "nil config", cfg: nil},
		{name: "missing section", cfg: &Config{}},
		{name: "empty section", cfg: &Config{Queue: &QueueConfig{}}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := tt.cfg.ResolveQueue()
			if err != nil {
				t.Fatal(err)
			}
			if got.PollInterval != DefaultQueuePollInterval {
				t.Fatalf("poll interval = %s, want %s", got.PollInterval, DefaultQueuePollInterval)
			}
			if got.AgentQuotaRetryAfter != DefaultQueueQuotaRetryAfter {
				t.Fatalf("quota retry = %s, want %s", got.AgentQuotaRetryAfter, DefaultQueueQuotaRetryAfter)
			}
			if !reflect.DeepEqual(got.CrashRetryDelays, DefaultQueueCrashRetryDelays) {
				t.Fatalf("crash retry delays = %#v, want %#v", got.CrashRetryDelays, DefaultQueueCrashRetryDelays)
			}
		})
	}
}

func TestLoadQueueConfigNoAgentsWarning(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(configPath, []byte(`
[queue]
poll_interval = "30s"
`), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(configPath)
	if err != nil {
		t.Fatal(err)
	}
	for _, w := range cfg.Warnings {
		if strings.Contains(w, "agents") {
			t.Fatalf("expected no agents-related warning, got: %v", cfg.Warnings)
		}
	}
}

func TestResolveQueueDurationErrors(t *testing.T) {
	tests := []struct {
		name string
		cfg  *Config
		want string
	}{
		{
			name: "poll interval",
			cfg:  &Config{Queue: &QueueConfig{PollInterval: "soon"}},
			want: "[queue] poll_interval",
		},
		{
			name: "quota retry",
			cfg:  &Config{Queue: &QueueConfig{AgentQuotaRetryAfter: "later"}},
			want: "[queue] agent_quota_retry_after",
		},
		{
			name: "crash retry list",
			cfg:  &Config{Queue: &QueueConfig{CrashRetryDelays: []string{"1s", "bad"}}},
			want: "[queue] crash_retry_delays[1]",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := tt.cfg.ResolveQueue()
			if err == nil {
				t.Fatal("expected error")
			}
			if !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("error = %q, want it to contain %q", err, tt.want)
			}
		})
	}
}

func TestResolveAttemptRetryDelays(t *testing.T) {
	tests := []struct {
		name string
		cfg  *Config
		want []time.Duration
	}{
		{name: "nil config", cfg: nil, want: DefaultTaskAttemptRetryDelays},
		{name: "missing section", cfg: &Config{}, want: DefaultTaskAttemptRetryDelays},
		{name: "empty list", cfg: &Config{Task: &TasksConfig{AttemptRetryDelays: []string{}}}, want: []time.Duration{}},
		{
			name: "custom list",
			cfg: &Config{Task: &TasksConfig{AttemptRetryDelays: []string{"10s", "1m"}}},
			want: []time.Duration{10 * time.Second, time.Minute},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := tt.cfg.ResolveAttemptRetryDelays()
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("delays = %#v, want %#v", got, tt.want)
			}
		})
	}
}

func TestResolveAttemptRetryDelaysError(t *testing.T) {
	_, err := (&Config{Task: &TasksConfig{AttemptRetryDelays: []string{"bad"}}}).ResolveAttemptRetryDelays()
	if err == nil || !strings.Contains(err.Error(), "[tasks] attempt_retry_delays[0]") {
		t.Fatalf("error = %v", err)
	}
}

func TestResolveImplementMaxTriesFromConfig(t *testing.T) {
	root := 4
	impl := 7
	tests := []struct {
		name string
		cfg  *Config
		want int
	}{
		{name: "nil config", cfg: nil, want: DefaultTaskMaxTries},
		{name: "root cap", cfg: &Config{Task: &TasksConfig{MaxTries: &root}}, want: 4},
		{name: "implement override", cfg: &Config{Task: &TasksConfig{
			MaxTries:  &root,
			Implement: &ImplementConfig{MaxTries: &impl},
		}}, want: 7},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.cfg.ResolveImplementMaxTries(); got != tt.want {
				t.Fatalf("max tries = %d, want %d", got, tt.want)
			}
		})
	}
}

func TestResolveVerifyMaxTriesFromConfig(t *testing.T) {
	root := 4
	verify := 9
	cfg := &Config{Task: &TasksConfig{
		MaxTries: &root,
		Verify:   &VerifyConfig{MaxTries: &verify},
	}}
	if got := cfg.ResolveVerifyMaxTries(); got != verify {
		t.Fatalf("verify max tries = %d, want %d", got, verify)
	}
	if got := cfg.ResolveImplementMaxTries(); got != root {
		t.Fatalf("implement max tries = %d, want root cap %d", got, root)
	}
}

func TestLoadTasksRetryConfig(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(configPath, []byte(`
[tasks]
max_tries = 5
attempt_retry_delays = ["10s", "30s"]

[tasks.implement]
max_tries = 8
`), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if got := cfg.ResolveImplementMaxTries(); got != 8 {
		t.Fatalf("implement max tries = %d, want 8", got)
	}
	delays, err := cfg.ResolveAttemptRetryDelays()
	if err != nil {
		t.Fatal(err)
	}
	want := []time.Duration{10 * time.Second, 30 * time.Second}
	if !reflect.DeepEqual(delays, want) {
		t.Fatalf("delays = %#v, want %#v", delays, want)
	}
}

func TestExpandProjectsWith(t *testing.T) {
	tests := []struct {
		name     string
		projects []ProjectEntry
		setupFS  func() *deps.MockFileSystem
		expected []ExpandedPath
	}{
		{
			name:     "expands home directory",
			projects: []ProjectEntry{{Path: "~/projects/myapp"}},
			setupFS: func() *deps.MockFileSystem {
				return &deps.MockFileSystem{
					UserHomeDirFunc: func() (string, error) {
						return "/home/user", nil
					},
					StatFunc: func(path string) (os.FileInfo, error) {
						if path == "/home/user/projects/myapp" {
							return deps.MockFileInfo{IsDirVal: true}, nil
						}
						return nil, os.ErrNotExist
					},
				}
			},
			expected: []ExpandedPath{{Path: "/home/user/projects/myapp", DisplayDepth: 1}},
		},
		{
			name:     "filters non-directories",
			projects: []ProjectEntry{{Path: "/projects/file.txt"}, {Path: "/projects/dir"}},
			setupFS: func() *deps.MockFileSystem {
				return &deps.MockFileSystem{
					StatFunc: func(path string) (os.FileInfo, error) {
						if path == "/projects/dir" {
							return deps.MockFileInfo{IsDirVal: true}, nil
						}
						if path == "/projects/file.txt" {
							return deps.MockFileInfo{IsDirVal: false}, nil
						}
						return nil, os.ErrNotExist
					},
				}
			},
			expected: []ExpandedPath{{Path: "/projects/dir", DisplayDepth: 1}},
		},
		{
			name:     "deduplicates paths",
			projects: []ProjectEntry{{Path: "/projects/app"}, {Path: "/projects/app"}},
			setupFS: func() *deps.MockFileSystem {
				return &deps.MockFileSystem{
					StatFunc: func(path string) (os.FileInfo, error) {
						return deps.MockFileInfo{IsDirVal: true}, nil
					},
				}
			},
			expected: []ExpandedPath{{Path: "/projects/app", DisplayDepth: 1}},
		},
		{
			name:     "handles non-existent paths",
			projects: []ProjectEntry{{Path: "/projects/nonexistent"}},
			setupFS: func() *deps.MockFileSystem {
				return &deps.MockFileSystem{
					StatFunc: func(path string) (os.FileInfo, error) {
						return nil, os.ErrNotExist
					},
				}
			},
			expected: nil,
		},
		{
			name:     "resolves symlinks to canonical paths",
			projects: []ProjectEntry{{Path: "/symlink/project"}},
			setupFS: func() *deps.MockFileSystem {
				return &deps.MockFileSystem{
					EvalSymlinksFunc: func(path string) (string, error) {
						if path == "/symlink/project" {
							return "/real/project", nil
						}
						return path, nil
					},
					StatFunc: func(path string) (os.FileInfo, error) {
						if path == "/real/project" {
							return deps.MockFileInfo{IsDirVal: true}, nil
						}
						return nil, os.ErrNotExist
					},
				}
			},
			expected: []ExpandedPath{{Path: "/real/project", DisplayDepth: 1}},
		},
		{
			name:     "deduplicates symlinks pointing to same path",
			projects: []ProjectEntry{{Path: "/symlink1/project"}, {Path: "/symlink2/project"}},
			setupFS: func() *deps.MockFileSystem {
				return &deps.MockFileSystem{
					EvalSymlinksFunc: func(path string) (string, error) {
						// Both symlinks resolve to the same real path
						if path == "/symlink1/project" || path == "/symlink2/project" {
							return "/real/project", nil
						}
						return path, nil
					},
					StatFunc: func(path string) (os.FileInfo, error) {
						if path == "/real/project" {
							return deps.MockFileInfo{IsDirVal: true}, nil
						}
						return nil, os.ErrNotExist
					},
				}
			},
			expected: []ExpandedPath{{Path: "/real/project", DisplayDepth: 1}},
		},
		{
			name:     "propagates display_depth",
			projects: []ProjectEntry{{Path: "/projects/app", DisplayDepth: 3}},
			setupFS: func() *deps.MockFileSystem {
				return &deps.MockFileSystem{
					StatFunc: func(path string) (os.FileInfo, error) {
						return deps.MockFileInfo{IsDirVal: true}, nil
					},
				}
			},
			expected: []ExpandedPath{{Path: "/projects/app", DisplayDepth: 3}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d := &Deps{FS: tt.setupFS()}
			cfg := &Config{Projects: tt.projects}

			result, err := cfg.ExpandProjectsWith(d)

			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if len(result) != len(tt.expected) {
				t.Errorf("got %d projects, want %d: %v", len(result), len(tt.expected), result)
				return
			}

			for i, p := range result {
				if p.Path != tt.expected[i].Path {
					t.Errorf("project[%d].Path = %q, want %q", i, p.Path, tt.expected[i].Path)
				}
				if p.DisplayDepth != tt.expected[i].DisplayDepth {
					t.Errorf("project[%d].DisplayDepth = %d, want %d", i, p.DisplayDepth, tt.expected[i].DisplayDepth)
				}
			}
		})
	}
}

func TestExpandHomeWith(t *testing.T) {
	tests := []struct {
		name     string
		path     string
		home     string
		expected string
	}{
		{
			name:     "expands tilde prefix",
			path:     "~/projects",
			home:     "/home/user",
			expected: "/home/user/projects",
		},
		{
			name:     "leaves absolute path unchanged",
			path:     "/absolute/path",
			home:     "/home/user",
			expected: "/absolute/path",
		},
		{
			name:     "leaves relative path unchanged",
			path:     "relative/path",
			home:     "/home/user",
			expected: "relative/path",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d := &Deps{
				FS: &deps.MockFileSystem{
					UserHomeDirFunc: func() (string, error) {
						return tt.home, nil
					},
				},
			}

			result := expandHomeWith(d, tt.path)

			if result != tt.expected {
				t.Errorf("expandHomeWith() = %q, want %q", result, tt.expected)
			}
		})
	}
}

func TestLoadUserDefinedCommands(t *testing.T) {
	tests := []struct {
		name          string
		toml          string
		expectedCmds  int
		checkFirstCmd func(t *testing.T, cmd UserDefinedCommand)
	}{
		{
			name: "loads single worktree command",
			toml: `
projects = [{ path = "~/Dev" }]

[[worktree.commands]]
key = "ctrl-l"
label = "cleanup"
command = "echo cleanup"
exit = true
`,
			expectedCmds: 1,
			checkFirstCmd: func(t *testing.T, cmd UserDefinedCommand) {
				if cmd.Key != "ctrl-l" {
					t.Errorf("Key = %q, want %q", cmd.Key, "ctrl-l")
				}
				if cmd.Label != "cleanup" {
					t.Errorf("Label = %q, want %q", cmd.Label, "cleanup")
				}
				if cmd.Command != "echo cleanup" {
					t.Errorf("Command = %q, want %q", cmd.Command, "echo cleanup")
				}
				if !cmd.Exit {
					t.Error("Exit = false, want true")
				}
			},
		},
		{
			name: "loads multiple worktree commands",
			toml: `
projects = [{ path = "~/Dev" }]

[[worktree.commands]]
key = "ctrl-l"
label = "cleanup"
command = "echo cleanup"
exit = true

[[worktree.commands]]
key = "ctrl-o"
label = "open"
command = "echo open"
exit = false
`,
			expectedCmds: 2,
			checkFirstCmd: func(t *testing.T, cmd UserDefinedCommand) {
				if cmd.Key != "ctrl-l" {
					t.Errorf("Key = %q, want %q", cmd.Key, "ctrl-l")
				}
			},
		},
		{
			name: "config without worktree section",
			toml: `
projects = [{ path = "~/Dev" }]
`,
			expectedCmds:  0,
			checkFirstCmd: nil,
		},
		{
			name: "exit defaults to false",
			toml: `
projects = [{ path = "~/Dev" }]

[[worktree.commands]]
key = "ctrl-t"
label = "test"
command = "echo test"
`,
			expectedCmds: 1,
			checkFirstCmd: func(t *testing.T, cmd UserDefinedCommand) {
				if cmd.Exit {
					t.Error("Exit = true, want false (default)")
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create temp config file
			tmpDir := t.TempDir()
			configPath := filepath.Join(tmpDir, "config.toml")
			if err := os.WriteFile(configPath, []byte(tt.toml), 0644); err != nil {
				t.Fatalf("failed to write temp config: %v", err)
			}

			cfg, err := Load(configPath)
			if err != nil {
				t.Fatalf("Load() error: %v", err)
			}

			// Check number of commands
			var cmdCount int
			if cfg.Worktree != nil {
				cmdCount = len(cfg.Worktree.Commands)
			}
			if cmdCount != tt.expectedCmds {
				t.Errorf("got %d commands, want %d", cmdCount, tt.expectedCmds)
			}

			// Check first command if expected
			if tt.checkFirstCmd != nil && cmdCount > 0 {
				tt.checkFirstCmd(t, cfg.Worktree.Commands[0])
			}
		})
	}
}

func TestProjectEntry(t *testing.T) {
	tests := []struct {
		name          string
		toml          string
		expectedCount int
		checkEntries  func(t *testing.T, entries []ProjectEntry)
	}{
		{
			name:          "object entry with display_depth",
			toml:          `projects = [{ path = "~/Dev/*/*", display_depth = 2 }]`,
			expectedCount: 1,
			checkEntries: func(t *testing.T, entries []ProjectEntry) {
				if entries[0].Path != "~/Dev/*/*" {
					t.Errorf("Path = %q, want %q", entries[0].Path, "~/Dev/*/*")
				}
				if d, err := entries[0].GetDisplayDepth(); err != nil || d != 2 {
					t.Errorf("GetDisplayDepth() = %d, %v, want 2, nil", d, err)
				}
			},
		},
		{
			name:          "object entry without display_depth defaults to 1",
			toml:          `projects = [{ path = "~/Dev/*" }]`,
			expectedCount: 1,
			checkEntries: func(t *testing.T, entries []ProjectEntry) {
				if entries[0].Path != "~/Dev/*" {
					t.Errorf("Path = %q, want %q", entries[0].Path, "~/Dev/*")
				}
				if d, err := entries[0].GetDisplayDepth(); err != nil || d != 1 {
					t.Errorf("GetDisplayDepth() = %d, %v, want 1, nil", d, err)
				}
			},
		},
		{
			name:          "multiple entries",
			toml:          `projects = [{ path = "~/simple/*" }, { path = "~/deep/*/*", display_depth = 2 }]`,
			expectedCount: 2,
			checkEntries: func(t *testing.T, entries []ProjectEntry) {
				if entries[0].Path != "~/simple/*" {
					t.Errorf("entries[0].Path = %q, want %q", entries[0].Path, "~/simple/*")
				}
				if d, err := entries[0].GetDisplayDepth(); err != nil || d != 1 {
					t.Errorf("entries[0].GetDisplayDepth() = %d, %v, want 1, nil", d, err)
				}
				if entries[1].Path != "~/deep/*/*" {
					t.Errorf("entries[1].Path = %q, want %q", entries[1].Path, "~/deep/*/*")
				}
				if d, err := entries[1].GetDisplayDepth(); err != nil || d != 2 {
					t.Errorf("entries[1].GetDisplayDepth() = %d, %v, want 2, nil", d, err)
				}
			},
		},
		{
			name: "array-of-tables syntax",
			toml: `
[[projects]]
path = "~/Dev/*"
display_depth = 3
`,
			expectedCount: 1,
			checkEntries: func(t *testing.T, entries []ProjectEntry) {
				if d, err := entries[0].GetDisplayDepth(); err != nil || d != 3 {
					t.Errorf("GetDisplayDepth() = %d, %v, want 3, nil", d, err)
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmpDir := t.TempDir()
			configPath := filepath.Join(tmpDir, "config.toml")
			if err := os.WriteFile(configPath, []byte(tt.toml), 0644); err != nil {
				t.Fatalf("failed to write config: %v", err)
			}
			cfg, err := Load(configPath)
			if err != nil {
				t.Fatalf("Load() error: %v", err)
			}
			if len(cfg.Projects) != tt.expectedCount {
				t.Fatalf("got %d projects, want %d", len(cfg.Projects), tt.expectedCount)
			}
			if tt.checkEntries != nil {
				tt.checkEntries(t, cfg.Projects)
			}
		})
	}
}

func TestUpdateNoticeEnabled(t *testing.T) {
	tests := []struct {
		name     string
		toml     string
		expected bool
	}{
		{
			name:     "defaults to true when section absent",
			toml:     `projects = [{ path = "~/Dev" }]`,
			expected: true,
		},
		{
			name:     "defaults to true when section present but key absent",
			toml:     "projects = [{ path = \"~/Dev\" }]\n[updates]",
			expected: true,
		},
		{
			name:     "explicit true",
			toml:     "projects = [{ path = \"~/Dev\" }]\n[updates]\nnotice_enabled = true",
			expected: true,
		},
		{
			name:     "explicit false disables",
			toml:     "projects = [{ path = \"~/Dev\" }]\n[updates]\nnotice_enabled = false",
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmpDir := t.TempDir()
			configPath := filepath.Join(tmpDir, "config.toml")
			if err := os.WriteFile(configPath, []byte(tt.toml), 0644); err != nil {
				t.Fatalf("failed to write config: %v", err)
			}
			cfg, err := Load(configPath)
			if err != nil {
				t.Fatalf("Load() error: %v", err)
			}
			if got := cfg.UpdateNoticeEnabled(); got != tt.expected {
				t.Errorf("UpdateNoticeEnabled() = %v, want %v", got, tt.expected)
			}
		})
	}

	// A nil receiver / nil Updates must not panic and defaults to true.
	if !(*Config)(nil).UpdateNoticeEnabled() {
		t.Errorf("nil Config UpdateNoticeEnabled() = false, want true")
	}
}

func TestDashboardZoomOnSwitch(t *testing.T) {
	tests := []struct {
		name     string
		toml     string
		expected bool
	}{
		{
			name:     "defaults to true when section absent",
			toml:     `projects = [{ path = "~/Dev" }]`,
			expected: true,
		},
		{
			name:     "defaults to true when section present but key absent",
			toml:     "projects = [{ path = \"~/Dev\" }]\n[dashboard]",
			expected: true,
		},
		{
			name:     "explicit true",
			toml:     "projects = [{ path = \"~/Dev\" }]\n[dashboard]\nzoom_on_switch = true",
			expected: true,
		},
		{
			name:     "explicit false focuses pane in place",
			toml:     "projects = [{ path = \"~/Dev\" }]\n[dashboard]\nzoom_on_switch = false",
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmpDir := t.TempDir()
			configPath := filepath.Join(tmpDir, "config.toml")
			if err := os.WriteFile(configPath, []byte(tt.toml), 0644); err != nil {
				t.Fatalf("failed to write config: %v", err)
			}
			cfg, err := Load(configPath)
			if err != nil {
				t.Fatalf("Load() error: %v", err)
			}
			if got := cfg.DashboardZoomOnSwitch(); got != tt.expected {
				t.Errorf("DashboardZoomOnSwitch() = %v, want %v", got, tt.expected)
			}
		})
	}

	// A nil receiver / nil Dashboard must not panic and defaults to true.
	if !(*Config)(nil).DashboardZoomOnSwitch() {
		t.Errorf("nil Config DashboardZoomOnSwitch() = false, want true")
	}
}

func TestGetDisambiguationStrategy(t *testing.T) {
	tests := []struct {
		name     string
		toml     string
		expected string
	}{
		{
			name:     "defaults to first_unique_segment when not set",
			toml:     `projects = [{ path = "~/Dev" }]`,
			expected: "first_unique_segment",
		},
		{
			name:     "explicit first_unique_segment",
			toml:     "projects = [{ path = \"~/Dev\" }]\ndisambiguation_strategy = \"first_unique_segment\"",
			expected: "first_unique_segment",
		},
		{
			name:     "explicit full_path",
			toml:     "projects = [{ path = \"~/Dev\" }]\ndisambiguation_strategy = \"full_path\"",
			expected: "full_path",
		},
		{
			name:     "invalid value defaults to first_unique_segment",
			toml:     "projects = [{ path = \"~/Dev\" }]\ndisambiguation_strategy = \"bogus\"",
			expected: "first_unique_segment",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmpDir := t.TempDir()
			configPath := filepath.Join(tmpDir, "config.toml")
			if err := os.WriteFile(configPath, []byte(tt.toml), 0644); err != nil {
				t.Fatalf("failed to write config: %v", err)
			}
			cfg, err := Load(configPath)
			if err != nil {
				t.Fatalf("Load() error: %v", err)
			}
			if cfg.GetDisambiguationStrategy() != tt.expected {
				t.Errorf("GetDisambiguationStrategy() = %q, want %q", cfg.GetDisambiguationStrategy(), tt.expected)
			}
		})
	}
}

func TestExpandProjectsRejectsDoubleStarGlob(t *testing.T) {
	tmpDir := t.TempDir()

	// Create nested dirs that ** would match
	os.MkdirAll(filepath.Join(tmpDir, "a", "b", "c"), 0755)

	cfg := &Config{Projects: []ProjectEntry{{Path: filepath.Join(tmpDir, "**")}}}
	result, err := cfg.ExpandProjects()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result) != 0 {
		t.Errorf("got %d projects, want 0 (** patterns should be skipped)", len(result))
	}
}

func TestGetQuickAccessModifier(t *testing.T) {
	tests := []struct {
		name     string
		value    string
		expected string
	}{
		{"default empty", "", "alt"},
		{"explicit alt", "alt", "alt"},
		{"explicit ctrl", "ctrl", "ctrl"},
		{"explicit disabled", "disabled", "disabled"},
		{"invalid value", "foo", "alt"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &Config{QuickAccessModifier: tt.value}
			if got := cfg.GetQuickAccessModifier(); got != tt.expected {
				t.Errorf("GetQuickAccessModifier() = %q, want %q", got, tt.expected)
			}
		})
	}
}

func TestExpandProjectsDisplayDepth(t *testing.T) {
	// Test that display_depth is propagated through expansion.
	// This test uses the real filesystem with temp directories.
	tmpDir := t.TempDir()

	// Create: tmpDir/work/app, tmpDir/personal/app
	os.MkdirAll(filepath.Join(tmpDir, "work", "app"), 0755)
	os.MkdirAll(filepath.Join(tmpDir, "personal", "app"), 0755)

	cfg := &Config{Projects: []ProjectEntry{{Path: filepath.Join(tmpDir, "*", "*"), DisplayDepth: 2}}}
	result, err := cfg.ExpandProjects()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(result) != 2 {
		t.Fatalf("got %d projects, want 2: %v", len(result), result)
	}

	for _, ep := range result {
		if ep.DisplayDepth != 2 {
			t.Errorf("path %q: DisplayDepth = %d, want 2", ep.Path, ep.DisplayDepth)
		}
	}
}

func TestExpandProjectsSkipsHiddenDirs(t *testing.T) {
	tmpDir := t.TempDir()

	os.MkdirAll(filepath.Join(tmpDir, "visible"), 0755)
	os.MkdirAll(filepath.Join(tmpDir, ".hidden"), 0755)

	cfg := &Config{Projects: []ProjectEntry{{Path: filepath.Join(tmpDir, "*")}}}
	result, err := cfg.ExpandProjects()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(result) != 1 {
		t.Fatalf("got %d projects, want 1: %v", len(result), result)
	}
	if filepath.Base(result[0].Path) != "visible" {
		t.Errorf("expected 'visible', got %q", filepath.Base(result[0].Path))
	}
}

func TestRemoveSubsumedPaths(t *testing.T) {
	tests := []struct {
		name     string
		input    []ExpandedPath
		expected []ExpandedPath
	}{
		{
			name:     "empty input",
			input:    nil,
			expected: nil,
		},
		{
			name: "no overlap",
			input: []ExpandedPath{
				{Path: "/a", DisplayDepth: 1},
				{Path: "/b", DisplayDepth: 1},
				{Path: "/c", DisplayDepth: 1},
			},
			expected: []ExpandedPath{
				{Path: "/a", DisplayDepth: 1},
				{Path: "/b", DisplayDepth: 1},
				{Path: "/c", DisplayDepth: 1},
			},
		},
		{
			name: "simple parent-child",
			input: []ExpandedPath{
				{Path: "/a", DisplayDepth: 1},
				{Path: "/a/b", DisplayDepth: 2},
			},
			expected: []ExpandedPath{
				{Path: "/a/b", DisplayDepth: 2},
			},
		},
		{
			name: "transitive subsumption",
			input: []ExpandedPath{
				{Path: "/a", DisplayDepth: 1},
				{Path: "/a/b", DisplayDepth: 1},
				{Path: "/a/b/c", DisplayDepth: 3},
			},
			expected: []ExpandedPath{
				{Path: "/a/b/c", DisplayDepth: 3},
			},
		},
		{
			name: "multiple independent subsumptions",
			input: []ExpandedPath{
				{Path: "/a", DisplayDepth: 1},
				{Path: "/a/x", DisplayDepth: 2},
				{Path: "/b", DisplayDepth: 1},
				{Path: "/b/y", DisplayDepth: 2},
			},
			expected: []ExpandedPath{
				{Path: "/a/x", DisplayDepth: 2},
				{Path: "/b/y", DisplayDepth: 2},
			},
		},
		{
			name: "no false positive on common prefix",
			input: []ExpandedPath{
				{Path: "/foo/bar", DisplayDepth: 1},
				{Path: "/foo/barbaz", DisplayDepth: 1},
			},
			expected: []ExpandedPath{
				{Path: "/foo/bar", DisplayDepth: 1},
				{Path: "/foo/barbaz", DisplayDepth: 1},
			},
		},
		{
			name: "order independent — child before parent",
			input: []ExpandedPath{
				{Path: "/a/b", DisplayDepth: 2},
				{Path: "/a", DisplayDepth: 1},
			},
			expected: []ExpandedPath{
				{Path: "/a/b", DisplayDepth: 2},
			},
		},
		{
			name: "parent with multiple children",
			input: []ExpandedPath{
				{Path: "/proj", DisplayDepth: 1},
				{Path: "/proj/v1", DisplayDepth: 2},
				{Path: "/proj/v2", DisplayDepth: 2},
			},
			expected: []ExpandedPath{
				{Path: "/proj/v1", DisplayDepth: 2},
				{Path: "/proj/v2", DisplayDepth: 2},
			},
		},
		{
			name: "explicit parent not subsumed",
			input: []ExpandedPath{
				{Path: "/a", DisplayDepth: 1, Explicit: true},
				{Path: "/a/b", DisplayDepth: 2},
			},
			expected: []ExpandedPath{
				{Path: "/a", DisplayDepth: 1, Explicit: true},
				{Path: "/a/b", DisplayDepth: 2},
			},
		},
		{
			name: "non-explicit parent still subsumed",
			input: []ExpandedPath{
				{Path: "/a", DisplayDepth: 1},
				{Path: "/a/b", DisplayDepth: 2},
			},
			expected: []ExpandedPath{
				{Path: "/a/b", DisplayDepth: 2},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := removeSubsumedPaths(tt.input)

			if len(result) != len(tt.expected) {
				t.Fatalf("got %d paths, want %d: %v", len(result), len(tt.expected), result)
			}

			for i, p := range result {
				if p.Path != tt.expected[i].Path {
					t.Errorf("result[%d].Path = %q, want %q", i, p.Path, tt.expected[i].Path)
				}
				if p.DisplayDepth != tt.expected[i].DisplayDepth {
					t.Errorf("result[%d].DisplayDepth = %d, want %d", i, p.DisplayDepth, tt.expected[i].DisplayDepth)
				}
				if p.Explicit != tt.expected[i].Explicit {
					t.Errorf("result[%d].Explicit = %v, want %v", i, p.Explicit, tt.expected[i].Explicit)
				}
			}
		})
	}
}

func TestLoadIncludes(t *testing.T) {
	t.Run("basic include merges projects", func(t *testing.T) {
		tmpDir := t.TempDir()
		writeFile := func(name, content string) string {
			p := filepath.Join(tmpDir, name)
			if err := os.WriteFile(p, []byte(content), 0644); err != nil {
				t.Fatal(err)
			}
			return p
		}

		writeFile("work.toml", `projects = [{ path = "~/Work/*" }]`)
		configPath := writeFile("config.toml", `
includes = ["work.toml"]
projects = [{ path = "~/Personal/*" }]
`)

		cfg, err := Load(configPath)
		if err != nil {
			t.Fatalf("Load() error: %v", err)
		}
		if len(cfg.Projects) != 2 {
			t.Fatalf("got %d projects, want 2", len(cfg.Projects))
		}
		if cfg.Projects[0].Path != "~/Personal/*" {
			t.Errorf("projects[0].Path = %q, want %q", cfg.Projects[0].Path, "~/Personal/*")
		}
		if cfg.Projects[1].Path != "~/Work/*" {
			t.Errorf("projects[1].Path = %q, want %q", cfg.Projects[1].Path, "~/Work/*")
		}
	})

	t.Run("multiple includes in order", func(t *testing.T) {
		tmpDir := t.TempDir()
		writeFile := func(name, content string) string {
			p := filepath.Join(tmpDir, name)
			if err := os.WriteFile(p, []byte(content), 0644); err != nil {
				t.Fatal(err)
			}
			return p
		}

		writeFile("a.toml", `projects = [{ path = "/a" }]`)
		writeFile("b.toml", `projects = [{ path = "/b" }]`)
		configPath := writeFile("config.toml", `
includes = ["a.toml", "b.toml"]
projects = [{ path = "/main" }]
`)

		cfg, err := Load(configPath)
		if err != nil {
			t.Fatalf("Load() error: %v", err)
		}
		if len(cfg.Projects) != 3 {
			t.Fatalf("got %d projects, want 3", len(cfg.Projects))
		}
		expected := []string{"/main", "/a", "/b"}
		for i, want := range expected {
			if cfg.Projects[i].Path != want {
				t.Errorf("projects[%d].Path = %q, want %q", i, cfg.Projects[i].Path, want)
			}
		}
	})

	t.Run("tilde expansion in include path", func(t *testing.T) {
		tmpDir := t.TempDir()
		// Create the include file inside a "home" directory
		homeDir := filepath.Join(tmpDir, "home")
		os.MkdirAll(filepath.Join(homeDir, ".config", "pop"), 0755)

		includePath := filepath.Join(homeDir, ".config", "pop", "extra.toml")
		os.WriteFile(includePath, []byte(`projects = [{ path = "/extra" }]`), 0644)

		configPath := filepath.Join(tmpDir, "config.toml")
		os.WriteFile(configPath, []byte(`
includes = ["~/.config/pop/extra.toml"]
projects = [{ path = "/main" }]
`), 0644)

		d := &Deps{
			FS: &deps.MockFileSystem{
				UserHomeDirFunc: func() (string, error) {
					return homeDir, nil
				},
			},
		}

		cfg, err := LoadWith(d, configPath)
		if err != nil {
			t.Fatalf("LoadWith() error: %v", err)
		}
		if len(cfg.Projects) != 2 {
			t.Fatalf("got %d projects, want 2", len(cfg.Projects))
		}
		if cfg.Projects[1].Path != "/extra" {
			t.Errorf("projects[1].Path = %q, want %q", cfg.Projects[1].Path, "/extra")
		}
	})

	t.Run("relative path resolved against config dir", func(t *testing.T) {
		tmpDir := t.TempDir()
		subDir := filepath.Join(tmpDir, "conf")
		os.MkdirAll(subDir, 0755)

		os.WriteFile(filepath.Join(subDir, "extra.toml"), []byte(`projects = [{ path = "/extra" }]`), 0644)
		configPath := filepath.Join(subDir, "config.toml")
		os.WriteFile(configPath, []byte(`
includes = ["extra.toml"]
projects = [{ path = "/main" }]
`), 0644)

		cfg, err := Load(configPath)
		if err != nil {
			t.Fatalf("Load() error: %v", err)
		}
		if len(cfg.Projects) != 2 {
			t.Fatalf("got %d projects, want 2", len(cfg.Projects))
		}
	})

	t.Run("missing include file prints warning and continues", func(t *testing.T) {
		tmpDir := t.TempDir()
		configPath := filepath.Join(tmpDir, "config.toml")
		os.WriteFile(configPath, []byte(`
includes = ["nonexistent.toml"]
projects = [{ path = "/main" }]
`), 0644)

		cfg, err := Load(configPath)
		if err != nil {
			t.Fatalf("expected no error for missing include, got: %v", err)
		}
		if len(cfg.Projects) != 1 || cfg.Projects[0].Path != "/main" {
			t.Fatalf("expected 1 project from main config, got: %v", cfg.Projects)
		}
		if len(cfg.Warnings) != 1 {
			t.Fatalf("expected 1 warning, got: %v", cfg.Warnings)
		}
	})

	t.Run("include with non-whitelisted keys warns and ignores them", func(t *testing.T) {
		tmpDir := t.TempDir()
		writeFile := func(name, content string) string {
			p := filepath.Join(tmpDir, name)
			os.WriteFile(p, []byte(content), 0644)
			return p
		}

		writeFile("extra.toml", `
exclude_current_dir = true
disambiguation_strategy = "full_path"
quick_access_modifier = "ctrl"
projects = [{ path = "/extra" }]

[[worktree.commands]]
key = "ctrl-x"
label = "test"
command = "echo test"
`)
		configPath := writeFile("config.toml", `
includes = ["extra.toml"]
projects = [{ path = "/main" }]
`)

		cfg, err := Load(configPath)
		if err != nil {
			t.Fatalf("Load() error: %v", err)
		}
		// Main config values should be preserved (defaults)
		if cfg.ShouldExcludeCurrentSession() {
			t.Error("ShouldExcludeCurrentSession() should be false (main config default)")
		}
		if cfg.GetDisambiguationStrategy() != "first_unique_segment" {
			t.Errorf("DisambiguationStrategy = %q, want %q", cfg.GetDisambiguationStrategy(), "first_unique_segment")
		}
		if cfg.GetQuickAccessModifier() != "alt" {
			t.Errorf("QuickAccessModifier = %q, want %q", cfg.GetQuickAccessModifier(), "alt")
		}
		if cfg.Worktree != nil {
			t.Error("Worktree should be nil (from main config)")
		}
		// But projects should be merged
		if len(cfg.Projects) != 2 {
			t.Fatalf("got %d projects, want 2", len(cfg.Projects))
		}
		// Check for warnings about non-whitelisted keys
		warnCount := 0
		for _, w := range cfg.Warnings {
			if strings.Contains(w, "extra.toml") && strings.Contains(w, "ignored") {
				warnCount++
			}
		}
		if warnCount == 0 {
			t.Errorf("expected warnings about non-whitelisted keys, got: %v", cfg.Warnings)
		}
	})

	t.Run("include with nested includes warns and ignores them", func(t *testing.T) {
		tmpDir := t.TempDir()
		writeFile := func(name, content string) string {
			p := filepath.Join(tmpDir, name)
			os.WriteFile(p, []byte(content), 0644)
			return p
		}

		writeFile("nested.toml", `projects = [{ path = "/nested" }]`)
		writeFile("extra.toml", `
includes = ["nested.toml"]
projects = [{ path = "/extra" }]
`)
		configPath := writeFile("config.toml", `
includes = ["extra.toml"]
projects = [{ path = "/main" }]
`)

		cfg, err := Load(configPath)
		if err != nil {
			t.Fatalf("Load() error: %v", err)
		}
		// Nested includes should not be processed
		if len(cfg.Projects) != 2 {
			t.Fatalf("got %d projects, want 2 (nested include should be ignored)", len(cfg.Projects))
		}
		// Check for warning about nested includes
		found := false
		for _, w := range cfg.Warnings {
			if strings.Contains(w, "extra.toml") && strings.Contains(w, "nested") {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("expected warning about nested includes, got: %v", cfg.Warnings)
		}
	})

	t.Run("empty includes array works fine", func(t *testing.T) {
		tmpDir := t.TempDir()
		configPath := filepath.Join(tmpDir, "config.toml")
		os.WriteFile(configPath, []byte(`
includes = []
projects = [{ path = "/main" }]
`), 0644)

		cfg, err := Load(configPath)
		if err != nil {
			t.Fatalf("Load() error: %v", err)
		}
		if len(cfg.Projects) != 1 {
			t.Fatalf("got %d projects, want 1", len(cfg.Projects))
		}
	})

	t.Run("no includes field works fine", func(t *testing.T) {
		tmpDir := t.TempDir()
		configPath := filepath.Join(tmpDir, "config.toml")
		os.WriteFile(configPath, []byte(`projects = [{ path = "/main" }]`), 0644)

		cfg, err := Load(configPath)
		if err != nil {
			t.Fatalf("Load() error: %v", err)
		}
		if len(cfg.Projects) != 1 {
			t.Fatalf("got %d projects, want 1", len(cfg.Projects))
		}
	})

	t.Run("malformed include file is fatal error", func(t *testing.T) {
		tmpDir := t.TempDir()
		writeFile := func(name, content string) string {
			p := filepath.Join(tmpDir, name)
			os.WriteFile(p, []byte(content), 0644)
			return p
		}

		writeFile("malformed.toml", `
projects = [{ path = /unquoted/path }]
`)
		configPath := writeFile("config.toml", `
includes = ["malformed.toml"]
projects = [{ path = "/main" }]
`)

		_, err := Load(configPath)
		if err == nil {
			t.Fatalf("expected error for malformed include, got nil")
		}
		if !strings.Contains(err.Error(), "malformed.toml") {
			t.Errorf("error should name the include file, got: %v", err)
		}
	})

	t.Run("missing include file warns and continues", func(t *testing.T) {
		tmpDir := t.TempDir()
		configPath := filepath.Join(tmpDir, "config.toml")
		os.WriteFile(configPath, []byte(`
includes = ["nonexistent.toml"]
projects = [{ path = "/main" }]
`), 0644)

		cfg, err := Load(configPath)
		if err != nil {
			t.Fatalf("expected no error for missing include, got: %v", err)
		}
		if len(cfg.Projects) != 1 || cfg.Projects[0].Path != "/main" {
			t.Fatalf("expected 1 project from main config, got: %v", cfg.Projects)
		}
		if len(cfg.Warnings) == 0 {
			t.Fatalf("expected warning for missing include, got none")
		}
		// Verify the warning mentions the missing file
		found := false
		for _, w := range cfg.Warnings {
			if strings.Contains(w, "nonexistent.toml") {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("warning should name the missing include file, got: %v", cfg.Warnings)
		}
	})

	t.Run("include paths are literal (no glob expansion)", func(t *testing.T) {
		tmpDir := t.TempDir()
		writeFile := func(name, content string) string {
			p := filepath.Join(tmpDir, name)
			os.WriteFile(p, []byte(content), 0644)
			return p
		}

		// Create actual files that would match a glob
		writeFile("extra1.toml", `projects = [{ path = "/extra1" }]`)
		writeFile("extra2.toml", `projects = [{ path = "/extra2" }]`)

		configPath := writeFile("config.toml", fmt.Sprintf(`
includes = ["%s"]
projects = [{ path = "/main" }]
`, filepath.Join(tmpDir, "extra*.toml")))

		cfg, err := Load(configPath)
		if err != nil {
			// Glob path doesn't expand, so it's treated as literal and won't exist
			// This should result in a warning about missing include, not an error
			if !strings.Contains(err.Error(), "loading include") {
				t.Fatalf("expected error about missing literal include file, got: %v", err)
			}
			return
		}

		// If we get here, the glob was NOT expanded (correct behavior)
		// The include should have failed or warned since the literal glob path doesn't exist
		if len(cfg.Projects) != 1 {
			t.Errorf("glob pattern should not be expanded in include paths, got %d projects", len(cfg.Projects))
		}
	})

	t.Run("include-only repo block is merged", func(t *testing.T) {
		tmpDir := t.TempDir()
		writeFile := func(name, content string) string {
			p := filepath.Join(tmpDir, name)
			if err := os.WriteFile(p, []byte(content), 0644); err != nil {
				t.Fatal(err)
			}
			return p
		}

		writeFile("private.toml", `
[repo."/home/user/secret"]
trunk = true
`)
		configPath := writeFile("config.toml", `
includes = ["private.toml"]
projects = [{ path = "/main" }]
`)

		cfg, err := Load(configPath)
		if err != nil {
			t.Fatalf("Load() error: %v", err)
		}
		block, ok := cfg.Repo["/home/user/secret"]
		if !ok {
			t.Fatal("expected [repo.\"/home/user/secret\"] to be merged from include")
		}
		if block.Trunk == nil || !*block.Trunk {
			t.Error("trunk should be true")
		}
		if len(cfg.Warnings) != 0 {
			t.Errorf("expected no warnings, got: %v", cfg.Warnings)
		}
	})

	t.Run("parent repo block wins over include collision", func(t *testing.T) {
		tmpDir := t.TempDir()
		writeFile := func(name, content string) string {
			p := filepath.Join(tmpDir, name)
			if err := os.WriteFile(p, []byte(content), 0644); err != nil {
				t.Fatal(err)
			}
			return p
		}

		writeFile("extra.toml", `
[repo."/shared/repo"]
trunk = false
`)
		configPath := writeFile("config.toml", `
includes = ["extra.toml"]

[repo."/shared/repo"]
trunk = true
`)

		cfg, err := Load(configPath)
		if err != nil {
			t.Fatalf("Load() error: %v", err)
		}
		block, ok := cfg.Repo["/shared/repo"]
		if !ok {
			t.Fatal("expected [repo.\"/shared/repo\"] in effective config")
		}
		if block.Trunk == nil || !*block.Trunk {
			t.Error("parent's trunk=true should win")
		}
		if len(cfg.Warnings) != 1 {
			t.Fatalf("expected 1 collision warning, got %d: %v", len(cfg.Warnings), cfg.Warnings)
		}
		if !strings.Contains(cfg.Warnings[0], "/shared/repo") {
			t.Errorf("warning should name the repo key, got: %q", cfg.Warnings[0])
		}
	})

	t.Run("earlier include repo block wins over later include collision", func(t *testing.T) {
		tmpDir := t.TempDir()
		writeFile := func(name, content string) string {
			p := filepath.Join(tmpDir, name)
			if err := os.WriteFile(p, []byte(content), 0644); err != nil {
				t.Fatal(err)
			}
			return p
		}

		writeFile("first.toml", `
[repo."/shared/repo"]
trunk = true
`)
		writeFile("second.toml", `
[repo."/shared/repo"]
trunk = false
`)
		configPath := writeFile("config.toml", `
includes = ["first.toml", "second.toml"]
`)

		cfg, err := Load(configPath)
		if err != nil {
			t.Fatalf("Load() error: %v", err)
		}
		block, ok := cfg.Repo["/shared/repo"]
		if !ok {
			t.Fatal("expected [repo.\"/shared/repo\"] in effective config")
		}
		if block.Trunk == nil || !*block.Trunk {
			t.Error("first include's trunk=true should win over second include's false")
		}
		if len(cfg.Warnings) != 1 {
			t.Fatalf("expected 1 collision warning, got %d: %v", len(cfg.Warnings), cfg.Warnings)
		}
		if !strings.Contains(cfg.Warnings[0], "/shared/repo") {
			t.Errorf("warning should name the repo key, got: %q", cfg.Warnings[0])
		}
	})

	t.Run("include-only tasks implement agents is merged", func(t *testing.T) {
		tmpDir := t.TempDir()
		writeFile := func(name, content string) string {
			p := filepath.Join(tmpDir, name)
			if err := os.WriteFile(p, []byte(content), 0644); err != nil {
				t.Fatal(err)
			}
			return p
		}

		writeFile("private.toml", `
[tasks.implement]
agents = ["codex", "claude"]
`)
		configPath := writeFile("config.toml", `
includes = ["private.toml"]
projects = [{ path = "/main" }]
`)

		cfg, err := Load(configPath)
		if err != nil {
			t.Fatalf("Load() error: %v", err)
		}
		if cfg.Task == nil || cfg.Task.Implement == nil {
			t.Fatal("expected [tasks.implement] to be merged from include")
		}
		if got, want := cfg.Task.Implement.Agents, []string{"codex", "claude"}; !reflect.DeepEqual(got, want) {
			t.Fatalf("implement agents = %#v, want %#v", got, want)
		}
		for _, w := range cfg.Warnings {
			if strings.Contains(w, "tasks") && strings.Contains(w, "ignored") {
				t.Fatalf("unexpected tasks warning: %q", w)
			}
		}
	})

	t.Run("parent tasks implement agents wins over include collision", func(t *testing.T) {
		tmpDir := t.TempDir()
		writeFile := func(name, content string) string {
			p := filepath.Join(tmpDir, name)
			if err := os.WriteFile(p, []byte(content), 0644); err != nil {
				t.Fatal(err)
			}
			return p
		}

		writeFile("private.toml", `
[tasks.implement]
agents = ["codex"]
`)
		configPath := writeFile("config.toml", `
includes = ["private.toml"]

[tasks.implement]
agents = ["claude"]
`)

		cfg, err := Load(configPath)
		if err != nil {
			t.Fatalf("Load() error: %v", err)
		}
		if got, want := cfg.Task.Implement.Agents, []string{"claude"}; !reflect.DeepEqual(got, want) {
			t.Fatalf("implement agents = %#v, want %#v", got, want)
		}
		found := false
		for _, w := range cfg.Warnings {
			if strings.Contains(w, "agents skipped") {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("expected agents collision warning, got: %v", cfg.Warnings)
		}
	})

	t.Run("include-only effort ladder is merged", func(t *testing.T) {
		tmpDir := t.TempDir()
		writeFile := func(name, content string) string {
			p := filepath.Join(tmpDir, name)
			if err := os.WriteFile(p, []byte(content), 0644); err != nil {
				t.Fatal(err)
			}
			return p
		}

		writeFile("private.toml", `
[effort.claude]
heavy = [{ model = "opus", reasoning = "xhigh" }]
standard = [{ model = "opus", reasoning = "high" }]
light = [{ model = "sonnet", reasoning = "high" }]
`)
		configPath := writeFile("config.toml", `
includes = ["private.toml"]
`)

		cfg, err := Load(configPath)
		if err != nil {
			t.Fatalf("Load() error: %v", err)
		}
		got := cfg.Effort["claude"].Heavy
		want := []EffortModel{{Model: "opus", Reasoning: "xhigh"}}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("effort.claude heavy = %#v, want %#v", got, want)
		}
	})

	t.Run("parent effort ladder wins over include collision", func(t *testing.T) {
		tmpDir := t.TempDir()
		writeFile := func(name, content string) string {
			p := filepath.Join(tmpDir, name)
			if err := os.WriteFile(p, []byte(content), 0644); err != nil {
				t.Fatal(err)
			}
			return p
		}

		writeFile("private.toml", `
[effort.claude]
heavy = [{ model = "sonnet", reasoning = "low" }]
`)
		configPath := writeFile("config.toml", `
includes = ["private.toml"]

[effort.claude]
heavy = [{ model = "opus", reasoning = "high" }]
`)

		cfg, err := Load(configPath)
		if err != nil {
			t.Fatalf("Load() error: %v", err)
		}
		got := cfg.Effort["claude"].Heavy
		want := []EffortModel{{Model: "opus", Reasoning: "high"}}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("effort.claude heavy = %#v, want %#v", got, want)
		}
		found := false
		for _, w := range cfg.Warnings {
			if strings.Contains(w, "[effort.claude] skipped") {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("expected effort collision warning, got: %v", cfg.Warnings)
		}
	})

	t.Run("unknown key in included repo block produces warning", func(t *testing.T) {
		tmpDir := t.TempDir()
		writeFile := func(name, content string) string {
			p := filepath.Join(tmpDir, name)
			if err := os.WriteFile(p, []byte(content), 0644); err != nil {
				t.Fatal(err)
			}
			return p
		}

		writeFile("private.toml", `
[repo."/some/repo"]
trunk = true
unknown_field = "oops"
`)
		configPath := writeFile("config.toml", `
includes = ["private.toml"]
`)

		cfg, err := Load(configPath)
		if err != nil {
			t.Fatalf("Load() error: %v", err)
		}
		if len(cfg.Warnings) != 1 {
			t.Fatalf("expected 1 warning for unknown key, got %d: %v", len(cfg.Warnings), cfg.Warnings)
		}
		if !strings.Contains(cfg.Warnings[0], "unknown_field") {
			t.Errorf("warning should name the unknown key, got: %q", cfg.Warnings[0])
		}
	})

	t.Run("projects merge unaffected by repo merge", func(t *testing.T) {
		tmpDir := t.TempDir()
		writeFile := func(name, content string) string {
			p := filepath.Join(tmpDir, name)
			if err := os.WriteFile(p, []byte(content), 0644); err != nil {
				t.Fatal(err)
			}
			return p
		}

		writeFile("extra.toml", `
projects = [{ path = "/extra" }]

[repo."/extra/repo"]
trunk = true
`)
		configPath := writeFile("config.toml", `
includes = ["extra.toml"]
projects = [{ path = "/main" }]
`)

		cfg, err := Load(configPath)
		if err != nil {
			t.Fatalf("Load() error: %v", err)
		}
		if len(cfg.Projects) != 2 {
			t.Fatalf("got %d projects, want 2", len(cfg.Projects))
		}
		if cfg.Projects[0].Path != "/main" || cfg.Projects[1].Path != "/extra" {
			t.Errorf("unexpected project order: %v", cfg.Projects)
		}
		if _, ok := cfg.Repo["/extra/repo"]; !ok {
			t.Error("repo block from include should be present")
		}
	})

	t.Run("include-only workbench options block is merged without warning", func(t *testing.T) {
		tmpDir := t.TempDir()
		writeFile := func(name, content string) string {
			p := filepath.Join(tmpDir, name)
			if err := os.WriteFile(p, []byte(content), 0644); err != nil {
				t.Fatal(err)
			}
			return p
		}

		writeFile("private.toml", `
[workbench]
pick_on_create = true
order = ["minimal", "<empty>"]
`)
		configPath := writeFile("config.toml", `
includes = ["private.toml"]
projects = [{ path = "/main" }]
`)

		cfg, err := Load(configPath)
		if err != nil {
			t.Fatalf("Load() error: %v", err)
		}
		if !cfg.WorkbenchPickOnCreate() {
			t.Error("pick_on_create from include should be enabled")
		}
		if got := cfg.WorkbenchOrder(); len(got) != 2 || got[0] != "minimal" || got[1] != "<empty>" {
			t.Errorf("WorkbenchOrder() = %v, want [minimal <empty>]", got)
		}
		if len(cfg.Warnings) != 0 {
			t.Errorf("expected no warnings, got: %v", cfg.Warnings)
		}
	})

	t.Run("main config workbench options win over include per field", func(t *testing.T) {
		tmpDir := t.TempDir()
		writeFile := func(name, content string) string {
			p := filepath.Join(tmpDir, name)
			if err := os.WriteFile(p, []byte(content), 0644); err != nil {
				t.Fatal(err)
			}
			return p
		}

		writeFile("private.toml", `
[workbench]
pick_on_create = true
order = ["from-include"]
`)
		configPath := writeFile("config.toml", `
includes = ["private.toml"]

[workbench]
pick_on_create = false
`)

		cfg, err := Load(configPath)
		if err != nil {
			t.Fatalf("Load() error: %v", err)
		}
		// Main defined pick_on_create=false → wins; include's true is skipped.
		if cfg.WorkbenchPickOnCreate() {
			t.Error("main's pick_on_create=false should win over include's true")
		}
		// Main left order unset → include fills it (field-level merge).
		if got := cfg.WorkbenchOrder(); len(got) != 1 || got[0] != "from-include" {
			t.Errorf("WorkbenchOrder() = %v, want [from-include] from include", got)
		}
		if len(cfg.Warnings) != 1 {
			t.Fatalf("expected 1 skip warning, got %d: %v", len(cfg.Warnings), cfg.Warnings)
		}
		if !strings.Contains(cfg.Warnings[0], "pick_on_create") {
			t.Errorf("warning should name pick_on_create, got: %q", cfg.Warnings[0])
		}
	})

	t.Run("earlier include workbench options win over later include", func(t *testing.T) {
		tmpDir := t.TempDir()
		writeFile := func(name, content string) string {
			p := filepath.Join(tmpDir, name)
			if err := os.WriteFile(p, []byte(content), 0644); err != nil {
				t.Fatal(err)
			}
			return p
		}

		writeFile("first.toml", `
[workbench]
order = ["first"]
`)
		writeFile("second.toml", `
[workbench]
order = ["second"]
`)
		configPath := writeFile("config.toml", `
includes = ["first.toml", "second.toml"]
`)

		cfg, err := Load(configPath)
		if err != nil {
			t.Fatalf("Load() error: %v", err)
		}
		if got := cfg.WorkbenchOrder(); len(got) != 1 || got[0] != "first" {
			t.Errorf("WorkbenchOrder() = %v, want [first] (first definition wins)", got)
		}
		if len(cfg.Warnings) != 1 {
			t.Fatalf("expected 1 skip warning, got %d: %v", len(cfg.Warnings), cfg.Warnings)
		}
		if !strings.Contains(cfg.Warnings[0], "second.toml") || !strings.Contains(cfg.Warnings[0], "order") {
			t.Errorf("warning should name second.toml and order, got: %q", cfg.Warnings[0])
		}
	})
}

func TestCommandsForMode(t *testing.T) {
	t.Run("global only returned for both modes", func(t *testing.T) {
		cfg := &Config{
			Commands: []UserDefinedCommand{
				{Key: "ctrl+o", Label: "global", Command: "echo global", Exit: true},
			},
		}

		for _, mode := range []string{"project", "select", "worktree"} {
			cmds := cfg.CommandsForMode(mode)
			if len(cmds) != 1 {
				t.Errorf("mode %q: got %d commands, want 1", mode, len(cmds))
				continue
			}
			if cmds[0].Label != "global" {
				t.Errorf("mode %q: label = %q, want %q", mode, cmds[0].Label, "global")
			}
		}
	})

	t.Run("section overrides global by key", func(t *testing.T) {
		cfg := &Config{
			Commands: []UserDefinedCommand{
				{Key: "ctrl+o", Label: "global", Command: "echo global"},
			},
			Worktree: &WorktreeConfig{
				Commands: []UserDefinedCommand{
					{Key: "ctrl+o", Label: "worktree", Command: "echo worktree"},
				},
			},
		}

		wt := cfg.CommandsForMode("worktree")
		if len(wt) != 1 {
			t.Fatalf("worktree: got %d commands, want 1", len(wt))
		}
		if wt[0].Label != "worktree" {
			t.Errorf("worktree: label = %q, want %q", wt[0].Label, "worktree")
		}

		// Project mode should still get global
		proj := cfg.CommandsForMode("project")
		if len(proj) != 1 {
			t.Fatalf("project: got %d commands, want 1", len(proj))
		}
		if proj[0].Label != "global" {
			t.Errorf("project: label = %q, want %q", proj[0].Label, "global")
		}
		// Deprecated alias still works
		sel := cfg.CommandsForMode("select")
		if len(sel) != 1 || sel[0].Label != "global" {
			t.Errorf("select alias: got %#v, want global command", sel)
		}
	})

	t.Run("section-only commands included", func(t *testing.T) {
		cfg := &Config{
			Worktree: &WorktreeConfig{
				Commands: []UserDefinedCommand{
					{Key: "ctrl+l", Label: "wt-only", Command: "echo wt"},
				},
			},
		}

		cmds := cfg.CommandsForMode("worktree")
		if len(cmds) != 1 {
			t.Fatalf("got %d commands, want 1", len(cmds))
		}
		if cmds[0].Label != "wt-only" {
			t.Errorf("label = %q, want %q", cmds[0].Label, "wt-only")
		}

		// Project mode should get nothing
		proj := cfg.CommandsForMode("project")
		if len(proj) != 0 {
			t.Errorf("project: got %d commands, want 0", len(proj))
		}
	})

	t.Run("no commands returns empty", func(t *testing.T) {
		cfg := &Config{}
		cmds := cfg.CommandsForMode("worktree")
		if len(cmds) != 0 {
			t.Errorf("got %d commands, want 0", len(cmds))
		}
	})

	t.Run("mixed override preserves order", func(t *testing.T) {
		cfg := &Config{
			Commands: []UserDefinedCommand{
				{Key: "ctrl+o", Label: "global-o", Command: "echo o"},
				{Key: "ctrl+k", Label: "global-k", Command: "echo k"},
			},
			Worktree: &WorktreeConfig{
				Commands: []UserDefinedCommand{
					{Key: "ctrl+o", Label: "wt-o", Command: "echo wt-o"},
					{Key: "ctrl+l", Label: "wt-l", Command: "echo wt-l"},
				},
			},
		}

		cmds := cfg.CommandsForMode("worktree")
		if len(cmds) != 3 {
			t.Fatalf("got %d commands, want 3", len(cmds))
		}
		// Order: global keys first (ctrl+o overridden, ctrl+k kept), then section-only (ctrl+l)
		if cmds[0].Label != "wt-o" {
			t.Errorf("cmds[0].Label = %q, want %q", cmds[0].Label, "wt-o")
		}
		if cmds[1].Label != "global-k" {
			t.Errorf("cmds[1].Label = %q, want %q", cmds[1].Label, "global-k")
		}
		if cmds[2].Label != "wt-l" {
			t.Errorf("cmds[2].Label = %q, want %q", cmds[2].Label, "wt-l")
		}
	})

	t.Run("project section works", func(t *testing.T) {
		cfg := &Config{
			Commands: []UserDefinedCommand{
				{Key: "ctrl+o", Label: "global", Command: "echo global"},
			},
			Project: &ProjectConfig{
				Commands: []UserDefinedCommand{
					{Key: "ctrl+o", Label: "project", Command: "echo project"},
				},
			},
		}

		cmds := cfg.CommandsForMode("project")
		if len(cmds) != 1 {
			t.Fatalf("got %d commands, want 1", len(cmds))
		}
		if cmds[0].Label != "project" {
			t.Errorf("label = %q, want %q", cmds[0].Label, "project")
		}
	})

	t.Run("deprecated select section works", func(t *testing.T) {
		cfg := &Config{
			Commands: []UserDefinedCommand{
				{Key: "ctrl+o", Label: "global", Command: "echo global"},
			},
			Select: &ProjectConfig{
				Commands: []UserDefinedCommand{
					{Key: "ctrl+o", Label: "legacy", Command: "echo legacy"},
				},
			},
		}

		cmds := cfg.CommandsForMode("select")
		if len(cmds) != 1 {
			t.Fatalf("got %d commands, want 1", len(cmds))
		}
		if cmds[0].Label != "legacy" {
			t.Errorf("label = %q, want %q", cmds[0].Label, "legacy")
		}
	})
}

func TestShouldExcludeCurrentSession(t *testing.T) {
	tests := []struct {
		name     string
		toml     string
		expected bool
	}{
		{
			name:     "new field set",
			toml:     "exclude_current_session = true\nprojects = []",
			expected: true,
		},
		{
			name:     "deprecated field set",
			toml:     "exclude_current_dir = true\nprojects = []",
			expected: true,
		},
		{
			name:     "neither set",
			toml:     "projects = []",
			expected: false,
		},
		{
			name:     "both set",
			toml:     "exclude_current_session = true\nexclude_current_dir = true\nprojects = []",
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmpDir := t.TempDir()
			configPath := filepath.Join(tmpDir, "config.toml")
			if err := os.WriteFile(configPath, []byte(tt.toml), 0644); err != nil {
				t.Fatalf("failed to write config: %v", err)
			}
			cfg, err := Load(configPath)
			if err != nil {
				t.Fatalf("Load() error: %v", err)
			}
			if got := cfg.ShouldExcludeCurrentSession(); got != tt.expected {
				t.Errorf("ShouldExcludeCurrentSession() = %v, want %v", got, tt.expected)
			}
		})
	}
}

func TestExpandProjectsSubsumption(t *testing.T) {
	// Integration test: broad glob + specific glob with different display_depth
	tmpDir := t.TempDir()

	// Create: tmpDir/work/proj_a, tmpDir/personal/proj_c,
	//         tmpDir/personal/proj_d/v1, tmpDir/personal/proj_d/v2
	os.MkdirAll(filepath.Join(tmpDir, "work", "proj_a"), 0755)
	os.MkdirAll(filepath.Join(tmpDir, "personal", "proj_c"), 0755)
	os.MkdirAll(filepath.Join(tmpDir, "personal", "proj_d", "v1"), 0755)
	os.MkdirAll(filepath.Join(tmpDir, "personal", "proj_d", "v2"), 0755)

	cfg := &Config{Projects: []ProjectEntry{
		{Path: filepath.Join(tmpDir, "*", "*")},
		{Path: filepath.Join(tmpDir, "personal", "proj_d", "*"), DisplayDepth: 2},
	}}

	result, err := cfg.ExpandProjects()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Should have: proj_a, proj_c, proj_d/v1, proj_d/v2 (NOT proj_d)
	if len(result) != 4 {
		t.Fatalf("got %d projects, want 4: %v", len(result), result)
	}

	// proj_d should NOT be in the results
	for _, ep := range result {
		if filepath.Base(ep.Path) == "proj_d" {
			t.Errorf("proj_d should be subsumed but is present: %v", ep)
		}
	}

	// Children should have display_depth = 2
	for _, ep := range result {
		dir := filepath.Base(filepath.Dir(ep.Path))
		if dir == "proj_d" {
			if ep.DisplayDepth != 2 {
				t.Errorf("child %q: DisplayDepth = %d, want 2", ep.Path, ep.DisplayDepth)
			}
		}
	}
}

// TestLoadDeprecatedUnreadRenameKeys verifies that the old attention-era
// config keys are still honored (so users don't lose behavior on upgrade)
// and that a deprecation warning is emitted for each one encountered.
func TestLoadDeprecatedUnreadRenameKeys(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.toml")
	os.WriteFile(configPath, []byte(`
[pane_monitoring]
dismiss_attention_in_active_pane = true

[select]
attention_notifications_enabled = true

[worktree]
attention_notifications_enabled = true
`), 0644)

	cfg, err := Load(configPath)
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}

	// Accessors should honor the legacy keys.
	if !cfg.DismissUnreadInActivePane() {
		t.Error("DismissUnreadInActivePane() = false, want true (legacy key should be honored)")
	}
	if !cfg.UnreadNotificationsEnabled("project") {
		t.Error("UnreadNotificationsEnabled(project) = false, want true (legacy key should be honored)")
	}
	if !cfg.UnreadNotificationsEnabled("select") {
		t.Error("UnreadNotificationsEnabled(select) = false, want true (deprecated mode alias should be honored)")
	}
	if !cfg.UnreadNotificationsEnabled("worktree") {
		t.Error("UnreadNotificationsEnabled(worktree) = false, want true (legacy key should be honored)")
	}

	// One deprecation warning per legacy key present, plus [select] section rename.
	if len(cfg.Warnings) != 4 {
		t.Fatalf("expected 4 deprecation warnings, got %d: %v", len(cfg.Warnings), cfg.Warnings)
	}
}

// TestLoadDeprecatedSelectSection verifies that [select] is still honored and
// emits a deprecation warning.
func TestLoadDeprecatedSelectSection(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.toml")
	os.WriteFile(configPath, []byte(`
projects = []

[select]
unread_notifications_enabled = true
`), 0644)

	cfg, err := Load(configPath)
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}

	if !cfg.UnreadNotificationsEnabled("project") {
		t.Error("UnreadNotificationsEnabled(project) = false, want true")
	}
	if cfg.Project == nil {
		t.Fatal("expected [select] to populate Project config")
	}

	found := false
	for _, w := range cfg.Warnings {
		if strings.Contains(w, "[select] is deprecated") {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected [select] deprecation warning, got: %v", cfg.Warnings)
	}
}

// TestLoadNewUnreadKeys verifies that the new canonical keys work and emit
// no warnings.
func TestLoadNewUnreadKeys(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.toml")
	os.WriteFile(configPath, []byte(`
[pane_monitoring]
dismiss_unread_in_active_pane = true

[project]
unread_notifications_enabled = true

[worktree]
unread_notifications_enabled = true
`), 0644)

	cfg, err := Load(configPath)
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}

	if !cfg.DismissUnreadInActivePane() {
		t.Error("DismissUnreadInActivePane() = false, want true")
	}
	if !cfg.UnreadNotificationsEnabled("project") {
		t.Error("UnreadNotificationsEnabled(project) = false, want true")
	}
	if !cfg.UnreadNotificationsEnabled("worktree") {
		t.Error("UnreadNotificationsEnabled(worktree) = false, want true")
	}

	if len(cfg.Warnings) != 0 {
		t.Errorf("expected no warnings, got %d: %v", len(cfg.Warnings), cfg.Warnings)
	}
}

func TestDashboardCursorPosition(t *testing.T) {
	tests := []struct {
		name string
		cfg  Config
		want string
	}{
		{
			name: "default",
			cfg:  Config{},
			want: DashboardCursorCurrentRegistered,
		},
		{
			name: "explicit current registered",
			cfg:  Config{Dashboard: &DashboardConfig{CursorPosition: DashboardCursorCurrentRegistered}},
			want: DashboardCursorCurrentRegistered,
		},
		{
			name: "explicit current any",
			cfg:  Config{Dashboard: &DashboardConfig{CursorPosition: DashboardCursorCurrentAny}},
			want: DashboardCursorCurrentAny,
		},
		{
			name: "explicit first active",
			cfg:  Config{Dashboard: &DashboardConfig{CursorPosition: DashboardCursorFirstActive}},
			want: DashboardCursorFirstActive,
		},
		{
			name: "invalid falls back to default",
			cfg:  Config{Dashboard: &DashboardConfig{CursorPosition: "later_maybe"}},
			want: DashboardCursorCurrentRegistered,
		},
		{
			name: "legacy boolean maps to current any",
			cfg:  Config{Dashboard: &DashboardConfig{CurrentPaneAlwaysUnderCursor: true}},
			want: DashboardCursorCurrentAny,
		},
		{
			name: "explicit cursor position takes precedence over legacy boolean",
			cfg: Config{Dashboard: &DashboardConfig{
				CurrentPaneAlwaysUnderCursor: true,
				CursorPosition:               DashboardCursorFirstActive,
			}},
			want: DashboardCursorFirstActive,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.cfg.DashboardCursorPosition(); got != tt.want {
				t.Errorf("DashboardCursorPosition() = %q, want %q", got, tt.want)
			}
		})
	}
}

// boolPtr returns a pointer to b for use in RepoOverrideConfig fields.
func boolPtr(b bool) *bool { return &b }

// makeFSWithBare returns a MockFileSystem whose Stat recognises
// bareDir+"/.bare" as a directory and everything else as not existing.
func makeFSWithBare(bareDir string) *deps.MockFileSystem {
	return &deps.MockFileSystem{
		StatFunc: func(path string) (os.FileInfo, error) {
			if path == filepath.Join(bareDir, ".bare") {
				return &deps.MockFileInfo{IsDirVal: true}, nil
			}
			return nil, os.ErrNotExist
		},
		ReadFileFunc: func(path string) ([]byte, error) {
			return nil, os.ErrNotExist
		},
		EvalSymlinksFunc: func(path string) (string, error) {
			return path, nil
		},
		UserHomeDirFunc: func() (string, error) {
			return "/home/user", nil
		},
	}
}

func TestResolveRepoConfigPrecedence(t *testing.T) {
	root := t.TempDir()

	real := deps.NewRealFileSystem()
	d := &Deps{FS: &deps.MockFileSystem{
		StatFunc:         real.Stat,
		ReadFileFunc:     real.ReadFile,
		EvalSymlinksFunc: real.EvalSymlinks,
		UserHomeDirFunc:  real.UserHomeDir,
	}}

	t.Run("global override sets trunk", func(t *testing.T) {
		cfg := &Config{
			Repo: map[string]RepoOverrideConfig{
				root: {Trunk: boolPtr(true)},
			},
		}
		got, err := cfg.ResolveRepoConfig(d, root)
		if err != nil {
			t.Fatal(err)
		}
		if !got.Trunk {
			t.Errorf("Trunk = false, want true (override wins)")
		}
	})

	t.Run("no override yields defaults", func(t *testing.T) {
		cfg := &Config{}
		got, err := cfg.ResolveRepoConfig(d, root)
		if err != nil {
			t.Fatal(err)
		}
		if got.Trunk {
			t.Errorf("Trunk = true, want false (no override)")
		}
	})

	t.Run("no override and no pop.toml yields defaults", func(t *testing.T) {
		cfg := &Config{}
		fakeDir := filepath.Join(t.TempDir(), "nopoptom")
		if err := os.MkdirAll(fakeDir, 0o755); err != nil {
			t.Fatal(err)
		}
		got, err := cfg.ResolveRepoConfig(d, fakeDir)
		if err != nil {
			t.Fatal(err)
		}
		if got.Trunk {
			t.Errorf("expected zero defaults, got %+v", got)
		}
	})
}

// TestLoadPreferredWorkbenchParsesOnRepoBlock asserts preferred_workbench parses
// on a global [repo."<path>"] block into RepoOverrideConfig and is not flagged as
// an unknown repo key (ADR-0078).
func TestLoadPreferredWorkbenchParsesOnRepoBlock(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.toml")
	body := "projects = [{ path = \"/main\" }]\n\n" +
		"[repo.\"/some/repo\"]\npreferred_workbench = \"gs-dev\"\n"
	if err := os.WriteFile(configPath, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	d := &Deps{FS: &deps.MockFileSystem{
		UserHomeDirFunc: func() (string, error) { return tmpDir, nil },
	}}

	cfg, err := LoadWith(d, configPath)
	if err != nil {
		t.Fatalf("LoadWith() error: %v", err)
	}
	block, ok := cfg.Repo["/some/repo"]
	if !ok {
		t.Fatal("expected [repo.\"/some/repo\"] block to load")
	}
	if block.PreferredWorkbench != "gs-dev" {
		t.Errorf("PreferredWorkbench = %q, want %q", block.PreferredWorkbench, "gs-dev")
	}
	for _, f := range cfg.Findings {
		if f.Path == "config.unknown_repo_key" {
			t.Errorf("preferred_workbench flagged as unknown repo key: %s", f.Message)
		}
	}
}

func TestResolvePreferredWorkbench(t *testing.T) {
	root := t.TempDir()
	real := deps.NewRealFileSystem()
	d := &Deps{FS: &deps.MockFileSystem{
		StatFunc:         real.Stat,
		ReadFileFunc:     real.ReadFile,
		EvalSymlinksFunc: real.EvalSymlinks,
		UserHomeDirFunc:  real.UserHomeDir,
	}}

	t.Run("repo default resolves to a real workbench", func(t *testing.T) {
		cfg := &Config{
			Workbenches: []Workbench{{Name: "gs-dev"}, {Name: "minimal"}},
			Repo: map[string]RepoOverrideConfig{
				root: {RepoScopeConfig: RepoScopeConfig{PreferredWorkbench: "gs-dev"}},
			},
		}
		name, warns := cfg.ResolvePreferredWorkbench(d, root)
		if name != "gs-dev" {
			t.Errorf("name = %q, want %q", name, "gs-dev")
		}
		if len(warns) != 0 {
			t.Errorf("unexpected warnings: %v", warns)
		}
	})

	t.Run("unset yields none without warning", func(t *testing.T) {
		cfg := &Config{
			Workbenches: []Workbench{{Name: "gs-dev"}},
			Repo: map[string]RepoOverrideConfig{
				root: {Trunk: boolPtr(true)},
			},
		}
		name, warns := cfg.ResolvePreferredWorkbench(d, root)
		if name != "" {
			t.Errorf("name = %q, want empty", name)
		}
		if len(warns) != 0 {
			t.Errorf("unexpected warnings: %v", warns)
		}
	})

	t.Run("no repo block yields none", func(t *testing.T) {
		cfg := &Config{Workbenches: []Workbench{{Name: "gs-dev"}}}
		name, warns := cfg.ResolvePreferredWorkbench(d, root)
		if name != "" || len(warns) != 0 {
			t.Errorf("name = %q warns = %v, want empty/none", name, warns)
		}
	})

	t.Run("stale name skips with a warning and falls through to none", func(t *testing.T) {
		cfg := &Config{
			Workbenches: []Workbench{{Name: "gs-dev"}},
			Repo: map[string]RepoOverrideConfig{
				root: {RepoScopeConfig: RepoScopeConfig{PreferredWorkbench: "ghost"}},
			},
		}
		name, warns := cfg.ResolvePreferredWorkbench(d, root)
		if name != "" {
			t.Errorf("name = %q, want empty (stale skips)", name)
		}
		if len(warns) != 1 {
			t.Fatalf("warnings = %v, want exactly one", warns)
		}
		if !strings.Contains(warns[0], "ghost") {
			t.Errorf("warning %q should name the stale workbench", warns[0])
		}
	})

	t.Run("nil receiver is safe", func(t *testing.T) {
		var cfg *Config
		if name, warns := cfg.ResolvePreferredWorkbench(d, root); name != "" || warns != nil {
			t.Errorf("nil receiver: got %q/%v, want empty/nil", name, warns)
		}
	})
}

// preferredResolverDeps builds a *Deps whose runtime file lands under a temp
// XDG_DATA_HOME while repo-identity / .pop.toml reads hit the real filesystem.
func preferredResolverDeps(t *testing.T) *Deps {
	t.Helper()
	dataDir := filepath.Join(t.TempDir(), "data")
	real := deps.NewRealFileSystem()
	return &Deps{FS: &deps.MockFileSystem{
		GetenvFunc: func(key string) string {
			if key == "XDG_DATA_HOME" {
				return dataDir
			}
			return ""
		},
		StatFunc:         real.Stat,
		ReadFileFunc:     real.ReadFile,
		WriteFileFunc:    os.WriteFile,
		MkdirAllFunc:     os.MkdirAll,
		RenameFunc:       os.Rename,
		RemoveAllFunc:    os.RemoveAll,
		EvalSymlinksFunc: real.EvalSymlinks,
		UserHomeDirFunc:  real.UserHomeDir,
	}}
}

// TestResolvePreferredWorkbenchPrecedence exercises the runtime tier under the
// user-first law (ADR-0083): the worktree runtime entry (layer 5) is now a
// gap-filler that sits BELOW the hand-authored [repo] default (layer 1), the
// reverse of the shipped scope-first ordering. It applies only where nothing
// hand-authored sets the key; three-valued semantics still hold within the tier.
func TestResolvePreferredWorkbenchPrecedence(t *testing.T) {
	t.Run("repo default beats the worktree runtime entry (slice-01 reversal)", func(t *testing.T) {
		d := preferredResolverDeps(t)
		root := t.TempDir()
		if err := SetRuntimePreferredWorkbenchWith(d, root, "minimal"); err != nil {
			t.Fatal(err)
		}
		cfg := &Config{
			Workbenches: []Workbench{{Name: "gs-dev"}, {Name: "minimal"}},
			Repo:        map[string]RepoOverrideConfig{root: {RepoScopeConfig: RepoScopeConfig{PreferredWorkbench: "gs-dev"}}},
		}
		name, warns := cfg.ResolvePreferredWorkbench(d, root)
		if name != "gs-dev" || len(warns) != 0 {
			t.Fatalf("name=%q warns=%v, want gs-dev/none (hand-authored [repo] beats runtime)", name, warns)
		}
	})

	t.Run("runtime entry applies where nothing hand-authored sets the key", func(t *testing.T) {
		d := preferredResolverDeps(t)
		root := t.TempDir()
		if err := SetRuntimePreferredWorkbenchWith(d, root, "minimal"); err != nil {
			t.Fatal(err)
		}
		cfg := &Config{Workbenches: []Workbench{{Name: "gs-dev"}, {Name: "minimal"}}}
		name, warns := cfg.ResolvePreferredWorkbench(d, root)
		if name != "minimal" || len(warns) != 0 {
			t.Fatalf("name=%q warns=%v, want minimal/none (runtime gap-fills)", name, warns)
		}
	})

	t.Run("explicit none short-circuits within the runtime tier", func(t *testing.T) {
		d := preferredResolverDeps(t)
		root := t.TempDir()
		if err := SetRuntimePreferredWorkbenchWith(d, root, ""); err != nil {
			t.Fatal(err)
		}
		cfg := &Config{Workbenches: []Workbench{{Name: "gs-dev"}}}
		name, warns := cfg.ResolvePreferredWorkbench(d, root)
		if name != "" || len(warns) != 0 {
			t.Fatalf("name=%q warns=%v, want empty/none (explicit-none short-circuits)", name, warns)
		}
	})

	t.Run("explicit none does NOT override a hand-authored repo default", func(t *testing.T) {
		d := preferredResolverDeps(t)
		root := t.TempDir()
		if err := SetRuntimePreferredWorkbenchWith(d, root, ""); err != nil {
			t.Fatal(err)
		}
		cfg := &Config{
			Workbenches: []Workbench{{Name: "gs-dev"}},
			Repo:        map[string]RepoOverrideConfig{root: {RepoScopeConfig: RepoScopeConfig{PreferredWorkbench: "gs-dev"}}},
		}
		name, warns := cfg.ResolvePreferredWorkbench(d, root)
		if name != "gs-dev" || len(warns) != 0 {
			t.Fatalf("name=%q warns=%v, want gs-dev/none (runtime explicit-none cannot beat hand-authored)", name, warns)
		}
	})

	t.Run("absent runtime entry falls through to the repo default", func(t *testing.T) {
		d := preferredResolverDeps(t)
		root := t.TempDir()
		cfg := &Config{
			Workbenches: []Workbench{{Name: "gs-dev"}},
			Repo:        map[string]RepoOverrideConfig{root: {RepoScopeConfig: RepoScopeConfig{PreferredWorkbench: "gs-dev"}}},
		}
		name, warns := cfg.ResolvePreferredWorkbench(d, root)
		if name != "gs-dev" || len(warns) != 0 {
			t.Fatalf("name=%q warns=%v, want gs-dev/none", name, warns)
		}
	})

	t.Run("stale runtime name warns and falls through to none", func(t *testing.T) {
		d := preferredResolverDeps(t)
		root := t.TempDir()
		if err := SetRuntimePreferredWorkbenchWith(d, root, "ghost"); err != nil {
			t.Fatal(err)
		}
		cfg := &Config{Workbenches: []Workbench{{Name: "gs-dev"}}}
		name, warns := cfg.ResolvePreferredWorkbench(d, root)
		if name != "" {
			t.Fatalf("name=%q, want empty (stale runtime skips, nothing below)", name)
		}
		if len(warns) != 1 || !strings.Contains(warns[0], "ghost") {
			t.Fatalf("warns=%v, want one naming the stale runtime name", warns)
		}
	})

	t.Run("stale at both layers warns twice and yields none", func(t *testing.T) {
		d := preferredResolverDeps(t)
		root := t.TempDir()
		if err := SetRuntimePreferredWorkbenchWith(d, root, "ghost"); err != nil {
			t.Fatal(err)
		}
		cfg := &Config{
			Workbenches: []Workbench{{Name: "gs-dev"}},
			Repo:        map[string]RepoOverrideConfig{root: {RepoScopeConfig: RepoScopeConfig{PreferredWorkbench: "phantom"}}},
		}
		name, warns := cfg.ResolvePreferredWorkbench(d, root)
		if name != "" {
			t.Fatalf("name=%q, want empty (both layers stale)", name)
		}
		if len(warns) != 2 {
			t.Fatalf("warns=%v, want two (one per stale layer)", warns)
		}
	})
}

// TestResolvePreferredWorkbenchTrunkInheritance exercises the runtime trunk
// inheritance layer (layer 6 under ADR-0083: config.runtime.toml[<trunk-path>]),
// the lowest layer that carries this key. A worktree with no runtime entry of
// its own inherits the Trunk worktree's runtime entry, resolved dynamically at
// open. These cases isolate the runtime tier (no hand-authored [repo]/.pop.toml
// value above), since anything hand-authored now beats runtime. The Trunk
// resolver is injected via Deps.Trunk (config cannot import tasks/binding).
func TestResolvePreferredWorkbenchTrunkInheritance(t *testing.T) {
	// withTrunk returns Deps whose Trunk resolver always points at trunkPath.
	withTrunk := func(t *testing.T, trunkPath string) *Deps {
		d := preferredResolverDeps(t)
		d.Trunk = func(string) (string, bool) { return trunkPath, true }
		return d
	}

	t.Run("child with no entry inherits the trunk's runtime entry", func(t *testing.T) {
		d := preferredResolverDeps(t)
		trunk := t.TempDir()
		child := t.TempDir()
		d.Trunk = func(string) (string, bool) { return trunk, true }
		if err := SetRuntimePreferredWorkbenchWith(d, trunk, "minimal"); err != nil {
			t.Fatal(err)
		}
		cfg := &Config{Workbenches: []Workbench{{Name: "gs-dev"}, {Name: "minimal"}}}
		name, warns := cfg.ResolvePreferredWorkbench(d, child)
		if name != "minimal" || len(warns) != 0 {
			t.Fatalf("name=%q warns=%v, want minimal/none (child inherits trunk)", name, warns)
		}
	})

	t.Run("child's own name overrides the trunk", func(t *testing.T) {
		d := preferredResolverDeps(t)
		trunk := t.TempDir()
		child := t.TempDir()
		d.Trunk = func(string) (string, bool) { return trunk, true }
		if err := SetRuntimePreferredWorkbenchWith(d, trunk, "minimal"); err != nil {
			t.Fatal(err)
		}
		if err := SetRuntimePreferredWorkbenchWith(d, child, "gs-dev"); err != nil {
			t.Fatal(err)
		}
		cfg := &Config{Workbenches: []Workbench{{Name: "gs-dev"}, {Name: "minimal"}}}
		name, warns := cfg.ResolvePreferredWorkbench(d, child)
		if name != "gs-dev" || len(warns) != 0 {
			t.Fatalf("name=%q warns=%v, want gs-dev/none (own entry wins)", name, warns)
		}
	})

	t.Run("child's explicit none overrides the inherited trunk entry", func(t *testing.T) {
		d := preferredResolverDeps(t)
		trunk := t.TempDir()
		child := t.TempDir()
		d.Trunk = func(string) (string, bool) { return trunk, true }
		if err := SetRuntimePreferredWorkbenchWith(d, trunk, "minimal"); err != nil {
			t.Fatal(err)
		}
		if err := SetRuntimePreferredWorkbenchWith(d, child, ""); err != nil {
			t.Fatal(err)
		}
		cfg := &Config{Workbenches: []Workbench{{Name: "gs-dev"}, {Name: "minimal"}}}
		name, warns := cfg.ResolvePreferredWorkbench(d, child)
		if name != "" || len(warns) != 0 {
			t.Fatalf("name=%q warns=%v, want empty/none (child explicit-none wins over trunk)", name, warns)
		}
	})

	t.Run("trunk's explicit none yields none when nothing hand-authored", func(t *testing.T) {
		d := preferredResolverDeps(t)
		trunk := t.TempDir()
		child := t.TempDir()
		d.Trunk = func(string) (string, bool) { return trunk, true }
		if err := SetRuntimePreferredWorkbenchWith(d, trunk, ""); err != nil {
			t.Fatal(err)
		}
		cfg := &Config{Workbenches: []Workbench{{Name: "gs-dev"}}}
		name, warns := cfg.ResolvePreferredWorkbench(d, child)
		if name != "" || len(warns) != 0 {
			t.Fatalf("name=%q warns=%v, want empty/none (trunk opts out)", name, warns)
		}
	})

	t.Run("stale trunk name warns and falls through to none", func(t *testing.T) {
		d := preferredResolverDeps(t)
		trunk := t.TempDir()
		child := t.TempDir()
		d.Trunk = func(string) (string, bool) { return trunk, true }
		if err := SetRuntimePreferredWorkbenchWith(d, trunk, "ghost"); err != nil {
			t.Fatal(err)
		}
		cfg := &Config{Workbenches: []Workbench{{Name: "gs-dev"}}}
		name, warns := cfg.ResolvePreferredWorkbench(d, child)
		if name != "" {
			t.Fatalf("name=%q, want empty (stale trunk skips, nothing below)", name)
		}
		if len(warns) != 1 || !strings.Contains(warns[0], "ghost") {
			t.Fatalf("warns=%v, want one naming the stale trunk workbench", warns)
		}
	})

	t.Run("bare repo without a trunk anchor skips the step without error", func(t *testing.T) {
		d := preferredResolverDeps(t)
		child := t.TempDir()
		// No trunk anchor: the resolver reports (_, false).
		d.Trunk = func(string) (string, bool) { return "", false }
		cfg := &Config{
			Workbenches: []Workbench{{Name: "gs-dev"}},
			Repo:        map[string]RepoOverrideConfig{child: {RepoScopeConfig: RepoScopeConfig{PreferredWorkbench: "gs-dev"}}},
		}
		name, warns := cfg.ResolvePreferredWorkbench(d, child)
		if name != "gs-dev" || len(warns) != 0 {
			t.Fatalf("name=%q warns=%v, want gs-dev/none (no trunk → repo default)", name, warns)
		}
	})

	t.Run("nil Trunk resolver disables inheritance", func(t *testing.T) {
		d := preferredResolverDeps(t) // Trunk left nil
		child := t.TempDir()
		cfg := &Config{
			Workbenches: []Workbench{{Name: "gs-dev"}},
			Repo:        map[string]RepoOverrideConfig{child: {RepoScopeConfig: RepoScopeConfig{PreferredWorkbench: "gs-dev"}}},
		}
		name, warns := cfg.ResolvePreferredWorkbench(d, child)
		if name != "gs-dev" || len(warns) != 0 {
			t.Fatalf("name=%q warns=%v, want gs-dev/none (nil Trunk → repo default)", name, warns)
		}
	})

	t.Run("re-pointing the trunk changes what an un-overridden child resolves to", func(t *testing.T) {
		d := preferredResolverDeps(t)
		trunk := t.TempDir()
		child := t.TempDir()
		d.Trunk = func(string) (string, bool) { return trunk, true }
		cfg := &Config{Workbenches: []Workbench{{Name: "gs-dev"}, {Name: "minimal"}}}

		if err := SetRuntimePreferredWorkbenchWith(d, trunk, "minimal"); err != nil {
			t.Fatal(err)
		}
		if name, _ := cfg.ResolvePreferredWorkbench(d, child); name != "minimal" {
			t.Fatalf("first open: name=%q, want minimal", name)
		}
		// Re-point the trunk; the child, which never set its own entry, follows.
		if err := SetRuntimePreferredWorkbenchWith(d, trunk, "gs-dev"); err != nil {
			t.Fatal(err)
		}
		if name, _ := cfg.ResolvePreferredWorkbench(d, child); name != "gs-dev" {
			t.Fatalf("after re-point: name=%q, want gs-dev (dynamic, not snapshotted)", name)
		}
	})

	t.Run("checkout that is itself the trunk does not double-warn on a stale name", func(t *testing.T) {
		trunk := t.TempDir()
		d := withTrunk(t, trunk)
		if err := SetRuntimePreferredWorkbenchWith(d, trunk, "ghost"); err != nil {
			t.Fatal(err)
		}
		cfg := &Config{Workbenches: []Workbench{{Name: "gs-dev"}}}
		name, warns := cfg.ResolvePreferredWorkbench(d, trunk)
		if name != "" {
			t.Fatalf("name=%q, want empty (stale name skips)", name)
		}
		if len(warns) != 1 {
			t.Fatalf("warns=%v, want exactly one (trunk runtime layer skipped for self-trunk)", warns)
		}
	})
}

// TestResolvePreferredWorkbenchUserFirstLadder exercises the ADR-0083 user-first
// precedence ladder end to end: each layer that carries preferred_workbench in
// isolation, the slice-01 reversal (hand-authored beats runtime), the
// global-shadow ([repo] beats committed .pop.toml), and the two-anchor .pop.toml
// inheritance (this worktree vs the Trunk worktree, identity-root fallback for a
// bare repo).
func TestResolvePreferredWorkbenchUserFirstLadder(t *testing.T) {
	writePopTOML := func(t *testing.T, dir, name string) {
		t.Helper()
		body := "preferred_workbench = \"" + name + "\"\n"
		if err := os.WriteFile(filepath.Join(dir, ".pop.toml"), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	t.Run("layer 1: [repo] block supplies the value", func(t *testing.T) {
		d := preferredResolverDeps(t)
		root := t.TempDir()
		cfg := &Config{
			Workbenches: []Workbench{{Name: "repo-wb"}},
			Repo:        map[string]RepoOverrideConfig{root: {RepoScopeConfig: RepoScopeConfig{PreferredWorkbench: "repo-wb"}}},
		}
		if name, warns := cfg.ResolvePreferredWorkbench(d, root); name != "repo-wb" || len(warns) != 0 {
			t.Fatalf("name=%q warns=%v, want repo-wb/none", name, warns)
		}
	})

	t.Run("layer 3: this worktree's committed .pop.toml supplies the value", func(t *testing.T) {
		d := preferredResolverDeps(t)
		root := t.TempDir()
		writePopTOML(t, root, "committed-wb")
		cfg := &Config{Workbenches: []Workbench{{Name: "committed-wb"}}}
		if name, warns := cfg.ResolvePreferredWorkbench(d, root); name != "committed-wb" || len(warns) != 0 {
			t.Fatalf("name=%q warns=%v, want committed-wb/none", name, warns)
		}
	})

	t.Run("layer 4: inherited trunk .pop.toml supplies the value", func(t *testing.T) {
		d := preferredResolverDeps(t)
		trunk := t.TempDir()
		child := t.TempDir()
		d.Trunk = func(string) (string, bool) { return trunk, true }
		writePopTOML(t, trunk, "trunk-wb")
		cfg := &Config{Workbenches: []Workbench{{Name: "trunk-wb"}}}
		if name, warns := cfg.ResolvePreferredWorkbench(d, child); name != "trunk-wb" || len(warns) != 0 {
			t.Fatalf("name=%q warns=%v, want trunk-wb/none (child inherits trunk .pop.toml)", name, warns)
		}
	})

	t.Run("layer 5: worktree runtime entry supplies the value", func(t *testing.T) {
		d := preferredResolverDeps(t)
		root := t.TempDir()
		if err := SetRuntimePreferredWorkbenchWith(d, root, "rt-wb"); err != nil {
			t.Fatal(err)
		}
		cfg := &Config{Workbenches: []Workbench{{Name: "rt-wb"}}}
		if name, warns := cfg.ResolvePreferredWorkbench(d, root); name != "rt-wb" || len(warns) != 0 {
			t.Fatalf("name=%q warns=%v, want rt-wb/none", name, warns)
		}
	})

	t.Run("layer 6: inherited trunk runtime entry supplies the value", func(t *testing.T) {
		d := preferredResolverDeps(t)
		trunk := t.TempDir()
		child := t.TempDir()
		d.Trunk = func(string) (string, bool) { return trunk, true }
		if err := SetRuntimePreferredWorkbenchWith(d, trunk, "rt-trunk-wb"); err != nil {
			t.Fatal(err)
		}
		cfg := &Config{Workbenches: []Workbench{{Name: "rt-trunk-wb"}}}
		if name, warns := cfg.ResolvePreferredWorkbench(d, child); name != "rt-trunk-wb" || len(warns) != 0 {
			t.Fatalf("name=%q warns=%v, want rt-trunk-wb/none", name, warns)
		}
	})

	t.Run("reversal: [repo] default beats a worktree runtime entry", func(t *testing.T) {
		d := preferredResolverDeps(t)
		root := t.TempDir()
		if err := SetRuntimePreferredWorkbenchWith(d, root, "rt-wb"); err != nil {
			t.Fatal(err)
		}
		cfg := &Config{
			Workbenches: []Workbench{{Name: "repo-wb"}, {Name: "rt-wb"}},
			Repo:        map[string]RepoOverrideConfig{root: {RepoScopeConfig: RepoScopeConfig{PreferredWorkbench: "repo-wb"}}},
		}
		if name, warns := cfg.ResolvePreferredWorkbench(d, root); name != "repo-wb" || len(warns) != 0 {
			t.Fatalf("name=%q warns=%v, want repo-wb/none (hand-authored beats runtime)", name, warns)
		}
	})

	t.Run("reversal: committed .pop.toml beats a worktree runtime entry", func(t *testing.T) {
		d := preferredResolverDeps(t)
		root := t.TempDir()
		writePopTOML(t, root, "committed-wb")
		if err := SetRuntimePreferredWorkbenchWith(d, root, "rt-wb"); err != nil {
			t.Fatal(err)
		}
		cfg := &Config{Workbenches: []Workbench{{Name: "committed-wb"}, {Name: "rt-wb"}}}
		if name, warns := cfg.ResolvePreferredWorkbench(d, root); name != "committed-wb" || len(warns) != 0 {
			t.Fatalf("name=%q warns=%v, want committed-wb/none (in-tree beats runtime)", name, warns)
		}
	})

	t.Run("global-shadow: [repo] beats committed .pop.toml for the key", func(t *testing.T) {
		d := preferredResolverDeps(t)
		root := t.TempDir()
		writePopTOML(t, root, "committed-wb")
		cfg := &Config{
			Workbenches: []Workbench{{Name: "central-wb"}, {Name: "committed-wb"}},
			Repo:        map[string]RepoOverrideConfig{root: {RepoScopeConfig: RepoScopeConfig{PreferredWorkbench: "central-wb"}}},
		}
		if name, warns := cfg.ResolvePreferredWorkbench(d, root); name != "central-wb" || len(warns) != 0 {
			t.Fatalf("name=%q warns=%v, want central-wb/none (central config.toml shadows committed .pop.toml)", name, warns)
		}
	})

	t.Run("two-anchor: worktree's own .pop.toml overrides the inherited trunk one", func(t *testing.T) {
		d := preferredResolverDeps(t)
		trunk := t.TempDir()
		child := t.TempDir()
		d.Trunk = func(string) (string, bool) { return trunk, true }
		writePopTOML(t, trunk, "trunk-wb")
		writePopTOML(t, child, "child-wb")
		cfg := &Config{Workbenches: []Workbench{{Name: "trunk-wb"}, {Name: "child-wb"}}}
		if name, warns := cfg.ResolvePreferredWorkbench(d, child); name != "child-wb" || len(warns) != 0 {
			t.Fatalf("name=%q warns=%v, want child-wb/none (own .pop.toml overrides inherited)", name, warns)
		}
	})

	t.Run("two-anchor: bare repo falls back to the identity-root .pop.toml", func(t *testing.T) {
		d := preferredResolverDeps(t)
		bareRoot := t.TempDir()
		if err := os.MkdirAll(filepath.Join(bareRoot, ".bare"), 0o755); err != nil {
			t.Fatal(err)
		}
		worktree := filepath.Join(bareRoot, "main")
		if err := os.MkdirAll(worktree, 0o755); err != nil {
			t.Fatal(err)
		}
		// Shared committed config lives at the identity root; no trunk anchor.
		writePopTOML(t, bareRoot, "id-root-wb")
		d.Trunk = func(string) (string, bool) { return "", false }
		cfg := &Config{Workbenches: []Workbench{{Name: "id-root-wb"}}}
		if name, warns := cfg.ResolvePreferredWorkbench(d, worktree); name != "id-root-wb" || len(warns) != 0 {
			t.Fatalf("name=%q warns=%v, want id-root-wb/none (bare repo inherits identity-root .pop.toml)", name, warns)
		}
	})

	t.Run("stale .pop.toml name warns and falls through to the runtime entry", func(t *testing.T) {
		d := preferredResolverDeps(t)
		root := t.TempDir()
		writePopTOML(t, root, "ghost")
		if err := SetRuntimePreferredWorkbenchWith(d, root, "rt-wb"); err != nil {
			t.Fatal(err)
		}
		cfg := &Config{Workbenches: []Workbench{{Name: "rt-wb"}}}
		name, warns := cfg.ResolvePreferredWorkbench(d, root)
		if name != "rt-wb" {
			t.Fatalf("name=%q, want rt-wb (stale in-tree skips to runtime gap-filler)", name)
		}
		if len(warns) != 1 || !strings.Contains(warns[0], "ghost") {
			t.Fatalf("warns=%v, want one naming the stale .pop.toml name", warns)
		}
	})
}

// TestResolveRepoConfigSharedSchema exercises the unified repo-scope schema
// (ADR-0083): preferred_workbench now rides the shared key set, so it parses
// from a committed .pop.toml as well as from a global [repo."<path>"] block, and
// the personal override beats .pop.toml for the same key. trunk stays
// [repo]-only and is rejected in .pop.toml.
func TestResolveRepoConfigSharedSchema(t *testing.T) {
	real := deps.NewRealFileSystem()
	newDeps := func() *Deps {
		return &Deps{FS: &deps.MockFileSystem{
			StatFunc:         real.Stat,
			ReadFileFunc:     real.ReadFile,
			EvalSymlinksFunc: real.EvalSymlinks,
			UserHomeDirFunc:  real.UserHomeDir,
		}}
	}
	writePopTOML := func(t *testing.T, dir, body string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(dir, ".pop.toml"), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	t.Run("preferred_workbench parses from .pop.toml", func(t *testing.T) {
		root := t.TempDir()
		writePopTOML(t, root, "preferred_workbench = \"gs-dev\"\n")
		cfg := &Config{}
		got, err := cfg.ResolveRepoConfig(newDeps(), root)
		if err != nil {
			t.Fatal(err)
		}
		if got.PreferredWorkbench != "gs-dev" {
			t.Errorf("PreferredWorkbench = %q, want %q (parsed from committed .pop.toml)", got.PreferredWorkbench, "gs-dev")
		}
	})

	t.Run("preferred_workbench parses from [repo] block", func(t *testing.T) {
		root := t.TempDir()
		cfg := &Config{Repo: map[string]RepoOverrideConfig{
			root: {RepoScopeConfig: RepoScopeConfig{PreferredWorkbench: "gs-dev"}},
		}}
		got, err := cfg.ResolveRepoConfig(newDeps(), root)
		if err != nil {
			t.Fatal(err)
		}
		if got.PreferredWorkbench != "gs-dev" {
			t.Errorf("PreferredWorkbench = %q, want %q (parsed from [repo] block)", got.PreferredWorkbench, "gs-dev")
		}
	})

	t.Run("[repo] block beats .pop.toml for the same key", func(t *testing.T) {
		root := t.TempDir()
		writePopTOML(t, root, "preferred_workbench = \"committed\"\n")
		cfg := &Config{Repo: map[string]RepoOverrideConfig{
			root: {RepoScopeConfig: RepoScopeConfig{PreferredWorkbench: "personal"}},
		}}
		got, err := cfg.ResolveRepoConfig(newDeps(), root)
		if err != nil {
			t.Fatal(err)
		}
		if got.PreferredWorkbench != "personal" {
			t.Errorf("PreferredWorkbench = %q, want %q (personal [repo] beats committed .pop.toml)", got.PreferredWorkbench, "personal")
		}
	})

	t.Run(".pop.toml retained when [repo] leaves the key unset", func(t *testing.T) {
		root := t.TempDir()
		writePopTOML(t, root, "preferred_workbench = \"committed\"\n")
		cfg := &Config{Repo: map[string]RepoOverrideConfig{
			root: {Trunk: boolPtr(true)},
		}}
		got, err := cfg.ResolveRepoConfig(newDeps(), root)
		if err != nil {
			t.Fatal(err)
		}
		if got.PreferredWorkbench != "committed" {
			t.Errorf("PreferredWorkbench = %q, want %q (.pop.toml retained when override unset)", got.PreferredWorkbench, "committed")
		}
	})

	t.Run("trunk stays [repo]-only: rejected in .pop.toml", func(t *testing.T) {
		root := t.TempDir()
		writePopTOML(t, root, "trunk = true\n")
		// Scope-legality (slice 03): trunk in .pop.toml is now non-fatal — it is
		// ignored and surfaced as a finding rather than aborting the load.
		cfg, err := LoadRepoConfig(root)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if cfg.Trunk {
			t.Error("trunk in .pop.toml must not be honored")
		}
		var warned bool
		for _, f := range cfg.Findings {
			if strings.Contains(f.Message, "trunk is only valid in a global") {
				warned = true
			}
		}
		if !warned {
			t.Fatalf("want trunk-only-in-global finding, got %+v", cfg.Findings)
		}
	})
}

// As of slice 02 (ADR-0083), ResolvePreferredWorkbench's ladder consults the
// committed .pop.toml at layer 3, so a .pop.toml-only preferred_workbench (with
// no [repo] override above it) now supplies the resolved value.
func TestPreferredWorkbenchFromPopTOML(t *testing.T) {
	root := t.TempDir()
	popTOML := "preferred_workbench = \"gs-dev\"\n" +
		"[[workbenches]]\nname = \"gs-dev\"\n"
	if err := os.WriteFile(filepath.Join(root, ".pop.toml"), []byte(popTOML), 0o644); err != nil {
		t.Fatal(err)
	}
	real := deps.NewRealFileSystem()
	d := &Deps{FS: &deps.MockFileSystem{
		StatFunc:         real.Stat,
		ReadFileFunc:     real.ReadFile,
		EvalSymlinksFunc: real.EvalSymlinks,
		UserHomeDirFunc:  real.UserHomeDir,
	}}
	cfg := &Config{}
	name, warns := cfg.ResolvePreferredWorkbench(d, root)
	if name != "gs-dev" {
		t.Errorf("name = %q, want gs-dev (committed .pop.toml supplies the preferred workbench)", name)
	}
	if len(warns) != 0 {
		t.Errorf("unexpected warnings: %v", warns)
	}
}

func TestResolveRepoConfigNoPOPTOML(t *testing.T) {
	// Global override sets trunk for a repo with no .pop.toml
	dir := t.TempDir()
	real := deps.NewRealFileSystem()
	d := &Deps{FS: &deps.MockFileSystem{
		StatFunc:         real.Stat,
		ReadFileFunc:     real.ReadFile,
		EvalSymlinksFunc: real.EvalSymlinks,
		UserHomeDirFunc:  real.UserHomeDir,
	}}
	cfg := &Config{
		Repo: map[string]RepoOverrideConfig{
			dir: {Trunk: boolPtr(true)},
		},
	}
	got, err := cfg.ResolveRepoConfig(d, dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !got.Trunk {
		t.Errorf("Trunk = false, want true")
	}
}

func TestResolveRepoConfigTrunkPerCheckout(t *testing.T) {
	// trunk=true block keyed by /bare/main must NOT propagate to /bare/feature.
	bareDir := "/bare"
	d := &Deps{FS: makeFSWithBare(bareDir)}

	cfg := &Config{
		Repo: map[string]RepoOverrideConfig{
			bareDir + "/main": {Trunk: boolPtr(true)},
		},
	}

	mainGot, err := cfg.ResolveRepoConfig(d, bareDir+"/main")
	if err != nil {
		t.Fatal(err)
	}
	if !mainGot.Trunk {
		t.Errorf("main: Trunk = false, want true (keyed checkout)")
	}

	featureGot, err := cfg.ResolveRepoConfig(d, bareDir+"/feature")
	if err != nil {
		t.Fatal(err)
	}
	if featureGot.Trunk {
		t.Errorf("feature: Trunk = true, want false (not the keyed checkout)")
	}
}

func TestRepoBlockUnknownKeyWarning(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "config.toml")
	content := `
[repo."/path/to/repo"]
trunk = true
projects = ["should-warn"]
`
	if err := os.WriteFile(configPath, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(configPath)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// The override block is still accepted (degrades to known fields only)
	block, ok := cfg.Repo["/path/to/repo"]
	if !ok {
		t.Fatalf("repo block not parsed")
	}
	if block.Trunk == nil || !*block.Trunk {
		t.Errorf("trunk not parsed correctly from repo block")
	}
	// A warning must be emitted for the unknown key
	found := false
	for _, w := range cfg.Warnings {
		if strings.Contains(w, "projects") && strings.Contains(w, "ignored") {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected warning about unknown key 'projects', got: %v", cfg.Warnings)
	}
}

// TestRepoBlockQueueBaseRenameIsFinding proves the migration tripwire is
// confined (ADR 0054): the queue_base→trunk rename no longer aborts Load(); it
// becomes a blocking "repo" finding that ResolveRepoConfig (the execution-config
// getter consuming commands hit) returns as its error, while getters for other
// sections (EffortFor) stay clean.
func TestRepoBlockQueueBaseRenameIsFinding(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "config.toml")
	content := `
[repo."/path/to/repo"]
queue_base = true
`
	if err := os.WriteFile(configPath, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(configPath)
	if err != nil {
		t.Fatalf("Load err = %v, want the rename to be a finding, not a Load() error", err)
	}
	if cfg.blockingFindingFor("repo") == nil {
		t.Fatalf("expected a blocking 'repo' finding, findings = %+v", cfg.Findings)
	}
	// The execution-config getter surfaces it as the migration message.
	d := &Deps{FS: deps.NewRealFileSystem()}
	if _, err := cfg.ResolveRepoConfig(d, "/path/to/repo"); err == nil || !strings.Contains(err.Error(), "queue_base was renamed to trunk") {
		t.Fatalf("ResolveRepoConfig err = %v, want queue_base rename error", err)
	}
	// A getter for an unrelated section is unaffected.
	if _, err := cfg.EffortFor("standard"); err != nil {
		t.Fatalf("EffortFor err = %v, want nil (rename must not poison unrelated getters)", err)
	}
	// The finding is still mirrored into the non-blocking warning banner.
	mirrored := false
	for _, w := range cfg.Warnings {
		if strings.Contains(w, "queue_base was renamed to trunk") {
			mirrored = true
			break
		}
	}
	if !mirrored {
		t.Fatalf("rename finding must mirror into Warnings, got %v", cfg.Warnings)
	}
}

func TestRepoLocalExecutionBaseHardError(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, ".pop.toml"), []byte("execution_base = true\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := LoadRepoConfigWith(&Deps{FS: deps.NewRealFileSystem()}, root)
	if err == nil || !strings.Contains(err.Error(), "execution_base was renamed to trunk") {
		t.Fatalf("LoadRepoConfig err = %v, want execution_base rename error", err)
	}
}

func TestRepoBlockGlobalOnlyKeysIgnored(t *testing.T) {
	// Global-only keys inside a repo block must degrade (warn) not hard-fail.
	configPath := filepath.Join(t.TempDir(), "config.toml")
	content := `
[repo."/path/to/repo"]
exclude_current_session = true
`
	if err := os.WriteFile(configPath, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(configPath)
	if err != nil {
		t.Fatalf("load must not hard-fail on unknown repo block key: %v", err)
	}
	if len(cfg.Warnings) == 0 {
		t.Errorf("expected at least one warning for unknown repo block key")
	}
	// The top-level ExcludeCurrentSession must NOT be set from a repo block
	if cfg.ExcludeCurrentSession {
		t.Errorf("global ExcludeCurrentSession must not be set from repo block")
	}
}

// TestBrokenConfigMatrix asserts the caller-scoped contract (ADR 0054) across
// the five canonical broken-config scenarios: each row feeds a deliberately
// broken config and verifies render-vs-abort per capability boundary.
func TestBrokenConfigMatrix(t *testing.T) {
	type row struct {
		name string
		toml string
		// Load-level expectations
		loadFails bool
		// ProjectEntries getter (project dashboard capability)
		projectsEmpty bool // Load succeeds, getter returns empty list, nil error
		projectsFails bool // getter returns non-nil error
		// EffortFor getter (tasks drain capability)
		effortFails bool
		// ResolveRepoConfig getter (queue/tasks-drain repo capability)
		repoFails bool
		// Findings/Warnings assertions (when Load succeeds)
		wantFindingPath string
		wantWarning     string
	}

	tests := []row{
		{
			name: "unrelated effort rename — dashboard renders, tasks drain hard-fails",
			toml: `
[[projects]]
path = "~/Dev"

[effort.opencode]
extreme = [{ model = "opencode/claude-opus-4-8" }]
`,
			effortFails:     true,
			wantFindingPath: "effort.opencode.extreme",
			wantWarning:     `unknown tier "extreme"`,
		},
		{
			name: "queue_base rename — dashboard renders, repo getter hard-fails",
			toml: `
[[projects]]
path = "~/Dev"

[repo."/path/to/repo"]
queue_base = true
`,
			repoFails:       true,
			wantFindingPath: "repo",
			wantWarning:     "queue_base was renamed to trunk",
		},
		{
			name: "bad display_depth type — picker renders at default depth, warns",
			toml: `
[[projects]]
path = "~/Dev"
display_depth = "two"
`,
			wantFindingPath: "projects[].display_depth",
			wantWarning:     "non-integer display_depth",
		},
		{
			name:          "empty projects list — Load succeeds, getter returns empty",
			toml:          `projects = []`,
			projectsEmpty: true,
		},
		{
			name:      "TOML syntax error — Load returns fatal parse error",
			toml:      "this is = not valid = toml\n",
			loadFails: true,
		},
	}

	fd := &Deps{FS: deps.NewRealFileSystem()}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			configPath := filepath.Join(t.TempDir(), "config.toml")
			if err := os.WriteFile(configPath, []byte(tt.toml), 0o644); err != nil {
				t.Fatal(err)
			}

			cfg, loadErr := Load(configPath)
			if tt.loadFails {
				if loadErr == nil {
					t.Fatal("Load() succeeded, want fatal parse error")
				}
				return
			}
			if loadErr != nil {
				t.Fatalf("Load() = %v, want nil (class-B problems must not abort Load)", loadErr)
			}

			// Finding path check
			if tt.wantFindingPath != "" {
				found := false
				for _, f := range cfg.Findings {
					if f.Path == tt.wantFindingPath {
						found = true
						break
					}
				}
				if !found {
					t.Errorf("no finding with path %q; findings = %+v", tt.wantFindingPath, cfg.Findings)
				}
			}

			// Warning mirror check
			if tt.wantWarning != "" && !containsSubstring(cfg.Warnings, tt.wantWarning) {
				t.Errorf("Warnings %v missing %q", cfg.Warnings, tt.wantWarning)
			}

			// ProjectEntries: project dashboard capability
			entries, err := cfg.ProjectEntries()
			if tt.projectsFails {
				if err == nil {
					t.Error("ProjectEntries() = nil error, want error (project dashboard should abort)")
				}
			} else {
				if err != nil {
					t.Errorf("ProjectEntries() error = %v, want nil (project dashboard should render)", err)
				}
			}
			if tt.projectsEmpty && len(entries) != 0 {
				t.Errorf("ProjectEntries() returned %d entries, want 0", len(entries))
			}

			// EffortFor: tasks drain capability
			if _, err := cfg.EffortFor("opencode"); tt.effortFails {
				if err == nil {
					t.Error("EffortFor() = nil error, want error (tasks drain should hard-fail)")
				}
			} else if err != nil {
				t.Errorf("EffortFor() error = %v, want nil (must not poison unrelated callers)", err)
			}

			// ResolveRepoConfig: queue/tasks repo capability
			if _, err := cfg.ResolveRepoConfig(fd, "/path/to/repo"); tt.repoFails {
				if err == nil {
					t.Error("ResolveRepoConfig() = nil error, want error (queue/tasks should hard-fail)")
				}
			} else if err != nil {
				t.Errorf("ResolveRepoConfig() error = %v, want nil (must not poison unrelated callers)", err)
			}
		})
	}
}

// TestPaneMonitoringTopicWords verifies the topic_words knob parses, defaults to
// DefaultTopicWords when unset/non-positive, and otherwise reports its value.
func TestPaneMonitoringTopicWords(t *testing.T) {
	t.Run("parses configured value", func(t *testing.T) {
		tmpDir := t.TempDir()
		configPath := filepath.Join(tmpDir, "config.toml")
		os.WriteFile(configPath, []byte("[pane_monitoring]\ntopic_words = 3\n"), 0644)

		cfg, err := Load(configPath)
		if err != nil {
			t.Fatalf("Load() error: %v", err)
		}
		if got := cfg.PaneMonitoringTopicWords(); got != 3 {
			t.Errorf("PaneMonitoringTopicWords() = %d, want 3", got)
		}
	})

	t.Run("defaults to 5 when unset", func(t *testing.T) {
		if got := (&Config{}).PaneMonitoringTopicWords(); got != DefaultTopicWords {
			t.Errorf("PaneMonitoringTopicWords() = %d, want %d", got, DefaultTopicWords)
		}
		cfg := &Config{PaneMonitoring: &PaneMonitoringConfig{}}
		if got := cfg.PaneMonitoringTopicWords(); got != DefaultTopicWords {
			t.Errorf("PaneMonitoringTopicWords() = %d, want %d", got, DefaultTopicWords)
		}
	})

	t.Run("non-positive falls back to default", func(t *testing.T) {
		cfg := &Config{PaneMonitoring: &PaneMonitoringConfig{TopicWords: 0}}
		if got := cfg.PaneMonitoringTopicWords(); got != DefaultTopicWords {
			t.Errorf("PaneMonitoringTopicWords() = %d, want %d", got, DefaultTopicWords)
		}
		cfg = &Config{PaneMonitoring: &PaneMonitoringConfig{TopicWords: -2}}
		if got := cfg.PaneMonitoringTopicWords(); got != DefaultTopicWords {
			t.Errorf("PaneMonitoringTopicWords() = %d, want %d", got, DefaultTopicWords)
		}
	})
}

// TestPaneMonitoringTopicDerivationTimeout verifies the topic_derivation_timeout
// knob parses (as seconds), defaults to DefaultTopicDerivationTimeoutSeconds when
// unset/non-positive, and otherwise reports its value as a duration.
func TestPaneMonitoringTopicDerivationTimeout(t *testing.T) {
	defaultDur := time.Duration(DefaultTopicDerivationTimeoutSeconds) * time.Second

	t.Run("parses configured value as seconds", func(t *testing.T) {
		tmpDir := t.TempDir()
		configPath := filepath.Join(tmpDir, "config.toml")
		os.WriteFile(configPath, []byte("[pane_monitoring]\ntopic_derivation_timeout = 45\n"), 0644)

		cfg, err := Load(configPath)
		if err != nil {
			t.Fatalf("Load() error: %v", err)
		}
		if got := cfg.PaneMonitoringTopicDerivationTimeout(); got != 45*time.Second {
			t.Errorf("PaneMonitoringTopicDerivationTimeout() = %v, want 45s", got)
		}
	})

	t.Run("defaults when unset", func(t *testing.T) {
		if got := (&Config{}).PaneMonitoringTopicDerivationTimeout(); got != defaultDur {
			t.Errorf("PaneMonitoringTopicDerivationTimeout() = %v, want %v", got, defaultDur)
		}
		cfg := &Config{PaneMonitoring: &PaneMonitoringConfig{}}
		if got := cfg.PaneMonitoringTopicDerivationTimeout(); got != defaultDur {
			t.Errorf("PaneMonitoringTopicDerivationTimeout() = %v, want %v", got, defaultDur)
		}
	})

	t.Run("non-positive falls back to default", func(t *testing.T) {
		cfg := &Config{PaneMonitoring: &PaneMonitoringConfig{TopicDerivationTimeout: 0}}
		if got := cfg.PaneMonitoringTopicDerivationTimeout(); got != defaultDur {
			t.Errorf("PaneMonitoringTopicDerivationTimeout() = %v, want %v", got, defaultDur)
		}
		cfg = &Config{PaneMonitoring: &PaneMonitoringConfig{TopicDerivationTimeout: -3}}
		if got := cfg.PaneMonitoringTopicDerivationTimeout(); got != defaultDur {
			t.Errorf("PaneMonitoringTopicDerivationTimeout() = %v, want %v", got, defaultDur)
		}
	})
}

func TestWorkbenchThreeHomeResolution(t *testing.T) {
	// Test that templates are resolved from three homes with most-specific-wins:
	// [repo."<path>"] > .pop.toml > global library

	t.Run("global templates only", func(t *testing.T) {
		tmpDir := t.TempDir()
		configPath := filepath.Join(tmpDir, "config.toml")
		if err := os.WriteFile(configPath, []byte(`
[[workbenches]]
name = "dev"
windows = [{name = "main", layout = {name = "editor", command = "vim"}}]
`), 0644); err != nil {
			t.Fatal(err)
		}

		cfg, err := Load(configPath)
		if err != nil {
			t.Fatalf("Load failed: %v", err)
		}

		d := &Deps{
			FS: &deps.MockFileSystem{
				StatFunc: func(path string) (os.FileInfo, error) {
					// No .bare directory, no .pop.toml
					return nil, os.ErrNotExist
				},
				ReadFileFunc: func(path string) ([]byte, error) {
					if path == configPath {
						return os.ReadFile(configPath)
					}
					return nil, os.ErrNotExist
				},
				EvalSymlinksFunc: func(path string) (string, error) {
					return path, nil
				},
				UserHomeDirFunc: func() (string, error) {
					return tmpDir, nil
				},
			},
		}

		templates, warnings := cfg.ResolveWorkbenchesWith(d, tmpDir)
		if len(warnings) != 0 {
			t.Errorf("expected no warnings, got %v", warnings)
		}
		if len(templates) != 1 {
			t.Fatalf("expected 1 template, got %d", len(templates))
		}
		if templates[0].Name != "dev" {
			t.Errorf("expected template name 'dev', got %q", templates[0].Name)
		}
	})

	t.Run(".pop.toml templates only", func(t *testing.T) {
		tmpDir := t.TempDir()
		configPath := filepath.Join(tmpDir, "config.toml")
		if err := os.WriteFile(configPath, []byte(""), 0644); err != nil {
			t.Fatal(err)
		}

		popTomlPath := filepath.Join(tmpDir, ".pop.toml")
		if err := os.WriteFile(popTomlPath, []byte(`
[[workbenches]]
name = "work"
windows = [{name = "main", layout = {name = "editor", command = "vim"}}]
`), 0644); err != nil {
			t.Fatal(err)
		}

		cfg, err := Load(configPath)
		if err != nil {
			t.Fatalf("Load failed: %v", err)
		}

		d := &Deps{
			FS: &deps.MockFileSystem{
				StatFunc: func(path string) (os.FileInfo, error) {
					if path == filepath.Join(tmpDir, ".bare") {
						return nil, os.ErrNotExist
					}
					return nil, os.ErrNotExist
				},
				ReadFileFunc: func(path string) ([]byte, error) {
					if path == configPath {
						return os.ReadFile(configPath)
					}
					if path == popTomlPath {
						return os.ReadFile(popTomlPath)
					}
					return nil, os.ErrNotExist
				},
				EvalSymlinksFunc: func(path string) (string, error) {
					return path, nil
				},
				UserHomeDirFunc: func() (string, error) {
					return tmpDir, nil
				},
			},
		}

		templates, warnings := cfg.ResolveWorkbenchesWith(d, tmpDir)
		if len(warnings) != 0 {
			t.Errorf("expected no warnings, got %v", warnings)
		}
		if len(templates) != 1 {
			t.Fatalf("expected 1 template, got %d", len(templates))
		}
		if templates[0].Name != "work" {
			t.Errorf("expected template name 'work', got %q", templates[0].Name)
		}
	})

	t.Run("[repo] override templates only", func(t *testing.T) {
		tmpDir := t.TempDir()
		configPath := filepath.Join(tmpDir, "config.toml")
		if err := os.WriteFile(configPath, []byte(fmt.Sprintf(`
[repo."%s"]
workbenches = [
  {name = "review", windows = [{name = "main", layout = {name = "editor", command = "vim"}}]}
]
`, tmpDir)), 0644); err != nil {
			t.Fatal(err)
		}

		cfg, err := Load(configPath)
		if err != nil {
			t.Fatalf("Load failed: %v", err)
		}

		d := &Deps{
			FS: &deps.MockFileSystem{
				StatFunc: func(path string) (os.FileInfo, error) {
					return nil, os.ErrNotExist
				},
				ReadFileFunc: func(path string) ([]byte, error) {
					if path == configPath {
						return os.ReadFile(configPath)
					}
					return nil, os.ErrNotExist
				},
				EvalSymlinksFunc: func(path string) (string, error) {
					return path, nil
				},
				UserHomeDirFunc: func() (string, error) {
					return tmpDir, nil
				},
			},
		}

		templates, warnings := cfg.ResolveWorkbenchesWith(d, tmpDir)
		if len(warnings) != 0 {
			t.Errorf("expected no warnings, got %v", warnings)
		}
		if len(templates) != 1 {
			t.Fatalf("expected 1 template, got %d", len(templates))
		}
		if templates[0].Name != "review" {
			t.Errorf("expected template name 'review', got %q", templates[0].Name)
		}
	})

	t.Run("precedence: [repo] > .pop.toml > global", func(t *testing.T) {
		tmpDir := t.TempDir()
		configPath := filepath.Join(tmpDir, "config.toml")
		// Global template named "dev"
		// [repo] override template also named "dev"
		if err := os.WriteFile(configPath, []byte(fmt.Sprintf(`
[[workbenches]]
name = "dev"
windows = [{name = "main", layout = {name = "editor", command = "vim"}}]

[repo."%s"]
workbenches = [
  {name = "dev", windows = [{name = "main", layout = {name = "editor", command = "code"}}]}
]
`, tmpDir)), 0644); err != nil {
			t.Fatal(err)
		}

		// .pop.toml also has "dev"
		popTomlPath := filepath.Join(tmpDir, ".pop.toml")
		if err := os.WriteFile(popTomlPath, []byte(`
[[workbenches]]
name = "dev"
windows = [{name = "main", layout = {name = "editor", command = "nano"}}]
`), 0644); err != nil {
			t.Fatal(err)
		}

		cfg, err := Load(configPath)
		if err != nil {
			t.Fatalf("Load failed: %v", err)
		}

		d := &Deps{
			FS: &deps.MockFileSystem{
				StatFunc: func(path string) (os.FileInfo, error) {
					return nil, os.ErrNotExist
				},
				ReadFileFunc: func(path string) ([]byte, error) {
					if path == configPath {
						return os.ReadFile(configPath)
					}
					if path == popTomlPath {
						return os.ReadFile(popTomlPath)
					}
					return nil, os.ErrNotExist
				},
				EvalSymlinksFunc: func(path string) (string, error) {
					return path, nil
				},
				UserHomeDirFunc: func() (string, error) {
					return tmpDir, nil
				},
			},
		}

		templates, warnings := cfg.ResolveWorkbenchesWith(d, tmpDir)
		// Should have 2 warnings: global vs .pop.toml, and .pop.toml vs [repo]
		if len(warnings) != 2 {
			t.Errorf("expected 2 warnings, got %d: %v", len(warnings), warnings)
		}
		if len(templates) != 1 {
			t.Fatalf("expected 1 template, got %d", len(templates))
		}
		if templates[0].Name != "dev" {
			t.Errorf("expected template name 'dev', got %q", templates[0].Name)
		}
		// The [repo] override should win, so command should be "code"
		if len(templates[0].Windows) == 0 || templates[0].Windows[0].Layout == nil {
			t.Fatal("template has no windows or layout")
		}
		if templates[0].Windows[0].Layout.Command != "code" {
			t.Errorf("expected [repo] override to win with command 'code', got %q",
				templates[0].Windows[0].Layout.Command)
		}
	})

	t.Run("bare repo .pop.toml applies to all worktrees", func(t *testing.T) {
		// Create a bare repo structure: bare/.bare/ and bare/worktrees/...
		bareDir := t.TempDir()
		bareSubdir := filepath.Join(bareDir, ".bare")
		if err := os.MkdirAll(bareSubdir, 0755); err != nil {
			t.Fatal(err)
		}

		configPath := filepath.Join(bareDir, "config.toml")
		if err := os.WriteFile(configPath, []byte(""), 0644); err != nil {
			t.Fatal(err)
		}

		// .pop.toml in bare repo root
		popTomlPath := filepath.Join(bareDir, ".pop.toml")
		if err := os.WriteFile(popTomlPath, []byte(`
[[workbenches]]
name = "bare-template"
windows = [{name = "main", layout = {name = "editor", command = "vim"}}]
`), 0644); err != nil {
			t.Fatal(err)
		}

		cfg, err := Load(configPath)
		if err != nil {
			t.Fatalf("Load failed: %v", err)
		}

		// Create a worktree path
		worktreeDir := filepath.Join(bareDir, "worktrees", "feature-1")
		if err := os.MkdirAll(worktreeDir, 0755); err != nil {
			t.Fatal(err)
		}

		d := &Deps{
			FS: &deps.MockFileSystem{
				StatFunc: func(path string) (os.FileInfo, error) {
					// .bare directory exists in bareDir
					if path == bareSubdir {
						return &deps.MockFileInfo{
							NameVal:  ".bare",
							IsDirVal: true,
						}, nil
					}
					return nil, os.ErrNotExist
				},
				ReadFileFunc: func(path string) ([]byte, error) {
					if path == configPath {
						return os.ReadFile(configPath)
					}
					// .pop.toml is in bareDir, not worktreeDir
					if path == popTomlPath {
						return os.ReadFile(popTomlPath)
					}
					return nil, os.ErrNotExist
				},
				EvalSymlinksFunc: func(path string) (string, error) {
					return path, nil
				},
				UserHomeDirFunc: func() (string, error) {
					return bareDir, nil
				},
			},
		}

		// Resolve from worktree path - should find .pop.toml in bare repo root
		templates, warnings := cfg.ResolveWorkbenchesWith(d, worktreeDir)
		if len(warnings) != 0 {
			t.Errorf("expected no warnings, got %v", warnings)
		}
		if len(templates) != 1 {
			t.Fatalf("expected 1 template, got %d", len(templates))
		}
		if templates[0].Name != "bare-template" {
			t.Errorf("expected template name 'bare-template', got %q", templates[0].Name)
		}
	})
}
