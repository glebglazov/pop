package config

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/glebglazov/pop/internal/deps"
)

// quoted spells a path as the TOML key of a [repo."<id>"] block.
func quoted(path string) string { return strconv.Quote(path) }

// overrideScopeFixture lays out one repository with two worktrees beside a
// hand-authored config.toml and pop's own data dir, so a test can state the same
// key in every place that can hold it and ask what is in force at a checkout.
type overrideScopeFixture struct {
	d            *Deps
	main         string
	feature      string
	identity     string
	elsewhere    string // a checkout of another repository entirely
	userPath     string
	overridePath string
}

func newOverrideScopeFixture(t *testing.T) *overrideScopeFixture {
	t.Helper()
	root := t.TempDir()
	dataDir := filepath.Join(root, "data")
	real := deps.NewRealFileSystem()
	d := &Deps{FS: &deps.MockFileSystem{
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
	bareRoot := filepath.Join(root, "repo")
	f := &overrideScopeFixture{
		d:            d,
		main:         filepath.Join(bareRoot, "main"),
		feature:      filepath.Join(bareRoot, "feature"),
		elsewhere:    filepath.Join(root, "other"),
		userPath:     filepath.Join(root, "config", "config.toml"),
		overridePath: filepath.Join(dataDir, "pop", "config.override.toml"),
	}
	for _, dir := range []string{filepath.Join(bareRoot, ".bare"), f.main, f.feature, f.elsewhere} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	f.identity = repoIdentity(d, f.main)
	return f
}

// commit writes a .pop/config.toml at dir declaring one named blueprint and
// preferring it — the team's committed statement, the rung an override must beat.
func (f *overrideScopeFixture) commit(t *testing.T, dir, tag string) {
	t.Helper()
	body := "preferred_workbench = \"" + tag + "\"\n" +
		"[[workbenches]]\nname = \"" + tag + "\"\n" +
		"windows = [{name = \"main\", layout = {name = \"editor\", command = \"vim\"}}]\n"
	if err := os.MkdirAll(filepath.Join(dir, ".pop"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".pop", "config.toml"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func (f *overrideScopeFixture) writeOverride(t *testing.T, body string) {
	t.Helper()
	writeConfigFile(t, f.overridePath, body)
}

// namedWorkbenches is a config whose global library holds every name these tests
// prefer, so a preferred name always resolves to a real Workbench and the chain
// never falls through for a reason the test did not intend.
func namedWorkbenches(names ...string) *Config {
	cfg := &Config{}
	for _, name := range names {
		cfg.Workbenches = append(cfg.Workbenches, Workbench{Name: name})
	}
	return cfg
}

// blueprintTag reads the tag the winning blueprint carries. The tag rides in
// before_apply so the source of a name defined by several layers is identifiable
// after the union merges them.
func blueprintTag(t *testing.T, wbs []Workbench) string {
	t.Helper()
	for _, wb := range wbs {
		if wb.Name != "shared" {
			continue
		}
		if len(wb.BeforeApply) != 1 {
			t.Fatalf("blueprint %q carries %v, want one tag", wb.Name, wb.BeforeApply)
		}
		return wb.BeforeApply[0]
	}
	t.Fatalf("no 'shared' blueprint in %+v", wbs)
	return ""
}

// TestRepoScopedOverrideAppliesToOneRepository pins the keying of ADR-0212
// decision 7: an override filed against a repository is filed under its
// Repository identity, so every worktree of that repository reads the one entry
// — and a checkout of any other repository reads none of it.
func TestRepoScopedOverrideAppliesToOneRepository(t *testing.T) {
	f := newOverrideScopeFixture(t)
	f.writeOverride(t, `
[repo.`+quoted(f.identity)+`]
preferred_workbench = "solo"
turn_cap = 7
`)
	cfg := namedWorkbenches("solo")

	for _, checkout := range []string{f.main, f.feature, f.identity} {
		if name, _ := cfg.ResolvePreferredWorkbench(f.d, checkout); name != "solo" {
			t.Errorf("preferred workbench at %s = %q, want solo (one entry per repository)", checkout, name)
		}
		repoCfg, err := cfg.ResolveRepoConfig(f.d, checkout)
		if err != nil {
			t.Fatalf("%s: %v", checkout, err)
		}
		if repoCfg.TurnCap != 7 {
			t.Errorf("turn cap at %s = %d, want 7", checkout, repoCfg.TurnCap)
		}
	}

	if name, _ := cfg.ResolvePreferredWorkbench(f.d, f.elsewhere); name != "" {
		t.Errorf("preferred workbench in another repository = %q, want none", name)
	}
	repoCfg, err := cfg.ResolveRepoConfig(f.d, f.elsewhere)
	if err != nil {
		t.Fatal(err)
	}
	if repoCfg.TurnCap != 0 {
		t.Errorf("turn cap in another repository = %d, want none", repoCfg.TurnCap)
	}
}

// TestRepoScopedOverrideBeatsGlobalOverride pins the specificity rule inside the
// layer (ADR-0212 decision 2): both scopes state the same key, and the
// repository — being the more specific one — is what is in force there, while a
// repository the layer says nothing about still reads the global statement.
func TestRepoScopedOverrideBeatsGlobalOverride(t *testing.T) {
	f := newOverrideScopeFixture(t)
	f.writeOverride(t, `
[[workbenches]]
name = "shared"
before_apply = ["global-override"]

[repo.`+quoted(f.identity)+`]
workbenches = [{ name = "shared", before_apply = ["repo-override"] }]
`)
	cfg := &Config{}

	unioned, _ := cfg.ResolveWorkbenchesWith(f.d, f.feature)
	if got := blueprintTag(t, unioned); got != "repo-override" {
		t.Errorf("in force at a worktree = %q, want repo-override (the more specific scope)", got)
	}
	repoCfg, err := cfg.ResolveRepoConfig(f.d, f.feature)
	if err != nil {
		t.Fatal(err)
	}
	if got := blueprintTag(t, repoCfg.Workbenches); got != "repo-override" {
		t.Errorf("ResolveRepoConfig says %q, ResolveWorkbenchesWith says repo-override; one answer", got)
	}

	elsewhere, _ := cfg.ResolveWorkbenchesWith(f.d, f.elsewhere)
	if got := blueprintTag(t, elsewhere); got != "global-override" {
		t.Errorf("in force elsewhere = %q, want global-override", got)
	}
}

// TestOverrideBeatsEveryLadderLayer is the point of decision 2: the override is
// laid over whatever the ladder resolved, so it wins whatever the ladder's own
// ordering was. Every rung that can hold these keys states something else — the
// global library, the committed .pop/config.toml, and the checkout-keyed
// [repo."<path>"] block that tops the ladder — and the override still answers.
func TestOverrideBeatsEveryLadderLayer(t *testing.T) {
	f := newOverrideScopeFixture(t)
	f.commit(t, f.main, "committed")
	f.writeOverride(t, `
[repo.`+quoted(f.identity)+`]
preferred_workbench = "overridden"
workbenches = [{ name = "shared", before_apply = ["override"] }]
turn_cap = 7
`)
	declared := 40
	cfg := namedWorkbenches("overridden", "committed", "declared")
	cfg.Workbenches = append(cfg.Workbenches, Workbench{
		Name: "shared", BeforeApply: []string{"global-declaration"},
	})
	cfg.Repo = map[string]RepoOverrideConfig{f.main: {
		RepoScopeConfig: RepoScopeConfig{
			PreferredWorkbench: "declared",
			Workbenches:        []Workbench{{Name: "shared", BeforeApply: []string{"block"}}},
		},
		TurnCap: &declared,
	}}

	if name, warnings := cfg.ResolvePreferredWorkbench(f.d, f.main); name != "overridden" {
		t.Errorf("preferred workbench = %q (warnings %v), want overridden", name, warnings)
	}
	unioned, _ := cfg.ResolveWorkbenchesWith(f.d, f.main)
	if got := blueprintTag(t, unioned); got != "override" {
		t.Errorf("blueprint in force = %q, want override", got)
	}
	repoCfg, err := cfg.ResolveRepoConfig(f.d, f.main)
	if err != nil {
		t.Fatal(err)
	}
	if repoCfg.TurnCap != 7 {
		t.Errorf("turn cap = %d, want 7 (the override beats the block's 40)", repoCfg.TurnCap)
	}

	// The same answer through the settings read, which names its layer: a reader
	// asking where the number came from must not be told the block it beats.
	settings, err := cfg.ResolveRepoSettings(f.d, f.main)
	if err != nil {
		t.Fatal(err)
	}
	if len(settings) == 0 || settings[0].Key != "turn_cap" {
		t.Fatalf("settings = %+v, want turn_cap first", settings)
	}
	if settings[0].Value != "7" || settings[0].Source != RepoSettingOverrideLayer {
		t.Errorf("turn_cap reads %+v, want 7 from %s", settings[0], RepoSettingOverrideLayer)
	}
}

// TestOverrideFileCarriesBothScopesInOneDocument pins the file shape of decision
// 7: one document a reader of config.toml can read on sight — global keys at the
// top, per-repository blocks below — written and removed through the same API,
// with the repository half staying out of the global merge so it can never be
// mistaken for a declaration of the ladder it outranks.
func TestOverrideFileCarriesBothScopesInOneDocument(t *testing.T) {
	f := newOverrideScopeFixture(t)
	writeConfigFile(t, f.userPath, "[work.implement]\nagents = [\"claude\"]\n")

	if err := SetOverrideValueWith(f.d, "work.implement.agents", []any{"codex"}); err != nil {
		t.Fatalf("SetOverrideValueWith: %v", err)
	}
	identity, err := SetRepoOverrideValueWith(f.d, f.feature, "turn_cap", 7)
	if err != nil {
		t.Fatalf("SetRepoOverrideValueWith: %v", err)
	}
	if identity != f.identity {
		t.Errorf("filed under %q, want the repository %q", identity, f.identity)
	}

	body, err := os.ReadFile(f.overridePath)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"[work.implement]", `agents = ["codex"]`, f.identity, "turn_cap = 7"} {
		if !strings.Contains(string(body), want) {
			t.Errorf("override file does not carry %q, got:\n%s", want, body)
		}
	}

	cfg, err := LoadWith(f.d, f.userPath)
	if err != nil {
		t.Fatalf("LoadWith: %v", err)
	}
	if got := cfg.ImplementAgents(); len(got) != 1 || got[0] != "codex" {
		t.Errorf("ImplementAgents() = %v, want the global override", got)
	}
	if len(cfg.Repo) != 0 {
		t.Errorf("merged config carries %v as [repo] blocks; the layer's repository half is not a declaration", cfg.Repo)
	}
	repoCfg, err := cfg.ResolveRepoConfig(f.d, f.main)
	if err != nil {
		t.Fatal(err)
	}
	if repoCfg.TurnCap != 7 {
		t.Errorf("turn cap = %d, want the 7 written from the sibling worktree", repoCfg.TurnCap)
	}

	value, ok, err := RepoOverrideValueWith(f.d, f.main, "turn_cap")
	if err != nil || !ok || value != int64(7) {
		t.Errorf("RepoOverrideValueWith = (%v, %v, %v), want 7 read back from any worktree", value, ok, err)
	}
	if err := DeleteRepoOverrideValueWith(f.d, f.main, "turn_cap"); err != nil {
		t.Fatalf("DeleteRepoOverrideValueWith: %v", err)
	}
	if _, ok, _ := RepoOverrideValueWith(f.d, f.main, "turn_cap"); ok {
		t.Error("the repository entry survived its removal")
	}
	after, err := os.ReadFile(f.overridePath)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(after), "[repo") {
		t.Errorf("the emptied block was left behind, got:\n%s", after)
	}
	if !strings.Contains(string(after), `agents = ["codex"]`) {
		t.Errorf("the global half was disturbed by a repository removal, got:\n%s", after)
	}
}

// TestSetRepoOverrideValueRefusesWhatTheRepoSurfaceCannotHold keeps the file pop
// writes from becoming the source of a finding: a key with no repository home
// and a value the key cannot hold are both refused before anything is written.
func TestSetRepoOverrideValueRefusesWhatTheRepoSurfaceCannotHold(t *testing.T) {
	f := newOverrideScopeFixture(t)

	if _, err := SetRepoOverrideValueWith(f.d, f.main, "projects", []any{"/x"}); err == nil {
		t.Error("a global-only key was accepted at repository scope")
	}
	if _, err := SetRepoOverrideValueWith(f.d, f.main, "turn_cap", "forty"); err == nil {
		t.Error("a turn cap of \"forty\" was accepted")
	}
	if _, err := os.Stat(f.overridePath); !os.IsNotExist(err) {
		t.Errorf("a refused write still created %s", f.overridePath)
	}
}

// TestEffectiveConfigReportsTheOverride pins the mirror on both scopes: the
// current-repo section answers with the override in force at the checkout, and
// the printed [repo."<path>"] block carries the overridden value rather than the
// declaration it beats.
func TestEffectiveConfigReportsTheOverride(t *testing.T) {
	f := newOverrideScopeFixture(t)
	f.commit(t, f.main, "committed")
	f.writeOverride(t, `
[repo.`+quoted(f.identity)+`]
preferred_workbench = "overridden"
turn_cap = 7
`)
	declared := 40
	cfg := namedWorkbenches("overridden")
	cfg.Repo = map[string]RepoOverrideConfig{f.main: {
		RepoScopeConfig: RepoScopeConfig{PreferredWorkbench: "committed"},
		TurnCap:         &declared,
	}}

	out, err := renderEffectiveTOML(f.d, cfg, &ResolvedTrunk{Path: f.main, Checkout: f.main})
	if err != nil {
		t.Fatalf("renderEffectiveTOML: %v", err)
	}
	for _, want := range []string{`preferred_workbench = "overridden"`, "turn_cap = 7"} {
		if !strings.Contains(out, want) {
			t.Errorf("the mirror does not report %q, got:\n%s", want, out)
		}
	}
	if strings.Contains(out, "turn_cap = 40") {
		t.Errorf("the mirror still prints the beaten declaration, got:\n%s", out)
	}
}
