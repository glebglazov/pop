package routine

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/glebglazov/pop/store"
)

// projectDashboardDeps wires routine Deps that can build and drive a dashboard
// from inside a checkout carrying `.pop/routines/`: the Project half reports
// `checkout` as the current worktree, a fake `claude` and claude-only config let
// FireWith run, and a recording tmux backs the fire/preview pane verbs.
func projectDashboardDeps(t *testing.T, dataHome, checkout string) *Deps {
	t.Helper()
	d := projectFireDeps(t, dataHome, checkout, io.Discard)
	d.Tmux = newRecordingTmux(false, "0")
	d.InTmux = func() bool { return true }
	d.Executable = func() (string, error) { return "/mock/bin/pop", nil }
	return d
}

func findRow(t *testing.T, snap DashboardSnapshot, id string) DashboardRow {
	t.Helper()
	for _, row := range snap.Rows {
		if row.ID == id {
			return row
		}
	}
	t.Fatalf("dashboard has no row %q; rows=%+v", id, snap.Rows)
	return DashboardRow{}
}

func TestBuildDashboardIncludesProjectRoutineRows(t *testing.T) {
	root := t.TempDir()
	dataHome := filepath.Join(root, "data")
	checkout := filepath.Join(root, "checkout")
	installFakeClaude(t, root, 0)
	d := projectDashboardDeps(t, dataHome, checkout)
	writeProjectRoutine(t, checkout, "newrelic", "---\nagents:\n  - claude\n---\nResearch NewRelic bugs.\n")

	// Fire once so the row surfaces a real per-checkout run.
	if _, err := FireWith(d, "project:newrelic"); err != nil {
		t.Fatal(err)
	}

	snap, err := BuildDashboardWith(d)
	if err != nil {
		t.Fatal(err)
	}
	row := findRow(t, snap, "project:newrelic")
	if !row.Project {
		t.Fatal("row should be flagged as a Project routine")
	}
	if row.Directory != checkout {
		t.Fatalf("Directory = %q, want checkout %q", row.Directory, checkout)
	}
	// SCHEDULE renders manual; the row carries no schedule and no pause bit.
	if ScheduleLabel(row.Schedule) != "manual" {
		t.Fatalf("schedule label = %q, want manual", ScheduleLabel(row.Schedule))
	}
	if row.Paused {
		t.Fatal("Project routine row must never be paused")
	}
	if row.Status != "ok" {
		t.Fatalf("Status = %q, want ok after a successful run", row.Status)
	}
	if row.LastRun == "never" {
		t.Fatal("row should reflect the last run, not never")
	}
	// LAST RUN / report peek read the per-checkout run state.
	key := checkoutKey(checkout)
	wantStoreID := projectStoreID(key, "newrelic")
	if row.StoreID != wantStoreID {
		t.Fatalf("StoreID = %q, want %q", row.StoreID, wantStoreID)
	}
	wantRunsRoot := filepath.Join(dataHome, "pop", projectRoutinesDataRoot, key, "newrelic")
	if !strings.HasPrefix(row.LastReportPath, wantRunsRoot) {
		t.Fatalf("LastReportPath %q not under per-checkout root %q", row.LastReportPath, wantRunsRoot)
	}
}

func TestBuildDashboardProjectRowFailedNeverPaused(t *testing.T) {
	root := t.TempDir()
	dataHome := filepath.Join(root, "data")
	checkout := filepath.Join(root, "checkout")
	installFakeClaude(t, root, 2) // non-zero exit → failed run
	d := projectDashboardDeps(t, dataHome, checkout)
	writeProjectRoutine(t, checkout, "flaky", "---\n---\nMight fail.\n")

	if _, err := FireWith(d, "project:flaky"); err == nil {
		t.Fatal("expected the failing run to error")
	}
	snap, err := BuildDashboardWith(d)
	if err != nil {
		t.Fatal(err)
	}
	row := findRow(t, snap, "project:flaky")
	if row.Status != "failed" {
		t.Fatalf("Status = %q, want failed", row.Status)
	}
	if strings.HasPrefix(row.Status, "paused") || row.Paused {
		t.Fatal("a failed Project routine run must never render as paused")
	}
}

func TestRoutineDashboardProjectRowBadgedAndStyled(t *testing.T) {
	root := t.TempDir()
	dataHome := filepath.Join(root, "data")
	checkout := filepath.Join(root, "checkout")
	d := projectDashboardDeps(t, dataHome, checkout)
	writeProjectRoutine(t, checkout, "newrelic", "---\n---\nResearch.\n")

	snap, err := BuildDashboardWith(d)
	if err != nil {
		t.Fatal(err)
	}
	m := newRoutineDashboard(d, snap)
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 20})
	m = updated.(RoutineDashboard)
	view := m.View().Content
	if !strings.Contains(view, projectRoutineBadge+" project:newrelic") {
		t.Fatalf("view missing badged project row:\n%s", view)
	}
}

func TestRoutineDashboardProjectMenuVerbs(t *testing.T) {
	items := routineMenuItems(DashboardRow{ID: "project:x", Project: true})
	got := map[routineMenuAction]bool{}
	for _, it := range items {
		got[it.action] = true
	}
	for _, want := range []routineMenuAction{menuActionFire, menuActionPreview, menuActionRuns, menuActionHandoff} {
		if !got[want] {
			t.Fatalf("Project routine menu missing action %v; items=%+v", want, items)
		}
	}
	for _, absent := range []routineMenuAction{menuActionPauseResume, menuActionEditSchedule} {
		if got[absent] {
			t.Fatalf("Project routine menu must not offer action %v; items=%+v", absent, items)
		}
	}
}

func TestRoutineDashboardProjectFireSpawnsProjectRef(t *testing.T) {
	root := t.TempDir()
	dataHome := filepath.Join(root, "data")
	checkout := filepath.Join(root, "checkout")
	d := projectDashboardDeps(t, dataHome, checkout)
	writeProjectRoutine(t, checkout, "newrelic", "---\n---\nResearch.\n")

	snap, err := BuildDashboardWith(d)
	if err != nil {
		t.Fatal(err)
	}
	m := newRoutineDashboard(d, snap)
	row := findRow(t, snap, "project:newrelic")

	cmd := m.fireRoutine(row)
	if cmd == nil {
		t.Fatal("fire verb should schedule a command")
	}
	msg, ok := cmd().(dashboardFireMsg)
	if !ok || msg.err != nil {
		t.Fatalf("fire msg = %#v", cmd())
	}
	rt := tmuxRecorder(d)
	found := false
	for _, c := range rt.commands {
		for _, arg := range c {
			if arg == "pop routine fire project:newrelic" {
				found = true
			}
		}
	}
	if !found {
		t.Fatalf("expected a pane running `pop routine fire project:newrelic`, commands=%v", rt.commands)
	}
}

func TestRoutineDashboardProjectHandoffCopiesProjectPrompt(t *testing.T) {
	root := t.TempDir()
	dataHome := filepath.Join(root, "data")
	checkout := filepath.Join(root, "checkout")
	d := projectDashboardDeps(t, dataHome, checkout)
	writeProjectRoutine(t, checkout, "newrelic", "---\n---\nResearch NewRelic bugs.\n")

	snap, err := BuildDashboardWith(d)
	if err != nil {
		t.Fatal(err)
	}
	row := findRow(t, snap, "project:newrelic")

	var captured string
	m := newRoutineDashboard(d, snap)
	m.copyFunc = func(s string) error { captured = s; return nil }
	if _, _ = m.dispatchMenuAction(menuActionHandoff, row); captured == "" {
		t.Fatal("handoff verb should copy a prompt")
	}
	for _, want := range []string{"project:newrelic", "Research NewRelic bugs.", checkout} {
		if !strings.Contains(captured, want) {
			t.Fatalf("handoff prompt missing %q:\n%s", want, captured)
		}
	}
}

func TestRoutineDashboardProjectRunsVerbReadsPerCheckout(t *testing.T) {
	root := t.TempDir()
	dataHome := filepath.Join(root, "data")
	checkout := filepath.Join(root, "checkout")
	installFakeClaude(t, root, 0)
	d := projectDashboardDeps(t, dataHome, checkout)
	writeProjectRoutine(t, checkout, "audit", "---\n---\nAudit.\n")
	if _, err := FireWith(d, "project:audit"); err != nil {
		t.Fatal(err)
	}

	snap, err := BuildDashboardWith(d)
	if err != nil {
		t.Fatal(err)
	}
	row := findRow(t, snap, "project:audit")
	m := newRoutineDashboard(d, snap)
	msg, ok := m.loadRuns(row)().(dashboardRunsMsg)
	if !ok || msg.err != nil {
		t.Fatalf("runs msg = %#v", m.loadRuns(row)())
	}
	if len(msg.runs) != 1 || msg.runs[0].Outcome != store.RoutineRunSucceeded {
		t.Fatalf("runs = %+v, want one succeeded per-checkout run", msg.runs)
	}
}

func TestBuildDashboardOutsideProjectHasNoProjectRows(t *testing.T) {
	root := t.TempDir()
	dataHome := filepath.Join(root, "data")
	home := filepath.Join(root, "home")
	if err := os.MkdirAll(home, 0o755); err != nil {
		t.Fatal(err)
	}
	// checkout="" simulates being outside any git checkout.
	d := checkoutDeps(t, dataHome, "")
	if _, err := AddWith(d, "authored", "every 6h", home); err != nil {
		t.Fatal(err)
	}

	snap, err := BuildDashboardWith(d)
	if err != nil {
		t.Fatal(err)
	}
	if len(snap.Rows) != 1 {
		t.Fatalf("want only the authored row, got %+v", snap.Rows)
	}
	if snap.Rows[0].Project || strings.HasPrefix(snap.Rows[0].ID, ProjectOrigin) {
		t.Fatalf("outside a checkout the dashboard must carry no Project rows, got %+v", snap.Rows[0])
	}
}
