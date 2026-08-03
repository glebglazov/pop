package supervisor

import (
	"bytes"
	"github.com/glebglazov/pop/internal/queuetest"
	"github.com/glebglazov/pop/tasks/drain"
	"path/filepath"
	"strings"
	"testing"

	"github.com/glebglazov/pop/config"
	"github.com/glebglazov/pop/project"
	"github.com/glebglazov/pop/tasks"
	"github.com/glebglazov/pop/tasks/binding"
)

// setSpec is one task set to seed into a shared repo: its stem (the set id) and
// the task rows written under it.
type setSpec struct {
	stem  string
	tasks []queuetest.SpawnTask
}

// setupSupervisorRepoSets builds a real git repo carrying one or more registered,
// auto-drained task sets. When managed is true each set is registered with a
// managed worktree intent (RegisterManagedWith) instead of the plain adopt path,
// so the supervisor provisions a worktree at first drain. It mirrors
// queuetest.SetupSpawnRepo's leaf setup for the multi-set / managed cases the
// gate-removal acceptance tests need.
func setupSupervisorRepoSets(t *testing.T, sets []setSpec, managed bool) string {
	t.Helper()
	repo := t.TempDir()
	queuetest.InitGitRepo(t, repo)
	t.Setenv("XDG_DATA_HOME", filepath.Join(repo, ".xdg"))

	dd := tasks.DefaultDeps()
	id, err := tasks.ResolveRepositoryIdentity(dd, repo)
	if err != nil {
		t.Fatal(err)
	}
	if err := tasks.EnsureStorage(dd, id); err != nil {
		t.Fatal(err)
	}
	tasksDir := id.TasksDir
	for _, s := range sets {
		setDir := filepath.Join(tasksDir, s.stem)
		for _, task := range s.tasks {
			queuetest.WriteSpawnTaskMD(t, setDir, task.File)
		}
		queuetest.WriteSpawnManifest(t, setDir, s.tasks)
	}
	statePath := tasks.StatePathFor(tasksDir)
	register := tasks.RegisterWith
	if managed {
		register = tasks.RegisterManagedWith
	}
	if _, err := register(dd, tasksDir, statePath); err != nil {
		t.Fatal(err)
	}
	if managed {
		cfg := &config.Config{Projects: []config.ProjectEntry{{Path: repo}}}
		for _, s := range sets {
			if _, err := binding.ProvisionManagedBinding(binding.ProvisionManagedBindingRequest{
				TD:           dd,
				Config:       cfg,
				CheckoutPath: repo,
				SetID:        s.stem,
			}); err != nil {
				t.Fatalf("provision managed %s: %v", s.stem, err)
			}
		}
	}
	for _, s := range sets {
		if _, err := tasks.ToggleAutoDrainWith(dd, tasksDir, statePath, s.stem); err != nil {
			t.Fatal(err)
		}
	}
	queuetest.WriteSpawnTestAgent(t, repo)
	return repo
}

// implementSpawnCommands returns every `pop tasks implement ...` command the
// recording tmux was sent, one per spawned drain.
func implementSpawnCommands(rt *queuetest.RecordingTmux) []string {
	var cmds []string
	for _, c := range rt.Commands {
		if len(c) == 0 || c[0] != "send-keys" {
			continue
		}
		for _, arg := range c {
			if strings.HasPrefix(arg, "pop tasks implement ") {
				cmds = append(cmds, arg)
			}
		}
	}
	return cmds
}

func oneTask() []queuetest.SpawnTask {
	return []queuetest.SpawnTask{{ID: "01-a", File: "01-a.md", Title: "A", Type: "AFK", Status: "open"}}
}

func supervisorDeps(t *testing.T, repo string) (*drain.Deps, *queuetest.RecordingTmux) {
	t.Helper()
	cfg := &config.Config{Projects: []config.ProjectEntry{{Path: repo}}}
	rt := queuetest.NewRecordingTmux(false, "0")
	td := queuetest.TasksDeps(t, true)
	return &drain.Deps{
		Tasks:      td,
		Project:    project.DefaultDeps(),
		Tmux:       rt,
		LoadConfig: func(string) (*config.Config, error) { return cfg, nil },
	}, rt
}

// TestSupervisorManagedIntentProvisionsWorktreeAndDrainsThere covers acceptance
// criterion 1: a set registered --managed, picked up by the supervisor, gets a
// provisioned (managed) worktree binding forked from the Trunk worktree, and its
// drain runs there — not at the trunk.
func TestSupervisorManagedIntentProvisionsWorktreeAndDrainsThere(t *testing.T) {
	setID := "managed-set"
	repo := setupSupervisorRepoSets(t, []setSpec{{stem: setID, tasks: oneTask()}}, true)
	d, rt := supervisorDeps(t, repo)
	// Provisioned eagerly at register (ADR-0147): the binding must already exist
	// before the supervisor tick — not created lazily at drain time.

	var out bytes.Buffer
	tick(d, &out, newRunOutputState())

	repoKey, err := drain.ResolveRepoKey(d, repo)
	if err != nil {
		t.Fatal(err)
	}
	b, ok := queuetest.LoadBindingStore(t, d.Tasks)[drain.SetScopedKey(repoKey, setID)]
	if !ok {
		t.Fatalf("managed register must bind a worktree before drain; got no binding for %s", setID)
	}
	if !b.Provisioned {
		t.Fatalf("binding must be Provisioned (managed), got adopted: %+v", b)
	}
	managedRoot := queuetest.Canon(t, d.Tasks, binding.ManagedWorktreesRoot(d.Tasks))
	if got := queuetest.Canon(t, d.Tasks, b.RuntimePath); !strings.HasPrefix(got, managedRoot+string(filepath.Separator)) {
		t.Fatalf("binding runtime %q must live under the managed-worktrees root %q", got, managedRoot)
	}
	if queuetest.Canon(t, d.Tasks, b.RuntimePath) == queuetest.Canon(t, d.Tasks, repo) {
		t.Fatalf("managed drain must not land on the trunk %q", repo)
	}

	cmd, ok := queuetest.ExtractSpawnCommand(rt)
	if !ok {
		t.Fatalf("supervisor tick must spawn a drain; output:\n%s", out.String())
	}
	if !strings.Contains(cmd, "pop tasks implement "+setID) || !strings.Contains(cmd, "--task-runtime-path "+b.RuntimePath) {
		t.Fatalf("spawn command = %q, want implement for %s pinned to managed worktree %q", cmd, setID, b.RuntimePath)
	}
	newWindow, ok := rt.FindCommand("new-window")
	if !ok || !queuetest.ArgsContain(newWindow, "-c", b.RuntimePath) {
		t.Fatalf("drain pane must open in the managed worktree %q: %v", b.RuntimePath, newWindow)
	}
}

// TestSupervisorBoundNonTrunkSetResumesInBoundCheckout covers acceptance
// criterion 2: a set bound to a non-trunk checkout is spawned there and its
// binding is not re-pointed to the trunk.
func TestSupervisorBoundNonTrunkSetResumesInBoundCheckout(t *testing.T) {
	setID := "bound-set"
	repo := setupSupervisorRepoSets(t, []setSpec{{stem: setID, tasks: oneTask()}}, false)
	bound := filepath.Join(t.TempDir(), "bound-checkout")
	runGit(t, repo, "worktree", "add", "--detach", bound, "HEAD")

	d, rt := supervisorDeps(t, repo)
	repoKey, err := drain.ResolveRepoKey(d, repo)
	if err != nil {
		t.Fatal(err)
	}
	queuetest.SeedBindingStore(t, d.Tasks, map[string]drain.WorktreeBinding{
		drain.SetScopedKey(repoKey, setID): {RuntimePath: bound, Branch: "bound", Project: "pop", Provisioned: false},
	})

	var out bytes.Buffer
	tick(d, &out, newRunOutputState())

	cmd, ok := queuetest.ExtractSpawnCommand(rt)
	if !ok {
		t.Fatalf("supervisor tick must spawn a drain; output:\n%s", out.String())
	}
	if !strings.Contains(cmd, "--task-runtime-path "+bound) {
		t.Fatalf("spawn command = %q, want drain pinned to bound checkout %q", cmd, bound)
	}
	newWindow, ok := rt.FindCommand("new-window")
	if !ok || !queuetest.ArgsContain(newWindow, "-c", bound) {
		t.Fatalf("drain pane must open in the bound checkout %q: %v", bound, newWindow)
	}

	b := queuetest.LoadBindingStore(t, d.Tasks)[drain.SetScopedKey(repoKey, setID)]
	if queuetest.Canon(t, d.Tasks, b.RuntimePath) != queuetest.Canon(t, d.Tasks, bound) {
		t.Fatalf("binding runtime = %q, want unchanged bound checkout %q (never re-pointed to trunk)", b.RuntimePath, bound)
	}
	if queuetest.Canon(t, d.Tasks, b.RuntimePath) == queuetest.Canon(t, d.Tasks, repo) {
		t.Fatalf("binding was re-pointed to the trunk %q", repo)
	}
}

// TestSupervisorUnboundNoIntentSetIsNeedsBindFault covers acceptance criterion 3:
// an unbound set with no worktree intent is reported as a needs-bind fault, never
// dispatched.
func TestSupervisorUnboundNoIntentSetIsNeedsBindFault(t *testing.T) {
	setID := "orphan-set"
	repo := setupSupervisorRepoSets(t, []setSpec{{stem: setID, tasks: oneTask()}}, false)
	d, rt := supervisorDeps(t, repo)
	// No binding and no managed intent.

	cfg := &config.Config{Projects: []config.ProjectEntry{{Path: repo}}}
	decisions, err := drain.Scan(d, cfg)
	if err != nil {
		t.Fatalf("drain.Scan: %v", err)
	}
	var found bool
	for _, dec := range decisions {
		if dec.BlockedSetID == setID || dec.TaskSetID == setID {
			found = true
			if dec.Actionable() {
				t.Fatalf("unbound no-intent set must not be actionable: %+v", dec)
			}
			if dec.Reason != drain.NeedsBindReason {
				t.Fatalf("reason = %q, want needs-bind fault", dec.Reason)
			}
		}
	}
	if !found {
		t.Fatalf("expected a needs-bind decision for %s, got %+v", setID, decisions)
	}

	var out bytes.Buffer
	tick(d, &out, newRunOutputState())
	if cmds := implementSpawnCommands(rt); len(cmds) != 0 {
		t.Fatalf("unbound no-intent set must never dispatch, got spawns: %v", cmds)
	}
}

// TestSupervisorDispatchesTwoBoundSetsInOneTick covers the first half of
// acceptance criterion 4: two queue-drainable Ready sets in one repo dispatch in
// a single supervisor tick, each to its own bound checkout.
func TestSupervisorDispatchesTwoBoundSetsInOneTick(t *testing.T) {
	repo := setupSupervisorRepoSets(t, []setSpec{
		{stem: "set-alpha", tasks: oneTask()},
		{stem: "set-beta", tasks: oneTask()},
	}, false)
	wtAlpha := filepath.Join(t.TempDir(), "alpha")
	wtBeta := filepath.Join(t.TempDir(), "beta")
	runGit(t, repo, "worktree", "add", "--detach", wtAlpha, "HEAD")
	runGit(t, repo, "worktree", "add", "--detach", wtBeta, "HEAD")

	d, rt := supervisorDeps(t, repo)
	repoKey, err := drain.ResolveRepoKey(d, repo)
	if err != nil {
		t.Fatal(err)
	}
	queuetest.SeedBindingStore(t, d.Tasks, map[string]drain.WorktreeBinding{
		drain.SetScopedKey(repoKey, "set-alpha"): {RuntimePath: wtAlpha, Branch: "alpha", Project: "pop"},
		drain.SetScopedKey(repoKey, "set-beta"):  {RuntimePath: wtBeta, Branch: "beta", Project: "pop"},
	})

	var out bytes.Buffer
	tick(d, &out, newRunOutputState())

	cmds := implementSpawnCommands(rt)
	if len(cmds) != 2 {
		t.Fatalf("want two drains dispatched in one tick, got %d: %v\noutput:\n%s", len(cmds), cmds, out.String())
	}
	joined := strings.Join(cmds, "\n")
	if !strings.Contains(joined, "set-alpha") || !strings.Contains(joined, "--task-runtime-path "+wtAlpha) {
		t.Fatalf("missing set-alpha drain to %q in %v", wtAlpha, cmds)
	}
	if !strings.Contains(joined, "set-beta") || !strings.Contains(joined, "--task-runtime-path "+wtBeta) {
		t.Fatalf("missing set-beta drain to %q in %v", wtBeta, cmds)
	}
}

// TestSupervisorLiveDrainInOneWorktreeDoesNotBusyWholeRepo covers the second half
// of acceptance criterion 4: a live drain in one worktree is accounted per
// checkout, so the repo's other bound set still dispatches in the same tick.
func TestSupervisorLiveDrainInOneWorktreeDoesNotBusyWholeRepo(t *testing.T) {
	repo := setupSupervisorRepoSets(t, []setSpec{
		{stem: "set-live", tasks: oneTask()},
		{stem: "set-idle", tasks: oneTask()},
	}, false)
	wtLive := filepath.Join(t.TempDir(), "live")
	wtIdle := filepath.Join(t.TempDir(), "idle")
	runGit(t, repo, "worktree", "add", "--detach", wtLive, "HEAD")
	runGit(t, repo, "worktree", "add", "--detach", wtIdle, "HEAD")

	d, rt := supervisorDeps(t, repo)
	repoKey, err := drain.ResolveRepoKey(d, repo)
	if err != nil {
		t.Fatal(err)
	}
	queuetest.SeedBindingStore(t, d.Tasks, map[string]drain.WorktreeBinding{
		drain.SetScopedKey(repoKey, "set-live"): {RuntimePath: wtLive, Branch: "live", Project: "pop"},
		drain.SetScopedKey(repoKey, "set-idle"): {RuntimePath: wtIdle, Branch: "idle", Project: "pop"},
	})

	// A live (unfinished) drain running in wtLive under this process's PID.
	if _, err := tasks.BeginDrain(d.Tasks, wtLive, "set-live", nil); err != nil {
		t.Fatalf("BeginDrain: %v", err)
	}

	var out bytes.Buffer
	tick(d, &out, newRunOutputState())

	cmds := implementSpawnCommands(rt)
	if len(cmds) != 1 {
		t.Fatalf("want exactly the idle set dispatched, got %d: %v\noutput:\n%s", len(cmds), cmds, out.String())
	}
	if !strings.Contains(cmds[0], "set-idle") || !strings.Contains(cmds[0], "--task-runtime-path "+wtIdle) {
		t.Fatalf("dispatched drain = %q, want set-idle to %q", cmds[0], wtIdle)
	}
	if strings.Contains(cmds[0], "set-live") {
		t.Fatalf("the live-draining set must not be re-dispatched: %q", cmds[0])
	}
}
