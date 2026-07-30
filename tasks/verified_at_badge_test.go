package tasks

import "testing"

func TestDeriveVerifiedAtBadge(t *testing.T) {
	cases := []struct {
		name  string
		row   Row
		want  VerifiedAtBadgeState
		wantSHA string
	}{
		{"green at HEAD", Row{Status: StatusDone, VerifiedAtSHA: "abc123", VerifiedAtDrifted: false}, VerifiedAtAtHead, "abc123"},
		{"yellow drifted", Row{Status: StatusDone, VerifiedAtSHA: "abc123", VerifiedAtDrifted: true}, VerifiedAtDrifted, "abc123"},
		{"red unverified NEEDS-VERIFY", Row{Status: StatusNeedsVerify}, VerifiedAtUnverified, ""},
		{"red unverified VERIFY-FAILED", Row{Status: StatusVerifyFailed}, VerifiedAtUnverified, ""},
		{"absent READY", Row{Status: StatusReady}, VerifiedAtAbsent, ""},
		{"absent DONE no SHA", Row{Status: StatusDone}, VerifiedAtAbsent, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := DeriveVerifiedAtBadge(tc.row)
			if got.State != tc.want {
				t.Fatalf("State = %v, want %v", got.State, tc.want)
			}
			if got.SHA != tc.wantSHA {
				t.Fatalf("SHA = %q, want %q", got.SHA, tc.wantSHA)
			}
		})
	}
}

func TestVerifiedAtBadgeText(t *testing.T) {
	if got := VerifiedAtBadgeText(VerifiedAtBadge{State: VerifiedAtAtHead, SHA: "abc123"}); got != "verified @ abc123" {
		t.Fatalf("at head = %q", got)
	}
	if got := VerifiedAtBadgeText(VerifiedAtBadge{State: VerifiedAtUnverified}); got != "unverified" {
		t.Fatalf("unverified = %q", got)
	}
	if got := VerifiedAtBadgeText(VerifiedAtBadge{State: VerifiedAtAbsent}); got != "" {
		t.Fatalf("absent = %q", got)
	}
}
