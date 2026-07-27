package binding

import (
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/glebglazov/pop/internal/deps"
	"github.com/glebglazov/pop/tasks"
)

// isolatedTasksDeps builds per-test tasks.Deps whose fake FileSystem maps
// XDG_DATA_HOME to an isolated temp dir (ADR-0145). Callers may use t.Parallel().
func isolatedTasksDeps(t *testing.T) *tasks.Deps {
	t.Helper()
	dir := t.TempDir()
	real := deps.NewRealFileSystem()
	d := &tasks.Deps{
		FS: &deps.MockFileSystem{
			GetenvFunc: func(key string) string {
				if key == "XDG_DATA_HOME" {
					return filepath.Join(dir, "xdg")
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
		Git:      deps.NewRealGit(),
		LookPath: exec.LookPath,
	}
	t.Cleanup(func() { _ = d.CloseStore() })
	return d
}
