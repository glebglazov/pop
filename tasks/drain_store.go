package tasks

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/glebglazov/pop/store"
)

// drainStoreFile is pop's single machine-global execution-state database. It
// lives in the data dir alongside the per-repo storage tree and holds layer-2
// facts (the Drain lifecycle); layer-1 Task set status stays manifest-derived on
// disk (ADR-0055/0056).
const drainStoreFile = "pop.db"

// DrainStorePathWith returns the path to the global execution-state store.
func DrainStorePathWith(d *Deps) string {
	return filepath.Join(popDataDirWith(d), drainStoreFile)
}

// openDrainStore opens the store, creating the data directory and the database
// file on first use. The store is real-disk-only (SQLite cannot ride the
// filesystem seam), so it uses os directly; the path is still derived through
// the seam-aware popDataDirWith.
func openDrainStore(d *Deps) (*store.Store, error) {
	if err := os.MkdirAll(popDataDirWith(d), 0o755); err != nil {
		return nil, exitErr(ExitOperational, "create data directory: %v", err)
	}
	s, err := store.Open(DrainStorePathWith(d))
	if err != nil {
		return nil, exitErr(ExitOperational, "open execution-state store: %v", err)
	}
	return s, nil
}

// openDrainStoreIfExists opens the store only when its file is already present,
// so pure readers (dashboard polls, status renders) never materialise an empty
// database as a side effect. The bool reports whether the store was opened.
func openDrainStoreIfExists(d *Deps) (*store.Store, bool, error) {
	path := DrainStorePathWith(d)
	if _, err := os.Stat(path); err != nil {
		if os.IsNotExist(err) {
			return nil, false, nil
		}
		return nil, false, err
	}
	s, err := store.Open(path)
	if err != nil {
		return nil, false, err
	}
	return s, true, nil
}

// DrainHandle tracks an in-progress Drain so the caller can record its terminal
// exit reason — or cancel it — when the drain ends. It holds the store open for
// the drain's lifetime; Finish and Cancel close it.
type DrainHandle struct {
	store *store.Store
	id    int64
}

// BeginDrain inserts a running Drain for the repository containing runtimePath
// and the given set, enforcing mutual exclusion transactionally: it refuses
// when a live running Drain already exists for the same set (in any checkout of
// the repository) or for the same runtime checkout. It replaces the runtime
// execution lock file and the cross-checkout backstop (ADR-0055).
func BeginDrain(d *Deps, runtimePath, setID string, noticeOut io.Writer) (*DrainHandle, error) {
	id, err := ResolveRepositoryIdentity(d, runtimePath)
	if err != nil {
		return nil, err
	}
	s, err := openDrainStore(d)
	if err != nil {
		return nil, err
	}
	drain, err := s.StartDrain(store.Drain{
		Repo:        id.CommonDir,
		SetID:       setID,
		RuntimePath: runtimePath,
		PID:         os.Getpid(),
		StartedAt:   time.Now().UTC(),
	}, func(pid int) bool { return processAlive(d, pid) })
	if err != nil {
		_ = s.Close()
		if errors.Is(err, store.ErrDrainInProgress) {
			return nil, exitErr(ExitOperational,
				"runtime execution already in progress (PID %d since %s at %s)",
				drain.PID, drain.StartedAt.Format(time.RFC3339), drain.RuntimePath)
		}
		return nil, exitErr(ExitOperational, "record drain start: %v", err)
	}
	return &DrainHandle{store: s, id: drain.ID}, nil
}

// Finish transitions the Drain to a terminal exit reason and closes the store.
// The exhausted-preset arguments are meaningful only for a quota-paused
// terminal. The set's work disposition is never recorded — it stays derived
// from the manifest (ADR-0056).
func (h *DrainHandle) Finish(terminal DrainOutcome, exhaustedPreset string, exhaustedPinned bool, exhaustedResetAt time.Time) error {
	if h == nil {
		return nil
	}
	defer func() { _ = h.store.Close() }()
	return h.store.FinishDrain(h.id, string(terminal), exhaustedPreset, exhaustedPinned, exhaustedResetAt, time.Now().UTC())
}

// Cancel removes the Drain row and closes the store. It is used when the drain
// never executed (declined at the confirmation gate), so no terminal applies.
func (h *DrainHandle) Cancel() error {
	if h == nil {
		return nil
	}
	defer func() { _ = h.store.Close() }()
	return h.store.CancelDrain(h.id)
}

// finalizeDrain records the appropriate exit-reason terminal for a finished
// drain, or cancels the row when the drain was declined and never executed.
func finalizeDrain(h *DrainHandle, declined, quotaPaused bool, preset string, pinned bool, resetAt time.Time, err error) {
	if h == nil {
		return
	}
	terminal, p, pin, r, executed := drainTerminal(declined, quotaPaused, preset, pinned, resetAt, err)
	if !executed {
		_ = h.Cancel()
		return
	}
	_ = h.Finish(terminal, p, pin, r)
}

// drainTerminal maps the observable end of a drain to its exit-reason terminal
// (ADR-0056). A declined run never executed, so it returns executed=false and
// the caller cancels the Drain row. Quota pause and SIGINT are the only two
// non-finished terminals on the clean-exit path; everything else — success,
// failure, blocked, setup error after the drain began — is a finished process
// whose disposition is read from the manifest, not the Drain.
func drainTerminal(declined, quotaPaused bool, preset string, pinned bool, resetAt time.Time, err error) (terminal DrainOutcome, _ string, _ bool, _ time.Time, executed bool) {
	if declined {
		return "", "", false, time.Time{}, false
	}
	if quotaPaused {
		return DrainOutcomeQuotaPaused, preset, pinned, resetAt, true
	}
	var ee *ExitError
	if errors.As(err, &ee) && ee.Code == ExitInterrupted {
		return DrainOutcomeInterrupted, "", false, time.Time{}, true
	}
	return DrainOutcomeFinished, "", false, time.Time{}, true
}
