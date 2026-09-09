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

// SupervisorLockMetadata is persisted in a daemon-half lock.
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
	Half      Half      `json:"half,omitempty"`
}

// Half names one independently live half of the Pop daemon.
type Half string

const (
	ErrandHalf Half = "errands"
	WorkHalf   Half = "work"
)

// Liveness is the running state of both Pop daemon halves.
type Liveness struct {
	Errands bool
	Work    bool
}

// SupervisorLock is one held daemon-half lock.
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

// SupervisorLockPath returns the established Work-half lock path.
func SupervisorLockPath(d *tasks.Deps) string {
	return filepath.Join(SupervisorLockDir(d), "supervisor.lock")
}

// HalfLockPath returns the lock path for one Pop daemon half. The Work half
// keeps the established path so a daemon from before the split still excludes
// a second Work half.
func HalfLockPath(d *tasks.Deps, half Half) string {
	if half == ErrandHalf {
		return filepath.Join(SupervisorLockDir(d), "errands.lock")
	}
	return SupervisorLockPath(d)
}

// LegacySupervisorLockPath returns the pre-cut lock path, under the queue-named
// data dir. A daemon started before `pop queue` became `pop work` holds that
// file and nothing else, so a post-cut binary that only consulted its own path
// would happily supervise alongside it. AcquireSupervisorLock therefore reads
// both. Delete this and its caller one release after the cut (CLEANUP.md).
func LegacySupervisorLockPath(d *tasks.Deps) string {
	return filepath.Join(drain.LegacyQueueDataDir(d), "supervisor.lock")
}

// AcquireSupervisorLock keeps the former Work-half API. New callers that must
// name their half use AcquireHalfLock.
func AcquireSupervisorLock(d *tasks.Deps) (*SupervisorLock, error) {
	return AcquireHalfLock(d, WorkHalf)
}

// AcquireHalfLock acquires the single-instance lock for one daemon half.
func AcquireHalfLock(d *tasks.Deps, half Half) (*SupervisorLock, error) {
	if half != ErrandHalf && half != WorkHalf {
		return nil, fmt.Errorf("unknown Pop daemon half %q", half)
	}
	if half == WorkHalf {
		if err := refuseIfLegacySupervisorLive(d); err != nil {
			return nil, err
		}
	}
	return acquireSupervisorLock(d, half, false)
}

// ReadLiveness reports which Pop daemon halves have live lock owners. A lock
// written before halves were recorded represents the former combined daemon.
func ReadLiveness(d *tasks.Deps) Liveness {
	var state Liveness
	for _, half := range []Half{ErrandHalf, WorkHalf} {
		data, err := d.FS.ReadFile(HalfLockPath(d, half))
		if err != nil {
			continue
		}
		meta, err := parseSupervisorLockMetadata(data)
		if err != nil || !tasks.ProcessLiveWithToken(d, meta.PID, meta.ProcStart) {
			continue
		}
		if meta.Half == "" {
			state.Errands = true
			state.Work = true
			continue
		}
		if half == ErrandHalf {
			state.Errands = true
		} else {
			state.Work = true
		}
	}
	if data, err := d.FS.ReadFile(LegacySupervisorLockPath(d)); err == nil {
		if meta, err := parseSupervisorLockMetadata(data); err == nil && tasks.ProcessLiveWithToken(d, meta.PID, meta.ProcStart) {
			state.Work = true
		}
	}
	return state
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

func acquireSupervisorLock(d *tasks.Deps, half Half, retried bool) (*SupervisorLock, error) {
	lockDir := SupervisorLockDir(d)
	if err := d.FS.MkdirAll(lockDir, 0o755); err != nil {
		return nil, &tasks.ExitError{Code: tasks.ExitOperational, Err: fmt.Errorf("create supervisor lock directory: %w", err)}
	}

	lockPath := HalfLockPath(d, half)
	pid := os.Getpid()
	procStart, _ := tasks.ProcessStartTokenFor(d, pid)
	meta := SupervisorLockMetadata{
		PID:       pid,
		StartedAt: time.Now().UTC(),
		ProcStart: procStart,
		Half:      half,
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
		return acquireSupervisorLock(d, half, true)
	}

	existingMeta, parseErr := parseSupervisorLockMetadata(existing)
	if parseErr != nil {
		_ = os.Remove(lockPath)
		if retried {
			return nil, &tasks.ExitError{Code: tasks.ExitOperational, Err: fmt.Errorf("acquire supervisor lock after recovery: %w", parseErr)}
		}
		return acquireSupervisorLock(d, half, true)
	}

	if tasks.ProcessLiveWithToken(d, existingMeta.PID, existingMeta.ProcStart) {
		return nil, &tasks.ExitError{Code: tasks.ExitOperational, Err: fmt.Errorf(
			"Pop daemon %s half already running (PID %d since %s) holding %s",
			half,
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
	return acquireSupervisorLock(d, half, true)
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
