package tasks

import (
	"io"
	"reflect"
	"strings"
	"sync"
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
	t.Parallel()
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
	t.Parallel()
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
	d := newTestDeps(t)
	d.Git = verifyStatusGit(commonDir, head)
	return d
}

func putStatusVerdict(t *testing.T, d *Deps, repo, setID, sha, verdict, findings string) {
	t.Helper()
	s, err := openDrainStore(d)
	if err != nil {
		t.Fatalf("openDrainStore: %v", err)
	}
	defer func() { _ = d.CloseStore() }()
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
	t.Parallel()
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
	t.Parallel()
	enabled := &config.Config{Work: &config.WorkConfig{Verify: &config.VerifyConfig{Enabled: true}}}

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
	t.Parallel()
	enabled := &config.Config{Work: &config.WorkConfig{Verify: &config.VerifyConfig{Enabled: true}}}
	d := setupVerifyStatusDeps(t, "/repo/.git\n", "shaCUR\n")
	putStatusVerdict(t, d, "/repo/.git", "demo", "shaCUR", "NEEDS-HUMAN", "would fail if graded")

	result := doneResult()
	result.ShowArchived = true

	ApplyVerifyVerdicts(d, result, enabled, "/rt")
	if got := rowStatus(result, "demo"); got != StatusDone {
		t.Fatalf("archived status = %q, want DONE (verdict overlay skipped)", got)
	}
}

// TestApplyVerifyVerdictsMemoizesCheckoutResolution guards the dashboard perf
// path: sets sharing one checkout resolve its repo identity and HEAD once, not
// per row, and a non-terminal row forks no git at all.
func TestApplyVerifyVerdictsMemoizesCheckoutResolution(t *testing.T) {
	t.Parallel()
	enabled := &config.Config{Work: &config.WorkConfig{Verify: &config.VerifyConfig{Enabled: true}}}
	var commonDirCalls, headCalls int
	git := &deps.MockGit{CommandInDirFunc: func(dir string, args ...string) (string, error) {
		switch {
		case len(args) >= 2 && args[0] == "rev-parse" && args[1] == "--git-common-dir":
			commonDirCalls++
			return "/repo/.git\n", nil
		case len(args) >= 2 && args[0] == "rev-parse" && args[1] == "HEAD":
			headCalls++
			return "shaCUR\n", nil
		}
		return "", nil
	}}
	d := newTestDeps(t)
	d.Git = git

	doneM := &Manifest{Valid: true, Stem: "d", Tasks: []Task{{ID: "01", File: "01.md", Type: "AFK", Status: "done"}}}
	readyM := &Manifest{Valid: true, Stem: "r", Tasks: []Task{{ID: "01", File: "01.md", Type: "AFK", Status: "open"}}}
	result := &RefreshResult{
		Rows: []Row{
			buildTaskSetRow(RegisteredTaskSet{ID: "a"}, doneM, 0),
			buildTaskSetRow(RegisteredTaskSet{ID: "b"}, doneM, 1),
			buildTaskSetRow(RegisteredTaskSet{ID: "c"}, doneM, 2),
			buildTaskSetRow(RegisteredTaskSet{ID: "ready"}, readyM, 3),
		},
		Manifests: map[string]*Manifest{"a": doneM, "b": doneM, "c": doneM, "ready": readyM},
	}

	// All three done sets share one checkout ("/rt"); the ready set is non-terminal.
	ApplyVerifyVerdicts(d, result, enabled, "/rt")

	if commonDirCalls != 1 {
		t.Fatalf("git common-dir resolved %d times, want 1 (memoized per checkout)", commonDirCalls)
	}
	if headCalls != 1 {
		t.Fatalf("git HEAD resolved %d times, want 1 (memoized per checkout)", headCalls)
	}
	// No PASS verdict at HEAD ⇒ every terminal set regresses to NEEDS-VERIFY.
	for _, id := range []string{"a", "b", "c"} {
		if got := rowStatus(result, id); got != StatusNeedsVerify {
			t.Fatalf("set %s status = %q, want NEEDS-VERIFY", id, got)
		}
	}
	if got := rowStatus(result, "ready"); got != StatusReady {
		t.Fatalf("ready set status = %q, want READY (untouched, no git)", got)
	}
}

func TestApplyVerifyVerdictsWithPerSetRuntime(t *testing.T) {
	t.Parallel()
	enabled := &config.Config{Work: &config.WorkConfig{Verify: &config.VerifyConfig{Enabled: true}}}
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

// TestApplyVerifyVerdictsWithUnplacedSkipsTrunkHEAD: an empty per-set runtime
// (unplaced) must leave the terminal status alone — never overlay a trunk HEAD
// verdict (ADR-0147).
func TestApplyVerifyVerdictsWithUnplacedSkipsTrunkHEAD(t *testing.T) {
	t.Parallel()
	enabled := &config.Config{Work: &config.WorkConfig{Verify: &config.VerifyConfig{Enabled: true}}}
	d := setupVerifyStatusDeps(t, "/repo/.git\n", "shaCUR\n")
	putStatusVerdict(t, d, "/repo/.git", "unplaced", "shaCUR", "NEEDS-HUMAN", "trunk-only")

	result := doneResult()
	result.Rows[0].ID = "unplaced"
	result.Manifests["unplaced"] = result.Manifests["demo"]
	delete(result.Manifests, "demo")

	ApplyVerifyVerdictsWith(d, result, enabled, func(string) string { return "" })
	if got := rowStatus(result, "unplaced"); got != StatusDone {
		t.Fatalf("unplaced status = %q, want DONE (no trunk HEAD overlay)", got)
	}
}

// TestApplyVerifyVerdictsLeavesNonTerminalRows guards the terminal-zone gate:
// a missing row (no manifest) and a ready row must be untouched even with the
// feature enabled, so re-derivation never corrupts a MISSING set into MALFORMED.
func TestApplyVerifyVerdictsLeavesNonTerminalRows(t *testing.T) {
	t.Parallel()
	enabled := &config.Config{Work: &config.WorkConfig{Verify: &config.VerifyConfig{Enabled: true}}}
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
	t.Parallel()
	enabled := &config.Config{Work: &config.WorkConfig{Verify: &config.VerifyConfig{Enabled: true}}}
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
	t.Parallel()
	enabled := &config.Config{Work: &config.WorkConfig{Verify: &config.VerifyConfig{Enabled: true}}}

	cases := []struct {
		name          string
		workSHA       string
		verdictSHA    string
		verdict       string
		wantStatus    TaskSetStatus
		wantVerified  string
		wantDrifted   bool
	}{
		{"fresh PASS at HEAD", "shaCUR", "shaCUR", "PASS", StatusDone, "shaCUR", false},
		{"stale PASS immunizes with different SHA", "shaCUR", "shaOLD", "PASS", StatusDone, "shaOLD", true},
		{"no verdict → NEEDS-VERIFY", "shaCUR", "", "", StatusNeedsVerify, "", false},
		{"current NEEDS-HUMAN → VERIFY-FAILED", "shaCUR", "shaCUR", "NEEDS-HUMAN", StatusVerifyFailed, "", false},
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
			if row.VerifiedAtDrifted != tc.wantDrifted {
				t.Fatalf("VerifiedAtDrifted = %v, want %v", row.VerifiedAtDrifted, tc.wantDrifted)
			}
		})
	}
}

// TestApplyVerifyVerdictsAwaitingApprovalVerifiedAtSHA confirms an
// AWAITING-APPROVAL set immunized at a different SHA also carries VerifiedAtSHA.
func TestApplyVerifyVerdictsAwaitingApprovalVerifiedAtSHA(t *testing.T) {
	t.Parallel()
	enabled := &config.Config{Work: &config.WorkConfig{Verify: &config.VerifyConfig{Enabled: true}}}
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
	if !row.VerifiedAtDrifted {
		t.Fatalf("VerifiedAtDrifted = false, want true")
	}
}

// TestRenderVerifiedAtBadge confirms the three-state Verified-at SHA badge in the
// Details column: green at HEAD, yellow when drifted, red unverified, absent when
// verification is off or irrelevant.
func TestRenderVerifiedAtBadge(t *testing.T) {
	t.Parallel()
	plainOut := outputFor(io.Discard)

	doneDrifted := Row{ID: "done", Status: StatusDone, Progress: "1/1 done", VerifyMark: VerifyMarkVerified, VerifiedAtSHA: "abcdef1234567890", VerifiedAtDrifted: true}
	if got := rowDetail(plainOut, doneDrifted); !strings.Contains(got, "verified @ abcdef123456") {
		t.Fatalf("drifted DONE detail missing suffix: %q", got)
	}

	doneAtHead := Row{ID: "done", Status: StatusDone, Progress: "1/1 done", VerifyMark: VerifyMarkVerified, VerifiedAtSHA: "abcdef1234567890", VerifiedAtDrifted: false}
	if got := rowDetail(plainOut, doneAtHead); !strings.Contains(got, "verified @ abcdef123456") {
		t.Fatalf("at-HEAD DONE detail missing suffix: %q", got)
	}

	await := Row{ID: "await", Status: StatusAwaitingApproval, Progress: "1/1 done", VerifyMark: VerifyMarkVerified, VerifiedAtSHA: "abcdef1234567890", VerifiedAtDrifted: true}
	if got := rowDetail(plainOut, await); !strings.Contains(got, "verified @ abcdef123456") {
		t.Fatalf("AWAITING-APPROVAL detail missing suffix: %q", got)
	}

	needs := Row{ID: "needs", Status: StatusNeedsVerify, Progress: "1/1 done"}
	if got := rowDetail(plainOut, needs); !strings.Contains(got, "unverified") {
		t.Fatalf("NEEDS-VERIFY detail should contain unverified: %q", got)
	}

	failed := Row{ID: "failed", Status: StatusVerifyFailed, Progress: "1/1 done"}
	if got := rowDetail(plainOut, failed); !strings.Contains(got, "unverified") {
		t.Fatalf("VERIFY-FAILED detail should contain unverified: %q", got)
	}

	ready := Row{ID: "ready", Status: StatusReady, Progress: "0/1 done"}
	if got := rowDetail(plainOut, ready); strings.Contains(got, "verified @") || strings.Contains(got, "unverified") {
		t.Fatalf("READY detail should have no badge: %q", got)
	}

	colorOut := &output{Writer: io.Discard, color: true}
	if got := rowDetail(colorOut, doneDrifted); !strings.Contains(got, ansiYellow+"verified @") {
		t.Fatalf("drifted color output should wrap suffix in yellow: %q", got)
	}
	if got := rowDetail(colorOut, doneAtHead); !strings.Contains(got, ansiGreen+"verified @") {
		t.Fatalf("at-HEAD color output should wrap suffix in green: %q", got)
	}
	if got := rowDetail(colorOut, needs); !strings.Contains(got, ansiRed+"unverified") {
		t.Fatalf("NEEDS-VERIFY color output should wrap unverified in red: %q", got)
	}
}

// TestApplyVerifyVerdictsResolvesCheckoutsConcurrently drives the whole rendered
// verdict pass and locks its git cost (ADR-0243): N rows spread over K distinct
// checkouts fork git 2K times — one repository identity and one HEAD per
// checkout — while a hidden, a non-terminal, and an unplaced row fork nothing.
func TestApplyVerifyVerdictsResolvesCheckoutsConcurrently(t *testing.T) {
	t.Parallel()
	enabled := &config.Config{Work: &config.WorkConfig{Verify: &config.VerifyConfig{Enabled: true}}}

	var mu sync.Mutex
	commonDirDirs := map[string]int{}
	headDirs := map[string]int{}
	git := &deps.MockGit{CommandInDirFunc: func(dir string, args ...string) (string, error) {
		mu.Lock()
		defer mu.Unlock()
		switch {
		case len(args) >= 2 && args[0] == "rev-parse" && args[1] == "--git-common-dir":
			commonDirDirs[dir]++
			return "/repo" + dir + "/.git\n", nil
		case len(args) >= 2 && args[0] == "rev-parse" && args[1] == "HEAD":
			headDirs[dir]++
			return "shaCUR\n", nil
		}
		return "", nil
	}}
	d := newTestDeps(t)
	d.Git = git

	doneM := &Manifest{Valid: true, Stem: "d", Tasks: []Task{{ID: "01", File: "01.md", Type: "AFK", Status: "done"}}}
	readyM := &Manifest{Valid: true, Stem: "r", Tasks: []Task{{ID: "01", File: "01.md", Type: "AFK", Status: "open"}}}

	// Six rows: four terminal and rendered over three distinct checkouts (a and b
	// share one), plus a hidden terminal row, a non-terminal row, and an unplaced
	// terminal row — none of which may cost a fork.
	runtimes := map[string]string{
		"a": "/wt/one", "b": "/wt/one", "c": "/wt/two", "e": "/wt/three",
		"hidden": "/wt/hidden", "ready": "/wt/ready", "unplaced": "",
	}
	rows := []Row{
		buildTaskSetRow(RegisteredTaskSet{ID: "a"}, doneM, 0),
		buildTaskSetRow(RegisteredTaskSet{ID: "b"}, doneM, 1),
		buildTaskSetRow(RegisteredTaskSet{ID: "c"}, doneM, 2),
		buildTaskSetRow(RegisteredTaskSet{ID: "e"}, doneM, 3),
		buildTaskSetRow(RegisteredTaskSet{ID: "hidden"}, doneM, 4),
		buildTaskSetRow(RegisteredTaskSet{ID: "ready"}, readyM, 5),
		buildTaskSetRow(RegisteredTaskSet{ID: "unplaced"}, doneM, 6),
	}
	manifests := map[string]*Manifest{"ready": readyM}
	for _, id := range []string{"a", "b", "c", "e", "hidden", "unplaced"} {
		manifests[id] = doneM
	}
	result := &RefreshResult{Rows: rows, Manifests: manifests}

	ApplyVerifyVerdictsForRendered(d, result, enabled,
		func(setID string) string { return runtimes[setID] },
		func(row Row) bool { return row.ID != "hidden" },
	)

	wantDirs := map[string]int{"/wt/one": 1, "/wt/two": 1, "/wt/three": 1}
	if !reflect.DeepEqual(commonDirDirs, wantDirs) {
		t.Fatalf("repository identity forks = %v, want %v (2K forks over K checkouts)", commonDirDirs, wantDirs)
	}
	if !reflect.DeepEqual(headDirs, wantDirs) {
		t.Fatalf("HEAD forks = %v, want %v (2K forks over K checkouts)", headDirs, wantDirs)
	}

	// No verdict is stored, so every resolved row regresses; the rows that cost no
	// fork keep their manifest-derived status.
	for _, id := range []string{"a", "b", "c", "e"} {
		if got := rowStatus(result, id); got != StatusNeedsVerify {
			t.Fatalf("set %s status = %q, want NEEDS-VERIFY", id, got)
		}
	}
	for _, id := range []string{"hidden", "unplaced"} {
		if got := rowStatus(result, id); got != StatusDone {
			t.Fatalf("set %s status = %q, want DONE (unresolved)", id, got)
		}
	}
	if got := rowStatus(result, "ready"); got != StatusReady {
		t.Fatalf("ready status = %q, want READY (unresolved)", got)
	}
}
