package tasks

import (
	"bytes"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// providerCrashMessage is the wording most of the corpus's non-zero exits carried
// (ADR-0231). No adapter recognises it, so it produces no Agent proceed verdict —
// the exact class of failure that used to end the walk where it stood and leave
// every remaining agent untried.
const providerCrashMessage = "API Error: Connection closed mid-response"

// TestUnrecognisedFailureHandsTheTaskToTheNextAgent is the defect the whole
// investigation started from, driven as a whole drain: the first agent spends its
// cap crashing for a reason nothing recognises, and the second agent — which used
// never to be invoked — takes the turn on its own full allowance and finishes.
func TestUnrecognisedFailureHandsTheTaskToTheNextAgent(t *testing.T) {
	env := setupRunTaskSetFixture(t, "demo", []Task{
		{ID: "01-a", File: "01-a.md", Title: "A", Type: "AFK", Status: "open"},
	})
	codexCalls := installCrashingCodexAgent(t, env.root, providerCrashMessage)
	claudeCalls := installFailsOnceThenFinishesClaudeAgent(t, env.root)

	var buf bytes.Buffer
	opts := env.runTaskSetOpts(true, "", &buf)
	opts.AgentPresets = []string{"codex", "claude"}
	opts.AgentExplicit = true
	opts.MaxTries = 2

	result, err := RunTaskSetWith(env.deps(), nil, nil, opts)
	if err != nil {
		t.Fatal(err)
	}
	if !result.TaskSetDone || len(result.Completed) != 1 {
		t.Fatalf("result = %#v, want the second agent to have finished the work", result)
	}
	if got := agentCalls(t, codexCalls); got != 2 {
		t.Fatalf("codex attempts = %d, want its whole cap of 2 spent before handing on", got)
	}
	// The cap is per agent: claude gets a full allowance of its own, so its own
	// first failure still leaves it a try to finish on.
	if got := agentCalls(t, claudeCalls); got != 2 {
		t.Fatalf("claude attempts = %d, want a fresh cap of its own, not codex's leftovers", got)
	}
	if want := "Agent codex spent its 2 attempts without finishing"; !strings.Contains(buf.String(), want) {
		t.Fatalf("output missing the exhaustion fall-through %q:\n%s", want, buf.String())
	}
	if !strings.Contains(buf.String(), providerCrashMessage) {
		t.Fatalf("output missing the provider's own reason for the fall-through:\n%s", buf.String())
	}
	if !strings.Contains(buf.String(), "trying next") {
		t.Fatalf("output never says the turn passed on:\n%s", buf.String())
	}
	assertTaskDone(t, env.execFixture(), "01-a")
}

// TestEveryAgentSpentFailsTheTaskAndStopsTheDrain is the other side of the same
// walk: with no agent left the ending is the one a spent cap always wrote — the
// task Failed, counted over every attempt every agent made — and the drain stops
// rather than handing the next task the same dead list.
func TestEveryAgentSpentFailsTheTaskAndStopsTheDrain(t *testing.T) {
	env := setupRunTaskSetFixture(t, "demo", []Task{
		{ID: "01-a", File: "01-a.md", Title: "A", Type: "AFK", Status: "open"},
		{ID: "02-b", File: "02-b.md", Title: "B", Type: "AFK", Status: "open"},
	})
	codexCalls := installCrashingCodexAgent(t, env.root, providerCrashMessage)
	claudeCalls := installCrashingClaudeAgent(t, env.root, "API Error: 529 Overloaded")

	var buf bytes.Buffer
	opts := env.runTaskSetOpts(true, "", &buf)
	opts.AgentPresets = []string{"codex", "claude"}
	opts.AgentExplicit = true
	opts.MaxTries = 2

	_, err := RunTaskSetWith(env.deps(), nil, nil, opts)
	assertExitCode(t, err, ExitOperational)
	if got := agentCalls(t, codexCalls); got != 2 {
		t.Fatalf("codex attempts = %d, want 2", got)
	}
	if got := agentCalls(t, claudeCalls); got != 2 {
		t.Fatalf("claude attempts = %d, want its own 2 after codex handed on", got)
	}
	// Four attempts is what the task actually had; the count is the task's, not
	// the last agent's share of it.
	assertTaskFailed(t, env.execFixture(), "01-a", 4)
	assertProgressContains(t, env.execFixture(), "FAILED", "API Error: 529 Overloaded")
	assertTaskStatus(t, env, "02-b", TaskOpen)
}

// TestEveryAgentTimingOutHandsTheTurnOn: a cap spent entirely on Task attempt
// timeouts bails out through its own branch, which returned out of the walk by
// the same mistake. The second agent is invoked, and only the exhausted list
// fails the task.
func TestEveryAgentTimingOutHandsTheTurnOn(t *testing.T) {
	env := setupRunTaskSetFixture(t, "demo", []Task{
		{ID: "01-a", File: "01-a.md", Title: "A", Type: "AFK", Status: "open"},
	})
	// Real subprocesses: the in-process fake never hangs, so a timeout kill needs
	// a shim that really sleeps past the deadline (ADR-0144).
	codexCalls := installHangingAgent(t, env.root, "codex", `if [ "$1" = login ]; then exit 0; fi`)
	claudeCalls := installHangingAgent(t, env.root, "claude", `if [ "$1" = auth ] && [ "$2" = status ]; then printf '{"loggedIn":true}\n'; exit 0; fi`)

	var buf bytes.Buffer
	opts := env.runTaskSetOpts(true, "", &buf)
	opts.AgentPresets = []string{"codex", "claude"}
	opts.AgentExplicit = true
	opts.MaxTries = 1
	opts.Timeout = 100 * time.Millisecond

	_, err := RunTaskSetWith(env.deps(), nil, nil, opts)
	assertExitCode(t, err, ExitOperational)
	if got := agentCalls(t, codexCalls); got != 1 {
		t.Fatalf("codex attempts = %d, want 1", got)
	}
	if got := agentCalls(t, claudeCalls); got != 1 {
		t.Fatalf("claude attempts = %d, want the turn handed on after codex timed out", got)
	}
	if want := "Agent codex spent its only attempt without finishing (last: timed out after 100ms)"; !strings.Contains(buf.String(), want) {
		t.Fatalf("output missing the timeout fall-through %q:\n%s", want, buf.String())
	}
	assertTaskFailed(t, env.execFixture(), "01-a", 2)
	assertProgressContains(t, env.execFixture(), "FAILED", "timed out")
}

// TestSpentCapFallThroughIsReportedOnTheSharedWalk: verify and review walk their
// lists past an exhausted retry loop already, but did it silently. Every Work
// group now reports the fall-through the same way, so the operator reads the same
// event whichever list is advancing. Driven through the Reviewer, which is the
// cheapest way into the walk both roles share.
func TestSpentCapFallThroughIsReportedOnTheSharedWalk(t *testing.T) {
	taskSetDir := t.TempDir()
	d, runner := reviewRunnerDeps(t,
		scriptedReviewRun{output: providerCrashMessage, exitCode: 1},
		scriptedReviewRun{output: claudeReviewStream("## Naming\\nAll good.")},
	)

	var out bytes.Buffer
	result, err := walkOverPresets(t, d, taskSetDir, &out, "codex", "claude")
	if err != nil {
		t.Fatalf("runAgentFallbackWalk: %v", err)
	}
	if !strings.Contains(result.Answer, "All good.") {
		t.Fatalf("answer = %q, want the next agent's document", result.Answer)
	}
	if runner.calls != 2 {
		t.Fatalf("agent invocations = %d, want the turn handed on after the first spent its cap", runner.calls)
	}
	if want := "Reviewer agent codex spent its only attempt without finishing"; !strings.Contains(out.String(), want) {
		t.Fatalf("output missing %q:\n%s", want, out.String())
	}
}

// installCrashingCodexAgent stubs a codex whose every attempt dies on the given
// provider diagnostic and exits 1, counting its invocations so a test can prove
// the whole cap was spent before the turn passed on.
func installCrashingCodexAgent(t *testing.T, root, message string) string {
	t.Helper()
	return installCountingAgentShim(t, root, "codex", `if [ "$1" = login ]; then exit 0; fi`,
		fmt.Sprintf("printf '%%s\\n' %s\nexit 1\n", shellQuote(message)))
}

// installCrashingClaudeAgent is installCrashingCodexAgent for the second entry in
// the list, so a walk can run out of agents with both having really been tried.
func installCrashingClaudeAgent(t *testing.T, root, message string) string {
	t.Helper()
	return installCountingAgentShim(t, root, "claude", claudeAuthStatusGuard,
		fmt.Sprintf("printf '%%s\\n' %s\nexit 1\n", shellQuote(message)))
}

// installFailsOnceThenFinishesClaudeAgent stubs an agent that needs two tries:
// its first attempt reports nothing usable and its second ticks the task's boxes
// and closes out. It is what proves the Task retry cap is per agent — a shared
// cap already spent by the previous preset would leave it no second try.
func installFailsOnceThenFinishesClaudeAgent(t *testing.T, root string) string {
	t.Helper()
	return installCountingAgentShim(t, root, "claude", claudeAuthStatusGuard, `if [ "$n" = 1 ]; then printf 'nothing usable yet\n'; exit 0; fi
`+tickTaskShellSnippet+`printf 'SUMMARY_START\nclaude done\nSUMMARY_END\nTASK_COMPLETE\n'
`)
}

// installHangingAgent stubs an agent that never answers, for the Task attempt
// timeout path: the attempt deadline SIGKILLs its process group.
func installHangingAgent(t *testing.T, root, preset, guard string) string {
	t.Helper()
	return installCountingAgentShim(t, root, preset, guard, "sleep 5\n")
}

const claudeAuthStatusGuard = `if [ "$1" = auth ] && [ "$2" = status ]; then printf '{"loggedIn":true}\n'; exit 0; fi`

// tickTaskShellSnippet ticks the acceptance boxes of the task named in the
// prompt, the way a real agent that did the work would leave the file.
const tickTaskShellSnippet = `TASK=$(cat "$(printf '%s' "$*" | sed -n 's|.*Read the file \([^ ]*\) in full:.*|\1|p' | head -1)" | sed -n 's|^.*You are implementing the task at: ||p' | head -1 | awk '{print $1}')
if [ -n "$TASK" ] && [ -f "$TASK" ]; then sed -i '' 's/- \[ \]/- [x]/g' "$TASK" 2>/dev/null || sed -i 's/- \[ \]/- [x]/g' "$TASK"; fi
`

// installCountingAgentShim installs a PATH stub for one preset that records each
// real invocation (availability probes excluded by the caller's guard) and then
// runs body, which may read `$n` for the invocation's own number. It returns the
// counter file agentCalls reads.
func installCountingAgentShim(t *testing.T, root, preset, guard, body string) string {
	t.Helper()
	// ADR-0145: PATH stub — callers stay serial deliberately.
	count := filepath.Join(root, ".agent-bin", preset+".calls")
	installAgentShim(t, root, preset, fmt.Sprintf(`#!/bin/sh
%[1]s
n=0
test -f %[2]q && n=$(cat %[2]q)
n=$((n + 1))
printf '%%s\n' "$n" > %[2]q
%[3]s`, guard, count, body))
	return count
}

func agentCalls(t *testing.T, countPath string) int {
	t.Helper()
	n := 0
	if _, err := fmt.Sscanf(strings.TrimSpace(readFileString(t, countPath)), "%d", &n); err != nil {
		t.Fatalf("read agent call count %s: %v", countPath, err)
	}
	return n
}

// assertTaskStatus pins one task's status, for the tasks a stopped drain must not
// have touched.
func assertTaskStatus(t *testing.T, env *runTaskSetFixture, taskID string, want TaskStatus) {
	t.Helper()
	m := LoadManifest(env.deps(), "demo", env.execFixture().demoManifest())
	for _, task := range m.Tasks {
		if task.ID == taskID {
			if task.Status != want {
				t.Fatalf("task %s status = %q, want %q", taskID, task.Status, want)
			}
			return
		}
	}
	t.Fatalf("task %s not found", taskID)
}
