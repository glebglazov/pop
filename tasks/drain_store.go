package tasks

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"sync"
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

// depsStoreInitMu guards the lazy allocation of a Deps's store-cache holder for a
// Deps built from a bare literal (tests) that never went through DefaultDeps. It
// is contended only by such a Deps on its very first store touch; production
// Deps arrive with the holder already set.
var depsStoreInitMu sync.Mutex

// storeCacheHolder returns the Deps's process-cached store handle holder,
// allocating it on first use for a literal-built Deps. Production Deps carry a
// pre-allocated holder (DefaultDeps) so their shallow copies share one handle.
func (d *Deps) storeCacheHolder() *storeCache {
	depsStoreInitMu.Lock()
	defer depsStoreInitMu.Unlock()
	if d.store == nil {
		d.store = &storeCache{}
	}
	return d.store
}

// Store returns the process-cached execution-state store handle, opening it
// (running the migration step) on first use and reusing it thereafter, so
// migrations run at most once per process. It is the single chokepoint every
// open site in the tasks package funnels through — the test-isolation guard
// fires here.
//
// createIfMissing selects the two modes. When true the data directory and the
// database file are created on first use. When false the store is opened only
// when its file already exists, so pure readers (dashboard polls, status
// renders) never materialise an empty database as a side effect; the returned
// bool reports whether a handle was available. Once a handle is cached it is
// returned regardless of the mode.
//
// The store is real-disk-only (SQLite cannot ride the filesystem seam), so it
// uses os directly; the path is still derived through the seam-aware
// popDataDirWith. The handle lives for the process (or until CloseStore); one-shot
// CLI runs rely on process exit, which is WAL-safe.
func (d *Deps) Store(createIfMissing bool) (*store.Store, bool, error) {
	c := d.storeCacheHolder()
	c.mu.Lock()
	defer c.mu.Unlock()
	path := DrainStorePathWith(d)
	if c.handle != nil {
		if c.path == path {
			return c.handle, true, nil
		}
		// The derived path changed (a test redirected its data dir): the cached
		// handle points at a different database. Drop it and reopen against path.
		_ = c.handle.Close()
		c.handle = nil
		c.path = ""
	}
	guardTestStorePath(path)
	if createIfMissing {
		if err := os.MkdirAll(popDataDirWith(d), 0o755); err != nil {
			return nil, false, exitErr(ExitOperational, "create data directory: %v", err)
		}
	} else if _, err := os.Stat(path); err != nil {
		if os.IsNotExist(err) {
			return nil, false, nil
		}
		return nil, false, err
	}
	s, err := store.Open(path, drainLiveness(d))
	if err != nil {
		if createIfMissing {
			return nil, false, exitErr(ExitOperational, "open execution-state store: %v", err)
		}
		return nil, false, err
	}
	c.handle = s
	c.path = path
	return s, true, nil
}

// CloseStore closes the process-cached store handle and drops it, so the next
// Store call reopens. The queue daemon loop and test cleanup call it; one-shot
// CLI runs rely on process exit (WAL-safe) and need not.
func (d *Deps) CloseStore() error {
	c := d.storeCacheHolder()
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.handle == nil {
		return nil
	}
	err := c.handle.Close()
	c.handle = nil
	c.path = ""
	return err
}

// openDrainStore resolves the process-cached store in create-if-needed mode. It
// funnels through Deps.Store; the boolean it returns is always true on success.
func openDrainStore(d *Deps) (*store.Store, error) {
	s, _, err := d.Store(true)
	return s, err
}

// openDrainStoreIfExists resolves the process-cached store in if-exists mode: a
// pure reader never materialises an empty database. The bool reports whether a
// handle was available.
func openDrainStoreIfExists(d *Deps) (*store.Store, bool, error) {
	return d.Store(false)
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
// whoever next reads — no always-on daemon. The same pass also sweeps checkout
// gate holds whose registering process died (a crash while a human sat at a
// Failed/HITL gate would otherwise orphan the hold and block Recovery-turn
// acquisition on that checkout forever). It opens the store only when it already
// exists (a pure reader never materialises an empty database), forks nothing (it
// reads only the drains and checkout_gate_holds tables), and does bounded
// transactions. It returns the number of Drains transitioned to crashed.
func ReconcileDrains(d *Deps) (int, error) {
	s, ok, err := openDrainStoreIfExists(d)
	if err != nil || !ok {
		return 0, err
	}
	now := time.Now().UTC()
	n, err := s.ReconcileCrashed(now)
	// Sweep dead-owner gate holds in the same pass. A sweep error must not mask a
	// successful drain reconcile, so it is only surfaced when the drain arm was
	// clean.
	if _, sweepErr := s.ReconcileGateHolds(); sweepErr != nil && err == nil {
		err = sweepErr
	}
	// Sweep pending-spawn markers whose owner died or whose TTL lapsed (a spawn
	// that never reached BeginDrain), so a stale intent cannot itself block
	// re-selection forever. Same rule: a sweep error only surfaces when the drain
	// arm was clean.
	if _, sweepErr := s.ReconcileSpawnIntents(now.Add(-spawnIntentTTL)); sweepErr != nil && err == nil {
		err = sweepErr
	}
	return n, err
}

// DrainHandle tracks an in-progress Drain so the caller can record its terminal
// exit reason — or cancel it — when the drain ends. It borrows the process-cached
// store handle; Finish and Cancel record the terminal (or remove the row) but no
// longer close the store, which lives for the process.
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
	})
	if err != nil {
		if errors.Is(err, store.ErrDrainInProgress) {
			return nil, exitErr(ExitOperational,
				"runtime execution already in progress (PID %d since %s at %s)",
				drain.PID, drain.StartedAt.Format(time.RFC3339), drain.RuntimePath)
		}
		return nil, exitErr(ExitOperational, "record drain start: %v", err)
	}
	// The running Drain row now covers this set, so its pending-spawn marker (if
	// the supervisor recorded one at dispatch) has served its purpose: drop it so
	// it stops shadowing the now-visible drain. Best-effort — a lingering intent
	// expires on its own and never blocks this drain.
	_ = s.DeleteSpawnIntent(id.CommonDir, setID)
	return &DrainHandle{store: s, id: drain.ID}, nil
}

// Finish transitions the Drain to a terminal exit-reason store state (one of the
// store.State* terminals). The exhausted-preset arguments are meaningful only for
// a quota-paused terminal. The set's work disposition is never recorded — it
// stays derived from the manifest (ADR-0056). It borrows the process-cached store
// handle and does not close it.
func (h *DrainHandle) Finish(terminal string, exhaustedPreset string, exhaustedPinned bool, exhaustedResetAt time.Time) error {
	if h == nil {
		return nil
	}
	return h.store.FinishDrain(h.id, terminal, exhaustedPreset, exhaustedPinned, exhaustedResetAt, time.Now().UTC())
}

// Cancel removes the Drain row. It is used when the drain never executed
// (declined at the confirmation gate), so no terminal applies. It borrows the
// process-cached store handle and does not close it.
func (h *DrainHandle) Cancel() error {
	if h == nil {
		return nil
	}
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
