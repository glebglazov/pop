package queue

import (
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

	if _, err := BuildDashboardWith(d, cfg, nil); err != nil {
		t.Fatalf("BuildDashboardWith: %v", err)
	}
	if reconciled != 1 {
		t.Fatalf("reconcile ran %d times during BuildDashboardWith, want 1", reconciled)
	}
}
