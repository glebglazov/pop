package tasks

import "testing"

func TestOpenHITLTasks(t *testing.T) {
	t.Parallel()
	m := &Manifest{
		Valid: true,
		Tasks: []Task{
			{ID: "01-a", Type: "AFK", Status: "done"},
			{ID: "02-gate", Type: "HITL", Status: "open"},
			{ID: "03-wait", Type: "HITL", Status: "open", BlockedBy: []string{"02-gate"}},
			{ID: "04-done", Type: "HITL", Status: "done"},
		},
	}
	got := OpenHITLTasks(m)
	if len(got) != 2 {
		t.Fatalf("OpenHITLTasks len = %d, want 2 (all open HITL, including blocked)", len(got))
	}
	if got[0].ID != "02-gate" || got[1].ID != "03-wait" {
		t.Fatalf("OpenHITLTasks = %#v, want 02-gate and 03-wait", got)
	}
}

func TestFoldSignOffTaskLabel(t *testing.T) {
	t.Parallel()
	cases := []struct {
		task Task
		want string
	}{
		{Task{ID: "09-review", Title: "Review"}, "09-review"},
		{Task{ID: "10-signoff", Title: "Sign off"}, "10-signoff"},
		{Task{ID: "09-review", Title: "Approve the release"}, "09-review — Approve the release"},
		{Task{ID: "gate", Title: ""}, "gate"},
	}
	for _, tc := range cases {
		if got := FoldSignOffTaskLabel(tc.task); got != tc.want {
			t.Errorf("FoldSignOffTaskLabel(%#v) = %q, want %q", tc.task, got, tc.want)
		}
	}
}

func TestFormatFoldSignOffConfirmation(t *testing.T) {
	t.Parallel()
	got := FormatFoldSignOffConfirmation([]Task{
		{ID: "09-review", Title: "Review"},
		{ID: "10-signoff", Title: "Sign off"},
	})
	want := "fold will complete: 09-review, 10-signoff"
	if got != want {
		t.Fatalf("confirmation = %q, want %q", got, want)
	}
}

func TestFoldEligibleStatus(t *testing.T) {
	t.Parallel()
	if !FoldEligibleStatus(StatusDone) || !FoldEligibleStatus(StatusAwaitingApproval) {
		t.Fatal("DONE and AWAITING-APPROVAL should be fold-eligible")
	}
	for _, status := range []TaskSetStatus{StatusReady, StatusBlocked, StatusNeedsVerify, StatusVerifyFailed, StatusFailed} {
		if FoldEligibleStatus(status) {
			t.Fatalf("%s should not be fold-eligible", status)
		}
	}
}
