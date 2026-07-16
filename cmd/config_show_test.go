package cmd

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/glebglazov/pop/config"
	"github.com/glebglazov/pop/internal/deps"
	"github.com/glebglazov/pop/tasks"
)

// showTrunkDeps returns real-git task deps rooted at an isolated XDG data home
// so the resolver can never accidentally read or write a shared task-binding
// store.
func showTrunkDeps(t *testing.T) *tasks.Deps {
	t.Helper()
	t.Setenv("XDG_DATA_HOME", filepath.Join(t.TempDir(), "xdg"))
	return &tasks.Deps{FS: deps.NewRealFileSystem(), Git: deps.NewRealGit()}
}

func runGitShow(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

func realPath(t *testing.T, path string) string {
	t.Helper()
	r, err := filepath.EvalSymlinks(path)
	if err != nil {
		return path
	}
	return r
}

// TestResolveCurrentRepoTrunkNonBareDerived covers a plain (non-bare) repo with
// no trunk config: the resolver reports the git-derived main worktree as the
// trunk and bare = false.
func TestResolveCurrentRepoTrunkNonBareDerived(t *testing.T) {
	td := showTrunkDeps(t)
	repo := t.TempDir()
	runGitShow(t, repo, "init")
	runGitShow(t, repo, "config", "user.email", "a@b.c")
	runGitShow(t, repo, "config", "user.name", "x")
	runGitShow(t, repo, "commit", "--allow-empty", "-m", "init")

	got, err := resolveCurrentRepoTrunk(td, &config.Config{}, repo)
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if got == nil {
		t.Fatal("expected a resolved trunk inside a git repo, got nil")
	}
	if got.Bare {
		t.Errorf("bare = true, want false for a non-bare repo")
	}
	if realPath(t, got.Path) != realPath(t, repo) {
		t.Errorf("trunk = %q, want the main worktree %q", got.Path, repo)
	}

	// The resolver must never touch the task-binding store.
	if entries, _ := os.ReadDir(filepath.Join(os.Getenv("XDG_DATA_HOME"), "pop", "repos")); len(entries) != 0 {
		t.Errorf("resolver wrote to the task-binding store: %v", entries)
	}
}

// TestResolveCurrentRepoTrunkBareConfigOverride covers a bare repo whose
// config-declared trunk = true names one worktree: the resolver reports that
// worktree as the trunk and bare = true.
func TestResolveCurrentRepoTrunkBareConfigOverride(t *testing.T) {
	td := showTrunkDeps(t)
	base := t.TempDir()
	seed := filepath.Join(base, "seed")
	runGitShow(t, base, "init", seed)
	runGitShow(t, seed, "config", "user.email", "a@b.c")
	runGitShow(t, seed, "config", "user.name", "x")
	runGitShow(t, seed, "commit", "--allow-empty", "-m", "init")

	bare := filepath.Join(base, "bare.git")
	if out, err := exec.Command("git", "clone", "--bare", seed, bare).CombinedOutput(); err != nil {
		t.Fatalf("clone --bare: %v\n%s", err, out)
	}
	wt := filepath.Join(base, "trunkwt")
	runGitShow(t, bare, "worktree", "add", "-b", "trunk", wt, "HEAD")

	cfg := &config.Config{
		Repo: map[string]config.RepoOverrideConfig{
			wt: {Trunk: boolPtrShow(true)},
		},
	}

	got, err := resolveCurrentRepoTrunk(td, cfg, wt)
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if got == nil {
		t.Fatal("expected a resolved trunk inside a bare repo worktree, got nil")
	}
	if !got.Bare {
		t.Errorf("bare = false, want true for a bare repo")
	}
	if realPath(t, got.Path) != realPath(t, wt) {
		t.Errorf("trunk = %q, want the config-declared worktree %q", got.Path, wt)
	}
}

// TestResolveCurrentRepoTrunkOutsideRepo covers running outside any git repo:
// the resolver returns nil so the current-repo section is omitted.
func TestResolveCurrentRepoTrunkOutsideRepo(t *testing.T) {
	td := showTrunkDeps(t)
	dir := t.TempDir() // a plain directory, not a git repo

	got, err := resolveCurrentRepoTrunk(td, &config.Config{}, dir)
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if got != nil {
		t.Errorf("expected nil trunk outside a git repo, got %+v", got)
	}
}

func boolPtrShow(b bool) *bool { return &b }
