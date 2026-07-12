package tasks

import (
	"io"
	"os"
	"syscall"
	"time"

	"github.com/glebglazov/pop/store"
)

// RuntimeLockMetadata describes the live Drain holding a runtime checkout. It is
// no longer persisted to a lock file — it is projected from the running Drain
// row in the global store (ADR-0055) — but the shape is retained because the
// queue dashboard, binding lifecycle, and status renderers read it.
type RuntimeLockMetadata struct {
	PID         int       `json:"pid"`
	RuntimePath string    `json:"runtime_path"`
	StartedAt   time.Time `json:"started_at"`
	// SetID identifies the task set the live Drain is draining.
	SetID string `json:"set_id,omitempty"`
}

// RuntimeLockStatus reports whether a runtime checkout is currently being
// drained, projected from the live running Drain in the store.
type RuntimeLockStatus struct {
	RuntimePath string
	Locked      bool
	Metadata    *RuntimeLockMetadata
	// Malformed is retained for API compatibility; the store cannot produce a
	// malformed record, so it is always false.
	Malformed bool
}

// RuntimeLock is a held claim on runtime execution. A set-scoped lock owns a
// running Drain row whose terminal is recorded on Release; a path-scoped lock
// (taken by integration to serialise against a live drain) owns no Drain and
// releases to a no-op.
type RuntimeLock struct {
	drain *DrainHandle
}

// Release records a clean (finished) terminal for a set-scoped lock's Drain, or
// is a no-op for a path-scoped lock. Drains driven by the executor record their
// specific exit-reason terminal directly via DrainHandle.Finish instead.
func (l *RuntimeLock) Release() error {
	if l == nil || l.drain == nil {
		return nil
	}
	return l.drain.Finish(store.StateFinished, "", false, time.Time{})
}

// ReadRuntimeLockStatus reports whether a runtime checkout is being drained,
// reading the live running Drain from the global store. A dead-PID running row
// is stale (an unreconciled crash) and reads as not locked.
func ReadRuntimeLockStatus(d *Deps, runtimeRoot string) *RuntimeLockStatus {
	status := &RuntimeLockStatus{RuntimePath: runtimeRoot}
	s, ok, err := openDrainStoreIfExists(d)
	if err != nil || !ok {
		return status
	}
	defer func() { _ = s.Close() }()
	drain, err := s.LiveDrainByRuntimePath(runtimeRoot, func(dr store.Drain) bool {
		return drainProcessAlive(d, dr.PID, dr.ProcStart)
	})
	if err != nil || drain == nil {
		return status
	}
	status.Locked = true
	status.Metadata = &RuntimeLockMetadata{
		PID:         drain.PID,
		RuntimePath: drain.RuntimePath,
		StartedAt:   drain.StartedAt,
		SetID:       drain.SetID,
	}
	return status
}

// AcquireRuntimeLock takes a path-scoped claim on a runtime checkout, used by
// integration to serialise against a live drain. It refuses when a drain is
// running against the checkout; otherwise it returns a no-op lock (it records no
// Drain — integration's own durable event lands in a later slice).
func AcquireRuntimeLock(d *Deps, runtimeRoot string, noticeOut io.Writer) (*RuntimeLock, error) {
	status := ReadRuntimeLockStatus(d, runtimeRoot)
	if status.Locked && status.Metadata != nil {
		return nil, exitErr(ExitOperational,
			"runtime execution already in progress (PID %d since %s at %s)",
			status.Metadata.PID,
			status.Metadata.StartedAt.Format(time.RFC3339),
			status.Metadata.RuntimePath,
		)
	}
	return &RuntimeLock{}, nil
}

// AcquireRuntimeLockForSet starts a Drain for setID against runtimeRoot,
// enforcing mutual exclusion through the store, and returns a lock whose Release
// records a clean terminal. The executor drives drains through BeginDrain
// directly so it can record quota-pause and interruption terminals; this helper
// covers callers that only need to claim and release a drain slot.
func AcquireRuntimeLockForSet(d *Deps, runtimeRoot, setID string, noticeOut io.Writer) (*RuntimeLock, error) {
	handle, err := BeginDrain(d, runtimeRoot, setID, noticeOut)
	if err != nil {
		return nil, err
	}
	return &RuntimeLock{drain: handle}, nil
}

func processAlive(d *Deps, pid int) bool {
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

// procStartToken resolves the process-start seam, defaulting to the platform
// implementation. The bool reports whether a token could be determined.
func procStartToken(d *Deps, pid int) (string, bool) {
	if d != nil && d.ProcessStartToken != nil {
		return d.ProcessStartToken(pid)
	}
	return defaultProcStartToken(pid)
}

// ProcessStartTokenFor resolves the process-start token for pid through the
// configured seam (or the platform default), reporting whether one could be
// determined. Exported so lock owners outside this package (the supervisor
// lock) record the same PID+start-token identity that drain rows use.
func ProcessStartTokenFor(d *Deps, pid int) (string, bool) {
	return procStartToken(d, pid)
}

// ProcessLiveWithToken reports whether the process that recorded a lock (or
// Drain row) is still running, pairing PID liveness with the stored start token
// to defeat PID reuse. Exported so the supervisor lock shares one liveness
// standard with drains; see drainProcessAlive for the conservative-on-alive
// contract when a token cannot be compared.
func ProcessLiveWithToken(d *Deps, pid int, storedToken string) bool {
	return drainProcessAlive(d, pid, storedToken)
}

// drainProcessAlive reports whether the process that owns a Drain is still
// running, using PID liveness plus the recorded start token to defeat PID reuse.
// It is conservative on the side of "alive": when no start token can be compared
// (the row predates the column, or this platform cannot read process start-time)
// it falls back to bare PID liveness so a genuinely-live drain is never
// misjudged dead. A reused PID is caught only when both tokens are available and
// differ.
func drainProcessAlive(d *Deps, pid int, storedToken string) bool {
	if !processAlive(d, pid) {
		return false
	}
	if storedToken == "" {
		return true
	}
	current, ok := procStartToken(d, pid)
	if !ok {
		return true
	}
	return current == storedToken
}
