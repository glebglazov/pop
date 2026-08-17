package tasks

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/glebglazov/pop/internal/deps"
)

// readTaskTrailer asks git itself for the commit's Pop-Task trailer, so the test
// proves git parses the line as a trailer rather than merely that the text is
// somewhere in the message.
func readTaskTrailer(t *testing.T, repo, sha string) string {
	t.Helper()
	out, err := realGitInDir(repo, "log", "-1", "--format=%(trailers:key=Pop-Task,valueonly)", sha)
	if err != nil {
		t.Fatalf("read trailer of %s: %v", sha, err)
	}
	return strings.TrimSpace(out)
}

// TestTaskTrailerKeepsTheDatedSetIdentifier pins the trailer's text against the
// timestamp stripping subjects apply: the value must read exactly as a Task
// target reference does.
func TestTaskTrailerKeepsTheDatedSetIdentifier(t *testing.T) {
	t.Parallel()
	if got, want := TaskTrailer("2026-08-17-commit-provenance", "01-a"), "Pop-Task: 2026-08-17-commit-provenance/01-a"; got != want {
		t.Fatalf("TaskTrailer = %q, want %q", got, want)
	}
	if got, want := TaskTrailer("2026-08-17-1430-commit-provenance", "02-b"), "Pop-Task: 2026-08-17-1430-commit-provenance/02-b"; got != want {
		t.Fatalf("TaskTrailer = %q, want %q", got, want)
	}
}

// TestCheckpointCommitCarriesNoTaskTrailer: the checkpoint captures the human's
// pre-existing changes, not a task's work, so marking it would return two
// commits for one task reference (ADR-0216).
func TestCheckpointCommitCarriesNoTaskTrailer(t *testing.T) {
	repo := t.TempDir()
	if _, err := realGitInDir(repo, "init"); err != nil {
		t.Fatalf("git init: %v", err)
	}
	if err := os.WriteFile(filepath.Join(repo, "dirty.txt"), []byte("human work\n"), 0o644); err != nil {
		t.Fatalf("write dirty.txt: %v", err)
	}

	d := &Deps{FS: deps.NewRealFileSystem(), Git: deps.NewRealGit()}
	if err := checkpointDirtyRuntime(d, repo, "2026-08-17-demo", "01-a", rootCommitOverrides); err != nil {
		t.Fatalf("checkpointDirtyRuntime: %v", err)
	}
	if got := readTaskTrailer(t, repo, "HEAD"); got != "" {
		t.Fatalf("checkpoint commit carries trailer %q, want none", got)
	}
}
