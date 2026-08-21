package queuetest

import (
	"testing"

	"github.com/glebglazov/pop/store"

	"github.com/glebglazov/pop/internal/deps"
	"github.com/glebglazov/pop/tasks"
)

// DataDeps returns task deps rooted at a fresh temp XDG data dir.
func DataDeps(t *testing.T) *tasks.Deps {
	t.Helper()
	dir := t.TempDir()
	// Pin the *real* process environment at the same temp dir too: helpers that
	// reach the store through tasks.DefaultDeps() (e.g. RefreshWith in
	// setupAbandonTaskManifest) resolve XDG_DATA_HOME from real env, not from the
	// mock seam below. Without this they would write into ~/.local/share/pop and
	// pollute the developer's machine-global store (slice 01).
	t.Setenv("XDG_DATA_HOME", dir)
	real := deps.NewRealFileSystem()
	d := tasks.DefaultDeps()
	d.FS = &deps.MockFileSystem{
		GetenvFunc: func(key string) string {
			if key == "XDG_DATA_HOME" {
				return dir
			}
			return ""
		},
		ReadFileFunc:  real.ReadFile,
		WriteFileFunc: real.WriteFile,
		MkdirAllFunc:  real.MkdirAll,
		RenameFunc:    real.Rename,
		RemoveAllFunc: real.RemoveAll,
	}
	// The store handle is now process-cached; close it at test end so it does not
	// outlive this test's temp data dir (test cleanup, per ADR-0118).
	t.Cleanup(func() { _ = d.CloseStore() })
	return d
}

// SeedAbnormalDrain records one abnormal (crashed) terminal Drain for a set,
// the unit the derived backoff/parking counts (ADR-0055). Only a genuine crash
// is abnormal; interrupted is a clean stop (ADR-0120).
func SeedAbnormalDrain(t *testing.T, td *tasks.Deps, runtimePath, setID string) {
	t.Helper()
	h, err := tasks.BeginDrain(td, runtimePath, setID, nil)
	if err != nil {
		t.Fatalf("BeginDrain: %v", err)
	}
	if err := h.Finish(store.DrainEnding{State: store.StateCrashed}); err != nil {
		t.Fatalf("Finish: %v", err)
	}
}

// RepoCommonDir resolves the Drain row's repo key (the canonical git common
// dir) for a checkout, matching what BeginDrain records.
func RepoCommonDir(t *testing.T, td *tasks.Deps, path string) string {
	t.Helper()
	id, err := tasks.ResolveRepositoryIdentity(td, path)
	if err != nil {
		t.Fatalf("resolve repository identity: %v", err)
	}
	return id.CommonDir
}
