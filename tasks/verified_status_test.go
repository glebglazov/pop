package tasks

import (
	"testing"

	"github.com/glebglazov/pop/store"
)

// TestResolveVerifiedStatus locks the read-side Verified status resolution
// directly — no store, no git, plain manifests and verdict structs. This is the
// surface the gate rule now presents; before the deepening it was only
// reachable through ApplyVerifyVerdicts with git-command mocks.
func TestResolveVerifiedStatus(t *testing.T) {
	pureAFKDone := []Task{
		{ID: "01-a", Type: "AFK", Status: "done"},
		{ID: "02-b", Type: "AFK", Status: "done"},
	}
	afkDoneHITLOpen := []Task{
		{ID: "01-a", Type: "AFK", Status: "done"},
		{ID: "02-gate", Type: "HITL", Status: "open"},
	}
	ready := []Task{
		{ID: "01-a", Type: "AFK", Status: "open"},
	}

	const head = "aaaaaaaaaaaa1111"  // current work SHA
	const older = "bbbbbbbbbbbb2222" // an earlier SHA
	verdictAt := func(sha string, v Verdict) *store.VerifyVerdict {
		return &store.VerifyVerdict{WorkSHA: sha, Verdict: string(v)}
	}

	cases := []struct {
		name         string
		tasks        []Task
		current      *store.VerifyVerdict
		latestPass   *store.VerifyVerdict
		wantStatus   TaskSetStatus
		wantVerified string
	}{
		{"PASS at HEAD → DONE, no SHA surfaced",
			pureAFKDone, verdictAt(head, VerdictPass), verdictAt(head, VerdictPass), StatusDone, ""},
		{"NEEDS-HUMAN at HEAD → VERIFY-FAILED",
			pureAFKDone, verdictAt(head, VerdictNeedsHuman), nil, StatusVerifyFailed, ""},
		{"FIXABLE at HEAD → VERIFY-FAILED",
			pureAFKDone, verdictAt(head, VerdictFixable), nil, StatusVerifyFailed, ""},
		{"no HEAD verdict, older PASS → DONE + immunization SHA (ADR-0096)",
			pureAFKDone, nil, verdictAt(older, VerdictPass), StatusDone, ShortSHA(older)},
		{"no HEAD verdict, PASS recorded at HEAD → DONE, no SHA surfaced",
			pureAFKDone, nil, verdictAt(head, VerdictPass), StatusDone, ""},
		{"no verdict at all → NEEDS-VERIFY",
			pureAFKDone, nil, nil, StatusNeedsVerify, ""},
		{"AWAITING-APPROVAL immunized by older PASS → AWAITING-APPROVAL + SHA",
			afkDoneHITLOpen, nil, verdictAt(older, VerdictPass), StatusAwaitingApproval, ShortSHA(older)},
		{"non-terminal manifest is never gated → READY, no SHA",
			ready, verdictAt(head, VerdictNeedsHuman), verdictAt(older, VerdictPass), StatusReady, ""},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m := &Manifest{Valid: true, Tasks: tc.tasks}
			gotStatus, gotVerified := ResolveVerifiedStatus(m, head, tc.current, tc.latestPass)
			if gotStatus != tc.wantStatus {
				t.Errorf("status = %q, want %q", gotStatus, tc.wantStatus)
			}
			if gotVerified != tc.wantVerified {
				t.Errorf("verifiedAtSHA = %q, want %q", gotVerified, tc.wantVerified)
			}
		})
	}
}
