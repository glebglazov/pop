package tasks

import (
	"bytes"
	"io"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/glebglazov/pop/internal/tty"
	"github.com/glebglazov/pop/store"
)

// waitingDeps is a Deps that spins the admission poll loop fast and answers the
// tty question without a terminal, so a wait line can be asserted in
// milliseconds.
func waitingDeps(t *testing.T, d *Deps, tty string) *Deps {
	t.Helper()
	cp := *d
	cp.AdmissionPollInterval = time.Millisecond
	cp.ProcessTTY = func(int) string { return tty }
	return &cp
}

// waitForAdmissionQueue blocks until a waiter for setID is registered on
// runtimePath, so a test can act on a wait that has demonstrably started rather
// than sleep and hope.
func waitForAdmissionQueue(t *testing.T, d *Deps, runtimePath, setID string) {
	t.Helper()
	s, err := openDrainStore(d)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		line, err := s.AdmissionWaitersOn(runtimePath)
		if err != nil {
			t.Fatalf("AdmissionWaitersOn: %v", err)
		}
		for _, w := range line {
			if w.SetID == setID {
				return
			}
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("no admission waiter for set %s on %s appeared", setID, runtimePath)
}

// A held checkout no longer sends the human away to re-run the command: the
// drain queues, and the line it prints while it waits says who holds the tree,
// why, and where to go and find them.
func TestImplementWaitsAndNamesWhoHoldsTheCheckout(t *testing.T) {
	base, repo := drainTestRepo(t)
	d := waitingDeps(t, base, "s003")

	rival, err := BeginDrain(d, repo, "rival", io.Discard)
	if err != nil {
		t.Fatalf("rival drain: %v", err)
	}
	// The rival's pane is the other half of "where to reach them": a set drained
	// by the supervisor is answerable in a tmux pane, and the wait line says which.
	if err := RecordDrainPane(d, DrainPane{
		ScopedKey: "k", SetID: "rival", RuntimePath: repo, PaneID: "%7", RecordedAt: time.Now().UTC(),
	}); err != nil {
		t.Fatalf("RecordDrainPane: %v", err)
	}

	var mu sync.Mutex
	var out bytes.Buffer
	type result struct {
		handle *DrainHandle
		waited bool
		err    error
	}
	done := make(chan result, 1)
	go func() {
		h, waited, err := BeginDrainWithAdmission(d, repo, "demo", &lockedWriter{mu: &mu, w: &out}, AdmissionWait)
		done <- result{h, waited, err}
	}()

	waitForAdmissionQueue(t, d, repo, "demo")
	// Let the loop print at least once before the window opens.
	deadline := time.Now().Add(10 * time.Second)
	for {
		mu.Lock()
		printed := out.String()
		mu.Unlock()
		if strings.Contains(printed, "Waiting for checkout") {
			if !strings.Contains(printed, "held by set rival (running drain)") {
				t.Fatalf("wait line must name the holder and why: %q", printed)
			}
			for _, want := range []string{"PID ", "tty s003", "pane %7"} {
				if !strings.Contains(printed, want) {
					t.Fatalf("wait line missing %q: %q", want, printed)
				}
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("no wait line printed: %q", printed)
		}
		time.Sleep(time.Millisecond)
	}

	if err := rival.Finish(store.DrainEnding{State: store.StateFinished}); err != nil {
		t.Fatalf("rival finish: %v", err)
	}

	select {
	case r := <-done:
		if r.err != nil {
			t.Fatalf("the wait must end in a grant, not an error: %v", r.err)
		}
		if !r.waited {
			t.Fatal("a command that queued must report that it waited, so its caller re-derives")
		}
		t.Cleanup(func() { _ = r.handle.Finish(store.DrainEnding{State: store.StateFinished}) })
	case <-time.After(10 * time.Second):
		t.Fatal("the wait never ended after the holder finished")
	}
	if status := ReadRuntimeLockStatus(d, repo); !status.Locked || status.Metadata.SetID != "demo" {
		t.Fatalf("the granted drain must hold the checkout: %#v", status)
	}
}

// A drain parked at a Failed gate over a dirty tree holds the checkout with no
// running drain at all. The waiter hears that as the gate — the thing a human can
// go and answer — not as the store's raw no-steal refusal.
func TestAdmissionWaitReportsAGateParkedDrain(t *testing.T) {
	base, repo := drainTestRepo(t)
	d := waitingDeps(t, base, "s004")

	s, err := openDrainStore(d)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	if err := s.PutCheckoutGateHold(store.CheckoutGateHold{
		RuntimePath: repo, SetID: "parked", PID: os.Getpid(), Claim: true, RegisteredAt: time.Now().UTC(),
	}); err != nil {
		t.Fatalf("PutCheckoutGateHold: %v", err)
	}

	var mu sync.Mutex
	var out bytes.Buffer
	done := make(chan error, 1)
	go func() {
		h, _, err := BeginDrainWithAdmission(d, repo, "demo", &lockedWriter{mu: &mu, w: &out}, AdmissionWait)
		if h != nil {
			_ = h.Finish(store.DrainEnding{State: store.StateFinished})
		}
		done <- err
	}()

	waitForAdmissionQueue(t, d, repo, "demo")
	deadline := time.Now().Add(10 * time.Second)
	for {
		mu.Lock()
		printed := out.String()
		mu.Unlock()
		if strings.Contains(printed, "failed gate, uncommitted changes") {
			if !strings.Contains(printed, "held by set parked") {
				t.Fatalf("the gate wait line must name the parked set: %q", printed)
			}
			if strings.Contains(printed, "held by another live owner") {
				t.Fatalf("the raw gate-hold refusal leaked into the wait line: %q", printed)
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("no gate wait line printed: %q", printed)
		}
		time.Sleep(time.Millisecond)
	}

	if err := s.DeleteCheckoutGateHold(repo, "parked"); err != nil {
		t.Fatalf("DeleteCheckoutGateHold: %v", err)
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("admission after the gate released: %v", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("the wait never ended after the gate hold was released")
	}
}

// SIGINT during a wait is a human changing their mind: the command leaves the
// queue — so nothing behind it stalls — and exits as an interrupt.
func TestAdmissionWaitInterruptLeavesTheQueue(t *testing.T) {
	// Keep the process alive through the SIGINT this test raises: with a channel
	// registered for the whole test, the runtime never falls back to the default
	// disposition, whatever order the wait loop's own Notify lands in.
	guard := make(chan os.Signal, 1)
	signal.Notify(guard, syscall.SIGINT)
	defer signal.Stop(guard)

	base, repo := drainTestRepo(t)
	d := waitingDeps(t, base, "")

	rival, err := BeginDrain(d, repo, "rival", io.Discard)
	if err != nil {
		t.Fatalf("rival drain: %v", err)
	}
	t.Cleanup(func() { _ = rival.Finish(store.DrainEnding{State: store.StateFinished}) })

	done := make(chan error, 1)
	go func() {
		_, _, err := BeginDrainWithAdmission(d, repo, "demo", io.Discard, AdmissionWait)
		done <- err
	}()
	waitForAdmissionQueue(t, d, repo, "demo")

	if err := syscall.Kill(os.Getpid(), syscall.SIGINT); err != nil {
		t.Fatalf("raise SIGINT: %v", err)
	}
	select {
	case err := <-done:
		assertExitCode(t, err, ExitInterrupted)
	case <-time.After(10 * time.Second):
		t.Fatal("SIGINT did not end the wait")
	}

	s, err := openDrainStore(d)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	line, err := s.AdmissionWaitersOn(repo)
	if err != nil {
		t.Fatalf("AdmissionWaitersOn: %v", err)
	}
	if len(line) != 0 {
		t.Fatalf("an interrupted wait left its place in the queue: %+v", line)
	}
}

// The wait line is neither a spinner nor a one-shot: it reprints when what it is
// waiting on changes, and otherwise only on the heartbeat, so an hour behind a
// gate is neither noise nor silence.
func TestAdmissionWaitLineReprintsOnChangeAndHeartbeat(t *testing.T) {
	var buf bytes.Buffer
	p := &admissionPrinter{out: outputFor(&buf), heartbeat: time.Minute}
	start := time.Date(2026, 8, 26, 10, 0, 0, 0, time.UTC)

	line := func(text string) func() string { return func() string { return text } }
	p.blocked(start, "a", line("held by set a"))
	p.blocked(start.Add(30*time.Second), "a", line("held by set a"))
	if got := strings.Count(buf.String(), "held by set a"); got != 1 {
		t.Fatalf("an unchanged reason printed %d times inside the heartbeat, want 1: %q", got, buf.String())
	}
	p.blocked(start.Add(31*time.Second), "b", line("held by set b"))
	if !strings.Contains(buf.String(), "held by set b") {
		t.Fatalf("a changed reason must reprint at once: %q", buf.String())
	}
	p.blocked(start.Add(2*time.Minute), "b", line("held by set b"))
	if got := strings.Count(buf.String(), "held by set b"); got != 2 {
		t.Fatalf("the heartbeat printed %d times, want a second line: %q", got, buf.String())
	}
}

// The default answers "is a human here?", and the two flags override it in both
// directions — a machine that would block forever and a human who would rather
// see the error are both mistakes the flags exist to prevent.
func TestAdmissionWaitChoiceResolvesAgainstTheTerminal(t *testing.T) {
	master, slaveName, err := tty.OpenPTY()
	if err != nil {
		t.Skipf("no pty available: %v", err)
	}
	defer master.Close()
	terminal, err := os.OpenFile(slaveName, os.O_RDWR, 0)
	if err != nil {
		t.Fatalf("open %s: %v", slaveName, err)
	}
	defer terminal.Close()

	cases := []struct {
		name   string
		choice AdmissionWaitChoice
		in     io.Reader
		want   AdmissionPolicy
	}{
		{"a terminal waits by default", AdmissionWaitAuto, terminal, AdmissionWait},
		{"a pipe refuses by default", AdmissionWaitAuto, bytes.NewBufferString(""), AdmissionRefuse},
		{"an explicit non-interactive caller refuses", AdmissionWaitAuto, NonInteractiveReader{}, AdmissionRefuse},
		{"--wait waits unattended", AdmissionWaitAlways, NonInteractiveReader{}, AdmissionWait},
		{"--no-wait refuses at a terminal", AdmissionWaitNever, terminal, AdmissionRefuse},
	}
	for _, tc := range cases {
		if got := tc.choice.Policy(tc.in); got != tc.want {
			t.Errorf("%s: policy = %v, want %v", tc.name, got, tc.want)
		}
	}
}

// A non-interactive drain finds the checkout held and exits with the claim's own
// sentence rather than blocking: a script that never returns is worse than one
// that reports busy.
func TestNonInteractiveImplementStillRefuses(t *testing.T) {
	env := setupRunTaskSetFixture(t, "demo", []Task{
		{ID: "01-a", File: "01-a.md", Title: "A", Type: "AFK", Status: "open"},
	})
	d := env.deps()
	runtimePath, err := ResolveRuntimePathWith(d, env.root, "")
	if err != nil {
		t.Fatalf("resolve runtime path: %v", err)
	}
	rival, err := BeginDrain(d, runtimePath, "rival", io.Discard)
	if err != nil {
		t.Fatalf("rival drain: %v", err)
	}
	t.Cleanup(func() { _ = rival.Finish(store.DrainEnding{State: store.StateFinished}) })

	opts := env.runTaskSetOpts(true, writeFakeAgent(t, env.root, fakeAgentConfig{checkTask: true, summary: "ok"}), nil)
	opts.ConfirmIn = NonInteractiveReader{}
	_, err = RunTaskSetWith(d, nil, nil, opts)
	assertExitCode(t, err, ExitOperational)
	if !strings.Contains(err.Error(), "is claimed by set rival") {
		t.Fatalf("refusal = %v, want the Checkout claim sentence", err)
	}
}

// Work that finished while the command waited is a clean success: the human
// asked for the set to be drained, and it is. A granted command therefore looks
// again at its target rather than acting on what it decided before the wait.
func TestAdmittedRunReportsNothingLeftWhenTheWorkFinished(t *testing.T) {
	env := setupRunTaskSetFixture(t, "demo", []Task{
		{ID: "01-a", File: "01-a.md", Title: "A", Type: "AFK", Status: "open"},
	})
	base := env.deps()
	d := waitingDeps(t, base, "")
	runtimePath, err := ResolveRuntimePathWith(d, env.root, "")
	if err != nil {
		t.Fatalf("resolve runtime path: %v", err)
	}
	rival, err := BeginDrain(d, runtimePath, "rival", io.Discard)
	if err != nil {
		t.Fatalf("rival drain: %v", err)
	}

	var out bytes.Buffer
	opts := env.runTaskSetOpts(true, writeFakeAgent(t, env.root, fakeAgentConfig{checkTask: true, summary: "ok"}), &out)
	opts.Wait = AdmissionWaitAlways

	type outcome struct {
		result *RunTaskSetResult
		err    error
	}
	done := make(chan outcome, 1)
	go func() {
		r, err := RunTaskSetWith(d, nil, nil, opts)
		done <- outcome{r, err}
	}()

	waitForAdmissionQueue(t, d, runtimePath, "demo")
	// While the command waits, the set is signed off elsewhere — here by archiving
	// it, the disposition a human reaches from an assist menu.
	if _, err := ArchiveTaskSetWith(d, nil, nil, ResolveInput{CWD: env.root}, "demo"); err != nil {
		t.Fatalf("archive while waiting: %v", err)
	}
	if err := rival.Finish(store.DrainEnding{State: store.StateFinished}); err != nil {
		t.Fatalf("rival finish: %v", err)
	}

	select {
	case got := <-done:
		if got.err != nil {
			t.Fatalf("work finished while waiting must be a clean success, got %v", got.err)
		}
		if got.result == nil || !got.result.TaskSetDone {
			t.Fatalf("result = %#v, want a done set", got.result)
		}
		if !strings.Contains(out.String(), "Nothing left to drain") {
			t.Fatalf("output must say nothing is left: %q", out.String())
		}
	case <-time.After(20 * time.Second):
		t.Fatal("the admitted run never returned")
	}
}

// A waiter whose process died must not keep its place: the opportunistic
// reconcile every layer-2 reader runs removes it, so a strict queue is never
// wedged behind someone who is gone.
func TestReconcileSweepsADeadWaitersPlaceInTheQueue(t *testing.T) {
	d, repo := drainTestRepo(t)
	s, err := openDrainStore(d)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	if _, err := s.RegisterAdmissionWaiter(store.AdmissionWaiter{
		RuntimePath: repo, SetID: "ghost", PID: 999999, ProcStart: "gone", RegisteredAt: time.Now().UTC(),
	}); err != nil {
		t.Fatalf("RegisterAdmissionWaiter: %v", err)
	}
	if _, err := ReconcileDrains(d); err != nil {
		t.Fatalf("ReconcileDrains: %v", err)
	}
	line, err := s.AdmissionWaitersOn(repo)
	if err != nil {
		t.Fatalf("AdmissionWaitersOn: %v", err)
	}
	if len(line) != 0 {
		t.Fatalf("the dead owner kept its place in the queue: %+v", line)
	}
}

// lockedWriter serialises the wait loop's writes against a test reading what has
// been printed so far.
type lockedWriter struct {
	mu *sync.Mutex
	w  io.Writer
}

func (l *lockedWriter) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.w.Write(p)
}
