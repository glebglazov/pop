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
		human        bool
		current      *store.VerifyVerdict
		latestPass   *store.VerifyVerdict
		wantStatus   TaskSetStatus
		wantMark     VerifyMark
		wantVerified string
		wantDrifted  bool
	}{
		{"PASS at HEAD → DONE, SHA surfaced at HEAD",
			pureAFKDone, false, verdictAt(head, VerdictPass), verdictAt(head, VerdictPass), StatusDone, VerifyMarkVerified, ShortSHA(head), false},
		{"NEEDS-HUMAN at HEAD → VERIFY-FAILED",
			pureAFKDone, false, verdictAt(head, VerdictNeedsHuman), nil, StatusVerifyFailed, VerifyMarkFailed, "", false},
		{"FIXABLE at HEAD → VERIFY-FAILED",
			pureAFKDone, false, verdictAt(head, VerdictFixable), nil, StatusVerifyFailed, VerifyMarkFailed, "", false},
		{"no HEAD verdict, older PASS → DONE + drifted SHA (ADR-0096)",
			pureAFKDone, false, nil, verdictAt(older, VerdictPass), StatusDone, VerifyMarkVerified, ShortSHA(older), true},
		{"no HEAD verdict, PASS recorded at HEAD → DONE, SHA at HEAD",
			pureAFKDone, false, nil, verdictAt(head, VerdictPass), StatusDone, VerifyMarkVerified, ShortSHA(head), false},
		{"no verdict at all → NEEDS-VERIFY",
			pureAFKDone, false, nil, nil, StatusNeedsVerify, VerifyMarkUnverified, "", false},
		{"AWAITING-APPROVAL immunized by older PASS → AWAITING-APPROVAL + drifted SHA",
			afkDoneHITLOpen, false, nil, verdictAt(older, VerdictPass), StatusAwaitingApproval, VerifyMarkVerified, ShortSHA(older), true},
		{"non-terminal manifest is never gated → READY, no mark, no SHA",
			ready, false, verdictAt(head, VerdictNeedsHuman), verdictAt(older, VerdictPass), StatusReady, VerifyMarkNone, "", false},

		// Human completion outranks the verdict: the status is the human's, the
		// verdict's answer rides beside it as the mark.
		{"human-completed, no verdict → DONE + unverified mark",
			pureAFKDone, true, nil, nil, StatusDone, VerifyMarkUnverified, "", false},
		{"human-completed, NEEDS-HUMAN at HEAD → DONE + verify-failed mark",
			pureAFKDone, true, verdictAt(head, VerdictNeedsHuman), nil, StatusDone, VerifyMarkFailed, "", false},
		{"human-completed, PASS at HEAD → DONE + verified mark and SHA",
			pureAFKDone, true, verdictAt(head, VerdictPass), nil, StatusDone, VerifyMarkVerified, ShortSHA(head), false},
		{"human-completed AWAITING-APPROVAL, no verdict → AWAITING-APPROVAL + unverified mark",
			afkDoneHITLOpen, true, nil, nil, StatusAwaitingApproval, VerifyMarkUnverified, "", false},
		{"a human-completion bit on a non-terminal set gates nothing → READY",
			ready, true, nil, nil, StatusReady, VerifyMarkNone, "", false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m := &Manifest{Valid: true, Tasks: tc.tasks, HumanCompleted: tc.human}
			got := ResolveVerifiedStatus(m, head, tc.current, tc.latestPass)
			if got.Status != tc.wantStatus {
				t.Errorf("status = %q, want %q", got.Status, tc.wantStatus)
			}
			if got.Mark != tc.wantMark {
				t.Errorf("mark = %q, want %q", got.Mark, tc.wantMark)
			}
			if got.VerifiedAtSHA != tc.wantVerified {
				t.Errorf("verifiedAtSHA = %q, want %q", got.VerifiedAtSHA, tc.wantVerified)
			}
			if got.Drifted != tc.wantDrifted {
				t.Errorf("drifted = %v, want %v", got.Drifted, tc.wantDrifted)
			}
		})
	}
}
