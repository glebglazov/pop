package tasks

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/glebglazov/pop/config"
	"github.com/glebglazov/pop/internal/deps"
	"github.com/glebglazov/pop/store"
)

func TestRunTaskHappyPathWithCommit(t *testing.T) {
	env := setupExecutorFixture(t, false)
	agent := writeFakeAgent(t, env.root, fakeAgentConfig{
		changeFile: "impl.txt",
		changeData: "implemented\n",
		checkTask:  true,
		summary:    "implemented feature",
	})

	d := env.deps()
	opts := env.runOpts(true, agent)
	opts.AgentCmd = agent

	result, err := RunTaskWith(d, nil, nil, opts)
	if err != nil {
		t.Fatalf("run failed: %v", err)
	}
	if result.NoOp || result.CommitSHA == "" {
		t.Fatalf("expected commit, got %#v", result)
	}

	assertTaskDone(t, env, "01-a")
	assertProgressContains(t, env, "DONE", "implemented feature")

	out, _ := env.deps().Git.CommandInDir(env.root, "log", "-1", "--format=%s")
	if !strings.Contains(out, "tasks(demo): 01-a") {
		t.Fatalf("commit subject = %q", out)
	}
}

func TestRunTaskNoOpCompletion(t *testing.T) {
	env := setupExecutorFixture(t, false)
	agent := writeFakeAgent(t, env.root, fakeAgentConfig{
		checkTask: true,
		summary:   "verified only",
	})

	d := env.deps()
	result, err := RunTaskWith(d, nil, nil, env.runOpts(true, agent))
	if err != nil {
		t.Fatal(err)
	}
	if !result.NoOp {
		t.Fatalf("expected no-op, got %#v", result)
	}
	assertTaskDone(t, env, "01-a")
}

func TestRunTaskTargetsEligibleTaskPath(t *testing.T) {
	env := setupExecutorFixture(t, false)
	setupManifest(t, env.tasksDir, "target", []Task{
		{ID: "02-b", File: "02-b.md", Title: "B", Type: "AFK", Status: "open"},
	})
	if _, err := RegisterWith(DefaultDeps(), env.tasksDir, DefaultStatePath()); err != nil {
		t.Fatal(err)
	}
	agent := writeFakeAgent(t, env.root, fakeAgentConfig{
		checkTask: true,
		summary:   "targeted task",
	})

	opts := env.runOpts(true, agent)
	opts.TaskPathOverride = "target/02-b.md"
	result, err := RunTaskWith(env.deps(), nil, nil, opts)
	if err != nil {
		t.Fatal(err)
	}
	if result.Selection.TaskSetID != "target" || result.Selection.TaskID != "02-b" {
		t.Fatalf("selection = %s/%s", result.Selection.TaskSetID, result.Selection.TaskID)
	}
}

func TestRunTaskRejectsBareManifestID(t *testing.T) {
	env := setupExecutorFixture(t, false)
	opts := env.runOpts(true, "")
	opts.TaskPathOverride = "01-a"

	_, err := RunTaskWith(env.deps(), nil, nil, opts)
	if err == nil || !strings.Contains(err.Error(), "valid: demo") {
		t.Fatalf("err = %v", err)
	}
}

func TestRunTaskDeclinedConfirmation(t *testing.T) {
	env := setupExecutorFixture(t, false)
	agent := writeFakeAgent(t, env.root, fakeAgentConfig{summary: "unused"})

	d := env.deps()
	opts := env.runOpts(false, agent)
	opts.ConfirmIn = strings.NewReader("n\n")

	result, err := RunTaskWith(d, nil, nil, opts)
	if err != nil {
		t.Fatal(err)
	}
	if !result.Declined {
		t.Fatal("expected declined")
	}
	assertTaskOpen(t, env, "01-a")
}

func TestRunTaskDeclinedConfirmationReleasesEarlyRuntimeLockAndWritesNoDrainOutcome(t *testing.T) {
	env := setupExecutorFixture(t, false)
	agent := writeFakeAgent(t, env.root, fakeAgentConfig{summary: "unused"})

	d := env.deps()
	d.ProcessAlive = func(pid int) bool { return pid == os.Getpid() }
	runtimePath, err := ResolveRuntimePathWith(d, env.root, "")
	if err != nil {
		t.Fatal(err)
	}
	opts := env.runOpts(false, agent)
	opts.ConfirmIn = strings.NewReader("n\n")

	result, err := RunTaskWith(d, nil, nil, opts)
	if err != nil {
		t.Fatal(err)
	}
	if !result.Declined {
		t.Fatal("expected declined")
	}

	status := ReadRuntimeLockStatus(d, runtimePath)
	if status.Locked {
		t.Fatalf("runtime lock leaked after declined run: %#v", status)
	}
	if rec := latestTerminalDrain(t, d, runtimePath); rec != nil {
		t.Fatalf("declined single-task run recorded a drain terminal: %#v", rec)
	}
	assertTaskOpen(t, env, "01-a")
}

func TestRunTaskNonInteractiveRefusal(t *testing.T) {
	env := setupExecutorFixture(t, false)
	agent := writeFakeAgent(t, env.root, fakeAgentConfig{summary: "unused"})

	d := env.deps()
	opts := env.runOpts(false, agent)
	opts.ConfirmIn = NonInteractiveReader{}

	_, err := RunTaskWith(d, nil, nil, opts)
	assertExitCode(t, err, ExitOperational)
}

func TestRunTaskPiWeeklyQuotaPause(t *testing.T) {
	env := setupExecutorFixture(t, false)
	quotaLine := "429 Weekly usage limit reached. Resets in 9hr 4min. To continue using this model now, enable usage from your available balance."
	writeFakePiAgent(t, env.root, quotaLine)
	t.Setenv("PATH", filepath.Join(env.root, ".agent")+":"+os.Getenv("PATH"))

	d := env.deps()
	opts := env.runOpts(true, "")
	opts.AgentPreset = "pi"
	opts.MaxTries = 3
	opts.Output = io.Discard

	result, err := RunTaskWith(d, nil, nil, opts)
	if err != nil {
		t.Fatalf("run failed: %v", err)
	}
	if !result.QuotaPaused {
		t.Fatalf("expected quota pause, got %#v", result)
	}
	if result.PausePreset != "pi" {
		t.Fatalf("pause preset = %q, want pi", result.PausePreset)
	}
	if result.PauseResetAt.IsZero() {
		t.Fatal("expected non-zero PauseResetAt")
	}
	wantReset := time.Now().Add(9*time.Hour + 4*time.Minute + opencodeGoQuotaAssuranceOffset)
	if result.PauseResetAt.Before(wantReset.Add(-time.Minute)) || result.PauseResetAt.After(wantReset.Add(time.Minute)) {
		t.Fatalf("PauseResetAt = %s, want near %s", result.PauseResetAt, wantReset)
	}
	if !strings.Contains(strings.ToLower(result.PauseReason), "weekly usage limit reached") {
		t.Fatalf("pause reason = %q, want weekly quota diagnostic", result.PauseReason)
	}
}

func TestRunTaskPiQuotaPause(t *testing.T) {
	env := setupExecutorFixture(t, false)
	quotaLine := "429 5-hour usage limit reached. Resets in 7min. Upgrade to continue."
	writeFakePiAgent(t, env.root, quotaLine)
	t.Setenv("PATH", filepath.Join(env.root, ".agent")+":"+os.Getenv("PATH"))

	d := env.deps()
	opts := env.runOpts(true, "")
	opts.AgentPreset = "pi"
	opts.MaxTries = 3
	opts.Output = io.Discard

	result, err := RunTaskWith(d, nil, nil, opts)
	if err != nil {
		t.Fatalf("run failed: %v", err)
	}
	if !result.QuotaPaused {
		t.Fatalf("expected quota pause, got %#v", result)
	}
	if result.PausePreset != "pi" {
		t.Fatalf("pause preset = %q, want pi", result.PausePreset)
	}
	if result.PauseResetAt.IsZero() {
		t.Fatal("expected non-zero PauseResetAt")
	}
	wantReset := time.Now().Add(7*time.Minute + opencodeGoQuotaAssuranceOffset)
	if result.PauseResetAt.Before(wantReset.Add(-time.Minute)) || result.PauseResetAt.After(wantReset.Add(time.Minute)) {
		t.Fatalf("PauseResetAt = %s, want near %s", result.PauseResetAt, wantReset)
	}

	assertTaskOpen(t, env, "01-a")

	progressPath := filepath.Join(env.demoDir(), "progress.txt")
	if _, statErr := os.Stat(progressPath); !os.IsNotExist(statErr) {
		t.Fatalf("progress.txt should not exist on quota pause")
	}

	runs, err := listSetRuns(d, env.demoDir())
	if err != nil {
		t.Fatalf("list runs: %v", err)
	}
	if len(runs) != 1 {
		t.Fatalf("expected exactly one captured run, got %d", len(runs))
	}
	if runs[0].meta.Outcome != streamOutcomeQuotaPaused {
		t.Fatalf("run outcome = %q, want quota_paused", runs[0].meta.Outcome)
	}
	if runs[0].meta.Attempt != 1 {
		t.Fatalf("run attempt = %d, want 1", runs[0].meta.Attempt)
	}
}

func TestRunTaskAgentFailureExitCode(t *testing.T) {
	env := setupExecutorFixture(t, false)
	agent := writeFakeAgent(t, env.root, fakeAgentConfig{exitCode: 1})

	d := env.deps()
	opts := env.runOpts(true, agent)
	opts.MaxTries = 1
	_, err := RunTaskWith(d, nil, nil, opts)
	assertExitCode(t, err, ExitOperational)
	assertTaskFailed(t, env, "01-a", 1)
}

func TestRunTaskMissingSentinelFails(t *testing.T) {
	env := setupExecutorFixture(t, false)
	agent := writeFakeAgent(t, env.root, fakeAgentConfig{
		changeFile:   "impl.txt",
		changeData:   "x\n",
		checkTask:    true,
		skipSentinel: true,
	})

	d := env.deps()
	opts := env.runOpts(true, agent)
	opts.MaxTries = 1
	_, err := RunTaskWith(d, nil, nil, opts)
	assertExitCode(t, err, ExitOperational)
	assertTaskFailed(t, env, "01-a", 1)
}

func TestRunTaskCommitFailurePreservesOpenTask(t *testing.T) {
	env := setupExecutorFixture(t, false)
	agent := writeFakeAgent(t, env.root, fakeAgentConfig{
		changeFile: "impl.txt",
		changeData: "x\n",
		checkTask:  true,
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

	_, err := RunTaskWith(d, nil, nil, env.runOpts(true, agent))
	assertExitCode(t, err, ExitOperational)
	assertTaskOpen(t, env, "01-a")
}

func TestRunTaskYesPrintsConciseSummary(t *testing.T) {
	env := setupExecutorFixture(t, false)
	agent := writeFakeAgent(t, env.root, fakeAgentConfig{
		checkTask: true,
		summary:   "done work",
	})

	var buf bytes.Buffer
	d := env.deps()
	opts := env.runOpts(true, agent)
	opts.Output = &buf

	_, err := RunTaskWith(d, nil, nil, opts)
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

func TestRunTaskInteractivePrintsRefreshedTable(t *testing.T) {
	env := setupExecutorFixture(t, true)
	agent := writeFakeAgent(t, env.root, fakeAgentConfig{
		checkTask: true,
		summary:   "done work",
	})

	var buf bytes.Buffer
	d := env.deps()
	opts := env.runOpts(false, agent)
	opts.ConfirmIn = strings.NewReader("y\n")
	opts.Output = &buf

	_, err := RunTaskWith(d, nil, nil, opts)
	if err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	if !strings.Contains(out, "RUN") {
		t.Fatalf("missing pre-run marker:\n%s", out)
	}
	if strings.Count(out, "STATUS") < 2 {
		t.Fatalf("expected pre and post tables:\n%s", out)
	}
}

func TestRunTaskNoRunnableWork(t *testing.T) {
	env := setupExecutorFixture(t, false)
	manifestPath := env.demoManifest()
	data, _ := os.ReadFile(manifestPath)
	var payload map[string]any
	_ = json.Unmarshal(data, &payload)
	payload["tasks"] = []map[string]any{
		{"id": "01-a", "file": "01-a.md", "title": "A", "type": "AFK", "status": "done"},
	}
	updated, _ := json.MarshalIndent(payload, "", "  ")
	_ = os.WriteFile(manifestPath, updated, 0o644)

	agent := writeFakeAgent(t, env.root, fakeAgentConfig{summary: "unused"})
	_, err := RunTaskWith(env.deps(), nil, nil, env.runOpts(true, agent))
	assertExitCode(t, err, ExitNoRunnable)
}

func TestRunTaskDirtyNonInteractiveRejection(t *testing.T) {
	env := setupExecutorFixture(t, false)
	writeFile(t, filepath.Join(env.root, "dirty.txt"), "pending\n")

	agent := writeFakeAgent(t, env.root, fakeAgentConfig{summary: "unused"})
	opts := env.runOpts(false, agent)
	opts.ConfirmIn = NonInteractiveReader{}
	_, err := RunTaskWith(env.deps(), nil, nil, opts)
	assertExitCode(t, err, ExitOperational)
}

func TestRunTaskDirtyDefaultContinuesWithConfirmation(t *testing.T) {
	env := setupExecutorFixture(t, false)
	writeFile(t, filepath.Join(env.root, "partial.txt"), "resume\n")

	agent := writeFakeAgent(t, env.root, fakeAgentConfig{
		changeFile: "impl.txt",
		changeData: "done\n",
		checkTask:  true,
		summary:    "ok",
	})

	opts := env.runOpts(false, agent)
	opts.ConfirmIn = strings.NewReader("y\n")
	var notice bytes.Buffer
	opts.ConfirmOut = &notice
	// AllowDirty left unset; the default resolves to continue.

	result, err := RunTaskWith(env.deps(), nil, nil, opts)
	if err != nil {
		t.Fatalf("run failed: %v", err)
	}
	if !strings.Contains(notice.String(), "Runtime checkout has uncommitted changes") {
		t.Fatalf("missing dirty status report:\n%s", notice.String())
	}
	if result.CommitSHA == "" {
		t.Fatal("expected implementation commit")
	}
}

func TestRunTaskAllowDirtyCheckpoint(t *testing.T) {
	env := setupExecutorFixture(t, false)
	writeFile(t, filepath.Join(env.root, "partial.txt"), "resume me\n")

	agent := writeFakeAgent(t, env.root, fakeAgentConfig{
		changeFile: "impl.txt",
		changeData: "done\n",
		checkTask:  true,
		summary:    "finished after checkpoint",
	})

	opts := env.runOpts(true, agent)
	opts.AllowDirty = DirtyRuntimeCommitAndContinue
	var notice bytes.Buffer
	opts.ConfirmOut = &notice

	result, err := RunTaskWith(env.deps(), nil, nil, opts)
	if err != nil {
		t.Fatalf("run failed: %v", err)
	}
	if !strings.Contains(notice.String(), "Strategy commit-and-continue") {
		t.Fatalf("missing dirty strategy notice:\n%s", notice.String())
	}

	subjectLog, _ := env.deps().Git.CommandInDir(env.root, "log", "--format=%s")
	if !strings.Contains(subjectLog, "capturing dirty state") {
		t.Fatalf("checkpoint subject missing from log:\n%s", subjectLog)
	}
	status, _ := env.deps().Git.CommandInDir(env.root, "status", "--porcelain")
	if strings.TrimSpace(status) != "" {
		t.Fatalf("runtime not clean before agent: %q", status)
	}
	if result.CommitSHA == "" {
		t.Fatal("expected implementation commit after agent")
	}
}

func TestRunTaskAllowDirtyContinueIncludesExistingChangesInImplementationCommit(t *testing.T) {
	env := setupExecutorFixture(t, false)
	writeFile(t, filepath.Join(env.root, "partial.txt"), "resume me\n")

	agent := writeFakeAgent(t, env.root, fakeAgentConfig{
		changeFile: "impl.txt",
		changeData: "done\n",
		checkTask:  true,
		summary:    "finished resumed work",
	})

	opts := env.runOpts(true, agent)
	opts.AllowDirty = DirtyRuntimeContinue
	var notice bytes.Buffer
	opts.ConfirmOut = &notice

	result, err := RunTaskWith(env.deps(), nil, nil, opts)
	if err != nil {
		t.Fatalf("run failed: %v", err)
	}
	if !strings.Contains(notice.String(), "Strategy continue") {
		t.Fatalf("missing continue notice:\n%s", notice.String())
	}
	if result.CommitSHA == "" {
		t.Fatal("expected implementation commit")
	}
	files, _ := env.deps().Git.CommandInDir(env.root, "show", "--format=", "--name-only", "HEAD")
	if !strings.Contains(files, "partial.txt") || !strings.Contains(files, "impl.txt") {
		t.Fatalf("implementation commit did not include resumed and agent changes:\n%s", files)
	}
	subjectLog, _ := env.deps().Git.CommandInDir(env.root, "log", "--format=%s")
	if strings.Contains(subjectLog, "capturing dirty state") {
		t.Fatalf("continue unexpectedly created checkpoint:\n%s", subjectLog)
	}
}

func TestRunTaskAllowDirtyStashLeavesCreatedStashForUser(t *testing.T) {
	env := setupExecutorFixture(t, false)
	writeFile(t, filepath.Join(env.root, "partial.txt"), "stash me\n")

	agent := writeFakeAgent(t, env.root, fakeAgentConfig{
		changeFile: "impl.txt",
		changeData: "done\n",
		checkTask:  true,
		summary:    "worked from clean checkout",
	})

	opts := env.runOpts(true, agent)
	opts.AllowDirty = DirtyRuntimeStashAndContinue
	var notice bytes.Buffer
	opts.ConfirmOut = &notice

	if _, err := RunTaskWith(env.deps(), nil, nil, opts); err != nil {
		t.Fatalf("run failed: %v", err)
	}
	if !strings.Contains(notice.String(), "Created stash: stash@{0}") {
		t.Fatalf("missing stash reference:\n%s", notice.String())
	}
	stashes, _ := env.deps().Git.CommandInDir(env.root, "stash", "list")
	if !strings.Contains(stashes, "stash@{0}") {
		t.Fatalf("stash was not retained:\n%s", stashes)
	}
	if _, err := os.Stat(filepath.Join(env.root, "partial.txt")); !os.IsNotExist(err) {
		t.Fatalf("stashed file was restored unexpectedly: %v", err)
	}
}

func TestRunTaskAllowDirtyStashContinuesWhenGitCreatesNoStash(t *testing.T) {
	env := setupExecutorFixture(t, false)
	writeFile(t, filepath.Join(env.root, "partial.txt"), "still present\n")
	agent := writeFakeAgent(t, env.root, fakeAgentConfig{checkTask: true, summary: "continued"})

	git := &deps.MockGit{
		CommandInDirFunc: func(dir string, args ...string) (string, error) {
			if len(args) >= 2 && args[0] == "stash" && args[1] == "push" {
				return "No local changes to save", nil
			}
			if len(args) >= 3 && args[0] == "rev-parse" && args[2] == "refs/stash" {
				return "", fmt.Errorf("no stash")
			}
			return realGitInDir(dir, args...)
		},
	}
	d := env.deps()
	d.Git = git
	opts := env.runOpts(true, agent)
	opts.AllowDirty = DirtyRuntimeStashAndContinue

	if _, err := RunTaskWith(d, nil, nil, opts); err != nil {
		t.Fatalf("run failed: %v", err)
	}
}

func TestRunTaskDirtyCheckpointFailureDoesNotInvokeAgent(t *testing.T) {
	env := setupExecutorFixture(t, false)
	writeFile(t, filepath.Join(env.root, "partial.txt"), "resume me\n")

	agent := writeFakeAgent(t, env.root, fakeAgentConfig{
		changeFile: "impl.txt",
		changeData: "should-not-run\n",
		checkTask:  true,
		summary:    "unused",
	})

	git := &deps.MockGit{
		CommandInDirFunc: func(dir string, args ...string) (string, error) {
			if len(args) >= 3 && args[0] == "commit" && strings.Contains(args[2], "capturing dirty state") {
				return "", fmt.Errorf("checkpoint commit rejected")
			}
			return realGitInDir(dir, args...)
		},
	}
	d := env.deps()
	d.Git = git

	opts := env.runOpts(true, agent)
	opts.AllowDirty = DirtyRuntimeCommitAndContinue
	_, err := RunTaskWith(d, nil, nil, opts)
	assertExitCode(t, err, ExitOperational)
	if !strings.Contains(err.Error(), "dirty-runtime strategy") {
		t.Fatalf("err = %v", err)
	}

	status, _ := realGitInDir(env.root, "status", "--porcelain")
	if !strings.Contains(status, "partial.txt") {
		t.Fatalf("working tree changed unexpectedly:\n%s", status)
	}
	if strings.Contains(status, "impl.txt") {
		t.Fatal("agent should not have run")
	}
}

func TestRunTaskSeparateRuntimePath(t *testing.T) {
	root := t.TempDir()
	defRoot := filepath.Join(root, "definition")
	runtimeRoot := filepath.Join(root, "runtime")
	if err := os.MkdirAll(defRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(runtimeRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	initExecutorGitRepo(t, defRoot)
	initExecutorGitRepo(t, runtimeRoot)

	t.Setenv("XDG_DATA_HOME", filepath.Join(root, ".xdg"))
	tasksDir := storageTasksDir(t, defRoot)
	setupManifest(t, tasksDir, "demo", []Task{
		{ID: "01-a", File: "01-a.md", Title: "A", Type: "AFK", Status: "open"},
	})
	if _, err := RegisterWith(DefaultDeps(), tasksDir, DefaultStatePath()); err != nil {
		t.Fatal(err)
	}

	agent := writeFakeAgent(t, runtimeRoot, fakeAgentConfig{
		changeFile: "impl.txt",
		changeData: "in runtime\n",
		checkTask:  true,
		summary:    "runtime work",
	})

	env := &execFixture{root: defRoot, tasksDir: tasksDir}
	opts := RunTaskOptions{
		ResolveInput: ResolveInput{
			CWD:             defRoot,
			RuntimeOverride: runtimeRoot,
		},
		AgentCmd: agent,
		Yes:      true,
	}
	_, err := RunTaskWith(env.deps(), nil, nil, opts)
	if err != nil {
		t.Fatalf("run failed: %v", err)
	}
	if _, err := os.Stat(filepath.Join(runtimeRoot, "impl.txt")); err != nil {
		t.Fatalf("implementation not in runtime checkout: %v", err)
	}
	if _, err := os.Stat(filepath.Join(defRoot, "impl.txt")); !os.IsNotExist(err) {
		t.Fatal("implementation leaked into definition checkout")
	}
}

func TestRunTaskRejectsNonGitRuntimePath(t *testing.T) {
	env := setupExecutorFixture(t, false)
	agent := writeFakeAgent(t, env.root, fakeAgentConfig{summary: "unused"})
	opts := env.runOpts(true, agent)
	opts.RuntimeOverride = filepath.Join(env.root, "not-git")

	_, err := RunTaskWith(env.deps(), nil, nil, opts)
	assertExitCode(t, err, ExitSetup)
}

func TestRunTaskReleasesRuntimeLockAfterExecution(t *testing.T) {
	env := setupExecutorFixture(t, false)
	agent := writeFakeAgent(t, env.root, fakeAgentConfig{
		checkTask: true,
		summary:   "locked run",
	})

	d := env.deps()
	runtimePath, err := ResolveRuntimePathWith(d, env.root, "")
	if err != nil {
		t.Fatal(err)
	}
	opts := env.runOpts(true, agent)
	if _, err := RunTaskWith(d, nil, nil, opts); err != nil {
		t.Fatal(err)
	}

	if status := ReadRuntimeLockStatus(d, runtimePath); status.Locked {
		t.Fatalf("drain still live after execution: %#v", status)
	}
}

// While the single-task implement path waits at the pre-run confirmation
// prompt, no Runtime execution lock is held (ADR-0067): ReadRuntimeLockStatus
// reports not-locked and a second drain can claim the same checkout. The lock
// is acquired only after confirmation, around the actual attempt.
func TestRunTaskReleasesRuntimeLockAtConfirmationPrompt(t *testing.T) {
	env := setupExecutorFixture(t, false)
	agent := writeFakeAgent(t, env.root, fakeAgentConfig{
		checkTask: true,
		summary:   "confirmed run",
	})

	d := env.deps()
	d.ProcessAlive = func(pid int) bool { return pid == os.Getpid() }
	runtimePath, err := ResolveRuntimePathWith(d, env.root, "")
	if err != nil {
		t.Fatal(err)
	}

	check := func(t *testing.T) {
		t.Helper()
		if status := ReadRuntimeLockStatus(d, runtimePath); status.Locked {
			t.Fatalf("runtime lock at confirmation prompt = %#v, want not locked", status)
		}
		// A concurrent drain can claim the still-free checkout while the human is
		// at the confirmation prompt; finish it so it does not collide with the
		// real attempt's BeginDrain.
		handle, err := BeginDrain(d, runtimePath, "second", io.Discard)
		if err != nil {
			t.Fatalf("second drain must claim the unlocked checkout: %v", err)
		}
		if err := handle.Finish(store.StateFinished, "", false, time.Time{}); err != nil {
			t.Fatalf("finish second drain: %v", err)
		}
	}

	opts := env.runOpts(false, agent)
	opts.ConfirmIn = &checkingPromptReader{t: t, check: check, response: "y\n"}

	if _, err := RunTaskWith(d, nil, nil, opts); err != nil {
		t.Fatal(err)
	}
	assertTaskDone(t, env, "01-a")

	if status := ReadRuntimeLockStatus(d, runtimePath); status.Locked {
		t.Fatalf("runtime lock leaked after execution: %#v", status)
	}
}

// BindCheckout adoption (ADR-0036) runs before the attempt on every
// non-declined path: a bind failure aborts before the agent runs, leaving the
// task open.
func TestRunTaskBindsCheckoutBeforeAttempt(t *testing.T) {
	env := setupExecutorFixture(t, false)
	agent := writeFakeAgent(t, env.root, fakeAgentConfig{
		checkTask: true,
		summary:   "should not run",
	})

	d := env.deps()
	opts := env.runOpts(true, agent)
	opts.BindCheckout = func(setID, projectPath, runtimePath string) error {
		return fmt.Errorf("bind boom")
	}

	_, err := RunTaskWith(d, nil, nil, opts)
	assertExitCode(t, err, ExitOperational)
	// The agent never ran, so the task is still open — adoption gated the attempt.
	assertTaskOpen(t, env, "01-a")
}

// A declined confirmation never reaches BindCheckout (it returns before the
// drain claim and adoption), so adoption is scoped to non-declined paths.
func TestRunTaskDeclinedSkipsBindCheckout(t *testing.T) {
	env := setupExecutorFixture(t, false)
	agent := writeFakeAgent(t, env.root, fakeAgentConfig{summary: "unused"})

	d := env.deps()
	bound := false
	opts := env.runOpts(false, agent)
	opts.ConfirmIn = strings.NewReader("n\n")
	opts.BindCheckout = func(setID, projectPath, runtimePath string) error {
		bound = true
		return nil
	}

	result, err := RunTaskWith(d, nil, nil, opts)
	if err != nil {
		t.Fatal(err)
	}
	if !result.Declined {
		t.Fatal("expected declined")
	}
	if bound {
		t.Fatal("declined run must not adopt the checkout")
	}
}

func TestRunTaskBookkeepingOrder(t *testing.T) {
	env := setupExecutorFixture(t, false)
	agent := writeFakeAgent(t, env.root, fakeAgentConfig{
		checkTask: true,
		summary:   "ordered bookkeeping",
	})

	_, err := RunTaskWith(env.deps(), nil, nil, env.runOpts(true, agent))
	if err != nil {
		t.Fatal(err)
	}

	progress, err := os.ReadFile(filepath.Join(env.demoDir(), "progress.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(progress), "ordered bookkeeping") {
		t.Fatalf("missing progress:\n%s", progress)
	}
	assertTaskDone(t, env, "01-a")
}

type execFixture struct {
	root     string
	tasksDir string
}

// demoDir returns the storage directory of the fixture's "demo" Task set.
func (e *execFixture) demoDir() string { return filepath.Join(e.tasksDir, "demo") }

// demoManifest returns the storage path to the fixture's "demo" Task set manifest.
func (e *execFixture) demoManifest() string { return filepath.Join(e.demoDir(), "index.json") }

// demoTaskRef returns the <task-set>/<file>.md Task target reference for a
// file in the fixture's "demo" Task set (see ADR 0039).
func (e *execFixture) demoTaskRef(_ *testing.T, file string) string {
	return "demo/" + file
}

// storageTasksDir resolves the Task storage tasks directory for a repository checkout.
// XDG_DATA_HOME must already be set so the storage location is deterministic.
func storageTasksDir(t *testing.T, repoRoot string) string {
	t.Helper()
	id, err := ResolveRepositoryIdentity(DefaultDeps(), repoRoot)
	if err != nil {
		t.Fatalf("resolve storage: %v", err)
	}
	return id.TasksDir
}

type fakeAgentConfig struct {
	changeFile   string
	changeData   string
	checkTask    bool
	summary      string
	exitCode     int
	skipSentinel bool
	sleepFor     time.Duration
}

func setupExecutorFixture(t *testing.T, interactive bool) *execFixture {
	t.Helper()
	root := t.TempDir()
	initExecutorGitRepo(t, root)
	t.Setenv("XDG_DATA_HOME", filepath.Join(root, ".xdg"))
	tasksDir := storageTasksDir(t, root)
	setupManifest(t, tasksDir, "demo", []Task{
		{ID: "01-a", File: "01-a.md", Title: "A", Type: "AFK", Status: "open"},
	})
	if _, err := RegisterWith(DefaultDeps(), tasksDir, DefaultStatePath()); err != nil {
		t.Fatal(err)
	}
	_ = interactive
	return &execFixture{root: root, tasksDir: tasksDir}
}

func (e *execFixture) deps() *Deps {
	return &Deps{
		FS:     deps.NewRealFileSystem(),
		Git:    deps.NewRealGit(),
		Runner: RealCommandRunner{},
	}
}

func (e *execFixture) runOpts(yes bool, agentCmd string) RunTaskOptions {
	return RunTaskOptions{
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
	b.WriteString("TASK=$(printf '%s' \"$1\" | sed -n 's|^You are implementing the task at: ||p' | head -1)\n")
	if cfg.changeFile != "" {
		fmt.Fprintf(&b, "printf %q >> %q\n", cfg.changeData, cfg.changeFile)
	}
	if cfg.sleepFor > 0 {
		fmt.Fprintf(&b, "sleep %g\n", cfg.sleepFor.Seconds())
	}
	if cfg.checkTask {
		b.WriteString("if [ -n \"$TASK\" ] && [ -f \"$TASK\" ]; then\n")
		b.WriteString("  sed -i '' 's/- \\[ \\]/- [x]/g' \"$TASK\" 2>/dev/null || sed -i 's/- \\[ \\]/- [x]/g' \"$TASK\"\n")
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

// writeFakePiAgent installs a fake `pi` binary into root/.agent/pi so the pi
// preset can be exercised through the real command runner. The binary prints
// the given quota line and exits non-zero, matching the live failure mode.
func writeFakePiAgent(t *testing.T, root, quotaLine string) string {
	t.Helper()
	path := filepath.Join(root, ".agent", "pi")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	var b strings.Builder
	b.WriteString("#!/bin/sh\n")
	fmt.Fprintf(&b, "printf '%%s\\n' %q\n", quotaLine)
	b.WriteString("exit 1\n")
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

func assertTaskDone(t *testing.T, env *execFixture, taskID string) {
	t.Helper()
	m := LoadManifest(DefaultDeps(), "demo", env.demoManifest())
	for _, task := range m.Tasks {
		if task.ID == taskID && task.Status != "done" {
			t.Fatalf("task %s status = %q", taskID, task.Status)
		}
	}
}

func assertTaskOpen(t *testing.T, env *execFixture, taskID string) {
	t.Helper()
	m := LoadManifest(DefaultDeps(), "demo", env.demoManifest())
	for _, task := range m.Tasks {
		if task.ID == taskID && task.Status != "open" {
			t.Fatalf("task %s status = %q, want open", taskID, task.Status)
		}
	}
}

func assertTaskSkipped(t *testing.T, env *execFixture, taskID string) {
	t.Helper()
	m := LoadManifest(DefaultDeps(), "demo", env.demoManifest())
	for _, task := range m.Tasks {
		if task.ID == taskID && task.Status != "skipped" {
			t.Fatalf("task %s status = %q, want skipped", taskID, task.Status)
		}
	}
}

func assertTaskFailed(t *testing.T, env *execFixture, taskID string, failedAfter int) {
	t.Helper()
	m := LoadManifest(DefaultDeps(), "demo", env.demoManifest())
	for _, task := range m.Tasks {
		if task.ID != taskID {
			continue
		}
		if task.Status != "failed" {
			t.Fatalf("task %s status = %q, want failed", taskID, task.Status)
		}
		if task.FailedAfter == nil || *task.FailedAfter != failedAfter {
			t.Fatalf("task %s failed_after = %v, want %d", taskID, task.FailedAfter, failedAfter)
		}
		return
	}
	t.Fatalf("task %s not found", taskID)
}

func assertProgressContains(t *testing.T, env *execFixture, parts ...string) {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(env.demoDir(), "progress.txt"))
	if err != nil {
		t.Fatal(err)
	}
	for _, part := range parts {
		if !strings.Contains(string(data), part) {
			t.Fatalf("progress missing %q:\n%s", part, data)
		}
	}
}

func assertProgressNotContains(t *testing.T, env *execFixture, parts ...string) {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(env.demoDir(), "progress.txt"))
	if err != nil {
		t.Fatal(err)
	}
	for _, part := range parts {
		if strings.Contains(string(data), part) {
			t.Fatalf("progress unexpectedly contains %q:\n%s", part, data)
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

// captureAgentRunner records every Start invocation without executing anything,
// completing each immediately with exit 0 and no output.
type captureAgentRunner struct {
	names    []string
	argLists [][]string
}

func (r *captureAgentRunner) Run(ctx context.Context, dir string, stdout, stderr io.Writer, name string, args ...string) (int, error) {
	proc, err := r.Start(ctx, dir, stdout, stderr, name, args...)
	if err != nil {
		return 1, err
	}
	return proc.Wait()
}

func (r *captureAgentRunner) Start(ctx context.Context, dir string, stdout, stderr io.Writer, name string, args ...string) (*ManagedProcess, error) {
	r.names = append(r.names, name)
	r.argLists = append(r.argLists, append([]string{}, args...))
	proc := &ManagedProcess{done: make(chan waitResult, 1)}
	proc.done <- waitResult{exitCode: 0}
	return proc, nil
}

func TestRunTaskConfiguredEffortResolvesOpencodeModel(t *testing.T) {
	setEnv := setupRunTaskSetFixture(t, "demo", []Task{
		{ID: "01-a", File: "01-a.md", Title: "A", Type: "AFK", Status: "open", Effort: "heavy", EffortExplicit: true},
	})
	env := setEnv.execFixture()
	runner := &captureAgentRunner{}
	d := env.deps()
	d.Runner = runner
	loadConfig := func(string) (*config.Config, error) {
		return &config.Config{Effort: map[string]config.EffortConfig{
			"opencode": {Heavy: []config.EffortModel{{Model: "opencode/claude-opus-4-8"}, {Model: "opencode/kimi-k2.6"}}},
		}}, nil
	}

	opts := env.runOpts(true, "")
	opts.AgentPreset = "opencode"
	opts.MaxTries = 1
	opts.MaxTriesExplicit = true
	opts.Output = io.Discard

	_, err := RunTaskWith(d, nil, loadConfig, opts)
	assertExitCode(t, err, ExitOperational)
	if len(runner.names) != 1 || runner.names[0] != "opencode" {
		t.Fatalf("agent binary = %v, want opencode", runner.names)
	}
	args := runner.argLists[0]
	if len(args) < 2 || args[0] != "--model" || args[1] != "opencode/claude-opus-4-8" {
		t.Fatalf("agent args = %v, want leading configured model", args)
	}
}

func TestRunTaskAgentCmdIgnoresEffortModelResolution(t *testing.T) {
	setEnv := setupRunTaskSetFixture(t, "demo", []Task{
		{ID: "01-a", File: "01-a.md", Title: "A", Type: "AFK", Status: "open", Effort: "heavy", EffortExplicit: true},
	})
	env := setEnv.execFixture()
	runner := &captureAgentRunner{}
	d := env.deps()
	d.Runner = runner

	opts := env.runOpts(true, "./my-agent.sh")
	opts.AgentPreset = "claude"
	opts.MaxTries = 1
	opts.Output = io.Discard

	_, _ = RunTaskWith(d, nil, nil, opts)
	if len(runner.names) != 1 || runner.names[0] != "sh" {
		t.Fatalf("agent binary = %v, want sh", runner.names)
	}
	if strings.Contains(strings.Join(runner.argLists[0], " "), "--model") {
		t.Fatalf("custom agent invocation unexpectedly contains model: %v", runner.argLists[0])
	}
}

func TestRunTaskNoEffortKeyKeepsLegacyClaudeInvocation(t *testing.T) {
	setEnv := setupRunTaskSetFixture(t, "demo", []Task{
		{ID: "01-a", File: "01-a.md", Title: "A", Type: "AFK", Status: "open"},
	})
	env := setEnv.execFixture()
	runner := &captureAgentRunner{}
	d := env.deps()
	d.Runner = runner

	opts := env.runOpts(true, "")
	opts.AgentPreset = "claude"
	opts.MaxTries = 1
	opts.Output = io.Discard

	_, _ = RunTaskWith(d, nil, nil, opts)
	if len(runner.argLists) != 1 {
		t.Fatalf("agent invocations = %d", len(runner.argLists))
	}
	if strings.Contains(strings.Join(runner.argLists[0], " "), "--model") {
		t.Fatalf("legacy invocation unexpectedly contains model: %v", runner.argLists[0])
	}
	if strings.Contains(strings.Join(runner.argLists[0], " "), "--effort") {
		t.Fatalf("legacy invocation unexpectedly contains effort: %v", runner.argLists[0])
	}
}
