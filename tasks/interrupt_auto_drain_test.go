package tasks

import (
	"bytes"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/glebglazov/pop/config"
)

// seedSetAutoDrain marks a registered set auto-drainable — the state a
// daemon-launched drain runs in, and the only state in which a revocation is
// observable at all.
func seedSetAutoDrain(t *testing.T, d *Deps, defPath, setID string) {
	t.Helper()
	if _, err := SetTaskSetAutoDrain(d, defPath, setID, true); err != nil {
		t.Fatalf("seed auto-drain: %v", err)
	}
}

// registeredAutoDrain reads the store-backed Auto-drain consent bit recorded for
// a registered set, so interrupt tests assert clear/revive against the same value
// the daemon's `Ready && AutoDrain` eligibility predicate reads.
func registeredAutoDrain(t *testing.T, d *Deps, defPath, setID string) bool {
	t.Helper()
	state, err := LoadGlobalStateWith(d, DefaultStatePathWith(d))
	if err != nil {
		t.Fatalf("LoadGlobalState: %v", err)
	}
	canon, err := CanonicalDefinitionPathWith(d, defPath)
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

// setDirManifest is the manifest handle readSetProgress needs for a fixture's
// set, for tests that hold the fixture rather than a loaded manifest.
func setDirManifest(env *runTaskSetFixture, setID string) *Manifest {
	return &Manifest{Dir: filepath.Join(env.tasksDir, setID)}
}

// waitForRecoveryWaiter blocks until the drain under test has parked on its quota
// pause and registered its recovery waiter — the point from which the wait can be
// interrupted.
func waitForRecoveryWaiter(t *testing.T, d *Deps, setID string) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		waiter, err := GetRecoveryWaiter(d, setID)
		if err != nil {
			t.Fatalf("GetRecoveryWaiter: %v", err)
		}
		if waiter != nil {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("recovery waiter for %q was never registered", setID)
}

// TestDrainInterruptedInQuotaRecoveryWaitClearsAutoDrain: a drain parked in the
// quota-recovery wait shows no interrupt gate — it ends straight from the wait —
// and the exit still revokes the set's Auto-drain consent (ADR-0120 names
// wait-state interrupts explicitly). Deregistering the waiter under the waiting
// drain drives the wait's interrupted exit, the same ExitInterrupted a SIGINT
// there produces, without signalling the test process.
func TestDrainInterruptedInQuotaRecoveryWaitClearsAutoDrain(t *testing.T) {
	// ADR-0145: installClaudeQuotaAgent stubs PATH — stays serial.
	env := setupRunTaskSetFixture(t, "demo", []Task{
		{ID: "01-a", File: "01-a.md", Title: "A", Type: "AFK", Status: "open"},
	})
	installClaudeQuotaAgent(t, env.root)
	d := env.deps()
	seedSetAutoDrain(t, d, env.tasksDir, "demo")

	var buf bytes.Buffer
	opts := env.runTaskSetOpts(true, "", &buf)
	opts.TaskSetOverride = "demo"
	opts.AgentPreset = "claude"

	errCh := make(chan error, 1)
	go func() {
		_, err := RunTaskSetWith(d, nil, nil, opts)
		errCh <- err
	}()

	waitForRecoveryWaiter(t, d, "demo")
	if err := DeregisterRecoveryWaiter(d, "demo"); err != nil {
		t.Fatalf("DeregisterRecoveryWaiter: %v", err)
	}

	select {
	case err := <-errCh:
		assertExitCode(t, err, ExitInterrupted)
	case <-time.After(10 * time.Second):
		t.Fatal("the drain did not exit after its recovery wait was interrupted")
	}

	if registeredAutoDrain(t, d, env.tasksDir, "demo") {
		t.Fatal("an interrupt during the quota-recovery wait must clear Auto-drain")
	}
	if !strings.Contains(buf.String(), "Auto-drain cleared for task set demo") {
		t.Fatalf("the clear must be announced:\n%s", buf.String())
	}
	if progress := readSetProgress(t, setDirManifest(env, "demo")); !strings.Contains(progress, "AUTO-DRAIN-CLEARED") {
		t.Fatalf("the clear must leave a durable per-set trace:\n%s", progress)
	}
}

// TestDrainInterruptedInVerifyPhaseClearsAutoDrain: the Verifier runs after the
// set's AFK work is exhausted, past the last point an interrupt gate can appear,
// so an interrupt there used to leave consent set and let the daemon re-grab a set
// the human had just taken over.
func TestDrainInterruptedInVerifyPhaseClearsAutoDrain(t *testing.T) {
	t.Parallel()
	env := setupDrainRefineFixture(t, signOffSet())
	d := env.deps()
	seedSetAutoDrain(t, d, env.tasksDir, "demo")

	var buf bytes.Buffer
	opts := env.runTaskSetOpts(true, "", &buf)
	opts.TaskSetOverride = "demo"
	opts.verifyRunner = func(string) (string, error) {
		return "", exitErr(ExitInterrupted, "interrupted")
	}

	_, err := RunTaskSetWith(d, nil, func(string) (*config.Config, error) {
		return verifyEnabledConfig(), nil
	}, opts)
	assertExitCode(t, err, ExitInterrupted)

	if registeredAutoDrain(t, d, env.tasksDir, "demo") {
		t.Fatal("an interrupt during the verify phase must clear Auto-drain")
	}
	if progress := readSetProgress(t, setDirManifest(env, "demo")); !strings.Contains(progress, "AUTO-DRAIN-CLEARED") {
		t.Fatalf("the clear must leave a durable per-set trace:\n%s", progress)
	}
}

// TestDrainInterruptedInRefinePhaseClearsAutoDrain: the refine phase is the last
// step before the terminal switch and the human's interrupt is the only directive
// it hands back, so it is the drain's last uncovered interrupt exit.
func TestDrainInterruptedInRefinePhaseClearsAutoDrain(t *testing.T) {
	t.Parallel()
	env := setupDrainRefineFixture(t, signOffSet())
	d := env.deps()
	seedSetAutoDrain(t, d, env.tasksDir, "demo")

	var buf bytes.Buffer
	opts := env.runTaskSetOpts(true, "", &buf)
	opts.TaskSetOverride = "demo"
	opts.verifyRunner = func(string) (string, error) { return "VERDICT: PASS\n", nil }
	opts.refineRunner = func(string) (string, string, error) {
		return "", "", exitErr(ExitInterrupted, "interrupted")
	}

	_, err := RunTaskSetWith(d, nil, func(string) (*config.Config, error) {
		return refineEnabledConfig(), nil
	}, opts)
	assertExitCode(t, err, ExitInterrupted)

	if registeredAutoDrain(t, d, env.tasksDir, "demo") {
		t.Fatal("an interrupt during the refine phase must clear Auto-drain")
	}
	if progress := readSetProgress(t, setDirManifest(env, "demo")); !strings.Contains(progress, "AUTO-DRAIN-CLEARED") {
		t.Fatalf("the clear must leave a durable per-set trace:\n%s", progress)
	}
}

// TestInterruptedExitRevocationIsIdempotentAfterTheGate: the gate's clear and the
// exit's clear are the same unconditional clear, so an interrupt that did pass the
// gate is announced and traced exactly once — the exit finds the bit already off.
func TestInterruptedExitRevocationIsIdempotentAfterTheGate(t *testing.T) {
	var buf bytes.Buffer
	run, _, refresh, sel := newRunSelectedTaskRun(t,
		[]Task{{ID: "01-a", File: "01-a.md", Title: "A", Type: "AFK", Status: "open"}},
		"", RunTaskSetOptions{ConfirmIn: bytes.NewBufferString("0\n"), Output: &buf})
	m := refresh.Manifests["demo"]
	seedSetAutoDrain(t, run.d, run.resolved.DefinitionPath, "demo")

	if _, err := run.interruptGate(m, findTaskInManifest(m, sel.TaskID)); err != nil {
		t.Fatalf("interruptGate: %v", err)
	}
	run.revokeAutoDrainOnInterruptedExit(taskExitErr(sel, ExitInterrupted, "interrupted"))

	if drainSetAutoDrain(t, run, "demo") {
		t.Fatal("the set must be left without Auto-drain consent")
	}
	if got := strings.Count(buf.String(), "Auto-drain cleared for task set demo"); got != 1 {
		t.Fatalf("clear announced %d times, want 1:\n%s", got, buf.String())
	}
	if got := strings.Count(readSetProgress(t, m), "AUTO-DRAIN-CLEARED"); got != 1 {
		t.Fatalf("clear traced %d times, want 1:\n%s", got, readSetProgress(t, m))
	}
}

// TestNonInterruptedExitKeepsAutoDrain: Auto-drain is sticky across the drains one
// set needs (ADR-0098) — only an interrupt revokes it at exit, so every other end
// of a drain must leave the bit exactly as it found it.
func TestNonInterruptedExitKeepsAutoDrain(t *testing.T) {
	var buf bytes.Buffer
	run, _, _, sel := newRunSelectedTaskRun(t,
		[]Task{{ID: "01-a", File: "01-a.md", Title: "A", Type: "AFK", Status: "open"}},
		"", RunTaskSetOptions{Yes: true, Output: &buf})
	seedSetAutoDrain(t, run.d, run.resolved.DefinitionPath, "demo")

	run.revokeAutoDrainOnInterruptedExit(nil)
	run.revokeAutoDrainOnInterruptedExit(taskExitErr(sel, ExitOperational, "attempts exhausted"))

	if !drainSetAutoDrain(t, run, "demo") {
		t.Fatal("only an interrupted exit may revoke Auto-drain")
	}
}
