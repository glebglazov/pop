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
// installAgentShim, installClaudeQuotaAgent) also stay on the real path for the
// same reason: they exercise real stream pumping and quota-signal parsing, not
// plain drain orchestration.
var realShimSmokeSet = []string{
	"TestRunTaskSetDrainsMultipleAFKTasksInOrder",
	"TestRunTaskStructuredAttemptWritesStream",
	"TestRunTaskSetTimeoutPropagation",
	"TestRunTaskSetFailedTaskStopsDrain",
	"TestRunTaskSetClaudeQuotaPauseRegistersRecoveryWaiter",
}

const fakeAgentTokenPrefix = "__pop_fake_agent_"

// fakeAgentBehavior is what a writeFakeAgent/writeSequentialFakeAgent shim used
// to encode. Behaviors live in a package-global registry keyed by token; go
// test runs these package tests serially (t.Parallel is deferred per ADR-0144),
// and each behavior is driven by a single drain at a time.
type fakeAgentBehavior struct {
	cfg   fakeAgentConfig // used when steps == nil
	steps []fakeAgentStep // non-nil for the sequential agent
	calls int             // advances across attempts for the sequential agent
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
