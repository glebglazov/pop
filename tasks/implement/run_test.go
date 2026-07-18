package implement

import (
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/glebglazov/pop/tasks"
	"github.com/glebglazov/pop/tasks/binding"
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
	if _, err := tasks.RegisterWith(tasks.DefaultDeps(), tasksDir, tasks.DefaultStatePath()); err != nil {
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

// TestResolveTaskSetRuntimeUnboundBindsCurrentCheckout asserts an unbound
// whole-set foreground drain with no flags stays in the current checkout,
// provisions no managed worktree (routing never provisions — ADR-0052), and
// persists a default (adopted) Worktree binding to that current checkout so later
// drains resume there (ADR-0062).
func TestResolveTaskSetRuntimeUnboundBindsCurrentCheckout(t *testing.T) {
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

	id, err := tasks.ResolveRepositoryIdentity(d.tasksDeps(), root)
	if err != nil {
		t.Fatal(err)
	}
	b, ok, err := binding.Lookup(d.tasksDeps(), binding.Key(id, "demo"))
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Fatalf("expected a default binding for the unbound foreground drain")
	}
	gotBound, _ := filepath.EvalSymlinks(b.RuntimePath)
	if gotBound != wantRoot {
		t.Fatalf("default binding RuntimePath = %q, want current checkout %q", gotBound, wantRoot)
	}
	if b.Provisioned {
		t.Fatalf("default binding must be adopted (Provisioned=false), got %+v", b)
	}
}

// TestResolveTaskSetRuntimeInWorktreeProvisionsAndBinds asserts --in-worktree
// forks a managed worktree off the current checkout, records a provisioned
// binding, and points the drain at the new checkout (ADR-0072).
func TestResolveTaskSetRuntimeInWorktreeProvisionsAndBinds(t *testing.T) {
	root, d := setupImplementFixture(t)

	resolved, err := ResolveTaskSetRuntime(d, tasks.ResolveInput{}, "demo", true)
	if err != nil {
		t.Fatalf("resolve runtime: %v", err)
	}

	id, err := tasks.ResolveRepositoryIdentity(d.tasksDeps(), root)
	if err != nil {
		t.Fatal(err)
	}
	b, ok, err := binding.Lookup(d.tasksDeps(), binding.Key(id, "demo"))
	if err != nil {
		t.Fatal(err)
	}
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

// seedManagedIntentImplement stamps a `managed` worktree directive onto the
// already-registered set, preserving the rest of its registration — the one-time
// seed routing reads (ADR-0059).
func seedManagedIntentImplement(t *testing.T, d *Deps, repoRoot, setID string) {
	t.Helper()
	id, err := tasks.ResolveRepositoryIdentity(d.tasksDeps(), repoRoot)
	if err != nil {
		t.Fatal(err)
	}
	defPath, err := tasks.CanonicalDefinitionPathWith(d.tasksDeps(), id.TasksDir)
	if err != nil {
		t.Fatal(err)
	}
	err = tasks.UpdateGlobalStateWith(d.tasksDeps(), tasks.StatePathFor(defPath), func(s *tasks.GlobalState) error {
		entry := s.Entry(defPath)
		for i := range entry.TaskSets {
			if entry.TaskSets[i].ID == setID {
				entry.TaskSets[i].WorktreeIntent = &tasks.WorktreeDirective{Managed: true}
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

// TestResolveTaskSetRuntimeManagedDirectiveForegroundIgnored asserts a plain `pop
// tasks implement` drain (no --in-worktree) of a set carrying a `managed`
// directive ignores the directive entirely (ADR-0072): the worktree directive is
// Queue-only, so a foreground drain provisions nothing and records a default
// (adopted) binding to the current checkout instead, draining there. A second
// drain resumes that binding.
func TestResolveTaskSetRuntimeManagedDirectiveForegroundIgnored(t *testing.T) {
	root, d := setupImplementFixture(t)
	seedManagedIntentImplement(t, d, root, "demo")

	resolved, err := ResolveTaskSetRuntime(d, tasks.ResolveInput{}, "demo", false)
	if err != nil {
		t.Fatalf("resolve runtime: %v", err)
	}
	// The default binding's checkout is the current checkout the executor already
	// resolves, so routing leaves RuntimeOverride untouched (no re-pointing).
	if resolved.RuntimeOverride != "" {
		t.Fatalf("RuntimeOverride = %q, want empty — foreground default binding needs no re-pointing", resolved.RuntimeOverride)
	}

	id, err := tasks.ResolveRepositoryIdentity(d.tasksDeps(), root)
	if err != nil {
		t.Fatal(err)
	}
	b, ok, err := binding.Lookup(d.tasksDeps(), binding.Key(id, "demo"))
	if err != nil {
		t.Fatal(err)
	}
	if !ok || b.Provisioned {
		t.Fatalf("binding = %+v ok=%v, want an adopted default binding (Provisioned=false)", b, ok)
	}
	currentRuntime, err := tasks.ResolveRuntimePathWith(d.tasksDeps(), root, "")
	if err != nil {
		t.Fatalf("runtime: %v", err)
	}
	if b.RuntimePath != currentRuntime {
		t.Fatalf("binding runtime = %q, want the current checkout %q", b.RuntimePath, currentRuntime)
	}
	if strings.HasPrefix(b.RuntimePath, binding.ManagedWorktreesRoot(d.tasksDeps())) {
		t.Fatalf("binding %q under managed root — foreground must not provision a managed worktree", b.RuntimePath)
	}

	// A second drain resumes the same default binding.
	resumed, err := ResolveTaskSetRuntime(d, tasks.ResolveInput{}, "demo", false)
	if err != nil {
		t.Fatalf("second resolve runtime: %v", err)
	}
	if resumed.RuntimeOverride != currentRuntime {
		t.Fatalf("second drain runtime = %q, want resumed bound checkout %q", resumed.RuntimeOverride, currentRuntime)
	}
}

// TestResolveTaskSetRuntimeManagedDirectiveYieldsToExistingBinding asserts a
// pre-existing binding at a different checkout is silently re-bound to the
// current checkout on foreground implement; the directive is ignored (ADR-0072).
func TestResolveTaskSetRuntimeManagedDirectiveYieldsToExistingBinding(t *testing.T) {
	root, d := setupImplementFixture(t)
	seedManagedIntentImplement(t, d, root, "demo")

	wt := filepath.Join(t.TempDir(), "bound-wt")
	if out, err := exec.Command("git", "-C", root, "worktree", "add", "-b", "bound", wt, "HEAD").CombinedOutput(); err != nil {
		t.Fatalf("worktree add: %v\n%s", err, out)
	}
	id, err := tasks.ResolveRepositoryIdentity(d.tasksDeps(), root)
	if err != nil {
		t.Fatal(err)
	}
	if err := binding.Put(d.tasksDeps(), binding.Key(id, "demo"), binding.Adopt(wt, "bound", "")); err != nil {
		t.Fatal(err)
	}

	resolved, err := ResolveTaskSetRuntime(d, tasks.ResolveInput{}, "demo", false)
	if err != nil {
		t.Fatalf("resolve runtime: %v", err)
	}
	// Rebound to current checkout (trunk); executor already resolves it.
	if resolved.RuntimeOverride != "" {
		t.Fatalf("RuntimeOverride = %q, want empty after silent rebind to current", resolved.RuntimeOverride)
	}

	currentRuntime, err := tasks.ResolveRuntimePathWith(d.tasksDeps(), root, "")
	if err != nil {
		t.Fatal(err)
	}
	b, ok, err := binding.Lookup(d.tasksDeps(), binding.Key(id, "demo"))
	if err != nil {
		t.Fatal(err)
	}
	if !ok || b.Provisioned || b.RuntimePath != currentRuntime {
		t.Fatalf("binding = %+v ok=%v, want adopted rebind at current %q", b, ok, currentRuntime)
	}
	if _, err := os.Stat(wt); err != nil {
		t.Fatalf("old adopted worktree must remain on disk: %v", err)
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
	if err := binding.Put(d.tasksDeps(), binding.Key(id, "demo"), binding.Adopt(wt, "feature", "")); err != nil {
		t.Fatal(err)
	}

	_, err = ResolveTaskSetRuntime(d, tasks.ResolveInput{}, "demo", true)
	if err == nil || !strings.Contains(err.Error(), "already bound") || !strings.Contains(err.Error(), "unbind-worktree") {
		t.Fatalf("err = %v, want already-bound rejection with unbind guidance", err)
	}
}

// TestResolveTaskSetRuntimeInWorktreeForksFromCurrentCheckoutHEAD asserts
// --in-worktree provisions a managed worktree whose branch starts at the
// current checkout's HEAD, not the Trunk worktree's (ADR-0072).
func TestResolveTaskSetRuntimeInWorktreeForksFromCurrentCheckoutHEAD(t *testing.T) {
	oldWd, _ := os.Getwd()
	parent := t.TempDir()

	trunkRoot := filepath.Join(parent, "repo")
	if err := os.MkdirAll(trunkRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{
		{"init"},
		{"config", "user.email", "test@test"},
		{"config", "user.name", "test"},
	} {
		if out, err := runImplementGit(trunkRoot, args...); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	writeImplementFile(t, filepath.Join(trunkRoot, "README.md"), "# trunk\n")
	if out, err := runImplementGit(trunkRoot, "add", "-A"); err != nil {
		t.Fatal(err, out)
	}
	if out, err := runImplementGit(trunkRoot, "commit", "-m", "init"); err != nil {
		t.Fatal(err, out)
	}
	trunkHead, err := runImplementGit(trunkRoot, "rev-parse", "HEAD")
	if err != nil {
		t.Fatal(err)
	}
	trunkHead = strings.TrimSpace(trunkHead)

	featureWT := filepath.Join(parent, "feature")
	if out, err := runImplementGit(trunkRoot, "worktree", "add", "-b", "feature", featureWT, "HEAD"); err != nil {
		t.Fatalf("worktree add: %v\n%s", err, out)
	}
	writeImplementFile(t, filepath.Join(featureWT, "feature.txt"), "ahead\n")
	if out, err := runImplementGit(featureWT, "add", "-A"); err != nil {
		t.Fatal(err, out)
	}
	if out, err := runImplementGit(featureWT, "commit", "-m", "feature"); err != nil {
		t.Fatal(err, out)
	}
	currentHead, err := runImplementGit(featureWT, "rev-parse", "HEAD")
	if err != nil {
		t.Fatal(err)
	}
	currentHead = strings.TrimSpace(currentHead)
	if currentHead == trunkHead {
		t.Fatalf("test setup: feature HEAD %q must differ from trunk HEAD %q", currentHead, trunkHead)
	}

	if err := os.Chdir(featureWT); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(oldWd) })
	t.Setenv("XDG_DATA_HOME", filepath.Join(parent, ".xdg"))

	tasksDir := implementTasksDir(t, featureWT)
	writeImplementThoughts(t, tasksDir, "demo")
	if _, err := tasks.RegisterWith(tasks.DefaultDeps(), tasksDir, tasks.DefaultStatePath()); err != nil {
		t.Fatal(err)
	}

	d := DefaultDeps()
	d.StdinInteractive = func(io.Reader) bool { return false }

	resolved, err := ResolveTaskSetRuntime(d, tasks.ResolveInput{}, "demo", true)
	if err != nil {
		t.Fatalf("resolve runtime: %v", err)
	}

	id, err := tasks.ResolveRepositoryIdentity(d.tasksDeps(), featureWT)
	if err != nil {
		t.Fatal(err)
	}
	b, ok, err := binding.Lookup(d.tasksDeps(), binding.Key(id, "demo"))
	if err != nil {
		t.Fatal(err)
	}
	if !ok || !b.Provisioned {
		t.Fatalf("binding = %+v ok=%v, want provisioned managed binding", b, ok)
	}
	if b.RuntimePath != resolved.RuntimeOverride {
		t.Fatalf("RuntimeOverride = %q, want provisioned worktree %q", resolved.RuntimeOverride, b.RuntimePath)
	}

	wtHead, err := runImplementGit(b.RuntimePath, "rev-parse", "HEAD")
	if err != nil {
		t.Fatal(err)
	}
	wtHead = strings.TrimSpace(wtHead)
	if wtHead != currentHead {
		t.Fatalf("provisioned worktree HEAD %q != current checkout HEAD %q", wtHead, currentHead)
	}
	if wtHead == trunkHead {
		t.Fatalf("provisioned worktree HEAD %q == trunk HEAD %q; want fork from current checkout", wtHead, trunkHead)
	}
}

// TestResolveTaskSetRuntimeInWorktreeWorksWithoutTrunk asserts --in-worktree in
// a bare repo with no configured trunk still provisions from the current
// checkout's HEAD — trunk is only required for Queue managed provisioning
// (ADR-0072).
func TestResolveTaskSetRuntimeInWorktreeWorksWithoutTrunk(t *testing.T) {
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
	if _, err := tasks.RegisterWith(tasks.DefaultDeps(), tasksDir, tasks.DefaultStatePath()); err != nil {
		t.Fatal(err)
	}

	d := DefaultDeps()
	d.StdinInteractive = func(io.Reader) bool { return false }

	_, err := ResolveTaskSetRuntime(d, tasks.ResolveInput{}, "demo", true)
	if err != nil {
		t.Fatalf("resolve runtime: %v", err)
	}

	id, err := tasks.ResolveRepositoryIdentity(d.tasksDeps(), wt)
	if err != nil {
		t.Fatal(err)
	}
	b, ok, err := binding.Lookup(d.tasksDeps(), binding.Key(id, "demo"))
	if err != nil {
		t.Fatal(err)
	}
	if !ok || !b.Provisioned {
		t.Fatalf("binding = %+v ok=%v, want provisioned managed binding without trunk", b, ok)
	}
	currentHead, err := runImplementGit(wt, "rev-parse", "HEAD")
	if err != nil {
		t.Fatal(err)
	}
	wtHead, err := runImplementGit(b.RuntimePath, "rev-parse", "HEAD")
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(wtHead) != strings.TrimSpace(currentHead) {
		t.Fatalf("provisioned worktree HEAD %q != current checkout HEAD %q", wtHead, currentHead)
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
	if err := binding.Put(d.tasksDeps(), binding.Key(id, "demo"), binding.Adopt(wt, "feature", "")); err != nil {
		t.Fatal(err)
	}

	// Implement from the same checkout the set is bound to resumes there.
	oldWd, _ := os.Getwd()
	if err := os.Chdir(wt); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(oldWd) })

	resolved, err := ResolveTaskSetRuntime(d, tasks.ResolveInput{}, "demo", false)
	if err != nil {
		t.Fatalf("resolve runtime: %v", err)
	}
	if resolved.RuntimeOverride != wt {
		t.Fatalf("RuntimeOverride = %q, want existing binding %q", resolved.RuntimeOverride, wt)
	}
}
