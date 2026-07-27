package queue

import (
	"errors"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/glebglazov/pop/config"
	"github.com/glebglazov/pop/internal/tmux/tmuxtest"
	"github.com/glebglazov/pop/project"
	"github.com/glebglazov/pop/wayfinder"
)

func wayfinderSpawnFixture(t *testing.T) (*Deps, *config.Config, DashboardRow, *tmuxtest.Fake, string) {
	t.Helper()
	storageDir := filepath.Join(t.TempDir(), "repos", "repo-wayfinder-spawn")
	activeMap := filepath.Join(storageDir, "wayfinder", "2026-07-01-active")
	files := map[string]string{
		filepath.Join(storageDir, "repo.json"):               `{"common_dir":"/repo/.git"}`,
		filepath.Join(activeMap, "map.md"):                 "Status: active\n\n## Destination\nShip it\n",
		filepath.Join(activeMap, "issues/01-frontier.md"):  "Type: research\nStatus: open\n\n## Question\nA\n",
		filepath.Join(activeMap, "issues/02-blocked.md"):   "Type: research\nStatus: open\nBlocked by: 01\n\n## Question\nB\n",
		filepath.Join(activeMap, "issues/03-claimed.md"):   "Type: grilling\nStatus: claimed\n\n## Question\nC\n",
	}
	d := dashboardTestDeps(t, nil, nil)
	withWayfinderMaps(t, d, storageDir, files)
	repo := "/repo/checkout"
	cfg := &config.Config{Projects: []config.ProjectEntry{{Path: repo}}}
	f := &tmuxtest.Fake{}
	d.Project = project.DefaultDeps()
	d.Tmux = f
	row := DashboardRow{
		IsMap:   true,
		Project: "pop",
		SetRef: SetRef{
			SetID:       "2026-07-01-active",
			DefPath:     filepath.Join(storageDir, "repo.json"),
			ProjectPath: repo,
			ProjectName: "pop",
		},
		MapOpen:     2,
		MapFrontier: 1,
	}
	return d, cfg, row, f, storageDir
}

const wayfinderMapID = "2026-07-01-active"

// wayfinderPaneCommand returns the command sent into the pane of the window
// named after the map, across whichever session hosts it.
func wayfinderPaneCommand(f *tmuxtest.Fake) (string, bool) {
	for _, windows := range f.Windows {
		panes, ok := windows[wayfinderMapID]
		if !ok || len(panes) == 0 {
			continue
		}
		cmds := f.SentCommands[panes[0]]
		if len(cmds) == 0 {
			return "", false
		}
		return strings.Join(cmds, " "), true
	}
	return "", false
}

func TestLaunchWayfinderSessionTargetsNextFrontier(t *testing.T) {
	d, cfg, row, f, _ := wayfinderSpawnFixture(t)
	result, err := LaunchWayfinderSession(d, cfg, row, "")
	if err != nil {
		t.Fatalf("LaunchWayfinderSession: %v", err)
	}
	if result.TicketID != "01" {
		t.Fatalf("TicketID = %q, want 01", result.TicketID)
	}
	cmd, ok := wayfinderPaneCommand(f)
	if !ok {
		t.Fatal("expected the wayfinder command to be sent into the map pane")
	}
	if !strings.Contains(cmd, "/pop-wayfinder work 2026-07-01-active 01") {
		t.Fatalf("spawn command = %q, want work-mode invocation with map and ticket", cmd)
	}
	if !strings.HasPrefix(cmd, "claude ") {
		t.Fatalf("spawn command = %q, want default interactive claude binary", cmd)
	}
}

func TestLaunchWayfinderSessionTargetsExplicitTicket(t *testing.T) {
	d, cfg, row, f, storageDir := wayfinderSpawnFixture(t)
	files := map[string]string{
		filepath.Join(storageDir, "repo.json"):               `{"common_dir":"/repo/.git"}`,
		filepath.Join(storageDir, "wayfinder", "2026-07-01-active", "map.md"): "Status: active\n\n## Destination\nShip it\n",
		filepath.Join(storageDir, "wayfinder", "2026-07-01-active", "issues/01-frontier.md"): "Type: research\nStatus: resolved\n\n## Question\nA\n",
		filepath.Join(storageDir, "wayfinder", "2026-07-01-active", "issues/02-blocked.md"):  "Type: research\nStatus: open\n\n## Question\nB\n",
	}
	withWayfinderMaps(t, d, storageDir, files)
	result, err := LaunchWayfinderSession(d, cfg, row, "02")
	if err != nil {
		t.Fatalf("LaunchWayfinderSession: %v", err)
	}
	if result.TicketID != "02" {
		t.Fatalf("TicketID = %q, want 02", result.TicketID)
	}
	cmd, ok := wayfinderPaneCommand(f)
	if !ok || !strings.Contains(cmd, " 02") {
		t.Fatalf("spawn command = %q, want explicit ticket 02", cmd)
	}
}

func TestLaunchWayfinderSessionWindowNamedAfterMap(t *testing.T) {
	d, cfg, row, f, _ := wayfinderSpawnFixture(t)
	if _, err := LaunchWayfinderSession(d, cfg, row, ""); err != nil {
		t.Fatalf("LaunchWayfinderSession: %v", err)
	}
	var found bool
	for _, windows := range f.Windows {
		if _, ok := windows[wayfinderMapID]; ok {
			found = true
		}
		if _, ok := windows[drainWindowName]; ok {
			t.Fatalf("must not create the %q drain window, windows=%v", drainWindowName, windows)
		}
	}
	if !found {
		t.Fatalf("expected a window named after the map %q, windows=%v", wayfinderMapID, f.Windows)
	}
}

func TestLaunchWayfinderSessionCreatesDetachedSession(t *testing.T) {
	d, cfg, row, f, _ := wayfinderSpawnFixture(t)
	if _, err := LaunchWayfinderSession(d, cfg, row, ""); err != nil {
		t.Fatalf("LaunchWayfinderSession: %v", err)
	}
	var gotCWD string
	for _, dir := range f.Live {
		gotCWD = dir
	}
	if gotCWD != row.ProjectPath {
		t.Fatalf("detached session cwd = %q, want %q", gotCWD, row.ProjectPath)
	}
}

func TestLaunchWayfinderSessionEmptyFrontier(t *testing.T) {
	d, cfg, row, f, storageDir := wayfinderSpawnFixture(t)
	files := map[string]string{
		filepath.Join(storageDir, "wayfinder", "2026-07-01-active", "issues/01-frontier.md"): "Type: research\nStatus: open\nBlocked by: 99\n\n## Question\nA\n",
	}
	withWayfinderMaps(t, d, storageDir, files)
	_, err := LaunchWayfinderSession(d, cfg, row, "")
	if !errors.Is(err, wayfinder.ErrEmptyFrontier) {
		t.Fatalf("err = %v, want ErrEmptyFrontier", err)
	}
	if _, ok := wayfinderPaneCommand(f); ok {
		t.Fatal("empty frontier must not spawn")
	}
}

func TestDashboardMapRowISpawnsNextFrontier(t *testing.T) {
	d, cfg, row, f, _ := wayfinderSpawnFixture(t)
	m := newQueueDashboard(d, cfg, DashboardSnapshot{Rows: []DashboardRow{row}})
	updated, cmd := m.Update(tea.KeyPressMsg{Code: 'i', Text: "i"})
	if cmd == nil {
		t.Fatal("i on map row did not return a command")
	}
	msg := cmd()
	wfMsg, ok := msg.(dashboardWayfinderMsg)
	if !ok {
		t.Fatalf("msg = %T, want dashboardWayfinderMsg", msg)
	}
	if wfMsg.err != nil {
		t.Fatalf("spawn err = %v", wfMsg.err)
	}
	if wfMsg.ticketID != "01" {
		t.Fatalf("ticketID = %q, want 01", wfMsg.ticketID)
	}
	updated, _ = updated.(QueueDashboard).Update(msg)
	got := updated.(QueueDashboard)
	if got.statusMsg == "" {
		t.Fatal("expected spawn status message")
	}
	if _, ok := wayfinderPaneCommand(f); !ok {
		t.Fatal("expected tmux spawn")
	}
}

func TestDashboardMapRowIEmptyFrontierMessage(t *testing.T) {
	d, cfg, row, _, storageDir := wayfinderSpawnFixture(t)
	files := map[string]string{
		filepath.Join(storageDir, "wayfinder", "2026-07-01-active", "issues/01-frontier.md"): "Type: research\nStatus: claimed\n\n## Question\nA\n",
		filepath.Join(storageDir, "wayfinder", "2026-07-01-active", "issues/02-blocked.md"):  "Type: research\nStatus: open\nBlocked by: 01\n\n## Question\nB\n",
	}
	withWayfinderMaps(t, d, storageDir, files)
	row.MapFrontier = 0
	m := newQueueDashboard(d, cfg, DashboardSnapshot{Rows: []DashboardRow{row}})
	updated, cmd := m.Update(tea.KeyPressMsg{Code: 'i', Text: "i"})
	if cmd == nil {
		t.Fatal("expected spawn command")
	}
	got := updated.(QueueDashboard)
	updated, _ = got.Update(cmd())
	got = updated.(QueueDashboard)
	if got.statusMsg != dashboardWayfinderEmptyFrontierMessage() {
		t.Fatalf("statusMsg = %q, want empty-frontier explanation", got.statusMsg)
	}
}
