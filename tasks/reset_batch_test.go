package tasks

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/glebglazov/pop/internal/deps"
)

func assertOpenTaskStatus(t *testing.T, env *execFixture, taskID string, want TaskStatus) {
	t.Helper()
	m := LoadManifest(DefaultDeps(), "demo", env.demoManifest())
	for _, task := range m.Tasks {
		if task.ID == taskID && task.Status != want {
			t.Fatalf("task %s status = %q, want %q", taskID, task.Status, want)
		}
	}
}

func TestBuildOpenSelectionThreeWaySplit(t *testing.T) {
	m := &Manifest{Tasks: []Task{
		{ID: "01-a", File: "01-a.md", Title: "A", Type: "AFK", Status: "failed"},
		{ID: "02-b", File: "02-b.md", Title: "B", Type: "AFK", Status: "skipped"},
		{ID: "03-c", File: "03-c.md", Title: "C", Type: "AFK", Status: "open"},
		{ID: "04-d", File: "04-d.md", Title: "D", Type: "AFK", Status: "done"},
	}}

	rows := BuildOpenSelection(m)
	if len(rows) != 4 {
		t.Fatalf("rows = %d, want 4", len(rows))
	}
	// Failed, Skipped, and Done are all checkable (ADR-0053).
	if rows[0].Locked || rows[1].Locked || rows[3].Locked {
		t.Fatalf("failed/skipped/done should be checkable: %+v %+v %+v", rows[0], rows[1], rows[3])
	}
	// Open is the only locked-at-target row, with a mark.
	if !rows[2].Locked || rows[2].LockedMark == "" {
		t.Fatalf("open should be locked at-target with a mark: %+v", rows[2])
	}
}

func TestOpenTasksBatchApply(t *testing.T) {
	failedAfter := 3
	env := setupCustomTaskFixture(t, []Task{
		{ID: "01-a", File: "01-a.md", Title: "A", Type: "AFK", Status: "failed", FailedAfter: &failedAfter},
		{ID: "02-b", File: "02-b.md", Title: "B", Type: "HITL", Status: "skipped"},
		{ID: "03-c", File: "03-c.md", Title: "C", Type: "AFK", Status: "open"},
	})

	result, err := OpenTasksWith(env.deps(), nil, nil, OpenTasksOptions{
		ResolveInput:    ResolveInput{CWD: env.root},
		TaskSetTarget:   "demo",
		SelectedTaskIDs: []string{"02-b", "01-a"},
	})
	if err != nil {
		t.Fatal(err)
	}

	if len(result.Transitions) != 2 {
		t.Fatalf("transitions = %d, want 2", len(result.Transitions))
	}
	// Applied in manifest order regardless of selection order.
	if result.Transitions[0].TaskID != "01-a" || result.Transitions[1].TaskID != "02-b" {
		t.Fatalf("transitions not in manifest order: %+v", result.Transitions)
	}
	if result.Transitions[0].Prior != "failed" || result.Transitions[1].Prior != "skipped" {
		t.Fatalf("priors wrong: %+v", result.Transitions)
	}
	assertTaskOpen(t, env, "01-a")
	assertTaskOpen(t, env, "02-b")

	// failed_after cleared on reopen.
	m := LoadManifest(DefaultDeps(), "demo", env.demoManifest())
	for _, task := range m.Tasks {
		if task.ID == "01-a" && task.FailedAfter != nil {
			t.Fatalf("failed_after not cleared: %v", task.FailedAfter)
		}
	}

	// One RESET progress record per task.
	data, err := os.ReadFile(filepath.Join(env.demoDir(), "progress.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if c := strings.Count(string(data), "RESET"); c != 2 {
		t.Fatalf("RESET records = %d, want 2:\n%s", c, data)
	}
}

func TestOpenTasksOneManifestWriteAfterAllProgress(t *testing.T) {
	env := setupCustomTaskFixture(t, []Task{
		{ID: "01-a", File: "01-a.md", Title: "A", Type: "AFK", Status: "failed"},
		{ID: "02-b", File: "02-b.md", Title: "B", Type: "AFK", Status: "skipped"},
	})
	order := &writeOrderTracker{}
	d := env.deps()
	d.FS = &atomicBlockingFS{FileSystem: deps.NewRealFileSystem(), tracker: order}

	_, err := OpenTasksWith(d, nil, nil, OpenTasksOptions{
		ResolveInput:    ResolveInput{CWD: env.root},
		TaskSetTarget:   "demo",
		SelectedTaskIDs: []string{"01-a", "02-b"},
	})
	if err != nil {
		t.Fatal(err)
	}

	manifestWrites := 0
	for _, e := range order.events {
		if e == "manifest" {
			manifestWrites++
		}
	}
	if manifestWrites != 1 {
		t.Fatalf("manifest writes = %d, want exactly 1: %v", manifestWrites, order.events)
	}
	if order.last != "manifest" {
		t.Fatalf("last write = %q, want manifest: %v", order.last, order.events)
	}
	if order.events[0] != "progress" || order.events[1] != "progress" {
		t.Fatalf("progress records must precede the manifest write: %v", order.events)
	}
}

func TestOpenTasksAlreadyOpenRejected(t *testing.T) {
	env := setupCustomTaskFixture(t, []Task{
		{ID: "01-a", File: "01-a.md", Title: "A", Type: "AFK", Status: "open"},
	})

	_, err := OpenTasksWith(env.deps(), nil, nil, OpenTasksOptions{
		ResolveInput:    ResolveInput{CWD: env.root},
		TaskSetTarget:   "demo",
		SelectedTaskIDs: []string{"01-a"},
	})
	assertExitCode(t, err, ExitNoRunnable)
	if !strings.Contains(err.Error(), "already open") {
		t.Fatalf("err = %v, want already-open rejection", err)
	}
}

func TestOpenTasksDoneReopensInBatch(t *testing.T) {
	// A Done task reopens like Failed/Skipped — it is the inverse of complete
	// (ADR-0053). Mixing Failed and Done in one batch reopens both.
	env := setupCustomTaskFixture(t, []Task{
		{ID: "01-a", File: "01-a.md", Title: "A", Type: "AFK", Status: "failed"},
		{ID: "02-b", File: "02-b.md", Title: "B", Type: "HITL", Status: "done"},
	})

	result, err := OpenTasksWith(env.deps(), nil, nil, OpenTasksOptions{
		ResolveInput:    ResolveInput{CWD: env.root},
		TaskSetTarget:   "demo",
		SelectedTaskIDs: []string{"01-a", "02-b"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Transitions) != 2 {
		t.Fatalf("transitions = %d, want 2", len(result.Transitions))
	}
	if result.Transitions[0].Prior != "failed" || result.Transitions[1].Prior != "done" {
		t.Fatalf("priors wrong: %+v", result.Transitions)
	}
	assertTaskOpen(t, env, "01-a")
	assertTaskOpen(t, env, "02-b")
}

func TestOpenTasksEmptySelectionNoop(t *testing.T) {
	env := setupCustomTaskFixture(t, []Task{
		{ID: "01-a", File: "01-a.md", Title: "A", Type: "AFK", Status: "failed"},
	})

	result, err := OpenTasksWith(env.deps(), nil, nil, OpenTasksOptions{
		ResolveInput:    ResolveInput{CWD: env.root},
		TaskSetTarget:   "demo",
		SelectedTaskIDs: nil,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Transitions) != 0 {
		t.Fatalf("transitions = %v, want none", result.Transitions)
	}
	assertOpenTaskStatus(t, env, "01-a", "failed")
	if _, err := os.Stat(filepath.Join(env.demoDir(), "progress.txt")); !os.IsNotExist(err) {
		t.Fatalf("empty selection must write nothing (progress err=%v)", err)
	}
}

func TestOpenTasksTrailingSlashTarget(t *testing.T) {
	env := setupCustomTaskFixture(t, []Task{
		{ID: "01-a", File: "01-a.md", Title: "A", Type: "AFK", Status: "skipped"},
	})

	_, err := OpenTasksWith(env.deps(), nil, nil, OpenTasksOptions{
		ResolveInput:    ResolveInput{CWD: env.root},
		TaskSetTarget:   "demo/",
		SelectedTaskIDs: []string{"01-a"},
	})
	if err != nil {
		t.Fatalf("trailing-slash whole-set target should resolve: %v", err)
	}
	assertTaskOpen(t, env, "01-a")
}
