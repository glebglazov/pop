package binding

import (
	"bytes"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/glebglazov/pop/config"
	"github.com/glebglazov/pop/tasks"
)

// TestClassifyManagedWorktreeMarksUnboundAfterFoldReleasesLastReferent proves
// the Worktree picker's "next open" guarantee (ADR-0152): classification reads
// live binding state fresh rather than caching it, so a checkout that was
// ManagedBound goes ManagedUnbound the moment its last referent's binding is
// released — here, by a fold whose teardown offer is declined.
func TestClassifyManagedWorktreeMarksUnboundAfterFoldReleasesLastReferent(t *testing.T) {
	t.Parallel()
	repo := initAdoptRepo(t)
	td := lifecycleTestDeps(t)
	seedDoneTaskSet(t, td, repo, "set-marker")
	b, err := ProvisionManagedBinding(ProvisionManagedBindingRequest{
		TD: td, CheckoutPath: repo, SetID: "set-marker",
	})
	if err != nil {
		t.Fatalf("provision: %v", err)
	}
	writeFileCommit(t, b.RuntimePath, "feature.txt", "work\n", "set work")

	before, err := ClassifyManagedWorktree(td, b.RuntimePath)
	if err != nil {
		t.Fatalf("classify before fold: %v", err)
	}
	if before != ManagedBound {
		t.Fatalf("before fold: state = %v, want ManagedBound", before)
	}

	cfg := &config.Config{Projects: []config.ProjectEntry{{Path: repo}}}
	got, err := Fold(td, nil, cfg, "set-marker", FoldOptions{In: strings.NewReader("n\n")}, LifecycleHooks{}, io.Discard)
	if err != nil {
		t.Fatalf("fold decline teardown: %v", err)
	}
	if got.TornDown {
		t.Fatal("TornDown should be false when declined")
	}

	after, err := ClassifyManagedWorktree(td, b.RuntimePath)
	if err != nil {
		t.Fatalf("classify after fold: %v", err)
	}
	if after != ManagedUnbound {
		t.Fatalf("after fold: state = %v, want ManagedUnbound", after)
	}
}

// TestClassifyManagedWorktreeMarksUnboundWhenOnlyReferentIsArchived covers the
// other way a managed checkout loses its last live referent: the binding row
// survives, but the set it points at is archived, so nothing is working there
// any more (ADR-0152). Archiving must reach the marker exactly like folding.
func TestClassifyManagedWorktreeMarksUnboundWhenOnlyReferentIsArchived(t *testing.T) {
	t.Parallel()
	repo := initAdoptRepo(t)
	td := lifecycleTestDeps(t)
	defPath := seedDoneTaskSet(t, td, repo, "set-archived")
	b, err := ProvisionManagedBinding(ProvisionManagedBindingRequest{
		TD: td, CheckoutPath: repo, SetID: "set-archived",
	})
	if err != nil {
		t.Fatalf("provision: %v", err)
	}

	before, err := ClassifyManagedWorktree(td, b.RuntimePath)
	if err != nil {
		t.Fatalf("classify before archive: %v", err)
	}
	if before != ManagedBound {
		t.Fatalf("before archive: state = %v, want ManagedBound", before)
	}

	if err := tasks.SetTaskSetArchived(td, defPath, []string{"set-archived"}, true); err != nil {
		t.Fatalf("archive set: %v", err)
	}

	after, err := ClassifyManagedWorktree(td, b.RuntimePath)
	if err != nil {
		t.Fatalf("classify after archive: %v", err)
	}
	if after != ManagedUnbound {
		t.Fatalf("after archive: state = %v, want ManagedUnbound", after)
	}
}

func TestBindWorktreeRebindLastManagedReferentPromptsAndDeletes(t *testing.T) {
	t.Parallel()
	repo := initAdoptRepo(t)
	td := lifecycleTestDeps(t)
	managed := seedManagedBindingAtRoot(t, td, repo, "set-a")
	newWT := addLinkedWorktree(t, repo, "rebind-target")
	cfg := &config.Config{Projects: []config.ProjectEntry{{Path: repo}}}

	var out bytes.Buffer
	got, err := BindWorktree(td, nil, cfg, "set-a", newWT, BindWorktreeOptions{
		Force: true,
		In:    strings.NewReader("y\n"),
	}, LifecycleHooks{}, &out)
	if err != nil {
		t.Fatalf("forced rebind: %v", err)
	}
	if !got.Replaced || got.RuntimePath != newWT {
		t.Fatalf("got = %+v, want rebind to %q", got, newWT)
	}
	if _, err := os.Stat(managed.RuntimePath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("managed worktree should be removed, stat err = %v", err)
	}
	if branch := runGitOutput(t, repo, "branch", "--list", managed.Branch); strings.TrimSpace(branch) != "" {
		t.Fatalf("managed branch should be deleted")
	}
}

func TestBindWorktreeRebindSharedManagedCheckoutSkipsTeardown(t *testing.T) {
	t.Parallel()
	repo := initAdoptRepo(t)
	td := lifecycleTestDeps(t)
	managed := seedManagedBindingAtRoot(t, td, repo, "set-a")
	seedLifecycleBinding(t, td, repo, "set-b", Binding{
		RuntimePath: managed.RuntimePath,
		Branch:      managed.Branch,
		Project:     filepath.Base(repo),
		Provisioned: false,
	})
	newWT := addLinkedWorktree(t, repo, "rebind-target")
	cfg := &config.Config{Projects: []config.ProjectEntry{{Path: repo}}}

	_, err := BindWorktree(td, nil, cfg, "set-a", newWT, BindWorktreeOptions{
		Force: true,
		In:    tasks.NonInteractiveReader{},
	}, LifecycleHooks{}, io.Discard)
	if err != nil {
		t.Fatalf("forced rebind with shared checkout: %v", err)
	}
	if _, err := os.Stat(managed.RuntimePath); err != nil {
		t.Fatalf("shared managed worktree must remain: %v", err)
	}
}

func TestBindWorktreeRebindDeclineRetainsCheckout(t *testing.T) {
	t.Parallel()
	repo := initAdoptRepo(t)
	td := lifecycleTestDeps(t)
	managed := seedManagedBindingAtRoot(t, td, repo, "set-a")
	newWT := addLinkedWorktree(t, repo, "rebind-target")
	cfg := &config.Config{Projects: []config.ProjectEntry{{Path: repo}}}

	got, err := BindWorktree(td, nil, cfg, "set-a", newWT, BindWorktreeOptions{
		Force: true,
		In:    strings.NewReader("n\n"),
	}, LifecycleHooks{}, io.Discard)
	if err != nil {
		t.Fatalf("declined teardown rebind: %v", err)
	}
	if !got.Replaced || got.RuntimePath != newWT {
		t.Fatalf("got = %+v, want rebind to %q", got, newWT)
	}
	if _, err := os.Stat(managed.RuntimePath); err != nil {
		t.Fatalf("managed worktree must remain after decline: %v", err)
	}
}

func TestBindWorktreeRebindYesSkipsPromptAndDeletes(t *testing.T) {
	t.Parallel()
	repo := initAdoptRepo(t)
	td := lifecycleTestDeps(t)
	managed := seedManagedBindingAtRoot(t, td, repo, "set-a")
	newWT := addLinkedWorktree(t, repo, "rebind-target")
	cfg := &config.Config{Projects: []config.ProjectEntry{{Path: repo}}}

	_, err := BindWorktree(td, nil, cfg, "set-a", newWT, BindWorktreeOptions{
		Force: true,
		Yes:     true,
		In:      tasks.NonInteractiveReader{},
	}, LifecycleHooks{}, io.Discard)
	if err != nil {
		t.Fatalf("rebind --yes: %v", err)
	}
	if _, err := os.Stat(managed.RuntimePath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("managed worktree should be removed with --yes")
	}
}
