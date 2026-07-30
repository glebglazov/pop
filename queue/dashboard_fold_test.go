package queue

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/glebglazov/pop/config"
	tmuxmod "github.com/glebglazov/pop/internal/tmux"
	"github.com/glebglazov/pop/project"
	"github.com/glebglazov/pop/tasks"
	"github.com/glebglazov/pop/tasks/binding"
)

func TestDashboardActionMenuFoldFiltering(t *testing.T) {
	keysFor := func(row DashboardRow) []string {
		var keys []string
		for _, item := range dashboardMenuItems(row) {
			keys = append(keys, item.key)
		}
		return keys
	}

	doneBound := keysFor(DashboardRow{SetRef: SetRef{SetID: "done", RawStatus: tasks.StatusDone, Bound: true}})
	if !contains(doneBound, "f") {
		t.Fatalf("DONE bound row missing fold: %v", doneBound)
	}

	awaitingBound := keysFor(DashboardRow{SetRef: SetRef{SetID: "await", RawStatus: tasks.StatusAwaitingApproval, Bound: true}})
	if !contains(awaitingBound, "f") {
		t.Fatalf("AWAITING-APPROVAL bound row missing fold: %v", awaitingBound)
	}

	readyBound := keysFor(DashboardRow{SetRef: SetRef{SetID: "ready", RawStatus: tasks.StatusReady, Bound: true}})
	if contains(readyBound, "f") {
		t.Fatalf("READY bound row should not offer fold: %v", readyBound)
	}

	doneUnbound := keysFor(DashboardRow{SetRef: SetRef{SetID: "done", RawStatus: tasks.StatusDone}})
	if contains(doneUnbound, "f") {
		t.Fatalf("DONE unbound row should not offer fold: %v", doneUnbound)
	}
}

func TestDashboardFoldRefusalSticky(t *testing.T) {
	repo, setID, _ := setupSupervisorSpawnRepo(t, "fold-dirty", []spawnTestTask{
		{ID: "01-a", File: "01-a.md", Title: "A", Type: "AFK", Status: "done"},
	})
	td := queueTestTasksDeps(t, true)
	b, err := binding.ProvisionManagedBinding(binding.ProvisionManagedBindingRequest{
		TD: td, CheckoutPath: repo, SetID: setID,
	})
	if err != nil {
		t.Fatalf("provision: %v", err)
	}
	if err := os.WriteFile(filepath.Join(b.RuntimePath, "dirt.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg := &config.Config{Projects: []config.ProjectEntry{{Path: repo}}}
	d := &Deps{
		Tasks:      td,
		Project:    project.DefaultDeps(),
		LoadConfig: func(string) (*config.Config, error) { return cfg, nil },
	}
	row := DashboardRow{
		Project: "pop",
		SetRef: SetRef{
			SetID:       setID,
			DefPath:     tasksDirForRepo(t, td, repo),
			StatePath:   statePathForRepo(t, td, repo),
			RuntimePath: b.RuntimePath,
			RawStatus:   tasks.StatusDone,
			Bound:       true,
		},
	}
	m := newQueueDashboard(d, cfg, DashboardSnapshot{Rows: []DashboardRow{row}})
	m.width, m.height = 120, 40

	updated, _ := m.Update(tea.KeyPressMsg{Code: 'a', Text: "a"})
	got := updated.(QueueDashboard)
	updated, _ = got.Update(tea.KeyPressMsg{Code: 'f', Text: "f"})
	got = updated.(QueueDashboard)
	if got.actionErr == nil || !strings.Contains(got.actionErr.Error(), "dirty") {
		t.Fatalf("action error = %v, want dirty refusal", got.actionErr)
	}
	if view := got.View().Content; !strings.Contains(view, "dirty") {
		t.Fatalf("view missing refusal:\n%s", view)
	}
}

// TestDashboardHandoffFoldSpawnsFocusesAndQuits drives fold from the action menu
// through TagFold spawn, focus, and quit (ADR-0158).
func TestDashboardHandoffFoldSpawnsFocusesAndQuits(t *testing.T) {
	repo, setID, _ := setupSupervisorSpawnRepo(t, "handoff-fold", []spawnTestTask{
		{ID: "01-a", File: "01-a.md", Title: "A", Type: "AFK", Status: "done"},
	})
	bound := filepath.Join(t.TempDir(), "handoff-fold-wt")
	runGit(t, repo, "worktree", "add", "--detach", bound, "HEAD")
	d, cfg, row, rt := dashboardLaunchFixture(t, repo, setID)
	repoKey, err := resolveRepoKey(d, repo)
	if err != nil {
		t.Fatal(err)
	}
	row.RepoKey = repoKey
	row.RuntimePath = bound
	row.ProjectPath = repo
	row.Bound = true
	row.RawStatus = tasks.StatusDone
	seedBindingStore(t, d.Tasks, map[string]WorktreeBinding{
		setScopedKey(repoKey, setID): {RuntimePath: bound, Branch: "handoff-fold", Project: "pop", Provisioned: false},
	})
	rt.Fake.Inside = true

	m := newQueueDashboard(d, cfg, DashboardSnapshot{Rows: []DashboardRow{row}})
	updated, _ := m.Update(tea.KeyPressMsg{Code: 'a', Text: "a"})
	got := updated.(QueueDashboard)
	_, cmd := got.Update(tea.KeyPressMsg{Code: 'f', Text: "f"})
	if cmd == nil {
		t.Fatal("f did not return a command")
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
	foldPane := ""
	for paneID, tags := range rt.PaneTagValues {
		if tags[tmuxmod.TagFold] == setID {
			foldPane = paneID
		}
	}
	if foldPane == "" {
		t.Fatalf("fold must tag a pane, tags=%v", rt.PaneTagValues)
	}
	if got := rt.PaneTitles[foldPane]; got != foldPaneTitle(setID) {
		t.Fatalf("fold pane title = %q, want %q", got, foldPaneTitle(setID))
	}
	if !rt.findSwitched(foldPane) {
		t.Fatalf("fold handoff must focus spawned pane, commands=%v", rt.commands)
	}
	sent := false
	for _, c := range rt.commands {
		if len(c) >= 4 && c[0] == "send-keys" && strings.Contains(c[3], "pop tasks fold") {
			sent = true
		}
	}
	if !sent {
		t.Fatalf("fold must send pop tasks fold, commands=%v", rt.commands)
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

// TestDashboardHandoffFoldReusesConflictPane focuses an existing fold pane
// without re-sending, so a mid-conflict fold stays attended in its pane.
func TestDashboardHandoffFoldReusesConflictPane(t *testing.T) {
	repo, setID, _ := setupSupervisorSpawnRepo(t, "handoff-fold-reuse", []spawnTestTask{
		{ID: "01-a", File: "01-a.md", Title: "A", Type: "AFK", Status: "done"},
	})
	bound := filepath.Join(t.TempDir(), "handoff-fold-reuse-wt")
	runGit(t, repo, "worktree", "add", "--detach", bound, "HEAD")
	d, cfg, row, rt := dashboardLaunchFixture(t, repo, setID)
	repoKey, err := resolveRepoKey(d, repo)
	if err != nil {
		t.Fatal(err)
	}
	row.RepoKey = repoKey
	row.RuntimePath = bound
	row.ProjectPath = repo
	row.Bound = true
	row.RawStatus = tasks.StatusDone
	seedBindingStore(t, d.Tasks, map[string]WorktreeBinding{
		setScopedKey(repoKey, setID): {RuntimePath: bound, Branch: "handoff-fold-reuse", Project: "pop", Provisioned: false},
	})
	rt.hasSession = true
	rt.windowNames["pop-queue"] = true
	seedTaggedPane(rt, "%11", tmuxmod.TagFold, setID)
	rt.Fake.Inside = true

	m := newQueueDashboard(d, cfg, DashboardSnapshot{Rows: []DashboardRow{row}})
	updated, _ := m.Update(tea.KeyPressMsg{Code: 'a', Text: "a"})
	got := updated.(QueueDashboard)
	_, cmd := got.Update(tea.KeyPressMsg{Code: 'f', Text: "f"})
	if cmd == nil {
		t.Fatal("f did not return a command")
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
		t.Fatalf("reuse must not re-send fold, commands=%v", rt.commands)
	}
	if !rt.findSwitched("%11") {
		t.Fatalf("reuse must focus existing fold pane, commands=%v", rt.commands)
	}
}

func writeFileCommitForQueue(t *testing.T, dir, name, contents, msg string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(contents), 0o644); err != nil {
		t.Fatal(err)
	}
	runGit(t, dir, "add", name)
	runGit(t, dir, "commit", "-m", msg)
}

func tasksDirForRepo(t *testing.T, td *tasks.Deps, repo string) string {
	t.Helper()
	id, err := tasks.ResolveRepositoryIdentity(td, repo)
	if err != nil {
		t.Fatal(err)
	}
	defPath, err := tasks.CanonicalDefinitionPathWith(td, id.TasksDir)
	if err != nil {
		t.Fatal(err)
	}
	return defPath
}

func statePathForRepo(t *testing.T, td *tasks.Deps, repo string) string {
	t.Helper()
	id, err := tasks.ResolveRepositoryIdentity(td, repo)
	if err != nil {
		t.Fatal(err)
	}
	return tasks.StatePathFor(id.TasksDir)
}
