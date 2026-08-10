package dashboard

import (
	"github.com/glebglazov/pop/internal/queuetest"
	"github.com/glebglazov/pop/tasks/drain"
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
		for _, item := range dashboardMenuItems(testKinds(), row) {
			keys = append(keys, item.key)
		}
		return keys
	}

	doneBound := keysFor(DashboardRow{ID: "done", RawStatus: tasks.StatusDone, Bound: true, Provisioned: true})
	if !contains(doneBound, "F") {
		t.Fatalf("DONE managed row missing fold: %v", doneBound)
	}

	awaitingBound := keysFor(DashboardRow{ID: "await", RawStatus: tasks.StatusAwaitingApproval, Bound: true, Provisioned: true})
	if !contains(awaitingBound, "F") {
		t.Fatalf("AWAITING-APPROVAL managed row missing fold: %v", awaitingBound)
	}

	doneAdopted := keysFor(DashboardRow{ID: "done", RawStatus: tasks.StatusDone, Bound: true, Provisioned: false})
	if contains(doneAdopted, "F") {
		t.Fatalf("DONE adopted row should not offer fold: %v", doneAdopted)
	}

	readyBound := keysFor(DashboardRow{ID: "ready", RawStatus: tasks.StatusReady, Bound: true, Provisioned: true})
	if contains(readyBound, "F") {
		t.Fatalf("READY bound row should not offer fold: %v", readyBound)
	}

	doneUnbound := keysFor(DashboardRow{ID: "done", RawStatus: tasks.StatusDone})
	if contains(doneUnbound, "F") {
		t.Fatalf("DONE unbound row should not offer fold: %v", doneUnbound)
	}
}

func TestDashboardFoldRefusalSticky(t *testing.T) {
	repo, setID, _ := queuetest.SetupSpawnRepo(t, "fold-dirty", []queuetest.SpawnTask{
		{ID: "01-a", File: "01-a.md", Title: "A", Type: "AFK", Status: "done"},
	})
	td := queuetest.TasksDeps(t, true)
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
	d := &drain.Deps{
		Tasks:      td,
		Project:    project.DefaultDeps(),
		LoadConfig: func(string) (*config.Config, error) { return cfg, nil },
	}
	row := DashboardRow{
		Project:     "pop",
		ID:          setID,
		DefPath:     tasksDirForRepo(t, td, repo),
		StatePath:   statePathForRepo(t, td, repo),
		RuntimePath: b.RuntimePath,
		RawStatus:   tasks.StatusDone,
		Bound:       true,
		Provisioned: true,
	}
	m := newQueueDashboard(d, cfg, DashboardSnapshot{Containers: []DashboardRow{row}})
	m.width, m.height = 120, 40

	updated, _ := m.Update(tea.KeyPressMsg{Code: 'a', Text: "a"})
	got := updated.(QueueDashboard)
	updated, _ = got.Update(tea.KeyPressMsg{Code: 'F', Text: "F"})
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
	repo, setID, _ := queuetest.SetupSpawnRepo(t, "handoff-fold", []queuetest.SpawnTask{
		{ID: "01-a", File: "01-a.md", Title: "A", Type: "AFK", Status: "done"},
	})
	bound := filepath.Join(t.TempDir(), "handoff-fold-wt")
	runGit(t, repo, "worktree", "add", "--detach", bound, "HEAD")
	d, cfg, row, rt := dashboardLaunchFixture(t, repo, setID)
	repoKey, err := drain.ResolveRepoKey(d, repo)
	if err != nil {
		t.Fatal(err)
	}
	row.RepoKey = repoKey
	row.RuntimePath = bound
	row.ProjectPath = repo
	row.Bound = true
	row.Provisioned = true
	row.RawStatus = tasks.StatusDone
	queuetest.SeedBindingStore(t, d.Tasks, map[string]drain.WorktreeBinding{
		drain.SetScopedKey(repoKey, setID): {RuntimePath: bound, Branch: "handoff-fold", Project: "pop", Provisioned: true},
	})
	rt.Fake.Inside = true

	m := newQueueDashboard(d, cfg, DashboardSnapshot{Containers: []DashboardRow{row}})
	updated, _ := m.Update(tea.KeyPressMsg{Code: 'a', Text: "a"})
	got := updated.(QueueDashboard)
	_, cmd := got.Update(tea.KeyPressMsg{Code: 'F', Text: "F"})
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
	if got := rt.PaneTitles[foldPane]; got != drain.FoldPaneTitle(setID) {
		t.Fatalf("fold pane title = %q, want %q", got, drain.FoldPaneTitle(setID))
	}
	if !rt.FindSwitched(foldPane) {
		t.Fatalf("fold handoff must focus spawned pane, commands=%v", rt.Commands)
	}
	sent := false
	for _, c := range rt.Commands {
		if len(c) >= 4 && c[0] == "send-keys" && strings.Contains(c[3], "pop tasks fold") {
			sent = true
		}
	}
	if !sent {
		t.Fatalf("fold must send pop tasks fold, commands=%v", rt.Commands)
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
	repo, setID, _ := queuetest.SetupSpawnRepo(t, "handoff-fold-reuse", []queuetest.SpawnTask{
		{ID: "01-a", File: "01-a.md", Title: "A", Type: "AFK", Status: "done"},
	})
	bound := filepath.Join(t.TempDir(), "handoff-fold-reuse-wt")
	runGit(t, repo, "worktree", "add", "--detach", bound, "HEAD")
	d, cfg, row, rt := dashboardLaunchFixture(t, repo, setID)
	repoKey, err := drain.ResolveRepoKey(d, repo)
	if err != nil {
		t.Fatal(err)
	}
	row.RepoKey = repoKey
	row.RuntimePath = bound
	row.ProjectPath = repo
	row.Bound = true
	row.Provisioned = true
	row.RawStatus = tasks.StatusDone
	queuetest.SeedBindingStore(t, d.Tasks, map[string]drain.WorktreeBinding{
		drain.SetScopedKey(repoKey, setID): {RuntimePath: bound, Branch: "handoff-fold-reuse", Project: "pop", Provisioned: true},
	})
	rt.SessionLive = true
	rt.WindowNames["pop-work"] = true
	seedTaggedPane(rt, "%11", tmuxmod.TagFold, setID)
	rt.Fake.Inside = true

	m := newQueueDashboard(d, cfg, DashboardSnapshot{Containers: []DashboardRow{row}})
	updated, _ := m.Update(tea.KeyPressMsg{Code: 'a', Text: "a"})
	got := updated.(QueueDashboard)
	_, cmd := got.Update(tea.KeyPressMsg{Code: 'F', Text: "F"})
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
	if rt.CountCommand("send-keys") != 0 {
		t.Fatalf("reuse must not re-send fold, commands=%v", rt.Commands)
	}
	if !rt.FindSwitched("%11") {
		t.Fatalf("reuse must focus existing fold pane, commands=%v", rt.Commands)
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
