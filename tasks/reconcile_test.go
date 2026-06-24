package tasks

import (
	"bytes"
	"os"
	"testing"

	"github.com/glebglazov/pop/internal/deps"
)

// seedRunningDrain stands up a running Drain owned by the current process,
// recording a fixed start token so reconcile-time liveness is deterministic. It
// returns the repo path the Drain runs against. The handle is intentionally not
// finished — the drain "dies" by leaving the running row behind.
func seedRunningDrain(t *testing.T, token string) (*Deps, string) {
	t.Helper()
	seed, repo := drainTestRepo(t)
	seed.ProcessStartToken = func(int) (string, bool) { return token, true }
	if _, err := AcquireRuntimeLockForSet(seed, repo, "demo", &bytes.Buffer{}); err != nil {
		t.Fatalf("seed running drain: %v", err)
	}
	if status := ReadRuntimeLockStatus(seed, repo); !status.Locked {
		t.Fatalf("expected a live drain after seeding")
	}
	return seed, repo
}

// gitForbidden fails any git fork, proving the reconcile pass forks no git.
func gitForbidden(t *testing.T) deps.Git {
	t.Helper()
	return &deps.MockGit{CommandFunc: func(args ...string) (string, error) {
		t.Fatalf("reconcile forked git: %v", args)
		return "", nil
	}}
}

func TestReconcileDrainsDeadPIDBecomesCrashed(t *testing.T) {
	_, repo := seedRunningDrain(t, "seed-token")

	// The owning process is gone: ProcessAlive false. Reconcile forks no git.
	dead := &Deps{
		FS:           deps.NewRealFileSystem(),
		Git:          gitForbidden(t),
		ProcessAlive: func(int) bool { return false },
	}
	n, err := ReconcileDrains(dead)
	if err != nil {
		t.Fatalf("ReconcileDrains: %v", err)
	}
	if n != 1 {
		t.Fatalf("crashed %d, want 1", n)
	}
	// crashed is an explicit terminal, not an absent record.
	if status := ReadRuntimeLockStatus(dead, repo); status.Locked {
		t.Fatalf("crashed drain still reads as live: %#v", status)
	}
	rec, err := ReadDrainOutcome(dead, repo)
	if err != nil {
		t.Fatalf("read terminal: %v", err)
	}
	if rec.Outcome != DrainOutcomeCrashed {
		t.Fatalf("terminal = %q, want crashed", rec.Outcome)
	}
	if !rec.Outcome.Abnormal() {
		t.Fatalf("crashed must be an abnormal terminal")
	}
}

func TestReconcileDrainsLiveDrainUntouched(t *testing.T) {
	_, repo := seedRunningDrain(t, "seed-token")

	// Same PID, same start token → genuinely the live drain. Never crash it.
	live := &Deps{
		FS:                deps.NewRealFileSystem(),
		Git:               gitForbidden(t),
		ProcessAlive:      func(pid int) bool { return pid == os.Getpid() },
		ProcessStartToken: func(int) (string, bool) { return "seed-token", true },
	}
	n, err := ReconcileDrains(live)
	if err != nil {
		t.Fatalf("ReconcileDrains: %v", err)
	}
	if n != 0 {
		t.Fatalf("crashed %d, want 0 (drain is live)", n)
	}
	if status := ReadRuntimeLockStatus(live, repo); !status.Locked {
		t.Fatalf("live drain wrongly reconciled away: %#v", status)
	}
}

func TestReconcileDrainsReusedPIDBecomesCrashed(t *testing.T) {
	_, repo := seedRunningDrain(t, "seed-token")

	// The PID is alive again, but it now belongs to a different process: its
	// start token differs from the one recorded for the drain. A reused PID must
	// not be mistaken for the live drain.
	reused := &Deps{
		FS:                deps.NewRealFileSystem(),
		Git:               gitForbidden(t),
		ProcessAlive:      func(pid int) bool { return pid == os.Getpid() },
		ProcessStartToken: func(int) (string, bool) { return "different-token", true },
	}
	n, err := ReconcileDrains(reused)
	if err != nil {
		t.Fatalf("ReconcileDrains: %v", err)
	}
	if n != 1 {
		t.Fatalf("crashed %d, want 1 (PID reused)", n)
	}
	rec, err := ReadDrainOutcome(reused, repo)
	if err != nil {
		t.Fatalf("read terminal: %v", err)
	}
	if rec.Outcome != DrainOutcomeCrashed {
		t.Fatalf("terminal = %q, want crashed", rec.Outcome)
	}
}

// TestReconcileDrainsUnverifiableTokenKeepsLiveDrain guards the conservative
// fallback: when the live process's start token cannot be read, an alive PID is
// trusted rather than crashed, so a genuinely-running drain is never lost.
func TestReconcileDrainsUnverifiableTokenKeepsLiveDrain(t *testing.T) {
	_, repo := seedRunningDrain(t, "seed-token")

	unverifiable := &Deps{
		FS:                deps.NewRealFileSystem(),
		Git:               gitForbidden(t),
		ProcessAlive:      func(pid int) bool { return pid == os.Getpid() },
		ProcessStartToken: func(int) (string, bool) { return "", false },
	}
	n, err := ReconcileDrains(unverifiable)
	if err != nil {
		t.Fatalf("ReconcileDrains: %v", err)
	}
	if n != 0 {
		t.Fatalf("crashed %d, want 0 (token unverifiable, PID alive)", n)
	}
	if status := ReadRuntimeLockStatus(unverifiable, repo); !status.Locked {
		t.Fatalf("live drain crashed on unverifiable token: %#v", status)
	}
}

// TestReconcileDrainsNoStoreIsNoOp confirms a pure reader never materialises an
// empty database: with no store on disk, reconcile is a no-op.
func TestReconcileDrainsNoStoreIsNoOp(t *testing.T) {
	d, _ := drainTestRepo(t)
	n, err := ReconcileDrains(d)
	if err != nil {
		t.Fatalf("ReconcileDrains with no store: %v", err)
	}
	if n != 0 {
		t.Fatalf("crashed %d, want 0", n)
	}
	if _, err := os.Stat(DrainStorePathWith(d)); !os.IsNotExist(err) {
		t.Fatalf("reconcile materialised a store: %v", err)
	}
}
