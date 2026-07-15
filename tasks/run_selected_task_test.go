package tasks

import (
	"bytes"
	"io"
	"os"
	"testing"
	"time"
)

// newRunSelectedTaskRun builds an implementRun wired to a real store-backed
// fixture holding a live Drain, then selects the fixture's single eligible AFK
// task, so runSelectedTask can be driven directly through the same setup the
// drain loop reaches. It threads the caller's opts (agent command, --yes,
// ConfirmIn) through newImplementRun + setup so the resolved plan / paths / result
// are populated exactly as production would. Returns the run, its fixture, the
// selection's refresh, and the selection.
func newRunSelectedTaskRun(t *testing.T, tasks []Task, agentCmd string, opts RunTaskSetOptions) (*implementRun, *runTaskSetFixture, *RefreshResult, *Selection) {
	t.Helper()
	return runFromFixture(t, setupRunTaskSetFixture(t, "demo", tasks), agentCmd, opts)
}

// TestRunSelectedTaskQuotaPauseFailedRegistrationExitsWithPauseFields drives the
// task-execution branch's quota-pause path (ADR-0100): a quota-paused attempt
// parks the Drain and tries to register a recovery waiter; when registration
// fails the branch exits cleanly (nil error, runTaskReturn) with the pause fields
// populated on the run's result. Registration is forced to fail by clearing the
// runtime path just before the branch runs, so RegisterRecoveryWaiter rejects the
// waiter on its missing-required-fields validation — the deterministic stand-in
// for a store write failure.
func TestRunSelectedTaskQuotaPauseFailedRegistrationExitsWithPauseFields(t *testing.T) {
	installClaudeQuotaAgent(t, t.TempDir())
	run, _, refresh, sel := newRunSelectedTaskRun(t,
		[]Task{{ID: "01-a", File: "01-a.md", Title: "A", Type: "AFK", Status: "open"}},
		"", RunTaskSetOptions{Yes: true, AgentPreset: "claude"})

	// The quota agent lives on PATH, so the attempt still quota-pauses with an empty
	// runtime path; the empty path only trips the waiter registration downstream.
	run.runtimePath = ""

	directive, res, err := run.runSelectedTask(refresh, sel)
	if err != nil {
		t.Fatalf("runSelectedTask: %v", err)
	}
	if directive != runTaskReturn {
		t.Fatalf("directive = %d, want runTaskReturn (%d)", directive, runTaskReturn)
	}
	if res != run.result {
		t.Fatal("a failed waiter registration must return the run's result")
	}
	if !res.QuotaPaused {
		t.Fatal("a failed waiter registration must populate result.QuotaPaused")
	}
	if res.PausePreset != "claude" {
		t.Fatalf("result.PausePreset = %q, want claude", res.PausePreset)
	}
	if res.PauseReason == "" {
		t.Fatal("result.PauseReason must carry the quota reason")
	}
	if res.PauseResetAt.IsZero() {
		t.Fatal("result.PauseResetAt must carry the reset instant")
	}
	// Parking the Drain for the wait dropped the live lock; the clean exit leaves it
	// parked (the finalize records nothing further).
	if run.drain != nil {
		t.Fatal("the quota-pause exit must leave the Drain parked")
	}
	// The attempt never completed, so nothing accumulated.
	if len(res.Completed) != 0 {
		t.Fatalf("result.Completed = %d, want 0 on a quota-pause exit", len(res.Completed))
	}
}

// TestRunSelectedTaskFailedGateUnhandledReturnsExecError pins the post-failure
// gate's fall-through: a failed attempt under --yes cannot open the Failed gate
// menu, so runSelectedTask prints the static stop advice and returns the original
// exec error (runTaskReturn), leaving the task failed.
func TestRunSelectedTaskFailedGateUnhandledReturnsExecError(t *testing.T) {
	env := setupRunTaskSetFixture(t, "demo", []Task{
		{ID: "01-a", File: "01-a.md", Title: "A", Type: "AFK", Status: "open"},
	})
	agent := writeFakeAgent(t, env.root, fakeAgentConfig{skipSentinel: true, summary: "no good"})

	var buf bytes.Buffer
	run, _, refresh, sel := runFromFixture(t, env, agent, RunTaskSetOptions{Yes: true, MaxTries: 1, MaxTriesExplicit: true, Output: &buf})

	directive, res, err := run.runSelectedTask(refresh, sel)
	if directive != runTaskReturn {
		t.Fatalf("directive = %d, want runTaskReturn (%d)", directive, runTaskReturn)
	}
	assertExitCode(t, err, ExitOperational)
	if res != run.result {
		t.Fatal("the unhandled Failed gate must return the run's result")
	}
	if !bytes.Contains(buf.Bytes(), []byte("pop tasks open demo/01-a.md")) {
		t.Fatalf("static Failed-gate advice missing reset hint:\n%s", buf.String())
	}
	assertTaskFailed(t, env.execFixture(), "01-a", 1)
}

// TestRunSelectedTaskFailedGateHandledContinues pins the other half: a failed
// attempt with an interactive input opens the Failed gate menu; selecting Re-run
// (1) resets the task to open and tells the loop to keep draining
// (runTaskContinue) rather than returning the exec error.
func TestRunSelectedTaskFailedGateHandledContinues(t *testing.T) {
	env := setupRunTaskSetFixture(t, "demo", []Task{
		{ID: "01-a", File: "01-a.md", Title: "A", Type: "AFK", Status: "open"},
	})
	agent := writeFakeAgent(t, env.root, fakeAgentConfig{skipSentinel: true, summary: "no good"})

	var buf bytes.Buffer
	run, _, refresh, sel := runFromFixture(t, env, agent, RunTaskSetOptions{
		MaxTries:         1,
		MaxTriesExplicit: true,
		ConfirmIn:        bytes.NewBufferString("1\n"),
		Output:           &buf,
	})

	directive, res, err := run.runSelectedTask(refresh, sel)
	if err != nil {
		t.Fatalf("runSelectedTask: %v", err)
	}
	if directive != runTaskContinue {
		t.Fatalf("directive = %d, want runTaskContinue (%d)", directive, runTaskContinue)
	}
	if res != nil {
		t.Fatalf("a handled Failed gate keeps draining and returns nil result, got %#v", res)
	}
	// Re-run reset the failed task back to open so the loop can pick it up again.
	assertTaskOpen(t, env.execFixture(), "01-a")
}

// runFromFixture builds an implementRun over an already-created fixture, mirroring
// newRunSelectedTaskRun's setup path for tests that need to install their own
// agent and inspect the fixture afterward.
func runFromFixture(t *testing.T, env *runTaskSetFixture, agentCmd string, opts RunTaskSetOptions) (*implementRun, *runTaskSetFixture, *RefreshResult, *Selection) {
	t.Helper()
	d := env.deps()
	d.ProcessAlive = func(pid int) bool { return pid == os.Getpid() }

	opts.ResolveInput = ResolveInput{CWD: env.root}
	opts.AgentCmd = agentCmd
	if opts.ConfirmOut == nil {
		opts.ConfirmOut = io.Discard
	}
	if opts.Output == nil {
		opts.Output = &bytes.Buffer{}
	}

	run, err := newImplementRun(d, nil, nil, opts)
	if err != nil {
		t.Fatalf("newImplementRun: %v", err)
	}
	t.Cleanup(func() {
		if run.drain != nil {
			finalizeDrain(run.drain, false, false, false, "", false, time.Time{}, nil)
		}
	})
	if err := run.setup(); err != nil {
		t.Fatalf("setup: %v", err)
	}
	refresh, err := RefreshWith(d, run.resolved.DefinitionPath, run.statePath)
	if err != nil {
		t.Fatalf("RefreshWith: %v", err)
	}
	sel, selErr := SelectTaskInSet(refresh, "demo")
	if selErr != nil {
		t.Fatalf("SelectTaskInSet: %v", selErr)
	}
	return run, env, refresh, sel
}
