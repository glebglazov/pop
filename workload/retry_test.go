package workload

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

func TestRunIssueRetriesPreserveEdits(t *testing.T) {
	env := setupExecutorFixture(t, false)
	agent := writeAttemptAgent(t, env.root, []attemptScript{
		{changeFile: "impl.txt", changeData: "partial\n", checkIssue: true, skipSentinel: true},
		{changeFile: "impl.txt", changeData: "more\n", checkIssue: true, summary: "finished on retry"},
	})

	opts := env.runOpts(true, agent)
	opts.MaxTries = 3
	var buf bytes.Buffer
	opts.Output = &buf

	_, err := RunIssueWith(env.deps(), nil, nil, opts)
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
	assertIssueDone(t, env, "01-a")
}

func TestRunIssueExhaustedRetriesMarkFailed(t *testing.T) {
	env := setupExecutorFixture(t, false)
	agent := writeFakeAgent(t, env.root, fakeAgentConfig{
		changeFile:   "impl.txt",
		changeData:   "left behind\n",
		checkIssue:   true,
		skipSentinel: true,
	})

	opts := env.runOpts(true, agent)
	opts.MaxTries = 2
	_, err := RunIssueWith(env.deps(), nil, nil, opts)
	assertExitCode(t, err, ExitOperational)
	assertIssueFailed(t, env, "01-a", 2)
	assertProgressContains(t, env, "FAILED", "failed after 2 attempts")
	if _, err := os.Stat(filepath.Join(env.root, "impl.txt")); err != nil {
		t.Fatal("partial runtime edits should be preserved")
	}
}

func TestRunIssueClaudeQuotaPauseLeavesIssueOpenWithoutRetry(t *testing.T) {
	env := setupExecutorFixture(t, false)
	counterPath := installClaudeQuotaAgent(t, env.root)

	opts := env.runOpts(true, "")
	opts.AgentPreset = "claude"
	opts.MaxTries = 3
	var buf bytes.Buffer
	opts.Output = &buf

	result, err := RunIssueWith(env.deps(), nil, nil, opts)
	if err != nil {
		t.Fatal(err)
	}
	if !result.QuotaPaused || !strings.Contains(result.PauseReason, "weekly limit") {
		t.Fatalf("result = %#v", result)
	}
	assertIssueOpen(t, env, "01-a")
	if got := strings.TrimSpace(string(mustReadFile(t, counterPath))); got != "1" {
		t.Fatalf("started attempts = %q, want 1", got)
	}
	if _, err := os.Stat(filepath.Join(env.root, "thoughts/issues/demo/progress.txt")); !os.IsNotExist(err) {
		t.Fatalf("quota pause wrote progress: %v", err)
	}
	if strings.Contains(buf.String(), "{\"type\"") {
		t.Fatalf("quota pause rendered raw JSONL:\n%s", buf.String())
	}
	if got := strings.Count(buf.String(), "You've hit your weekly limit"); got != 1 {
		t.Fatalf("quota reason rendered %d times, want 1:\n%s", got, buf.String())
	}
}

func TestRunIssueConfigurableMaxTries(t *testing.T) {
	env := setupExecutorFixture(t, false)
	var calls int32
	runner := &countingRunner{t: t, calls: &calls, exitCode: 1}
	d := env.deps()
	d.Runner = runner

	opts := env.runOpts(true, "ignored")
	opts.MaxTries = 5
	opts.AgentCmd = writeFakeAgent(t, env.root, fakeAgentConfig{exitCode: 1})

	_, err := RunIssueWith(d, nil, nil, opts)
	assertExitCode(t, err, ExitOperational)
	if got := atomic.LoadInt32(&calls); got != 5 {
		t.Fatalf("started attempts = %d, want 5", got)
	}
	assertIssueFailed(t, env, "01-a", 5)
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

func TestRunIssueTimeoutMarksFailedWithoutRetry(t *testing.T) {
	env := setupExecutorFixture(t, false)
	agent := writeSlowAgent(t, env.root, 2*time.Second)

	opts := env.runOpts(true, agent)
	opts.MaxTries = 3
	opts.Timeout = 100 * time.Millisecond
	var buf bytes.Buffer
	opts.Output = &buf
	_, err := RunIssueWith(env.deps(), nil, nil, opts)
	assertExitCode(t, err, ExitOperational)
	if !strings.Contains(err.Error(), "timed out after 100ms on attempt 1") {
		t.Fatalf("err = %v", err)
	}
	if !strings.Contains(buf.String(), "Agent killed (timeout) for demo/01-a") {
		t.Fatalf("missing kill banner:\n%s", buf.String())
	}
	if !strings.Contains(buf.String(), "✗ Attempt 1/3 timed out after 100ms") {
		t.Fatalf("missing timeout failure line:\n%s", buf.String())
	}
	assertIssueFailed(t, env, "01-a", 1)
	assertProgressContains(t, env, "FAILED", "timed out")
}

func TestRunIssueTimeoutRecordsAttemptsStarted(t *testing.T) {
	env := setupExecutorFixture(t, false)
	agent := writeSlowAgent(t, env.root, 2*time.Second)

	opts := env.runOpts(true, agent)
	opts.MaxTries = 4
	opts.Timeout = 100 * time.Millisecond
	_, err := RunIssueWith(env.deps(), nil, nil, opts)
	assertExitCode(t, err, ExitOperational)
	assertIssueFailed(t, env, "01-a", 1)
}

func TestRunIssueTimeoutKillsProcessGroup(t *testing.T) {
	env := setupExecutorFixture(t, false)
	agent := writeProcessGroupAgent(t, env.root, 5*time.Second)

	opts := env.runOpts(true, agent)
	opts.MaxTries = 3
	opts.Timeout = 200 * time.Millisecond
	_, err := RunIssueWith(env.deps(), nil, nil, opts)
	assertExitCode(t, err, ExitOperational)
	if _, err := os.Stat(filepath.Join(env.root, ".child-alive")); !os.IsNotExist(err) {
		t.Fatal("child process should have been terminated with the group")
	}
}

func TestRunIssueSignalLeavesIssueOpen(t *testing.T) {
	env := setupExecutorFixture(t, false)
	agent := writeSlowAgent(t, env.root, 10*time.Second)

	opts := env.runOpts(true, agent)
	opts.Timeout = time.Minute
	signalOwnPidWhenAgentStarts(t, env.root)

	_, err := RunIssueWith(env.deps(), nil, nil, opts)
	assertExitCode(t, err, ExitInterrupted)
	assertIssueOpen(t, env, "01-a")
}

func TestRunIssueSignalReleasesRuntimeLock(t *testing.T) {
	env := setupExecutorFixture(t, false)
	agent := writeSlowAgent(t, env.root, 10*time.Second)
	d := env.deps()

	opts := env.runOpts(true, agent)
	opts.Timeout = time.Minute
	signalOwnPidWhenAgentStarts(t, env.root)

	_, err := RunIssueWith(d, nil, nil, opts)
	assertExitCode(t, err, ExitInterrupted)

	lockPath := RuntimeLockPathFor(d, env.root)
	if _, err := os.Stat(lockPath); !os.IsNotExist(err) {
		t.Fatalf("lock not released after interruption: %v", err)
	}
}

func TestRunIssuePreAgentLockFailureImmutable(t *testing.T) {
	env := setupExecutorFixture(t, false)
	agent := writeFakeAgent(t, env.root, fakeAgentConfig{summary: "unused"})
	d := env.deps()
	d.ProcessAlive = func(pid int) bool { return true }

	runtimePath, err := ResolveRuntimePathWith(d, env.root, "")
	if err != nil {
		t.Fatal(err)
	}
	lock, err := AcquireRuntimeLock(d, runtimePath, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = lock.Release() })

	_, err = RunIssueWith(d, nil, nil, env.runOpts(true, agent))
	assertExitCode(t, err, ExitOperational)
	assertIssueOpen(t, env, "01-a")
}

func TestRunIssueBookkeepingFailureManualRepair(t *testing.T) {
	env := setupExecutorFixture(t, false)
	agent := writeFakeAgent(t, env.root, fakeAgentConfig{
		checkIssue: true,
		summary:    "done but bookkeeping fails",
	})

	fs := &atomicBlockingFS{
		FileSystem:        deps.NewRealFileSystem(),
		failManifestWrite: true,
	}
	d := env.deps()
	d.FS = fs

	_, err := RunIssueWith(d, nil, nil, env.runOpts(true, agent))
	assertExitCode(t, err, ExitOperational)
	if !strings.Contains(err.Error(), "manual repair required") {
		t.Fatalf("err = %v", err)
	}
	assertIssueOpen(t, env, "01-a")
}

func TestResetIssueReturnsFailedToOpen(t *testing.T) {
	env := setupFailedIssueFixture(t)

	result, err := ResetIssueWith(env.deps(), nil, nil, ResetIssueOptions{
		ResolveInput: ResolveInput{CWD: env.root},
		IssuePath:    "thoughts/issues/demo/01-a.md",
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.IssueSetID != "demo" || result.IssueID != "01-a" {
		t.Fatalf("reset target = %s/%s", result.IssueSetID, result.IssueID)
	}
	assertIssueOpen(t, env, "01-a")
	assertProgressContains(t, env, "RESET")
	if result.Refresh.Rows[0].Status != StatusReady {
		t.Fatalf("status = %s", result.Refresh.Rows[0].Status)
	}
}

func TestResetIssueRequiresFailed(t *testing.T) {
	env := setupExecutorFixture(t, false)
	_, err := ResetIssueWith(env.deps(), nil, nil, ResetIssueOptions{
		ResolveInput: ResolveInput{CWD: env.root},
		IssuePath:    "thoughts/issues/demo/01-a.md",
	})
	assertExitCode(t, err, ExitNoRunnable)
	if !strings.Contains(err.Error(), "failed or skipped") {
		t.Fatalf("err = %v", err)
	}
}

func TestResetIssueSkippedToOpen(t *testing.T) {
	env := setupCustomIssueFixture(t, []Issue{
		{ID: "01-a", File: "01-a.md", Title: "A", Type: "HITL", Status: "skipped"},
	})

	result, err := ResetIssueWith(env.deps(), nil, nil, ResetIssueOptions{
		ResolveInput: ResolveInput{CWD: env.root},
		IssuePath:    "thoughts/issues/demo/01-a.md",
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.IssueSetID != "demo" || result.IssueID != "01-a" {
		t.Fatalf("reset target = %s/%s", result.IssueSetID, result.IssueID)
	}
	assertIssueOpen(t, env, "01-a")
	assertProgressContains(t, env, "RESET", "was skipped")
}

func TestResetIssueProgressBeforeManifest(t *testing.T) {
	env := setupFailedIssueFixture(t)
	order := &writeOrderTracker{}
	fs := &atomicBlockingFS{
		FileSystem: deps.NewRealFileSystem(),
		tracker:    order,
	}
	d := env.deps()
	d.FS = fs

	_, err := ResetIssueWith(d, nil, nil, ResetIssueOptions{
		ResolveInput: ResolveInput{CWD: env.root},
		IssuePath:    "thoughts/issues/demo/01-a.md",
	})
	if err != nil {
		t.Fatal(err)
	}
	if order.last != "manifest" || len(order.events) < 2 || order.events[0] != "progress" {
		t.Fatalf("write order = %v last=%q", order.events, order.last)
	}
}

func TestResetIssueFailureManualRepair(t *testing.T) {
	env := setupFailedIssueFixture(t)
	fs := &atomicBlockingFS{
		FileSystem:        deps.NewRealFileSystem(),
		failManifestWrite: true,
	}
	d := env.deps()
	d.FS = fs

	_, err := ResetIssueWith(d, nil, nil, ResetIssueOptions{
		ResolveInput: ResolveInput{CWD: env.root},
		IssuePath:    "thoughts/issues/demo/01-a.md",
	})
	assertExitCode(t, err, ExitOperational)
	if !strings.Contains(err.Error(), "manual repair required") {
		t.Fatalf("err = %v", err)
	}
	assertProgressContains(t, env, "RESET")
	assertIssueFailed(t, env, "01-a", 2)
}

func TestSelectIssueSkipsFailedIssueSetInAutomaticSelection(t *testing.T) {
	refresh := &RefreshResult{
		Rows: []Row{
			{ID: "failed", Status: StatusFailed, Priority: 100},
			{ID: "ready", Status: StatusReady, Priority: 0},
		},
		Manifests: map[string]*Manifest{
			"failed": {Stem: "failed", Valid: true, Issues: []Issue{
				{ID: "01-x", File: "01-x.md", Type: "AFK", Status: "failed"},
			}},
			"ready": {Stem: "ready", Valid: true, Issues: []Issue{
				{ID: "01-a", File: "01-a.md", Type: "AFK", Status: "open"},
			}},
		},
	}
	sel, err := SelectIssue(refresh, "", "")
	if err != nil {
		t.Fatal(err)
	}
	if sel.IssueSetID != "ready" {
		t.Fatalf("selected %q, want ready", sel.IssueSetID)
	}
}

func TestFailedRowMultipleResetHints(t *testing.T) {
	root := t.TempDir()
	setupManifest(t, root, "failed-prd", []Issue{
		{ID: "01-a", File: "01-a.md", Title: "A", Type: "AFK", Status: "failed"},
		{ID: "02-b", File: "02-b.md", Title: "B", Type: "AFK", Status: "failed"},
	})

	result, err := RefreshWith(DefaultDeps(), root, filepath.Join(root, "state.json"))
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
	checkIssue   bool
	skipSentinel bool
	summary      string
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
	b.WriteString("case \"$n\" in\n")
	for i, script := range scripts {
		fmt.Fprintf(&b, "%d)\n", i+1)
		if script.changeFile != "" {
			fmt.Fprintf(&b, "printf %q >> %q\n", script.changeData, script.changeFile)
		}
		if script.checkIssue {
			b.WriteString("ISSUE=$(printf '%s' \"$1\" | sed -n 's|^You are implementing the issue at: ||p' | head -1)\n")
			b.WriteString("if [ -n \"$ISSUE\" ] && [ -f \"$ISSUE\" ]; then sed -i '' 's/- \\[ \\]/- [x]/g' \"$ISSUE\" 2>/dev/null || sed -i 's/- \\[ \\]/- [x]/g' \"$ISSUE\"; fi\n")
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
	b.WriteString("echo $((n+1)) > \"$COUNTER\"\n")
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

func setupFailedIssueFixture(t *testing.T) *execFixture {
	t.Helper()
	env := setupExecutorFixture(t, false)
	m := LoadManifest(DefaultDeps(), "demo", filepath.Join(env.root, "thoughts/issues/demo/index.json"))
	failedAfter := 2
	m.Issues[0].Status = "failed"
	m.Issues[0].FailedAfter = &failedAfter
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
