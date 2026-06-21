package binding

import (
	"errors"
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
		WorktreeReady:   true,
		WorktreesRoot:   filepath.Join(tasks.TaskStorageRoot(td), "worktrees"),
	})
	if err != nil {
		t.Fatalf("route: %v", err)
	}
	if !got.UsedExistingBinding || got.RuntimePath != wt {
		t.Fatalf("result = %+v, want existing binding at %q", got, wt)
	}
}

func TestRouteDrainCheckoutInlineRejectedWhenBound(t *testing.T) {
	td := routeTestDeps(t)
	repo := initAdoptRepo(t)
	wt := addLinkedWorktree(t, repo, "feature")
	seedBinding(t, td, wt, "set-a", Adopt(wt, "feature", "proj"))

	_, err := RouteDrainCheckout(RouteDrainCheckoutRequest{
		TD:              td,
		CurrentCheckout: repo,
		SetID:           "set-a",
		Trigger:         TriggerImplementForeground,
		Inline:          true,
	})
	if !errors.Is(err, ErrInlineWhenBound) {
		t.Fatalf("err = %v, want ErrInlineWhenBound", err)
	}
}

func TestRouteDrainCheckoutWorktreeReadyProvisionsFromExecutionBase(t *testing.T) {
	td := routeTestDeps(t)
	repo := initAdoptRepo(t)
	worktreeAddCalls := 0
	gotDir := ""
	gotArgs := []string(nil)
	innerGit := deps.NewRealGit()
	rec := &interceptGit{
		inner: innerGit,
		onCommandInDir: func(dir string, args ...string) (string, error) {
			if len(args) >= 2 && args[0] == "worktree" && args[1] == "add" {
				worktreeAddCalls++
				gotDir = dir
				gotArgs = append([]string(nil), args...)
				return "", nil
			}
			return innerGit.CommandInDir(dir, args...)
		},
	}
	td.Git = rec

	worktreesRoot := filepath.Join(tasks.TaskStorageRoot(td), "worktrees")
	got, err := RouteDrainCheckout(RouteDrainCheckoutRequest{
		TD:              td,
		CurrentCheckout: repo,
		SetID:           "set-with-spaces",
		Trigger:         TriggerImplementForeground,
		WorktreeReady:   true,
		WorktreesRoot:   worktreesRoot,
		Now:             time.Date(2026, 6, 14, 9, 8, 7, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("route: %v", err)
	}
	if !got.ProvisionedNew {
		t.Fatalf("result = %+v, want provisioned new worktree", got)
	}
	wantBase, _ := filepath.EvalSymlinks(repo)
	gotBase, _ := filepath.EvalSymlinks(got.ExecutionBase)
	if gotBase != wantBase {
		t.Fatalf("ExecutionBase = %q, want %q", gotBase, wantBase)
	}
	if worktreeAddCalls != 1 {
		t.Fatalf("worktree add calls = %d, want 1", worktreeAddCalls)
	}
	if gotDir != repo {
		gotDirCanon, _ := filepath.EvalSymlinks(gotDir)
		wantDir, _ := filepath.EvalSymlinks(repo)
		if gotDirCanon != wantDir {
			t.Fatalf("git worktree add dir = %q, want %q", gotDir, repo)
		}
	}
	if len(gotArgs) < 4 || gotArgs[len(gotArgs)-1] != "HEAD" {
		t.Fatalf("git args = %#v, want ... HEAD", gotArgs)
	}
}

func TestRouteDrainCheckoutQueueProvisionFallbackInline(t *testing.T) {
	td := routeTestDeps(t)
	repo := initAdoptRepo(t)
	realFS := deps.NewRealFileSystem()
	td.FS = &deps.MockFileSystem{
		GetenvFunc:       os.Getenv,
		EvalSymlinksFunc: realFS.EvalSymlinks,
		MkdirAllFunc: func(path string, perm os.FileMode) error {
			if strings.Contains(path, string(filepath.Separator)+"worktrees"+string(filepath.Separator)) {
				return errors.New("boom")
			}
			return realFS.MkdirAll(path, perm)
		},
		WriteFileFunc: realFS.WriteFile,
		ReadFileFunc:  realFS.ReadFile,
		RenameFunc:    realFS.Rename,
		StatFunc:      realFS.Stat,
	}

	got, err := RouteDrainCheckout(RouteDrainCheckoutRequest{
		TD:                 td,
		CurrentCheckout:    repo,
		SetID:              "set-a",
		Trigger:            TriggerQueueSpawn,
		WorktreeReady:      true,
		OnProvisionFailure: ProvisionFallbackInline,
		WorktreesRoot:      filepath.Join(tasks.TaskStorageRoot(td), "worktrees"),
		Now:                time.Date(2026, 6, 14, 9, 8, 7, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("route: %v", err)
	}
	currentRuntime, err := tasks.ResolveRuntimePathWith(td, repo, "")
	if err != nil {
		t.Fatalf("runtime: %v", err)
	}
	if got.RuntimePath != currentRuntime || got.ProvisionedNew {
		t.Fatalf("result = %+v, want in-place fallback at %q", got, currentRuntime)
	}
}

func TestResolveExecutionBasePathUsesConfigOverride(t *testing.T) {
	td := routeTestDeps(t)
	main := initAdoptRepo(t)
	base := addLinkedWorktree(t, main, "exec-base")
	cfg := &config.Config{
		Repo: map[string]config.RepoOverrideConfig{
			base: {ExecutionBase: boolPtr(true)},
		},
	}
	path, bare, err := ResolveExecutionBasePath(td, cfg, main)
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
