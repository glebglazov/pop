package routine

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/glebglazov/pop/store"
)

const executionStoreFile = "pop.db"

func executionStorePath(d *Deps) string {
	return filepath.Join(popDataDir(d), executionStoreFile)
}

func openExecutionStore(d *Deps) (*store.Store, error) {
	guardTestStorePath(executionStorePath(d))
	if err := d.FS.MkdirAll(popDataDir(d), 0o755); err != nil {
		return nil, fmt.Errorf("create data directory: %w", err)
	}
	s, err := store.Open(executionStorePath(d), d.ProcessAlive)
	if err != nil {
		return nil, fmt.Errorf("open execution-state store: %w", err)
	}
	return s, nil
}

var prodDataDirAtStartup = realProductionDataDir()

func guardTestStorePath(path string) {
	if !testing.Testing() {
		return
	}
	if prodDataDirAtStartup == "" {
		return
	}
	if filepath.Dir(path) == prodDataDirAtStartup {
		panic("routine: test attempted to open the real pop store at " + path +
			"; isolate the data dir to a temp location (XDG_DATA_HOME) before touching the store")
	}
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
