package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// prodDataDirAtStartup and prodConfigDirAtStartup are the developer's real pop
// data dir and hand-authored config dir, resolved from the real process
// environment once at package load — before any test calls t.Setenv. The guard
// compares against this snapshot, so a test that redirects XDG_DATA_HOME or
// XDG_CONFIG_HOME to a temp dir, or injects a fake FileSystem, is safe, while a
// test that lands back on the machine's own files is caught.
var (
	prodDataDirAtStartup   = realProductionDataDir()
	prodConfigDirAtStartup = realProductionConfigDir()
)

// guardTestConfigFile is the isolation backstop for the config layers (the
// counterpart of tasks' guardTestStorePath): under `go test`, reading the
// machine's config.toml, an include beside it, or a file pop writes in its data
// dir makes the test's outcome depend on whoever runs it, and a write would edit
// the developer's own config. Touching such a file panics instead, so the leak
// cannot silently return. Only the file access trips it: computing a default
// path that an injected loader then ignores reads nothing. It is a no-op outside
// tests.
func guardTestConfigFile(path string) {
	if !testing.Testing() {
		return
	}
	if !withinDir(path, prodDataDirAtStartup) && !withinDir(path, prodConfigDirAtStartup) {
		return
	}
	panic("config: test touched the real pop config file " + path +
		"; route the load through a Deps whose FileSystem maps XDG_DATA_HOME / XDG_CONFIG_HOME to a temp dir, or pass a temp config path")
}

func withinDir(path, dir string) bool {
	if dir == "" {
		return false
	}
	rel, err := filepath.Rel(dir, filepath.Clean(path))
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

func realProductionDataDir() string {
	if xdgData := os.Getenv("XDG_DATA_HOME"); xdgData != "" {
		return filepath.Join(xdgData, "pop")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".local", "share", "pop")
}

func realProductionConfigDir() string {
	if xdgConfig := os.Getenv("XDG_CONFIG_HOME"); xdgConfig != "" {
		return filepath.Join(xdgConfig, "pop")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".config", "pop")
}
