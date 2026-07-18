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
	"github.com/glebglazov/pop/internal/deps"
	"github.com/glebglazov/pop/project"
	"github.com/glebglazov/pop/tasks"
)

func lifecycleTestDeps(t *testing.T) *tasks.Deps {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("XDG_DATA_HOME", dir)
	return &tasks.Deps{FS: routeTestDeps(t).FS, Git: routeTestDeps(t).Git}
}

func seedLifecycleBinding(t *testing.T, td *tasks.Deps, repoPath, setID string, b Binding) string {
	t.Helper()
	id, err := tasks.ResolveRepositoryIdentity(td, repoPath)
	if err != nil {
		t.Fatalf("identity: %v", err)
	}
	key := Key(id, setID)
	if err := Put(td, key, b); err != nil {
		t.Fatalf("save: %v", err)
	}
	return key
}

func loadLifecycleBindings(t *testing.T, td *tasks.Deps) map[string]Binding {
	t.Helper()
	all, err := AllBindings(td)
	if err != nil {
		t.Fatal(err)
	}
	return all
}

func TestUnbindAdoptedRetainsCheckout(t *testing.T) {
	repo := initAdoptRepo(t)
	wt := addLinkedWorktree(t, repo, "adopted-branch")
	td := lifecycleTestDeps(t)
	seedLifecycleBinding(t, td, repo, "set-a", Binding{
		RuntimePath: wt,
		Branch:      "adopted-branch",
		Project:     filepath.Base(repo),
		Provisioned: false,
	})
	cfg := &config.Config{Projects: []config.ProjectEntry{{Path: repo}}}

	var out bytes.Buffer
	got, err := UnbindWorktree(td, nil, cfg, "set-a", UnbindWorktreeOptions{Yes: true, In: tasks.NonInteractiveReader{}}, LifecycleHooks{}, &out)
	if err != nil {
		t.Fatalf("unbind: %v", err)
	}
	if got.Noop {
		t.Fatalf("result = %+v, want success", got)
	}
	if _, err := os.Stat(wt); err != nil {
		t.Fatalf("adopted worktree must be retained: %v", err)
	}
	if branch := runGitOutput(t, repo, "branch", "--list", "adopted-branch"); strings.TrimSpace(branch) == "" {
		t.Fatalf("adopted branch should still exist after unbind")
	}
	if len(loadLifecycleBindings(t, td)) != 0 {
		t.Fatalf("binding = %+v, want cleared", loadLifecycleBindings(t, td))
	}
	if !strings.Contains(out.String(), "retained") {
		t.Fatalf("output = %q, want mention of retained checkout", out.String())
	}
}

func TestUnbindProvisionedRetainsCheckout(t *testing.T) {
	repo := initAdoptRepo(t)
	wt := addLinkedWorktree(t, repo, "provisioned-branch")
	td := lifecycleTestDeps(t)
	seedLifecycleBinding(t, td, repo, "set-p", Binding{
		RuntimePath: wt,
		Branch:      "provisioned-branch",
		Project:     filepath.Base(repo),
		Provisioned: true,
	})
	cfg := &config.Config{Projects: []config.ProjectEntry{{Path: repo}}}

	var out bytes.Buffer
	got, err := UnbindWorktree(td, nil, cfg, "set-p", UnbindWorktreeOptions{Yes: true, In: tasks.NonInteractiveReader{}}, LifecycleHooks{}, &out)
	if err != nil {
		t.Fatalf("unbind: %v", err)
	}
	if got.Noop {
		t.Fatalf("result = %+v, want success", got)
	}
	if _, err := os.Stat(wt); err != nil {
		t.Fatalf("provisioned worktree must be retained: %v", err)
	}
	if branch := runGitOutput(t, repo, "branch", "--list", "provisioned-branch"); strings.TrimSpace(branch) == "" {
		t.Fatalf("provisioned branch should still exist after unbind")
	}
	if len(loadLifecycleBindings(t, td)) != 0 {
		t.Fatalf("binding = %+v, want cleared", loadLifecycleBindings(t, td))
	}
	if !strings.Contains(out.String(), "retained") {
		t.Fatalf("output = %q, want mention of retained checkout", out.String())
	}
	if strings.Contains(out.String(), "removed worktree") {
		t.Fatalf("output = %q, must not claim worktree removal", out.String())
	}
}

func TestTeardownManagedWorktreeRemovesCheckoutAndBranch(t *testing.T) {
	repo := initAdoptRepo(t)
	wt := addLinkedWorktree(t, repo, "managed-teardown")
	td := lifecycleTestDeps(t)
	b := Binding{
		RuntimePath: wt,
		Branch:      "managed-teardown",
		Project:     filepath.Base(repo),
		Provisioned: true,
	}
	cfg := &config.Config{Projects: []config.ProjectEntry{{Path: repo}}}

	if err := TeardownManagedWorktree(td, nil, cfg, b, LifecycleHooks{}); err != nil {
		t.Fatalf("TeardownManagedWorktree: %v", err)
	}
	if _, err := os.Stat(wt); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("managed worktree should be removed, stat err = %v", err)
	}
	if branch := runGitOutput(t, repo, "branch", "--list", "managed-teardown"); strings.TrimSpace(branch) != "" {
		t.Fatalf("managed branch should be deleted, still have %q", branch)
	}
}

func TestBindWorktreeCreatesAdoptedBinding(t *testing.T) {
	repo := initAdoptRepo(t)
	wt := addLinkedWorktree(t, repo, "my-branch")
	td := lifecycleTestDeps(t)
	cfg := &config.Config{Projects: []config.ProjectEntry{{Path: repo}}}

	var out bytes.Buffer
	got, err := BindWorktree(td, nil, cfg, "set-x", wt, BindWorktreeOptions{}, LifecycleHooks{}, &out)
	if err != nil {
		t.Fatalf("bind-worktree: %v", err)
	}
	if got.SetID != "set-x" || got.RuntimePath != wt || got.Branch != "my-branch" {
		t.Fatalf("result = %+v, want set-x@%s branch my-branch", got, wt)
	}

	bindings := loadLifecycleBindings(t, td)
	if len(bindings) == 0 {
		t.Fatalf("no bindings written")
	}
	var binding Binding
	for _, b := range bindings {
		binding = b
	}
	if binding.RuntimePath != wt || binding.Branch != "my-branch" || binding.Provisioned {
		t.Fatalf("binding = %+v, want adopted checkout", binding)
	}
	if !strings.Contains(out.String(), "Bound") {
		t.Fatalf("output = %q, want bind confirmation", out.String())
	}
}

// TestBindWorktreeProjectNameSkipsDetect verifies that a supplied ProjectName is
// used verbatim as the binding's Project label and that DetectProject's
// per-project git fan-out is never invoked (ADR-0060). The project deps carry a
// spy git that fails the test if touched.
func TestBindWorktreeProjectNameSkipsDetect(t *testing.T) {
	repo := initAdoptRepo(t)
	wt := addLinkedWorktree(t, repo, "explicit-branch")
	td := lifecycleTestDeps(t)
	// A glob-heavy config would make DetectProject fan out; ProjectName must
	// short-circuit it entirely, so pd's git is never called.
	cfg := &config.Config{Projects: []config.ProjectEntry{{Path: filepath.Join(t.TempDir(), "*", "*")}}}
	pd := &project.Deps{Git: &deps.MockGit{
		CommandFunc: func(args ...string) (string, error) {
			t.Fatalf("DetectProject fan-out must not run when ProjectName is supplied; git called with %v", args)
			return "", nil
		},
		CommandInDirFunc: func(dir string, args ...string) (string, error) {
			t.Fatalf("DetectProject fan-out must not run when ProjectName is supplied; git -C %s %v", dir, args)
			return "", nil
		},
	}}

	got, err := BindWorktree(td, pd, cfg, "set-e", wt, BindWorktreeOptions{ProjectName: "explicit-name"}, LifecycleHooks{}, io.Discard)
	if err != nil {
		t.Fatalf("bind-worktree: %v", err)
	}
	if got.SetID != "set-e" {
		t.Fatalf("result = %+v, want set-e", got)
	}

	bindings := loadLifecycleBindings(t, td)
	var binding Binding
	for _, b := range bindings {
		binding = b
	}
	if binding.Project != "explicit-name" {
		t.Fatalf("binding.Project = %q, want explicit-name (supplied, not DetectProject)", binding.Project)
	}
}

// TestBindWorktreeEmptyProjectNameFallsBack verifies the cwd-based path (empty
// ProjectName) still resolves the label via DetectProject.
func TestBindWorktreeEmptyProjectNameFallsBack(t *testing.T) {
	repo := initAdoptRepo(t)
	wt := addLinkedWorktree(t, repo, "fallback-branch")
	td := lifecycleTestDeps(t)
	cfg := &config.Config{Projects: []config.ProjectEntry{{Path: repo}}}
	pd := &project.Deps{FS: routeTestDeps(t).FS, Git: routeTestDeps(t).Git}

	got, err := BindWorktree(td, pd, cfg, "set-f", wt, BindWorktreeOptions{}, LifecycleHooks{}, io.Discard)
	if err != nil {
		t.Fatalf("bind-worktree: %v", err)
	}
	if got.SetID != "set-f" {
		t.Fatalf("result = %+v, want set-f", got)
	}

	bindings := loadLifecycleBindings(t, td)
	var binding Binding
	for _, b := range bindings {
		binding = b
	}
	if binding.Project != filepath.Base(repo) {
		t.Fatalf("binding.Project = %q, want %q (via DetectProject)", binding.Project, filepath.Base(repo))
	}
}

func TestBindWorktreeRefusesAlreadyBoundWithoutForce(t *testing.T) {
	repo := initAdoptRepo(t)
	wt1 := addLinkedWorktree(t, repo, "branch-1")
	wt2 := addLinkedWorktree(t, repo, "branch-2")
	td := lifecycleTestDeps(t)
	cfg := &config.Config{Projects: []config.ProjectEntry{{Path: repo}}}

	if _, err := BindWorktree(td, nil, cfg, "set-y", wt1, BindWorktreeOptions{}, LifecycleHooks{}, io.Discard); err != nil {
		t.Fatalf("initial bind: %v", err)
	}

	_, err := BindWorktree(td, nil, cfg, "set-y", wt2, BindWorktreeOptions{Force: false}, LifecycleHooks{}, io.Discard)
	if err == nil || !strings.Contains(err.Error(), "--force") {
		t.Fatalf("err = %v, want force refusal", err)
	}

	afterBindings := loadLifecycleBindings(t, td)
	var found bool
	for _, b := range afterBindings {
		if b.RuntimePath == wt1 {
			found = true
		}
	}
	if !found {
		t.Fatalf("bindings = %+v, want wt1 still bound", afterBindings)
	}

	var out bytes.Buffer
	got, err := BindWorktree(td, nil, cfg, "set-y", wt2, BindWorktreeOptions{Force: true}, LifecycleHooks{}, &out)
	if err != nil {
		t.Fatalf("forced bind-worktree: %v", err)
	}
	if !got.Replaced {
		t.Fatalf("got.Replaced = false, want true")
	}
	afterBindings = loadLifecycleBindings(t, td)
	var foundWt2 bool
	for _, b := range afterBindings {
		if b.RuntimePath == wt2 {
			foundWt2 = true
		}
	}
	if !foundWt2 {
		t.Fatalf("bindings after force = %+v, want wt2", afterBindings)
	}
}

func TestBindWorktreeRefusesWhileLocked(t *testing.T) {
	repo := initAdoptRepo(t)
	wt1 := addLinkedWorktree(t, repo, "branch-locked")
	wt2 := addLinkedWorktree(t, repo, "branch-new")
	td := lifecycleTestDeps(t)
	cfg := &config.Config{Projects: []config.ProjectEntry{{Path: repo}}}

	if _, err := BindWorktree(td, nil, cfg, "set-locked", wt1, BindWorktreeOptions{}, LifecycleHooks{}, io.Discard); err != nil {
		t.Fatalf("initial bind: %v", err)
	}

	hooks := LifecycleHooks{
		ReadLock: func(runtimePath string) *tasks.RuntimeLockStatus {
			return &tasks.RuntimeLockStatus{Locked: true}
		},
	}
	_, err := BindWorktree(td, nil, cfg, "set-locked", wt2, BindWorktreeOptions{Force: true}, hooks, io.Discard)
	if err == nil || !strings.Contains(err.Error(), "refusing bind-worktree") {
		t.Fatalf("err = %v, want lock refusal", err)
	}
}

func TestUnbindRefusesWhileBusy(t *testing.T) {
	repo := initAdoptRepo(t)
	wt := addLinkedWorktree(t, repo, "set-busy")
	td := lifecycleTestDeps(t)
	seedLifecycleBinding(t, td, repo, "set-1", Binding{
		RuntimePath: wt,
		Branch:      "set-busy",
		Project:     filepath.Base(repo),
	})
	cfg := &config.Config{Projects: []config.ProjectEntry{{Path: repo}}}

	hooks := LifecycleHooks{
		ReadLock: func(runtimePath string) *tasks.RuntimeLockStatus {
			return &tasks.RuntimeLockStatus{Locked: true}
		},
	}
	_, err := UnbindWorktree(td, nil, cfg, "set-1", UnbindWorktreeOptions{Yes: true, In: tasks.NonInteractiveReader{}}, hooks, io.Discard)
	if err == nil || !strings.Contains(err.Error(), "refusing unbind") {
		t.Fatalf("err = %v, want refuse while busy", err)
	}
	if len(loadLifecycleBindings(t, td)) != 1 {
		t.Fatalf("binding should be retained while busy")
	}
}

func TestUnbindNoopWhenUnbound(t *testing.T) {
	td := lifecycleTestDeps(t)
	hooks := LifecycleHooks{
		ReadLock: func(runtimePath string) *tasks.RuntimeLockStatus {
			t.Fatalf("unbound unbind must not read runtime lock")
			return nil
		},
	}
	var out bytes.Buffer
	got, err := UnbindWorktree(td, nil, &config.Config{}, "set-1", UnbindWorktreeOptions{Yes: true, In: tasks.NonInteractiveReader{}}, hooks, &out)
	if err != nil {
		t.Fatalf("unbind: %v", err)
	}
	if !got.Noop || !strings.Contains(out.String(), "no worktree binding") {
		t.Fatalf("result = %+v output = %q, want noop", got, out.String())
	}
}

func TestUnbindNeedsConfirmUnlessYes(t *testing.T) {
	repo := initAdoptRepo(t)
	wt := addLinkedWorktree(t, repo, "set-done")
	td := lifecycleTestDeps(t)
	seedLifecycleBinding(t, td, repo, "set-1", Binding{
		RuntimePath: wt,
		Branch:      "set-done",
		Project:     filepath.Base(repo),
		Provisioned: true,
	})
	cfg := &config.Config{Projects: []config.ProjectEntry{{Path: repo}}}

	hooks := LifecycleHooks{
		NeedsConfirm: func(setID string, b Binding) (bool, error) {
			return true, nil
		},
	}

	_, err := UnbindWorktree(td, nil, cfg, "set-1", UnbindWorktreeOptions{In: tasks.NonInteractiveReader{}}, hooks, io.Discard)
	if err == nil || !strings.Contains(err.Error(), "requires --yes") {
		t.Fatalf("err = %v, want non-interactive confirmation refusal", err)
	}

	var declined bytes.Buffer
	got, err := UnbindWorktree(td, nil, cfg, "set-1", UnbindWorktreeOptions{In: strings.NewReader("n\n")}, hooks, &declined)
	if err != nil {
		t.Fatalf("declined unbind: %v", err)
	}
	if !got.Noop || !strings.Contains(declined.String(), "cancelled") {
		t.Fatalf("declined result = %+v output = %q", got, declined.String())
	}

	var confirmed bytes.Buffer
	got, err = UnbindWorktree(td, nil, cfg, "set-1", UnbindWorktreeOptions{In: strings.NewReader("y\n")}, hooks, &confirmed)
	if err != nil {
		t.Fatalf("confirmed unbind: %v", err)
	}
	if got.Noop {
		t.Fatalf("confirmed result = %+v, want success", got)
	}
	if len(loadLifecycleBindings(t, td)) != 0 {
		t.Fatalf("binding should be cleared after confirm")
	}
}

func runGitOutput(t *testing.T, dir string, args ...string) string {
	t.Helper()
	out, err := routeTestDeps(t).Git.CommandInDir(dir, args...)
	if err != nil {
		t.Fatalf("git -C %s %v: %v", dir, args, err)
	}
	return out
}
