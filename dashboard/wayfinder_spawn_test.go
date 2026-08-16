package dashboard

import (
	"errors"
	"github.com/glebglazov/pop/tasks/drain"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/glebglazov/pop/config"
	tmuxmod "github.com/glebglazov/pop/internal/tmux"
	"github.com/glebglazov/pop/internal/tmux/tmuxtest"
	"github.com/glebglazov/pop/project"
	"github.com/glebglazov/pop/tasks"
	"github.com/glebglazov/pop/wayfinder"
	"github.com/glebglazov/pop/work/ref"
)

func wayfinderSpawnFixture(t *testing.T) (*drain.Deps, *config.Config, DashboardRow, *tmuxtest.Fake, string) {
	t.Helper()
	storageDir := filepath.Join(t.TempDir(), "repos", "repo-wayfinder-spawn")
	activeMap := filepath.Join(storageDir, "maps", "2026-07-01-active")
	files := map[string]string{
		filepath.Join(storageDir, "repo.json"):            `{"common_dir":"/repo/.git"}`,
		filepath.Join(activeMap, "map.md"):                "Status: active\n\n## Destination\nShip it\n",
		filepath.Join(activeMap, "issues/01-frontier.md"): "Type: research\nStatus: open\n\n## Question\nA\n",
		filepath.Join(activeMap, "issues/02-blocked.md"):  "Type: research\nStatus: open\nBlocked by: 01\n\n## Question\nB\n",
		filepath.Join(activeMap, "issues/03-answered.md"): "Type: grilling\nStatus: resolved\n\n## Question\nC\n",
	}
	d := dashboardTestDeps(t, nil, nil)
	withWayfinderMaps(t, d, storageDir, files)
	repo := "/repo/checkout"
	cfg := &config.Config{Projects: []config.ProjectEntry{{Path: repo}}}
	f := &tmuxtest.Fake{}
	d.Project = project.DefaultDeps()
	d.Tmux = f
	row := DashboardRow{
		Kind:        ref.KindMap,
		ID:          "2026-07-01-active",
		Project:     "pop",
		DefPath:     filepath.Join(storageDir, "repo.json"),
		ProjectPath: repo,
		MapOpen:     2,
		MapFrontier: 1,
	}
	return d, cfg, row, f, storageDir
}

const (
	wayfinderMapID = "2026-07-01-active"
	// mapPaneWindow mirrors wayfinder's single Map window: every ticket agent is a
	// tiled pane in it, and there is no overview pane to step over (ADR-0182).
	mapPaneWindow = "map"
)

func wayfinderMapSession() string { return wayfinder.MapSessionName(wayfinderMapID) }

// wayfinderPaneCommand returns the command sent into a grilling pane of the Map
// session's one window — whichever ticket it is for.
func wayfinderPaneCommand(f *tmuxtest.Fake) (string, bool) {
	for _, pane := range f.Windows[wayfinderMapSession()][mapPaneWindow] {
		if cmds := f.SentCommands[pane]; len(cmds) > 0 {
			return strings.Join(cmds, " "), true
		}
	}
	return "", false
}

// seedWayfinderPane arranges a Map session already grilling one ticket in a tagged
// pane, so the reuse path (ADR-0158) has something to find.
func seedWayfinderPane(f *tmuxtest.Fake, ticketID, paneID, command string) {
	session := wayfinderMapSession()
	if f.Live == nil {
		f.Live = map[string]string{}
	}
	f.Live[session] = "/repo/checkout"
	if f.Windows == nil {
		f.Windows = map[string]map[string][]string{}
	}
	if f.Windows[session] == nil {
		f.Windows[session] = map[string][]string{}
	}
	f.Windows[session][mapPaneWindow] = []string{paneID}
	if f.PaneInfos == nil {
		f.PaneInfos = map[string]tmuxmod.PaneInfo{}
	}
	f.PaneInfos[paneID] = tmuxmod.PaneInfo{Session: session, Command: command}
	if f.PaneTagValues == nil {
		f.PaneTagValues = map[string]map[tmuxmod.PaneTag]string{}
	}
	f.PaneTagValues[paneID] = map[tmuxmod.PaneTag]string{tmuxmod.TagTicket: ticketID}
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
		filepath.Join(storageDir, "repo.json"):                                          `{"common_dir":"/repo/.git"}`,
		filepath.Join(storageDir, "maps", "2026-07-01-active", "map.md"):                "Status: active\n\n## Destination\nShip it\n",
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

// TestLaunchWayfinderSessionSpawnsIntoTheMapSession pins the reconciliation: the
// dashboard's map row lands in `pop-map-<id>` alongside `pop map next`'s windows,
// never in the repo session's drain window.
func TestLaunchWayfinderSessionSpawnsIntoTheMapSession(t *testing.T) {
	d, cfg, row, f, _ := wayfinderSpawnFixture(t)
	result, err := LaunchWayfinderSession(d, cfg, row, "")
	if err != nil {
		t.Fatalf("LaunchWayfinderSession: %v", err)
	}
	if result.Session != wayfinderMapSession() {
		t.Fatalf("session = %q, want %q", result.Session, wayfinderMapSession())
	}
	windows := f.Windows[wayfinderMapSession()]
	if len(windows) != 1 || len(windows[mapPaneWindow]) != 1 {
		t.Fatalf("expected one pane in the single map window, windows=%v", windows)
	}
	if got := f.PaneTitles[windows[mapPaneWindow][0]]; got != "01-frontier · "+tasks.FormatAgentEntry(tasks.EffectiveAttendedEntry(cfg)) {
		t.Fatalf("pane title = %q, want the ticket stem with attended entry", got)
	}
	for session, ws := range f.Windows {
		if _, ok := ws[drain.DrainWindowName]; ok {
			t.Fatalf("must not create the %q drain window in %q", drain.DrainWindowName, session)
		}
	}
}

func TestLaunchWayfinderSessionCreatesDetachedSession(t *testing.T) {
	d, cfg, row, f, _ := wayfinderSpawnFixture(t)
	if _, err := LaunchWayfinderSession(d, cfg, row, ""); err != nil {
		t.Fatalf("LaunchWayfinderSession: %v", err)
	}
	if got := f.Live[wayfinderMapSession()]; got != row.ProjectPath {
		t.Fatalf("detached session cwd = %q, want %q", got, row.ProjectPath)
	}
	stamp := f.WorkStamps[wayfinderMapSession()]
	if stamp.Kind != "map" || stamp.ID != wayfinderMapID {
		t.Fatalf("work stamp = %+v, want kind map and id %q", stamp, wayfinderMapID)
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
	seedWayfinderPane(f, "01", "%9", "claude")

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

// verbCmd unwraps the batch Update builds when a verb sets a flash: the verb's
// own command comes first and the flash-expiry timer after, so a test that wants
// the verb's message runs only the first half and never waits out the timer.
func verbCmd(t *testing.T, cmd tea.Cmd) tea.Cmd {
	t.Helper()
	if cmd == nil {
		return nil
	}
	batch, ok := cmd().(tea.BatchMsg)
	if !ok {
		return cmd
	}
	if len(batch) == 0 {
		t.Fatal("empty batch carries no verb command")
	}
	return batch[0]
}

func TestDashboardMapRowISpawnsFocusesAndQuits(t *testing.T) {
	d, cfg, row, f, _ := wayfinderSpawnFixture(t)
	f.Inside = true
	m := newQueueDashboard(d, cfg, DashboardSnapshot{Containers: []DashboardRow{row}})
	updated, cmd := m.Update(tea.KeyPressMsg{Code: 'I', Text: "I"})
	if cmd == nil {
		t.Fatal("I on map row did not return a command")
	}
	msg := verbCmd(t, cmd)()
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

// The dashboard's two fan-out verbs differ only in whether they move the operator:
// the uppercase one hands off and quits, the lowercase one reports and stays, and
// both spawn the same wall of panes (ADR-0182).
func TestDashboardMapRowFanOutGoesOrStays(t *testing.T) {
	d, cfg, row, f, storageDir := wayfinderSpawnFixture(t)
	// Two frontier tickets, so a fan-out is visibly more than one pane.
	withWayfinderMaps(t, d, storageDir, map[string]string{
		filepath.Join(storageDir, "repo.json"): `{"common_dir":"/repo/.git"}`,
		filepath.Join(storageDir, "maps", wayfinderMapID, "map.md"):                "Status: active\n\n## Destination\nShip it\n",
		filepath.Join(storageDir, "maps", wayfinderMapID, "issues/01-frontier.md"): "Type: research\nStatus: open\n\n## Question\nA\n",
		filepath.Join(storageDir, "maps", wayfinderMapID, "issues/02-blocked.md"):  "Type: research\nStatus: open\n\n## Question\nB\n",
	})
	f.Inside = true
	m := newQueueDashboard(d, cfg, DashboardSnapshot{Containers: []DashboardRow{row}})

	stayed, ok := m.spawnWayfinderFanOut(row)().(dashboardHandoffMsg)
	if !ok || stayed.err != nil {
		t.Fatalf("stay fan-out = %+v", stayed)
	}
	if stayed.quit {
		t.Fatalf("the lowercase fan-out quit the dashboard: %+v", stayed)
	}
	if !strings.Contains(stayed.status, "fanned out 2 frontier tickets into "+wayfinderMapSession()) {
		t.Fatalf("stay status = %q", stayed.status)
	}
	if panes := f.Windows[wayfinderMapSession()][mapPaneWindow]; len(panes) != 2 {
		t.Fatalf("panes = %v, want one per frontier ticket", f.Windows[wayfinderMapSession()])
	}

	// Everything is claimed now, so the focusing verb reports the empty frontier
	// the same way the single-ticket verb does.
	went, ok := m.launchWayfinderFanOut(row)().(dashboardHandoffMsg)
	if !ok || !errors.Is(went.err, wayfinder.ErrEmptyFrontier) {
		t.Fatalf("fan-out over an exhausted frontier = %+v, want ErrEmptyFrontier", went)
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
	m := newQueueDashboard(d, cfg, DashboardSnapshot{Containers: []DashboardRow{row}})
	updated, cmd := m.Update(tea.KeyPressMsg{Code: 'I', Text: "I"})
	if cmd == nil {
		t.Fatal("expected spawn command")
	}
	got := updated.(QueueDashboard)
	updated, _ = got.Update(cmd())
	got = updated.(QueueDashboard)
	if got.flash.Text() != dashboardWayfinderEmptyFrontierMessage() {
		t.Fatalf("flash = %q, want empty-frontier explanation", got.flash.Text())
	}
}

func TestDashboardMapRowIReusesRunningWithoutResend(t *testing.T) {
	d, cfg, row, f, _ := wayfinderSpawnFixture(t)
	seedWayfinderPane(f, "01", "%9", "claude")
	f.Inside = true

	m := newQueueDashboard(d, cfg, DashboardSnapshot{Containers: []DashboardRow{row}})
	updated, cmd := m.Update(tea.KeyPressMsg{Code: 'I', Text: "I"})
	if cmd == nil {
		t.Fatal("I did not return a command")
	}
	msg := verbCmd(t, cmd)()
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

// TestDashboardMapDetailWorksFrontierTicketFromItemMenu covers the Map's item
// verb through the generic detail: the menu the kind fills over a frontier
// ticket carries `work ticket`, and running it hands the operator off to the
// Map's grilling pane.
func TestDashboardMapDetailWorksFrontierTicketFromItemMenu(t *testing.T) {
	m, d := newMapDetailDashboard(t)
	repo := "/repo/checkout"
	m.cfg = &config.Config{Projects: []config.ProjectEntry{{Path: repo}}}
	d.Project = project.DefaultDeps()
	d.Tmux = &tmuxtest.Fake{Inside: true}
	m.d = d
	got := openMapDetail(t, m)
	got.detail.row.ProjectPath = repo
	got.detail.row.ProjectPath = repo

	updated, _ := got.Update(tea.KeyPressMsg{Code: 'a', Text: "a"})
	got = updated.(QueueDashboard)
	if got.itemMenu == nil {
		t.Fatal("a on a frontier ticket did not open the item menu")
	}
	if keys := itemMenuKeys(got.itemMenu); !slices.Contains(keys, "I") {
		t.Fatalf("frontier ticket menu = %v, want the work verb on I", keys)
	}
	updated, cmd := got.Update(tea.KeyPressMsg{Code: 'I', Text: "I"})
	if cmd == nil {
		t.Fatal("work verb on frontier ticket did not return a command")
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
			wayfinderMapSession(): {
				mapPaneWindow: {"%3", "%2"},
			},
			"pop": {"pop-work": {"%1"}},
		},
		PaneInfos: map[string]tmuxmod.PaneInfo{
			"%3": {Session: wayfinderMapSession(), Command: "claude"},
			"%2": {Session: wayfinderMapSession(), Command: "zsh"},
			"%1": {Session: "pop", Command: "node"},
		},
	}
	cache := loadLivePaneCache(&drain.Deps{Tmux: f})
	if cache.wayfinderState(wayfinderMapID) != livePaneRunning {
		t.Fatalf("map window state = %v, want running", cache.wayfinderState(wayfinderMapID))
	}
	styled := dashboardActivityCluster(DashboardRow{Kind: ref.KindMap, ID: wayfinderMapID}, cache, true)
	if !strings.Contains(styled, livePaneRunningStyle.Render("I")) {
		t.Fatalf("map row cluster = %q, want green I", styled)
	}
}

// `S` on a map row opens the Map-scoped assist session and quits to it — and it
// does so with the frontier resolved away, which is the state the frontier keys
// disappear in and assist is most wanted in (ADR-0184).
func TestDashboardMapRowSAssistsWithNoFrontier(t *testing.T) {
	d, cfg, row, f, storageDir := wayfinderSpawnFixture(t)
	withWayfinderMaps(t, d, storageDir, map[string]string{
		filepath.Join(storageDir, "repo.json"): `{"common_dir":"/repo/.git"}`,
		filepath.Join(storageDir, "maps", wayfinderMapID, "map.md"):                "Status: active\n\n## Destination\nShip it\n",
		filepath.Join(storageDir, "maps", wayfinderMapID, "issues/01-frontier.md"): "Type: research\nStatus: resolved\n\n## Question\nA\n",
	})
	row.MapFrontier = 0
	f.Inside = true
	m := newQueueDashboard(d, cfg, DashboardSnapshot{Containers: []DashboardRow{row}})

	opened, _ := m.update(tea.KeyPressMsg{Code: 'a', Text: "a"})
	updated, cmd := opened.(QueueDashboard).update(tea.KeyPressMsg{Code: 'S', Text: "S"})
	if cmd == nil {
		t.Fatal("S on a map row with no frontier did not return a command")
	}
	msg := cmd()
	handoff, ok := msg.(dashboardHandoffMsg)
	if !ok {
		t.Fatalf("msg = %T, want dashboardHandoffMsg", msg)
	}
	if handoff.err != nil || !handoff.quit {
		t.Fatalf("handoff = %+v, want a successful quit to the assist session", handoff)
	}
	panes := f.Windows[wayfinderMapSession()][mapPaneWindow]
	if len(panes) != 1 {
		t.Fatalf("panes = %v, want the single assist pane", f.Windows[wayfinderMapSession()])
	}
	if got, _ := f.PaneTagValue(panes[0], tmuxmod.TagAssist); got != wayfinderMapID {
		t.Fatalf("assist pane tag = %q, want the map id", got)
	}
	if got := strings.Join(f.SentCommands[panes[0]], " "); !strings.Contains(got, "assist "+wayfinderMapID) {
		t.Fatalf("assist pane runs %q, want the assist-mode invocation", got)
	}

	// A second press returns to the same pane rather than opening a second
	// conversation on the Map's prose.
	f.PaneInfos = map[string]tmuxmod.PaneInfo{panes[0]: {Session: wayfinderMapSession(), Command: "claude"}}
	sentBefore := len(f.SentCommands[panes[0]])
	again, ok := updated.(QueueDashboard).launchWayfinderAssist(row)().(dashboardHandoffMsg)
	if !ok || again.err != nil {
		t.Fatalf("second assist = %+v", again)
	}
	if got := f.Windows[wayfinderMapSession()][mapPaneWindow]; len(got) != 1 {
		t.Fatalf("second assist opened another pane: %v", got)
	}
	if got := len(f.SentCommands[panes[0]]); got != sentBefore {
		t.Fatalf("second assist re-sent work into a live pane (%d sends, was %d)", got, sentBefore)
	}
}
