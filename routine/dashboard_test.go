package routine

import (
	"bytes"
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
	// idle-r and running-r are armed; only paused-r stays paused.
	if _, err := ResumeWith(d, "idle-r"); err != nil {
		t.Fatal(err)
	}
	if _, err := ResumeWith(d, "running-r"); err != nil {
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
	// idle-r has a succeeded last run, so its STATUS now surfaces the outcome.
	if status["idle-r"] != "ok" {
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
	// Arm alpha so the pause-toggle verb pauses it (routines are created paused).
	if _, err := ResumeWith(d, "alpha"); err != nil {
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
	if cmd != nil {
		t.Fatal("a should open the action menu, not schedule a command")
	}
	if updated.(RoutineDashboard).menu == nil {
		t.Fatal("a should open the action menu")
	}
	updated, cmd = updated.(RoutineDashboard).Update(tea.KeyPressMsg{Code: 'a', Text: "a"})
	if cmd == nil {
		t.Fatal("a in menu should schedule pause toggle")
	}
	msg = cmd()
	if toggle, ok := msg.(dashboardTogglePauseMsg); !ok || toggle.err != nil {
		t.Fatalf("toggle msg = %#v", msg)
	}
	if updated.(RoutineDashboard).menu != nil {
		t.Fatal("dispatching a menu verb should close the menu")
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

func TestRoutineDashboardActionMenuOpenClose(t *testing.T) {
	m := newRoutineDashboard(&Deps{}, DashboardSnapshot{Rows: []DashboardRow{{ID: "gamma", Status: "idle"}}})
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 100, Height: 24})
	m = updated.(RoutineDashboard)

	updated, cmd := m.Update(tea.KeyPressMsg{Code: 'a', Text: "a"})
	m = updated.(RoutineDashboard)
	if cmd != nil {
		t.Fatal("opening the menu should not schedule a command")
	}
	if m.menu == nil {
		t.Fatal("a should open the action menu")
	}
	view := m.View().Content
	for _, verb := range []string{"fire now", "pause", "preview pane", "edit prompt", "runs"} {
		if !strings.Contains(view, verb) {
			t.Fatalf("menu view missing verb %q:\n%s", verb, view)
		}
	}

	updated, cmd = m.Update(tea.KeyPressMsg{Code: tea.KeyEsc})
	m = updated.(RoutineDashboard)
	if cmd != nil {
		t.Fatal("esc should close the menu without acting")
	}
	if m.menu != nil {
		t.Fatal("esc should close the action menu")
	}
}

func TestRoutineDashboardActionMenuPauseLabel(t *testing.T) {
	items := routineMenuItems(DashboardRow{ID: "x"})
	if items[1].label != "pause" {
		t.Fatalf("unpaused row menu label = %q, want pause", items[1].label)
	}
	pausedItems := routineMenuItems(DashboardRow{ID: "x", Paused: true})
	if pausedItems[1].label != "resume" {
		t.Fatalf("paused row menu label = %q, want resume", pausedItems[1].label)
	}

	m := newRoutineDashboard(&Deps{}, DashboardSnapshot{Rows: []DashboardRow{{ID: "p", Status: "paused", Paused: true}}})
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 100, Height: 24})
	m = updated.(RoutineDashboard)
	updated, _ = m.Update(tea.KeyPressMsg{Code: 'a', Text: "a"})
	view := updated.(RoutineDashboard).View().Content
	if !strings.Contains(view, "resume") {
		t.Fatalf("paused-row menu should show resume:\n%s", view)
	}
}

func TestRoutineDashboardActionMenuVerbDispatch(t *testing.T) {
	d, home := routineDashboardDeps(t)
	if _, err := AddWith(d, "delta", "every 6h", home); err != nil {
		t.Fatal(err)
	}
	snap, err := BuildDashboardWith(d)
	if err != nil {
		t.Fatal(err)
	}
	m := newRoutineDashboard(d, snap)
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 100, Height: 24})
	m = updated.(RoutineDashboard)

	// fire via the menu's "i" verb
	updated, _ = m.Update(tea.KeyPressMsg{Code: 'a', Text: "a"})
	updated, cmd := updated.(RoutineDashboard).Update(tea.KeyPressMsg{Code: 'i', Text: "i"})
	if cmd == nil {
		t.Fatal("i in menu should schedule fire")
	}
	if updated.(RoutineDashboard).menu != nil {
		t.Fatal("dispatching fire should close the menu")
	}
	if fire, ok := cmd().(dashboardFireMsg); !ok || fire.err != nil {
		t.Fatalf("fire msg = %#v", cmd())
	}

	// runs via the menu's "l" verb opens the detail view
	m = updated.(RoutineDashboard)
	updated, _ = m.Update(tea.KeyPressMsg{Code: 'a', Text: "a"})
	updated, cmd = updated.(RoutineDashboard).Update(tea.KeyPressMsg{Code: 'l', Text: "l"})
	got := updated.(RoutineDashboard)
	if got.menu != nil {
		t.Fatal("dispatching runs should close the menu")
	}
	if got.detail == nil {
		t.Fatal("l in menu should open the runs detail view")
	}
	if cmd == nil {
		t.Fatal("l in menu should schedule loadRuns")
	}
}

func TestRoutineDashboardActionMenuRefine(t *testing.T) {
	d, home := routineDashboardDeps(t)
	if _, err := AddWith(d, "eta", "every 6h", home); err != nil {
		t.Fatal(err)
	}
	snap, err := BuildDashboardWith(d)
	if err != nil {
		t.Fatal(err)
	}
	m := newRoutineDashboard(d, snap)
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 100, Height: 24})
	m = updated.(RoutineDashboard)

	updated, _ = m.Update(tea.KeyPressMsg{Code: 'a', Text: "a"})
	m = updated.(RoutineDashboard)
	if !strings.Contains(m.View().Content, "refine") {
		t.Fatalf("action menu missing refine verb:\n%s", m.View().Content)
	}

	updated, cmd := m.Update(tea.KeyPressMsg{Code: 'r', Text: "r"})
	m = updated.(RoutineDashboard)
	if cmd == nil {
		t.Fatal("r in menu should suspend into the refinement gate")
	}
	if m.menu != nil {
		t.Fatal("dispatching refine should close the menu")
	}
}

// TestRoutineDashboardRefineExecEntersGate drives the tea.ExecCommand wrapper
// directly with scripted stdin, proving selecting refine enters the gate for the
// row and a resume inside it is persisted (created-paused routine unpauses).
func TestRoutineDashboardRefineExecEntersGate(t *testing.T) {
	d, home := routineDashboardDeps(t)
	if _, err := AddWith(d, "zeta", "every 6h", home); err != nil {
		t.Fatal(err)
	}
	r, err := loadManifest(d, "zeta")
	if err != nil || !r.Manifest.Paused {
		t.Fatalf("routine should start paused: %v paused=%v", err, r != nil && r.Manifest.Paused)
	}

	var out bytes.Buffer
	ex := &refineExec{d: d, id: "zeta"}
	ex.SetStdin(strings.NewReader("6\n"))
	ex.SetStdout(&out)
	ex.SetStderr(&out)
	if err := ex.Run(); err != nil {
		t.Fatalf("refine gate run: %v", err)
	}
	if !strings.Contains(out.String(), "Refine routine") {
		t.Fatalf("gate did not render its menu:\n%s", out.String())
	}
	r, err = loadManifest(d, "zeta")
	if err != nil {
		t.Fatal(err)
	}
	if r.Manifest.Paused {
		t.Fatal("resume inside the gate should unpause the routine")
	}
	// The dashboard's own deps are left untouched by the gate session.
	if d.IsInteractive == nil || d.IsInteractive() {
		t.Fatal("refineExec must not mutate the dashboard's deps")
	}
}

// TestRoutineDashboardRefineReturnReloads proves returning from the gate lands
// back in the dashboard with rows refreshed: a resume applied while the gate was
// open is reflected after the return message reloads.
func TestRoutineDashboardRefineReturnReloads(t *testing.T) {
	d, home := routineDashboardDeps(t)
	if _, err := AddWith(d, "theta", "every 6h", home); err != nil {
		t.Fatal(err)
	}
	snap, err := BuildDashboardWith(d)
	if err != nil {
		t.Fatal(err)
	}
	m := newRoutineDashboard(d, snap)
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 100, Height: 24})
	m = updated.(RoutineDashboard)
	if m.snap.Rows[0].Status != "paused" {
		t.Fatalf("row should start paused, got %q", m.snap.Rows[0].Status)
	}

	// Simulate a resume that happened inside the suspended gate.
	if _, err := ResumeWith(d, "theta"); err != nil {
		t.Fatal(err)
	}

	updated, cmd := m.Update(dashboardRefineMsg{id: "theta"})
	m = updated.(RoutineDashboard)
	if m.statusMsg != "refined theta" {
		t.Fatalf("status = %q, want refined theta", m.statusMsg)
	}
	if cmd == nil {
		t.Fatal("return from the gate should schedule a reload")
	}
	msg := cmd()
	rows, ok := msg.(dashboardRowsMsg)
	if !ok || rows.err != nil {
		t.Fatalf("reload msg = %#v", msg)
	}
	updated, _ = m.Update(rows)
	m = updated.(RoutineDashboard)
	if got := m.snap.Rows[0].Status; got != "idle" {
		t.Fatalf("row status after reload = %q, want idle (resume reflected)", got)
	}
}

func TestRoutineEditPromptCommand(t *testing.T) {
	d, home := routineDashboardDeps(t)
	if _, err := AddWith(d, "epsilon", "every 6h", home); err != nil {
		t.Fatal(err)
	}
	t.Setenv("EDITOR", "my-editor")
	cmd := editPromptCommand(d, "epsilon")
	wantPath := filepath.Join(routineDir(d, "epsilon"), promptFileName)
	if len(cmd.Args) != 2 {
		t.Fatalf("cmd args = %v, want editor + path", cmd.Args)
	}
	if !strings.HasSuffix(cmd.Args[0], "my-editor") && cmd.Args[0] != "my-editor" {
		t.Fatalf("cmd editor = %q, want my-editor", cmd.Args[0])
	}
	if cmd.Args[1] != wantPath {
		t.Fatalf("cmd path = %q, want %q", cmd.Args[1], wantPath)
	}

	t.Setenv("EDITOR", "")
	fallback := editPromptCommand(d, "epsilon")
	if !strings.HasSuffix(fallback.Args[0], "vi") && fallback.Args[0] != "vi" {
		t.Fatalf("fallback editor = %q, want vi", fallback.Args[0])
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

// typeSchedule feeds each rune of s to the model as a printable key press,
// carrying the rune in Text so spaces (e.g. "daily at 09:00") land in the
// working expression.
func typeSchedule(t *testing.T, m RoutineDashboard, s string) RoutineDashboard {
	t.Helper()
	for _, r := range s {
		updated, _ := m.Update(tea.KeyPressMsg{Code: r, Text: string(r)})
		m = updated.(RoutineDashboard)
	}
	return m
}

// clearScheduleInput backspaces the working expression down to empty.
func clearScheduleInput(t *testing.T, m RoutineDashboard) RoutineDashboard {
	t.Helper()
	for m.sched != nil && len(m.sched.input) > 0 {
		updated, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyBackspace})
		m = updated.(RoutineDashboard)
	}
	return m
}

func openScheduleModal(t *testing.T, d *Deps) RoutineDashboard {
	t.Helper()
	snap, err := BuildDashboardWith(d)
	if err != nil {
		t.Fatal(err)
	}
	m := newRoutineDashboard(d, snap)
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 100, Height: 24})
	m = updated.(RoutineDashboard)
	// a opens the action menu; s selects the edit-schedule verb.
	updated, _ = m.Update(tea.KeyPressMsg{Code: 'a', Text: "a"})
	updated, cmd := updated.(RoutineDashboard).Update(tea.KeyPressMsg{Code: 's', Text: "s"})
	if cmd != nil {
		t.Fatal("opening the schedule modal should not schedule a command")
	}
	m = updated.(RoutineDashboard)
	if m.sched == nil {
		t.Fatal("s in menu should open the edit-schedule modal")
	}
	if m.menu != nil {
		t.Fatal("opening the schedule modal should close the action menu")
	}
	return m
}

func TestRoutineDashboardEditSchedulePrefill(t *testing.T) {
	d, home := routineDashboardDeps(t)
	if _, err := AddWith(d, "alpha", "every 6h", home); err != nil {
		t.Fatal(err)
	}
	m := openScheduleModal(t, d)
	if m.sched.input != "every 6h" {
		t.Fatalf("modal pre-fill = %q, want %q", m.sched.input, "every 6h")
	}
	view := m.View().Content
	if !strings.Contains(view, "edit schedule") || !strings.Contains(view, "schedule: every 6h") {
		t.Fatalf("modal view missing pre-fill:\n%s", view)
	}
}

func TestRoutineDashboardEditScheduleValidWrite(t *testing.T) {
	d, home := routineDashboardDeps(t)
	if _, err := AddWith(d, "alpha", "every 6h", home); err != nil {
		t.Fatal(err)
	}
	m := openScheduleModal(t, d)
	m = clearScheduleInput(t, m)
	m = typeSchedule(t, m, "daily at 09:00")

	updated, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	m = updated.(RoutineDashboard)
	if m.sched != nil {
		t.Fatal("valid enter should close the modal")
	}
	if cmd == nil {
		t.Fatal("valid enter should schedule a reload")
	}
	if !strings.Contains(m.statusMsg, "alpha") || !strings.Contains(m.statusMsg, "daily at 09:00") {
		t.Fatalf("status message = %q", m.statusMsg)
	}
	r, err := loadManifest(d, "alpha")
	if err != nil {
		t.Fatal(err)
	}
	if r.Manifest.Schedule != "daily at 09:00" {
		t.Fatalf("manifest schedule = %q, want %q", r.Manifest.Schedule, "daily at 09:00")
	}

	// The reload refreshes the row's SCHEDULE column.
	msg := cmd()
	rows, ok := msg.(dashboardRowsMsg)
	if !ok || rows.err != nil {
		t.Fatalf("reload msg = %#v", msg)
	}
	updated, _ = m.Update(rows)
	m = updated.(RoutineDashboard)
	if got := m.snap.Rows[0].Schedule; got != "daily at 09:00" {
		t.Fatalf("row schedule after reload = %q, want %q", got, "daily at 09:00")
	}
}

func TestRoutineDashboardEditScheduleInvalidReedit(t *testing.T) {
	d, home := routineDashboardDeps(t)
	if _, err := AddWith(d, "alpha", "every 6h", home); err != nil {
		t.Fatal(err)
	}
	m := openScheduleModal(t, d)
	m = clearScheduleInput(t, m)
	m = typeSchedule(t, m, "every 0h")

	updated, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	m = updated.(RoutineDashboard)
	if m.sched == nil {
		t.Fatal("invalid enter should keep the modal open")
	}
	if cmd != nil {
		t.Fatal("invalid enter should not schedule a reload")
	}
	if m.sched.err == nil {
		t.Fatal("invalid enter should record a parse error")
	}
	if !strings.Contains(m.View().Content, "error:") {
		t.Fatalf("modal view should show the inline error:\n%s", m.View().Content)
	}
	// The manifest is untouched by the rejected expression.
	r, err := loadManifest(d, "alpha")
	if err != nil {
		t.Fatal(err)
	}
	if r.Manifest.Schedule != "every 6h" {
		t.Fatalf("manifest schedule after invalid attempt = %q, want unchanged %q", r.Manifest.Schedule, "every 6h")
	}

	// The user corrects the expression and retries.
	m = clearScheduleInput(t, m)
	m = typeSchedule(t, m, "every 12h")
	updated, cmd = m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	m = updated.(RoutineDashboard)
	if m.sched != nil {
		t.Fatal("corrected enter should close the modal")
	}
	if cmd == nil {
		t.Fatal("corrected enter should schedule a reload")
	}
	r, err = loadManifest(d, "alpha")
	if err != nil {
		t.Fatal(err)
	}
	if r.Manifest.Schedule != "every 12h" {
		t.Fatalf("manifest schedule after correction = %q, want %q", r.Manifest.Schedule, "every 12h")
	}
}

func TestRoutineDashboardEditScheduleCancel(t *testing.T) {
	d, home := routineDashboardDeps(t)
	if _, err := AddWith(d, "alpha", "every 6h", home); err != nil {
		t.Fatal(err)
	}
	m := openScheduleModal(t, d)
	m = clearScheduleInput(t, m)
	m = typeSchedule(t, m, "daily at 09:00")

	updated, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEsc})
	m = updated.(RoutineDashboard)
	if cmd != nil {
		t.Fatal("esc should not schedule a command")
	}
	if m.sched != nil {
		t.Fatal("esc should close the modal")
	}
	r, err := loadManifest(d, "alpha")
	if err != nil {
		t.Fatal(err)
	}
	if r.Manifest.Schedule != "every 6h" {
		t.Fatalf("manifest schedule after cancel = %q, want unchanged %q", r.Manifest.Schedule, "every 6h")
	}
}

func tmuxHasCommand(rt *recordingTmux, name string) bool {
	for _, cmd := range rt.commands {
		if len(cmd) > 0 && cmd[0] == name {
			return true
		}
	}
	return false
}
