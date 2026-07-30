package queue

import (
	"errors"
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

// TestDashboardAssistMenuPlacement asserts Assist sits with preview and shell,
// not among the drain/verify work verbs.
func TestDashboardAssistMenuPlacement(t *testing.T) {
	items := dashboardMenuItems(DashboardRow{SetRef: SetRef{SetID: "demo"}})
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
	p, status, assist, o, i, v := idx("p"), idx("s"), idx("S"), idx("O"), idx("i"), idx("v")
	if p < 0 || status < 0 || assist < 0 || o < 0 {
		t.Fatalf("menu missing preview/status/assist/shell keys: %+v", keys)
	}
	if !(p < status && status < assist && assist < o) {
		t.Fatalf("assist must sit between status submenu and shell, got keys %v", keys)
	}
	if i >= 0 && i > assist {
		t.Fatalf("assist must not follow drain, got keys %v", keys)
	}
	if v >= 0 && v > assist {
		t.Fatalf("assist must not follow verify, got keys %v", keys)
	}

	m := newQueueDashboard(nil, nil, DashboardSnapshot{Rows: []DashboardRow{
		{SetRef: SetRef{SetID: "demo"}},
	}})
	updated, _ := m.Update(tea.KeyPressMsg{Code: 'a', Text: "a"})
	got := updated.(QueueDashboard)
	view := got.View().Content
	if !strings.Contains(view, "S  assist") {
		t.Fatalf("menu view missing assist verb:\n%s", view)
	}
}

func extractAssistSpawnCommand(rt *recordingTmux) (string, bool) {
	sendKeys, ok := rt.findCommand("send-keys")
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

// TestDashboardLaunchAssistSpawnsTaggedPane asserts LaunchAssist creates a
// pop-queue pane tagged @pop_assist, titled <set>-assist, running assist.
func TestDashboardLaunchAssistSpawnsTaggedPane(t *testing.T) {
	repo, setID, _ := setupSupervisorSpawnRepo(t, "assist-spawn", []spawnTestTask{
		{ID: "01-a", File: "01-a.md", Title: "A", Type: "AFK", Status: "done"},
	})
	d, cfg, row, rt := dashboardLaunchFixture(t, repo, setID)
	row.RuntimePath = repo
	row.ProjectPath = repo

	if _, err := LaunchAssist(d, cfg, row.SetRef); err != nil {
		t.Fatalf("LaunchAssist: %v", err)
	}

	cmd, ok := extractAssistSpawnCommand(rt)
	if !ok {
		t.Fatalf("no assist spawn command recorded; commands=%v", rt.commands)
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
	if title := rt.Fake.PaneTitles["%3"]; title != assistPaneTitle(setID) {
		t.Fatalf("pane title = %q, want %q", title, assistPaneTitle(setID))
	}
}

// TestDashboardLaunchAssistBoundCheckoutUsesProjectSession asserts assist panes
// for a bound non-trunk checkout open in the project's session with the binding
// as cwd — never a worktree-derived session.
func TestDashboardLaunchAssistBoundCheckoutUsesProjectSession(t *testing.T) {
	repo, setID, _ := setupSupervisorSpawnRepo(t, "assist-bound", []spawnTestTask{
		{ID: "01-a", File: "01-a.md", Title: "A", Type: "AFK", Status: "done"},
	})
	bound := filepath.Join(t.TempDir(), "assist-bound-wt")
	runGit(t, repo, "worktree", "add", "--detach", bound, "HEAD")
	d, cfg, row, rt := dashboardLaunchFixture(t, repo, setID)
	repoKey, err := resolveRepoKey(d, repo)
	if err != nil {
		t.Fatal(err)
	}
	seedBindingStore(t, d.Tasks, map[string]WorktreeBinding{
		setScopedKey(repoKey, setID): {RuntimePath: bound, Branch: "assist-bound", Project: "pop", Provisioned: false},
	})
	row.RuntimePath = bound
	row.ProjectPath = repo

	if _, err := LaunchAssist(d, cfg, row.SetRef); err != nil {
		t.Fatalf("LaunchAssist: %v", err)
	}
	assertSetPaneProjectSessionAndCheckout(t, rt, repo, bound)
}

// TestDashboardPreviewAssistFindsBoundCheckoutPane asserts preview still focuses
// an assist pane after it was opened in the project session at a bound checkout.
func TestDashboardPreviewAssistFindsBoundCheckoutPane(t *testing.T) {
	repo, setID, _ := setupSupervisorSpawnRepo(t, "assist-preview", []spawnTestTask{
		{ID: "01-a", File: "01-a.md", Title: "A", Type: "AFK", Status: "done"},
	})
	bound := filepath.Join(t.TempDir(), "assist-preview-wt")
	runGit(t, repo, "worktree", "add", "--detach", bound, "HEAD")
	d, cfg, row, rt := dashboardLaunchFixture(t, repo, setID)
	repoKey, err := resolveRepoKey(d, repo)
	if err != nil {
		t.Fatal(err)
	}
	seedBindingStore(t, d.Tasks, map[string]WorktreeBinding{
		setScopedKey(repoKey, setID): {RuntimePath: bound, Branch: "assist-preview", Project: "pop", Provisioned: false},
	})
	row.RuntimePath = bound
	row.ProjectPath = repo

	if _, err := LaunchAssist(d, cfg, row.SetRef); err != nil {
		t.Fatalf("LaunchAssist: %v", err)
	}
	if pane := rt.Fake.PaneTagValues["%3"][tmuxmod.TagAssist]; pane != setID {
		t.Fatalf("@pop_assist = %q, want %q", pane, setID)
	}
	rt.paneList = setID + " %3"

	if err := PreviewDrain(d, SetRef{SetID: setID, ProjectPath: repo, RuntimePath: bound}); err != nil {
		t.Fatalf("PreviewDrain: %v", err)
	}
	if !rt.findSwitched("%3") {
		t.Fatalf("preview must focus assist pane, commands=%v", rt.commands)
	}
	if paneID, err := assistPaneID(d, SetRef{SetID: setID, ProjectPath: repo, RuntimePath: bound}); err != nil {
		t.Fatal(err)
	} else if paneID != "%3" {
		t.Fatalf("assistPaneID = %q, want %%3 in project session", paneID)
	}
}

// TestDashboardLaunchAssistReusesPane asserts a second LaunchAssist on a set
// with a live assist pane focuses it instead of splitting again.
func TestDashboardLaunchAssistReusesPane(t *testing.T) {
	repo, setID, _ := setupSupervisorSpawnRepo(t, "assist-reuse", []spawnTestTask{
		{ID: "01-a", File: "01-a.md", Title: "A", Type: "AFK", Status: "done"},
	})
	d, cfg, row, rt := dashboardLaunchFixture(t, repo, setID)
	row.RuntimePath = repo
	row.ProjectPath = repo
	rt.hasSession = true
	rt.windowNames["pop-queue"] = true
	seedTaggedPane(rt, "%5", tmuxmod.TagAssist, setID)

	result, err := LaunchAssist(d, cfg, row.SetRef)
	if err != nil {
		t.Fatalf("LaunchAssist reuse: %v", err)
	}
	if result.PaneID != "%5" {
		t.Fatalf("reuse PaneID = %q, want %%5", result.PaneID)
	}
	if rt.countCommand("split-window") != 0 {
		t.Fatalf("reuse must not split, commands=%v", rt.commands)
	}
	if rt.countCommand("send-keys") != 0 {
		t.Fatalf("reuse must not re-send assist command, commands=%v", rt.commands)
	}
	// Focus belongs to the dashboard handoff path; LaunchAssist only returns the
	// existing pane id.
	if rt.findSwitched("%5") {
		t.Fatalf("LaunchAssist must not focus; handoff does, commands=%v", rt.commands)
	}
}

func (rt *recordingTmux) findSwitched(target string) bool {
	for _, c := range rt.commands {
		if len(c) >= 2 && c[0] == "switch-client" && c[len(c)-1] == target {
			return true
		}
		if len(c) >= 2 && c[0] == "select-pane" && c[len(c)-1] == target {
			return true
		}
	}
	return false
}

// TestLaunchAssistRefusesLiveDrain asserts LaunchAssist applies the same live-drain
// refusal as the CLI entry.
func TestLaunchAssistRefusesLiveDrain(t *testing.T) {
	repo, setID, _ := setupSupervisorSpawnRepo(t, "assist-refuse", []spawnTestTask{
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

	_, err = LaunchAssist(d, cfg, row.SetRef)
	if err == nil {
		t.Fatal("LaunchAssist must refuse while drain is live")
	}
	if !strings.Contains(err.Error(), "live drain") {
		t.Fatalf("refusal must name live drain: %v", err)
	}
}

// TestDashboardAssistRefusalSticky asserts a refused assist surfaces as a sticky
// dashboard action error.
func TestDashboardAssistRefusalSticky(t *testing.T) {
	const refusal = `task set "demo" has a live drain (pid 9 on /repo)`
	row := TestDashboardRow("proj", "demo", SetRef{SetID: "demo", RuntimePath: "/repo/wt"})
	m := newQueueDashboard(&Deps{}, &config.Config{}, DashboardSnapshot{Rows: []DashboardRow{row}})
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
