package supervisor

import (
	"bytes"
	"github.com/glebglazov/pop/tasks"
	"strings"
	"testing"
	"time"

	"github.com/glebglazov/pop/internal/queuetest"
	"github.com/glebglazov/pop/routine"
	"github.com/glebglazov/pop/store"
)

// TestBuildLogFromStore checks the Queue journal view is derived from Drain
// transitions, integration events, and park-clears in the store — there is no
// standalone journal file (ADR-0055).
func TestBuildLogFromStore(t *testing.T) {
	td := queuetest.DataDeps(t)
	repo := initGitRepoWithBase(t)
	commonDir := queuetest.RepoCommonDir(t, td, repo)

	// A drain that quota-paused: contributes a spawn and a quota_paused outcome.
	h, err := tasks.BeginDrain(td, repo, "set-1", nil)
	if err != nil {
		t.Fatalf("BeginDrain: %v", err)
	}
	if err := h.Finish(store.StateQuotaPaused, "codex", false, time.Time{}); err != nil {
		t.Fatalf("Finish: %v", err)
	}
	// A park-clear (unpark) event.
	if err := tasks.RecordParkClear(td, commonDir, "set-1"); err != nil {
		t.Fatalf("RecordParkClear: %v", err)
	}
	// An integration event.
	if err := tasks.RecordIntegrationEvent(td, tasks.IntegrationEvent{
		ScopedKey: "k", SetID: "set-2", Project: "pop", BaseRef: "main", BranchSHA: "abc",
	}); err != nil {
		t.Fatalf("RecordIntegrationEvent: %v", err)
	}

	events, err := BuildLog(td)
	if err != nil {
		t.Fatalf("BuildLog: %v", err)
	}
	var out bytes.Buffer
	RenderLog(&out, events, 50)
	text := out.String()
	for _, want := range []string{
		"set-1 spawned",
		"set-1 quota_paused agent=codex",
		"set-1 unparked",
		"set-2 integrated base=main",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("log output missing %q:\n%s", want, text)
		}
	}
}

// TestBuildLogCarriesRoutineFiresAndSkips pins the one-journal property: every
// Routine decision the daemon made reaches `pop work log` beside the Drain
// events — a fire and its outcome, an overlap skip, and the skip a
// run-affecting-fingerprint drift pause stands for — each carrying the reason it
// was recorded with.
func TestBuildLogCarriesRoutineFiresAndSkips(t *testing.T) {
	td := queuetest.DataDeps(t)
	s, _, err := td.Store(true)
	if err != nil {
		t.Fatalf("Store: %v", err)
	}
	fired := time.Now().Add(-time.Hour).UTC()

	run, err := s.StartRoutineRun(store.RoutineRun{RoutineID: "nightly", FiredAt: fired}, nil)
	if err != nil {
		t.Fatalf("StartRoutineRun: %v", err)
	}
	if err := s.FinishRoutineRun(run.ID, store.RoutineRunFailed, "", "agent exited 1", fired.Add(time.Minute)); err != nil {
		t.Fatalf("FinishRoutineRun: %v", err)
	}
	for _, skip := range []struct {
		id     string
		reason string
	}{
		{"busy", routine.SkipReasonOverlap},
		{"drifted", routine.SkipReasonChanged},
	} {
		if _, err := s.InsertSkippedRoutineRun(store.RoutineRun{
			RoutineID: skip.id, FiredAt: fired, SkipReason: skip.reason,
		}); err != nil {
			t.Fatalf("InsertSkippedRoutineRun(%s): %v", skip.id, err)
		}
	}

	events, err := BuildLog(td)
	if err != nil {
		t.Fatalf("BuildLog: %v", err)
	}
	var out bytes.Buffer
	RenderLog(&out, events, 50)
	text := out.String()
	for _, want := range []string{
		"nightly fired",
		"nightly failed agent exited 1",
		"busy skipped " + routine.SkipReasonOverlap,
		"drifted skipped " + routine.SkipReasonChanged,
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("log output missing %q:\n%s", want, text)
		}
	}
}

// TestBuildLogDoesNotPoisonSharedHandle guards the regression from ADR-0140:
// building the journal borrows the process-cached store handle in if-exists mode
// to read routine runs and must never close it. Closing the shared handle would
// poison the cache for every later store call in the process. After BuildLog, a
// read through the same cached handle must still succeed.
func TestBuildLogDoesNotPoisonSharedHandle(t *testing.T) {
	td := queuetest.DataDeps(t)

	// Materialise and cache the shared handle.
	if _, _, err := td.Store(true); err != nil {
		t.Fatalf("Store: %v", err)
	}

	if _, err := BuildLog(td); err != nil {
		t.Fatalf("BuildLog: %v", err)
	}

	// The handle BuildLog borrowed must still be open: a read through the
	// process-cached handle succeeds rather than hitting a closed database.
	s, ok, err := td.Store(false)
	if err != nil {
		t.Fatalf("Store after BuildLog: %v", err)
	}
	if !ok {
		t.Fatal("store handle unexpectedly unavailable after BuildLog")
	}
	if _, err := s.ListAllRoutineRuns(); err != nil {
		t.Fatalf("read through shared handle poisoned by BuildLog: %v", err)
	}
}
