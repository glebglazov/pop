package tasks

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// TestFailedGateDirtyTreeRegistersClaimingHold pins ADR-0135: a Failed gate
// parked over a dirty working tree registers a claim-bearing hold. The dirtiness
// is snapshotted at park time and never re-evaluated — cleaning the tree while the
// human sits at the gate does not drop the claim.
func TestFailedGateDirtyTreeRegistersClaimingHold(t *testing.T) {
	env := setupRunTaskSetFixture(t, "demo", []Task{
		{ID: "01-a", File: "01-a.md", Title: "A", Type: "AFK", Status: "failed", FailedAfter: intPtr(3)},
	})
	agent := writeFakeAgent(t, env.root, fakeAgentConfig{summary: "unused"})
	d := env.deps()
	runtimePath, err := ResolveRuntimePathWith(d, env.root, "")
	if err != nil {
		t.Fatal(err)
	}
	// Make the working tree dirty before the drain reaches the Failed gate.
	dirtyFile := filepath.Join(runtimePath, "uncommitted.txt")
	if err := os.WriteFile(dirtyFile, []byte("work in progress\n"), 0o644); err != nil {
		t.Fatalf("write dirty file: %v", err)
	}

	check := func(t *testing.T) {
		t.Helper()
		hold, err := GetCheckoutGateHold(d, runtimePath)
		if err != nil {
			t.Fatalf("GetCheckoutGateHold: %v", err)
		}
		if hold == nil {
			t.Fatal("checkout gate hold missing at Failed gate prompt")
		}
		if !hold.Claim {
			t.Fatal("Failed gate over a dirty tree must register a claim-bearing hold")
		}
		// Snapshot invariant: cleaning the tree mid-gate does not release the claim.
		if err := os.Remove(dirtyFile); err != nil {
			t.Fatalf("clean tree mid-gate: %v", err)
		}
		hold, err = GetCheckoutGateHold(d, runtimePath)
		if err != nil {
			t.Fatalf("GetCheckoutGateHold after clean: %v", err)
		}
		if hold == nil || !hold.Claim {
			t.Fatalf("claim re-evaluated live after cleaning the tree: %#v", hold)
		}
	}

	var buf bytes.Buffer
	opts := env.runTaskSetOpts(false, agent, &buf)
	opts.TaskSetOverride = "demo"
	opts.ConfirmIn = &checkingPromptReader{t: t, check: check, response: "0\n"}

	_, err = RunTaskSetWith(d, nil, nil, opts)
	assertExitCode(t, err, ExitOperational)
}

// TestFailedGateCleanTreeRegistersNonClaimingHold pins the other half of ADR-0135:
// a Failed gate over a clean tree registers a non-claiming hold (quiescence
// occupancy only) — there is no uncommitted work for another set to clobber.
func TestFailedGateCleanTreeRegistersNonClaimingHold(t *testing.T) {
	env := setupRunTaskSetFixture(t, "demo", []Task{
		{ID: "01-a", File: "01-a.md", Title: "A", Type: "AFK", Status: "failed", FailedAfter: intPtr(3)},
	})
	agent := writeFakeAgent(t, env.root, fakeAgentConfig{summary: "unused"})
	d := env.deps()
	runtimePath, err := ResolveRuntimePathWith(d, env.root, "")
	if err != nil {
		t.Fatal(err)
	}

	check := func(t *testing.T) {
		t.Helper()
		hold, err := GetCheckoutGateHold(d, runtimePath)
		if err != nil {
			t.Fatalf("GetCheckoutGateHold: %v", err)
		}
		if hold == nil {
			t.Fatal("checkout gate hold missing at Failed gate prompt")
		}
		if hold.Claim {
			t.Fatal("Failed gate over a clean tree must register a non-claiming hold")
		}
	}

	var buf bytes.Buffer
	opts := env.runTaskSetOpts(false, agent, &buf)
	opts.TaskSetOverride = "demo"
	opts.ConfirmIn = &checkingPromptReader{t: t, check: check, response: "0\n"}

	_, err = RunTaskSetWith(d, nil, nil, opts)
	assertExitCode(t, err, ExitOperational)
}

// TestHITLGateRegistersNonClaimingHold pins ADR-0135: the HITL gate is a human
// wait that claims nothing, even over a dirty tree — the hold is quiescence
// occupancy only.
func TestHITLGateRegistersNonClaimingHold(t *testing.T) {
	var (
		run         *implementRun
		d           *Deps
		runtimePath string
	)
	check := func(t *testing.T) {
		t.Helper()
		hold, err := GetCheckoutGateHold(d, runtimePath)
		if err != nil {
			t.Fatalf("GetCheckoutGateHold: %v", err)
		}
		if hold == nil {
			t.Fatal("checkout gate hold missing at HITL gate prompt")
		}
		if hold.Claim {
			t.Fatal("HITL gate must register a non-claiming hold")
		}
	}
	reader := &checkingPromptReader{t: t, check: check, response: "0\n"}

	run, d, runtimePath, m, hitl := newHITLGateRun(t, reader, false)
	// Dirty the tree to prove the HITL gate never claims regardless of dirtiness.
	if err := os.WriteFile(filepath.Join(runtimePath, "uncommitted.txt"), []byte("wip\n"), 0o644); err != nil {
		t.Fatalf("write dirty file: %v", err)
	}

	if _, err := run.hitlGate(m, hitl); err != nil {
		t.Fatalf("hitlGate: %v", err)
	}
}

// TestVerifyFailGateRegistersNonClaimingHold pins ADR-0135: the verify-fail gate
// is a human wait that claims nothing. A NEEDS-HUMAN verdict parks the set at the
// interactive gate; the hold observed during the menu is non-claiming.
func TestVerifyFailGateRegistersNonClaimingHold(t *testing.T) {
	env := setupRunTaskSetFixture(t, "demo", doneAFKSet())
	d := env.deps()
	d.ProcessAlive = func(pid int) bool { return pid == os.Getpid() }

	_, runtimePath, _ := runtimeHead(t, d, env.root)

	handle, err := BeginDrain(d, runtimePath, "demo", io.Discard)
	if err != nil {
		t.Fatalf("BeginDrain: %v", err)
	}
	t.Cleanup(func() { finalizeDrain(handle, false, false, false, "", false, time.Time{}, nil) })

	holdSeen := false
	check := func(t *testing.T) {
		t.Helper()
		hold, err := GetCheckoutGateHold(d, runtimePath)
		if err != nil {
			t.Fatalf("GetCheckoutGateHold: %v", err)
		}
		if hold == nil {
			t.Fatal("checkout gate hold missing at verify-fail gate prompt")
		}
		if hold.Claim {
			t.Fatal("verify-fail gate must register a non-claiming hold")
		}
		holdSeen = true
	}
	reader := &checkingPromptReader{t: t, check: check, response: "0\n"}

	run := &implementRun{
		d:           d,
		plan:        &runPlan{cfg: verifyEnabledConfig()},
		opts:        RunTaskSetOptions{Yes: false, ConfirmIn: reader, verifyRunner: func(string) (string, error) { return "VERDICT: NEEDS-HUMAN\nFINDINGS: needs a human call\n", nil }},
		runtimePath: runtimePath,
		taskSetID:   "demo",
		resolved:    &ResolvedPaths{DefinitionPath: env.tasksDir, ProjectPath: env.root},
		confirmOut:  io.Discard,
		out:         &bytes.Buffer{},
		timeout:     time.Minute,
		drain:       handle,
		result:      &RunTaskSetResult{TaskSetID: "demo"},
	}

	refresh, err := RefreshWith(d, env.tasksDir, DefaultStatePath())
	if err != nil {
		t.Fatalf("RefreshWith: %v", err)
	}
	row := findRow(refresh, "demo")
	if row == nil {
		t.Fatal("no demo row in refresh")
	}

	// Exiting the gate (menu "0") falls through to the verify-failed return; the
	// returned error is that expected fall-through, not a test failure. The hold
	// assertion runs inside the prompt reader during the menu.
	_, _ = run.verifyPhase(refresh, row)
	if !holdSeen {
		t.Fatal("verify-fail gate never prompted; the hold assertion did not run")
	}
}
