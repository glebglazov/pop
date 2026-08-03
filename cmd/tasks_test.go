package cmd

// Task verb tests stay serial: run*With reads package-level
// taskProject/taskPath flags (ADR-0145).

import (
	"bytes"
	"compress/gzip"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/glebglazov/pop/config"
	"github.com/glebglazov/pop/tasks"
	"github.com/glebglazov/pop/tasks/binding"
	"github.com/spf13/cobra"
)

// The cmd tasks tests are a deliberate smoke layer, not full e2e coverage
// (ADR-0144). Roughly one test per verb proves wiring — flags → handler →
// persisted state — plus the handful of behaviors that live only here (the
// deprecated-manifest warning text, the archive --yes/no-arg guards, the
// per-verb archived-target rejection, and the status join that surfaces an
// unsatisfiable worktree directive). The register/adopt, batch archive/
// unarchive, run/drain binding, and status-render breadth that used to be
// re-driven here now lives at the tasks / tasks/binding / work exported
// surface, where the API is stable. A future engineer seeing thin coverage
// should not "fix" it by re-adding e2e breadth — that shape is intentional.

func TestTaskSetPriorityRefreshesTable(t *testing.T) {
	root, _, td := setupCmdRepoTest(t)
	taskProject = ""
	taskPath = ""
	taskDefPath = ""
	t.Cleanup(func() {
		taskProject = ""
		taskPath = ""
		taskDefPath = ""
	})

	tasksDir := cmdTasksDir(t, td, root)
	taskDir := filepath.Join(tasksDir, "feature")
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


	if _, err := tasks.RegisterWith(td, tasksDir, tasks.DefaultStatePath()); err != nil {
		t.Fatal(err)
	}

	var buf bytes.Buffer
	if err := runTaskSetPriorityWith(td, &buf, "feature", "7"); err != nil {
		t.Fatalf("set-priority failed: %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, "Updated priority for feature: 0 -> 7") {
		t.Fatalf("missing change report:\n%s", out)
	}
	if !strings.Contains(out, "7 NEXT") {
		t.Fatalf("missing refreshed table with NEXT:\n%s", out)
	}
}

func TestTaskArchiveCommandsAndArchivedStatus(t *testing.T) {
	root, _, td := setupCmdRepoTest(t)
	resetTaskFlags()
	t.Cleanup(resetTaskFlags)

	tasksDir := cmdTasksDir(t, td, root)
	writeTaskThoughts(t, tasksDir, "alpha")
	writeTaskThoughts(t, tasksDir, "beta")


	if _, err := tasks.RegisterWith(td, tasksDir, tasks.StatePathFor(tasksDir)); err != nil {
		t.Fatal(err)
	}

	var archiveOut bytes.Buffer
	if err := runTaskArchiveWith(td, &archiveOut, "alpha"); err != nil {
		t.Fatalf("archive failed: %v", err)
	}
	if !strings.Contains(archiveOut.String(), "Archived task set alpha") {
		t.Fatalf("missing archive report:\n%s", archiveOut.String())
	}

	var defaultOut bytes.Buffer
	taskStatusArchived = false
	if err := runTaskStatusWith(td, &defaultOut, ""); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(defaultOut.String(), "alpha") || !strings.Contains(defaultOut.String(), "beta") {
		t.Fatalf("default status wrong:\n%s", defaultOut.String())
	}
	if !strings.Contains(defaultOut.String(), "pop tasks status --archived") {
		t.Fatalf("default status missing archive hint:\n%s", defaultOut.String())
	}

	var archivedOut bytes.Buffer
	taskStatusArchived = true
	if err := runTaskStatusWith(td, &archivedOut, ""); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(archivedOut.String(), "alpha") || strings.Contains(archivedOut.String(), "beta") {
		t.Fatalf("archived status wrong:\n%s", archivedOut.String())
	}

	taskStatusArchived = false
	var unarchiveOut bytes.Buffer
	if err := runTaskUnarchiveWith(td, &unarchiveOut, "alpha"); err != nil {
		t.Fatalf("unarchive failed: %v", err)
	}
	if !strings.Contains(unarchiveOut.String(), "Unarchived task set alpha") {
		t.Fatalf("missing unarchive report:\n%s", unarchiveOut.String())
	}
}

// TestTaskRegisterEagerBindsCurrentCheckoutAdopted covers ADR-0115: the first
// register of a set materializes an adopted (never-delete, no worktree created)
// Worktree binding to the current checkout, visible in the store the moment the
// set registers — no drain required.
func TestTaskRegisterEagerBindsCurrentCheckoutAdopted(t *testing.T) {
	root, _, td := setupCmdRepoTest(t)
	resetTaskFlags()
	t.Cleanup(resetTaskFlags)

	tasksDir := cmdTasksDir(t, td, root)
	writeTaskThoughts(t, tasksDir, "draft")


	origLoad := taskConfigLoad
	taskConfigLoad = func(string) (*config.Config, error) {
		return &config.Config{Projects: []config.ProjectEntry{{Path: root}}}, nil
	}
	t.Cleanup(func() { taskConfigLoad = origLoad })

	wantPath, err := tasks.ResolveRuntimePathWith(td, root, "")
	if err != nil {
		t.Fatalf("resolve runtime path: %v", err)
	}

	var regOut bytes.Buffer
	if err := runTaskRegisterWith(td, &regOut, ""); err != nil {
		t.Fatalf("register failed: %v", err)
	}

	// The binding exists in the store immediately, with no drain having run.
	_, b, bound, err := binding.FindBySetID(td, "draft")
	if err != nil {
		t.Fatalf("find binding: %v", err)
	}
	if !bound {
		t.Fatalf("register did not eager-bind the set:\n%s", regOut.String())
	}
	if b.RuntimePath != wantPath {
		t.Fatalf("binding points at %q, want current checkout %q", b.RuntimePath, wantPath)
	}
	if b.Provisioned {
		t.Fatalf("eager binding must be adopted (Provisioned=false), got provisioned=true: %+v", b)
	}
}

// TestTaskRegisterWarnsOnDeprecatedManifestKeys covers ADR-0115: a manifest
// still carrying the retired worktree/auto_drain keys registers as READY (never
// MALFORMED) and emits a deprecation warning naming the ignored keys.
func TestTaskRegisterWarnsOnDeprecatedManifestKeys(t *testing.T) {
	root, _, td := setupCmdRepoTest(t)
	resetTaskFlags()
	t.Cleanup(resetTaskFlags)

	tasksDir := cmdTasksDir(t, td, root)

	// A legacy manifest carrying both retired set-level keys.
	taskDir := filepath.Join(tasksDir, "legacy")
	if err := os.MkdirAll(taskDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(taskDir, "01-a.md"), []byte("## Acceptance criteria\n\n- [ ] ok\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	manifest := `{"tasks":[{"id":"01-a","file":"01-a.md","title":"A","type":"AFK","status":"open"}],"auto_drain":true,"worktree":{"name":"whatever"}}`
	if err := os.WriteFile(filepath.Join(taskDir, "index.json"), []byte(manifest), 0o644); err != nil {
		t.Fatal(err)
	}


	origLoad := taskConfigLoad
	taskConfigLoad = func(string) (*config.Config, error) {
		return &config.Config{Projects: []config.ProjectEntry{{Path: root}}}, nil
	}
	t.Cleanup(func() { taskConfigLoad = origLoad })

	var out bytes.Buffer
	if err := runTaskRegisterWith(td, &out, ""); err != nil {
		t.Fatalf("register failed: %v", err)
	}

	s := out.String()
	if !strings.Contains(s, "warning:") || !strings.Contains(s, "auto_drain") || !strings.Contains(s, "worktree") {
		t.Fatalf("expected a deprecation warning naming both keys:\n%s", s)
	}
	// Never MALFORMED for carrying the retired keys.
	if strings.Contains(s, "MALFORMED") {
		t.Fatalf("legacy-key set was marked MALFORMED:\n%s", s)
	}
	// The keys are ignored: no worktree/auto-drain seeded from the manifest. The
	// binding is the eager adoption of the current checkout, not the manifest name.
	_, b, bound, err := binding.FindBySetID(td, "legacy")
	if err != nil {
		t.Fatalf("find binding: %v", err)
	}
	if !bound {
		t.Fatalf("register did not eager-bind the legacy set:\n%s", s)
	}
	wantPath, err := tasks.ResolveRuntimePathWith(td, root, "")
	if err != nil {
		t.Fatalf("resolve runtime path: %v", err)
	}
	if b.RuntimePath != wantPath {
		t.Fatalf("binding at %q, want the current checkout %q (name key ignored)", b.RuntimePath, wantPath)
	}
}

// registeredIntent reads the store-backed worktree intent seeded for setID
// under root's repository, so a test can assert what `register` recorded.
func registeredIntent(t *testing.T, td *tasks.Deps, root, setID string) *tasks.WorktreeDirective {
	t.Helper()
	id, err := tasks.ResolveRepositoryIdentity(td, root)
	if err != nil {
		t.Fatalf("identity: %v", err)
	}
	defPath, err := tasks.CanonicalDefinitionPathWith(td, id.TasksDir)
	if err != nil {
		t.Fatalf("def path: %v", err)
	}
	intent, err := tasks.RegisteredWorktreeIntent(td, defPath, setID)
	if err != nil {
		t.Fatalf("read intent: %v", err)
	}
	return intent
}

// TestTaskRegisterManagedProvisionsEagerBinding covers ADR-0147: `register
// --managed` forks a managed worktree from the Trunk worktree and records a
// provisioned binding before returning — the set is drainable immediately.
func TestTaskRegisterManagedProvisionsEagerBinding(t *testing.T) {
	root, _, td := setupCmdRepoTest(t)
	resetTaskFlags()
	t.Cleanup(resetTaskFlags)

	tasksDir := cmdTasksDir(t, td, root)
	writeTaskThoughts(t, tasksDir, "draft")

	origLoad := taskConfigLoad
	taskConfigLoad = func(string) (*config.Config, error) {
		return &config.Config{Projects: []config.ProjectEntry{{Path: root}}}, nil
	}
	t.Cleanup(func() { taskConfigLoad = origLoad })

	taskRegisterManaged = true
	var regOut bytes.Buffer
	if err := runTaskRegisterWith(td, &regOut, ""); err != nil {
		t.Fatalf("register --managed failed: %v", err)
	}

	_, b, ok, err := binding.FindBySetID(td, "draft")
	if err != nil {
		t.Fatalf("find binding: %v", err)
	}
	if !ok || !b.Provisioned {
		t.Fatalf("register --managed must record a provisioned binding, got ok=%v binding=%+v\n%s", ok, b, regOut.String())
	}
	managedRoot := binding.ManagedWorktreesRoot(td)
	if !strings.HasPrefix(b.RuntimePath, managedRoot+string(filepath.Separator)) {
		t.Fatalf("binding runtime %q must live under managed root %q", b.RuntimePath, managedRoot)
	}
	if _, err := os.Stat(b.RuntimePath); err != nil {
		t.Fatalf("managed worktree missing on disk: %v", err)
	}
	if intent := registeredIntent(t, td, root, "draft"); intent != nil {
		t.Fatalf("register --managed must not record a managed intent, got %+v", intent)
	}
}

// TestTaskRegisterManagedRefusesWithoutTrunk asserts a bare repo with no
// configured trunk refuses managed register with an error naming --trunk.
func TestTaskRegisterManagedRefusesWithoutTrunk(t *testing.T) {
	seed := t.TempDir()
	initGitRepoWithCommitCmd(t, seed)
	bare := filepath.Join(t.TempDir(), "bare.git")
	if out, err := exec.Command("git", "clone", "--bare", seed, bare).CombinedOutput(); err != nil {
		t.Fatalf("clone --bare: %v\n%s", err, out)
	}
	wt := filepath.Join(t.TempDir(), "wt")
	if out, err := exec.Command("git", "-C", bare, "worktree", "add", wt, "main").CombinedOutput(); err != nil {
		t.Fatalf("worktree add: %v\n%s", err, out)
	}

	xdg := filepath.Join(t.TempDir(), "xdg")
	cd := newTestCmdDeps(t, wt, xdg, xdg)
	setCmdLayerDeps(t, cd)
	td := cd.tasksDeps()
	t.Cleanup(resetTaskFlags)
	resetTaskFlags()

	tasksDir := cmdTasksDir(t, td, wt)
	writeTaskThoughts(t, tasksDir, "bare-set")

	origLoad := taskConfigLoad
	taskConfigLoad = func(string) (*config.Config, error) {
		return &config.Config{Projects: []config.ProjectEntry{{Path: bare}}}, nil
	}
	t.Cleanup(func() { taskConfigLoad = origLoad })

	taskRegisterManaged = true
	err := runTaskRegisterWith(td, io.Discard, "")
	if err == nil || !strings.Contains(err.Error(), "--trunk") {
		t.Fatalf("register --managed without trunk = %v, want error naming --trunk", err)
	}
	if _, _, bound, _ := binding.FindBySetID(td, "bare-set"); bound {
		t.Fatal("register must not leave a binding when trunk is missing")
	}
	state, err := tasks.RefreshWith(td, tasksDir, tasks.StatePathFor(tasksDir))
	if err != nil {
		t.Fatal(err)
	}
	for _, row := range state.Rows {
		if row.ID == "bare-set" {
			t.Fatal("register must not leave a registered set when trunk is missing")
		}
	}
	_ = tasksDir
}

// TestTaskRegisterManagedTrunkFlagPersists asserts --trunk satisfies a bare
// repo, persists trunk = true to config.runtime.toml, and a later managed
// register needs no flag.
func TestTaskRegisterManagedTrunkFlagPersists(t *testing.T) {
	seed := t.TempDir()
	initGitRepoWithCommitCmd(t, seed)
	bare := filepath.Join(t.TempDir(), "bare.git")
	if out, err := exec.Command("git", "clone", "--bare", seed, bare).CombinedOutput(); err != nil {
		t.Fatalf("clone --bare: %v\n%s", err, out)
	}
	wt := filepath.Join(t.TempDir(), "wt")
	if out, err := exec.Command("git", "-C", bare, "worktree", "add", wt, "main").CombinedOutput(); err != nil {
		t.Fatalf("worktree add: %v\n%s", err, out)
	}

	xdg := filepath.Join(t.TempDir(), "xdg")
	cd := newTestCmdDeps(t, wt, xdg, xdg)
	setCmdLayerDeps(t, cd)
	td := cd.tasksDeps()
	t.Cleanup(resetTaskFlags)
	resetTaskFlags()

	cfgPath := filepath.Join(xdg, "pop", "config.toml")
	runtimePath := filepath.Join(xdg, "pop", "config.runtime.toml")
	tasksDir := cmdTasksDir(t, td, wt)

	writeTaskThoughts(t, tasksDir, "first")
	origLoad := taskConfigLoad
	taskConfigLoad = func(path string) (*config.Config, error) {
		cfg, err := config.LoadWith(cd.Config, path)
		if err != nil {
			if os.IsNotExist(err) {
				return &config.Config{Projects: []config.ProjectEntry{{Path: bare}}}, nil
			}
			return nil, err
		}
		return cfg, nil
	}
	t.Cleanup(func() { taskConfigLoad = origLoad })

	userCfgBody := "# user config\nprojects = [{ path = \"/bare\" }]\n"
	if err := os.WriteFile(cfgPath, []byte(userCfgBody), 0o644); err != nil {
		t.Fatal(err)
	}

	taskRegisterManaged = true
	taskRegisterTrunk = wt
	if err := runTaskRegisterWith(td, io.Discard, ""); err != nil {
		t.Fatalf("first managed register with --trunk: %v", err)
	}
	cfgData, err := os.ReadFile(cfgPath)
	if err != nil {
		t.Fatalf("read config: %v", err)
	}
	if string(cfgData) != userCfgBody {
		t.Fatalf("user config.toml changed:\n%s", cfgData)
	}
	runtimeData, err := os.ReadFile(runtimePath)
	if err != nil {
		t.Fatalf("read runtime config: %v", err)
	}
	if !strings.Contains(string(runtimeData), "trunk = true") {
		t.Fatalf("runtime config missing trunk = true:\n%s", runtimeData)
	}

	writeTaskThoughts(t, tasksDir, "second")
	taskRegisterTrunk = ""
	if err := runTaskRegisterWith(td, io.Discard, ""); err != nil {
		t.Fatalf("second managed register without --trunk: %v", err)
	}
	if _, _, ok, err := binding.FindBySetID(td, "second"); err != nil || !ok {
		t.Fatalf("second set binding: ok=%v err=%v", ok, err)
	}
}

func TestTaskArchiveYesArchivesDoneOnly(t *testing.T) {
	root, _, td := setupCmdRepoTest(t)
	resetTaskFlags()
	t.Cleanup(resetTaskFlags)

	tasksDir := cmdTasksDir(t, td, root)
	writeTaskThoughtsWithStatus(t, tasksDir, "done", "done")
	writeTaskThoughtsWithStatus(t, tasksDir, "ready", "open")
	if _, err := tasks.RegisterWith(td, tasksDir, tasks.StatePathFor(tasksDir)); err != nil {
		t.Fatal(err)
	}


	var stdout bytes.Buffer
	if err := runTaskArchiveSelectionWith(td, &stdout, strings.NewReader(""), true); err != nil {
		t.Fatalf("--yes archive failed: %v", err)
	}
	if !strings.Contains(stdout.String(), "Archived task set done") {
		t.Fatalf("missing done archive report:\n%s", stdout.String())
	}
	active, err := tasks.RefreshWith(td, tasksDir, tasks.StatePathFor(tasksDir))
	if err != nil {
		t.Fatal(err)
	}
	if len(active.Rows) != 1 || active.Rows[0].ID != "ready" {
		t.Fatalf("--yes should leave only ready active: %#v", active.Rows)
	}
}

func TestTaskArchiveYesNoDoneNoop(t *testing.T) {
	root, _, td := setupCmdRepoTest(t)
	resetTaskFlags()
	t.Cleanup(resetTaskFlags)

	tasksDir := cmdTasksDir(t, td, root)
	writeTaskThoughtsWithStatus(t, tasksDir, "ready", "open")
	if _, err := tasks.RegisterWith(td, tasksDir, tasks.StatePathFor(tasksDir)); err != nil {
		t.Fatal(err)
	}


	before, _ := os.ReadFile(tasks.StatePathFor(tasksDir))
	var stdout bytes.Buffer
	if err := runTaskArchiveSelectionWith(td, &stdout, strings.NewReader(""), true); err != nil {
		t.Fatalf("--yes zero done should be clean: %v", err)
	}
	if !strings.Contains(stdout.String(), "No done task sets to archive.") {
		t.Fatalf("missing no-op message:\n%s", stdout.String())
	}
	after, _ := os.ReadFile(tasks.StatePathFor(tasksDir))
	if string(before) != string(after) {
		t.Fatalf("zero-done --yes must not write:\nbefore:%s\nafter:%s", before, after)
	}
}

func cmdArchiveTestWorktree(t *testing.T, repo, branch string) string {
	t.Helper()
	cmd := exec.Command("git", "-C", repo, "commit", "--allow-empty", "-m", "base")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("initial commit: %v\n%s", err, out)
	}
	wt := filepath.Join(t.TempDir(), "wt-"+branch)
	cmd = exec.Command("git", "-C", repo, "worktree", "add", "-b", branch, wt, "HEAD")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("worktree add: %v\n%s", err, out)
	}
	return wt
}

func TestTaskArchiveNoArgNonInteractiveRejected(t *testing.T) {
	root, _, td := setupCmdRepoTest(t)
	resetTaskFlags()
	t.Cleanup(resetTaskFlags)

	tasksDir := cmdTasksDir(t, td, root)
	writeTaskThoughtsWithStatus(t, tasksDir, "done", "done")
	if _, err := tasks.RegisterWith(td, tasksDir, tasks.StatePathFor(tasksDir)); err != nil {
		t.Fatal(err)
	}

	stubCompleteInteractive(t, false)

	err := runTaskArchiveSelectionWith(td, &bytes.Buffer{}, strings.NewReader(""), false)
	if err == nil {
		t.Fatal("no-arg non-interactive archive should error")
	}
	ee, ok := err.(*tasks.ExitError)
	if !ok || ee.Code != tasks.ExitOperational {
		t.Fatalf("err = %v, want ExitOperational", err)
	}
	if !strings.Contains(err.Error(), "--yes") || !strings.Contains(err.Error(), "bare identifier") {
		t.Fatalf("err should point to --yes or a bare identifier: %v", err)
	}
}

func TestTaskUnarchiveNoArgNonInteractiveRejected(t *testing.T) {
	root, _, td := setupCmdRepoTest(t)
	resetTaskFlags()
	t.Cleanup(resetTaskFlags)

	tasksDir := cmdTasksDir(t, td, root)
	writeTaskThoughtsWithStatus(t, tasksDir, "demo", "open")
	if _, err := tasks.RegisterWith(td, tasksDir, tasks.StatePathFor(tasksDir)); err != nil {
		t.Fatal(err)
	}
	if _, err := tasks.ArchiveTaskSetWith(td, nil, nil, tasks.ResolveInput{DefinitionOverride: tasksDir, CWD: root}, "demo"); err != nil {
		t.Fatal(err)
	}

	stubCompleteInteractive(t, false)

	err := runTaskUnarchiveSelectionWith(td, &bytes.Buffer{}, strings.NewReader(""))
	if err == nil {
		t.Fatal("no-arg non-interactive unarchive should error")
	}
	ee, ok := err.(*tasks.ExitError)
	if !ok || ee.Code != tasks.ExitOperational {
		t.Fatalf("err = %v, want ExitOperational", err)
	}
	if !strings.Contains(err.Error(), "bare identifier") || !strings.Contains(err.Error(), "pop tasks unarchive <task-set>") {
		t.Fatalf("err should point to the bare identifier form: %v", err)
	}
}

func TestTaskActionVerbsRejectArchivedTargets(t *testing.T) {
	root, td := setupRunTaskCmdFixture(t)
	agent := writeRunTaskFakeAgent(t, root)

	resetTaskFlags()
	taskAgentCmd = agent
	t.Cleanup(resetTaskFlags)

	tasksDir := cmdTasksDir(t, td, root)
	if _, err := tasks.RegisterWith(td, tasksDir, tasks.StatePathFor(tasksDir)); err != nil {
		t.Fatal(err)
	}
	if _, err := tasks.ArchiveTaskSetWith(td, nil, nil, tasks.ResolveInput{CWD: root}, "demo"); err != nil {
		t.Fatal(err)
	}

	cases := []struct {
		name string
		run  func() error
	}{
		{"implement set", func() error {
			return runTaskRunTasksWith(td, &bytes.Buffer{}, io.Discard, strings.NewReader("n\n"), "demo", false, false)
		}},
		{"implement task", func() error {
			return runTaskRunTaskWith(td, &bytes.Buffer{}, io.Discard, strings.NewReader("n\n"), "demo/01-a.md", false, false)
		}},
		{"open task", func() error {
			return runTaskResetTaskWith(td, &bytes.Buffer{}, "demo/01-a.md")
		}},
		{"open set", func() error {
			return runTaskOpenTasksWith(td, &bytes.Buffer{}, strings.NewReader(""), "demo")
		}},
		{"complete task", func() error {
			return runTaskCompleteTaskWith(td, &bytes.Buffer{}, "demo/01-a.md")
		}},
		{"complete set", func() error {
			return runTaskCompleteTasksWith(td, &bytes.Buffer{}, strings.NewReader(""), "demo")
		}},
		{"skip task", func() error {
			return runTaskSkipTaskWith(td, &bytes.Buffer{}, "demo/01-a.md")
		}},
		{"skip set", func() error {
			return runTaskSkipTasksWith(td, &bytes.Buffer{}, strings.NewReader(""), "demo")
		}},
		{"set-priority", func() error {
			return runTaskSetPriorityWith(td, &bytes.Buffer{}, "demo", "4")
		}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.run()
			var ee *tasks.ExitError
			if !errors.As(err, &ee) || ee.Code == 0 {
				t.Fatalf("err = %v", err)
			}
			if !strings.Contains(err.Error(), "pop tasks unarchive demo") || !strings.Contains(err.Error(), "first") {
				t.Fatalf("missing unarchive-first guidance: %v", err)
			}
		})
	}
}

func TestTaskSnapshotVerbsAcceptArchivedTargets(t *testing.T) {
	root, td := setupRunTaskCmdFixture(t)

	resetTaskFlags()
	taskExportOutput = filepath.Join(root, "archived-demo.tar.gz")
	t.Cleanup(resetTaskFlags)

	tasksDir := cmdTasksDir(t, td, root)
	if _, err := tasks.RegisterWith(td, tasksDir, tasks.StatePathFor(tasksDir)); err != nil {
		t.Fatal(err)
	}
	if _, err := tasks.ArchiveTaskSetWith(td, nil, nil, tasks.ResolveInput{CWD: root}, "demo"); err != nil {
		t.Fatal(err)
	}

	t.Run("show-path", func(t *testing.T) {
		var buf bytes.Buffer
		if err := runTaskShowPathWith(td, &buf, "demo"); err != nil {
			t.Fatalf("show-path: %v", err)
		}
		if !strings.Contains(buf.String(), filepath.Join("tasks", "demo")) {
			t.Fatalf("show-path output = %q", buf.String())
		}
	})

	t.Run("export", func(t *testing.T) {
		var buf bytes.Buffer
		if err := runTaskExportWith(td, &buf, []string{"demo"}); err != nil {
			t.Fatalf("export: %v", err)
		}
		if _, err := os.Stat(strings.TrimSpace(buf.String())); err != nil {
			t.Fatalf("exported archive missing: %v", err)
		}
	})
}

// cmdTasksDir resolves the Task storage tasks directory for a repository checkout.
// cmd-layer deps must already route XDG_DATA_HOME for deterministic resolution.
func cmdTasksDir(t *testing.T, d *tasks.Deps, repoRoot string) string {
	t.Helper()
	id, err := tasks.ResolveRepositoryIdentity(d, repoRoot)
	if err != nil {
		t.Fatalf("resolve storage: %v", err)
	}
	return id.TasksDir
}

// writeTaskThoughts creates a minimal valid Task set under tasksDir/<stem>.
func writeTaskThoughts(t *testing.T, tasksDir, stem string) {
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

func writeTaskThoughtsWithStatus(t *testing.T, tasksDir, stem, status string) {
	t.Helper()
	taskDir := filepath.Join(tasksDir, stem)
	if err := os.MkdirAll(taskDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(taskDir, "01-a.md"), []byte("## Acceptance criteria\n\n- [ ] ok\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	manifest := fmt.Sprintf(`{"tasks":[{"id":"01-a","file":"01-a.md","title":"A","type":"AFK","status":%q}]}`, status)
	if err := os.WriteFile(filepath.Join(taskDir, "index.json"), []byte(manifest), 0o644); err != nil {
		t.Fatal(err)
	}
}

// TestTaskStatusSurfacesUnsatisfiableWorktreeDirective asserts `pop tasks status`
// shows a `name` worktree directive that names a worktree absent on this machine
// as a config/registration-class error on the set (ADR-0059), without provisioning
// or draining anything.
func TestTaskStatusSurfacesUnsatisfiableWorktreeDirective(t *testing.T) {
	root, _, td := setupCmdRepoTest(t)
	tasksDir := cmdTasksDir(t, td, root)
	writeTaskThoughts(t, tasksDir, "demo")
	d := td
	if _, err := tasks.RegisterWith(d, tasksDir, tasks.DefaultStatePath()); err != nil {
		t.Fatal(err)
	}

	canon, err := tasks.CanonicalDefinitionPathWith(d, tasksDir)
	if err != nil {
		t.Fatal(err)
	}
	if err := tasks.UpdateGlobalStateWith(d, tasks.StatePathFor(canon), func(s *tasks.GlobalState) error {
		entry := s.Tasks[canon]
		for i := range entry.TaskSets {
			if entry.TaskSets[i].ID == "demo" {
				entry.TaskSets[i].WorktreeIntent = &tasks.WorktreeDirective{Name: "absent"}
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}

	taskProject = ""
	taskPath = ""
	taskDefPath = ""
	t.Cleanup(resetTaskFlags)


	var buf bytes.Buffer
	if err := runTaskStatusWith(d, &buf, ""); err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	if !strings.Contains(out, "Config errors:") {
		t.Fatalf("missing config-error diagnostics:\n%s", out)
	}
	if !strings.Contains(out, "no worktree of that name") {
		t.Fatalf("missing the named-directive fault:\n%s", out)
	}
}

func TestTaskStatusSetArgDrillsIn(t *testing.T) {
	root, _, td := setupCmdRepoTest(t)
	tasksDir := cmdTasksDir(t, td, root)
	writeTaskThoughts(t, tasksDir, "alpha")
	writeTaskThoughts(t, tasksDir, "beta")
	if _, err := tasks.RegisterWith(td, tasksDir, tasks.DefaultStatePath()); err != nil {
		t.Fatal(err)
	}

	taskProject = ""
	taskPath = ""
	taskDefPath = ""
	t.Cleanup(resetTaskFlags)


	var buf bytes.Buffer
	if err := runTaskStatusWith(td, &buf, "alpha"); err != nil {
		t.Fatalf("drill-in should succeed: %v", err)
	}
	out := buf.String()
	// Per-task table, not the all-sets overview.
	if strings.Contains(out, "TASK SET") {
		t.Fatalf("expected per-task breakdown, got overview:\n%s", out)
	}
	for _, want := range []string{"alpha", "STATUS", "TYPE", "ID", "TITLE", "BLOCKED-BY", "01-a"} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %q in breakdown:\n%s", want, out)
		}
	}
	// Scoped to the named set only.
	if strings.Contains(out, "beta") {
		t.Fatalf("breakdown leaked another set:\n%s", out)
	}
}

func TestTaskStatusUnknownSetArgErrors(t *testing.T) {
	root, _, td := setupCmdRepoTest(t)
	tasksDir := cmdTasksDir(t, td, root)
	writeTaskThoughts(t, tasksDir, "alpha")
	if _, err := tasks.RegisterWith(td, tasksDir, tasks.DefaultStatePath()); err != nil {
		t.Fatal(err)
	}

	taskProject = ""
	taskPath = ""
	taskDefPath = ""
	t.Cleanup(resetTaskFlags)


	err := runTaskStatusWith(td, &bytes.Buffer{}, "nope")
	if err == nil {
		t.Fatal("expected error for unknown set")
	}
	// The error lists the valid identifiers so a typo becomes the answer.
	if !strings.Contains(err.Error(), "alpha") {
		t.Fatalf("error should list valid ids: %v", err)
	}
}

func initGitRepoCmd(t *testing.T, root string) {
	t.Helper()
	cmd := exec.Command("git", "init")
	cmd.Dir = root
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git init: %v\n%s", err, out)
	}
	for _, args := range [][]string{
		{"config", "user.email", "test@test"},
		{"config", "user.name", "test"},
	} {
		c := exec.Command("git", args...)
		c.Dir = root
		if out, err := c.CombinedOutput(); err != nil {
			t.Fatal(err, string(out))
		}
	}
}

func initGitRepoWithCommitCmd(t *testing.T, root string) {
	t.Helper()
	initGitRepoCmd(t, root)
	c := exec.Command("git", "commit", "--allow-empty", "-m", "base")
	c.Dir = root
	if out, err := c.CombinedOutput(); err != nil {
		t.Fatalf("git commit: %v\n%s", err, out)
	}
}

func TestHandleTaskExitMapsCodes(t *testing.T) {
	tests := []struct {
		err  error
		code int
	}{
		{nil, 0},
		{&tasks.ExitError{Code: tasks.ExitNoRunnable, Err: fmt.Errorf("no work")}, tasks.ExitNoRunnable},
		{&tasks.ExitError{Code: tasks.ExitInterrupted, Err: fmt.Errorf("interrupted")}, tasks.ExitInterrupted},
	}
	for _, tt := range tests {
		if tt.err == nil {
			continue
		}
		var ee *tasks.ExitError
		if !errors.As(tt.err, &ee) || ee.Code != tt.code {
			t.Fatalf("code = %v, want %d", tt.err, tt.code)
		}
	}
}

func TestRunTaskCmdDeclinedIsSuccess(t *testing.T) {
	root, td := setupRunTaskCmdFixture(t)
	agent := writeRunTaskFakeAgent(t, root)

	taskProject = ""
	taskPath = ""
	taskDefPath = ""
	taskAgentPreset = ""
	taskAgentCmd = agent
	taskRunYes = false
	t.Cleanup(resetTaskFlags)

	var stdout bytes.Buffer
	err := runTaskRunTaskWith(td, &stdout, io.Discard, strings.NewReader("n\n"), "", false, false)
	if err != nil {
		t.Fatalf("declined should succeed: %v", err)
	}
	if !strings.Contains(stdout.String(), "RUN") {
		t.Fatalf("missing pre-run table:\n%s", stdout.String())
	}
	_ = root
}

func TestRunTasksCmdStartsWithoutAFKConsent(t *testing.T) {
	root, td := setupRunTaskCmdFixture(t)
	agent := writeRunTaskFakeAgent(t, root)

	resetTaskFlags()
	taskAgentCmd = agent
	t.Cleanup(resetTaskFlags)

	var stdout bytes.Buffer
	err := runTaskRunTasksWith(td, &stdout, io.Discard, strings.NewReader("n\n"), "", false, false)
	if err != nil {
		t.Fatalf("set drain should proceed without AFK consent: %v", err)
	}
	if !strings.Contains(stdout.String(), "RUN") {
		t.Fatalf("missing pre-run table:\n%s", stdout.String())
	}
	if strings.Contains(stdout.String(), "Run AFK tasks in this Task set?") {
		t.Fatalf("set drain must not ask for AFK consent:\n%s", stdout.String())
	}
	if !strings.Contains(stdout.String(), "✓ Completed demo/01-a") {
		t.Fatalf("expected set to drain:\n%s", stdout.String())
	}
	_ = root
}

func TestRunTasksCmdRejectsRelativeTaskSetPath(t *testing.T) {
	root, td := setupRunTaskCmdFixture(t)
	resetTaskFlags()
	t.Cleanup(resetTaskFlags)

	err := runTaskRunTasksWith(td, &bytes.Buffer{}, io.Discard, strings.NewReader("n\n"), relTo(t, root, runTaskCmdDemoDir(t, td, root)), false, false)
	if err == nil || !strings.Contains(err.Error(), "invalid target") || !strings.Contains(err.Error(), "valid: demo") {
		t.Fatalf("relative Task set path error = %v", err)
	}
}

func TestRunTaskCmdRejectsRelativeTaskPath(t *testing.T) {
	root, td := setupRunTaskCmdFixture(t)
	resetTaskFlags()
	t.Cleanup(resetTaskFlags)

	err := runTaskRunTaskWith(td, &bytes.Buffer{}, io.Discard, strings.NewReader("n\n"), relTo(t, root, filepath.Join(runTaskCmdDemoDir(t, td, root), "01-a.md")), false, false)
	if err == nil || !strings.Contains(err.Error(), "invalid target") || !strings.Contains(err.Error(), "valid: demo") {
		t.Fatalf("relative task path error = %v", err)
	}
}

func TestRunTaskCmdTargetsTaskSetRelativeFile(t *testing.T) {
	root, td := setupRunTaskCmdFixture(t)
	resetTaskFlags()
	t.Cleanup(resetTaskFlags)

	err := runTaskRunTaskWith(td, &bytes.Buffer{}, io.Discard, strings.NewReader("n\n"), "demo/01-a.md", false, false)
	if err != nil {
		t.Fatalf("task-set-relative file failed: %v", err)
	}
	_ = root
}

func TestRunTaskCmdTargetsTaskSetIdentifier(t *testing.T) {
	root, td := setupRunTaskCmdFixture(t)
	resetTaskFlags()
	t.Cleanup(resetTaskFlags)

	err := runTaskRunTaskWith(td, &bytes.Buffer{}, io.Discard, strings.NewReader("n\n"), "demo", false, false)
	if err != nil {
		t.Fatalf("Task set identifier failed: %v", err)
	}
	_ = root
}

func TestRunTaskCmdRejectsInvalidTaskTargets(t *testing.T) {
	root, td := setupRunTaskCmdFixture(t)
	resetTaskFlags()
	t.Cleanup(resetTaskFlags)

	err := runTaskRunTaskWith(td, &bytes.Buffer{}, io.Discard, strings.NewReader("n\n"), "01-a", false, false)
	if err == nil || !strings.Contains(err.Error(), "valid: demo") {
		t.Fatalf("bare task ID error = %v", err)
	}

	err = runTaskRunTaskWith(td, &bytes.Buffer{}, io.Discard, strings.NewReader("n\n"), "01-a.md", false, false)
	if err == nil || !strings.Contains(err.Error(), "bare filenames") {
		t.Fatalf("bare filename error = %v", err)
	}

	err = runTaskRunTaskWith(td, &bytes.Buffer{}, io.Discard, strings.NewReader("n\n"), filepath.Join(runTaskCmdDemoDir(t, td, root), "01-a.md"), false, false)
	if err == nil || !strings.Contains(err.Error(), "absolute paths") {
		t.Fatalf("absolute path error = %v", err)
	}
}

func TestImplementCmdRejectsMoreThanOnePositional(t *testing.T) {
	err := taskImplementCmd.Args(taskImplementCmd, []string{"one", "two"})
	if err == nil {
		t.Fatal("expected usage error")
	}
}

func TestImplementTimeoutDefaultMatchesAttemptTimeout(t *testing.T) {
	// The flag default is a clean literal ("45m") for pretty help text, while the
	// executor's zero-value fallback is the DefaultAttemptTimeout constant. They
	// are independent sources; this guards them against drift.
	def := taskImplementCmd.Flags().Lookup("timeout").DefValue
	got, err := time.ParseDuration(def)
	if err != nil {
		t.Fatalf("flag default %q does not parse: %v", def, err)
	}
	if got != tasks.DefaultAttemptTimeout {
		t.Errorf("flag default %q = %v, want DefaultAttemptTimeout %v", def, got, tasks.DefaultAttemptTimeout)
	}
}

func TestImplementDispatchByTargetShape(t *testing.T) {
	// A ".md" target is a Task-set-relative file reference (single task); a bare
	// identifier or empty target (no argument) drains an auto-selected set.
	cases := []struct {
		target   string
		wantFile bool
	}{
		{"", false},
		{"demo", false},
		{"thoughts/issues/live-agent-smoke", false},
		{"demo/01-a.md", true},
		{"2026-06-08-feature/03-x.md", true},
	}
	for _, c := range cases {
		if got := isTaskFileTarget(c.target); got != c.wantFile {
			t.Errorf("isTaskFileTarget(%q) = %v, want %v", c.target, got, c.wantFile)
		}
	}
}

func TestResetTaskCmdRequiresOnePositional(t *testing.T) {
	for _, args := range [][]string{nil, {"one", "two"}} {
		if err := taskResetTaskCmd.Args(taskResetTaskCmd, args); err == nil {
			t.Fatalf("args %v should fail as a usage error", args)
		}
	}
}

func TestResetTaskCmdTargetsTaskSetRelativeFile(t *testing.T) {
	root, td := setupRunTaskCmdFixture(t)
	resetTaskFlags()
	t.Cleanup(resetTaskFlags)

	manifestPath := filepath.Join(runTaskCmdDemoDir(t, td, root), "index.json")
	manifest := `{"tasks":[{"id":"01-a","file":"01-a.md","title":"A","type":"AFK","status":"failed","failed_after":2}]}`
	if err := os.WriteFile(manifestPath, []byte(manifest), 0o644); err != nil {
		t.Fatal(err)
	}

	var stdout bytes.Buffer
	if err := runTaskResetTaskWith(td, &stdout, "demo/01-a.md"); err != nil {
		t.Fatalf("task-set-relative file failed: %v", err)
	}
	if !strings.Contains(stdout.String(), "Reset task demo/01-a to open") {
		t.Fatalf("missing canonical success output:\n%s", stdout.String())
	}
	_ = root
}

func TestResetTaskCmdRejectsBareIdentifier(t *testing.T) {
	root, td := setupRunTaskCmdFixture(t)
	resetTaskFlags()
	t.Cleanup(resetTaskFlags)

	err := runTaskResetTaskWith(td, &bytes.Buffer{}, "demo")
	if err == nil || !strings.Contains(err.Error(), "<task-set>/<file>.md") {
		t.Fatalf("bare identifier error = %v", err)
	}
	_ = root
}

func TestCompleteTaskCmdRequiresOnePositional(t *testing.T) {
	for _, args := range [][]string{nil, {"one", "two"}} {
		if err := taskCompleteTaskCmd.Args(taskCompleteTaskCmd, args); err == nil {
			t.Fatalf("args %v should fail as a usage error", args)
		}
	}
}

func TestCompleteTaskCmdTargetsTaskSetRelativeFile(t *testing.T) {
	root, td := setupRunTaskCmdFixture(t)
	resetTaskFlags()
	t.Cleanup(resetTaskFlags)

	var stdout bytes.Buffer
	if err := runTaskCompleteTaskWith(td, &stdout, "demo/01-a.md"); err != nil {
		t.Fatalf("task-set-relative file failed: %v", err)
	}
	if !strings.Contains(stdout.String(), "Completed task demo/01-a") {
		t.Fatalf("missing canonical success output:\n%s", stdout.String())
	}
	manifestPath := filepath.Join(runTaskCmdDemoDir(t, td, root), "index.json")
	data, err := os.ReadFile(manifestPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), `"status": "done"`) {
		t.Fatalf("task not marked done:\n%s", data)
	}
}

func TestRunTasksCmdRejectsTaskSetRelativeFile(t *testing.T) {
	root, td := setupRunTaskCmdFixture(t)
	resetTaskFlags()
	t.Cleanup(resetTaskFlags)

	err := runTaskRunTasksWith(td, &bytes.Buffer{}, io.Discard, strings.NewReader("n\n"), "demo/01-a.md", false, false)
	if err == nil || !strings.Contains(err.Error(), "bare task set identifier") {
		t.Fatalf("file reference error = %v", err)
	}
	_ = root
}

func TestRunTasksCmdTargetsTaskSetIdentifier(t *testing.T) {
	root, td := setupRunTaskCmdFixture(t)
	agent := writeRunTaskFakeAgent(t, root)
	resetTaskFlags()
	taskAgentCmd = agent
	t.Cleanup(resetTaskFlags)

	err := runTaskRunTasksWith(td, &bytes.Buffer{}, io.Discard, strings.NewReader("n\n"), "demo", false, false)
	if err != nil {
		t.Fatalf("Task set identifier failed: %v", err)
	}
	_ = root
}

func TestRunTasksCmdRejectsAbsoluteTaskSetPath(t *testing.T) {
	root, td := setupRunTaskCmdFixture(t)
	resetTaskFlags()
	t.Cleanup(resetTaskFlags)

	err := runTaskRunTasksWith(td, &bytes.Buffer{}, io.Discard, strings.NewReader("n\n"), runTaskCmdDemoDir(t, td, root), false, false)
	if err == nil || !strings.Contains(err.Error(), "absolute paths") {
		t.Fatalf("absolute path error = %v", err)
	}
}

func TestTaskCommandSurfaceUsesTaskSetVocabulary(t *testing.T) {
	names := map[string]*cobra.Command{}
	for _, c := range taskCmd.Commands() {
		names[c.Name()] = c
	}

	if _, ok := names["implement"]; !ok {
		t.Fatal("implement command is not registered")
	}
	// run and drain merged into the single implement verb (ADR 0015).
	if _, ok := names["run"]; ok {
		t.Fatal("removed run verb is still registered")
	}
	if _, ok := names["drain"]; ok {
		t.Fatal("removed drain verb is still registered")
	}
	if _, ok := names["run-prd"]; ok {
		t.Fatal("removed run-prd alias is still registered")
	}

	if names["open"] == nil {
		t.Fatal("open command is not registered")
	}
	// The pre-rename --issue-set / --issue flags were removed; assert by their
	// legacy names that they stay gone.
	if names["open"].Flags().Lookup("issue-set") != nil {
		t.Fatal("open still exposes removed --issue-set flag")
	}
	if names["open"].Flags().Lookup("issue") != nil {
		t.Fatal("open still exposes removed --issue flag")
	}
	if names["implement"].Flags().Lookup("issue-set") != nil {
		t.Fatal("implement still exposes removed --issue-set flag")
	}
	if names["implement"].Flags().Lookup("issue") != nil {
		t.Fatal("implement still exposes removed --issue flag")
	}
}

func TestTaskAllowDirtyFlagAcceptsOptionalStrategies(t *testing.T) {
	t.Cleanup(resetTaskFlags)
	for _, command := range []*cobra.Command{taskImplementCmd} {
		flag := command.Flags().Lookup("allow-dirty")
		if flag == nil {
			t.Fatalf("%s missing --allow-dirty", command.Name())
		}
		if flag.NoOptDefVal != string(tasks.DirtyRuntimeContinue) {
			t.Fatalf("%s bare --allow-dirty = %q", command.Name(), flag.NoOptDefVal)
		}
		if err := command.Flags().Parse([]string{"--allow-dirty"}); err != nil {
			t.Fatalf("%s rejected bare --allow-dirty: %v", command.Name(), err)
		}
		if taskAllowDirty != tasks.DirtyRuntimeContinue {
			t.Fatalf("%s bare --allow-dirty parsed as %q", command.Name(), taskAllowDirty)
		}
		for _, strategy := range tasks.ValidDirtyRuntimeStrategies() {
			if err := command.Flags().Parse([]string{"--allow-dirty=" + strategy}); err != nil {
				t.Fatalf("%s rejected %q: %v", command.Name(), strategy, err)
			}
		}
		err := command.Flags().Parse([]string{"--allow-dirty=invalid"})
		if err == nil || !strings.Contains(err.Error(), "continue, commit-and-continue, stash-and-continue") {
			t.Fatalf("%s invalid strategy error = %v", command.Name(), err)
		}
	}
}

func TestRunTaskCmdNonInteractiveFails(t *testing.T) {
	root, td := setupRunTaskCmdFixture(t)
	agent := writeRunTaskFakeAgent(t, root)

	resetTaskFlags()
	taskAgentCmd = agent
	t.Cleanup(resetTaskFlags)

	err := runTaskRunTaskWith(td, &bytes.Buffer{}, io.Discard, tasks.NonInteractiveReader{}, "", false, false)
	var ee *tasks.ExitError
	if !errors.As(err, &ee) || ee.Code != tasks.ExitOperational {
		t.Fatalf("err = %v", err)
	}
	_ = root
}

func resetTaskFlags() {
	taskProject = ""
	taskPath = ""
	taskDefPath = ""
	taskRuntimePath = ""
	taskStatusArchived = false
	taskRegisterManaged = false
	taskRegisterTrunk = ""
	taskRegisterAutoDrain = false
	taskCheckoutLocality = false
	taskCheckoutJSON = false
	taskBindWorktreeForce = false
	taskBindWorktreeManaged = false
	taskBindWorktreeTrunk = ""
	taskAgentPreset = ""
	taskAgentPresets = nil
	taskAgentCmd = ""
	taskAgentOutput = ""
	taskRunYes = false
	taskInWorktree = false
	taskAllowDirty = tasks.DirtyRuntimeContinue
	taskExportOutput = ""
	taskImportAs = ""
	taskStreamFull = false
	taskStreamRaw = false
	taskStreamLast = false
}

func setupRunTaskCmdFixture(t *testing.T) (root string, td *tasks.Deps) {
	t.Helper()
	root = t.TempDir()
	cd := newTestCmdDeps(t, root, "", "")
	setCmdLayerDeps(t, cd)
	td = cd.tasksDeps()

	cmd := exec.Command("git", "init")
	cmd.Dir = root
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git init: %v\n%s", err, out)
	}
	for _, args := range [][]string{
		{"config", "user.email", "test@test"},
		{"config", "user.name", "test"},
	} {
		c := exec.Command("git", args...)
		c.Dir = root
		if out, err := c.CombinedOutput(); err != nil {
			t.Fatal(err, string(out))
		}
	}
	writeFileCmd(t, filepath.Join(root, ".gitignore"), ".agent/\n.xdg/\n")
	writeFileCmd(t, filepath.Join(root, "README.md"), "# test\n")
	if out, err := exec.Command("git", "-C", root, "add", "-A").CombinedOutput(); err != nil {
		t.Fatal(err, string(out))
	}
	if out, err := exec.Command("git", "-C", root, "commit", "-m", "init").CombinedOutput(); err != nil {
		t.Fatal(err, string(out))
	}

	xdgConfig := filepath.Join(root, ".xdg-config")
	cd.FS = cmdTestFS(filepath.Join(root, ".xdg"), xdgConfig)
	configDir := filepath.Join(xdgConfig, "pop")
	if err := os.MkdirAll(configDir, 0o755); err != nil {
		t.Fatal(err)
	}
	writeFileCmd(t, filepath.Join(configDir, "config.toml"), "[tasks.verify]\nenabled = false\n")
	cfgPath := filepath.Join(configDir, "config.toml")
	origLoad := taskConfigLoad
	taskConfigLoad = func(path string) (*config.Config, error) {
		return config.Load(cfgPath)
	}
	t.Cleanup(func() { taskConfigLoad = origLoad })

	tasksDir := cmdTasksDir(t, td, root)
	writeTaskThoughts(t, tasksDir, "demo")
	if _, err := tasks.RegisterWith(td, tasksDir, tasks.DefaultStatePath()); err != nil {
		t.Fatal(err)
	}
	return root, td
}

// runTaskCmdDemoDir returns the storage directory of the fixture's "demo" Task set.
func runTaskCmdDemoDir(t *testing.T, d *tasks.Deps, root string) string {
	t.Helper()
	return filepath.Join(cmdTasksDir(t, d, root), "demo")
}

// relTo returns a relative path from base to target, failing the test on error.
func relTo(t *testing.T, base, target string) string {
	t.Helper()
	rel, err := filepath.Rel(base, target)
	if err != nil {
		t.Fatal(err)
	}
	return rel
}

func writeRunTaskFakeAgent(t *testing.T, root string) string {
	t.Helper()
	dir := filepath.Join(root, ".agent")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "fake-agent.sh")
	script := "#!/bin/sh\nTASK=$(cat \"$(printf '%s' \"$*\" | sed -n 's|.*Read the file \\([^ ]*\\) in full:.*|\\1|p' | head -1)\" | sed -n 's|^You are implementing the task at: ||p' | head -1)\n" +
		"if [ -n \"$TASK\" ] && [ -f \"$TASK\" ]; then sed -i '' 's/- \\[ \\]/- [x]/g' \"$TASK\" 2>/dev/null || sed -i 's/- \\[ \\]/- [x]/g' \"$TASK\"; fi\n" +
		"printf 'SUMMARY_START\\ncmd test\\nSUMMARY_END\\nTASK_COMPLETE\\n'\n"
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}

func writeFileCmd(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// The quota-pause exit contract (a drain that parks on an agent quota pause
// surfaces ExitQuotaPaused, not ExitSuccess) is pinned deterministically by
// tasks.TestQuotaPausedExit (the exit-code mapping) plus
// tasks.TestRunSelectedTaskQuotaPauseFailedRegistrationExitsWithPauseFields
// (the run reaching a QuotaPaused result). The former end-to-end cmd test was
// removed: under ADR-0100's always-wait design a real drain blocks in
// WaitForRecovery until the agent-reported reset instant, and the cmd entry
// point exposes no seam to force the reg-fail fast path, so it hung to the
// go-test timeout depending on wall-clock time of day.

// TestImplementAgentFlagExplicitness pins the distinction between the built-in
// fallback and an explicitly supplied --agent fallback list.
func TestImplementAgentFlagExplicitness(t *testing.T) {
	f := taskImplementCmd.Flags().Lookup("agent")
	if f == nil {
		t.Fatal("agent flag not registered")
	}
	if f.Changed {
		t.Fatal("defaulted agent flag must not report Changed")
	}
	if err := taskImplementCmd.Flags().Set("agent", "claude"); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		f.Changed = false
		_ = f.Value.Set(f.DefValue)
	})
	if !taskImplementCmd.Flags().Changed("agent") {
		t.Fatal("explicitly passed agent flag must report Changed even at the default value")
	}
}

func TestVerifierSteeringFlagsRegistered(t *testing.T) {
	// `pop tasks verify --task-runtime-path <checkout>` pins the Verifier to a
	// specific checkout root instead of resolving from the repo root.
	if taskVerifyCmd.Flags().Lookup("task-runtime-path") == nil {
		t.Fatal("tasks verify --task-runtime-path flag not registered")
	}

	// `pop tasks verify` accepts repeatable --agent and an --effort override.
	agent := taskVerifyCmd.Flags().Lookup("agent")
	if agent == nil {
		t.Fatal("tasks verify --agent flag not registered")
	}
	if agent.Value.Type() != "stringArray" {
		t.Fatalf("tasks verify --agent type = %q, want stringArray (repeatable)", agent.Value.Type())
	}
	if taskVerifyCmd.Flags().Lookup("effort") == nil {
		t.Fatal("tasks verify --effort flag not registered")
	}
	// `pop tasks verify --accept "<note>"` records a human-authored PASS (ADR-0103).
	if taskVerifyCmd.Flags().Lookup("accept") == nil {
		t.Fatal("tasks verify --accept flag not registered")
	}
	// `pop tasks verify --remediate "<note>"` spawns a human-triggered Remediation task (ADR-0103).
	if taskVerifyCmd.Flags().Lookup("remediate") == nil {
		t.Fatal("tasks verify --remediate flag not registered")
	}

	// `pop tasks implement` accepts repeatable --verify-agent and --verify-effort.
	verifyAgent := taskImplementCmd.Flags().Lookup("verify-agent")
	if verifyAgent == nil {
		t.Fatal("tasks implement --verify-agent flag not registered")
	}
	if verifyAgent.Value.Type() != "stringArray" {
		t.Fatalf("tasks implement --verify-agent type = %q, want stringArray (repeatable)", verifyAgent.Value.Type())
	}
	if taskImplementCmd.Flags().Lookup("verify-effort") == nil {
		t.Fatal("tasks implement --verify-effort flag not registered")
	}
}

// TestVerifyTaskRuntimePathFlagThreadsIntoResolveInput asserts that
// `pop tasks verify --task-runtime-path <checkout>` pins the runtime path used
// for the work-SHA read and verdict key via Binding-first resolution (ADR-0146),
// mirroring `pop tasks implement`'s override instead of resolving from the
// project root.
func TestVerifyTaskRuntimePathFlagThreadsIntoResolveInput(t *testing.T) {
	root, _, td := setupCmdRepoTest(t)
	wt := cmdArchiveTestWorktree(t, root, "verify-runtime-flag")

	resetTaskFlags()
	t.Cleanup(resetTaskFlags)

	if err := taskVerifyCmd.Flags().Set("task-runtime-path", wt); err != nil {
		t.Fatal(err)
	}

	in, err := bindingFirstVerifyResolveInput(td, "any-set")
	if err != nil {
		t.Fatalf("bindingFirstVerifyResolveInput: %v", err)
	}
	wantRuntime, err := tasks.ResolveRuntimePathWith(td, wt, "")
	if err != nil {
		t.Fatal(err)
	}
	if in.RuntimeOverride != wantRuntime {
		t.Fatalf("ResolveInput.RuntimeOverride = %q, want worktree %q (project root %q must not win)", in.RuntimeOverride, wantRuntime, root)
	}
}

// TestBindingFirstVerifyResolveInputBoundSetPinsBinding asserts that
// `pop tasks verify` without --task-runtime-path resolves a bound set to its
// Worktree binding even when invoked from the trunk checkout (ADR-0146).
func TestBindingFirstVerifyResolveInputBoundSetPinsBinding(t *testing.T) {
	root, _, td := setupCmdRepoTest(t)
	wt := cmdArchiveTestWorktree(t, root, "verify-binding-first")

	resetTaskFlags()
	t.Cleanup(resetTaskFlags)

	id, err := tasks.ResolveRepositoryIdentity(td, root)
	if err != nil {
		t.Fatalf("identity: %v", err)
	}
	if err := binding.Put(td, binding.Key(id, "bound-set"), binding.Adopt(td, wt, "verify-binding-first", "")); err != nil {
		t.Fatalf("seed binding: %v", err)
	}

	in, err := bindingFirstVerifyResolveInput(td, "bound-set")
	if err != nil {
		t.Fatalf("bindingFirstVerifyResolveInput: %v", err)
	}
	wantRuntime, err := tasks.ResolveRuntimePathWith(td, wt, "")
	if err != nil {
		t.Fatal(err)
	}
	if in.RuntimeOverride != wantRuntime {
		t.Fatalf("ResolveInput.RuntimeOverride = %q, want binding %q", in.RuntimeOverride, wantRuntime)
	}
}

// TestBindingFirstVerifyResolveInputUnboundUsesCheckout asserts an unbound set
// still resolves to the current checkout (ADR-0146).
func TestBindingFirstVerifyResolveInputUnboundUsesCheckout(t *testing.T) {
	root, _, td := setupCmdRepoTest(t)

	resetTaskFlags()
	t.Cleanup(resetTaskFlags)

	in, err := bindingFirstVerifyResolveInput(td, "unbound-set")
	if err != nil {
		t.Fatalf("bindingFirstVerifyResolveInput: %v", err)
	}
	wantRuntime, err := tasks.ResolveRuntimePathWith(td, root, "")
	if err != nil {
		t.Fatal(err)
	}
	if in.RuntimeOverride != wantRuntime {
		t.Fatalf("ResolveInput.RuntimeOverride = %q, want current checkout %q", in.RuntimeOverride, wantRuntime)
	}
}

func TestTaskExportImportRoundtripCmd(t *testing.T) {
	root, cd, td := setupCmdRepoTest(t)
	tasksDir := cmdTasksDir(t, td, root)
	const setID = "2026-06-01-user-auth"
	writeTaskThoughts(t, tasksDir, setID)

	taskProject = ""
	taskPath = ""
	taskDefPath = ""
	taskExportOutput = filepath.Join(root, setID+".tar.gz")
	taskImportAs = ""
	t.Cleanup(resetTaskFlags)

	setCmdLayerDeps(t, cd)
	var exportBuf bytes.Buffer
	if err := runTaskExportWith(td, &exportBuf, []string{setID}); err != nil {
		t.Fatalf("export: %v", err)
	}
	archivePath := strings.TrimSpace(exportBuf.String())
	if _, err := os.Stat(archivePath); err != nil {
		t.Fatalf("archive missing: %v", err)
	}

	dstRoot := t.TempDir()
	initGitRepoCmd(t, dstRoot)
	cd2 := newTestCmdDeps(t, dstRoot, filepath.Join(dstRoot, ".xdg"), "")
	setCmdLayerDeps(t, cd2)

	var importBuf bytes.Buffer
	if err := runTaskImportWith(cd2.tasksDeps(), &importBuf, archivePath); err != nil {
		t.Fatalf("import: %v", err)
	}
	importedPath := strings.TrimSpace(importBuf.String())
	if _, err := os.Stat(filepath.Join(importedPath, "index.json")); err != nil {
		t.Fatalf("imported set missing manifest: %v", err)
	}
}

// writeStreamData writes a gzipped attempt stream file at dir/name with
// the given header, events, and footer records.
func writeStreamData(t *testing.T, dir, name string, agent string, attempt int, start time.Time, outcome string, durationMS int64) {
	t.Helper()
	var jsonl bytes.Buffer
	enc := json.NewEncoder(&jsonl)
	if err := enc.Encode(map[string]any{
		"type": "header", "agent": agent, "attempt": attempt, "start_time": start.UTC().Format(time.RFC3339),
	}); err != nil {
		t.Fatal(err)
	}
	if err := enc.Encode(map[string]any{
		"type": "event", "at_ms": int64(5), "raw": `{"type":"system","subtype":"init"}`,
	}); err != nil {
		t.Fatal(err)
	}
	if err := enc.Encode(map[string]any{
		"type": "event", "at_ms": int64(100), "raw": `{"type":"assistant","message":{"content":[{"type":"text","text":"hello"}]}}`,
	}); err != nil {
		t.Fatal(err)
	}
	if err := enc.Encode(map[string]any{
		"type": "footer", "outcome": outcome, "duration_ms": durationMS,
	}); err != nil {
		t.Fatal(err)
	}

	var gz bytes.Buffer
	zw := gzip.NewWriter(&gz)
	if _, err := zw.Write(jsonl.Bytes()); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, name), gz.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
}

// setupStreamCmdFixture creates a git repo with one registered task set
// ("demo") and writes one attempt stream for the task, returning the root.
func setupStreamCmdFixture(t *testing.T) (root string, td *tasks.Deps) {
	t.Helper()
	root, _, td = setupCmdRepoTest(t)

	tasksDir := cmdTasksDir(t, td, root)
	writeTaskThoughts(t, tasksDir, "demo")
	if _, err := tasks.RegisterWith(td, tasksDir, tasks.StatePathFor(tasksDir)); err != nil {
		t.Fatal(err)
	}

	// Write stream data for the single task.
	streamDir := filepath.Join(tasksDir, "demo", "streams", "01-a")
	base := time.Date(2026, 6, 10, 12, 0, 0, 0, time.UTC)
	writeStreamData(t, streamDir, "attempt-001.jsonl.gz", "claude", 1, base, "completed", 60_000)

	return root, td
}

// TestTaskStreamNonTTYBypassesPager verifies that when stdout is not
// interactive (piped/redirected), the stream output is written directly
// to the provided writer without passing through a pager, for both the
// rendered path and the --raw path.
func TestTaskStreamNonTTYBypassesPager(t *testing.T) {
	_, td := setupStreamCmdFixture(t)
	resetTaskFlags()
	t.Cleanup(resetTaskFlags)

	// Rendered output through a buffer (pipded path) — pager must not intervene.
	var buf bytes.Buffer
	if err := runTaskStreamWith(td, &buf, "demo"); err != nil {
		t.Fatalf("runTaskStreamWith: %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, "demo") {
		t.Fatalf("expected rendered output, got:\n%s", out)
	}
	if !strings.Contains(out, "claude") {
		t.Fatalf("expected agent info in output:\n%s", out)
	}
	if !strings.Contains(out, "completed") {
		t.Fatalf("expected outcome in output:\n%s", out)
	}
	if !strings.Contains(out, "hello") {
		t.Fatalf("expected event text in output:\n%s", out)
	}
}

func TestTaskStreamRawNonTTYBypassesPager(t *testing.T) {
	_, td := setupStreamCmdFixture(t)
	resetTaskFlags()
	t.Cleanup(resetTaskFlags)

	// --raw output through a buffer (pipded path) — pager must not intervene.
	taskStreamRaw = true
	var buf bytes.Buffer
	if err := runTaskStreamWith(td, &buf, "demo"); err != nil {
		t.Fatalf("runTaskStreamWith (--raw): %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, `"agent":"claude"`) {
		t.Fatalf("expected raw JSONL with agent, got:\n%s", out)
	}
	if !strings.Contains(out, `"attempt":1`) {
		t.Fatalf("expected raw JSONL with attempt, got:\n%s", out)
	}
}

func TestTaskStreamTTYPipesThroughPager(t *testing.T) {
	_, _ = setupStreamCmdFixture(t)
	resetTaskFlags()
	t.Cleanup(resetTaskFlags)

	origInteractive := taskStdoutInteractive
	origPager := taskOpenPager
	t.Cleanup(func() {
		taskStdoutInteractive = origInteractive
		taskOpenPager = origPager
	})

	taskStdoutInteractive = func() bool { return true }

	// Mock pager that captures output into a buffer.
	var pagerBuf bytes.Buffer
	taskOpenPager = func() (io.WriteCloser, func() error, error) {
		return &nopWriteCloser{&pagerBuf}, func() error { return nil }, nil
	}

	runTaskStream(taskStreamCmd, []string{"demo"})

	if pagerBuf.Len() == 0 {
		t.Fatal("expected output through pager, but pager buffer is empty")
	}
	if !strings.Contains(pagerBuf.String(), "hello") {
		t.Fatalf("expected event text in pager output, got:\n%s", pagerBuf.String())
	}
}

func TestTaskStreamRawTTYPipesThroughPager(t *testing.T) {
	_, _ = setupStreamCmdFixture(t)
	resetTaskFlags()
	t.Cleanup(resetTaskFlags)

	origInteractive := taskStdoutInteractive
	origPager := taskOpenPager
	t.Cleanup(func() {
		taskStdoutInteractive = origInteractive
		taskOpenPager = origPager
	})

	taskStdoutInteractive = func() bool { return true }

	var pagerBuf bytes.Buffer
	taskOpenPager = func() (io.WriteCloser, func() error, error) {
		return &nopWriteCloser{&pagerBuf}, func() error { return nil }, nil
	}

	taskStreamRaw = true
	runTaskStream(taskStreamCmd, []string{"demo"})

	if pagerBuf.Len() == 0 {
		t.Fatal("expected --raw output through pager, but pager buffer is empty")
	}
	if !strings.Contains(pagerBuf.String(), `"agent":"claude"`) {
		t.Fatalf("expected raw JSONL in pager output, got:\n%s", pagerBuf.String())
	}
}

// nopWriteCloser wraps a byte buffer as an io.WriteCloser.
type nopWriteCloser struct {
	*bytes.Buffer
}

func (nopWriteCloser) Close() error { return nil }
