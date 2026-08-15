package config

import (
	"os"
	"reflect"
	"strings"
	"testing"
)

// The four writers that record what a human wants — the repo setter, `--trunk`,
// the component decline and the workbench preference — share one destination and
// one gate with the Config dashboard (ADR-0212 decisions 5 and 6). These tests
// drive all four through the library entry points their commands call.

// statedWritersConfig is a config declaring the one Workbench the preference
// tests name, so a stated preference resolves rather than being warned away.
const statedWritersConfig = `
[[workbenches]]
name = "minimal"

[[workbenches.windows]]
name = "work"

[workbenches.windows.layout]
name = "shell"
command = "zsh"
`

// TestEveryWriterOfIntentLandsInTheOverrideLayer is the slice in one run: each
// of the four states its value, all four land in the one file at the scope the
// value belongs to, and nothing is written where pop used to record guesses.
func TestEveryWriterOfIntentLandsInTheOverrideLayer(t *testing.T) {
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

	body := readFile(t, fx.overridePath)
	for _, want := range []string{
		`skills = ["tasks"]`, // global scope: the decline
		"turn_cap = 40",      // repository scope: the bound
		"trunk = ",           // repository scope: the fork base
		`preferred_workbench = "minimal"`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("the override layer does not hold %q:\n%s", want, body)
		}
	}
	// One block per repository, whichever worktree stated the value.
	if n := strings.Count(body, "[repo."); n != 1 {
		t.Errorf("the layer holds %d repository blocks, want 1:\n%s", n, body)
	}
	if _, err := os.Stat(fx.runtimePath); !os.IsNotExist(err) {
		t.Errorf("a writer still wrote the retired record at %s", fx.runtimePath)
	}
}

// TestStatedValuesReadBackThroughEffectiveConfigAndTheDashboard is the other
// half: what the writers stated is what resolution answers with, and what the
// Config dashboard shows as in force with the layer that produced it.
func TestStatedValuesReadBackThroughEffectiveConfigAndTheDashboard(t *testing.T) {
	fx := newPopRepoFixture(t)
	writeFixtureConfig(t, fx, statedWritersConfig)

	if _, err := SetRepoSettingWith(fx.d, fx.main, "turn_cap", "40"); err != nil {
		t.Fatalf("config repo set: %v", err)
	}
	if err := PersistRepoTrunkWith(fx.d, fx.main); err != nil {
		t.Fatalf("--trunk: %v", err)
	}
	if err := StatePreferredWorkbenchWith(fx.d, fx.main, "minimal"); err != nil {
		t.Fatalf("workbench prefer: %v", err)
	}
	if err := DeclineIntegrationSkillsWith(fx.d, IntegrationSkillPane); err != nil {
		t.Fatalf("--no-pane-skills: %v", err)
	}

	cfg, err := LoadWith(fx.d, fx.configPath)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	skills, err := cfg.IntegrationsSkills()
	if err != nil {
		t.Fatalf("IntegrationsSkills: %v", err)
	}
	if !reflect.DeepEqual(skills, []string{"tasks"}) {
		t.Errorf("skills = %#v, want the declined pane skill gone", skills)
	}

	// The sibling worktree reads every repository-scoped value, one entry serving
	// the whole repository.
	repoCfg, err := cfg.ResolveRepoConfig(fx.d, fx.feature)
	if err != nil {
		t.Fatal(err)
	}
	if repoCfg.TurnCap != 40 {
		t.Errorf("TurnCap = %d, want the stated 40", repoCfg.TurnCap)
	}
	if repoCfg.Trunk != canonicalPath(fx.d, fx.main) {
		t.Errorf("Trunk = %q, want the stated %s", repoCfg.Trunk, fx.main)
	}
	if name, _ := cfg.ResolvePreferredWorkbench(fx.d, fx.feature); name != "minimal" {
		t.Errorf("preferred workbench = %q, want the stated minimal", name)
	}

	// The dashboard says the same thing, and names the layer that produced it.
	views, err := RepoOverrideKeyViews(fx.d, fx.configPath, fx.feature)
	if err != nil {
		t.Fatalf("RepoOverrideKeyViews: %v", err)
	}
	want := map[string]string{
		"repo.turn_cap":            "repo.turn_cap = 40",
		"repo.preferred_workbench": `repo.preferred_workbench = "minimal"`,
	}
	for _, view := range views {
		expected, ok := want[view.Key]
		if !ok {
			continue
		}
		delete(want, view.Key)
		if !view.Overridden {
			t.Errorf("%s is not marked as overridden", view.Key)
		}
		if view.EffectiveTOML != expected {
			t.Errorf("%s renders %q, want %q", view.Key, view.EffectiveTOML, expected)
		}
		if view.Layer != OverrideLayerOverride {
			t.Errorf("%s came from %v, want the override layer", view.Key, view.Layer)
		}
	}
	if len(want) > 0 {
		t.Errorf("the dashboard offers no row for %v", want)
	}

	globals, err := OverrideKeyViewsWith(fx.d, fx.configPath)
	if err != nil {
		t.Fatalf("OverrideKeyViewsWith: %v", err)
	}
	for _, view := range globals {
		if view.Key != integrationSkillsKey {
			continue
		}
		if !view.Overridden || view.Layer != OverrideLayerOverride {
			t.Errorf("%s = %+v, want it marked as an override", view.Key, view)
		}
	}
}

// TestStatedPreferenceIsThreeValued keeps `--none` meaning what it says at the
// key's new home: an entry stated as "" is an explicit none that stops the walk,
// which is a different state from no entry at all.
func TestStatedPreferenceIsThreeValued(t *testing.T) {
	fx := newPopRepoFixture(t)
	writeFixtureConfig(t, fx, statedWritersConfig+"\n[repo."+quotedPath(fx.main)+"]\npreferred_workbench = \"minimal\"\n")

	cfg, err := LoadWith(fx.d, fx.configPath)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if name, _ := cfg.ResolvePreferredWorkbench(fx.d, fx.feature); name != "minimal" {
		t.Fatalf("declared preference = %q, want minimal before anything is stated", name)
	}

	if err := StatePreferredWorkbenchWith(fx.d, fx.feature, ""); err != nil {
		t.Fatalf("workbench prefer --none: %v", err)
	}
	if name, _ := cfg.ResolvePreferredWorkbench(fx.d, fx.feature); name != "" {
		t.Errorf("preferred workbench = %q, want the stated none to win", name)
	}
	if name, stated, err := StatedPreferredWorkbenchWith(fx.d, fx.main); err != nil || !stated || name != "" {
		t.Errorf("stated entry = (%q, %v, %v), want an explicit none at every worktree", name, stated, err)
	}

	if err := ClearPreferredWorkbenchWith(fx.d, fx.feature); err != nil {
		t.Fatalf("workbench prefer --clear: %v", err)
	}
	if _, stated, _ := StatedPreferredWorkbenchWith(fx.d, fx.feature); stated {
		t.Error("the entry survived a clear")
	}
	if name, _ := cfg.ResolvePreferredWorkbench(fx.d, fx.feature); name != "minimal" {
		t.Errorf("preferred workbench = %q, want the declaration back after a clear", name)
	}
}

// TestWritersOfIntentPassTheSameGateAsTheDashboard proves the refusals are the
// editor's, not each writer's own: a value that would produce a finding is
// refused before anything reaches the disk, at both scopes.
func TestWritersOfIntentPassTheSameGateAsTheDashboard(t *testing.T) {
	fx := newPopRepoFixture(t)
	writeFixtureConfig(t, fx, statedWritersConfig)

	// The gate's own answer, and the one the dashboard's editor shows.
	if problem := OverrideValueProblem(integrationSkillsKey, []any{"nope"}); problem == "" {
		t.Error("an unknown skill alias passed the global gate")
	}
	if problem := RepoOverrideValueProblem("repo.turn_cap", "forty"); problem == "" {
		t.Error("a turn cap of \"forty\" passed the repository gate")
	}

	// The same answer on the write side, which is what a writer with no editor
	// to re-open runs into.
	if err := SetOverrideValueWith(fx.d, integrationSkillsKey, []any{"nope"}); err == nil {
		t.Error("an unknown skill alias was written")
	}
	if _, err := SetRepoOverrideValueWith(fx.d, fx.main, "turn_cap", "forty"); err == nil {
		t.Error("a turn cap of \"forty\" was written")
	}
	if _, err := SetRepoOverrideValueWith(fx.d, fx.main, "trunk", 7); err == nil {
		t.Error("a trunk that is not a path was written")
	}
	if _, err := os.Stat(fx.overridePath); !os.IsNotExist(err) {
		t.Errorf("a refused write created %s:\n%s", fx.overridePath, readFile(t, fx.overridePath))
	}

	// A decline computed from a config pop could not read back is refused too:
	// the writer states a whole list, and the list has to be one that loads.
	writeFixtureConfig(t, fx, "[integrations]\nskills = [\"nope\", \"pane\"]\n")
	if err := DeclineIntegrationSkillsWith(fx.d, IntegrationSkillPane); err == nil {
		t.Error("a decline over an unreadable skills list was accepted")
	}
	if _, err := os.Stat(fx.overridePath); !os.IsNotExist(err) {
		t.Errorf("the refused decline created %s", fx.overridePath)
	}
}

func writeFixtureConfig(t *testing.T, fx popRepoFixture, body string) {
	t.Helper()
	if err := os.WriteFile(fx.configPath, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(data)
}

func quotedPath(path string) string { return `"` + path + `"` }
