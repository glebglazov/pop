package dashboard

import (
	"github.com/glebglazov/pop/internal/queuetest"
	"github.com/glebglazov/pop/tasks/drain"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	tmuxmod "github.com/glebglazov/pop/internal/tmux"
	"github.com/glebglazov/pop/project"
	"github.com/glebglazov/pop/tasks"
)

// TestDashboardHandoffAssistSpawnsFocusesAndQuits drives assist from the action
// menu key through spawn, SelectPane+SwitchClient, and tea.Quit (ADR-0158).
func TestDashboardHandoffAssistSpawnsFocusesAndQuits(t *testing.T) {
	repo, setID, _ := queuetest.SetupSpawnRepo(t, "handoff-assist", []queuetest.SpawnTask{
		{ID: "01-a", File: "01-a.md", Title: "A", Type: "AFK", Status: "done"},
	})
	d, cfg, row, rt := dashboardLaunchFixture(t, repo, setID)
	row.RuntimePath = repo
	row.ProjectPath = repo
	rt.Fake.Inside = true

	m := newQueueDashboard(d, cfg, DashboardSnapshot{Containers: []DashboardRow{row}})
	updated, _ := m.update(tea.KeyPressMsg{Code: 'a', Text: "a"})
	got := updated.(QueueDashboard)
	_, cmd := got.update(tea.KeyPressMsg{Code: 'S', Text: "S"})
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
	if !rt.FindSwitched("%3") {
		t.Fatalf("assist handoff must focus spawned pane, commands=%v", rt.Commands)
	}

	updated, quitCmd := got.update(handoff)
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
	repo, setID, _ := queuetest.SetupSpawnRepo(t, "handoff-assist-reuse", []queuetest.SpawnTask{
		{ID: "01-a", File: "01-a.md", Title: "A", Type: "AFK", Status: "done"},
	})
	d, cfg, row, rt := dashboardLaunchFixture(t, repo, setID)
	row.RuntimePath = repo
	row.ProjectPath = repo
	rt.SessionLive = true
	rt.WindowNames["pop-work"] = true
	seedTaggedPane(rt, "%5", tmuxmod.TagAssist, setID)
	rt.Fake.Inside = true

	m := newQueueDashboard(d, cfg, DashboardSnapshot{Containers: []DashboardRow{row}})
	updated, _ := m.update(tea.KeyPressMsg{Code: 'a', Text: "a"})
	got := updated.(QueueDashboard)
	_, cmd := got.update(tea.KeyPressMsg{Code: 'S', Text: "S"})
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
	if rt.CountCommand("send-keys") != 0 {
		t.Fatalf("reuse must not re-send assist, commands=%v", rt.Commands)
	}
	if !rt.FindSwitched("%5") {
		t.Fatalf("reuse must focus existing pane, commands=%v", rt.Commands)
	}
}

// TestDashboardHandoffAssistOutsideTmuxStays reports the session and leaves the
// dashboard open when focus is unavailable outside tmux.
func TestDashboardHandoffAssistOutsideTmuxStays(t *testing.T) {
	repo, setID, _ := queuetest.SetupSpawnRepo(t, "handoff-assist-out", []queuetest.SpawnTask{
		{ID: "01-a", File: "01-a.md", Title: "A", Type: "AFK", Status: "done"},
	})
	d, cfg, row, rt := dashboardLaunchFixture(t, repo, setID)
	row.RuntimePath = repo
	row.ProjectPath = repo
	// Fake.Inside defaults false — outside tmux.

	m := newQueueDashboard(d, cfg, DashboardSnapshot{Containers: []DashboardRow{row}})
	m.width, m.height = 120, 40
	updated, _ := m.update(tea.KeyPressMsg{Code: 'a', Text: "a"})
	got := updated.(QueueDashboard)
	_, cmd := got.update(tea.KeyPressMsg{Code: 'S', Text: "S"})
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
	if rt.FindSwitched("%3") {
		t.Fatalf("outside tmux must not switch-client, commands=%v", rt.Commands)
	}

	updated, quitCmd := got.update(handoff)
	after := updated.(QueueDashboard)
	if quitCmd != nil {
		t.Fatalf("outside tmux handoff must leave the dashboard open, got %T", quitCmd())
	}
	if after.flash.Text() != handoff.status {
		t.Fatalf("flash = %q, want %q", after.flash.Text(), handoff.status)
	}
	if view := after.View().Content; !strings.Contains(view, wantSession) {
		t.Fatalf("view missing session status:\n%s", view)
	}
}

// TestDashboardHandoffVerifySpawnsFocusesAndQuits drives verify from the menu
// through focus and quit.
func TestDashboardHandoffVerifySpawnsFocusesAndQuits(t *testing.T) {
	repo, setID, _ := queuetest.SetupSpawnRepo(t, "handoff-verify", []queuetest.SpawnTask{
		{ID: "01-a", File: "01-a.md", Title: "A", Type: "AFK", Status: "done"},
	})
	d, cfg, row, rt := dashboardLaunchFixture(t, repo, setID)
	row.RuntimePath = repo
	row.ProjectPath = repo
	row.RawStatus = tasks.StatusNeedsVerify
	row.VerifyMark = tasks.VerifyMarkUnverified
	rt.Fake.Inside = true

	m := newQueueDashboard(d, cfg, DashboardSnapshot{Containers: []DashboardRow{row}})
	updated, _ := m.update(tea.KeyPressMsg{Code: 'a', Text: "a"})
	got := updated.(QueueDashboard)
	_, cmd := got.update(tea.KeyPressMsg{Code: 'V', Text: "V"})
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
	if !rt.FindSwitched("%3") {
		t.Fatalf("verify handoff must focus spawned pane, commands=%v", rt.Commands)
	}
	updated, quitCmd := got.update(handoff)
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
	repo, setID, _ := queuetest.SetupSpawnRepo(t, "handoff-verify-reuse", []queuetest.SpawnTask{
		{ID: "01-a", File: "01-a.md", Title: "A", Type: "AFK", Status: "done"},
	})
	d, cfg, row, rt := dashboardLaunchFixture(t, repo, setID)
	row.RuntimePath = repo
	row.ProjectPath = repo
	row.RawStatus = tasks.StatusNeedsVerify
	row.VerifyMark = tasks.VerifyMarkUnverified
	rt.SessionLive = true
	rt.WindowNames["pop-work"] = true
	seedTaggedPane(rt, "%9", tmuxmod.TagVerify, setID)
	rt.Fake.Inside = true

	m := newQueueDashboard(d, cfg, DashboardSnapshot{Containers: []DashboardRow{row}})
	updated, _ := m.update(tea.KeyPressMsg{Code: 'a', Text: "a"})
	got := updated.(QueueDashboard)
	_, cmd := got.update(tea.KeyPressMsg{Code: 'V', Text: "V"})
	msg := cmd()
	handoff, ok := msg.(dashboardHandoffMsg)
	if !ok {
		t.Fatalf("msg = %T, want dashboardHandoffMsg", msg)
	}
	if !handoff.quit {
		t.Fatalf("handoff = %+v, want quit", handoff)
	}
	if rt.CountCommand("send-keys") != 0 {
		t.Fatalf("reuse must not re-send verify, commands=%v", rt.Commands)
	}
	if !rt.FindSwitched("%9") {
		t.Fatalf("reuse must focus existing pane, commands=%v", rt.Commands)
	}
}

// TestDashboardHandoffDrainSpawnsFocusesAndQuits drives a bound drain from the
// menu through focus and quit.
func TestDashboardHandoffDrainSpawnsFocusesAndQuits(t *testing.T) {
	repo, setID, _ := queuetest.SetupSpawnRepo(t, "handoff-drain", []queuetest.SpawnTask{
		{ID: "01-a", File: "01-a.md", Title: "A", Type: "AFK", Status: "open"},
	})
	bound := filepath.Join(t.TempDir(), "handoff-drain-wt")
	runGit(t, repo, "worktree", "add", "--detach", bound, "HEAD")
	d, cfg, row, rt := dashboardLaunchFixture(t, repo, setID)
	repoKey, err := drain.ResolveRepoKey(d, repo)
	if err != nil {
		t.Fatal(err)
	}
	row.RepoKey = repoKey
	row.RuntimePath = bound
	row.Bound = true
	queuetest.SeedBindingStore(t, d.Tasks, map[string]drain.WorktreeBinding{
		drain.SetScopedKey(repoKey, setID): {RuntimePath: bound, Branch: "handoff-drain", Project: "pop", Provisioned: false},
	})
	rt.Fake.Inside = true

	m := newQueueDashboard(d, cfg, DashboardSnapshot{Containers: []DashboardRow{row}})
	updated, _ := m.update(tea.KeyPressMsg{Code: 'a', Text: "a"})
	got := updated.(QueueDashboard)
	_, cmd := got.update(tea.KeyPressMsg{Code: 'I', Text: "I"})
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
	if !rt.FindSwitched("%3") {
		t.Fatalf("drain handoff must focus spawned pane, commands=%v", rt.Commands)
	}
	updated, quitCmd := got.update(handoff)
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
	repo, setID, _ := queuetest.SetupSpawnRepo(t, "handoff-drain-reuse", []queuetest.SpawnTask{
		{ID: "01-a", File: "01-a.md", Title: "A", Type: "AFK", Status: "open"},
	})
	bound := filepath.Join(t.TempDir(), "handoff-drain-reuse-wt")
	runGit(t, repo, "worktree", "add", "--detach", bound, "HEAD")
	d, cfg, row, rt := dashboardLaunchFixture(t, repo, setID)
	repoKey, err := drain.ResolveRepoKey(d, repo)
	if err != nil {
		t.Fatal(err)
	}
	row.RepoKey = repoKey
	row.RuntimePath = bound
	row.Bound = true
	queuetest.SeedBindingStore(t, d.Tasks, map[string]drain.WorktreeBinding{
		drain.SetScopedKey(repoKey, setID): {RuntimePath: bound, Branch: "handoff-drain-reuse", Project: "pop", Provisioned: false},
	})
	rt.SessionLive = true
	rt.WindowNames["pop-work"] = true
	seedTaggedPane(rt, "%4", tmuxmod.TagSet, setID)
	rt.Fake.Inside = true

	m := newQueueDashboard(d, cfg, DashboardSnapshot{Containers: []DashboardRow{row}})
	updated, _ := m.update(tea.KeyPressMsg{Code: 'a', Text: "a"})
	got := updated.(QueueDashboard)
	_, cmd := got.update(tea.KeyPressMsg{Code: 'I', Text: "I"})
	msg := cmd()
	handoff, ok := msg.(dashboardHandoffMsg)
	if !ok {
		t.Fatalf("msg = %T, want dashboardHandoffMsg", msg)
	}
	if !handoff.quit {
		t.Fatalf("handoff = %+v, want quit", handoff)
	}
	if rt.CountCommand("send-keys") != 0 {
		t.Fatalf("reuse must not re-send implement, commands=%v", rt.Commands)
	}
	if !rt.FindSwitched("%4") {
		t.Fatalf("reuse must focus existing pane, commands=%v", rt.Commands)
	}
}

// TestDashboardHandoffDrainOutsideTmuxStays reports the session and stays open.
func TestDashboardHandoffDrainOutsideTmuxStays(t *testing.T) {
	repo, setID, _ := queuetest.SetupSpawnRepo(t, "handoff-drain-out", []queuetest.SpawnTask{
		{ID: "01-a", File: "01-a.md", Title: "A", Type: "AFK", Status: "open"},
	})
	bound := filepath.Join(t.TempDir(), "handoff-drain-out-wt")
	runGit(t, repo, "worktree", "add", "--detach", bound, "HEAD")
	d, cfg, row, _ := dashboardLaunchFixture(t, repo, setID)
	repoKey, err := drain.ResolveRepoKey(d, repo)
	if err != nil {
		t.Fatal(err)
	}
	row.RepoKey = repoKey
	row.RuntimePath = bound
	row.Bound = true
	queuetest.SeedBindingStore(t, d.Tasks, map[string]drain.WorktreeBinding{
		drain.SetScopedKey(repoKey, setID): {RuntimePath: bound, Branch: "handoff-drain-out", Project: "pop", Provisioned: false},
	})

	m := newQueueDashboard(d, cfg, DashboardSnapshot{Containers: []DashboardRow{row}})
	m.width, m.height = 120, 40
	updated, _ := m.update(tea.KeyPressMsg{Code: 'a', Text: "a"})
	got := updated.(QueueDashboard)
	_, cmd := got.update(tea.KeyPressMsg{Code: 'I', Text: "I"})
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

	updated, quitCmd := got.update(handoff)
	after := updated.(QueueDashboard)
	if quitCmd != nil {
		t.Fatalf("outside tmux must leave dashboard open, got %T", quitCmd())
	}
	if after.flash.Text() != handoff.status {
		t.Fatalf("flash = %q, want %q", after.flash.Text(), handoff.status)
	}
}

// TestDashboardHandoffNothingSpawnedStays surfaces a status and stays open when
// the launch produced no pane to focus.
func TestDashboardHandoffNothingSpawnedStays(t *testing.T) {
	m := newQueueDashboard(&drain.Deps{}, nil, DashboardSnapshot{})
	m.width, m.height = 80, 24
	msg := handoffAfterLaunch(&drain.Deps{}, drain.DashboardDrainResult{}, nil)
	if msg.quit || msg.err != nil {
		t.Fatalf("empty handoff = %+v, want status stay", msg)
	}
	if msg.status != "nothing to hand off to" {
		t.Fatalf("status = %q, want nothing-to-hand-off", msg.status)
	}
	updated, cmd := m.update(msg)
	got := updated.(QueueDashboard)
	if cmd != nil {
		t.Fatalf("empty handoff must not dispatch a command, got %T", cmd())
	}
	if got.flash.Text() != "nothing to hand off to" {
		t.Fatalf("flash = %q, want nothing-to-hand-off", got.flash.Text())
	}
}
