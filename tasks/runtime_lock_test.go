package tasks

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/glebglazov/pop/internal/deps"
	"github.com/glebglazov/pop/store"
)

// latestTerminalDrain reads the latest terminal Drain for runtimePath directly
// from the store — the store drain state is the surviving vocabulary now that the
// DrainOutcome bridge is retired. It returns nil when no terminal drain (or no
// store) exists, standing in for the old ReadDrainOutcome os.ErrNotExist contract.
func latestTerminalDrain(t *testing.T, d *Deps, runtimePath string) *store.Drain {
	t.Helper()
	s, ok, err := openDrainStoreIfExists(d)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	if !ok {
		return nil
	}
	defer func() { _ = s.Close() }()
	dr, err := s.LatestTerminalByRuntimePath(runtimePath)
	if err != nil {
		t.Fatalf("latest terminal: %v", err)
	}
	return dr
}

// drainTestRepo stands up a real git repo and a private data dir, returning deps
// whose store-backed Drain lifecycle resolves repository identity from it. The
// store is real-disk-only, so these tests use the real filesystem and git.
func drainTestRepo(t *testing.T) (*Deps, string) {
	t.Helper()
	root := t.TempDir()
	t.Setenv("XDG_DATA_HOME", filepath.Join(root, ".xdg"))
	repo := filepath.Join(root, "checkout")
	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatal(err)
	}
	initExecutorGitRepo(t, repo)
	d := &Deps{
		FS:           deps.NewRealFileSystem(),
		Git:          deps.NewRealGit(),
		ProcessAlive: func(pid int) bool { return pid == os.Getpid() },
	}
	return d, repo
}

func TestReadRuntimeLockStatusIdleWhenNoDrain(t *testing.T) {
	d, repo := drainTestRepo(t)
	status := ReadRuntimeLockStatus(d, repo)
	if status.Locked || status.Malformed || status.Metadata != nil {
		t.Fatalf("idle status = %#v", status)
	}
}

func TestAcquireRuntimeLockForSetShowsLiveDrain(t *testing.T) {
	d, repo := drainTestRepo(t)
	lock, err := AcquireRuntimeLockForSet(d, repo, "demo", &bytes.Buffer{})
	if err != nil {
		t.Fatalf("acquire: %v", err)
	}
	t.Cleanup(func() { _ = lock.Release() })

	status := ReadRuntimeLockStatus(d, repo)
	if !status.Locked || status.Metadata == nil {
		t.Fatalf("status = %#v, want live drain", status)
	}
	if status.Metadata.SetID != "demo" || status.Metadata.PID != os.Getpid() || status.Metadata.StartedAt.IsZero() {
		t.Fatalf("metadata = %#v", status.Metadata)
	}
}

func TestAcquireRuntimeLockForSetReleaseClearsLive(t *testing.T) {
	d, repo := drainTestRepo(t)
	lock, err := AcquireRuntimeLockForSet(d, repo, "demo", &bytes.Buffer{})
	if err != nil {
		t.Fatalf("acquire: %v", err)
	}
	if err := lock.Release(); err != nil {
		t.Fatalf("release: %v", err)
	}
	if status := ReadRuntimeLockStatus(d, repo); status.Locked {
		t.Fatalf("drain still live after release: %#v", status)
	}
	// Release records a finished terminal, not a running row.
	rec := latestTerminalDrain(t, d, repo)
	if rec == nil {
		t.Fatal("no terminal drain recorded")
	}
	if rec.State != store.StateFinished {
		t.Fatalf("terminal = %q, want finished", rec.State)
	}
}

func TestBeginDrainRefusesConcurrentSameSet(t *testing.T) {
	d, repo := drainTestRepo(t)
	first, err := AcquireRuntimeLockForSet(d, repo, "demo", &bytes.Buffer{})
	if err != nil {
		t.Fatalf("first acquire: %v", err)
	}
	t.Cleanup(func() { _ = first.Release() })

	_, err = BeginDrain(d, repo, "demo", &bytes.Buffer{})
	assertExitCode(t, err, ExitOperational)
	if !strings.Contains(err.Error(), "already in progress") {
		t.Fatalf("err = %v, want mutual-exclusion refusal", err)
	}
}

func TestBeginDrainRefusesConcurrentSameCheckoutDifferentSet(t *testing.T) {
	d, repo := drainTestRepo(t)
	first, err := AcquireRuntimeLockForSet(d, repo, "set-a", &bytes.Buffer{})
	if err != nil {
		t.Fatalf("first acquire: %v", err)
	}
	t.Cleanup(func() { _ = first.Release() })

	// A different set draining the same checkout would clobber its working tree.
	_, err = BeginDrain(d, repo, "set-b", &bytes.Buffer{})
	assertExitCode(t, err, ExitOperational)
}

func TestBeginDrainStaleRunningDoesNotBlock(t *testing.T) {
	_, repo := drainTestRepo(t)
	// First drain owned by a dead PID; its running row is stale.
	dead := &Deps{FS: deps.NewRealFileSystem(), Git: deps.NewRealGit(), ProcessAlive: func(int) bool { return false }}
	stale, err := AcquireRuntimeLockForSet(dead, repo, "demo", &bytes.Buffer{})
	if err != nil {
		t.Fatalf("seed stale drain: %v", err)
	}
	t.Cleanup(func() { _ = stale.Release() })

	// A fresh start over the dead-PID row succeeds.
	live := &Deps{FS: deps.NewRealFileSystem(), Git: deps.NewRealGit(), ProcessAlive: func(int) bool { return false }}
	h, err := BeginDrain(live, repo, "demo", &bytes.Buffer{})
	if err != nil {
		t.Fatalf("start over stale row: %v", err)
	}
	_ = h.Cancel()
}

func TestAcquireRuntimeLockPathScopedNoopWhenIdle(t *testing.T) {
	d, repo := drainTestRepo(t)
	lock, err := AcquireRuntimeLock(d, repo, &bytes.Buffer{})
	if err != nil {
		t.Fatalf("acquire: %v", err)
	}
	// A path-scoped lock records no Drain, so the checkout still reads idle.
	if status := ReadRuntimeLockStatus(d, repo); status.Locked {
		t.Fatalf("path-scoped lock must not register a drain: %#v", status)
	}
	if err := lock.Release(); err != nil {
		t.Fatalf("release: %v", err)
	}
}

func TestAcquireRuntimeLockPathScopedRefusesWhileDrainLive(t *testing.T) {
	d, repo := drainTestRepo(t)
	drain, err := AcquireRuntimeLockForSet(d, repo, "demo", &bytes.Buffer{})
	if err != nil {
		t.Fatalf("start drain: %v", err)
	}
	t.Cleanup(func() { _ = drain.Release() })

	_, err = AcquireRuntimeLock(d, repo, &bytes.Buffer{})
	assertExitCode(t, err, ExitOperational)
	if !strings.Contains(err.Error(), "already in progress") {
		t.Fatalf("err = %v, want refusal while drain live", err)
	}
}

func TestDistinctReposDrainConcurrently(t *testing.T) {
	root := t.TempDir()
	t.Setenv("XDG_DATA_HOME", filepath.Join(root, ".xdg"))
	d := &Deps{FS: deps.NewRealFileSystem(), Git: deps.NewRealGit(), ProcessAlive: func(pid int) bool { return pid == os.Getpid() }}

	repoA := filepath.Join(root, "a")
	repoB := filepath.Join(root, "b")
	for _, r := range []string{repoA, repoB} {
		if err := os.MkdirAll(r, 0o755); err != nil {
			t.Fatal(err)
		}
		initExecutorGitRepo(t, r)
	}

	lockA, err := AcquireRuntimeLockForSet(d, repoA, "set-a", &bytes.Buffer{})
	if err != nil {
		t.Fatalf("acquire A: %v", err)
	}
	t.Cleanup(func() { _ = lockA.Release() })
	lockB, err := AcquireRuntimeLockForSet(d, repoB, "set-b", &bytes.Buffer{})
	if err != nil {
		t.Fatalf("acquire B: %v", err)
	}
	t.Cleanup(func() { _ = lockB.Release() })

	if s := ReadRuntimeLockStatus(d, repoA); !s.Locked || s.Metadata == nil || s.Metadata.SetID != "set-a" {
		t.Fatalf("repoA status = %#v", s)
	}
	if s := ReadRuntimeLockStatus(d, repoB); !s.Locked || s.Metadata == nil || s.Metadata.SetID != "set-b" {
		t.Fatalf("repoB status = %#v", s)
	}
}

func TestRenderRuntimeLockStatus(t *testing.T) {
	var buf bytes.Buffer
	renderRuntimeLock(&buf, &RuntimeLockStatus{
		RuntimePath: "/tmp/runtime",
		Locked:      true,
		Metadata: &RuntimeLockMetadata{
			PID:         42,
			RuntimePath: "/tmp/runtime",
			StartedAt:   time.Date(2026, 5, 31, 12, 0, 0, 0, time.UTC),
		},
	})
	out := buf.String()
	if !strings.Contains(out, "PID 42") || !strings.Contains(out, "/tmp/runtime") {
		t.Fatalf("render = %q", out)
	}
}
