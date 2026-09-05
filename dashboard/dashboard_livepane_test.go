package dashboard

import (
	"github.com/glebglazov/pop/internal/queuetest"
	"github.com/glebglazov/pop/tasks/drain"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	tmuxmod "github.com/glebglazov/pop/internal/tmux"
	"github.com/glebglazov/pop/tasks/setkind"
	"github.com/glebglazov/pop/work"
)

// TestLivePaneMenuKeyColours asserts handoff keys render dark/grey/green from
// the cached live-pane state, and that shell stays dark (ADR-0158).
func TestLivePaneMenuKeyColours(t *testing.T) {
	setID := "2026-07-31-live"
	row := DashboardRow{ID: setID, Bound: true, RuntimePath: "/wt"}
	live := livePaneCache{}
	live.set(tmuxmod.TagSet, setID, livePaneRunning)
	live.set(tmuxmod.TagAssist, setID, livePaneIdle)

	menu := newDashboardMenu(testKinds(), row)
	lines := dashboardMenuLines(menu, 80, live)
	joined := strings.Join(lines, "\n")

	greenI := livePaneRunningStyle.Render("I")
	greyA := livePaneIdleStyle().Render("A")
	if !strings.Contains(joined, greenI) {
		t.Fatalf("drain key must be green when running:\n%s", joined)
	}
	if !strings.Contains(joined, greyA) {
		t.Fatalf("assist key must be grey when idle:\n%s", joined)
	}
	if strings.Contains(joined, livePaneRunningStyle.Render("O")) || strings.Contains(joined, livePaneIdleStyle().Render("O")) {
		t.Fatalf("shell key must stay dark:\n%s", joined)
	}
	if !strings.Contains(joined, "O  shell") {
		t.Fatalf("shell verb missing:\n%s", joined)
	}
}

// TestLivePanePreviewVerbGone asserts the preview verb and its key are unbound.
func TestLivePanePreviewVerbGone(t *testing.T) {
	for _, item := range dashboardMenuItems(testKinds(), DashboardRow{ID: "x"}) {
		if item.key == "p" || item.label == "preview" {
			t.Fatalf("preview must be gone, found %+v", item)
		}
	}
	m := newQueueDashboard(&drain.Deps{}, nil, DashboardSnapshot{Containers: []DashboardRow{
		{CursorKey: "p\x00x", ID: "x"},
	}})
	updated, _ := m.Update(tea.KeyPressMsg{Code: 'r', Text: "r"})
	got := updated.(QueueDashboard)
	_, cmd := got.Update(tea.KeyPressMsg{Code: 'p', Text: "p"})
	if cmd != nil {
		t.Fatal("p must be unbound in the Run menu")
	}
}

// TestLivePaneGreenKeyJumpsWithoutResend asserts a running tagged pane is a
// jump target: no send-keys, no split.
func TestLivePaneGreenKeyJumpsWithoutResend(t *testing.T) {
	repo, setID, _ := queuetest.SetupSpawnRepo(t, "live-green", []queuetest.SpawnTask{
		{ID: "01-a", File: "01-a.md", Title: "A", Type: "AFK", Status: "done"},
	})
	d, cfg, row, rt := dashboardLaunchFixture(t, repo, setID)
	row.RuntimePath = repo
	row.ProjectPath = repo
	rt.SessionLive = true
	rt.WindowNames["pop-work"] = true
	seedTaggedPane(rt, "%5", tmuxmod.TagAssist, setID)
	rt.Fake.PaneInfos = map[string]tmuxmod.PaneInfo{
		"%5": {Session: "proj", Command: "node"},
	}

	result, err := drain.LaunchAssist(d, cfg, row)
	if err != nil {
		t.Fatal(err)
	}
	if result.PaneID != "%5" {
		t.Fatalf("PaneID = %q, want %%5", result.PaneID)
	}
	if rt.CountCommand("send-keys") != 0 {
		t.Fatalf("green key must not re-send, commands=%v", rt.Commands)
	}
	if rt.CountCommand("split-window") != 0 {
		t.Fatalf("green key must not spawn, commands=%v", rt.Commands)
	}
}

// TestLivePaneGreyKeyRespawns asserts an idle (bare shell) tagged pane is
// respawned via send-keys without splitting a twin.
func TestLivePaneGreyKeyRespawns(t *testing.T) {
	repo, setID, _ := queuetest.SetupSpawnRepo(t, "live-grey", []queuetest.SpawnTask{
		{ID: "01-a", File: "01-a.md", Title: "A", Type: "AFK", Status: "done"},
	})
	d, cfg, row, rt := dashboardLaunchFixture(t, repo, setID)
	row.RuntimePath = repo
	row.ProjectPath = repo
	rt.SessionLive = true
	rt.WindowNames["pop-work"] = true
	seedTaggedPane(rt, "%5", tmuxmod.TagAssist, setID)
	rt.Fake.PaneInfos = map[string]tmuxmod.PaneInfo{
		"%5": {Session: "proj", Command: "zsh"},
	}
	rt.Fake.PaneCwd = map[string]string{"%5": repo}

	result, err := drain.LaunchAssist(d, cfg, row)
	if err != nil {
		t.Fatal(err)
	}
	if result.PaneID != "%5" {
		t.Fatalf("PaneID = %q, want reused %%5", result.PaneID)
	}
	if rt.CountCommand("send-keys") != 1 {
		t.Fatalf("grey key must respawn via send-keys, commands=%v", rt.Commands)
	}
	if rt.CountCommand("split-window") != 0 {
		t.Fatalf("grey key must not split a new pane, commands=%v", rt.Commands)
	}
}

// TestLivePaneCacheFromTmux asserts loadLivePaneCache maps tagged panes to
// running/idle from tmux — not from the DrainPane store.
func TestLivePaneCacheFromTmux(t *testing.T) {
	rt := queuetest.NewRecordingTmux(false, "0")
	seedTaggedPane(rt, "%1", tmuxmod.TagSet, "set-run")
	seedTaggedPane(rt, "%2", tmuxmod.TagVerify, "set-idle")
	rt.Fake.PaneInfos = map[string]tmuxmod.PaneInfo{
		"%1": {Command: "claude"},
		"%2": {Command: "bash"},
	}
	cache := loadLivePaneCache(&drain.Deps{Tmux: rt})
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
	row := DashboardRow{CursorKey: "p\x00" + setID, Project: "p", ID: setID}
	m := newQueueDashboard(&drain.Deps{}, nil, DashboardSnapshot{Containers: []DashboardRow{row}})
	updated, _ := m.Update(tea.KeyPressMsg{Code: 'r', Text: "r"})
	got := updated.(QueueDashboard)

	live := livePaneCache{}
	live.set(tmuxmod.TagAssist, setID, livePaneRunning)
	updated, _ = got.Update(dashboardRowsMsg{
		snap: DashboardSnapshot{Containers: []DashboardRow{row}},
		live: live,
	})
	got = updated.(QueueDashboard)
	if got.liveCache().state(tmuxmod.TagAssist, setID) != livePaneRunning {
		t.Fatalf("live cache not stored on reload")
	}
	view := got.View().Content
	if !strings.Contains(view, livePaneRunningStyle.Render("A")) {
		t.Fatalf("menu must colour assist green from cached live:\n%s", view)
	}
}

// TestLivePaneDarkSpawnsFresh covers the no-pane path still splits and sends.
func TestLivePaneDarkSpawnsFresh(t *testing.T) {
	repo, setID, _ := queuetest.SetupSpawnRepo(t, "live-dark", []queuetest.SpawnTask{
		{ID: "01-a", File: "01-a.md", Title: "A", Type: "AFK", Status: "done"},
	})
	d, cfg, row, rt := dashboardLaunchFixture(t, repo, setID)
	row.RuntimePath = repo
	row.ProjectPath = repo

	if _, err := drain.LaunchAssist(d, cfg, row); err != nil {
		t.Fatal(err)
	}
	if rt.CountCommand("send-keys") != 1 {
		t.Fatalf("dark key must spawn+send, commands=%v", rt.Commands)
	}
}

func TestLivePaneShellAlwaysDarkInMenu(t *testing.T) {
	row := DashboardRow{ID: "s", RuntimePath: "/wt", Bound: true}
	live := livePaneCache{}
	live.set(tmuxmod.TagSet, "s", livePaneRunning)
	menu := newDashboardMenu(testKinds(), row)
	for _, item := range menu.list.Items() {
		if item.verb == work.VerbShell {
			if st := menuItemLiveState(item, row, live); st != livePaneNone {
				t.Fatalf("shell live state = %v, want none", st)
			}
		}
	}
}

// TestLivePaneReloadFillsCache asserts reload builds the live cache from tmux
// in the same poll as the snapshot rebuild.
func TestLivePaneReloadFillsCache(t *testing.T) {
	rec := queuetest.NewRecordingTmux(false, "0")
	seedTaggedPane(rec, "%1", tmuxmod.TagSet, "set-a")
	rec.Fake.PaneInfos = map[string]tmuxmod.PaneInfo{"%1": {Command: "node"}}
	d := &drain.Deps{Tmux: rec}
	// loadLivePaneCache is what reload embeds alongside BuildSnapshot.
	live := loadLivePaneCache(d)
	if live.state(tmuxmod.TagSet, "set-a") != livePaneRunning {
		t.Fatalf("live = %v, want running for set-a", live.state(tmuxmod.TagSet, "set-a"))
	}
	msg := dashboardRowsMsg{live: live}
	m := newQueueDashboard(d, nil, DashboardSnapshot{})
	updated, _ := m.Update(msg)
	got := updated.(QueueDashboard)
	if got.liveCache().state(tmuxmod.TagSet, "set-a") != livePaneRunning {
		t.Fatalf("model live after rows msg = %v", got.liveCache().state(tmuxmod.TagSet, "set-a"))
	}
}

// TestLivePaneRowClusterMatchesMenu asserts the row activity cluster uses the
// same dark/grey/green scheme as the action-menu handoff keys (ADR-0158).
func TestLivePaneRowClusterMatchesMenu(t *testing.T) {
	setID := "2026-07-31-cluster"
	row := DashboardRow{ID: setID, Bound: true, RuntimePath: "/wt"}
	live := livePaneCache{}
	live.set(tmuxmod.TagSet, setID, livePaneRunning)
	live.set(tmuxmod.TagVerify, setID, livePaneIdle)
	live.set(tmuxmod.TagFold, setID, livePaneNone)
	live.set(tmuxmod.TagAssist, setID, livePaneRunning)

	cluster := dashboardActivityCluster(row, live, true)
	menu := newDashboardMenu(testKinds(), row)
	lines := dashboardMenuLines(menu, 80, live)
	joined := strings.Join(lines, "\n")

	for _, item := range menu.list.Items() {
		switch item.verb {
		case setkind.VerbDrain, setkind.VerbVerify, setkind.VerbFold, setkind.VerbAssist:
			want := styleHandoffKey(item.key, menuItemLiveState(item, row, live))
			if !strings.Contains(cluster, want) {
				t.Fatalf("cluster missing %q styled like menu for %s:\ncluster=%q\nmenu=%q", want, item.label, cluster, joined)
			}
		}
	}
}

// TestLivePaneRowClusterInView asserts the main dashboard view colours the row
// cluster from the cached per-poll liveness without an extra tmux query.
func TestLivePaneRowClusterInView(t *testing.T) {
	setID := "set-row-cluster"
	row := DashboardRow{CursorKey: "p\x00" + setID, Project: "p", ID: setID}
	m := newQueueDashboard(&drain.Deps{}, nil, DashboardSnapshot{Containers: []DashboardRow{row}})

	live := livePaneCache{}
	live.set(tmuxmod.TagSet, setID, livePaneRunning)
	live.set(tmuxmod.TagAssist, setID, livePaneIdle)
	updated, _ := m.Update(dashboardRowsMsg{
		snap: DashboardSnapshot{Containers: []DashboardRow{row}},
		live: live,
	})
	got := updated.(QueueDashboard)
	view := got.View().Content
	if !strings.Contains(view, livePaneRunningStyle.Render("I")) {
		t.Fatalf("view must show green drain in row cluster:\n%s", view)
	}
	if !strings.Contains(view, livePaneIdleStyle().Render("A")) {
		t.Fatalf("view must show grey assist in row cluster:\n%s", view)
	}
}

// TestLivePaneRowClusterClearsOnReload asserts a pane dying is reflected on the
// row by the next poll — the cluster goes dark when tmux no longer reports it.
func TestLivePaneRowClusterClearsOnReload(t *testing.T) {
	setID := "set-died"
	row := DashboardRow{CursorKey: "p\x00" + setID, Project: "p", ID: setID}
	m := newQueueDashboard(&drain.Deps{}, nil, DashboardSnapshot{Containers: []DashboardRow{row}})

	live := livePaneCache{}
	live.set(tmuxmod.TagSet, setID, livePaneRunning)
	updated, _ := m.Update(dashboardRowsMsg{
		snap: DashboardSnapshot{Containers: []DashboardRow{row}},
		live: live,
	})
	got := updated.(QueueDashboard)
	if !strings.Contains(got.View().Content, livePaneRunningStyle.Render("I")) {
		t.Fatal("expected green drain before pane died")
	}

	updated, _ = got.Update(dashboardRowsMsg{
		snap: DashboardSnapshot{Containers: []DashboardRow{row}},
		live: livePaneCache{},
	})
	got = updated.(QueueDashboard)
	view := got.View().Content
	if strings.Contains(view, livePaneRunningStyle.Render("I")) || strings.Contains(view, livePaneIdleStyle().Render("I")) {
		t.Fatalf("cluster must be dark after pane died:\n%s", view)
	}
	if !strings.Contains(view, dashboardActivityClusterPlain) {
		t.Fatalf("cluster letters must remain:\n%s", view)
	}
}
