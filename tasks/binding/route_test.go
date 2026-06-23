package binding

import (
	"path/filepath"
	"testing"

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
