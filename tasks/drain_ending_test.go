package tasks

import (
	"bytes"
	"testing"

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

// TestOrdinaryFallThroughRecordsNoEnding is the healthy multi-agent walk, which
// must not compete for attention with the drain above: one agent stepped aside,
// the next finished the work, and nothing was lost.
func TestOrdinaryFallThroughRecordsNoEnding(t *testing.T) {
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
		t.Fatalf("terminal = %q ending = %q, want a plain finish with nothing to report", dr.State, dr.Ending)
	}
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
