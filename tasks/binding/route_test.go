package binding

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/glebglazov/pop/config"
	"github.com/glebglazov/pop/internal/deps"
	"github.com/glebglazov/pop/tasks"
)

func routeTestDeps(t *testing.T) *tasks.Deps {
	t.Helper()
	t.Setenv("XDG_DATA_HOME", filepath.Join(t.TempDir(), "xdg"))
	return &tasks.Deps{FS: deps.NewRealFileSystem(), Git: deps.NewRealGit()}
}

func seedBinding(t *testing.T, td *tasks.Deps, checkoutPath, setID string, b Binding) {
	t.Helper()
	id, err := tasks.ResolveRepositoryIdentity(td, checkoutPath)
	if err != nil {
		t.Fatalf("identity: %v", err)
	}
	store := &Store{}
	store.Put(Key(id, setID), b)
	if err := Save(td, store); err != nil {
		t.Fatalf("save: %v", err)
	}
}

func TestRouteDrainCheckoutExistingBindingWins(t *testing.T) {
	td := routeTestDeps(t)
	repo := initAdoptRepo(t)
	wt := addLinkedWorktree(t, repo, "feature")
	seedBinding(t, td, wt, "set-a", Adopt(wt, "feature", "proj"))

	got, err := RouteDrainCheckout(RouteDrainCheckoutRequest{
		TD:              td,
		CurrentCheckout: repo,
		SetID:           "set-a",
		Trigger:         TriggerImplementForeground,
	})
	if err != nil {
		t.Fatalf("route: %v", err)
	}
	if !got.UsedExistingBinding || got.RuntimePath != wt {
		t.Fatalf("result = %+v, want existing binding at %q", got, wt)
	}
}

// TestRouteDrainCheckoutUnboundUsesCurrentCheckout asserts an unbound whole-set
// drain with no flags resolves to the current checkout and never provisions a
// managed worktree — routing has no `git worktree add` path (ADR-0052).
func TestRouteDrainCheckoutUnboundUsesCurrentCheckout(t *testing.T) {
	td := routeTestDeps(t)
	repo := initAdoptRepo(t)
	worktreeAddCalls := 0
	innerGit := deps.NewRealGit()
	td.Git = &interceptGit{
		inner: innerGit,
		onCommandInDir: func(dir string, args ...string) (string, error) {
			if len(args) >= 2 && args[0] == "worktree" && args[1] == "add" {
				worktreeAddCalls++
			}
			return innerGit.CommandInDir(dir, args...)
		},
	}

	got, err := RouteDrainCheckout(RouteDrainCheckoutRequest{
		TD:              td,
		CurrentCheckout: repo,
		SetID:           "set-with-spaces",
		Trigger:         TriggerImplementForeground,
	})
	if err != nil {
		t.Fatalf("route: %v", err)
	}
	currentRuntime, err := tasks.ResolveRuntimePathWith(td, repo, "")
	if err != nil {
		t.Fatalf("runtime: %v", err)
	}
	if got.RuntimePath != currentRuntime || got.UsedExistingBinding {
		t.Fatalf("result = %+v, want current checkout %q with no binding", got, currentRuntime)
	}
	if worktreeAddCalls != 0 {
		t.Fatalf("worktree add calls = %d, want 0 — routing must not provision", worktreeAddCalls)
	}
}

// TestRouteDrainCheckoutNoDirectiveForegroundBindsCurrentCheckout asserts a first
// no-directive foreground drain persists a default (adopted, never-delete) Worktree
// binding to the current checkout, recording its path and branch (ADR-0062).
func TestRouteDrainCheckoutNoDirectiveForegroundBindsCurrentCheckout(t *testing.T) {
	td := routeTestDeps(t)
	repo := initAdoptRepo(t)
	wt := addLinkedWorktree(t, repo, "feature")
	addCalls := countingGit(t, td)

	got, err := RouteDrainCheckout(RouteDrainCheckoutRequest{
		TD:              td,
		CurrentCheckout: wt,
		SetID:           "set-a",
		Trigger:         TriggerImplementForeground,
	})
	if err != nil {
		t.Fatalf("route: %v", err)
	}
	currentRuntime, err := tasks.ResolveRuntimePathWith(td, wt, "")
	if err != nil {
		t.Fatalf("runtime: %v", err)
	}
	if !got.BoundDefault || got.RuntimePath != currentRuntime {
		t.Fatalf("result = %+v, want default binding at %q", got, currentRuntime)
	}
	if got.Binding.Branch != "feature" {
		t.Fatalf("binding branch = %q, want %q", got.Binding.Branch, "feature")
	}
	if got.Binding.Provisioned {
		t.Fatalf("default binding must be adopted (Provisioned=false), got %+v", got.Binding)
	}
	if *addCalls != 0 {
		t.Fatalf("worktree add calls = %d, want 0 — default binding never provisions", *addCalls)
	}

	id, err := tasks.ResolveRepositoryIdentity(td, wt)
	if err != nil {
		t.Fatalf("identity: %v", err)
	}
	store, err := Load(td)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	b, ok := store.Get(Key(id, "set-a"))
	if !ok || b.RuntimePath != currentRuntime || b.Branch != "feature" {
		t.Fatalf("persisted binding = %+v ok=%v, want path %q branch feature", b, ok, currentRuntime)
	}
	if store.ShouldTeardown(Key(id, "set-a")) {
		t.Fatalf("default binding must never be torn down")
	}
}

// TestRouteDrainCheckoutNoDirectiveQueueBindsIntegrationTarget asserts a first
// no-directive headless Queue drain persists a default binding to the integration
// target the Queue routes into (the checkout it passed as the current checkout) —
// ADR-0062. The Queue resolves the integration target (non-bare: main worktree)
// before calling routing and feeds it as CurrentCheckout.
func TestRouteDrainCheckoutNoDirectiveQueueBindsIntegrationTarget(t *testing.T) {
	td := routeTestDeps(t)
	repo := initAdoptRepo(t)
	addCalls := countingGit(t, td)

	// The Queue routes the repo into its integration target. For a non-bare repo
	// that is the main worktree (trunk), which it passes as CurrentCheckout.
	got, err := RouteDrainCheckout(RouteDrainCheckoutRequest{
		TD:              td,
		CurrentCheckout: repo,
		SetID:           "set-a",
		Trigger:         TriggerQueueSpawn,
	})
	if err != nil {
		t.Fatalf("route: %v", err)
	}
	integrationTarget, err := tasks.ResolveRuntimePathWith(td, repo, "")
	if err != nil {
		t.Fatalf("runtime: %v", err)
	}
	if !got.BoundDefault || got.RuntimePath != integrationTarget {
		t.Fatalf("result = %+v, want default binding at integration target %q", got, integrationTarget)
	}
	if got.Binding.Provisioned {
		t.Fatalf("default binding must be adopted (Provisioned=false), got %+v", got.Binding)
	}
	if *addCalls != 0 {
		t.Fatalf("worktree add calls = %d, want 0 — default binding never provisions", *addCalls)
	}

	id, err := tasks.ResolveRepositoryIdentity(td, repo)
	if err != nil {
		t.Fatalf("identity: %v", err)
	}
	store, err := Load(td)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if b, ok := store.Get(Key(id, "set-a")); !ok || b.RuntimePath != integrationTarget {
		t.Fatalf("persisted binding = %+v ok=%v, want integration target %q", b, ok, integrationTarget)
	}
}

// TestRouteDrainCheckoutNoDirectiveSecondDrainResumesBoundCheckout asserts the
// second drain of a no-directive set resumes the checkout the first drain bound,
// not the (different) current cwd — the set is sticky to where it first ran
// (ADR-0062).
func TestRouteDrainCheckoutNoDirectiveSecondDrainResumesBoundCheckout(t *testing.T) {
	td := routeTestDeps(t)
	repo := initAdoptRepo(t)
	first := addLinkedWorktree(t, repo, "first")
	second := addLinkedWorktree(t, repo, "second")

	firstRes, err := RouteDrainCheckout(RouteDrainCheckoutRequest{
		TD:              td,
		CurrentCheckout: first,
		SetID:           "set-a",
		Trigger:         TriggerImplementForeground,
	})
	if err != nil {
		t.Fatalf("first route: %v", err)
	}
	if !firstRes.BoundDefault {
		t.Fatalf("first drain must persist a default binding, got %+v", firstRes)
	}

	// A later drain from a *different* checkout resumes the bound one, not the cwd.
	secondRes, err := RouteDrainCheckout(RouteDrainCheckoutRequest{
		TD:              td,
		CurrentCheckout: second,
		SetID:           "set-a",
		Trigger:         TriggerImplementForeground,
	})
	if err != nil {
		t.Fatalf("second route: %v", err)
	}
	if !secondRes.UsedExistingBinding {
		t.Fatalf("second drain must resume the existing binding, got %+v", secondRes)
	}
	if secondRes.RuntimePath != firstRes.RuntimePath {
		t.Fatalf("second runtime %q != first %q; must resume bound checkout not cwd", secondRes.RuntimePath, firstRes.RuntimePath)
	}
}

// TestRouteDrainCheckoutOperatorBindingWinsOverDefault asserts a pre-existing
// operator binding is resumed and the no-directive default never overwrites it
// (ADR-0062: bind/override consulted first).
func TestRouteDrainCheckoutOperatorBindingWinsOverDefault(t *testing.T) {
	td := routeTestDeps(t)
	repo := initAdoptRepo(t)
	bound := addLinkedWorktree(t, repo, "operator")
	seedBinding(t, td, repo, "set-a", Adopt(bound, "operator", "proj"))

	got, err := RouteDrainCheckout(RouteDrainCheckoutRequest{
		TD:              td,
		CurrentCheckout: repo, // cwd differs from the operator-bound checkout
		SetID:           "set-a",
		Trigger:         TriggerImplementForeground,
	})
	if err != nil {
		t.Fatalf("route: %v", err)
	}
	if !got.UsedExistingBinding || got.BoundDefault || got.RuntimePath != bound {
		t.Fatalf("result = %+v, want operator binding at %q to win", got, bound)
	}
}

// TestRouteDrainCheckoutOverrideWinsOverDefault asserts an explicit runtime-path
// override resolves to that checkout with no default binding persisted (ADR-0062
// precedence: override before the default step).
func TestRouteDrainCheckoutOverrideWinsOverDefault(t *testing.T) {
	td := routeTestDeps(t)
	repo := initAdoptRepo(t)
	override := addLinkedWorktree(t, repo, "override")

	got, err := RouteDrainCheckout(RouteDrainCheckoutRequest{
		TD:              td,
		CurrentCheckout: repo,
		SetID:           "set-a",
		Trigger:         TriggerImplementForeground,
		RuntimeOverride: override,
	})
	if err != nil {
		t.Fatalf("route: %v", err)
	}
	canonOverride, _ := filepath.EvalSymlinks(override)
	if got.BoundDefault || got.UsedExistingBinding || got.RuntimePath != canonOverride {
		t.Fatalf("result = %+v, want override checkout %q with no default binding", got, canonOverride)
	}

	id, err := tasks.ResolveRepositoryIdentity(td, repo)
	if err != nil {
		t.Fatalf("identity: %v", err)
	}
	store, err := Load(td)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if _, ok := store.Get(Key(id, "set-a")); ok {
		t.Fatalf("an explicit override must not persist a default binding")
	}
}

// seedManagedIntent registers setID under checkout's repository with a `managed`
// worktree directive (ADR-0059), the registration seed routing consults.
func seedManagedIntent(t *testing.T, td *tasks.Deps, checkout, setID string) {
	t.Helper()
	id, err := tasks.ResolveRepositoryIdentity(td, checkout)
	if err != nil {
		t.Fatalf("identity: %v", err)
	}
	defPath, err := tasks.CanonicalDefinitionPathWith(td, id.TasksDir)
	if err != nil {
		t.Fatalf("def path: %v", err)
	}
	err = tasks.UpdateGlobalStateWith(td, tasks.StatePathFor(defPath), func(s *tasks.GlobalState) error {
		s.Entry(defPath).TaskSets = []tasks.RegisteredTaskSet{
			{ID: setID, WorktreeIntent: &tasks.WorktreeDirective{Managed: true}},
		}
		return nil
	})
	if err != nil {
		t.Fatalf("seed intent: %v", err)
	}
}

// seedNamedIntent registers setID under checkout's repository with a `name`
// worktree directive (ADR-0059), the registration seed routing consults to adopt
// the named worktree.
func seedNamedIntent(t *testing.T, td *tasks.Deps, checkout, setID, name string) {
	t.Helper()
	id, err := tasks.ResolveRepositoryIdentity(td, checkout)
	if err != nil {
		t.Fatalf("identity: %v", err)
	}
	defPath, err := tasks.CanonicalDefinitionPathWith(td, id.TasksDir)
	if err != nil {
		t.Fatalf("def path: %v", err)
	}
	err = tasks.UpdateGlobalStateWith(td, tasks.StatePathFor(defPath), func(s *tasks.GlobalState) error {
		s.Entry(defPath).TaskSets = []tasks.RegisteredTaskSet{
			{ID: setID, WorktreeIntent: &tasks.WorktreeDirective{Name: name}},
		}
		return nil
	})
	if err != nil {
		t.Fatalf("seed intent: %v", err)
	}
}

// addNamedWorktree adds a linked worktree whose checkout basename — its
// operator-facing name — is exactly name, on a fresh branch, and returns its path.
func addNamedWorktree(t *testing.T, repo, name, branch string) string {
	t.Helper()
	wt := filepath.Join(t.TempDir(), name)
	adoptRunGit(t, repo, "worktree", "add", "-b", branch, wt, "HEAD")
	return wt
}

// countingGit wraps real git, tallying `git worktree add` invocations so a test
// can assert routing provisioned (or did not) without mocking git semantics.
func countingGit(t *testing.T, td *tasks.Deps) *int {
	t.Helper()
	calls := 0
	inner := deps.NewRealGit()
	td.Git = &interceptGit{
		inner: inner,
		onCommandInDir: func(dir string, args ...string) (string, error) {
			if len(args) >= 2 && args[0] == "worktree" && args[1] == "add" {
				calls++
			}
			return inner.CommandInDir(dir, args...)
		},
	}
	return &calls
}

// TestRouteDrainCheckoutManagedDirectiveProvisions asserts an unbound set
// carrying a `managed` directive forks a managed worktree from the Trunk
// worktree's HEAD, records a provisioned binding, and drains there — identically
// whether the trigger is foreground implement or a Queue spawn (ADR-0059).
func TestRouteDrainCheckoutManagedDirectiveProvisions(t *testing.T) {
	for _, tc := range []struct {
		name    string
		trigger DrainTrigger
	}{
		{"implement", TriggerImplementForeground},
		{"queue", TriggerQueueSpawn},
	} {
		t.Run(tc.name, func(t *testing.T) {
			td := routeTestDeps(t)
			repo := initAdoptRepo(t)
			seedManagedIntent(t, td, repo, "managed-set")
			addCalls := countingGit(t, td)
			now := time.Date(2026, 6, 24, 9, 8, 7, 0, time.UTC)

			got, err := RouteDrainCheckout(RouteDrainCheckoutRequest{
				TD:              td,
				Now:             now,
				CurrentCheckout: repo,
				SetID:           "managed-set",
				Trigger:         tc.trigger,
			})
			if err != nil {
				t.Fatalf("route: %v", err)
			}
			if !got.ProvisionedManaged || got.UsedExistingBinding {
				t.Fatalf("result = %+v, want a freshly provisioned managed worktree", got)
			}
			if *addCalls != 1 {
				t.Fatalf("worktree add calls = %d, want 1", *addCalls)
			}
			root := ManagedWorktreesRoot(td)
			if !strings.HasPrefix(got.RuntimePath, root) {
				t.Fatalf("provisioned worktree %q not under managed root %q", got.RuntimePath, root)
			}
			canonRepo, _ := filepath.EvalSymlinks(repo)
			if got.RuntimePath == canonRepo {
				t.Fatalf("provisioned worktree is the current checkout %q, want a distinct managed worktree", canonRepo)
			}
			if _, err := os.Stat(got.RuntimePath); err != nil {
				t.Fatalf("provisioned worktree missing on disk: %v", err)
			}
			if !strings.HasPrefix(got.Binding.Branch, "pop/managed-set/") {
				t.Fatalf("branch = %q, want pop/managed-set/ prefix", got.Binding.Branch)
			}
			// Forked from trunk: the new worktree's HEAD equals the Trunk worktree's.
			trunkHead, err := deps.NewRealGit().CommandInDir(repo, "rev-parse", "HEAD")
			if err != nil {
				t.Fatalf("trunk head: %v", err)
			}
			wtHead, err := deps.NewRealGit().CommandInDir(got.RuntimePath, "rev-parse", "HEAD")
			if err != nil {
				t.Fatalf("worktree head: %v", err)
			}
			if strings.TrimSpace(trunkHead) != strings.TrimSpace(wtHead) {
				t.Fatalf("worktree HEAD %q != trunk HEAD %q; not forked from trunk", wtHead, trunkHead)
			}

			// The binding is recorded as managed (provisioned) and resolvable.
			_, b, ok, err := GetForSet(td, repo, "managed-set")
			if err != nil {
				t.Fatalf("get for set: %v", err)
			}
			if !ok || !b.Provisioned || b.RuntimePath != got.RuntimePath {
				t.Fatalf("binding = %+v ok=%v, want provisioned at %q", b, ok, got.RuntimePath)
			}
		})
	}
}

// TestRouteDrainCheckoutManagedDirectiveSecondDrainResumes asserts a later drain
// of the same set resumes the binding the first drain recorded — the directive is
// consulted only when unbound, so no second worktree is provisioned (ADR-0059).
func TestRouteDrainCheckoutManagedDirectiveSecondDrainResumes(t *testing.T) {
	td := routeTestDeps(t)
	repo := initAdoptRepo(t)
	seedManagedIntent(t, td, repo, "managed-set")
	addCalls := countingGit(t, td)
	now := time.Date(2026, 6, 24, 9, 8, 7, 0, time.UTC)

	req := RouteDrainCheckoutRequest{
		TD:              td,
		Now:             now,
		CurrentCheckout: repo,
		SetID:           "managed-set",
		Trigger:         TriggerImplementForeground,
	}
	first, err := RouteDrainCheckout(req)
	if err != nil {
		t.Fatalf("first route: %v", err)
	}
	second, err := RouteDrainCheckout(req)
	if err != nil {
		t.Fatalf("second route: %v", err)
	}
	if !second.UsedExistingBinding || second.ProvisionedManaged {
		t.Fatalf("second route = %+v, want resumed existing binding", second)
	}
	if second.RuntimePath != first.RuntimePath {
		t.Fatalf("second runtime %q != first %q; resume must reuse the same worktree", second.RuntimePath, first.RuntimePath)
	}
	if *addCalls != 1 {
		t.Fatalf("worktree add calls = %d, want 1 — second drain must not provision again", *addCalls)
	}
}

// TestRouteDrainCheckoutBindingWinsOverManagedDirective asserts a pre-existing
// binding takes precedence over the directive: routing resumes the bound checkout
// and never consults the directive, so no managed worktree is provisioned.
func TestRouteDrainCheckoutBindingWinsOverManagedDirective(t *testing.T) {
	td := routeTestDeps(t)
	repo := initAdoptRepo(t)
	wt := addLinkedWorktree(t, repo, "feature")
	seedManagedIntent(t, td, repo, "managed-set")
	seedBinding(t, td, repo, "managed-set", Adopt(wt, "feature", "proj"))
	addCalls := countingGit(t, td)

	got, err := RouteDrainCheckout(RouteDrainCheckoutRequest{
		TD:              td,
		Now:             time.Date(2026, 6, 24, 9, 8, 7, 0, time.UTC),
		CurrentCheckout: repo,
		SetID:           "managed-set",
		Trigger:         TriggerImplementForeground,
	})
	if err != nil {
		t.Fatalf("route: %v", err)
	}
	if !got.UsedExistingBinding || got.ProvisionedManaged || got.RuntimePath != wt {
		t.Fatalf("result = %+v, want resumed binding at %q with no provisioning", got, wt)
	}
	if *addCalls != 0 {
		t.Fatalf("worktree add calls = %d, want 0 — a binding must shadow the directive", *addCalls)
	}
}

// TestRouteDrainCheckoutOverrideWinsOverManagedDirective asserts an explicit
// runtime-path override is honored before the directive is even read: routing
// resolves the override and provisions nothing (ADR-0059 precedence).
func TestRouteDrainCheckoutOverrideWinsOverManagedDirective(t *testing.T) {
	td := routeTestDeps(t)
	repo := initAdoptRepo(t)
	wt := addLinkedWorktree(t, repo, "override-target")
	seedManagedIntent(t, td, repo, "managed-set")
	addCalls := countingGit(t, td)

	got, err := RouteDrainCheckout(RouteDrainCheckoutRequest{
		TD:              td,
		Now:             time.Date(2026, 6, 24, 9, 8, 7, 0, time.UTC),
		CurrentCheckout: repo,
		SetID:           "managed-set",
		Trigger:         TriggerImplementForeground,
		RuntimeOverride: wt,
	})
	if err != nil {
		t.Fatalf("route: %v", err)
	}
	canonWT, _ := filepath.EvalSymlinks(wt)
	if got.ProvisionedManaged || got.UsedExistingBinding || got.RuntimePath != canonWT {
		t.Fatalf("result = %+v, want override checkout %q with no provisioning", got, canonWT)
	}
	if *addCalls != 0 {
		t.Fatalf("worktree add calls = %d, want 0 — an override must shadow the directive", *addCalls)
	}
}

// TestRouteDrainCheckoutNamedDirectiveAdopts asserts an unbound set carrying a
// `name` directive resolves the worktree of that name on this machine, records an
// adopted (never-delete) binding to it, and drains there — identically whether the
// trigger is foreground implement or a Queue spawn, and without routing ever
// running `git worktree add` (ADR-0059).
func TestRouteDrainCheckoutNamedDirectiveAdopts(t *testing.T) {
	for _, tc := range []struct {
		name    string
		trigger DrainTrigger
	}{
		{"implement", TriggerImplementForeground},
		{"queue", TriggerQueueSpawn},
	} {
		t.Run(tc.name, func(t *testing.T) {
			td := routeTestDeps(t)
			repo := initAdoptRepo(t)
			named := addNamedWorktree(t, repo, "feature-x", "feature-x")
			seedNamedIntent(t, td, repo, "named-set", "feature-x")
			addCalls := countingGit(t, td)

			got, err := RouteDrainCheckout(RouteDrainCheckoutRequest{
				TD:              td,
				CurrentCheckout: repo,
				SetID:           "named-set",
				Trigger:         tc.trigger,
			})
			if err != nil {
				t.Fatalf("route: %v", err)
			}
			if !got.AdoptedNamed || got.UsedExistingBinding || got.ProvisionedManaged {
				t.Fatalf("result = %+v, want a freshly adopted named worktree", got)
			}
			if *addCalls != 0 {
				t.Fatalf("worktree add calls = %d, want 0 — adopting an existing worktree never provisions", *addCalls)
			}
			if filepath.Base(got.RuntimePath) != "feature-x" {
				t.Fatalf("runtime %q, want a worktree named feature-x", got.RuntimePath)
			}
			canonNamed, _ := filepath.EvalSymlinks(named)
			canonGot, _ := filepath.EvalSymlinks(got.RuntimePath)
			if canonGot != canonNamed {
				t.Fatalf("runtime %q != named worktree %q", canonGot, canonNamed)
			}
			if _, err := os.Stat(got.RuntimePath); err != nil {
				t.Fatalf("named worktree missing on disk: %v", err)
			}

			// The binding is recorded as adopted (never-delete) and resolvable.
			key, b, ok, err := GetForSet(td, repo, "named-set")
			if err != nil {
				t.Fatalf("get for set: %v", err)
			}
			if !ok || b.Provisioned || b.RuntimePath != got.RuntimePath {
				t.Fatalf("binding = %+v ok=%v, want adopted at %q", b, ok, got.RuntimePath)
			}
			if b.Branch != "feature-x" {
				t.Fatalf("binding branch = %q, want feature-x", b.Branch)
			}
			// Adopted: unbinding leaves the checkout and branch on disk.
			if ShouldTeardown(td, key) {
				t.Fatalf("ShouldTeardown = true for an adopted binding; the checkout must never be torn down")
			}
		})
	}
}

// TestRouteDrainCheckoutNamedDirectiveUnbindLeavesCheckout asserts the adopted
// binding's never-delete semantics end to end: forgetting the binding (unbind)
// removes only the association — the worktree checkout and its branch stay on disk
// (ADR-0059, matching bind-worktree).
func TestRouteDrainCheckoutNamedDirectiveUnbindLeavesCheckout(t *testing.T) {
	td := routeTestDeps(t)
	repo := initAdoptRepo(t)
	named := addNamedWorktree(t, repo, "feature-x", "feature-x")
	seedNamedIntent(t, td, repo, "named-set", "feature-x")

	if _, err := RouteDrainCheckout(RouteDrainCheckoutRequest{
		TD:              td,
		CurrentCheckout: repo,
		SetID:           "named-set",
		Trigger:         TriggerImplementForeground,
	}); err != nil {
		t.Fatalf("route: %v", err)
	}

	key, _, ok, err := GetForSet(td, repo, "named-set")
	if err != nil || !ok {
		t.Fatalf("get for set: ok=%v err=%v", ok, err)
	}
	if err := Delete(td, key); err != nil { // unbind: forget the association only
		t.Fatalf("unbind: %v", err)
	}

	if _, err := os.Stat(named); err != nil {
		t.Fatalf("checkout removed by unbind: %v — adopted worktrees must survive", err)
	}
	out, err := deps.NewRealGit().CommandInDir(repo, "branch", "--list", "feature-x")
	if err != nil {
		t.Fatalf("branch list: %v", err)
	}
	if !strings.Contains(out, "feature-x") {
		t.Fatalf("branch feature-x gone after unbind: %q", out)
	}
}

// TestRouteDrainCheckoutNamedDirectiveSecondDrainResumes asserts a later drain of
// the same set resumes the adopted binding the first drain recorded; the directive
// is consulted only when unbound (ADR-0059).
func TestRouteDrainCheckoutNamedDirectiveSecondDrainResumes(t *testing.T) {
	td := routeTestDeps(t)
	repo := initAdoptRepo(t)
	addNamedWorktree(t, repo, "feature-x", "feature-x")
	seedNamedIntent(t, td, repo, "named-set", "feature-x")

	req := RouteDrainCheckoutRequest{
		TD:              td,
		CurrentCheckout: repo,
		SetID:           "named-set",
		Trigger:         TriggerImplementForeground,
	}
	first, err := RouteDrainCheckout(req)
	if err != nil {
		t.Fatalf("first route: %v", err)
	}
	second, err := RouteDrainCheckout(req)
	if err != nil {
		t.Fatalf("second route: %v", err)
	}
	if !second.UsedExistingBinding || second.AdoptedNamed {
		t.Fatalf("second route = %+v, want resumed existing binding", second)
	}
	if second.RuntimePath != first.RuntimePath {
		t.Fatalf("second runtime %q != first %q; resume must reuse the adopted worktree", second.RuntimePath, first.RuntimePath)
	}
}

// TestRouteDrainCheckoutBindingWinsOverNamedDirective asserts a pre-existing
// binding shadows the `name` directive: routing resumes the bound checkout and
// never consults the directive.
func TestRouteDrainCheckoutBindingWinsOverNamedDirective(t *testing.T) {
	td := routeTestDeps(t)
	repo := initAdoptRepo(t)
	addNamedWorktree(t, repo, "feature-x", "feature-x")
	bound := addLinkedWorktree(t, repo, "bound")
	seedNamedIntent(t, td, repo, "named-set", "feature-x")
	seedBinding(t, td, repo, "named-set", Adopt(bound, "bound", "proj"))

	got, err := RouteDrainCheckout(RouteDrainCheckoutRequest{
		TD:              td,
		CurrentCheckout: repo,
		SetID:           "named-set",
		Trigger:         TriggerImplementForeground,
	})
	if err != nil {
		t.Fatalf("route: %v", err)
	}
	if !got.UsedExistingBinding || got.AdoptedNamed || got.RuntimePath != bound {
		t.Fatalf("result = %+v, want resumed binding at %q with no adoption", got, bound)
	}
}

// TestRouteDrainCheckoutOverrideWinsOverNamedDirective asserts an explicit
// runtime-path override is honored before the `name` directive is read.
func TestRouteDrainCheckoutOverrideWinsOverNamedDirective(t *testing.T) {
	td := routeTestDeps(t)
	repo := initAdoptRepo(t)
	addNamedWorktree(t, repo, "feature-x", "feature-x")
	override := addLinkedWorktree(t, repo, "override-target")
	seedNamedIntent(t, td, repo, "named-set", "feature-x")

	got, err := RouteDrainCheckout(RouteDrainCheckoutRequest{
		TD:              td,
		CurrentCheckout: repo,
		SetID:           "named-set",
		Trigger:         TriggerImplementForeground,
		RuntimeOverride: override,
	})
	if err != nil {
		t.Fatalf("route: %v", err)
	}
	canonOverride, _ := filepath.EvalSymlinks(override)
	if got.AdoptedNamed || got.UsedExistingBinding || got.RuntimePath != canonOverride {
		t.Fatalf("result = %+v, want override checkout %q with no adoption", got, canonOverride)
	}
}

// TestRouteDrainCheckoutNamedDirectiveMatchesNameNotPath asserts resolution keys
// on the operator-facing worktree name, never a path: with two worktrees present
// the set lands in the one whose name matches the directive (ADR-0059).
func TestRouteDrainCheckoutNamedDirectiveMatchesNameNotPath(t *testing.T) {
	td := routeTestDeps(t)
	repo := initAdoptRepo(t)
	other := addNamedWorktree(t, repo, "other-wt", "other")
	target := addNamedWorktree(t, repo, "feature-x", "feature-x")
	seedNamedIntent(t, td, repo, "named-set", "feature-x")

	got, err := RouteDrainCheckout(RouteDrainCheckoutRequest{
		TD:              td,
		CurrentCheckout: repo,
		SetID:           "named-set",
		Trigger:         TriggerImplementForeground,
	})
	if err != nil {
		t.Fatalf("route: %v", err)
	}
	canonTarget, _ := filepath.EvalSymlinks(target)
	canonGot, _ := filepath.EvalSymlinks(got.RuntimePath)
	if canonGot != canonTarget {
		t.Fatalf("runtime %q, want the worktree named feature-x at %q (not %q)", canonGot, canonTarget, other)
	}
}

func TestResolveTrunkPathUsesConfigOverride(t *testing.T) {
	td := routeTestDeps(t)
	main := initAdoptRepo(t)
	base := addLinkedWorktree(t, main, "exec-base")
	cfg := &config.Config{
		Repo: map[string]config.RepoOverrideConfig{
			base: {Trunk: boolPtr(true)},
		},
	}
	path, bare, err := ResolveTrunkPath(td, cfg, main)
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	want, err := filepath.EvalSymlinks(base)
	if err != nil {
		want = base
	}
	gotPath, err := filepath.EvalSymlinks(path)
	if err != nil {
		gotPath = path
	}
	if bare || gotPath != want {
		t.Fatalf("path = %q bare = %v, want %q false", gotPath, bare, want)
	}
}

// initBareWithWorktree creates a bare repository (no working tree) with one
// linked worktree and returns the worktree path. A `managed` directive resolved
// from this worktree finds no Trunk worktree to fork from — bare with no
// trunk = true override — so it is unsatisfiable.
func initBareWithWorktree(t *testing.T, name string) string {
	t.Helper()
	seed := initAdoptRepo(t)
	bare := filepath.Join(t.TempDir(), "bare.git")
	if out, err := exec.Command("git", "clone", "--bare", seed, bare).CombinedOutput(); err != nil {
		t.Fatalf("clone --bare: %v\n%s", err, out)
	}
	wt := filepath.Join(t.TempDir(), name)
	adoptRunGit(t, bare, "worktree", "add", "-b", "feature", wt, "HEAD")
	return wt
}

// TestProbeWorktreeDirectiveManagedNoTrunk asserts a `managed` directive over a
// bare repo with no resolvable Trunk worktree is reported as the
// ErrNoResolvableTrunk config-class error — read-only, no provisioning (ADR-0059).
func TestProbeWorktreeDirectiveManagedNoTrunk(t *testing.T) {
	td := routeTestDeps(t)
	wt := initBareWithWorktree(t, "wt-managed")
	calls := countingGit(t, td)
	seedManagedIntent(t, td, wt, "managed-set")

	err := ProbeWorktreeDirective(td, nil, nil, wt, "managed-set")
	if !errors.Is(err, ErrNoResolvableTrunk) {
		t.Fatalf("probe err = %v, want ErrNoResolvableTrunk", err)
	}
	if *calls != 0 {
		t.Fatalf("worktree add calls = %d, want 0 — probe must not provision", *calls)
	}
}

// TestProbeWorktreeDirectiveManagedSatisfiable asserts a `managed` directive over
// a normal repo (a resolvable git main worktree) probes clean.
func TestProbeWorktreeDirectiveManagedSatisfiable(t *testing.T) {
	td := routeTestDeps(t)
	repo := initAdoptRepo(t)
	seedManagedIntent(t, td, repo, "managed-set")

	if err := ProbeWorktreeDirective(td, nil, nil, repo, "managed-set"); err != nil {
		t.Fatalf("probe err = %v, want nil", err)
	}
}

// TestProbeWorktreeDirectiveNamedAbsent asserts a `name` directive naming a
// worktree that does not exist on this machine is reported as
// ErrNamedWorktreeNotFound.
func TestProbeWorktreeDirectiveNamedAbsent(t *testing.T) {
	td := routeTestDeps(t)
	repo := initAdoptRepo(t)
	calls := countingGit(t, td)
	seedNamedIntent(t, td, repo, "named-set", "absent")

	err := ProbeWorktreeDirective(td, nil, nil, repo, "named-set")
	if !errors.Is(err, ErrNamedWorktreeNotFound) {
		t.Fatalf("probe err = %v, want ErrNamedWorktreeNotFound", err)
	}
	if *calls != 0 {
		t.Fatalf("worktree add calls = %d, want 0 — probe must not provision", *calls)
	}
}

// TestProbeWorktreeDirectiveNamedPresent asserts a `name` directive whose
// worktree exists on this machine probes clean — resolving the environment (here,
// creating the named worktree) lets the next drain proceed (ADR-0059).
func TestProbeWorktreeDirectiveNamedPresent(t *testing.T) {
	td := routeTestDeps(t)
	repo := initAdoptRepo(t)
	addNamedWorktree(t, repo, "feature-x", "feature-x")
	seedNamedIntent(t, td, repo, "named-set", "feature-x")

	if err := ProbeWorktreeDirective(td, nil, nil, repo, "named-set"); err != nil {
		t.Fatalf("probe err = %v, want nil", err)
	}
}

// TestProbeWorktreeDirectiveNoIntent asserts a set with no worktree directive
// probes clean (the no-directive default drains in the current checkout).
func TestProbeWorktreeDirectiveNoIntent(t *testing.T) {
	td := routeTestDeps(t)
	repo := initAdoptRepo(t)

	if err := ProbeWorktreeDirective(td, nil, nil, repo, "plain-set"); err != nil {
		t.Fatalf("probe err = %v, want nil", err)
	}
}

// TestProbeWorktreeDirectiveBoundSatisfied asserts an already-bound set probes
// clean even when the raw directive would be unsatisfiable: the binding records
// that the directive was satisfied on a prior drain, and later drains resume
// there (ADR-0059).
func TestProbeWorktreeDirectiveBoundSatisfied(t *testing.T) {
	td := routeTestDeps(t)
	repo := initAdoptRepo(t)
	wt := addLinkedWorktree(t, repo, "feature")
	seedNamedIntent(t, td, repo, "named-set", "absent")
	seedBinding(t, td, repo, "named-set", Adopt(wt, "feature", "proj"))

	if err := ProbeWorktreeDirective(td, nil, nil, repo, "named-set"); err != nil {
		t.Fatalf("probe err = %v, want nil for a bound set", err)
	}
}

func boolPtr(v bool) *bool { return &v }

type interceptGit struct {
	inner          deps.Git
	onCommandInDir func(dir string, args ...string) (string, error)
}

func (g *interceptGit) Command(args ...string) (string, error) {
	return g.inner.Command(args...)
}

func (g *interceptGit) CommandInDir(dir string, args ...string) (string, error) {
	if g.onCommandInDir != nil {
		return g.onCommandInDir(dir, args...)
	}
	return g.inner.CommandInDir(dir, args...)
}
