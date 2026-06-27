package tasks

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/glebglazov/pop/internal/deps"
)

func setupCustomTaskFixture(t *testing.T, tasks []Task) *execFixture {
	t.Helper()
	root := t.TempDir()
	initExecutorGitRepo(t, root)
	t.Setenv("XDG_DATA_HOME", filepath.Join(root, ".xdg"))
	tasksDir := storageTasksDir(t, root)
	setupManifest(t, tasksDir, "demo", tasks)
	if _, err := RegisterWith(DefaultDeps(), tasksDir, DefaultStatePath()); err != nil {
		t.Fatal(err)
	}
	return &execFixture{root: root, tasksDir: tasksDir}
}

func TestCompleteTaskOpenToDone(t *testing.T) {
	env := setupExecutorFixture(t, false)

	result, err := CompleteTaskWith(env.deps(), nil, nil, CompleteTaskOptions{
		ResolveInput: ResolveInput{CWD: env.root},
		TaskPath:     env.demoTaskRef(t, "01-a.md"),
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.TaskSetID != "demo" || result.TaskID != "01-a" {
		t.Fatalf("complete target = %s/%s", result.TaskSetID, result.TaskID)
	}
	assertTaskDone(t, env, "01-a")
	assertProgressContains(t, env, "COMPLETE", "was open")
}

func TestCompleteTaskHITLOpenToDone(t *testing.T) {
	env := setupCustomTaskFixture(t, []Task{
		{ID: "01-a", File: "01-a.md", Title: "A", Type: "HITL", Status: "open"},
	})

	_, err := CompleteTaskWith(env.deps(), nil, nil, CompleteTaskOptions{
		ResolveInput: ResolveInput{CWD: env.root},
		TaskPath:     env.demoTaskRef(t, "01-a.md"),
	})
	if err != nil {
		t.Fatal(err)
	}
	assertTaskDone(t, env, "01-a")
	assertProgressContains(t, env, "COMPLETE", "was open")
}

func TestCompleteTaskFailedToDone(t *testing.T) {
	env := setupFailedTaskFixture(t)

	_, err := CompleteTaskWith(env.deps(), nil, nil, CompleteTaskOptions{
		ResolveInput: ResolveInput{CWD: env.root},
		TaskPath:     env.demoTaskRef(t, "01-a.md"),
	})
	if err != nil {
		t.Fatal(err)
	}
	assertTaskDone(t, env, "01-a")
	assertProgressContains(t, env, "COMPLETE", "was failed")
}

func TestCompleteTaskSkippedToDone(t *testing.T) {
	env := setupCustomTaskFixture(t, []Task{
		{ID: "01-a", File: "01-a.md", Title: "A", Type: "HITL", Status: "skipped"},
	})

	_, err := CompleteTaskWith(env.deps(), nil, nil, CompleteTaskOptions{
		ResolveInput: ResolveInput{CWD: env.root},
		TaskPath:     env.demoTaskRef(t, "01-a.md"),
	})
	if err != nil {
		t.Fatal(err)
	}
	assertTaskDone(t, env, "01-a")
	assertProgressContains(t, env, "COMPLETE", "was skipped")
}

func TestCompleteTaskSkippedBlockedByUndoneRejected(t *testing.T) {
	env := setupCustomTaskFixture(t, []Task{
		{ID: "01-a", File: "01-a.md", Title: "A", Type: "AFK", Status: "open"},
		{ID: "02-b", File: "02-b.md", Title: "B", Type: "HITL", Status: "skipped", BlockedBy: []string{"01-a"}},
	})

	_, err := CompleteTaskWith(env.deps(), nil, nil, CompleteTaskOptions{
		ResolveInput: ResolveInput{CWD: env.root},
		TaskPath:     env.demoTaskRef(t, "02-b.md"),
	})
	assertExitCode(t, err, ExitNoRunnable)
	if !strings.Contains(err.Error(), "blocked by 01-a") {
		t.Fatalf("err = %v", err)
	}
}

func TestCompleteTaskAlreadyDoneRejected(t *testing.T) {
	env := setupCustomTaskFixture(t, []Task{
		{ID: "01-a", File: "01-a.md", Title: "A", Type: "AFK", Status: "done"},
	})

	_, err := CompleteTaskWith(env.deps(), nil, nil, CompleteTaskOptions{
		ResolveInput: ResolveInput{CWD: env.root},
		TaskPath:     env.demoTaskRef(t, "01-a.md"),
	})
	assertExitCode(t, err, ExitNoRunnable)
	if !strings.Contains(err.Error(), "already done") {
		t.Fatalf("err = %v", err)
	}
}

func TestCompleteTaskBlockedByUndoneRejected(t *testing.T) {
	env := setupCustomTaskFixture(t, []Task{
		{ID: "01-a", File: "01-a.md", Title: "A", Type: "AFK", Status: "open"},
		{ID: "02-b", File: "02-b.md", Title: "B", Type: "AFK", Status: "open", BlockedBy: []string{"01-a"}},
	})

	_, err := CompleteTaskWith(env.deps(), nil, nil, CompleteTaskOptions{
		ResolveInput: ResolveInput{CWD: env.root},
		TaskPath:     env.demoTaskRef(t, "02-b.md"),
	})
	assertExitCode(t, err, ExitNoRunnable)
	if !strings.Contains(err.Error(), "blocked by 01-a") {
		t.Fatalf("err = %v", err)
	}
	assertTaskOpen(t, env, "02-b")
}

func TestCompleteTaskBlockedByDoneAllowed(t *testing.T) {
	env := setupCustomTaskFixture(t, []Task{
		{ID: "01-a", File: "01-a.md", Title: "A", Type: "AFK", Status: "done"},
		{ID: "02-b", File: "02-b.md", Title: "B", Type: "AFK", Status: "open", BlockedBy: []string{"01-a"}},
	})

	_, err := CompleteTaskWith(env.deps(), nil, nil, CompleteTaskOptions{
		ResolveInput: ResolveInput{CWD: env.root},
		TaskPath:     env.demoTaskRef(t, "02-b.md"),
	})
	if err != nil {
		t.Fatal(err)
	}
	assertTaskDone(t, env, "02-b")
}

func TestCompleteTaskRejectsBareIdentifier(t *testing.T) {
	env := setupExecutorFixture(t, false)

	_, err := CompleteTaskWith(env.deps(), nil, nil, CompleteTaskOptions{
		ResolveInput: ResolveInput{CWD: env.root},
		TaskPath:     "01-a",
	})
	assertExitCode(t, err, ExitSetup)
}

func TestCompleteTaskRejectsAbsolutePath(t *testing.T) {
	env := setupExecutorFixture(t, false)

	_, err := CompleteTaskWith(env.deps(), nil, nil, CompleteTaskOptions{
		ResolveInput: ResolveInput{CWD: env.root},
		TaskPath:     filepath.Join(env.demoDir(), "01-a.md"),
	})
	assertExitCode(t, err, ExitSetup)
}

func TestCompleteTaskRejectsBareFilename(t *testing.T) {
	env := setupExecutorFixture(t, false)

	_, err := CompleteTaskWith(env.deps(), nil, nil, CompleteTaskOptions{
		ResolveInput: ResolveInput{CWD: env.root},
		TaskPath:     "01-a.md",
	})
	assertExitCode(t, err, ExitSetup)
}

func TestCompleteTaskAcceptsTaskSetRelativeFile(t *testing.T) {
	env := setupExecutorFixture(t, false)

	_, err := CompleteTaskWith(env.deps(), nil, nil, CompleteTaskOptions{
		ResolveInput: ResolveInput{CWD: env.root},
		TaskPath:     env.demoTaskRef(t, "01-a.md"),
	})
	if err != nil {
		t.Fatal(err)
	}
	assertTaskDone(t, env, "01-a")
}

func TestCompleteTaskDoesNotStageChanges(t *testing.T) {
	env := setupExecutorFixture(t, false)
	writeFile(t, filepath.Join(env.root, "impl.txt"), "human work\n")

	_, err := CompleteTaskWith(env.deps(), nil, nil, CompleteTaskOptions{
		ResolveInput: ResolveInput{CWD: env.root},
		TaskPath:     env.demoTaskRef(t, "01-a.md"),
	})
	if err != nil {
		t.Fatal(err)
	}

	staged, err := realGitInDir(env.root, "diff", "--cached", "--name-only")
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(staged) != "" {
		t.Fatalf("command staged changes: %q", staged)
	}
}

func TestCompleteTaskProgressBeforeManifest(t *testing.T) {
	env := setupExecutorFixture(t, false)
	order := &writeOrderTracker{}
	fs := &atomicBlockingFS{
		FileSystem: deps.NewRealFileSystem(),
		tracker:    order,
	}
	d := env.deps()
	d.FS = fs

	_, err := CompleteTaskWith(d, nil, nil, CompleteTaskOptions{
		ResolveInput: ResolveInput{CWD: env.root},
		TaskPath:     env.demoTaskRef(t, "01-a.md"),
	})
	if err != nil {
		t.Fatal(err)
	}
	if order.last != "manifest" || len(order.events) < 2 || order.events[0] != "progress" {
		t.Fatalf("write order = %v last=%q", order.events, order.last)
	}
}

func TestCompleteTaskManifestFailureManualRepair(t *testing.T) {
	env := setupExecutorFixture(t, false)
	fs := &atomicBlockingFS{
		FileSystem:        deps.NewRealFileSystem(),
		failManifestWrite: true,
	}
	d := env.deps()
	d.FS = fs

	_, err := CompleteTaskWith(d, nil, nil, CompleteTaskOptions{
		ResolveInput: ResolveInput{CWD: env.root},
		TaskPath:     env.demoTaskRef(t, "01-a.md"),
	})
	assertExitCode(t, err, ExitOperational)
	if !strings.Contains(err.Error(), "manual repair required") {
		t.Fatalf("err = %v", err)
	}
}
