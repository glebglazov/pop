package workload

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/glebglazov/pop/internal/deps"
)

func TestRunIssueHappyPathWithCommit(t *testing.T) {
	env := setupExecutorFixture(t, false)
	agent := writeFakeAgent(t, env.root, fakeAgentConfig{
		changeFile: "impl.txt",
		changeData: "implemented\n",
		checkIssue: true,
		summary:    "implemented feature",
	})

	d := env.deps()
	opts := env.runOpts(true, agent)
	opts.AgentCmd = agent

	result, err := RunIssueWith(d, nil, nil, opts)
	if err != nil {
		t.Fatalf("run failed: %v", err)
	}
	if result.NoOp || result.CommitSHA == "" {
		t.Fatalf("expected commit, got %#v", result)
	}

	assertIssueDone(t, env, "01-a")
	assertProgressContains(t, env, "DONE", "implemented feature")

	out, _ := env.deps().Git.CommandInDir(env.root, "log", "-1", "--format=%s")
	if !strings.Contains(out, "workload(demo): 01-a") {
		t.Fatalf("commit subject = %q", out)
	}
}

func TestRunIssueNoOpCompletion(t *testing.T) {
	env := setupExecutorFixture(t, false)
	agent := writeFakeAgent(t, env.root, fakeAgentConfig{
		checkIssue: true,
		summary:    "verified only",
	})

	d := env.deps()
	result, err := RunIssueWith(d, nil, nil, env.runOpts(true, agent))
	if err != nil {
		t.Fatal(err)
	}
	if !result.NoOp {
		t.Fatalf("expected no-op, got %#v", result)
	}
	assertIssueDone(t, env, "01-a")
}

func TestRunIssueDeclinedConfirmation(t *testing.T) {
	env := setupExecutorFixture(t, false)
	agent := writeFakeAgent(t, env.root, fakeAgentConfig{summary: "unused"})

	d := env.deps()
	opts := env.runOpts(false, agent)
	opts.ConfirmIn = strings.NewReader("n\n")

	result, err := RunIssueWith(d, nil, nil, opts)
	if err != nil {
		t.Fatal(err)
	}
	if !result.Declined {
		t.Fatal("expected declined")
	}
	assertIssueOpen(t, env, "01-a")
}

func TestRunIssueNonInteractiveRefusal(t *testing.T) {
	env := setupExecutorFixture(t, false)
	agent := writeFakeAgent(t, env.root, fakeAgentConfig{summary: "unused"})

	d := env.deps()
	opts := env.runOpts(false, agent)
	opts.ConfirmIn = NonInteractiveReader{}

	_, err := RunIssueWith(d, nil, nil, opts)
	assertExitCode(t, err, ExitOperational)
}

func TestRunIssueAgentFailureExitCode(t *testing.T) {
	env := setupExecutorFixture(t, false)
	agent := writeFakeAgent(t, env.root, fakeAgentConfig{exitCode: 1})

	d := env.deps()
	_, err := RunIssueWith(d, nil, nil, env.runOpts(true, agent))
	assertExitCode(t, err, ExitOperational)
}

func TestRunIssueMissingSentinelFails(t *testing.T) {
	env := setupExecutorFixture(t, false)
	agent := writeFakeAgent(t, env.root, fakeAgentConfig{
		changeFile: "impl.txt",
		changeData: "x\n",
		checkIssue: true,
		skipSentinel: true,
	})

	d := env.deps()
	_, err := RunIssueWith(d, nil, nil, env.runOpts(true, agent))
	assertExitCode(t, err, ExitOperational)
}

func TestRunIssueCommitFailurePreservesOpenIssue(t *testing.T) {
	env := setupExecutorFixture(t, false)
	agent := writeFakeAgent(t, env.root, fakeAgentConfig{
		changeFile: "impl.txt",
		changeData: "x\n",
		checkIssue: true,
		summary:    "work done",
	})

	git := &deps.MockGit{
		CommandInDirFunc: func(dir string, args ...string) (string, error) {
			if len(args) >= 2 && args[0] == "commit" {
				return "", fmt.Errorf("commit rejected")
			}
			return realGitInDir(dir, args...)
		},
	}
	d := env.deps()
	d.Git = git

	_, err := RunIssueWith(d, nil, nil, env.runOpts(true, agent))
	assertExitCode(t, err, ExitOperational)
	assertIssueOpen(t, env, "01-a")
}

func TestRunIssueYesPrintsConciseSummary(t *testing.T) {
	env := setupExecutorFixture(t, false)
	agent := writeFakeAgent(t, env.root, fakeAgentConfig{
		checkIssue: true,
		summary:    "done work",
	})

	var buf bytes.Buffer
	d := env.deps()
	opts := env.runOpts(true, agent)
	opts.Output = &buf

	_, err := RunIssueWith(d, nil, nil, opts)
	if err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	if !strings.Contains(out, "Completed demo/01-a") {
		t.Fatalf("missing concise summary:\n%s", out)
	}
	if strings.Count(out, "STATUS") != 1 {
		t.Fatalf("expected pre-run table only, got %d tables:\n%s", strings.Count(out, "STATUS"), out)
	}
}

func TestRunIssueInteractivePrintsRefreshedTable(t *testing.T) {
	env := setupExecutorFixture(t, true)
	agent := writeFakeAgent(t, env.root, fakeAgentConfig{
		checkIssue: true,
		summary:    "done work",
	})

	var buf bytes.Buffer
	d := env.deps()
	opts := env.runOpts(false, agent)
	opts.ConfirmIn = strings.NewReader("y\n")
	opts.Output = &buf

	_, err := RunIssueWith(d, nil, nil, opts)
	if err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	if !strings.Contains(out, "AUTO RUN") {
		t.Fatalf("missing pre-run marker:\n%s", out)
	}
	if strings.Count(out, "STATUS") < 2 {
		t.Fatalf("expected pre and post tables:\n%s", out)
	}
}

func TestRunIssueNoRunnableWork(t *testing.T) {
	env := setupExecutorFixture(t, false)
	manifestPath := filepath.Join(env.root, "thoughts/issues/demo/index.json")
	data, _ := os.ReadFile(manifestPath)
	var payload map[string]any
	_ = json.Unmarshal(data, &payload)
	payload["issues"] = []map[string]any{
		{"id": "01-a", "file": "01-a.md", "title": "A", "type": "AFK", "status": "done"},
	}
	updated, _ := json.MarshalIndent(payload, "", "  ")
	_ = os.WriteFile(manifestPath, updated, 0o644)

	agent := writeFakeAgent(t, env.root, fakeAgentConfig{summary: "unused"})
	_, err := RunIssueWith(env.deps(), nil, nil, env.runOpts(true, agent))
	assertExitCode(t, err, ExitNoRunnable)
}

func TestRunIssueBookkeepingOrder(t *testing.T) {
	env := setupExecutorFixture(t, false)
	agent := writeFakeAgent(t, env.root, fakeAgentConfig{
		checkIssue: true,
		summary:    "ordered bookkeeping",
	})

	_, err := RunIssueWith(env.deps(), nil, nil, env.runOpts(true, agent))
	if err != nil {
		t.Fatal(err)
	}

	progress, err := os.ReadFile(filepath.Join(env.root, "thoughts/issues/demo/progress.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(progress), "ordered bookkeeping") {
		t.Fatalf("missing progress:\n%s", progress)
	}
	assertIssueDone(t, env, "01-a")
}

type execFixture struct {
	root string
}

type fakeAgentConfig struct {
	changeFile   string
	changeData   string
	checkIssue   bool
	summary      string
	exitCode     int
	skipSentinel bool
}

func setupExecutorFixture(t *testing.T, interactive bool) *execFixture {
	t.Helper()
	root := t.TempDir()
	initExecutorGitRepo(t, root)
	writeFile(t, filepath.Join(root, "thoughts/prds/demo.md"), "# Demo\n")
	setupManifest(t, root, "demo", []Issue{
		{ID: "01-a", File: "01-a.md", Title: "A", Type: "AFK", Status: "open"},
	})
	t.Setenv("XDG_DATA_HOME", filepath.Join(root, ".xdg"))
	if _, err := RefreshWith(DefaultDeps(), root, DefaultStatePath()); err != nil {
		t.Fatal(err)
	}
	_ = interactive
	return &execFixture{root: root}
}

func (e *execFixture) deps() *Deps {
	return &Deps{
		FS:     deps.NewRealFileSystem(),
		Git:    deps.NewRealGit(),
		Runner: RealCommandRunner{},
	}
}

func (e *execFixture) runOpts(yes bool, agentCmd string) RunIssueOptions {
	return RunIssueOptions{
		ResolveInput: ResolveInput{CWD: e.root},
		AgentCmd:     agentCmd,
		Yes:          yes,
	}
}

func writeFakeAgent(t *testing.T, root string, cfg fakeAgentConfig) string {
	t.Helper()
	path := filepath.Join(root, ".agent", "fake-agent.sh")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	var b strings.Builder
	b.WriteString("#!/bin/sh\n")
	b.WriteString("ISSUE=$(printf '%s' \"$1\" | sed -n 's|^You are implementing the issue at: ||p' | head -1)\n")
	if cfg.changeFile != "" {
		fmt.Fprintf(&b, "printf %q >> %q\n", cfg.changeData, cfg.changeFile)
	}
	if cfg.checkIssue {
		b.WriteString("if [ -n \"$ISSUE\" ] && [ -f \"$ISSUE\" ]; then\n")
		b.WriteString("  sed -i '' 's/- \\[ \\]/- [x]/g' \"$ISSUE\" 2>/dev/null || sed -i 's/- \\[ \\]/- [x]/g' \"$ISSUE\"\n")
		b.WriteString("fi\n")
	}
	if cfg.summary == "" {
		cfg.summary = "work complete"
	}
	if !cfg.skipSentinel {
		fmt.Fprintf(&b, "printf 'SUMMARY_START\\n%s\\nSUMMARY_END\\nTASK_COMPLETE\\n' \"%s\"\n", cfg.summary, cfg.summary)
	} else {
		b.WriteString("echo incomplete\n")
	}
	if cfg.exitCode != 0 {
		fmt.Fprintf(&b, "exit %d\n", cfg.exitCode)
	}
	if err := os.WriteFile(path, []byte(b.String()), 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}

func initExecutorGitRepo(t *testing.T, root string) {
	t.Helper()
	runGit(t, root, "init")
	runGit(t, root, "config", "user.email", "test@test")
	runGit(t, root, "config", "user.name", "test")
	writeFile(t, filepath.Join(root, ".gitignore"), "thoughts/\n.agent/\n.xdg/\n")
	writeFile(t, filepath.Join(root, "README.md"), "# test\n")
	runGit(t, root, "add", "-A")
	runGit(t, root, "commit", "-m", "init")
}

func realGitInDir(dir string, args ...string) (string, error) {
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

func assertIssueDone(t *testing.T, env *execFixture, issueID string) {
	t.Helper()
	m := LoadManifest(DefaultDeps(), "demo", filepath.Join(env.root, "thoughts/issues/demo/index.json"))
	for _, issue := range m.Issues {
		if issue.ID == issueID && issue.Status != "done" {
			t.Fatalf("issue %s status = %q", issueID, issue.Status)
		}
	}
}

func assertIssueOpen(t *testing.T, env *execFixture, issueID string) {
	t.Helper()
	m := LoadManifest(DefaultDeps(), "demo", filepath.Join(env.root, "thoughts/issues/demo/index.json"))
	for _, issue := range m.Issues {
		if issue.ID == issueID && issue.Status != "open" {
			t.Fatalf("issue %s status = %q, want open", issueID, issue.Status)
		}
	}
}

func assertProgressContains(t *testing.T, env *execFixture, parts ...string) {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(env.root, "thoughts/issues/demo/progress.txt"))
	if err != nil {
		t.Fatal(err)
	}
	for _, part := range parts {
		if !strings.Contains(string(data), part) {
			t.Fatalf("progress missing %q:\n%s", part, data)
		}
	}
}

func assertExitCode(t *testing.T, err error, code int) {
	t.Helper()
	if err == nil {
		t.Fatalf("expected exit %d", code)
	}
	ee, ok := err.(*ExitError)
	if !ok || ee.Code != code {
		t.Fatalf("err = %v, want code %d", err, code)
	}
}
