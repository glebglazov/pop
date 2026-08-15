package config

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/glebglazov/pop/internal/deps"
)

// runtimeTestDeps builds deps whose pop-written files land in an isolated data
// dir over the real filesystem, and hands back the path of the retired runtime
// record — the file these tests seed and nothing in pop writes.
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

// TestRetiredRuntimeRecordFoldsIntoTheOverrideLayer is the slice in one run: a
// machine holding every section the record could hold loads config once, and the
// trunk, the turn cap and the declined component come out as stated values —
// beating the hand-authored list they used to lose to — while the file itself is
// gone from the path pop reads.
func TestRetiredRuntimeRecordFoldsIntoTheOverrideLayer(t *testing.T) {
	fx := newPopRepoFixture(t)
	writeFixtureConfig(t, fx, `
[integrations]
skills = ["tasks", "pane"]

[repo.`+quotedPath(fx.main)+`]
turn_cap = 99
`)
	writeRuntimeFile(t, fx.runtimePath, `
[integrations]
skills = ["tasks"]

[repo_settings.`+quotedPath(fx.identity)+`]
turn_cap = 40

[`+quotedPath(fx.main)+`]
trunk = true

[workbench.preferred]
`+quotedPath(fx.feature)+` = "minimal"
`)

	cfg, err := LoadWith(fx.d, fx.configPath)
	if err != nil {
		t.Fatalf("LoadWith: %v", err)
	}

	// Global scope: the recorded decline now states the list, over the user's own.
	skills, err := cfg.IntegrationsSkills()
	if err != nil {
		t.Fatalf("IntegrationsSkills: %v", err)
	}
	if !reflect.DeepEqual(skills, []string{"tasks"}) {
		t.Errorf("skills = %#v, want the folded decline to beat the hand-authored list", skills)
	}

	// Repository scope: one entry, read from either worktree, over a declaration.
	for _, checkout := range []string{fx.main, fx.feature} {
		repoCfg, err := cfg.ResolveRepoConfig(fx.d, checkout)
		if err != nil {
			t.Fatalf("%s: %v", checkout, err)
		}
		if !repoCfg.IsTrunk(fx.d, fx.main) {
			t.Errorf("trunk at %s = %q, want the recorded %s", checkout, repoCfg.Trunk, fx.main)
		}
		if repoCfg.TurnCap != 40 {
			t.Errorf("turn cap at %s = %d, want the folded 40 over the declared 99", checkout, repoCfg.TurnCap)
		}
	}

	body := readFile(t, fx.overridePath)
	for _, want := range []string{`skills = ["tasks"]`, "turn_cap = 40", "trunk = "} {
		if !strings.Contains(body, want) {
			t.Errorf("the override layer does not hold %q:\n%s", want, body)
		}
	}
	// A Preferred workbench was recorded per checkout by a chord that no longer
	// exists; it has no repository-wide meaning and is not folded.
	if strings.Contains(body, "preferred_workbench") {
		t.Errorf("the fold invented a repository preference from a per-checkout record:\n%s", body)
	}

	if _, err := os.Stat(fx.runtimePath); !os.IsNotExist(err) {
		t.Errorf("the record is still at the path pop reads: %v", err)
	}
	if _, err := os.Stat(fx.runtimePath + retiredRuntimeFoldedSuffix); err != nil {
		t.Errorf("the retired copy is not on disk as its own rollback: %v", err)
	}
}

// TestPopWritesOneConfigFile is the claim the fold exists to make true: with
// every scriptable writer run and config loaded, the data dir holds the override
// layer and nothing else pop wrote. The retired copy is not one of pop's config
// files — nothing reads it, and pop only ever renames it there.
func TestPopWritesOneConfigFile(t *testing.T) {
	fx := newPopRepoFixture(t)
	writeFixtureConfig(t, fx, statedWritersConfig)

	if _, err := SetRepoSettingWith(fx.d, fx.main, "turn_cap", "40"); err != nil {
		t.Fatalf("config repo set: %v", err)
	}
	if err := PersistRepoTrunkWith(fx.d, fx.main); err != nil {
		t.Fatalf("--trunk: %v", err)
	}
	if err := StatePreferredWorkbenchWith(fx.d, fx.feature, "minimal"); err != nil {
		t.Fatalf("workbench prefer: %v", err)
	}
	if err := DeclineIntegrationSkillsWith(fx.d, IntegrationSkillPane); err != nil {
		t.Fatalf("--no-pane-skills: %v", err)
	}
	if _, err := LoadWith(fx.d, fx.configPath); err != nil {
		t.Fatalf("LoadWith: %v", err)
	}

	entries, err := os.ReadDir(filepath.Dir(fx.overridePath))
	if err != nil {
		t.Fatal(err)
	}
	var written []string
	for _, entry := range entries {
		written = append(written, entry.Name())
	}
	if !reflect.DeepEqual(written, []string{"config.override.toml"}) {
		t.Errorf("pop's data dir holds %v, want config.override.toml alone", written)
	}
}

// TestTheFoldRunsOnlyOnce is the rank inversion's safety catch: a value the
// human removed after the fold must stay removed, which it can only do if a
// second load never folds again.
func TestTheFoldRunsOnlyOnce(t *testing.T) {
	fx := newPopRepoFixture(t)
	writeRuntimeFile(t, fx.runtimePath, "[repo_settings."+quotedPath(fx.identity)+"]\nturn_cap = 40\n")

	folded, err := foldRetiredRuntimeRecordWith(fx.d)
	if err != nil || !folded {
		t.Fatalf("first fold = %v, %v; want it to run", folded, err)
	}
	if err := DeleteRepoOverrideValueWith(fx.d, fx.main, "turn_cap"); err != nil {
		t.Fatalf("remove the override: %v", err)
	}

	folded, err = foldRetiredRuntimeRecordWith(fx.d)
	if err != nil || folded {
		t.Fatalf("second fold = %v, %v; want it skipped", folded, err)
	}
	repoCfg, err := (&Config{}).ResolveRepoConfig(fx.d, fx.feature)
	if err != nil {
		t.Fatal(err)
	}
	if repoCfg.TurnCap != 0 {
		t.Errorf("turn cap = %d, want the removed value to stay removed", repoCfg.TurnCap)
	}
}

// TestFoldLeavesWhatIsAlreadyStated covers the interrupted-fold case: a value
// stated through pop is newer than one pop recorded for itself, so re-running
// the fold over it changes nothing.
func TestFoldLeavesWhatIsAlreadyStated(t *testing.T) {
	fx := newPopRepoFixture(t)
	if _, err := SetRepoSettingWith(fx.d, fx.main, "turn_cap", "5"); err != nil {
		t.Fatalf("state a cap: %v", err)
	}
	if err := SetOverrideValueWith(fx.d, "integrations.skills", []any{"pane"}); err != nil {
		t.Fatalf("state a skills list: %v", err)
	}
	writeRuntimeFile(t, fx.runtimePath, `
[integrations]
skills = ["tasks"]

[repo_settings.`+quotedPath(fx.identity)+`]
turn_cap = 40
`)

	if _, err := foldRetiredRuntimeRecordWith(fx.d); err != nil {
		t.Fatalf("fold: %v", err)
	}
	repoCfg, err := (&Config{}).ResolveRepoConfig(fx.d, fx.feature)
	if err != nil {
		t.Fatal(err)
	}
	if repoCfg.TurnCap != 5 {
		t.Errorf("turn cap = %d, want the stated 5 kept over the recorded 40", repoCfg.TurnCap)
	}
	value, _, err := OverrideValueWith(fx.d, "integrations.skills")
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(value, []any{"pane"}) {
		t.Errorf("skills = %#v, want the stated list kept over the recorded one", value)
	}
}

// TestFoldRefusesWhatTheGateRefuses keeps the one rule the layer has: a file pop
// wrote itself is never the source of a finding. A record holding a value the
// gate refuses folds nothing of it, retires anyway, and says what it dropped —
// the retired copy being where that value survives.
func TestFoldRefusesWhatTheGateRefuses(t *testing.T) {
	fx := newPopRepoFixture(t)
	writeRuntimeFile(t, fx.runtimePath, `
[integrations]
skills = ["bogus"]

[repo_settings.`+quotedPath(fx.identity)+`]
turn_cap = 40
`)

	folded, err := foldRetiredRuntimeRecordWith(fx.d)
	if !folded {
		t.Fatal("the record was not retired")
	}
	if err == nil || !strings.Contains(err.Error(), "integrations.skills") {
		t.Errorf("fold error = %v, want it to name the refused key", err)
	}
	body := readFile(t, fx.overridePath)
	if strings.Contains(body, "bogus") {
		t.Errorf("the refused value reached the layer:\n%s", body)
	}
	if !strings.Contains(body, "turn_cap = 40") {
		t.Errorf("one refusal stranded the rest of the record:\n%s", body)
	}
}

// TestFoldLeavesAnUnreadableRecordAlone: a record pop cannot parse is one it
// cannot fold, and retiring it would turn an unreadable file into an invisible
// one.
func TestFoldLeavesAnUnreadableRecordAlone(t *testing.T) {
	fx := newPopRepoFixture(t)
	writeRuntimeFile(t, fx.runtimePath, "this is not TOML =\n")

	folded, err := foldRetiredRuntimeRecordWith(fx.d)
	if folded || err == nil {
		t.Fatalf("fold = %v, %v; want it refused", folded, err)
	}
	if _, err := os.Stat(fx.runtimePath); err != nil {
		t.Errorf("the unreadable record was retired anyway: %v", err)
	}
}
