package tasks

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/glebglazov/pop/store"
)

// newVerifyPhaseRun builds an implementRun wired to a real store-backed checkout
// and holding a live Drain, ready to drive verifyPhase directly. Only the fields
// verifyPhase (and the Drain-lifecycle methods it calls) read are populated, so
// the seam can be exercised without standing up the whole drain loop. The set is
// a fully-drained pure-AFK set, so its row reads DONE and the pre-approval verify
// guard fires.
func newVerifyPhaseRun(t *testing.T, verify func(string) (string, error)) (*implementRun, *RefreshResult, *Row, string) {
	t.Helper()
	return newVerifyPhaseRunWithKeys(t, verify, nil)
}

// newVerifyPhaseRunWithKeys is newVerifyPhaseRun with set-level manifest keys
// (e.g. {"human_completed": true}), so the phase can be driven against a set
// whose own manifest changes what the verdict is allowed to do.
func newVerifyPhaseRunWithKeys(t *testing.T, verify func(string) (string, error), setKeys map[string]any) (*implementRun, *RefreshResult, *Row, string) {
	t.Helper()
	env := setupRunTaskSetFixtureWithKeys(t, "demo", doneAFKSet(), setKeys)
	d := env.deps()
	d.ProcessAlive = func(pid int) bool { return pid == os.Getpid() }

	_, runtimePath, _ := runtimeHead(t, d, env.root)

	handle, err := BeginDrain(d, runtimePath, "demo", io.Discard)
	if err != nil {
		t.Fatalf("BeginDrain: %v", err)
	}
	t.Cleanup(func() { finalizeDrain(handle, false, nil, false, false, nil) })

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

	refresh, err := RefreshWith(d, env.tasksDir, DefaultStatePathWith(d))
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

// TestVerifyPhaseCachedFixableUsesStoredSummary: a cache-hit FIXABLE verdict
// must carry its persisted SUMMARY into the spawned Remediation title.
func TestVerifyPhaseCachedFixableUsesStoredSummary(t *testing.T) {
	run, refresh, row, indexPath := newVerifyPhaseRun(t, func(string) (string, error) {
		t.Fatal("cache hit must not invoke the Verifier")
		return "", nil
	})

	id, err := ResolveRepositoryIdentity(run.d, run.runtimePath)
	if err != nil {
		t.Fatalf("ResolveRepositoryIdentity: %v", err)
	}
	headOut, err := run.d.Git.CommandInDir(run.runtimePath, "rev-parse", "HEAD")
	if err != nil {
		t.Fatalf("rev-parse HEAD: %v", err)
	}
	workSHA := strings.TrimSpace(headOut)
	s, err := openDrainStore(run.d)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	if err := s.PutVerifyVerdict(store.VerifyVerdict{
		Repo:     id.CommonDir,
		SetID:    "demo",
		WorkSHA:  workSHA,
		Verdict:  "FIXABLE",
		Findings: "criterion 2 unmet",
		Summary:  "widget never renders",
	}); err != nil {
		t.Fatalf("seed cached FIXABLE verdict: %v", err)
	}

	directive, err := run.verifyPhase(refresh, row)
	if err != nil {
		t.Fatalf("verifyPhase: %v", err)
	}
	if directive != verifyContinue {
		t.Fatalf("directive = %d, want verifyContinue (%d)", directive, verifyContinue)
	}
	m := LoadManifest(run.d, "demo", indexPath)
	for _, tk := range m.Tasks {
		if tk.ID == "02-remediation" && tk.Title != "Remediation 1: widget never renders" {
			t.Fatalf("title = %q, want Remediation 1: widget never renders", tk.Title)
		}
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

// TestVerifyPhaseQuotaPauseOnHumanCompletedFallsThrough: the verdict a
// human-completed set would get is informational (ADR-0179), so a quota-paused
// Verifier has nothing to wait for. The phase reports the pause and falls through
// to the terminal switch, leaving no verdict on record — the mark stays unverified
// and the Verifier is still scheduled, so a later drain records it.
func TestVerifyPhaseQuotaPauseOnHumanCompletedFallsThrough(t *testing.T) {
	verdictText := ""
	run, refresh, row, _ := newVerifyPhaseRunWithKeys(t, func(string) (string, error) {
		if verdictText != "" {
			return verdictText, nil
		}
		// The reset instant is already elapsed, so a phase that parked would recover
		// and answer verifyContinue rather than hanging the test.
		return "", newVerifyQuotaPause(VerifyQuotaPause{
			Preset:  "claude",
			ResetAt: time.Now().Add(-time.Hour),
			Reason:  "verifier quota exhausted",
		})
	}, map[string]any{"human_completed": true})

	directive, err := run.verifyPhase(refresh, row)
	if err != nil {
		t.Fatalf("verifyPhase: %v", err)
	}
	if directive != verifyFallThrough {
		t.Fatalf("directive = %d, want verifyFallThrough (%d) — a human-completed set must not park on quota", directive, verifyFallThrough)
	}
	if run.result.QuotaPaused || run.result.TaskSetVerifyFailed {
		t.Fatalf("result must carry no pause or verify-failed terminal: %+v", run.result)
	}
	if row.Status != StatusDone {
		t.Fatalf("display row status = %q, want DONE", row.Status)
	}
	repo, _, head := runtimeHead(t, run.d, run.runtimePath)
	if stored := readStoredVerdict(t, run.d, repo, "demo", head); stored != nil {
		t.Fatalf("stored verdict = %+v, want none — an unrun Verifier must leave the mark unverified", stored)
	}

	// The work SHA has not moved, so the Verifier is still scheduled: the next
	// drain over the same set invokes it again and records the verdict.
	verdictText = "VERDICT: PASS\nFINDINGS: none\n"
	directive, err = run.verifyPhase(refresh, row)
	if err != nil {
		t.Fatalf("second verifyPhase: %v", err)
	}
	if directive != verifyFallThrough {
		t.Fatalf("second directive = %d, want verifyFallThrough (%d)", directive, verifyFallThrough)
	}
	if stored := readStoredVerdict(t, run.d, repo, "demo", head); stored == nil || stored.Verdict != string(VerdictPass) {
		t.Fatalf("stored verdict = %+v, want PASS recorded by the later drain", stored)
	}
}
