package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/BurntSushi/toml"
)

// trunkPtr states a trunk the way a [repo] block holds it: a path value.
func trunkPtr(path string) *TrunkPath {
	t := TrunkPath(path)
	return &t
}

// legacyTrunkPtr is the retired `trunk = true` as it decodes: a marker holding no
// path, which reads back as the key of the block that carries it.
func legacyTrunkPtr() *TrunkPath {
	t := trunkIsBlockKey
	return &t
}

// TestTrunkDecodesBothSpellings pins the read-path fold of ADR-0212 decision 3.
// A trunk is a path; the boolean that decision retired named no path of its own,
// so it resolves to the checkout that keys its block — and `trunk = false`, which
// used to mean "not this checkout", declares nothing at all.
func TestTrunkDecodesBothSpellings(t *testing.T) {
	t.Parallel()
	var cfg Config
	if _, err := toml.Decode(`
[repo."/srv/app/main"]
trunk = true

[repo."/srv/other"]
trunk = "/srv/other/wt"

[repo."/srv/none"]
trunk = false

[repo."/srv/silent"]
turn_cap = 3
`, &cfg); err != nil {
		t.Fatalf("decode: %v", err)
	}

	cases := []struct {
		key  string
		want string
		ok   bool
	}{
		{"/srv/app/main", "/srv/app/main", true}, // folded to the block's own key
		{"/srv/other", "/srv/other/wt", true},    // the path as stated
		{"/srv/none", "", false},
		{"/srv/silent", "", false},
	}
	for _, tc := range cases {
		got, ok := cfg.Repo[tc.key].Trunk.Resolve(tc.key)
		if got != tc.want || ok != tc.ok {
			t.Errorf("[repo.%q] trunk = (%q, %v), want (%q, %v)", tc.key, got, ok, tc.want, tc.ok)
		}
	}

	// A value that is neither spelling is refused rather than silently ignored,
	// so a typo surfaces as a load error instead of a repository with no trunk.
	if _, err := toml.Decode("[repo.\"/srv/app\"]\ntrunk = 3\n", &Config{}); err == nil {
		t.Error("a numeric trunk must not decode")
	}
}

// TestTrunkResolvesForEveryWorktree pins the scope of decision 3: a trunk is
// stated at repository scope, so every worktree of the repository resolves the
// one path. The path spelling keys its block by a *sibling* worktree, which is
// what the retired boolean could never do — its block had to be the trunk.
func TestTrunkResolvesForEveryWorktree(t *testing.T) {
	cases := []struct {
		name string
		body func(f *overrideScopeFixture) string
	}{
		{"path value keyed by another worktree", func(f *overrideScopeFixture) string {
			return "[repo." + quoted(f.feature) + "]\ntrunk = " + quoted(f.main) + "\n"
		}},
		{"retired boolean keyed by the trunk", func(f *overrideScopeFixture) string {
			return "[repo." + quoted(f.main) + "]\ntrunk = true\n"
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newOverrideScopeFixture(t)
			var cfg Config
			if _, err := toml.Decode(tc.body(f), &cfg); err != nil {
				t.Fatalf("decode: %v", err)
			}
			for _, checkout := range []string{f.main, f.feature, f.identity} {
				got, err := cfg.ResolveRepoConfig(f.d, checkout)
				if err != nil {
					t.Fatalf("%s: %v", checkout, err)
				}
				if !got.IsTrunk(f.d, f.main) {
					t.Errorf("trunk at %s = %q, want %s", checkout, got.Trunk, f.main)
				}
				if got.IsTrunk(f.d, f.feature) {
					t.Errorf("trunk at %s reads the sibling worktree as the Trunk", checkout)
				}
			}
		})
	}
}

// TestRecordedTrunkStillResolves pins the other half of the fold: a machine whose
// trunk was recorded by an older `--trunk` holds a path-keyed `trunk = true`
// record, and it must keep resolving to that checkout with no operator action.
func TestRecordedTrunkStillResolves(t *testing.T) {
	f := newOverrideScopeFixture(t)
	writeConfigFile(t, filepath.Join(filepath.Dir(f.overridePath), "config.runtime.toml"),
		"["+quoted(f.main)+"]\ntrunk = true\n")

	got, err := (&Config{}).ResolveRepoConfig(f.d, f.feature)
	if err != nil {
		t.Fatal(err)
	}
	if !got.IsTrunk(f.d, f.main) {
		t.Errorf("trunk from the retired record = %q, want %s", got.Trunk, f.main)
	}

	// A declaration is more specific than what pop recorded, so it takes over.
	cfg := &Config{Repo: map[string]RepoOverrideConfig{f.main: {Trunk: trunkPtr(f.feature)}}}
	got, err = cfg.ResolveRepoConfig(f.d, f.main)
	if err != nil {
		t.Fatal(err)
	}
	if !got.IsTrunk(f.d, f.feature) {
		t.Errorf("trunk = %q, want the declared %s to beat the record", got.Trunk, f.feature)
	}
}

// TestTrunkNeverResolvesThroughTheTrunkAnchor pins ADR-0150's self-reference
// guard, which decision 3 must not cost: the in-tree .pop/config.toml anchors are
// found *through* the trunk, so a trunk stated in one could never be read. It is
// no key of the committed surface, and resolution ignores it wherever it sits.
func TestTrunkNeverResolvesThroughTheTrunkAnchor(t *testing.T) {
	f := newOverrideScopeFixture(t)
	for _, dir := range []string{f.main, f.feature} {
		if err := os.MkdirAll(filepath.Join(dir, ".pop"), 0o755); err != nil {
			t.Fatal(err)
		}
		body := "trunk = " + quoted(f.main) + "\n"
		if err := os.WriteFile(filepath.Join(dir, ".pop", "config.toml"), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	got, err := (&Config{}).ResolveRepoConfig(f.d, f.feature)
	if err != nil {
		t.Fatal(err)
	}
	if got.Trunk != "" {
		t.Errorf("trunk = %q, want empty: a committed file must not be able to name one", got.Trunk)
	}
	if len(got.Findings) == 0 {
		t.Error("a committed trunk must surface as a scope-legality finding")
	}
}

// TestPersistedTrunkReadsBackAsAPath covers the writer `--trunk` drives: naming a
// trunk states intent, so it lands in the override layer under the repository's
// identity, and every worktree — plus the trunk resolver's own read — sees it.
func TestPersistedTrunkReadsBackAsAPath(t *testing.T) {
	f := newOverrideScopeFixture(t)
	if err := PersistRepoTrunkWith(f.d, f.main); err != nil {
		t.Fatalf("persist: %v", err)
	}

	data, err := os.ReadFile(f.overridePath)
	if err != nil {
		t.Fatalf("read override layer: %v", err)
	}
	if !strings.Contains(string(data), "trunk = "+quoted(canonicalPath(f.d, f.main))) {
		t.Fatalf("override layer does not state the trunk as a path:\n%s", data)
	}

	stated, err := OverrideTrunkPathsWith(f.d)
	if err != nil || len(stated) != 1 || stated[0] != canonicalPath(f.d, f.main) {
		t.Fatalf("stated trunks = %q (err %v), want [%s]", stated, err, canonicalPath(f.d, f.main))
	}

	for _, checkout := range []string{f.main, f.feature} {
		got, err := (&Config{}).ResolveRepoConfig(f.d, checkout)
		if err != nil {
			t.Fatal(err)
		}
		if !got.IsTrunk(f.d, f.main) {
			t.Errorf("resolved trunk at %s = %q, want %s", checkout, got.Trunk, f.main)
		}
	}

	// The override is laid over the ladder, so it wins over a declaration too.
	cfg := &Config{Repo: map[string]RepoOverrideConfig{f.main: {Trunk: trunkPtr(f.feature)}}}
	got, err := cfg.ResolveRepoConfig(f.d, f.main)
	if err != nil {
		t.Fatal(err)
	}
	if !got.IsTrunk(f.d, f.main) {
		t.Errorf("trunk = %q, want the override %s to beat the declaration", got.Trunk, f.main)
	}
}
