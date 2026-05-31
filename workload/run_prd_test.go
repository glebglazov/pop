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

func TestRunPRDDrainsMultipleAFKIssuesInOrder(t *testing.T) {
	env := setupRunPRDFixture(t, "demo", []Issue{
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
	result, err := RunPRDWith(env.deps(), nil, nil, env.runPRDOpts(true, agent, &buf))
	if err != nil {
		t.Fatalf("run failed: %v", err)
	}
	if !result.PRDDone || len(result.Completed) != 2 {
		t.Fatalf("result = %#v", result)
	}
	if result.Completed[0].Selection.IssueID != "01-a" || result.Completed[1].Selection.IssueID != "02-b" {
		t.Fatalf("issue order = %s, %s", result.Completed[0].Selection.IssueID, result.Completed[1].Selection.IssueID)
	}
	assertIssueDone(t, env.execFixture(), "01-a")
	assertIssueDone(t, env.execFixture(), "02-b")
}

func TestRunPRDSequentialDependencyUnblocking(t *testing.T) {
	env := setupRunPRDFixture(t, "demo", []Issue{
		{ID: "01-a", File: "01-a.md", Title: "A", Type: "AFK", Status: "open"},
		{ID: "02-b", File: "02-b.md", Title: "B", Type: "AFK", Status: "open", BlockedBy: []string{"01-a"}},
	})
	agent := writeFakeAgent(t, env.root, fakeAgentConfig{checkIssue: true, summary: "ok"})

	result, err := RunPRDWith(env.deps(), nil, nil, env.runPRDOpts(true, agent, nil))
	if err != nil {
		t.Fatalf("run failed: %v", err)
	}
	if len(result.Completed) != 2 {
		t.Fatalf("completed = %d", len(result.Completed))
	}
	assertIssueDone(t, env.execFixture(), "02-b")
}

func TestRunPRDNoOpContinuation(t *testing.T) {
	env := setupRunPRDFixture(t, "demo", []Issue{
		{ID: "01-a", File: "01-a.md", Title: "A", Type: "AFK", Status: "open"},
		{ID: "02-b", File: "02-b.md", Title: "B", Type: "AFK", Status: "open"},
	})
	agent := writeFakeAgent(t, env.root, fakeAgentConfig{checkIssue: true, summary: "verified"})

	result, err := RunPRDWith(env.deps(), nil, nil, env.runPRDOpts(true, agent, nil))
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Completed) != 2 || !result.Completed[0].NoOp {
		t.Fatalf("result = %#v", result)
	}
}

func TestRunPRDSingleConfirmation(t *testing.T) {
	env := setupRunPRDFixture(t, "demo", []Issue{
		{ID: "01-a", File: "01-a.md", Title: "A", Type: "AFK", Status: "open"},
		{ID: "02-b", File: "02-b.md", Title: "B", Type: "AFK", Status: "open"},
	})
	agent := writeFakeAgent(t, env.root, fakeAgentConfig{checkIssue: true, summary: "ok"})

	var confirmOut bytes.Buffer
	opts := env.runPRDOpts(false, agent, nil)
	opts.ConfirmIn = strings.NewReader("y\n")
	opts.ConfirmOut = &confirmOut

	_, err := RunPRDWith(env.deps(), nil, nil, opts)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Count(confirmOut.String(), "Run PRD?") != 1 {
		t.Fatalf("expected one confirmation prompt:\n%s", confirmOut.String())
	}
}

func TestRunPRDTargetedPRD(t *testing.T) {
	root := t.TempDir()
	initExecutorGitRepo(t, root)
	writeFile(t, filepath.Join(root, "thoughts/prds/high.md"), "# High\n")
	writeFile(t, filepath.Join(root, "thoughts/prds/low.md"), "# Low\n")
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
	env := &runPRDFixture{root: root}
	opts := env.runPRDOpts(true, agent, nil)
	opts.PRDOverride = "high"

	result, err := RunPRDWith(env.deps(), nil, nil, opts)
	if err != nil {
		t.Fatal(err)
	}
	if result.PRDID != "high" || len(result.Completed) != 1 || result.Completed[0].Selection.IssueID != "01-a" {
		t.Fatalf("result = %#v", result)
	}
}

func TestRunPRDBlockedStopsWithReason(t *testing.T) {
	env := setupRunPRDFixture(t, "demo", []Issue{
		{ID: "01-a", File: "01-a.md", Title: "A", Type: "AFK", Status: "open"},
		{ID: "02-hitl", File: "02-hitl.md", Title: "Review", Type: "HITL", Status: "open"},
	})
	agent := writeFakeAgent(t, env.root, fakeAgentConfig{checkIssue: true, summary: "first done"})

	_, err := RunPRDWith(env.deps(), nil, nil, env.runPRDOpts(true, agent, nil))
	assertExitCode(t, err, ExitNoRunnable)
	if !strings.Contains(err.Error(), "HITL") {
		t.Fatalf("err = %v", err)
	}
}

func TestRunPRDHITLOnlyPRDRejectedAtSelection(t *testing.T) {
	env := setupRunPRDFixture(t, "demo", []Issue{
		{ID: "01-hitl", File: "01-hitl.md", Title: "Review", Type: "HITL", Status: "open"},
	})
	agent := writeFakeAgent(t, env.root, fakeAgentConfig{summary: "unused"})

	_, err := RunPRDWith(env.deps(), nil, nil, env.runPRDOpts(true, agent, nil))
	assertExitCode(t, err, ExitNoRunnable)
}

func TestRunPRDFailedIssueStopsDrain(t *testing.T) {
	env := setupRunPRDFixture(t, "demo", []Issue{
		{ID: "01-a", File: "01-a.md", Title: "A", Type: "AFK", Status: "open"},
		{ID: "02-b", File: "02-b.md", Title: "B", Type: "AFK", Status: "open"},
	})
	agent := writeSequentialFakeAgent(t, env.root, []fakeAgentStep{
		{summary: "ok"},
		{exitCode: 1},
	})

	opts := env.runPRDOpts(true, agent, nil)
	opts.MaxTries = 1
	_, err := RunPRDWith(env.deps(), nil, nil, opts)
	assertExitCode(t, err, ExitOperational)
	assertIssueDone(t, env.execFixture(), "01-a")
	assertIssueFailed(t, env.execFixture(), "02-b", 1)
}

func TestRunPRDTimeoutPropagation(t *testing.T) {
	env := setupRunPRDFixture(t, "demo", []Issue{
		{ID: "01-a", File: "01-a.md", Title: "A", Type: "AFK", Status: "open"},
	})
	agent := writeFakeAgent(t, env.root, fakeAgentConfig{
		summary:  "slow",
		sleepFor: 200 * time.Millisecond,
	})

	opts := env.runPRDOpts(true, agent, nil)
	opts.Timeout = 50 * time.Millisecond
	opts.MaxTries = 1
	_, err := RunPRDWith(env.deps(), nil, nil, opts)
	assertExitCode(t, err, ExitOperational)
	assertIssueFailed(t, env.execFixture(), "01-a", 1)
}

func TestRunPRDOperationalStopOnCommitFailure(t *testing.T) {
	env := setupRunPRDFixture(t, "demo", []Issue{
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

	_, err := RunPRDWith(d, nil, nil, env.runPRDOpts(true, agent, nil))
	assertExitCode(t, err, ExitOperational)
	assertIssueOpen(t, env.execFixture(), "01-a")
}

func TestRunPRDDoesNotContinueIntoAnotherPRD(t *testing.T) {
	root := t.TempDir()
	initExecutorGitRepo(t, root)
	writeFile(t, filepath.Join(root, "thoughts/prds/one.md"), "# One\n")
	writeFile(t, filepath.Join(root, "thoughts/prds/two.md"), "# Two\n")
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
	env := &runPRDFixture{root: root}
	result, err := RunPRDWith(env.deps(), nil, nil, env.runPRDOpts(true, agent, nil))
	if err != nil {
		t.Fatal(err)
	}
	if !result.PRDDone || result.PRDID != "two" || len(result.Completed) != 1 {
		t.Fatalf("result = %#v", result)
	}
	assertIssueOpen(t, &execFixture{root: root}, "01-x")
}

func TestRunPRDFailedPRDRejected(t *testing.T) {
	env := setupRunPRDFixture(t, "demo", []Issue{
		{ID: "01-a", File: "01-a.md", Title: "A", Type: "AFK", Status: "failed", FailedAfter: intPtr(3)},
	})
	agent := writeFakeAgent(t, env.root, fakeAgentConfig{summary: "unused"})
	opts := env.runPRDOpts(true, agent, nil)
	opts.PRDOverride = "demo"

	_, err := RunPRDWith(env.deps(), nil, nil, opts)
	assertExitCode(t, err, ExitNoRunnable)
}

func TestRunPRDYesPrintsConciseSummary(t *testing.T) {
	env := setupRunPRDFixture(t, "demo", []Issue{
		{ID: "01-a", File: "01-a.md", Title: "A", Type: "AFK", Status: "open"},
	})
	agent := writeFakeAgent(t, env.root, fakeAgentConfig{checkIssue: true, summary: "ok"})

	var buf bytes.Buffer
	_, err := RunPRDWith(env.deps(), nil, nil, env.runPRDOpts(true, agent, &buf))
	if err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	if !strings.Contains(out, "Completed demo/01-a") || !strings.Contains(out, "Completed PRD demo") {
		t.Fatalf("missing concise summary:\n%s", out)
	}
	if strings.Count(out, "STATUS") != 1 {
		t.Fatalf("expected pre-run table only:\n%s", out)
	}
}

func TestRunPRDInteractivePrintsRefreshedTable(t *testing.T) {
	env := setupRunPRDFixture(t, "demo", []Issue{
		{ID: "01-a", File: "01-a.md", Title: "A", Type: "AFK", Status: "open"},
	})
	agent := writeFakeAgent(t, env.root, fakeAgentConfig{checkIssue: true, summary: "ok"})

	var buf bytes.Buffer
	opts := env.runPRDOpts(false, agent, &buf)
	opts.ConfirmIn = strings.NewReader("y\n")

	_, err := RunPRDWith(env.deps(), nil, nil, opts)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Count(buf.String(), "STATUS") < 2 {
		t.Fatalf("expected pre and post tables:\n%s", buf.String())
	}
}

func TestRunPRDDeclinedConfirmation(t *testing.T) {
	env := setupRunPRDFixture(t, "demo", []Issue{
		{ID: "01-a", File: "01-a.md", Title: "A", Type: "AFK", Status: "open"},
	})
	agent := writeFakeAgent(t, env.root, fakeAgentConfig{summary: "unused"})
	opts := env.runPRDOpts(false, agent, nil)
	opts.ConfirmIn = strings.NewReader("n\n")

	result, err := RunPRDWith(env.deps(), nil, nil, opts)
	if err != nil {
		t.Fatal(err)
	}
	if !result.Declined {
		t.Fatal("expected declined")
	}
}

func TestRunPRDInterruptionPropagation(t *testing.T) {
	env := setupRunPRDFixture(t, "demo", []Issue{
		{ID: "01-a", File: "01-a.md", Title: "A", Type: "AFK", Status: "open"},
	})
	agent := writeSlowAgent(t, env.root, 10*time.Second)

	opts := env.runPRDOpts(true, agent, nil)
	opts.Timeout = time.Minute
	go func() {
		time.Sleep(150 * time.Millisecond)
		_ = syscall.Kill(syscall.Getpid(), syscall.SIGTERM)
	}()

	_, err := RunPRDWith(env.deps(), nil, nil, opts)
	assertExitCode(t, err, ExitInterrupted)
	assertIssueOpen(t, env.execFixture(), "01-a")
}

func TestSelectPRDAutomaticAndExplicit(t *testing.T) {
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

	id, err := SelectPRD(refresh, "")
	if err != nil || id != "auto" {
		t.Fatalf("auto = %q, err = %v", id, err)
	}
	id, err = SelectPRD(refresh, "target")
	if err != nil || id != "target" {
		t.Fatalf("target = %q, err = %v", id, err)
	}
}

type runPRDFixture struct {
	root string
}

func setupRunPRDFixture(t *testing.T, stem string, issues []Issue) *runPRDFixture {
	t.Helper()
	root := t.TempDir()
	initExecutorGitRepo(t, root)
	writeFile(t, filepath.Join(root, "thoughts/prds", stem+".md"), "# Demo\n")
	setupManifest(t, root, stem, issues)
	t.Setenv("XDG_DATA_HOME", filepath.Join(root, ".xdg"))
	if _, err := RefreshWith(DefaultDeps(), root, DefaultStatePath()); err != nil {
		t.Fatal(err)
	}
	return &runPRDFixture{root: root}
}

func (e *runPRDFixture) deps() *Deps {
	return &Deps{
		FS:     deps.NewRealFileSystem(),
		Git:    deps.NewRealGit(),
		Runner: RealCommandRunner{},
	}
}

func (e *runPRDFixture) execFixture() *execFixture {
	return &execFixture{root: e.root}
}

func (e *runPRDFixture) runPRDOpts(yes bool, agentCmd string, out io.Writer) RunPRDOptions {
	opts := RunPRDOptions{
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
