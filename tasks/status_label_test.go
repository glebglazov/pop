package tasks

import "testing"

func TestStatusLabelInProgress(t *testing.T) {
	cases := []struct {
		name    string
		status  TaskSetStatus
		started bool
		want    string
	}{
		{"ready started is in progress", StatusReady, true, "IN PROGRESS"},
		{"ready fresh is ready", StatusReady, false, "READY"},
		{"blocked never relabels", StatusBlocked, true, "BLOCKED"},
		{"awaiting-approval never relabels", StatusAwaitingApproval, true, "AWAITING-APPROVAL"},
		{"done never relabels", StatusDone, true, "DONE"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := StatusLabel(Row{Status: tc.status, Started: tc.started})
			if got != tc.want {
				t.Fatalf("StatusLabel(%s, started=%v) = %q, want %q", tc.status, tc.started, got, tc.want)
			}
		})
	}
}

// anyDone drives Row.Started: only a done task marks a set as started, so a
// started Ready set renders IN PROGRESS while a fresh one stays READY.
func TestStatusLabelStartedDerivation(t *testing.T) {
	fresh := &Manifest{Valid: true, Tasks: []Task{
		{ID: "01-a", Type: "AFK", Status: "open"},
		{ID: "02-b", Type: "AFK", Status: "open"},
	}}
	if anyDone(fresh) {
		t.Fatalf("fresh set should not be started")
	}

	started := &Manifest{Valid: true, Tasks: []Task{
		{ID: "01-a", Type: "AFK", Status: "done"},
		{ID: "02-b", Type: "AFK", Status: "open"},
	}}
	if !anyDone(started) {
		t.Fatalf("set with a done task should be started")
	}
	if DeriveStatus(started) != StatusReady {
		t.Fatalf("a started set with an eligible task is still READY")
	}
	if got := StatusLabel(Row{Status: DeriveStatus(started), Started: anyDone(started)}); got != "IN PROGRESS" {
		t.Fatalf("label = %q, want IN PROGRESS", got)
	}
}
