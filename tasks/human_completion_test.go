package tasks

import (
	"bytes"
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/glebglazov/pop/store"
	"github.com/glebglazov/pop/work"
)

// manifestHumanCompletedBit reads the raw `human_completed` key off a manifest on
// disk, reporting whether the key is present and what it says. The raw read is
// the point: the bit's lifetime is a property of the file a human edits, not of
// whatever the loader would default it to.
func manifestHumanCompletedBit(t *testing.T, path string) (present, value bool) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read manifest: %v", err)
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("unmarshal manifest: %v", err)
	}
	v, ok := raw["human_completed"]
	if !ok {
		return false, false
	}
	var b bool
	if err := json.Unmarshal(v, &b); err != nil {
		t.Fatalf("human_completed is not a bool: %s", v)
	}
	return true, b
}

// humanCompleteDemoSet completes the fixture's sole open AFK task as a human
// would — the `pop tasks complete` verb — and returns the deps and the resolved
// status of the set afterwards, with the Verify verdict overlay applied. This is
// the whole path criterion 1 names: complete, then read what every surface reads.
func humanCompleteDemoSet(t *testing.T, env *execFixture) (*Deps, *Row, *RefreshResult) {
	t.Helper()
	d := env.deps()
	if _, err := CompleteTaskWith(d, nil, nil, CompleteTaskOptions{
		ResolveInput: ResolveInput{CWD: env.root},
		TaskPath:     env.demoTaskRef(t, "01-a.md"),
	}); err != nil {
		t.Fatalf("CompleteTaskWith: %v", err)
	}
	refresh, err := RefreshWith(d, env.tasksDir, StatePathFor(env.tasksDir))
	if err != nil {
		t.Fatalf("RefreshWith: %v", err)
	}
	ApplyVerifyVerdicts(d, refresh, verifyEnabledConfig(), env.root)
	row := findRow(refresh, "demo")
	if row == nil {
		t.Fatal("refresh missing demo row")
	}
	return d, row, refresh
}

// TestHumanCompletionReadsDoneWithAnUnverifiedMark: a human's `complete` that
// carries the set terminal records the bit and the set reads DONE with the
// verification outcome beside it, not NEEDS-VERIFY. The three surfaces criterion
// 1 names read the same two facts from the same resolution: `pop tasks status`
// renders the row, and the Work dashboard / `pop work status` render the STATUS
// cell of the container built from it.
func TestHumanCompletionReadsDoneWithAnUnverifiedMark(t *testing.T) {
	t.Parallel()
	env := setupCustomTaskFixture(t, []Task{
		{ID: "01-a", File: "01-a.md", Title: "A", Type: "AFK", Status: "open"},
	})
	_, row, refresh := humanCompleteDemoSet(t, env)

	if present, value := manifestHumanCompletedBit(t, env.demoManifest()); !present || !value {
		t.Fatalf("human_completed = (present %v, value %v), want the bit recorded", present, value)
	}
	if row.Status != StatusDone {
		t.Fatalf("status = %q, want DONE — the human said so", row.Status)
	}
	if row.VerifyMark != VerifyMarkUnverified {
		t.Fatalf("mark = %q, want unverified — nobody has checked it", row.VerifyMark)
	}

	var buf bytes.Buffer
	Render(&buf, refresh)
	out := buf.String()
	if strings.Contains(out, "NEEDS-VERIFY") {
		t.Fatalf("`pop tasks status` must not demote a human-completed set:\n%s", out)
	}
	if !strings.Contains(out, "DONE") || !strings.Contains(out, "unverified") {
		t.Fatalf("`pop tasks status` must show DONE and the unverified mark:\n%s", out)
	}

	// The Work surfaces read the mark off the container the Task-set kind fills.
	cell := WorkRowStatusCell(work.Container{RawStatus: row.Status, VerifyMark: row.VerifyMark})
	if cell != "DONE · unverified" {
		t.Fatalf("STATUS cell = %q, want %q", cell, "DONE · unverified")
	}
}

// TestHumanCompletionKeepsVerifyFailedAsAMark: a non-PASS verdict at HEAD on a
// human-completed set is information, not a veto — DONE stands, the mark carries
// the failure, and the findings stay reachable in the rendered detail.
func TestHumanCompletionKeepsVerifyFailedAsAMark(t *testing.T) {
	t.Parallel()
	env := setupCustomTaskFixture(t, []Task{
		{ID: "01-a", File: "01-a.md", Title: "A", Type: "AFK", Status: "open"},
	})
	// Complete first: an AFK →done edge ends the prior verification episode
	// (ADR-0109), so the verdict that judges this work is recorded afterwards.
	d, _, _ := humanCompleteDemoSet(t, env)
	repo, head := repoAndHead(t, d, env.root)
	seedVerifyVerdict(t, d, store.VerifyVerdict{
		Repo: repo, SetID: "demo", WorkSHA: head,
		Verdict: string(VerdictNeedsHuman), Findings: "criterion 3 unmet",
	})

	refresh, err := RefreshWith(d, env.tasksDir, StatePathFor(env.tasksDir))
	if err != nil {
		t.Fatalf("RefreshWith: %v", err)
	}
	ApplyVerifyVerdicts(d, refresh, verifyEnabledConfig(), env.root)
	row := findRow(refresh, "demo")
	if row == nil {
		t.Fatal("refresh missing demo row")
	}

	if row.Status != StatusDone {
		t.Fatalf("status = %q, want DONE — a verdict may not demote a human's assertion", row.Status)
	}
	if row.VerifyMark != VerifyMarkFailed {
		t.Fatalf("mark = %q, want verify-failed", row.VerifyMark)
	}
	if row.VerifyFindings != "criterion 3 unmet" {
		t.Fatalf("findings = %q, want them carried onto the row", row.VerifyFindings)
	}

	var buf bytes.Buffer
	Render(&buf, refresh)
	out := buf.String()
	if strings.Contains(out, "VERIFY-FAILED") {
		t.Fatalf("VERIFY-FAILED must be a mark, not the status:\n%s", out)
	}
	for _, want := range []string{"DONE", "verify-failed", "criterion 3 unmet", "re-verify: pop tasks verify demo"} {
		if !strings.Contains(out, want) {
			t.Fatalf("rendered detail missing %q:\n%s", want, out)
		}
	}

	cell := WorkRowStatusCell(work.Container{RawStatus: row.Status, VerifyMark: row.VerifyMark})
	if cell != "DONE · verify-failed" {
		t.Fatalf("STATUS cell = %q, want %q", cell, "DONE · verify-failed")
	}
}

// TestSelfCompletedSetStillGatesOnTheVerdict: a set that reached terminal without
// a human intervening is unchanged by any of this — no verdict is NEEDS-VERIFY,
// and a non-PASS verdict at HEAD is VERIFY-FAILED.
func TestSelfCompletedSetStillGatesOnTheVerdict(t *testing.T) {
	t.Parallel()
	env := setupCustomTaskFixture(t, []Task{
		{ID: "01-a", File: "01-a.md", Title: "A", Type: "AFK", Status: "done"},
	})
	d := env.deps()
	if present, _ := manifestHumanCompletedBit(t, env.demoManifest()); present {
		t.Fatal("a set nobody intervened on must carry no human-completion bit")
	}

	resolve := func() *Row {
		t.Helper()
		refresh, err := RefreshWith(d, env.tasksDir, StatePathFor(env.tasksDir))
		if err != nil {
			t.Fatalf("RefreshWith: %v", err)
		}
		ApplyVerifyVerdicts(d, refresh, verifyEnabledConfig(), env.root)
		row := findRow(refresh, "demo")
		if row == nil {
			t.Fatal("refresh missing demo row")
		}
		return row
	}

	if row := resolve(); row.Status != StatusNeedsVerify {
		t.Fatalf("status = %q, want NEEDS-VERIFY with no verdict", row.Status)
	}

	repo, head := repoAndHead(t, d, env.root)
	seedVerifyVerdict(t, d, store.VerifyVerdict{
		Repo: repo, SetID: "demo", WorkSHA: head,
		Verdict: string(VerdictFixable), Findings: "criterion 1 unmet",
	})
	if row := resolve(); row.Status != StatusVerifyFailed {
		t.Fatalf("status = %q, want VERIFY-FAILED with a non-PASS verdict at HEAD", row.Status)
	}
}

// TestHumanCompletionBitSurvivesCommitsAndClearsOnReopen: the assertion is about
// the set's work, not about a checkout's HEAD, so a later commit leaves it
// standing (unlike a Verifier's PASS, which ADR-0096 lets drift invalidate). A
// reopened task is the one thing that ends it: the assertion no longer describes
// the set.
func TestHumanCompletionBitSurvivesCommitsAndClearsOnReopen(t *testing.T) {
	t.Parallel()
	env := setupCustomTaskFixture(t, []Task{
		{ID: "01-a", File: "01-a.md", Title: "A", Type: "AFK", Status: "open"},
	})
	d, row, _ := humanCompleteDemoSet(t, env)
	if row.Status != StatusDone {
		t.Fatalf("status = %q, want DONE before the commit", row.Status)
	}

	commitNewFile(t, env.root, "later.txt", "moved on\n")

	refresh, err := RefreshWith(d, env.tasksDir, StatePathFor(env.tasksDir))
	if err != nil {
		t.Fatalf("RefreshWith: %v", err)
	}
	ApplyVerifyVerdicts(d, refresh, verifyEnabledConfig(), env.root)
	if row := findRow(refresh, "demo"); row == nil || row.Status != StatusDone {
		t.Fatalf("row after the commit = %+v, want DONE — the branch moving is not the human changing their mind", row)
	}
	if present, value := manifestHumanCompletedBit(t, env.demoManifest()); !present || !value {
		t.Fatalf("human_completed = (present %v, value %v) after a commit, want it intact", present, value)
	}

	if _, err := ResetTaskWith(d, nil, nil, ResetTaskOptions{
		ResolveInput: ResolveInput{CWD: env.root},
		TaskPath:     env.demoTaskRef(t, "01-a.md"),
	}); err != nil {
		t.Fatalf("ResetTaskWith: %v", err)
	}
	if present, value := manifestHumanCompletedBit(t, env.demoManifest()); present && value {
		t.Fatal("human_completed must clear when the set leaves the terminal zone")
	}

	refresh, err = RefreshWith(d, env.tasksDir, StatePathFor(env.tasksDir))
	if err != nil {
		t.Fatalf("RefreshWith: %v", err)
	}
	ApplyVerifyVerdicts(d, refresh, verifyEnabledConfig(), env.root)
	row = findRow(refresh, "demo")
	if row == nil || row.Status != StatusReady {
		t.Fatalf("row after the reopen = %+v, want READY", row)
	}
	if row.VerifyMark != VerifyMarkNone {
		t.Fatalf("mark = %q, want none on a non-terminal set", row.VerifyMark)
	}
}

// TestVerifyPhaseHumanCompletedRunsVerifierWithoutParking: the pre-approval
// Verifier phase still schedules the Verifier for a human-completed set — the
// verification is deferred to a mark, never skipped — and a NEEDS-HUMAN verdict
// neither parks the set nor spawns remediation, so the drain falls through to its
// terminal switch and the set stays foldable.
func TestVerifyPhaseHumanCompletedRunsVerifierWithoutParking(t *testing.T) {
	called := false
	run, refresh, row, indexPath := newVerifyPhaseRunWithKeys(t, func(string) (string, error) {
		called = true
		return "VERDICT: NEEDS-HUMAN\nFINDINGS: criterion 2 unmet\n", nil
	}, map[string]any{"human_completed": true})

	directive, err := run.verifyPhase(refresh, row)
	if err != nil {
		t.Fatalf("verifyPhase: %v", err)
	}
	if !called {
		t.Fatal("the Verifier must still run on a human-completed set")
	}
	if directive != verifyFallThrough {
		t.Fatalf("directive = %d, want verifyFallThrough (%d)", directive, verifyFallThrough)
	}
	if run.result.TaskSetVerifyFailed {
		t.Fatal("a non-PASS verdict must not park a set a human already completed")
	}
	if row.Status != StatusDone {
		t.Fatalf("display row status = %q, want DONE", row.Status)
	}
	m := LoadManifest(run.d, "demo", indexPath)
	if !m.Valid {
		t.Fatalf("reloaded manifest invalid: %v", m.Errors)
	}
	if got := remediationDepth(m); got != 0 {
		t.Fatalf("remediationDepth = %d, want 0 — remediation would reopen work the human closed", got)
	}
	// The verdict is on record, which is what puts the verify-failed mark on the
	// set's status and keeps the finding reachable.
	repo, _, head := runtimeHead(t, run.d, run.runtimePath)
	if stored := readStoredVerdict(t, run.d, repo, "demo", head); stored == nil || stored.Verdict != string(VerdictNeedsHuman) {
		t.Fatalf("stored verdict = %+v, want NEEDS-HUMAN recorded at HEAD", stored)
	}
}

// TestManifestHumanCompletedRoundTripsAndToleratesGarbage: the bit is a key in a
// file a human edits, so it must survive a rewrite untouched and a typo in it
// must read as absent (the pre-existing, verification-gates-the-status behaviour)
// rather than turning the set MALFORMED.
func TestManifestHumanCompletedRoundTripsAndToleratesGarbage(t *testing.T) {
	t.Parallel()
	d := newTestDeps(t)
	dir := t.TempDir()

	writeTaskMD(t, dir, "01-a.md", "## Acceptance criteria\n\n- [x] ok\n")
	writeManifestWithSetKeys(t, dir, doneAFKSet(), map[string]any{"human_completed": true})
	path := dir + "/index.json"
	m := LoadManifest(d, "demo", path)
	if !m.Valid {
		t.Fatalf("manifest invalid: %v", m.Errors)
	}
	if !m.HumanCompleted {
		t.Fatal("human_completed: true must load as the bit set")
	}
	if err := WriteManifestAtomic(d, m); err != nil {
		t.Fatalf("WriteManifestAtomic: %v", err)
	}
	if present, value := manifestHumanCompletedBit(t, path); !present || !value {
		t.Fatalf("human_completed = (present %v, value %v) after a rewrite, want it preserved", present, value)
	}

	writeTaskMD(t, dir, "01-a.md", "## Acceptance criteria\n\n- [x] ok\n")
	writeManifestWithSetKeys(t, dir, doneAFKSet(), map[string]any{"human_completed": "yes"})
	m = LoadManifest(d, "demo", path)
	if !m.Valid {
		t.Fatalf("a malformed human_completed must not make the set MALFORMED: %v", m.Errors)
	}
	if m.HumanCompleted {
		t.Fatal("a malformed human_completed must read as absent")
	}
	if got := DeriveStatusWithVerdict(m, true, nil, nil); got != StatusNeedsVerify {
		t.Fatalf("status = %q, want NEEDS-VERIFY — an unreadable bit gates as if absent", got)
	}
}
