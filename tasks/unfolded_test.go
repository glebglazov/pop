package tasks

import "testing"

func TestUnfolded(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name        string
		bound       bool
		provisioned bool
		status      TaskSetStatus
		want        bool
	}{
		{"managed DONE", true, true, StatusDone, true},
		{"managed AWAITING-APPROVAL", true, true, StatusAwaitingApproval, true},
		{"adopted DONE", true, false, StatusDone, false},
		{"adopted AWAITING-APPROVAL", true, false, StatusAwaitingApproval, false},
		{"unbound DONE", false, false, StatusDone, false},
		{"unbound AWAITING-APPROVAL", false, false, StatusAwaitingApproval, false},
		{"managed READY", true, true, StatusReady, false},
		{"managed BLOCKED", true, true, StatusBlocked, false},
		{"managed NEEDS-VERIFY", true, true, StatusNeedsVerify, false},
		{"managed VERIFY-FAILED", true, true, StatusVerifyFailed, false},
		{"managed FAILED", true, true, StatusFailed, false},
		{"unbound READY", false, false, StatusReady, false},
		// Provisioned without Bound is not a real state; still not unfolded.
		{"provisioned-only DONE", false, true, StatusDone, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := Unfolded(tc.bound, tc.provisioned, tc.status); got != tc.want {
				t.Fatalf("Unfolded(%v, %v, %s) = %v, want %v", tc.bound, tc.provisioned, tc.status, got, tc.want)
			}
		})
	}
}

func TestUnfoldedMatchesFoldEligible(t *testing.T) {
	t.Parallel()
	// Unfolded is the foldable condition named: provisioned binding plus
	// FoldEligibleStatus.
	for _, status := range []TaskSetStatus{
		StatusDone, StatusAwaitingApproval, StatusReady, StatusBlocked,
		StatusNeedsVerify, StatusVerifyFailed, StatusFailed, StatusDeferred,
	} {
		for _, bound := range []bool{true, false} {
			for _, provisioned := range []bool{true, false} {
				want := bound && provisioned && FoldEligibleStatus(status)
				if got := Unfolded(bound, provisioned, status); got != want {
					t.Fatalf("Unfolded(%v, %v, %s) = %v, want foldable %v", bound, provisioned, status, got, want)
				}
			}
		}
	}
}
