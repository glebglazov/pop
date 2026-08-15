package integrate

import (
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/glebglazov/pop/config"
	"github.com/glebglazov/pop/internal/deps"
)

var testConfigDepsStore sync.Map // *testing.T pointer -> *config.Deps

func testFS(dataHome, configHome string) deps.FileSystem {
	real := deps.NewRealFileSystem()
	return &deps.MockFileSystem{
		GetenvFunc: func(key string) string {
			switch key {
			case "XDG_DATA_HOME":
				return dataHome
			case "XDG_CONFIG_HOME":
				return configHome
			case "XDG_CACHE_HOME":
				if dataHome != "" {
					return filepath.Join(dataHome, "cache")
				}
			}
			return ""
		},
		GetwdFunc:        real.Getwd,
		UserHomeDirFunc:  func() (string, error) { return filepath.Join(dataHome, "home"), nil },
		StatFunc:         real.Stat,
		ReadDirFunc:      real.ReadDir,
		ReadFileFunc:     real.ReadFile,
		WriteFileFunc:    real.WriteFile,
		MkdirAllFunc:     real.MkdirAll,
		RenameFunc:       real.Rename,
		RemoveAllFunc:    real.RemoveAll,
		DirFSFunc:        real.DirFS,
		EvalSymlinksFunc: real.EvalSymlinks,
	}
}

func testConfigDeps(t *testing.T) *config.Deps {
	t.Helper()
	if v, ok := testConfigDepsStore.Load(t); ok {
		return v.(*config.Deps)
	}
	return setupIntegrateConfigLayer(t)
}

func setupIntegrateConfigLayer(t *testing.T) *config.Deps {
	t.Helper()
	dataHome := t.TempDir()
	configHome := filepath.Join(dataHome, "config")
	popConfigDir := filepath.Join(configHome, "pop")
	if err := os.MkdirAll(popConfigDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(popConfigDir, "config.toml"), []byte("projects = []\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	fs := testFS(dataHome, configHome)
	cd := &config.Deps{FS: fs}
	testConfigDepsStore.Store(t, cd)
	t.Cleanup(func() { testConfigDepsStore.Delete(t) })
	return cd
}

func testDataHome(t *testing.T) string {
	t.Helper()
	cd := testConfigDeps(t)
	if xdg := cd.FS.Getenv("XDG_DATA_HOME"); xdg != "" {
		return xdg
	}
	t.Fatal("test XDG_DATA_HOME must be set")
	return ""
}

func integrateRuntimePath(t *testing.T) string {
	t.Helper()
	cd := testConfigDeps(t)
	if xdg := cd.FS.Getenv("XDG_DATA_HOME"); xdg != "" {
		return filepath.Join(xdg, "pop", "config.runtime.toml")
	}
	t.Fatal("test XDG_DATA_HOME must be set")
	return ""
}

// integrateOverridePath is config.override.toml under the test data home: where
// a declined component's stated skills list lands.
func integrateOverridePath(t *testing.T) string {
	t.Helper()
	return filepath.Join(testDataHome(t), "pop", "config.override.toml")
}
