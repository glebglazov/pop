package tasks

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/glebglazov/pop/internal/deps"
)

func TestRuntimeLockPathDeterministic(t *testing.T) {
	d := &Deps{FS: deps.NewRealFileSystem()}
	root := "/tmp/runtime-a"
	path1 := RuntimeLockPathFor(d, root)
	path2 := RuntimeLockPathFor(d, root)
	if path1 != path2 {
		t.Fatalf("lock paths differ: %q vs %q", path1, path2)
	}
	if !strings.HasSuffix(path1, ".lock") {
		t.Fatalf("unexpected lock path %q", path1)
	}
}

func TestAcquireReleaseRuntimeLock(t *testing.T) {
	root := t.TempDir()
	t.Setenv("XDG_DATA_HOME", root)
	d := &Deps{FS: deps.NewRealFileSystem()}

	runtimeRoot := filepath.Join(root, "checkout")
	lock, err := AcquireRuntimeLock(d, runtimeRoot, &bytes.Buffer{})
	if err != nil {
		t.Fatal(err)
	}
	lockPath := RuntimeLockPathFor(d, runtimeRoot)
	if _, err := os.Stat(lockPath); err != nil {
		t.Fatalf("lock file missing: %v", err)
	}
	data, err := os.ReadFile(lockPath)
	if err != nil {
		t.Fatal(err)
	}
	var meta RuntimeLockMetadata
	if err := json.Unmarshal(data, &meta); err != nil {
		t.Fatal(err)
	}
	if meta.PID != os.Getpid() || meta.RuntimePath != runtimeRoot || meta.StartedAt.IsZero() {
		t.Fatalf("metadata = %#v", meta)
	}
	if err := lock.Release(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(lockPath); !os.IsNotExist(err) {
		t.Fatalf("lock file still present: %v", err)
	}
}

func TestRuntimeLockRefusesLiveConcurrentExecution(t *testing.T) {
	root := t.TempDir()
	t.Setenv("XDG_DATA_HOME", root)
	d := &Deps{FS: deps.NewRealFileSystem(), ProcessAlive: func(pid int) bool { return pid == os.Getpid() }}

	runtimeRoot := filepath.Join(root, "checkout")
	first, err := AcquireRuntimeLock(d, runtimeRoot, &bytes.Buffer{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = first.Release() })

	var notice bytes.Buffer
	_, err = AcquireRuntimeLock(d, runtimeRoot, &notice)
	assertExitCode(t, err, ExitOperational)
	if !strings.Contains(err.Error(), "already in progress") {
		t.Fatalf("err = %v", err)
	}
}

func TestRuntimeLockRecoversStaleLock(t *testing.T) {
	root := t.TempDir()
	t.Setenv("XDG_DATA_HOME", root)
	d := &Deps{
		FS:           deps.NewRealFileSystem(),
		ProcessAlive: func(int) bool { return false },
	}

	runtimeRoot := filepath.Join(root, "checkout")
	lockPath := RuntimeLockPathFor(d, runtimeRoot)
	if err := os.MkdirAll(filepath.Dir(lockPath), 0o755); err != nil {
		t.Fatal(err)
	}
	stale := RuntimeLockMetadata{
		PID:         999999,
		RuntimePath: runtimeRoot,
		StartedAt:   time.Now().UTC().Add(-time.Hour),
	}
	payload, _ := json.MarshalIndent(stale, "", "  ")
	if err := os.WriteFile(lockPath, payload, 0o644); err != nil {
		t.Fatal(err)
	}

	var notice bytes.Buffer
	lock, err := AcquireRuntimeLock(d, runtimeRoot, &notice)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = lock.Release() })
	if !strings.Contains(notice.String(), "stale runtime execution lock") {
		t.Fatalf("notice = %q", notice.String())
	}
}

func TestRuntimeLockRecoversMalformedLock(t *testing.T) {
	root := t.TempDir()
	t.Setenv("XDG_DATA_HOME", root)
	d := &Deps{FS: deps.NewRealFileSystem()}

	runtimeRoot := filepath.Join(root, "checkout")
	lockPath := RuntimeLockPathFor(d, runtimeRoot)
	if err := os.MkdirAll(filepath.Dir(lockPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(lockPath, []byte("not-json"), 0o644); err != nil {
		t.Fatal(err)
	}

	var notice bytes.Buffer
	lock, err := AcquireRuntimeLock(d, runtimeRoot, &notice)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = lock.Release() })
	if !strings.Contains(notice.String(), "malformed runtime execution lock") {
		t.Fatalf("notice = %q", notice.String())
	}
}

func TestDistinctRuntimeRootsLockConcurrently(t *testing.T) {
	root := t.TempDir()
	t.Setenv("XDG_DATA_HOME", root)
	d := &Deps{FS: deps.NewRealFileSystem()}

	aRoot := filepath.Join(root, "a")
	bRoot := filepath.Join(root, "b")
	lockA, err := AcquireRuntimeLock(d, aRoot, &bytes.Buffer{})
	if err != nil {
		t.Fatal(err)
	}
	lockB, err := AcquireRuntimeLock(d, bRoot, &bytes.Buffer{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = lockA.Release()
		_ = lockB.Release()
	})
}

func TestDistinctRuntimeRootsLockConcurrentlyForSeparateSets(t *testing.T) {
	root := t.TempDir()
	t.Setenv("XDG_DATA_HOME", root)
	d := &Deps{FS: deps.NewRealFileSystem(), ProcessAlive: func(pid int) bool { return pid == os.Getpid() }}

	aRoot := filepath.Join(root, "worktree-a")
	bRoot := filepath.Join(root, "worktree-b")
	lockA, err := AcquireRuntimeLockForSet(d, aRoot, "set-a", &bytes.Buffer{})
	if err != nil {
		t.Fatal(err)
	}
	lockB, err := AcquireRuntimeLockForSet(d, bRoot, "set-b", &bytes.Buffer{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = lockA.Release()
		_ = lockB.Release()
	})

	statusA := ReadRuntimeLockStatus(d, aRoot)
	statusB := ReadRuntimeLockStatus(d, bRoot)
	if !statusA.Locked || statusA.Metadata == nil || statusA.Metadata.SetID != "set-a" {
		t.Fatalf("statusA = %#v, want live set-a lock", statusA)
	}
	if !statusB.Locked || statusB.Metadata == nil || statusB.Metadata.SetID != "set-b" {
		t.Fatalf("statusB = %#v, want live set-b lock", statusB)
	}
}

func TestReadRuntimeLockStatusLiveAndIdle(t *testing.T) {
	root := t.TempDir()
	t.Setenv("XDG_DATA_HOME", root)
	d := &Deps{FS: deps.NewRealFileSystem(), ProcessAlive: func(pid int) bool { return pid == os.Getpid() }}

	runtimeRoot := filepath.Join(root, "checkout")
	idle := ReadRuntimeLockStatus(d, runtimeRoot)
	if idle.Locked || idle.Malformed {
		t.Fatalf("idle status = %#v", idle)
	}

	lock, err := AcquireRuntimeLock(d, runtimeRoot, &bytes.Buffer{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = lock.Release() })

	live := ReadRuntimeLockStatus(d, runtimeRoot)
	if !live.Locked || live.Metadata == nil || live.Metadata.PID != os.Getpid() {
		t.Fatalf("live status = %#v", live)
	}
}

func TestReadRuntimeLockStatusMalformed(t *testing.T) {
	root := t.TempDir()
	t.Setenv("XDG_DATA_HOME", root)
	d := &Deps{FS: deps.NewRealFileSystem()}

	runtimeRoot := filepath.Join(root, "checkout")
	lockPath := RuntimeLockPathFor(d, runtimeRoot)
	if err := os.MkdirAll(filepath.Dir(lockPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(lockPath, []byte("{"), 0o644); err != nil {
		t.Fatal(err)
	}

	status := ReadRuntimeLockStatus(d, runtimeRoot)
	if !status.Malformed {
		t.Fatalf("status = %#v", status)
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
