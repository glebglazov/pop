package binding

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/glebglazov/pop/config"
	"github.com/glebglazov/pop/internal/deps"
	"github.com/glebglazov/pop/tasks"
)

func archiveTestDeps(t *testing.T) *tasks.Deps {
	t.Helper()
	return isolatedTasksDeps(t)
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
	if err := Put(td, Key(id, setID), b); err != nil {
		t.Fatalf("save binding: %v", err)
	}
}

func archiveManagedBinding(t *testing.T, td *tasks.Deps, repo, setID string) Binding {
	t.Helper()
	b, err := ProvisionWorktree(td, ManagedWorktreesRoot(td), repo, setID, "HEAD", time.Now())
	if err != nil {
		t.Fatalf("provision managed worktree: %v", err)
	}
	seedArchiveBinding(t, td, repo, setID, b)
	return b
}

func TestPrepareManagedWorktreesForArchiveConfirmDeletesWorktree(t *testing.T) {
	t.Parallel()
	repo := archiveTestRepo(t)
	td := archiveTestDeps(t)
	b := archiveManagedBinding(t, td, repo, "managed-done")
	cfg := &config.Config{Projects: []config.ProjectEntry{{Path: repo}}}

	if err := PrepareManagedWorktreesForArchive(td, nil, cfg, []string{"managed-done"}, ArchiveConfirmOptions{
		In: strings.NewReader("y\n"),
	}); err != nil {
		t.Fatalf("prepare: %v", err)
	}
	if _, err := os.Stat(b.RuntimePath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("worktree should be removed, stat err = %v", err)
	}
	if branch := archiveTestGitOutput(t, repo, "branch", "--list", b.Branch); strings.TrimSpace(branch) != "" {
		t.Fatalf("branch should be deleted, still have %q", branch)
	}
	all, err := AllBindings(td)
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 0 {
		t.Fatalf("binding should be released: %#v", all)
	}
}

func TestPrepareManagedWorktreesForArchiveDeclineAborts(t *testing.T) {
	t.Parallel()
	repo := archiveTestRepo(t)
	td := archiveTestDeps(t)
	b := archiveManagedBinding(t, td, repo, "managed-done")
	cfg := &config.Config{Projects: []config.ProjectEntry{{Path: repo}}}

	err := PrepareManagedWorktreesForArchive(td, nil, cfg, []string{"managed-done"}, ArchiveConfirmOptions{
		In: strings.NewReader("n\n"),
	})
	if !errors.Is(err, ErrArchiveCancelled) {
		t.Fatalf("err = %v, want ErrArchiveCancelled", err)
	}
	if _, err := os.Stat(b.RuntimePath); err != nil {
		t.Fatalf("worktree should remain: %v", err)
	}
	all, err := AllBindings(td)
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 1 {
		t.Fatalf("binding should remain: %#v", all)
	}
}

func TestPrepareManagedWorktreesForArchiveYesSkipsPrompt(t *testing.T) {
	t.Parallel()
	repo := archiveTestRepo(t)
	td := archiveTestDeps(t)
	b := archiveManagedBinding(t, td, repo, "managed-done")
	cfg := &config.Config{Projects: []config.ProjectEntry{{Path: repo}}}

	if err := PrepareManagedWorktreesForArchive(td, nil, cfg, []string{"managed-done"}, ArchiveConfirmOptions{
		Yes: true,
		In:  tasks.NonInteractiveReader{},
	}); err != nil {
		t.Fatalf("prepare --yes: %v", err)
	}
	if _, err := os.Stat(b.RuntimePath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("worktree should be removed")
	}
}

func TestPrepareManagedWorktreesForArchiveSkipsOutsideManagedRoot(t *testing.T) {
	t.Parallel()
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
	all, err := AllBindings(td)
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 1 {
		t.Fatalf("adopted binding must remain: %#v", all)
	}
}

func TestPrepareManagedWorktreesForArchiveSkipsWhenOtherSetStillBinds(t *testing.T) {
	t.Parallel()
	repo := archiveTestRepo(t)
	td := archiveTestDeps(t)
	b := archiveManagedBinding(t, td, repo, "managed-a")
	seedArchiveBinding(t, td, repo, "managed-b", Binding{
		RuntimePath: b.RuntimePath,
		Branch:      b.Branch,
		Project:     filepath.Base(repo),
		Provisioned: false,
	})
	cfg := &config.Config{Projects: []config.ProjectEntry{{Path: repo}}}

	if err := PrepareManagedWorktreesForArchive(td, nil, cfg, []string{"managed-a"}, ArchiveConfirmOptions{
		In: tasks.NonInteractiveReader{},
	}); err != nil {
		t.Fatalf("prepare shared checkout: %v", err)
	}
	if _, err := os.Stat(b.RuntimePath); err != nil {
		t.Fatalf("shared managed worktree must remain: %v", err)
	}
	all, err := AllBindings(td)
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 2 {
		t.Fatalf("both bindings must remain: %#v", all)
	}
}

func TestPrepareManagedWorktreesForArchiveAdoptedLastReferentDeletes(t *testing.T) {
	t.Parallel()
	repo := archiveTestRepo(t)
	td := archiveTestDeps(t)
	managed := archiveManagedBinding(t, td, repo, "managed-done")
	id, err := tasks.ResolveRepositoryIdentity(td, repo)
	if err != nil {
		t.Fatalf("identity: %v", err)
	}
	if err := Delete(td, Key(id, "managed-done")); err != nil {
		t.Fatalf("delete provisioned binding: %v", err)
	}
	seedArchiveBinding(t, td, repo, "adopted-done", Binding{
		RuntimePath: managed.RuntimePath,
		Branch:      managed.Branch,
		Project:     filepath.Base(repo),
		Provisioned: false,
	})
	cfg := &config.Config{Projects: []config.ProjectEntry{{Path: repo}}}

	if err := PrepareManagedWorktreesForArchive(td, nil, cfg, []string{"adopted-done"}, ArchiveConfirmOptions{
		Yes: true,
		In:  tasks.NonInteractiveReader{},
	}); err != nil {
		t.Fatalf("prepare adopted last referent: %v", err)
	}
	if _, err := os.Stat(managed.RuntimePath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("managed worktree should be removed for adopted last referent")
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
