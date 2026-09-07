package dashboard

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/glebglazov/pop/config"
	"github.com/glebglazov/pop/internal/queuetest"
	tmuxmod "github.com/glebglazov/pop/internal/tmux"
	"github.com/glebglazov/pop/tasks"
	"github.com/glebglazov/pop/tasks/drain"
)

// seedSlottedPane arranges one assist pane of setID on slot, running command —
// both as the pane the launchers will find and as the poll the menu is built
// from, which is how the two agree in production.
func seedSlottedPane(rt *queuetest.RecordingTmux, paneID, setID string, slot tmuxmod.PaneSlot, command string) {
	seedTaggedPane(rt, paneID, tmuxmod.TagAssist, setID)
	seedTaggedPane(rt, paneID, tmuxmod.TagSlot, slot.String())
	if rt.Fake.PaneInfos == nil {
		rt.Fake.PaneInfos = map[string]tmuxmod.PaneInfo{}
	}
	rt.Fake.PaneInfos[paneID] = tmuxmod.PaneInfo{Session: "proj", Command: command}
}

// openAssistMenuOverRow presses the two keys an operator does — `r` for the Run
// menu, then the assist verb — over a dashboard whose poll saw panes.
func openAssistMenuOverRow(t *testing.T, m QueueDashboard, panes map[tmuxmod.PaneSlot]livePaneState, setID string) QueueDashboard {
	t.Helper()
	m.width, m.height = 120, 40
	for slot, state := range panes {
		m.live.setAssistPane(setID, slot, state)
	}
	updated, _ := m.Update(tea.KeyPressMsg{Code: 'r', Text: "r"})
	updated, _ = updated.(QueueDashboard).Update(tea.KeyPressMsg{Code: 'A', Text: "A"})
	return updated.(QueueDashboard)
}

// TestAssistVerbOpensThePaneMenu is the shape of ADR-0263 on a Task set: the
// assist verb spawns nothing, it puts each pane the set holds on its own digit —
// green where the session is running, grey where it exited — and esc goes back to
// the rows it was pressed on.
func TestAssistVerbOpensThePaneMenu(t *testing.T) {
	setID := "assist-menu"
	row := DashboardRow{Project: "pop", CursorKey: "pop\x00" + setID, ID: setID, RawStatus: tasks.StatusReady}
	m := newQueueDashboard(&drain.Deps{}, &config.Config{}, DashboardSnapshot{Containers: []DashboardRow{row}})

	got := openAssistMenuOverRow(t, m, map[tmuxmod.PaneSlot]livePaneState{
		1: livePaneRunning,
		3: livePaneIdle,
	}, setID)
	view := got.View().Content

	if !strings.Contains(view, "assist · "+setID) {
		t.Fatalf("menu must name itself and the set it is over:\n%s", view)
	}
	if !strings.Contains(view, livePaneRunningStyle.Render("1")) {
		t.Fatalf("a running pane's digit must be green:\n%s", view)
	}
	if !strings.Contains(view, livePaneIdleStyle().Render("3")) {
		t.Fatalf("an exited pane's digit must be grey:\n%s", view)
	}
	if strings.Contains(view, "  2  ") {
		t.Fatalf("a slot holding no pane must not be listed:\n%s", view)
	}
	if !strings.Contains(view, "n  open another assist pane") {
		t.Fatalf("menu missing the open-another entry:\n%s", view)
	}

	back, _ := got.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	if view := back.(QueueDashboard).View().Content; strings.Contains(view, "open another assist pane") {
		t.Fatalf("esc must go back to the rows:\n%s", view)
	}
}

// TestAssistMenuOpensWithNoPaneLive asserts the menu still opens over a set
// holding nothing, offering only `n`: the keystrokes must not change shape with
// tmux state the row cannot show.
func TestAssistMenuOpensWithNoPaneLive(t *testing.T) {
	setID := "assist-empty"
	row := DashboardRow{Project: "pop", CursorKey: "pop\x00" + setID, ID: setID, RawStatus: tasks.StatusReady}
	m := newQueueDashboard(&drain.Deps{}, &config.Config{}, DashboardSnapshot{Containers: []DashboardRow{row}})

	got := openAssistMenuOverRow(t, m, nil, setID)
	view := got.View().Content
	if !strings.Contains(view, "n  open another assist pane") {
		t.Fatalf("the menu must open with only n:\n%s", view)
	}
	if !strings.Contains(view, "assist · "+setID) {
		t.Fatalf("menu missing its rule:\n%s", view)
	}
}

// TestAssistMenuDigitHandsOffToThatSlot drives the digit end to end: a running
// pane on slot 2 is walked into untouched, and an exited one has its session
// restarted in place rather than spawning a twin.
func TestAssistMenuDigitHandsOffToThatSlot(t *testing.T) {
	for _, tc := range []struct {
		name      string
		command   string
		state     livePaneState
		wantSends int
	}{
		{name: "running", command: "claude", state: livePaneRunning, wantSends: 0},
		{name: "exited", command: "zsh", state: livePaneIdle, wantSends: 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			repo, setID, _ := queuetest.SetupSpawnRepo(t, "assist-digit-"+tc.name, []queuetest.SpawnTask{
				{ID: "01-a", File: "01-a.md", Title: "A", Type: "AFK", Status: "done"},
			})
			d, cfg, row, rt := dashboardLaunchFixture(t, repo, setID)
			row.RuntimePath, row.ProjectPath = repo, repo
			rt.SessionLive = true
			rt.Fake.Inside = true
			rt.WindowNames["pop-work"] = true
			rt.Fake.PaneCwd = map[string]string{"%5": repo}
			seedSlottedPane(rt, "%5", setID, 2, tc.command)

			m := newQueueDashboard(d, cfg, DashboardSnapshot{Containers: []DashboardRow{row}})
			got := openAssistMenuOverRow(t, m, map[tmuxmod.PaneSlot]livePaneState{2: tc.state}, setID)
			_, cmd := got.Update(tea.KeyPressMsg{Code: '2', Text: "2"})
			if cmd == nil {
				t.Fatal("the digit launched nothing")
			}
			msg, ok := verbCmd(t, cmd)().(dashboardHandoffMsg)
			if !ok || msg.err != nil || !msg.quit {
				t.Fatalf("handoff = %+v, want the pane on slot 2 walked into", msg)
			}
			if rt.CountCommand("split-window") != 0 {
				t.Fatalf("the slot already holds a pane; commands=%v", rt.Commands)
			}
			if got := rt.CountCommand("send-keys"); got != tc.wantSends {
				t.Fatalf("send-keys = %d, want %d; commands=%v", got, tc.wantSends, rt.Commands)
			}
			if !rt.FindSwitched("%5") {
				t.Fatalf("the handoff must focus the slot's own pane; commands=%v", rt.Commands)
			}
		})
	}
}

// TestAssistMenuNewTakesTheLowestFreeSlot asserts `n` opens beside the pane the
// set already holds — on the lowest digit no pane sits on, tagged with the set
// like any other assist pane.
func TestAssistMenuNewTakesTheLowestFreeSlot(t *testing.T) {
	repo, setID, _ := queuetest.SetupSpawnRepo(t, "assist-new-slot", []queuetest.SpawnTask{
		{ID: "01-a", File: "01-a.md", Title: "A", Type: "AFK", Status: "done"},
	})
	d, cfg, row, rt := dashboardLaunchFixture(t, repo, setID)
	row.RuntimePath, row.ProjectPath = repo, repo
	rt.SessionLive = true
	rt.Fake.Inside = true
	rt.WindowNames["pop-work"] = true
	seedSlottedPane(rt, "%5", setID, 1, "claude")

	m := newQueueDashboard(d, cfg, DashboardSnapshot{Containers: []DashboardRow{row}})
	got := openAssistMenuOverRow(t, m, map[tmuxmod.PaneSlot]livePaneState{1: livePaneRunning}, setID)
	_, cmd := got.Update(tea.KeyPressMsg{Code: 'n', Text: "n"})
	if cmd == nil {
		t.Fatal("n opened nothing")
	}
	msg, ok := verbCmd(t, cmd)().(dashboardHandoffMsg)
	if !ok || msg.err != nil || !msg.quit {
		t.Fatalf("handoff = %+v, want a second assist pane walked into", msg)
	}
	if !rt.FindSwitched("%7") {
		t.Fatalf("the handoff must focus the pane it opened; commands=%v", rt.Commands)
	}
	fresh := rt.Fake.PaneTagValues["%7"]
	if fresh[tmuxmod.TagAssist] != setID {
		t.Fatalf("@pop_assist = %q, want the bare set id %q", fresh[tmuxmod.TagAssist], setID)
	}
	if fresh[tmuxmod.TagSlot] != "2" {
		t.Fatalf("@pop_slot = %q, want the lowest free slot 2", fresh[tmuxmod.TagSlot])
	}
}

// TestAssistMenuNewRefusedWhenEverySlotIsTaken asserts the tenth pane is refused
// on the surface, in a flash, with the menu still open and tmux untouched.
func TestAssistMenuNewRefusedWhenEverySlotIsTaken(t *testing.T) {
	repo, setID, _ := queuetest.SetupSpawnRepo(t, "assist-full", []queuetest.SpawnTask{
		{ID: "01-a", File: "01-a.md", Title: "A", Type: "AFK", Status: "done"},
	})
	d, cfg, row, rt := dashboardLaunchFixture(t, repo, setID)
	row.RuntimePath, row.ProjectPath = repo, repo
	held := map[tmuxmod.PaneSlot]livePaneState{}
	for slot := tmuxmod.FirstPaneSlot; slot <= tmuxmod.LastPaneSlot; slot++ {
		held[slot] = livePaneRunning
	}

	m := newQueueDashboard(d, cfg, DashboardSnapshot{Containers: []DashboardRow{row}})
	got := openAssistMenuOverRow(t, m, held, setID)
	updated, _ := got.Update(tea.KeyPressMsg{Code: 'n', Text: "n"})
	after := updated.(QueueDashboard)

	if len(rt.Commands) != 0 {
		t.Fatalf("a refused pane must not touch tmux: %v", rt.Commands)
	}
	view := after.View().Content
	if !strings.Contains(view, assistMenuFullRefusal()) {
		t.Fatalf("the refusal must say why nothing opened:\n%s", view)
	}
	if !strings.Contains(view, livePaneRunningStyle.Render("9")) {
		t.Fatalf("the menu must stay open under its refusal:\n%s", view)
	}
}

// TestRowAssistKeyReadsTheWholeSet asserts the row keeps one assist affordance
// for nine panes: green while any of them is running, grey once they have all
// fallen back to a shell.
func TestRowAssistKeyReadsTheWholeSet(t *testing.T) {
	setID := "assist-cluster"
	row := DashboardRow{ID: setID}
	for _, tc := range []struct {
		name    string
		second  string
		wantKey string
	}{
		{name: "one still running", second: "claude", wantKey: livePaneRunningStyle.Render("A")},
		{name: "all exited", second: "zsh", wantKey: livePaneIdleStyle().Render("A")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rt := queuetest.NewRecordingTmux(false, "0")
			seedSlottedPane(rt, "%1", setID, 1, "zsh")
			seedSlottedPane(rt, "%2", setID, 2, tc.second)

			cache := loadLivePaneCache(&drain.Deps{Tmux: rt})
			if got := dashboardActivityCluster(row, cache, true); !strings.Contains(got, tc.wantKey) {
				t.Fatalf("row cluster = %q, want the assist key %q", got, tc.wantKey)
			}
			if panes := cache.assistPanes(setID); len(panes) != 2 || panes[0].slot != 1 || panes[1].slot != 2 {
				t.Fatalf("assist panes = %+v, want both slots in order", panes)
			}
		})
	}
}

// The Assist pane menu is one menu over both kinds. A map row's assist verb
// lists the Map's own assist panes on the same digits, and a digit lands in the
// Map's session on that slot rather than in the shared drain window (ADR-0263).
func TestAssistMenuOverAMapRowReachesTheMapsSession(t *testing.T) {
	d, cfg, row, f, _ := wayfinderSpawnFixture(t)
	f.Inside = true
	m := newQueueDashboard(d, cfg, DashboardSnapshot{Containers: []DashboardRow{row}})

	got := openAssistMenuOverRow(t, m, map[tmuxmod.PaneSlot]livePaneState{
		1: livePaneRunning,
		3: livePaneIdle,
	}, row.ID)
	view := got.View().Content
	if !strings.Contains(view, livePaneRunningStyle.Render("1")) || !strings.Contains(view, livePaneIdleStyle().Render("3")) {
		t.Fatalf("a map row's menu must colour the Map's own panes by slot:\n%s", view)
	}

	updated, cmd := got.update(tea.KeyPressMsg{Code: '3', Text: "3"})
	if cmd == nil {
		t.Fatalf("a digit did not launch:\n%s", updated.(QueueDashboard).View().Content)
	}
	handoff, ok := cmd().(dashboardHandoffMsg)
	if !ok || handoff.err != nil || !handoff.quit {
		t.Fatalf("handoff = %+v (%T), want a quit into the Map's assist pane", handoff, cmd())
	}
	panes := f.Windows[wayfinderMapSession()][mapPaneWindow]
	if len(panes) != 1 {
		t.Fatalf("panes = %v, want the one pane the digit addressed", f.Windows[wayfinderMapSession()])
	}
	if slot, _ := f.PaneTagValue(panes[0], tmuxmod.TagSlot); slot != "3" {
		t.Fatalf("pane slot = %q, want the digit that was pressed", slot)
	}
	if mapID, _ := f.PaneTagValue(panes[0], tmuxmod.TagAssist); mapID != row.ID {
		t.Fatalf("pane tag = %q, want the map id", mapID)
	}
}
