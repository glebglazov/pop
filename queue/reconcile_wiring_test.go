package queue

import (
	"bytes"
	"errors"
	"strings"
	"testing"

	"github.com/glebglazov/pop/config"
	"github.com/glebglazov/pop/project"
	"github.com/glebglazov/pop/tasks"
)

// TestScanReconcilesBeforeReading guards that the daemon tick and
// `pop queue status` (both routed through Scan) run the opportunistic
// crash-detection pass before projecting from lock/outcome state (ADR-0055).
func TestScanReconcilesBeforeReading(t *testing.T) {
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
	if reconciled != 1 {
		t.Fatalf("reconcile ran %d times during Scan, want 1", reconciled)
	}
}

// TestBuildDashboardReconcilesBeforeReading guards the dashboard read path.
func TestBuildDashboardReconcilesBeforeReading(t *testing.T) {
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

	if _, err := BuildDashboard(d, cfg); err != nil {
		t.Fatalf("BuildDashboard: %v", err)
	}
	if reconciled != 1 {
		t.Fatalf("reconcile ran %d times during BuildDashboard, want 1", reconciled)
	}
}

// TestScanSurfacesReconcileErrorButContinues guards that a failing
// opportunistic reconcile pass is logged to ReconcileOut instead of discarded,
// while Scan still succeeds and returns decisions from the pre-reconcile
// snapshot (reconcile is opportunistic; it must never fail the read).
func TestScanSurfacesReconcileErrorButContinues(t *testing.T) {
	gitRepo := t.TempDir()
	spawnInitGitRepo(t, gitRepo)
	cfg := &config.Config{Projects: []config.ProjectEntry{{Path: gitRepo}}}

	reconcileErr := errors.New("store: database is locked")
	var out bytes.Buffer
	d := &Deps{
		Tasks:        queueTestTasksDeps(t, true),
		Project:      project.DefaultDeps(),
		LoadConfig:   func(string) (*config.Config, error) { return cfg, nil },
		ReadLock:     func(rt string) *tasks.RuntimeLockStatus { return idleLock(rt) },
		Refresh:      func(string) (*tasks.RefreshResult, error) { return &tasks.RefreshResult{}, nil },
		Reconcile:    func() (int, error) { return 0, reconcileErr },
		ReconcileOut: &out,
	}

	decisions, err := Scan(d, cfg)
	if err != nil {
		t.Fatalf("Scan: %v, want reconcile failure to not fail the scan", err)
	}
	if len(decisions) == 0 {
		t.Fatal("Scan returned no decisions after reconcile failure, want the pre-reconcile snapshot to still be projected")
	}
	if !strings.Contains(out.String(), reconcileErr.Error()) {
		t.Fatalf("ReconcileOut = %q, want it to mention %q", out.String(), reconcileErr.Error())
	}
}

// TestBuildDashboardSurfacesReconcileErrorButContinues mirrors the Scan case
// for the dashboard read path.
func TestBuildDashboardSurfacesReconcileErrorButContinues(t *testing.T) {
	gitRepo := t.TempDir()
	spawnInitGitRepo(t, gitRepo)
	cfg := &config.Config{Projects: []config.ProjectEntry{{Path: gitRepo}}}

	reconcileErr := errors.New("store: database is locked")
	var out bytes.Buffer
	d := &Deps{
		Tasks:        queueTestTasksDeps(t, true),
		Project:      project.DefaultDeps(),
		LoadConfig:   func(string) (*config.Config, error) { return cfg, nil },
		ReadLock:     func(rt string) *tasks.RuntimeLockStatus { return idleLock(rt) },
		Refresh:      func(string) (*tasks.RefreshResult, error) { return &tasks.RefreshResult{}, nil },
		Reconcile:    func() (int, error) { return 0, reconcileErr },
		ReconcileOut: &out,
	}

	if _, err := BuildDashboard(d, cfg); err != nil {
		t.Fatalf("BuildDashboard: %v, want reconcile failure to not fail the build", err)
	}
	if !strings.Contains(out.String(), reconcileErr.Error()) {
		t.Fatalf("ReconcileOut = %q, want it to mention %q", out.String(), reconcileErr.Error())
	}
}
