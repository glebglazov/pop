package dashboard

import (
	"github.com/glebglazov/pop/internal/queuetest"
	"github.com/glebglazov/pop/tasks/drain"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/glebglazov/pop/tasks"
	"github.com/glebglazov/pop/tasks/binding"
)

// TestDrainTargetEntriesOrderAndExclusions asserts the Drain target
// picker for an unbound set lists, in order, the repo's adoptable worktrees, a
// "new managed worktree" option, and the trunk — excluding the trunk itself,
// pop-managed worktrees, and worktrees bound to other sets. It also pins the
// cursor default (ADR-0192): opened from an adoptable checkout it lands on that
// checkout's own entry, never on "new managed worktree".
func TestDrainTargetEntriesOrderAndExclusions(t *testing.T) {
	repo, setID, _ := queuetest.SetupSpawnRepo(t, "drain-target", []queuetest.SpawnTask{
		{ID: "01-a", File: "01-a.md", Title: "A", Type: "AFK", Status: "open"},
	})
	wt1 := filepath.Join(t.TempDir(), "adopt-one")
	wt2 := filepath.Join(t.TempDir(), "bound-other")
	runGit(t, repo, "worktree", "add", "-b", "adopt-one", wt1, "HEAD")
	runGit(t, repo, "worktree", "add", "-b", "bound-other", wt2, "HEAD")
	d, cfg, row, _ := dashboardLaunchFixture(t, repo, setID)
	repoKey, err := drain.ResolveRepoKey(d, repo)
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
	queuetest.SeedBindingStore(t, d.Tasks, map[string]drain.WorktreeBinding{
		drain.SetScopedKey(repoKey, "other-set"): {RuntimePath: wt2, Branch: "bound-other", Provisioned: false},
	})

	entries, err := drain.DrainTargetEntries(d, cfg, row)
	if err != nil {
		t.Fatalf("drain.DrainTargetEntries: %v", err)
	}
	if len(entries) != 3 {
		t.Fatalf("entries = %+v, want [adopt-one, new-managed, trunk]", entries)
	}
	if entries[0].Kind != drain.DrainTargetWorktree || queuetest.Canon(t, d.Tasks, entries[0].Path) != queuetest.Canon(t, d.Tasks, wt1) {
		t.Fatalf("entries[0] = %+v, want adopt %s", entries[0], wt1)
	}
	if entries[1].Kind != drain.DrainTargetNewManaged {
		t.Fatalf("entries[1] = %+v, want new managed worktree", entries[1])
	}
	if entries[2].Kind != drain.DrainTargetTrunk {
		t.Fatalf("entries[2] = %+v, want trunk", entries[2])
	}
	if got := defaultDrainCursor(d, entries, wt1); got != 0 {
		t.Fatalf("default cursor from %s = %d, want 0 (that checkout's own entry)", wt1, got)
	}
	if got := defaultDrainCursor(d, entries, wt1); entries[got].Kind == drain.DrainTargetNewManaged {
		t.Fatalf("default cursor landed on new managed worktree (index %d)", got)
	}
	for _, e := range entries {
		if e.Kind == drain.DrainTargetWorktree {
			if queuetest.Canon(t, d.Tasks, e.Path) == queuetest.Canon(t, d.Tasks, wt2) {
				t.Fatalf("worktree bound to another set must be excluded: %+v", e)
			}
			if queuetest.Canon(t, d.Tasks, e.Path) == queuetest.Canon(t, d.Tasks, managed) {
				t.Fatalf("pop-managed worktree must be excluded: %+v", e)
			}
			if queuetest.Canon(t, d.Tasks, e.Path) == queuetest.Canon(t, d.Tasks, repo) {
				t.Fatalf("trunk must not appear as an adopt option: %+v", e)
			}
		}
	}
}

// TestDashboardDrainTargetAdoptsWorktreeAndDrains asserts that selecting an
// existing worktree adopts it (adopted binding) and drains there in one action.
func TestDashboardDrainTargetAdoptsWorktreeAndDrains(t *testing.T) {
	repo, setID, _ := queuetest.SetupSpawnRepo(t, "drain-adopt", []queuetest.SpawnTask{
		{ID: "01-a", File: "01-a.md", Title: "A", Type: "AFK", Status: "open"},
	})
	wt := filepath.Join(t.TempDir(), "adopt-here")
	runGit(t, repo, "worktree", "add", "-b", "adopt-here", wt, "HEAD")
	d, cfg, row, rt := dashboardLaunchFixture(t, repo, setID)
	repoKey, err := drain.ResolveRepoKey(d, repo)
	if err != nil {
		t.Fatal(err)
	}

	result, err := drain.LaunchDrainTarget(d, cfg, row, drain.DrainEntry{Kind: drain.DrainTargetWorktree, Path: wt, Branch: "adopt-here"})
	if err != nil {
		t.Fatalf("drain.LaunchDrainTarget adopt: %v", err)
	}
	if result.RuntimePath != wt {
		t.Fatalf("runtime = %q, want adopted checkout %q", result.RuntimePath, wt)
	}
	b := queuetest.LoadBindingStore(t, d.Tasks)[drain.SetScopedKey(repoKey, setID)]
	if b.RuntimePath != wt || b.Provisioned {
		t.Fatalf("binding = %+v, want adopted %s", b, wt)
	}
	if cmd, ok := queuetest.ExtractSpawnCommand(rt); !ok || !strings.Contains(cmd, "pop tasks implement "+setID) || !strings.Contains(cmd, "--task-runtime-path "+wt) {
		t.Fatalf("spawn command = %q, want implement for %s pinned to adopted checkout %q", cmd, setID, wt)
	}
}

// TestDashboardDrainTargetNewManagedProvisionsOffTrunkAndDrains asserts that the
// "new managed worktree" option provisions a managed checkout forked from the
// trunk, records a provisioned binding, and drains there.
func TestDashboardDrainTargetNewManagedProvisionsOffTrunkAndDrains(t *testing.T) {
	repo, setID, _ := queuetest.SetupSpawnRepo(t, "drain-managed", []queuetest.SpawnTask{
		{ID: "01-a", File: "01-a.md", Title: "A", Type: "AFK", Status: "open"},
	})
	d, cfg, row, rt := dashboardLaunchFixture(t, repo, setID)
	repoKey, err := drain.ResolveRepoKey(d, repo)
	if err != nil {
		t.Fatal(err)
	}

	result, err := drain.LaunchDrainTarget(d, cfg, row, drain.DrainEntry{Kind: drain.DrainTargetNewManaged})
	if err != nil {
		t.Fatalf("drain.LaunchDrainTarget new managed: %v", err)
	}
	managedRoot := binding.ManagedWorktreesRoot(d.Tasks)
	if !drain.PathUnder(queuetest.Canon(t, d.Tasks, result.RuntimePath), queuetest.Canon(t, d.Tasks, managedRoot)) {
		t.Fatalf("runtime = %q, want a managed worktree under %q", result.RuntimePath, managedRoot)
	}
	b := queuetest.LoadBindingStore(t, d.Tasks)[drain.SetScopedKey(repoKey, setID)]
	if b.RuntimePath != result.RuntimePath || !b.Provisioned {
		t.Fatalf("binding = %+v, want provisioned managed worktree", b)
	}
	if !strings.HasPrefix(b.Branch, "pop/") {
		t.Fatalf("branch = %q, want pop/<set>/<stamp> forked from trunk", b.Branch)
	}
	if cmd, ok := queuetest.ExtractSpawnCommand(rt); !ok || !strings.Contains(cmd, "pop tasks implement "+setID) || !strings.Contains(cmd, "--task-runtime-path "+result.RuntimePath) {
		t.Fatalf("spawn command = %q, want implement for %s pinned to managed worktree %q", cmd, setID, result.RuntimePath)
	}
}

// TestDashboardDrainTargetTrunkDrainsInlineNoBinding asserts the trunk option
// drains in the trunk worktree and records no binding (an inline drain).
func TestDashboardDrainTargetTrunkDrainsInlineNoBinding(t *testing.T) {
	repo, setID, _ := queuetest.SetupSpawnRepo(t, "drain-trunk", []queuetest.SpawnTask{
		{ID: "01-a", File: "01-a.md", Title: "A", Type: "AFK", Status: "open"},
	})
	d, cfg, row, rt := dashboardLaunchFixture(t, repo, setID)
	repoKey, err := drain.ResolveRepoKey(d, repo)
	if err != nil {
		t.Fatal(err)
	}

	result, err := drain.LaunchDrainTarget(d, cfg, row, drain.DrainEntry{Kind: drain.DrainTargetTrunk, Path: repo})
	if err != nil {
		t.Fatalf("drain.LaunchDrainTarget trunk: %v", err)
	}
	if queuetest.Canon(t, d.Tasks, result.RuntimePath) != queuetest.Canon(t, d.Tasks, repo) {
		t.Fatalf("runtime = %q, want trunk %q", result.RuntimePath, repo)
	}
	if b, ok := queuetest.LoadBindingStore(t, d.Tasks)[drain.SetScopedKey(repoKey, setID)]; ok {
		t.Fatalf("trunk drain recorded a binding: %+v", b)
	}
	if cmd, ok := queuetest.ExtractSpawnCommand(rt); !ok || !strings.Contains(cmd, "pop tasks implement "+setID) || !strings.Contains(cmd, "--task-runtime-path "+result.RuntimePath) {
		t.Fatalf("spawn command = %q, want implement for %s pinned to trunk %q", cmd, setID, result.RuntimePath)
	}
}

// TestDashboardDrainTargetBareHidesTrunkOptions asserts a bare repo with no
// resolvable trunk offers only adoptable worktrees — never the trunk-dependent
// "new managed worktree" or trunk options.
func TestDashboardDrainTargetBareHidesTrunkOptions(t *testing.T) {
	_, wts := queuetest.InitBareRepoWithWorktrees(t, 2)
	checkout := wts[0]
	t.Setenv("XDG_DATA_HOME", filepath.Join(t.TempDir(), "xdg"))
	id, err := tasks.ResolveRepositoryIdentity(tasks.DefaultDeps(), checkout)
	if err != nil {
		t.Fatal(err)
	}
	setID := "bare-target"
	setDir := filepath.Join(id.TasksDir, setID)
	queuetest.WriteSpawnTaskMD(t, setDir, "01-a.md")
	queuetest.WriteSpawnManifest(t, setDir, []queuetest.SpawnTask{{ID: "01-a", File: "01-a.md", Title: "A", Type: "AFK", Status: "open"}})
	if _, err := tasks.RegisterWith(tasks.DefaultDeps(), id.TasksDir, tasks.StatePathFor(id.TasksDir)); err != nil {
		t.Fatal(err)
	}
	d, cfg, row, _ := dashboardLaunchFixture(t, checkout, setID)

	entries, err := drain.DrainTargetEntries(d, cfg, row)
	if err != nil {
		t.Fatalf("drain.DrainTargetEntries: %v", err)
	}
	if len(entries) == 0 {
		t.Fatalf("bare repo with worktrees should still list adopt targets")
	}
	for _, e := range entries {
		if e.Kind != drain.DrainTargetWorktree {
			t.Fatalf("bare repo must hide trunk-dependent options, got %+v", e)
		}
	}
	// With no trunk entry to prefer and a checkout the picker does not list, the
	// cursor keeps its first-entry fallback.
	if got := defaultDrainCursor(d, entries, filepath.Join(t.TempDir(), "elsewhere")); got != 0 {
		t.Fatalf("bare-repo fallback cursor = %d, want 0", got)
	}
	if got := defaultDrainCursor(d, entries, ""); got != 0 {
		t.Fatalf("unresolvable-checkout cursor = %d, want 0", got)
	}
}

// TestDrainTargetCursorOnTrunkPicksTrunkEntry asserts the picker opened from the
// Trunk worktree lands on "Trunk worktree (drain inline)" — drain here — rather
// than on the new-managed option that precedes it (ADR-0192).
func TestDrainTargetCursorOnTrunkPicksTrunkEntry(t *testing.T) {
	repo, setID, _ := queuetest.SetupSpawnRepo(t, "drain-cursor-trunk", []queuetest.SpawnTask{
		{ID: "01-a", File: "01-a.md", Title: "A", Type: "AFK", Status: "open"},
	})
	d, cfg, row, _ := dashboardLaunchFixture(t, repo, setID)

	entries, err := drain.DrainTargetEntries(d, cfg, row)
	if err != nil {
		t.Fatalf("drain.DrainTargetEntries: %v", err)
	}
	got := defaultDrainCursor(d, entries, repo)
	if got < 0 || got >= len(entries) || entries[got].Kind != drain.DrainTargetTrunk {
		t.Fatalf("cursor from trunk = %d of %+v, want the trunk entry", got, entries)
	}
}

// TestDashboardIKeyUnboundOpensPicker asserts that `i` on an unbound set opens
// the Drain target picker with the cursor on the checkout the dashboard runs in
// — here the trunk, so the trunk entry — while `i` on a bound set drains its
// binding directly with no picker.
func TestDashboardIKeyUnboundOpensPicker(t *testing.T) {
	repo, setID, _ := queuetest.SetupSpawnRepo(t, "i-key", []queuetest.SpawnTask{
		{ID: "01-a", File: "01-a.md", Title: "A", Type: "AFK", Status: "open"},
	})
	d, cfg, row, _ := dashboardLaunchFixture(t, repo, setID)
	repoKey, err := drain.ResolveRepoKey(d, repo)
	if err != nil {
		t.Fatal(err)
	}
	row.RepoKey = repoKey
	row.CursorKey = "pop\x00" + setID
	// The cursor default is read off the checkout the dashboard is standing in.
	t.Chdir(repo)

	m := newQueueDashboard(d, cfg, DashboardSnapshot{Containers: []DashboardRow{row}})
	// Drain now lives behind the action menu: open with `a`, then `i`.
	updated, _ := m.Update(tea.KeyPressMsg{Code: 'a', Text: "a"})
	got := updated.(QueueDashboard)
	updated, cmd := got.Update(tea.KeyPressMsg{Code: 'I', Text: "I"})
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
	if !ok || selected.Kind != drain.DrainTargetTrunk {
		t.Fatalf("default cursor entry = %+v (ok=%v), want the trunk entry", selected, ok)
	}
}

// TestDashboardIKeyBoundDrainsWithoutPicker asserts a bound set resumes in its
// binding on `i` with no picker.
func TestDashboardIKeyBoundDrainsWithoutPicker(t *testing.T) {
	repo, setID, _ := queuetest.SetupSpawnRepo(t, "i-bound", []queuetest.SpawnTask{
		{ID: "01-a", File: "01-a.md", Title: "A", Type: "AFK", Status: "open"},
	})
	bound := filepath.Join(t.TempDir(), "bound")
	runGit(t, repo, "worktree", "add", "--detach", bound, "HEAD")
	d, cfg, row, rt := dashboardLaunchFixture(t, repo, setID)
	repoKey, err := drain.ResolveRepoKey(d, repo)
	if err != nil {
		t.Fatal(err)
	}
	row.RepoKey = repoKey
	queuetest.SeedBindingStore(t, d.Tasks, map[string]drain.WorktreeBinding{
		drain.SetScopedKey(repoKey, setID): {RuntimePath: bound, Branch: "bound", Project: "pop", Provisioned: false},
	})

	m := newQueueDashboard(d, cfg, DashboardSnapshot{Containers: []DashboardRow{row}})
	// Drain now lives behind the action menu: open with `a`, then `i`.
	updated, _ := m.Update(tea.KeyPressMsg{Code: 'a', Text: "a"})
	got := updated.(QueueDashboard)
	_, cmd := got.Update(tea.KeyPressMsg{Code: 'I', Text: "I"})
	if cmd == nil {
		t.Fatal("i did not return a command")
	}
	msg := cmd()
	drainMsg, ok := msg.(dashboardHandoffMsg)
	if !ok {
		t.Fatalf("i on bound set produced %T, want dashboardHandoffMsg (no picker)", msg)
	}
	if drainMsg.err != nil {
		t.Fatalf("bound drain err = %v", drainMsg.err)
	}
	if cmd, ok := queuetest.ExtractSpawnCommand(rt); !ok || !strings.Contains(cmd, "pop tasks implement "+setID) || !strings.Contains(cmd, "--task-runtime-path "+bound) {
		t.Fatalf("spawn command = %q, want implement for %s pinned to bound checkout %q", cmd, setID, bound)
	}
}
