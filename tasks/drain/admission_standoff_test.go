package drain

import (
	"bytes"
	"io"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/glebglazov/pop/internal/queuetest"
	"github.com/glebglazov/pop/store"
	"github.com/glebglazov/pop/tasks"
)

// TestQueuedCommandIsGrantedAheadOfPollingDaemonDispatch drives the whole
// ordering guarantee (ADR-0239) through the real store: a human command queues
// behind a running drain, and the daemon — polling the same readiness selector
// it polls every tick — never takes the window that opens, so the waiter is the
// one admitted.
//
// It is the test the guarantee actually needs. Everything else about the queue
// is strict FIFO among waiters; the way a human loses their turn is the daemon
// racing in the instant the current holder finishes, which no ordering among
// waiters can prevent.
func TestQueuedCommandIsGrantedAheadOfPollingDaemonDispatch(t *testing.T) {
	td := queuetest.TasksDeps(t, true)
	// A slow retry cadence is what makes the window under test observable: the
	// waiter's first attempt is spent while the holder still runs, so the tick
	// after the holder leaves lands inside the interval before its next one.
	td.AdmissionPollInterval = 2 * time.Second
	checkout := queuetest.InitGitRepoWithBase(t)

	holder, err := tasks.BeginDrain(td, checkout, "set-holder", io.Discard)
	if err != nil {
		t.Fatalf("holder BeginDrain: %v", err)
	}

	// The daemon's tick: one Ready set bound to the held checkout, selected
	// through the same claim read dispatch uses.
	deps := &Deps{Tasks: td}
	refresh := &tasks.RefreshResult{Rows: []tasks.Row{
		{ID: "set-daemon", Status: tasks.StatusReady, AutoDrain: true, Priority: 100},
	}}
	claimFor := func(string) *store.CheckoutClaim { return deps.CheckoutClaimAt(checkout) }
	tick := func() (SpawnDeferral, bool) {
		ids, deferral, ok := selectReadySets(refresh, nil, nil, claimFor)
		if ok || len(ids) > 0 {
			t.Fatalf("daemon dispatched %v while the checkout was spoken for", ids)
		}
		return deferral, ok
	}

	if deferral, _ := tick(); deferral.Reason != DeferCheckoutClaim {
		t.Fatalf("deferral while the holder runs = %+v, want DeferCheckoutClaim", deferral)
	}

	var waitLine lockedBuffer
	type grant struct {
		handle *tasks.DrainHandle
		waited bool
		err    error
	}
	granted := make(chan grant, 1)
	go func() {
		h, waited, err := tasks.BeginDrainWithAdmission(td, checkout, "set-waiter", &waitLine, tasks.AdmissionWait)
		granted <- grant{h, waited, err}
	}()

	// Act on a wait that has demonstrably started *and* been refused once: the
	// waiter must be in the line, with its first attempt spent, before the holder
	// finishes — otherwise the window the daemon must not take never exists.
	waitForQueuedCommand(t, td, "set-waiter")
	waitForWaitLine(t, &waitLine, "set-holder")

	if err := holder.Finish(store.DrainEnding{State: store.StateFinished}); err != nil {
		t.Fatalf("holder Finish: %v", err)
	}

	// The window is open and nothing holds the tree — and the daemon still stands
	// off, naming the queue rather than the holder that just left.
	deferral, _ := tick()
	if deferral.Reason != DeferAdmissionQueue {
		t.Fatalf("deferral over an idle-but-queued checkout = %+v, want DeferAdmissionQueue", deferral)
	}
	if deferral.Claim == nil || deferral.Claim.Holder.ContainerID != "set-waiter" {
		t.Fatalf("deferral claim = %+v, want the queued set-waiter", deferral.Claim)
	}
	if want := "checkout awaited by set set-waiter (queued command)"; deferral.Message() != want {
		t.Fatalf("deferral message = %q, want %q", deferral.Message(), want)
	}

	got := <-granted
	if got.err != nil {
		t.Fatalf("waiting command: %v", got.err)
	}
	if !got.waited {
		t.Fatal("waiting command reports it never queued")
	}
	defer func() { _ = got.handle.Finish(store.DrainEnding{State: store.StateFinished}) }()

	// The grant consumed the place in the line, and the checkout now reads as the
	// waiter's running drain — the daemon's next tick defers behind a holder again.
	if claim := deps.CheckoutClaimAt(checkout); claim == nil || claim.Reason != store.ClaimRunningDrain || claim.Holder.ContainerID != "set-waiter" {
		t.Fatalf("claim after the grant = %+v, want set-waiter's running drain", claim)
	}
}

// lockedBuffer is a wait-line sink two goroutines share: the waiting command
// writes it, the test reads it to see which block the wait is reporting.
type lockedBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *lockedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *lockedBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

// waitForWaitLine blocks until the waiting command has printed a wait line
// naming want — proof that one grant attempt has already been refused.
func waitForWaitLine(t *testing.T, out *lockedBuffer, want string) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if strings.Contains(out.String(), want) {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("wait line never named %s; got %q", want, out.String())
}

// waitForQueuedCommand blocks until setID holds a live place in an Admission
// queue.
func waitForQueuedCommand(t *testing.T, td *tasks.Deps, setID string) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		queued, err := tasks.LiveAdmissionWaiters(td)
		if err != nil {
			t.Fatalf("LiveAdmissionWaiters: %v", err)
		}
		for _, q := range queued {
			if q.SetID == setID {
				return
			}
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("no queued command for set %s appeared", setID)
}
