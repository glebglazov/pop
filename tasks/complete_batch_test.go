package tasks

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/glebglazov/pop/internal/deps"
)

func TestBuildCompleteSelectionLocksDone(t *testing.T) {
	m := &Manifest{Tasks: []Task{
		{ID: "01-a", File: "01-a.md", Title: "A", Type: "AFK", Status: "done"},
		{ID: "02-b", File: "02-b.md", Title: "B", Type: "AFK", Status: "open"},
		{ID: "03-c", File: "03-c.md", Title: "C", Type: "HITL", Status: "failed"},
	}}

	rows := BuildCompleteSelection(m)
	if len(rows) != 3 {
		t.Fatalf("rows = %d, want 3", len(rows))
	}
	if rows[0].TaskID != "01-a" || !rows[0].Locked {
		t.Fatalf("row 0 = %+v, want 01-a locked", rows[0])
	}
	if rows[1].TaskID != "02-b" || rows[1].Locked {
		t.Fatalf("row 1 = %+v, want 02-b unlocked", rows[1])
	}
	if rows[2].TaskID != "03-c" || rows[2].Locked {
		t.Fatalf("row 2 = %+v, want 03-c unlocked (failed is checkable)", rows[2])
	}
}

func TestCompleteTasksTopologicalApply(t *testing.T) {
	env := setupCustomTaskFixture(t, []Task{
		{ID: "02-b", File: "02-b.md", Title: "B", Type: "AFK", Status: "open", BlockedBy: []string{"01-a"}},
		{ID: "01-a", File: "01-a.md", Title: "A", Type: "AFK", Status: "open"},
	})

	// Select in dependency-reverse order; apply must still order 01-a before 02-b.
	result, err := CompleteTasksWith(env.deps(), nil, nil, CompleteTasksOptions{
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
	if result.Transitions[0].TaskID != "01-a" || result.Transitions[1].TaskID != "02-b" {
		t.Fatalf("transitions out of topological order: %+v", result.Transitions)
	}
	assertTaskDone(t, env, "01-a")
	assertTaskDone(t, env, "02-b")

	// Two COMPLETE progress records, blocker first.
	data, err := os.ReadFile(filepath.Join(env.demoDir(), "progress.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if c := strings.Count(string(data), "COMPLETE"); c != 2 {
		t.Fatalf("COMPLETE records = %d, want 2:\n%s", c, data)
	}
	if strings.Index(string(data), "01-a") > strings.Index(string(data), "02-b") {
		t.Fatalf("progress not in topological order:\n%s", data)
	}
}

func TestCompleteTasksOneManifestWriteAfterAllProgress(t *testing.T) {
	env := setupCustomTaskFixture(t, []Task{
		{ID: "01-a", File: "01-a.md", Title: "A", Type: "AFK", Status: "open"},
		{ID: "02-b", File: "02-b.md", Title: "B", Type: "AFK", Status: "open", BlockedBy: []string{"01-a"}},
	})
	order := &writeOrderTracker{}
	d := env.deps()
	d.FS = &atomicBlockingFS{FileSystem: deps.NewRealFileSystem(), tracker: order}

	_, err := CompleteTasksWith(d, nil, nil, CompleteTasksOptions{
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

func TestCompleteTasksBlockerRejectionNamesOffender(t *testing.T) {
	env := setupCustomTaskFixture(t, []Task{
		{ID: "01-a", File: "01-a.md", Title: "A", Type: "AFK", Status: "open"},
		{ID: "02-b", File: "02-b.md", Title: "B", Type: "AFK", Status: "open", BlockedBy: []string{"01-a"}},
	})

	_, err := CompleteTasksWith(env.deps(), nil, nil, CompleteTasksOptions{
		ResolveInput:    ResolveInput{CWD: env.root},
		TaskSetTarget:   "demo",
		SelectedTaskIDs: []string{"02-b"},
	})
	assertExitCode(t, err, ExitNoRunnable)
	if !strings.Contains(err.Error(), "blocked by 01-a") {
		t.Fatalf("err = %v, want it to name offender 01-a", err)
	}

	// Whole batch rejected before any write.
	assertTaskOpen(t, env, "01-a")
	assertTaskOpen(t, env, "02-b")
	if _, err := os.Stat(filepath.Join(env.demoDir(), "progress.txt")); !os.IsNotExist(err) {
		t.Fatalf("progress.txt should not exist after rejected batch (err=%v)", err)
	}
}

func TestCompleteTasksSkippedBlockerSatisfies(t *testing.T) {
	env := setupCustomTaskFixture(t, []Task{
		{ID: "01-a", File: "01-a.md", Title: "A", Type: "HITL", Status: "skipped"},
		{ID: "02-b", File: "02-b.md", Title: "B", Type: "AFK", Status: "open", BlockedBy: []string{"01-a"}},
	})

	_, err := CompleteTasksWith(env.deps(), nil, nil, CompleteTasksOptions{
		ResolveInput:    ResolveInput{CWD: env.root},
		TaskSetTarget:   "demo",
		SelectedTaskIDs: []string{"02-b"},
	})
	if err != nil {
		t.Fatalf("skipped blocker should satisfy: %v", err)
	}
	assertTaskDone(t, env, "02-b")
}

func TestCompleteTasksAlreadyDoneRejected(t *testing.T) {
	env := setupCustomTaskFixture(t, []Task{
		{ID: "01-a", File: "01-a.md", Title: "A", Type: "AFK", Status: "done"},
	})

	_, err := CompleteTasksWith(env.deps(), nil, nil, CompleteTasksOptions{
		ResolveInput:    ResolveInput{CWD: env.root},
		TaskSetTarget:   "demo",
		SelectedTaskIDs: []string{"01-a"},
	})
	assertExitCode(t, err, ExitNoRunnable)
	if !strings.Contains(err.Error(), "already done") {
		t.Fatalf("err = %v", err)
	}
}

func TestCompleteTasksEmptySelectionNoop(t *testing.T) {
	env := setupCustomTaskFixture(t, []Task{
		{ID: "01-a", File: "01-a.md", Title: "A", Type: "AFK", Status: "open"},
	})

	result, err := CompleteTasksWith(env.deps(), nil, nil, CompleteTasksOptions{
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
	assertTaskOpen(t, env, "01-a")
	if _, err := os.Stat(filepath.Join(env.demoDir(), "progress.txt")); !os.IsNotExist(err) {
		t.Fatalf("empty selection must write nothing (progress err=%v)", err)
	}
}

func TestCompleteTasksTrailingSlashTarget(t *testing.T) {
	env := setupCustomTaskFixture(t, []Task{
		{ID: "01-a", File: "01-a.md", Title: "A", Type: "AFK", Status: "open"},
	})

	_, err := CompleteTasksWith(env.deps(), nil, nil, CompleteTasksOptions{
		ResolveInput:    ResolveInput{CWD: env.root},
		TaskSetTarget:   "demo/",
		SelectedTaskIDs: []string{"01-a"},
	})
	if err != nil {
		t.Fatalf("trailing-slash whole-set target should resolve: %v", err)
	}
	assertTaskDone(t, env, "01-a")
}
