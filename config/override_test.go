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

// overrideFixture wires a pop data dir (home of both pop-written files) beside a
// hand-authored config.toml over a real temp tree, so one test can write any of
// the three layers and read the merge that comes out.
type overrideFixture struct {
	d            *Deps
	userPath     string
	runtimePath  string
	overridePath string
}

func newOverrideFixture(t *testing.T) *overrideFixture {
	t.Helper()
	root := t.TempDir()
	dataDir := filepath.Join(root, "data")
	f := &overrideFixture{
		userPath:     filepath.Join(root, "config", "config.toml"),
		runtimePath:  filepath.Join(dataDir, "pop", "config.runtime.toml"),
		overridePath: filepath.Join(dataDir, "pop", "config.override.toml"),
	}
	f.d = &Deps{FS: &deps.MockFileSystem{
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
	return f
}

func (f *overrideFixture) load(t *testing.T) *Config {
	t.Helper()
	cfg, err := LoadWith(f.d, f.userPath)
	if err != nil {
		t.Fatalf("LoadWith() error: %v", err)
	}
	return cfg
}

// handAuthoredWork is the user's config.toml for the precedence tests: an agent
// list to be overridden, a sibling key in the same table, and a sibling table.
const handAuthoredWork = `
projects = [{ path = "/main" }]

[work.implement]
agents = ["claude"]
max_tries = 5

[work.verify]
enabled = true
agents = ["claude --verify"]
`

func TestOverrideLayerBeatsHandAuthoredConfig(t *testing.T) {
	f := newOverrideFixture(t)
	writeConfigFile(t, f.userPath, handAuthoredWork)
	writeConfigFile(t, f.overridePath, `
[work.implement]
agents = ["codex --model gpt"]
`)

	cfg := f.load(t)
	if got := cfg.ImplementAgents(); !reflect.DeepEqual(got, []string{"codex --model gpt"}) {
		t.Fatalf("ImplementAgents() = %#v, want the override layer's list", got)
	}
	if tries := cfg.Work.Implement.MaxTries; tries == nil || *tries != 5 {
		t.Errorf("max_tries = %v, want the hand-authored 5 beside the overridden key", tries)
	}
	if cfg.Work.Verify == nil || !cfg.Work.Verify.Enabled {
		t.Errorf("[work.verify] = %#v, want the hand-authored table intact", cfg.Work.Verify)
	}
}

func TestOverrideLayerWithoutTheKeyLeavesHandAuthoredValue(t *testing.T) {
	t.Run("no override file at all", func(t *testing.T) {
		f := newOverrideFixture(t)
		writeConfigFile(t, f.userPath, handAuthoredWork)

		cfg := f.load(t)
		if got := cfg.ImplementAgents(); !reflect.DeepEqual(got, []string{"claude"}) {
			t.Fatalf("ImplementAgents() = %#v, want the hand-authored list", got)
		}
	})

	t.Run("override file present but silent on the key", func(t *testing.T) {
		f := newOverrideFixture(t)
		writeConfigFile(t, f.userPath, handAuthoredWork)
		writeConfigFile(t, f.overridePath, `
[work.attended]
agents = ["codex"]
`)

		cfg := f.load(t)
		if got := cfg.ImplementAgents(); !reflect.DeepEqual(got, []string{"claude"}) {
			t.Fatalf("ImplementAgents() = %#v, want the hand-authored list (no zero-value leakage)", got)
		}
		if got := cfg.VerifyAgents(); !reflect.DeepEqual(got, []string{"claude --verify"}) {
			t.Fatalf("VerifyAgents() = %#v, want the hand-authored list", got)
		}
		if got := cfg.AttendedAgents(); !reflect.DeepEqual(got, []string{"codex"}) {
			t.Fatalf("AttendedAgents() = %#v, want the overridden list", got)
		}
	})

	t.Run("deleting the last override restores the hand-authored value", func(t *testing.T) {
		f := newOverrideFixture(t)
		writeConfigFile(t, f.userPath, handAuthoredWork)
		if err := SetOverrideValueWith(f.d, "work.implement.agents", []any{"codex"}); err != nil {
			t.Fatalf("SetOverrideValueWith() error: %v", err)
		}
		if got := f.load(t).ImplementAgents(); !reflect.DeepEqual(got, []string{"codex"}) {
			t.Fatalf("ImplementAgents() = %#v, want the override in force", got)
		}
		if err := DeleteOverrideValueWith(f.d, "work.implement.agents"); err != nil {
			t.Fatalf("DeleteOverrideValueWith() error: %v", err)
		}
		if got := f.load(t).ImplementAgents(); !reflect.DeepEqual(got, []string{"claude"}) {
			t.Fatalf("ImplementAgents() = %#v, want the hand-authored list back", got)
		}
	})
}

// TestRuntimeLayerKeepsItsGapFillerRank pins the two pop-written files to
// opposite ranks in one merge: the runtime layer still loses to the
// hand-authored file, and the override layer still beats it.
func TestRuntimeLayerKeepsItsGapFillerRank(t *testing.T) {
	f := newOverrideFixture(t)
	writeConfigFile(t, f.runtimePath, `
[work.implement]
agents = ["runtime-agent"]

[integrations]
skills = ["pane"]
`)
	writeConfigFile(t, f.userPath, `
projects = [{ path = "/main" }]

[work.implement]
agents = ["user-agent"]

[integrations]
skills = ["tasks"]
`)

	cfg := f.load(t)
	if got := cfg.ImplementAgents(); !reflect.DeepEqual(got, []string{"user-agent"}) {
		t.Fatalf("ImplementAgents() = %#v, want the hand-authored list (runtime is a gap-filler)", got)
	}
	skills, err := cfg.IntegrationsSkills()
	if err != nil {
		t.Fatalf("IntegrationsSkills() error: %v", err)
	}
	if !reflect.DeepEqual(skills, []string{"tasks"}) {
		t.Fatalf("IntegrationsSkills() = %#v, want [tasks] (runtime loses to the user file)", skills)
	}

	writeConfigFile(t, f.overridePath, `
[work.implement]
agents = ["override-agent"]
`)
	if got := f.load(t).ImplementAgents(); !reflect.DeepEqual(got, []string{"override-agent"}) {
		t.Fatalf("ImplementAgents() = %#v, want the override layer's list", got)
	}
}

// TestRuntimePreferredWorkbenchSentinelSurvivesSharedDocument guards the
// three-valued explicit-none entry (ADR-0078) now that both pop-written files
// share one document reader: an empty name must still read back as present.
func TestRuntimePreferredWorkbenchSentinelSurvivesSharedDocument(t *testing.T) {
	f := newOverrideFixture(t)
	if err := SetRuntimePreferredWorkbenchWith(f.d, "/repo/app", ""); err != nil {
		t.Fatalf("SetRuntimePreferredWorkbenchWith() error: %v", err)
	}
	name, present, err := RuntimePreferredWorkbenchWith(f.d, "/repo/app")
	if err != nil {
		t.Fatalf("RuntimePreferredWorkbenchWith() error: %v", err)
	}
	if name != "" || !present {
		t.Fatalf("name=%q present=%v, want explicit none (present with an empty name)", name, present)
	}
	if _, present, _ := RuntimePreferredWorkbenchWith(f.d, "/repo/other"); present {
		t.Fatal("an unrecorded checkout must read as absent, not explicit none")
	}
}

func TestOverrideWriteSetsOnePruneAndDeletesTheFile(t *testing.T) {
	f := newOverrideFixture(t)

	if _, ok, err := OverrideValueWith(f.d, "work.implement.agents"); err != nil || ok {
		t.Fatalf("OverrideValueWith() = ok %v, err %v, want absent with no error", ok, err)
	}
	if err := SetOverrideValueWith(f.d, "work.implement.agents", []any{"codex"}); err != nil {
		t.Fatalf("SetOverrideValueWith(implement) error: %v", err)
	}
	if err := SetOverrideValueWith(f.d, "work.attended.agents", []any{"claude"}); err != nil {
		t.Fatalf("SetOverrideValueWith(attended) error: %v", err)
	}
	if value, ok, err := OverrideValueWith(f.d, "work.implement.agents"); err != nil || !ok ||
		!reflect.DeepEqual(value, []any{"codex"}) {
		t.Fatalf("OverrideValueWith() = %#v, ok %v, err %v, want the stored list", value, ok, err)
	}

	// Deleting one key prunes nothing else: the sibling override and the [work]
	// table it lives in survive.
	if err := DeleteOverrideValueWith(f.d, "work.implement.agents"); err != nil {
		t.Fatalf("DeleteOverrideValueWith(implement) error: %v", err)
	}
	var doc map[string]any
	if _, err := toml.DecodeFile(f.overridePath, &doc); err != nil {
		t.Fatalf("decode override file: %v", err)
	}
	work, ok := doc["work"].(map[string]any)
	if !ok {
		t.Fatalf("[work] table lost: %#v", doc)
	}
	if _, still := work["implement"]; still {
		t.Errorf("emptied [work.implement] table kept: %#v", work)
	}
	if _, ok := work["attended"].(map[string]any); !ok {
		t.Errorf("sibling override lost: %#v", work)
	}

	// Deleting a key with no override changes nothing.
	if err := DeleteOverrideValueWith(f.d, "work.implement.agents"); err != nil {
		t.Fatalf("DeleteOverrideValueWith() on an unset key: %v", err)
	}
	if _, err := os.Stat(f.overridePath); err != nil {
		t.Fatalf("override file gone while a key remains: %v", err)
	}

	// The last key going takes the file with it, rather than leaving an empty table.
	if err := DeleteOverrideValueWith(f.d, "work.attended.agents"); err != nil {
		t.Fatalf("DeleteOverrideValueWith(attended) error: %v", err)
	}
	if _, err := os.Stat(f.overridePath); !os.IsNotExist(err) {
		data, _ := os.ReadFile(f.overridePath)
		t.Fatalf("override file survived its last key: err=%v content=%q", err, data)
	}
}

func TestOverrideWriteIsAtomic(t *testing.T) {
	f := newOverrideFixture(t)
	var renameTarget string
	mock := f.d.FS.(*deps.MockFileSystem)
	mock.RenameFunc = func(oldpath, newpath string) error {
		renameTarget = newpath
		if !strings.HasPrefix(filepath.Base(oldpath), ".config.override.tmp-") {
			t.Errorf("wrote %q directly, want a temp file then a rename", oldpath)
		}
		return os.Rename(oldpath, newpath)
	}

	if err := SetOverrideValueWith(f.d, "work.verify.agents", []any{"codex"}); err != nil {
		t.Fatalf("SetOverrideValueWith() error: %v", err)
	}
	if renameTarget != f.overridePath {
		t.Fatalf("atomic rename target = %q, want %q", renameTarget, f.overridePath)
	}
}

// TestSetOverrideValueRejectsAKeyTheLayerCannotHold pins the gate on the write
// itself: a path that is no config key at all is refused before the document is
// touched, in the words the dashboard's editor would have shown.
func TestSetOverrideValueRejectsAKeyTheLayerCannotHold(t *testing.T) {
	f := newOverrideFixture(t)
	if err := SetOverrideValueWith(f.d, "work.verify.enabled", true); err != nil {
		t.Fatalf("SetOverrideValueWith() error: %v", err)
	}
	err := SetOverrideValueWith(f.d, "work.verify.enabled.deeper", "x")
	if err == nil || !strings.Contains(err.Error(), "not a key pop can override") {
		t.Fatalf("error = %v, want the key refused", err)
	}
	body, readErr := os.ReadFile(f.overridePath)
	if readErr != nil {
		t.Fatal(readErr)
	}
	if strings.Contains(string(body), "deeper") {
		t.Errorf("a refused key reached the file:\n%s", body)
	}
}

// TestConfigShowRendersTheOverrideLayer checks the merge `pop config show`
// prints carries the override value: the layer needs no rendering work of its
// own to be visible there.
func TestConfigShowRendersTheOverrideLayer(t *testing.T) {
	f := newOverrideFixture(t)
	writeConfigFile(t, f.userPath, handAuthoredWork)
	writeConfigFile(t, f.overridePath, `
[work.implement]
agents = ["codex --model gpt"]
`)

	out, err := EffectiveTOMLWith(f.d, f.userPath, nil)
	if err != nil {
		t.Fatalf("EffectiveTOMLWith() error: %v", err)
	}
	if !strings.Contains(out, "codex --model gpt") {
		t.Fatalf("effective TOML missing the override value:\n%s", out)
	}
	if strings.Contains(out, `cmd = "claude"`) {
		t.Fatalf("effective TOML still shows the overridden hand-authored value:\n%s", out)
	}
}

// TestOverrideLayerBeatsAnIncludeFile covers the other hand-authored source: an
// include is merged after the layer ladder, so the override must claim its keys
// or a bare include file would win over it.
func TestOverrideLayerBeatsAnIncludeFile(t *testing.T) {
	f := newOverrideFixture(t)
	includePath := filepath.Join(filepath.Dir(f.userPath), "extra.toml")
	writeConfigFile(t, includePath, `
[work.implement]
agents = ["include-agent"]

[work.verify]
agents = ["include-verifier"]
`)
	writeConfigFile(t, f.userPath, `
projects = [{ path = "/main" }]
includes = ["extra.toml"]
`)
	writeConfigFile(t, f.overridePath, `
[work.implement]
agents = []

[work.verify]
agents = ["override-verifier"]
`)

	cfg := f.load(t)
	if got := cfg.ImplementAgents(); len(got) != 0 {
		t.Errorf("ImplementAgents() = %#v, want the override's deliberately empty list", got)
	}
	if got := cfg.VerifyAgents(); !reflect.DeepEqual(got, []string{"override-verifier"}) {
		t.Errorf("VerifyAgents() = %#v, want the override layer's list", got)
	}
}

func TestDefaultOverrideConfigPathWith(t *testing.T) {
	d := &Deps{FS: &deps.MockFileSystem{
		GetenvFunc: func(key string) string {
			if key == "XDG_DATA_HOME" {
				return "/custom/data"
			}
			return ""
		},
	}}
	got := DefaultOverrideConfigPathWith(d)
	want := "/custom/data/pop/config.override.toml"
	if got != want {
		t.Fatalf("DefaultOverrideConfigPathWith() = %q, want %q", got, want)
	}
}
