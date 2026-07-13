package tasks

import (
	"path/filepath"
	"strings"
	"testing"
)

func taskStatus(t *testing.T, env *execFixture, taskID string) TaskStatus {
	t.Helper()
	m := LoadManifest(DefaultDeps(), "demo", env.demoManifest())
	for _, task := range m.Tasks {
		if task.ID == taskID {
			return task.Status
		}
	}
	t.Fatalf("task %s not found", taskID)
	return ""
}

func TestSkipTaskOpenToSkipped(t *testing.T) {
	env := setupExecutorFixture(t, false)

	result, err := SkipTaskWith(env.deps(), nil, nil, SkipTaskOptions{
		ResolveInput: ResolveInput{CWD: env.root},
		TaskPath:     env.demoTaskRef(t, "01-a.md"),
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.TaskSetID != "demo" || result.TaskID != "01-a" {
		t.Fatalf("skip target = %s/%s", result.TaskSetID, result.TaskID)
	}
	if s := taskStatus(t, env, "01-a"); s != "skipped" {
		t.Fatalf("task 01-a status = %q, want skipped", s)
	}
	assertProgressContains(t, env, "SKIP", "skipped demo/01-a")
}

func TestSkipTaskHITLOpenToSkipped(t *testing.T) {
	env := setupCustomTaskFixture(t, []Task{
		{ID: "01-a", File: "01-a.md", Title: "A", Type: "HITL", Status: "open"},
	})

	_, err := SkipTaskWith(env.deps(), nil, nil, SkipTaskOptions{
		ResolveInput: ResolveInput{CWD: env.root},
		TaskPath:     env.demoTaskRef(t, "01-a.md"),
	})
	if err != nil {
		t.Fatal(err)
	}
	if s := taskStatus(t, env, "01-a"); s != "skipped" {
		t.Fatalf("task 01-a status = %q, want skipped", s)
	}
}

func TestSkipTaskDoneRejected(t *testing.T) {
	env := setupCustomTaskFixture(t, []Task{
		{ID: "01-a", File: "01-a.md", Title: "A", Type: "AFK", Status: "done"},
	})

	_, err := SkipTaskWith(env.deps(), nil, nil, SkipTaskOptions{
		ResolveInput: ResolveInput{CWD: env.root},
		TaskPath:     env.demoTaskRef(t, "01-a.md"),
	})
	assertExitCode(t, err, ExitNoRunnable)
	if !strings.Contains(err.Error(), "is done") {
		t.Fatalf("err = %v", err)
	}
}

func TestSkipTaskFailedRejected(t *testing.T) {
	env := setupFailedTaskFixture(t)

	_, err := SkipTaskWith(env.deps(), nil, nil, SkipTaskOptions{
		ResolveInput: ResolveInput{CWD: env.root},
		TaskPath:     env.demoTaskRef(t, "01-a.md"),
	})
	assertExitCode(t, err, ExitNoRunnable)
	if !strings.Contains(err.Error(), "is failed") {
		t.Fatalf("err = %v", err)
	}
}

func TestSkipTaskAlreadySkippedRejected(t *testing.T) {
	env := setupCustomTaskFixture(t, []Task{
		{ID: "01-a", File: "01-a.md", Title: "A", Type: "AFK", Status: "skipped"},
	})

	_, err := SkipTaskWith(env.deps(), nil, nil, SkipTaskOptions{
		ResolveInput: ResolveInput{CWD: env.root},
		TaskPath:     env.demoTaskRef(t, "01-a.md"),
	})
	assertExitCode(t, err, ExitNoRunnable)
	if !strings.Contains(err.Error(), "is skipped") {
		t.Fatalf("err = %v", err)
	}
}

func TestSkipTaskUnblocksDependent(t *testing.T) {
	env := setupCustomTaskFixture(t, []Task{
		{ID: "01-a", File: "01-a.md", Title: "A", Type: "HITL", Status: "open"},
		{ID: "02-b", File: "02-b.md", Title: "B", Type: "AFK", Status: "open", BlockedBy: []string{"01-a"}},
	})

	result, err := SkipTaskWith(env.deps(), nil, nil, SkipTaskOptions{
		ResolveInput: ResolveInput{CWD: env.root},
		TaskPath:     env.demoTaskRef(t, "01-a.md"),
	})
	if err != nil {
		t.Fatal(err)
	}

	row := findRow(result.Refresh, "demo")
	if row == nil || row.Status != StatusReady {
		t.Fatalf("set status = %v, want READY", row)
	}

	sel, err := SelectTaskInSet(result.Refresh, "demo")
	if err != nil {
		t.Fatalf("select after skip: %v", err)
	}
	if sel.TaskID != "02-b" {
		t.Fatalf("selected %q, want 02-b", sel.TaskID)
	}
}

func TestSkippedTaskNotSelectedExplicit(t *testing.T) {
	env := setupCustomTaskFixture(t, []Task{
		{ID: "01-a", File: "01-a.md", Title: "A", Type: "AFK", Status: "skipped"},
	})

	refresh, err := RefreshWith(env.deps(), env.tasksDir, DefaultStatePathWith(env.deps()))
	if err != nil {
		t.Fatal(err)
	}

	_, err = SelectTask(refresh, "demo", "01-a")
	assertExitCode(t, err, ExitNoRunnable)
	if !strings.Contains(err.Error(), "is skipped") {
		t.Fatalf("err = %v", err)
	}
}

func TestSkippedTaskNotSelectedAutomatic(t *testing.T) {
	env := setupCustomTaskFixture(t, []Task{
		{ID: "01-a", File: "01-a.md", Title: "A", Type: "AFK", Status: "skipped"},
	})

	refresh, err := RefreshWith(env.deps(), env.tasksDir, DefaultStatePathWith(env.deps()))
	if err != nil {
		t.Fatal(err)
	}

	if _, err := firstEligibleTask("demo", refresh.Manifests["demo"]); err == nil {
		t.Fatal("skipped task was selected as eligible")
	}
}

func TestSkipTaskRejectsBareIdentifier(t *testing.T) {
	env := setupExecutorFixture(t, false)

	_, err := SkipTaskWith(env.deps(), nil, nil, SkipTaskOptions{
		ResolveInput: ResolveInput{CWD: env.root},
		TaskPath:     "01-a",
	})
	assertExitCode(t, err, ExitSetup)
}

func TestSkipTaskRejectsAbsolutePath(t *testing.T) {
	env := setupExecutorFixture(t, false)

	_, err := SkipTaskWith(env.deps(), nil, nil, SkipTaskOptions{
		ResolveInput: ResolveInput{CWD: env.root},
		TaskPath:     filepath.Join(env.demoDir(), "01-a.md"),
	})
	assertExitCode(t, err, ExitSetup)
}

func TestSkippedCountInProgressText(t *testing.T) {
	m := &Manifest{
		Valid: true,
		Tasks: []Task{
			{ID: "01-a", Type: "AFK", Status: "done"},
			{ID: "02-b", Type: "HITL", Status: "skipped"},
		},
	}
	got := BuildProgress(m, DeriveStatus(m))
	if !strings.Contains(got, "1 skipped") {
		t.Fatalf("progress = %q, want %q segment", got, "1 skipped")
	}
}
