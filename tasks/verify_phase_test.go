package tasks

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// newVerifyPhaseRun builds an implementRun wired to a real store-backed checkout
// and holding a live Drain, ready to drive verifyPhase directly. Only the fields
// verifyPhase (and the Drain-lifecycle methods it calls) read are populated, so
// the seam can be exercised without standing up the whole drain loop. The set is
// a fully-drained pure-AFK set, so its row reads DONE and the pre-approval verify
// guard fires.
func newVerifyPhaseRun(t *testing.T, verify func(string) (string, error)) (*implementRun, *RefreshResult, *Row, string) {
	t.Helper()
	env := setupRunTaskSetFixture(t, "demo", doneAFKSet())
	d := env.deps()
	d.ProcessAlive = func(pid int) bool { return pid == os.Getpid() }

	_, runtimePath, _ := runtimeHead(t, d, env.root)

	handle, err := BeginDrain(d, runtimePath, "demo", io.Discard)
	if err != nil {
		t.Fatalf("BeginDrain: %v", err)
	}
	t.Cleanup(func() { finalizeDrain(handle, false, false, false, "", false, time.Time{}, nil) })

	run := &implementRun{
		d:           d,
		plan:        &runPlan{cfg: verifyEnabledConfig()},
		opts:        RunTaskSetOptions{Yes: true, verifyRunner: verify},
		runtimePath: runtimePath,
		taskSetID:   "demo",
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
	if row.Status != StatusDone {
		t.Fatalf("fixture row status = %q, want DONE", row.Status)
	}
	return run, refresh, row, filepath.Join(env.tasksDir, "demo", "index.json")
}

// TestVerifyPhaseFixableUnderCapSpawnsRemediationAndContinues drives verifyPhase
// directly: a FIXABLE verdict on an exhausted set that is under its remediation
// depth cap spawns an AFK Remediation task and tells the loop to keep draining
// (verifyContinue) rather than parking as VERIFY-FAILED (ADR-0086).
func TestVerifyPhaseFixableUnderCapSpawnsRemediationAndContinues(t *testing.T) {
	run, refresh, row, indexPath := newVerifyPhaseRun(t, func(string) (string, error) {
		return "VERDICT: FIXABLE\nFINDINGS: criterion 2 unmet\n", nil
	})

	directive, err := run.verifyPhase(refresh, row)
	if err != nil {
		t.Fatalf("verifyPhase: %v", err)
	}
	if directive != verifyContinue {
		t.Fatalf("directive = %d, want verifyContinue (%d)", directive, verifyContinue)
	}
	if run.result.TaskSetVerifyFailed {
		t.Fatal("a FIXABLE spawn under the cap must not mark the set verify-failed")
	}
	// The Verifier acted as a task producer: a Remediation task now sits in the
	// manifest, moving the set's remediation depth to 1.
	m := LoadManifest(run.d, "demo", indexPath)
	if !m.Valid {
		t.Fatalf("reloaded manifest invalid: %v", m.Errors)
	}
	if got := remediationDepth(m); got != 1 {
		t.Fatalf("remediationDepth = %d, want 1 (one spawned Remediation task)", got)
	}
}

// TestVerifyPhaseQuotaPauseParksAndResumes drives verifyPhase directly through
// the quota-pause branch: a quota-paused Verifier parks the held Drain and waits
// for recovery; with the reset instant already elapsed the wait clears, a fresh
// Drain is re-acquired, and the loop is told to keep draining (verifyContinue)
// without a QuotaPaused exit (ADR-0100).
func TestVerifyPhaseQuotaPauseParksAndResumes(t *testing.T) {
	run, refresh, row, _ := newVerifyPhaseRun(t, func(string) (string, error) {
		return "", newVerifyQuotaPause(VerifyQuotaPause{
			Preset:  "claude",
			ResetAt: time.Now().Add(-time.Hour),
			Reason:  "verifier quota exhausted",
		})
	})

	directive, err := run.verifyPhase(refresh, row)
	if err != nil {
		t.Fatalf("verifyPhase: %v", err)
	}
	if directive != verifyContinue {
		t.Fatalf("directive = %d, want verifyContinue (%d) after recovery", directive, verifyContinue)
	}
	if run.result.QuotaPaused {
		t.Fatal("a recovered quota pause must not populate the QuotaPaused result")
	}
	// Park-and-wait resumed: the run re-acquired a live Drain for the next segment.
	if run.drain == nil {
		t.Fatal("verifyPhase must re-acquire the Drain after a recovered quota pause")
	}
	if status := ReadRuntimeLockStatus(run.d, run.runtimePath); !status.Locked {
		t.Fatalf("runtime lock not held after resume: %#v", status)
	}
}
