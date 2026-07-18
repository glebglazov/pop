package dashboardshell

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/glebglazov/pop/config"
	"github.com/glebglazov/pop/queue"
	"github.com/glebglazov/pop/routine"
	"github.com/glebglazov/pop/tasks"
)

func queueRows() []queue.DashboardRow {
	return []queue.DashboardRow{
		queue.TestDashboardRow("alpha", "set-a", queue.SetRef{RawStatus: tasks.StatusReady, DefPath: "/a/tasks", StatePath: "/a/state.json"}),
		queue.TestDashboardRow("beta", "set-b", queue.SetRef{RawStatus: tasks.StatusReady, DefPath: "/b/tasks", StatePath: "/b/state.json"}),
		queue.TestDashboardRow("gamma", "set-g", queue.SetRef{RawStatus: tasks.StatusReady, DefPath: "/g/tasks", StatePath: "/g/state.json"}),
	}
}

func routineRows() []routine.DashboardRow {
	return []routine.DashboardRow{
		{ID: "daily", Directory: "/home/daily", Schedule: "daily at 10:00", LastRun: "never", Status: "idle"},
		{ID: "hourly", Directory: "/home/hourly", Schedule: "every 6h", LastRun: "never", Status: "idle"},
	}
}

func newTestShell(start View) Shell {
	return Shell{
		active:  start,
		queue:   queue.NewDashboard(&queue.Deps{}, &config.Config{}, queue.DashboardSnapshot{Rows: queueRows()}),
		routine: routine.NewDashboard(&routine.Deps{}, routine.DashboardSnapshot{Rows: routineRows()}),
	}
}

func applySize(m Shell) Shell {
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 24})
	return updated.(Shell)
}

func TestShellStartsOnQueueEntryView(t *testing.T) {
	s := applySize(newTestShell(ViewQueue))
	if s.ActiveView() != ViewQueue {
		t.Fatalf("active view = %v, want queue", s.ActiveView())
	}
	if !strings.Contains(s.View().Content, "Queue ·") {
		t.Fatalf("expected queue header, got:\n%s", s.View().Content)
	}
}

func TestShellStartsOnRoutineEntryView(t *testing.T) {
	s := applySize(newTestShell(ViewRoutine))
	if s.ActiveView() != ViewRoutine {
		t.Fatalf("active view = %v, want routine", s.ActiveView())
	}
	if !strings.Contains(s.View().Content, "Routines ·") {
		t.Fatalf("expected routines header, got:\n%s", s.View().Content)
	}
}

func TestShellVTogglesBetweenViews(t *testing.T) {
	s := applySize(newTestShell(ViewQueue))
	updated, cmd := s.Update(tea.KeyPressMsg{Code: 'v', Text: "v"})
	if cmd == nil {
		t.Fatal("v should restart active view tick")
	}
	s = updated.(Shell)
	if s.ActiveView() != ViewRoutine {
		t.Fatalf("after v active = %v, want routine", s.ActiveView())
	}
	if !strings.Contains(s.View().Content, "Routines ·") {
		t.Fatalf("expected routines view, got:\n%s", s.View().Content)
	}

	updated, _ = s.Update(tea.KeyPressMsg{Code: 'v', Text: "v"})
	s = updated.(Shell)
	if s.ActiveView() != ViewQueue {
		t.Fatalf("after second v active = %v, want queue", s.ActiveView())
	}
	if !strings.Contains(s.View().Content, "Queue ·") {
		t.Fatalf("expected queue view, got:\n%s", s.View().Content)
	}
}

func TestShellTogglePreservesCursorAndFilter(t *testing.T) {
	s := applySize(newTestShell(ViewQueue))
	q := s.QueueDashboard()
	var qModel tea.Model
	qModel, _ = q.Update(tea.KeyPressMsg{Code: 'j', Text: "j"})
	q = qModel.(queue.QueueDashboard)
	qModel, _ = q.Update(tea.KeyPressMsg{Code: 'j', Text: "j"})
	q = qModel.(queue.QueueDashboard)
	if q.ListCursor() != 2 {
		t.Fatalf("queue cursor = %d, want 2", q.ListCursor())
	}
	s.queue = q

	qModel, _ = q.Update(tea.KeyPressMsg{Code: '/', Text: "/"})
	q = qModel.(queue.QueueDashboard)
	s.queue = q
	if !q.FilterActive() {
		t.Fatal("expected queue filter mode")
	}

	updated, _ := s.Update(tea.KeyPressMsg{Code: 'v', Text: "v"})
	s = updated.(Shell)
	r := s.RoutineDashboard()
	var rModel tea.Model
	rModel, _ = r.Update(tea.KeyPressMsg{Code: 'j', Text: "j"})
	r = rModel.(routine.RoutineDashboard)
	if r.ListCursor() != 1 {
		t.Fatalf("routine cursor = %d, want 1", r.ListCursor())
	}
	s.routine = r

	updated, _ = s.Update(tea.KeyPressMsg{Code: 'v', Text: "v"})
	s = updated.(Shell)
	if s.QueueDashboard().ListCursor() != 2 {
		t.Fatalf("restored queue cursor = %d, want 2", s.QueueDashboard().ListCursor())
	}
	if !s.QueueDashboard().FilterActive() {
		t.Fatal("expected queue filter to persist across toggle")
	}
	if s.RoutineDashboard().ListCursor() != 1 {
		t.Fatalf("restored routine cursor = %d, want 1", s.RoutineDashboard().ListCursor())
	}
}

func TestShellHelpDocumentsViewToggle(t *testing.T) {
	s := applySize(newTestShell(ViewQueue))
	q, _ := s.QueueDashboard().Update(tea.KeyPressMsg{Code: 'h', Mod: tea.ModCtrl})
	s.queue = q.(queue.QueueDashboard)
	view := s.View().Content
	if !strings.Contains(view, "routines view") {
		t.Fatalf("queue help missing v:\n%s", view)
	}

	s = applySize(newTestShell(ViewRoutine))
	r, _ := s.RoutineDashboard().Update(tea.KeyPressMsg{Code: 'h', Mod: tea.ModCtrl})
	s.routine = r.(routine.RoutineDashboard)
	s.active = ViewRoutine
	view = s.View().Content
	if !strings.Contains(view, "queue view") {
		t.Fatalf("routine help missing v:\n%s", view)
	}
}

func TestShellVIgnoredInQueueDetail(t *testing.T) {
	s := applySize(newTestShell(ViewQueue))
	q, cmd := s.QueueDashboard().Update(tea.KeyPressMsg{Code: 'l', Text: "l"})
	if cmd == nil {
		t.Fatal("enter detail should load")
	}
	s.queue = q.(queue.QueueDashboard)

	updated, cmd := s.Update(tea.KeyPressMsg{Code: 'v', Text: "v"})
	if cmd != nil {
		t.Fatal("v should not toggle from queue detail")
	}
	if updated.(Shell).ActiveView() != ViewQueue {
		t.Fatal("should stay on queue view in detail")
	}
}
