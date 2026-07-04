package tasks

import "testing"

func TestAFKOrdinal(t *testing.T) {
	// Mixed set: HITL tasks are excluded from both position and total, and
	// non-open AFK tasks still occupy a slot so the position is a fixed
	// property of the task rather than a drain-progress counter.
	m := &Manifest{Tasks: []Task{
		{ID: "01-a", Type: "AFK", Status: "done"},
		{ID: "02-h", Type: "HITL", Status: "open"},
		{ID: "03-b", Type: "AFK", Status: "skipped"},
		{ID: "04-c", Type: "AFK", Status: "open"},
		{ID: "05-h", Type: "HITL", Status: "open"},
	}}

	cases := []struct {
		taskID    string
		wantPos   int
		wantTotal int
	}{
		{"01-a", 1, 3},
		{"03-b", 2, 3}, // skipped AFK still counted; HITL 02-h does not shift it
		{"04-c", 3, 3},
		{"02-h", 0, 3}, // HITL has no position
		{"99-x", 0, 3}, // unknown task
	}
	for _, tc := range cases {
		pos, total := afkOrdinal(m, tc.taskID)
		if pos != tc.wantPos || total != tc.wantTotal {
			t.Errorf("afkOrdinal(%q) = (%d/%d), want (%d/%d)", tc.taskID, pos, total, tc.wantPos, tc.wantTotal)
		}
	}

	if pos, total := afkOrdinal(nil, "01-a"); pos != 0 || total != 0 {
		t.Errorf("afkOrdinal(nil) = (%d/%d), want (0/0)", pos, total)
	}
}
