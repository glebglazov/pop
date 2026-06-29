package binding

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/glebglazov/pop/config"
	"github.com/glebglazov/pop/internal/deps"
	"github.com/glebglazov/pop/tasks"
)

func archiveTestDeps(t *testing.T) *tasks.Deps {
	t.Helper()
	t.Setenv("XDG_DATA_HOME", filepath.Join(t.TempDir(), "xdg"))
	return &tasks.Deps{FS: deps.NewRealFileSystem(), Git: deps.NewRealGit()}
}

func archiveTestRepo(t *testing.T) string {
	t.Helper()
	repo := t.TempDir()
	runArchiveTestGit(t, repo, "init")
	runArchiveTestGit(t, repo, "config", "user.email", "pop@example.test")
	runArchiveTestGit(t, repo, "config", "user.name", "Pop Test")
	if err := exec.Command("git", "-C", repo, "commit", "--allow-empty", "-m", "base").Run(); err != nil {
		t.Fatalf("commit: %v", err)
	}
	return repo
}

func runArchiveTestGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git -C %s %v: %v\n%s", dir, args, err, out)
	}
}

func archiveTestWorktree(t *testing.T, repo, branch string) string {
	t.Helper()
	wt := filepath.Join(t.TempDir(), "wt-"+branch)
	runArchiveTestGit(t, repo, "worktree", "add", "-b", branch, wt, "HEAD")
	return wt
}

func seedArchiveBinding(t *testing.T, td *tasks.Deps, repo, setID string, b Binding) {
	t.Helper()
	id, err := tasks.ResolveRepositoryIdentity(td, repo)
	if err != nil {
		t.Fatalf("identity: %v", err)
	}
	store := &Store{}
	store.Put(Key(id, setID), b)
	if err := Save(td, store); err != nil {
		t.Fatalf("save binding: %v", err)
	}
}

func TestPrepareManagedWorktreesForArchiveConfirmDeletesWorktree(t *testing.T) {
	repo := archiveTestRepo(t)
	wt := archiveTestWorktree(t, repo, "managed-branch")
	td := archiveTestDeps(t)
	seedArchiveBinding(t, td, repo, "managed-done", Binding{
		RuntimePath: wt,
		Branch:      "managed-branch",
		Project:     filepath.Base(repo),
		Provisioned: true,
	})
	cfg := &config.Config{Projects: []config.ProjectEntry{{Path: repo}}}

	if err := PrepareManagedWorktreesForArchive(td, nil, cfg, []string{"managed-done"}, ArchiveConfirmOptions{
		In: strings.NewReader("y\n"),
	}); err != nil {
		t.Fatalf("prepare: %v", err)
	}
	if _, err := os.Stat(wt); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("worktree should be removed, stat err = %v", err)
	}
	if branch := archiveTestGitOutput(t, repo, "branch", "--list", "managed-branch"); strings.TrimSpace(branch) != "" {
		t.Fatalf("branch should be deleted, still have %q", branch)
	}
	store, err := Load(td)
	if err != nil {
		t.Fatal(err)
	}
	if store != nil && len(store.Bindings) != 0 {
		t.Fatalf("binding should be released: %#v", store.Bindings)
	}
}

func TestPrepareManagedWorktreesForArchiveDeclineAborts(t *testing.T) {
	repo := archiveTestRepo(t)
	wt := archiveTestWorktree(t, repo, "managed-branch")
	td := archiveTestDeps(t)
	seedArchiveBinding(t, td, repo, "managed-done", Binding{
		RuntimePath: wt,
		Branch:      "managed-branch",
		Project:     filepath.Base(repo),
		Provisioned: true,
	})
	cfg := &config.Config{Projects: []config.ProjectEntry{{Path: repo}}}

	err := PrepareManagedWorktreesForArchive(td, nil, cfg, []string{"managed-done"}, ArchiveConfirmOptions{
		In: strings.NewReader("n\n"),
	})
	if !errors.Is(err, ErrArchiveCancelled) {
		t.Fatalf("err = %v, want ErrArchiveCancelled", err)
	}
	if _, err := os.Stat(wt); err != nil {
		t.Fatalf("worktree should remain: %v", err)
	}
	store, err := Load(td)
	if err != nil {
		t.Fatal(err)
	}
	if store == nil || len(store.Bindings) != 1 {
		t.Fatalf("binding should remain: %#v", store)
	}
}

func TestPrepareManagedWorktreesForArchiveYesSkipsPrompt(t *testing.T) {
	repo := archiveTestRepo(t)
	wt := archiveTestWorktree(t, repo, "managed-branch")
	td := archiveTestDeps(t)
	seedArchiveBinding(t, td, repo, "managed-done", Binding{
		RuntimePath: wt,
		Branch:      "managed-branch",
		Project:     filepath.Base(repo),
		Provisioned: true,
	})
	cfg := &config.Config{Projects: []config.ProjectEntry{{Path: repo}}}

	if err := PrepareManagedWorktreesForArchive(td, nil, cfg, []string{"managed-done"}, ArchiveConfirmOptions{
		Yes: true,
		In:  tasks.NonInteractiveReader{},
	}); err != nil {
		t.Fatalf("prepare --yes: %v", err)
	}
	if _, err := os.Stat(wt); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("worktree should be removed")
	}
}

func TestPrepareManagedWorktreesForArchiveSkipsAdoptedAndUnbound(t *testing.T) {
	repo := archiveTestRepo(t)
	wt := archiveTestWorktree(t, repo, "adopted-branch")
	td := archiveTestDeps(t)
	seedArchiveBinding(t, td, repo, "adopted-done", Binding{
		RuntimePath: wt,
		Branch:      "adopted-branch",
		Project:     filepath.Base(repo),
		Provisioned: false,
	})
	cfg := &config.Config{Projects: []config.ProjectEntry{{Path: repo}}}

	if err := PrepareManagedWorktreesForArchive(td, nil, cfg, []string{"adopted-done", "missing-set"}, ArchiveConfirmOptions{
		In: tasks.NonInteractiveReader{},
	}); err != nil {
		t.Fatalf("prepare adopted/unbound: %v", err)
	}
	if _, err := os.Stat(wt); err != nil {
		t.Fatalf("adopted worktree must remain: %v", err)
	}
	store, err := Load(td)
	if err != nil {
		t.Fatal(err)
	}
	if store == nil || len(store.Bindings) != 1 {
		t.Fatalf("adopted binding must remain: %#v", store)
	}
}

func archiveTestGitOutput(t *testing.T, dir string, args ...string) string {
	t.Helper()
	out, err := deps.NewRealGit().CommandInDir(dir, args...)
	if err != nil {
		t.Fatalf("git -C %s %v: %v", dir, args, err)
	}
	return out
}
