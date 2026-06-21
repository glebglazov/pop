package binding

import (
	"os/exec"
	"path/filepath"
	"reflect"
	"sync"
	"testing"

	"github.com/glebglazov/pop/internal/deps"
	"github.com/glebglazov/pop/tasks"
)

// bindingTestDeps returns task Deps backed by real git and a real filesystem,
// with pop's data dir redirected under a temp XDG_DATA_HOME so the shared
// binding store writes into the test sandbox.
func bindingTestDeps(t *testing.T) *tasks.Deps {
	t.Helper()
	t.Setenv("XDG_DATA_HOME", filepath.Join(t.TempDir(), "xdg"))
	return &tasks.Deps{FS: deps.NewRealFileSystem(), Git: deps.NewRealGit()}
}

func adoptRunGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git -C %s %v: %v\n%s", dir, args, err, out)
	}
}

// initAdoptRepo creates a real repo with one commit and returns the trunk path.
func initAdoptRepo(t *testing.T) string {
	t.Helper()
	repo := t.TempDir()
	adoptRunGit(t, repo, "init")
	adoptRunGit(t, repo, "config", "user.email", "pop@example.test")
	adoptRunGit(t, repo, "config", "user.name", "Pop Test")
	if err := exec.Command("git", "-C", repo, "commit", "--allow-empty", "-m", "base").Run(); err != nil {
		t.Fatalf("commit: %v", err)
	}
	return repo
}

// addLinkedWorktree adds a linked worktree on a fresh branch and returns its path.
func addLinkedWorktree(t *testing.T, repo, branch string) string {
	t.Helper()
	wt := filepath.Join(t.TempDir(), "wt-"+branch)
	adoptRunGit(t, repo, "worktree", "add", "-b", branch, wt, "HEAD")
	return wt
}

// recordingGit wraps a real git, recording every invocation's args so a test can
// assert which subcommands ran (e.g. that `worktree add` never did).
type recordingGit struct {
	inner deps.Git
	mu    sync.Mutex
	calls [][]string
}

func (g *recordingGit) record(args []string) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.calls = append(g.calls, append([]string(nil), args...))
}

func (g *recordingGit) Command(args ...string) (string, error) {
	g.record(args)
	return g.inner.Command(args...)
}

func (g *recordingGit) CommandInDir(dir string, args ...string) (string, error) {
	g.record(args)
	return g.inner.CommandInDir(dir, args...)
}

func (g *recordingGit) ran(sub ...string) bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	for _, call := range g.calls {
		if len(call) < len(sub) {
			continue
		}
		match := true
		for i := range sub {
			if call[i] != sub[i] {
				match = false
				break
			}
		}
		if match {
			return true
		}
	}
	return false
}

// TestAdoptCurrentCheckoutWorktreeLocus: running implement inside a worktree
// records a never-delete (Provisioned=false) adopted binding for that checkout.
func TestAdoptCurrentCheckoutWorktreeLocus(t *testing.T) {
	td := bindingTestDeps(t)
	repo := initAdoptRepo(t)
	wt := addLinkedWorktree(t, repo, "feature")

	adopted, err := AdoptCurrentCheckout(td, nil, nil, repo, wt, "set-a")
	if err != nil {
		t.Fatalf("adopt: %v", err)
	}
	if !adopted {
		t.Fatalf("worktree-locus run must record an adopted binding")
	}

	id, err := tasks.ResolveRepositoryIdentity(td, wt)
	if err != nil {
		t.Fatalf("identity: %v", err)
	}
	store, err := Load(td)
	if err != nil {
		t.Fatalf("load store: %v", err)
	}
	b, ok := store.Get(Key(id, "set-a"))
	if !ok {
		t.Fatalf("expected a binding for set-a")
	}
	if b.Provisioned {
		t.Fatalf("adopted binding must be Provisioned=false (never-delete)")
	}
	if b.RuntimePath != wt {
		t.Fatalf("RuntimePath = %q, want %q", b.RuntimePath, wt)
	}
	if b.Branch != "feature" {
		t.Fatalf("Branch = %q, want %q", b.Branch, "feature")
	}
	if store.ShouldTeardown(Key(id, "set-a")) {
		t.Fatalf("adopted binding must never be torn down")
	}
}

// TestAdoptCurrentCheckoutTrunkLocus: running implement in trunk records no
// worktree binding, leaving the set non-integrateable.
func TestAdoptCurrentCheckoutTrunkLocus(t *testing.T) {
	td := bindingTestDeps(t)
	repo := initAdoptRepo(t)

	adopted, err := AdoptCurrentCheckout(td, nil, nil, repo, repo, "set-a")
	if err != nil {
		t.Fatalf("adopt: %v", err)
	}
	if adopted {
		t.Fatalf("trunk-locus run must not record a binding")
	}

	id, err := tasks.ResolveRepositoryIdentity(td, repo)
	if err != nil {
		t.Fatalf("identity: %v", err)
	}
	store, err := Load(td)
	if err != nil {
		t.Fatalf("load store: %v", err)
	}
	if _, ok := store.Get(Key(id, "set-a")); ok {
		t.Fatalf("trunk-locus run must leave no worktree binding")
	}
}

// TestAdoptCurrentCheckoutNeverRunsWorktreeAdd: adoption must never provision —
// it never invokes `git worktree add` under any path.
func TestAdoptCurrentCheckoutNeverRunsWorktreeAdd(t *testing.T) {
	td := bindingTestDeps(t)
	repo := initAdoptRepo(t)
	wt := addLinkedWorktree(t, repo, "feature")

	rec := &recordingGit{inner: deps.NewRealGit()}
	td.Git = rec

	if _, err := AdoptCurrentCheckout(td, nil, nil, repo, wt, "set-a"); err != nil {
		t.Fatalf("adopt: %v", err)
	}
	if rec.ran("worktree", "add") {
		t.Fatalf("adoption must never run `git worktree add`")
	}
}

// TestAdoptCurrentCheckoutDoesNotClobberManagedBinding: an implement spawned by
// the Queue into a provisioned (managed) worktree must never overwrite the
// Queue's binding — teardown ownership stays intact.
func TestAdoptCurrentCheckoutDoesNotClobberManagedBinding(t *testing.T) {
	td := bindingTestDeps(t)
	repo := initAdoptRepo(t)
	wt := addLinkedWorktree(t, repo, "feature")

	id, err := tasks.ResolveRepositoryIdentity(td, wt)
	if err != nil {
		t.Fatalf("identity: %v", err)
	}
	key := Key(id, "set-a")

	// Pre-seed a managed binding, as `pop queue run` would after provisioning.
	store := &Store{}
	store.Put(key, Binding{RuntimePath: wt, Branch: "feature", Provisioned: true})
	if err := Save(td, store); err != nil {
		t.Fatalf("seed store: %v", err)
	}

	adopted, err := AdoptCurrentCheckout(td, nil, nil, repo, wt, "set-a")
	if err != nil {
		t.Fatalf("adopt: %v", err)
	}
	if adopted {
		t.Fatalf("must not adopt over an existing binding")
	}

	after, err := Load(td)
	if err != nil {
		t.Fatalf("load store: %v", err)
	}
	b, ok := after.Get(key)
	if !ok || !b.Provisioned {
		t.Fatalf("managed binding must survive untouched, got %+v ok=%v", b, ok)
	}
}

// TestAdoptCurrentCheckoutShapeMatchesBindWorktree: an implement-adopted binding
// is identical in shape to a bind-worktree adoption — both are Adopt() records.
func TestAdoptCurrentCheckoutShapeMatchesBindWorktree(t *testing.T) {
	td := bindingTestDeps(t)
	repo := initAdoptRepo(t)
	wt := addLinkedWorktree(t, repo, "feature")

	if _, err := AdoptCurrentCheckout(td, nil, nil, repo, wt, "set-a"); err != nil {
		t.Fatalf("adopt: %v", err)
	}

	id, err := tasks.ResolveRepositoryIdentity(td, wt)
	if err != nil {
		t.Fatalf("identity: %v", err)
	}
	store, err := Load(td)
	if err != nil {
		t.Fatalf("load store: %v", err)
	}
	got, _ := store.Get(Key(id, "set-a"))

	// bind-worktree records exactly Adopt(checkout, branch, project); the
	// implement adopter must produce a byte-identical record.
	want := Adopt(wt, "feature", "")
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("adopted binding = %+v, want %+v (identical to bind-worktree)", got, want)
	}
}
