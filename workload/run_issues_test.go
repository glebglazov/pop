package workload

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/glebglazov/pop/internal/deps"
)

func TestRunIssueSetDrainsMultipleAFKIssuesInOrder(t *testing.T) {
	env := setupRunIssueSetFixture(t, "demo", []Issue{
		{ID: "01-a", File: "01-a.md", Title: "A", Type: "AFK", Status: "open"},
		{ID: "02-b", File: "02-b.md", Title: "B", Type: "AFK", Status: "open"},
	})
	agent := writeFakeAgent(t, env.root, fakeAgentConfig{
		changeFile: "impl.txt",
		changeData: "x\n",
		checkIssue: true,
		summary:    "done",
	})

	var buf bytes.Buffer
	result, err := RunIssueSetWith(env.deps(), nil, nil, env.runIssueSetOpts(true, agent, &buf))
	if err != nil {
		t.Fatalf("run failed: %v", err)
	}
	if !result.IssueSetDone || len(result.Completed) != 2 {
		t.Fatalf("result = %#v", result)
	}
	if result.Completed[0].Selection.IssueID != "01-a" || result.Completed[1].Selection.IssueID != "02-b" {
		t.Fatalf("issue order = %s, %s", result.Completed[0].Selection.IssueID, result.Completed[1].Selection.IssueID)
	}
	assertIssueDone(t, env.execFixture(), "01-a")
	assertIssueDone(t, env.execFixture(), "02-b")
}

func TestRunIssueSetSequentialDependencyUnblocking(t *testing.T) {
	env := setupRunIssueSetFixture(t, "demo", []Issue{
		{ID: "01-a", File: "01-a.md", Title: "A", Type: "AFK", Status: "open"},
		{ID: "02-b", File: "02-b.md", Title: "B", Type: "AFK", Status: "open", BlockedBy: []string{"01-a"}},
	})
	agent := writeFakeAgent(t, env.root, fakeAgentConfig{checkIssue: true, summary: "ok"})

	result, err := RunIssueSetWith(env.deps(), nil, nil, env.runIssueSetOpts(true, agent, nil))
	if err != nil {
		t.Fatalf("run failed: %v", err)
	}
	if len(result.Completed) != 2 {
		t.Fatalf("completed = %d", len(result.Completed))
	}
	assertIssueDone(t, env.execFixture(), "02-b")
}

func TestRunIssueSetNoOpContinuation(t *testing.T) {
	env := setupRunIssueSetFixture(t, "demo", []Issue{
		{ID: "01-a", File: "01-a.md", Title: "A", Type: "AFK", Status: "open"},
		{ID: "02-b", File: "02-b.md", Title: "B", Type: "AFK", Status: "open"},
	})
	agent := writeFakeAgent(t, env.root, fakeAgentConfig{checkIssue: true, summary: "verified"})

	result, err := RunIssueSetWith(env.deps(), nil, nil, env.runIssueSetOpts(true, agent, nil))
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Completed) != 2 || !result.Completed[0].NoOp {
		t.Fatalf("result = %#v", result)
	}
}

func TestRunIssueSetSingleConfirmation(t *testing.T) {
	env := setupRunIssueSetFixture(t, "demo", []Issue{
		{ID: "01-a", File: "01-a.md", Title: "A", Type: "AFK", Status: "open"},
		{ID: "02-b", File: "02-b.md", Title: "B", Type: "AFK", Status: "open"},
	})
	agent := writeFakeAgent(t, env.root, fakeAgentConfig{checkIssue: true, summary: "ok"})

	var confirmOut bytes.Buffer
	opts := env.runIssueSetOpts(false, agent, nil)
	opts.ConfirmIn = strings.NewReader("y\n")
	opts.ConfirmOut = &confirmOut

	_, err := RunIssueSetWith(env.deps(), nil, nil, opts)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Count(confirmOut.String(), "Run Issue set?") != 1 {
		t.Fatalf("expected one confirmation prompt:\n%s", confirmOut.String())
	}
}

func TestRunIssueSetDirtyNonInteractiveRejection(t *testing.T) {
	env := setupRunIssueSetFixture(t, "demo", []Issue{
		{ID: "01-a", File: "01-a.md", Title: "A", Type: "AFK", Status: "open"},
	})
	writeFile(t, filepath.Join(env.root, "partial.txt"), "pending\n")
	agent := writeFakeAgent(t, env.root, fakeAgentConfig{summary: "unused"})

	opts := env.runIssueSetOpts(false, agent, nil)
	opts.ConfirmIn = NonInteractiveReader{}
	_, err := RunIssueSetWith(env.deps(), nil, nil, opts)
	assertExitCode(t, err, ExitOperational)
}

func TestRunIssueSetAppliesDirtyStrategyOnceBeforeDrain(t *testing.T) {
	env := setupRunIssueSetFixture(t, "demo", []Issue{
		{ID: "01-a", File: "01-a.md", Title: "A", Type: "AFK", Status: "open"},
		{ID: "02-b", File: "02-b.md", Title: "B", Type: "AFK", Status: "open"},
	})
	writeFile(t, filepath.Join(env.root, "partial.txt"), "stash once\n")
	agent := writeFakeAgent(t, env.root, fakeAgentConfig{checkIssue: true, summary: "done"})

	stashPushes := 0
	git := &deps.MockGit{
		CommandInDirFunc: func(dir string, args ...string) (string, error) {
			if len(args) >= 2 && args[0] == "stash" && args[1] == "push" {
				stashPushes++
			}
			return realGitInDir(dir, args...)
		},
	}
	d := env.deps()
	d.Git = git
	opts := env.runIssueSetOpts(true, agent, nil)
	opts.AllowDirty = DirtyRuntimeStashAndContinue

	result, err := RunIssueSetWith(d, nil, nil, opts)
	if err != nil {
		t.Fatalf("run failed: %v", err)
	}
	if len(result.Completed) != 2 {
		t.Fatalf("completed = %d", len(result.Completed))
	}
	if stashPushes != 1 {
		t.Fatalf("stash pushes = %d, want 1", stashPushes)
	}
}

func TestRunIssueSetTargetedIssueSet(t *testing.T) {
	root := t.TempDir()
	initExecutorGitRepo(t, root)
	setupManifest(t, root, "high", []Issue{
		{ID: "01-a", File: "01-a.md", Title: "A", Type: "AFK", Status: "open"},
	})
	setupManifest(t, root, "low", []Issue{
		{ID: "01-x", File: "01-x.md", Title: "X", Type: "AFK", Status: "open"},
	})
	t.Setenv("XDG_DATA_HOME", filepath.Join(root, ".xdg"))
	refresh, err := RefreshWith(DefaultDeps(), root, DefaultStatePath())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := SetPriorityWith(DefaultDeps(), nil, nil, ResolveInput{CWD: root}, "low", 99); err != nil {
		t.Fatal(err)
	}
	_ = refresh

	agent := writeFakeAgent(t, root, fakeAgentConfig{checkIssue: true, summary: "targeted"})
	env := &runIssueSetFixture{root: root}
	opts := env.runIssueSetOpts(true, agent, nil)
	opts.IssueSetOverride = "thoughts/issues/high"

	result, err := RunIssueSetWith(env.deps(), nil, nil, opts)
	if err != nil {
		t.Fatal(err)
	}
	if result.IssueSetID != "high" || len(result.Completed) != 1 || result.Completed[0].Selection.IssueID != "01-a" {
		t.Fatalf("result = %#v", result)
	}
}

func TestRunIssueSetBlockedStopsWithReason(t *testing.T) {
	env := setupRunIssueSetFixture(t, "demo", []Issue{
		{ID: "01-a", File: "01-a.md", Title: "A", Type: "AFK", Status: "open"},
		{ID: "02-hitl", File: "02-hitl.md", Title: "Review", Type: "HITL", Status: "open"},
	})
	agent := writeFakeAgent(t, env.root, fakeAgentConfig{checkIssue: true, summary: "first done"})

	_, err := RunIssueSetWith(env.deps(), nil, nil, env.runIssueSetOpts(true, agent, nil))
	assertExitCode(t, err, ExitNoRunnable)
	if !strings.Contains(err.Error(), "HITL") {
		t.Fatalf("err = %v", err)
	}
}

func TestRunIssueSetHITLGatePrintsRecoveryAdvice(t *testing.T) {
	env := setupRunIssueSetFixture(t, "demo", []Issue{
		{ID: "01-a", File: "01-a.md", Title: "A", Type: "AFK", Status: "open"},
		{ID: "02-hitl", File: "02-hitl.md", Title: "Review", Type: "HITL", Status: "open"},
	})
	agent := writeFakeAgent(t, env.root, fakeAgentConfig{checkIssue: true, summary: "first done"})

	var buf bytes.Buffer
	_, err := RunIssueSetWith(env.deps(), nil, nil, env.runIssueSetOpts(true, agent, &buf))
	assertExitCode(t, err, ExitNoRunnable)

	out := buf.String()
	for _, want := range []string{
		"Human-blocked: demo/02-hitl",
		"pop workload complete-issue thoughts/issues/demo/02-hitl.md",
		"$EDITOR thoughts/issues/demo/02-hitl.md && pop workload run-issues",
		"pop workload skip-issue thoughts/issues/demo/02-hitl.md",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("advice missing %q:\n%s", want, out)
		}
	}
}

func TestRunIssueSetFailedStopMentionsCompleteAndReset(t *testing.T) {
	env := setupRunIssueSetFixture(t, "demo", []Issue{
		{ID: "01-a", File: "01-a.md", Title: "A", Type: "AFK", Status: "open"},
	})
	agent := writeSequentialFakeAgent(t, env.root, []fakeAgentStep{{exitCode: 1}})

	var buf bytes.Buffer
	opts := env.runIssueSetOpts(true, agent, &buf)
	opts.MaxTries = 1
	_, err := RunIssueSetWith(env.deps(), nil, nil, opts)
	assertExitCode(t, err, ExitOperational)

	out := buf.String()
	if !strings.Contains(out, "pop workload reset-issue thoughts/issues/demo/01-a.md") {
		t.Fatalf("advice missing reset hint:\n%s", out)
	}
	if !strings.Contains(out, "pop workload complete-issue thoughts/issues/demo/01-a.md") {
		t.Fatalf("advice missing complete hint:\n%s", out)
	}
}

func TestRunIssueSetHITLOnlyIssueSetRejectedAtSelection(t *testing.T) {
	env := setupRunIssueSetFixture(t, "demo", []Issue{
		{ID: "01-hitl", File: "01-hitl.md", Title: "Review", Type: "HITL", Status: "open"},
	})
	agent := writeFakeAgent(t, env.root, fakeAgentConfig{summary: "unused"})

	_, err := RunIssueSetWith(env.deps(), nil, nil, env.runIssueSetOpts(true, agent, nil))
	assertExitCode(t, err, ExitNoRunnable)
}

func TestRunIssueSetFailedIssueStopsDrain(t *testing.T) {
	env := setupRunIssueSetFixture(t, "demo", []Issue{
		{ID: "01-a", File: "01-a.md", Title: "A", Type: "AFK", Status: "open"},
		{ID: "02-b", File: "02-b.md", Title: "B", Type: "AFK", Status: "open"},
	})
	agent := writeSequentialFakeAgent(t, env.root, []fakeAgentStep{
		{summary: "ok"},
		{exitCode: 1},
	})

	opts := env.runIssueSetOpts(true, agent, nil)
	opts.MaxTries = 1
	_, err := RunIssueSetWith(env.deps(), nil, nil, opts)
	assertExitCode(t, err, ExitOperational)
	assertIssueDone(t, env.execFixture(), "01-a")
	assertIssueFailed(t, env.execFixture(), "02-b", 1)
}

func TestRunIssueSetClaudeQuotaPauseStopsCleanly(t *testing.T) {
	env := setupRunIssueSetFixture(t, "demo", []Issue{
		{ID: "01-a", File: "01-a.md", Title: "A", Type: "AFK", Status: "open"},
		{ID: "02-b", File: "02-b.md", Title: "B", Type: "AFK", Status: "open"},
	})
	counterPath := installClaudeQuotaAgent(t, env.root)
	var buf bytes.Buffer
	opts := env.runIssueSetOpts(true, "", &buf)
	opts.AgentPreset = "claude"

	result, err := RunIssueSetWith(env.deps(), nil, nil, opts)
	if err != nil {
		t.Fatal(err)
	}
	if !result.QuotaPaused || len(result.Completed) != 0 {
		t.Fatalf("result = %#v", result)
	}
	assertIssueOpen(t, env.execFixture(), "01-a")
	assertIssueOpen(t, env.execFixture(), "02-b")
	if got := strings.TrimSpace(string(mustReadFile(t, counterPath))); got != "1" {
		t.Fatalf("started attempts = %q, want 1", got)
	}
	if !strings.Contains(buf.String(), "Issue set demo paused") {
		t.Fatalf("missing pause summary:\n%s", buf.String())
	}
}

func TestRunIssueSetTimeoutPropagation(t *testing.T) {
	env := setupRunIssueSetFixture(t, "demo", []Issue{
		{ID: "01-a", File: "01-a.md", Title: "A", Type: "AFK", Status: "open"},
	})
	agent := writeFakeAgent(t, env.root, fakeAgentConfig{
		summary:  "slow",
		sleepFor: 200 * time.Millisecond,
	})

	opts := env.runIssueSetOpts(true, agent, nil)
	opts.Timeout = 50 * time.Millisecond
	opts.MaxTries = 1
	_, err := RunIssueSetWith(env.deps(), nil, nil, opts)
	assertExitCode(t, err, ExitOperational)
	assertIssueFailed(t, env.execFixture(), "01-a", 1)
}

func TestRunIssueSetOperationalStopOnCommitFailure(t *testing.T) {
	env := setupRunIssueSetFixture(t, "demo", []Issue{
		{ID: "01-a", File: "01-a.md", Title: "A", Type: "AFK", Status: "open"},
	})
	agent := writeFakeAgent(t, env.root, fakeAgentConfig{
		changeFile: "impl.txt",
		changeData: "x\n",
		checkIssue: true,
		summary:    "done",
	})
	git := &deps.MockGit{
		CommandInDirFunc: func(dir string, args ...string) (string, error) {
			if len(args) >= 2 && args[0] == "commit" && !strings.Contains(args[2], "capturing dirty state") {
				return "", fmt.Errorf("commit rejected")
			}
			return realGitInDir(dir, args...)
		},
	}
	d := env.deps()
	d.Git = git

	_, err := RunIssueSetWith(d, nil, nil, env.runIssueSetOpts(true, agent, nil))
	assertExitCode(t, err, ExitOperational)
	if !strings.Contains(err.Error(), "issue demo/01-a") {
		t.Fatalf("error missing issue reference: %v", err)
	}
	assertIssueOpen(t, env.execFixture(), "01-a")
}

func TestRunIssueSetDoesNotContinueIntoAnotherIssueSet(t *testing.T) {
	root := t.TempDir()
	initExecutorGitRepo(t, root)
	setupManifest(t, root, "one", []Issue{
		{ID: "01-a", File: "01-a.md", Title: "A", Type: "AFK", Status: "open"},
	})
	setupManifest(t, root, "two", []Issue{
		{ID: "01-x", File: "01-x.md", Title: "X", Type: "AFK", Status: "open"},
	})
	t.Setenv("XDG_DATA_HOME", filepath.Join(root, ".xdg"))
	if _, err := RefreshWith(DefaultDeps(), root, DefaultStatePath()); err != nil {
		t.Fatal(err)
	}
	if _, err := SetPriorityWith(DefaultDeps(), nil, nil, ResolveInput{CWD: root}, "two", 10); err != nil {
		t.Fatal(err)
	}

	agent := writeFakeAgent(t, root, fakeAgentConfig{checkIssue: true, summary: "one only"})
	env := &runIssueSetFixture{root: root}
	result, err := RunIssueSetWith(env.deps(), nil, nil, env.runIssueSetOpts(true, agent, nil))
	if err != nil {
		t.Fatal(err)
	}
	if !result.IssueSetDone || result.IssueSetID != "two" || len(result.Completed) != 1 {
		t.Fatalf("result = %#v", result)
	}
	assertIssueOpen(t, &execFixture{root: root}, "01-x")
}

func TestRunIssueSetFailedIssueSetRejected(t *testing.T) {
	env := setupRunIssueSetFixture(t, "demo", []Issue{
		{ID: "01-a", File: "01-a.md", Title: "A", Type: "AFK", Status: "failed", FailedAfter: intPtr(3)},
	})
	agent := writeFakeAgent(t, env.root, fakeAgentConfig{summary: "unused"})
	opts := env.runIssueSetOpts(true, agent, nil)
	opts.IssueSetOverride = "thoughts/issues/demo"

	_, err := RunIssueSetWith(env.deps(), nil, nil, opts)
	assertExitCode(t, err, ExitNoRunnable)
}

func TestRunIssueSetYesPrintsConciseSummary(t *testing.T) {
	env := setupRunIssueSetFixture(t, "demo", []Issue{
		{ID: "01-a", File: "01-a.md", Title: "A", Type: "AFK", Status: "open"},
	})
	agent := writeFakeAgent(t, env.root, fakeAgentConfig{checkIssue: true, summary: "ok"})

	var buf bytes.Buffer
	_, err := RunIssueSetWith(env.deps(), nil, nil, env.runIssueSetOpts(true, agent, &buf))
	if err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	for _, want := range []string{
		"━━ Running issue demo/01-a: A",
		"   Attempt 1/3",
		"── Agent output",
		"── Agent finished for demo/01-a",
		"✓ Completed demo/01-a",
		"✓ Completed Issue set demo",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("output missing %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "\033[") {
		t.Fatalf("redirected output contains ANSI:\n%q", out)
	}
	if !strings.Contains(out, "Completed demo/01-a") || !strings.Contains(out, "Completed Issue set demo") {
		t.Fatalf("missing concise summary:\n%s", out)
	}
	if strings.Count(out, "STATUS") != 1 {
		t.Fatalf("expected pre-run table only:\n%s", out)
	}
}

func TestRunIssueSetInteractivePrintsRefreshedTable(t *testing.T) {
	env := setupRunIssueSetFixture(t, "demo", []Issue{
		{ID: "01-a", File: "01-a.md", Title: "A", Type: "AFK", Status: "open"},
	})
	agent := writeFakeAgent(t, env.root, fakeAgentConfig{checkIssue: true, summary: "ok"})

	var buf bytes.Buffer
	opts := env.runIssueSetOpts(false, agent, &buf)
	opts.ConfirmIn = strings.NewReader("y\n")

	_, err := RunIssueSetWith(env.deps(), nil, nil, opts)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Count(buf.String(), "STATUS") < 2 {
		t.Fatalf("expected pre and post tables:\n%s", buf.String())
	}
}

func TestRunIssueSetDeclinedConfirmation(t *testing.T) {
	env := setupRunIssueSetFixture(t, "demo", []Issue{
		{ID: "01-a", File: "01-a.md", Title: "A", Type: "AFK", Status: "open"},
	})
	agent := writeFakeAgent(t, env.root, fakeAgentConfig{summary: "unused"})
	opts := env.runIssueSetOpts(false, agent, nil)
	opts.ConfirmIn = strings.NewReader("n\n")

	result, err := RunIssueSetWith(env.deps(), nil, nil, opts)
	if err != nil {
		t.Fatal(err)
	}
	if !result.Declined {
		t.Fatal("expected declined")
	}
}

func TestRunIssueSetInterruptionPropagation(t *testing.T) {
	env := setupRunIssueSetFixture(t, "demo", []Issue{
		{ID: "01-a", File: "01-a.md", Title: "A", Type: "AFK", Status: "open"},
	})
	agent := writeSlowAgent(t, env.root, 10*time.Second)

	opts := env.runIssueSetOpts(true, agent, nil)
	opts.Timeout = time.Minute
	go func() {
		time.Sleep(150 * time.Millisecond)
		_ = syscall.Kill(syscall.Getpid(), syscall.SIGTERM)
	}()

	_, err := RunIssueSetWith(env.deps(), nil, nil, opts)
	assertExitCode(t, err, ExitInterrupted)
	assertIssueOpen(t, env.execFixture(), "01-a")
}

func TestRunIssueSetStopsCleanlyOnDeferred(t *testing.T) {
	env := setupRunIssueSetFixture(t, "demo", []Issue{
		{ID: "01-a", File: "01-a.md", Title: "A", Type: "AFK", Status: "open"},
		{ID: "02-skip", File: "02-skip.md", Title: "Skip", Type: "HITL", Status: "skipped"},
	})
	agent := writeFakeAgent(t, env.root, fakeAgentConfig{checkIssue: true, summary: "ok"})

	var buf bytes.Buffer
	result, err := RunIssueSetWith(env.deps(), nil, nil, env.runIssueSetOpts(true, agent, &buf))
	if err != nil {
		t.Fatalf("run failed (deferred should not error): %v", err)
	}
	if !result.IssueSetDeferred {
		t.Fatalf("result = %#v, want IssueSetDeferred", result)
	}
	if result.IssueSetDone {
		t.Fatal("deferred set must not be reported as done")
	}
	if len(result.Completed) != 1 {
		t.Fatalf("completed = %d, want 1", len(result.Completed))
	}
	if len(result.SkippedIssues) != 1 || result.SkippedIssues[0] != "02-skip" {
		t.Fatalf("skipped issues = %v, want [02-skip]", result.SkippedIssues)
	}
	out := buf.String()
	if !strings.Contains(out, "deferred") || !strings.Contains(out, "02-skip") {
		t.Fatalf("missing deferral message:\n%s", out)
	}
	assertIssueDone(t, env.execFixture(), "01-a")
}

func TestSelectIssueSetAutomaticAndExplicit(t *testing.T) {
	refresh := &RefreshResult{
		Rows: []Row{
			{ID: "auto", Status: StatusReady, Priority: 10},
			{ID: "target", Status: StatusReady, Priority: 0},
		},
		Manifests: map[string]*Manifest{
			"auto": {Stem: "auto", Valid: true, Issues: []Issue{
				{ID: "01-a", File: "01-a.md", Type: "AFK", Status: "open"},
			}},
			"target": {Stem: "target", Valid: true, Issues: []Issue{
				{ID: "01-x", File: "01-x.md", Type: "AFK", Status: "open"},
			}},
		},
	}

	id, err := SelectIssueSet(refresh, "")
	if err != nil || id != "auto" {
		t.Fatalf("auto = %q, err = %v", id, err)
	}
	id, err = SelectIssueSet(refresh, "target")
	if err != nil || id != "target" {
		t.Fatalf("target = %q, err = %v", id, err)
	}
}

type runIssueSetFixture struct {
	root string
}

func setupRunIssueSetFixture(t *testing.T, stem string, issues []Issue) *runIssueSetFixture {
	t.Helper()
	root := t.TempDir()
	initExecutorGitRepo(t, root)
	setupManifest(t, root, stem, issues)
	t.Setenv("XDG_DATA_HOME", filepath.Join(root, ".xdg"))
	if _, err := RefreshWith(DefaultDeps(), root, DefaultStatePath()); err != nil {
		t.Fatal(err)
	}
	return &runIssueSetFixture{root: root}
}

func (e *runIssueSetFixture) deps() *Deps {
	return &Deps{
		FS:     deps.NewRealFileSystem(),
		Git:    deps.NewRealGit(),
		Runner: RealCommandRunner{},
	}
}

func (e *runIssueSetFixture) execFixture() *execFixture {
	return &execFixture{root: e.root}
}

func (e *runIssueSetFixture) runIssueSetOpts(yes bool, agentCmd string, out io.Writer) RunIssueSetOptions {
	opts := RunIssueSetOptions{
		ResolveInput: ResolveInput{CWD: e.root},
		AgentCmd:     agentCmd,
		Yes:          yes,
	}
	if out != nil {
		opts.Output = out
	}
	return opts
}

type fakeAgentStep struct {
	summary  string
	exitCode int
}

func writeSequentialFakeAgent(t *testing.T, root string, steps []fakeAgentStep) string {
	t.Helper()
	path := filepath.Join(root, ".agent", "seq-agent.sh")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	counterPath := filepath.Join(root, ".agent", "step.count")
	var b strings.Builder
	b.WriteString("#!/bin/sh\n")
	b.WriteString("COUNT=0\n")
	b.WriteString("if [ -f " + counterPath + " ]; then COUNT=$(cat " + counterPath + "); fi\n")
	b.WriteString("ISSUE=$(printf '%s' \"$1\" | sed -n 's|^You are implementing the issue at: ||p' | head -1)\n")
	b.WriteString("if [ -n \"$ISSUE\" ] && [ -f \"$ISSUE\" ]; then sed -i '' 's/- \\[ \\]/- [x]/g' \"$ISSUE\" 2>/dev/null || sed -i 's/- \\[ \\]/- [x]/g' \"$ISSUE\"; fi\n")
	for i, step := range steps {
		summary := step.summary
		if summary == "" {
			summary = "step"
		}
		exit := step.exitCode
		fmt.Fprintf(&b, "if [ \"$COUNT\" -eq %d ]; then\n", i)
		fmt.Fprintf(&b, "  echo %d > %q\n", i+1, counterPath)
		fmt.Fprintf(&b, "  printf 'SUMMARY_START\\n%s\\nSUMMARY_END\\nTASK_COMPLETE\\n' \"%s\"\n", summary, summary)
		if exit != 0 {
			fmt.Fprintf(&b, "  exit %d\n", exit)
		}
		b.WriteString("fi\n")
	}
	if err := os.WriteFile(path, []byte(b.String()), 0o755); err != nil {
		t.Fatal(err)
	}
	_ = os.WriteFile(counterPath, []byte("0"), 0o644)
	return path
}

func intPtr(v int) *int { return &v }
