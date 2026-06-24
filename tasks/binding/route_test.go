package binding

import (
	"os"
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
