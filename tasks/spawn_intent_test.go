package tasks

import (
	"testing"

	"github.com/glebglazov/pop/internal/deps"
)

func repoCommonDirForTest(t *testing.T, d *Deps, path string) string {
	t.Helper()
	id, err := ResolveRepositoryIdentity(d, path)
	if err != nil {
		t.Fatalf("resolve repository identity: %v", err)
	}
	return id.CommonDir
}

func TestRecordSpawnIntentThenPendingSpawns(t *testing.T) {
	d, repo := drainTestRepo(t)
	if err := RecordSpawnIntent(d, repo, "demo"); err != nil {
		t.Fatalf("RecordSpawnIntent: %v", err)
	}
	commonDir := repoCommonDirForTest(t, d, repo)
	pending, err := PendingSpawns(d, commonDir)
	if err != nil {
		t.Fatalf("PendingSpawns: %v", err)
	}
	if len(pending) != 1 || pending[0].SetID != "demo" {
		t.Fatalf("PendingSpawns = %+v, want one entry for 'demo'", pending)
	}
}

// TestBeginDrainClearsSpawnIntent proves the happy path: once the drain reaches
// BeginDrain and a running row exists, the pending-spawn marker is dropped so it
// stops shadowing the now-visible drain.
func TestBeginDrainClearsSpawnIntent(t *testing.T) {
	d, repo := drainTestRepo(t)
	if err := RecordSpawnIntent(d, repo, "demo"); err != nil {
		t.Fatalf("RecordSpawnIntent: %v", err)
	}
	h, err := BeginDrain(d, repo, "demo", nil)
	if err != nil {
		t.Fatalf("BeginDrain: %v", err)
	}
	t.Cleanup(func() { _ = h.Cancel() })

	commonDir := repoCommonDirForTest(t, d, repo)
	pending, err := PendingSpawns(d, commonDir)
	if err != nil {
		t.Fatalf("PendingSpawns: %v", err)
	}
	if len(pending) != 0 {
		t.Fatalf("BeginDrain did not clear the spawn intent: %+v", pending)
	}
}

// TestReconcileDrainsSweepsDeadOwnerSpawnIntent proves criterion 2: an intent
// whose recording process died (the spawned implement never reached BeginDrain)
// is reconciled away, so it cannot orphan and block re-selection forever.
func TestReconcileDrainsSweepsDeadOwnerSpawnIntent(t *testing.T) {
	d, repo := drainTestRepo(t)
	if err := RecordSpawnIntent(d, repo, "demo"); err != nil {
		t.Fatalf("RecordSpawnIntent: %v", err)
	}
	commonDir := repoCommonDirForTest(t, d, repo)

	// The recording process is gone. ReconcileDrains sweeps the intent alongside
	// crashed drains and dead gate holds.
	dead := &Deps{
		FS:           deps.NewRealFileSystem(),
		Git:          deps.NewRealGit(),
		ProcessAlive: func(int) bool { return false },
	}
	if _, err := ReconcileDrains(dead); err != nil {
		t.Fatalf("ReconcileDrains: %v", err)
	}
	pending, err := PendingSpawns(d, commonDir)
	if err != nil {
		t.Fatalf("PendingSpawns: %v", err)
	}
	if len(pending) != 0 {
		t.Fatalf("dead-owner spawn intent survived reconcile: %+v", pending)
	}
}

// TestPendingSpawnsIgnoresDeadOwner proves the read path itself never surfaces a
// dead-owner intent even before reconcile runs.
func TestPendingSpawnsIgnoresDeadOwner(t *testing.T) {
	d, repo := drainTestRepo(t)
	if err := RecordSpawnIntent(d, repo, "demo"); err != nil {
		t.Fatalf("RecordSpawnIntent: %v", err)
	}
	commonDir := repoCommonDirForTest(t, d, repo)
	reader := &Deps{
		FS:           deps.NewRealFileSystem(),
		Git:          deps.NewRealGit(),
		ProcessAlive: func(int) bool { return false },
	}
	// PendingSpawns resolves the store path from the reader's real-FS data dir,
	// which matches the recorder's: drainTestRepo pins XDG_DATA_HOME for the test
	// process.
	pending, err := PendingSpawns(reader, commonDir)
	if err != nil {
		t.Fatalf("PendingSpawns: %v", err)
	}
	if len(pending) != 0 {
		t.Fatalf("dead-owner intent was surfaced by PendingSpawns: %+v", pending)
	}
}
