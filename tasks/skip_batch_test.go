package tasks

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/glebglazov/pop/internal/deps"
)

func TestBuildSkipSelectionThreeWaySplit(t *testing.T) {
	m := &Manifest{Tasks: []Task{
		{ID: "01-a", File: "01-a.md", Title: "A", Type: "AFK", Status: "open"},
		{ID: "02-b", File: "02-b.md", Title: "B", Type: "AFK", Status: "skipped"},
		{ID: "03-c", File: "03-c.md", Title: "C", Type: "AFK", Status: "done"},
		{ID: "04-d", File: "04-d.md", Title: "D", Type: "AFK", Status: "failed"},
	}}

	rows := BuildSkipSelection(m)
	if len(rows) != 4 {
		t.Fatalf("rows = %d, want 4", len(rows))
	}
	// Open is checkable.
	if rows[0].Locked {
		t.Fatalf("open should be checkable: %+v", rows[0])
	}
	// Skipped is locked at-target with a mark.
	if !rows[1].Locked || rows[1].LockedMark == "" {
		t.Fatalf("skipped should be locked at-target with a mark: %+v", rows[1])
	}
	// Done and Failed are inert locked.
	if !rows[2].Locked || !rows[3].Locked {
		t.Fatalf("done/failed should be inert-locked: %+v %+v", rows[2], rows[3])
	}
	if rows[1].LockedMark == rows[2].LockedMark {
		t.Fatalf("at-target and inert marks should differ: skipped=%q done=%q", rows[1].LockedMark, rows[2].LockedMark)
	}
}

func TestSkipTasksBatchApply(t *testing.T) {
	env := setupCustomTaskFixture(t, []Task{
		{ID: "01-a", File: "01-a.md", Title: "A", Type: "AFK", Status: "open"},
		{ID: "02-b", File: "02-b.md", Title: "B", Type: "HITL", Status: "open"},
		{ID: "03-c", File: "03-c.md", Title: "C", Type: "AFK", Status: "skipped"},
	})

	result, err := SkipTasksWith(env.deps(), nil, nil, SkipTasksOptions{
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
	if result.Transitions[0].Prior != "open" || result.Transitions[1].Prior != "open" {
		t.Fatalf("priors wrong: %+v", result.Transitions)
	}
	assertTaskSkipped(t, env, "01-a")
	assertTaskSkipped(t, env, "02-b")

	// One SKIP progress record per task.
	data, err := os.ReadFile(filepath.Join(env.demoDir(), "progress.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if c := strings.Count(string(data), "SKIP"); c != 2 {
		t.Fatalf("SKIP records = %d, want 2:\n%s", c, data)
	}
}

func TestSkipTasksOneManifestWriteAfterAllProgress(t *testing.T) {
	env := setupCustomTaskFixture(t, []Task{
		{ID: "01-a", File: "01-a.md", Title: "A", Type: "AFK", Status: "open"},
		{ID: "02-b", File: "02-b.md", Title: "B", Type: "AFK", Status: "open"},
	})
	order := &writeOrderTracker{}
	d := env.deps()
	d.FS = &atomicBlockingFS{FileSystem: deps.NewRealFileSystem(), tracker: order}

	_, err := SkipTasksWith(d, nil, nil, SkipTasksOptions{
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

func TestSkipTasksAlreadySkippedRejected(t *testing.T) {
	env := setupCustomTaskFixture(t, []Task{
		{ID: "01-a", File: "01-a.md", Title: "A", Type: "AFK", Status: "skipped"},
	})

	_, err := SkipTasksWith(env.deps(), nil, nil, SkipTasksOptions{
		ResolveInput:    ResolveInput{CWD: env.root},
		TaskSetTarget:   "demo",
		SelectedTaskIDs: []string{"01-a"},
	})
	assertExitCode(t, err, ExitNoRunnable)
	if !strings.Contains(err.Error(), "already skipped") {
		t.Fatalf("err = %v, want already-skipped rejection", err)
	}
}

func TestSkipTasksDoneRejectedBeforeAnyWrite(t *testing.T) {
	env := setupCustomTaskFixture(t, []Task{
		{ID: "01-a", File: "01-a.md", Title: "A", Type: "AFK", Status: "open"},
		{ID: "02-b", File: "02-b.md", Title: "B", Type: "AFK", Status: "done"},
	})

	_, err := SkipTasksWith(env.deps(), nil, nil, SkipTasksOptions{
		ResolveInput:    ResolveInput{CWD: env.root},
		TaskSetTarget:   "demo",
		SelectedTaskIDs: []string{"01-a", "02-b"},
	})
	assertExitCode(t, err, ExitNoRunnable)
	if !strings.Contains(err.Error(), "02-b") || !strings.Contains(err.Error(), "done") {
		t.Fatalf("err = %v, want it to name the Done offender 02-b", err)
	}

	// Whole batch rejected before any write — the eligible task stays open.
	assertTaskOpen(t, env, "01-a")
	if _, err := os.Stat(filepath.Join(env.demoDir(), "progress.txt")); !os.IsNotExist(err) {
		t.Fatalf("progress.txt should not exist after rejected batch (err=%v)", err)
	}
}

func TestSkipTasksEmptySelectionNoop(t *testing.T) {
	env := setupCustomTaskFixture(t, []Task{
		{ID: "01-a", File: "01-a.md", Title: "A", Type: "AFK", Status: "open"},
	})

	result, err := SkipTasksWith(env.deps(), nil, nil, SkipTasksOptions{
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

func TestSkipTasksTrailingSlashTarget(t *testing.T) {
	env := setupCustomTaskFixture(t, []Task{
		{ID: "01-a", File: "01-a.md", Title: "A", Type: "AFK", Status: "open"},
	})

	_, err := SkipTasksWith(env.deps(), nil, nil, SkipTasksOptions{
		ResolveInput:    ResolveInput{CWD: env.root},
		TaskSetTarget:   "demo/",
		SelectedTaskIDs: []string{"01-a"},
	})
	if err != nil {
		t.Fatalf("trailing-slash whole-set target should resolve: %v", err)
	}
	assertTaskSkipped(t, env, "01-a")
}
