package tasks

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/glebglazov/pop/internal/deps"
)

// newTransitionManifest builds a bare Manifest rooted in a fresh temp dir. The
// chokepoint only writes progress.txt and index.json under Dir; it never reads
// the task markdown files, so they need not exist on disk.
func newTransitionManifest(t *testing.T, tasks []Task) *Manifest {
	t.Helper()
	dir := t.TempDir()
	return &Manifest{
		Stem:  "demo",
		Dir:   dir,
		Path:  filepath.Join(dir, "index.json"),
		Tasks: tasks,
		Valid: true,
	}
}

func TestApplyTransitionsLegalEdgesPerActor(t *testing.T) {
	cases := []struct {
		from  TaskStatus
		to    TaskStatus
		actor TransitionActor
	}{
		{TaskOpen, TaskDone, ActorExecutor},
		{TaskOpen, TaskFailed, ActorExecutor},
		{TaskOpen, TaskDone, ActorHuman},
		{TaskFailed, TaskOpen, ActorHuman},
		{TaskFailed, TaskDone, ActorHuman},
		{TaskOpen, TaskSkipped, ActorHuman},
		{TaskSkipped, TaskOpen, ActorHuman},
		{TaskSkipped, TaskDone, ActorHuman},
		{TaskDone, TaskOpen, ActorHuman},
	}

	for _, tc := range cases {
		t.Run(string(tc.actor)+"_"+string(tc.from)+"_to_"+string(tc.to), func(t *testing.T) {
			d := realFSDeps()
			m := newTransitionManifest(t, []Task{
				{ID: "01-a", File: "01-a.md", Title: "A", Type: "AFK", Status: tc.from},
			})
			err := ApplyTransitions(d, m, "", []TransitionOp{{
				TaskID:  "01-a",
				To:      tc.to,
				Actor:   tc.actor,
				Marker:  "MARK",
				Summary: "did the thing",
			}})
			if err != nil {
				t.Fatalf("legal edge %s→%s by %s rejected: %v", tc.from, tc.to, tc.actor, err)
			}
			if got := m.Tasks[0].Status; got != tc.to {
				t.Fatalf("status = %q, want %q", got, tc.to)
			}
			// Manifest and progress were written.
			if _, statErr := os.Stat(m.Path); statErr != nil {
				t.Fatalf("manifest not written: %v", statErr)
			}
			progress, readErr := os.ReadFile(filepath.Join(m.Dir, "progress.txt"))
			if readErr != nil {
				t.Fatalf("progress not written: %v", readErr)
			}
			if !strings.Contains(string(progress), "MARK") || !strings.Contains(string(progress), "did the thing") {
				t.Fatalf("progress record = %q", progress)
			}
		})
	}
}

func TestApplyTransitionsIllegalEdgesRejected(t *testing.T) {
	cases := []struct {
		from  TaskStatus
		to    TaskStatus
		actor TransitionActor
	}{
		// Executor attempting human-only edges.
		{TaskFailed, TaskOpen, ActorExecutor},
		{TaskFailed, TaskDone, ActorExecutor},
		{TaskOpen, TaskSkipped, ActorExecutor},
		{TaskSkipped, TaskDone, ActorExecutor},
		{TaskDone, TaskOpen, ActorExecutor},
		// Edges no actor may drive.
		{TaskDone, TaskDone, ActorHuman},
		{TaskDone, TaskFailed, ActorHuman},
		{TaskSkipped, TaskFailed, ActorHuman},
		{TaskOpen, TaskOpen, ActorHuman},
	}

	for _, tc := range cases {
		t.Run(string(tc.actor)+"_"+string(tc.from)+"_to_"+string(tc.to), func(t *testing.T) {
			d := realFSDeps()
			m := newTransitionManifest(t, []Task{
				{ID: "01-a", File: "01-a.md", Title: "A", Type: "AFK", Status: tc.from},
			})
			err := ApplyTransitions(d, m, "", []TransitionOp{{
				TaskID:  "01-a",
				To:      tc.to,
				Actor:   tc.actor,
				Marker:  "MARK",
				Summary: "should not apply",
			}})
			if err == nil {
				t.Fatalf("illegal edge %s→%s by %s accepted", tc.from, tc.to, tc.actor)
			}
			// Error names the illegal edge.
			msg := err.Error()
			if !strings.Contains(msg, string(tc.from)) || !strings.Contains(msg, string(tc.to)) || !strings.Contains(msg, string(tc.actor)) {
				t.Fatalf("error does not name edge: %v", err)
			}
			// Nothing was written: status unchanged, no manifest, no progress.
			if got := m.Tasks[0].Status; got != tc.from {
				t.Fatalf("status mutated to %q on rejected edge", got)
			}
			if _, statErr := os.Stat(m.Path); !os.IsNotExist(statErr) {
				t.Fatalf("manifest written on rejected edge: %v", statErr)
			}
			if _, statErr := os.Stat(filepath.Join(m.Dir, "progress.txt")); !os.IsNotExist(statErr) {
				t.Fatalf("progress written on rejected edge: %v", statErr)
			}
		})
	}
}

func TestApplyTransitionsAttemptCountSetOnFailed(t *testing.T) {
	d := realFSDeps()
	m := newTransitionManifest(t, []Task{
		{ID: "01-a", File: "01-a.md", Title: "A", Type: "AFK", Status: "open"},
	})
	err := ApplyTransitions(d, m, "", []TransitionOp{{
		TaskID:       "01-a",
		To:           TaskFailed,
		Actor:        ActorExecutor,
		Marker:       "FAILED",
		Summary:      "gave up",
		AttemptCount: 3,
	}})
	if err != nil {
		t.Fatal(err)
	}
	if m.Tasks[0].FailedAfter == nil || *m.Tasks[0].FailedAfter != 3 {
		t.Fatalf("FailedAfter = %v, want 3", m.Tasks[0].FailedAfter)
	}
}

func TestApplyTransitionsAttemptCountClearedOnNonFailed(t *testing.T) {
	prior := 5
	for _, to := range []struct {
		status TaskStatus
		actor  TransitionActor
	}{
		{TaskOpen, ActorHuman},
		{TaskDone, ActorHuman},
	} {
		t.Run(string(to.status), func(t *testing.T) {
			d := realFSDeps()
			m := newTransitionManifest(t, []Task{
				{ID: "01-a", File: "01-a.md", Title: "A", Type: "AFK", Status: "failed", FailedAfter: &prior},
			})
			err := ApplyTransitions(d, m, "", []TransitionOp{{
				TaskID:  "01-a",
				To:      to.status,
				Actor:   to.actor,
				Marker:  "MARK",
				Summary: "moved off failed",
			}})
			if err != nil {
				t.Fatal(err)
			}
			if m.Tasks[0].FailedAfter != nil {
				t.Fatalf("FailedAfter = %v, want nil after →%s", *m.Tasks[0].FailedAfter, to.status)
			}
		})
	}
}

func TestApplyTransitionsBatchAtomicOneWrite(t *testing.T) {
	dir := t.TempDir()
	tracker := &writeOrderTracker{}
	d := &Deps{FS: &atomicBlockingFS{
		FileSystem: deps.NewRealFileSystem(),
		tracker:    tracker,
	}}
	m := &Manifest{
		Stem: "demo",
		Dir:  dir,
		Path: filepath.Join(dir, "index.json"),
		Tasks: []Task{
			{ID: "01-a", File: "01-a.md", Title: "A", Type: "AFK", Status: "open"},
			{ID: "02-b", File: "02-b.md", Title: "B", Type: "AFK", Status: "open"},
			{ID: "03-c", File: "03-c.md", Title: "C", Type: "AFK", Status: "open"},
		},
		Valid: true,
	}

	ops := make([]TransitionOp, len(m.Tasks))
	for i, task := range m.Tasks {
		ops[i] = TransitionOp{TaskID: task.ID, To: TaskDone, Actor: ActorHuman, Marker: "COMPLETE", Summary: "done " + task.ID}
	}
	if err := ApplyTransitions(d, m, "", ops); err != nil {
		t.Fatal(err)
	}

	var manifestWrites, progressWrites int
	for _, ev := range tracker.events {
		switch ev {
		case "manifest":
			manifestWrites++
		case "progress":
			progressWrites++
		}
	}
	if manifestWrites != 1 {
		t.Fatalf("manifest writes = %d, want exactly 1 for %d ops", manifestWrites, len(ops))
	}
	if progressWrites != len(ops) {
		t.Fatalf("progress writes = %d, want %d (one per op)", progressWrites, len(ops))
	}
	if tracker.last != "manifest" {
		t.Fatalf("last write = %q, want manifest (progress records precede the single manifest write)", tracker.last)
	}
	for _, task := range m.Tasks {
		if task.Status != "done" {
			t.Fatalf("task %q status = %q, want done", task.ID, task.Status)
		}
	}
}

func TestApplyTransitionsBatchRejectsWholeBatchOnIllegalEdge(t *testing.T) {
	d := realFSDeps()
	m := newTransitionManifest(t, []Task{
		{ID: "01-a", File: "01-a.md", Title: "A", Type: "AFK", Status: "open"},
		{ID: "02-b", File: "02-b.md", Title: "B", Type: "AFK", Status: "done"},
	})
	// First op legal (open→done human), second illegal (done→failed human).
	err := ApplyTransitions(d, m, "", []TransitionOp{
		{TaskID: "01-a", To: TaskDone, Actor: ActorHuman, Marker: "COMPLETE", Summary: "a"},
		{TaskID: "02-b", To: TaskFailed, Actor: ActorHuman, Marker: "COMPLETE", Summary: "b"},
	})
	if err == nil {
		t.Fatal("batch with an illegal edge accepted")
	}
	if m.Tasks[0].Status != "open" || m.Tasks[1].Status != "done" {
		t.Fatalf("statuses mutated on rejected batch: %q, %q", m.Tasks[0].Status, m.Tasks[1].Status)
	}
	if _, statErr := os.Stat(m.Path); !os.IsNotExist(statErr) {
		t.Fatalf("manifest written on rejected batch: %v", statErr)
	}
	if _, statErr := os.Stat(filepath.Join(m.Dir, "progress.txt")); !os.IsNotExist(statErr) {
		t.Fatalf("progress written on rejected batch: %v", statErr)
	}
}

func TestApplyTransitionsUnknownTaskRejected(t *testing.T) {
	d := realFSDeps()
	m := newTransitionManifest(t, []Task{
		{ID: "01-a", File: "01-a.md", Title: "A", Type: "AFK", Status: "open"},
	})
	err := ApplyTransitions(d, m, "", []TransitionOp{{
		TaskID: "99-z", To: TaskDone, Actor: ActorHuman, Marker: "COMPLETE", Summary: "x",
	}})
	if err == nil {
		t.Fatal("unknown task accepted")
	}
	if !strings.Contains(err.Error(), "99-z") {
		t.Fatalf("error does not name unknown task: %v", err)
	}
	if _, statErr := os.Stat(m.Path); !os.IsNotExist(statErr) {
		t.Fatalf("manifest written on unknown task: %v", statErr)
	}
}
