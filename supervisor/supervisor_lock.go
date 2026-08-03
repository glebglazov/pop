package supervisor

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/glebglazov/pop/tasks"
	"github.com/glebglazov/pop/tasks/drain"
)

// SupervisorLockMetadata is persisted in the single-instance supervisor lock.
// ProcStart records the owning process's start token so a recycled PID does not
// make a stale lock read "already running" — liveness pairs PID with start
// token, the same standard drain rows use (ADR-0055). It is empty on platforms
// that cannot read process start time (see tasks.ProcStartSupported) and on
// locks written before the column existed, in which case liveness degrades to
// bare PID.
type SupervisorLockMetadata struct {
	PID       int       `json:"pid"`
	StartedAt time.Time `json:"started_at"`
	ProcStart string    `json:"proc_start,omitempty"`
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
// It mirrors the daemon's other runtime files so all supervisor state lives
// together.
func SupervisorLockDir(d *tasks.Deps) string {
	return drain.WorkDataDir(d)
}

// SupervisorLockPath returns the path to the single-instance supervisor lock.
func SupervisorLockPath(d *tasks.Deps) string {
	return filepath.Join(SupervisorLockDir(d), "supervisor.lock")
}

// LegacySupervisorLockPath returns the pre-cut lock path, under the queue-named
// data dir. A daemon started before `pop queue` became `pop work` holds that
// file and nothing else, so a post-cut binary that only consulted its own path
// would happily supervise alongside it. AcquireSupervisorLock therefore reads
// both. Delete this and its caller one release after the cut (CLEANUP.md).
func LegacySupervisorLockPath(d *tasks.Deps) string {
	return filepath.Join(drain.LegacyQueueDataDir(d), "supervisor.lock")
}

// AcquireSupervisorLock acquires the single-instance supervisor lock. A second
// `pop work daemon` while one is already supervising is refused with an
// operational error naming the running PID; a stale lock (PID no longer alive)
// is reclaimed, mirroring the runtime execution lock's self-healing.
func AcquireSupervisorLock(d *tasks.Deps) (*SupervisorLock, error) {
	if err := refuseIfLegacySupervisorLive(d); err != nil {
		return nil, err
	}
	return acquireSupervisorLock(d, false)
}

// refuseIfLegacySupervisorLive refuses when a pre-cut daemon is still supervising
// under the old lock path. A stale or unreadable legacy lock is ignored: only a
// live process can double-supervise, and the file is left for its owner.
func refuseIfLegacySupervisorLive(d *tasks.Deps) error {
	path := LegacySupervisorLockPath(d)
	if path == SupervisorLockPath(d) {
		return nil
	}
	data, err := d.FS.ReadFile(path)
	if err != nil {
		return nil
	}
	meta, err := parseSupervisorLockMetadata(data)
	if err != nil {
		return nil
	}
	if !tasks.ProcessLiveWithToken(d, meta.PID, meta.ProcStart) {
		return nil
	}
	return &tasks.ExitError{Code: tasks.ExitOperational, Err: fmt.Errorf(
		"work supervisor already running (PID %d since %s) holding the pre-cut lock %s",
		meta.PID,
		meta.StartedAt.Format(time.RFC3339),
		path,
	)}
}

func acquireSupervisorLock(d *tasks.Deps, retried bool) (*SupervisorLock, error) {
	lockDir := SupervisorLockDir(d)
	if err := d.FS.MkdirAll(lockDir, 0o755); err != nil {
		return nil, &tasks.ExitError{Code: tasks.ExitOperational, Err: fmt.Errorf("create supervisor lock directory: %w", err)}
	}

	lockPath := SupervisorLockPath(d)
	pid := os.Getpid()
	procStart, _ := tasks.ProcessStartTokenFor(d, pid)
	meta := SupervisorLockMetadata{
		PID:       pid,
		StartedAt: time.Now().UTC(),
		ProcStart: procStart,
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

	if tasks.ProcessLiveWithToken(d, existingMeta.PID, existingMeta.ProcStart) {
		return nil, &tasks.ExitError{Code: tasks.ExitOperational, Err: fmt.Errorf(
			"work supervisor already running (PID %d since %s) holding %s",
			existingMeta.PID,
			existingMeta.StartedAt.Format(time.RFC3339),
			lockPath,
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
