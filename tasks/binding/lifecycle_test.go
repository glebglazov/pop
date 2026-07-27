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
	return isolatedTasksDeps(t)
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
	t.Parallel()
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
	t.Parallel()
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
	t.Parallel()
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
	t.Parallel()
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
	t.Parallel()
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
	t.Parallel()
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
	t.Parallel()
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
	t.Parallel()
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

func lockedBySet(setID string) func(string) *tasks.RuntimeLockStatus {
	return func(runtimePath string) *tasks.RuntimeLockStatus {
		return &tasks.RuntimeLockStatus{
			Locked:   true,
			Metadata: &tasks.RuntimeLockMetadata{RuntimePath: runtimePath, SetID: setID},
		}
	}
}

// TestBindWorktreeSucceedsWhileOtherSetLocked: N sets can share one checkout
// (ADR-0115/0116); an unrelated set's drain must not block re-pointing set-A.
func TestBindWorktreeSucceedsWhileOtherSetLocked(t *testing.T) {
	t.Parallel()
	repo := initAdoptRepo(t)
	wt1 := addLinkedWorktree(t, repo, "branch-shared")
	wt2 := addLinkedWorktree(t, repo, "branch-new")
	td := lifecycleTestDeps(t)
	cfg := &config.Config{Projects: []config.ProjectEntry{{Path: repo}}}

	if _, err := BindWorktree(td, nil, cfg, "set-A", wt1, BindWorktreeOptions{}, LifecycleHooks{}, io.Discard); err != nil {
		t.Fatalf("initial bind: %v", err)
	}

	hooks := LifecycleHooks{ReadLock: lockedBySet("set-B")}
	got, err := BindWorktree(td, nil, cfg, "set-A", wt2, BindWorktreeOptions{Force: true}, hooks, io.Discard)
	if err != nil {
		t.Fatalf("bind while other set locked: %v", err)
	}
	if !got.Replaced {
		t.Fatalf("got.Replaced = false, want true (re-point)")
	}
}

// TestBindWorktreeRefusesWhileSameSetLocked: the refusal remains when set-A
// itself holds the live runtime execution lock.
func TestBindWorktreeRefusesWhileSameSetLocked(t *testing.T) {
	t.Parallel()
	repo := initAdoptRepo(t)
	wt1 := addLinkedWorktree(t, repo, "branch-self")
	wt2 := addLinkedWorktree(t, repo, "branch-self-new")
	td := lifecycleTestDeps(t)
	cfg := &config.Config{Projects: []config.ProjectEntry{{Path: repo}}}

	if _, err := BindWorktree(td, nil, cfg, "set-A", wt1, BindWorktreeOptions{}, LifecycleHooks{}, io.Discard); err != nil {
		t.Fatalf("initial bind: %v", err)
	}

	hooks := LifecycleHooks{ReadLock: lockedBySet("set-A")}
	_, err := BindWorktree(td, nil, cfg, "set-A", wt2, BindWorktreeOptions{Force: true}, hooks, io.Discard)
	if err == nil || !strings.Contains(err.Error(), "currently executing") {
		t.Fatalf("err = %v, want currently-executing refusal", err)
	}
}

// TestUnbindSucceedsWhileOtherSetLocked: unbinding set-A only deletes set-A's
// binding row; an unrelated set-B drain on the shared checkout is untouched.
func TestUnbindSucceedsWhileOtherSetLocked(t *testing.T) {
	t.Parallel()
	repo := initAdoptRepo(t)
	wt := addLinkedWorktree(t, repo, "set-shared")
	td := lifecycleTestDeps(t)
	seedLifecycleBinding(t, td, repo, "set-A", Binding{
		RuntimePath: wt,
		Branch:      "set-shared",
		Project:     filepath.Base(repo),
	})
	cfg := &config.Config{Projects: []config.ProjectEntry{{Path: repo}}}

	hooks := LifecycleHooks{ReadLock: lockedBySet("set-B")}
	if _, err := UnbindWorktree(td, nil, cfg, "set-A", UnbindWorktreeOptions{Yes: true, In: tasks.NonInteractiveReader{}}, hooks, io.Discard); err != nil {
		t.Fatalf("unbind while other set locked: %v", err)
	}
	if len(loadLifecycleBindings(t, td)) != 0 {
		t.Fatalf("binding should be released")
	}
}

// TestUnbindRefusesWhileSameSetLocked: the refusal remains when set-A itself
// holds the live runtime execution lock.
func TestUnbindRefusesWhileSameSetLocked(t *testing.T) {
	t.Parallel()
	repo := initAdoptRepo(t)
	wt := addLinkedWorktree(t, repo, "set-self")
	td := lifecycleTestDeps(t)
	seedLifecycleBinding(t, td, repo, "set-A", Binding{
		RuntimePath: wt,
		Branch:      "set-self",
		Project:     filepath.Base(repo),
	})
	cfg := &config.Config{Projects: []config.ProjectEntry{{Path: repo}}}

	hooks := LifecycleHooks{ReadLock: lockedBySet("set-A")}
	_, err := UnbindWorktree(td, nil, cfg, "set-A", UnbindWorktreeOptions{Yes: true, In: tasks.NonInteractiveReader{}}, hooks, io.Discard)
	if err == nil || !strings.Contains(err.Error(), "currently executing") {
		t.Fatalf("err = %v, want currently-executing refusal", err)
	}
	if len(loadLifecycleBindings(t, td)) != 1 {
		t.Fatalf("binding should be retained while set-A executes")
	}
}

func TestUnbindRefusesWhileBusy(t *testing.T) {
	t.Parallel()
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
	t.Parallel()
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
	t.Parallel()
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

// seedRegisteredSet registers setID under repo's repository with no worktree
// directive (an unbound, no-intent set) so the managed-bind write path can find
// it and flip the intent.
func seedRegisteredSet(t *testing.T, td *tasks.Deps, repo, setID string) string {
	t.Helper()
	id, err := tasks.ResolveRepositoryIdentity(td, repo)
	if err != nil {
		t.Fatalf("identity: %v", err)
	}
	defPath, err := tasks.CanonicalDefinitionPathWith(td, id.TasksDir)
	if err != nil {
		t.Fatalf("def path: %v", err)
	}
	err = tasks.UpdateGlobalStateWith(td, tasks.StatePathFor(defPath), func(s *tasks.GlobalState) error {
		s.Entry(defPath).TaskSets = append(s.Entry(defPath).TaskSets, tasks.RegisteredTaskSet{ID: setID})
		return nil
	})
	if err != nil {
		t.Fatalf("register set: %v", err)
	}
	return defPath
}

func managedIntentRecorded(t *testing.T, td *tasks.Deps, defPath, setID string) bool {
	t.Helper()
	intent, err := tasks.RegisteredWorktreeIntent(td, defPath, setID)
	if err != nil {
		t.Fatalf("read intent: %v", err)
	}
	return intent != nil && intent.Managed
}

// TestBindWorktreeManagedRecordsIntentOnUnboundSet covers acceptance criterion 1:
// bind-worktree --managed on an unbound set records a managed intent and adopts
// no checkout, leaving provisioning to the next Queue drain.
func TestBindWorktreeManagedRecordsIntentOnUnboundSet(t *testing.T) {
	t.Parallel()
	repo := initAdoptRepo(t)
	td := lifecycleTestDeps(t)
	defPath := seedRegisteredSet(t, td, repo, "set-m")
	cfg := &config.Config{Projects: []config.ProjectEntry{{Path: repo}}}

	var out bytes.Buffer
	got, err := BindWorktree(td, nil, cfg, "set-m", repo, BindWorktreeOptions{Managed: true}, LifecycleHooks{}, &out)
	if err != nil {
		t.Fatalf("bind-worktree --managed: %v", err)
	}
	if got.Replaced {
		t.Fatalf("got.Replaced = true, want false for an unbound set")
	}
	if got.RuntimePath != "" {
		t.Fatalf("got.RuntimePath = %q, want empty (no checkout adopted)", got.RuntimePath)
	}
	if n := len(loadLifecycleBindings(t, td)); n != 0 {
		t.Fatalf("bindings = %d, want none (managed intent provisions lazily)", n)
	}
	if !managedIntentRecorded(t, td, defPath, "set-m") {
		t.Fatalf("managed intent was not recorded for set-m")
	}
	if !strings.Contains(out.String(), "managed") {
		t.Fatalf("output = %q, want mention of managed intent", out.String())
	}
}

// TestBindWorktreeManagedRefusesBoundWithoutForce covers the first half of
// acceptance criterion 2: --managed on a bound set refuses without --force,
// leaving the binding and the (absent) intent untouched.
func TestBindWorktreeManagedRefusesBoundWithoutForce(t *testing.T) {
	t.Parallel()
	repo := initAdoptRepo(t)
	wt := addLinkedWorktree(t, repo, "bound-branch")
	td := lifecycleTestDeps(t)
	defPath := seedRegisteredSet(t, td, repo, "set-m")
	seedLifecycleBinding(t, td, repo, "set-m", Binding{
		RuntimePath: wt,
		Branch:      "bound-branch",
		Project:     filepath.Base(repo),
	})
	cfg := &config.Config{Projects: []config.ProjectEntry{{Path: repo}}}

	_, err := BindWorktree(td, nil, cfg, "set-m", repo, BindWorktreeOptions{Managed: true}, LifecycleHooks{}, io.Discard)
	if err == nil || !strings.Contains(err.Error(), "--force") {
		t.Fatalf("err = %v, want force refusal", err)
	}
	if n := len(loadLifecycleBindings(t, td)); n != 1 {
		t.Fatalf("bindings = %d, want the original binding retained", n)
	}
	if managedIntentRecorded(t, td, defPath, "set-m") {
		t.Fatalf("intent must not be recorded on a refused bind")
	}
}

// TestBindWorktreeManagedForceDropsBindingRetainsCheckout covers the second half
// of acceptance criterion 2: with --force it drops the old binding forget-only
// (the checkout and branch stay on disk) and records the managed intent.
func TestBindWorktreeManagedForceDropsBindingRetainsCheckout(t *testing.T) {
	t.Parallel()
	repo := initAdoptRepo(t)
	wt := addLinkedWorktree(t, repo, "bound-branch")
	td := lifecycleTestDeps(t)
	defPath := seedRegisteredSet(t, td, repo, "set-m")
	seedLifecycleBinding(t, td, repo, "set-m", Binding{
		RuntimePath: wt,
		Branch:      "bound-branch",
		Project:     filepath.Base(repo),
	})
	cfg := &config.Config{Projects: []config.ProjectEntry{{Path: repo}}}

	got, err := BindWorktree(td, nil, cfg, "set-m", repo, BindWorktreeOptions{Managed: true, Force: true}, LifecycleHooks{}, io.Discard)
	if err != nil {
		t.Fatalf("forced bind-worktree --managed: %v", err)
	}
	if !got.Replaced {
		t.Fatalf("got.Replaced = false, want true")
	}
	if n := len(loadLifecycleBindings(t, td)); n != 0 {
		t.Fatalf("bindings = %d, want the old binding dropped", n)
	}
	if _, err := os.Stat(wt); err != nil {
		t.Fatalf("old checkout must be retained on disk: %v", err)
	}
	// Use td.Git rather than runGitOutput here: a fresh isolatedTasksDeps call
	// would use a different store dir and hide the just-written managed intent.
	if branch, err := td.Git.CommandInDir(repo, "branch", "--list", "bound-branch"); err != nil || strings.TrimSpace(branch) == "" {
		t.Fatalf("old branch should still exist after forget-only drop (err=%v)", err)
	}
	if !managedIntentRecorded(t, td, defPath, "set-m") {
		t.Fatalf("managed intent was not recorded after forced re-point")
	}
}

// TestBindWorktreeManagedSucceedsWhileOtherSetLocked covers acceptance criterion
// 3: an unrelated set's drain holding the old checkout's runtime lock does not
// block the managed re-point (same-set-only guard, slice 02).
func TestBindWorktreeManagedSucceedsWhileOtherSetLocked(t *testing.T) {
	t.Parallel()
	repo := initAdoptRepo(t)
	wt := addLinkedWorktree(t, repo, "shared-branch")
	td := lifecycleTestDeps(t)
	defPath := seedRegisteredSet(t, td, repo, "set-A")
	seedLifecycleBinding(t, td, repo, "set-A", Binding{
		RuntimePath: wt,
		Branch:      "shared-branch",
		Project:     filepath.Base(repo),
	})
	cfg := &config.Config{Projects: []config.ProjectEntry{{Path: repo}}}

	hooks := LifecycleHooks{ReadLock: lockedBySet("set-B")}
	got, err := BindWorktree(td, nil, cfg, "set-A", repo, BindWorktreeOptions{Managed: true, Force: true}, hooks, io.Discard)
	if err != nil {
		t.Fatalf("managed bind while other set locked: %v", err)
	}
	if !got.Replaced {
		t.Fatalf("got.Replaced = false, want true")
	}
	if !managedIntentRecorded(t, td, defPath, "set-A") {
		t.Fatalf("managed intent was not recorded")
	}
}

// TestBindWorktreeManagedRefusesWhileSameSetLocked: the set's own live runtime
// lock still refuses the re-point.
func TestBindWorktreeManagedRefusesWhileSameSetLocked(t *testing.T) {
	t.Parallel()
	repo := initAdoptRepo(t)
	wt := addLinkedWorktree(t, repo, "self-branch")
	td := lifecycleTestDeps(t)
	defPath := seedRegisteredSet(t, td, repo, "set-A")
	seedLifecycleBinding(t, td, repo, "set-A", Binding{
		RuntimePath: wt,
		Branch:      "self-branch",
		Project:     filepath.Base(repo),
	})
	cfg := &config.Config{Projects: []config.ProjectEntry{{Path: repo}}}

	hooks := LifecycleHooks{ReadLock: lockedBySet("set-A")}
	_, err := BindWorktree(td, nil, cfg, "set-A", repo, BindWorktreeOptions{Managed: true, Force: true}, hooks, io.Discard)
	if err == nil || !strings.Contains(err.Error(), "currently executing") {
		t.Fatalf("err = %v, want currently-executing refusal", err)
	}
	if managedIntentRecorded(t, td, defPath, "set-A") {
		t.Fatalf("intent must not be recorded while the set executes")
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
