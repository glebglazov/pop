package tasks

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// The four endings a walk that ran out of agents can have (ADR-0231), each
// driven through a whole drain on the wording that produces it. What separates
// them is only ever how the last agent stopped, so every test here reaches the
// disposition the way the drain does — by exhausting a real agent list — rather
// than by asking for a disposition directly.

// TestExhaustedByProviderCollapseLeavesTheTaskOpen is the ending the corpus is
// almost entirely made of: every attempt died on the provider's side. Nothing
// about the work failed, so the task keeps its Open status and no progress
// record is written — and the drain after it, which is what Work supervision
// runs once the machine is awake and the network is back, finishes the task.
func TestExhaustedByProviderCollapseLeavesTheTaskOpen(t *testing.T) {
	env := setupRunTaskSetFixture(t, "demo", []Task{
		{ID: "01-a", File: "01-a.md", Title: "A", Type: "AFK", Status: "open"},
	})
	// Attempts 1 and 2 are the first drain's whole cap; attempt 3 is the drain
	// after it, by which time the connection is back.
	installCountingAgentShim(t, env.root, "claude", claudeAuthStatusGuard,
		fmt.Sprintf(`if [ "$n" -le 2 ]; then printf '%%s\n' %s; exit 1; fi
`, shellQuote(providerCrashMessage))+tickTaskShellSnippet+`printf 'SUMMARY_START\nclaude done\nSUMMARY_END\nTASK_COMPLETE\n'
`)

	var buf bytes.Buffer
	opts := env.runTaskSetOpts(true, "", &buf)
	opts.AgentPresets = []string{"claude"}
	opts.AgentExplicit = true
	opts.MaxTries = 2

	_, err := RunTaskSetWith(env.deps(), nil, nil, opts)
	assertExitCode(t, err, ExitOperational)
	if !strings.Contains(err.Error(), providerCrashMessage) {
		t.Fatalf("err = %v, want the provider's own sentence", err)
	}
	for _, want := range []string{
		"Out of agents for demo/01-a after 2 attempts",
		"Last: " + providerCrashMessage,
		"stays open and a later drain retries it",
	} {
		if !strings.Contains(buf.String(), want) {
			t.Fatalf("output missing %q:\n%s", want, buf.String())
		}
	}
	assertTaskStatus(t, env, "01-a", TaskOpen)
	if _, statErr := os.Stat(filepath.Join(env.execFixture().demoDir(), "progress.txt")); !os.IsNotExist(statErr) {
		t.Fatalf("a provider collapse wrote a terminal progress record: %v", statErr)
	}

	// The task was left where unattended supervision can pick it up, so the next
	// drain is the whole point of the ending.
	var second bytes.Buffer
	next := env.runTaskSetOpts(true, "", &second)
	next.AgentPresets = []string{"claude"}
	next.AgentExplicit = true
	next.MaxTries = 2
	result, err := RunTaskSetWith(env.deps(), nil, nil, next)
	if err != nil {
		t.Fatalf("second drain: %v", err)
	}
	if !result.TaskSetDone || len(result.Completed) != 1 {
		t.Fatalf("result = %#v, want the later drain to have finished the work", result)
	}
	assertTaskDone(t, env.execFixture(), "01-a")
}

// TestExhaustedByCleanRunsFailsTheTask is the other side of the exit status: both
// agents ran to an ending of their own and neither satisfied the contract — one
// reported a blocker it found, the other left the acceptance criteria unchecked.
// That is a task fault, and it is the ending a human is for.
func TestExhaustedByCleanRunsFailsTheTask(t *testing.T) {
	env := setupRunTaskSetFixture(t, "demo", []Task{
		{ID: "01-a", File: "01-a.md", Title: "A", Type: "AFK", Status: "open"},
		{ID: "02-b", File: "02-b.md", Title: "B", Type: "AFK", Status: "open"},
	})
	const blocker = "the migration needs a product decision I cannot make"
	installCountingAgentShim(t, env.root, "codex", `if [ "$1" = login ]; then exit 0; fi`,
		fmt.Sprintf(`printf 'SUMMARY_START\nlooked into it\nSUMMARY_END\nTASK_FAILED: %%s\n' %s
`, shellQuote(blocker)))
	// A clean run that says it finished and leaves the boxes unticked: the
	// harness's own assessment, not the agent's word for it.
	installCountingAgentShim(t, env.root, "claude", claudeAuthStatusGuard,
		`printf 'SUMMARY_START\nall done\nSUMMARY_END\nTASK_COMPLETE\n'
`)

	var buf bytes.Buffer
	opts := env.runTaskSetOpts(true, "", &buf)
	opts.AgentPresets = []string{"codex", "claude"}
	opts.AgentExplicit = true
	opts.MaxTries = 1

	_, err := RunTaskSetWith(env.deps(), nil, nil, opts)
	assertExitCode(t, err, ExitOperational)
	// The blocker codex reported is what the operator reads on the fall-through,
	// and the assessment's own words are what the ending records.
	if !strings.Contains(buf.String(), blocker) {
		t.Fatalf("output missing the agent's own blocker:\n%s", buf.String())
	}
	assertTaskFailed(t, env.execFixture(), "01-a", 2)
	assertProgressContains(t, env.execFixture(), "FAILED", reasonUncheckedBoxes)
	if strings.Contains(buf.String(), "split the task") {
		t.Fatalf("a contract failure must not advise splitting:\n%s", buf.String())
	}
	// The stop is per drain: the next task is not ground through the same list.
	assertTaskStatus(t, env, "02-b", TaskOpen)
}

// TestExhaustedByTimeoutsAsksForTheTaskToBeSplit: several agents' worth of
// evidence that the work does not fit one attempt is not something a later drain
// fixes, so the ending is Failed — with the one piece of advice retrying cannot
// deliver.
func TestExhaustedByTimeoutsAsksForTheTaskToBeSplit(t *testing.T) {
	env := setupRunTaskSetFixture(t, "demo", []Task{
		{ID: "01-a", File: "01-a.md", Title: "A", Type: "AFK", Status: "open"},
	})
	// Real subprocesses: the in-process fake never hangs, so a timeout kill needs
	// a shim that really sleeps past the deadline (ADR-0144).
	installHangingAgent(t, env.root, "codex", `if [ "$1" = login ]; then exit 0; fi`)
	installHangingAgent(t, env.root, "claude", claudeAuthStatusGuard)

	var buf bytes.Buffer
	opts := env.runTaskSetOpts(true, "", &buf)
	opts.AgentPresets = []string{"codex", "claude"}
	opts.AgentExplicit = true
	opts.MaxTries = 1
	opts.Timeout = 100 * time.Millisecond

	_, err := RunTaskSetWith(env.deps(), nil, nil, opts)
	assertExitCode(t, err, ExitOperational)
	for _, want := range []string{
		"Last: timed out after 100ms",
		"split the task rather than re-running it",
	} {
		if !strings.Contains(buf.String(), want) {
			t.Fatalf("output missing %q:\n%s", want, buf.String())
		}
	}
	assertTaskFailed(t, env.execFixture(), "01-a", 2)
	assertProgressContains(t, env.execFixture(), "FAILED", "timed out after 100ms")
}

// TestNoAgentCouldStartIsAReportedNoOp: with every agent unusable before it was
// ever invoked, nothing was attempted — so nothing failed, no task changed
// state, and the stop says so rather than reading as an exhausted list.
func TestNoAgentCouldStartIsAReportedNoOp(t *testing.T) {
	env := setupRunTaskSetFixture(t, "demo", []Task{
		{ID: "01-a", File: "01-a.md", Title: "A", Type: "AFK", Status: "open"},
	})
	claudeCalls := installCountingAgentShim(t, env.root, "claude",
		`if [ "$1" = auth ] && [ "$2" = status ]; then printf '{"loggedIn":false}\n'; exit 0; fi`,
		"printf 'unexpected work invocation\\n'\nexit 1\n")
	cursorCalls := installCountingAgentShim(t, env.root, "cursor-agent",
		`if [ "$1" = status ]; then printf '{"isAuthenticated":false}\n'; exit 0; fi`,
		"printf 'unexpected work invocation\\n'\nexit 1\n")

	var buf bytes.Buffer
	opts := env.runTaskSetOpts(true, "", &buf)
	opts.AgentPresets = []string{"claude", "cursor"}
	opts.AgentExplicit = true
	opts.MaxTries = 3

	_, err := RunTaskSetWith(env.deps(), nil, nil, opts)
	assertExitCode(t, err, ExitSetup)
	for _, want := range []string{
		"no agent could be started, so demo/01-a was not attempted and is unchanged",
		`claude: "{\"loggedIn\":false}"`,
		`cursor: "{\"isAuthenticated\":false}"`,
	} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("err = %v, want it to carry %q", err, want)
		}
	}
	if strings.Contains(err.Error(), "Out of agents") {
		t.Fatalf("err = %v, want a no-op rather than an exhausted list", err)
	}
	for _, counter := range []string{claudeCalls, cursorCalls} {
		if _, statErr := os.Stat(counter); !os.IsNotExist(statErr) {
			t.Fatalf("agent was invoked for work despite being unusable (%s): %v", counter, statErr)
		}
	}
	assertTaskStatus(t, env, "01-a", TaskOpen)
	if _, statErr := os.Stat(filepath.Join(env.execFixture().demoDir(), "progress.txt")); !os.IsNotExist(statErr) {
		t.Fatalf("a no-op wrote a progress record: %v", statErr)
	}
}
