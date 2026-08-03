package dashboard

import (
	"errors"
	"github.com/glebglazov/pop/internal/queuetest"
	"github.com/glebglazov/pop/tasks/drain"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/glebglazov/pop/config"
	tmuxmod "github.com/glebglazov/pop/internal/tmux"
	"github.com/glebglazov/pop/store"
	"github.com/glebglazov/pop/tasks"
)

// TestDashboardAssistMenuPlacement asserts Assist sits with status and shell,
// not among the drain/verify work verbs.
func TestDashboardAssistMenuPlacement(t *testing.T) {
	items := dashboardMenuItems(testKinds(), DashboardRow{ID: "demo"})
	var keys []string
	for _, item := range items {
		keys = append(keys, item.key)
	}
	idx := func(key string) int {
		for i, k := range keys {
			if k == key {
				return i
			}
		}
		return -1
	}
	status, assist, o, drain, verify, p := idx("s"), idx("S"), idx("O"), idx("I"), idx("V"), idx("p")
	if status < 0 || assist < 0 || o < 0 {
		t.Fatalf("menu missing status/assist/shell keys: %+v", keys)
	}
	if p >= 0 {
		t.Fatalf("preview verb must be gone, got keys %v", keys)
	}
	if !(status < assist && assist < o) {
		t.Fatalf("assist must sit between status submenu and shell, got keys %v", keys)
	}
	if drain >= 0 && drain > assist {
		t.Fatalf("assist must not follow drain, got keys %v", keys)
	}
	if verify >= 0 && verify > assist {
		t.Fatalf("assist must not follow verify, got keys %v", keys)
	}

	m := newQueueDashboard(nil, nil, DashboardSnapshot{Containers: []DashboardRow{
		{ID: "demo"},
	}})
	updated, _ := m.Update(tea.KeyPressMsg{Code: 'a', Text: "a"})
	got := updated.(QueueDashboard)
	view := got.View().Content
	if !strings.Contains(view, "S  assist") {
		t.Fatalf("menu view missing assist verb:\n%s", view)
	}
}

func extractAssistSpawnCommand(rt *queuetest.RecordingTmux) (string, bool) {
	sendKeys, ok := rt.FindCommand("send-keys")
	if !ok {
		return "", false
	}
	joined := strings.Join(sendKeys, " ")
	idx := strings.Index(joined, "pop tasks assist ")
	if idx < 0 {
		return "", false
	}
	cmd := joined[idx:]
	if end := strings.Index(cmd, " Enter"); end >= 0 {
		cmd = cmd[:end]
	}
	return cmd, true
}

// TestDashboardLaunchAssistSpawnsTaggedPane asserts drain.LaunchAssist creates a
// pop-work pane tagged @pop_assist, titled <set>-assist, running assist.
func TestDashboardLaunchAssistSpawnsTaggedPane(t *testing.T) {
	repo, setID, _ := queuetest.SetupSpawnRepo(t, "assist-spawn", []queuetest.SpawnTask{
		{ID: "01-a", File: "01-a.md", Title: "A", Type: "AFK", Status: "done"},
	})
	d, cfg, row, rt := dashboardLaunchFixture(t, repo, setID)
	row.RuntimePath = repo
	row.ProjectPath = repo

	if _, err := drain.LaunchAssist(d, cfg, row); err != nil {
		t.Fatalf("drain.LaunchAssist: %v", err)
	}

	cmd, ok := extractAssistSpawnCommand(rt)
	if !ok {
		t.Fatalf("no assist spawn command recorded; commands=%v", rt.Commands)
	}
	if !strings.Contains(cmd, "pop tasks assist "+setID) {
		t.Fatalf("assist command = %q, want assist for %q", cmd, setID)
	}
	if !strings.Contains(cmd, "--task-runtime-path") || !strings.Contains(cmd, filepath.Base(repo)) {
		t.Fatalf("assist command must pin runtime path: %q", cmd)
	}

	pane := rt.Fake.PaneTagValues["%3"][tmuxmod.TagAssist]
	if pane != setID {
		t.Fatalf("@pop_assist = %q, want %q", pane, setID)
	}
	if title := rt.Fake.PaneTitles["%3"]; title != drain.AssistPaneTitle(setID) {
		t.Fatalf("pane title = %q, want %q", title, drain.AssistPaneTitle(setID))
	}
}

// TestDashboardLaunchAssistBoundCheckoutUsesProjectSession asserts assist panes
// for a bound non-trunk checkout open in the project session with the binding
// as cwd — never a worktree-derived session.
func TestDashboardLaunchAssistBoundCheckoutUsesProjectSession(t *testing.T) {
	repo, setID, _ := queuetest.SetupSpawnRepo(t, "assist-bound", []queuetest.SpawnTask{
		{ID: "01-a", File: "01-a.md", Title: "A", Type: "AFK", Status: "done"},
	})
	bound := filepath.Join(t.TempDir(), "assist-bound-wt")
	runGit(t, repo, "worktree", "add", "--detach", bound, "HEAD")
	d, cfg, row, rt := dashboardLaunchFixture(t, repo, setID)
	repoKey, err := drain.ResolveRepoKey(d, repo)
	if err != nil {
		t.Fatal(err)
	}
	queuetest.SeedBindingStore(t, d.Tasks, map[string]drain.WorktreeBinding{
		drain.SetScopedKey(repoKey, setID): {RuntimePath: bound, Branch: "assist-bound", Project: "pop", Provisioned: false},
	})
	row.RuntimePath = bound
	row.ProjectPath = repo

	if _, err := drain.LaunchAssist(d, cfg, row); err != nil {
		t.Fatalf("drain.LaunchAssist: %v", err)
	}
	assertSetPaneProjectSessionAndCheckout(t, rt, repo, bound)
}

// TestDashboardLaunchAssistReusesPane asserts a second drain.LaunchAssist on a set
// with a live assist pane focuses it instead of splitting again.
func TestDashboardLaunchAssistReusesPane(t *testing.T) {
	repo, setID, _ := queuetest.SetupSpawnRepo(t, "assist-reuse", []queuetest.SpawnTask{
		{ID: "01-a", File: "01-a.md", Title: "A", Type: "AFK", Status: "done"},
	})
	d, cfg, row, rt := dashboardLaunchFixture(t, repo, setID)
	row.RuntimePath = repo
	row.ProjectPath = repo
	rt.SessionLive = true
	rt.WindowNames["pop-work"] = true
	seedTaggedPane(rt, "%5", tmuxmod.TagAssist, setID)

	result, err := drain.LaunchAssist(d, cfg, row)
	if err != nil {
		t.Fatalf("drain.LaunchAssist reuse: %v", err)
	}
	if result.PaneID != "%5" {
		t.Fatalf("reuse PaneID = %q, want %%5", result.PaneID)
	}
	if rt.CountCommand("split-window") != 0 {
		t.Fatalf("reuse must not split, commands=%v", rt.Commands)
	}
	if rt.CountCommand("send-keys") != 0 {
		t.Fatalf("reuse must not re-send assist command, commands=%v", rt.Commands)
	}
	// Focus belongs to the dashboard handoff path; drain.LaunchAssist only returns the
	// existing pane id.
	if rt.FindSwitched("%5") {
		t.Fatalf("drain.LaunchAssist must not focus; handoff does, commands=%v", rt.Commands)
	}
}

// TestLaunchAssistRefusesLiveDrain asserts drain.LaunchAssist applies the same live-drain
// refusal as the CLI entry.
func TestLaunchAssistRefusesLiveDrain(t *testing.T) {
	repo, setID, _ := queuetest.SetupSpawnRepo(t, "assist-refuse", []queuetest.SpawnTask{
		{ID: "01-a", File: "01-a.md", Title: "A", Type: "AFK", Status: "done"},
	})
	d, cfg, row, _ := dashboardLaunchFixture(t, repo, setID)
	row.RuntimePath = repo
	row.ProjectPath = repo
	d.Tasks.ProcessAlive = func(pid int) bool { return pid == 4242 }

	s, err := store.Open(tasks.DrainStorePathWith(d.Tasks), func(int, string) bool { return true })
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.StartDrain(store.Drain{
		Repo:        "/repo/.git",
		SetID:       setID,
		RuntimePath: repo,
		PID:         4242,
		ProcStart:   "live-tok",
		StartedAt:   time.Now().UTC(),
	}); err != nil {
		t.Fatalf("StartDrain: %v", err)
	}

	_, err = drain.LaunchAssist(d, cfg, row)
	if err == nil {
		t.Fatal("drain.LaunchAssist must refuse while drain is live")
	}
	if !strings.Contains(err.Error(), "live drain") {
		t.Fatalf("refusal must name live drain: %v", err)
	}
}

// TestDashboardAssistRefusalSticky asserts a refused assist surfaces as a sticky
// dashboard action error.
func TestDashboardAssistRefusalSticky(t *testing.T) {
	const refusal = `task set "demo" has a live drain (pid 9 on /repo)`
	row := TestDashboardRow("proj", "demo", DashboardRow{ID: "demo", RuntimePath: "/repo/wt"})
	m := newQueueDashboard(&drain.Deps{}, &config.Config{}, DashboardSnapshot{Containers: []DashboardRow{row}})
	m.width, m.height = 120, 40

	updated, _ := m.Update(dashboardHandoffMsg{err: errors.New(refusal)})
	got := updated.(QueueDashboard)
	if got.actionErr == nil || got.actionErr.Error() != refusal {
		t.Fatalf("action error = %v, want %q", got.actionErr, refusal)
	}
	if view := got.View().Content; !strings.Contains(view, refusal) {
		t.Fatalf("view missing refusal:\n%s", view)
	}
}
