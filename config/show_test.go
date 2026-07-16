package config

import (
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

	out, err := renderEffectiveTOML(d, cfg)
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

	out, err := renderEffectiveTOML(d, cfg)
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
