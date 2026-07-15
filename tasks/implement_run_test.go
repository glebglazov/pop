package tasks

import (
	"io"
	"strings"
	"testing"
	"time"

	"github.com/glebglazov/pop/store"
)

// newTestImplementRun builds a minimal implementRun wired to a real store-backed
// checkout for the Drain-lifecycle methods (parkDrain / ensureDrain / finalize).
// Only the fields those methods read are populated, so the seams can be driven
// directly without standing up a whole drain loop.
func newTestImplementRun(d *Deps, repo, setID string) *implementRun {
	return &implementRun{
		d:           d,
		runtimePath: repo,
		taskSetID:   setID,
		confirmOut:  io.Discard,
		out:         io.Discard,
	}
}

// TestImplementRunParkDrainIdempotent pins the park idempotence invariant: the
// first parkDrain finishes the held Drain and drops the handle; a second call is
// a guard-return no-op, so a double park still records exactly one Finish
// (ADR-0067).
func TestImplementRunParkDrainIdempotent(t *testing.T) {
	d, repo := drainTestRepo(t)
	run := newTestImplementRun(d, repo, "demo")
	h, err := BeginDrain(d, repo, "demo", io.Discard)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	run.drain = h

	if status := ReadRuntimeLockStatus(d, repo); !status.Locked {
		t.Fatalf("expected a live drain before park: %#v", status)
	}

	run.parkDrain()
	if run.drain != nil {
		t.Fatal("parkDrain must clear the held Drain handle")
	}
	if status := ReadRuntimeLockStatus(d, repo); status.Locked {
		t.Fatalf("runtime lock still held after park: %#v", status)
	}
	rec := latestTerminalDrain(t, d, repo)
	if rec == nil || rec.State != store.StateFinished {
		t.Fatalf("park terminal = %#v, want finished", rec)
	}
	finishedAt := rec.FinishedAt

	// Second park: the handle is already nil, so the guard returns before any
	// second Finish and the recorded terminal is left untouched.
	run.parkDrain()
	if run.drain != nil {
		t.Fatal("second parkDrain must remain a no-op")
	}
	rec2 := latestTerminalDrain(t, d, repo)
	if rec2 == nil || rec2.State != store.StateFinished || !rec2.FinishedAt.Equal(finishedAt) {
		t.Fatalf("double park must not re-finalize: first=%v second=%#v", finishedAt, rec2)
	}
}

// TestImplementRunEnsureDrainReacquiresAndRefusesCollision drives ensureDrain
// directly: a no-op while a Drain is held, a clean re-acquire after a park, and a
// clean "already in progress" refusal when a rival drain has claimed the freed
// checkout — leaving the run parked (nil Drain) with nothing lost (ADR-0067).
func TestImplementRunEnsureDrainReacquiresAndRefusesCollision(t *testing.T) {
	d, repo := drainTestRepo(t)
	run := newTestImplementRun(d, repo, "demo")

	// Held: ensureDrain is a no-op and keeps the same handle.
	h, err := BeginDrain(d, repo, "demo", io.Discard)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	run.drain = h
	if err := run.ensureDrain(); err != nil {
		t.Fatalf("ensureDrain must no-op while a Drain is held: %v", err)
	}
	if run.drain != h {
		t.Fatal("ensureDrain must not replace a held Drain")
	}

	// Parked: ensureDrain re-acquires a fresh, live Drain.
	run.parkDrain()
	if run.drain != nil {
		t.Fatal("parkDrain should clear the handle")
	}
	if err := run.ensureDrain(); err != nil {
		t.Fatalf("ensureDrain re-acquire: %v", err)
	}
	if run.drain == nil {
		t.Fatal("ensureDrain must re-acquire a live Drain after a park")
	}
	if status := ReadRuntimeLockStatus(d, repo); !status.Locked {
		t.Fatalf("expected a live drain after re-acquire: %#v", status)
	}

	// Collision: park, let a rival claim the checkout, then the re-acquire must
	// refuse cleanly and stay parked.
	run.parkDrain()
	rival, err := BeginDrain(d, repo, "rival", io.Discard)
	if err != nil {
		t.Fatalf("rival must claim the parked checkout: %v", err)
	}
	t.Cleanup(func() { _ = rival.Finish(store.StateFinished, "", false, time.Time{}) })

	err = run.ensureDrain()
	assertExitCode(t, err, ExitOperational)
	if !strings.Contains(err.Error(), "already in progress") {
		t.Fatalf("collision must refuse with the mutual-exclusion error: %v", err)
	}
	if run.drain != nil {
		t.Fatal("a refused re-acquire must leave the run parked (nil Drain)")
	}
}

// TestImplementRunFinalizeDeclinedCancels pins the deferred-finalize declined
// path: a declined run never executed, so its Drain row is cancelled — no
// terminal is recorded and the checkout reads idle (ADR-0056).
func TestImplementRunFinalizeDeclinedCancels(t *testing.T) {
	d, repo := drainTestRepo(t)
	run := newTestImplementRun(d, repo, "demo")
	h, err := BeginDrain(d, repo, "demo", io.Discard)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	run.drain = h
	run.result = &RunTaskSetResult{Declined: true}

	var ferr error
	run.finalize(&ferr)

	if rec := latestTerminalDrain(t, d, repo); rec != nil {
		t.Fatalf("declined run must cancel, not record a terminal: %#v", rec)
	}
	if status := ReadRuntimeLockStatus(d, repo); status.Locked {
		t.Fatalf("declined run must free the checkout: %#v", status)
	}
}

// TestImplementRunFinalizeNilDrainIsNoop pins the parked-at-gate exit: with no
// held Drain the deferred finalize is a no-op (the park already recorded the
// segment's terminal), and must not panic or record anything.
func TestImplementRunFinalizeNilDrainIsNoop(t *testing.T) {
	d, repo := drainTestRepo(t)
	run := newTestImplementRun(d, repo, "demo") // drain nil: parked at a gate
	var ferr error
	run.finalize(&ferr)
	if rec := latestTerminalDrain(t, d, repo); rec != nil {
		t.Fatalf("parked finalize must be a no-op: %#v", rec)
	}
}

// TestImplementRunFinalizeErrorExitRecordsFinished pins the normal/error-exit
// terminal: a run that ends holding its Drain with a non-interrupt error records
// a finished terminal, its work disposition staying manifest-derived (ADR-0056).
func TestImplementRunFinalizeErrorExitRecordsFinished(t *testing.T) {
	d, repo := drainTestRepo(t)
	run := newTestImplementRun(d, repo, "demo")
	h, err := BeginDrain(d, repo, "demo", io.Discard)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	run.drain = h
	run.result = &RunTaskSetResult{TaskSetID: "demo"}

	var ferr error = exitErr(ExitNoRunnable, "no eligible AFK task")
	run.finalize(&ferr)

	rec := latestTerminalDrain(t, d, repo)
	if rec == nil || rec.State != store.StateFinished {
		t.Fatalf("error-exit terminal = %#v, want finished", rec)
	}
}
