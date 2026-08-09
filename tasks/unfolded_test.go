package tasks

import "testing"

func TestUnfolded(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name   string
		bound  bool
		status TaskSetStatus
		want   bool
	}{
		{"bound DONE", true, StatusDone, true},
		{"bound AWAITING-APPROVAL", true, StatusAwaitingApproval, true},
		{"unbound DONE", false, StatusDone, false},
		{"unbound AWAITING-APPROVAL", false, StatusAwaitingApproval, false},
		{"bound READY", true, StatusReady, false},
		{"bound BLOCKED", true, StatusBlocked, false},
		{"bound NEEDS-VERIFY", true, StatusNeedsVerify, false},
		{"bound VERIFY-FAILED", true, StatusVerifyFailed, false},
		{"bound FAILED", true, StatusFailed, false},
		{"unbound READY", false, StatusReady, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := Unfolded(tc.bound, tc.status); got != tc.want {
				t.Fatalf("Unfolded(%v, %s) = %v, want %v", tc.bound, tc.status, got, tc.want)
			}
		})
	}
}

func TestUnfoldedMatchesFoldEligible(t *testing.T) {
	t.Parallel()
	// Unfolded is the foldable condition named: bound plus FoldEligibleStatus.
	for _, status := range []TaskSetStatus{
		StatusDone, StatusAwaitingApproval, StatusReady, StatusBlocked,
		StatusNeedsVerify, StatusVerifyFailed, StatusFailed, StatusDeferred,
	} {
		for _, bound := range []bool{true, false} {
			want := bound && FoldEligibleStatus(status)
			if got := Unfolded(bound, status); got != want {
				t.Fatalf("Unfolded(%v, %s) = %v, want foldable %v", bound, status, got, want)
			}
		}
	}
}
