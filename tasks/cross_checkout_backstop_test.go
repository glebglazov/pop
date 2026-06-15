package tasks

import (
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/glebglazov/pop/internal/deps"
)

// mockDepsForBackstop returns Deps whose git mock returns commonDirResult for
// any rev-parse --git-common-dir query, simulating all checkouts belonging to
// the same repository.
func mockDepsForBackstop(processAlive func(int) bool, commonDirResult string) *Deps {
	return &Deps{
		FS:           deps.NewRealFileSystem(),
		ProcessAlive: processAlive,
		Git: &deps.MockGit{
			CommandInDirFunc: func(dir string, args ...string) (string, error) {
				if len(args) >= 2 && args[0] == "rev-parse" && args[1] == "--git-common-dir" {
					return commonDirResult, nil
				}
				return "", nil
			},
		},
	}
}

// writeStaleLock writes a runtime lock file directly with the given PID and SetID,
// bypassing AcquireRuntimeLockForSet so we can simulate dead-PID scenarios.
func writeStaleLock(t *testing.T, d *Deps, checkoutPath, setID string, pid int) {
	t.Helper()
	lockPath := RuntimeLockPathFor(d, checkoutPath)
	if err := os.MkdirAll(filepath.Dir(lockPath), 0o755); err != nil {
		t.Fatal(err)
	}
	meta := RuntimeLockMetadata{
		PID:         pid,
		RuntimePath: checkoutPath,
		StartedAt:   time.Now().UTC().Add(-time.Hour),
		SetID:       setID,
	}
	payload, err := json.MarshalIndent(meta, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(lockPath, payload, 0o644); err != nil {
		t.Fatal(err)
	}
}

// TestCrossCheckoutBackstopBlocksSameSet: a live lock for the same set in
// another checkout of the same repo must block the incoming drain.
func TestCrossCheckoutBackstopBlocksSameSet(t *testing.T) {
	root := t.TempDir()
	t.Setenv("XDG_DATA_HOME", root)

	checkoutA := filepath.Join(root, "checkout-a")
	checkoutB := filepath.Join(root, "checkout-b")

	// Both checkouts share the same commonDir — same repository.
	sharedCommonDir := filepath.Join(root, "repo.git")
	d := mockDepsForBackstop(func(pid int) bool { return pid == os.Getpid() }, sharedCommonDir)

	// Acquire a live lock in checkout-a for set "my-set".
	lock, err := AcquireRuntimeLockForSet(d, checkoutA, "my-set", io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = lock.Release() })

	// checkout-b (projectPath = checkoutB) tries to drain the same set: backstop must block.
	err = CheckCrossCheckoutConflict(d, checkoutB, checkoutB, "my-set")
	assertExitCode(t, err, ExitOperational)
	if !strings.Contains(err.Error(), "already in progress") {
		t.Fatalf("err = %v, want 'already in progress'", err)
	}
}

// TestCrossCheckoutBackstopIgnoresDeadPID: a lock file whose PID is no longer
// alive must be treated as stale and must not block the incoming drain.
func TestCrossCheckoutBackstopIgnoresDeadPID(t *testing.T) {
	root := t.TempDir()
	t.Setenv("XDG_DATA_HOME", root)

	checkoutA := filepath.Join(root, "checkout-a")
	checkoutB := filepath.Join(root, "checkout-b")

	// All PIDs reported dead — simulates a stale lock from a previous run.
	sharedCommonDir := filepath.Join(root, "repo.git")
	d := mockDepsForBackstop(func(int) bool { return false }, sharedCommonDir)

	// Write a stale lock in checkout-a for set "my-set" with a dead PID.
	writeStaleLock(t, d, checkoutA, "my-set", 999999)

	// checkout-b must not be blocked by the stale lock.
	if err := CheckCrossCheckoutConflict(d, checkoutB, checkoutB, "my-set"); err != nil {
		t.Fatalf("dead-PID lock must not block: %v", err)
	}
}

// TestCrossCheckoutBackstopNoConflict: no lock files at all — drain proceeds.
func TestCrossCheckoutBackstopNoConflict(t *testing.T) {
	root := t.TempDir()
	t.Setenv("XDG_DATA_HOME", root)

	sharedCommonDir := filepath.Join(root, "repo.git")
	d := mockDepsForBackstop(func(pid int) bool { return pid == os.Getpid() }, sharedCommonDir)

	checkoutB := filepath.Join(root, "checkout-b")
	if err := CheckCrossCheckoutConflict(d, checkoutB, checkoutB, "my-set"); err != nil {
		t.Fatalf("no locks must not block: %v", err)
	}
}

// TestCrossCheckoutBackstopDifferentSetDoesNotBlock: a live lock for a
// different set in another checkout must not block the incoming drain.
func TestCrossCheckoutBackstopDifferentSetDoesNotBlock(t *testing.T) {
	root := t.TempDir()
	t.Setenv("XDG_DATA_HOME", root)

	checkoutA := filepath.Join(root, "checkout-a")
	checkoutB := filepath.Join(root, "checkout-b")

	sharedCommonDir := filepath.Join(root, "repo.git")
	d := mockDepsForBackstop(func(pid int) bool { return pid == os.Getpid() }, sharedCommonDir)

	// checkout-a holds a lock for "other-set".
	lock, err := AcquireRuntimeLockForSet(d, checkoutA, "other-set", io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = lock.Release() })

	// checkout-b drains "my-set" — different set, must not be blocked.
	if err := CheckCrossCheckoutConflict(d, checkoutB, checkoutB, "my-set"); err != nil {
		t.Fatalf("different set must not block: %v", err)
	}
}

// TestCrossCheckoutBackstopSameCheckoutSkipped: the current checkout's own lock
// is excluded from the scan so the local per-checkout lock handles that case.
func TestCrossCheckoutBackstopSameCheckoutSkipped(t *testing.T) {
	root := t.TempDir()
	t.Setenv("XDG_DATA_HOME", root)

	checkoutA := filepath.Join(root, "checkout-a")

	sharedCommonDir := filepath.Join(root, "repo.git")
	d := mockDepsForBackstop(func(pid int) bool { return pid == os.Getpid() }, sharedCommonDir)

	// A live lock exists for checkout-a itself (same checkout, same set).
	lock, err := AcquireRuntimeLockForSet(d, checkoutA, "my-set", io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = lock.Release() })

	// The backstop must skip checkoutA (it's the current runtimePath) — the
	// local per-checkout lock handles same-checkout conflicts.
	if err := CheckCrossCheckoutConflict(d, checkoutA, checkoutA, "my-set"); err != nil {
		t.Fatalf("same-checkout lock must not be caught by backstop: %v", err)
	}
}

// TestCrossCheckoutBackstopDifferentRepoDoesNotBlock: a live lock with the same
// set in another checkout of a DIFFERENT repository must not block.
func TestCrossCheckoutBackstopDifferentRepoDoesNotBlock(t *testing.T) {
	root := t.TempDir()
	t.Setenv("XDG_DATA_HOME", root)

	checkoutA := filepath.Join(root, "checkout-a")
	checkoutB := filepath.Join(root, "checkout-b")

	d := &Deps{
		FS:           deps.NewRealFileSystem(),
		ProcessAlive: func(pid int) bool { return pid == os.Getpid() },
		Git: &deps.MockGit{
			CommandInDirFunc: func(dir string, args ...string) (string, error) {
				if len(args) >= 2 && args[0] == "rev-parse" && args[1] == "--git-common-dir" {
					// checkoutA and checkoutB report different common dirs.
					if strings.HasSuffix(dir, "checkout-a") {
						return filepath.Join(root, "repo-a.git"), nil
					}
					return filepath.Join(root, "repo-b.git"), nil
				}
				return "", nil
			},
		},
	}

	// checkout-a (repo-a) holds a live lock for "my-set".
	lock, err := AcquireRuntimeLockForSet(d, checkoutA, "my-set", io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = lock.Release() })

	// checkout-b (repo-b) drains the same set name but in a different repo.
	if err := CheckCrossCheckoutConflict(d, checkoutB, checkoutB, "my-set"); err != nil {
		t.Fatalf("different-repo lock must not block: %v", err)
	}
}
