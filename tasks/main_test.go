package tasks

import (
	"io"
	"os"
	"strings"
	"testing"
	"time"
)

// testRetryDelayWaitHook skips wall-clock retry sleeps in package tests while
// preserving the retry notice tests assert on. Wired onto Deps.RetryDelayWait
// by newTestDeps and the shared fixtures (ADR-0145).
func testRetryDelayWaitHook(out io.Writer, delay time.Duration) bool {
	if delay > 0 {
		outputFor(out).line(ansiYellow, "↻ Retrying with preserved changes...")
	}
	return false
}

// hangGuard is how long a test waits for an event that must come. It only stops
// a broken run from hanging the package; it is never the assertion, so it is
// sized for a loaded machine running the whole tree, not for the event.
const hangGuard = time.Minute

// retryDelayRecorder keeps every retry delay a run waited out, so a test
// asserts on the pacing the run chose rather than on how long a loaded machine
// took to run it.
type retryDelayRecorder struct {
	delays []time.Duration
}

// recordRetryDelays wires d.RetryDelayWait to the returned recorder. Only a
// positive delay reaches the seam; an instant retry never calls it.
func recordRetryDelays(d *Deps) *retryDelayRecorder {
	r := &retryDelayRecorder{}
	d.RetryDelayWait = func(out io.Writer, delay time.Duration) bool {
		r.delays = append(r.delays, delay)
		return testRetryDelayWaitHook(out, delay)
	}
	return r
}

func (r *retryDelayRecorder) assertNone(t *testing.T) {
	t.Helper()
	if len(r.delays) != 0 {
		t.Fatalf("run waited out retry delays %v, want none", r.delays)
	}
}

// waitForRecoveryWaiter blocks until the drain under test has parked on its
// quota pause and registered its recovery waiter, and returns that waiter.
func waitForRecoveryWaiter(t *testing.T, d *Deps, setID string) *RecoveryWaiter {
	t.Helper()
	deadline := time.Now().Add(hangGuard)
	for time.Now().Before(deadline) {
		waiter, err := GetRecoveryWaiter(d, setID)
		if err != nil {
			t.Fatalf("GetRecoveryWaiter: %v", err)
		}
		if waiter != nil {
			return waiter
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("recovery waiter for %q was never registered", setID)
	return nil
}

// TestMain cleans up shared git template state after the package run.
func TestMain(m *testing.M) {
	code := m.Run()
	if gitTemplateDir != "" {
		_ = os.RemoveAll(gitTemplateDir)
	}
	os.Exit(code)
}

// TestGuardTestStorePathPanicsWithoutIsolation confirms the store-open guard
// still trips when a test reaches the real machine-global data dir (ADR-0145).
func TestGuardTestStorePathPanicsWithoutIsolation(t *testing.T) {
	if prodDataDirAtStartup == "" {
		t.Skip("no production data dir detected")
	}
	d := DefaultDeps()
	defer func() {
		r := recover()
		if r == nil {
			t.Fatal("expected panic when opening store without test isolation")
		}
		msg, ok := r.(string)
		if !ok || !strings.Contains(msg, "real pop store") {
			t.Fatalf("panic = %#v, want guard message about real pop store", r)
		}
	}()
	_, _, _ = d.Store(true)
}
