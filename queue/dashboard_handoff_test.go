package queue

import (
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/glebglazov/pop/project"
	"github.com/glebglazov/pop/tasks"
)

// TestDashboardHandoffAssistSpawnsFocusesAndQuits drives assist from the action
// menu key through spawn, SelectPane+SwitchClient, and tea.Quit (ADR-0158).
func TestDashboardHandoffAssistSpawnsFocusesAndQuits(t *testing.T) {
	repo, setID, _ := setupSupervisorSpawnRepo(t, "handoff-assist", []spawnTestTask{
		{ID: "01-a", File: "01-a.md", Title: "A", Type: "AFK", Status: "done"},
	})
	d, cfg, row, rt := dashboardLaunchFixture(t, repo, setID)
	row.RuntimePath = repo
	row.ProjectPath = repo
	rt.Fake.Inside = true

	m := newQueueDashboard(d, cfg, DashboardSnapshot{Rows: []DashboardRow{row}})
	updated, _ := m.Update(tea.KeyPressMsg{Code: 'a', Text: "a"})
	got := updated.(QueueDashboard)
	_, cmd := got.Update(tea.KeyPressMsg{Code: 'S', Text: "S"})
	if cmd == nil {
		t.Fatal("S did not return a command")
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
	if !rt.findSwitched("%3") {
		t.Fatalf("assist handoff must focus spawned pane, commands=%v", rt.commands)
	}

	updated, quitCmd := got.Update(handoff)
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

// TestDashboardHandoffAssistReusesWithoutResend asserts a second assist on a
// set with a live assist pane focuses it and quits without send-keys.
func TestDashboardHandoffAssistReusesWithoutResend(t *testing.T) {
	repo, setID, _ := setupSupervisorSpawnRepo(t, "handoff-assist-reuse", []spawnTestTask{
		{ID: "01-a", File: "01-a.md", Title: "A", Type: "AFK", Status: "done"},
	})
	d, cfg, row, rt := dashboardLaunchFixture(t, repo, setID)
	row.RuntimePath = repo
	row.ProjectPath = repo
	rt.hasSession = true
	rt.windowNames["pop-queue"] = true
	rt.paneList = setID + " %5"
	rt.Fake.Inside = true

	m := newQueueDashboard(d, cfg, DashboardSnapshot{Rows: []DashboardRow{row}})
	updated, _ := m.Update(tea.KeyPressMsg{Code: 'a', Text: "a"})
	got := updated.(QueueDashboard)
	_, cmd := got.Update(tea.KeyPressMsg{Code: 'S', Text: "S"})
	if cmd == nil {
		t.Fatal("S did not return a command")
	}
	msg := cmd()
	handoff, ok := msg.(dashboardHandoffMsg)
	if !ok {
		t.Fatalf("msg = %T, want dashboardHandoffMsg", msg)
	}
	if !handoff.quit || handoff.err != nil {
		t.Fatalf("handoff = %+v, want quit without err", handoff)
	}
	if rt.countCommand("send-keys") != 0 {
		t.Fatalf("reuse must not re-send assist, commands=%v", rt.commands)
	}
	if !rt.findSwitched("%5") {
		t.Fatalf("reuse must focus existing pane, commands=%v", rt.commands)
	}
}

// TestDashboardHandoffAssistOutsideTmuxStays reports the session and leaves the
// dashboard open when focus is unavailable outside tmux.
func TestDashboardHandoffAssistOutsideTmuxStays(t *testing.T) {
	repo, setID, _ := setupSupervisorSpawnRepo(t, "handoff-assist-out", []spawnTestTask{
		{ID: "01-a", File: "01-a.md", Title: "A", Type: "AFK", Status: "done"},
	})
	d, cfg, row, rt := dashboardLaunchFixture(t, repo, setID)
	row.RuntimePath = repo
	row.ProjectPath = repo
	// Fake.Inside defaults false — outside tmux.

	m := newQueueDashboard(d, cfg, DashboardSnapshot{Rows: []DashboardRow{row}})
	m.width, m.height = 120, 40
	updated, _ := m.Update(tea.KeyPressMsg{Code: 'a', Text: "a"})
	got := updated.(QueueDashboard)
	_, cmd := got.Update(tea.KeyPressMsg{Code: 'S', Text: "S"})
	if cmd == nil {
		t.Fatal("S did not return a command")
	}
	msg := cmd()
	handoff, ok := msg.(dashboardHandoffMsg)
	if !ok {
		t.Fatalf("msg = %T, want dashboardHandoffMsg", msg)
	}
	if handoff.err != nil {
		t.Fatalf("outside tmux must not error: %v", handoff.err)
	}
	if handoff.quit {
		t.Fatal("outside tmux must not quit")
	}
	wantSession := project.SessionNameWith(project.DefaultDeps(), repo)
	if !strings.Contains(handoff.status, wantSession) {
		t.Fatalf("status = %q, want session %q", handoff.status, wantSession)
	}
	if !strings.Contains(handoff.status, "not inside tmux") {
		t.Fatalf("status = %q, want outside-tmux reason", handoff.status)
	}
	if rt.findSwitched("%3") {
		t.Fatalf("outside tmux must not switch-client, commands=%v", rt.commands)
	}

	updated, quitCmd := got.Update(handoff)
	after := updated.(QueueDashboard)
	if quitCmd != nil {
		t.Fatalf("outside tmux handoff must leave the dashboard open, got %T", quitCmd())
	}
	if after.statusMsg != handoff.status {
		t.Fatalf("statusMsg = %q, want %q", after.statusMsg, handoff.status)
	}
	if view := after.View().Content; !strings.Contains(view, wantSession) {
		t.Fatalf("view missing session status:\n%s", view)
	}
}

// TestDashboardHandoffVerifySpawnsFocusesAndQuits drives verify from the menu
// through focus and quit.
func TestDashboardHandoffVerifySpawnsFocusesAndQuits(t *testing.T) {
	repo, setID, _ := setupSupervisorSpawnRepo(t, "handoff-verify", []spawnTestTask{
		{ID: "01-a", File: "01-a.md", Title: "A", Type: "AFK", Status: "done"},
	})
	d, cfg, row, rt := dashboardLaunchFixture(t, repo, setID)
	row.RuntimePath = repo
	row.ProjectPath = repo
	row.RawStatus = tasks.StatusNeedsVerify
	rt.Fake.Inside = true

	m := newQueueDashboard(d, cfg, DashboardSnapshot{Rows: []DashboardRow{row}})
	updated, _ := m.Update(tea.KeyPressMsg{Code: 'a', Text: "a"})
	got := updated.(QueueDashboard)
	_, cmd := got.Update(tea.KeyPressMsg{Code: 'v', Text: "v"})
	if cmd == nil {
		t.Fatal("v did not return a command")
	}
	msg := cmd()
	handoff, ok := msg.(dashboardHandoffMsg)
	if !ok {
		t.Fatalf("msg = %T, want dashboardHandoffMsg", msg)
	}
	if !handoff.quit || handoff.err != nil {
		t.Fatalf("handoff = %+v, want quit", handoff)
	}
	if !rt.findSwitched("%3") {
		t.Fatalf("verify handoff must focus spawned pane, commands=%v", rt.commands)
	}
	updated, quitCmd := got.Update(handoff)
	_ = updated
	if quitCmd == nil {
		t.Fatal("successful verify handoff must quit")
	}
	if _, ok := quitCmd().(tea.QuitMsg); !ok {
		t.Fatalf("quit cmd = %T, want tea.QuitMsg", quitCmd())
	}
}

// TestDashboardHandoffVerifyReusesWithoutResend asserts an existing tagged
// verify/drain pane is focused without send-keys.
func TestDashboardHandoffVerifyReusesWithoutResend(t *testing.T) {
	repo, setID, _ := setupSupervisorSpawnRepo(t, "handoff-verify-reuse", []spawnTestTask{
		{ID: "01-a", File: "01-a.md", Title: "A", Type: "AFK", Status: "done"},
	})
	d, cfg, row, rt := dashboardLaunchFixture(t, repo, setID)
	row.RuntimePath = repo
	row.ProjectPath = repo
	row.RawStatus = tasks.StatusNeedsVerify
	rt.hasSession = true
	rt.windowNames["pop-queue"] = true
	rt.paneList = setID + " %9"
	rt.Fake.Inside = true

	m := newQueueDashboard(d, cfg, DashboardSnapshot{Rows: []DashboardRow{row}})
	updated, _ := m.Update(tea.KeyPressMsg{Code: 'a', Text: "a"})
	got := updated.(QueueDashboard)
	_, cmd := got.Update(tea.KeyPressMsg{Code: 'v', Text: "v"})
	msg := cmd()
	handoff, ok := msg.(dashboardHandoffMsg)
	if !ok {
		t.Fatalf("msg = %T, want dashboardHandoffMsg", msg)
	}
	if !handoff.quit {
		t.Fatalf("handoff = %+v, want quit", handoff)
	}
	if rt.countCommand("send-keys") != 0 {
		t.Fatalf("reuse must not re-send verify, commands=%v", rt.commands)
	}
	if !rt.findSwitched("%9") {
		t.Fatalf("reuse must focus existing pane, commands=%v", rt.commands)
	}
}

// TestDashboardHandoffDrainSpawnsFocusesAndQuits drives a bound drain from the
// menu through focus and quit.
func TestDashboardHandoffDrainSpawnsFocusesAndQuits(t *testing.T) {
	repo, setID, _ := setupSupervisorSpawnRepo(t, "handoff-drain", []spawnTestTask{
		{ID: "01-a", File: "01-a.md", Title: "A", Type: "AFK", Status: "open"},
	})
	bound := filepath.Join(t.TempDir(), "handoff-drain-wt")
	runGit(t, repo, "worktree", "add", "--detach", bound, "HEAD")
	d, cfg, row, rt := dashboardLaunchFixture(t, repo, setID)
	repoKey, err := resolveRepoKey(d, repo)
	if err != nil {
		t.Fatal(err)
	}
	row.RepoKey = repoKey
	row.RuntimePath = bound
	row.Bound = true
	seedBindingStore(t, d.Tasks, map[string]WorktreeBinding{
		setScopedKey(repoKey, setID): {RuntimePath: bound, Branch: "handoff-drain", Project: "pop", Provisioned: false},
	})
	rt.Fake.Inside = true

	m := newQueueDashboard(d, cfg, DashboardSnapshot{Rows: []DashboardRow{row}})
	updated, _ := m.Update(tea.KeyPressMsg{Code: 'a', Text: "a"})
	got := updated.(QueueDashboard)
	_, cmd := got.Update(tea.KeyPressMsg{Code: 'i', Text: "i"})
	if cmd == nil {
		t.Fatal("i did not return a command")
	}
	msg := cmd()
	handoff, ok := msg.(dashboardHandoffMsg)
	if !ok {
		t.Fatalf("msg = %T, want dashboardHandoffMsg", msg)
	}
	if !handoff.quit || handoff.err != nil {
		t.Fatalf("handoff = %+v, want quit", handoff)
	}
	if !rt.findSwitched("%3") {
		t.Fatalf("drain handoff must focus spawned pane, commands=%v", rt.commands)
	}
	updated, quitCmd := got.Update(handoff)
	_ = updated
	if quitCmd == nil {
		t.Fatal("successful drain handoff must quit")
	}
	if _, ok := quitCmd().(tea.QuitMsg); !ok {
		t.Fatalf("quit cmd = %T, want tea.QuitMsg", quitCmd())
	}
}

// TestDashboardHandoffDrainReusesWithoutResend asserts an existing drain pane
// is focused without re-sending implement.
func TestDashboardHandoffDrainReusesWithoutResend(t *testing.T) {
	repo, setID, _ := setupSupervisorSpawnRepo(t, "handoff-drain-reuse", []spawnTestTask{
		{ID: "01-a", File: "01-a.md", Title: "A", Type: "AFK", Status: "open"},
	})
	bound := filepath.Join(t.TempDir(), "handoff-drain-reuse-wt")
	runGit(t, repo, "worktree", "add", "--detach", bound, "HEAD")
	d, cfg, row, rt := dashboardLaunchFixture(t, repo, setID)
	repoKey, err := resolveRepoKey(d, repo)
	if err != nil {
		t.Fatal(err)
	}
	row.RepoKey = repoKey
	row.RuntimePath = bound
	row.Bound = true
	seedBindingStore(t, d.Tasks, map[string]WorktreeBinding{
		setScopedKey(repoKey, setID): {RuntimePath: bound, Branch: "handoff-drain-reuse", Project: "pop", Provisioned: false},
	})
	rt.hasSession = true
	rt.windowNames["pop-queue"] = true
	rt.paneList = setID + " %4"
	rt.Fake.Inside = true

	m := newQueueDashboard(d, cfg, DashboardSnapshot{Rows: []DashboardRow{row}})
	updated, _ := m.Update(tea.KeyPressMsg{Code: 'a', Text: "a"})
	got := updated.(QueueDashboard)
	_, cmd := got.Update(tea.KeyPressMsg{Code: 'i', Text: "i"})
	msg := cmd()
	handoff, ok := msg.(dashboardHandoffMsg)
	if !ok {
		t.Fatalf("msg = %T, want dashboardHandoffMsg", msg)
	}
	if !handoff.quit {
		t.Fatalf("handoff = %+v, want quit", handoff)
	}
	if rt.countCommand("send-keys") != 0 {
		t.Fatalf("reuse must not re-send implement, commands=%v", rt.commands)
	}
	if !rt.findSwitched("%4") {
		t.Fatalf("reuse must focus existing pane, commands=%v", rt.commands)
	}
}

// TestDashboardHandoffDrainOutsideTmuxStays reports the session and stays open.
func TestDashboardHandoffDrainOutsideTmuxStays(t *testing.T) {
	repo, setID, _ := setupSupervisorSpawnRepo(t, "handoff-drain-out", []spawnTestTask{
		{ID: "01-a", File: "01-a.md", Title: "A", Type: "AFK", Status: "open"},
	})
	bound := filepath.Join(t.TempDir(), "handoff-drain-out-wt")
	runGit(t, repo, "worktree", "add", "--detach", bound, "HEAD")
	d, cfg, row, _ := dashboardLaunchFixture(t, repo, setID)
	repoKey, err := resolveRepoKey(d, repo)
	if err != nil {
		t.Fatal(err)
	}
	row.RepoKey = repoKey
	row.RuntimePath = bound
	row.Bound = true
	seedBindingStore(t, d.Tasks, map[string]WorktreeBinding{
		setScopedKey(repoKey, setID): {RuntimePath: bound, Branch: "handoff-drain-out", Project: "pop", Provisioned: false},
	})

	m := newQueueDashboard(d, cfg, DashboardSnapshot{Rows: []DashboardRow{row}})
	m.width, m.height = 120, 40
	updated, _ := m.Update(tea.KeyPressMsg{Code: 'a', Text: "a"})
	got := updated.(QueueDashboard)
	_, cmd := got.Update(tea.KeyPressMsg{Code: 'i', Text: "i"})
	msg := cmd()
	handoff, ok := msg.(dashboardHandoffMsg)
	if !ok {
		t.Fatalf("msg = %T, want dashboardHandoffMsg", msg)
	}
	if handoff.err != nil || handoff.quit {
		t.Fatalf("outside tmux handoff = %+v, want stay without err", handoff)
	}
	wantSession := project.SessionNameWith(project.DefaultDeps(), repo)
	if !strings.Contains(handoff.status, wantSession) || !strings.Contains(handoff.status, "not inside tmux") {
		t.Fatalf("status = %q, want session %q outside tmux", handoff.status, wantSession)
	}

	updated, quitCmd := got.Update(handoff)
	after := updated.(QueueDashboard)
	if quitCmd != nil {
		t.Fatalf("outside tmux must leave dashboard open, got %T", quitCmd())
	}
	if after.statusMsg != handoff.status {
		t.Fatalf("statusMsg = %q, want %q", after.statusMsg, handoff.status)
	}
}

// TestDashboardHandoffNothingSpawnedStays surfaces a status and stays open when
// the launch produced no pane to focus.
func TestDashboardHandoffNothingSpawnedStays(t *testing.T) {
	m := newQueueDashboard(&Deps{}, nil, DashboardSnapshot{})
	m.width, m.height = 80, 24
	msg := handoffAfterLaunch(&Deps{}, DashboardDrainResult{}, nil)
	if msg.quit || msg.err != nil {
		t.Fatalf("empty handoff = %+v, want status stay", msg)
	}
	if msg.status != "nothing to hand off to" {
		t.Fatalf("status = %q, want nothing-to-hand-off", msg.status)
	}
	updated, cmd := m.Update(msg)
	got := updated.(QueueDashboard)
	if cmd != nil {
		t.Fatalf("empty handoff must not dispatch a command, got %T", cmd())
	}
	if got.statusMsg != "nothing to hand off to" {
		t.Fatalf("statusMsg = %q, want nothing-to-hand-off", got.statusMsg)
	}
}
