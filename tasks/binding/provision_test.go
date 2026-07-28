package binding

import (
	"fmt"
	"testing"
	"time"

	"github.com/glebglazov/pop/internal/deps"
	"github.com/glebglazov/pop/tasks"
)

type failSecondWorktreeGit struct {
	deps.Git
	addCalls int
}

func (g *failSecondWorktreeGit) CommandInDir(dir string, args ...string) (string, error) {
	if len(args) >= 2 && args[0] == "worktree" && args[1] == "add" {
		g.addCalls++
		if g.addCalls > 1 {
			return "", fmt.Errorf("simulated worktree add failure")
		}
	}
	return g.Git.CommandInDir(dir, args...)
}

func TestProvisionManagedBindingRollbackOnPartialFailure(t *testing.T) {
	t.Parallel()
	td := routeTestDeps(t)
	repo := initAdoptRepo(t)
	inner := td.Git
	td.Git = &failSecondWorktreeGit{Git: inner}

	now := time.Date(2026, 7, 28, 12, 0, 0, 0, time.UTC)
	if _, err := ProvisionManagedBinding(ProvisionManagedBindingRequest{
		TD:           td,
		CheckoutPath: repo,
		SetID:        "alpha",
		Now:          now,
	}); err != nil {
		t.Fatalf("first provision: %v", err)
	}
	if _, err := ProvisionManagedBinding(ProvisionManagedBindingRequest{
		TD:           td,
		CheckoutPath: repo,
		SetID:        "beta",
		Now:          now,
	}); err == nil {
		t.Fatal("expected second provision to fail")
	}
	if _, ok, err := Lookup(td, Key(mustRepoID(t, td, repo), "alpha")); err != nil || !ok {
		t.Fatalf("first binding should remain after second failure: ok=%v err=%v", ok, err)
	}
	if _, ok, err := Lookup(td, Key(mustRepoID(t, td, repo), "beta")); err != nil || ok {
		t.Fatalf("second binding must not exist after failure: ok=%v err=%v", ok, err)
	}
}

func mustRepoID(t *testing.T, td *tasks.Deps, checkout string) *tasks.RepositoryIdentity {
	t.Helper()
	id, err := tasks.ResolveRepositoryIdentity(td, checkout)
	if err != nil {
		t.Fatal(err)
	}
	return id
}
