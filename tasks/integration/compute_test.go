package integration

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/glebglazov/pop/tasks"
)

func TestComputeClean(t *testing.T) {
	repo := initMergeabilityRepo(t)
	wt := filepath.Join(t.TempDir(), "set-clean")
	runGit(t, repo, "worktree", "add", "-b", "set-clean", wt, "HEAD")
	writeFile(t, filepath.Join(wt, "set.txt"), "set\n")
	runGit(t, wt, "add", "set.txt")
	runGit(t, wt, "commit", "-m", "set change")

	got, err := Compute(tasks.DefaultDeps(), repo, wt)
	if err != nil {
		t.Fatalf("Compute: %v", err)
	}
	if got.Status != StatusClean {
		t.Fatalf("status = %q, want clean", got.Status)
	}
}

func TestComputeConflictAfterWorkingBranchAdvanced(t *testing.T) {
	repo := initMergeabilityRepo(t)
	wt := filepath.Join(t.TempDir(), "set-conflict")
	runGit(t, repo, "worktree", "add", "-b", "set-conflict", wt, "HEAD")

	writeFile(t, filepath.Join(wt, "shared.txt"), "set branch\n")
	runGit(t, wt, "add", "shared.txt")
	runGit(t, wt, "commit", "-m", "set edits shared")

	writeFile(t, filepath.Join(repo, "shared.txt"), "working branch\n")
	runGit(t, repo, "add", "shared.txt")
	runGit(t, repo, "commit", "-m", "working edits shared")

	got, err := Compute(tasks.DefaultDeps(), repo, wt)
	if err != nil {
		t.Fatalf("Compute: %v", err)
	}
	if got.Status != StatusConflicts {
		t.Fatalf("status = %q, want conflicts", got.Status)
	}
}

func initMergeabilityRepo(t *testing.T) string {
	t.Helper()
	repo := t.TempDir()
	runGit(t, repo, "init")
	runGit(t, repo, "config", "user.email", "pop@example.test")
	runGit(t, repo, "config", "user.name", "Pop Test")
	writeFile(t, filepath.Join(repo, "shared.txt"), "base\n")
	runGit(t, repo, "add", "shared.txt")
	runGit(t, repo, "commit", "-m", "base")
	return repo
}

func writeFile(t *testing.T, path, contents string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(contents), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func runGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git -C %s %v: %v\n%s", dir, args, err, out)
	}
}
