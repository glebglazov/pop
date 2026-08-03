package queuetest

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// InitGitRepoWithBase creates a temp git repo with one committed file and
// returns its path.
func InitGitRepoWithBase(t *testing.T) string {
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

// runGit runs one git command in dir, failing the test on error.
func runGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

// writeFile writes contents to path, creating parent directories.
func writeFile(t *testing.T, path, contents string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(contents), 0o644); err != nil {
		t.Fatal(err)
	}
}
