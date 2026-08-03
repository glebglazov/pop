package queuetest

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/glebglazov/pop/tasks/binding"

	"github.com/glebglazov/pop/store"
	"github.com/glebglazov/pop/tasks"
)

func SeedBindingStore(t *testing.T, td *tasks.Deps, bindings map[string]store.Binding) {
	t.Helper()
	// Replace the whole set the tests reason about: clear any rows a prior seed
	// left, then write the given ones. An empty map therefore clears the store.
	existing, err := binding.AllBindings(td)
	if err != nil {
		t.Fatal(err)
	}
	for key := range existing {
		if err := binding.Delete(td, key); err != nil {
			t.Fatal(err)
		}
	}
	for key, b := range bindings {
		if err := binding.Put(td, key, b); err != nil {
			t.Fatal(err)
		}
	}
}

func LoadBindingStore(t *testing.T, td *tasks.Deps) map[string]store.Binding {
	t.Helper()
	all, err := binding.AllBindings(td)
	if err != nil {
		t.Fatal(err)
	}
	return all
}

func TasksDeps(t *testing.T, allFound bool) *tasks.Deps {
	t.Helper()
	// Default isolation (slice 01): point the data dir at a temp location so any
	// store touch lands in a throwaway dir, never the developer's real
	// machine-global store. Only set one when the caller hasn't already isolated
	// XDG_DATA_HOME (e.g. queuetest.SetupSpawnRepo pins it to repo/.xdg and seeds
	// the store there) — clobbering it would hide the seeded rows. The
	// guardTestStorePath backstop panics if isolation is ever missed entirely.
	if os.Getenv("XDG_DATA_HOME") == "" {
		t.Setenv("XDG_DATA_HOME", t.TempDir())
	}
	d := tasks.DefaultDeps()
	d.LookPath = func(file string) (string, error) {
		if allFound {
			return "/bin/" + file, nil
		}
		return "", fmt.Errorf("missing %s", file)
	}
	// The store handle is now process-cached; close it at test end so it does not
	// outlive this test's temp data dir (test cleanup, per ADR-0118).
	t.Cleanup(func() { _ = d.CloseStore() })
	return d
}

func ArgsContain(args []string, want ...string) bool {
	for i := 0; i+len(want) <= len(args); i++ {
		match := true
		for j, w := range want {
			if args[i+j] != w {
				match = false
				break
			}
		}
		if match {
			return true
		}
	}
	return false
}

func IdleLock(path string) *tasks.RuntimeLockStatus {
	return &tasks.RuntimeLockStatus{RuntimePath: path}
}

// LiveLock returns a runtime-lock status that reads as a live (busy) lock.
func LiveLock(path string) *tasks.RuntimeLockStatus {
	return &tasks.RuntimeLockStatus{
		RuntimePath: path,
		Locked:      true,
		Metadata:    &tasks.RuntimeLockMetadata{PID: 4242, RuntimePath: path},
	}
}

// InitBareRepoWithWorktrees clones a committed source repo into a bare repo and
// adds n detached worktrees, returning the bare dir and the worktree paths. All
// worktrees share one Repository identity.
func InitBareRepoWithWorktrees(t *testing.T, n int) (string, []string) {
	t.Helper()
	src := InitGitRepoWithBase(t)
	parent := t.TempDir()
	runGit(t, parent, "clone", "--bare", src, "repo.git")
	bareDir := filepath.Join(parent, "repo.git")
	var wts []string
	for i := 0; i < n; i++ {
		wt := filepath.Join(parent, "wt"+string(rune('0'+i)))
		runGit(t, bareDir, "worktree", "add", "--detach", wt, "HEAD")
		wts = append(wts, wt)
	}
	return bareDir, wts
}

// canon canonicalizes a checkout path the way the scheduler does, for comparison.
func Canon(t *testing.T, td *tasks.Deps, path string) string {
	t.Helper()
	abs, err := filepath.Abs(path)
	if err != nil {
		t.Fatalf("absolutize %q: %v", path, err)
	}
	c, err := td.FS.EvalSymlinks(abs)
	if err != nil {
		t.Fatalf("canonicalize %q: %v", path, err)
	}
	return c
}
