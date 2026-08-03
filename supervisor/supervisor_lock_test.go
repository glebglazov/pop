package supervisor

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/glebglazov/pop/internal/deps"
	"github.com/glebglazov/pop/tasks"
)

// lockDeps wires a tasks.Deps whose lock directory resolves under a temp XDG
// data home, with an injectable process-liveness probe and a deterministic
// process-start token so liveness is exercised the same way on every platform.
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
	d.ProcessStartToken = func(int) (string, bool) { return "start-token", true }
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

func TestAcquireSupervisorLockTakesOverRecycledPID(t *testing.T) {
	// The first holder writes a lock with start token A, then crashes leaving the
	// file behind. The OS later hands the same PID to an unrelated process, so the
	// PID reads alive but its start token differs. Bare-PID liveness would wrongly
	// refuse; pairing PID with the start token detects the recycle and takes over.
	d := lockDeps(t, true)
	token := "start-A"
	d.ProcessStartToken = func(int) (string, bool) { return token, true }

	first, err := AcquireSupervisorLock(d)
	if err != nil {
		t.Fatalf("first acquire: %v", err)
	}
	// Do not release; simulate a crash that left the file behind.
	_ = first

	// PID recycled by a different process => same live PID, different start token.
	token = "start-B"
	second, err := AcquireSupervisorLock(d)
	if err != nil {
		t.Fatalf("expected recycled-PID lock to be taken over, got: %v", err)
	}
	if second == nil {
		t.Fatal("expected a held lock after taking over a recycled-PID lock")
	}
	if err := second.Release(); err != nil {
		t.Fatalf("release: %v", err)
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

// TestSupervisorLockPathBranches pins the lock's new home across the three
// branches every pop data path resolves through: XDG_DATA_HOME, the home
// directory, and /tmp when the home directory is unknowable.
func TestSupervisorLockPathBranches(t *testing.T) {
	real := deps.NewRealFileSystem()
	tests := []struct {
		name string
		fs   *deps.MockFileSystem
		want string
	}{
		{
			name: "XDG_DATA_HOME",
			fs: &deps.MockFileSystem{
				GetenvFunc: func(key string) string {
					if key == "XDG_DATA_HOME" {
						return "/xdg"
					}
					return ""
				},
			},
			want: filepath.Join("/xdg", "pop", "work", "supervisor.lock"),
		},
		{
			name: "home directory",
			fs: &deps.MockFileSystem{
				GetenvFunc:      func(string) string { return "" },
				UserHomeDirFunc: func() (string, error) { return "/home/dev", nil },
			},
			want: filepath.Join("/home/dev", ".local", "share", "pop", "work", "supervisor.lock"),
		},
		{
			name: "no home directory",
			fs: &deps.MockFileSystem{
				GetenvFunc:      func(string) string { return "" },
				UserHomeDirFunc: func() (string, error) { return "", errors.New("no home") },
			},
			want: filepath.Join("/tmp", "pop", "work", "supervisor.lock"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.fs.ReadFileFunc = real.ReadFile
			tt.fs.MkdirAllFunc = real.MkdirAll
			d := tasks.DefaultDeps()
			d.FS = tt.fs
			if got := SupervisorLockPath(d); got != tt.want {
				t.Fatalf("SupervisorLockPath() = %q, want %q", got, tt.want)
			}
		})
	}
}

// TestAcquireSupervisorLockRefusesLivePreCutDaemon covers the handover: a daemon
// started before the rename holds only the queue-named lock, invisibly to this
// binary's own path, so both are read and the refusal names the pre-cut file.
func TestAcquireSupervisorLockRefusesLivePreCutDaemon(t *testing.T) {
	d := lockDeps(t, true)
	legacy := LegacySupervisorLockPath(d)
	if err := os.MkdirAll(filepath.Dir(legacy), 0o755); err != nil {
		t.Fatal(err)
	}
	payload, err := json.Marshal(SupervisorLockMetadata{
		PID:       4242,
		StartedAt: time.Now().UTC(),
		ProcStart: "start-token",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(legacy, payload, 0o644); err != nil {
		t.Fatal(err)
	}

	_, err = AcquireSupervisorLock(d)
	if err == nil {
		t.Fatal("a live pre-cut supervisor must refuse the new daemon")
	}
	var exitErr *tasks.ExitError
	if !errors.As(err, &exitErr) || exitErr.Code != tasks.ExitOperational {
		t.Fatalf("expected an operational tasks.ExitError, got %T: %v", err, err)
	}
	for _, want := range []string{"4242", legacy} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("refusal = %q, want it to name %q", err, want)
		}
	}
	if _, statErr := os.Stat(SupervisorLockPath(d)); !os.IsNotExist(statErr) {
		t.Fatalf("the new lock must not be written while a pre-cut daemon holds the old one (stat: %v)", statErr)
	}

	// A dead pre-cut holder is no obstacle: its file is left alone and the new
	// daemon takes its own lock.
	d.ProcessAlive = func(int) bool { return false }
	lock, err := AcquireSupervisorLock(d)
	if err != nil {
		t.Fatalf("stale pre-cut lock must not block startup: %v", err)
	}
	if _, statErr := os.Stat(legacy); statErr != nil {
		t.Fatalf("the pre-cut lock file belongs to its owner and must be left in place: %v", statErr)
	}
	_ = lock.Release()
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
