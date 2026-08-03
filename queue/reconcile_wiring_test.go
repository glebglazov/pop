package queue

import (
	"bytes"
	"errors"
	"strings"
	"testing"

	"github.com/glebglazov/pop/config"
	"github.com/glebglazov/pop/project"
	"github.com/glebglazov/pop/tasks"
	"github.com/glebglazov/pop/work"
)

// TestReadPathsDoNotReconcile guards the move of the crash-detection pass off
// every read: neither the scheduling scan (`pop queue status`) nor the Work
// snapshot (the dashboard) heals anything, because healing is a write and both
// are reads.
func TestReadPathsDoNotReconcile(t *testing.T) {
	gitRepo := t.TempDir()
	spawnInitGitRepo(t, gitRepo)
	cfg := &config.Config{Projects: []config.ProjectEntry{{Path: gitRepo}}}

	reconciled := 0
	d := &Deps{
		Tasks:      queueTestTasksDeps(t, true),
		Project:    project.DefaultDeps(),
		LoadConfig: func(string) (*config.Config, error) { return cfg, nil },
		ReadLock:   func(rt string) *tasks.RuntimeLockStatus { return idleLock(rt) },
		Refresh:    func(string) (*tasks.RefreshResult, error) { return &tasks.RefreshResult{}, nil },
		Reconcile:  func() (int, error) { reconciled++; return 0, nil },
	}

	if _, err := Scan(d, cfg); err != nil {
		t.Fatalf("Scan: %v", err)
	}
	if _, err := work.BuildSnapshot(d.WorkKinds(cfg)); err != nil {
		t.Fatalf("BuildSnapshot: %v", err)
	}
	if reconciled != 0 {
		t.Fatalf("reconcile ran %d times across the read paths, want 0", reconciled)
	}
}

// TestSupervisorReconcilesEachTick guards the other half: the pass still runs,
// once per tick, as the supervisor's explicit phase (ADR-0055).
func TestSupervisorReconcilesEachTick(t *testing.T) {
	gitRepo := t.TempDir()
	spawnInitGitRepo(t, gitRepo)
	cfg := &config.Config{Projects: []config.ProjectEntry{{Path: gitRepo}}}

	reconciled := 0
	d := &Deps{
		Tasks:      queueTestTasksDeps(t, true),
		Project:    project.DefaultDeps(),
		LoadConfig: func(string) (*config.Config, error) { return cfg, nil },
		ReadLock:   func(rt string) *tasks.RuntimeLockStatus { return idleLock(rt) },
		Refresh:    func(string) (*tasks.RefreshResult, error) { return &tasks.RefreshResult{}, nil },
		Reconcile:  func() (int, error) { reconciled++; return 0, nil },
	}

	var out bytes.Buffer
	tick(d, &out, newRunOutputState())
	if reconciled != 1 {
		t.Fatalf("reconcile ran %d times during a tick, want 1", reconciled)
	}
}

// TestSupervisorSurfacesReconcileErrorButContinues guards that a failing
// reconcile phase is logged to ReconcileOut instead of discarded, while the tick
// still reads candidates from the pre-reconcile snapshot (reconciliation is
// opportunistic; it must never abandon the pass).
func TestSupervisorSurfacesReconcileErrorButContinues(t *testing.T) {
	repo, _, _ := setupSupervisorSpawnRepo(t, "reconcile-fails", []spawnTestTask{
		{ID: "01-a", File: "01-a.md", Title: "A", Type: "AFK", Status: "open"},
	})
	cfg := &config.Config{Projects: []config.ProjectEntry{{Path: repo}}}

	reconcileErr := errors.New("store: database is locked")
	var reconcileOut, out bytes.Buffer
	refreshed := 0
	d := &Deps{
		Tasks:        queueTestTasksDeps(t, true),
		Project:      project.DefaultDeps(),
		LoadConfig:   func(string) (*config.Config, error) { return cfg, nil },
		ReadLock:     func(rt string) *tasks.RuntimeLockStatus { return idleLock(rt) },
		Refresh:      func(string) (*tasks.RefreshResult, error) { refreshed++; return &tasks.RefreshResult{}, nil },
		Reconcile:    func() (int, error) { return 0, reconcileErr },
		ReconcileOut: &reconcileOut,
	}

	tick(d, &out, newRunOutputState())

	if !strings.Contains(reconcileOut.String(), reconcileErr.Error()) {
		t.Fatalf("ReconcileOut = %q, want it to mention %q", reconcileOut.String(), reconcileErr.Error())
	}
	if refreshed == 0 {
		t.Fatal("a failed reconcile must not abandon the tick: the candidate read still ran on the pre-reconcile snapshot")
	}
}
