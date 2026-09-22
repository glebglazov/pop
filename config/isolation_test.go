package config

import (
	"path/filepath"
	"testing"

	"github.com/glebglazov/pop/internal/deps"
)

// isolatedFS is the real filesystem with pop's two machine-scoped roots moved
// into a test's temp dir, so a load reads only the files the test wrote.
type isolatedFS struct {
	*deps.RealFileSystem
	env map[string]string
}

func (f isolatedFS) Getenv(key string) string {
	if v, ok := f.env[key]; ok {
		return v
	}
	return f.RealFileSystem.Getenv(key)
}

// isolatedDeps is the Deps a test loads config through when it wants the real
// filesystem but none of the developer's own config.toml or config.override.toml.
func isolatedDeps(t testing.TB) *Deps {
	t.Helper()
	root := t.TempDir()
	return &Deps{FS: isolatedFS{RealFileSystem: deps.NewRealFileSystem(), env: map[string]string{
		"XDG_DATA_HOME":   filepath.Join(root, "data"),
		"XDG_CONFIG_HOME": filepath.Join(root, "config"),
	}}}
}
