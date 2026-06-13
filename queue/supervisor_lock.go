package queue

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"syscall"
	"time"

	"github.com/glebglazov/pop/tasks"
)

// SupervisorLockMetadata is persisted in the single-instance supervisor lock.
type SupervisorLockMetadata struct {
	PID       int       `json:"pid"`
	StartedAt time.Time `json:"started_at"`
}

// SupervisorLock is a held single-instance supervisor lock.
type SupervisorLock struct {
	path string
}

// Release removes the supervisor lock file.
func (l *SupervisorLock) Release() error {
	if l == nil || l.path == "" {
		return nil
	}
	err := os.Remove(l.path)
	if os.IsNotExist(err) {
		return nil
	}
	return err
}

// SupervisorLockDir returns the directory holding the supervisor lock file.
// It mirrors the runtime-lock data location so all pop state lives together.
func SupervisorLockDir(d *tasks.Deps) string {
	if xdgData := d.FS.Getenv("XDG_DATA_HOME"); xdgData != "" {
		return filepath.Join(xdgData, "pop", "queue")
	}
	home, err := d.FS.UserHomeDir()
	if err != nil {
		return filepath.Join("/tmp", "pop", "queue")
	}
	return filepath.Join(home, ".local", "share", "pop", "queue")
}

// SupervisorLockPath returns the path to the single-instance supervisor lock.
func SupervisorLockPath(d *tasks.Deps) string {
	return filepath.Join(SupervisorLockDir(d), "supervisor.lock")
}

// AcquireSupervisorLock acquires the single-instance supervisor lock. A second
// `pop queue run` while one is already supervising is refused with an
// operational error naming the running PID; a stale lock (PID no longer alive)
// is reclaimed, mirroring the runtime execution lock's self-healing.
func AcquireSupervisorLock(d *tasks.Deps) (*SupervisorLock, error) {
	return acquireSupervisorLock(d, false)
}

func acquireSupervisorLock(d *tasks.Deps, retried bool) (*SupervisorLock, error) {
	lockDir := SupervisorLockDir(d)
	if err := d.FS.MkdirAll(lockDir, 0o755); err != nil {
		return nil, &tasks.ExitError{Code: tasks.ExitOperational, Err: fmt.Errorf("create supervisor lock directory: %w", err)}
	}

	lockPath := SupervisorLockPath(d)
	meta := SupervisorLockMetadata{
		PID:       os.Getpid(),
		StartedAt: time.Now().UTC(),
	}
	payload, err := json.MarshalIndent(meta, "", "  ")
	if err != nil {
		return nil, &tasks.ExitError{Code: tasks.ExitOperational, Err: fmt.Errorf("encode supervisor lock: %w", err)}
	}

	f, err := os.OpenFile(lockPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err == nil {
		if _, err := f.Write(payload); err != nil {
			_ = f.Close()
			_ = os.Remove(lockPath)
			return nil, &tasks.ExitError{Code: tasks.ExitOperational, Err: fmt.Errorf("write supervisor lock: %w", err)}
		}
		if err := f.Close(); err != nil {
			_ = os.Remove(lockPath)
			return nil, &tasks.ExitError{Code: tasks.ExitOperational, Err: fmt.Errorf("close supervisor lock: %w", err)}
		}
		return &SupervisorLock{path: lockPath}, nil
	}
	if !os.IsExist(err) {
		return nil, &tasks.ExitError{Code: tasks.ExitOperational, Err: fmt.Errorf("acquire supervisor lock: %w", err)}
	}

	existing, readErr := d.FS.ReadFile(lockPath)
	if readErr != nil {
		_ = os.Remove(lockPath)
		if retried {
			return nil, &tasks.ExitError{Code: tasks.ExitOperational, Err: fmt.Errorf("acquire supervisor lock after recovery: %w", readErr)}
		}
		return acquireSupervisorLock(d, true)
	}

	existingMeta, parseErr := parseSupervisorLockMetadata(existing)
	if parseErr != nil {
		_ = os.Remove(lockPath)
		if retried {
			return nil, &tasks.ExitError{Code: tasks.ExitOperational, Err: fmt.Errorf("acquire supervisor lock after recovery: %w", parseErr)}
		}
		return acquireSupervisorLock(d, true)
	}

	if processAlive(d, existingMeta.PID) {
		return nil, &tasks.ExitError{Code: tasks.ExitOperational, Err: fmt.Errorf(
			"queue supervisor already running (PID %d since %s)",
			existingMeta.PID,
			existingMeta.StartedAt.Format(time.RFC3339),
		)}
	}

	if removeErr := os.Remove(lockPath); removeErr != nil && !os.IsNotExist(removeErr) {
		return nil, &tasks.ExitError{Code: tasks.ExitOperational, Err: fmt.Errorf("remove stale supervisor lock: %w", removeErr)}
	}
	if retried {
		return nil, &tasks.ExitError{Code: tasks.ExitOperational, Err: fmt.Errorf("acquire supervisor lock after removing stale lock")}
	}
	return acquireSupervisorLock(d, true)
}

func parseSupervisorLockMetadata(data []byte) (*SupervisorLockMetadata, error) {
	var meta SupervisorLockMetadata
	if err := json.Unmarshal(data, &meta); err != nil {
		return nil, err
	}
	if meta.PID <= 0 || meta.StartedAt.IsZero() {
		return nil, fmt.Errorf("incomplete supervisor lock metadata")
	}
	return &meta, nil
}

// processAlive reports whether a PID is running, honoring an injected probe so
// tests can simulate live and stale holders.
func processAlive(d *tasks.Deps, pid int) bool {
	if d != nil && d.ProcessAlive != nil {
		return d.ProcessAlive(pid)
	}
	if pid <= 0 {
		return false
	}
	proc, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	return proc.Signal(syscall.Signal(0)) == nil
}
