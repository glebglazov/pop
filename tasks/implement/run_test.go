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

func TestResolveTaskSetRuntimeWorktreeReadyProvisionsManagedWorktree(t *testing.T) {
	root, d := setupImplementFixture(t)
	writeImplementFile(t, filepath.Join(root, ".pop.toml"), "worktree_ready = true\n")

	resolved, err := ResolveTaskSetRuntime(d, tasks.ResolveInput{}, "demo", false)
	if err != nil {
		t.Fatalf("resolve runtime: %v", err)
	}
	if strings.TrimSpace(resolved.RuntimeOverride) == "" || resolved.RuntimeOverride == root {
		t.Fatalf("RuntimeOverride = %q, want managed worktree distinct from %q", resolved.RuntimeOverride, root)
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
		t.Fatalf("missing binding for demo: %+v", store.Bindings)
	}
	if !b.Provisioned {
		t.Fatalf("binding Provisioned = false, want managed worktree: %+v", b)
	}
	if b.RuntimePath != resolved.RuntimeOverride {
		t.Fatalf("binding runtime = %q, resolved = %q", b.RuntimePath, resolved.RuntimeOverride)
	}
}

func TestResolveTaskSetRuntimeInlineBypassesWorktreeReady(t *testing.T) {
	root, d := setupImplementFixture(t)
	writeImplementFile(t, filepath.Join(root, ".pop.toml"), "worktree_ready = true\n")

	resolved, err := ResolveTaskSetRuntime(d, tasks.ResolveInput{}, "demo", true)
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
		t.Fatalf("inline runtime = %q, want trunk %q", gotRuntime, wantRoot)
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
		t.Fatalf("unexpected worktree binding for inline run: %+v", b)
	}
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
