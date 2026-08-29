package tasks

import (
	"context"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/glebglazov/pop/internal/deps"
)

// testFSWithDataHome builds a MockFileSystem that routes XDG_DATA_HOME to
// dataHome and delegates other operations to the real filesystem (ADR-0145).
func testFSWithDataHome(dataHome string) deps.FileSystem {
	real := deps.NewRealFileSystem()
	return &deps.MockFileSystem{
		GetenvFunc: func(key string) string {
			if key == "XDG_DATA_HOME" {
				return dataHome
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

// newTestDeps builds a per-test Deps whose fake FileSystem maps XDG_DATA_HOME
// to an isolated t.TempDir(), with the fake agent runner, fast recovery
// cadence, and no-sleep retry wait pre-wired (ADR-0145). Isolation rides the
// Deps seam — never t.Setenv or os.Chdir — so callers may use t.Parallel().
//
// The store path derived from the injected temp dir never equals
// prodDataDirAtStartup, so guardTestStorePath stays effective.
func newTestDeps(t *testing.T) *Deps {
	t.Helper()
	dir := t.TempDir()
	d := &Deps{
		FS:                           testFSWithDataHome(dir),
		Git:                          deps.NewRealGit(),
		Clock:                        deps.FixedClock{Instant: time.Date(2026, 8, 17, 18, 0, 0, 0, time.UTC)},
		Runner:                       fakeAwareRunner{},
		LookPath:                     exec.LookPath,
		RateTableFetcher:             panicRateTableFetcher{},
		RecoveryFastCheckInterval:    2 * time.Millisecond,
		RecoveryPollInterval:         2 * time.Millisecond,
		RecoveryPollImminentInterval: 2 * time.Millisecond,
		AdmissionPollInterval:        2 * time.Millisecond,
		RetryDelayWait:               testRetryDelayWaitHook,
		store:                        &storeCache{},
	}
	t.Cleanup(func() { _ = d.CloseStore() })
	t.Cleanup(func() { _ = d.CloseCacheDB() })
	return d
}

// panicRateTableFetcher fails loudly if a test triggers a Rate table refresh
// without injecting its own fetcher — the Spend lens must never reach the network.
type panicRateTableFetcher struct{}

func (panicRateTableFetcher) Fetch(context.Context) ([]byte, error) {
	panic("rate table fetch reached the network in a test")
}
