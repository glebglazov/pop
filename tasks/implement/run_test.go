package implement

import (
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/glebglazov/pop/tasks/binding"
	"github.com/glebglazov/pop/tasks"
)

func setupImplementFixture(t *testing.T) (root string, d *Deps) {
	t.Helper()
	root = t.TempDir()
	oldWd, _ := os.Getwd()
	if err := os.Chdir(root); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(oldWd) })

	cmd := exec.Command("git", "init")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git init: %v\n%s", err, out)
	}
	for _, args := range [][]string{
		{"config", "user.email", "test@test"},
		{"config", "user.name", "test"},
	} {
		if out, err := exec.Command("git", args...).CombinedOutput(); err != nil {
			t.Fatal(err, string(out))
		}
	}
	writeImplementFile(t, filepath.Join(root, ".gitignore"), ".agent/\n.xdg/\n")
	writeImplementFile(t, filepath.Join(root, "README.md"), "# test\n")
	if out, err := exec.Command("git", "add", "-A").CombinedOutput(); err != nil {
		t.Fatal(err, string(out))
	}
	if out, err := exec.Command("git", "commit", "-m", "init").CombinedOutput(); err != nil {
		t.Fatal(err, string(out))
	}

	t.Setenv("XDG_DATA_HOME", filepath.Join(root, ".xdg"))
	tasksDir := implementTasksDir(t, root)
	writeImplementThoughts(t, tasksDir, "demo")
	if _, err := tasks.RefreshWith(tasks.DefaultDeps(), tasksDir, tasks.DefaultStatePath()); err != nil {
		t.Fatal(err)
	}

	d = DefaultDeps()
	d.StdinInteractive = func(io.Reader) bool { return false }
	return root, d
}

func implementTasksDir(t *testing.T, repoRoot string) string {
	t.Helper()
	id, err := tasks.ResolveRepositoryIdentity(tasks.DefaultDeps(), repoRoot)
	if err != nil {
		t.Fatalf("resolve storage: %v", err)
	}
	return id.TasksDir
}

func writeImplementThoughts(t *testing.T, tasksDir, stem string) {
	t.Helper()
	taskDir := filepath.Join(tasksDir, stem)
	if err := os.MkdirAll(taskDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(taskDir, "01-a.md"), []byte("## Acceptance criteria\n\n- [ ] ok\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	manifest := `{"tasks":[{"id":"01-a","file":"01-a.md","title":"A","type":"AFK","status":"open"}]}`
	if err := os.WriteFile(filepath.Join(taskDir, "index.json"), []byte(manifest), 0o644); err != nil {
		t.Fatal(err)
	}
}

func writeImplementFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// TestResolveTaskSetRuntimeUnboundUsesCurrentCheckout asserts an unbound
// whole-set drain with no flags stays in the current checkout and provisions no
// managed worktree — routing never provisions (ADR-0052).
func TestResolveTaskSetRuntimeUnboundUsesCurrentCheckout(t *testing.T) {
	root, d := setupImplementFixture(t)

	resolved, err := ResolveTaskSetRuntime(d, tasks.ResolveInput{}, "demo", false)
	if err != nil {
		t.Fatalf("resolve runtime: %v", err)
	}
	runtimePath, err := tasks.ResolveRuntimePathWith(d.tasksDeps(), root, resolved.RuntimeOverride)
	if err != nil {
		t.Fatal(err)
	}
	wantRoot, _ := filepath.EvalSymlinks(root)
	gotRuntime, _ := filepath.EvalSymlinks(runtimePath)
	if gotRuntime != wantRoot {
		t.Fatalf("runtime = %q, want current checkout %q", gotRuntime, wantRoot)
	}

	store, err := binding.Load(d.tasksDeps())
	if err != nil {
		t.Fatal(err)
	}
	id, err := tasks.ResolveRepositoryIdentity(d.tasksDeps(), root)
	if err != nil {
		t.Fatal(err)
	}
	if b, ok := store.Get(binding.Key(id, "demo")); ok {
		t.Fatalf("unexpected worktree binding for unbound drain: %+v", b)
	}
}

// TestResolveTaskSetRuntimeInWorktreeProvisionsAndBinds asserts --in-worktree
// forks a managed worktree off the trunk, records a provisioned binding, and
// points the drain at the new checkout (ADR-0052).
func TestResolveTaskSetRuntimeInWorktreeProvisionsAndBinds(t *testing.T) {
	root, d := setupImplementFixture(t)

	resolved, err := ResolveTaskSetRuntime(d, tasks.ResolveInput{}, "demo", true)
	if err != nil {
		t.Fatalf("resolve runtime: %v", err)
	}

	store, err := binding.Load(d.tasksDeps())
	if err != nil {
		t.Fatal(err)
	}
	id, err := tasks.ResolveRepositoryIdentity(d.tasksDeps(), root)
	if err != nil {
		t.Fatal(err)
	}
	b, ok := store.Get(binding.Key(id, "demo"))
	if !ok {
		t.Fatalf("expected provisioned binding for --in-worktree drain")
	}
	if !b.Provisioned {
		t.Fatalf("binding = %+v, want Provisioned=true", b)
	}
	if b.RuntimePath != resolved.RuntimeOverride {
		t.Fatalf("RuntimeOverride = %q, want provisioned worktree %q", resolved.RuntimeOverride, b.RuntimePath)
	}

	wantRoot, _ := filepath.EvalSymlinks(root)
	gotRuntime, _ := filepath.EvalSymlinks(b.RuntimePath)
	if gotRuntime == wantRoot {
		t.Fatalf("provisioned worktree = current checkout %q, want a distinct managed worktree", wantRoot)
	}
	if _, err := os.Stat(b.RuntimePath); err != nil {
		t.Fatalf("provisioned worktree missing on disk: %v", err)
	}
	if !strings.HasPrefix(b.RuntimePath, binding.ManagedWorktreesRoot(d.tasksDeps())) {
		t.Fatalf("provisioned worktree %q not under managed root %q", b.RuntimePath, binding.ManagedWorktreesRoot(d.tasksDeps()))
	}
}

// TestResolveTaskSetRuntimeInWorktreeRejectsBoundSet asserts --in-worktree on an
// already-bound set is rejected with guidance to unbind first — a binding wins.
func TestResolveTaskSetRuntimeInWorktreeRejectsBoundSet(t *testing.T) {
	root, d := setupImplementFixture(t)
	wt := filepath.Join(t.TempDir(), "existing-wt")
	if out, err := exec.Command("git", "-C", root, "worktree", "add", "-b", "feature", wt, "HEAD").CombinedOutput(); err != nil {
		t.Fatalf("worktree add: %v\n%s", err, out)
	}
	id, err := tasks.ResolveRepositoryIdentity(d.tasksDeps(), root)
	if err != nil {
		t.Fatal(err)
	}
	store := &binding.Store{}
	store.Put(binding.Key(id, "demo"), binding.Adopt(wt, "feature", ""))
	if err := binding.Save(d.tasksDeps(), store); err != nil {
		t.Fatal(err)
	}

	_, err = ResolveTaskSetRuntime(d, tasks.ResolveInput{}, "demo", true)
	if err == nil || !strings.Contains(err.Error(), "already bound") || !strings.Contains(err.Error(), "unbind-worktree") {
		t.Fatalf("err = %v, want already-bound rejection with unbind guidance", err)
	}
}

// TestResolveTaskSetRuntimeInWorktreeRefusesWithoutTrunk asserts --in-worktree in
// a bare repo with no configured trunk refuses with the "set `trunk`" message —
// there is no canonical fork base to provision from (ADR-0052).
func TestResolveTaskSetRuntimeInWorktreeRefusesWithoutTrunk(t *testing.T) {
	oldWd, _ := os.Getwd()
	parent := t.TempDir()

	src := filepath.Join(parent, "src")
	if err := os.MkdirAll(src, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{
		{"init"},
		{"config", "user.email", "test@test"},
		{"config", "user.name", "test"},
	} {
		if out, err := runImplementGit(src, args...); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	writeImplementFile(t, filepath.Join(src, "README.md"), "# test\n")
	if out, err := runImplementGit(src, "add", "-A"); err != nil {
		t.Fatal(err, out)
	}
	if out, err := runImplementGit(src, "commit", "-m", "init"); err != nil {
		t.Fatal(err, out)
	}
	if out, err := runImplementGit(parent, "clone", "--bare", src, "repo.git"); err != nil {
		t.Fatalf("clone --bare: %v\n%s", err, out)
	}
	bareDir := filepath.Join(parent, "repo.git")
	wt := filepath.Join(parent, "wt")
	if out, err := runImplementGit(bareDir, "worktree", "add", "--detach", wt, "HEAD"); err != nil {
		t.Fatalf("worktree add: %v\n%s", err, out)
	}

	if err := os.Chdir(wt); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(oldWd) })
	t.Setenv("XDG_DATA_HOME", filepath.Join(parent, ".xdg"))

	tasksDir := implementTasksDir(t, wt)
	writeImplementThoughts(t, tasksDir, "demo")
	if _, err := tasks.RefreshWith(tasks.DefaultDeps(), tasksDir, tasks.DefaultStatePath()); err != nil {
		t.Fatal(err)
	}

	d := DefaultDeps()
	d.StdinInteractive = func(io.Reader) bool { return false }

	_, err := ResolveTaskSetRuntime(d, tasks.ResolveInput{}, "demo", true)
	if err == nil || !strings.Contains(err.Error(), "Trunk worktree configured") || !strings.Contains(err.Error(), "trunk = true") {
		t.Fatalf("err = %v, want \"set trunk\" refusal", err)
	}
}

func runImplementGit(dir string, args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	return string(out), err
}

func TestResolveTaskSetRuntimeRejectsRelativeTaskSetPath(t *testing.T) {
	root, d := setupImplementFixture(t)
	demoDir := filepath.Join(implementTasksDir(t, root), "demo")
	rel, err := filepath.Rel(root, demoDir)
	if err != nil {
		t.Fatal(err)
	}

	_, err = ResolveTaskSetRuntime(d, tasks.ResolveInput{}, rel, false)
	if err == nil || !strings.Contains(err.Error(), "invalid target") || !strings.Contains(err.Error(), "valid: demo") {
		t.Fatalf("relative task set path error = %v", err)
	}
}

func TestResolveTaskSetRuntimeUsesExistingBinding(t *testing.T) {
	root, d := setupImplementFixture(t)
	wt := filepath.Join(t.TempDir(), "existing-wt")
	if out, err := exec.Command("git", "-C", root, "worktree", "add", "-b", "feature", wt, "HEAD").CombinedOutput(); err != nil {
		t.Fatalf("worktree add: %v\n%s", err, out)
	}
	id, err := tasks.ResolveRepositoryIdentity(d.tasksDeps(), root)
	if err != nil {
		t.Fatal(err)
	}
	store := &binding.Store{}
	store.Put(binding.Key(id, "demo"), binding.Adopt(wt, "feature", ""))
	if err := binding.Save(d.tasksDeps(), store); err != nil {
		t.Fatal(err)
	}

	resolved, err := ResolveTaskSetRuntime(d, tasks.ResolveInput{}, "demo", false)
	if err != nil {
		t.Fatalf("resolve runtime: %v", err)
	}
	if resolved.RuntimeOverride != wt {
		t.Fatalf("RuntimeOverride = %q, want existing binding %q", resolved.RuntimeOverride, wt)
	}
}
