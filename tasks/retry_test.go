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
	dir := filepath.Join(root, ".agent-bin")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	counterPath := filepath.Join(dir, "claude.count")
	script := "#!/bin/sh\n" +
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
	agent := writeAttemptAgent(t, env.root, []attemptScript{
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

	fs := &atomicBlockingFS{
		FileSystem:        deps.NewRealFileSystem(),
		failManifestWrite: true,
	}
	d := env.deps()
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
	fs := &atomicBlockingFS{
		FileSystem: deps.NewRealFileSystem(),
		tracker:    order,
	}
	d := env.deps()
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
	fs := &atomicBlockingFS{
		FileSystem:        deps.NewRealFileSystem(),
		failManifestWrite: true,
	}
	d := env.deps()
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
	root := t.TempDir()
	setupManifest(t, root, "failed-prd", []Task{
		{ID: "01-a", File: "01-a.md", Title: "A", Type: "AFK", Status: "failed"},
		{ID: "02-b", File: "02-b.md", Title: "B", Type: "AFK", Status: "failed"},
	})

	result, err := RegisterWith(DefaultDeps(), root, filepath.Join(root, "state.json"))
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
}

func writeAttemptAgent(t *testing.T, root string, scripts []attemptScript) string {
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
			b.WriteString("TASK=$(printf '%s' \"$1\" | sed -n 's|^You are implementing the task at: ||p' | head -1)\n")
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
	m := LoadManifest(DefaultDeps(), "demo", env.demoManifest())
	failedAfter := 2
	m.Tasks[0].Status = "failed"
	m.Tasks[0].FailedAfter = &failedAfter
	if err := WriteManifestAtomic(DefaultDeps(), m); err != nil {
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
	atomic.AddInt32(r.calls, 1)
	return RealCommandRunner{}.Start(ctx, dir, stdout, stderr, name, args...)
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

// customAgentPrompt extracts the prompt from a custom-agent invocation captured
// by captureAgentRunner. Custom agents are invoked as:
//   sh -c '<agentCmd> "$@"' task-agent <prompt>
func customAgentPrompt(args []string) string {
	if len(args) >= 4 {
		return args[3]
	}
	return ""
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
	prompt := customAgentPrompt(runner.argLists[0])
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
	prompt := customAgentPrompt(runner.argLists[0])
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
	prompt := customAgentPrompt(runner.argLists[0])
	if strings.Contains(prompt, "Prior attempts on THIS task") {
		t.Fatalf("fresh task prompt should not contain prior-attempt digest:\n%s", prompt)
	}
	if strings.Contains(prompt, "Sibling tasks already completed") {
		t.Fatalf("fresh task prompt should not contain sibling briefs:\n%s", prompt)
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
	prompt := customAgentPrompt(runner.argLists[0])
	if strings.Contains(prompt, "Prior attempts on THIS task") {
		t.Fatalf("reopened task prompt should not contain pre-reset digest:\n%s", prompt)
	}
}
