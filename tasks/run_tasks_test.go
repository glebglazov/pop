package tasks

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/glebglazov/pop/config"
	"github.com/glebglazov/pop/internal/deps"
)

func TestRunTaskSetDrainsMultipleAFKTasksInOrder(t *testing.T) {
	env := setupRunTaskSetFixture(t, "demo", []Task{
		{ID: "01-a", File: "01-a.md", Title: "A", Type: "AFK", Status: "open"},
		{ID: "02-b", File: "02-b.md", Title: "B", Type: "AFK", Status: "open"},
	})
	agent := writeFakeAgent(t, env.root, fakeAgentConfig{
		changeFile: "impl.txt",
		changeData: "x\n",
		checkTask:  true,
		summary:    "done",
	})

	var buf bytes.Buffer
	result, err := RunTaskSetWith(env.deps(), nil, nil, env.runTaskSetOpts(true, agent, &buf))
	if err != nil {
		t.Fatalf("run failed: %v", err)
	}
	if !result.TaskSetDone || len(result.Completed) != 2 {
		t.Fatalf("result = %#v", result)
	}
	if result.Completed[0].Selection.TaskID != "01-a" || result.Completed[1].Selection.TaskID != "02-b" {
		t.Fatalf("task order = %s, %s", result.Completed[0].Selection.TaskID, result.Completed[1].Selection.TaskID)
	}
	assertTaskDone(t, env.execFixture(), "01-a")
	assertTaskDone(t, env.execFixture(), "02-b")
}

func TestRunTaskSetSequentialDependencyUnblocking(t *testing.T) {
	env := setupRunTaskSetFixture(t, "demo", []Task{
		{ID: "01-a", File: "01-a.md", Title: "A", Type: "AFK", Status: "open"},
		{ID: "02-b", File: "02-b.md", Title: "B", Type: "AFK", Status: "open", BlockedBy: []string{"01-a"}},
	})
	agent := writeFakeAgent(t, env.root, fakeAgentConfig{checkTask: true, summary: "ok"})

	result, err := RunTaskSetWith(env.deps(), nil, nil, env.runTaskSetOpts(true, agent, nil))
	if err != nil {
		t.Fatalf("run failed: %v", err)
	}
	if len(result.Completed) != 2 {
		t.Fatalf("completed = %d", len(result.Completed))
	}
	assertTaskDone(t, env.execFixture(), "02-b")
}

func TestRunTaskSetNoOpContinuation(t *testing.T) {
	env := setupRunTaskSetFixture(t, "demo", []Task{
		{ID: "01-a", File: "01-a.md", Title: "A", Type: "AFK", Status: "open"},
		{ID: "02-b", File: "02-b.md", Title: "B", Type: "AFK", Status: "open"},
	})
	agent := writeFakeAgent(t, env.root, fakeAgentConfig{checkTask: true, summary: "verified"})

	result, err := RunTaskSetWith(env.deps(), nil, nil, env.runTaskSetOpts(true, agent, nil))
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Completed) != 2 || !result.Completed[0].NoOp {
		t.Fatalf("result = %#v", result)
	}
}

func TestRunTaskSetStartsWithoutAFKConsentPrompt(t *testing.T) {
	env := setupRunTaskSetFixture(t, "demo", []Task{
		{ID: "01-a", File: "01-a.md", Title: "A", Type: "AFK", Status: "open"},
		{ID: "02-b", File: "02-b.md", Title: "B", Type: "AFK", Status: "open"},
	})
	agent := writeFakeAgent(t, env.root, fakeAgentConfig{checkTask: true, summary: "ok"})

	var confirmOut bytes.Buffer
	opts := env.runTaskSetOpts(false, agent, nil)
	opts.ConfirmIn = strings.NewReader("n\n")
	opts.ConfirmOut = &confirmOut

	result, err := RunTaskSetWith(env.deps(), nil, nil, opts)
	if err != nil {
		t.Fatal(err)
	}
	if !result.TaskSetDone || len(result.Completed) != 2 {
		t.Fatalf("result = %#v, want done with two completions", result)
	}
	if strings.Contains(confirmOut.String(), "Run AFK tasks in this Task set?") {
		t.Fatalf("set drain must not ask for AFK consent:\n%s", confirmOut.String())
	}
}

func TestRunTaskSetDirtyNonInteractiveProceeds(t *testing.T) {
	env := setupRunTaskSetFixture(t, "demo", []Task{
		{ID: "01-a", File: "01-a.md", Title: "A", Type: "AFK", Status: "open"},
	})
	writeFile(t, filepath.Join(env.root, "partial.txt"), "pending\n")
	agent := writeFakeAgent(t, env.root, fakeAgentConfig{checkTask: true, summary: "done"})

	opts := env.runTaskSetOpts(false, agent, nil)
	opts.ConfirmIn = NonInteractiveReader{}
	result, err := RunTaskSetWith(env.deps(), nil, nil, opts)
	if err != nil {
		t.Fatalf("non-interactive set drain should proceed without AFK consent: %v", err)
	}
	if len(result.Completed) != 1 {
		t.Fatalf("result = %#v", result)
	}
}

func TestRunTaskSetAppliesDirtyStrategyOnceBeforeDrain(t *testing.T) {
	env := setupRunTaskSetFixture(t, "demo", []Task{
		{ID: "01-a", File: "01-a.md", Title: "A", Type: "AFK", Status: "open"},
		{ID: "02-b", File: "02-b.md", Title: "B", Type: "AFK", Status: "open"},
	})
	writeFile(t, filepath.Join(env.root, "partial.txt"), "stash once\n")
	agent := writeFakeAgent(t, env.root, fakeAgentConfig{checkTask: true, summary: "done"})

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
	opts := env.runTaskSetOpts(true, agent, nil)
	opts.AllowDirty = DirtyRuntimeStashAndContinue

	result, err := RunTaskSetWith(d, nil, nil, opts)
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

func TestRunTaskSetTargetedTaskSet(t *testing.T) {
	root := t.TempDir()
	initExecutorGitRepo(t, root)
	t.Setenv("XDG_DATA_HOME", filepath.Join(root, ".xdg"))
	tasksDir := storageTasksDir(t, root)
	setupManifest(t, tasksDir, "high", []Task{
		{ID: "01-a", File: "01-a.md", Title: "A", Type: "AFK", Status: "open"},
	})
	setupManifest(t, tasksDir, "low", []Task{
		{ID: "01-x", File: "01-x.md", Title: "X", Type: "AFK", Status: "open"},
	})
	refresh, err := RefreshWith(DefaultDeps(), tasksDir, DefaultStatePath())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := SetPriorityWith(DefaultDeps(), nil, nil, ResolveInput{CWD: root}, "low", 99); err != nil {
		t.Fatal(err)
	}
	_ = refresh

	agent := writeFakeAgent(t, root, fakeAgentConfig{checkTask: true, summary: "targeted"})
	env := &runTaskSetFixture{root: root, tasksDir: tasksDir}
	opts := env.runTaskSetOpts(true, agent, nil)
	opts.TaskSetOverride = "high"

	result, err := RunTaskSetWith(env.deps(), nil, nil, opts)
	if err != nil {
		t.Fatal(err)
	}
	if result.TaskSetID != "high" || len(result.Completed) != 1 || result.Completed[0].Selection.TaskID != "01-a" {
		t.Fatalf("result = %#v", result)
	}
}

func setupTwoSetHumanBlockedFixture(t *testing.T) (*runTaskSetFixture, string) {
	t.Helper()
	root := t.TempDir()
	initExecutorGitRepo(t, root)
	t.Setenv("XDG_DATA_HOME", filepath.Join(root, ".xdg"))
	tasksDir := storageTasksDir(t, root)
	// target is Human-blocked: only an open HITL task gates the set.
	setupManifest(t, tasksDir, "target", []Task{
		{ID: "01-hitl", File: "01-hitl.md", Title: "Review", Type: "HITL", Status: "open"},
	})
	// ready would be auto-selected by a bare drain.
	setupManifest(t, tasksDir, "ready", []Task{
		{ID: "01-a", File: "01-a.md", Title: "A", Type: "AFK", Status: "open"},
	})
	if _, err := RefreshWith(DefaultDeps(), tasksDir, DefaultStatePath()); err != nil {
		t.Fatal(err)
	}
	agent := writeFakeAgent(t, root, fakeAgentConfig{checkTask: true, summary: "ready done"})
	return &runTaskSetFixture{root: root, tasksDir: tasksDir}, agent
}

func setupSoleHumanBlockedFixture(t *testing.T) (*runTaskSetFixture, string) {
	t.Helper()
	root := t.TempDir()
	initExecutorGitRepo(t, root)
	t.Setenv("XDG_DATA_HOME", filepath.Join(root, ".xdg"))
	tasksDir := storageTasksDir(t, root)
	// solo is the only set and is Human-blocked: an open HITL task gates it and no
	// Ready Task set exists anywhere, so bare drain must fall back to its HITL gate.
	setupManifest(t, tasksDir, "solo", []Task{
		{ID: "02-hitl", File: "02-hitl.md", Title: "Review", Type: "HITL", Status: "open"},
	})
	if _, err := RefreshWith(DefaultDeps(), tasksDir, DefaultStatePath()); err != nil {
		t.Fatal(err)
	}
	agent := writeFakeAgent(t, root, fakeAgentConfig{checkTask: true, summary: "done"})
	return &runTaskSetFixture{root: root, tasksDir: tasksDir}, agent
}

func TestRunTaskSetBareDrainFallsBackToSoleHITLGate(t *testing.T) {
	env, agent := setupSoleHumanBlockedFixture(t)

	var buf bytes.Buffer
	opts := env.runTaskSetOpts(false, agent, &buf)
	opts.ConfirmIn = strings.NewReader("4\n")

	result, err := RunTaskSetWith(env.deps(), nil, nil, opts)
	assertExitCode(t, err, ExitNoRunnable)
	if result != nil && len(result.Completed) != 0 {
		t.Fatalf("fallback gate should not drain AFK work: %#v", result)
	}

	out := buf.String()
	for _, want := range []string{
		"No runnable AFK work",
		"Human-blocked: solo/02-hitl",
		"1. Get agent assistance (default)",
		"4. Exit",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("fallback gate output missing %q:\n%s", want, out)
		}
	}
	if strings.Index(out, "No runnable AFK work") > strings.Index(out, "Human-blocked: solo/02-hitl") {
		t.Fatalf("No runnable AFK work must precede the HITL gate:\n%s", out)
	}
}

func TestRunTaskSetHoldsRuntimeLockAtInitialHITLGatePrompt(t *testing.T) {
	env, agent := setupSoleHumanBlockedFixture(t)
	d := env.deps()
	d.ProcessAlive = func(pid int) bool { return pid == os.Getpid() }
	runtimePath, err := ResolveRuntimePathWith(d, env.root, "")
	if err != nil {
		t.Fatal(err)
	}

	check := func(t *testing.T) {
		t.Helper()
		status := ReadRuntimeLockStatus(d, runtimePath)
		if !status.Locked || status.Metadata == nil || status.Metadata.SetID != "solo" {
			t.Fatalf("runtime lock at HITL prompt = %#v, want live solo lock", status)
		}
	}

	var buf bytes.Buffer
	opts := env.runTaskSetOpts(false, agent, &buf)
	opts.ConfirmIn = &checkingPromptReader{t: t, check: check, response: "4\n"}

	_, err = RunTaskSetWith(d, nil, nil, opts)
	assertExitCode(t, err, ExitNoRunnable)

	status := ReadRuntimeLockStatus(d, runtimePath)
	if status.Locked {
		t.Fatalf("runtime lock leaked after HITL gate exit: %#v", status)
	}
}

func TestRunTaskSetBareDrainFallbackDefaultGetsAgentAssistance(t *testing.T) {
	env, agent := setupSoleHumanBlockedFixture(t)
	runner := &configurableHITLAssistanceRunner{t: t, tasksDir: env.tasksDir, onRun: func(t *testing.T, tasksDir string) {
		m := LoadManifest(DefaultDeps(), "solo", filepath.Join(tasksDir, "solo", "index.json"))
		for i := range m.Tasks {
			if m.Tasks[i].ID == "02-hitl" {
				m.Tasks[i].Status = "done"
			}
		}
		if err := WriteManifestAtomic(DefaultDeps(), m); err != nil {
			t.Fatal(err)
		}
	}}
	d := env.deps()
	d.Runner = runner

	var buf bytes.Buffer
	opts := env.runTaskSetOpts(false, agent, &buf)
	opts.ConfirmIn = strings.NewReader("\n")

	if _, err := RunTaskSetWith(d, nil, nil, opts); err != nil {
		t.Fatalf("fallback assistance should resolve the gate: %v", err)
	}
	if runner.attendedCalls != 1 {
		t.Fatalf("fallback default must start attended assistance once: attended=%d", runner.attendedCalls)
	}
}

func TestRunTaskSetInitialHITLGateContinuesDrainingWithoutAFKConsent(t *testing.T) {
	env := setupRunTaskSetFixture(t, "demo", []Task{
		{ID: "01-hitl", File: "01-hitl.md", Title: "Review", Type: "HITL", Status: "open"},
		{ID: "02-a", File: "02-a.md", Title: "A", Type: "AFK", Status: "open", BlockedBy: []string{"01-hitl"}},
	})
	agent := writeFakeAgent(t, env.root, fakeAgentConfig{checkTask: true, summary: "done"})

	var buf bytes.Buffer
	opts := env.runTaskSetOpts(false, agent, &buf)
	opts.ConfirmIn = strings.NewReader("2\n")
	opts.ConfirmOut = &buf

	result, err := RunTaskSetWith(env.deps(), nil, nil, opts)
	if err != nil {
		t.Fatal(err)
	}
	if !result.TaskSetDone || len(result.Completed) != 1 {
		t.Fatalf("result = %#v, want done with one AFK completion", result)
	}
	out := buf.String()
	for _, want := range []string{"Human-blocked: demo/01-hitl", "✓ Completed task demo/01-hitl", "━━ Running task demo/02-a"} {
		if !strings.Contains(out, want) {
			t.Fatalf("output missing %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "Run AFK tasks in this Task set?") {
		t.Fatalf("AFK consent must not be requested after the initial HITL gate clears:\n%s", out)
	}
	assertTaskDone(t, env.execFixture(), "01-hitl")
	assertTaskDone(t, env.execFixture(), "02-a")
}

func TestRunTaskSetBareDrainFallbackYesStopsWithoutAssistance(t *testing.T) {
	env, agent := setupSoleHumanBlockedFixture(t)
	runner := &configurableHITLAssistanceRunner{t: t, tasksDir: env.tasksDir}
	d := env.deps()
	d.Runner = runner

	var buf bytes.Buffer
	opts := env.runTaskSetOpts(true, agent, &buf)

	_, err := RunTaskSetWith(d, nil, nil, opts)
	assertExitCode(t, err, ExitNoRunnable)
	if runner.calls != 0 {
		t.Fatalf("--yes must not start attended assistance: calls=%d", runner.calls)
	}
	out := buf.String()
	if !strings.Contains(out, "pop tasks complete solo/02-hitl.md") {
		t.Fatalf("stop-and-advice missing:\n%s", out)
	}
}

func TestRunTaskSetExplicitHumanBlockedShowsGateDespiteReadyElsewhere(t *testing.T) {
	env, agent := setupTwoSetHumanBlockedFixture(t)

	var buf bytes.Buffer
	opts := env.runTaskSetOpts(false, agent, &buf)
	opts.TaskSetOverride = "target"
	opts.ConfirmIn = strings.NewReader("4\n")

	result, err := RunTaskSetWith(env.deps(), nil, nil, opts)
	assertExitCode(t, err, ExitNoRunnable)
	if result != nil && len(result.Completed) != 0 {
		t.Fatalf("explicit target should not drain the Ready set: %#v", result)
	}

	out := buf.String()
	for _, want := range []string{
		"Human-blocked: target/01-hitl",
		"1. Get agent assistance (default)",
		"4. Exit",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("gate output missing %q:\n%s", want, out)
		}
	}
	// The Ready set elsewhere must not have been touched.
	assertTaskOpen(t, &execFixture{root: env.root, tasksDir: env.tasksDir}, "01-a")
}

func TestRunTaskSetExplicitHumanBlockedYesStopsWithoutAssistance(t *testing.T) {
	env, agent := setupTwoSetHumanBlockedFixture(t)
	runner := &configurableHITLAssistanceRunner{t: t, tasksDir: env.tasksDir}
	d := env.deps()
	d.Runner = runner

	var buf bytes.Buffer
	opts := env.runTaskSetOpts(true, agent, &buf)
	opts.TaskSetOverride = "target"

	_, err := RunTaskSetWith(d, nil, nil, opts)
	assertExitCode(t, err, ExitNoRunnable)
	if runner.calls != 0 {
		t.Fatalf("--yes must not start attended assistance: calls=%d", runner.calls)
	}
	out := buf.String()
	if !strings.Contains(out, "pop tasks complete target/01-hitl.md") {
		t.Fatalf("stop-and-advice missing:\n%s", out)
	}
}

func TestRunTaskSetBlockedStopsWithReason(t *testing.T) {
	env := setupRunTaskSetFixture(t, "demo", []Task{
		{ID: "01-a", File: "01-a.md", Title: "A", Type: "AFK", Status: "open"},
		{ID: "02-hitl", File: "02-hitl.md", Title: "Review", Type: "HITL", Status: "open"},
	})
	agent := writeFakeAgent(t, env.root, fakeAgentConfig{checkTask: true, summary: "first done"})

	_, err := RunTaskSetWith(env.deps(), nil, nil, env.runTaskSetOpts(true, agent, nil))
	assertExitCode(t, err, ExitNoRunnable)
	if !strings.Contains(err.Error(), "HITL") {
		t.Fatalf("err = %v", err)
	}
}

func TestRunTaskSetHITLGatePrintsRecoveryAdvice(t *testing.T) {
	env := setupRunTaskSetFixture(t, "demo", []Task{
		{ID: "01-a", File: "01-a.md", Title: "A", Type: "AFK", Status: "open"},
		{ID: "02-hitl", File: "02-hitl.md", Title: "Review", Type: "HITL", Status: "open"},
	})
	agent := writeFakeAgent(t, env.root, fakeAgentConfig{checkTask: true, summary: "first done"})

	var buf bytes.Buffer
	_, err := RunTaskSetWith(env.deps(), nil, nil, env.runTaskSetOpts(true, agent, &buf))
	assertExitCode(t, err, ExitNoRunnable)

	out := buf.String()
	// After the AFK task completes, only the HITL remains → StatusUnverified:
	// all agent work is done, terminal verification framing applies.
	for _, want := range []string{
		"Agents done — verify: demo/02-hitl",
		"--- demo/02-hitl.md ---",
		"- [ ] ok",
		"--- end ---",
		"pop tasks complete demo/02-hitl.md",
		"$EDITOR demo/02-hitl.md && pop tasks implement",
		"pop tasks skip demo/02-hitl.md",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("advice missing %q:\n%s", want, out)
		}
	}
	if strings.Index(out, "--- end ---") > strings.Index(out, "finish by hand") {
		t.Fatalf("task body should precede recovery options:\n%s", out)
	}
}

func TestRunTaskSetInteractiveHITLGateShowsNumberedMenu(t *testing.T) {
	env := setupRunTaskSetFixture(t, "demo", []Task{
		{ID: "01-a", File: "01-a.md", Title: "A", Type: "AFK", Status: "open"},
		{ID: "02-hitl", File: "02-hitl.md", Title: "Review", Type: "HITL", Status: "open"},
	})
	agent := writeFakeAgent(t, env.root, fakeAgentConfig{checkTask: true, summary: "first done"})

	var buf bytes.Buffer
	opts := env.runTaskSetOpts(false, agent, &buf)
	opts.ConfirmIn = strings.NewReader("4\n")

	_, err := RunTaskSetWith(env.deps(), nil, nil, opts)
	assertExitCode(t, err, ExitNoRunnable)

	out := buf.String()
	for _, want := range []string{
		"1. Get agent assistance (default)",
		"2. Complete task",
		"3. Defer task",
		"4. Exit",
		"Choose [1]:",
		"claude <HITL assistance prompt>",
		"using claude native attended assistance",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("menu missing %q:\n%s", want, out)
		}
	}
}

func TestRunTaskSetInteractiveHITLGateDefaultGetsAgentAssistance(t *testing.T) {
	env := setupRunTaskSetFixture(t, "demo", []Task{
		{ID: "01-a", File: "01-a.md", Title: "A", Type: "AFK", Status: "open"},
		{ID: "02-hitl", File: "02-hitl.md", Title: "Review", Type: "HITL", Status: "open"},
	})
	agent := writeFakeAgent(t, env.root, fakeAgentConfig{checkTask: true, summary: "first done"})
	runner := &hitlAssistanceRunner{t: t, tasksDir: env.tasksDir}
	d := env.deps()
	d.Runner = runner

	var buf bytes.Buffer
	opts := env.runTaskSetOpts(false, agent, &buf)
	opts.ConfirmIn = strings.NewReader("\n")

	result, err := RunTaskSetWith(d, nil, nil, opts)
	if err != nil {
		t.Fatal(err)
	}
	if runner.calls != 1 {
		t.Fatalf("assistance calls = %d, want 1", runner.calls)
	}
	if runner.name != "claude" || len(runner.args) != 1 {
		t.Fatalf("assistance command = %s %v", runner.name, runner.args)
	}
	if !strings.Contains(runner.args[0], "You are assisting a human at a HITL gate") {
		t.Fatalf("assistance prompt missing HITL context:\n%s", runner.args[0])
	}
	if !result.TaskSetDone {
		t.Fatalf("result = %#v, want TaskSetDone", result)
	}
	if !strings.Contains(buf.String(), "Starting HITL assistance: claude <HITL assistance prompt>") {
		t.Fatalf("missing assistance start detail:\n%s", buf.String())
	}
	assertTaskDone(t, env.execFixture(), "02-hitl")
}

func TestRunTaskSetInteractiveHITLGateAssistanceStartFailureReprompts(t *testing.T) {
	env := setupRunTaskSetFixture(t, "demo", []Task{
		{ID: "01-a", File: "01-a.md", Title: "A", Type: "AFK", Status: "open"},
		{ID: "02-hitl", File: "02-hitl.md", Title: "Review", Type: "HITL", Status: "open", BlockedBy: []string{"01-a"}},
	})
	agent := writeFakeAgent(t, env.root, fakeAgentConfig{checkTask: true, summary: "done"})
	runner := &configurableHITLAssistanceRunner{t: t, runErr: fmt.Errorf("exec: claude: not found")}
	d := env.deps()
	d.Runner = runner

	var buf bytes.Buffer
	opts := env.runTaskSetOpts(false, agent, &buf)
	opts.ConfirmIn = strings.NewReader("\n4\n")

	_, err := RunTaskSetWith(d, nil, nil, opts)
	assertExitCode(t, err, ExitNoRunnable)
	out := buf.String()
	if !strings.Contains(out, "Could not start HITL assistance: exec: claude: not found") {
		t.Fatalf("missing start-failure message:\n%s", out)
	}
	if strings.Count(out, "Choose [1]:") < 2 {
		t.Fatalf("start failure did not return to gate prompt:\n%s", out)
	}
	if runner.calls != 1 {
		t.Fatalf("assistance calls = %d, want 1", runner.calls)
	}
	assertTaskOpen(t, env.execFixture(), "02-hitl")
}

func TestRunTaskSetInteractiveHITLGateAssistanceClearedGateContinuesDraining(t *testing.T) {
	env := setupRunTaskSetFixture(t, "demo", []Task{
		{ID: "01-a", File: "01-a.md", Title: "A", Type: "AFK", Status: "open"},
		{ID: "02-hitl", File: "02-hitl.md", Title: "Review", Type: "HITL", Status: "open", BlockedBy: []string{"01-a"}},
		{ID: "03-b", File: "03-b.md", Title: "B", Type: "AFK", Status: "open", BlockedBy: []string{"02-hitl"}},
	})
	agent := writeFakeAgent(t, env.root, fakeAgentConfig{checkTask: true, summary: "done"})
	runner := &configurableHITLAssistanceRunner{t: t, tasksDir: env.tasksDir, onRun: func(t *testing.T, tasksDir string) {
		setTaskStatus(t, tasksDir, "02-hitl", "skipped", nil)
	}}
	d := env.deps()
	d.Runner = runner

	var buf bytes.Buffer
	opts := env.runTaskSetOpts(false, agent, &buf)
	opts.ConfirmIn = strings.NewReader("\n")

	result, err := RunTaskSetWith(d, nil, nil, opts)
	if err != nil {
		t.Fatal(err)
	}
	if !result.TaskSetDeferred || len(result.Completed) != 2 {
		t.Fatalf("result = %#v, want deferred with two AFK completions", result)
	}
	if runner.calls != 1 {
		t.Fatalf("assistance calls = %d, want 1", runner.calls)
	}
	if runner.attendedCalls != 1 || runner.runCalls != 0 {
		t.Fatalf("runner calls: attended=%d run=%d, want attended only", runner.attendedCalls, runner.runCalls)
	}
	if runner.name != "claude" || len(runner.args) != 1 || !strings.Contains(runner.args[0], "You are assisting a human at a HITL gate") {
		t.Fatalf("assistance command = %s %v", runner.name, runner.args)
	}
	if !strings.Contains(buf.String(), "━━ Running task demo/03-b") {
		t.Fatalf("did not continue draining after cleared gate:\n%s", buf.String())
	}
	assertTaskSkipped(t, env.execFixture(), "02-hitl")
	assertTaskDone(t, env.execFixture(), "03-b")
}

func TestRunTaskSetInteractiveHITLGateAssistanceStillBlockedReprompts(t *testing.T) {
	env := setupRunTaskSetFixture(t, "demo", []Task{
		{ID: "01-a", File: "01-a.md", Title: "A", Type: "AFK", Status: "open"},
		{ID: "02-hitl", File: "02-hitl.md", Title: "Review", Type: "HITL", Status: "open", BlockedBy: []string{"01-a"}},
	})
	agent := writeFakeAgent(t, env.root, fakeAgentConfig{checkTask: true, summary: "done"})
	runner := &configurableHITLAssistanceRunner{t: t, tasksDir: env.tasksDir}
	d := env.deps()
	d.Runner = runner

	var buf bytes.Buffer
	opts := env.runTaskSetOpts(false, agent, &buf)
	opts.ConfirmIn = strings.NewReader("\n4\n")

	_, err := RunTaskSetWith(d, nil, nil, opts)
	assertExitCode(t, err, ExitNoRunnable)
	out := buf.String()
	if strings.Count(out, "Choose [1]:") < 2 {
		t.Fatalf("still-blocked assistance did not return to gate prompt:\n%s", out)
	}
	if runner.calls != 1 {
		t.Fatalf("assistance calls = %d, want 1", runner.calls)
	}
	assertTaskOpen(t, env.execFixture(), "02-hitl")
}

func TestRunTaskSetInteractiveHITLGateAssistanceChangedStatusUsesNormalHandling(t *testing.T) {
	env := setupRunTaskSetFixture(t, "demo", []Task{
		{ID: "01-a", File: "01-a.md", Title: "A", Type: "AFK", Status: "open"},
		{ID: "02-hitl", File: "02-hitl.md", Title: "Review", Type: "HITL", Status: "open", BlockedBy: []string{"01-a"}},
		{ID: "03-b", File: "03-b.md", Title: "B", Type: "AFK", Status: "open", BlockedBy: []string{"02-hitl"}},
	})
	agent := writeFakeAgent(t, env.root, fakeAgentConfig{checkTask: true, summary: "done"})
	failedAfter := 1
	runner := &configurableHITLAssistanceRunner{t: t, tasksDir: env.tasksDir, onRun: func(t *testing.T, tasksDir string) {
		setTaskStatus(t, tasksDir, "02-hitl", "failed", &failedAfter)
	}}
	d := env.deps()
	d.Runner = runner

	var buf bytes.Buffer
	opts := env.runTaskSetOpts(false, agent, &buf)
	opts.ConfirmIn = strings.NewReader("\n")

	_, err := RunTaskSetWith(d, nil, nil, opts)
	assertExitCode(t, err, ExitOperational)
	out := buf.String()
	if !strings.Contains(out, "FAILED") || !strings.Contains(out, "pop tasks open demo/02-hitl.md") {
		t.Fatalf("normal failed-status handling did not apply:\n%s", out)
	}
	assertTaskFailed(t, env.execFixture(), "02-hitl", failedAfter)
	assertTaskOpen(t, env.execFixture(), "03-b")
}

func TestRunTaskSetInteractiveHITLGateCompletionContinuesDraining(t *testing.T) {
	env := setupRunTaskSetFixture(t, "demo", []Task{
		{ID: "01-a", File: "01-a.md", Title: "A", Type: "AFK", Status: "open"},
		{ID: "02-hitl", File: "02-hitl.md", Title: "Review", Type: "HITL", Status: "open", BlockedBy: []string{"01-a"}},
		{ID: "03-b", File: "03-b.md", Title: "B", Type: "AFK", Status: "open", BlockedBy: []string{"02-hitl"}},
	})
	agent := writeFakeAgent(t, env.root, fakeAgentConfig{checkTask: true, summary: "done"})

	var buf bytes.Buffer
	opts := env.runTaskSetOpts(false, agent, &buf)
	opts.ConfirmIn = strings.NewReader("2\n")

	result, err := RunTaskSetWith(env.deps(), nil, nil, opts)
	if err != nil {
		t.Fatal(err)
	}
	if !result.TaskSetDone || len(result.Completed) != 2 {
		t.Fatalf("result = %#v, want done with two AFK completions", result)
	}
	out := buf.String()
	for _, want := range []string{"✓ Completed task demo/02-hitl", "━━ Running task demo/03-b"} {
		if !strings.Contains(out, want) {
			t.Fatalf("output missing %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "Complete task? [y/N]:") {
		t.Fatalf("completion choice should not ask for a second confirmation:\n%s", out)
	}
	assertTaskDone(t, env.execFixture(), "01-a")
	assertTaskDone(t, env.execFixture(), "02-hitl")
	assertTaskDone(t, env.execFixture(), "03-b")
	assertProgressContains(t, env.execFixture(), "COMPLETE", "manually completed demo/02-hitl (was open)")
}

func TestRunTaskSetInteractiveHITLGateDeferralContinuesDraining(t *testing.T) {
	env := setupRunTaskSetFixture(t, "demo", []Task{
		{ID: "01-a", File: "01-a.md", Title: "A", Type: "AFK", Status: "open"},
		{ID: "02-hitl", File: "02-hitl.md", Title: "Review", Type: "HITL", Status: "open", BlockedBy: []string{"01-a"}},
		{ID: "03-b", File: "03-b.md", Title: "B", Type: "AFK", Status: "open", BlockedBy: []string{"02-hitl"}},
	})
	agent := writeFakeAgent(t, env.root, fakeAgentConfig{checkTask: true, summary: "done"})

	var buf bytes.Buffer
	opts := env.runTaskSetOpts(false, agent, &buf)
	opts.ConfirmIn = strings.NewReader("3\n")

	result, err := RunTaskSetWith(env.deps(), nil, nil, opts)
	if err != nil {
		t.Fatal(err)
	}
	if !result.TaskSetDeferred || len(result.Completed) != 2 {
		t.Fatalf("result = %#v, want deferred after two AFK completions", result)
	}
	out := buf.String()
	for _, want := range []string{"Skipped task demo/02-hitl", "━━ Running task demo/03-b", "Task set demo deferred: skipped 02-hitl"} {
		if !strings.Contains(out, want) {
			t.Fatalf("output missing %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "Defer task? [y/N]:") {
		t.Fatalf("deferral choice should not ask for a second confirmation:\n%s", out)
	}
	assertTaskDone(t, env.execFixture(), "01-a")
	assertTaskSkipped(t, env.execFixture(), "02-hitl")
	assertTaskDone(t, env.execFixture(), "03-b")
	assertProgressContains(t, env.execFixture(), "SKIP", "skipped demo/02-hitl")
}

func TestRunTaskSetHITLGateNonInteractiveKeepsAdvice(t *testing.T) {
	env := setupRunTaskSetFixture(t, "demo", []Task{
		{ID: "01-a", File: "01-a.md", Title: "A", Type: "AFK", Status: "open"},
		{ID: "02-hitl", File: "02-hitl.md", Title: "Review", Type: "HITL", Status: "open"},
	})
	agent := writeFakeAgent(t, env.root, fakeAgentConfig{checkTask: true, summary: "first done"})

	var buf bytes.Buffer
	opts := env.runTaskSetOpts(true, agent, &buf)
	opts.ConfirmIn = NonInteractiveReader{}

	_, err := RunTaskSetWith(env.deps(), nil, nil, opts)
	assertExitCode(t, err, ExitNoRunnable)

	out := buf.String()
	if strings.Contains(out, "Get agent assistance") || strings.Contains(out, "Choose [1]:") {
		t.Fatalf("non-interactive run prompted:\n%s", out)
	}
	// After AFK completes, set is UNVERIFIED → terminal framing.
	if !strings.Contains(out, "Agents done — verify: demo/02-hitl") || !strings.Contains(out, "pop tasks complete demo/02-hitl.md") {
		t.Fatalf("missing HITL advice:\n%s", out)
	}
}

func TestRunTaskSetHITLGateYesKeepsAdvice(t *testing.T) {
	env := setupRunTaskSetFixture(t, "demo", []Task{
		{ID: "01-a", File: "01-a.md", Title: "A", Type: "AFK", Status: "open"},
		{ID: "02-hitl", File: "02-hitl.md", Title: "Review", Type: "HITL", Status: "open"},
	})
	agent := writeFakeAgent(t, env.root, fakeAgentConfig{checkTask: true, summary: "first done"})

	var buf bytes.Buffer
	opts := env.runTaskSetOpts(true, agent, &buf)
	opts.ConfirmIn = strings.NewReader("2\n")

	_, err := RunTaskSetWith(env.deps(), nil, nil, opts)
	assertExitCode(t, err, ExitNoRunnable)

	out := buf.String()
	if strings.Contains(out, "Get agent assistance") || strings.Contains(out, "Choose [1]:") {
		t.Fatalf("--yes run prompted:\n%s", out)
	}
	// After AFK completes, set is UNVERIFIED → terminal framing.
	if !strings.Contains(out, "Agents done — verify: demo/02-hitl") || !strings.Contains(out, "pop tasks skip demo/02-hitl.md") {
		t.Fatalf("missing HITL advice:\n%s", out)
	}
}

func TestHITLGateAdviceSurvivesUnreadableTaskFile(t *testing.T) {
	d := &Deps{FS: &deps.MockFileSystem{
		ReadFileFunc: func(string) ([]byte, error) {
			return nil, fmt.Errorf("no such file")
		},
	}}
	var buf bytes.Buffer
	printHITLGateAdvice(d, &buf, "demo", "/tmp/demo", &Task{ID: "02-hitl", File: "02-hitl.md"})

	out := buf.String()
	if !strings.Contains(out, "could not read demo/02-hitl.md") {
		t.Fatalf("missing read-failure notice:\n%s", out)
	}
	if !strings.Contains(out, "pop tasks complete demo/02-hitl.md") {
		t.Fatalf("advice block missing after read failure:\n%s", out)
	}
}

func TestRunTaskSetFailedStopMentionsCompleteAndReset(t *testing.T) {
	env := setupRunTaskSetFixture(t, "demo", []Task{
		{ID: "01-a", File: "01-a.md", Title: "A", Type: "AFK", Status: "open"},
	})
	agent := writeSequentialFakeAgent(t, env.root, []fakeAgentStep{{exitCode: 1}})

	var buf bytes.Buffer
	opts := env.runTaskSetOpts(true, agent, &buf)
	opts.MaxTries = 1
	_, err := RunTaskSetWith(env.deps(), nil, nil, opts)
	assertExitCode(t, err, ExitOperational)

	out := buf.String()
	if !strings.Contains(out, "pop tasks open demo/01-a.md") {
		t.Fatalf("advice missing reset hint:\n%s", out)
	}
	if !strings.Contains(out, "pop tasks complete demo/01-a.md") {
		t.Fatalf("advice missing complete hint:\n%s", out)
	}
}

func TestRunTaskSetHITLOnlyTaskSetRejectedAtSelection(t *testing.T) {
	env := setupRunTaskSetFixture(t, "demo", []Task{
		{ID: "01-hitl", File: "01-hitl.md", Title: "Review", Type: "HITL", Status: "open"},
	})
	agent := writeFakeAgent(t, env.root, fakeAgentConfig{summary: "unused"})

	_, err := RunTaskSetWith(env.deps(), nil, nil, env.runTaskSetOpts(true, agent, nil))
	assertExitCode(t, err, ExitNoRunnable)
}

func TestRunTaskSetFailedTaskStopsDrain(t *testing.T) {
	env := setupRunTaskSetFixture(t, "demo", []Task{
		{ID: "01-a", File: "01-a.md", Title: "A", Type: "AFK", Status: "open"},
		{ID: "02-b", File: "02-b.md", Title: "B", Type: "AFK", Status: "open"},
	})
	agent := writeSequentialFakeAgent(t, env.root, []fakeAgentStep{
		{summary: "ok"},
		{exitCode: 1},
	})

	opts := env.runTaskSetOpts(true, agent, nil)
	opts.MaxTries = 1
	_, err := RunTaskSetWith(env.deps(), nil, nil, opts)
	assertExitCode(t, err, ExitOperational)
	assertTaskDone(t, env.execFixture(), "01-a")
	assertTaskFailed(t, env.execFixture(), "02-b", 1)
}

func TestRunTaskSetClaudeQuotaPauseStopsCleanly(t *testing.T) {
	env := setupRunTaskSetFixture(t, "demo", []Task{
		{ID: "01-a", File: "01-a.md", Title: "A", Type: "AFK", Status: "open"},
		{ID: "02-b", File: "02-b.md", Title: "B", Type: "AFK", Status: "open"},
	})
	counterPath := installClaudeQuotaAgent(t, env.root)
	var buf bytes.Buffer
	opts := env.runTaskSetOpts(true, "", &buf)
	opts.AgentPreset = "claude"

	result, err := RunTaskSetWith(env.deps(), nil, nil, opts)
	if err != nil {
		t.Fatal(err)
	}
	if !result.QuotaPaused || len(result.Completed) != 0 {
		t.Fatalf("result = %#v", result)
	}
	assertTaskOpen(t, env.execFixture(), "01-a")
	assertTaskOpen(t, env.execFixture(), "02-b")
	if got := strings.TrimSpace(string(mustReadFile(t, counterPath))); got != "1" {
		t.Fatalf("started attempts = %q, want 1", got)
	}
	if !strings.Contains(buf.String(), "Task set demo paused") {
		t.Fatalf("missing pause summary:\n%s", buf.String())
	}
}

func TestRunTaskSetTimeoutPropagation(t *testing.T) {
	env := setupRunTaskSetFixture(t, "demo", []Task{
		{ID: "01-a", File: "01-a.md", Title: "A", Type: "AFK", Status: "open"},
	})
	agent := writeFakeAgent(t, env.root, fakeAgentConfig{
		summary:  "slow",
		sleepFor: 200 * time.Millisecond,
	})

	opts := env.runTaskSetOpts(true, agent, nil)
	opts.Timeout = 50 * time.Millisecond
	opts.MaxTries = 1
	_, err := RunTaskSetWith(env.deps(), nil, nil, opts)
	assertExitCode(t, err, ExitOperational)
	assertTaskFailed(t, env.execFixture(), "01-a", 1)
}

func TestRunTaskSetOperationalStopOnCommitFailure(t *testing.T) {
	env := setupRunTaskSetFixture(t, "demo", []Task{
		{ID: "01-a", File: "01-a.md", Title: "A", Type: "AFK", Status: "open"},
	})
	agent := writeFakeAgent(t, env.root, fakeAgentConfig{
		changeFile: "impl.txt",
		changeData: "x\n",
		checkTask:  true,
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

	_, err := RunTaskSetWith(d, nil, nil, env.runTaskSetOpts(true, agent, nil))
	assertExitCode(t, err, ExitOperational)
	if !strings.Contains(err.Error(), "task demo/01-a") {
		t.Fatalf("error missing task reference: %v", err)
	}
	assertTaskOpen(t, env.execFixture(), "01-a")
}

func TestRunTaskSetDoesNotContinueIntoAnotherTaskSet(t *testing.T) {
	root := t.TempDir()
	initExecutorGitRepo(t, root)
	t.Setenv("XDG_DATA_HOME", filepath.Join(root, ".xdg"))
	tasksDir := storageTasksDir(t, root)
	setupManifest(t, tasksDir, "one", []Task{
		{ID: "01-a", File: "01-a.md", Title: "A", Type: "AFK", Status: "open"},
	})
	setupManifest(t, tasksDir, "two", []Task{
		{ID: "01-x", File: "01-x.md", Title: "X", Type: "AFK", Status: "open"},
	})
	if _, err := RefreshWith(DefaultDeps(), tasksDir, DefaultStatePath()); err != nil {
		t.Fatal(err)
	}
	if _, err := SetPriorityWith(DefaultDeps(), nil, nil, ResolveInput{CWD: root}, "two", 10); err != nil {
		t.Fatal(err)
	}

	agent := writeFakeAgent(t, root, fakeAgentConfig{checkTask: true, summary: "one only"})
	env := &runTaskSetFixture{root: root, tasksDir: tasksDir}
	result, err := RunTaskSetWith(env.deps(), nil, nil, env.runTaskSetOpts(true, agent, nil))
	if err != nil {
		t.Fatal(err)
	}
	if !result.TaskSetDone || result.TaskSetID != "two" || len(result.Completed) != 1 {
		t.Fatalf("result = %#v", result)
	}
	assertTaskOpen(t, &execFixture{root: root, tasksDir: tasksDir}, "01-x")
}

// Under --yes the Failed gate cannot prompt, so re-entry into an already-Failed
// set preserves the static printFailedStopAdvice output and exits with
// operational failure so wrapping automation still sees the failure.
func TestRunTaskSetFailedReentryYesFallsBackToStaticAdvice(t *testing.T) {
	env := setupRunTaskSetFixture(t, "demo", []Task{
		{ID: "01-a", File: "01-a.md", Title: "A", Type: "AFK", Status: "failed", FailedAfter: intPtr(3)},
	})
	agent := writeFakeAgent(t, env.root, fakeAgentConfig{summary: "unused"})

	var buf bytes.Buffer
	opts := env.runTaskSetOpts(true, agent, &buf)
	opts.TaskSetOverride = "demo"

	_, err := RunTaskSetWith(env.deps(), nil, nil, opts)
	assertExitCode(t, err, ExitOperational)

	out := buf.String()
	if !strings.Contains(out, "pop tasks open demo/01-a.md") {
		t.Fatalf("static advice missing reset hint:\n%s", out)
	}
	if strings.Contains(out, "Re-run (default)") {
		t.Fatalf("--yes must not show the interactive Failed gate menu:\n%s", out)
	}
	assertTaskFailed(t, env.execFixture(), "01-a", 3)
}

// A non-interactive input (NonInteractiveReader, not --yes) also cannot prompt,
// so the Failed gate falls back to the same static advice and operational exit.
func TestRunTaskSetFailedReentryNonInteractiveFallsBack(t *testing.T) {
	env := setupRunTaskSetFixture(t, "demo", []Task{
		{ID: "01-a", File: "01-a.md", Title: "A", Type: "AFK", Status: "failed", FailedAfter: intPtr(3)},
	})
	agent := writeFakeAgent(t, env.root, fakeAgentConfig{summary: "unused"})

	var buf bytes.Buffer
	opts := env.runTaskSetOpts(false, agent, &buf)
	opts.TaskSetOverride = "demo"
	opts.ConfirmIn = NonInteractiveReader{}

	_, err := RunTaskSetWith(env.deps(), nil, nil, opts)
	assertExitCode(t, err, ExitOperational)
	if !strings.Contains(buf.String(), "pop tasks open demo/01-a.md") {
		t.Fatalf("static advice missing reset hint:\n%s", buf.String())
	}
}

// Re-run (empty input selects the default) resets the failed task and retries
// it in the same invocation, with no second AFK consent prompt.
func TestRunTaskSetFailedGateRerunRetriesInProcess(t *testing.T) {
	env := setupRunTaskSetFixture(t, "demo", []Task{
		{ID: "01-a", File: "01-a.md", Title: "A", Type: "AFK", Status: "failed", FailedAfter: intPtr(3)},
	})
	agent := writeFakeAgent(t, env.root, fakeAgentConfig{checkTask: true, summary: "fixed"})

	var buf bytes.Buffer
	opts := env.runTaskSetOpts(false, agent, &buf)
	opts.TaskSetOverride = "demo"
	opts.ConfirmIn = strings.NewReader("\n")

	result, err := RunTaskSetWith(env.deps(), nil, nil, opts)
	if err != nil {
		t.Fatal(err)
	}
	if !result.TaskSetDone || len(result.Completed) != 1 {
		t.Fatalf("result = %#v, want done with one completion", result)
	}
	out := buf.String()
	for _, want := range []string{
		"Failed: demo/01-a failed before the set could continue.",
		"1. Re-run (default)",
		"Reset task demo/01-a to open",
		"━━ Running task demo/01-a",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("output missing %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "Run AFK tasks in this Task set?") {
		t.Fatalf("Re-run must not ask for AFK consent:\n%s", out)
	}
	assertTaskDone(t, env.execFixture(), "01-a")
}

// Finish by hand marks the failed task done and lets the set continue draining
// from a task that the completion newly unblocks.
func TestRunTaskSetFailedGateFinishByHandContinues(t *testing.T) {
	env := setupRunTaskSetFixture(t, "demo", []Task{
		{ID: "01-a", File: "01-a.md", Title: "A", Type: "AFK", Status: "failed", FailedAfter: intPtr(3)},
		{ID: "02-b", File: "02-b.md", Title: "B", Type: "AFK", Status: "open", BlockedBy: []string{"01-a"}},
	})
	agent := writeFakeAgent(t, env.root, fakeAgentConfig{checkTask: true, summary: "second"})

	var buf bytes.Buffer
	opts := env.runTaskSetOpts(false, agent, &buf)
	opts.TaskSetOverride = "demo"
	opts.ConfirmIn = strings.NewReader("3\n")

	result, err := RunTaskSetWith(env.deps(), nil, nil, opts)
	if err != nil {
		t.Fatal(err)
	}
	if !result.TaskSetDone || len(result.Completed) != 1 {
		t.Fatalf("result = %#v, want done with one completion", result)
	}
	out := buf.String()
	for _, want := range []string{
		"✓ Completed task demo/01-a",
		"━━ Running task demo/02-b",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("output missing %q:\n%s", want, out)
		}
	}
	assertTaskDone(t, env.execFixture(), "01-a")
	assertTaskDone(t, env.execFixture(), "02-b")
}

// Exit leaves the failure in place, prints the static advice, and exits with
// operational failure.
func TestRunTaskSetFailedGateExitStops(t *testing.T) {
	env := setupRunTaskSetFixture(t, "demo", []Task{
		{ID: "01-a", File: "01-a.md", Title: "A", Type: "AFK", Status: "failed", FailedAfter: intPtr(3)},
	})
	agent := writeFakeAgent(t, env.root, fakeAgentConfig{summary: "unused"})

	var buf bytes.Buffer
	opts := env.runTaskSetOpts(false, agent, &buf)
	opts.TaskSetOverride = "demo"
	opts.ConfirmIn = strings.NewReader("4\n")

	_, err := RunTaskSetWith(env.deps(), nil, nil, opts)
	assertExitCode(t, err, ExitOperational)
	if !strings.Contains(buf.String(), "pop tasks open demo/01-a.md") {
		t.Fatalf("exit must print static advice:\n%s", buf.String())
	}
	assertTaskFailed(t, env.execFixture(), "01-a", 3)
}

// Invalid input re-prompts before accepting a valid selection.
func TestRunTaskSetFailedGateInvalidReprompts(t *testing.T) {
	env := setupRunTaskSetFixture(t, "demo", []Task{
		{ID: "01-a", File: "01-a.md", Title: "A", Type: "AFK", Status: "failed", FailedAfter: intPtr(3)},
	})
	agent := writeFakeAgent(t, env.root, fakeAgentConfig{summary: "unused"})

	var buf bytes.Buffer
	opts := env.runTaskSetOpts(false, agent, &buf)
	opts.TaskSetOverride = "demo"
	opts.ConfirmIn = strings.NewReader("9\n4\n")

	_, err := RunTaskSetWith(env.deps(), nil, nil, opts)
	assertExitCode(t, err, ExitOperational)
	if !strings.Contains(buf.String(), "Choose 1, 2, 3, or 4.") {
		t.Fatalf("invalid input must re-prompt:\n%s", buf.String())
	}
}

// The Failed gate also appears right after a live attempt exhausts its retries;
// Re-run retries the task in the same invocation.
func TestRunTaskSetFailedGateLiveFailureRerunRetries(t *testing.T) {
	env := setupRunTaskSetFixture(t, "demo", []Task{
		{ID: "01-a", File: "01-a.md", Title: "A", Type: "AFK", Status: "open"},
	})
	agent := writeSequentialFakeAgent(t, env.root, []fakeAgentStep{
		{exitCode: 1},
		{summary: "recovered"},
	})

	var buf bytes.Buffer
	opts := env.runTaskSetOpts(false, agent, &buf)
	opts.MaxTries = 1
	opts.ConfirmIn = strings.NewReader("1\n")

	result, err := RunTaskSetWith(env.deps(), nil, nil, opts)
	if err != nil {
		t.Fatal(err)
	}
	if !result.TaskSetDone {
		t.Fatalf("result = %#v, want done after re-run", result)
	}
	if !strings.Contains(buf.String(), "Failed: demo/01-a failed before the set could continue.") {
		t.Fatalf("live failure must show the Failed gate:\n%s", buf.String())
	}
	assertTaskDone(t, env.execFixture(), "01-a")
}

// The Failed gate offers Agent assistance as option 2; choosing it launches the
// attended agent for the configured preset, then refreshes the set and re-shows
// the Failed gate while the task is still failed (the assist agent does not
// change task state on its own).
func TestRunTaskSetFailedGateAgentAssistanceRefreshesAndReprompts(t *testing.T) {
	env := setupRunTaskSetFixture(t, "demo", []Task{
		{ID: "01-a", File: "01-a.md", Title: "A", Type: "AFK", Status: "failed", FailedAfter: intPtr(3)},
	})
	agent := writeFakeAgent(t, env.root, fakeAgentConfig{summary: "unused"})
	runner := &configurableHITLAssistanceRunner{t: t, tasksDir: env.tasksDir}
	d := env.deps()
	d.Runner = runner

	var buf bytes.Buffer
	opts := env.runTaskSetOpts(false, agent, &buf)
	opts.TaskSetOverride = "demo"
	// Choose Agent assistance, then Exit at the re-shown gate.
	opts.ConfirmIn = strings.NewReader("2\n4\n")

	_, err := RunTaskSetWith(d, nil, nil, opts)
	assertExitCode(t, err, ExitOperational)
	out := buf.String()

	for _, want := range []string{
		"1. Re-run (default)",
		"2. Agent assistance",
		"3. Finish by hand",
		"4. Exit",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("Failed gate missing menu option %q:\n%s", want, out)
		}
	}
	if runner.calls != 1 {
		t.Fatalf("assistance calls = %d, want 1", runner.calls)
	}
	if runner.attendedCalls != 1 || runner.runCalls != 0 {
		t.Fatalf("runner calls: attended=%d run=%d, want attended only", runner.attendedCalls, runner.runCalls)
	}
	if runner.name != "claude" || len(runner.args) != 1 || !strings.Contains(runner.args[0], "You are assisting a human with a failed task") {
		t.Fatalf("assistance command = %s %v", runner.name, runner.args)
	}
	if !strings.Contains(out, "Starting Failed assistance: claude") {
		t.Fatalf("missing assistance start detail:\n%s", out)
	}
	if strings.Count(out, "Choose [1]:") < 2 {
		t.Fatalf("assistance did not refresh and re-show the Failed gate:\n%s", out)
	}
	// The assist agent did not change task state; the task is still failed.
	assertTaskFailed(t, env.execFixture(), "01-a", 3)
}

func TestRunTaskSetYesPrintsConciseSummary(t *testing.T) {
	env := setupRunTaskSetFixture(t, "demo", []Task{
		{ID: "01-a", File: "01-a.md", Title: "A", Type: "AFK", Status: "open"},
	})
	agent := writeFakeAgent(t, env.root, fakeAgentConfig{checkTask: true, summary: "ok"})

	var buf bytes.Buffer
	_, err := RunTaskSetWith(env.deps(), nil, nil, env.runTaskSetOpts(true, agent, &buf))
	if err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	for _, want := range []string{
		"━━ Running task demo/01-a: A",
		"   Attempt 1/3",
		"── Agent output",
		"── Agent finished for demo/01-a",
		"✓ Completed demo/01-a",
		"✓ Completed task set demo",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("output missing %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "\033[") {
		t.Fatalf("redirected output contains ANSI:\n%q", out)
	}
	if !strings.Contains(out, "Completed demo/01-a") || !strings.Contains(out, "Completed task set demo") {
		t.Fatalf("missing concise summary:\n%s", out)
	}
	if strings.Count(out, "STATUS") != 1 {
		t.Fatalf("expected pre-run table only:\n%s", out)
	}
}

func TestRunTaskSetAttemptStartPrintsRequestedAgent(t *testing.T) {
	env := setupRunTaskSetFixture(t, "demo", []Task{
		{ID: "01-a", File: "01-a.md", Title: "A", Type: "AFK", Status: "open"},
	})
	agent := writeFakeAgent(t, env.root, fakeAgentConfig{checkTask: true, summary: "ok"})
	opts := env.runTaskSetOpts(true, agent, nil)
	opts.AgentPreset = "claude --model opus4.8"

	var buf bytes.Buffer
	opts.Output = &buf
	_, err := RunTaskSetWith(env.deps(), nil, nil, opts)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(buf.String(), "   Attempt 1/3 · claude --model opus4.8") {
		t.Fatalf("attempt start missing requested agent:\n%s", buf.String())
	}
}

func TestRunTaskSetAttemptStartPrintsEffortResolvedRequestedAgent(t *testing.T) {
	env := setupRunTaskSetFixture(t, "demo", []Task{
		{ID: "01-a", File: "01-a.md", Title: "A", Type: "AFK", Status: "open", Effort: "heavy", EffortExplicit: true},
	})
	runner := &captureAgentRunner{}
	d := env.deps()
	d.Runner = runner

	var buf bytes.Buffer
	opts := env.runTaskSetOpts(true, "", &buf)
	opts.AgentPreset = "claude"
	opts.MaxTries = 1

	_, err := RunTaskSetWith(d, nil, nil, opts)
	assertExitCode(t, err, ExitOperational)
	if !strings.Contains(buf.String(), "   Attempt 1/1 · claude --model opus") {
		t.Fatalf("attempt start missing effort-resolved requested agent:\n%s", buf.String())
	}
	if len(runner.argLists) != 1 || runner.argLists[0][0] != "--model" || runner.argLists[0][1] != "opus" {
		t.Fatalf("agent args = %v, want leading --model opus", runner.argLists)
	}
}

func TestRunTaskSetAgentFallbackAdvancesOnQuota(t *testing.T) {
	env := setupRunTaskSetFixture(t, "demo", []Task{
		{ID: "01-a", File: "01-a.md", Title: "A", Type: "AFK", Status: "open"},
	})
	installAgentShim(t, env.root, "claude", `#!/bin/sh
printf '%s\n' '{"type":"result","subtype":"error_during_execution","result":"You'\''ve hit your weekly limit · resets Mon 12:00am"}'
`)
	installAgentShim(t, env.root, "codex", `#!/bin/sh
TASK=$(printf '%s' "$*" | sed -n 's|^.*You are implementing the task at: ||p' | head -1 | awk '{print $1}')
if [ -n "$TASK" ] && [ -f "$TASK" ]; then sed -i '' 's/- \[ \]/- [x]/g' "$TASK" 2>/dev/null || sed -i 's/- \[ \]/- [x]/g' "$TASK"; fi
printf 'SUMMARY_START\ncodex done\nSUMMARY_END\nTASK_COMPLETE\n'
`)

	var buf bytes.Buffer
	opts := env.runTaskSetOpts(true, "", &buf)
	opts.AgentPresets = []string{"claude", "codex"}
	opts.AgentExplicit = true
	opts.MaxTries = 1

	result, err := RunTaskSetWith(env.deps(), nil, nil, opts)
	if err != nil {
		t.Fatal(err)
	}
	if !result.TaskSetDone || len(result.Completed) != 1 {
		t.Fatalf("result = %#v", result)
	}
	out := buf.String()
	if !strings.Contains(out, "Attempt 1/1 · claude") || !strings.Contains(out, "Attempt 1/1 · codex") {
		t.Fatalf("fallback attempts not rendered:\n%s", out)
	}
	assertTaskDone(t, env.execFixture(), "01-a")
}

func TestRunTaskSetAgentFallbackDoesNotAdvanceOnPlainFailure(t *testing.T) {
	env := setupRunTaskSetFixture(t, "demo", []Task{
		{ID: "01-a", File: "01-a.md", Title: "A", Type: "AFK", Status: "open"},
	})
	claudeCount := filepath.Join(env.root, ".agent-bin", "claude.count")
	codexCount := filepath.Join(env.root, ".agent-bin", "codex.count")
	installAgentShim(t, env.root, "claude", fmt.Sprintf(`#!/bin/sh
n=0
test -f %[1]q && n=$(cat %[1]q)
n=$((n + 1))
printf '%%s\n' "$n" > %[1]q
printf 'TASK_FAILED: plain failure\n'
exit 2
`, claudeCount))
	installAgentShim(t, env.root, "codex", fmt.Sprintf(`#!/bin/sh
printf 'called\n' >> %[1]q
TASK=$(printf '%%s' "$*" | sed -n 's|^.*You are implementing the task at: ||p' | head -1 | awk '{print $1}')
if [ -n "$TASK" ] && [ -f "$TASK" ]; then sed -i '' 's/- \[ \]/- [x]/g' "$TASK" 2>/dev/null || sed -i 's/- \[ \]/- [x]/g' "$TASK"; fi
printf 'SUMMARY_START\ncodex done\nSUMMARY_END\nTASK_COMPLETE\n'
`, codexCount))

	opts := env.runTaskSetOpts(true, "", io.Discard)
	opts.AgentPresets = []string{"claude", "codex"}
	opts.AgentExplicit = true
	opts.MaxTries = 2

	_, err := RunTaskSetWith(env.deps(), nil, nil, opts)
	assertExitCode(t, err, ExitOperational)
	if got := strings.TrimSpace(readFileString(t, claudeCount)); got != "2" {
		t.Fatalf("claude attempts = %q, want 2", got)
	}
	if _, err := os.Stat(codexCount); !os.IsNotExist(err) {
		t.Fatalf("codex should not be called on plain failure: %v", err)
	}
}

func TestRunTaskSetAgentFallbackReportsEarliestReset(t *testing.T) {
	env := setupRunTaskSetFixture(t, "demo", []Task{
		{ID: "01-a", File: "01-a.md", Title: "A", Type: "AFK", Status: "open"},
	})
	installAgentShim(t, env.root, "claude", `#!/bin/sh
printf '%s\n' '{"type":"result","subtype":"error_during_execution","result":"You'\''ve hit your weekly limit · resets Mon 12:00am"}'
`)
	installAgentShim(t, env.root, "codex", `#!/bin/sh
printf '%s\n' '{"type":"error","message":"You'\''ve hit your usage limit. try again at 11:59 PM."}'
`)

	opts := env.runTaskSetOpts(true, "", io.Discard)
	opts.AgentPresets = []string{"claude", "codex"}
	opts.AgentExplicit = true
	opts.MaxTries = 1

	result, err := RunTaskSetWith(env.deps(), nil, nil, opts)
	if err != nil {
		t.Fatal(err)
	}
	if !result.QuotaPaused || result.PausePreset != "codex" {
		t.Fatalf("result = %#v, want codex quota pause", result)
	}
	if result.PauseResetAt.IsZero() {
		t.Fatal("expected reset time to be reported")
	}
	assertTaskOpen(t, env.execFixture(), "01-a")
}

func TestRunTaskSetAgentFallbackSkipsCoolingAgentBeforeSpawn(t *testing.T) {
	env := setupRunTaskSetFixture(t, "demo", []Task{
		{ID: "01-a", File: "01-a.md", Title: "A", Type: "AFK", Status: "open"},
	})
	codexCount := filepath.Join(env.root, ".agent-bin", "codex.count")
	installAgentShim(t, env.root, "codex", fmt.Sprintf(`#!/bin/sh
printf 'called\n' >> %[1]q
`, codexCount))
	installAgentShim(t, env.root, "claude", `#!/bin/sh
TASK=$(printf '%s' "$*" | sed -n 's|^.*You are implementing the task at: ||p' | head -1 | awk '{print $1}')
if [ -n "$TASK" ] && [ -f "$TASK" ]; then sed -i '' 's/- \[ \]/- [x]/g' "$TASK" 2>/dev/null || sed -i 's/- \[ \]/- [x]/g' "$TASK"; fi
printf 'SUMMARY_START\nclaude done\nSUMMARY_END\nTASK_COMPLETE\n'
`)
	if err := updateAgentCooldown(env.deps(), "codex", time.Now().Add(time.Hour)); err != nil {
		t.Fatal(err)
	}

	opts := env.runTaskSetOpts(true, "", io.Discard)
	opts.AgentPresets = []string{"codex", "claude"}
	opts.AgentExplicit = true
	opts.MaxTries = 1

	result, err := RunTaskSetWith(env.deps(), nil, nil, opts)
	if err != nil {
		t.Fatal(err)
	}
	if !result.TaskSetDone || len(result.Completed) != 1 {
		t.Fatalf("result = %#v", result)
	}
	if _, err := os.Stat(codexCount); !os.IsNotExist(err) {
		t.Fatalf("cooling codex should not be spawned: %v", err)
	}
	assertTaskDone(t, env.execFixture(), "01-a")
}

func TestRunTaskSetAgentFallbackWritesResetAwareCooldown(t *testing.T) {
	env := setupRunTaskSetFixture(t, "demo", []Task{
		{ID: "01-a", File: "01-a.md", Title: "A", Type: "AFK", Status: "open"},
	})
	reason := "You've hit your usage limit. try again at 11:59 PM."
	installAgentShim(t, env.root, "codex", fmt.Sprintf("#!/bin/sh\nprintf '%%s\\n' %q\n", fmt.Sprintf(`{"type":"error","message":%s}`, strconv.Quote(reason))))

	opts := env.runTaskSetOpts(true, "", io.Discard)
	opts.AgentPresets = []string{"codex"}
	opts.AgentExplicit = true
	opts.MaxTries = 1
	before := time.Now()
	result, err := RunTaskSetWith(env.deps(), nil, nil, opts)
	if err != nil {
		t.Fatal(err)
	}
	if !result.QuotaPaused || result.PausePreset != "codex" {
		t.Fatalf("result = %#v", result)
	}

	store, err := readAgentCooldowns(env.deps())
	if err != nil {
		t.Fatal(err)
	}
	want := codexQuotaResetAt(reason, before).Add(agentQuotaResetSkew)
	got := store["codex"].ExhaustedUntil
	if got.Before(want.Add(-5*time.Second)) || got.After(want.Add(5*time.Second)) {
		t.Fatalf("cooldown until = %s, want around reset+skew %s", got, want)
	}
	if result.PauseResetAt.IsZero() || !got.After(result.PauseResetAt) {
		t.Fatalf("result reset = %s, store cooldown = %s", result.PauseResetAt, got)
	}
	assertTaskOpen(t, env.execFixture(), "01-a")
}

func TestRunTaskSetAgentFallbackWritesFixedIntervalCooldownWhenResetMissing(t *testing.T) {
	env := setupRunTaskSetFixture(t, "demo", []Task{
		{ID: "01-a", File: "01-a.md", Title: "A", Type: "AFK", Status: "open"},
	})
	installAgentShim(t, env.root, "claude", `#!/bin/sh
printf '%s\n' '{"type":"result","subtype":"error_during_execution","result":"You'\''ve hit your weekly limit"}'
`)
	loadConfig := func(string) (*config.Config, error) {
		return &config.Config{Queue: &config.QueueConfig{AgentQuotaRetryAfter: "17m"}}, nil
	}

	opts := env.runTaskSetOpts(true, "", io.Discard)
	opts.AgentPresets = []string{"claude"}
	opts.AgentExplicit = true
	opts.MaxTries = 1
	before := time.Now().UTC()
	result, err := RunTaskSetWith(env.deps(), nil, loadConfig, opts)
	after := time.Now().UTC()
	if err != nil {
		t.Fatal(err)
	}
	if !result.QuotaPaused || result.PausePreset != "claude" {
		t.Fatalf("result = %#v", result)
	}

	store, err := readAgentCooldowns(env.deps())
	if err != nil {
		t.Fatal(err)
	}
	got := store["claude"].ExhaustedUntil
	if got.Before(before.Add(17*time.Minute)) || got.After(after.Add(17*time.Minute+2*time.Second)) {
		t.Fatalf("cooldown until = %s, want about now+17m", got)
	}
	assertTaskOpen(t, env.execFixture(), "01-a")
}

func TestRunTaskSetAgentFallbackAllCoolingReturnsEarliestWithoutSpawn(t *testing.T) {
	env := setupRunTaskSetFixture(t, "demo", []Task{
		{ID: "01-a", File: "01-a.md", Title: "A", Type: "AFK", Status: "open"},
	})
	claudeCount := filepath.Join(env.root, ".agent-bin", "claude.count")
	codexCount := filepath.Join(env.root, ".agent-bin", "codex.count")
	installAgentShim(t, env.root, "claude", fmt.Sprintf(`#!/bin/sh
printf 'called\n' >> %[1]q
`, claudeCount))
	installAgentShim(t, env.root, "codex", fmt.Sprintf(`#!/bin/sh
printf 'called\n' >> %[1]q
`, codexCount))
	earliest := time.Now().UTC().Add(10 * time.Minute).Truncate(time.Second)
	later := time.Now().UTC().Add(30 * time.Minute).Truncate(time.Second)
	if err := updateAgentCooldown(env.deps(), "claude", later); err != nil {
		t.Fatal(err)
	}
	if err := updateAgentCooldown(env.deps(), "codex", earliest); err != nil {
		t.Fatal(err)
	}

	opts := env.runTaskSetOpts(true, "", io.Discard)
	opts.AgentPresets = []string{"claude", "codex"}
	opts.AgentExplicit = true
	opts.MaxTries = 1

	result, err := RunTaskSetWith(env.deps(), nil, nil, opts)
	if err != nil {
		t.Fatal(err)
	}
	if !result.QuotaPaused || result.PausePreset != "codex" || !result.PauseResetAt.Equal(earliest) {
		t.Fatalf("result = %#v, want codex earliest reset %s", result, earliest)
	}
	if _, err := os.Stat(claudeCount); !os.IsNotExist(err) {
		t.Fatalf("cooling claude should not be spawned: %v", err)
	}
	if _, err := os.Stat(codexCount); !os.IsNotExist(err) {
		t.Fatalf("cooling codex should not be spawned: %v", err)
	}
	assertTaskOpen(t, env.execFixture(), "01-a")
}

func TestRunTaskSetDefaultAgentsConfigAndFlagOverride(t *testing.T) {
	loadConfig := func(string) (*config.Config, error) {
		return &config.Config{Task: &config.TaskConfig{DefaultAgents: []string{"codex"}}}, nil
	}
	t.Run("config default", func(t *testing.T) {
		env := setupRunTaskSetFixture(t, "demo", []Task{
			{ID: "01-a", File: "01-a.md", Title: "A", Type: "AFK", Status: "open"},
		})
		installAgentShim(t, env.root, "codex", `#!/bin/sh
TASK=$(printf '%s' "$*" | sed -n 's|^.*You are implementing the task at: ||p' | head -1 | awk '{print $1}')
if [ -n "$TASK" ] && [ -f "$TASK" ]; then sed -i '' 's/- \[ \]/- [x]/g' "$TASK" 2>/dev/null || sed -i 's/- \[ \]/- [x]/g' "$TASK"; fi
printf 'SUMMARY_START\ncodex done\nSUMMARY_END\nTASK_COMPLETE\n'
`)
		var buf bytes.Buffer
		opts := env.runTaskSetOpts(true, "", &buf)
		opts.MaxTries = 1

		result, err := RunTaskSetWith(env.deps(), nil, loadConfig, opts)
		if err != nil {
			t.Fatal(err)
		}
		if !result.TaskSetDone || !strings.Contains(buf.String(), "Attempt 1/1 · codex") {
			t.Fatalf("result = %#v output:\n%s", result, buf.String())
		}
	})
	t.Run("flag override", func(t *testing.T) {
		env := setupRunTaskSetFixture(t, "demo", []Task{
			{ID: "01-a", File: "01-a.md", Title: "A", Type: "AFK", Status: "open"},
		})
		installAgentShim(t, env.root, "claude", `#!/bin/sh
TASK=$(printf '%s' "$*" | sed -n 's|^.*You are implementing the task at: ||p' | head -1 | awk '{print $1}')
if [ -n "$TASK" ] && [ -f "$TASK" ]; then sed -i '' 's/- \[ \]/- [x]/g' "$TASK" 2>/dev/null || sed -i 's/- \[ \]/- [x]/g' "$TASK"; fi
printf 'SUMMARY_START\nclaude done\nSUMMARY_END\nTASK_COMPLETE\n'
`)
		var buf bytes.Buffer
		opts := env.runTaskSetOpts(true, "", &buf)
		opts.AgentPresets = []string{"claude"}
		opts.AgentExplicit = true
		opts.MaxTries = 1

		result, err := RunTaskSetWith(env.deps(), nil, loadConfig, opts)
		if err != nil {
			t.Fatal(err)
		}
		if !result.TaskSetDone || !strings.Contains(buf.String(), "Attempt 1/1 · claude") || strings.Contains(buf.String(), "codex") {
			t.Fatalf("flag did not override config: result=%#v output:\n%s", result, buf.String())
		}
	})
}

func installAgentShim(t *testing.T, root, name, script string) string {
	t.Helper()
	dir := filepath.Join(root, ".agent-bin")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	return path
}

func readFileString(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func TestRunTaskSetInteractivePrintsRefreshedTable(t *testing.T) {
	env := setupRunTaskSetFixture(t, "demo", []Task{
		{ID: "01-a", File: "01-a.md", Title: "A", Type: "AFK", Status: "open"},
	})
	agent := writeFakeAgent(t, env.root, fakeAgentConfig{checkTask: true, summary: "ok"})

	var buf bytes.Buffer
	opts := env.runTaskSetOpts(false, agent, &buf)

	_, err := RunTaskSetWith(env.deps(), nil, nil, opts)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Count(buf.String(), "STATUS") < 2 {
		t.Fatalf("expected pre and post tables:\n%s", buf.String())
	}
}

func TestRunTaskSetNonInteractiveProceedsWithoutAFKConsent(t *testing.T) {
	env := setupRunTaskSetFixture(t, "demo", []Task{
		{ID: "01-a", File: "01-a.md", Title: "A", Type: "AFK", Status: "open"},
	})
	agent := writeFakeAgent(t, env.root, fakeAgentConfig{checkTask: true, summary: "done"})
	opts := env.runTaskSetOpts(false, agent, nil)
	opts.ConfirmIn = NonInteractiveReader{}

	result, err := RunTaskSetWith(env.deps(), nil, nil, opts)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Completed) != 1 {
		t.Fatalf("result = %#v", result)
	}
}

func TestRunTaskSetInterruptionPropagation(t *testing.T) {
	env := setupRunTaskSetFixture(t, "demo", []Task{
		{ID: "01-a", File: "01-a.md", Title: "A", Type: "AFK", Status: "open"},
	})
	agent := writeSlowAgent(t, env.root, 10*time.Second)

	opts := env.runTaskSetOpts(true, agent, nil)
	opts.Timeout = time.Minute
	signalOwnPidWhenAgentStarts(t, env.root)

	_, err := RunTaskSetWith(env.deps(), nil, nil, opts)
	assertExitCode(t, err, ExitInterrupted)
	assertTaskOpen(t, env.execFixture(), "01-a")
}

func TestRunTaskSetStopsCleanlyOnDeferred(t *testing.T) {
	env := setupRunTaskSetFixture(t, "demo", []Task{
		{ID: "01-a", File: "01-a.md", Title: "A", Type: "AFK", Status: "open"},
		{ID: "02-skip", File: "02-skip.md", Title: "Skip", Type: "HITL", Status: "skipped"},
	})
	agent := writeFakeAgent(t, env.root, fakeAgentConfig{checkTask: true, summary: "ok"})

	var buf bytes.Buffer
	result, err := RunTaskSetWith(env.deps(), nil, nil, env.runTaskSetOpts(true, agent, &buf))
	if err != nil {
		t.Fatalf("run failed (deferred should not error): %v", err)
	}
	if !result.TaskSetDeferred {
		t.Fatalf("result = %#v, want TaskSetDeferred", result)
	}
	if result.TaskSetDone {
		t.Fatal("deferred set must not be reported as done")
	}
	if len(result.Completed) != 1 {
		t.Fatalf("completed = %d, want 1", len(result.Completed))
	}
	if len(result.SkippedTasks) != 1 || result.SkippedTasks[0] != "02-skip" {
		t.Fatalf("skipped tasks = %v, want [02-skip]", result.SkippedTasks)
	}
	out := buf.String()
	if !strings.Contains(out, "deferred") || !strings.Contains(out, "02-skip") {
		t.Fatalf("missing deferral message:\n%s", out)
	}
	assertTaskDone(t, env.execFixture(), "01-a")
}

func TestSelectTaskSetAutomaticAndExplicit(t *testing.T) {
	refresh := &RefreshResult{
		Rows: []Row{
			{ID: "auto", Status: StatusReady, Priority: 10},
			{ID: "target", Status: StatusReady, Priority: 0},
		},
		Manifests: map[string]*Manifest{
			"auto": {Stem: "auto", Valid: true, Tasks: []Task{
				{ID: "01-a", File: "01-a.md", Type: "AFK", Status: "open"},
			}},
			"target": {Stem: "target", Valid: true, Tasks: []Task{
				{ID: "01-x", File: "01-x.md", Type: "AFK", Status: "open"},
			}},
		},
	}

	id, fallback, err := SelectTaskSet(refresh, "")
	if err != nil || id != "auto" {
		t.Fatalf("auto = %q, err = %v", id, err)
	}
	if fallback {
		t.Fatalf("Ready selection must not be a HITL fallback")
	}
	id, _, err = SelectTaskSet(refresh, "target")
	if err != nil || id != "target" {
		t.Fatalf("target = %q, err = %v", id, err)
	}
}

// TestRunTaskSetRefusesWhenSetLiveElsewhere proves the ADR-0035 cross-checkout
// backstop still fires through the implement drain — and that checkout adoption
// (ADR-0036) is correctly sequenced after it: a refused run never reaches the
// BindCheckout hook, so a live set is never re-bound from a losing checkout.
func TestRunTaskSetRefusesWhenSetLiveElsewhere(t *testing.T) {
	env := setupRunTaskSetFixture(t, "demo", []Task{
		{ID: "01-a", File: "01-a.md", Title: "A", Type: "AFK", Status: "open"},
	})
	d := env.deps()
	d.ProcessAlive = func(pid int) bool { return pid == os.Getpid() }

	// A sibling linked worktree of the same repo holds a live lock for "demo".
	other := filepath.Join(t.TempDir(), "sibling")
	runGit(t, env.root, "worktree", "add", "-b", "sibling-branch", other, "HEAD")
	lock, err := AcquireRuntimeLockForSet(d, other, "demo", io.Discard)
	if err != nil {
		t.Fatalf("acquire sibling lock: %v", err)
	}
	t.Cleanup(func() { _ = lock.Release() })

	bindCalls := 0
	opts := env.runTaskSetOpts(true, "", io.Discard)
	opts.BindCheckout = func(setID, projectPath, runtimePath string) error {
		bindCalls++
		return nil
	}

	_, err = RunTaskSetWith(d, nil, nil, opts)
	assertExitCode(t, err, ExitOperational)
	if !strings.Contains(err.Error(), "already in progress") {
		t.Fatalf("err = %v, want cross-checkout refusal", err)
	}
	if bindCalls != 0 {
		t.Fatalf("BindCheckout must not run when the drain is refused; got %d calls", bindCalls)
	}
}

// TestRunTaskSetInvokesBindCheckoutAfterBackstop confirms the drain reaches the
// BindCheckout hook with the resolved set and runtime checkout once the
// cross-checkout backstop passes.
func TestRunTaskSetInvokesBindCheckoutAfterBackstop(t *testing.T) {
	env := setupRunTaskSetFixture(t, "demo", []Task{
		{ID: "01-a", File: "01-a.md", Title: "A", Type: "AFK", Status: "open"},
	})
	agent := writeFakeAgent(t, env.root, fakeAgentConfig{checkTask: true, summary: "ok"})

	var gotSet, gotRuntime string
	opts := env.runTaskSetOpts(true, agent, io.Discard)
	opts.BindCheckout = func(setID, projectPath, runtimePath string) error {
		gotSet, gotRuntime = setID, runtimePath
		return nil
	}

	if _, err := RunTaskSetWith(env.deps(), nil, nil, opts); err != nil {
		t.Fatalf("run failed: %v", err)
	}
	if gotSet != "demo" {
		t.Fatalf("BindCheckout setID = %q, want demo", gotSet)
	}
	if gotRuntime == "" {
		t.Fatalf("BindCheckout runtimePath must be the resolved checkout")
	}
}

type runTaskSetFixture struct {
	root     string
	tasksDir string
}

func setupRunTaskSetFixture(t *testing.T, stem string, tasks []Task) *runTaskSetFixture {
	t.Helper()
	root := t.TempDir()
	initExecutorGitRepo(t, root)
	t.Setenv("XDG_DATA_HOME", filepath.Join(root, ".xdg"))
	tasksDir := storageTasksDir(t, root)
	setupManifest(t, tasksDir, stem, tasks)
	if _, err := RefreshWith(DefaultDeps(), tasksDir, DefaultStatePath()); err != nil {
		t.Fatal(err)
	}
	return &runTaskSetFixture{root: root, tasksDir: tasksDir}
}

func (e *runTaskSetFixture) deps() *Deps {
	return &Deps{
		FS:     deps.NewRealFileSystem(),
		Git:    deps.NewRealGit(),
		Runner: RealCommandRunner{},
	}
}

func (e *runTaskSetFixture) execFixture() *execFixture {
	return &execFixture{root: e.root, tasksDir: e.tasksDir}
}

func (e *runTaskSetFixture) runTaskSetOpts(yes bool, agentCmd string, out io.Writer) RunTaskSetOptions {
	opts := RunTaskSetOptions{
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

type checkingPromptReader struct {
	t        *testing.T
	check    func(*testing.T)
	response string
	done     bool
}

func (r *checkingPromptReader) Read(p []byte) (int, error) {
	if r.done {
		return 0, io.EOF
	}
	r.check(r.t)
	r.done = true
	return copy(p, r.response), nil
}

type hitlAssistanceRunner struct {
	t        *testing.T
	tasksDir string
	calls    int
	name     string
	args     []string
}

func (r *hitlAssistanceRunner) Run(ctx context.Context, dir string, stdout, stderr io.Writer, name string, args ...string) (int, error) {
	r.calls++
	r.name = name
	r.args = append([]string{}, args...)
	m := LoadManifest(DefaultDeps(), "demo", filepath.Join(r.tasksDir, "demo", "index.json"))
	for i := range m.Tasks {
		if m.Tasks[i].ID == "02-hitl" {
			m.Tasks[i].Status = "done"
		}
	}
	if err := WriteManifestAtomic(DefaultDeps(), m); err != nil {
		r.t.Fatal(err)
	}
	fmt.Fprintln(stdout, "assistance complete")
	return 0, nil
}

func (r *hitlAssistanceRunner) Start(ctx context.Context, dir string, stdout, stderr io.Writer, name string, args ...string) (*ManagedProcess, error) {
	return RealCommandRunner{}.Start(ctx, dir, stdout, stderr, name, args...)
}

type configurableHITLAssistanceRunner struct {
	t             *testing.T
	tasksDir      string
	calls         int
	runCalls      int
	attendedCalls int
	name          string
	args          []string
	exitCode      int
	runErr        error
	onRun         func(*testing.T, string)
}

func (r *configurableHITLAssistanceRunner) Run(ctx context.Context, dir string, stdout, stderr io.Writer, name string, args ...string) (int, error) {
	r.runCalls++
	return r.run(name, args...)
}

func (r *configurableHITLAssistanceRunner) RunAttended(ctx context.Context, dir string, stdin io.Reader, stdout, stderr io.Writer, name string, args ...string) (int, error) {
	r.attendedCalls++
	return r.run(name, args...)
}

func (r *configurableHITLAssistanceRunner) run(name string, args ...string) (int, error) {
	r.calls++
	r.name = name
	r.args = append([]string{}, args...)
	if r.runErr != nil {
		return 1, r.runErr
	}
	if r.onRun != nil {
		r.onRun(r.t, r.tasksDir)
	}
	return r.exitCode, nil
}

func (r *configurableHITLAssistanceRunner) Start(ctx context.Context, dir string, stdout, stderr io.Writer, name string, args ...string) (*ManagedProcess, error) {
	return RealCommandRunner{}.Start(ctx, dir, stdout, stderr, name, args...)
}

func setTaskStatus(t *testing.T, tasksDir, taskID, status string, failedAfter *int) {
	t.Helper()
	m := LoadManifest(DefaultDeps(), "demo", filepath.Join(tasksDir, "demo", "index.json"))
	for i := range m.Tasks {
		if m.Tasks[i].ID == taskID {
			m.Tasks[i].Status = status
			m.Tasks[i].FailedAfter = failedAfter
			if err := WriteManifestAtomic(DefaultDeps(), m); err != nil {
				t.Fatal(err)
			}
			return
		}
	}
	t.Fatalf("task %s not found", taskID)
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
	b.WriteString("TASK=$(printf '%s' \"$1\" | sed -n 's|^You are implementing the task at: ||p' | head -1)\n")
	b.WriteString("if [ -n \"$TASK\" ] && [ -f \"$TASK\" ]; then sed -i '' 's/- \\[ \\]/- [x]/g' \"$TASK\" 2>/dev/null || sed -i 's/- \\[ \\]/- [x]/g' \"$TASK\"; fi\n")
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
