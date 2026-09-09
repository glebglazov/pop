package project

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/glebglazov/pop/internal/deps"
)

func TestRemoveCheckoutFinishesHalfRemovedCheckout(t *testing.T) {
	t.Parallel()
	repo, checkout := checkoutRemovalRepo(t)
	if err := os.Remove(filepath.Join(checkout, ".git")); err != nil {
		t.Fatalf("remove checkout git file: %v", err)
	}

	if err := RemoveCheckout(DefaultDeps(), repo, checkout); err != nil {
		t.Fatalf("remove half-removed checkout: %v", err)
	}
	if _, err := os.Stat(checkout); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("checkout remains: %v", err)
	}
	if got := checkoutRemovalGit(t, repo, "worktree", "list", "--porcelain"); strings.Contains(got, checkout) {
		t.Fatalf("worktree administration remains:\n%s", got)
	}
}

func TestRemoveCheckoutContinuesAfterEntryFailureAndReportsSurvivor(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	checkout := filepath.Join(root, "checkout")
	blocked := filepath.Join(checkout, "blocked")
	removed := filepath.Join(checkout, "removed")
	if err := os.MkdirAll(checkout, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{blocked, removed} {
		if err := os.WriteFile(path, []byte("data"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	real := deps.NewRealFileSystem()
	fs := &deps.MockFileSystem{
		StatFunc:    real.Stat,
		ReadDirFunc: real.ReadDir,
		RemoveAllFunc: func(path string) error {
			if path == blocked || path == checkout {
				return os.ErrPermission
			}
			return real.RemoveAll(path)
		},
	}
	pruned := false
	git := &deps.MockGit{CommandInDirFunc: func(_ string, args ...string) (string, error) {
		pruned = strings.Join(args, " ") == "worktree prune"
		return "", nil
	}}
	err := RemoveCheckout(&Deps{FS: fs, Git: git}, root, checkout)
	if err == nil || !strings.Contains(err.Error(), blocked) {
		t.Fatalf("error = %v, want surviving path %s", err, blocked)
	}
	if _, err := os.Stat(removed); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("sibling after failed entry was not removed: %v", err)
	}
	if _, err := os.Stat(blocked); err != nil {
		t.Fatalf("blocked entry should survive: %v", err)
	}
	if !pruned {
		t.Fatal("worktree administration was not pruned after partial removal")
	}
}

func checkoutRemovalRepo(t *testing.T) (string, string) {
	t.Helper()
	repo := filepath.Join(t.TempDir(), "repo")
	checkout := filepath.Join(filepath.Dir(repo), "feature")
	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatal(err)
	}
	checkoutRemovalGit(t, repo, "init", "-q")
	checkoutRemovalGit(t, repo, "config", "user.email", "pop@example.test")
	checkoutRemovalGit(t, repo, "config", "user.name", "Pop Test")
	if err := os.WriteFile(filepath.Join(repo, "seed"), []byte("seed"), 0o644); err != nil {
		t.Fatal(err)
	}
	checkoutRemovalGit(t, repo, "add", "seed")
	checkoutRemovalGit(t, repo, "commit", "-qm", "seed")
	checkoutRemovalGit(t, repo, "worktree", "add", "-qb", "feature", checkout)
	return repo, checkout
}

func checkoutRemovalGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return string(out)
}
