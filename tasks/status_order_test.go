package tasks

import "testing"

func ids(rows []Row) []string {
	out := make([]string, 0, len(rows))
	for _, row := range rows {
		out = append(out, row.ID)
	}
	return out
}

func TestOrderStatusRowsPartitionsThenBreaksTiesNewestFirst(t *testing.T) {
	// Registration order is deliberately the reverse of identifier order, so a
	// row that kept the old RegIndex tiebreak shows up immediately.
	rows := orderStatusRows([]Row{
		{ID: "2026-01-01-old-done", Status: StatusDone, RegIndex: 0},
		{ID: "2026-03-01-new-done", Status: StatusDone, RegIndex: 1},
		{ID: "2026-01-02-old-ready", Status: StatusReady, Priority: 10, RegIndex: 2},
		{ID: "2026-04-01-new-ready", Status: StatusReady, Priority: 10, RegIndex: 3},
		{ID: "2026-05-01-low-priority", Status: StatusBlocked, Priority: 0, RegIndex: 4},
		{ID: "2026-02-01-missing", Status: StatusMissing, RegIndex: 5},
	})

	want := []string{
		"2026-02-01-missing",
		"2026-03-01-new-done",
		"2026-01-01-old-done",
		"2026-04-01-new-ready",
		"2026-01-02-old-ready",
		"2026-05-01-low-priority",
	}
	got := ids(rows)
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("order = %v, want %v", got, want)
		}
	}
}

func TestOrderStatusRowsIsStableUnderReapplication(t *testing.T) {
	// verify_status re-applies the ordering after Verify verdicts change
	// statuses; an already-ordered table must not shuffle.
	once := orderStatusRows([]Row{
		{ID: "2026-01-01-a", Status: StatusReady, Priority: 5, RegIndex: 0},
		{ID: "2026-02-01-b", Status: StatusReady, Priority: 5, RegIndex: 1},
		{ID: "2026-03-01-c", Status: StatusDone, RegIndex: 2},
	})
	twice := orderStatusRows(once)
	for i := range once {
		if once[i].ID != twice[i].ID {
			t.Fatalf("re-applied = %v, want %v", ids(twice), ids(once))
		}
	}
}

func TestSelectTaskSetPicksNewerOfEqualPriorityReadySets(t *testing.T) {
	manifest := func(stem string) *Manifest {
		return &Manifest{Stem: stem, Valid: true, Tasks: []Task{
			{ID: "01-a", File: "01-a.md", Type: "AFK", Status: "open"},
		}}
	}
	refresh := &RefreshResult{
		Rows: orderStatusRows([]Row{
			{ID: "2026-01-01-older", Status: StatusReady, Priority: 5, RegIndex: 0},
			{ID: "2026-06-01-newer", Status: StatusReady, Priority: 5, RegIndex: 1},
		}),
		Manifests: map[string]*Manifest{
			"2026-01-01-older": manifest("2026-01-01-older"),
			"2026-06-01-newer": manifest("2026-06-01-newer"),
		},
	}

	id, hitl, err := SelectTaskSet(refresh, "")
	if err != nil {
		t.Fatal(err)
	}
	if hitl {
		t.Fatal("unexpected HITL fallback")
	}
	if id != "2026-06-01-newer" {
		t.Fatalf("picked %s, want 2026-06-01-newer", id)
	}
}

func TestSelectTaskSetKeepsPriorityAboveRecency(t *testing.T) {
	manifest := func(stem string) *Manifest {
		return &Manifest{Stem: stem, Valid: true, Tasks: []Task{
			{ID: "01-a", File: "01-a.md", Type: "AFK", Status: "open"},
		}}
	}
	refresh := &RefreshResult{
		Rows: orderStatusRows([]Row{
			{ID: "2026-01-01-older", Status: StatusReady, Priority: 10, RegIndex: 0},
			{ID: "2026-06-01-newer", Status: StatusReady, Priority: 1, RegIndex: 1},
		}),
		Manifests: map[string]*Manifest{
			"2026-01-01-older": manifest("2026-01-01-older"),
			"2026-06-01-newer": manifest("2026-06-01-newer"),
		},
	}

	id, _, err := SelectTaskSet(refresh, "")
	if err != nil {
		t.Fatal(err)
	}
	if id != "2026-01-01-older" {
		t.Fatalf("picked %s, want 2026-01-01-older", id)
	}
}
