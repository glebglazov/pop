package queue

import (
	"errors"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/glebglazov/pop/config"
	tmuxmod "github.com/glebglazov/pop/internal/tmux"
	"github.com/glebglazov/pop/internal/tmux/tmuxtest"
	"github.com/glebglazov/pop/project"
	"github.com/glebglazov/pop/wayfinder"
)

func wayfinderSpawnFixture(t *testing.T) (*Deps, *config.Config, DashboardRow, *tmuxtest.Fake, string) {
	t.Helper()
	storageDir := filepath.Join(t.TempDir(), "repos", "repo-wayfinder-spawn")
	activeMap := filepath.Join(storageDir, "maps", "2026-07-01-active")
	files := map[string]string{
		filepath.Join(storageDir, "repo.json"):               `{"common_dir":"/repo/.git"}`,
		filepath.Join(activeMap, "map.md"):                 "Status: active\n\n## Destination\nShip it\n",
		filepath.Join(activeMap, "issues/01-frontier.md"):  "Type: research\nStatus: open\n\n## Question\nA\n",
		filepath.Join(activeMap, "issues/02-blocked.md"):   "Type: research\nStatus: open\nBlocked by: 01\n\n## Question\nB\n",
		filepath.Join(activeMap, "issues/03-answered.md"):  "Type: grilling\nStatus: resolved\n\n## Question\nC\n",
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

func seedWayfinderWindow(f *tmuxtest.Fake, session, paneID, command string) {
	if f.Windows == nil {
		f.Windows = map[string]map[string][]string{}
	}
	if f.Windows[session] == nil {
		f.Windows[session] = map[string][]string{}
	}
	f.Windows[session][wayfinderMapID] = []string{paneID}
	if f.PaneInfos == nil {
		f.PaneInfos = map[string]tmuxmod.PaneInfo{}
	}
	f.PaneInfos[paneID] = tmuxmod.PaneInfo{Session: session, Command: command}
}

func TestLaunchWayfinderSessionTargetsNextFrontier(t *testing.T) {
	d, cfg, row, f, _ := wayfinderSpawnFixture(t)
	result, err := LaunchWayfinderSession(d, cfg, row, "")
	if err != nil {
		t.Fatalf("LaunchWayfinderSession: %v", err)
	}
	if result.PaneID == "" {
		t.Fatal("expected pane id")
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
		filepath.Join(storageDir, "maps", "2026-07-01-active", "map.md"): "Status: active\n\n## Destination\nShip it\n",
		filepath.Join(storageDir, "maps", "2026-07-01-active", "issues/01-frontier.md"): "Type: research\nStatus: resolved\n\n## Question\nA\n",
		filepath.Join(storageDir, "maps", "2026-07-01-active", "issues/02-blocked.md"):  "Type: research\nStatus: open\n\n## Question\nB\n",
	}
	withWayfinderMaps(t, d, storageDir, files)
	result, err := LaunchWayfinderSession(d, cfg, row, "02")
	if err != nil {
		t.Fatalf("LaunchWayfinderSession: %v", err)
	}
	if result.PaneID == "" {
		t.Fatal("expected pane id")
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
		filepath.Join(storageDir, "maps", "2026-07-01-active", "issues/01-frontier.md"): "Type: research\nStatus: open\nBlocked by: 99\n\n## Question\nA\n",
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

func TestLaunchWayfinderSessionReusesRunningWithoutResend(t *testing.T) {
	d, cfg, row, f, _ := wayfinderSpawnFixture(t)
	session := project.SessionNameWith(d.Project, row.ProjectPath)
	seedWayfinderWindow(f, session, "%9", "claude")

	result, err := LaunchWayfinderSession(d, cfg, row, "")
	if err != nil {
		t.Fatalf("LaunchWayfinderSession: %v", err)
	}
	if result.PaneID != "%9" {
		t.Fatalf("PaneID = %q, want %%9", result.PaneID)
	}
	if f.SentCommands["%9"] != nil {
		t.Fatalf("reuse must not re-send wayfinder, commands=%v", f.SentCommands)
	}
}

func TestDashboardMapRowISpawnsFocusesAndQuits(t *testing.T) {
	d, cfg, row, f, _ := wayfinderSpawnFixture(t)
	f.Inside = true
	m := newQueueDashboard(d, cfg, DashboardSnapshot{Rows: []DashboardRow{row}})
	updated, cmd := m.Update(tea.KeyPressMsg{Code: 'I', Text: "I"})
	if cmd == nil {
		t.Fatal("I on map row did not return a command")
	}
	msg := cmd()
	handoff, ok := msg.(dashboardHandoffMsg)
	if !ok {
		t.Fatalf("msg = %T, want dashboardHandoffMsg", msg)
	}
	if handoff.err != nil {
		t.Fatalf("handoff err = %v", handoff.err)
	}
	if !handoff.quit {
		t.Fatalf("handoff quit = false, status=%q; want quit after focus", handoff.status)
	}
	if _, ok := wayfinderPaneCommand(f); !ok {
		t.Fatal("expected tmux spawn")
	}
	if len(f.Selected) == 0 {
		t.Fatalf("handoff must focus spawned pane, selected=%v", f.Selected)
	}

	updated, quitCmd := updated.(QueueDashboard).Update(handoff)
	if _, ok := updated.(QueueDashboard); !ok {
		t.Fatalf("Update returned %T", updated)
	}
	if quitCmd == nil {
		t.Fatal("successful handoff must quit the dashboard")
	}
	if _, ok := quitCmd().(tea.QuitMsg); !ok {
		t.Fatalf("quit cmd = %T, want tea.QuitMsg", quitCmd())
	}
}

func TestDashboardMapRowIEmptyFrontierMessage(t *testing.T) {
	d, cfg, row, _, storageDir := wayfinderSpawnFixture(t)
	files := map[string]string{
		filepath.Join(storageDir, "maps", "2026-07-01-active", "issues/01-frontier.md"): "Type: research\nStatus: resolved\n\n## Question\nA\n",
		filepath.Join(storageDir, "maps", "2026-07-01-active", "issues/02-blocked.md"):  "Type: research\nStatus: resolved\n\n## Question\nB\n",
	}
	withWayfinderMaps(t, d, storageDir, files)
	row.MapFrontier = 0
	m := newQueueDashboard(d, cfg, DashboardSnapshot{Rows: []DashboardRow{row}})
	updated, cmd := m.Update(tea.KeyPressMsg{Code: 'I', Text: "I"})
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

func TestDashboardMapRowIReusesRunningWithoutResend(t *testing.T) {
	d, cfg, row, f, _ := wayfinderSpawnFixture(t)
	session := project.SessionNameWith(d.Project, row.ProjectPath)
	seedWayfinderWindow(f, session, "%9", "claude")
	f.Inside = true

	m := newQueueDashboard(d, cfg, DashboardSnapshot{Rows: []DashboardRow{row}})
	updated, cmd := m.Update(tea.KeyPressMsg{Code: 'I', Text: "I"})
	if cmd == nil {
		t.Fatal("I did not return a command")
	}
	msg := cmd()
	handoff, ok := msg.(dashboardHandoffMsg)
	if !ok {
		t.Fatalf("msg = %T, want dashboardHandoffMsg", msg)
	}
	if !handoff.quit || handoff.err != nil {
		t.Fatalf("handoff = %+v, want quit without err", handoff)
	}
	if f.SentCommands["%9"] != nil {
		t.Fatalf("reuse must not re-send wayfinder, commands=%v", f.SentCommands)
	}
	if len(f.Selected) == 0 || f.Selected[len(f.Selected)-1] != "%9" {
		t.Fatalf("reuse must focus existing pane, selected=%v", f.Selected)
	}
	_, quitCmd := updated.(QueueDashboard).Update(handoff)
	if quitCmd == nil {
		t.Fatal("successful handoff must quit the dashboard")
	}
}

func TestDashboardMapDetailEnterHandsOffFrontierTicket(t *testing.T) {
	m, d := newMapDetailDashboard(t)
	repo := "/repo/checkout"
	m.cfg = &config.Config{Projects: []config.ProjectEntry{{Path: repo}}}
	d.Project = project.DefaultDeps()
	d.Tmux = &tmuxtest.Fake{Inside: true}
	m.d = d
	got := loadMapDetail(t, m)
	got.detail.row.ProjectPath = repo
	got.detail.row.SetRef.ProjectPath = repo

	updated, cmd := got.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("enter on frontier ticket did not return a command")
	}
	msg := cmd()
	handoff, ok := msg.(dashboardHandoffMsg)
	if !ok {
		t.Fatalf("msg = %T, want dashboardHandoffMsg", msg)
	}
	if !handoff.quit || handoff.err != nil {
		t.Fatalf("handoff = %+v, want quit without err", handoff)
	}
	updated, quitCmd := updated.(QueueDashboard).Update(handoff)
	if quitCmd == nil {
		t.Fatal("successful handoff must quit the dashboard")
	}
	if _, ok := updated.(QueueDashboard); !ok {
		t.Fatalf("Update returned %T", updated)
	}
}

func TestLivePaneCacheWayfinderWindow(t *testing.T) {
	f := &tmuxtest.Fake{
		Windows: map[string]map[string][]string{
			"pop": {
				wayfinderMapID: {"%3"},
				"pop-queue":    {"%1"},
			},
		},
		PaneInfos: map[string]tmuxmod.PaneInfo{
			"%3": {Session: "pop", Command: "claude"},
			"%1": {Session: "pop", Command: "node"},
		},
	}
	cache := loadLivePaneCache(&Deps{Tmux: f})
	if cache.wayfinderState(wayfinderMapID) != livePaneRunning {
		t.Fatalf("map window state = %v, want running", cache.wayfinderState(wayfinderMapID))
	}
	styled := dashboardActivityCluster(DashboardRow{IsMap: true, SetRef: SetRef{SetID: wayfinderMapID}}, cache, true)
	if !strings.Contains(styled, livePaneRunningStyle.Render("I")) {
		t.Fatalf("map row cluster = %q, want green I", styled)
	}
}
