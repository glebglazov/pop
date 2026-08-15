package config

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/glebglazov/pop/internal/deps"
)

// The declared-trunk set is what the project picker reads instead of resolving a
// trunk per checkout: every source that can state one without knowing it first —
// blocks, the override layer, the retired records — home-expanded, cleaned,
// sorted, and no git anywhere near it. A block states a path; the retired boolean
// states the block's own key, and both arrive here as the checkout meant.
func TestDeclaredTrunkPaths(t *testing.T) {
	t.Parallel()

	home := t.TempDir()
	dataHome := filepath.Join(home, ".local", "share")
	if err := os.MkdirAll(filepath.Join(dataHome, "pop"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	runtimeFile := filepath.Join(dataHome, "pop", "config.runtime.toml")
	if err := os.WriteFile(runtimeFile, []byte("[\"/srv/kestrel/main\"]\ntrunk = true\n\n[\"/srv/other/wt\"]\nworkbench = \"x\"\n"), 0o644); err != nil {
		t.Fatalf("write runtime: %v", err)
	}
	overrideFile := filepath.Join(dataHome, "pop", "config.override.toml")
	if err := os.WriteFile(overrideFile, []byte("[repo.\"/srv/wren\"]\ntrunk = \"/srv/wren/main\"\n"), 0o644); err != nil {
		t.Fatalf("write override: %v", err)
	}

	real := deps.NewRealFileSystem()
	d := &Deps{FS: &deps.MockFileSystem{
		UserHomeDirFunc: func() (string, error) { return home, nil },
		ReadFileFunc:    real.ReadFile,
		StatFunc:        real.Stat,
	}}

	cfg := &Config{Repo: map[string]RepoOverrideConfig{
		"~/Dev/work/game_server": {Trunk: trunkPtr("~/Dev/work/game_server/main")},
		"/srv/hawk/wt":           {Trunk: trunkPtr("/srv/hawk/./trunk")},
		"/srv/legacy":            {Trunk: legacyTrunkPtr()},
		"/srv/not-a-trunk":       {Trunk: trunkPtr("")},
		"/srv/no-opinion":        {},
	}}

	got := cfg.DeclaredTrunkPathsWith(d)
	want := []string{
		"/srv/hawk/trunk",
		"/srv/kestrel/main",
		"/srv/legacy",
		"/srv/wren/main",
		filepath.Join(home, "Dev", "work", "game_server", "main"),
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("DeclaredTrunkPathsWith:\n got %q\nwant %q", got, want)
	}

	// A nil Config is the zero-declaration case, not a panic: the picker asks
	// before it knows whether config loaded.
	var nilCfg *Config
	want = []string{"/srv/kestrel/main", "/srv/wren/main"}
	if got := nilCfg.DeclaredTrunkPathsWith(d); !reflect.DeepEqual(got, want) {
		t.Errorf("nil Config = %q, want the pop-written sources alone (%q)", got, want)
	}
}
