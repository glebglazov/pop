package config

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/glebglazov/pop/internal/deps"
)

// The declared-trunk set is what the project picker reads instead of resolving a
// trunk per checkout: both path-keyed tiers, home-expanded, cleaned, sorted, and
// no git anywhere near it.
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

	real := deps.NewRealFileSystem()
	d := &Deps{FS: &deps.MockFileSystem{
		UserHomeDirFunc: func() (string, error) { return home, nil },
		ReadFileFunc:    real.ReadFile,
		StatFunc:        real.Stat,
	}}

	cfg := &Config{Repo: map[string]RepoOverrideConfig{
		"~/Dev/work/game_server/main": {Trunk: boolPtr(true)},
		"/srv/hawk/./trunk":           {Trunk: boolPtr(true)},
		"/srv/not-a-trunk":            {Trunk: boolPtr(false)},
		"/srv/no-opinion":             {},
	}}

	got := cfg.DeclaredTrunkPathsWith(d)
	want := []string{
		"/srv/hawk/trunk",
		"/srv/kestrel/main",
		filepath.Join(home, "Dev", "work", "game_server", "main"),
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("DeclaredTrunkPathsWith:\n got %q\nwant %q", got, want)
	}

	// A nil Config is the zero-declaration case, not a panic: the picker asks
	// before it knows whether config loaded.
	var nilCfg *Config
	if got := nilCfg.DeclaredTrunkPathsWith(d); len(got) != 1 || got[0] != "/srv/kestrel/main" {
		t.Errorf("nil Config = %q, want the runtime tier alone", got)
	}
}
