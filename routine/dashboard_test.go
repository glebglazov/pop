package routine

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/glebglazov/pop/config"
	"github.com/glebglazov/pop/internal/deps"
	"github.com/glebglazov/pop/store"
	"github.com/glebglazov/pop/tasks"
)

func routineDashboardDeps(t *testing.T) (*Deps, string) {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("XDG_DATA_HOME", dir)
	real := deps.NewRealFileSystem()
	td := tasks.DefaultDeps()
	td.FS = &deps.MockFileSystem{
		GetenvFunc: func(key string) string {
			if key == "XDG_DATA_HOME" {
				return dir
			}
			return ""
		},
		ReadFileFunc:  real.ReadFile,
		WriteFileFunc: real.WriteFile,
		MkdirAllFunc:  real.MkdirAll,
		RenameFunc:    real.Rename,
		RemoveAllFunc: real.RemoveAll,
		StatFunc:      real.Stat,
		ReadDirFunc:   real.ReadDir,
		UserHomeDirFunc: func() (string, error) {
			return filepath.Join(dir, "home"), nil
		},
	}
	home := filepath.Join(dir, "home")
	if err := os.MkdirAll(home, 0o755); err != nil {
		t.Fatal(err)
	}
	d := &Deps{
		FS:            td.FS,
		Now:           func() time.Time { return time.Date(2026, 7, 18, 12, 0, 0, 0, time.UTC) },
		LoadConfig:    func() (*config.Config, error) { return &config.Config{}, nil },
		Tasks:         td,
		Tmux:          newRecordingTmux(false, "0"),
		IsInteractive: func() bool { return false },
		PID:           func() int { return 4242 },
		ProcStartToken: func(pid int) (string, bool) { return "test", true },
		ProcessAlive:  func(pid int, procStart string) bool { return pid == 9999 },
	}
	return d, home
}

type recordingTmux struct {
	deps.MockTmux
	commands [][]string
}

func newRecordingTmux(hasSession bool, listOut string) deps.Tmux {
	rt := &recordingTmux{}
	rt.HasSessionFunc = func(name string) bool { return hasSession }
	rt.NewSessionFunc = func(name, dir string) error {
		rt.commands = append(rt.commands, []string{"new-session", name, dir})
		return nil
	}
	rt.CommandFunc = func(args ...string) (string, error) {
		rt.commands = append(rt.commands, append([]string(nil), args...))
		if len(args) > 0 && args[0] == "list-windows" {
			return listOut, nil
		}
		if len(args) > 0 && args[0] == "list-panes" {
			return "", nil
		}
		if len(args) > 0 && args[0] == "new-window" {
			return "%42", nil
		}
		if len(args) > 0 && args[0] == "split-window" {
			return "%43", nil
		}
		return "", nil
	}
	return rt
}

func tmuxRecorder(d *Deps) *recordingTmux {
	return d.Tmux.(*recordingTmux)
}

func TestBuildDashboardStatuses(t *testing.T) {
	d, home := routineDashboardDeps(t)
	if _, err := AddWith(d, "idle-r", "every 6h", home); err != nil {
		t.Fatal(err)
	}
	if _, err := AddWith(d, "paused-r", "every 6h", home); err != nil {
		t.Fatal(err)
	}
	if _, err := PauseWith(d, "paused-r"); err != nil {
		t.Fatal(err)
	}
	if _, err := AddWith(d, "running-r", "every 6h", home); err != nil {
		t.Fatal(err)
	}

	s, err := openExecutionStore(d)
	if err != nil {
		t.Fatal(err)
	}
	now := d.Now()
	if _, err := s.StartRoutineRun(store.RoutineRun{
		RoutineID: "running-r",
		FiredAt:   now,
		PID:       9999,
		ProcStart: "test",
	}, func(store.RoutineRun) bool { return true }); err != nil {
		t.Fatal(err)
	}
	if _, err := s.StartRoutineRun(store.RoutineRun{
		RoutineID: "idle-r",
		FiredAt:   now.Add(-2 * time.Hour),
		PID:       1,
		ProcStart: "dead",
	}, func(store.RoutineRun) bool { return false }); err != nil {
		t.Fatal(err)
	}
	if err := s.FinishRoutineRun(2, store.RoutineRunSucceeded, "", "", now.Add(-2*time.Hour)); err != nil {
		t.Fatal(err)
	}
	_ = s.Close()

	snap, err := BuildDashboardWith(d)
	if err != nil {
		t.Fatal(err)
	}
	status := map[string]string{}
	lastRun := map[string]string{}
	for _, row := range snap.Rows {
		status[row.ID] = row.Status
		lastRun[row.ID] = row.LastRun
	}
	if status["idle-r"] != "idle" {
		t.Fatalf("idle-r status = %q", status["idle-r"])
	}
	if status["paused-r"] != "paused" {
		t.Fatalf("paused-r status = %q", status["paused-r"])
	}
	if status["running-r"] != "running" {
		t.Fatalf("running-r status = %q", status["running-r"])
	}
	if lastRun["idle-r"] == "never" {
		t.Fatal("idle-r should have last run")
	}
}

func TestRoutineDashboardRendersColumns(t *testing.T) {
	rows := []DashboardRow{{
		ID:        "daily",
		Directory: "/home/user/proj",
		Schedule:  "daily at 10:00",
		LastRun:   "2026-07-18 09:00",
		Status:    "idle",
	}}
	m := newRoutineDashboard(&Deps{}, DashboardSnapshot{Rows: rows})
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 20})
	m = updated.(RoutineDashboard)
	view := m.View().Content
	for _, col := range []string{"ROUTINE", "DIRECTORY", "SCHEDULE", "LAST RUN", "STATUS"} {
		if !strings.Contains(view, col) {
			t.Fatalf("view missing column %q:\n%s", col, view)
		}
	}
	if !strings.Contains(view, "daily") || !strings.Contains(view, "idle") {
		t.Fatalf("view missing row data:\n%s", view)
	}
}

func TestRoutineDashboardFirePausePreviewKeys(t *testing.T) {
	d, home := routineDashboardDeps(t)
	if _, err := AddWith(d, "alpha", "every 6h", home); err != nil {
		t.Fatal(err)
	}
	snap, err := BuildDashboardWith(d)
	if err != nil {
		t.Fatal(err)
	}
	m := newRoutineDashboard(d, snap)
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 100, Height: 20})
	m = updated.(RoutineDashboard)

	updated, cmd := m.Update(tea.KeyPressMsg{Code: 'i', Text: "i"})
	if cmd == nil {
		t.Fatal("i should schedule fire command")
	}
	msg := cmd()
	fire, ok := msg.(dashboardFireMsg)
	if !ok || fire.err != nil {
		t.Fatalf("fire msg = %#v", msg)
	}
	rt := tmuxRecorder(d)
	if !tmuxHasCommand(rt, "send-keys") {
		t.Fatalf("expected tmux send-keys after fire, commands=%v", rt.commands)
	}

	updated, cmd = updated.(RoutineDashboard).Update(tea.KeyPressMsg{Code: 'a', Text: "a"})
	if cmd == nil {
		t.Fatal("a should schedule pause toggle")
	}
	msg = cmd()
	if toggle, ok := msg.(dashboardTogglePauseMsg); !ok || toggle.err != nil {
		t.Fatalf("toggle msg = %#v", msg)
	}
	r, err := loadManifest(d, "alpha")
	if err != nil || !r.Manifest.Paused {
		t.Fatalf("alpha should be paused after toggle: %v paused=%v", err, r != nil && r.Manifest.Paused)
	}

	updated, cmd = updated.(RoutineDashboard).Update(tea.KeyPressMsg{Code: 'p', Text: "p"})
	if cmd == nil {
		t.Fatal("p should schedule preview")
	}
	msg = cmd()
	if preview, ok := msg.(dashboardPreviewMsg); !ok || preview.err != nil {
		t.Fatalf("preview msg = %#v", msg)
	}
}

func TestRoutineDashboardRunsDetailAndReport(t *testing.T) {
	d, home := routineDashboardDeps(t)
	if _, err := AddWith(d, "beta", "every 6h", home); err != nil {
		t.Fatal(err)
	}
	reportPath := filepath.Join(routineDir(d, "beta"), runsDirName, "2026-07-18T10-00-00Z.md")
	if err := d.FS.MkdirAll(filepath.Dir(reportPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := d.FS.WriteFile(reportPath, []byte("# report body\nline two\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	s, err := openExecutionStore(d)
	if err != nil {
		t.Fatal(err)
	}
	fired := time.Date(2026, 7, 18, 10, 0, 0, 0, time.UTC)
	if _, err := s.StartRoutineRun(store.RoutineRun{
		RoutineID: "beta",
		FiredAt:   fired,
		PID:       1,
		ProcStart: "dead",
	}, func(store.RoutineRun) bool { return false }); err != nil {
		t.Fatal(err)
	}
	if err := s.FinishRoutineRun(1, store.RoutineRunSucceeded, reportPath, "", fired); err != nil {
		t.Fatal(err)
	}
	_ = s.Close()

	snap, err := BuildDashboardWith(d)
	if err != nil {
		t.Fatal(err)
	}
	m := newRoutineDashboard(d, snap)
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 100, Height: 24})
	m = updated.(RoutineDashboard)

	updated, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("enter should load runs")
	}
	msg := cmd()
	runsMsg, ok := msg.(dashboardRunsMsg)
	if !ok || runsMsg.err != nil || len(runsMsg.runs) != 1 {
		t.Fatalf("runs msg = %#v", msg)
	}
	updated, _ = updated.(RoutineDashboard).Update(runsMsg)
	got := updated.(RoutineDashboard)
	if got.detail == nil || len(got.detail.runs) != 1 {
		t.Fatal("detail should have one run")
	}

	updated, cmd = got.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("enter in detail should load report")
	}
	msg = cmd()
	reportMsg, ok := msg.(dashboardReportMsg)
	if !ok || reportMsg.err != nil {
		t.Fatalf("report msg = %#v", msg)
	}
	updated, _ = updated.(RoutineDashboard).Update(reportMsg)
	view := updated.(RoutineDashboard).View().Content
	if !strings.Contains(view, "report body") {
		t.Fatalf("report peek missing body:\n%s", view)
	}
}

func TestRoutineDashboardHelpOverlay(t *testing.T) {
	m := newRoutineDashboard(&Deps{}, DashboardSnapshot{Rows: []DashboardRow{{ID: "x", Status: "idle"}}})
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 80, Height: 20})
	got := updated.(RoutineDashboard)
	updated, _ = got.Update(tea.KeyPressMsg{Code: 'h', Mod: tea.ModCtrl})
	got = updated.(RoutineDashboard)
	if !got.showHelp {
		t.Fatal("C-h should open help")
	}
	view := got.View().Content
	for _, key := range []string{"i", "a", "p", "l/enter", "v", "C-h"} {
		if !strings.Contains(view, key) {
			t.Fatalf("help missing %q:\n%s", key, view)
		}
	}
}

func TestRoutineDashboardNarrowPaneColumnShrink(t *testing.T) {
	row := DashboardRow{
		ID:        "long-routine-id",
		Directory: "/very/long/path/to/some/project/directory",
		Schedule:  "every 6 hours on weekdays",
		LastRun:   "2026-07-18 09:00",
		Status:    "idle",
	}
	for _, termW := range []int{40, 60} {
		t.Run(fmt.Sprintf("width=%d", termW), func(t *testing.T) {
			m := newRoutineDashboard(&Deps{}, DashboardSnapshot{Rows: []DashboardRow{row}})
			updated, _ := m.Update(tea.WindowSizeMsg{Width: termW, Height: 20})
			m = updated.(RoutineDashboard)
			for _, line := range routineDashboardTableLines(m.View().Content) {
				if got := lipgloss.Width(line); got > termW {
					t.Fatalf("line width %d exceeds terminal %d:\n%q", got, termW, line)
				}
			}
		})
	}
}

func routineDashboardTableLines(view string) []string {
	var lines []string
	inTable := false
	for _, line := range strings.Split(view, "\n") {
		if strings.Contains(line, "ROUTINE") && strings.Contains(line, "STATUS") {
			inTable = true
		}
		if !inTable {
			continue
		}
		if strings.Contains(line, "h/esc quit") {
			break
		}
		lines = append(lines, line)
	}
	return lines
}

func tmuxHasCommand(rt *recordingTmux, name string) bool {
	for _, cmd := range rt.commands {
		if len(cmd) > 0 && cmd[0] == name {
			return true
		}
	}
	return false
}
