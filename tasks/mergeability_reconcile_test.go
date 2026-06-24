package tasks

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/glebglazov/pop/internal/deps"
)

// mergeabilityScenario stands up a repo whose feature worktree merges cleanly
// into the trunk, returning the deps, the trunk checkout, the feature worktree,
// and their current HEADs. shared.txt is edited only on the feature branch, so
// merging feature into an untouched trunk is clean.
func mergeabilityScenario(t *testing.T) (d *Deps, trunk, feature, baseSHA, branchSHA string) {
	t.Helper()
	root := t.TempDir()
	t.Setenv("XDG_DATA_HOME", filepath.Join(root, ".xdg"))
	trunk = filepath.Join(root, "checkout")
	if err := os.MkdirAll(trunk, 0o755); err != nil {
		t.Fatal(err)
	}
	initExecutorGitRepo(t, trunk)
	writeFile(t, filepath.Join(trunk, "shared.txt"), "base\n")
	runGit(t, trunk, "add", "shared.txt")
	runGit(t, trunk, "commit", "-m", "add shared")

	feature = filepath.Join(root, "feature")
	runGit(t, trunk, "worktree", "add", "-b", "feature", feature, "HEAD")
	writeFile(t, filepath.Join(feature, "shared.txt"), "feature edit\n")
	runGit(t, feature, "add", "shared.txt")
	runGit(t, feature, "commit", "-m", "feature edits shared")

	d = &Deps{
		FS:           deps.NewRealFileSystem(),
		Git:          deps.NewRealGit(),
		ProcessAlive: func(pid int) bool { return pid == os.Getpid() },
	}
	baseSHA = mustRevParse(t, trunk)
	branchSHA = mustRevParse(t, feature)
	return d, trunk, feature, baseSHA, branchSHA
}

func mustRevParse(t *testing.T, dir string) string {
	t.Helper()
	sha, err := realGitInDir(dir, "rev-parse", "HEAD")
	if err != nil {
		t.Fatalf("rev-parse %s: %v", dir, err)
	}
	return sha
}

// TestResolveHeadSHAMatchesGit confirms the filesystem HEAD reader returns the
// same commit `git rev-parse HEAD` would — for both the trunk (a .git directory)
// and a linked worktree (a .git file whose branch ref lives in the common dir).
func TestResolveHeadSHAMatchesGit(t *testing.T) {
	d, trunk, feature, baseSHA, branchSHA := mergeabilityScenario(t)

	if got, ok := ResolveHeadSHA(d, trunk); !ok || got != baseSHA {
		t.Fatalf("trunk HEAD = %q ok=%v, want %q", got, ok, baseSHA)
	}
	if got, ok := ResolveHeadSHA(d, feature); !ok || got != branchSHA {
		t.Fatalf("feature HEAD = %q ok=%v, want %q", got, ok, branchSHA)
	}
}

// TestReconcileMergeabilityUnchangedForksNoGit is the SHA gate: a stored entry
// whose HEADs still match the recorded SHAs is left alone and forks no git.
func TestReconcileMergeabilityUnchangedForksNoGit(t *testing.T) {
	d, trunk, feature, baseSHA, branchSHA := mergeabilityScenario(t)

	key := "repo\x00set-clean"
	if err := PutMergeabilityEntry(d, key, MergeabilityEntry{
		RuntimePath: feature,
		WorkingPath: trunk,
		SetID:       "set-clean",
		Verdict:     MergeVerdictClean,
		BaseSHA:     baseSHA,
		BranchSHA:   branchSHA,
	}); err != nil {
		t.Fatalf("seed entry: %v", err)
	}

	// Any git fork now fails the test: an unchanged set must touch no git.
	forbid := &deps.MockGit{
		CommandFunc:      func(args ...string) (string, error) { t.Fatalf("forked git: %v", args); return "", nil },
		CommandInDirFunc: func(dir string, args ...string) (string, error) { t.Fatalf("forked git in %s: %v", dir, args); return "", nil },
	}
	noGit := &Deps{FS: deps.NewRealFileSystem(), Git: forbid, ProcessAlive: d.ProcessAlive}
	if _, err := ReconcileDrains(noGit); err != nil {
		t.Fatalf("ReconcileDrains: %v", err)
	}

	entries, err := LoadMergeabilityEntries(d)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if got := entries[key]; got.Verdict != MergeVerdictClean || got.BaseSHA != baseSHA || got.BranchSHA != branchSHA {
		t.Fatalf("entry mutated by unchanged reconcile: %#v", got)
	}
}

// TestReconcileMergeabilityFlipsCleanToConflicts covers the stale-verdict fix: a
// once-clean set whose trunk advanced into a conflicting state is recomputed to
// conflicts on the next reconcile, gated on the changed base SHA.
func TestReconcileMergeabilityFlipsCleanToConflicts(t *testing.T) {
	d, trunk, feature, baseSHA, branchSHA := mergeabilityScenario(t)

	key := "repo\x00set-flip"
	if err := PutMergeabilityEntry(d, key, MergeabilityEntry{
		RuntimePath: feature,
		WorkingPath: trunk,
		SetID:       "set-flip",
		Verdict:     MergeVerdictClean,
		BaseSHA:     baseSHA,
		BranchSHA:   branchSHA,
	}); err != nil {
		t.Fatalf("seed entry: %v", err)
	}

	// Trunk advances into a conflicting edit of the same file the feature branch
	// touched: the once-clean merge is now a conflict.
	writeFile(t, filepath.Join(trunk, "shared.txt"), "trunk edit\n")
	runGit(t, trunk, "add", "shared.txt")
	runGit(t, trunk, "commit", "-m", "trunk edits shared")
	newBase := mustRevParse(t, trunk)

	if _, err := ReconcileDrains(d); err != nil {
		t.Fatalf("ReconcileDrains: %v", err)
	}

	entries, err := LoadMergeabilityEntries(d)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	got := entries[key]
	if got.Verdict != MergeVerdictConflicts {
		t.Fatalf("verdict = %q, want conflicts after trunk advanced", got.Verdict)
	}
	if got.BaseSHA != newBase {
		t.Fatalf("base SHA = %q, want refreshed %q", got.BaseSHA, newBase)
	}
	if got.BranchSHA != branchSHA {
		t.Fatalf("branch SHA = %q, want unchanged %q", got.BranchSHA, branchSHA)
	}
}
