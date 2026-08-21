package tasks

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"github.com/glebglazov/pop/store"
)

// The Drain ending is what makes an Agent fallback walk's outcome legible to a
// human who was not watching (ADR-0231). Every walk below reaches its ending the
// way a drain does, and each asserts the row the journal reads afterwards: the
// terminal, the ending beside it, and the agent the stop belongs to.

// TestExhaustedWalkRecordsItsEndingOnTheDrain: with the list spent, the drain
// stops on an ordinary clean-finish exit reason — so without the ending beside it
// the row is indistinguishable from a healthy drain, which is exactly what hid
// this event from the journal.
func TestExhaustedWalkRecordsItsEndingOnTheDrain(t *testing.T) {
	env := setupRunTaskSetFixture(t, "demo", []Task{
		{ID: "01-a", File: "01-a.md", Title: "A", Type: "AFK", Status: "open"},
	})
	installCrashingCodexAgent(t, env.root, providerCrashMessage)
	installCrashingClaudeAgent(t, env.root, providerCrashMessage)

	var buf bytes.Buffer
	opts := env.runTaskSetOpts(true, "", &buf)
	opts.AgentPresets = []string{"codex", "claude"}
	opts.AgentExplicit = true
	opts.MaxTries = 2

	_, err := RunTaskSetWith(env.deps(), nil, nil, opts)
	assertExitCode(t, err, ExitOperational)

	dr := terminalDrainForFixture(t, env)
	if dr.State != store.StateFinished {
		t.Fatalf("terminal = %q, want the clean finish the process really made", dr.State)
	}
	if dr.Ending != store.EndingAgentsExhausted {
		t.Fatalf("ending = %q, want %q", dr.Ending, store.EndingAgentsExhausted)
	}
	// The agent that spent the last cap: the one a human looks at first.
	if dr.ExhaustedPreset != "claude" {
		t.Fatalf("agent = %q, want the agent that spent the last cap", dr.ExhaustedPreset)
	}
}

// TestRescuedBurnRecordsItsSpentCapWithoutAnEnding is the walk the whole work
// stream started from, once slice 04 made it survivable: the first agent burned
// its entire cap on the task and the second finished the work. The drain really
// did finish, so it carries no ending — and the attempts the first agent spent
// are recorded on their own, because a later rescue does not refund them and the
// human who paid for them has to be able to see them (ADR-0231).
func TestRescuedBurnRecordsItsSpentCapWithoutAnEnding(t *testing.T) {
	env := setupRunTaskSetFixture(t, "demo", []Task{
		{ID: "01-a", File: "01-a.md", Title: "A", Type: "AFK", Status: "open"},
	})
	installCrashingCodexAgent(t, env.root, providerCrashMessage)
	installFailsOnceThenFinishesClaudeAgent(t, env.root)

	var buf bytes.Buffer
	opts := env.runTaskSetOpts(true, "", &buf)
	opts.AgentPresets = []string{"codex", "claude"}
	opts.AgentExplicit = true
	opts.MaxTries = 2

	result, err := RunTaskSetWith(env.deps(), nil, nil, opts)
	if err != nil {
		t.Fatal(err)
	}
	if !result.TaskSetDone {
		t.Fatalf("result = %#v, want the second agent to have finished the work", result)
	}
	dr := terminalDrainForFixture(t, env)
	if dr.State != store.StateFinished || dr.Ending != "" {
		t.Fatalf("terminal = %q ending = %q, want the plain finish the drain really made", dr.State, dr.Ending)
	}

	caps := spentRetryCapsForFixture(t, env)
	if len(caps) != 1 {
		t.Fatalf("spent caps = %#v, want just the agent that burned its budget", caps)
	}
	spent := caps[0]
	if spent.Preset != "codex" || spent.Attempts != 2 {
		t.Fatalf("spent cap = %+v, want codex's two attempts", spent)
	}
	if spent.SetID != "demo" || spent.TaskID != "01-a" || spent.Phase != spendPhaseImplement {
		t.Fatalf("spent cap = %+v, want the task the attempts were spent on", spent)
	}
	if !strings.Contains(spent.Reason, providerCrashMessage) {
		t.Fatalf("spent cap reason = %q, want the provider's own last words", spent.Reason)
	}
	if spent.SpentAt.IsZero() {
		t.Fatal("spent cap carries no instant, so no journal window can hold it")
	}
}

// TestSpentCapOutlivesAQuotaPark closes the other half of the same gap: a walk
// whose later agent is quota-paused parks the drain and waits for the quota to
// come back rather than failing the task, and that park is a routine event —
// pop recovers from it by itself. The cap the earlier agent burned getting
// there is not routine, and it stays recorded through the park.
func TestSpentCapOutlivesAQuotaPark(t *testing.T) {
	env := setupRunTaskSetFixture(t, "demo", []Task{
		{ID: "01-a", File: "01-a.md", Title: "A", Type: "AFK", Status: "open"},
	})
	installCrashingCodexAgent(t, env.root, providerCrashMessage)
	installClaudeQuotaAgent(t, env.root)

	var buf bytes.Buffer
	opts := env.runTaskSetOpts(true, "", &buf)
	opts.AgentPresets = []string{"codex", "claude"}
	opts.AgentExplicit = true
	opts.MaxTries = 2

	d := env.deps()
	// The park blocks on the recovery waiter until claude's quota returns, so the
	// assertions run against the parked drain and the wait is ended by
	// deregistering the waiter — the same move the other park tests make.
	done := make(chan struct{})
	go func() {
		defer close(done)
		_, _ = RunTaskSetWith(d, nil, nil, opts)
	}()

	var waiter *RecoveryWaiter
	for i := 0; i < 200 && waiter == nil; i++ {
		time.Sleep(100 * time.Millisecond)
		var err error
		if waiter, err = GetRecoveryWaiter(d, "demo"); err != nil {
			t.Fatalf("GetRecoveryWaiter: %v", err)
		}
	}
	if waiter == nil || waiter.Preset != "claude" {
		t.Fatalf("waiter = %#v, want the drain parked on claude's quota", waiter)
	}

	dr := terminalDrainForFixture(t, env)
	if dr.State != store.StateQuotaPaused || dr.Ending != "" {
		t.Fatalf("terminal = %q ending = %q, want a park with no walk ending to report", dr.State, dr.Ending)
	}
	caps := spentRetryCapsForFixture(t, env)
	if len(caps) != 1 || caps[0].Preset != "codex" || caps[0].Attempts != 2 {
		t.Fatalf("spent caps = %#v, want codex's burned budget recorded through the park", caps)
	}

	if err := DeregisterRecoveryWaiter(d, "demo"); err != nil {
		t.Fatalf("DeregisterRecoveryWaiter: %v", err)
	}
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("the parked drain did not return after its waiter was deregistered")
	}
}

// spentRetryCapsForFixture reads the spent-cap events the journal will rank,
// scoped to nothing — the store is machine-global and the fixture is the only
// thing that has run.
func spentRetryCapsForFixture(t *testing.T, env *runTaskSetFixture) []SpentRetryCapRecord {
	t.Helper()
	caps, err := AllSpentRetryCaps(env.deps())
	if err != nil {
		t.Fatalf("AllSpentRetryCaps: %v", err)
	}
	return caps
}

// TestNoAgentStartedRecordsItsOwnEnding keeps the no-op distinguishable from the
// exhausted list: nothing was attempted, so the row says the drain could start no
// agent rather than that it spent one.
func TestNoAgentStartedRecordsItsOwnEnding(t *testing.T) {
	env := setupRunTaskSetFixture(t, "demo", []Task{
		{ID: "01-a", File: "01-a.md", Title: "A", Type: "AFK", Status: "open"},
	})
	installCountingAgentShim(t, env.root, "claude",
		`if [ "$1" = auth ] && [ "$2" = status ]; then printf '{"loggedIn":false}\n'; exit 0; fi`,
		"printf 'unexpected work invocation\\n'\nexit 1\n")
	installCountingAgentShim(t, env.root, "cursor-agent",
		`if [ "$1" = status ]; then printf '{"isAuthenticated":false}\n'; exit 0; fi`,
		"printf 'unexpected work invocation\\n'\nexit 1\n")

	var buf bytes.Buffer
	opts := env.runTaskSetOpts(true, "", &buf)
	opts.AgentPresets = []string{"claude", "cursor"}
	opts.AgentExplicit = true
	opts.MaxTries = 2

	_, err := RunTaskSetWith(env.deps(), nil, nil, opts)
	assertExitCode(t, err, ExitSetup)

	dr := terminalDrainForFixture(t, env)
	if dr.Ending != store.EndingNoAgentStarted {
		t.Fatalf("ending = %q, want %q", dr.Ending, store.EndingNoAgentStarted)
	}
	if dr.ExhaustedPreset == "" {
		t.Fatal("ending names no agent, so the journal cannot say which login to fix")
	}
}

// terminalDrainForFixture reads the row the journal will read: the latest
// terminal Drain of the fixture's runtime checkout.
func terminalDrainForFixture(t *testing.T, env *runTaskSetFixture) *store.Drain {
	t.Helper()
	_, runtimePath, _ := runtimeHead(t, env.deps(), env.root)
	dr := latestTerminalDrain(t, env.deps(), runtimePath)
	if dr == nil {
		t.Fatal("no terminal drain recorded")
	}
	return dr
}
