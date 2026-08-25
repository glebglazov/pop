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
	"github.com/glebglazov/pop/store/storetest"
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
	if testing.Testing() {
		// Under go test, hundreds of fixtures each open a first-touch store at a
		// fresh temp path; seeding a pre-migrated copy here (a no-op if a file
		// already sits at path) turns their migration cost into a version check
		// (ADR-0144). Production opens never call testing.Testing() true, so real
		// stores always run the genuine forward migration.
		_ = storetest.WriteTemplate(path)
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
// acquisition on that checkout forever). It likewise sweeps recovery waiters
// whose registering process died, so a dead owner's waiter (a kill -9 or terminal
// close mid-quota-wait) is not left deferring its set in the Queue forever
// (ADR-0135), and admission waiters whose owner died, so a dead head never
// stalls the Admission queue behind it (ADR-0239). It opens the store only when
// it already exists (a pure reader never materialises an empty database), forks
// nothing (it reads only the drains, checkout_gate_holds, spawn_intents,
// recovery_waiters and admission_waiters tables), and does bounded transactions.
// It returns the number of Drains transitioned to crashed.
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
	// Sweep recovery waiters whose registering process died (a kill -9 or terminal
	// close mid-quota-wait), so a dead owner's waiter is not left claiming the
	// checkout and permanently deferring its set in the Queue (ADR-0135). Same
	// rule: a sweep error only surfaces when the drain arm was clean.
	if _, sweepErr := s.ReconcileRecoveryWaiters(); sweepErr != nil && err == nil {
		err = sweepErr
	}
	// Sweep admission waiters whose registering process died. A strict-FIFO queue
	// cannot step over a dead head forever: the grant's ordering check skips a
	// dead owner so nobody stalls in the meantime, and this pass removes the row
	// (ADR-0239). Same rule: a sweep error only surfaces when the drain arm was
	// clean.
	if _, sweepErr := s.ReconcileAdmissionWaiters(); sweepErr != nil && err == nil {
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
// and the given set, refusing when a live Set claim or Checkout claim stands in
// the way. It is admission taken without a place in the queue — the shape every
// internal caller with no human to show a wait line to wants.
func BeginDrain(d *Deps, runtimePath, setID string, noticeOut io.Writer) (*DrainHandle, error) {
	handle, _, err := BeginDrainWithAdmission(d, runtimePath, setID, noticeOut, AdmissionRefuse)
	return handle, err
}

// BeginDrainWithAdmission is BeginDrain under an explicit Admission policy, and
// is the single chokepoint both the CLI and the gate-resume path share
// (ADR-0239). Under AdmissionRefuse it exits non-zero naming the claim, as it
// always has. Under AdmissionWait it joins the checkout's Admission queue and
// blocks until a grant arrives, printing who holds the checkout and where to
// reach them.
//
// The bool reports whether the command actually sat in the queue. A caller that
// waited must re-derive its target's status before acting on it: the work it was
// admitted for may have been finished by whoever it was waiting on.
func BeginDrainWithAdmission(d *Deps, runtimePath, setID string, noticeOut io.Writer, policy AdmissionPolicy) (*DrainHandle, bool, error) {
	return admitDrainRow(d, runtimePath, setID, noticeOut, policy, true)
}

// admitDrainRow is the acquisition itself: it takes the Checkout claim and the
// Set claim together — as one running Drain row — under the given Admission
// policy, and hands back the handle that releases them.
//
// clearSpawnIntent separates a drain from a Tree-stable operation that is not
// one (ADR-0238). A drain consumes the supervisor's pending-spawn marker,
// because the row it just inserted is what that marker was standing in for; a
// standalone Verifier or Reviewer holding the same checkout is not the drain the
// supervisor dispatched, so it leaves the marker alone rather than hiding a
// spawn that has yet to happen.
func admitDrainRow(d *Deps, runtimePath, setID string, noticeOut io.Writer, policy AdmissionPolicy, clearSpawnIntent bool) (*DrainHandle, bool, error) {
	id, err := ResolveRepositoryIdentity(d, runtimePath)
	if err != nil {
		return nil, false, err
	}
	s, err := openDrainStore(d)
	if err != nil {
		return nil, false, err
	}
	pid := os.Getpid()
	procStart, _ := procStartToken(d, pid)
	row := store.Drain{
		Repo:        id.CommonDir,
		SetID:       setID,
		RuntimePath: runtimePath,
		PID:         pid,
		ProcStart:   procStart,
		StartedAt:   time.Now().UTC(),
	}

	var drain store.Drain
	var waited bool
	if policy == AdmissionWait {
		drain, waited, err = awaitAdmission(d, s, row, noticeOut)
	} else {
		drain, err = s.StartDrain(row)
	}
	if err != nil {
		// Each refusal names its own resource: the Set claim sends the human to the
		// checkout already draining the set, the Checkout claim to the tree that
		// must hold still. The store owns both sentences so this path and
		// AcquireRuntimeLock cannot drift apart. A wait that ended on an interrupt
		// already carries its own exit code and passes straight through.
		var setClaimed *store.SetClaimedError
		if errors.As(err, &setClaimed) {
			return nil, waited, exitErr(ExitOperational, "%s", setClaimed.Claim.Sentence())
		}
		var claimed *store.CheckoutClaimedError
		if errors.As(err, &claimed) {
			return nil, waited, exitErr(ExitOperational, "%s", claimed.Claim.Sentence())
		}
		var exit *ExitError
		if errors.As(err, &exit) {
			return nil, waited, err
		}
		return nil, waited, exitErr(ExitOperational, "record drain start: %v", err)
	}
	if clearSpawnIntent {
		// The running Drain row now covers this set, so its pending-spawn marker (if
		// the supervisor recorded one at dispatch) has served its purpose: drop it so
		// it stops shadowing the now-visible drain. Best-effort — a lingering intent
		// expires on its own and never blocks this drain.
		_ = s.DeleteSpawnIntent(id.CommonDir, setID)
	}
	return &DrainHandle{store: s, id: drain.ID}, waited, nil
}

// Finish transitions the Drain to the terminal it ended on (a store.State*
// exit reason, plus the Agent fallback walk ending behind it where the exit
// reason cannot say it). The set's work disposition is never recorded — it stays
// derived from the manifest (ADR-0056). It borrows the process-cached store
// handle and does not close it.
func (h *DrainHandle) Finish(ending store.DrainEnding) error {
	if h == nil {
		return nil
	}
	return h.store.FinishDrain(h.id, ending, time.Now().UTC())
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

// drainOutcome is everything the process knows about how its drain ended by the
// time it finalizes the row: the dispositions the result carries and the final
// error. It is one value rather than a parameter list because the terminal is a
// judgment over all of it at once — an Agent fallback walk that spent its list,
// for instance, is read off the error while the agent it spent is read off the
// same error's ending.
type drainOutcome struct {
	declined     bool
	unavail      *AgentProceedVerdict
	verifyFailed bool
	pinned       bool
	// noAgentStarted marks a walk in which not one preset ran a Task attempt, so
	// the stop is a no-op rather than a failure (ADR-0231).
	noAgentStarted bool
	err            error
}

// finalizeDrain records the appropriate exit-reason terminal for a finished
// drain, or cancels the row when the drain was declined and never executed.
func finalizeDrain(h *DrainHandle, o drainOutcome) {
	if h == nil {
		return
	}
	ending, executed := drainTerminal(o)
	if !executed {
		_ = h.Cancel()
		return
	}
	_ = h.Finish(ending)
}

// drainTerminal maps the observable end of a drain to its exit-reason store
// state (ADR-0056). A declined run never executed, so it returns executed=false
// and the caller cancels the Drain row. A time-healing Agent proceed verdict,
// SIGINT, and a failed pre-approval verification (NEEDS-HUMAN or an exhausted
// remediation cap, ADR-0086/0087) are the non-finished terminals; everything
// else — success, failure, blocked, setup error after the drain began — is a
// finished process whose disposition is read from the manifest, not the Drain.
//
// Two clean stops carry a Drain ending beside the terminal, because they are the
// ones a human coming back to the machine has to be able to pick out of a
// journal of clean finishes: a drain that ran out of agents, and one that could
// not start a single one (ADR-0231).
func drainTerminal(o drainOutcome) (store.DrainEnding, bool) {
	if o.declined {
		return store.DrainEnding{}, false
	}
	if o.unavail != nil {
		if th, ok := o.unavail.TimeHealing(); ok {
			return store.DrainEnding{
				State:            store.StateQuotaPaused,
				ExhaustedPreset:  o.unavail.Preset,
				ExhaustedPinned:  o.pinned,
				ExhaustedResetAt: th.ResetAt,
			}, true
		}
		// A human-healing verdict is a clean finished stop (ADR-0153).
		if o.noAgentStarted {
			return store.DrainEnding{
				State:           store.StateFinished,
				Ending:          store.EndingNoAgentStarted,
				ExhaustedPreset: o.unavail.Preset,
			}, true
		}
	}
	if isInterrupted(o.err) {
		return store.DrainEnding{State: store.StateInterrupted}, true
	}
	if o.verifyFailed {
		return store.DrainEnding{State: store.StateVerifyFailed}, true
	}
	if spent, ok := exhaustedWalkPreset(o.err); ok {
		return store.DrainEnding{
			State:           store.StateFinished,
			Ending:          store.EndingAgentsExhausted,
			ExhaustedPreset: spent,
		}, true
	}
	return store.DrainEnding{State: store.StateFinished}, true
}
