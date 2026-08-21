package tasks

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"github.com/glebglazov/pop/internal/deps"
)

func TestRunTaskRetriesPreserveEdits(t *testing.T) {
	env := setupExecutorFixture(t, false)
	agent := writeAttemptAgent(t, env.root, []attemptScript{
		{changeFile: "impl.txt", changeData: "partial\n", checkTask: true, skipSentinel: true},
		{changeFile: "impl.txt", changeData: "more\n", checkTask: true, summary: "finished on retry"},
	})

	opts := env.runOpts(true, agent)
	opts.MaxTries = 3
	var buf bytes.Buffer
	opts.Output = &buf

	_, err := RunTaskWith(env.deps(), nil, nil, opts)
	if err != nil {
		t.Fatalf("run failed: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(env.root, "impl.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "partial") || !strings.Contains(string(data), "more") {
		t.Fatalf("partial edits not preserved: %q", data)
	}
	if strings.Count(buf.String(), "Retrying with preserved changes") != 1 {
		t.Fatalf("expected one retry notice:\n%s", buf.String())
	}
	assertTaskDone(t, env, "01-a")
}

// TestAttemptScriptCombinesRawOutputAndExitCode proves the crossing ADR-0231
// names unwritable before this slice: one attempt in a multi-attempt sequence
// printing a provider's own error prose and exiting non-zero. The raw text
// must reach assessAttempt the way a real agent's stdout would, and the
// declared exit code must be the one the attempt loop sees.
func TestAttemptScriptCombinesRawOutputAndExitCode(t *testing.T) {
	env := setupExecutorFixture(t, false)
	const providerErr = "Error: rate_limit_exceeded: please retry after 30s"
	agent := writeAttemptAgent(t, env.root, []attemptScript{
		{rawOutput: providerErr, exitCode: 1},
	})

	opts := env.runOpts(true, agent)
	opts.MaxTries = 1
	var buf bytes.Buffer
	opts.Output = &buf

	_, err := RunTaskWith(env.deps(), nil, nil, opts)
	assertExitCode(t, err, ExitOperational)
	if !strings.Contains(buf.String(), providerErr) {
		t.Fatalf("captured attempt stream missing declared provider text:\n%s", buf.String())
	}
	// The attempt loop reports the provider's own sentence for a non-zero exit
	// (ADR-0231), so the declared exit code shows up as the text that came with
	// it rather than as "status 1".
	if !strings.Contains(err.Error(), providerErr) {
		t.Fatalf("err = %v, want it to carry the declared provider text", err)
	}
	assertTaskFailed(t, env, "01-a", 1)
}

// TestFailedAttemptsReportProviderDiagnostic drives two of the real wordings
// observed on this machine through a two-attempt run and follows the recorded
// reason to every surface it feeds: the failure the human sees, the Failed line
// in the progress record, and the digest handed to the attempt after it
// (ADR-0231).
func TestFailedAttemptsReportProviderDiagnostic(t *testing.T) {
	env := setupExecutorFixture(t, false)
	const (
		asleep     = "API Error: Your computer went to sleep mid-response"
		overloaded = "API Error: 529 Overloaded"
	)
	agent := writeAttemptAgent(t, env.root, []attemptScript{
		{changeFile: "impl.txt", changeData: "half done\n", rawOutput: "reading the task\n" + asleep + "\n", exitCode: 1},
		{rawOutput: "picking it back up\n" + overloaded + "\n", exitCode: 1},
	})
	opts := env.runOpts(true, agent)
	opts.MaxTries = 2
	opts.Output = io.Discard

	_, err := RunTaskWith(env.deps(), nil, nil, opts)
	assertExitCode(t, err, ExitOperational)
	if !strings.Contains(err.Error(), overloaded) {
		t.Fatalf("err = %v, want the last attempt's provider diagnostic", err)
	}
	if strings.Contains(err.Error(), "agent exited with status") {
		t.Fatalf("err = %v, want no exit-code phrasing when the provider spoke", err)
	}
	assertTaskFailed(t, env, "01-a", 2)
	assertProgressContains(t, env, "FAILED", overloaded)
}

// installClaudeCrashingAgent puts a fake `claude` on PATH that opens its
// stream-json normally and then dies the way the corpus records: a plain-text
// provider diagnostic as its last line, exit 1, on every attempt. Structured
// output is what makes the run recorded, which is what the prior-attempt digest
// is built from.
func installClaudeCrashingAgent(t *testing.T, root, diagnostic string) {
	t.Helper()
	// ADR-0145: PATH stub — callers stay serial deliberately.
	dir := filepath.Join(root, ".agent-bin")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	script := "#!/bin/sh\n" +
		"if [ \"$1\" = auth ] && [ \"$2\" = status ]; then\n" +
		"  printf '{\"loggedIn\":true}\\n'\n" +
		"  exit 0\n" +
		"fi\n" +
		`printf '%s\n' '{"type":"system","subtype":"init"}'` + "\n" +
		"printf '%s\\n' " + shellQuote(diagnostic) + "\n" +
		"exit 1\n"
	writeFile(t, filepath.Join(dir, "claude"), script)
	if err := os.Chmod(filepath.Join(dir, "claude"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
}

// TestFailedAttemptDiagnosticReachesNextAttemptDigest follows the recorded
// reason into the one surface that is not a report to a human: the digest the
// next attempt is handed. Without it a retry — on this agent or the next one in
// the fallback list — is told only that something exited (ADR-0231).
func TestFailedAttemptDiagnosticReachesNextAttemptDigest(t *testing.T) {
	env := setupExecutorFixture(t, false)
	const capped = "You hit your spend cap set by the owner of your workspace."
	installClaudeCrashingAgent(t, env.root, capped)

	opts := env.runOpts(true, "")
	opts.AgentPreset = "claude"
	opts.MaxTries = 2
	opts.Output = io.Discard

	_, err := RunTaskWith(env.deps(), nil, nil, opts)
	assertExitCode(t, err, ExitOperational)
	if !strings.Contains(err.Error(), capped) {
		t.Fatalf("err = %v, want the provider's own sentence", err)
	}
	digest := buildPriorAttemptDigest(env.deps(), env.demoDir(), "01-a.md")
	if !strings.Contains(digest, capped) {
		t.Fatalf("prior-attempt digest missing the provider diagnostic:\n%s", digest)
	}
}

// A crash that printed nothing usable still has to say something: the exit code
// is all pop knows, and a reasonless failure is worse than a thin one.
func TestFailedAttemptWithoutDiagnosticNamesExitCode(t *testing.T) {
	env := setupExecutorFixture(t, false)
	agent := writeAttemptAgent(t, env.root, []attemptScript{
		{rawOutput: "   \n", exitCode: 1},
	})

	opts := env.runOpts(true, agent)
	opts.MaxTries = 1
	opts.Output = io.Discard

	_, err := RunTaskWith(env.deps(), nil, nil, opts)
	assertExitCode(t, err, ExitOperational)
	if !strings.Contains(err.Error(), "agent exited with status 1") {
		t.Fatalf("err = %v, want the exit-code fallback", err)
	}
	assertProgressContains(t, env, "FAILED", "agent exited with status 1")
}

func TestRunTaskExhaustedRetriesMarkFailed(t *testing.T) {
	env := setupExecutorFixture(t, false)
	agent := writeFakeAgent(t, env.root, fakeAgentConfig{
		changeFile:   "impl.txt",
		changeData:   "left behind\n",
		checkTask:    true,
		skipSentinel: true,
	})

	opts := env.runOpts(true, agent)
	opts.MaxTries = 2
	_, err := RunTaskWith(env.deps(), nil, nil, opts)
	assertExitCode(t, err, ExitOperational)
	assertTaskFailed(t, env, "01-a", 2)
	assertProgressContains(t, env, "FAILED", "failed after 2 attempts")
	if _, err := os.Stat(filepath.Join(env.root, "impl.txt")); err != nil {
		t.Fatal("partial runtime edits should be preserved")
	}
}

func TestRunTaskClaudeQuotaPauseLeavesTaskOpenWithoutRetry(t *testing.T) {
	env := setupExecutorFixture(t, false)
	counterPath := installClaudeQuotaAgent(t, env.root)

	opts := env.runOpts(true, "")
	opts.AgentPreset = "claude"
	opts.MaxTries = 3
	var buf bytes.Buffer
	opts.Output = &buf

	result, err := RunTaskWith(env.deps(), nil, nil, opts)
	if err != nil {
		t.Fatal(err)
	}
	if !result.QuotaPaused || !strings.Contains(result.PauseReason, "weekly limit") {
		t.Fatalf("result = %#v", result)
	}
	assertTaskOpen(t, env, "01-a")
	if got := strings.TrimSpace(string(mustReadFile(t, counterPath))); got != "1" {
		t.Fatalf("started attempts = %q, want 1", got)
	}
	if _, err := os.Stat(filepath.Join(env.demoDir(), "progress.txt")); !os.IsNotExist(err) {
		t.Fatalf("quota pause wrote progress: %v", err)
	}
	if strings.Contains(buf.String(), "{\"type\"") {
		t.Fatalf("quota pause rendered raw JSONL:\n%s", buf.String())
	}
	if got := strings.Count(buf.String(), "You've hit your weekly limit"); got != 1 {
		t.Fatalf("quota reason rendered %d times, want 1:\n%s", got, buf.String())
	}
}

func TestRunTaskConfigurableMaxTries(t *testing.T) {
	env := setupExecutorFixture(t, false)
	var calls int32
	runner := &countingRunner{t: t, calls: &calls, exitCode: 1}
	d := env.deps()
	d.Runner = runner

	opts := env.runOpts(true, "ignored")
	opts.MaxTries = 5
	opts.AgentCmd = writeFakeAgent(t, env.root, fakeAgentConfig{exitCode: 1})

	_, err := RunTaskWith(d, nil, nil, opts)
	assertExitCode(t, err, ExitOperational)
	if got := atomic.LoadInt32(&calls); got != 5 {
		t.Fatalf("started attempts = %d, want 5", got)
	}
	assertTaskFailed(t, env, "01-a", 5)
}

func installClaudeQuotaAgent(t *testing.T, root string) string {
	t.Helper()
	// ADR-0145: PATH stub — callers stay serial deliberately.
	dir := filepath.Join(root, ".agent-bin")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	counterPath := filepath.Join(dir, "claude.count")
	script := "#!/bin/sh\n" +
		"if [ \"$1\" = auth ] && [ \"$2\" = status ]; then\n" +
		"  printf '{\"loggedIn\":true}\\n'\n" +
		"  exit 0\n" +
		"fi\n" +
		"COUNT=0\n" +
		"if [ -f " + counterPath + " ]; then COUNT=$(cat " + counterPath + "); fi\n" +
		"echo $((COUNT + 1)) > " + counterPath + "\n" +
		"printf '%s\\n' '{\"type\":\"result\",\"subtype\":\"error_during_execution\",\"result\":\"You'\"'\"'ve hit your weekly limit · resets Mon 12:00am\"}'\n"
	writeFile(t, filepath.Join(dir, "claude"), script)
	if err := os.Chmod(filepath.Join(dir, "claude"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	return counterPath
}

func mustReadFile(t *testing.T, path string) []byte {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func TestRunTaskTimeoutRetriesInstantlyThenFailsAtCap(t *testing.T) {
	env := setupExecutorFixture(t, false)
	agent := writeSlowAgent(t, env.root, 2*time.Second)

	opts := env.runOpts(true, agent)
	opts.MaxTries = 3
	opts.Timeout = 100 * time.Millisecond
	var buf bytes.Buffer
	opts.Output = &buf
	start := time.Now()
	_, err := RunTaskWith(env.deps(), nil, nil, opts)
	elapsed := time.Since(start)
	assertExitCode(t, err, ExitOperational)
	// A timeout on a non-final attempt retries with zero delay, so three 100ms
	// timeouts finish well under the 2s the slow agent would take to complete.
	if elapsed > 2*time.Second {
		t.Fatalf("timeout retries took %s, want instant retries", elapsed)
	}
	if !strings.Contains(err.Error(), "timed out after 100ms on attempt 3") {
		t.Fatalf("err = %v", err)
	}
	if !strings.Contains(buf.String(), "✗ Attempt 1/3 timed out after 100ms") {
		t.Fatalf("missing timeout failure line:\n%s", buf.String())
	}
	if got := strings.Count(buf.String(), "Retrying instantly with preserved changes"); got != 2 {
		t.Fatalf("instant retry notices = %d, want 2:\n%s", got, buf.String())
	}
	// The cap is exhausted by timeouts: task Failed with a Failed progress record
	// and the drain stops at the Failed gate.
	assertTaskFailed(t, env, "01-a", 3)
	assertProgressContains(t, env, "FAILED", "timed out")
}

// One timeout plus two assessment failures share the same max_tries budget: the
// timeout counts as one attempt, so the task Fails at the default cap of 3.
func TestRunTaskTimeoutSharesRetryBudget(t *testing.T) {
	env := setupExecutorFixture(t, false)
	// Real-subprocess smoke: timeout kill in retry pacing (see realShimSmokeSet).
	// Attempt 1 hangs on a real `sleep` so the deadline SIGKILLs it; the
	// never-hanging in-process fake cannot drive this.
	agent := writeRealShimAttemptAgent(t, env.root, []attemptScript{
		{sleep: 3 * time.Second}, // attempt 1: times out
		{changeFile: "impl.txt", changeData: "a\n", checkTask: true, skipSentinel: true}, // attempt 2: assessment failure
		{changeFile: "impl.txt", changeData: "b\n", checkTask: true, skipSentinel: true}, // attempt 3: assessment failure → Failed
	})

	opts := env.runOpts(true, agent)
	opts.MaxTries = 3
	opts.Timeout = 700 * time.Millisecond
	var buf bytes.Buffer
	opts.Output = &buf
	_, err := RunTaskWith(env.deps(), nil, nil, opts)
	assertExitCode(t, err, ExitOperational)
	if !strings.Contains(err.Error(), "failed after 3 attempts") {
		t.Fatalf("err = %v", err)
	}
	if !strings.Contains(buf.String(), "✗ Attempt 1/3 timed out after 700ms") {
		t.Fatalf("missing timeout line for attempt 1:\n%s", buf.String())
	}
	// The timeout consumed one slot, leaving two assessment-failure attempts
	// before the shared cap is hit.
	assertTaskFailed(t, env, "01-a", 3)
}

func TestRunTaskTimeoutCarriesContinueDigestForward(t *testing.T) {
	env := setupExecutorFixture(t, false)
	installClaudeHangingAgent(t, env.root, false)

	opts := env.runOpts(true, "")
	opts.AgentPreset = "claude"
	opts.MaxTries = 2
	opts.Timeout = 500 * time.Millisecond
	opts.Output = io.Discard
	_, err := RunTaskWith(env.deps(), nil, nil, opts)
	assertExitCode(t, err, ExitOperational)

	// The prior-attempt "continue" digest is built from the persisted timed-out
	// stream, so a retry carries the ADR-0040 continue lesson forward.
	digest := buildPriorAttemptDigest(env.deps(), env.demoDir(), "01-a.md")
	if !strings.Contains(digest, lessonContinue) {
		t.Fatalf("timeout digest missing continue lesson:\n%s", digest)
	}
}

func TestRunTaskTimeoutKillsProcessGroup(t *testing.T) {
	env := setupExecutorFixture(t, false)
	agent := writeProcessGroupAgent(t, env.root, 5*time.Second)

	opts := env.runOpts(true, agent)
	// A single attempt keeps this focused on the process-group kill, not the
	// timeout retry policy exercised elsewhere.
	opts.MaxTries = 1
	opts.Timeout = 200 * time.Millisecond
	_, err := RunTaskWith(env.deps(), nil, nil, opts)
	assertExitCode(t, err, ExitOperational)
	if _, err := os.Stat(filepath.Join(env.root, ".child-alive")); !os.IsNotExist(err) {
		t.Fatal("child process should have been terminated with the group")
	}
}

func TestRunTaskSignalLeavesTaskOpen(t *testing.T) {
	env := setupExecutorFixture(t, false)
	agent := writeSlowAgent(t, env.root, 10*time.Second)

	opts := env.runOpts(true, agent)
	opts.Timeout = time.Minute
	signalOwnPidWhenAgentStarts(t, env.root)

	_, err := RunTaskWith(env.deps(), nil, nil, opts)
	assertExitCode(t, err, ExitInterrupted)
	assertTaskOpen(t, env, "01-a")
}

func TestRunTaskSignalReleasesRuntimeLock(t *testing.T) {
	env := setupExecutorFixture(t, false)
	agent := writeSlowAgent(t, env.root, 10*time.Second)
	d := env.deps()

	opts := env.runOpts(true, agent)
	opts.Timeout = time.Minute
	signalOwnPidWhenAgentStarts(t, env.root)

	runtimePath, err := ResolveRuntimePathWith(d, env.root, "")
	if err != nil {
		t.Fatal(err)
	}
	_, err = RunTaskWith(d, nil, nil, opts)
	assertExitCode(t, err, ExitInterrupted)

	if status := ReadRuntimeLockStatus(d, runtimePath); status.Locked {
		t.Fatalf("drain still live after interruption: %#v", status)
	}
}

func TestRunTaskPreAgentLockFailureImmutable(t *testing.T) {
	env := setupExecutorFixture(t, false)
	agent := writeFakeAgent(t, env.root, fakeAgentConfig{summary: "unused"})
	d := env.deps()
	d.ProcessAlive = func(pid int) bool { return true }

	runtimePath, err := ResolveRuntimePathWith(d, env.root, "")
	if err != nil {
		t.Fatal(err)
	}
	lock, err := AcquireRuntimeLockForSet(d, runtimePath, "busy-set", io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = lock.Release() })

	_, err = RunTaskWith(d, nil, nil, env.runOpts(true, agent))
	assertExitCode(t, err, ExitOperational)
	assertTaskOpen(t, env, "01-a")
}

func TestRunTaskBookkeepingFailureManualRepair(t *testing.T) {
	env := setupExecutorFixture(t, false)
	agent := writeFakeAgent(t, env.root, fakeAgentConfig{
		checkTask: true,
		summary:   "done but bookkeeping fails",
	})

	d := env.deps()
	fs := &atomicBlockingFS{
		FileSystem:        d.FS,
		failManifestWrite: true,
	}
	d.FS = fs

	_, err := RunTaskWith(d, nil, nil, env.runOpts(true, agent))
	assertExitCode(t, err, ExitOperational)
	if !strings.Contains(err.Error(), "manual repair required") {
		t.Fatalf("err = %v", err)
	}
	assertTaskOpen(t, env, "01-a")
}

func TestResetTaskReturnsFailedToOpen(t *testing.T) {
	env := setupFailedTaskFixture(t)

	result, err := ResetTaskWith(env.deps(), nil, nil, ResetTaskOptions{
		ResolveInput: ResolveInput{CWD: env.root},
		TaskPath:     env.demoTaskRef(t, "01-a.md"),
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.TaskSetID != "demo" || result.TaskID != "01-a" {
		t.Fatalf("reset target = %s/%s", result.TaskSetID, result.TaskID)
	}
	assertTaskOpen(t, env, "01-a")
	assertProgressContains(t, env, "RESET")
	if result.Refresh.Rows[0].Status != StatusReady {
		t.Fatalf("status = %s", result.Refresh.Rows[0].Status)
	}
}

func TestResetTaskRejectsAlreadyOpen(t *testing.T) {
	// Open is the only status open cannot reopen; every non-Open status
	// (failed, skipped, done) is reopenable (ADR-0053).
	env := setupExecutorFixture(t, false)
	_, err := ResetTaskWith(env.deps(), nil, nil, ResetTaskOptions{
		ResolveInput: ResolveInput{CWD: env.root},
		TaskPath:     env.demoTaskRef(t, "01-a.md"),
	})
	assertExitCode(t, err, ExitNoRunnable)
	if !strings.Contains(err.Error(), "already open") {
		t.Fatalf("err = %v", err)
	}
}

func TestResetTaskSkippedToOpen(t *testing.T) {
	env := setupCustomTaskFixture(t, []Task{
		{ID: "01-a", File: "01-a.md", Title: "A", Type: "HITL", Status: "skipped"},
	})

	result, err := ResetTaskWith(env.deps(), nil, nil, ResetTaskOptions{
		ResolveInput: ResolveInput{CWD: env.root},
		TaskPath:     env.demoTaskRef(t, "01-a.md"),
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.TaskSetID != "demo" || result.TaskID != "01-a" {
		t.Fatalf("reset target = %s/%s", result.TaskSetID, result.TaskID)
	}
	assertTaskOpen(t, env, "01-a")
	assertProgressContains(t, env, "RESET", "was skipped")
}

func TestResetTaskDoneToOpen(t *testing.T) {
	// Reopening a Done task undoes a completion — the motivating case is a HITL
	// task marked Done prematurely (ADR-0053).
	env := setupCustomTaskFixture(t, []Task{
		{ID: "01-a", File: "01-a.md", Title: "A", Type: "HITL", Status: "done"},
	})

	result, err := ResetTaskWith(env.deps(), nil, nil, ResetTaskOptions{
		ResolveInput: ResolveInput{CWD: env.root},
		TaskPath:     env.demoTaskRef(t, "01-a.md"),
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.TaskSetID != "demo" || result.TaskID != "01-a" {
		t.Fatalf("reset target = %s/%s", result.TaskSetID, result.TaskID)
	}
	assertTaskOpen(t, env, "01-a")
	assertProgressContains(t, env, "RESET", "was done")
}

func TestResetTaskProgressBeforeManifest(t *testing.T) {
	env := setupFailedTaskFixture(t)
	order := &writeOrderTracker{}
	d := env.deps()
	fs := &atomicBlockingFS{
		FileSystem: d.FS,
		tracker:    order,
	}
	d.FS = fs

	_, err := ResetTaskWith(d, nil, nil, ResetTaskOptions{
		ResolveInput: ResolveInput{CWD: env.root},
		TaskPath:     env.demoTaskRef(t, "01-a.md"),
	})
	if err != nil {
		t.Fatal(err)
	}
	if order.last != "manifest" || len(order.events) < 2 || order.events[0] != "progress" {
		t.Fatalf("write order = %v last=%q", order.events, order.last)
	}
}

func TestResetTaskFailureManualRepair(t *testing.T) {
	env := setupFailedTaskFixture(t)
	d := env.deps()
	fs := &atomicBlockingFS{
		FileSystem:        d.FS,
		failManifestWrite: true,
	}
	d.FS = fs

	_, err := ResetTaskWith(d, nil, nil, ResetTaskOptions{
		ResolveInput: ResolveInput{CWD: env.root},
		TaskPath:     env.demoTaskRef(t, "01-a.md"),
	})
	assertExitCode(t, err, ExitOperational)
	if !strings.Contains(err.Error(), "manual repair required") {
		t.Fatalf("err = %v", err)
	}
	assertProgressContains(t, env, "RESET")
	assertTaskFailed(t, env, "01-a", 2)
}

func TestSelectTaskSkipsFailedTaskSetInAutomaticSelection(t *testing.T) {
	refresh := &RefreshResult{
		Rows: []Row{
			{ID: "failed", Status: StatusFailed, Priority: 100},
			{ID: "ready", Status: StatusReady, Priority: 0},
		},
		Manifests: map[string]*Manifest{
			"failed": {Stem: "failed", Valid: true, Tasks: []Task{
				{ID: "01-x", File: "01-x.md", Type: "AFK", Status: "failed"},
			}},
			"ready": {Stem: "ready", Valid: true, Tasks: []Task{
				{ID: "01-a", File: "01-a.md", Type: "AFK", Status: "open"},
			}},
		},
	}
	sel, err := SelectTask(refresh, "", "")
	if err != nil {
		t.Fatal(err)
	}
	if sel.TaskSetID != "ready" {
		t.Fatalf("selected %q, want ready", sel.TaskSetID)
	}
}

func TestFailedRowMultipleResetHints(t *testing.T) {
	t.Parallel()
	d := newTestDeps(t)
	root := t.TempDir()
	setupManifest(t, root, "failed-prd", []Task{
		{ID: "01-a", File: "01-a.md", Title: "A", Type: "AFK", Status: "failed"},
		{ID: "02-b", File: "02-b.md", Title: "B", Type: "AFK", Status: "failed"},
	})

	result, err := RegisterWith(d, root, filepath.Join(root, "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Rows[0].ResetHints) != 2 {
		t.Fatalf("reset hints = %v", result.Rows[0].ResetHints)
	}
}

type attemptScript struct {
	changeFile   string
	changeData   string
	checkTask    bool
	skipSentinel bool
	summary      string
	sleep        time.Duration
	// rawOutput, when non-empty, is written verbatim as the attempt's stdout
	// instead of the SUMMARY block or "incomplete" sentinel — the shape a real
	// provider's own error text takes. exitCode is the status the attempt loop
	// sees for this attempt; both are additive over writeRealShimAttemptAgent's
	// real-shim fields and default to today's behaviour (empty, exit 0). Only
	// the in-process writeAttemptAgent (fakeAgentBehavior.play) honors them.
	rawOutput string
	exitCode  int
}

// writeRealShimAttemptAgent installs a real #!/bin/sh agent scripted over a
// sequence of attempts. It is the pre-ADR-0144 writeAttemptAgent, kept for the
// named real-subprocess smoke set (see realShimSmokeSet) — specifically the
// timeout-kill test whose attempt hangs on a real `sleep`. Orchestration-only
// callers use the in-process writeAttemptAgent instead.
func writeRealShimAttemptAgent(t *testing.T, root string, scripts []attemptScript) string {
	t.Helper()
	path := filepath.Join(root, ".agent", "attempt-agent.sh")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	counter := filepath.Join(root, ".agent", "attempt.count")
	var b strings.Builder
	b.WriteString("#!/bin/sh\n")
	fmt.Fprintf(&b, "COUNTER=%q\n", counter)
	b.WriteString("n=1\nif [ -f \"$COUNTER\" ]; then n=$(cat \"$COUNTER\"); fi\n")
	// Advance the counter before running the case body so a timeout kill mid-sleep
	// still records the attempt as started; otherwise a killed attempt would replay
	// the same case on the next try.
	b.WriteString("echo $((n+1)) > \"$COUNTER\"\n")
	b.WriteString("case \"$n\" in\n")
	for i, script := range scripts {
		fmt.Fprintf(&b, "%d)\n", i+1)
		if script.changeFile != "" {
			fmt.Fprintf(&b, "printf %q >> %q\n", script.changeData, script.changeFile)
		}
		if script.sleep > 0 {
			fmt.Fprintf(&b, "sleep %f\n", script.sleep.Seconds())
		}
		if script.checkTask {
			b.WriteString("TASK=$(cat \"$(printf '%s' \"$*\" | sed -n 's|.*Read the file \\([^ ]*\\) in full:.*|\\1|p' | head -1)\" | sed -n 's|^You are implementing the task at: ||p' | head -1)\n")
			b.WriteString("if [ -n \"$TASK\" ] && [ -f \"$TASK\" ]; then sed -i '' 's/- \\[ \\]/- [x]/g' \"$TASK\" 2>/dev/null || sed -i 's/- \\[ \\]/- [x]/g' \"$TASK\"; fi\n")
		}
		summary := script.summary
		if summary == "" {
			summary = "attempt complete"
		}
		if script.skipSentinel {
			b.WriteString("echo incomplete\n")
		} else {
			fmt.Fprintf(&b, "printf 'SUMMARY_START\\n%s\\nSUMMARY_END\\nTASK_COMPLETE\\n' \"%s\"\n", summary, summary)
		}
		b.WriteString(";;\n")
	}
	b.WriteString("*) echo unexpected attempt; exit 2;;\n")
	b.WriteString("esac\n")
	if err := os.WriteFile(path, []byte(b.String()), 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}

func writeSlowAgent(t *testing.T, root string, delay time.Duration) string {
	t.Helper()
	path := filepath.Join(root, ".agent", "slow-agent.sh")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	sentinel := slowAgentSentinel(root)
	script := fmt.Sprintf("#!/bin/sh\n: > %s\nsleep %f\nprintf 'SUMMARY_START\\nslow\\nSUMMARY_END\\nTASK_COMPLETE\\n'\n", sentinel, delay.Seconds())
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}

func slowAgentSentinel(root string) string {
	return filepath.Join(root, ".agent", "started")
}

// signalOwnPidWhenAgentStarts waits for the slow agent's start sentinel, then
// SIGTERMs the test process. The agent only starts after runAgentAttempt has
// installed its signal handler, so the signal can never hit the default
// (fatal) action — unlike a fixed sleep, which raced against setup.
func signalOwnPidWhenAgentStarts(t *testing.T, root string) {
	t.Helper()
	sentinel := slowAgentSentinel(root)
	go func() {
		deadline := time.Now().Add(5 * time.Second)
		for time.Now().Before(deadline) {
			if _, err := os.Stat(sentinel); err == nil {
				_ = syscall.Kill(syscall.Getpid(), syscall.SIGTERM)
				return
			}
			time.Sleep(10 * time.Millisecond)
		}
	}()
}

func writeProcessGroupAgent(t *testing.T, root string, delay time.Duration) string {
	t.Helper()
	path := filepath.Join(root, ".agent", "group-agent.sh")
	childMarker := filepath.Join(root, ".child-alive")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	script := fmt.Sprintf("#!/bin/sh\n( while true; do echo alive > %q; sleep 0.05; done ) &\nsleep %f\n", childMarker, delay.Seconds())
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}

func setupFailedTaskFixture(t *testing.T) *execFixture {
	t.Helper()
	env := setupExecutorFixture(t, false)
	d := env.deps()
	m := LoadManifest(d, "demo", env.demoManifest())
	failedAfter := 2
	m.Tasks[0].Status = "failed"
	m.Tasks[0].FailedAfter = &failedAfter
	if err := WriteManifestAtomic(d, m); err != nil {
		t.Fatal(err)
	}
	return env
}

type countingRunner struct {
	t        *testing.T
	calls    *int32
	exitCode int
}

func (r *countingRunner) Run(ctx context.Context, dir string, stdout, stderr io.Writer, name string, args ...string) (int, error) {
	proc, err := r.Start(ctx, dir, stdout, stderr, name, args...)
	if err != nil {
		return 1, err
	}
	return proc.Wait()
}

func (r *countingRunner) Start(ctx context.Context, dir string, stdout, stderr io.Writer, name string, args ...string) (*ManagedProcess, error) {
	if !IsAgentAvailabilityProbeCommand(name, args) {
		atomic.AddInt32(r.calls, 1)
	}
	return fakeAwareRunner{}.Start(ctx, dir, stdout, stderr, name, args...)
}

type atomicBlockingFS struct {
	deps.FileSystem
	failManifestWrite bool
	tracker           *writeOrderTracker
}

func (f *atomicBlockingFS) WriteFile(name string, data []byte, perm os.FileMode) error {
	if f.tracker != nil {
		if strings.Contains(name, "progress.txt") {
			f.tracker.events = append(f.tracker.events, "progress")
			f.tracker.last = "progress"
		}
		if strings.Contains(name, "index.json") {
			f.tracker.events = append(f.tracker.events, "manifest")
			f.tracker.last = "manifest"
		}
	}
	if f.failManifestWrite && strings.Contains(name, "index.json") {
		if strings.Contains(string(data), `"status": "done"`) || strings.Contains(string(data), `"status": "open"`) {
			return fmt.Errorf("manifest write blocked")
		}
	}
	return f.FileSystem.WriteFile(name, data, perm)
}

func (f *atomicBlockingFS) Rename(oldpath, newpath string) error {
	if f.tracker != nil {
		if strings.Contains(newpath, "progress.txt") {
			f.tracker.events = append(f.tracker.events, "progress")
			f.tracker.last = "progress"
		}
		if strings.Contains(newpath, "index.json") {
			f.tracker.events = append(f.tracker.events, "manifest")
			f.tracker.last = "manifest"
		}
	}
	if f.failManifestWrite && strings.Contains(newpath, "index.json") {
		data, err := os.ReadFile(oldpath)
		if err != nil {
			return err
		}
		if strings.Contains(string(data), `"status": "done"`) || strings.Contains(string(data), `"status": "open"`) {
			return fmt.Errorf("manifest write blocked")
		}
	}
	if renamer, ok := f.FileSystem.(interface{ Rename(string, string) error }); ok {
		return renamer.Rename(oldpath, newpath)
	}
	return os.Rename(oldpath, newpath)
}

type writeOrderTracker struct {
	events []string
	last   string
}

func TestRunTaskResumesInterruptedAttemptOnFirstTry(t *testing.T) {
	env := setupExecutorFixture(t, false)
	streamDir := taskStreamDir(env.demoDir(), "01-a.md")
	start := time.Date(2026, 6, 12, 10, 0, 0, 0, time.UTC)
	writeTimingStreamRecords(t, streamDir, "attempt-001.jsonl.gz",
		streamHeaderRecord{Type: "header", Agent: "claude", Attempt: 1, StartTime: start},
		[]streamEventRecord{claudeAssistantEvent(10, "Partial work from the interrupted attempt.")},
		streamFooterRecord{Type: "footer", Outcome: streamOutcomeInterrupted, DurationMS: 100, Reason: "", ExitCode: 143})

	runner := &captureAgentRunner{}
	d := env.deps()
	d.Runner = runner

	opts := env.runOpts(true, "./agent.sh")
	opts.AgentPreset = "claude"
	opts.MaxTries = 1
	opts.Output = io.Discard

	_, _ = RunTaskWith(d, nil, nil, opts)
	if len(runner.argLists) != 1 {
		t.Fatalf("expected 1 attempt, got %d", len(runner.argLists))
	}
	prompt := runner.attemptPrompt(0)
	if !strings.Contains(prompt, lessonResume) {
		t.Fatalf("attempt 1 prompt missing resume lesson:\n%s", prompt)
	}
	if !strings.Contains(prompt, "Partial work from the interrupted attempt.") {
		t.Fatalf("attempt 1 prompt missing prior narrative:\n%s", prompt)
	}
}

func TestRunTaskResumesQuotaPausedAttemptOnFirstTry(t *testing.T) {
	env := setupExecutorFixture(t, false)
	streamDir := taskStreamDir(env.demoDir(), "01-a.md")
	start := time.Date(2026, 6, 12, 10, 0, 0, 0, time.UTC)
	writeTimingStreamRecords(t, streamDir, "attempt-001.jsonl.gz",
		streamHeaderRecord{Type: "header", Agent: "claude", Attempt: 1, StartTime: start},
		[]streamEventRecord{claudeAssistantEvent(10, "Partial work before the quota pause.")},
		streamFooterRecord{Type: "footer", Outcome: streamOutcomeQuotaPaused, DurationMS: 100, Reason: "", ExitCode: 0})

	runner := &captureAgentRunner{}
	d := env.deps()
	d.Runner = runner

	opts := env.runOpts(true, "./agent.sh")
	opts.AgentPreset = "claude"
	opts.MaxTries = 1
	opts.Output = io.Discard

	_, _ = RunTaskWith(d, nil, nil, opts)
	if len(runner.argLists) != 1 {
		t.Fatalf("expected 1 attempt, got %d", len(runner.argLists))
	}
	prompt := runner.attemptPrompt(0)
	if !strings.Contains(prompt, lessonResume) {
		t.Fatalf("attempt 1 prompt missing resume lesson:\n%s", prompt)
	}
	if !strings.Contains(prompt, "Partial work before the quota pause.") {
		t.Fatalf("attempt 1 prompt missing prior narrative:\n%s", prompt)
	}
}

func TestRunTaskFreshTaskPromptHasNoCarry(t *testing.T) {
	env := setupExecutorFixture(t, false)
	runner := &captureAgentRunner{}
	d := env.deps()
	d.Runner = runner

	opts := env.runOpts(true, "./agent.sh")
	opts.AgentPreset = "claude"
	opts.MaxTries = 1
	opts.Output = io.Discard

	_, _ = RunTaskWith(d, nil, nil, opts)
	if len(runner.argLists) != 1 {
		t.Fatalf("expected 1 attempt, got %d", len(runner.argLists))
	}
	prompt := runner.attemptPrompt(0)
	if strings.Contains(prompt, "Prior attempts on THIS task") {
		t.Fatalf("fresh task prompt should not contain prior-attempt digest:\n%s", prompt)
	}
	if strings.Contains(prompt, "Sibling tasks already completed") {
		t.Fatalf("fresh task prompt should not contain sibling briefs:\n%s", prompt)
	}
	if strings.Contains(prompt, "Remediation history") {
		t.Fatalf("fresh task prompt should not contain remediation history:\n%s", prompt)
	}
}

// A later AFK attempt in a set with done remediations receives the history
// block (ADR-0154), as a separate channel from the prior-attempt digest.
func TestRunTaskPromptCarriesRemediationHistory(t *testing.T) {
	setEnv := setupRunTaskSetFixture(t, "demo", []Task{
		{ID: "01-a", File: "01-a.md", Title: "A", Type: "AFK", Status: "done"},
		{ID: "02-remediation", File: "02-remediation.md", Title: "Remediation 1: retry cap", Type: "AFK", Status: "done"},
		{ID: "03-b", File: "03-b.md", Title: "B", Type: "AFK", Status: "open"},
	})
	env := setEnv.execFixture()
	writeRemediationProgress(t, env.demoDir(), "2026-06-10T09:00:00Z [02-remediation.md] DONE\nraised the retry cap to three")

	runner := &captureAgentRunner{}
	d := env.deps()
	d.Runner = runner

	opts := env.runOpts(true, "./agent.sh")
	opts.AgentPreset = "claude"
	opts.MaxTries = 1
	opts.Output = io.Discard
	opts.TaskPathOverride = env.demoTaskRef(t, "03-b.md")

	_, _ = RunTaskWith(d, nil, nil, opts)
	if len(runner.argLists) != 1 {
		t.Fatalf("expected 1 attempt, got %d", len(runner.argLists))
	}
	prompt := runner.attemptPrompt(0)
	for _, want := range []string{
		"Remediation history",
		"not work for you to do",
		"Remediation 1: retry cap",
		"raised the retry cap to three",
	} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("AFK prompt missing remediation history %q:\n%s", want, prompt)
		}
	}
	// Prior-attempt digest stays a separate channel and stays empty on a fresh task.
	if strings.Contains(prompt, "Prior attempts on THIS task") {
		t.Fatalf("fresh task must not fuse/include prior-attempt digest:\n%s", prompt)
	}
}

// A later remediation attempt also receives earlier remediations' history —
// cycle 2 must not re-tread cycle 1 blind (ADR-0154).
func TestRunTaskRemediationAttemptCarriesPriorRemediationHistory(t *testing.T) {
	setEnv := setupRunTaskSetFixture(t, "demo", []Task{
		{ID: "01-a", File: "01-a.md", Title: "A", Type: "AFK", Status: "done"},
		{ID: "02-remediation", File: "02-remediation.md", Title: "Remediation 1: first", Type: "AFK", Status: "done"},
		{ID: "03-remediation", File: "03-remediation.md", Title: "Remediation 2: second", Type: "AFK", Status: "open"},
	})
	env := setEnv.execFixture()
	writeRemediationProgress(t, env.demoDir(), "2026-06-10T09:00:00Z [02-remediation.md] DONE\nfixed the flaky assertion")

	runner := &captureAgentRunner{}
	d := env.deps()
	d.Runner = runner

	opts := env.runOpts(true, "./agent.sh")
	opts.AgentPreset = "claude"
	opts.MaxTries = 1
	opts.Output = io.Discard
	opts.TaskPathOverride = env.demoTaskRef(t, "03-remediation.md")

	_, _ = RunTaskWith(d, nil, nil, opts)
	if len(runner.argLists) != 1 {
		t.Fatalf("expected 1 attempt, got %d", len(runner.argLists))
	}
	prompt := runner.attemptPrompt(0)
	for _, want := range []string{
		"Remediation history",
		"Remediation 1: first",
		"fixed the flaky assertion",
	} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("remediation attempt missing prior history %q:\n%s", want, prompt)
		}
	}
}

func TestRunTaskReopenedTaskPromptHasNoPriorDigest(t *testing.T) {
	env := setupFailedTaskFixture(t)
	streamDir := taskStreamDir(env.demoDir(), "01-a.md")
	preReset := time.Date(2026, 6, 12, 9, 0, 0, 0, time.UTC)
	writeTimingStreamRecords(t, streamDir, "attempt-001.jsonl.gz",
		streamHeaderRecord{Type: "header", Agent: "claude", Attempt: 1, StartTime: preReset},
		[]streamEventRecord{claudeAssistantEvent(10, "Abandoned work before the reset.")},
		streamFooterRecord{Type: "footer", Outcome: streamOutcomeInterrupted, DurationMS: 100, Reason: "", ExitCode: 143})

	if _, err := ResetTaskWith(env.deps(), nil, nil, ResetTaskOptions{
		ResolveInput: ResolveInput{CWD: env.root},
		TaskPath:     env.demoTaskRef(t, "01-a.md"),
	}); err != nil {
		t.Fatal(err)
	}

	runner := &captureAgentRunner{}
	d := env.deps()
	d.Runner = runner

	opts := env.runOpts(true, "./agent.sh")
	opts.AgentPreset = "claude"
	opts.MaxTries = 1
	opts.Output = io.Discard

	_, _ = RunTaskWith(d, nil, nil, opts)
	if len(runner.argLists) != 1 {
		t.Fatalf("expected 1 attempt, got %d", len(runner.argLists))
	}
	prompt := runner.attemptPrompt(0)
	if strings.Contains(prompt, "Prior attempts on THIS task") {
		t.Fatalf("reopened task prompt should not contain pre-reset digest:\n%s", prompt)
	}
}
