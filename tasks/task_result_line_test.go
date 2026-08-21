package tasks

import (
	"bytes"
	"strings"
	"testing"
	"time"
)

// The Task result line is the drain's one word per task. The runs below drive
// real drains to each ending and read the lines back, because the point of the
// line is that a human scrolling a finished drain can see how every task ended
// without reconstructing it from attempt narration.

// TestDrainResultLineOnDone: a completed task gets one green line and keeps the
// commit detail beneath it, and the old success-only completion line is gone
// rather than doubled.
func TestDrainResultLineOnDone(t *testing.T) {
	env := setupRunTaskSetFixture(t, "demo", []Task{
		{ID: "01-a", File: "01-a.md", Title: "A", Type: "AFK", Status: "open"},
	})
	agent := writeFakeAgent(t, env.root, fakeAgentConfig{checkTask: true, changeFile: "impl.txt", changeData: "work", summary: "ok"})

	var buf bytes.Buffer
	if _, err := RunTaskSetWith(env.deps(), nil, nil, env.runTaskSetOpts(true, agent, &buf)); err != nil {
		t.Fatalf("RunTaskSetWith: %v", err)
	}
	out := buf.String()
	if n := strings.Count(out, "✓ demo/01-a done"); n != 1 {
		t.Fatalf("result line printed %d times, want once:\n%s", n, out)
	}
	if strings.Contains(out, "✓ Completed demo/01-a") {
		t.Fatalf("the absorbed completion line is still printed:\n%s", out)
	}
	line := strings.Index(out, "✓ demo/01-a done")
	detail := strings.Index(out, "Implementation commit:")
	if detail == -1 || detail < line {
		t.Fatalf("commit detail is not beneath the result line:\n%s", out)
	}
}

// TestDrainResultLineOnFailed: a task that spends every retry on clean exits
// that miss the contract is the task's own failure, and reads as failed.
func TestDrainResultLineOnFailed(t *testing.T) {
	env := setupRunTaskSetFixture(t, "demo", []Task{
		{ID: "01-a", File: "01-a.md", Title: "A", Type: "AFK", Status: "open"},
	})
	agent := writeFakeAgent(t, env.root, fakeAgentConfig{skipSentinel: true})

	var buf bytes.Buffer
	opts := env.runTaskSetOpts(true, agent, &buf)
	opts.MaxTries, opts.MaxTriesExplicit = 1, true

	_, err := RunTaskSetWith(env.deps(), nil, nil, opts)
	assertExitCode(t, err, ExitOperational)
	out := buf.String()
	if n := strings.Count(out, "✗ demo/01-a failed"); n != 1 {
		t.Fatalf("failed result line printed %d times, want once:\n%s", n, out)
	}
	assertTaskStatus(t, env, "01-a", TaskFailed)
}

// TestDrainResultLineOnOutOfAgents: every agent dying on the provider's side
// leaves the task Open, and the line says so rather than blaming the task.
func TestDrainResultLineOnOutOfAgents(t *testing.T) {
	env := setupRunTaskSetFixture(t, "demo", []Task{
		{ID: "01-a", File: "01-a.md", Title: "A", Type: "AFK", Status: "open"},
	})
	agent := writeFakeAgent(t, env.root, fakeAgentConfig{skipSentinel: true, exitCode: 1})

	var buf bytes.Buffer
	opts := env.runTaskSetOpts(true, agent, &buf)
	opts.MaxTries, opts.MaxTriesExplicit = 1, true

	_, err := RunTaskSetWith(env.deps(), nil, nil, opts)
	assertExitCode(t, err, ExitOperational)
	out := buf.String()
	if n := strings.Count(out, "✗ demo/01-a out of agents (left open)"); n != 1 {
		t.Fatalf("out-of-agents result line printed %d times, want once:\n%s", n, out)
	}
	if strings.Contains(out, "demo/01-a failed") {
		t.Fatalf("a task left open must not read as failed:\n%s", out)
	}
	assertTaskStatus(t, env, "01-a", TaskOpen)
}

// TestDrainResultLineOnQuotaPause: the pause line names the preset and is said
// before the recovery wait, which is what makes it useful — the wait can outlast
// the operator's attention.
func TestDrainResultLineOnQuotaPause(t *testing.T) {
	env := setupRunTaskSetFixture(t, "demo", []Task{
		{ID: "01-a", File: "01-a.md", Title: "A", Type: "AFK", Status: "open"},
	})
	installClaudeQuotaAgent(t, env.root)

	var buf bytes.Buffer
	opts := env.runTaskSetOpts(true, "", &buf)
	opts.AgentPreset = "claude"
	d := env.deps()

	done := make(chan struct{})
	go func() {
		defer close(done)
		_, _ = RunTaskSetWith(d, nil, nil, opts)
	}()
	// The line is printed before the waiter is registered, so a registered
	// waiter means the buffer already holds it.
	var registered bool
	for i := 0; i < 50 && !registered; i++ {
		time.Sleep(20 * time.Millisecond)
		waiter, err := GetRecoveryWaiter(d, "demo")
		if err != nil {
			t.Fatalf("get recovery waiter: %v", err)
		}
		registered = waiter != nil
	}
	if !registered {
		t.Fatal("drain never parked on a quota pause")
	}
	if err := DeregisterRecoveryWaiter(d, "demo"); err != nil {
		t.Fatalf("deregister recovery waiter: %v", err)
	}
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("drain did not exit after its waiter was deregistered")
	}
	if want := "◌ demo/01-a quota-paused (claude)"; !strings.Contains(buf.String(), want) {
		t.Fatalf("output missing %q:\n%s", want, buf.String())
	}
}

// TestDrainResultLineSkipsSetLevelTerminals: Blocked and Deferred are the set's
// endings, not a task's — the tasks they name never ran, so they get no line.
func TestDrainResultLineSkipsSetLevelTerminals(t *testing.T) {
	env := setupRunTaskSetFixture(t, "demo", []Task{
		{ID: "01-a", File: "01-a.md", Title: "A", Type: "AFK", Status: "open"},
		{ID: "02-hitl", File: "02-hitl.md", Title: "Review", Type: "HITL", Status: "open"},
		{ID: "03-c", File: "03-c.md", Title: "C", Type: "AFK", Status: "open", BlockedBy: []string{"02-hitl"}},
	})
	agent := writeFakeAgent(t, env.root, fakeAgentConfig{checkTask: true, summary: "ok"})

	var buf bytes.Buffer
	// The set stops on its HITL, which leaves 03-c blocked behind it.
	_, err := RunTaskSetWith(env.deps(), nil, nil, env.runTaskSetOpts(true, agent, &buf))
	assertExitCode(t, err, ExitNoRunnable)
	out := buf.String()
	if !strings.Contains(out, "✓ demo/01-a done") {
		t.Fatalf("the task that ran has no result line:\n%s", out)
	}
	for _, forbidden := range []string{"demo/02-hitl done", "demo/02-hitl failed", "demo/03-c done", "demo/03-c failed", "demo/03-c interrupted"} {
		if strings.Contains(out, forbidden) {
			t.Fatalf("a set-level terminal produced a Task result line (%q):\n%s", forbidden, out)
		}
	}
}

// TestTaskResultLineFormsAndColor pins the five forms and the styling rule: the
// line always prints, and only a colored writer carries escapes.
func TestTaskResultLineFormsAndColor(t *testing.T) {
	for _, tc := range []struct {
		ending taskEnding
		preset string
		want   string
		style  string
	}{
		{taskEndingDone, "", "✓ demo/01-a done", ansiGreen},
		{taskEndingFailed, "", "✗ demo/01-a failed", ansiRed},
		{taskEndingOutOfAgents, "", "✗ demo/01-a out of agents (left open)", ansiRed},
		{taskEndingQuotaPaused, "claude", "◌ demo/01-a quota-paused (claude)", ansiYellow},
		{taskEndingInterrupted, "", "◌ demo/01-a interrupted", ansiYellow},
	} {
		var plain, colored bytes.Buffer
		renderTaskResultLine(&output{Writer: &plain}, "demo", "01-a", tc.ending, tc.preset)
		renderTaskResultLine(&output{Writer: &colored, color: true}, "demo", "01-a", tc.ending, tc.preset)
		if got := strings.TrimRight(plain.String(), "\n"); got != tc.want {
			t.Fatalf("plain line = %q, want %q", got, tc.want)
		}
		if got, want := colored.String(), tc.style+tc.want+ansiReset+"\n"; got != want {
			t.Fatalf("colored line = %q, want %q", got, want)
		}
	}

	t.Setenv("NO_COLOR", "1")
	if colorEnabled(true) {
		t.Fatal("NO_COLOR left color enabled on a terminal")
	}
}

// TestTaskEndingForExecError: an interrupt and a provider-collapsed walk both
// leave the task Open, and each says its own thing; everything else is failed.
func TestTaskEndingForExecError(t *testing.T) {
	sel := &Selection{TaskSetID: "demo", TaskID: "01-a"}
	cases := []struct {
		name string
		err  error
		want taskEnding
	}{
		{"interrupt", taskExitErr(sel, ExitInterrupted, "interrupted"), taskEndingInterrupted},
		{"provider collapse", &exhaustedWalkError{fault: faultProvider, preset: "claude", err: exitErr(ExitOperational, "left open")}, taskEndingOutOfAgents},
		{"contract miss", &exhaustedWalkError{fault: faultContract, preset: "claude", err: exitErr(ExitOperational, "failed")}, taskEndingFailed},
		{"plain error", exitErr(ExitOperational, "boom"), taskEndingFailed},
	}
	for _, tc := range cases {
		if got := taskEndingForExecError(tc.err); got != tc.want {
			t.Fatalf("%s ending = %v, want %v", tc.name, got, tc.want)
		}
	}
}
