package binding

import (
	"bytes"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/glebglazov/pop/config"
	"github.com/glebglazov/pop/tasks"
)

// TestProvisionScratchWorktreeNamesAndForksFromStartPoint pins the managed
// worktree created ahead of a Task set (ADR-0152): the directory is
// scratch-<YYYYMMDD>-<n> under the managed-worktree root with n disambiguating
// within the day, the branch matches the managed pop/<name>/<stamp> shape, the
// fork starts at the explicit start-point rather than HEAD, and no binding row
// is recorded — the worktree is unbound from birth.
func TestProvisionScratchWorktreeNamesAndForksFromStartPoint(t *testing.T) {
	t.Parallel()
	repo := initAdoptRepo(t)
	td := lifecycleTestDeps(t)
	now := time.Date(2026, 7, 29, 10, 30, 0, 0, time.UTC)

	trunkBranch := CurrentBranch(td, repo)
	trunkHead := revParse(t, td, repo, "HEAD")
	adoptRunGit(t, repo, "checkout", "-b", "feature-x")
	writeFileCommit(t, repo, "feature.txt", "feature work\n", "feature work")
	featureHead := revParse(t, td, repo, "HEAD")
	adoptRunGit(t, repo, "checkout", trunkBranch)

	id, err := tasks.ResolveRepositoryIdentity(td, repo)
	if err != nil {
		t.Fatalf("identity: %v", err)
	}
	wantDir := filepath.Join(ManagedWorktreesRoot(td), RepoKey(id), "scratch-20260729-1")

	b, err := ProvisionScratchWorktree(td, repo, "feature-x", now)
	if err != nil {
		t.Fatalf("provision scratch: %v", err)
	}
	if b.RuntimePath != wantDir {
		t.Errorf("RuntimePath = %q, want %q", b.RuntimePath, wantDir)
	}
	if want := "pop/scratch-20260729-1/20260729T103000Z"; b.Branch != want {
		t.Errorf("Branch = %q, want %q", b.Branch, want)
	}
	if !b.Provisioned {
		t.Errorf("Provisioned = false, want true for a managed worktree")
	}
	if head := revParse(t, td, b.RuntimePath, "HEAD"); head != featureHead {
		t.Errorf("scratch HEAD = %q, want fork at start-point %q (trunk HEAD is %q)", head, featureHead, trunkHead)
	}

	// A second scratch worktree the same day disambiguates to n=2.
	b2, err := ProvisionScratchWorktree(td, repo, "feature-x", now)
	if err != nil {
		t.Fatalf("provision second scratch: %v", err)
	}
	if want := filepath.Join(ManagedWorktreesRoot(td), RepoKey(id), "scratch-20260729-2"); b2.RuntimePath != want {
		t.Errorf("second RuntimePath = %q, want %q", b2.RuntimePath, want)
	}

	// Unbound from birth: no binding row references either worktree.
	all, err := AllBindings(td)
	if err != nil {
		t.Fatalf("list bindings: %v", err)
	}
	if len(all) != 0 {
		t.Errorf("bindings = %d, want zero rows for unbound scratch worktrees", len(all))
	}
}

// TestScratchWorktreeBindsAndFoldsThroughExistingPath drives the flow ADR-0152
// enables end to end: a set registered from inside a scratch worktree binds it
// as-is (no rename, no path reconciliation, no name-mismatch warning), and
// folding that set tears the worktree down through the existing
// reference-counted path.
func TestScratchWorktreeBindsAndFoldsThroughExistingPath(t *testing.T) {
	t.Parallel()
	repo := initAdoptRepo(t)
	td := lifecycleTestDeps(t)
	seedDoneTaskSet(t, td, repo, "docs-set")
	now := time.Date(2026, 7, 29, 10, 30, 0, 0, time.UTC)

	// Fork from the trunk's own branch — the base ref the picker's preselected
	// Enter accepts — even though that branch is checked out in the trunk.
	b, err := ProvisionScratchWorktree(td, repo, CurrentBranch(td, repo), now)
	if err != nil {
		t.Fatalf("provision scratch: %v", err)
	}

	cfg := &config.Config{Projects: []config.ProjectEntry{{Path: repo}}}
	var out bytes.Buffer
	res, err := BindWorktree(td, nil, cfg, "docs-set", b.RuntimePath, BindWorktreeOptions{}, LifecycleHooks{}, &out)
	if err != nil {
		t.Fatalf("bind-worktree: %v", err)
	}
	if res.RuntimePath != b.RuntimePath {
		t.Errorf("bound RuntimePath = %q, want the scratch path as-is %q", res.RuntimePath, b.RuntimePath)
	}
	if strings.Contains(strings.ToLower(out.String()), "warning") {
		t.Errorf("bind output should carry no name-mismatch warning, got %q", out.String())
	}

	_, stored, ok, err := FindBySetID(td, "docs-set")
	if err != nil || !ok {
		t.Fatalf("lookup binding: ok=%v err=%v", ok, err)
	}
	if stored.RuntimePath != b.RuntimePath {
		t.Errorf("stored RuntimePath = %q, want %q (the generated name is a label, never reconciled)", stored.RuntimePath, b.RuntimePath)
	}
	if !stored.Provisioned {
		t.Errorf("stored binding must be provisioned (managed-root checkout), got %+v", stored)
	}

	writeFileCommit(t, b.RuntimePath, "docs.txt", "docs\n", "docs work")

	got, err := Fold(td, nil, cfg, "docs-set", FoldOptions{Yes: true, In: tasks.NonInteractiveReader{}}, LifecycleHooks{}, io.Discard)
	if err != nil {
		t.Fatalf("fold: %v", err)
	}
	if !got.TornDown {
		t.Fatal("TornDown = false, want the reference-counted teardown to fire on fold")
	}
	if _, err := os.Stat(b.RuntimePath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("scratch worktree should be torn down, stat err = %v", err)
	}
}

func revParse(t *testing.T, td *tasks.Deps, dir, ref string) string {
	t.Helper()
	out, err := td.Git.CommandInDir(dir, "rev-parse", ref)
	if err != nil {
		t.Fatalf("git rev-parse %s in %s: %v", ref, dir, err)
	}
	return strings.TrimSpace(out)
}
