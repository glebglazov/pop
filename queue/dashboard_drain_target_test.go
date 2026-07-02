package queue

import (
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/glebglazov/pop/tasks"
	"github.com/glebglazov/pop/tasks/binding"
)

// TestDashboardDrainTargetEntriesOrderAndExclusions asserts the Drain target
// picker for an unbound set lists, in order, the repo's adoptable worktrees, a
// "new managed worktree" option (the default cursor), and the trunk — excluding
// the trunk itself, pop-managed worktrees, and worktrees bound to other sets.
func TestDashboardDrainTargetEntriesOrderAndExclusions(t *testing.T) {
	repo, setID, _ := setupSupervisorSpawnRepo(t, "drain-target", []spawnTestTask{
		{ID: "01-a", File: "01-a.md", Title: "A", Type: "AFK", Status: "open"},
	})
	wt1 := filepath.Join(t.TempDir(), "adopt-one")
	wt2 := filepath.Join(t.TempDir(), "bound-other")
	runGit(t, repo, "worktree", "add", "-b", "adopt-one", wt1, "HEAD")
	runGit(t, repo, "worktree", "add", "-b", "bound-other", wt2, "HEAD")
	d, cfg, row, _ := dashboardLaunchFixture(t, repo, setID)
	repoKey, err := resolveRepoKey(d, repo)
	if err != nil {
		t.Fatal(err)
	}

	// A pop-managed worktree (under ManagedWorktreesRoot) must be excluded.
	managed := filepath.Join(binding.ManagedWorktreesRoot(d.Tasks), repoKey, "managed-set")
	if err := d.Tasks.FS.MkdirAll(filepath.Dir(managed), 0o755); err != nil {
		t.Fatal(err)
	}
	runGit(t, repo, "worktree", "add", "-b", "managed-branch", managed, "HEAD")

	// wt2 is bound to a different set, so 1:1 mapping excludes it.
	seedBindingStore(t, d.Tasks, map[string]WorktreeBinding{
		setScopedKey(repoKey, "other-set"): {RuntimePath: wt2, Branch: "bound-other", Provisioned: false},
	})

	entries, err := DrainTargetEntries(d, cfg, row.SetRef)
	if err != nil {
		t.Fatalf("DrainTargetEntries: %v", err)
	}
	if len(entries) != 3 {
		t.Fatalf("entries = %+v, want [adopt-one, new-managed, trunk]", entries)
	}
	if entries[0].Kind != drainTargetWorktree || canon(t, d, entries[0].Path) != canon(t, d, wt1) {
		t.Fatalf("entries[0] = %+v, want adopt %s", entries[0], wt1)
	}
	if entries[1].Kind != drainTargetNewManaged {
		t.Fatalf("entries[1] = %+v, want new managed worktree", entries[1])
	}
	if entries[2].Kind != drainTargetTrunk {
		t.Fatalf("entries[2] = %+v, want trunk", entries[2])
	}
	if got := defaultDrainCursor(entries); got != 1 {
		t.Fatalf("default cursor = %d, want 1 (new managed worktree)", got)
	}
	for _, e := range entries {
		if e.Kind == drainTargetWorktree {
			if canon(t, d, e.Path) == canon(t, d, wt2) {
				t.Fatalf("worktree bound to another set must be excluded: %+v", e)
			}
			if canon(t, d, e.Path) == canon(t, d, managed) {
				t.Fatalf("pop-managed worktree must be excluded: %+v", e)
			}
			if canon(t, d, e.Path) == canon(t, d, repo) {
				t.Fatalf("trunk must not appear as an adopt option: %+v", e)
			}
		}
	}
}

// TestDashboardDrainTargetAdoptsWorktreeAndDrains asserts that selecting an
// existing worktree adopts it (adopted binding) and drains there in one action.
func TestDashboardDrainTargetAdoptsWorktreeAndDrains(t *testing.T) {
	repo, setID, _ := setupSupervisorSpawnRepo(t, "drain-adopt", []spawnTestTask{
		{ID: "01-a", File: "01-a.md", Title: "A", Type: "AFK", Status: "open"},
	})
	wt := filepath.Join(t.TempDir(), "adopt-here")
	runGit(t, repo, "worktree", "add", "-b", "adopt-here", wt, "HEAD")
	d, cfg, row, rt := dashboardLaunchFixture(t, repo, setID)
	repoKey, err := resolveRepoKey(d, repo)
	if err != nil {
		t.Fatal(err)
	}

	result, err := LaunchDrainTarget(d, cfg, row.SetRef, dashboardDrainEntry{Kind: drainTargetWorktree, Path: wt, Branch: "adopt-here"})
	if err != nil {
		t.Fatalf("LaunchDrainTarget adopt: %v", err)
	}
	if result.RuntimePath != wt {
		t.Fatalf("runtime = %q, want adopted checkout %q", result.RuntimePath, wt)
	}
	b := loadBindingStore(t, d.Tasks)[setScopedKey(repoKey, setID)]
	if b.RuntimePath != wt || b.Provisioned {
		t.Fatalf("binding = %+v, want adopted %s", b, wt)
	}
	if cmd, ok := extractSpawnCommand(rt); !ok || !strings.Contains(cmd, "pop tasks implement "+setID) {
		t.Fatalf("spawn command = %q, want implement for %s", cmd, setID)
	}
}

// TestDashboardDrainTargetNewManagedProvisionsOffTrunkAndDrains asserts that the
// "new managed worktree" option provisions a managed checkout forked from the
// trunk, records a provisioned binding, and drains there.
func TestDashboardDrainTargetNewManagedProvisionsOffTrunkAndDrains(t *testing.T) {
	repo, setID, _ := setupSupervisorSpawnRepo(t, "drain-managed", []spawnTestTask{
		{ID: "01-a", File: "01-a.md", Title: "A", Type: "AFK", Status: "open"},
	})
	d, cfg, row, rt := dashboardLaunchFixture(t, repo, setID)
	repoKey, err := resolveRepoKey(d, repo)
	if err != nil {
		t.Fatal(err)
	}

	result, err := LaunchDrainTarget(d, cfg, row.SetRef, dashboardDrainEntry{Kind: drainTargetNewManaged})
	if err != nil {
		t.Fatalf("LaunchDrainTarget new managed: %v", err)
	}
	managedRoot := binding.ManagedWorktreesRoot(d.Tasks)
	if !pathUnder(canon(t, d, result.RuntimePath), canon(t, d, managedRoot)) {
		t.Fatalf("runtime = %q, want a managed worktree under %q", result.RuntimePath, managedRoot)
	}
	b := loadBindingStore(t, d.Tasks)[setScopedKey(repoKey, setID)]
	if b.RuntimePath != result.RuntimePath || !b.Provisioned {
		t.Fatalf("binding = %+v, want provisioned managed worktree", b)
	}
	if !strings.HasPrefix(b.Branch, "pop/") {
		t.Fatalf("branch = %q, want pop/<set>/<stamp> forked from trunk", b.Branch)
	}
	if cmd, ok := extractSpawnCommand(rt); !ok || !strings.Contains(cmd, "pop tasks implement "+setID) {
		t.Fatalf("spawn command = %q, want implement for %s", cmd, setID)
	}
}

// TestDashboardDrainTargetTrunkDrainsInlineNoBinding asserts the trunk option
// drains in the trunk worktree and records no binding (an inline drain).
func TestDashboardDrainTargetTrunkDrainsInlineNoBinding(t *testing.T) {
	repo, setID, _ := setupSupervisorSpawnRepo(t, "drain-trunk", []spawnTestTask{
		{ID: "01-a", File: "01-a.md", Title: "A", Type: "AFK", Status: "open"},
	})
	d, cfg, row, rt := dashboardLaunchFixture(t, repo, setID)
	repoKey, err := resolveRepoKey(d, repo)
	if err != nil {
		t.Fatal(err)
	}

	result, err := LaunchDrainTarget(d, cfg, row.SetRef, dashboardDrainEntry{Kind: drainTargetTrunk, Path: repo})
	if err != nil {
		t.Fatalf("LaunchDrainTarget trunk: %v", err)
	}
	if canon(t, d, result.RuntimePath) != canon(t, d, repo) {
		t.Fatalf("runtime = %q, want trunk %q", result.RuntimePath, repo)
	}
	if b, ok := loadBindingStore(t, d.Tasks)[setScopedKey(repoKey, setID)]; ok {
		t.Fatalf("trunk drain recorded a binding: %+v", b)
	}
	if cmd, ok := extractSpawnCommand(rt); !ok || strings.Contains(cmd, "--task-runtime-path") {
		t.Fatalf("spawn command = %q, want in-place trunk drain", cmd)
	}
}

// TestDashboardDrainTargetBareHidesTrunkOptions asserts a bare repo with no
// resolvable trunk offers only adoptable worktrees — never the trunk-dependent
// "new managed worktree" or trunk options.
func TestDashboardDrainTargetBareHidesTrunkOptions(t *testing.T) {
	_, wts := initBareRepoWithWorktrees(t, 2)
	checkout := wts[0]
	t.Setenv("XDG_DATA_HOME", filepath.Join(t.TempDir(), "xdg"))
	id, err := tasks.ResolveRepositoryIdentity(tasks.DefaultDeps(), checkout)
	if err != nil {
		t.Fatal(err)
	}
	setID := "bare-target"
	setDir := filepath.Join(id.TasksDir, setID)
	writeSpawnTaskMD(t, setDir, "01-a.md")
	writeSpawnManifest(t, setDir, []spawnTestTask{{ID: "01-a", File: "01-a.md", Title: "A", Type: "AFK", Status: "open"}})
	if _, err := tasks.RegisterWith(tasks.DefaultDeps(), id.TasksDir, tasks.StatePathFor(id.TasksDir)); err != nil {
		t.Fatal(err)
	}
	d, cfg, row, _ := dashboardLaunchFixture(t, checkout, setID)

	entries, err := DrainTargetEntries(d, cfg, row.SetRef)
	if err != nil {
		t.Fatalf("DrainTargetEntries: %v", err)
	}
	if len(entries) == 0 {
		t.Fatalf("bare repo with worktrees should still list adopt targets")
	}
	for _, e := range entries {
		if e.Kind != drainTargetWorktree {
			t.Fatalf("bare repo must hide trunk-dependent options, got %+v", e)
		}
	}
}

// TestDashboardIKeyUnboundOpensPicker asserts that `i` on an unbound set opens
// the Drain target picker (default cursor on "new managed worktree"), while `i`
// on a bound set drains its binding directly with no picker.
func TestDashboardIKeyUnboundOpensPicker(t *testing.T) {
	repo, setID, _ := setupSupervisorSpawnRepo(t, "i-key", []spawnTestTask{
		{ID: "01-a", File: "01-a.md", Title: "A", Type: "AFK", Status: "open"},
	})
	d, cfg, row, _ := dashboardLaunchFixture(t, repo, setID)
	repoKey, err := resolveRepoKey(d, repo)
	if err != nil {
		t.Fatal(err)
	}
	row.RepoKey = repoKey
	row.cursorKey = "pop\x00" + setID

	m := newQueueDashboard(d, cfg, DashboardSnapshot{Rows: []DashboardRow{row}})
	// Drain now lives behind the action menu: open with `a`, then `i`.
	updated, _ := m.Update(tea.KeyPressMsg{Code: 'a', Text: "a"})
	got := updated.(QueueDashboard)
	updated, cmd := got.Update(tea.KeyPressMsg{Code: 'i', Text: "i"})
	got = updated.(QueueDashboard)
	if cmd == nil {
		t.Fatal("i did not return a command")
	}
	msg := cmd()
	listMsg, ok := msg.(dashboardDrainListMsg)
	if !ok {
		t.Fatalf("i on unbound set produced %T, want dashboardDrainListMsg", msg)
	}
	if listMsg.err != nil {
		t.Fatalf("drain target list err = %v", listMsg.err)
	}
	updated, _ = got.Update(listMsg)
	got = updated.(QueueDashboard)
	if got.drainPick == nil {
		t.Fatal("i on unbound set did not open the drain target picker")
	}
	selected, ok := got.drainPick.list.Selected()
	if !ok || selected.Kind != drainTargetNewManaged {
		t.Fatalf("default cursor entry = %+v (ok=%v), want new managed worktree", selected, ok)
	}
}

// TestDashboardIKeyBoundDrainsWithoutPicker asserts a bound set resumes in its
// binding on `i` with no picker.
func TestDashboardIKeyBoundDrainsWithoutPicker(t *testing.T) {
	repo, setID, _ := setupSupervisorSpawnRepo(t, "i-bound", []spawnTestTask{
		{ID: "01-a", File: "01-a.md", Title: "A", Type: "AFK", Status: "open"},
	})
	bound := filepath.Join(t.TempDir(), "bound")
	runGit(t, repo, "worktree", "add", "--detach", bound, "HEAD")
	d, cfg, row, rt := dashboardLaunchFixture(t, repo, setID)
	repoKey, err := resolveRepoKey(d, repo)
	if err != nil {
		t.Fatal(err)
	}
	row.RepoKey = repoKey
	seedBindingStore(t, d.Tasks, map[string]WorktreeBinding{
		setScopedKey(repoKey, setID): {RuntimePath: bound, Branch: "bound", Project: "pop", Provisioned: false},
	})

	m := newQueueDashboard(d, cfg, DashboardSnapshot{Rows: []DashboardRow{row}})
	// Drain now lives behind the action menu: open with `a`, then `i`.
	updated, _ := m.Update(tea.KeyPressMsg{Code: 'a', Text: "a"})
	got := updated.(QueueDashboard)
	_, cmd := got.Update(tea.KeyPressMsg{Code: 'i', Text: "i"})
	if cmd == nil {
		t.Fatal("i did not return a command")
	}
	msg := cmd()
	drainMsg, ok := msg.(dashboardDrainMsg)
	if !ok {
		t.Fatalf("i on bound set produced %T, want dashboardDrainMsg (no picker)", msg)
	}
	if drainMsg.err != nil {
		t.Fatalf("bound drain err = %v", drainMsg.err)
	}
	if cmd, ok := extractSpawnCommand(rt); !ok || !strings.Contains(cmd, "pop tasks implement "+setID) {
		t.Fatalf("spawn command = %q, want implement for %s in bound checkout", cmd, setID)
	}
}
