package tasks

import (
	"io"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/glebglazov/pop/config"
	"github.com/glebglazov/pop/internal/deps"
	"github.com/glebglazov/pop/store"
)

func verdictPtr(v Verdict) *Verdict { return &v }

// TestDeriveStatusWithVerdictDisabled locks the default: with verification off,
// the verdict is ignored entirely and status derives from the manifest alone.
func TestDeriveStatusWithVerdictDisabled(t *testing.T) {
	pureAFKDone := []Task{
		{ID: "01-a", Type: "AFK", Status: "done"},
		{ID: "02-b", Type: "AFK", Status: "done"},
	}
	afkDoneHITLOpen := []Task{
		{ID: "01-a", Type: "AFK", Status: "done"},
		{ID: "02-gate", Type: "HITL", Status: "open"},
	}
	cases := []struct {
		name       string
		tasks      []Task
		verdict    *Verdict
		latestPass *Verdict
		want       TaskSetStatus
	}{
		{"all AFK done, no verdict → DONE", pureAFKDone, nil, nil, StatusDone},
		{"all AFK done, NEEDS-HUMAN verdict ignored → DONE", pureAFKDone, verdictPtr(VerdictNeedsHuman), nil, StatusDone},
		{"AFK done + HITL open → AWAITING-APPROVAL", afkDoneHITLOpen, nil, nil, StatusAwaitingApproval},
		{"all AFK done, stale PASS ignored → DONE", pureAFKDone, nil, verdictPtr(VerdictPass), StatusDone},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m := &Manifest{Valid: true, Tasks: tc.tasks}
			if got := DeriveStatusWithVerdict(m, false, tc.verdict, tc.latestPass); got != tc.want {
				t.Fatalf("status = %q, want %q", got, tc.want)
			}
		})
	}
}

// TestDeriveStatusWithVerdictEnabled locks the verdict-gated precedence: the
// verdict only decides the terminal zone (AFK work complete); every other
// manifest status is untouched, including BLOCKED.
func TestDeriveStatusWithVerdictEnabled(t *testing.T) {
	pureAFKDone := []Task{
		{ID: "01-a", Type: "AFK", Status: "done"},
		{ID: "02-b", Type: "AFK", Status: "done"},
	}
	afkDoneHITLOpen := []Task{
		{ID: "01-a", Type: "AFK", Status: "done"},
		{ID: "02-gate", Type: "HITL", Status: "open"},
	}
	blocked := []Task{
		{ID: "01-gate", Type: "HITL", Status: "open"},
		{ID: "02-a", Type: "AFK", Status: "open", BlockedBy: []string{"01-gate"}},
	}
	ready := []Task{{ID: "01-a", Type: "AFK", Status: "open"}}
	failed := []Task{{ID: "01-a", Type: "AFK", Status: "failed"}}
	deferred := []Task{
		{ID: "01-a", Type: "AFK", Status: "done"},
		{ID: "02-b", Type: "AFK", Status: "skipped"},
	}

	cases := []struct {
		name       string
		tasks      []Task
		verdict    *Verdict
		latestPass *Verdict
		want       TaskSetStatus
	}{
		{"all AFK done, absent verdict → NEEDS-VERIFY", pureAFKDone, nil, nil, StatusNeedsVerify},
		{"all AFK done, PASS → DONE", pureAFKDone, verdictPtr(VerdictPass), nil, StatusDone},
		{"all AFK done, NEEDS-HUMAN → VERIFY-FAILED", pureAFKDone, verdictPtr(VerdictNeedsHuman), nil, StatusVerifyFailed},
		{"all AFK done, FIXABLE → VERIFY-FAILED", pureAFKDone, verdictPtr(VerdictFixable), nil, StatusVerifyFailed},
		{"AFK done + HITL open, absent → NEEDS-VERIFY", afkDoneHITLOpen, nil, nil, StatusNeedsVerify},
		{"AFK done + HITL open, PASS → AWAITING-APPROVAL", afkDoneHITLOpen, verdictPtr(VerdictPass), nil, StatusAwaitingApproval},
		{"AFK done + HITL open, NEEDS-HUMAN → VERIFY-FAILED", afkDoneHITLOpen, verdictPtr(VerdictNeedsHuman), nil, StatusVerifyFailed},
		{"open AFK gated behind HITL stays BLOCKED even with PASS", blocked, verdictPtr(VerdictPass), nil, StatusBlocked},
		{"ready set untouched by absent verdict", ready, nil, nil, StatusReady},
		{"failed set untouched by PASS", failed, verdictPtr(VerdictPass), nil, StatusFailed},
		{"deferred set untouched by absent verdict", deferred, nil, nil, StatusDeferred},
		// ADR-0096: an older PASS immunizes the terminal status against later commits.
		{"all AFK done, stale PASS immunizes → DONE", pureAFKDone, nil, verdictPtr(VerdictPass), StatusDone},
		{"AFK done + HITL open, stale PASS immunizes → AWAITING-APPROVAL", afkDoneHITLOpen, nil, verdictPtr(VerdictPass), StatusAwaitingApproval},
		{"all AFK done, current non-PASS beats stale PASS → VERIFY-FAILED", pureAFKDone, verdictPtr(VerdictNeedsHuman), verdictPtr(VerdictPass), StatusVerifyFailed},
		{"AFK done + HITL open, current non-PASS beats stale PASS → VERIFY-FAILED", afkDoneHITLOpen, verdictPtr(VerdictFixable), verdictPtr(VerdictPass), StatusVerifyFailed},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m := &Manifest{Valid: true, Tasks: tc.tasks}
			if got := DeriveStatusWithVerdict(m, true, tc.verdict, tc.latestPass); got != tc.want {
				t.Fatalf("status = %q, want %q", got, tc.want)
			}
		})
	}
}

// verifyStatusGit answers the two git commands ApplyVerifyVerdicts issues: the
// common-dir probe (repository identity) and HEAD (the current work SHA).
func verifyStatusGit(commonDir, head string) *deps.MockGit {
	return &deps.MockGit{CommandInDirFunc: func(dir string, args ...string) (string, error) {
		switch {
		case len(args) >= 2 && args[0] == "rev-parse" && args[1] == "--git-common-dir":
			return commonDir, nil
		case len(args) >= 2 && args[0] == "rev-parse" && args[1] == "HEAD":
			return head, nil
		}
		return "", nil
	}}
}

// setupVerifyStatusDeps isolates the data dir to a temp location and returns
// deps whose git reports the given common dir and HEAD.
func setupVerifyStatusDeps(t *testing.T, commonDir, head string) *Deps {
	t.Helper()
	root := t.TempDir()
	t.Setenv("XDG_DATA_HOME", filepath.Join(root, ".xdg"))
	return &Deps{FS: deps.NewRealFileSystem(), Git: verifyStatusGit(commonDir, head)}
}

func putStatusVerdict(t *testing.T, d *Deps, repo, setID, sha, verdict, findings string) {
	t.Helper()
	s, err := openDrainStore(d)
	if err != nil {
		t.Fatalf("openDrainStore: %v", err)
	}
	defer func() { _ = s.Close() }()
	if err := s.PutVerifyVerdict(store.VerifyVerdict{
		Repo: repo, SetID: setID, WorkSHA: sha, Verdict: verdict, Findings: findings, ComputedAt: time.Now().UTC(),
	}); err != nil {
		t.Fatalf("PutVerifyVerdict: %v", err)
	}
}

// doneResult builds a one-row RefreshResult for a pure-AFK set whose manifest
// status is DONE, so ApplyVerifyVerdicts can gate it.
func doneResult() *RefreshResult {
	m := &Manifest{Valid: true, Stem: "demo", Tasks: []Task{{ID: "01-a", File: "01-a.md", Type: "AFK", Status: "done"}}}
	return &RefreshResult{
		Rows:      []Row{buildTaskSetRow(RegisteredTaskSet{ID: "demo"}, m, 0)},
		Manifests: map[string]*Manifest{"demo": m},
	}
}

func rowStatus(result *RefreshResult, id string) TaskSetStatus {
	for _, row := range result.Rows {
		if row.ID == id {
			return row.Status
		}
	}
	return ""
}

func TestApplyVerifyVerdictsDisabledIsNoOp(t *testing.T) {
	d := setupVerifyStatusDeps(t, "/repo/.git\n", "shaCUR\n")
	putStatusVerdict(t, d, "/repo/.git", "demo", "shaCUR", "NEEDS-HUMAN", "bad")

	result := doneResult()
	// nil config ⇒ feature disabled ⇒ status stays manifest-derived DONE even
	// though a NEEDS-HUMAN verdict sits in the store at the current SHA.
	ApplyVerifyVerdicts(d, result, nil, "/rt")
	if got := rowStatus(result, "demo"); got != StatusDone {
		t.Fatalf("disabled status = %q, want DONE (manifest alone)", got)
	}
}

func TestApplyVerifyVerdictsEnabledGatesOnVerdict(t *testing.T) {
	enabled := &config.Config{Task: &config.TasksConfig{Verify: &config.VerifyConfig{Enabled: true}}}

	cases := []struct {
		name         string
		writeVerdict func(d *Deps)
		want         TaskSetStatus
	}{
		{
			name:         "no verdict → NEEDS-VERIFY",
			writeVerdict: func(*Deps) {},
			want:         StatusNeedsVerify,
		},
		{
			name:         "fresh PASS → DONE",
			writeVerdict: func(d *Deps) { putStatusVerdict(t, d, "/repo/.git", "demo", "shaCUR", "PASS", "") },
			want:         StatusDone,
		},
		{
			name:         "NEEDS-HUMAN → VERIFY-FAILED",
			writeVerdict: func(d *Deps) { putStatusVerdict(t, d, "/repo/.git", "demo", "shaCUR", "NEEDS-HUMAN", "criterion 2 unmet") },
			want:         StatusVerifyFailed,
		},
		{
			name:         "stale-SHA PASS immunizes → DONE",
			writeVerdict: func(d *Deps) { putStatusVerdict(t, d, "/repo/.git", "demo", "shaOLD", "PASS", "") },
			want:         StatusDone,
		},
		{
			name: "stale-SHA PASS ignored when current HEAD non-PASS → VERIFY-FAILED",
			writeVerdict: func(d *Deps) {
				putStatusVerdict(t, d, "/repo/.git", "demo", "shaOLD", "PASS", "")
				putStatusVerdict(t, d, "/repo/.git", "demo", "shaCUR", "NEEDS-HUMAN", "contract drift")
			},
			want: StatusVerifyFailed,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			d := setupVerifyStatusDeps(t, "/repo/.git\n", "shaCUR\n")
			tc.writeVerdict(d)
			result := doneResult()
			ApplyVerifyVerdicts(d, result, enabled, "/rt")
			if got := rowStatus(result, "demo"); got != tc.want {
				t.Fatalf("status = %q, want %q", got, tc.want)
			}
		})
	}
}

// TestApplyVerifyVerdictsSkipsArchivedView confirms the archived listing
// (result.ShowArchived) is outside the verification loop (ADR-0026): a
// formerly-Done archived set keeps its manifest-derived DONE status even with
// a NEEDS-HUMAN verdict at the current SHA that would otherwise force
// VERIFY-FAILED / NEEDS-VERIFY.
func TestApplyVerifyVerdictsSkipsArchivedView(t *testing.T) {
	enabled := &config.Config{Task: &config.TasksConfig{Verify: &config.VerifyConfig{Enabled: true}}}
	d := setupVerifyStatusDeps(t, "/repo/.git\n", "shaCUR\n")
	putStatusVerdict(t, d, "/repo/.git", "demo", "shaCUR", "NEEDS-HUMAN", "would fail if graded")

	result := doneResult()
	result.ShowArchived = true

	ApplyVerifyVerdicts(d, result, enabled, "/rt")
	if got := rowStatus(result, "demo"); got != StatusDone {
		t.Fatalf("archived status = %q, want DONE (verdict overlay skipped)", got)
	}
}

func TestApplyVerifyVerdictsWithPerSetRuntime(t *testing.T) {
	enabled := &config.Config{Task: &config.TasksConfig{Verify: &config.VerifyConfig{Enabled: true}}}
	d := setupVerifyStatusDeps(t, "/repo/.git\n", "shaCUR\n")
	putStatusVerdict(t, d, "/repo/.git", "bound", "shaCUR", "NEEDS-HUMAN", "bound set failed")

	result := doneResult()
	result.Rows[0].ID = "bound"
	result.Manifests["bound"] = result.Manifests["demo"]
	delete(result.Manifests, "demo")

	ApplyVerifyVerdictsWith(d, result, enabled, func(setID string) string {
		if setID == "bound" {
			return "/wt/bound"
		}
		return "/trunk"
	})
	if got := rowStatus(result, "bound"); got != StatusVerifyFailed {
		t.Fatalf("bound status = %q, want VERIFY-FAILED", got)
	}
}

// TestApplyVerifyVerdictsLeavesNonTerminalRows guards the terminal-zone gate:
// a missing row (no manifest) and a ready row must be untouched even with the
// feature enabled, so re-derivation never corrupts a MISSING set into MALFORMED.
func TestApplyVerifyVerdictsLeavesNonTerminalRows(t *testing.T) {
	enabled := &config.Config{Task: &config.TasksConfig{Verify: &config.VerifyConfig{Enabled: true}}}
	d := setupVerifyStatusDeps(t, "/repo/.git\n", "shaCUR\n")

	readyM := &Manifest{Valid: true, Stem: "live", Tasks: []Task{{ID: "01-a", File: "01-a.md", Type: "AFK", Status: "open"}}}
	result := &RefreshResult{
		Rows: []Row{
			{ID: "gone", Status: StatusMissing},
			buildTaskSetRow(RegisteredTaskSet{ID: "live"}, readyM, 0),
		},
		Manifests: map[string]*Manifest{"live": readyM},
	}

	ApplyVerifyVerdicts(d, result, enabled, "/rt")
	if got := rowStatus(result, "gone"); got != StatusMissing {
		t.Fatalf("missing row status = %q, want MISSING (untouched)", got)
	}
	if got := rowStatus(result, "live"); got != StatusReady {
		t.Fatalf("ready row status = %q, want READY (untouched)", got)
	}
}

// TestApplyVerifyVerdictsSurfacesFindingsAndReorders confirms a VERIFY-FAILED
// row carries the Verifier findings and that a formerly-Done set moves out of
// the Done group, and that `pop tasks status` renders the new label.
func TestApplyVerifyVerdictsSurfacesFindingsAndReorders(t *testing.T) {
	enabled := &config.Config{Task: &config.TasksConfig{Verify: &config.VerifyConfig{Enabled: true}}}
	d := setupVerifyStatusDeps(t, "/repo/.git\n", "shaCUR\n")
	putStatusVerdict(t, d, "/repo/.git", "demo", "shaCUR", "NEEDS-HUMAN", "the API contract drifted\nsecond line")

	result := doneResult()
	ApplyVerifyVerdicts(d, result, enabled, "/rt")

	row := FindRow(result, "demo")
	if row == nil || row.Status != StatusVerifyFailed {
		t.Fatalf("row = %+v, want VERIFY-FAILED", row)
	}
	if !strings.Contains(row.VerifyFindings, "API contract drifted") {
		t.Fatalf("findings = %q, want the Verifier reasons", row.VerifyFindings)
	}
	table := formatTable(result.Rows)
	if !strings.Contains(table, "VERIFY-FAILED") {
		t.Fatalf("status table missing VERIFY-FAILED disposition:\n%s", table)
	}
	plainOut := outputFor(io.Discard)
	if !strings.Contains(rowDetail(plainOut, *row), "API contract drifted") {
		t.Fatalf("detail missing findings hint: %q", rowDetail(plainOut, *row))
	}
}

// TestApplyVerifyVerdictsSetsVerifiedAtSHA confirms an immunized terminal row
// whose HEAD differs from the PASS verdict's work SHA carries VerifiedAtSHA,
// and that fresh PASS, non-PASS, and non-immunized rows leave it empty.
func TestApplyVerifyVerdictsSetsVerifiedAtSHA(t *testing.T) {
	enabled := &config.Config{Task: &config.TasksConfig{Verify: &config.VerifyConfig{Enabled: true}}}

	cases := []struct {
		name          string
		workSHA       string
		verdictSHA    string
		verdict       string
		wantStatus    TaskSetStatus
		wantVerified  string
	}{
		{"fresh PASS at HEAD", "shaCUR", "shaCUR", "PASS", StatusDone, ""},
		{"stale PASS immunizes with different SHA", "shaCUR", "shaOLD", "PASS", StatusDone, "shaOLD"},
		{"no verdict → NEEDS-VERIFY", "shaCUR", "", "", StatusNeedsVerify, ""},
		{"current NEEDS-HUMAN → VERIFY-FAILED", "shaCUR", "shaCUR", "NEEDS-HUMAN", StatusVerifyFailed, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			d := setupVerifyStatusDeps(t, "/repo/.git\n", tc.workSHA+"\n")
			if tc.verdictSHA != "" {
				putStatusVerdict(t, d, "/repo/.git", "demo", tc.verdictSHA, tc.verdict, "")
			}
			result := doneResult()
			ApplyVerifyVerdicts(d, result, enabled, "/rt")
			row := FindRow(result, "demo")
			if row == nil {
				t.Fatalf("row missing")
			}
			if row.Status != tc.wantStatus {
				t.Fatalf("status = %q, want %q", row.Status, tc.wantStatus)
			}
			if row.VerifiedAtSHA != tc.wantVerified {
				t.Fatalf("VerifiedAtSHA = %q, want %q", row.VerifiedAtSHA, tc.wantVerified)
			}
		})
	}
}

// TestApplyVerifyVerdictsAwaitingApprovalVerifiedAtSHA confirms an
// AWAITING-APPROVAL set immunized at a different SHA also carries VerifiedAtSHA.
func TestApplyVerifyVerdictsAwaitingApprovalVerifiedAtSHA(t *testing.T) {
	enabled := &config.Config{Task: &config.TasksConfig{Verify: &config.VerifyConfig{Enabled: true}}}
	d := setupVerifyStatusDeps(t, "/repo/.git\n", "shaCUR\n")
	putStatusVerdict(t, d, "/repo/.git", "demo", "shaOLD", "PASS", "")

	m := &Manifest{Valid: true, Stem: "demo", Tasks: []Task{
		{ID: "01-a", File: "01-a.md", Type: "AFK", Status: "done"},
		{ID: "02-gate", File: "02-gate.md", Type: "HITL", Status: "open"},
	}}
	result := &RefreshResult{
		Rows:      []Row{buildTaskSetRow(RegisteredTaskSet{ID: "demo"}, m, 0)},
		Manifests: map[string]*Manifest{"demo": m},
	}
	ApplyVerifyVerdicts(d, result, enabled, "/rt")

	row := FindRow(result, "demo")
	if row == nil || row.Status != StatusAwaitingApproval {
		t.Fatalf("row = %+v, want AWAITING-APPROVAL", row)
	}
	if row.VerifiedAtSHA != "shaOLD" {
		t.Fatalf("VerifiedAtSHA = %q, want shaOLD", row.VerifiedAtSHA)
	}
}

// TestRenderVerifiedAtSHASuffix confirms the yellow `verified @ <shortSHA>`
// suffix appears in the Details column for immunized DONE and AWAITING-APPROVAL
// rows, and is absent for NEEDS-VERIFY / VERIFY-FAILED rows.
func TestRenderVerifiedAtSHASuffix(t *testing.T) {
	plainOut := outputFor(io.Discard)

	done := Row{ID: "done", Status: StatusDone, Progress: "1/1 done", VerifiedAtSHA: "abcdef1234567890"}
	if got := rowDetail(plainOut, done); !strings.Contains(got, "verified @ abcdef123456") {
		t.Fatalf("DONE detail missing suffix: %q", got)
	}

	await := Row{ID: "await", Status: StatusAwaitingApproval, Progress: "1/1 done", VerifiedAtSHA: "abcdef1234567890"}
	if got := rowDetail(plainOut, await); !strings.Contains(got, "verified @ abcdef123456") {
		t.Fatalf("AWAITING-APPROVAL detail missing suffix: %q", got)
	}

	needs := Row{ID: "needs", Status: StatusNeedsVerify, Progress: "1/1 done", VerifiedAtSHA: ""}
	if got := rowDetail(plainOut, needs); strings.Contains(got, "verified @") {
		t.Fatalf("NEEDS-VERIFY detail should not contain suffix: %q", got)
	}

	failed := Row{ID: "failed", Status: StatusVerifyFailed, Progress: "1/1 done", VerifiedAtSHA: ""}
	if got := rowDetail(plainOut, failed); strings.Contains(got, "verified @") {
		t.Fatalf("VERIFY-FAILED detail should not contain suffix: %q", got)
	}

	// A DONE row whose HEAD matches the verified SHA shows no suffix.
	matched := Row{ID: "matched", Status: StatusDone, Progress: "1/1 done", VerifiedAtSHA: ""}
	if got := rowDetail(plainOut, matched); strings.Contains(got, "verified @") {
		t.Fatalf("matched DONE detail should not contain suffix: %q", got)
	}

	// With color enabled the suffix is wrapped in yellow ANSI codes.
	colorOut := &output{Writer: io.Discard, color: true}
	if got := rowDetail(colorOut, done); !strings.Contains(got, ansiYellow+"verified @") {
		t.Fatalf("color output should wrap suffix in yellow: %q", got)
	}
}
