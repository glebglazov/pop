package queue

import (
	"errors"
	"testing"

	"github.com/glebglazov/pop/internal/deps"
	"github.com/glebglazov/pop/tasks"
)

// lockDeps wires a tasks.Deps whose lock directory resolves under a temp XDG
// data home, with an injectable process-liveness probe.
func lockDeps(t *testing.T, alive bool) *tasks.Deps {
	t.Helper()
	dir := t.TempDir()
	d := tasks.DefaultDeps()
	d.FS = &deps.MockFileSystem{
		GetenvFunc: func(key string) string {
			if key == "XDG_DATA_HOME" {
				return dir
			}
			return ""
		},
		// Lock recovery reads the existing file off the real disk; the real
		// supervisor lock writes through os, so delegate ReadFile to the real FS.
		ReadFileFunc: deps.NewRealFileSystem().ReadFile,
		MkdirAllFunc: deps.NewRealFileSystem().MkdirAll,
	}
	d.ProcessAlive = func(pid int) bool { return alive }
	return d
}

func TestAcquireSupervisorLockRefusesSecondLiveInstance(t *testing.T) {
	d := lockDeps(t, true)

	first, err := AcquireSupervisorLock(d)
	if err != nil {
		t.Fatalf("first acquire: %v", err)
	}
	defer first.Release()

	_, err = AcquireSupervisorLock(d)
	if err == nil {
		t.Fatal("second acquire while a live supervisor holds the lock must be refused")
	}
	var exitErr *tasks.ExitError
	if !errors.As(err, &exitErr) {
		t.Fatalf("expected a tasks.ExitError, got %T: %v", err, err)
	}
	if exitErr.Code != tasks.ExitOperational {
		t.Fatalf("expected operational exit code, got %d", exitErr.Code)
	}
}

func TestAcquireSupervisorLockReclaimsStale(t *testing.T) {
	// A first holder writes the lock, then "dies" (ProcessAlive false on the
	// next acquire), so the stale lock must be reclaimed rather than refused.
	d := lockDeps(t, false)

	first, err := AcquireSupervisorLock(d)
	if err != nil {
		t.Fatalf("first acquire: %v", err)
	}
	// Do not release; simulate a crash that left the file behind.
	_ = first

	second, err := AcquireSupervisorLock(d)
	if err != nil {
		t.Fatalf("expected stale lock to be reclaimed, got: %v", err)
	}
	if second == nil {
		t.Fatal("expected a held lock after reclaiming a stale one")
	}
	if err := second.Release(); err != nil {
		t.Fatalf("release: %v", err)
	}
}

func TestSupervisorLockReleaseRemovesFile(t *testing.T) {
	d := lockDeps(t, true)

	lock, err := AcquireSupervisorLock(d)
	if err != nil {
		t.Fatalf("acquire: %v", err)
	}
	if err := lock.Release(); err != nil {
		t.Fatalf("release: %v", err)
	}

	// After release the path is free, so a fresh acquire must succeed even with
	// a live process probe.
	again, err := AcquireSupervisorLock(d)
	if err != nil {
		t.Fatalf("acquire after release: %v", err)
	}
	_ = again.Release()
}
