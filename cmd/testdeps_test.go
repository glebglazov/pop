package cmd

import (
	"path/filepath"
	"testing"

	"github.com/glebglazov/pop/config"
	"github.com/glebglazov/pop/internal/deps"
	"github.com/glebglazov/pop/project"
	"github.com/glebglazov/pop/routine"
	"github.com/glebglazov/pop/tasks"
	"github.com/glebglazov/pop/tasks/drain"
	"github.com/glebglazov/pop/wayfinder"
)

// cmdTestFS builds a MockFileSystem routing XDG_* through the deps seam
// (ADR-0145). File operations delegate to the real filesystem.
func cmdTestFS(dataHome, configHome string) deps.FileSystem {
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

// newTestCmdDeps builds per-test cmd.Deps with isolated XDG routing and an
// explicit working directory (ADR-0145).
func newTestCmdDeps(t *testing.T, workDir, dataHome, configHome string) *Deps {
	t.Helper()
	if dataHome == "" {
		if workDir != "" {
			dataHome = filepath.Join(workDir, ".xdg")
		} else {
			dataHome = filepath.Join(t.TempDir(), "xdg")
		}
	}
	fs := cmdTestFS(dataHome, configHome)
	td := &tasks.Deps{
		FS:     fs,
		Git:    deps.NewRealGit(),
		Runner: tasks.RealCommandRunner{},
	}
	return &Deps{
		Dir:   workDir,
		FS:    fs,
		Tasks: td,
		Config: &config.Deps{
			FS: fs,
		},
		Project:   project.DefaultDeps(),
		Queue:     &drain.Deps{Tasks: td},
		Routine:   &routine.Deps{FS: fs, Tasks: td},
		Wayfinder: &wayfinder.Deps{FS: fs, Tasks: td},
	}
}

// setCmdLayerDeps overrides cmdLayerDeps for the calling goroutine for the test
// duration (parallel-safe via goroutine-local storage, ADR-0145). Subtests
// inherit the same deps via the root test name.
func setCmdLayerDeps(t *testing.T, d *Deps) {
	t.Helper()
	gid := goroutineID()
	root := rootTestName(t)
	cmdLayerDepsLocal.Store(gid, d)
	cmdLayerDepsGroups.Store(root, d)
	t.Cleanup(func() {
		cmdLayerDepsLocal.Delete(gid)
		cmdLayerDepsGroups.Delete(root)
	})
}

// setupCmdRepoTest initializes a temp git repo with cmd-layer deps wired for
// parallel-safe cmd smoke tests.
func setupCmdRepoTest(t *testing.T) (root string, cd *Deps, td *tasks.Deps) {
	t.Helper()
	root = t.TempDir()
	initGitRepoWithCommitCmd(t, root)
	cd = newTestCmdDeps(t, root, filepath.Join(root, ".xdg"), "")
	setCmdLayerDeps(t, cd)
	return root, cd, cd.tasksDeps()
}
