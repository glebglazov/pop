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
