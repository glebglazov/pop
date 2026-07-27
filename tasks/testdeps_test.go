package tasks

import (
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/glebglazov/pop/internal/deps"
)

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
	real := deps.NewRealFileSystem()
	d := &Deps{
		FS: &deps.MockFileSystem{
			GetenvFunc: func(key string) string {
				if key == "XDG_DATA_HOME" {
					return dir
				}
				return ""
			},
			GetwdFunc:        real.Getwd,
			UserHomeDirFunc:  func() (string, error) { return filepath.Join(dir, "home"), nil },
			StatFunc:         real.Stat,
			ReadDirFunc:      real.ReadDir,
			ReadFileFunc:     real.ReadFile,
			WriteFileFunc:    real.WriteFile,
			MkdirAllFunc:     real.MkdirAll,
			RenameFunc:       real.Rename,
			RemoveAllFunc:    real.RemoveAll,
			DirFSFunc:        real.DirFS,
			EvalSymlinksFunc: real.EvalSymlinks,
		},
		Git:                          deps.NewRealGit(),
		Runner:                       fakeAwareRunner{},
		LookPath:                     exec.LookPath,
		RecoveryFastCheckInterval:    2 * time.Millisecond,
		RecoveryPollInterval:         2 * time.Millisecond,
		RecoveryPollImminentInterval: 2 * time.Millisecond,
		RetryDelayWait:               testRetryDelayWaitHook,
		store:                        &storeCache{},
	}
	t.Cleanup(func() { _ = d.CloseStore() })
	return d
}
