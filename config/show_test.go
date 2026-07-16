package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/glebglazov/pop/internal/deps"
)

// identityFS resolves ~ against a fixed home and leaves paths otherwise
// unchanged — enough for tests that only care about ~ expansion, not symlinks.
func identityFS(home string) *deps.MockFileSystem {
	return &deps.MockFileSystem{
		UserHomeDirFunc:  func() (string, error) { return home, nil },
		EvalSymlinksFunc: func(path string) (string, error) { return path, nil },
	}
}

// TestRenderEffectiveTOMLDropsIncludes verifies the effective mirror does not
// re-list includes: the loader has already merged them, so the rendered TOML
// carries the merged values but no includes array to re-resolve.
func TestRenderEffectiveTOMLDropsIncludes(t *testing.T) {
	d := &Deps{FS: identityFS("/home/u")}
	cfg := &Config{
		Includes: []string{"extra.toml", "~/more.toml"},
		Projects: []ProjectEntry{{Path: "/home/u/Dev/merged-in"}},
	}

	out, err := renderEffectiveTOML(d, cfg, nil)
	if err != nil {
		t.Fatalf("renderEffectiveTOML: %v", err)
	}
	if strings.Contains(out, "includes") {
		t.Errorf("effective TOML should not re-list includes, got:\n%s", out)
	}
	if !strings.Contains(out, "/home/u/Dev/merged-in") {
		t.Errorf("merged-in value missing from effective TOML, got:\n%s", out)
	}
}

// TestRenderEffectiveTOMLCanonicalizesRepoKeys verifies every [repo."<path>"]
// key is emitted as an absolute realpath: ~ expanded against home and symlinks
// resolved to their canonical target.
func TestRenderEffectiveTOMLCanonicalizesRepoKeys(t *testing.T) {
	fs := &deps.MockFileSystem{
		UserHomeDirFunc: func() (string, error) { return "/home/u", nil },
		EvalSymlinksFunc: func(path string) (string, error) {
			// ~/Dev is a symlink into ~/private/Dev on this machine.
			if strings.HasPrefix(path, "/home/u/Dev/") {
				return strings.Replace(path, "/home/u/Dev/", "/home/u/private/Dev/", 1), nil
			}
			return path, nil
		},
	}
	d := &Deps{FS: fs}
	trunk := true
	cfg := &Config{
		Repo: map[string]RepoOverrideConfig{
			"~/Dev/app": {
				RepoScopeConfig: RepoScopeConfig{PreferredWorkbench: "dev"},
				Trunk:           &trunk,
			},
		},
	}

	out, err := renderEffectiveTOML(d, cfg, nil)
	if err != nil {
		t.Fatalf("renderEffectiveTOML: %v", err)
	}
	if !strings.Contains(out, `[repo."/home/u/private/Dev/app"]`) {
		t.Errorf("repo key not canonicalized to absolute realpath, got:\n%s", out)
	}
	if strings.Contains(out, "~/Dev/app") {
		t.Errorf("raw ~ repo key leaked into effective TOML, got:\n%s", out)
	}
	if !strings.Contains(out, "preferred_workbench") || !strings.Contains(out, "trunk = true") {
		t.Errorf("repo block body missing from effective TOML, got:\n%s", out)
	}
}

// TestRenderEffectiveTOMLAppendsResolvedTrunk verifies a non-nil resolved trunk
// is appended as a [current_repo] table with the trunk path canonicalized to an
// absolute realpath and the bare flag surfaced.
func TestRenderEffectiveTOMLAppendsResolvedTrunk(t *testing.T) {
	fs := &deps.MockFileSystem{
		UserHomeDirFunc: func() (string, error) { return "/home/u", nil },
		EvalSymlinksFunc: func(path string) (string, error) {
			if strings.HasPrefix(path, "/home/u/Dev/") {
				return strings.Replace(path, "/home/u/Dev/", "/home/u/private/Dev/", 1), nil
			}
			return path, nil
		},
	}
	d := &Deps{FS: fs}
	cfg := &Config{}

	out, err := renderEffectiveTOML(d, cfg, &ResolvedTrunk{Path: "~/Dev/app/main", Bare: true})
	if err != nil {
		t.Fatalf("renderEffectiveTOML: %v", err)
	}
	if !strings.Contains(out, "[current_repo]") {
		t.Errorf("missing [current_repo] table, got:\n%s", out)
	}
	if !strings.Contains(out, `trunk = "/home/u/private/Dev/app/main"`) {
		t.Errorf("trunk not canonicalized to absolute realpath, got:\n%s", out)
	}
	if !strings.Contains(out, "bare = true") {
		t.Errorf("bare flag missing, got:\n%s", out)
	}
	if strings.Contains(out, "~/Dev/app") {
		t.Errorf("raw ~ trunk path leaked, got:\n%s", out)
	}
}

// TestRenderEffectiveTOMLOmitsTrunkSection verifies a nil resolved trunk (run
// outside any git repo) leaves the current-repo section out entirely.
func TestRenderEffectiveTOMLOmitsTrunkSection(t *testing.T) {
	d := &Deps{FS: identityFS("/home/u")}
	out, err := renderEffectiveTOML(d, &Config{}, nil)
	if err != nil {
		t.Fatalf("renderEffectiveTOML: %v", err)
	}
	if strings.Contains(out, "current_repo") {
		t.Errorf("current-repo section should be absent for nil trunk, got:\n%s", out)
	}
}

// TestRenderEffectiveTOMLTrunkBareNoOverride verifies a bare repo with no
// resolvable trunk emits bare = true with no trunk key (path omitted).
func TestRenderEffectiveTOMLTrunkBareNoOverride(t *testing.T) {
	d := &Deps{FS: identityFS("/home/u")}
	out, err := renderEffectiveTOML(d, &Config{}, &ResolvedTrunk{Path: "", Bare: true})
	if err != nil {
		t.Fatalf("renderEffectiveTOML: %v", err)
	}
	if !strings.Contains(out, "[current_repo]") || !strings.Contains(out, "bare = true") {
		t.Errorf("expected [current_repo] with bare = true, got:\n%s", out)
	}
	if strings.Contains(out, "trunk =") {
		t.Errorf("trunk key should be omitted when no trunk resolves, got:\n%s", out)
	}
}

// writeShowConfig writes a minimal config.toml to a temp dir and returns its
// path, so EffectiveJSONWith (which loads through toml.DecodeFile) has a real
// file to read.
func writeShowConfig(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}
	return path
}

// TestEffectiveJSONWithMatchesTOMLShape verifies --json's renderer emits
// valid JSON carrying the same content as the TOML form: merged includes,
// canonicalized repo keys, and the current-repo trunk/bare nested under
// current_repo exactly as it appears as a [current_repo] table in TOML.
func TestEffectiveJSONWithMatchesTOMLShape(t *testing.T) {
	fs := &deps.MockFileSystem{
		UserHomeDirFunc: func() (string, error) { return "/home/u", nil },
		EvalSymlinksFunc: func(path string) (string, error) {
			if strings.HasPrefix(path, "/home/u/Dev/") {
				return strings.Replace(path, "/home/u/Dev/", "/home/u/private/Dev/", 1), nil
			}
			return path, nil
		},
		ReadFileFunc: func(path string) ([]byte, error) { return os.ReadFile(path) },
	}
	d := &Deps{FS: fs}
	path := writeShowConfig(t, `
[[projects]]
path = "/home/u/Dev/merged-in"

[repo."~/Dev/app"]
trunk = true
`)

	trunkFn := func(*Config) (*ResolvedTrunk, error) {
		return &ResolvedTrunk{Path: "~/Dev/app/main", Bare: true}, nil
	}

	tomlOut, err := EffectiveTOMLWith(d, path, trunkFn)
	if err != nil {
		t.Fatalf("EffectiveTOMLWith: %v", err)
	}
	jsonOut, err := EffectiveJSONWith(d, path, trunkFn)
	if err != nil {
		t.Fatalf("EffectiveJSONWith: %v", err)
	}

	var got map[string]interface{}
	if err := json.Unmarshal([]byte(jsonOut), &got); err != nil {
		t.Fatalf("--json output is not valid JSON: %v\n%s", err, jsonOut)
	}

	currentRepo, ok := got["current_repo"].(map[string]interface{})
	if !ok {
		t.Fatalf("current_repo missing or wrong shape in JSON, got: %v", got)
	}
	if currentRepo["trunk"] != "/home/u/private/Dev/app/main" {
		t.Errorf("current_repo.trunk = %v, want canonical realpath", currentRepo["trunk"])
	}
	if currentRepo["bare"] != true {
		t.Errorf("current_repo.bare = %v, want true", currentRepo["bare"])
	}
	if _, ok := got["includes"]; ok {
		t.Errorf("effective JSON should not re-list includes, got: %v", got)
	}
	if !strings.Contains(tomlOut, "/home/u/private/Dev/app/main") {
		t.Fatalf("sanity: TOML form missing canonical trunk, got:\n%s", tomlOut)
	}
}

// TestEffectiveJSONWithOmitsCurrentRepoOutsideRepo verifies a nil resolved
// trunk (outside any git repo) leaves current_repo out of the JSON entirely,
// so a consumer sees it absent rather than null-but-present or malformed.
func TestEffectiveJSONWithOmitsCurrentRepoOutsideRepo(t *testing.T) {
	d := &Deps{FS: identityFS("/home/u")}
	path := writeShowConfig(t, "")

	jsonOut, err := EffectiveJSONWith(d, path, func(*Config) (*ResolvedTrunk, error) {
		return nil, nil
	})
	if err != nil {
		t.Fatalf("EffectiveJSONWith: %v", err)
	}

	var got map[string]interface{}
	if err := json.Unmarshal([]byte(jsonOut), &got); err != nil {
		t.Fatalf("--json output is not valid JSON: %v\n%s", err, jsonOut)
	}
	if _, ok := got["current_repo"]; ok {
		t.Errorf("current_repo should be absent outside a repo, got: %v", got["current_repo"])
	}
}
