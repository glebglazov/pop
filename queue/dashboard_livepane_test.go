package queue

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	tmuxmod "github.com/glebglazov/pop/internal/tmux"
)

// TestLivePaneMenuKeyColours asserts handoff keys render dark/grey/green from
// the cached live-pane state, and that shell stays dark (ADR-0158).
func TestLivePaneMenuKeyColours(t *testing.T) {
	setID := "2026-07-31-live"
	row := DashboardRow{SetRef: SetRef{SetID: setID, Bound: true, RuntimePath: "/wt"}}
	live := livePaneCache{}
	live.set(tmuxmod.TagSet, setID, livePaneRunning)
	live.set(tmuxmod.TagAssist, setID, livePaneIdle)

	menu := newDashboardMenu(row)
	lines := dashboardMenuLines(menu, 80, live)
	joined := strings.Join(lines, "\n")

	greenI := livePaneRunningStyle.Render("i")
	greyS := livePaneIdleStyle.Render("S")
	if !strings.Contains(joined, greenI) {
		t.Fatalf("drain key must be green when running:\n%s", joined)
	}
	if !strings.Contains(joined, greyS) {
		t.Fatalf("assist key must be grey when idle:\n%s", joined)
	}
	if strings.Contains(joined, livePaneRunningStyle.Render("O")) || strings.Contains(joined, livePaneIdleStyle.Render("O")) {
		t.Fatalf("shell key must stay dark:\n%s", joined)
	}
	if !strings.Contains(joined, "O  shell") {
		t.Fatalf("shell verb missing:\n%s", joined)
	}
}

// TestLivePanePreviewVerbGone asserts the preview verb and its key are unbound.
func TestLivePanePreviewVerbGone(t *testing.T) {
	for _, item := range dashboardMenuItems(DashboardRow{SetRef: SetRef{SetID: "x"}}) {
		if item.key == "p" || item.label == "preview" {
			t.Fatalf("preview must be gone, found %+v", item)
		}
	}
	m := newQueueDashboard(&Deps{}, nil, DashboardSnapshot{Rows: []DashboardRow{
		{CursorKey: "p\x00x", SetRef: SetRef{SetID: "x"}},
	}})
	updated, _ := m.Update(tea.KeyPressMsg{Code: 'a', Text: "a"})
	got := updated.(QueueDashboard)
	_, cmd := got.Update(tea.KeyPressMsg{Code: 'p', Text: "p"})
	if cmd != nil {
		t.Fatal("p must be unbound in the action menu")
	}
}

// TestLivePaneGreenKeyJumpsWithoutResend asserts a running tagged pane is a
// jump target: no send-keys, no split.
func TestLivePaneGreenKeyJumpsWithoutResend(t *testing.T) {
	repo, setID, _ := setupSupervisorSpawnRepo(t, "live-green", []spawnTestTask{
		{ID: "01-a", File: "01-a.md", Title: "A", Type: "AFK", Status: "done"},
	})
	d, cfg, row, rt := dashboardLaunchFixture(t, repo, setID)
	row.RuntimePath = repo
	row.ProjectPath = repo
	rt.hasSession = true
	rt.windowNames["pop-queue"] = true
	seedTaggedPane(rt, "%5", tmuxmod.TagAssist, setID)
	rt.Fake.PaneInfos = map[string]tmuxmod.PaneInfo{
		"%5": {Session: "proj", Command: "node"},
	}

	result, err := LaunchAssist(d, cfg, row.SetRef)
	if err != nil {
		t.Fatal(err)
	}
	if result.PaneID != "%5" {
		t.Fatalf("PaneID = %q, want %%5", result.PaneID)
	}
	if rt.countCommand("send-keys") != 0 {
		t.Fatalf("green key must not re-send, commands=%v", rt.commands)
	}
	if rt.countCommand("split-window") != 0 {
		t.Fatalf("green key must not spawn, commands=%v", rt.commands)
	}
}

// TestLivePaneGreyKeyRespawns asserts an idle (bare shell) tagged pane is
// respawned via send-keys without splitting a twin.
func TestLivePaneGreyKeyRespawns(t *testing.T) {
	repo, setID, _ := setupSupervisorSpawnRepo(t, "live-grey", []spawnTestTask{
		{ID: "01-a", File: "01-a.md", Title: "A", Type: "AFK", Status: "done"},
	})
	d, cfg, row, rt := dashboardLaunchFixture(t, repo, setID)
	row.RuntimePath = repo
	row.ProjectPath = repo
	rt.hasSession = true
	rt.windowNames["pop-queue"] = true
	seedTaggedPane(rt, "%5", tmuxmod.TagAssist, setID)
	rt.Fake.PaneInfos = map[string]tmuxmod.PaneInfo{
		"%5": {Session: "proj", Command: "zsh"},
	}
	rt.Fake.PaneCwd = map[string]string{"%5": repo}

	result, err := LaunchAssist(d, cfg, row.SetRef)
	if err != nil {
		t.Fatal(err)
	}
	if result.PaneID != "%5" {
		t.Fatalf("PaneID = %q, want reused %%5", result.PaneID)
	}
	if rt.countCommand("send-keys") != 1 {
		t.Fatalf("grey key must respawn via send-keys, commands=%v", rt.commands)
	}
	if rt.countCommand("split-window") != 0 {
		t.Fatalf("grey key must not split a new pane, commands=%v", rt.commands)
	}
}

// TestLivePaneCacheFromTmux asserts loadLivePaneCache maps tagged panes to
// running/idle from tmux — not from the DrainPane store.
func TestLivePaneCacheFromTmux(t *testing.T) {
	rt := newRecordingTmux(false, "0")
	seedTaggedPane(rt, "%1", tmuxmod.TagSet, "set-run")
	seedTaggedPane(rt, "%2", tmuxmod.TagVerify, "set-idle")
	rt.Fake.PaneInfos = map[string]tmuxmod.PaneInfo{
		"%1": {Command: "claude"},
		"%2": {Command: "bash"},
	}
	cache := loadLivePaneCache(&Deps{Tmux: rt})
	if got := cache.state(tmuxmod.TagSet, "set-run"); got != livePaneRunning {
		t.Fatalf("running drain = %v, want running", got)
	}
	if got := cache.state(tmuxmod.TagVerify, "set-idle"); got != livePaneIdle {
		t.Fatalf("idle verify = %v, want idle", got)
	}
	if got := cache.state(tmuxmod.TagFold, "set-run"); got != livePaneNone {
		t.Fatalf("absent fold = %v, want none", got)
	}
}

// TestLivePaneMenuReloadUsesCache asserts a dashboardRowsMsg carrying live
// state colours the open menu from the cache.
func TestLivePaneMenuReloadUsesCache(t *testing.T) {
	setID := "set-cache"
	row := DashboardRow{CursorKey: "p\x00" + setID, Project: "p", SetRef: SetRef{SetID: setID}}
	m := newQueueDashboard(&Deps{}, nil, DashboardSnapshot{Rows: []DashboardRow{row}})
	updated, _ := m.Update(tea.KeyPressMsg{Code: 'a', Text: "a"})
	got := updated.(QueueDashboard)

	live := livePaneCache{}
	live.set(tmuxmod.TagAssist, setID, livePaneRunning)
	updated, _ = got.Update(dashboardRowsMsg{
		snap: DashboardSnapshot{Rows: []DashboardRow{row}},
		live: live,
	})
	got = updated.(QueueDashboard)
	if got.live.state(tmuxmod.TagAssist, setID) != livePaneRunning {
		t.Fatalf("live cache not stored on reload")
	}
	view := got.View().Content
	if !strings.Contains(view, livePaneRunningStyle.Render("S")) {
		t.Fatalf("menu must colour assist green from cached live:\n%s", view)
	}
}

// TestLivePaneDarkSpawnsFresh covers the no-pane path still splits and sends.
func TestLivePaneDarkSpawnsFresh(t *testing.T) {
	repo, setID, _ := setupSupervisorSpawnRepo(t, "live-dark", []spawnTestTask{
		{ID: "01-a", File: "01-a.md", Title: "A", Type: "AFK", Status: "done"},
	})
	d, cfg, row, rt := dashboardLaunchFixture(t, repo, setID)
	row.RuntimePath = repo
	row.ProjectPath = repo

	if _, err := LaunchAssist(d, cfg, row.SetRef); err != nil {
		t.Fatal(err)
	}
	if rt.countCommand("send-keys") != 1 {
		t.Fatalf("dark key must spawn+send, commands=%v", rt.commands)
	}
}

func TestLivePaneShellAlwaysDarkInMenu(t *testing.T) {
	row := DashboardRow{SetRef: SetRef{SetID: "s", RuntimePath: "/wt", Bound: true}}
	live := livePaneCache{}
	live.set(tmuxmod.TagSet, "s", livePaneRunning)
	menu := newDashboardMenu(row)
	for _, item := range menu.list.Items() {
		if item.action == menuActionShell {
			if st := menuItemLiveState(item, row, live); st != livePaneNone {
				t.Fatalf("shell live state = %v, want none", st)
			}
		}
	}
}

// TestLivePaneReloadFillsCache asserts reload builds the live cache from tmux
// in the same poll as the snapshot rebuild.
func TestLivePaneReloadFillsCache(t *testing.T) {
	rec := newRecordingTmux(false, "0")
	seedTaggedPane(rec, "%1", tmuxmod.TagSet, "set-a")
	rec.Fake.PaneInfos = map[string]tmuxmod.PaneInfo{"%1": {Command: "node"}}
	d := &Deps{Tmux: rec}
	// loadLivePaneCache is what reload embeds alongside BuildSnapshot.
	live := loadLivePaneCache(d)
	if live.state(tmuxmod.TagSet, "set-a") != livePaneRunning {
		t.Fatalf("live = %v, want running for set-a", live.state(tmuxmod.TagSet, "set-a"))
	}
	msg := dashboardRowsMsg{live: live}
	m := newQueueDashboard(d, nil, DashboardSnapshot{})
	updated, _ := m.Update(msg)
	got := updated.(QueueDashboard)
	if got.live.state(tmuxmod.TagSet, "set-a") != livePaneRunning {
		t.Fatalf("model live after rows msg = %v", got.live.state(tmuxmod.TagSet, "set-a"))
	}
}
