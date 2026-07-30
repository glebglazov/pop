package queue

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/glebglazov/pop/config"
	"github.com/glebglazov/pop/project"
	"github.com/glebglazov/pop/tasks"
	"github.com/glebglazov/pop/tasks/binding"
	"github.com/glebglazov/pop/work"
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

func TestDashboardFoldSuccessClearsBoundRow(t *testing.T) {
	repo, setID, _ := setupSupervisorSpawnRepo(t, "fold-ok", []spawnTestTask{
		{ID: "01-a", File: "01-a.md", Title: "A", Type: "AFK", Status: "done"},
	})
	td := queueTestTasksDeps(t, true)
	b, err := binding.ProvisionManagedBinding(binding.ProvisionManagedBindingRequest{
		TD: td, CheckoutPath: repo, SetID: setID,
	})
	if err != nil {
		t.Fatalf("provision: %v", err)
	}
	writeFileCommitForQueue(t, b.RuntimePath, "feature.txt", "set work\n", "set work")
	writeFileCommitForQueue(t, repo, "trunk.txt", "trunk work\n", "trunk work")

	cfg := &config.Config{Projects: []config.ProjectEntry{{Path: repo}}}
	d := &Deps{
		Tasks:       td,
		Project:     project.DefaultDeps(),
		LoadConfig:  func(string) (*config.Config, error) { return cfg, nil },
		IncludeDone: true,
	}
	d.FoldSet = func(ref SetRef, out io.Writer, opts FoldOptions) (FoldResult, error) {
		opts.Yes = true
		opts.In = tasks.NonInteractiveReader{}
		return binding.Fold(td, project.DefaultDeps(), cfg, ref.SetID, opts, binding.LifecycleHooks{ReadLock: d.readLock}, out)
	}
	row := DashboardRow{
		Project:   "pop",
		CursorKey: "pop\x00" + setID,
		SetRef: SetRef{
			SetID:       setID,
			DefPath:     tasksDirForRepo(t, td, repo),
			StatePath:   statePathForRepo(t, td, repo),
			RuntimePath: b.RuntimePath,
			ProjectPath: repo,
			RawStatus:   tasks.StatusDone,
			Bound:       true,
		},
		Worktree:  setID,
		DestKind:  work.DestDoneManagedBound,
	}
	m := newQueueDashboard(d, cfg, DashboardSnapshot{Rows: []DashboardRow{row}})

	updated, _ := m.Update(tea.KeyPressMsg{Code: 'a', Text: "a"})
	got := updated.(QueueDashboard)
	updated, cmd := got.Update(tea.KeyPressMsg{Code: 'f', Text: "f"})
	got = updated.(QueueDashboard)
	if cmd == nil {
		t.Fatal("fold dispatch returned no command")
	}
	msg, ok := cmd().(dashboardFoldMsg)
	if !ok {
		t.Fatalf("msg type = %T", cmd())
	}
	if msg.err != nil {
		t.Fatalf("fold msg err = %v", msg.err)
	}

	updated, cmd = got.Update(msg)
	got = updated.(QueueDashboard)
	if cmd == nil {
		t.Fatal("reload returned no command")
	}
	reloadMsg, ok := cmd().(dashboardRowsMsg)
	if !ok {
		t.Fatalf("reload msg type = %T", cmd())
	}
	if reloadMsg.err != nil {
		t.Fatalf("reload err = %v", reloadMsg.err)
	}
	updated, _ = got.Update(reloadMsg)
	got = updated.(QueueDashboard)
	for _, r := range got.snap.Rows {
		if r.SetID == setID && r.Bound {
			t.Fatalf("row still bound after fold: %+v", r)
		}
	}
	if _, _, ok, err := binding.FindBySetID(td, setID); err != nil || ok {
		t.Fatalf("binding still present: ok=%v err=%v", ok, err)
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
