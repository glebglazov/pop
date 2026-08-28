package dashboard

import (
	"testing"

	"github.com/glebglazov/pop/tasks"
)

// TestReload_StaleResultIsDropped drives the overlap ADR-0242 decision 1 is about:
// a poll-tick rebuild that started before a write finishes after the post-write
// rebuild. The older result must not put pre-write rows back on screen.
func TestReload_StaleResultIsDropped(t *testing.T) {
	m := filterTestModel()

	preWrite := []DashboardRow{
		{Project: "alpha", CursorKey: "alpha\x00set-one", RawStatus: tasks.StatusReady, ID: "set-one"},
	}
	postWrite := []DashboardRow{
		{Project: "alpha", CursorKey: "alpha\x00set-one", RawStatus: tasks.StatusBlocked, ID: "set-one"},
	}

	// The write's own reload lands first, then the tick reload that started before it.
	updated, _ := m.Update(dashboardRowsMsg{snap: DashboardSnapshot{Containers: postWrite}, seq: 7})
	m = updated.(QueueDashboard)
	updated, _ = m.Update(dashboardRowsMsg{snap: DashboardSnapshot{Containers: preWrite}, seq: 6})
	m = updated.(QueueDashboard)

	if got := m.allRows[0].RawStatus; got != tasks.StatusBlocked {
		t.Fatalf("allRows status = %v, want the post-write %v: the stale reload was applied", got, tasks.StatusBlocked)
	}
	if got := m.snap.Containers[0].RawStatus; got != tasks.StatusBlocked {
		t.Fatalf("snap status = %v, want the post-write %v", got, tasks.StatusBlocked)
	}

	// A newer result still lands, so the guard drops only what is stale.
	newer := []DashboardRow{
		{Project: "alpha", CursorKey: "alpha\x00set-one", RawStatus: tasks.StatusFailed, ID: "set-one"},
	}
	updated, _ = m.Update(dashboardRowsMsg{snap: DashboardSnapshot{Containers: newer}, seq: 8})
	m = updated.(QueueDashboard)
	if got := m.allRows[0].RawStatus; got != tasks.StatusFailed {
		t.Fatalf("allRows status = %v, want the newer %v", got, tasks.StatusFailed)
	}
}

// TestReload_StampsRise asserts the stamp is taken when the reload is asked for,
// off a counter shared by every copy of the model — the property the guard needs
// to tell two overlapping reloads apart.
func TestReload_StampsRise(t *testing.T) {
	d, cfg, _, _ := taskSetPaneFixture(t)
	m := openFromPane(t, d, cfg)

	first, ok := m.reload()().(dashboardRowsMsg)
	if !ok {
		t.Fatal("reload did not produce a rows message")
	}
	second, ok := m.reload()().(dashboardRowsMsg)
	if !ok {
		t.Fatal("reload did not produce a rows message")
	}
	if first.seq == 0 {
		t.Fatal("a reload from a built model must be stamped")
	}
	if second.seq <= first.seq {
		t.Fatalf("stamps = %d then %d, want the second to be newer", first.seq, second.seq)
	}

	// The older of the two lands last and must be dropped, even though both were
	// built by the model itself.
	updated, _ := m.Update(second)
	m = updated.(QueueDashboard)
	m.allRows = nil
	updated, _ = m.Update(first)
	if got := updated.(QueueDashboard).allRows; got != nil {
		t.Fatalf("the older reload was applied: allRows = %d rows, want it left untouched", len(got))
	}
}
