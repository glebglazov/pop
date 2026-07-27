package tasks

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

// In-process fake agent runner (ADR-0144).
//
// The drain-orchestration family (TestRunTask* / TestRunTaskSet*) used to
// install #!/bin/sh agent shims via writeFakeAgent/writeSequentialFakeAgent and
// spawn them through RealCommandRunner. Every one of those spawns forked `sh`
// (plus a nested `sed`) per attempt, which dominated the tasks package wall
// time. Those shims only ever produced plain output — a SUMMARY/TASK_COMPLETE
// (or "incomplete") block, an exit code, and two filesystem side effects
// (append to a change file, tick the task's checkboxes). This fake reproduces
// exactly those observable bytes, exit code, and side effects in-process, so
// the drain behaves identically without a subprocess.
//
// The seam is opt-out, not opt-in: writeFakeAgent now returns an opaque token
// instead of a script path, and the test fixtures' default runner is
// fakeAwareRunner, which replays a token in-process and delegates every other
// invocation (real shim scripts, preset binaries like claude/codex/pi) to the
// real runner. A named smoke set keeps the genuinely real-shell behaviors
// honest on the real path (see realShimSmokeSet below); do NOT "fix" the fakes
// back to real shells.

// realShimSmokeSet names the deliberate real-subprocess smoke tests per
// ADR-0144, one per distinct real-shell behavior. A future reader should not
// convert these to the in-process fake, nor convert the fake family back to
// real shims:
//
//   - spawn: TestRunTaskSetDrainsMultipleAFKTasksInOrder — a real `sh` shim is
//     spawned per attempt and drives a two-task drain to completion.
//   - stream pumping: TestRunTaskStructuredAttemptWritesStream — a real `claude`
//     shim emits structured events pumped through the live writer and captured
//     to a stream file.
//   - timeout kill: TestRunTaskSetTimeoutPropagation — the shim sleeps past the
//     timeout and is SIGKILLed via the process group; the fake never hangs, so
//     this must spawn.
//   - non-zero exit: TestRunTaskSetFailedTaskStopsDrain — a real shim exits
//     non-zero and the drain records the failure.
//   - quota-signal parsing: TestRunTaskSetClaudeQuotaPauseRegistersRecoveryWaiter
//     — a real `claude` preset binary emits a quota-limit result line parsed by
//     the normalizer.
//
// The other structured-stream and agent-fallback tests (installClaudeStreamAgent,
// installClaudeHangingAgent, installAgentShim, installClaudeQuotaAgent) also stay
// on the real path for the same reason: they exercise real stream pumping, real
// SIGKILL escalation of a hanging agent, and quota-signal parsing — not plain
// drain orchestration. The hanging shims sleep in 50ms ticks until killed, so a
// short attempt deadline (not a multi-second sleep) bounds their cost.
//
// Slice 03 (ADR-0144) extends the naming to the remaining real-subprocess
// surfaces outside the drain family, each of which genuinely depends on real OS
// semantics the in-process fake cannot reproduce:
//
//   - attended exec / tty foreground handover: TestRunAttendedNonTerminalStdinPlainExec
//     and TestRunAttendedTTYNotStdinFd drive RealCommandRunner.RunAttended over a
//     real `true` binary — the whole point is the process-group/tty behavior.
//   - verify fall-through spawn: TestRunConfiguredVerifierFallsThroughMissingBinary
//     spawns a real `claude` on a controlled PATH so the missing-binary skip is
//     exercised end-to-end through a real spawn.
//   - timeout kill (retry pacing): TestRunTaskTimeoutRetriesInstantlyThenFailsAtCap,
//     TestRunTaskTimeoutSharesRetryBudget, TestRunTaskTimeoutCarriesContinueDigestForward,
//     and TestRunTaskTimeoutKillsProcessGroup hang a real agent past a short
//     deadline; the timeout path SIGKILLs the real process group and waits on the
//     real proc, which the never-hanging fake cannot drive.
//   - signal handling: TestRunTaskSignalLeavesTaskOpen, TestRunTaskSignalReleasesRuntimeLock,
//     and TestRunTaskSetInterruptionPropagation SIGTERM the live drain while a real
//     agent is running; the fake never installs a real process to interrupt.
//
// These pace themselves on short (sub-second) deadlines or a start-sentinel
// signal, not on the shims' nominal multi-second sleeps, so they no longer cost
// real seconds even though they spawn.
var realShimSmokeSet = []string{
	"TestRunTaskSetDrainsMultipleAFKTasksInOrder",
	"TestRunTaskStructuredAttemptWritesStream",
	"TestRunTaskSetTimeoutPropagation",
	"TestRunTaskSetFailedTaskStopsDrain",
	"TestRunTaskSetClaudeQuotaPauseRegistersRecoveryWaiter",
	"TestRunAttendedNonTerminalStdinPlainExec",
	"TestRunAttendedTTYNotStdinFd",
	"TestRunConfiguredVerifierFallsThroughMissingBinary",
	"TestRunTaskTimeoutRetriesInstantlyThenFailsAtCap",
	"TestRunTaskTimeoutSharesRetryBudget",
	"TestRunTaskTimeoutCarriesContinueDigestForward",
	"TestRunTaskTimeoutKillsProcessGroup",
	"TestRunTaskSignalLeavesTaskOpen",
	"TestRunTaskSignalReleasesRuntimeLock",
	"TestRunTaskSetInterruptionPropagation",
}

const fakeAgentTokenPrefix = "__pop_fake_agent_"

// fakeAgentBehavior is what a writeFakeAgent/writeSequentialFakeAgent shim used
// to encode. Behaviors live in a package-global registry keyed by token; go
// test runs these package tests serially (t.Parallel is deferred per ADR-0144),
// and each behavior is driven by a single drain at a time.
type fakeAgentBehavior struct {
	cfg      fakeAgentConfig // used when steps == nil && attempts == nil
	steps    []fakeAgentStep // non-nil for the sequential agent
	attempts []attemptScript // non-nil for the per-attempt scripted agent
	calls    int             // advances across attempts for the scripted agents
}

var (
	fakeAgentMu       sync.Mutex
	fakeAgentNextID   int
	fakeAgentRegistry = map[string]*fakeAgentBehavior{}
)

func registerFakeAgent(t *testing.T, b *fakeAgentBehavior) string {
	t.Helper()
	fakeAgentMu.Lock()
	fakeAgentNextID++
	token := fmt.Sprintf("%s%d__", fakeAgentTokenPrefix, fakeAgentNextID)
	fakeAgentRegistry[token] = b
	fakeAgentMu.Unlock()
	t.Cleanup(func() {
		fakeAgentMu.Lock()
		delete(fakeAgentRegistry, token)
		fakeAgentMu.Unlock()
	})
	return token
}

// lookupFakeAgent finds the registered behavior an invocation encodes, if any.
// Custom-command invocations wrap the agent command as
// `sh -c '<token> "$@"' task-agent <prompt>`, so the token is the first field
// of one of the args and the prompt is the final arg.
func lookupFakeAgent(args []string) (*fakeAgentBehavior, string, bool) {
	fakeAgentMu.Lock()
	defer fakeAgentMu.Unlock()
	if len(fakeAgentRegistry) == 0 {
		return nil, "", false
	}
	for _, arg := range args {
		fields := strings.Fields(arg)
		if len(fields) == 0 {
			continue
		}
		if b, ok := fakeAgentRegistry[fields[0]]; ok {
			prompt := ""
			if len(args) > 0 {
				prompt = args[len(args)-1]
			}
			return b, prompt, true
		}
	}
	return nil, "", false
}

// fakeAwareRunner is the default CommandRunner in the tasks test fixtures. A
// registered fake-agent token is replayed in-process; everything else (real
// shim scripts, preset binaries) is delegated to the real runner so the smoke
// set and the structured-output/quota tests keep spawning real subprocesses.
type fakeAwareRunner struct{}

func (fakeAwareRunner) Run(ctx context.Context, dir string, stdout, stderr io.Writer, name string, args ...string) (int, error) {
	if b, prompt, ok := lookupFakeAgent(args); ok {
		return b.play(dir, stdout, prompt), nil
	}
	return RealCommandRunner{}.Run(ctx, dir, stdout, stderr, name, args...)
}

func (fakeAwareRunner) Start(ctx context.Context, dir string, stdout, stderr io.Writer, name string, args ...string) (*ManagedProcess, error) {
	if b, prompt, ok := lookupFakeAgent(args); ok {
		exit := b.play(dir, stdout, prompt)
		proc := &ManagedProcess{done: make(chan waitResult, 1)}
		proc.done <- waitResult{exitCode: exit}
		return proc, nil
	}
	return RealCommandRunner{}.Start(ctx, dir, stdout, stderr, name, args...)
}

func (fakeAwareRunner) RunAttended(ctx context.Context, dir string, stdin io.Reader, stdout, stderr io.Writer, name string, args ...string) (int, error) {
	return RealCommandRunner{}.RunAttended(ctx, dir, stdin, stdout, stderr, name, args...)
}

// play reproduces the shim's observable effects: the change-file append and
// checkbox tick side effects, the plain SUMMARY/TASK_COMPLETE (or "incomplete")
// output, and the exit code. It mirrors writeFakeAgent/writeSequentialFakeAgent
// byte for byte.
func (b *fakeAgentBehavior) play(dir string, stdout io.Writer, prompt string) int {
	taskPath := parseFakeAgentTaskPath(prompt)

	if b.attempts != nil {
		// Mirror writeRealShimAttemptAgent's case body for the current attempt:
		// advance the counter first (so a would-be timeout kill still records the
		// attempt as started), then replay the step's side effects and output. An
		// out-of-range attempt reproduces the shim's `*)` fallthrough.
		n := b.calls
		b.calls++
		if n >= len(b.attempts) {
			fmt.Fprintln(stdout, "unexpected attempt")
			return 2
		}
		s := b.attempts[n]
		if s.changeFile != "" {
			appendFakeAgentChange(dir, s.changeFile, s.changeData)
		}
		if s.checkTask {
			tickTaskFile(taskPath)
		}
		summary := s.summary
		if summary == "" {
			summary = "attempt complete"
		}
		if s.skipSentinel {
			fmt.Fprintln(stdout, "incomplete")
		} else {
			fmt.Fprintf(stdout, "SUMMARY_START\n%s\nSUMMARY_END\nTASK_COMPLETE\n", summary)
		}
		return 0
	}

	if b.steps != nil {
		// The sequential shim ticks the task unconditionally, then plays the
		// step for the current attempt.
		tickTaskFile(taskPath)
		i := b.calls
		b.calls++
		var step fakeAgentStep
		if i < len(b.steps) {
			step = b.steps[i]
		}
		summary := step.summary
		if summary == "" {
			summary = "step"
		}
		fmt.Fprintf(stdout, "SUMMARY_START\n%s\nSUMMARY_END\nTASK_COMPLETE\n", summary)
		return step.exitCode
	}

	cfg := b.cfg
	if cfg.changeFile != "" {
		appendFakeAgentChange(dir, cfg.changeFile, cfg.changeData)
	}
	// cfg.sleepFor is intentionally not honored: the fake never hangs, so any
	// test that needs a timeout kill stays on the real shim path.
	if cfg.checkTask {
		tickTaskFile(taskPath)
	}
	summary := cfg.summary
	if summary == "" {
		summary = "work complete"
	}
	if !cfg.skipSentinel {
		fmt.Fprintf(stdout, "SUMMARY_START\n%s\nSUMMARY_END\nTASK_COMPLETE\n", summary)
	} else {
		fmt.Fprintln(stdout, "incomplete")
	}
	return cfg.exitCode
}

// parseFakeAgentTaskPath extracts the task path from the agent prompt, the same
// way the shim did with `sed -n 's|^You are implementing the task at: ||p'`.
func parseFakeAgentTaskPath(prompt string) string {
	const marker = "You are implementing the task at: "
	for _, line := range strings.Split(prompt, "\n") {
		if strings.HasPrefix(line, marker) {
			return strings.TrimPrefix(line, marker)
		}
	}
	return ""
}

func appendFakeAgentChange(dir, changeFile, data string) {
	path := changeFile
	if !filepath.IsAbs(path) {
		path = filepath.Join(dir, changeFile)
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return
	}
	defer f.Close()
	_, _ = f.WriteString(data)
}

func tickTaskFile(taskPath string) {
	if taskPath == "" {
		return
	}
	data, err := os.ReadFile(taskPath)
	if err != nil {
		return
	}
	ticked := strings.ReplaceAll(string(data), "- [ ]", "- [x]")
	_ = os.WriteFile(taskPath, []byte(ticked), 0o644)
}

// writeFakeAgent registers an in-process fake agent (ADR-0144) and returns an
// opaque token to pass as the agent command. It replaces the #!/bin/sh shim the
// old helper installed; the fixtures' fakeAwareRunner replays the token without
// spawning a subprocess. Use writeRealShimAgent for the smoke set that must run
// a real shell.
func writeFakeAgent(t *testing.T, _ string, cfg fakeAgentConfig) string {
	t.Helper()
	return registerFakeAgent(t, &fakeAgentBehavior{cfg: cfg})
}

// writeSequentialFakeAgent is writeFakeAgent for a scripted sequence of
// attempts: each invocation plays the next step's summary and exit code.
func writeSequentialFakeAgent(t *testing.T, _ string, steps []fakeAgentStep) string {
	t.Helper()
	return registerFakeAgent(t, &fakeAgentBehavior{steps: append([]fakeAgentStep(nil), steps...)})
}

// writeAttemptAgent registers an in-process fake (ADR-0144) that replays a
// per-attempt attemptScript sequence, reproducing writeRealShimAttemptAgent's
// observable effects (change-file append, conditional checkbox tick, summary /
// "incomplete" output, exit 0, and the out-of-range "unexpected attempt"
// fallthrough) without spawning a shell. A script that needs the agent to hang
// (sleep > 0, for a timeout kill) cannot be faked — those tests keep the real
// shim via writeRealShimAttemptAgent and live in realShimSmokeSet.
func writeAttemptAgent(t *testing.T, _ string, scripts []attemptScript) string {
	t.Helper()
	for i, s := range scripts {
		if s.sleep > 0 {
			t.Fatalf("attempt %d sets sleep=%s; the in-process fake never hangs — use writeRealShimAttemptAgent", i+1, s.sleep)
		}
	}
	return registerFakeAgent(t, &fakeAgentBehavior{attempts: append([]attemptScript(nil), scripts...)})
}
