package tasks

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"strings"
	"testing"
	"time"
)

// newHITLGateRun builds an implementRun wired to the sole-Human-blocked fixture
// (a real store-backed checkout holding a live Drain) with just the fields the
// HITL gate choreography reads, so r.hitlGate can be driven directly. confirmIn /
// yes decide whether the menu will actually prompt.
func newHITLGateRun(t *testing.T, confirmIn io.Reader, yes bool) (*implementRun, *Deps, string, *Manifest, *Task) {
	t.Helper()
	env, agent := setupSoleHumanBlockedFixture(t)
	d := env.deps()
	d.ProcessAlive = func(pid int) bool { return pid == os.Getpid() }

	runtimePath, err := ResolveRuntimePathWith(d, env.root, "")
	if err != nil {
		t.Fatalf("resolve runtime path: %v", err)
	}
	statePath := DefaultStatePath()
	refresh, err := RefreshWith(d, env.tasksDir, statePath)
	if err != nil {
		t.Fatalf("refresh: %v", err)
	}
	m := refresh.Manifests["solo"]
	hitl := BlockingHITLTask(m)
	if hitl == nil {
		t.Fatal("fixture must present a blocking HITL task")
	}

	handle, err := BeginDrain(d, runtimePath, "solo", io.Discard)
	if err != nil {
		t.Fatalf("BeginDrain: %v", err)
	}

	run := &implementRun{
		d:    d,
		plan: &runPlan{},
		opts: RunTaskSetOptions{
			ResolveInput: ResolveInput{CWD: env.root},
			AgentCmd:     agent,
			ConfirmIn:    confirmIn,
			Yes:          yes,
		},
		resolved:    &ResolvedPaths{DefinitionPath: env.tasksDir, ProjectPath: env.root},
		runtimePath: runtimePath,
		statePath:   statePath,
		taskSetID:   "solo",
		confirmOut:  io.Discard,
		out:         &bytes.Buffer{},
		timeout:     time.Minute,
		drain:       handle,
	}
	t.Cleanup(func() {
		if run.drain != nil {
			finalizeDrain(run.drain, false, nil, false, false, nil)
		}
	})
	return run, d, runtimePath, m, hitl
}

// TestHITLGateShowsBlockedWaiterCount pins the ADR-0100 blocked-waiter line: a
// hold-registering gate menu prints how many quota-recovery waiters are queued
// behind the same checkout, and prints nothing when none are registered.
func TestHITLGateShowsBlockedWaiterCount(t *testing.T) {
	const blockedLine = "blocked on this checkout"

	t.Run("waiters present", func(t *testing.T) {
		run, d, runtimePath, m, hitl := newHITLGateRun(t, &checkingPromptReader{
			t: t, check: func(*testing.T) {}, response: "0\n",
		}, false)
		if _, err := RegisterRecoveryWaiter(d, RecoveryWaiter{
			SetID:       "waiter-set",
			Preset:      "sonnet",
			ResetAt:     time.Now().Add(time.Hour),
			RuntimePath: runtimePath,
		}); err != nil {
			t.Fatalf("RegisterRecoveryWaiter: %v", err)
		}

		if _, err := run.hitlGate(m, hitl); err != nil {
			t.Fatalf("hitlGate: %v", err)
		}
		got := run.out.(*bytes.Buffer).String()
		if !strings.Contains(got, "1 quota waiter "+blockedLine) {
			t.Fatalf("gate menu missing blocked-waiter count line; output:\n%s", got)
		}
	})

	t.Run("no waiters", func(t *testing.T) {
		run, _, _, m, hitl := newHITLGateRun(t, &checkingPromptReader{
			t: t, check: func(*testing.T) {}, response: "0\n",
		}, false)

		if _, err := run.hitlGate(m, hitl); err != nil {
			t.Fatalf("hitlGate: %v", err)
		}
		got := run.out.(*bytes.Buffer).String()
		if strings.Contains(got, blockedLine) {
			t.Fatalf("gate menu printed a blocked-waiter line with zero waiters; output:\n%s", got)
		}
	})
}

// TestImplementRunHITLGateHoldPairsAroundPromptingMenu pins the gate-hold
// register/release pairing the terminal-status HITL choreography (ADR-0067/0100)
// depends on: a prompting HITL gate parks the Drain and holds a checkout gate
// hold for the whole menu, and releases it when the menu ends. The hold is
// observed *during* the menu via a prompt reader whose Read callback fires while
// the handler waits on a selection.
func TestImplementRunHITLGateHoldPairsAroundPromptingMenu(t *testing.T) {
	var (
		run         *implementRun
		d           *Deps
		runtimePath string
		holdSeen    bool
	)
	check := func(t *testing.T) {
		t.Helper()
		hold, err := GetCheckoutGateHold(d, runtimePath)
		if err != nil {
			t.Fatalf("GetCheckoutGateHold during menu: %v", err)
		}
		if hold == nil || hold.SetID != "solo" {
			t.Fatalf("gate hold missing/mismatched during menu: %#v", hold)
		}
		// Parking dropped the live Drain so the menu runs lock-free.
		if run.drain != nil {
			t.Fatal("a prompting HITL gate must park the Drain before the menu")
		}
		holdSeen = true
	}
	reader := &checkingPromptReader{t: t, check: check, response: "0\n"}

	run, d, runtimePath, m, hitl := newHITLGateRun(t, reader, false)

	handled, err := run.hitlGate(m, hitl)
	if err != nil {
		t.Fatalf("hitlGate: %v", err)
	}
	if handled {
		t.Fatal("Exit (menu 0) must return handled=false")
	}
	if !holdSeen {
		t.Fatal("the menu never prompted; the hold-present assertion did not run")
	}
	if run.drain != nil {
		t.Fatal("the Drain must remain parked after a prompting gate")
	}
	hold, err := GetCheckoutGateHold(d, runtimePath)
	if err != nil {
		t.Fatalf("GetCheckoutGateHold after menu: %v", err)
	}
	if hold != nil {
		t.Fatalf("gate hold leaked after the menu ended: %#v", hold)
	}
}

// TestImplementRunHITLGateSkipsParkWhenNotPrompting pins the other half of the
// pairing: under --yes the menu will not prompt, so hitlGate must neither park
// the Drain nor register a gate hold — the held Drain stays live and the normal
// finalize records the terminal (ADR-0067).
func TestImplementRunHITLGateSkipsParkWhenNotPrompting(t *testing.T) {
	run, d, runtimePath, m, hitl := newHITLGateRun(t, nil, true)
	held := run.drain

	handled, err := run.hitlGate(m, hitl)
	if err != nil {
		t.Fatalf("hitlGate: %v", err)
	}
	if handled {
		t.Fatal("a non-prompting gate must return handled=false")
	}
	if run.drain != held {
		t.Fatal("a non-prompting gate must not park the Drain")
	}
	hold, err := GetCheckoutGateHold(d, runtimePath)
	if err != nil {
		t.Fatalf("GetCheckoutGateHold: %v", err)
	}
	if hold != nil {
		t.Fatalf("a non-prompting gate must register no hold: %#v", hold)
	}
}

// fakeAttendedRunner is a minimal CommandRunner + AttendedCommandRunner fake
// recording the one attended launch runAttendedAssistanceCommand issues, so
// clipboard-delivery tests can assert the launch happens exactly once and
// carries the expected argv.
type fakeAttendedRunner struct {
	attendedCalls int
	name          string
	args          []string
}

func (f *fakeAttendedRunner) Run(ctx context.Context, dir string, stdout, stderr io.Writer, name string, args ...string) (int, error) {
	return 0, nil
}

func (f *fakeAttendedRunner) Start(ctx context.Context, dir string, stdout, stderr io.Writer, name string, args ...string) (*ManagedProcess, error) {
	return nil, nil
}

func (f *fakeAttendedRunner) RunAttended(ctx context.Context, dir string, stdin io.Reader, stdout, stderr io.Writer, name string, args ...string) (int, error) {
	f.attendedCalls++
	f.name = name
	f.args = append([]string{}, args...)
	return 0, nil
}

// TestRunAttendedAssistanceCommandDeliversClipboardBriefing pins kimi's
// clipboard-delivery path (ADR-0151): a briefing with no positional prompt
// form is copied to the clipboard before launch, and the gate tells the human
// to paste it.
func TestRunAttendedAssistanceCommandDeliversClipboardBriefing(t *testing.T) {
	runner := &fakeAttendedRunner{}
	var copied string
	d := &Deps{Runner: runner, ClipboardCopy: func(text string) error {
		copied = text
		return nil
	}}
	invocation := &AgentAssistanceInvocation{
		Command:         AgentCommand{Name: "kimi", Args: []string{"--model", "moonshot-ai/kimi-k3"}},
		ClipboardPrompt: "briefing text",
	}
	var out bytes.Buffer
	exitCode, err := runAttendedAssistanceCommand(d, nil, "/tmp/runtime", &out, invocation)
	if err != nil {
		t.Fatalf("runAttendedAssistanceCommand: %v", err)
	}
	if exitCode != 0 {
		t.Fatalf("exitCode = %d, want 0", exitCode)
	}
	if copied != "briefing text" {
		t.Fatalf("clipboard copy got %q, want the briefing", copied)
	}
	if runner.attendedCalls != 1 || runner.name != "kimi" {
		t.Fatalf("attended launch = %d call(s), name %q, want 1 call to kimi", runner.attendedCalls, runner.name)
	}
	if !strings.Contains(out.String(), "Briefing copied to clipboard") {
		t.Fatalf("output missing clipboard-paste instruction:\n%s", out.String())
	}
}

// TestRunAttendedAssistanceCommandClipboardFailureFallsBackToPrintingBriefing
// pins the never-blocks-the-launch guarantee: a clipboard write failure prints
// the full briefing text instead, and the interactive binary still launches.
func TestRunAttendedAssistanceCommandClipboardFailureFallsBackToPrintingBriefing(t *testing.T) {
	runner := &fakeAttendedRunner{}
	d := &Deps{Runner: runner, ClipboardCopy: func(text string) error {
		return fmt.Errorf("no clipboard available")
	}}
	invocation := &AgentAssistanceInvocation{
		Command:         AgentCommand{Name: "kimi"},
		ClipboardPrompt: "the full briefing text",
	}
	var out bytes.Buffer
	exitCode, err := runAttendedAssistanceCommand(d, nil, "/tmp/runtime", &out, invocation)
	if err != nil {
		t.Fatalf("runAttendedAssistanceCommand: %v", err)
	}
	if exitCode != 0 {
		t.Fatalf("exitCode = %d, want 0", exitCode)
	}
	if runner.attendedCalls != 1 {
		t.Fatalf("clipboard failure must not block the launch: attended calls = %d", runner.attendedCalls)
	}
	got := out.String()
	if !strings.Contains(got, "no clipboard available") || !strings.Contains(got, "the full briefing text") {
		t.Fatalf("output missing failure detail and fallback briefing text:\n%s", got)
	}
}

// TestRunAttendedAssistanceCommandSkipsClipboardWhenNoBriefing pins the
// no-op for every preset whose prompt already rides in argv (every preset but
// kimi): no clipboard touch, no extra output.
func TestRunAttendedAssistanceCommandSkipsClipboardWhenNoBriefing(t *testing.T) {
	runner := &fakeAttendedRunner{}
	copyCalled := false
	d := &Deps{Runner: runner, ClipboardCopy: func(text string) error {
		copyCalled = true
		return nil
	}}
	invocation := &AgentAssistanceInvocation{Command: AgentCommand{Name: "claude", Args: []string{"prompt"}}}
	var out bytes.Buffer
	if _, err := runAttendedAssistanceCommand(d, nil, "/tmp/runtime", &out, invocation); err != nil {
		t.Fatalf("runAttendedAssistanceCommand: %v", err)
	}
	if copyCalled {
		t.Fatal("a preset whose prompt rides in argv must not touch the clipboard")
	}
	if out.Len() != 0 {
		t.Fatalf("output = %q, want empty", out.String())
	}
}
