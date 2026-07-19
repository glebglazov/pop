package routine

import (
	"errors"
	"path/filepath"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/glebglazov/pop/store"
)

// TestBuildDashboardReportPath pins which run's report path lands on a row:
// the latest succeeded run's absolute path, empty for a never-fired routine,
// and empty for a routine whose latest run was skipped (no report written).
func TestBuildDashboardReportPath(t *testing.T) {
	d, home := routineDashboardDeps(t)
	if _, err := AddWith(d, "never-fired", "every 6h", home); err != nil {
		t.Fatal(err)
	}
	if _, err := AddWith(d, "reported", "every 6h", home); err != nil {
		t.Fatal(err)
	}
	if _, err := AddWith(d, "skipped-r", "every 6h", home); err != nil {
		t.Fatal(err)
	}

	s, err := openExecutionStore(d)
	if err != nil {
		t.Fatal(err)
	}
	fired := time.Date(2026, 7, 18, 10, 0, 0, 0, time.UTC)
	reportPath := filepath.Join(routineDir(d, "reported"), runsDirName, "2026-07-18T10-00-00Z.md")
	if _, err := s.StartRoutineRun(store.RoutineRun{
		RoutineID: "reported",
		FiredAt:   fired,
		PID:       1,
		ProcStart: "dead",
	}, func(store.RoutineRun) bool { return false }); err != nil {
		t.Fatal(err)
	}
	if err := s.FinishRoutineRun(1, store.RoutineRunSucceeded, reportPath, "", fired); err != nil {
		t.Fatal(err)
	}
	if _, err := s.InsertSkippedRoutineRun(store.RoutineRun{
		RoutineID:  "skipped-r",
		FiredAt:    fired,
		SkipReason: "checkout busy",
	}); err != nil {
		t.Fatal(err)
	}
	_ = s.Close()

	snap, err := BuildDashboardWith(d)
	if err != nil {
		t.Fatal(err)
	}
	paths := map[string]string{}
	for _, row := range snap.Rows {
		paths[row.ID] = row.LastReportPath
	}
	if paths["reported"] != reportPath {
		t.Fatalf("reported row report path = %q, want %q", paths["reported"], reportPath)
	}
	if paths["never-fired"] != "" {
		t.Fatalf("never-fired row report path = %q, want empty", paths["never-fired"])
	}
	if paths["skipped-r"] != "" {
		t.Fatalf("skipped-r row report path = %q, want empty", paths["skipped-r"])
	}
}

// TestRoutineDashboardCopyMainView covers the `c` verb in the main list: a row
// with a report copies its absolute path through the injected clipboard
// helper and confirms, while a row without a report is a no-op that leaves
// the clipboard helper uncalled and surfaces a status note.
func TestRoutineDashboardCopyMainView(t *testing.T) {
	rows := []DashboardRow{
		{ID: "has-report", Status: "ok", LastReportPath: "/abs/path/report.md"},
		{ID: "no-report", Status: "idle"},
	}
	m := newRoutineDashboard(&Deps{}, DashboardSnapshot{Rows: rows})
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 100, Height: 24})
	m = updated.(RoutineDashboard)

	var captured string
	callCount := 0
	m.copyFunc = func(s string) error {
		callCount++
		captured = s
		return nil
	}

	updated, cmd := m.Update(tea.KeyPressMsg{Code: 'c', Text: "c"})
	if cmd != nil {
		t.Fatal("c should not schedule a command")
	}
	m = updated.(RoutineDashboard)
	if callCount != 1 || captured != "/abs/path/report.md" {
		t.Fatalf("copyFunc called %d times with %q", callCount, captured)
	}
	if m.statusMsg != "copied report path" {
		t.Fatalf("statusMsg = %q, want copied confirmation", m.statusMsg)
	}

	// Move to the no-report row and press c again: no-op, clipboard untouched.
	m.list.MoveDown()
	updated, cmd = m.Update(tea.KeyPressMsg{Code: 'c', Text: "c"})
	if cmd != nil {
		t.Fatal("c should not schedule a command")
	}
	m = updated.(RoutineDashboard)
	if callCount != 1 {
		t.Fatalf("copyFunc should not be called again, count=%d", callCount)
	}
	if m.statusMsg != noReportStatusMsg {
		t.Fatalf("statusMsg = %q, want %q", m.statusMsg, noReportStatusMsg)
	}
}

// TestRoutineDashboardCopyErrorSurfaces confirms a failing clipboard write is
// surfaced in the status line rather than silently swallowed.
func TestRoutineDashboardCopyErrorSurfaces(t *testing.T) {
	rows := []DashboardRow{{ID: "has-report", Status: "ok", LastReportPath: "/abs/path/report.md"}}
	m := newRoutineDashboard(&Deps{}, DashboardSnapshot{Rows: rows})
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 100, Height: 24})
	m = updated.(RoutineDashboard)
	m.copyFunc = func(string) error { return errors.New("boom") }

	updated, _ = m.Update(tea.KeyPressMsg{Code: 'c', Text: "c"})
	m = updated.(RoutineDashboard)
	if m.statusMsg != "copy failed: boom" {
		t.Fatalf("statusMsg = %q, want copy failed message", m.statusMsg)
	}
}

// TestRoutineDashboardCopyRunsDetail covers the `c` verb in the runs detail
// list: the focused run's own report path is copied, and a skipped run (no
// report) is a no-op with a status note on the detail view.
func TestRoutineDashboardCopyRunsDetail(t *testing.T) {
	row := DashboardRow{ID: "beta"}
	m := newRoutineDashboard(&Deps{}, DashboardSnapshot{Rows: []DashboardRow{row}})
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 100, Height: 24})
	m = updated.(RoutineDashboard)

	var captured string
	m.copyFunc = func(s string) error {
		captured = s
		return nil
	}

	m.detail = newRunsDetailView(row)
	m.detail.loading = false
	m.detail.runs = []store.RoutineRun{{ID: 1, ReportPath: "/abs/runs/1.md"}}
	m.detail.list.ReplaceItems(m.detail.runs)

	updated, cmd := m.Update(tea.KeyPressMsg{Code: 'c', Text: "c"})
	if cmd != nil {
		t.Fatal("c should not schedule a command")
	}
	m = updated.(RoutineDashboard)
	if captured != "/abs/runs/1.md" {
		t.Fatalf("captured = %q, want run report path", captured)
	}
	if m.detail.status != "copied report path" {
		t.Fatalf("detail.status = %q, want copied confirmation", m.detail.status)
	}

	// A skipped run has no report path: no-op, clipboard untouched.
	captured = ""
	m.detail.runs = []store.RoutineRun{{ID: 2, Outcome: store.RoutineRunSkipped}}
	m.detail.list.ReplaceItems(m.detail.runs)
	updated, _ = m.Update(tea.KeyPressMsg{Code: 'c', Text: "c"})
	m = updated.(RoutineDashboard)
	if captured != "" {
		t.Fatalf("copyFunc should not be called for a skipped run, captured=%q", captured)
	}
	if m.detail.status != noReportStatusMsg {
		t.Fatalf("detail.status = %q, want %q", m.detail.status, noReportStatusMsg)
	}
}

// TestRoutineDashboardCopyReportPeek covers the `c` verb in the report peek
// overlay: the viewed report's own path is copied and confirmed.
func TestRoutineDashboardCopyReportPeek(t *testing.T) {
	row := DashboardRow{ID: "beta"}
	m := newRoutineDashboard(&Deps{}, DashboardSnapshot{Rows: []DashboardRow{row}})
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 100, Height: 24})
	m = updated.(RoutineDashboard)

	var captured string
	m.copyFunc = func(s string) error {
		captured = s
		return nil
	}

	m.detail = newRunsDetailView(row)
	m.detail.loading = false
	m.detail.peek = &reportPeek{path: "/abs/runs/1.md", text: "hello"}

	updated, cmd := m.Update(tea.KeyPressMsg{Code: 'c', Text: "c"})
	if cmd != nil {
		t.Fatal("c should not schedule a command")
	}
	m = updated.(RoutineDashboard)
	if captured != "/abs/runs/1.md" {
		t.Fatalf("captured = %q, want peek path", captured)
	}
	if m.detail.peek.status != "copied report path" {
		t.Fatalf("peek.status = %q, want copied confirmation", m.detail.peek.status)
	}
}
