package tasks

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"
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
	guardTestStorePath(DrainStorePathWith(d))
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
	guardTestStorePath(path)
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

// prodDataDirAtStartup is the developer's real machine-global data dir,
// resolved once at package load — before any test calls t.Setenv. The guard
// compares against this snapshot rather than the live environment so that a
// test which redirects XDG_DATA_HOME to a temp dir (the correct isolation) is
// recognised as safe, while a test that never isolates and lands back on the
// real store is caught.
var prodDataDirAtStartup = realProductionDataDir()

// guardTestStorePath is the default isolation backstop (slice 01): under `go
// test`, opening the developer's real machine-global store would pollute it
// with throwaway rows. Any test that reaches a store open without first
// redirecting its data dir to a temp location (via XDG_DATA_HOME / a test
// helper such as queueDataDeps) trips this panic, so the leak can't silently
// return. It is a no-op outside tests.
func guardTestStorePath(path string) {
	if !testing.Testing() {
		return
	}
	if prodDataDirAtStartup == "" {
		return
	}
	if filepath.Dir(path) == prodDataDirAtStartup {
		panic("tasks: test attempted to open the real pop store at " + path +
			"; isolate the data dir to a temp location (XDG_DATA_HOME / queueDataDeps) before touching the store")
	}
}

// realProductionDataDir resolves pop's data directory from the *real* process
// environment (not the filesystem seam), mirroring popDataDirWith. Evaluated at
// package load to snapshot the true machine store location.
func realProductionDataDir() string {
	if xdgData := os.Getenv("XDG_DATA_HOME"); xdgData != "" {
		return filepath.Join(xdgData, "pop")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".local", "share", "pop")
}

// ReconcileDrains is the opportunistic reconcile pass every layer-2 reader runs
// before reading (ADR-0055): it transitions running Drains whose owning process
// is no longer alive to crashed, so a foreground drain that died is healed by
// whoever next reads — no always-on daemon. It opens the store only when it
// already exists (a pure reader never materialises an empty database), forks
// nothing (it reads only the drains table), and does a single bounded
// transaction. It returns the number of Drains transitioned to crashed.
func ReconcileDrains(d *Deps) (int, error) {
	s, ok, err := openDrainStoreIfExists(d)
	if err != nil || !ok {
		return 0, err
	}
	defer func() { _ = s.Close() }()
	now := time.Now().UTC()
	n, err := s.ReconcileCrashed(func(dr store.Drain) bool {
		return drainProcessAlive(d, dr.PID, dr.ProcStart)
	}, now)
	return n, err
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
	pid := os.Getpid()
	procStart, _ := procStartToken(d, pid)
	drain, err := s.StartDrain(store.Drain{
		Repo:        id.CommonDir,
		SetID:       setID,
		RuntimePath: runtimePath,
		PID:         pid,
		ProcStart:   procStart,
		StartedAt:   time.Now().UTC(),
	}, func(dr store.Drain) bool {
		return drainProcessAlive(d, dr.PID, dr.ProcStart)
	})
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

// Finish transitions the Drain to a terminal exit-reason store state (one of the
// store.State* terminals) and closes the store. The exhausted-preset arguments
// are meaningful only for a quota-paused terminal. The set's work disposition is
// never recorded — it stays derived from the manifest (ADR-0056).
func (h *DrainHandle) Finish(terminal string, exhaustedPreset string, exhaustedPinned bool, exhaustedResetAt time.Time) error {
	if h == nil {
		return nil
	}
	defer func() { _ = h.store.Close() }()
	return h.store.FinishDrain(h.id, terminal, exhaustedPreset, exhaustedPinned, exhaustedResetAt, time.Now().UTC())
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
func finalizeDrain(h *DrainHandle, declined, quotaPaused, verifyFailed bool, preset string, pinned bool, resetAt time.Time, err error) {
	if h == nil {
		return
	}
	terminal, p, pin, r, executed := drainTerminal(declined, quotaPaused, verifyFailed, preset, pinned, resetAt, err)
	if !executed {
		_ = h.Cancel()
		return
	}
	_ = h.Finish(terminal, p, pin, r)
}

// drainTerminal maps the observable end of a drain to its exit-reason store
// state (ADR-0056). A declined run never executed, so it returns executed=false
// and the caller cancels the Drain row. Quota pause, SIGINT, and a failed
// pre-approval verification (NEEDS-HUMAN or an exhausted remediation cap,
// ADR-0086/0087) are the non-finished terminals; everything else — success,
// failure, blocked, setup error after the drain began — is a finished process
// whose disposition is read from the manifest, not the Drain.
func drainTerminal(declined, quotaPaused, verifyFailed bool, preset string, pinned bool, resetAt time.Time, err error) (terminal string, _ string, _ bool, _ time.Time, executed bool) {
	if declined {
		return "", "", false, time.Time{}, false
	}
	if quotaPaused {
		return store.StateQuotaPaused, preset, pinned, resetAt, true
	}
	var ee *ExitError
	if errors.As(err, &ee) && ee.Code == ExitInterrupted {
		return store.StateInterrupted, "", false, time.Time{}, true
	}
	if verifyFailed {
		return store.StateVerifyFailed, "", false, time.Time{}, true
	}
	return store.StateFinished, "", false, time.Time{}, true
}
