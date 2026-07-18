package tasks

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/glebglazov/pop/store"
)

// drainSetAutoDrain reads the store-backed auto-drain consent bit recorded for a
// run's registered set, so interrupt tests can assert clear/revive against the
// same value the queue's `Ready && AutoDrain` eligibility predicate reads.
func drainSetAutoDrain(t *testing.T, run *implementRun, setID string) bool {
	t.Helper()
	state, err := LoadGlobalState(run.statePath)
	if err != nil {
		t.Fatalf("LoadGlobalState: %v", err)
	}
	canon, err := CanonicalDefinitionPathWith(run.d, run.resolved.DefinitionPath)
	if err != nil {
		t.Fatalf("CanonicalDefinitionPathWith: %v", err)
	}
	for _, set := range state.Tasks[canon].TaskSets {
		if set.ID == setID {
			return set.AutoDrain
		}
	}
	t.Fatalf("set %q not registered", setID)
	return false
}

// readSetProgress returns the set-level progress.txt for the run's manifest dir,
// or "" when none was written.
func readSetProgress(t *testing.T, m *Manifest) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(m.Dir, "progress.txt"))
	if os.IsNotExist(err) {
		return ""
	}
	if err != nil {
		t.Fatalf("read progress.txt: %v", err)
	}
	return string(data)
}

// stubInterruptTask is the minimal manifest+task the interrupt gate menu reads
// (it renders the set id / task id and guards on non-nil). No store or fixture is
// needed for the handler-level menu tests.
func stubInterruptTask() (*Manifest, *Task) {
	return &Manifest{}, &Task{ID: "01-a", File: "01-a.md", Title: "A", Type: "AFK", Status: "open"}
}

// TestInterruptGateContinueKeepsDraining: choosing Continue (menu "1", and the
// empty default) returns cont=true so the drain re-runs the interrupted task.
func TestInterruptGateContinueKeepsDraining(t *testing.T) {
	m, task := stubInterruptTask()
	for _, answer := range []string{"1\n", "\n"} {
		var out bytes.Buffer
		env := gateEnv{out: &out, in: strings.NewReader(answer), taskSetID: "demo"}
		cont, err := handleInteractiveInterruptGate(env, m, task, nil)
		if err != nil {
			t.Fatalf("answer %q: handleInteractiveInterruptGate: %v", answer, err)
		}
		if !cont {
			t.Fatalf("answer %q: Continue must return cont=true", answer)
		}
		if !strings.Contains(out.String(), "Interrupted:") {
			t.Fatalf("answer %q: menu not rendered:\n%s", answer, out.String())
		}
	}
}

// TestInterruptGateExitFallsThrough: choosing Exit (menu "0") returns cont=false
// so the caller returns the interrupt error and finalize stamps the terminal.
func TestInterruptGateExitFallsThrough(t *testing.T) {
	m, task := stubInterruptTask()
	var out bytes.Buffer
	env := gateEnv{out: &out, in: strings.NewReader("0\n"), taskSetID: "demo"}
	cont, err := handleInteractiveInterruptGate(env, m, task, nil)
	if err != nil {
		t.Fatalf("handleInteractiveInterruptGate: %v", err)
	}
	if cont {
		t.Fatal("Exit must return cont=false")
	}
}

// TestInterruptGateReprompt: an unrecognized selection re-prompts, then a valid
// choice resolves — the menu loops rather than defaulting silently.
func TestInterruptGateReprompt(t *testing.T) {
	m, task := stubInterruptTask()
	var out bytes.Buffer
	env := gateEnv{out: &out, in: strings.NewReader("9\n0\n"), taskSetID: "demo"}
	cont, err := handleInteractiveInterruptGate(env, m, task, nil)
	if err != nil {
		t.Fatalf("handleInteractiveInterruptGate: %v", err)
	}
	if cont {
		t.Fatal("final choice was Exit, want cont=false")
	}
	if !strings.Contains(out.String(), "Choose 1, 2, 3, or 0.") {
		t.Fatalf("re-prompt hint missing:\n%s", out.String())
	}
}

// TestInterruptGateAssistDispatchesToSharedHandler: choosing Agent assistance
// (menu "2") launches the attended session through the shared
// runAttendedAssistanceCommand handler with the interrupt prompt, then returns to
// the interrupt menu (no state change, no refresh) where Exit resolves it.
func TestInterruptGateAssistDispatchesToSharedHandler(t *testing.T) {
	m, task := stubInterruptTask()
	runner := &configurableHITLAssistanceRunner{t: t}
	var out bytes.Buffer
	env := gateEnv{
		d:         &Deps{Runner: runner},
		out:       &out,
		in:        strings.NewReader("2\n0\n"),
		taskSetID: "demo",
	}
	cont, err := handleInteractiveInterruptGate(env, m, task, nil)
	if err != nil {
		t.Fatalf("handleInteractiveInterruptGate: %v", err)
	}
	if cont {
		t.Fatal("final choice was Exit, want cont=false")
	}
	if runner.calls != 1 || runner.attendedCalls != 1 || runner.runCalls != 0 {
		t.Fatalf("assistance must dispatch once via the attended handler: calls=%d attended=%d run=%d",
			runner.calls, runner.attendedCalls, runner.runCalls)
	}
	if len(runner.args) != 1 || !strings.Contains(runner.args[0], "You are assisting a human with an interrupted task") {
		t.Fatalf("assistance must carry the interrupt prompt, got %s %v", runner.name, runner.args)
	}
	if !strings.Contains(out.String(), "Starting interrupt assistance:") {
		t.Fatalf("missing assistance start detail:\n%s", out.String())
	}
	// The gate re-displays after assistance returns (two menu prompts).
	if strings.Count(out.String(), "Choose [1]:") < 2 {
		t.Fatalf("gate must re-display after assistance exits:\n%s", out.String())
	}
}

// TestInterruptGateShellDispatchesToSharedHandler: choosing open Runtime shell
// (menu "3") spawns the subshell through the shared spawnRuntimeShell handler at
// the Runtime path, then returns to the interrupt menu where Exit resolves it.
func TestInterruptGateShellDispatchesToSharedHandler(t *testing.T) {
	m, task := stubInterruptTask()
	runner := &shellSpawnRunner{}
	var out bytes.Buffer
	env := gateEnv{
		d:           &Deps{Runner: runner},
		out:         &out,
		in:          strings.NewReader("3\n0\n"),
		runtimePath: "/runtime/checkout",
		taskSetID:   "demo",
	}
	cont, err := handleInteractiveInterruptGate(env, m, task, nil)
	if err != nil {
		t.Fatalf("handleInteractiveInterruptGate: %v", err)
	}
	if cont {
		t.Fatal("final choice was Exit, want cont=false")
	}
	if runner.shellCalls != 1 {
		t.Fatalf("shell must spawn once, got %d", runner.shellCalls)
	}
	if runner.shellDir != "/runtime/checkout" {
		t.Fatalf("shell must root at the Runtime path, got %q", runner.shellDir)
	}
	// The gate re-displays after the shell returns (two menu prompts).
	if strings.Count(out.String(), "Choose [1]:") < 2 {
		t.Fatalf("gate must re-display after shell exits:\n%s", out.String())
	}
}

// TestInterruptGateForceQuitOnSecondSignal: a second SIGINT while the menu is up
// force-quits the process immediately (interruptGateExit) with ExitInterrupted,
// rather than reading a selection.
func TestInterruptGateForceQuitOnSecondSignal(t *testing.T) {
	m, task := stubInterruptTask()

	var gotCode int
	var called bool
	orig := interruptGateExit
	interruptGateExit = func(code int) { called = true; gotCode = code }
	defer func() { interruptGateExit = orig }()

	sigCh := make(chan os.Signal, 1)
	sigCh <- syscall.SIGINT

	var out bytes.Buffer
	// The input would resolve to Continue if read; the pending signal must win.
	env := gateEnv{out: &out, in: strings.NewReader("1\n"), taskSetID: "demo"}
	cont, err := handleInteractiveInterruptGate(env, m, task, sigCh)
	if err != nil {
		t.Fatalf("handleInteractiveInterruptGate: %v", err)
	}
	if !called {
		t.Fatal("a second SIGINT must trigger the force-quit exit")
	}
	if gotCode != ExitInterrupted {
		t.Fatalf("force-quit exit code = %d, want %d", gotCode, ExitInterrupted)
	}
	if cont {
		t.Fatal("force-quit must not resolve as Continue")
	}
}

// TestInterruptGateYesSkipsPrompt: --yes no-ops the gate — no menu, no input
// consumed — so unattended runs keep the teardown-and-exit.
func TestInterruptGateYesSkipsPrompt(t *testing.T) {
	m, task := stubInterruptTask()
	var out bytes.Buffer
	env := gateEnv{out: &out, in: strings.NewReader("1\n"), yes: true, taskSetID: "demo"}
	cont, err := handleInteractiveInterruptGate(env, m, task, nil)
	if err != nil {
		t.Fatalf("handleInteractiveInterruptGate: %v", err)
	}
	if cont {
		t.Fatal("--yes must return cont=false (no menu)")
	}
	if strings.Contains(out.String(), "Interrupted:") {
		t.Fatalf("--yes must not render the menu:\n%s", out.String())
	}
}

// TestInterruptGateNonInteractiveSkipsPrompt: a non-interactive input source
// no-ops the gate the same way, so a piped-input drain keeps teardown-and-exit.
func TestInterruptGateNonInteractiveSkipsPrompt(t *testing.T) {
	m, task := stubInterruptTask()
	var out bytes.Buffer
	env := gateEnv{out: &out, in: NonInteractiveReader{}, taskSetID: "demo"}
	cont, err := handleInteractiveInterruptGate(env, m, task, nil)
	if err != nil {
		t.Fatalf("handleInteractiveInterruptGate: %v", err)
	}
	if cont {
		t.Fatal("a non-interactive input must return cont=false (no menu)")
	}
	if strings.Contains(out.String(), "Interrupted:") {
		t.Fatalf("a non-interactive input must not render the menu:\n%s", out.String())
	}
}

// TestImplementRunInterruptGateContinueReacquiresLock: the drain-loop interrupt
// gate parks the Runtime execution lock, runs the menu lock-free, and on Continue
// re-acquires the lock — leaving a live Drain and no interrupted terminal, so the
// run can resume and reach its own later stopping point.
func TestImplementRunInterruptGateContinueReacquiresLock(t *testing.T) {
	run, _, refresh, sel := newRunSelectedTaskRun(t,
		[]Task{{ID: "01-a", File: "01-a.md", Title: "A", Type: "AFK", Status: "open"}},
		"", RunTaskSetOptions{ConfirmIn: bytes.NewBufferString("1\n")})
	m := refresh.Manifests["demo"]
	interrupted := findTaskInManifest(m, sel.TaskID)

	cont, err := run.interruptGate(m, interrupted)
	if err != nil {
		t.Fatalf("interruptGate: %v", err)
	}
	if !cont {
		t.Fatal("Continue must return cont=true")
	}
	if run.drain == nil {
		t.Fatal("Continue must re-acquire the Runtime execution lock (live Drain)")
	}

	// The park recorded a clean finished terminal; finalize the resumed segment
	// cleanly and confirm no interrupted terminal was ever stamped.
	runtimePath := run.runtimePath
	finalizeDrain(run.drain, false, false, false, "", false, time.Time{}, nil)
	run.drain = nil
	if rec := latestTerminalDrain(t, run.d, runtimePath); rec == nil || rec.State == store.StateInterrupted {
		t.Fatalf("Continue must record no interrupted terminal, got %#v", rec)
	}
}

// TestImplementRunInterruptGateExitRecordsInterruptedTerminal: on Exit the gate
// re-holds the lock so the deferred finalize records the interrupted terminal —
// today's teardown-and-exit behavior, reached through the park/resume discipline.
func TestImplementRunInterruptGateExitRecordsInterruptedTerminal(t *testing.T) {
	run, _, refresh, sel := newRunSelectedTaskRun(t,
		[]Task{{ID: "01-a", File: "01-a.md", Title: "A", Type: "AFK", Status: "open"}},
		"", RunTaskSetOptions{ConfirmIn: bytes.NewBufferString("0\n")})
	m := refresh.Manifests["demo"]
	interrupted := findTaskInManifest(m, sel.TaskID)

	cont, err := run.interruptGate(m, interrupted)
	if err != nil {
		t.Fatalf("interruptGate: %v", err)
	}
	if cont {
		t.Fatal("Exit must return cont=false")
	}
	if run.drain == nil {
		t.Fatal("Exit must re-hold a Drain so finalize can stamp the interrupted terminal")
	}

	// Mirror runSelectedTask returning the interrupt error, whose finalize stamps
	// the terminal.
	runtimePath := run.runtimePath
	finalizeDrain(run.drain, false, false, false, "", false, time.Time{}, taskExitErr(sel, ExitInterrupted, "interrupted"))
	run.drain = nil
	rec := latestTerminalDrain(t, run.d, runtimePath)
	if rec == nil || rec.State != store.StateInterrupted {
		t.Fatalf("Exit must record the interrupted terminal, got %#v", rec)
	}
}

// TestImplementRunInterruptGateYesKeepsLockHeld: under --yes the gate does not
// prompt and does not park — it leaves the opening Drain held so the normal
// finalize records the interrupted terminal, exactly as today.
func TestImplementRunInterruptGateYesKeepsLockHeld(t *testing.T) {
	run, _, refresh, sel := newRunSelectedTaskRun(t,
		[]Task{{ID: "01-a", File: "01-a.md", Title: "A", Type: "AFK", Status: "open"}},
		"", RunTaskSetOptions{Yes: true})
	m := refresh.Manifests["demo"]
	interrupted := findTaskInManifest(m, sel.TaskID)
	held := run.drain

	cont, err := run.interruptGate(m, interrupted)
	if err != nil {
		t.Fatalf("interruptGate: %v", err)
	}
	if cont {
		t.Fatal("--yes must return cont=false (no menu)")
	}
	if run.drain != held {
		t.Fatal("--yes must leave the opening Drain held (no park, no re-acquire)")
	}

	runtimePath := run.runtimePath
	finalizeDrain(run.drain, false, false, false, "", false, time.Time{}, taskExitErr(sel, ExitInterrupted, "interrupted"))
	run.drain = nil
	rec := latestTerminalDrain(t, run.d, runtimePath)
	if rec == nil || rec.State != store.StateInterrupted {
		t.Fatalf("--yes teardown must record the interrupted terminal, got %#v", rec)
	}
}

// TestImplementRunInterruptClearsAutoDrainOnExit: interrupting a live drain whose
// set has Auto-drain on clears the consent bit at interrupt time — announced and
// left as a durable AUTO-DRAIN-CLEARED per-set trace — and choosing Exit leaves it
// cleared (ADR-0120). With the bit off, `pop queue run`'s `Ready && AutoDrain`
// predicate no longer selects the set.
func TestImplementRunInterruptClearsAutoDrainOnExit(t *testing.T) {
	var buf bytes.Buffer
	run, _, refresh, sel := newRunSelectedTaskRun(t,
		[]Task{{ID: "01-a", File: "01-a.md", Title: "A", Type: "AFK", Status: "open"}},
		"", RunTaskSetOptions{ConfirmIn: bytes.NewBufferString("0\n"), Output: &buf})
	m := refresh.Manifests["demo"]
	interrupted := findTaskInManifest(m, sel.TaskID)

	if _, err := SetTaskSetAutoDrain(run.d, run.resolved.DefinitionPath, "demo", true); err != nil {
		t.Fatalf("seed auto-drain: %v", err)
	}

	cont, err := run.interruptGate(m, interrupted)
	if err != nil {
		t.Fatalf("interruptGate: %v", err)
	}
	if cont {
		t.Fatal("Exit must return cont=false")
	}
	if drainSetAutoDrain(t, run, "demo") {
		t.Fatal("interrupt must clear Auto-drain; Exit must leave it cleared")
	}
	if !strings.Contains(buf.String(), "Auto-drain cleared for task set demo") {
		t.Fatalf("clear must be announced:\n%s", buf.String())
	}
	if !strings.Contains(readSetProgress(t, m), "AUTO-DRAIN-CLEARED") {
		t.Fatalf("clear must leave a durable per-set trace:\n%s", readSetProgress(t, m))
	}
}

// TestImplementRunInterruptRevivesAutoDrainOnContinue: choosing Continue at the
// interrupt gate revives the pre-interrupt Auto-drain value (turning the bit back
// on) and tells the user (ADR-0120), so the daemon may pick the set up again once
// the resumed drain finishes.
func TestImplementRunInterruptRevivesAutoDrainOnContinue(t *testing.T) {
	var buf bytes.Buffer
	run, _, refresh, sel := newRunSelectedTaskRun(t,
		[]Task{{ID: "01-a", File: "01-a.md", Title: "A", Type: "AFK", Status: "open"}},
		"", RunTaskSetOptions{ConfirmIn: bytes.NewBufferString("1\n"), Output: &buf})
	m := refresh.Manifests["demo"]
	interrupted := findTaskInManifest(m, sel.TaskID)

	if _, err := SetTaskSetAutoDrain(run.d, run.resolved.DefinitionPath, "demo", true); err != nil {
		t.Fatalf("seed auto-drain: %v", err)
	}

	cont, err := run.interruptGate(m, interrupted)
	if err != nil {
		t.Fatalf("interruptGate: %v", err)
	}
	if !cont {
		t.Fatal("Continue must return cont=true")
	}
	if !drainSetAutoDrain(t, run, "demo") {
		t.Fatal("Continue must revive the snapshotted Auto-drain value")
	}
	if !strings.Contains(buf.String(), "Auto-drain restored for task set demo") {
		t.Fatalf("revive must be announced:\n%s", buf.String())
	}
	if !strings.Contains(readSetProgress(t, m), "AUTO-DRAIN-RESTORED") {
		t.Fatalf("revive must leave a durable per-set trace:\n%s", readSetProgress(t, m))
	}

	// Clean up the live Drain the Continue re-acquired.
	if run.drain != nil {
		finalizeDrain(run.drain, false, false, false, "", false, time.Time{}, nil)
		run.drain = nil
	}
}

// TestImplementRunInterruptContinueLeavesOffBitOff: when the set's Auto-drain was
// already off, an interrupt neither announces nor traces a clear, and Continue does
// not spuriously enable the bit — revive restores the snapshot, which was off.
func TestImplementRunInterruptContinueLeavesOffBitOff(t *testing.T) {
	var buf bytes.Buffer
	run, _, refresh, sel := newRunSelectedTaskRun(t,
		[]Task{{ID: "01-a", File: "01-a.md", Title: "A", Type: "AFK", Status: "open"}},
		"", RunTaskSetOptions{ConfirmIn: bytes.NewBufferString("1\n"), Output: &buf})
	m := refresh.Manifests["demo"]
	interrupted := findTaskInManifest(m, sel.TaskID)

	cont, err := run.interruptGate(m, interrupted)
	if err != nil {
		t.Fatalf("interruptGate: %v", err)
	}
	if !cont {
		t.Fatal("Continue must return cont=true")
	}
	if drainSetAutoDrain(t, run, "demo") {
		t.Fatal("an already-off bit must stay off through interrupt + Continue")
	}
	if strings.Contains(buf.String(), "Auto-drain cleared") || strings.Contains(buf.String(), "Auto-drain restored") {
		t.Fatalf("an off bit must not announce clear/revive:\n%s", buf.String())
	}

	if run.drain != nil {
		finalizeDrain(run.drain, false, false, false, "", false, time.Time{}, nil)
		run.drain = nil
	}
}

// TestImplementRunInterruptClearsAutoDrainUnattended: an unattended (--yes)
// interrupt still clears Auto-drain — the clear runs ahead of the prompt guard, so
// a manually interrupted daemon drain stops `pop queue run` from re-firing the set
// even though no gate menu is shown (ADR-0120).
func TestImplementRunInterruptClearsAutoDrainUnattended(t *testing.T) {
	var buf bytes.Buffer
	run, _, refresh, sel := newRunSelectedTaskRun(t,
		[]Task{{ID: "01-a", File: "01-a.md", Title: "A", Type: "AFK", Status: "open"}},
		"", RunTaskSetOptions{Yes: true, Output: &buf})
	m := refresh.Manifests["demo"]
	interrupted := findTaskInManifest(m, sel.TaskID)

	if _, err := SetTaskSetAutoDrain(run.d, run.resolved.DefinitionPath, "demo", true); err != nil {
		t.Fatalf("seed auto-drain: %v", err)
	}

	cont, err := run.interruptGate(m, interrupted)
	if err != nil {
		t.Fatalf("interruptGate: %v", err)
	}
	if cont {
		t.Fatal("--yes must return cont=false (no menu)")
	}
	if drainSetAutoDrain(t, run, "demo") {
		t.Fatal("an unattended interrupt must still clear Auto-drain")
	}
	if strings.Contains(buf.String(), "Interrupted:") {
		t.Fatalf("--yes must not render the menu:\n%s", buf.String())
	}
}
