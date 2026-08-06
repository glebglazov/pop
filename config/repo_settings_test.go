package config

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// popRepoFixture lays out a bare repo with two worktrees and returns deps whose
// runtime file lives in an isolated data dir, plus the paths this slice cares
// about: the two checkouts, the repository they share, and the two files whose
// roles must not blur — the runtime file pop writes and the config.toml it must
// not touch.
type popRepoFixture struct {
	d           *Deps
	main        string
	feature     string
	identity    string
	runtimePath string
	configPath  string
}

func newPopRepoFixture(t *testing.T) popRepoFixture {
	t.Helper()
	d, runtimePath := runtimeTestDeps(t)
	bareRoot := t.TempDir()
	main := filepath.Join(bareRoot, "main")
	feature := filepath.Join(bareRoot, "feature")
	for _, dir := range []string{filepath.Join(bareRoot, ".bare"), main, feature} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	configPath := DefaultConfigPathWith(d)
	if err := os.MkdirAll(filepath.Dir(configPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(configPath, []byte("[worktree]\nauto_open = true\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return popRepoFixture{
		d: d, main: main, feature: feature, identity: bareRoot,
		runtimePath: runtimePath, configPath: configPath,
	}
}

// TestRepoSettableKeysComeFromTheSchema pins ADR-0191 decision 5: the settable
// key set is generated from RepoRuntimeConfig, and the per-key behaviour that
// reflection cannot supply is pinned against it in both directions — a schema
// key with no entry is unsettable in practice, an entry with no schema key is a
// key the config would never read back.
func TestRepoSettableKeysComeFromTheSchema(t *testing.T) {
	t.Parallel()
	keys := RepoSettableKeys()
	if len(keys) == 0 {
		t.Fatal("no settable repo keys reflected from RepoRuntimeConfig")
	}
	declared := make(map[string]bool, len(keys))
	for _, key := range keys {
		declared[key] = true
		if _, ok := repoSettingKinds[key]; !ok {
			t.Errorf("schema key %q has no repoSettingKinds entry, so pop could not read or write it", key)
		}
		// A settable key must also be a key the config itself accepts centrally,
		// or pop would write a value nothing resolves.
		if _, ok := scopeField(reflect.TypeOf(RepoOverrideConfig{}), key); !ok {
			t.Errorf("settable key %q is not a [repo.\"<path>\"] key of RepoOverrideConfig", key)
		}
	}
	for key := range repoSettingKinds {
		if !declared[key] {
			t.Errorf("%q is declared settable but is not a field of RepoRuntimeConfig", key)
		}
	}
	if !declared["turn_cap"] {
		t.Errorf("turn_cap is not settable; settable keys are %v", keys)
	}
}

// TestRepoSettingWrittenOnceServesEveryWorktree is the whole point of the
// identity keying (ADR-0191 decision 4): the value is set from one worktree and
// read from another, and it lands in pop's runtime state with the hand-authored
// config.toml untouched (decision 3).
func TestRepoSettingWrittenOnceServesEveryWorktree(t *testing.T) {
	fx := newPopRepoFixture(t)
	before, err := os.ReadFile(fx.configPath)
	if err != nil {
		t.Fatal(err)
	}

	identity, err := SetRepoSettingWith(fx.d, fx.main, "turn_cap", "40")
	if err != nil {
		t.Fatalf("SetRepoSettingWith: %v", err)
	}
	if identity != fx.identity {
		t.Errorf("filed under %q, want the repository %q", identity, fx.identity)
	}

	cfg := &Config{}
	for _, checkout := range []string{fx.main, fx.feature, fx.identity} {
		repoCfg, err := cfg.ResolveRepoConfig(fx.d, checkout)
		if err != nil {
			t.Fatalf("%s: %v", checkout, err)
		}
		if repoCfg.TurnCap != 40 {
			t.Errorf("TurnCap at %s = %d, want 40 (one value per repository)", checkout, repoCfg.TurnCap)
		}
	}

	runtimeBody, err := os.ReadFile(fx.runtimePath)
	if err != nil {
		t.Fatalf("runtime file missing: %v", err)
	}
	if !strings.Contains(string(runtimeBody), "[repo_settings") ||
		!strings.Contains(string(runtimeBody), "turn_cap = 40") {
		t.Errorf("runtime file does not hold the value:\n%s", runtimeBody)
	}
	after, err := os.ReadFile(fx.configPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != string(before) {
		t.Errorf("hand-authored config.toml was modified:\n%s", after)
	}
}

// TestHandAuthoredRepoBlockBeatsPopWrittenSetting pins the ordering that makes
// this layer safe to write into: what a human wrote always wins, including when
// he writes the number back to nothing, and the read says which layer answered.
func TestHandAuthoredRepoBlockBeatsPopWrittenSetting(t *testing.T) {
	fx := newPopRepoFixture(t)
	if _, err := SetRepoSettingWith(fx.d, fx.main, "turn_cap", "5"); err != nil {
		t.Fatalf("SetRepoSettingWith: %v", err)
	}

	popWritten := &Config{}
	repoCfg, err := popWritten.ResolveRepoConfig(fx.d, fx.feature)
	if err != nil {
		t.Fatal(err)
	}
	if repoCfg.TurnCap != 5 {
		t.Errorf("TurnCap = %d, want the 5 pop wrote", repoCfg.TurnCap)
	}
	settings, err := popWritten.ResolveRepoSettings(fx.d, fx.feature)
	if err != nil {
		t.Fatal(err)
	}
	got := settingByKey(t, settings, "turn_cap")
	if got.Value != "5" || got.Source != RepoSettingRuntime || got.Locus != fx.runtimePath {
		t.Errorf("pop-written read = %+v, want 5 from %s at %s", got, RepoSettingRuntime, fx.runtimePath)
	}

	handAuthored := &Config{Repo: map[string]RepoOverrideConfig{fx.main: {TurnCap: intPtr(40)}}}
	repoCfg, err = handAuthored.ResolveRepoConfig(fx.d, fx.feature)
	if err != nil {
		t.Fatal(err)
	}
	if repoCfg.TurnCap != 40 {
		t.Errorf("TurnCap = %d, want the hand-authored 40", repoCfg.TurnCap)
	}
	settings, err = handAuthored.ResolveRepoSettings(fx.d, fx.feature)
	if err != nil {
		t.Fatal(err)
	}
	got = settingByKey(t, settings, "turn_cap")
	if got.Value != "40" || got.Source != RepoSettingOverride || !strings.Contains(got.Locus, fx.main) {
		t.Errorf("hand-authored read = %+v, want 40 from %s naming %s", got, RepoSettingOverride, fx.main)
	}

	// A hand-written non-positive number is a deliberate "no bound", and it must
	// beat the bound pop recorded like any other hand-authored value.
	takenBack := &Config{Repo: map[string]RepoOverrideConfig{fx.main: {TurnCap: intPtr(0)}}}
	repoCfg, err = takenBack.ResolveRepoConfig(fx.d, fx.feature)
	if err != nil {
		t.Fatal(err)
	}
	if repoCfg.TurnCap != 0 {
		t.Errorf("TurnCap = %d, want 0 (a hand-written 0 takes the bound back)", repoCfg.TurnCap)
	}
}

// TestSetRepoSettingRefusesWhatItCannotSet proves the refusals write nothing: an
// unknown key names the keys that exist, and an unreadable value names the key.
func TestSetRepoSettingRefusesWhatItCannotSet(t *testing.T) {
	fx := newPopRepoFixture(t)

	// trunk is a real [repo."<path>"] key that pop deliberately does not set here
	// — it is per-checkout topology, not a repository-wide setting.
	_, err := SetRepoSettingWith(fx.d, fx.main, "trunk", "true")
	if err == nil {
		t.Fatal("setting trunk was accepted; only the reflected keys are settable")
	}
	if !strings.Contains(err.Error(), "trunk") || !strings.Contains(err.Error(), "turn_cap") {
		t.Errorf("refusal %q should name the key and list the keys that exist", err)
	}

	if _, err := SetRepoSettingWith(fx.d, fx.main, "turn_cap", "lots"); err == nil {
		t.Fatal("a non-numeric turn cap was accepted")
	} else if !strings.Contains(err.Error(), "turn_cap") {
		t.Errorf("refusal %q should name the key", err)
	}

	if _, err := os.Stat(fx.runtimePath); !os.IsNotExist(err) {
		body, _ := os.ReadFile(fx.runtimePath)
		t.Errorf("a refused set still wrote runtime state:\n%s", body)
	}
}

// TestSetRepoSettingKeepsUnrelatedRuntimeState proves the layer is additive: an
// existing runtime file keeps its other sections, and a second repository's
// entry is not disturbed by writing this one.
func TestSetRepoSettingKeepsUnrelatedRuntimeState(t *testing.T) {
	fx := newPopRepoFixture(t)
	writeRuntimeFile(t, fx.runtimePath, "[integrations]\nskills = [\"tasks\"]\n\n[repo_settings.\"/srv/other\"]\nturn_cap = 3\n")

	if _, err := SetRepoSettingWith(fx.d, fx.feature, "turn_cap", "12"); err != nil {
		t.Fatalf("SetRepoSettingWith: %v", err)
	}
	body, err := os.ReadFile(fx.runtimePath)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"skills = [\"tasks\"]", "/srv/other", "turn_cap = 3", "turn_cap = 12"} {
		if !strings.Contains(string(body), want) {
			t.Errorf("runtime file lost %q:\n%s", want, body)
		}
	}
}

func settingByKey(t *testing.T, settings []RepoSetting, key string) RepoSetting {
	t.Helper()
	for _, setting := range settings {
		if setting.Key == key {
			return setting
		}
	}
	t.Fatalf("no %q among %+v", key, settings)
	return RepoSetting{}
}
