package binding

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/glebglazov/pop/internal/deps"
)

func TestRefreshCommitlessManagedBranchFastForwardsOntoTrunk(t *testing.T) {
	t.Parallel()
	td := routeTestDeps(t)
	repo := initAdoptRepo(t)
	now := time.Date(2026, 7, 28, 12, 0, 0, 0, time.UTC)
	b, err := ProvisionManagedBinding(ProvisionManagedBindingRequest{
		TD:           td,
		CheckoutPath: repo,
		SetID:        "lagging-set",
		Now:          now,
	})
	if err != nil {
		t.Fatalf("provision: %v", err)
	}
	forkSHA := strings.TrimSpace(runGitOutput(t, b.RuntimePath, "rev-parse", "HEAD"))

	if err := os.WriteFile(filepath.Join(repo, "trunk-only.txt"), []byte("trunk\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	adoptRunGit(t, repo, "add", "trunk-only.txt")
	adoptRunGit(t, repo, "commit", "-m", "trunk advance")
	trunkSHA := strings.TrimSpace(runGitOutput(t, repo, "rev-parse", "HEAD"))
	if forkSHA == trunkSHA {
		t.Fatalf("test setup: trunk must have advanced past fork %q", forkSHA)
	}
	if got := strings.TrimSpace(runGitOutput(t, b.RuntimePath, "rev-parse", "HEAD")); got != forkSHA {
		t.Fatalf("managed HEAD before refresh = %q, want fork %q", got, forkSHA)
	}

	if err := RefreshCommitlessManagedBranch(td, nil, repo, b); err != nil {
		t.Fatalf("refresh: %v", err)
	}
	if got := strings.TrimSpace(runGitOutput(t, b.RuntimePath, "rev-parse", "HEAD")); got != trunkSHA {
		t.Fatalf("managed HEAD after refresh = %q, want trunk %q", got, trunkSHA)
	}
	if _, err := os.Stat(filepath.Join(b.RuntimePath, "trunk-only.txt")); err != nil {
		t.Fatalf("managed worktree must include trunk file after fast-forward: %v", err)
	}
}

func TestRefreshCommitlessManagedBranchLeavesBranchWithOwnCommits(t *testing.T) {
	t.Parallel()
	td := routeTestDeps(t)
	repo := initAdoptRepo(t)
	now := time.Date(2026, 7, 28, 12, 0, 0, 0, time.UTC)
	b, err := ProvisionManagedBinding(ProvisionManagedBindingRequest{
		TD:           td,
		CheckoutPath: repo,
		SetID:        "working-set",
		Now:          now,
	})
	if err != nil {
		t.Fatalf("provision: %v", err)
	}

	if err := os.WriteFile(filepath.Join(b.RuntimePath, "managed.txt"), []byte("managed\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	adoptRunGit(t, b.RuntimePath, "add", "managed.txt")
	adoptRunGit(t, b.RuntimePath, "commit", "-m", "managed work")
	ownSHA := strings.TrimSpace(runGitOutput(t, b.RuntimePath, "rev-parse", "HEAD"))

	if err := os.WriteFile(filepath.Join(repo, "trunk-only.txt"), []byte("trunk\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	adoptRunGit(t, repo, "add", "trunk-only.txt")
	adoptRunGit(t, repo, "commit", "-m", "trunk advance")

	if err := RefreshCommitlessManagedBranch(td, nil, repo, b); err != nil {
		t.Fatalf("refresh: %v", err)
	}
	if got := strings.TrimSpace(runGitOutput(t, b.RuntimePath, "rev-parse", "HEAD")); got != ownSHA {
		t.Fatalf("managed HEAD = %q, want unchanged own commit %q", got, ownSHA)
	}
	if _, err := os.Stat(filepath.Join(b.RuntimePath, "trunk-only.txt")); !os.IsNotExist(err) {
		t.Fatalf("managed branch must not absorb trunk commits when it has its own work")
	}
}

func TestRefreshCommitlessManagedBranchSkipsNonFastForwardWithoutError(t *testing.T) {
	t.Parallel()
	td := routeTestDeps(t)
	repo := initAdoptRepo(t)
	now := time.Date(2026, 7, 28, 12, 0, 0, 0, time.UTC)
	b, err := ProvisionManagedBinding(ProvisionManagedBindingRequest{
		TD:           td,
		CheckoutPath: repo,
		SetID:        "stuck-set",
		Now:          now,
	})
	if err != nil {
		t.Fatalf("provision: %v", err)
	}
	before := strings.TrimSpace(runGitOutput(t, b.RuntimePath, "rev-parse", "HEAD"))

	inner := deps.NewRealGit()
	td.Git = &interceptGit{
		inner: inner,
		onCommandInDir: func(dir string, args ...string) (string, error) {
			if dir == b.RuntimePath && len(args) >= 3 && args[0] == "merge" && args[1] == "--ff-only" {
				return "", fmt.Errorf("not possible to fast-forward")
			}
			return inner.CommandInDir(dir, args...)
		},
	}

	if err := RefreshCommitlessManagedBranch(td, nil, repo, b); err != nil {
		t.Fatalf("refresh must not fail when ff-only is impossible: %v", err)
	}
	if got := strings.TrimSpace(runGitOutput(t, b.RuntimePath, "rev-parse", "HEAD")); got != before {
		t.Fatalf("managed HEAD = %q, want unchanged %q", got, before)
	}
}

func TestRefreshCommitlessManagedBranchSkipsDirtyWorktree(t *testing.T) {
	t.Parallel()
	td := routeTestDeps(t)
	repo := initAdoptRepo(t)
	now := time.Date(2026, 7, 28, 12, 0, 0, 0, time.UTC)
	b, err := ProvisionManagedBinding(ProvisionManagedBindingRequest{
		TD:           td,
		CheckoutPath: repo,
		SetID:        "dirty-set",
		Now:          now,
	})
	if err != nil {
		t.Fatalf("provision: %v", err)
	}
	forkSHA := strings.TrimSpace(runGitOutput(t, b.RuntimePath, "rev-parse", "HEAD"))

	if err := os.WriteFile(filepath.Join(repo, "trunk-only.txt"), []byte("trunk\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	adoptRunGit(t, repo, "add", "trunk-only.txt")
	adoptRunGit(t, repo, "commit", "-m", "trunk advance")

	if err := os.WriteFile(filepath.Join(b.RuntimePath, "dirty.txt"), []byte("wip\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	mergeCalls := 0
	inner := deps.NewRealGit()
	td.Git = &interceptGit{
		inner: inner,
		onCommandInDir: func(dir string, args ...string) (string, error) {
			if dir == b.RuntimePath && len(args) >= 2 && args[0] == "merge" {
				mergeCalls++
			}
			return inner.CommandInDir(dir, args...)
		},
	}

	if err := RefreshCommitlessManagedBranch(td, nil, repo, b); err != nil {
		t.Fatalf("refresh: %v", err)
	}
	if mergeCalls != 0 {
		t.Fatalf("dirty worktree must not be merged; merge calls = %d", mergeCalls)
	}
	if got := strings.TrimSpace(runGitOutput(t, b.RuntimePath, "rev-parse", "HEAD")); got != forkSHA {
		t.Fatalf("managed HEAD = %q, want unchanged fork %q", got, forkSHA)
	}
	if _, err := os.Stat(filepath.Join(b.RuntimePath, "dirty.txt")); err != nil {
		t.Fatalf("dirty file must remain: %v", err)
	}
}

func TestRefreshCommitlessManagedBranchNoopForAdoptedBinding(t *testing.T) {
	t.Parallel()
	td := routeTestDeps(t)
	repo := initAdoptRepo(t)
	wt := addLinkedWorktree(t, repo, "adopted")
	b := Adopt(wt, "adopted", "")

	mergeCalls := 0
	inner := deps.NewRealGit()
	td.Git = &interceptGit{
		inner: inner,
		onCommandInDir: func(dir string, args ...string) (string, error) {
			if len(args) >= 2 && args[0] == "merge" {
				mergeCalls++
			}
			return inner.CommandInDir(dir, args...)
		},
	}

	if err := RefreshCommitlessManagedBranch(td, nil, repo, b); err != nil {
		t.Fatalf("refresh: %v", err)
	}
	if mergeCalls != 0 {
		t.Fatalf("adopted binding must not refresh; merge calls = %d", mergeCalls)
	}
}
