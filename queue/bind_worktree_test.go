package queue

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

// TestBindingProvisionedDefault verifies that a WorktreeBinding written without
// Provisioned (e.g. from an older or hand-written state.json) is treated as
// adopted — teardown must never delete it.
func TestBindingProvisionedDefault(t *testing.T) {
	td := queueDataDeps(t)
	seedBindingStore(t, td, map[string]WorktreeBinding{
		"repo\x00set-1": {RuntimePath: "/some/path", Branch: "b"},
	})
	if bindingProvisioned(td, "repo\x00set-1") {
		t.Fatalf("absent Provisioned should be false (adopted/safe-by-default)")
	}
}

// TestBindingProvisionedTrue verifies pop-provisioned bindings are marked true.
func TestBindingProvisionedTrue(t *testing.T) {
	td := queueDataDeps(t)
	seedBindingStore(t, td, map[string]WorktreeBinding{
		"repo\x00set-1": {RuntimePath: "/p", Provisioned: true},
	})
	if !bindingProvisioned(td, "repo\x00set-1") {
		t.Fatalf("Provisioned:true should return true")
	}
}

// TestAbandonAdoptedRetainsCheckout verifies that abandoning an adopted binding
// forgets the association but leaves the directory and branch untouched.
func TestAbandonAdoptedRetainsCheckout(t *testing.T) {
	repo := initMergeabilityRepo(t)
	wt := filepath.Join(t.TempDir(), "adopted-wt")
	runGit(t, repo, "worktree", "add", "-b", "adopted-branch", wt, "HEAD")

	td := queueDataDeps(t)
	key := testScopedKey(t, repo, "set-a")
	if err := WriteDaemonState(td, &DaemonState{Version: 1}); err != nil {
		t.Fatalf("write state: %v", err)
	}
	seedBindingStore(t, td, map[string]WorktreeBinding{
		key: {
			RuntimePath: wt,
			Branch:      "adopted-branch",
			Project:     filepath.Base(repo),
			Provisioned: false, // adopted
		},
	})

	d := &Deps{Tasks: td}
	cfg := &config.Config{Projects: []config.ProjectEntry{{Path: repo}}}

	var out bytes.Buffer
	got, err := AbandonWithOptions(d, cfg, "set-a", &out, AbandonOptions{Yes: true, In: tasks.NonInteractiveReader{}})
	if err != nil {
		t.Fatalf("abandon: %v", err)
	}
	if got.Noop {
		t.Fatalf("result = %+v, want success", got)
	}

	// Directory must still exist
	if _, err := os.Stat(wt); err != nil {
		t.Fatalf("adopted worktree must be retained: %v", err)
	}
	// Branch must still exist
	if branch := runGitOutput(t, repo, "branch", "--list", "adopted-branch"); strings.TrimSpace(branch) == "" {
		t.Fatalf("adopted branch should still exist after abandon")
	}

	// Binding must be cleared from store
	if len(loadBindingStore(t, td)) != 0 {
		t.Fatalf("binding = %+v, want cleared", loadBindingStore(t, td))
	}

	// Output should mention "retained"
	if !strings.Contains(out.String(), "retained") {
		t.Fatalf("output = %q, want mention of retained checkout", out.String())
	}
}

// TestAbandonProvisionedTearsDown verifies that abandoning a provisioned binding
// still removes the worktree and branch.
func TestAbandonProvisionedTearsDown(t *testing.T) {
	repo := initMergeabilityRepo(t)
	wt := filepath.Join(t.TempDir(), "provisioned-wt")
	runGit(t, repo, "worktree", "add", "-b", "provisioned-branch", wt, "HEAD")

	td := queueDataDeps(t)
	key := testScopedKey(t, repo, "set-p")
	if err := WriteDaemonState(td, &DaemonState{Version: 1}); err != nil {
		t.Fatalf("write state: %v", err)
	}
	seedBindingStore(t, td, map[string]WorktreeBinding{
		key: {
			RuntimePath: wt,
			Branch:      "provisioned-branch",
			Project:     filepath.Base(repo),
			Provisioned: true,
		},
	})

	d := &Deps{Tasks: td}
	cfg := &config.Config{Projects: []config.ProjectEntry{{Path: repo}}}

	_, err := AbandonWithOptions(d, cfg, "set-p", io.Discard, AbandonOptions{Yes: true, In: tasks.NonInteractiveReader{}})
	if err != nil {
		t.Fatalf("abandon: %v", err)
	}

	// Directory must be removed
	if _, err := os.Stat(wt); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("provisioned worktree should be removed, stat err = %v", err)
	}
}

// TestBindWorktreeCreatesAdoptedBinding verifies that bind-worktree creates a
// Provisioned=false binding pointing to the current checkout.
func TestBindWorktreeCreatesAdoptedBinding(t *testing.T) {
	repo := initMergeabilityRepo(t)
	wt := filepath.Join(t.TempDir(), "my-checkout")
	runGit(t, repo, "worktree", "add", "-b", "my-branch", wt, "HEAD")

	td := queueDataDeps(t)
	d := &Deps{Tasks: td}
	cfg := &config.Config{Projects: []config.ProjectEntry{{Path: repo}}}

	var out bytes.Buffer
	got, err := BindWorktree(d, cfg, "set-x", wt, BindWorktreeOptions{}, &out)
	if err != nil {
		t.Fatalf("bind-worktree: %v", err)
	}
	if got.SetID != "set-x" || got.RuntimePath != wt || got.Branch != "my-branch" {
		t.Fatalf("result = %+v, want set-x@%s branch my-branch", got, wt)
	}

	bindings := loadBindingStore(t, td)
	if len(bindings) == 0 {
		t.Fatalf("no bindings written")
	}
	var binding WorktreeBinding
	for _, b := range bindings {
		binding = b
	}
	if binding.RuntimePath != wt {
		t.Fatalf("binding.RuntimePath = %q, want %q", binding.RuntimePath, wt)
	}
	if binding.Branch != "my-branch" {
		t.Fatalf("binding.Branch = %q, want my-branch", binding.Branch)
	}
	if binding.Provisioned {
		t.Fatalf("adopted binding must have Provisioned=false")
	}

	if !strings.Contains(out.String(), "Bound") {
		t.Fatalf("output = %q, want bind confirmation", out.String())
	}
}

// TestBindWorktreeRefusesAlreadyBoundWithoutForce verifies that re-pointing a
// set to a different path requires --force.
func TestBindWorktreeRefusesAlreadyBoundWithoutForce(t *testing.T) {
	repo := initMergeabilityRepo(t)
	wt1 := filepath.Join(t.TempDir(), "checkout-1")
	wt2 := filepath.Join(t.TempDir(), "checkout-2")
	runGit(t, repo, "worktree", "add", "-b", "branch-1", wt1, "HEAD")
	runGit(t, repo, "worktree", "add", "-b", "branch-2", wt2, "HEAD")

	td := queueDataDeps(t)
	d := &Deps{Tasks: td}
	cfg := &config.Config{Projects: []config.ProjectEntry{{Path: repo}}}

	// Establish initial binding via BindWorktree itself so key computation is consistent
	if _, err := BindWorktree(d, cfg, "set-y", wt1, BindWorktreeOptions{}, io.Discard); err != nil {
		t.Fatalf("initial bind: %v", err)
	}

	// Without --force: must refuse
	_, err := BindWorktree(d, cfg, "set-y", wt2, BindWorktreeOptions{Force: false}, io.Discard)
	if err == nil || !strings.Contains(err.Error(), "--force") {
		t.Fatalf("err = %v, want force refusal", err)
	}

	// Binding must be unchanged (still wt1)
	afterBindings := loadBindingStore(t, td)
	var found bool
	for _, b := range afterBindings {
		if b.RuntimePath == wt1 {
			found = true
		}
	}
	if !found {
		t.Fatalf("bindings = %+v, want wt1 still bound", afterBindings)
	}

	// With --force: succeeds and updates
	var out bytes.Buffer
	got, err := BindWorktree(d, cfg, "set-y", wt2, BindWorktreeOptions{Force: true}, &out)
	if err != nil {
		t.Fatalf("forced bind-worktree: %v", err)
	}
	if !got.Replaced {
		t.Fatalf("got.Replaced = false, want true")
	}
	if !strings.Contains(out.String(), "Bound") {
		t.Fatalf("output = %q, want bind message", out.String())
	}
	afterBindings = loadBindingStore(t, td)
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

// TestBindWorktreeRefusesWhileLocked verifies that bind-worktree refuses when
// the existing binding's runtime path holds a live execution lock.
func TestBindWorktreeRefusesWhileLocked(t *testing.T) {
	repo := initMergeabilityRepo(t)
	wt1 := filepath.Join(t.TempDir(), "checkout-locked")
	wt2 := filepath.Join(t.TempDir(), "checkout-new")
	runGit(t, repo, "worktree", "add", "-b", "branch-locked", wt1, "HEAD")
	runGit(t, repo, "worktree", "add", "-b", "branch-new", wt2, "HEAD")

	td := queueDataDeps(t)
	// First bind wt1 without a lock so the initial binding is established
	d0 := &Deps{Tasks: td}
	cfg := &config.Config{Projects: []config.ProjectEntry{{Path: repo}}}
	if _, err := BindWorktree(d0, cfg, "set-locked", wt1, BindWorktreeOptions{}, io.Discard); err != nil {
		t.Fatalf("initial bind: %v", err)
	}

	// Now set up deps with a live lock on wt1
	d := &Deps{
		Tasks: td,
		ReadLock: func(runtimePath string) *tasks.RuntimeLockStatus {
			return liveLock(runtimePath)
		},
	}

	// Even with --force, locked set must be refused
	_, err := BindWorktree(d, cfg, "set-locked", wt2, BindWorktreeOptions{Force: true}, io.Discard)
	if err == nil || !strings.Contains(err.Error(), "refusing bind-worktree") {
		t.Fatalf("err = %v, want lock refusal", err)
	}

	// Binding must remain pointing to wt1
	afterBindings := loadBindingStore(t, td)
	var foundWt1 bool
	for _, b := range afterBindings {
		if b.RuntimePath == wt1 {
			foundWt1 = true
		}
	}
	if !foundWt1 {
		t.Fatalf("binding = %+v, want wt1 unchanged", afterBindings)
	}
}

// TestIntegrateAdoptedBindingRetainsCheckout verifies that integrating a set
// with an adopted (Provisioned=false) binding merges but does not delete the
// worktree directory or the branch.
func TestIntegrateAdoptedBindingRetainsCheckout(t *testing.T) {
	repo := initMergeabilityRepo(t)
	wt := filepath.Join(t.TempDir(), "adopted-int")
	runGit(t, repo, "worktree", "add", "-b", "adopted-int-branch", wt, "HEAD")
	writeFile(t, filepath.Join(wt, "adopted.txt"), "adopted\n")
	runGit(t, wt, "add", "adopted.txt")
	runGit(t, wt, "commit", "-m", "adopted change")

	td := queueDataDeps(t)
	key := testScopedKey(t, repo, "set-adopted")
	seedMergeabilityStore(t, td, map[string]MergeabilityRecord{
		key: {
			Project:     filepath.Base(repo),
			RuntimePath: wt,
			SetID:       "set-adopted",
			Status:      MergeabilityClean,
		},
	})
	seedBindingStore(t, td, map[string]WorktreeBinding{
		key: {
			RuntimePath: wt,
			Branch:      "adopted-int-branch",
			Project:     filepath.Base(repo),
			Provisioned: false, // adopted
		},
	})

	d := &Deps{
		Tasks: td,
		AcquireRuntimeLock: func(runtimePath string) (runtimeLock, error) {
			return tasks.AcquireRuntimeLock(td, runtimePath, nil)
		},
	}
	cfg := &config.Config{Projects: []config.ProjectEntry{{Path: repo}}}

	var out bytes.Buffer
	got, err := Integrate(d, cfg, "set-adopted", &out)
	if err != nil {
		t.Fatalf("integrate: %v", err)
	}
	if got.Noop {
		t.Fatalf("result = %+v, want success", got)
	}

	// Merge must have landed
	if _, err := os.Stat(filepath.Join(repo, "adopted.txt")); err != nil {
		t.Fatalf("merged file missing from working branch: %v", err)
	}

	// Adopted checkout must be retained
	if _, err := os.Stat(wt); err != nil {
		t.Fatalf("adopted worktree must be retained after integrate: %v", err)
	}
	// Branch must be retained
	if branch := runGitOutput(t, repo, "branch", "--list", "adopted-int-branch"); strings.TrimSpace(branch) == "" {
		t.Fatalf("adopted branch should still exist after integrate")
	}

	// Binding and mergeability must be cleared from state
	if len(loadBindingStore(t, td)) != 0 {
		t.Fatalf("worktree bindings = %+v, want cleared", loadBindingStore(t, td))
	}
	if len(loadMergeabilityStore(t, td)) != 0 {
		t.Fatalf("mergeability = %+v, want cleared", loadMergeabilityStore(t, td))
	}

	// Output should mention "retained"
	if !strings.Contains(out.String(), "retained") {
		t.Fatalf("output = %q, want mention of retained checkout", out.String())
	}
}
