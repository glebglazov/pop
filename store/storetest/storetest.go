// Package storetest builds one pre-migrated pop.db template per test binary
// and hands out copies of it, so a store.Open under test pays only a version
// check instead of the full forward-migration DDL (ADR-0144). It is a plain
// package rather than a _test.go file so tasks, queue, and routine test code
// can share the one template across package boundaries; the store package's
// own tests keep migrating from an empty database, since that is the
// behavior they exercise.
package storetest

import (
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/glebglazov/pop/store"
)

var (
	buildOnce sync.Once
	tmplData  []byte
	buildErr  error
)

func build() ([]byte, error) {
	buildOnce.Do(func() {
		dir, err := os.MkdirTemp("", "pop-store-template")
		if err != nil {
			buildErr = err
			return
		}
		defer os.RemoveAll(dir)
		path := filepath.Join(dir, "pop.db")
		s, err := store.Open(path, func(int, string) bool { return false })
		if err != nil {
			buildErr = err
			return
		}
		closeErr := s.Close()
		if closeErr != nil {
			buildErr = closeErr
			return
		}
		tmplData, buildErr = os.ReadFile(path)
	})
	return tmplData, buildErr
}

// WriteTemplate copies the pre-migrated template database to dbPath, creating
// its parent directory as needed. It never overwrites an already-present file
// at dbPath, so it is safe to call unconditionally right before a create-if-
// missing store open. On any failure it leaves dbPath as it found it, and the
// caller's own store.Open falls back to migrating from empty.
func WriteTemplate(dbPath string) error {
	if _, err := os.Stat(dbPath); err == nil {
		return nil
	}
	data, err := build()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(dbPath), 0o755); err != nil {
		return err
	}
	return os.WriteFile(dbPath, data, 0o644)
}

// Seed writes the template at dbPath, failing the test on error. Use it in
// test code that opens a store directly at a path tasks.Deps.Store has not
// already seeded (which does so automatically under go test).
func Seed(t *testing.T, dbPath string) {
	t.Helper()
	if err := WriteTemplate(dbPath); err != nil {
		t.Fatalf("storetest: seed template at %s: %v", dbPath, err)
	}
}
