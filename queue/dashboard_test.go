package queue

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/glebglazov/pop/config"
	"github.com/glebglazov/pop/internal/deps"
	"github.com/glebglazov/pop/project"
	"github.com/glebglazov/pop/store"
	"github.com/glebglazov/pop/tasks"
)

func mkdirDrainStoreDir(t *testing.T, td *tasks.Deps) {
	t.Helper()
	dir := filepath.Dir(tasks.DrainStorePathWith(td))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("MkdirAll drain store dir: %v", err)
	}
}

func dashboardTestDeps(t *testing.T, rows []tasks.Row, locks map[string]*tasks.RuntimeLockStatus) *Deps {
	t.Helper()
	fs := &deps.MockFileSystem{
		EvalSymlinksFunc: func(path string) (string, error) { return path, nil },
		ReadFileFunc: func(path string) ([]byte, error) {
			return nil, os.ErrNotExist
		},
		StatFunc: func(path string) (os.FileInfo, error) {
			return nil, os.ErrNotExist
		},
	}
	git := &deps.MockGit{
		CommandInDirFunc: func(dir string, args ...string) (string, error) {
			cmd := strings.Join(args, " ")
			switch cmd {
			case "worktree list --porcelain":
				if strings.Contains(dir, "bare") {
					return "worktree /repo/bare.git\nbare\n\n", nil
				}
				return "worktree /repo/main\nHEAD abc\nbranch refs/heads/main\n\nworktree /repo/feature\nHEAD def\nbranch refs/heads/feature\n", nil
			case "branch --show-current":
				switch dir {
				case "/repo/main":
					return "main", nil
				case "/repo/feature":
					return "feature", nil
				case "/repo/bound":
					return "bound-branch", nil
				case "/repo/done":
					return "done-branch", nil
				}
				return "", nil
			default:
				return "", errors.New("unexpected git command: " + cmd)
			}
		},
	}
	return &Deps{
		Tasks:   &tasks.Deps{FS: fs, Git: git},
		Project: &project.Deps{FS: fs, Git: git},
		Refresh: func(string) (*tasks.RefreshResult, error) {
			return &tasks.RefreshResult{Rows: rows}, nil
		},
		ReadLock: func(runtimePath string) *tasks.RuntimeLockStatus {
			if locks != nil {
				if lock, ok := locks[runtimePath]; ok {
					return lock
				}
			}
			return &tasks.RuntimeLockStatus{RuntimePath: runtimePath}
		},
	}
}

// staticForScan builds the dashboardRepoStatic that the (removed) git-forking
// resolveRepoStatic used to produce for a synthetic single-scan repo group. The
// static side is now derived fork-free from markers (ADR-0060), so these tests
// express the integration target (rep) and its branch directly rather than
// mocking `git worktree list` / `git branch --show-current`. A bare repo has no
// integration target: rep is nil.
func staticForScan(scan projectScan, repBranch string, bare bool) dashboardRepoStatic {
	var rep *projectScan
	if !bare {
		s := scan
		rep = &s
	}
	return dashboardRepoStatic{
		defPath:       scan.DefinitionPath,
		statePath:     tasks.StatePathFor(scan.DefinitionPath),
		repoKey:       scan.RepoKey,
		repoCommonDir: scan.RepoCommonDir,
		projectName:   scan.Name,
		rep:           rep,
		repBranch:     repBranch,
		bare:          bare,
	}
}

// TestDashboardRowsVerifyFailedStatus confirms the dashboard applies the same
// SHA-gated Verify overlay as `pop tasks status`, not manifest status alone.
func TestDashboardRowsVerifyFailedStatus(t *testing.T) {
	enabled := &config.Config{Task: &config.TasksConfig{Verify: &config.VerifyConfig{Enabled: true}}}
	doneManifest := &tasks.Manifest{
		Valid: true,
		Tasks: []tasks.Task{{ID: "01-a", File: "01-a.md", Type: "AFK", Status: "done"}},
	}
	rows := []tasks.Row{{ID: "demo", Status: tasks.StatusDone}}
	td := queueDataDeps(t)
	d := dashboardTestDeps(t, rows, nil)
	d.Tasks = td
	d.Refresh = func(string) (*tasks.RefreshResult, error) {
		return &tasks.RefreshResult{
			Rows:      rows,
			Manifests: map[string]*tasks.Manifest{"demo": doneManifest},
		}, nil
	}
	d.Tasks.Git = &deps.MockGit{CommandInDirFunc: func(dir string, args ...string) (string, error) {
		switch {
		case len(args) >= 2 && args[0] == "rev-parse" && args[1] == "--git-common-dir":
			return "/repo/.git", nil
		case len(args) >= 2 && args[0] == "rev-parse" && args[1] == "HEAD":
			return "shaCUR", nil
		}
		return "", nil
	}}
	mkdirDrainStoreDir(t, td)
	s, err := store.Open(tasks.DrainStorePathWith(td))
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	if err := s.PutVerifyVerdict(store.VerifyVerdict{
		Repo: "/repo/.git", SetID: "demo", WorkSHA: "shaCUR", Verdict: "NEEDS-HUMAN", Findings: "criterion drift",
	}); err != nil {
		t.Fatalf("PutVerifyVerdict: %v", err)
	}
	_ = s.Close()

	scan := projectScan{
		Name:           "pop",
		ProjectPath:    "/repo/main",
		DefinitionPath: "/def",
		RepoKey:        "repo-key",
		RepoCommonDir:  "/repo/.git",
	}
	got, err := dashboardRowsForStatic(d, enabled, &DaemonState{Version: 1}, staticForScan(scan, "main", false))
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("rows = %+v, want one", got)
	}
	if got[0].RawStatus != tasks.StatusVerifyFailed {
		t.Fatalf("RawStatus = %q, want VERIFY-FAILED", got[0].RawStatus)
	}
	if got[0].Status != "VERIFY-FAILED" {
		t.Fatalf("Status = %q, want VERIFY-FAILED", got[0].Status)
	}
}

func TestDashboardShowRuleFiltering(t *testing.T) {
	rows := []tasks.Row{
		{ID: "ready", Status: tasks.StatusReady, AutoDrain: true},
		{ID: "failed", Status: tasks.StatusFailed},
		{ID: "blocked", Status: tasks.StatusBlocked},
		{ID: "deferred", Status: tasks.StatusDeferred},
		{ID: "missing", Status: tasks.StatusMissing},
		{ID: "malformed", Status: tasks.StatusMalformed},
		{ID: "done-integrating", Status: tasks.StatusDone},
		{ID: "done-concluded", Status: tasks.StatusDone},
	}
	d := dashboardTestDeps(t, rows, nil)
	dataHome := t.TempDir()
	real := deps.NewRealFileSystem()
	origFS := d.Tasks.FS.(*deps.MockFileSystem)
	d.Tasks.FS = &deps.MockFileSystem{
		GetenvFunc: func(key string) string {
			if key == "XDG_DATA_HOME" {
				return dataHome
			}
			return origFS.GetenvFunc(key)
		},
		EvalSymlinksFunc: origFS.EvalSymlinksFunc,
		ReadFileFunc:     real.ReadFile,
		WriteFileFunc:    real.WriteFile,
		MkdirAllFunc:     real.MkdirAll,
		RenameFunc:       real.Rename,
		StatFunc:         origFS.StatFunc,
	}
	// Binding-driven membership (ADR-0070): a Done set stays listed only while it
	// still holds a managed (pop-provisioned) Worktree binding. done-integrating
	// has one and still shows as DONE; done-concluded has none and stays hidden.
	seedBindingStore(t, d.Tasks, map[string]WorktreeBinding{
		setScopedKey("repo-key", "done-integrating"): {RuntimePath: "/repo/done", Branch: "done-branch", Provisioned: true},
	})
	state := &DaemonState{Version: 1}
	scan := projectScan{Name: "pop", ProjectPath: "/repo/main", RuntimePath: "/repo/main", DefinitionPath: "/def", RepoKey: "repo-key"}

	got, err := dashboardRowsForStatic(d, &config.Config{}, state, staticForScan(scan, "main", false))
	if err != nil {
		t.Fatal(err)
	}
	var ids []string
	byID := map[string]DashboardRow{}
	for _, row := range got {
		ids = append(ids, row.SetID)
		byID[row.SetID] = row
	}
	want := []string{"ready", "failed", "blocked", "deferred", "missing", "malformed", "done-integrating"}
	if !reflect.DeepEqual(ids, want) {
		t.Fatalf("ids = %v, want %v", ids, want)
	}
	if got := byID["done-integrating"]; got.Status != "DONE" {
		t.Fatalf("done-integrating row = %+v, want DONE", got)
	}
}

func TestDashboardSortOrder(t *testing.T) {
	rows := []DashboardRow{
		{Project: "zeta", SetRef: SetRef{SetID: "2026-01-01-old"}},
		{Project: "alpha", SetRef: SetRef{SetID: "2026-01-01-old"}},
		{Project: "alpha", SetRef: SetRef{SetID: "2026-06-18-new"}},
	}
	sortDashboardRows(rows)
	got := []string{rows[0].Project + "/" + rows[0].SetID, rows[1].Project + "/" + rows[1].SetID, rows[2].Project + "/" + rows[2].SetID}
	want := []string{"alpha/2026-06-18-new", "alpha/2026-01-01-old", "zeta/2026-01-01-old"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("order = %v, want %v", got, want)
	}
}

// TestDashboardTieredSortOrder drives the full agreed total order across a
// mixed fixture that exercises every membership tier and the per-project
// status sink. Tier precedence is running → auto-drain → orphaned → the rest;
// the orphaned + auto-drain set must land in the auto-drain tier; "the rest"
// groups by project then sinks AWAITING-APPROVAL below normal and DONE below those;
// and SetID descending is the global tiebreak.
func TestDashboardTieredSortOrder(t *testing.T) {
	rows := []DashboardRow{
		// "the rest" — project bravo, status sink within the project.
		{Project: "bravo", SetRef: SetRef{SetID: "2026-02-01-done", RawStatus: tasks.StatusDone}},
		{Project: "bravo", SetRef: SetRef{SetID: "2026-02-02-unver", RawStatus: tasks.StatusAwaitingApproval}},
		{Project: "bravo", SetRef: SetRef{SetID: "2026-02-03-ready", RawStatus: tasks.StatusReady}},
		// "the rest" — project alpha sorts before bravo.
		{Project: "alpha", SetRef: SetRef{SetID: "2026-03-01-a", RawStatus: tasks.StatusReady}},
		{Project: "alpha", SetRef: SetRef{SetID: "2026-03-02-b", RawStatus: tasks.StatusReady}},
		// Orphaned tier.
		{Project: "zoo", SetRef: SetRef{SetID: "2026-04-01-orph", RawStatus: tasks.StatusReady, Orphaned: true}},
		// Auto-drain tier — the orphaned+auto-drain set belongs here, not orphaned.
		{Project: "kilo", SetRef: SetRef{SetID: "2026-05-01-ad", RawStatus: tasks.StatusReady, AutoDrain: true}},
		{Project: "kilo", SetRef: SetRef{SetID: "2026-05-02-ado", RawStatus: tasks.StatusReady, AutoDrain: true, Orphaned: true}},
		// Running / Picked-up tier — highest precedence even with auto-drain set.
		{Project: "delta", Drain: "picked up", SetRef: SetRef{SetID: "2026-06-01-run", RawStatus: tasks.StatusReady, AutoDrain: true}},
	}
	sortDashboardRows(rows)
	got := make([]string, len(rows))
	for i, r := range rows {
		got[i] = r.Project + "/" + r.SetID
	}
	want := []string{
		// Tier 1: running.
		"delta/2026-06-01-run",
		// Tier 2: auto-drain, project name then SetID descending.
		"kilo/2026-05-02-ado",
		"kilo/2026-05-01-ad",
		// Tier 3: orphaned.
		"zoo/2026-04-01-orph",
		// Tier 4: the rest, grouped by project, status sink per project.
		"alpha/2026-03-02-b",
		"alpha/2026-03-01-a",
		"bravo/2026-02-03-ready", // normal first
		"bravo/2026-02-02-unver", // AWAITING-APPROVAL sinks below normal
		"bravo/2026-02-01-done",  // DONE sinks below AWAITING-APPROVAL
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("order = %v, want %v", got, want)
	}
}

func TestDashboardColumnDerivation(t *testing.T) {
	rows := []tasks.Row{
		{ID: "done", Status: tasks.StatusDone},
		{ID: "ready", Status: tasks.StatusReady, AutoDrain: true},
		{ID: "bound", Status: tasks.StatusBlocked},
	}
	d := dashboardTestDeps(t, rows, nil)
	dataHome := t.TempDir()
	real := deps.NewRealFileSystem()
	origFS := d.Tasks.FS.(*deps.MockFileSystem)
	d.Tasks.FS = &deps.MockFileSystem{
		GetenvFunc: func(key string) string {
			if key == "XDG_DATA_HOME" {
				return dataHome
			}
			return ""
		},
		EvalSymlinksFunc: origFS.EvalSymlinksFunc,
		ReadFileFunc: func(path string) ([]byte, error) {
			if origFS.ReadFileFunc != nil {
				if data, err := origFS.ReadFileFunc(path); err == nil || !errors.Is(err, os.ErrNotExist) {
					return data, err
				}
			}
			return real.ReadFile(path)
		},
		WriteFileFunc: real.WriteFile,
		MkdirAllFunc:  real.MkdirAll,
		RenameFunc:    real.Rename,
		StatFunc:      origFS.StatFunc,
	}
	state := &DaemonState{Version: 1}
	seedBindingStore(t, d.Tasks, map[string]WorktreeBinding{
		setScopedKey("repo-key", "done"):  {RuntimePath: "/repo/done", Branch: "done-branch", Provisioned: true},
		setScopedKey("repo-key", "bound"): {RuntimePath: "/repo/bound", Branch: "bound-branch"},
	})
	scan := projectScan{Name: "pop", ProjectPath: "/repo/main", RuntimePath: "/repo/main", DefinitionPath: "/def", RepoKey: "repo-key"}

	got, err := dashboardRowsForStatic(d, &config.Config{}, state, staticForScan(scan, "main", false))
	if err != nil {
		t.Fatal(err)
	}
	byID := map[string]DashboardRow{}
	for _, row := range got {
		byID[row.SetID] = row
	}
	if byID["done"].Status != "DONE" || byID["done"].Worktree != "done-branch" || byID["done"].destKind != dashboardDestDoneManagedBound {
		t.Fatalf("done row = %+v", byID["done"])
	}
	if byID["ready"].Status != "READY" || byID["ready"].Worktree != dashboardDestLabelNeedsBind || byID["ready"].destKind != dashboardDestNeedsBind {
		t.Fatalf("ready row = %+v", byID["ready"])
	}
	if byID["bound"].Worktree != "bound-branch" || byID["bound"].destKind != dashboardDestBound {
		t.Fatalf("bound row = %+v", byID["bound"])
	}
}

func TestDashboardNoBaseWorktree(t *testing.T) {
	d := dashboardTestDeps(t, []tasks.Row{{ID: "missing", Status: tasks.StatusMissing}}, nil)
	scan := projectScan{Name: "bare", ProjectPath: "/repo/bare.git", RuntimePath: "/repo/bare.git", DefinitionPath: "/def", RepoKey: "bare-key"}

	got, err := dashboardRowsForStatic(d, &config.Config{}, &DaemonState{Version: 1}, staticForScan(scan, "", true))
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Worktree != dashboardDestLabelNeedsBind || got[0].destKind != dashboardDestNeedsBind {
		t.Fatalf("rows = %+v, want needs bind", got)
	}
}

func TestDashboardPickedUpIndicator(t *testing.T) {
	d := dashboardTestDeps(t, []tasks.Row{
		{ID: "ready", Status: tasks.StatusReady, AutoDrain: true},
		{ID: "other", Status: tasks.StatusReady, AutoDrain: true},
	}, nil)
	// The drain column is served from the per-build snapshot's live-drain map
	// (one RunningDrains read), so the live drain is injected through the
	// LiveDrains seam rather than a per-row runtime-lock open.
	d.LiveDrains = func() ([]tasks.RunningDrain, error) {
		return []tasks.RunningDrain{
			{RuntimePath: "/repo/bound", SetID: "ready", PID: 123},
		}, nil
	}
	dataHome := t.TempDir()
	real := deps.NewRealFileSystem()
	origFS := d.Tasks.FS.(*deps.MockFileSystem)
	d.Tasks.FS = &deps.MockFileSystem{
		GetenvFunc: func(key string) string {
			if key == "XDG_DATA_HOME" {
				return dataHome
			}
			return ""
		},
		EvalSymlinksFunc: origFS.EvalSymlinksFunc,
		ReadFileFunc:     real.ReadFile,
		WriteFileFunc:    real.WriteFile,
		MkdirAllFunc:     real.MkdirAll,
		RenameFunc:       real.Rename,
		StatFunc:         origFS.StatFunc,
	}
	seedBindingStore(t, d.Tasks, map[string]WorktreeBinding{
		setScopedKey("repo-key", "ready"): {RuntimePath: "/repo/bound", Branch: "ready-branch"},
	})
	scan := projectScan{Name: "pop", ProjectPath: "/repo/main", RuntimePath: "/repo/main", DefinitionPath: "/def", RepoKey: "repo-key"}

	got, err := dashboardRowsForStatic(d, &config.Config{}, &DaemonState{Version: 1}, staticForScan(scan, "main", false))
	if err != nil {
		t.Fatal(err)
	}
	byID := map[string]DashboardRow{}
	for _, row := range got {
		byID[row.SetID] = row
	}
	if byID["ready"].Drain != "picked up" {
		t.Fatalf("ready drain = %q, want picked up", byID["ready"].Drain)
	}
	if byID["other"].Drain != "" {
		t.Fatalf("other drain = %q, want empty", byID["other"].Drain)
	}
}

// TestDashboardOrphanedIndicator covers the three orphaned-detection cases: a
// set whose bound checkout is missing on disk is orphaned; a set whose bound
// checkout still stats present is not; and a set with no binding can never be
// orphaned. Detection is a filesystem stat only — the mocked Git would error on
// any command, so this also asserts the build adds no git subprocess.
func TestDashboardOrphanedIndicator(t *testing.T) {
	rows := []tasks.Row{
		{ID: "present", Status: tasks.StatusBlocked},
		{ID: "missing", Status: tasks.StatusBlocked},
		{ID: "unbound", Status: tasks.StatusBlocked},
	}
	d := dashboardTestDeps(t, rows, nil)
	dataHome := t.TempDir()
	real := deps.NewRealFileSystem()
	origFS := d.Tasks.FS.(*deps.MockFileSystem)
	const presentPath = "/repo/present"
	d.Tasks.FS = &deps.MockFileSystem{
		GetenvFunc: func(key string) string {
			if key == "XDG_DATA_HOME" {
				return dataHome
			}
			return ""
		},
		EvalSymlinksFunc: origFS.EvalSymlinksFunc,
		ReadFileFunc:     real.ReadFile,
		WriteFileFunc:    real.WriteFile,
		MkdirAllFunc:     real.MkdirAll,
		RenameFunc:       real.Rename,
		StatFunc: func(path string) (os.FileInfo, error) {
			if path == presentPath {
				return deps.MockFileInfo{NameVal: "present", IsDirVal: true}, nil
			}
			return nil, os.ErrNotExist
		},
	}
	seedBindingStore(t, d.Tasks, map[string]WorktreeBinding{
		setScopedKey("repo-key", "present"): {RuntimePath: presentPath, Branch: "present-branch"},
		setScopedKey("repo-key", "missing"): {RuntimePath: "/repo/gone", Branch: "missing-branch"},
	})
	scan := projectScan{Name: "pop", ProjectPath: "/repo/main", RuntimePath: "/repo/main", DefinitionPath: "/def", RepoKey: "repo-key"}

	got, err := dashboardRowsForStatic(d, &config.Config{}, &DaemonState{Version: 1}, staticForScan(scan, "main", false))
	if err != nil {
		t.Fatal(err)
	}
	byID := map[string]DashboardRow{}
	for _, row := range got {
		byID[row.SetID] = row
	}
	if !byID["missing"].Orphaned {
		t.Fatalf("missing set should be orphaned: %+v", byID["missing"])
	}
	if byID["present"].Orphaned {
		t.Fatalf("present set should not be orphaned: %+v", byID["present"])
	}
	if byID["unbound"].Orphaned {
		t.Fatalf("unbound set should not be orphaned: %+v", byID["unbound"])
	}
	// The orphaned set must render its indicator; the present/unbound sets must not.
	var rendered strings.Builder
	renderDashboardTable(&rendered, []DashboardRow{byID["missing"]}, 0, 0)
	if !strings.Contains(rendered.String(), "orphaned") {
		t.Fatalf("orphaned indicator missing from row render:\n%s", rendered.String())
	}
}

// TestDashboardBuildBoundedStoreOpens asserts the per-build snapshot reads the
// store a bounded number of times no matter how many rows the build renders: the
// binding, mergeability, and live-drain reads are served from one snapshot, not
// reopened per row. It builds the same repo twice — once with a handful of rows,
// once with ten times as many — and asserts the store-open delta is identical
// (and small), which it cannot be if any of those reads scaled with row count.
func TestDashboardBuildBoundedStoreOpens(t *testing.T) {
	rowsN := func(n int) []tasks.Row {
		out := make([]tasks.Row, 0, n)
		for i := 0; i < n; i++ {
			out = append(out, tasks.Row{ID: fmt.Sprintf("set-%02d", i), Status: tasks.StatusFailed})
		}
		return out
	}

	build := func(t *testing.T, rows []tasks.Row) int64 {
		t.Helper()
		d := dashboardTestDeps(t, rows, nil)
		dataHome := t.TempDir()
		real := deps.NewRealFileSystem()
		origFS := d.Tasks.FS.(*deps.MockFileSystem)
		d.Tasks.FS = &deps.MockFileSystem{
			GetenvFunc: func(key string) string {
				if key == "XDG_DATA_HOME" {
					return dataHome
				}
				return ""
			},
			EvalSymlinksFunc: origFS.EvalSymlinksFunc,
			ReadFileFunc: func(path string) ([]byte, error) {
				if origFS.ReadFileFunc != nil {
					if data, err := origFS.ReadFileFunc(path); err == nil || !errors.Is(err, os.ErrNotExist) {
						return data, err
					}
				}
				return real.ReadFile(path)
			},
			WriteFileFunc: real.WriteFile,
			MkdirAllFunc:  real.MkdirAll,
			RenameFunc:    real.Rename,
			StatFunc:      origFS.StatFunc,
		}
		// Seed the binding store so pop.db exists: the snapshot's reads only open
		// the store when its file is already present.
		seedBindingStore(t, d.Tasks, map[string]WorktreeBinding{
			setScopedKey("repo-key", "set-00"): {RuntimePath: "/repo/bound", Branch: "bound-branch"},
		})
		scan := projectScan{Name: "pop", ProjectPath: "/repo/main", RuntimePath: "/repo/main", DefinitionPath: "/def", RepoKey: "repo-key"}

		before := store.OpenCount()
		if _, err := dashboardRowsForStatic(d, &config.Config{}, &DaemonState{Version: 1}, staticForScan(scan, "main", false)); err != nil {
			t.Fatal(err)
		}
		return store.OpenCount() - before
	}

	small := build(t, rowsN(3))
	large := build(t, rowsN(30))
	if large == 0 {
		t.Fatalf("build performed no store opens; the test is not exercising the snapshot reads")
	}
	if small != large {
		t.Fatalf("store opens scaled with row count: 3 rows = %d opens, 30 rows = %d opens", small, large)
	}
	// One snapshot per build: AllBindings + LiveRunningDrains. Allow a
	// little slack for incidental opens, but it must not grow with rows.
	if large > 6 {
		t.Fatalf("per-build store opens = %d, want a small bounded count", large)
	}
}

func TestDashboardAutoDrainBadgeAndToggle(t *testing.T) {
	rows := []DashboardRow{
		{Project: "pop", Status: "READY", Worktree: "/repo/main (main)", SetRef: SetRef{SetID: "marked", AutoDrain: true}},
		{Project: "pop", Status: "READY", Worktree: "/repo/main (main)", SetRef: SetRef{SetID: "plain"}},
	}
	var rendered strings.Builder
	renderDashboardTable(&rendered, rows, 0, 0)
	if !strings.Contains(rendered.String(), "Auto-drain") {
		t.Fatalf("missing auto-drain badge:\n%s", rendered.String())
	}

	var toggledDef, toggledState, toggledSet string
	d := &Deps{
		ToggleAutoDrain: func(defPath, statePath, setID string) (*tasks.AutoDrainResult, error) {
			toggledDef, toggledState, toggledSet = defPath, statePath, setID
			return &tasks.AutoDrainResult{TaskSetID: setID, AutoDrain: true}, nil
		},
	}
	m := newQueueDashboard(d, &config.Config{}, DashboardSnapshot{Rows: []DashboardRow{{Project: "pop", Status: "READY", Worktree: "/repo/main (main)", cursorKey: "pop\x00plain", SetRef: SetRef{SetID: "plain", DefPath: "/repo/tasks", StatePath: "/repo/state.json"}}}})
	// Auto-drain now lives behind the action menu: open with `a`, toggle with `a`.
	updated, _ := m.Update(tea.KeyPressMsg{Code: 'a', Text: "a"})
	got := updated.(QueueDashboard)
	if got.menu == nil {
		t.Fatal("a did not open the action menu")
	}
	updated, cmd := got.Update(tea.KeyPressMsg{Code: 'a', Text: "a"})
	got = updated.(QueueDashboard)
	if got.menu != nil {
		t.Fatal("a did not close the action menu after dispatch")
	}
	if !got.snap.Rows[0].AutoDrain {
		t.Fatalf("toggle did not update badge immediately: %+v", got.snap.Rows[0])
	}
	msg := cmd().(dashboardToggleMsg)
	updated, _ = got.Update(msg)
	got = updated.(QueueDashboard)
	if toggledDef != "/repo/tasks" || toggledState != "/repo/state.json" || toggledSet != "plain" {
		t.Fatalf("toggle target = (%q, %q, %q)", toggledDef, toggledState, toggledSet)
	}
	if !got.snap.Rows[0].AutoDrain || got.err != nil {
		t.Fatalf("toggle result = row %+v err %v", got.snap.Rows[0], got.err)
	}
}

func TestDashboardBKeyOpensBindModal(t *testing.T) {
	m := newQueueDashboard(&Deps{}, &config.Config{}, DashboardSnapshot{Rows: []DashboardRow{{Project: "pop", Status: "READY", Worktree: "/repo/main (main)", cursorKey: "pop\x00set-bind", SetRef: SetRef{SetID: "set-bind", DefPath: "/repo/tasks", StatePath: "/repo/state.json"}}}})
	// Bind now lives behind the action menu: open with `a`, then `b`.
	updated, _ := m.Update(tea.KeyPressMsg{Code: 'a', Text: "a"})
	got := updated.(QueueDashboard)
	if got.menu == nil {
		t.Fatal("a did not open the action menu")
	}
	updated, cmd := got.Update(tea.KeyPressMsg{Code: 'b', Text: "b"})
	got = updated.(QueueDashboard)
	if got.menu != nil {
		t.Fatal("b did not close the action menu after dispatch")
	}
	if got.bind == nil || !got.bind.loading || got.bind.row.SetID != "set-bind" {
		t.Fatalf("bind modal = %+v, want loading modal for set-bind", got.bind)
	}
	if cmd == nil {
		t.Fatalf("b key did not return a worktree-loading command")
	}
}

func TestDashboardActionMenuOpenAndClose(t *testing.T) {
	m := newQueueDashboard(&Deps{}, &config.Config{}, DashboardSnapshot{Rows: []DashboardRow{
		{Project: "pop", Status: "READY", cursorKey: "pop\x00set", SetRef: SetRef{SetID: "set", RuntimePath: "/repo/wt"}},
	}})
	m.width = 120
	m.height = 20

	// `a` opens the overlay, anchored to the cursored row, with the menu hint.
	updated, cmd := m.Update(tea.KeyPressMsg{Code: 'a', Text: "a"})
	got := updated.(QueueDashboard)
	if got.menu == nil {
		t.Fatal("a did not open the action menu")
	}
	if cmd != nil {
		t.Fatal("opening the menu should not dispatch a command")
	}
	if got.menu.row.SetID != "set" {
		t.Fatalf("menu opened on %q, want set", got.menu.row.SetID)
	}
	view := got.View().Content
	if !strings.Contains(view, "actions") {
		t.Fatalf("menu caption not rendered:\n%s", view)
	}
	if !strings.Contains(view, "enter/letter run · esc close") {
		t.Fatalf("menu hint not rendered:\n%s", view)
	}

	// `esc` closes the overlay without quitting.
	updated, cmd = got.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	got = updated.(QueueDashboard)
	if got.menu != nil {
		t.Fatal("esc did not close the action menu")
	}
	if cmd != nil {
		t.Fatal("closing the menu should not quit or dispatch")
	}
}

func TestDashboardFormerDirectKeysInertAtTopLevel(t *testing.T) {
	for _, key := range []string{"i", "I", "b", "U", "p", "P", "O", "d"} {
		m := newQueueDashboard(&Deps{}, &config.Config{}, DashboardSnapshot{Rows: []DashboardRow{{Project: "pop", Status: "READY", Worktree: "/repo/wt (main)", cursorKey: "pop\x00set", SetRef: SetRef{SetID: "set", DefPath: "/repo/tasks", StatePath: "/repo/state.json", RuntimePath: "/repo/wt", Bound: true, Parked: true}}}})
		updated, cmd := m.Update(tea.KeyPressMsg{Code: []rune(key)[0], Text: key})
		got := updated.(QueueDashboard)
		if cmd != nil {
			t.Fatalf("%q at top level dispatched a command; verbs must run only through the menu", key)
		}
		if got.menu != nil || got.bind != nil || got.abandon != nil || got.drainPick != nil {
			t.Fatalf("%q at top level opened a modal: menu=%v bind=%v abandon=%v drain=%v",
				key, got.menu, got.bind, got.abandon, got.drainPick)
		}
		if got.snap.Rows[0].AutoDrain {
			t.Fatalf("%q at top level toggled auto-drain", key)
		}
	}
}

func TestDashboardActionMenuContextFiltering(t *testing.T) {
	keysFor := func(row DashboardRow) []string {
		var keys []string
		for _, item := range dashboardMenuItems(row) {
			keys = append(keys, item.key)
		}
		return keys
	}

	// A plain ready set: only the unconditional verbs plus auto-drain (non-orphaned).
	plain := keysFor(DashboardRow{SetRef: SetRef{SetID: "plain", RuntimePath: "/wt"}})
	if want := []string{"i", "b", "a", "p", "O", "A"}; !reflect.DeepEqual(plain, want) {
		t.Fatalf("plain row verbs = %v, want %v", plain, want)
	}

	// Bound row gains unbind; unbound row does not.
	if got := keysFor(DashboardRow{SetRef: SetRef{SetID: "bound", Bound: true}}); !contains(got, "U") {
		t.Fatalf("bound row missing unbind: %v", got)
	}
	if got := keysFor(DashboardRow{SetRef: SetRef{SetID: "unbound"}}); contains(got, "U") {
		t.Fatalf("unbound row should not offer unbind: %v", got)
	}

	// Parked row gains unpark; non-parked row does not.
	if got := keysFor(DashboardRow{SetRef: SetRef{SetID: "parked", Parked: true}}); !contains(got, "P") {
		t.Fatalf("parked row missing unpark: %v", got)
	}
	if got := keysFor(DashboardRow{SetRef: SetRef{SetID: "live"}}); contains(got, "P") {
		t.Fatalf("non-parked row should not offer unpark: %v", got)
	}

	// Auto-drain is offered for non-orphaned rows only.
	if got := keysFor(DashboardRow{SetRef: SetRef{SetID: "orphan", Orphaned: true}}); contains(got, "a") {
		t.Fatalf("orphaned row should not offer auto-drain: %v", got)
	}
}

func TestDashboardActionMenuVerbDispatch(t *testing.T) {
	newModel := func() QueueDashboard {
		return newQueueDashboard(&Deps{}, &config.Config{}, DashboardSnapshot{Rows: []DashboardRow{{Project: "pop", Status: "READY", Worktree: "/repo/wt (main)", cursorKey: "pop\x00set", SetRef: SetRef{SetID: "set", DefPath: "/repo/tasks", StatePath: "/repo/state.json", RuntimePath: "/repo/wt", Bound: true}}}})
	}

	// Letter path: `a` then `U` opens the unbind confirm and closes the menu.
	updated, _ := newModel().Update(tea.KeyPressMsg{Code: 'a', Text: "a"})
	got := updated.(QueueDashboard)
	updated, cmd := got.Update(tea.KeyPressMsg{Code: 'U', Text: "U"})
	got = updated.(QueueDashboard)
	if got.menu != nil {
		t.Fatal("letter dispatch did not close the menu")
	}
	if got.abandon == nil {
		t.Fatal("letter dispatch did not open the unbind confirm")
	}
	if cmd != nil {
		t.Fatal("unbind confirm should not dispatch before confirmation")
	}

	// Highlight + Enter path: move onto the bind verb, press Enter.
	updated, _ = newModel().Update(tea.KeyPressMsg{Code: 'a', Text: "a"})
	got = updated.(QueueDashboard)
	bindIdx := -1
	for i, item := range got.menu.list.Items() {
		if item.key == "b" {
			bindIdx = i
		}
	}
	if bindIdx < 0 {
		t.Fatalf("bind verb absent from menu: %+v", got.menu.list.Items())
	}
	for got.menu.list.Cursor() != bindIdx {
		updated, _ = got.Update(tea.KeyPressMsg{Code: 'j', Text: "j"})
		got = updated.(QueueDashboard)
	}
	updated, cmd = got.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	got = updated.(QueueDashboard)
	if got.menu != nil {
		t.Fatal("enter dispatch did not close the menu")
	}
	if got.bind == nil || got.bind.row.SetID != "set" {
		t.Fatalf("enter dispatch did not open bind modal: %+v", got.bind)
	}
	if cmd == nil {
		t.Fatal("bind dispatch should return a worktree-loading command")
	}
}

func TestDashboardActionMenuArchiveDispatch(t *testing.T) {
	var archivedDef, archivedSet string
	d := &Deps{
		ArchiveSet: func(defPath, setID string) error {
			archivedDef, archivedSet = defPath, setID
			return nil
		},
	}
	// A DONE, bound row: archive is offered regardless of status.
	m := newQueueDashboard(d, &config.Config{}, DashboardSnapshot{Rows: []DashboardRow{{Project: "pop", Status: "DONE", Worktree: "/repo/wt (main)", cursorKey: "pop\x00set", SetRef: SetRef{SetID: "set", DefPath: "/repo/tasks", StatePath: "/repo/state.json", RuntimePath: "/repo/wt", Bound: true}}}})

	// Archive lives behind the action menu: open with `a`, archive with `A`.
	updated, _ := m.Update(tea.KeyPressMsg{Code: 'a', Text: "a"})
	got := updated.(QueueDashboard)
	if got.menu == nil {
		t.Fatal("a did not open the action menu")
	}
	var keys []string
	for _, item := range got.menu.list.Items() {
		keys = append(keys, item.key)
	}
	if !contains(keys, "A") {
		t.Fatalf("archive verb absent from a DONE bound row's menu: %v", keys)
	}

	updated, cmd := got.Update(tea.KeyPressMsg{Code: 'A', Text: "A"})
	got = updated.(QueueDashboard)
	if got.menu != nil {
		t.Fatal("A did not close the action menu after dispatch")
	}
	// No confirmation prompt: archiving opens no modal (it is fully reversible).
	if got.bind != nil || got.abandon != nil || got.drainPick != nil {
		t.Fatalf("archive opened a confirmation modal: bind=%v abandon=%v drain=%v", got.bind, got.abandon, got.drainPick)
	}
	if cmd == nil {
		t.Fatal("archive dispatch returned no command")
	}
	msg, ok := cmd().(dashboardArchiveMsg)
	if !ok {
		t.Fatalf("archive cmd produced %T, want dashboardArchiveMsg", msg)
	}
	if msg.err != nil {
		t.Fatalf("archive msg err = %v", msg.err)
	}
	if archivedDef != "/repo/tasks" || archivedSet != "set" {
		t.Fatalf("archive target = (%q, %q), want (/repo/tasks, set)", archivedDef, archivedSet)
	}

	updated, _ = got.Update(msg)
	got = updated.(QueueDashboard)
	if got.err != nil {
		t.Fatalf("archive result err = %v", got.err)
	}
	if !strings.Contains(got.statusMsg, "archived") {
		t.Fatalf("status = %q, want archived confirmation", got.statusMsg)
	}
}

// TestDashboardArchiveRetainsBinding exercises the default archive flag-write
// path end to end: archiving a bound set sets the reversible archived flag in
// Task state (so the row drops out on the next build, which excludes Archived
// sets) while leaving the set's Worktree binding intact.
func TestDashboardArchiveRetainsBinding(t *testing.T) {
	td := queueDataDeps(t)

	tasksDir := filepath.Join(t.TempDir(), "tasks")
	statePath := tasks.StatePathFor(tasksDir)
	canon, err := tasks.CanonicalDefinitionPathWith(td, tasksDir)
	if err != nil {
		t.Fatal(err)
	}

	// Register a non-archived set directly in Task state.
	if err := tasks.UpdateGlobalStateWith(td, statePath, func(state *tasks.GlobalState) error {
		if state.Tasks == nil {
			state.Tasks = map[string]*tasks.TaskEntry{}
		}
		state.Tasks[canon] = &tasks.TaskEntry{TaskSets: []tasks.RegisteredTaskSet{{ID: "set-1"}}}
		return nil
	}); err != nil {
		t.Fatalf("seed state: %v", err)
	}

	// A Worktree binding exists for the set before archiving.
	seedBindingStore(t, td, map[string]WorktreeBinding{
		"proj\x00set-1": {RuntimePath: "/repo/wt", Branch: "set-1", Project: "proj"},
	})

	d := &Deps{Tasks: td}
	m := newQueueDashboard(d, &config.Config{}, DashboardSnapshot{Rows: []DashboardRow{{Project: "proj", Status: "DONE", cursorKey: "proj\x00set-1", SetRef: SetRef{SetID: "set-1", DefPath: tasksDir, StatePath: statePath, RuntimePath: "/repo/wt", Bound: true}}}})

	updated, _ := m.Update(tea.KeyPressMsg{Code: 'a', Text: "a"})
	got := updated.(QueueDashboard)
	updated, cmd := got.Update(tea.KeyPressMsg{Code: 'A', Text: "A"})
	got = updated.(QueueDashboard)
	if cmd == nil {
		t.Fatal("archive dispatch returned no command")
	}
	msg, ok := cmd().(dashboardArchiveMsg)
	if !ok {
		t.Fatalf("archive msg type = %T", cmd())
	}
	if msg.err != nil {
		t.Fatalf("archive msg err = %v", msg.err)
	}

	// The reversible archived flag is now set through the real flag-write path,
	// so the dashboard's next build (which excludes Archived sets) drops the row.
	state, err := tasks.LoadGlobalStateWith(td, statePath)
	if err != nil {
		t.Fatal(err)
	}
	entry := state.Tasks[canon]
	if entry == nil || len(entry.TaskSets) != 1 {
		t.Fatalf("registered sets = %+v, want one", entry)
	}
	if !entry.TaskSets[0].Archived {
		t.Fatalf("set-1 not archived after dispatch: %+v", entry.TaskSets[0])
	}

	// The Worktree binding is retained — archive never touches it.
	after := loadBindingStore(t, td)
	if len(after) != 1 {
		t.Fatalf("binding state = %+v, want retained after archive", after)
	}
	if _, ok := after["proj\x00set-1"]; !ok {
		t.Fatalf("binding for set-1 lost after archive: %+v", after)
	}
}

func TestDashboardActionMenuAnchorsBelowAndFlipsAbove(t *testing.T) {
	// dashboardMenuPlaceBelow: a cursor near the top fits the menu below it; a
	// cursor low in the list does not, so it flips above.
	if !dashboardMenuPlaceBelow(0, 6, 24) {
		t.Fatal("menu should render below a top-of-list cursor")
	}
	if dashboardMenuPlaceBelow(18, 6, 24) {
		t.Fatal("menu should flip above a bottom-of-list cursor")
	}
	if !dashboardMenuPlaceBelow(5, 6, 0) {
		t.Fatal("menu should default below when height is unknown")
	}

	rows := make([]DashboardRow, 20)
	for i := range rows {
		id := fmt.Sprintf("set-%02d", i)
		rows[i] = DashboardRow{Project: "pop", Status: "READY", cursorKey: "pop\x00" + id, SetRef: SetRef{SetID: id}}
	}

	// Cursor at the top: the menu caption sits below the cursor row.
	mTop := newQueueDashboard(&Deps{}, &config.Config{}, DashboardSnapshot{Rows: rows})
	mTop.width = 120
	mTop.height = 24
	mTop.list.SetCursor(0)
	updated, _ := mTop.Update(tea.KeyPressMsg{Code: 'a', Text: "a"})
	topView := updated.(QueueDashboard).View().Content
	if got := menuCaptionLine(topView); got <= cursorRowLine(topView, "set-00") {
		t.Fatalf("top cursor: caption line %d should be below cursor row:\n%s", got, topView)
	}

	// Cursor at the bottom: the menu caption flips above the cursor row.
	mBot := newQueueDashboard(&Deps{}, &config.Config{}, DashboardSnapshot{Rows: rows})
	mBot.width = 120
	mBot.height = 24
	mBot.list.SetCursor(len(rows) - 1)
	updated, _ = mBot.Update(tea.KeyPressMsg{Code: 'a', Text: "a"})
	botView := updated.(QueueDashboard).View().Content
	if got := menuCaptionLine(botView); got >= cursorRowLine(botView, "set-19") {
		t.Fatalf("bottom cursor: caption line %d should be above cursor row:\n%s", got, botView)
	}
}

func contains(haystack []string, needle string) bool {
	for _, s := range haystack {
		if s == needle {
			return true
		}
	}
	return false
}

func menuCaptionLine(view string) int {
	return dashboardTestLineIndex(strings.Split(view, "\n"), "actions")
}

func cursorRowLine(view, setID string) int {
	return dashboardTestLineIndex(strings.Split(view, "\n"), setID)
}

func TestDashboardStatusKeysOpenDetailViewAndClosePreservesCursor(t *testing.T) {
	m := newQueueDashboard(&Deps{}, &config.Config{}, DashboardSnapshot{Rows: []DashboardRow{
		{Project: "pop", Status: "READY", cursorKey: "pop\x00first", SetRef: SetRef{SetID: "first"}},
		{Project: "pop", Status: "READY", cursorKey: "pop\x00second", SetRef: SetRef{SetID: "second"}},
	}})
	m.list.SetCursor(1)

	updated, cmd := m.Update(tea.KeyPressMsg{Code: 'l', Text: "l"})
	got := updated.(QueueDashboard)
	if got.detail == nil || !got.detail.loading || got.detail.row.SetID != "second" {
		t.Fatalf("detail view = %+v, want loading for second", got.detail)
	}
	if cmd == nil {
		t.Fatalf("l key did not return a loading command")
	}
	// View must be the detail view (no table).
	view := got.View().Content
	if strings.Contains(view, "project") && strings.Contains(view, "task set") {
		t.Fatalf("view should not show the table when detail is open:\n%s", view)
	}

	// Exit: h closes detail, returns to queue table, cursor unchanged.
	updated, cmd = got.Update(tea.KeyPressMsg{Code: 'h', Text: "h"})
	got = updated.(QueueDashboard)
	if cmd != nil {
		t.Fatalf("h in detail view should close without quitting")
	}
	if got.detail != nil || got.list.Cursor() != 1 {
		t.Fatalf("after close: detail=%+v cursor=%d, want nil and 1", got.detail, got.list.Cursor())
	}

	// Exit via esc also works.
	m2 := newQueueDashboard(&Deps{}, &config.Config{}, DashboardSnapshot{Rows: []DashboardRow{
		{Project: "pop", Status: "READY", cursorKey: "pop\x00alpha", SetRef: SetRef{SetID: "alpha"}},
	}})
	updated, _ = m2.Update(tea.KeyPressMsg{Code: 'l', Text: "l"})
	updated, cmd = updated.(QueueDashboard).Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	if cmd != nil || updated.(QueueDashboard).detail != nil {
		t.Fatalf("esc should close detail view without quitting")
	}

	for _, tc := range []struct {
		name string
		msg  tea.KeyPressMsg
	}{
		{name: "enter", msg: tea.KeyPressMsg{Code: tea.KeyEnter}},
	} {
		t.Run(tc.name+" opens detail", func(t *testing.T) {
			m := newQueueDashboard(&Deps{}, &config.Config{}, DashboardSnapshot{Rows: []DashboardRow{
				{Project: "pop", Status: "READY", cursorKey: "pop\x00target", SetRef: SetRef{SetID: "target"}},
			}})
			updated, cmd := m.Update(tc.msg)
			got := updated.(QueueDashboard)
			if got.detail == nil || !got.detail.loading || got.detail.row.SetID != "target" {
				t.Fatalf("detail view = %+v, want loading for target", got.detail)
			}
			if cmd == nil {
				t.Fatalf("%s key did not return a loading command", tc.name)
			}
		})
	}
}

func TestDashboardViewUsesTaskTableHeaderAndBottomShortcutLegend(t *testing.T) {
	m := newQueueDashboard(&Deps{}, &config.Config{}, DashboardSnapshot{Rows: []DashboardRow{
		{Project: "pop", Status: "READY", Worktree: "main", Drain: "picked up", cursorKey: "pop\x00set", SetRef: SetRef{SetID: "set", RawStatus: tasks.StatusReady, AutoDrain: true}},
		{Project: "pop", Status: "DONE", Worktree: "main", cursorKey: "pop\x00done", SetRef: SetRef{SetID: "done", RawStatus: tasks.StatusDone}},
	}})
	m.width = 120
	m.height = 8

	view := m.View().Content
	if strings.Contains(view, "Queue dashboard") {
		t.Fatalf("task-set list should use summary instead of dashboard title:\n%s", view)
	}
	if !strings.Contains(view, "Queue · 2 task sets · 1 ready · 1 running · 1 auto-drain") {
		t.Fatalf("task-set list should render useful summary:\n%s", view)
	}
	for _, want := range []string{"PROJECT  TASK SET  STATUS", "-------  --------  ------"} {
		if !strings.Contains(view, want) {
			t.Fatalf("view missing %q:\n%s", want, view)
		}
	}
	lines := strings.Split(view, "\n")
	if got, want := len(lines), m.height; got != want {
		t.Fatalf("line count = %d, want %d:\n%s", got, want, view)
	}
	if !strings.Contains(lines[len(lines)-1], "j/k move") {
		t.Fatalf("shortcut legend should be on bottom line:\n%s", view)
	}
	if got, want := dashboardTestLineIndex(lines, "PROJECT"), 2; got != want {
		t.Fatalf("task-set table header line = %d, want %d:\n%s", got, want, view)
	}
}

func TestDashboardTableClampsToBodyHeight(t *testing.T) {
	// Many rows on a short terminal must not overflow: the List scroll window
	// caps the body at the Frame's budget instead of rendering every row.
	rows := make([]DashboardRow, 40)
	for i := range rows {
		id := fmt.Sprintf("set-%02d", i)
		rows[i] = DashboardRow{Project: "pop", Status: "READY", cursorKey: "pop\x00" + id, SetRef: SetRef{SetID: id}}
	}
	m := newQueueDashboard(&Deps{}, &config.Config{}, DashboardSnapshot{Rows: rows})
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 10})
	m = updated.(QueueDashboard)

	view := m.View().Content
	lines := strings.Split(view, "\n")
	if got, want := len(lines), m.height; got != want {
		t.Fatalf("view line count = %d, want %d (clamped to body height):\n%s", got, want, view)
	}
}

func TestDashboardDetailViewOmitsTitleAndUsesBottomShortcutLegend(t *testing.T) {
	manifest := &tasks.Manifest{
		Valid: true,
		Tasks: []tasks.Task{{ID: "01-a", File: "01-a.md", Title: "First", Type: "AFK", Status: "open"}},
	}
	m := newQueueDashboard(&Deps{}, &config.Config{}, DashboardSnapshot{Rows: []DashboardRow{
		{Project: "pop", Status: "READY", cursorKey: "pop\x00set-normal", SetRef: SetRef{SetID: "set-normal"}},
	}})
	m.width = 120
	m.height = 8
	d := newDetailView(m.snap.Rows[0])
	d.syncManifest(manifest, nil)
	m.detail = d

	view := m.View().Content
	if strings.Contains(view, "Queue dashboard") {
		t.Fatalf("detail view should not render dashboard title:\n%s", view)
	}
	if !strings.Contains(view, "Task · set-normal") {
		t.Fatalf("detail view should render task prefix:\n%s", view)
	}
	lines := strings.Split(view, "\n")
	if got, want := len(lines), m.height; got != want {
		t.Fatalf("line count = %d, want %d:\n%s", got, want, view)
	}
	if !strings.Contains(lines[len(lines)-1], "a actions") {
		t.Fatalf("detail shortcut legend should be on bottom line:\n%s", view)
	}
	if got, want := dashboardTestLineIndex(lines, "STATUS"), 2; got != want {
		t.Fatalf("detail table header line = %d, want %d:\n%s", got, want, view)
	}
}

func TestDashboardDetailViewClampsToBodyHeight(t *testing.T) {
	// Many tasks on a short terminal must not overflow: the detail List scroll
	// window caps the body at the Frame's budget instead of rendering every task.
	manifestTasks := make([]tasks.Task, 40)
	for i := range manifestTasks {
		id := fmt.Sprintf("%02d-t", i)
		manifestTasks[i] = tasks.Task{ID: id, File: id + ".md", Title: "T", Type: "AFK", Status: "open"}
	}
	manifest := &tasks.Manifest{Valid: true, Tasks: manifestTasks}
	m := newQueueDashboard(&Deps{}, &config.Config{}, DashboardSnapshot{Rows: []DashboardRow{
		{Project: "pop", Status: "READY", cursorKey: "pop\x00set-long", SetRef: SetRef{SetID: "set-long"}},
	}})
	m.width = 120
	m.height = 10
	d := newDetailView(m.snap.Rows[0])
	d.syncManifest(manifest, nil)
	m.detail = d

	view := m.viewDetail()
	lines := strings.Split(view, "\n")
	if got, want := len(lines), m.height; got != want {
		t.Fatalf("detail view line count = %d, want %d (clamped to body height):\n%s", got, want, view)
	}
}

// TestDashboardBindModalListStagesNavigateAndSelect drives the bind modal's two
// list stages through the List: j moves the cursor (wrapping), and Enter on the
// base-ref stage records the highlighted ref and advances to the name stage.
func TestDashboardBindModalListStagesNavigateAndSelect(t *testing.T) {
	row := DashboardRow{Project: "pop", SetRef: SetRef{SetID: "set-bind"}}
	m := newQueueDashboard(&Deps{}, &config.Config{}, DashboardSnapshot{Rows: []DashboardRow{row}})

	// Worktree stage: entries arrive; wrap up from the top lands on the last.
	updated, _ := m.Update(dashboardBindListMsg{row: row, entries: []dashboardBindEntry{
		{Label: "wt-a", Path: "/a"},
		{Label: "wt-b", Path: "/b"},
		{Create: true, Label: "create new..."},
	}})
	m = updated.(QueueDashboard)
	updated, _ = m.Update(tea.KeyPressMsg{Code: 'k', Text: "k"})
	m = updated.(QueueDashboard)
	sel, ok := m.bind.list.Selected()
	if !ok || !sel.Create {
		t.Fatalf("k wrap in worktree stage selected %+v (ok=%v), want the create entry", sel, ok)
	}

	// Base-ref stage: refs arrive; j moves to the second ref, Enter records it.
	updated, _ = m.Update(dashboardBindRefsMsg{refs: []string{"main", "develop"}})
	m = updated.(QueueDashboard)
	if m.bind.stage != dashboardBindStageBaseRef {
		t.Fatalf("stage = %d, want base-ref", m.bind.stage)
	}
	updated, _ = m.Update(tea.KeyPressMsg{Code: 'j', Text: "j"})
	m = updated.(QueueDashboard)
	updated, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	m = updated.(QueueDashboard)
	if m.bind.stage != dashboardBindStageName {
		t.Fatalf("stage = %d, want name after base-ref select", m.bind.stage)
	}
	if m.bind.baseRef != "develop" {
		t.Fatalf("baseRef = %q, want develop", m.bind.baseRef)
	}
}

func TestDashboardBindModalClampsToBodyHeight(t *testing.T) {
	// A long worktree list on a short terminal must not overflow: the modal's
	// List scroll window caps its body instead of rendering every entry.
	entries := make([]dashboardBindEntry, 40)
	for i := range entries {
		entries[i] = dashboardBindEntry{Label: fmt.Sprintf("wt-%02d", i)}
	}
	m := newQueueDashboard(&Deps{}, &config.Config{}, DashboardSnapshot{Rows: []DashboardRow{
		{Project: "pop", Status: "READY", cursorKey: "pop\x00set-bind", SetRef: SetRef{SetID: "set-bind"}},
	}})
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 12})
	m = updated.(QueueDashboard)
	m.bind = &dashboardBindModal{row: m.snap.Rows[0], list: newBindEntryList(entries)}

	view := m.View().Content
	lines := strings.Split(view, "\n")
	if got, want := len(lines), m.height; got != want {
		t.Fatalf("bind modal view line count = %d, want %d (clamped to body height):\n%s", got, want, view)
	}
}

func TestDashboardDrainModalClampsToBodyHeight(t *testing.T) {
	// A long drain-target list on a short terminal must not overflow: the modal's
	// List scroll window caps its body instead of rendering every entry.
	entries := make([]dashboardDrainEntry, 40)
	for i := range entries {
		entries[i] = dashboardDrainEntry{Label: fmt.Sprintf("target-%02d", i)}
	}
	m := newQueueDashboard(&Deps{}, &config.Config{}, DashboardSnapshot{Rows: []DashboardRow{
		{Project: "pop", Status: "READY", cursorKey: "pop\x00set-drain", SetRef: SetRef{SetID: "set-drain"}},
	}})
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 12})
	m = updated.(QueueDashboard)
	m.drainPick = newDashboardDrainModal(m.snap.Rows[0], entries)

	view := m.View().Content
	lines := strings.Split(view, "\n")
	if got, want := len(lines), m.height; got != want {
		t.Fatalf("drain modal view line count = %d, want %d (clamped to body height):\n%s", got, want, view)
	}
}

func dashboardTestLineIndex(lines []string, needle string) int {
	for i, line := range lines {
		if strings.Contains(line, needle) {
			return i
		}
	}
	return -1
}

func TestDashboardQAndSAreUnbound(t *testing.T) {
	m := newQueueDashboard(&Deps{}, &config.Config{}, DashboardSnapshot{Rows: []DashboardRow{
		{Project: "pop", Status: "READY", cursorKey: "pop\x00set", SetRef: SetRef{SetID: "set"}},
	}})
	got := m
	for _, key := range []string{"q", "s"} {
		updated, cmd := got.Update(tea.KeyPressMsg{Code: []rune(key)[0], Text: key})
		got = updated.(QueueDashboard)
		if cmd != nil {
			t.Fatalf("%s at top level returned command, want no-op", key)
		}
		if got.list.Cursor() != 0 || got.detail != nil {
			t.Fatalf("%s changed model: cursor=%d detail=%+v", key, got.list.Cursor(), got.detail)
		}
	}

	got.detail = newDetailView(got.snap.Rows[0])
	for _, key := range []string{"q", "s"} {
		updated, cmd := got.Update(tea.KeyPressMsg{Code: []rune(key)[0], Text: key})
		got = updated.(QueueDashboard)
		if cmd != nil {
			t.Fatalf("%s in detail returned command, want no-op", key)
		}
		if got.detail == nil {
			t.Fatalf("%s in detail closed detail view", key)
		}
	}
}

func TestDashboardDetailViewPeekTaskText(t *testing.T) {
	taskPath := filepath.Join("/tasks", "set-peek", "01-a.md")
	d := &Deps{Tasks: &tasks.Deps{FS: &deps.MockFileSystem{
		ReadFileFunc: func(path string) ([]byte, error) {
			if path != taskPath {
				t.Fatalf("read path = %q, want %q", path, taskPath)
			}
			return []byte("# Task A\n\nFull task body.\n"), nil
		},
	}}}
	manifest := &tasks.Manifest{
		Dir:   filepath.Join("/tasks", "set-peek"),
		Valid: true,
		Tasks: []tasks.Task{{ID: "01-a", File: "01-a.md", Type: "AFK", Status: "open"}},
	}
	m := newQueueDashboard(d, &config.Config{}, DashboardSnapshot{Rows: []DashboardRow{
		{Project: "pop", Status: "READY", cursorKey: "pop\x00set-peek", SetRef: SetRef{SetID: "set-peek"}},
	}})
	d0 := newDetailView(m.snap.Rows[0])
	d0.syncManifest(manifest, nil)
	m.detail = d0

	updated, cmd := m.Update(tea.KeyPressMsg{Code: 'l', Text: "l"})
	got := updated.(QueueDashboard)
	if got.detail.peek == nil || !got.detail.peek.loading || got.detail.peek.taskID != "01-a" {
		t.Fatalf("peek = %+v, want loading peek for 01-a", got.detail.peek)
	}
	if cmd == nil {
		t.Fatalf("l in detail did not return task-text loading command")
	}
	msg := cmd()
	updated, _ = got.Update(msg)
	got = updated.(QueueDashboard)
	if got.detail.peek == nil || got.detail.peek.loading || got.detail.peek.err != nil {
		t.Fatalf("loaded peek = %+v, want loaded text", got.detail.peek)
	}
	view := got.View().Content
	for _, want := range []string{"set-peek / 01-a", taskPath, "# Task A", "Full task body."} {
		if !strings.Contains(view, want) {
			t.Fatalf("peek view missing %q:\n%s", want, view)
		}
	}

	updated, cmd = got.Update(tea.KeyPressMsg{Code: 'h', Text: "h"})
	got = updated.(QueueDashboard)
	if cmd != nil {
		t.Fatalf("h from task-text peek returned command")
	}
	if got.detail == nil || got.detail.peek != nil {
		t.Fatalf("h should close peek but keep detail: detail=%+v", got.detail)
	}

	m2 := newQueueDashboard(d, &config.Config{}, DashboardSnapshot{Rows: []DashboardRow{
		{Project: "pop", Status: "READY", cursorKey: "pop\x00set-peek", SetRef: SetRef{SetID: "set-peek"}},
	}})
	d2 := newDetailView(m2.snap.Rows[0])
	d2.syncManifest(manifest, nil)
	m2.detail = d2
	updated, cmd = m2.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	got = updated.(QueueDashboard)
	if got.detail.peek == nil || !got.detail.peek.loading || got.detail.peek.taskID != "01-a" {
		t.Fatalf("enter peek = %+v, want loading peek for 01-a", got.detail.peek)
	}
	if cmd == nil {
		t.Fatalf("enter in detail did not return task-text loading command")
	}
}

func TestDashboardTaskTextPeekScrolls(t *testing.T) {
	m := newQueueDashboard(&Deps{}, &config.Config{}, DashboardSnapshot{Rows: []DashboardRow{
		{Project: "pop", Status: "READY", cursorKey: "pop\x00set-scroll", SetRef: SetRef{SetID: "set-scroll"}},
	}})
	m.height = 8
	m.width = 80
	m.detail = &detailView{
		row: m.snap.Rows[0],
		peek: &taskTextPeek{
			taskID: "01-a",
			path:   filepath.Join("/tasks", "set-scroll", "01-a.md"),
			text:   "line 1\nline 2\nline 3\nline 4\nline 5\nline 6\n",
		},
	}

	view := m.View().Content
	for _, want := range []string{"line 1", "line 2", "line 3"} {
		if !strings.Contains(view, want) {
			t.Fatalf("initial peek missing %q:\n%s", want, view)
		}
	}
	if strings.Contains(view, "line 4") {
		t.Fatalf("initial peek should clip line 4:\n%s", view)
	}

	updated, _ := m.Update(tea.KeyPressMsg{Code: 'j', Text: "j"})
	got := updated.(QueueDashboard)
	if got.detail.peek.scroll != 1 {
		t.Fatalf("after j scroll = %d, want 1", got.detail.peek.scroll)
	}
	view = got.View().Content
	if strings.Contains(view, "line 1") || !strings.Contains(view, "line 4") {
		t.Fatalf("after j view should show lines 2-4:\n%s", view)
	}

	updated, _ = got.Update(tea.KeyPressMsg{Code: 'G', Text: "G"})
	got = updated.(QueueDashboard)
	if got.detail.peek.scroll != 3 {
		t.Fatalf("after G scroll = %d, want 3", got.detail.peek.scroll)
	}
	view = got.View().Content
	if !strings.Contains(view, "line 6") || strings.Contains(view, "line 1") {
		t.Fatalf("after G view should show bottom lines:\n%s", view)
	}

	updated, _ = got.Update(tea.KeyPressMsg{Code: 'g', Text: "g"})
	got = updated.(QueueDashboard)
	if got.detail.peek.scroll != 3 {
		t.Fatalf("first g scroll = %d, want 3", got.detail.peek.scroll)
	}
	updated, _ = got.Update(tea.KeyPressMsg{Code: 'g', Text: "g"})
	got = updated.(QueueDashboard)
	if got.detail.peek.scroll != 0 {
		t.Fatalf("after gg scroll = %d, want 0", got.detail.peek.scroll)
	}
}

func TestDashboardTopLevelVimNavigation(t *testing.T) {
	m := newQueueDashboard(&Deps{}, &config.Config{}, DashboardSnapshot{Rows: []DashboardRow{
		{Project: "pop", Status: "READY", cursorKey: "pop\x00first", SetRef: SetRef{SetID: "first"}},
		{Project: "pop", Status: "READY", cursorKey: "pop\x00second", SetRef: SetRef{SetID: "second"}},
		{Project: "pop", Status: "READY", cursorKey: "pop\x00third", SetRef: SetRef{SetID: "third"}},
	}})

	updated, _ := m.Update(tea.KeyPressMsg{Code: 'G', Text: "G"})
	got := updated.(QueueDashboard)
	if got.list.Cursor() != 2 {
		t.Fatalf("G cursor = %d, want 2", got.list.Cursor())
	}

	updated, _ = got.Update(tea.KeyPressMsg{Code: 'g', Text: "g"})
	got = updated.(QueueDashboard)
	if got.list.Cursor() != 2 {
		t.Fatalf("first g cursor = %d, want 2", got.list.Cursor())
	}
	updated, _ = got.Update(tea.KeyPressMsg{Code: 'g', Text: "g"})
	got = updated.(QueueDashboard)
	if got.list.Cursor() != 0 {
		t.Fatalf("gg cursor = %d, want 0", got.list.Cursor())
	}

	_, cmd := got.Update(tea.KeyPressMsg{Code: 'h', Text: "h"})
	if cmd == nil {
		t.Fatalf("h should quit from top level")
	}
}

func TestDashboardReloadPreservesCursorByKey(t *testing.T) {
	m := newQueueDashboard(&Deps{}, &config.Config{}, DashboardSnapshot{Rows: []DashboardRow{
		{Project: "pop", Status: "READY", cursorKey: "pop\x00a", SetRef: SetRef{SetID: "a"}},
		{Project: "pop", Status: "READY", cursorKey: "pop\x00b", SetRef: SetRef{SetID: "b"}},
		{Project: "pop", Status: "READY", cursorKey: "pop\x00c", SetRef: SetRef{SetID: "c"}},
	}})
	m.list.SetCursor(2) // on "c"

	// A tick reload delivers the same sets reordered; the cursor must follow "c".
	reordered := []DashboardRow{
		{Project: "pop", Status: "READY", cursorKey: "pop\x00c", SetRef: SetRef{SetID: "c"}},
		{Project: "pop", Status: "READY", cursorKey: "pop\x00a", SetRef: SetRef{SetID: "a"}},
		{Project: "pop", Status: "READY", cursorKey: "pop\x00b", SetRef: SetRef{SetID: "b"}},
	}
	updated, _ := m.Update(dashboardRowsMsg{snap: DashboardSnapshot{Rows: reordered}})
	got := updated.(QueueDashboard)
	if sel, ok := got.list.Selected(); !ok || sel.SetID != "c" {
		t.Fatalf("cursor after reload = %+v (ok=%v), want set c", sel, ok)
	}
}

func TestDashboardDetailViewRendersTaskList(t *testing.T) {
	manifest := &tasks.Manifest{
		Valid: true,
		Tasks: []tasks.Task{
			{ID: "01-a", File: "01-a.md", Title: "First", Type: "AFK", Status: "done"},
			{ID: "02-b", File: "02-b.md", Title: "Second", Type: "AFK", Status: "open", BlockedBy: []string{"01-a"}},
		},
	}
	taskRow := &tasks.Row{ID: "set-normal", Status: tasks.StatusReady, Progress: "1/2 done, 1 open"}
	m := newQueueDashboard(&Deps{}, &config.Config{}, DashboardSnapshot{Rows: []DashboardRow{
		{Project: "pop", Status: "READY", cursorKey: "pop\x00set-normal", SetRef: SetRef{SetID: "set-normal"}},
	}})
	m.width = 120
	m.height = 20
	d := newDetailView(m.snap.Rows[0])
	d.syncManifest(manifest, taskRow)
	m.detail = d
	out := m.viewDetail()

	for _, want := range []string{"set-normal  [READY]  1/2 done, 1 open", "STATUS", "01-a", "02-b"} {
		if !strings.Contains(out, want) {
			t.Fatalf("rendered detail missing %q:\n%s", want, out)
		}
	}
	if strings.Index(out, "01-a") > strings.Index(out, "02-b") {
		t.Fatalf("tasks out of manifest order:\n%s", out)
	}
	// Cursor indicator on first task.
	if !strings.Contains(out, "█") {
		t.Fatalf("expected cursor indicator:\n%s", out)
	}
}

func TestDashboardDetailViewCursorByIDPinsAcrossRefresh(t *testing.T) {
	manifest1 := &tasks.Manifest{
		Valid: true,
		Tasks: []tasks.Task{
			{ID: "01-a", Type: "AFK", Status: "done"},
			{ID: "02-b", Type: "AFK", Status: "open"},
		},
	}
	// Same tasks, different order (simulates a refresh that reorders).
	manifest2 := &tasks.Manifest{
		Valid: true,
		Tasks: []tasks.Task{
			{ID: "02-b", Type: "AFK", Status: "done"}, // promoted
			{ID: "01-a", Type: "AFK", Status: "done"},
		},
	}

	d := newDetailView(DashboardRow{SetRef: SetRef{SetID: "set-x"}})
	d.syncManifest(manifest1, nil)
	d.list.SetCursorToKey("02-b")

	// Cursor is on 02-b at index 1 before refresh.
	if sel, ok := d.list.Selected(); !ok || sel.ID != "02-b" || d.list.Cursor() != 1 {
		t.Fatalf("before refresh selected = %+v (ok=%v) cursor=%d, want 02-b at index 1", sel, ok, d.list.Cursor())
	}

	// After a refresh that reorders, the cursor follows 02-b to its new index.
	d.syncManifest(manifest2, nil)
	if sel, ok := d.list.Selected(); !ok || sel.ID != "02-b" || d.list.Cursor() != 0 {
		t.Fatalf("after refresh selected = %+v (ok=%v) cursor=%d, want 02-b at index 0", sel, ok, d.list.Cursor())
	}
}

func TestDashboardDetailViewVimNavigation(t *testing.T) {
	manifest := &tasks.Manifest{
		Valid: true,
		Tasks: []tasks.Task{
			{ID: "01-a", Type: "AFK", Status: "done"},
			{ID: "02-b", Type: "AFK", Status: "open"},
			{ID: "03-c", Type: "AFK", Status: "open"},
		},
	}
	m := newQueueDashboard(&Deps{}, &config.Config{}, DashboardSnapshot{Rows: []DashboardRow{
		{Project: "pop", Status: "READY", cursorKey: "pop\x00set-nav", SetRef: SetRef{SetID: "set-nav"}},
	}})
	d := newDetailView(m.snap.Rows[0])
	d.syncManifest(manifest, nil)
	m.detail = d

	selID := func(m QueueDashboard) string {
		task, _ := m.detail.list.Selected()
		return task.ID
	}

	// j moves cursor down.
	updated, _ := m.Update(tea.KeyPressMsg{Code: 'j', Text: "j"})
	got := updated.(QueueDashboard)
	if id := selID(got); id != "02-b" {
		t.Fatalf("after j: selected = %q, want 02-b", id)
	}

	// k moves cursor up.
	updated, _ = got.Update(tea.KeyPressMsg{Code: 'k', Text: "k"})
	got = updated.(QueueDashboard)
	if id := selID(got); id != "01-a" {
		t.Fatalf("after k: selected = %q, want 01-a", id)
	}

	// j/k clamp at boundaries.
	updated, _ = got.Update(tea.KeyPressMsg{Code: 'k', Text: "k"})
	got = updated.(QueueDashboard)
	if id := selID(got); id != "01-a" {
		t.Fatalf("k at top should clamp: selected = %q, want 01-a", id)
	}

	updated, _ = got.Update(tea.KeyPressMsg{Code: 'G', Text: "G"})
	got = updated.(QueueDashboard)
	if id := selID(got); id != "03-c" {
		t.Fatalf("G should move to bottom: selected = %q, want 03-c", id)
	}

	updated, _ = got.Update(tea.KeyPressMsg{Code: 'g', Text: "g"})
	got = updated.(QueueDashboard)
	if id := selID(got); id != "03-c" {
		t.Fatalf("first g should not move cursor: selected = %q, want 03-c", id)
	}
	updated, _ = got.Update(tea.KeyPressMsg{Code: 'g', Text: "g"})
	got = updated.(QueueDashboard)
	if id := selID(got); id != "01-a" {
		t.Fatalf("gg should move to top: selected = %q, want 01-a", id)
	}
}

// menuHasKey reports whether the action menu offers a verb bound to key.
func menuHasKey(menu *dashboardMenu, key string) bool {
	if menu == nil {
		return false
	}
	for _, item := range menu.list.Items() {
		if item.key == key {
			return true
		}
	}
	return false
}

func TestDashboardBaseRefsMainMasterFirst(t *testing.T) {
	got := parseDashboardBaseRefs("feature\norigin/master\norigin/HEAD\nmaster\norigin/main\nmain\n")
	want := []string{"main", "master", "origin/main", "origin/master", "feature"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("refs = %v, want %v", got, want)
	}
}

func TestDashboardLaunchDrainRoutesPlainTrunkWorktreeAndRecordsPane(t *testing.T) {
	repo, setID, _ := setupSupervisorSpawnRepo(t, "plain-drain", []spawnTestTask{
		{ID: "01-a", File: "01-a.md", Title: "A", Type: "AFK", Status: "open"},
	})
	d, cfg, row, rt := dashboardLaunchFixture(t, repo, setID)

	result, err := LaunchDrain(d, cfg, row.SetRef)
	if err != nil {
		t.Fatalf("LaunchDrain: %v", err)
	}
	if canon(t, d, result.RuntimePath) != canon(t, d, repo) {
		t.Fatalf("runtime = %q, want execution base %q", result.RuntimePath, repo)
	}
	cmd, ok := extractSpawnCommand(rt)
	if !ok {
		t.Fatal("expected drain spawn command")
	}
	if !strings.Contains(cmd, "pop tasks implement "+setID) || strings.Contains(cmd, "--task-runtime-path") {
		t.Fatalf("spawn command = %q, want in-place queue-base drain", cmd)
	}
	assertDashboardPaneMapping(t, d, repo, setID, "%3", "dashboard")
}

func TestDashboardLaunchDrainRoutesBoundCheckout(t *testing.T) {
	repo, setID, _ := setupSupervisorSpawnRepo(t, "bound-drain", []spawnTestTask{
		{ID: "01-a", File: "01-a.md", Title: "A", Type: "AFK", Status: "open"},
	})
	bound := filepath.Join(t.TempDir(), "bound")
	runGit(t, repo, "worktree", "add", "--detach", bound, "HEAD")
	d, cfg, row, rt := dashboardLaunchFixture(t, repo, setID)
	repoKey, err := resolveRepoKey(d, repo)
	if err != nil {
		t.Fatal(err)
	}
	if err := WriteDaemonState(d.Tasks, &DaemonState{Version: 1}); err != nil {
		t.Fatal(err)
	}
	seedBindingStore(t, d.Tasks, map[string]WorktreeBinding{
		setScopedKey(repoKey, setID): {RuntimePath: bound, Branch: "bound", Project: "pop", Provisioned: false},
	})

	result, err := LaunchDrain(d, cfg, row.SetRef)
	if err != nil {
		t.Fatalf("LaunchDrain: %v", err)
	}
	if result.RuntimePath != bound {
		t.Fatalf("runtime = %q, want bound checkout %q", result.RuntimePath, bound)
	}
	newWindow, ok := rt.findCommand("new-window")
	if !ok || !argsContain(newWindow, "-c", bound) {
		t.Fatalf("new-window = %v, want cwd %q", newWindow, bound)
	}
	assertDashboardPaneMapping(t, d, repo, setID, "%3", "dashboard")
}

// TestDashboardLaunchDrainUnboundUsesRepresentativeCheckout asserts the
// dashboard launch-drain action no longer auto-provisions a managed worktree for
// an unbound worktree-ready set: routing collapsed, so the drain lands on the
// representative checkout (the repo) and records no binding (ADR-0052). Explicit
// provisioning returns later via the Drain target picker.
func TestDashboardLaunchDrainUnboundUsesRepresentativeCheckout(t *testing.T) {
	repo, setID, _ := setupSupervisorSpawnRepo(t, "managed-drain", []spawnTestTask{
		{ID: "01-a", File: "01-a.md", Title: "A", Type: "AFK", Status: "open"},
	})
	d, cfg, row, rt := dashboardLaunchFixture(t, repo, setID)

	result, err := LaunchDrain(d, cfg, row.SetRef)
	if err != nil {
		t.Fatalf("LaunchDrain: %v", err)
	}
	wantRepo, _ := filepath.EvalSymlinks(repo)
	gotRuntime, _ := filepath.EvalSymlinks(result.RuntimePath)
	if gotRuntime != wantRepo || strings.Contains(result.RuntimePath, filepath.Join("pop", "queue", "worktrees")) {
		t.Fatalf("runtime = %q, want representative checkout %q with no provisioned worktree", result.RuntimePath, repo)
	}
	cmd, ok := extractSpawnCommand(rt)
	if !ok || !strings.Contains(cmd, "pop tasks implement "+setID) {
		t.Fatalf("spawn command = %q, want implement command for set %q", cmd, setID)
	}
	repoKey, err := resolveRepoKey(d, repo)
	if err != nil {
		t.Fatal(err)
	}
	bindings := loadBindingStore(t, d.Tasks)
	if b, ok := bindings[setScopedKey(repoKey, setID)]; ok {
		t.Fatalf("unexpected binding for unbound dashboard drain: %+v", b)
	}
	assertDashboardPaneMapping(t, d, repo, setID, "%3", "dashboard")
}

// TestDashboardShowsUnsatisfiableWorktreeDirective asserts the queue dashboard
// shows a set whose `name` worktree directive names a worktree absent on this
// machine as a config error on the set's row (ADR-0059), read-only — no drain, no
// provisioning.
func TestDashboardShowsUnsatisfiableWorktreeDirective(t *testing.T) {
	repo, setID, _ := setupSupervisorSpawnRepo(t, "named-directive", []spawnTestTask{
		{ID: "01-a", File: "01-a.md", Title: "A", Type: "AFK", Status: "open"},
	})
	d, cfg, _, _ := dashboardLaunchFixture(t, repo, setID)

	id, err := tasks.ResolveRepositoryIdentity(d.Tasks, repo)
	if err != nil {
		t.Fatal(err)
	}
	canonDef, err := tasks.CanonicalDefinitionPathWith(d.Tasks, id.TasksDir)
	if err != nil {
		t.Fatal(err)
	}
	if err := tasks.UpdateGlobalStateWith(d.Tasks, tasks.StatePathFor(canonDef), func(s *tasks.GlobalState) error {
		entry := s.Tasks[canonDef]
		for i := range entry.TaskSets {
			if entry.TaskSets[i].ID == setID {
				entry.TaskSets[i].WorktreeIntent = &tasks.WorktreeDirective{Name: "absent"}
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}

	snap, err := BuildDashboard(d, cfg)
	if err != nil {
		t.Fatalf("BuildDashboard: %v", err)
	}
	var row *DashboardRow
	for i := range snap.Rows {
		if snap.Rows[i].SetID == setID {
			row = &snap.Rows[i]
		}
	}
	if row == nil {
		t.Fatalf("set %s not in dashboard rows: %+v", setID, snap.Rows)
	}
	if !strings.Contains(row.Drain, "config error") || !strings.Contains(row.Drain, "no worktree of that name") {
		t.Fatalf("Drain = %q, want a config error for the unsatisfiable named directive", row.Drain)
	}
}

func TestDashboardBindPickerListsAndAdoptsExistingWorktree(t *testing.T) {
	repo, setID, _ := setupSupervisorSpawnRepo(t, "bind-existing", []spawnTestTask{
		{ID: "01-a", File: "01-a.md", Title: "A", Type: "AFK", Status: "open"},
	})
	wt1 := filepath.Join(t.TempDir(), "existing-one")
	wt2 := filepath.Join(t.TempDir(), "existing-two")
	runGit(t, repo, "worktree", "add", "-b", "existing-one", wt1, "HEAD")
	runGit(t, repo, "worktree", "add", "-b", "existing-two", wt2, "HEAD")
	d, cfg, row, _ := dashboardLaunchFixture(t, repo, setID)

	entries, err := BindWorktreeEntries(d, cfg, row.SetRef)
	if err != nil {
		t.Fatalf("BindWorktreeEntries: %v", err)
	}
	if len(entries) < 3 || !entries[len(entries)-1].Create {
		t.Fatalf("entries = %+v, want existing worktrees plus create entry", entries)
	}
	var sawWT1 bool
	for _, entry := range entries {
		if entry.Path != "" && canon(t, d, entry.Path) == canon(t, d, wt1) && entry.Branch == "existing-one" {
			sawWT1 = true
		}
	}
	if !sawWT1 {
		t.Fatalf("entries = %+v, want %s on branch existing-one", entries, wt1)
	}

	got, err := AdoptWorktree(d, cfg, row.SetRef, wt1)
	if err != nil {
		t.Fatalf("AdoptWorktree: %v", err)
	}
	if got.RuntimePath != wt1 || got.Branch != "existing-one" {
		t.Fatalf("adopt result = %+v, want %s existing-one", got, wt1)
	}

	repointed, err := AdoptWorktree(d, cfg, row.SetRef, wt2)
	if err != nil {
		t.Fatalf("idle re-point should not require force prompt: %v", err)
	}
	if !repointed.Replaced || repointed.RuntimePath != wt2 {
		t.Fatalf("repoint result = %+v, want replaced binding to %s", repointed, wt2)
	}
	repoKey, err := resolveRepoKey(d, repo)
	if err != nil {
		t.Fatal(err)
	}
	bindings := loadBindingStore(t, d.Tasks)
	binding := bindings[setScopedKey(repoKey, setID)]
	if binding.RuntimePath != wt2 || binding.Provisioned {
		t.Fatalf("binding = %+v, want adopted %s", binding, wt2)
	}
	snap, err := BuildDashboard(d, cfg)
	if err != nil {
		t.Fatalf("BuildDashboard: %v", err)
	}
	if len(snap.Rows) == 0 || snap.Rows[0].Worktree != "existing-two" {
		t.Fatalf("dashboard rows = %+v, want worktree column updated", snap.Rows)
	}
}

// staticGitGuard wraps a real git and fails the test if the dashboard build ever
// forks one of the static-path commands — identity (`rev-parse`), integration
// target (`worktree list`), or branch (`branch --show-current`). Those facts are
// derived fork-free from the repo.json marker and config (ADR-0060); any such
// fork is a regression. Other commands (e.g. mergeability in reconcile) pass
// through to the real git so the build still completes.
type staticGitGuard struct {
	t     *testing.T
	inner deps.Git
}

func (g *staticGitGuard) check(dir string, args []string) {
	if len(args) == 0 {
		return
	}
	static := args[0] == "rev-parse" ||
		args[0] == "branch" ||
		(args[0] == "worktree" && len(args) > 1 && args[1] == "list")
	if static {
		g.t.Errorf("dashboard build forked git on the static path: git %s (dir %q)", strings.Join(args, " "), dir)
	}
}

func (g *staticGitGuard) Command(args ...string) (string, error) {
	g.check("", args)
	return g.inner.Command(args...)
}

func (g *staticGitGuard) CommandInDir(dir string, args ...string) (string, error) {
	g.check(dir, args)
	return g.inner.CommandInDir(dir, args...)
}

// TestDashboardBuildForksNoStaticGit is the ADR-0060 guard: a dashboard build
// resolves identity, integration target, and branch from the marker + config
// with zero git. The guard git fails the test if any static-path command is
// forked, yet the build still yields rows whose branch column is populated —
// proving the branch came from the integration target's HEAD file, not a fork.
func TestDashboardBuildForksNoStaticGit(t *testing.T) {
	repo, setID, _ := setupSupervisorSpawnRepo(t, "fork-free", []spawnTestTask{
		{ID: "01-a", File: "01-a.md", Title: "A", Type: "AFK", Status: "open"},
	})
	d, cfg, _, _ := dashboardLaunchFixture(t, repo, setID)
	guard := &staticGitGuard{t: t, inner: d.Tasks.Git}
	d.Tasks.Git = guard
	d.Project.Git = guard

	snap, err := BuildDashboard(d, cfg)
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	if len(snap.Rows) == 0 {
		t.Fatalf("build produced no rows; fixture did not exercise resolution")
	}
	if strings.TrimSpace(snap.Rows[0].Worktree) == "" {
		t.Fatalf("branch/worktree column empty; HEAD-file branch resolution failed: %+v", snap.Rows[0])
	}
}

func dashboardBoolPtr(b bool) *bool { return &b }

// TestDashboardIntegrationTargetDerivedForkFree covers ADR-0060's integration
// target rules without forking git (a guard git fails the test on any static
// command): a non-bare repo's target is the main worktree (parent of the common
// dir) and needs no config; a bare repo's target is its config trunk; and a bare
// repo without a declared trunk surfaces a config-class error instead of forking
// or crashing.
func TestDashboardIntegrationTargetDerivedForkFree(t *testing.T) {
	dataHome := t.TempDir()
	mkDeps := func() *Deps {
		fs := &deps.MockFileSystem{
			GetenvFunc: func(k string) string {
				if k == "XDG_DATA_HOME" {
					return dataHome
				}
				return ""
			},
			EvalSymlinksFunc: func(p string) (string, error) { return p, nil },
			StatFunc:         func(string) (os.FileInfo, error) { return nil, os.ErrNotExist },
			ReadFileFunc:     func(string) ([]byte, error) { return nil, os.ErrNotExist },
			UserHomeDirFunc:  func() (string, error) { return dataHome, nil },
		}
		guard := &staticGitGuard{t: t, inner: &deps.MockGit{
			CommandInDirFunc: func(_ string, args ...string) (string, error) {
				return "", errors.New("unexpected git: " + strings.Join(args, " "))
			},
		}}
		return &Deps{Tasks: &tasks.Deps{FS: fs, Git: guard}, Project: &project.Deps{FS: fs, Git: guard}}
	}

	t.Run("non-bare resolves target with no config", func(t *testing.T) {
		d := mkDeps()
		scans := []projectScan{{Name: "repo", ProjectPath: "/repo", RuntimePath: "/repo"}}
		st, err := dashboardRepoStaticFromMarker(d, &config.Config{}, "/repo/.git", scans)
		if err != nil {
			t.Fatal(err)
		}
		if st.bare || st.configErr != "" || st.rep == nil || st.rep.ProjectPath != "/repo" {
			t.Fatalf("non-bare static = %+v rep = %+v", st, st.rep)
		}
	})

	t.Run("bare uses config trunk", func(t *testing.T) {
		d := mkDeps()
		cfg := &config.Config{Repo: map[string]config.RepoOverrideConfig{
			"/repo/main": {Trunk: dashboardBoolPtr(true)},
		}}
		scans := []projectScan{
			{Name: "repo/feat", ProjectPath: "/repo/feat", RuntimePath: "/repo/feat"},
			{Name: "repo/main", ProjectPath: "/repo/main", RuntimePath: "/repo/main"},
		}
		st, err := dashboardRepoStaticFromMarker(d, cfg, "/repo/.bare", scans)
		if err != nil {
			t.Fatal(err)
		}
		if st.configErr != "" || st.rep == nil || st.rep.ProjectPath != "/repo/main" {
			t.Fatalf("bare+trunk static = %+v rep = %+v", st, st.rep)
		}
	})

	t.Run("bare without trunk surfaces config error", func(t *testing.T) {
		d := mkDeps()
		scans := []projectScan{{Name: "repo/feat", ProjectPath: "/repo/feat", RuntimePath: "/repo/feat"}}
		st, err := dashboardRepoStaticFromMarker(d, &config.Config{}, "/repo/.bare", scans)
		if err != nil {
			t.Fatal(err)
		}
		if st.rep != nil || !st.bare || st.configErr == "" {
			t.Fatalf("bare-no-trunk static = %+v rep = %+v", st, st.rep)
		}
	})
}

// TestDashboardBareWithoutTrunkRendersConfigError covers the rendered half of
// ADR-0060's bare-without-trunk rule: an unbound set in such a repo shows a
// config-class error in the drain column and needs bind for its worktree,
// derived fork-free from the static (no git probe).
func TestDashboardBareWithoutTrunkRendersConfigError(t *testing.T) {
	d := dashboardTestDeps(t, []tasks.Row{{ID: "ready", Status: tasks.StatusReady, AutoDrain: true}}, nil)
	d.LiveDrains = func() ([]tasks.RunningDrain, error) { return nil, nil }
	st := dashboardRepoStatic{
		defPath:     "/def",
		statePath:   tasks.StatePathFor("/def"),
		repoKey:     "bare-key",
		projectName: "bare",
		rep:         nil,
		bare:        true,
		configErr:   repoScanReason,
	}
	got, err := dashboardRowsForStatic(d, &config.Config{}, &DaemonState{Version: 1}, st)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("rows = %+v, want one", got)
	}
	wantDrain := "config error: " + repoScanReason
	if got[0].Drain != wantDrain || got[0].Worktree != dashboardDestLabelNeedsBind {
		t.Fatalf("row = %+v, want drain %q worktree %q", got[0], wantDrain, dashboardDestLabelNeedsBind)
	}
}

// TestDashboardBranchColumnSources covers ADR-0070/0072 destination rules: a bound
// set shows its binding-row branch plainly; an unbound set with no directive shows
// needs bind.
func TestDashboardBranchColumnSources(t *testing.T) {
	d := dashboardTestDeps(t, []tasks.Row{
		{ID: "bound", Status: tasks.StatusBlocked},
		{ID: "unbound", Status: tasks.StatusReady, AutoDrain: true},
	}, nil)
	d.LiveDrains = func() ([]tasks.RunningDrain, error) { return nil, nil }
	dataHome := t.TempDir()
	real := deps.NewRealFileSystem()
	origFS := d.Tasks.FS.(*deps.MockFileSystem)
	d.Tasks.FS = &deps.MockFileSystem{
		GetenvFunc: func(key string) string {
			if key == "XDG_DATA_HOME" {
				return dataHome
			}
			return ""
		},
		EvalSymlinksFunc: origFS.EvalSymlinksFunc,
		ReadFileFunc:     real.ReadFile,
		WriteFileFunc:    real.WriteFile,
		MkdirAllFunc:     real.MkdirAll,
		RenameFunc:       real.Rename,
		StatFunc:         origFS.StatFunc,
	}
	seedBindingStore(t, d.Tasks, map[string]WorktreeBinding{
		setScopedKey("repo-key", "bound"): {RuntimePath: "/repo/bound", Branch: "bound-branch"},
	})

	// "unbound" reads the integration target's branch carried on the static.
	st := dashboardRepoStatic{
		defPath:     "/def",
		statePath:   tasks.StatePathFor("/def"),
		repoKey:     "repo-key",
		projectName: "pop",
		rep:         &projectScan{Name: "pop", ProjectPath: "/repo/main", RuntimePath: "/repo/main"},
		repBranch:   "trunk-branch",
		bare:        false,
	}
	got, err := dashboardRowsForStatic(d, &config.Config{}, &DaemonState{Version: 1}, st)
	if err != nil {
		t.Fatal(err)
	}
	byID := map[string]DashboardRow{}
	for _, row := range got {
		byID[row.SetID] = row
	}
	if byID["bound"].Worktree != "bound-branch" || byID["bound"].destKind != dashboardDestBound {
		t.Fatalf("bound worktree = %+v, want binding-row branch", byID["bound"])
	}
	if byID["unbound"].Worktree != dashboardDestLabelNeedsBind || byID["unbound"].destKind != dashboardDestNeedsBind {
		t.Fatalf("unbound worktree = %+v, want needs bind", byID["unbound"])
	}
}

func TestDashboardManagedDirectiveDestColumn(t *testing.T) {
	rows := []tasks.Row{
		{ID: "managed", Status: tasks.StatusReady, AutoDrain: true},
	}
	d := dashboardTestDeps(t, rows, nil)
	dataHome := t.TempDir()
	real := deps.NewRealFileSystem()
	origFS := d.Tasks.FS.(*deps.MockFileSystem)
	d.Tasks.FS = &deps.MockFileSystem{
		GetenvFunc: func(key string) string {
			if key == "XDG_DATA_HOME" {
				return dataHome
			}
			return ""
		},
		EvalSymlinksFunc: origFS.EvalSymlinksFunc,
		ReadFileFunc:     real.ReadFile,
		WriteFileFunc:    real.WriteFile,
		MkdirAllFunc:     real.MkdirAll,
		RenameFunc:       real.Rename,
		StatFunc:         origFS.StatFunc,
	}
	defPath := "/def"
	if err := tasks.UpdateGlobalStateWith(d.Tasks, tasks.StatePathFor(defPath), func(s *tasks.GlobalState) error {
		s.Tasks[defPath] = &tasks.TaskEntry{
			TaskSets: []tasks.RegisteredTaskSet{
				{ID: "managed", WorktreeIntent: &tasks.WorktreeDirective{Managed: true}},
			},
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	scan := projectScan{Name: "pop", ProjectPath: "/repo/main", RuntimePath: "/repo/main", DefinitionPath: defPath, RepoKey: "repo-key"}

	got, err := dashboardRowsForStatic(d, &config.Config{}, &DaemonState{Version: 1}, staticForScan(scan, "main", false))
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("rows = %+v, want one managed-directive row", got)
	}
	if got[0].Worktree != dashboardDestLabelManagedWt || got[0].destKind != dashboardDestManagedDirective {
		t.Fatalf("managed row = %+v, want [managed wt] badge", got[0])
	}
	var rendered strings.Builder
	renderDashboardTable(&rendered, got, 0, 0)
	out := rendered.String()
	if !strings.Contains(out, "[managed wt]") {
		t.Fatalf("render missing [managed wt] badge:\n%s", out)
	}
	if strings.Contains(out, "↳") {
		t.Fatalf("render must not contain worktree marker glyph:\n%s", out)
	}
}

func TestDashboardDoneAdoptedBindingExcluded(t *testing.T) {
	rows := []tasks.Row{
		{ID: "done-adopted", Status: tasks.StatusDone},
		{ID: "done-managed", Status: tasks.StatusDone},
	}
	d := dashboardTestDeps(t, rows, nil)
	dataHome := t.TempDir()
	real := deps.NewRealFileSystem()
	origFS := d.Tasks.FS.(*deps.MockFileSystem)
	d.Tasks.FS = &deps.MockFileSystem{
		GetenvFunc: func(key string) string {
			if key == "XDG_DATA_HOME" {
				return dataHome
			}
			return ""
		},
		EvalSymlinksFunc: origFS.EvalSymlinksFunc,
		ReadFileFunc:     real.ReadFile,
		WriteFileFunc:    real.WriteFile,
		MkdirAllFunc:     real.MkdirAll,
		RenameFunc:       real.Rename,
		StatFunc:         origFS.StatFunc,
	}
	seedBindingStore(t, d.Tasks, map[string]WorktreeBinding{
		setScopedKey("repo-key", "done-adopted"): {RuntimePath: "/repo/adopted", Branch: "adopted-branch"},
		setScopedKey("repo-key", "done-managed"): {RuntimePath: "/repo/managed", Branch: "managed-branch", Provisioned: true},
	})
	scan := projectScan{Name: "pop", ProjectPath: "/repo/main", RuntimePath: "/repo/main", DefinitionPath: "/def", RepoKey: "repo-key"}

	got, err := dashboardRowsForStatic(d, &config.Config{}, &DaemonState{Version: 1}, staticForScan(scan, "main", false))
	if err != nil {
		t.Fatal(err)
	}
	byID := map[string]DashboardRow{}
	for _, row := range got {
		byID[row.SetID] = row
	}
	if _, ok := byID["done-adopted"]; ok {
		t.Fatalf("adopted Done binding should be excluded, got %+v", byID["done-adopted"])
	}
	if row, ok := byID["done-managed"]; !ok {
		t.Fatal("managed Done binding should remain visible")
	} else if row.destKind != dashboardDestDoneManagedBound || row.Worktree != "managed-branch" {
		t.Fatalf("done-managed row = %+v", row)
	}
}

func TestDashboardNeedsBindRenderedDim(t *testing.T) {
	d := dashboardTestDeps(t, []tasks.Row{{ID: "plain", Status: tasks.StatusReady}}, nil)
	scan := projectScan{Name: "pop", ProjectPath: "/repo/main", RuntimePath: "/repo/main", DefinitionPath: "/def", RepoKey: "repo-key"}
	got, err := dashboardRowsForStatic(d, &config.Config{}, &DaemonState{Version: 1}, staticForScan(scan, "main", false))
	if err != nil {
		t.Fatal(err)
	}
	var rendered strings.Builder
	renderDashboardTable(&rendered, got, 0, 0)
	if !strings.Contains(rendered.String(), "needs bind") {
		t.Fatalf("render missing needs bind:\n%s", rendered.String())
	}
}

// TestHeadBranchFromCheckout covers ADR-0060's fork-free branch read: a main
// worktree's branch is parsed from <checkout>/.git/HEAD, a linked worktree's via
// its `.git` gitdir pointer, a detached HEAD yields "" (the branch is omitted),
// and the common-dir fallback applies when there is no `.git` entry.
func TestHeadBranchFromCheckout(t *testing.T) {
	files := map[string]string{
		"/main/.git/HEAD":              "ref: refs/heads/trunk\n",
		"/wt/.git":                     "gitdir: /repo/.git/worktrees/wt\n",
		"/repo/.git/worktrees/wt/HEAD": "ref: refs/heads/feature\n",
		"/detached/.git/HEAD":          "a1b2c3d4e5f6\n",
		"/common-only/.git/HEAD":       "ref: refs/heads/from-common\n",
	}
	dirs := map[string]bool{"/main/.git": true, "/detached/.git": true}
	fs := &deps.MockFileSystem{
		StatFunc: func(p string) (os.FileInfo, error) {
			if dirs[p] {
				return deps.MockFileInfo{NameVal: filepath.Base(p), IsDirVal: true}, nil
			}
			if _, ok := files[p]; ok {
				return deps.MockFileInfo{NameVal: filepath.Base(p)}, nil
			}
			return nil, os.ErrNotExist
		},
		ReadFileFunc: func(p string) ([]byte, error) {
			if data, ok := files[p]; ok {
				return []byte(data), nil
			}
			return nil, os.ErrNotExist
		},
	}
	td := &tasks.Deps{FS: fs}

	cases := []struct {
		name      string
		checkout  string
		commonDir string
		want      string
	}{
		{"main worktree", "/main", "/main/.git", "trunk"},
		{"linked worktree", "/wt", "/repo/.git", "feature"},
		{"detached", "/detached", "/detached/.git", ""},
		{"common-dir fallback", "/common-only", "/common-only/.git", "from-common"},
		{"missing", "/nope", "", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := headBranchFromCheckout(td, tc.checkout, tc.commonDir); got != tc.want {
				t.Fatalf("headBranchFromCheckout(%q, %q) = %q, want %q", tc.checkout, tc.commonDir, got, tc.want)
			}
		})
	}
}

func TestDashboardCreateWorktreeManagedFreshBranchNoSession(t *testing.T) {
	repo, setID, _ := setupSupervisorSpawnRepo(t, "bind-create", []spawnTestTask{
		{ID: "01-a", File: "01-a.md", Title: "A", Type: "AFK", Status: "open"},
	})
	d, cfg, row, rt := dashboardLaunchFixture(t, repo, setID)

	refs, err := BindBaseRefs(d, cfg, row.SetRef)
	if err != nil {
		t.Fatalf("BindBaseRefs: %v", err)
	}
	if len(refs) == 0 || refs[0] != "main" {
		t.Fatalf("refs = %v, want main first", refs)
	}

	got, err := CreateWorktree(d, cfg, row.SetRef, "main", "fresh-dashboard-branch")
	if err != nil {
		t.Fatalf("CreateWorktree: %v", err)
	}
	if got.Branch != "fresh-dashboard-branch" || got.BaseRef != "main" {
		t.Fatalf("create result = %+v", got)
	}
	if len(rt.commands) != 0 {
		t.Fatalf("create-new must not spawn or switch tmux sessions, got %v", rt.commands)
	}
	if branch := runGitOutput(t, repo, "branch", "--list", "fresh-dashboard-branch"); strings.TrimSpace(branch) == "" {
		t.Fatalf("fresh branch was not created")
	}
	repoKey, err := resolveRepoKey(d, repo)
	if err != nil {
		t.Fatal(err)
	}
	bindings := loadBindingStore(t, d.Tasks)
	binding := bindings[setScopedKey(repoKey, setID)]
	if binding.RuntimePath != got.RuntimePath || binding.Branch != "fresh-dashboard-branch" || !binding.Provisioned {
		t.Fatalf("binding = %+v, want managed fresh branch at %s", binding, got.RuntimePath)
	}
}

func TestDashboardBindRefusesLiveLock(t *testing.T) {
	repo, setID, _ := setupSupervisorSpawnRepo(t, "bind-locked", []spawnTestTask{
		{ID: "01-a", File: "01-a.md", Title: "A", Type: "AFK", Status: "open"},
	})
	locked := filepath.Join(t.TempDir(), "locked")
	target := filepath.Join(t.TempDir(), "target")
	runGit(t, repo, "worktree", "add", "-b", "locked-branch", locked, "HEAD")
	runGit(t, repo, "worktree", "add", "-b", "target-branch", target, "HEAD")
	d, cfg, row, _ := dashboardLaunchFixture(t, repo, setID)
	repoKey, err := resolveRepoKey(d, repo)
	if err != nil {
		t.Fatal(err)
	}
	row.RepoKey = repoKey
	row.RuntimePath = locked
	if err := WriteDaemonState(d.Tasks, &DaemonState{Version: 1}); err != nil {
		t.Fatal(err)
	}
	seedBindingStore(t, d.Tasks, map[string]WorktreeBinding{
		setScopedKey(repoKey, setID): {RuntimePath: locked, Branch: "locked-branch", Project: "pop", Provisioned: false},
	})
	d.ReadLock = func(runtimePath string) *tasks.RuntimeLockStatus {
		if runtimePath == locked {
			lock := liveLock(runtimePath)
			lock.Metadata.SetID = setID
			return lock
		}
		return idleLock(runtimePath)
	}

	_, err = AdoptWorktree(d, cfg, row.SetRef, target)
	if err == nil || !strings.Contains(err.Error(), "currently executing") {
		t.Fatalf("AdoptWorktree err = %v, want live-lock refusal", err)
	}
	afterBindings := loadBindingStore(t, d.Tasks)
	if got := afterBindings[setScopedKey(repoKey, setID)].RuntimePath; got != locked {
		t.Fatalf("binding runtime = %q, want unchanged %q", got, locked)
	}
}

func TestDashboardUKeyRequiresInlineConfirmBeforeUnbind(t *testing.T) {
	m := newQueueDashboard(&Deps{}, &config.Config{}, DashboardSnapshot{Rows: []DashboardRow{{Project: "pop", Status: "FAILED", Worktree: "/repo/bound (branch)", cursorKey: "pop\x00set-unbind", SetRef: SetRef{SetID: "set-unbind", DefPath: "/repo/tasks", StatePath: "/repo/state.json", Bound: true}}}})

	// Unbind now lives behind the action menu: open with `a`, then `U`.
	openMenu := func(model QueueDashboard) QueueDashboard {
		updated, _ := model.Update(tea.KeyPressMsg{Code: 'a', Text: "a"})
		got := updated.(QueueDashboard)
		if !menuHasKey(got.menu, "U") {
			t.Fatalf("unbind not offered on bound row: %+v", got.menu)
		}
		return got
	}

	got := openMenu(m)
	updated, cmd := got.Update(tea.KeyPressMsg{Code: 'U', Text: "U"})
	got = updated.(QueueDashboard)
	if cmd != nil {
		t.Fatalf("U key returned command before confirmation")
	}
	if got.menu != nil {
		t.Fatal("U did not close the action menu")
	}
	if got.abandon == nil || got.abandon.row.SetID != "set-unbind" {
		t.Fatalf("abandon modal = %+v, want set-unbind", got.abandon)
	}
	if !strings.Contains(got.View().Content, "Unbind worktree for set-unbind") {
		t.Fatalf("view missing unbind modal:\n%s", got.View().Content)
	}

	updated, cmd = got.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	got = updated.(QueueDashboard)
	if cmd == nil {
		t.Fatalf("confirm did not return unbind command")
	}
	if got.abandon == nil || !got.abandon.loading {
		t.Fatalf("abandon modal after confirm = %+v, want loading", got.abandon)
	}

	got = openMenu(m)
	updated, cmd = got.Update(tea.KeyPressMsg{Code: 'U', Text: "U"})
	got = updated.(QueueDashboard)
	updated, cmd = got.Update(tea.KeyPressMsg{Code: 'n', Text: "n"})
	got = updated.(QueueDashboard)
	if cmd != nil || got.abandon != nil {
		t.Fatalf("cancel should close modal without command: modal=%+v cmd=%v", got.abandon, cmd)
	}
}

func TestDashboardUnbindManagedOnlyForgetsBindingAndKeepsCheckout(t *testing.T) {
	repo, setID, _ := setupSupervisorSpawnRepo(t, "dashboard-unbind-managed", []spawnTestTask{
		{ID: "01-a", File: "01-a.md", Title: "A", Type: "AFK", Status: "failed"},
	})
	id, err := tasks.ResolveRepositoryIdentity(tasks.DefaultDeps(), repo)
	if err != nil {
		t.Fatal(err)
	}
	beforeManifest := mustReadFile(t, filepath.Join(id.TasksDir, setID, "index.json"))
	wt := filepath.Join(t.TempDir(), "managed")
	runGit(t, repo, "worktree", "add", "-b", "managed-unbind", wt, "HEAD")
	d, cfg, row, _ := dashboardLaunchFixture(t, repo, setID)
	repoKey, err := resolveRepoKey(d, repo)
	if err != nil {
		t.Fatal(err)
	}
	row.RepoKey = repoKey
	row.RuntimePath = wt
	if err := WriteDaemonState(d.Tasks, &DaemonState{Version: 1}); err != nil {
		t.Fatal(err)
	}
	seedBindingStore(t, d.Tasks, map[string]WorktreeBinding{
		setScopedKey(repoKey, setID): {RuntimePath: wt, Branch: "managed-unbind", Project: filepath.Base(repo), Provisioned: true},
	})

	got, err := UnbindWorktree(d, cfg, row.SetRef)
	if err != nil {
		t.Fatalf("UnbindWorktree: %v", err)
	}
	if got.Noop {
		t.Fatalf("unbind result = %+v, want success", got)
	}
	if _, err := os.Stat(wt); err != nil {
		t.Fatalf("managed checkout should remain: %v", err)
	}
	if branch := runGitOutput(t, repo, "branch", "--list", "managed-unbind"); strings.TrimSpace(branch) == "" {
		t.Fatalf("managed branch was removed")
	}
	if len(loadBindingStore(t, d.Tasks)) != 0 {
		t.Fatalf("bindings = %+v, want cleared", loadBindingStore(t, d.Tasks))
	}
	afterManifest := mustReadFile(t, filepath.Join(id.TasksDir, setID, "index.json"))
	if string(beforeManifest) != string(afterManifest) {
		t.Fatalf("manifest changed:\nbefore:%s\nafter:%s", beforeManifest, afterManifest)
	}
}

func TestDashboardUnbindAdoptedOnlyForgetsBindingAndKeepsStatus(t *testing.T) {
	repo, setID, _ := setupSupervisorSpawnRepo(t, "dashboard-unbind-adopted", []spawnTestTask{
		{ID: "01-a", File: "01-a.md", Title: "A", Type: "AFK", Status: "done"},
	})
	id, err := tasks.ResolveRepositoryIdentity(tasks.DefaultDeps(), repo)
	if err != nil {
		t.Fatal(err)
	}
	beforeManifest := mustReadFile(t, filepath.Join(id.TasksDir, setID, "index.json"))
	wt := filepath.Join(t.TempDir(), "adopted")
	runGit(t, repo, "worktree", "add", "-b", "adopted-unbind", wt, "HEAD")
	d, cfg, row, _ := dashboardLaunchFixture(t, repo, setID)
	repoKey, err := resolveRepoKey(d, repo)
	if err != nil {
		t.Fatal(err)
	}
	row.RepoKey = repoKey
	row.RuntimePath = wt
	if err := WriteDaemonState(d.Tasks, &DaemonState{Version: 1}); err != nil {
		t.Fatal(err)
	}
	seedBindingStore(t, d.Tasks, map[string]WorktreeBinding{
		setScopedKey(repoKey, setID): {RuntimePath: wt, Branch: "adopted-unbind", Project: filepath.Base(repo), Provisioned: false},
	})

	got, err := UnbindWorktree(d, cfg, row.SetRef)
	if err != nil {
		t.Fatalf("UnbindWorktree: %v", err)
	}
	if got.Noop {
		t.Fatalf("unbind result = %+v, want success", got)
	}
	if _, err := os.Stat(wt); err != nil {
		t.Fatalf("adopted checkout should remain: %v", err)
	}
	if branch := runGitOutput(t, repo, "branch", "--list", "adopted-unbind"); strings.TrimSpace(branch) == "" {
		t.Fatalf("adopted branch was removed")
	}
	if len(loadBindingStore(t, d.Tasks)) != 0 {
		t.Fatalf("bindings = %+v, want cleared", loadBindingStore(t, d.Tasks))
	}
	afterManifest := mustReadFile(t, filepath.Join(id.TasksDir, setID, "index.json"))
	if string(beforeManifest) != string(afterManifest) {
		t.Fatalf("manifest changed:\nbefore:%s\nafter:%s", beforeManifest, afterManifest)
	}
}

func TestDashboardUnbindRefusesLiveLockAndNoopsWithoutBinding(t *testing.T) {
	repo, setID, _ := setupSupervisorSpawnRepo(t, "dashboard-unbind-locked", []spawnTestTask{
		{ID: "01-a", File: "01-a.md", Title: "A", Type: "AFK", Status: "failed"},
	})
	wt := filepath.Join(t.TempDir(), "locked")
	runGit(t, repo, "worktree", "add", "-b", "locked-unbind", wt, "HEAD")
	d, cfg, row, _ := dashboardLaunchFixture(t, repo, setID)
	repoKey, err := resolveRepoKey(d, repo)
	if err != nil {
		t.Fatal(err)
	}
	row.RepoKey = repoKey
	row.RuntimePath = wt
	if err := WriteDaemonState(d.Tasks, &DaemonState{Version: 1}); err != nil {
		t.Fatal(err)
	}
	seedBindingStore(t, d.Tasks, map[string]WorktreeBinding{
		setScopedKey(repoKey, setID): {RuntimePath: wt, Branch: "locked-unbind", Project: filepath.Base(repo), Provisioned: false},
	})
	d.ReadLock = func(runtimePath string) *tasks.RuntimeLockStatus {
		if runtimePath == wt {
			lock := liveLock(runtimePath)
			lock.Metadata.SetID = setID
			return lock
		}
		return idleLock(runtimePath)
	}

	_, err = UnbindWorktree(d, cfg, row.SetRef)
	if err == nil || !strings.Contains(err.Error(), "refusing unbind") {
		t.Fatalf("UnbindWorktree err = %v, want live-lock refusal", err)
	}
	afterBindings := loadBindingStore(t, d.Tasks)
	if got := afterBindings[setScopedKey(repoKey, setID)].RuntimePath; got != wt {
		t.Fatalf("binding runtime = %q, want unchanged %q", got, wt)
	}

	d.ReadLock = func(runtimePath string) *tasks.RuntimeLockStatus {
		t.Fatalf("no-binding unbind must not read runtime lock")
		return nil
	}
	seedBindingStore(t, d.Tasks, map[string]WorktreeBinding{})
	got, err := UnbindWorktree(d, cfg, row.SetRef)
	if err != nil {
		t.Fatalf("no-binding UnbindWorktree: %v", err)
	}
	if !got.Noop {
		t.Fatalf("no-binding result = %+v, want noop", got)
	}
}

func TestDashboardLaunchDrainRefusesBareWithoutTrunk(t *testing.T) {
	_, wts := initBareRepoWithWorktrees(t, 1)
	checkout := wts[0]
	t.Setenv("XDG_DATA_HOME", filepath.Join(t.TempDir(), "xdg"))
	id, err := tasks.ResolveRepositoryIdentity(tasks.DefaultDeps(), checkout)
	if err != nil {
		t.Fatal(err)
	}
	setID := "bare-drain"
	setDir := filepath.Join(id.TasksDir, setID)
	writeSpawnTaskMD(t, setDir, "01-a.md")
	writeSpawnManifest(t, setDir, []spawnTestTask{{ID: "01-a", File: "01-a.md", Title: "A", Type: "AFK", Status: "open"}})
	if _, err := tasks.RegisterWith(tasks.DefaultDeps(), id.TasksDir, tasks.StatePathFor(id.TasksDir)); err != nil {
		t.Fatal(err)
	}
	d, cfg, row, rt := dashboardLaunchFixture(t, checkout, setID)

	_, err = LaunchDrain(d, cfg, row.SetRef)
	if err == nil || !strings.Contains(err.Error(), repoScanReason) {
		t.Fatalf("LaunchDrain err = %v, want %q", err, repoScanReason)
	}
	if len(rt.commands) != 0 {
		t.Fatalf("bare no-base refusal must not touch tmux, got %v", rt.commands)
	}
}

func TestDashboardPreviewDrainPaneAndNoOp(t *testing.T) {
	rt := newRecordingTmux(true, drainWindowName)
	d := &Deps{Tmux: rt}
	if err := PreviewDrain(d, SetRef{PaneID: "%9"}); err != nil {
		t.Fatalf("PreviewDrain: %v", err)
	}
	if _, ok := rt.findCommand("select-pane"); !ok {
		t.Fatal("expected select-pane")
	}
	switchClient, ok := rt.findCommand("switch-client")
	if !ok || !argsContain(switchClient, "-t", "%9") {
		t.Fatalf("switch-client = %v, want pane %%9", switchClient)
	}
	rt.commands = nil
	if err := PreviewDrain(d, SetRef{}); err != nil {
		t.Fatalf("PreviewDrain no-op: %v", err)
	}
	if len(rt.commands) != 0 {
		t.Fatalf("preview without pane must no-op, got %v", rt.commands)
	}
}

func dashboardLaunchFixture(t *testing.T, repo, setID string) (*Deps, *config.Config, DashboardRow, *recordingTmux) {
	t.Helper()
	id, err := tasks.ResolveRepositoryIdentity(tasks.DefaultDeps(), repo)
	if err != nil {
		t.Fatal(err)
	}
	// A real repo with task sets always carries a repo.json storage marker
	// (EnsureStorage writes it on first task touch); the spawn fixture writes
	// task files directly and skips it, so write it here so BuildDashboard's
	// storage-scoped discovery sees this repo.
	if err := tasks.EnsureStorage(tasks.DefaultDeps(), id); err != nil {
		t.Fatal(err)
	}
	cfg := &config.Config{Projects: []config.ProjectEntry{{Path: repo}}}
	rt := newRecordingTmux(false, "0")
	td := queueTestTasksDeps(t, true)
	d := &Deps{Tasks: td, Project: project.DefaultDeps(), Tmux: rt}
	projects, err := tasks.ListPickerProjectsWith(d.Project, cfg)
	if err != nil {
		t.Fatal(err)
	}
	if len(projects) == 0 {
		t.Fatalf("no picker projects for %s", repo)
	}
	scan, err := resolveScan(d, projects[0])
	if err != nil {
		t.Fatal(err)
	}
	row := DashboardRow{Project: "pop", SetRef: SetRef{SetID: setID, DefPath: scan.DefinitionPath, StatePath: tasks.StatePathFor(id.TasksDir)}}
	return d, cfg, row, rt
}

func assertDashboardPaneMapping(t *testing.T, d *Deps, repo, setID, paneID, source string) {
	t.Helper()
	state, err := ReadDaemonState(d.Tasks)
	if err != nil {
		t.Fatal(err)
	}
	repoKey, err := resolveRepoKey(d, repo)
	if err != nil {
		t.Fatal(err)
	}
	pane := state.DrainPanes[setScopedKey(repoKey, setID)]
	if pane.PaneID != paneID || pane.SetID != setID || pane.Source != source {
		t.Fatalf("pane mapping = %+v, want pane=%s set=%s source=%s", pane, paneID, setID, source)
	}
}

func filterTestModel() QueueDashboard {
	rows := []DashboardRow{
		{Project: "alpha", Status: "READY", cursorKey: "alpha\x00set-one", SetRef: SetRef{SetID: "set-one"}},
		{Project: "beta", Status: "READY", cursorKey: "beta\x00set-two", SetRef: SetRef{SetID: "set-two"}},
		{Project: "gamma", Status: "FAILED", cursorKey: "gamma\x00feature", SetRef: SetRef{SetID: "feature"}},
	}
	m := newQueueDashboard(&Deps{}, &config.Config{}, DashboardSnapshot{Rows: rows})
	m.list.SetCursor(2)
	return m
}

func TestDashboardFilterMode_SlashEntersFilterMode(t *testing.T) {
	m := filterTestModel()
	updated, _ := m.Update(tea.KeyPressMsg{Code: '/', Text: "/"})
	got := updated.(QueueDashboard)
	if !got.filterMode {
		t.Fatal("expected filterMode = true after /")
	}
	if len(got.snap.Rows) != 3 {
		t.Fatalf("rows = %d, want 3 (no filter applied yet)", len(got.snap.Rows))
	}
}

func TestDashboardFilterMode_EscExitsAndClearsFilter(t *testing.T) {
	m := filterTestModel()
	// Enter filter mode
	updated, _ := m.Update(tea.KeyPressMsg{Code: '/', Text: "/"})
	m = updated.(QueueDashboard)
	// Type a filter
	updated, _ = m.Update(tea.KeyPressMsg{Code: 'b', Text: "b"})
	m = updated.(QueueDashboard)
	if len(m.snap.Rows) != 1 {
		t.Fatalf("after 'b' filter: rows = %d, want 1", len(m.snap.Rows))
	}
	// Esc exits filter mode and restores all rows
	updated, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	got := updated.(QueueDashboard)
	if got.filterMode {
		t.Fatal("expected filterMode = false after esc")
	}
	if len(got.snap.Rows) != 3 {
		t.Fatalf("after esc: rows = %d, want 3 (filter cleared)", len(got.snap.Rows))
	}
	if got.filterInput.Value() != "" {
		t.Fatalf("filterInput value = %q, want empty after esc", got.filterInput.Value())
	}
}

func TestDashboardFilterMode_TypingNarrowsRows(t *testing.T) {
	m := filterTestModel()
	updated, _ := m.Update(tea.KeyPressMsg{Code: '/', Text: "/"})
	m = updated.(QueueDashboard)

	// Type "alpha" — matches Project "alpha"
	for _, ch := range "alpha" {
		updated, _ = m.Update(tea.KeyPressMsg{Code: ch, Text: string(ch)})
		m = updated.(QueueDashboard)
	}
	if len(m.snap.Rows) != 1 {
		t.Fatalf("after 'alpha': rows = %d, want 1", len(m.snap.Rows))
	}
	if m.snap.Rows[0].Project != "alpha" {
		t.Fatalf("filtered row project = %q, want alpha", m.snap.Rows[0].Project)
	}
}

func TestDashboardFilterMode_MatchesSetID(t *testing.T) {
	m := filterTestModel()
	updated, _ := m.Update(tea.KeyPressMsg{Code: '/', Text: "/"})
	m = updated.(QueueDashboard)

	// "feature" matches SetID "feature" in project "gamma"
	for _, ch := range "feature" {
		updated, _ = m.Update(tea.KeyPressMsg{Code: ch, Text: string(ch)})
		m = updated.(QueueDashboard)
	}
	if len(m.snap.Rows) != 1 {
		t.Fatalf("after 'feature': rows = %d, want 1", len(m.snap.Rows))
	}
	if m.snap.Rows[0].SetID != "feature" {
		t.Fatalf("filtered row setID = %q, want feature", m.snap.Rows[0].SetID)
	}
}

func TestDashboardFilterMode_CursorClampedToFilteredRows(t *testing.T) {
	m := filterTestModel()
	m.list.SetCursor(2) // on gamma/feature
	updated, _ := m.Update(tea.KeyPressMsg{Code: '/', Text: "/"})
	m = updated.(QueueDashboard)

	// Type "alpha" — only alpha/set-one matches; cursor must move within bounds
	for _, ch := range "alpha" {
		updated, _ = m.Update(tea.KeyPressMsg{Code: ch, Text: string(ch)})
		m = updated.(QueueDashboard)
	}
	if c := m.list.Cursor(); c < 0 || c >= len(m.snap.Rows) {
		t.Fatalf("cursor = %d, out of bounds for %d filtered rows", c, len(m.snap.Rows))
	}
}

func TestDashboardFilterMode_NavigationWorksInsideFilter(t *testing.T) {
	m := filterTestModel()
	updated, _ := m.Update(tea.KeyPressMsg{Code: '/', Text: "/"})
	m = updated.(QueueDashboard)
	// Type "set" to match two rows
	for _, ch := range "set" {
		updated, _ = m.Update(tea.KeyPressMsg{Code: ch, Text: string(ch)})
		m = updated.(QueueDashboard)
	}
	if len(m.snap.Rows) != 2 {
		t.Fatalf("after 'set': rows = %d, want 2", len(m.snap.Rows))
	}
	m.list.SetCursor(0)
	updated, _ = m.Update(tea.KeyPressMsg{Code: 'j', Text: "j"})
	got := updated.(QueueDashboard)
	if got.list.Cursor() != 1 {
		t.Fatalf("j in filter mode: cursor = %d, want 1", got.list.Cursor())
	}
	updated, _ = got.Update(tea.KeyPressMsg{Code: 'k', Text: "k"})
	got = updated.(QueueDashboard)
	if got.list.Cursor() != 0 {
		t.Fatalf("k in filter mode: cursor = %d, want 0", got.list.Cursor())
	}
}

func TestDashboardFilterMode_BareActionsInertInFilterMode(t *testing.T) {
	called := false
	d := &Deps{
		ToggleAutoDrain: func(defPath, statePath, setID string) (*tasks.AutoDrainResult, error) {
			called = true
			return &tasks.AutoDrainResult{}, nil
		},
	}
	rows := []DashboardRow{
		{Project: "alpha", Status: "READY", cursorKey: "alpha\x00set-one", SetRef: SetRef{SetID: "set-one", DefPath: "/def", StatePath: "/state"}},
	}
	m := newQueueDashboard(d, &config.Config{}, DashboardSnapshot{Rows: rows})
	m.list.SetCursor(0)
	// Enter filter mode
	updated, _ := m.Update(tea.KeyPressMsg{Code: '/', Text: "/"})
	m = updated.(QueueDashboard)
	// Action keys should NOT trigger actions — they go to the filter input
	for _, key := range []tea.KeyPressMsg{
		{Code: 'i', Text: "i"},
		{Code: 'I', Text: "I"},
		{Code: 'b', Text: "b"},
		{Code: 'U', Text: "U"},
		{Code: 'a', Text: "a"},
		{Code: 'p', Text: "p"},
		{Code: 's', Text: "s"},
		{Code: 'l', Text: "l"},
		{Code: tea.KeyEnter},
	} {
		updated, _ = m.Update(key)
		m = updated.(QueueDashboard)
	}
	if called {
		t.Fatal("bare-letter actions must be inert in filter mode")
	}
	if m.bind != nil || m.abandon != nil || m.detail != nil || m.menu != nil {
		t.Fatal("modals must not open while in filter mode")
	}
}

func TestDashboardFilterMode_QKeyGoesToInputNotQuit(t *testing.T) {
	m := filterTestModel()
	updated, _ := m.Update(tea.KeyPressMsg{Code: '/', Text: "/"})
	m = updated.(QueueDashboard)
	// 'q' in filter mode goes to the input box, not quit
	updated, _ = m.Update(tea.KeyPressMsg{Code: 'q', Text: "q"})
	got := updated.(QueueDashboard)
	if !got.filterMode {
		t.Fatal("q in filter mode must not exit filter mode")
	}
	if got.filterInput.Value() != "q" {
		t.Fatalf("filter input = %q after q, want 'q'", got.filterInput.Value())
	}
}

func TestDashboardFilterMode_ReloadPreservesFilter(t *testing.T) {
	m := filterTestModel()
	updated, _ := m.Update(tea.KeyPressMsg{Code: '/', Text: "/"})
	m = updated.(QueueDashboard)
	for _, ch := range "alpha" {
		updated, _ = m.Update(tea.KeyPressMsg{Code: ch, Text: string(ch)})
		m = updated.(QueueDashboard)
	}
	if len(m.snap.Rows) != 1 {
		t.Fatalf("before reload: rows = %d, want 1", len(m.snap.Rows))
	}

	// Simulate a reload with new rows that still include alpha
	newRows := []DashboardRow{
		{Project: "alpha", Status: "BLOCKED", cursorKey: "alpha\x00set-one", SetRef: SetRef{SetID: "set-one"}},
		{Project: "beta", Status: "READY", cursorKey: "beta\x00set-two", SetRef: SetRef{SetID: "set-two"}},
		{Project: "delta", Status: "READY", cursorKey: "delta\x00alpha-task", SetRef: SetRef{SetID: "alpha-task"}},
	}
	updated, _ = m.Update(dashboardRowsMsg{snap: DashboardSnapshot{Rows: newRows}})
	got := updated.(QueueDashboard)

	if !got.filterMode {
		t.Fatal("filter mode must persist across reload")
	}
	// Filter "alpha" should match "alpha" project and "alpha-task" set
	if len(got.snap.Rows) != 2 {
		t.Fatalf("after reload with filter 'alpha': rows = %d, want 2", len(got.snap.Rows))
	}
}

func TestFilterDashboardRows(t *testing.T) {
	rows := []DashboardRow{
		{Project: "alpha", SetRef: SetRef{SetID: "set-one"}},
		{Project: "beta", SetRef: SetRef{SetID: "set-two"}},
		{Project: "gamma", SetRef: SetRef{SetID: "feature"}},
	}

	t.Run("empty query returns all rows", func(t *testing.T) {
		got := filterDashboardRows(rows, "")
		if len(got) != 3 {
			t.Fatalf("empty filter: got %d rows, want 3", len(got))
		}
	})

	t.Run("matches project name", func(t *testing.T) {
		got := filterDashboardRows(rows, "beta")
		if len(got) != 1 || got[0].Project != "beta" {
			t.Fatalf("got %+v, want beta row", got)
		}
	})

	t.Run("matches set ID", func(t *testing.T) {
		got := filterDashboardRows(rows, "feature")
		if len(got) != 1 || got[0].SetID != "feature" {
			t.Fatalf("got %+v, want feature row", got)
		}
	})

	t.Run("case insensitive", func(t *testing.T) {
		got := filterDashboardRows(rows, "ALPHA")
		if len(got) != 1 || got[0].Project != "alpha" {
			t.Fatalf("got %+v, want alpha row", got)
		}
	})

	t.Run("partial match works", func(t *testing.T) {
		got := filterDashboardRows(rows, "set")
		if len(got) != 2 {
			t.Fatalf("got %d rows for 'set', want 2", len(got))
		}
	})

	t.Run("no match returns nil", func(t *testing.T) {
		got := filterDashboardRows(rows, "zzz")
		if len(got) != 0 {
			t.Fatalf("got %d rows for 'zzz', want 0", len(got))
		}
	})
}

// detailOverrideModel builds a QueueDashboard with a loaded detailView and
// injectable override seams. The seams record calls and return the provided error.
func detailOverrideModel(row DashboardRow, task tasks.Task, completeErr, resetErr, skipErr error) (QueueDashboard, *int, *int, *int) {
	completeCalls, resetCalls, skipCalls := 0, 0, 0
	d := &Deps{
		CompleteDetailTask: func(defPath, taskPath string) error {
			completeCalls++
			return completeErr
		},
		ResetDetailTask: func(defPath, taskPath string) error {
			resetCalls++
			return resetErr
		},
		SkipDetailTask: func(defPath, taskPath string) error {
			skipCalls++
			return skipErr
		},
	}
	manifest := &tasks.Manifest{
		Valid: true,
		Tasks: []tasks.Task{task},
	}
	m := newQueueDashboard(d, nil, DashboardSnapshot{Rows: []DashboardRow{row}})
	dv := newDetailView(row)
	dv.syncManifest(manifest, nil)
	m.detail = dv
	return m, &completeCalls, &resetCalls, &skipCalls
}

// taskMenuItemKeys returns the verb-letter keys offered by an open task menu.
func taskMenuItemKeys(menu *taskMenu) []string {
	if menu == nil {
		return nil
	}
	items := menu.list.Items()
	keys := make([]string, len(items))
	for i, item := range items {
		keys[i] = item.key
	}
	return keys
}

// openTaskMenu presses `a` in the detail view and returns the resulting model.
func openTaskMenu(t *testing.T, m QueueDashboard) QueueDashboard {
	t.Helper()
	updated, _ := m.Update(tea.KeyPressMsg{Code: 'a', Text: "a"})
	return updated.(QueueDashboard)
}

func TestDetailTaskMenuCompleteVerb(t *testing.T) {
	row := DashboardRow{SetRef: SetRef{SetID: "set-x", DefPath: "/def"}}

	// Complete on open task: menu offers C, dispatching it runs the command.
	openTask := tasks.Task{ID: "01-a", File: "01-a.md", Status: "open"}
	m, completeCalls, _, _ := detailOverrideModel(row, openTask, nil, nil, nil)
	m = openTaskMenu(t, m)
	if m.taskMenu == nil {
		t.Fatal("a on open task: expected task menu to open")
	}
	if got := taskMenuItemKeys(m.taskMenu); !slices.Contains(got, "C") {
		t.Fatalf("open task menu = %v, want to contain C", got)
	}
	updated, cmd := m.Update(tea.KeyPressMsg{Code: 'C', Text: "C"})
	got := updated.(QueueDashboard)
	if got.taskMenu != nil {
		t.Fatal("C should close the menu")
	}
	if cmd == nil {
		t.Fatal("C on open task: expected a command to be dispatched")
	}
	msg := cmd()
	if *completeCalls != 1 {
		t.Fatalf("completeCalls = %d, want 1", *completeCalls)
	}
	updated, _ = got.Update(msg)
	got = updated.(QueueDashboard)
	if !strings.Contains(got.detail.statusMsg, "complete") {
		t.Fatalf("C confirmation = %q, want 'complete'", got.detail.statusMsg)
	}

	// Done task: Complete does not apply, but Open (reopen) does — the menu
	// opens with O only, mirroring CanReopen.
	doneTask := tasks.Task{ID: "01-a", File: "01-a.md", Status: "done"}
	m2, completeCalls2, resetCalls2, _ := detailOverrideModel(row, doneTask, nil, nil, nil)
	m2 = openTaskMenu(t, m2)
	if m2.taskMenu == nil {
		t.Fatal("a on done task: expected task menu to open with Open verb")
	}
	keys := taskMenuItemKeys(m2.taskMenu)
	if slices.Contains(keys, "C") {
		t.Fatalf("done task menu = %v, want NOT to contain C", keys)
	}
	if !slices.Contains(keys, "O") {
		t.Fatalf("done task menu = %v, want to contain O", keys)
	}
	_, cmd2 := m2.Update(tea.KeyPressMsg{Code: 'O', Text: "O"})
	if cmd2 == nil {
		t.Fatal("O on done task: expected a command")
	}
	cmd2()
	if *resetCalls2 != 1 {
		t.Fatalf("O on done: resetCalls = %d, want 1", *resetCalls2)
	}
	if *completeCalls2 != 0 {
		t.Fatalf("done task: completeCalls = %d, want 0", *completeCalls2)
	}
}

func TestDetailTaskMenuOpenVerb(t *testing.T) {
	row := DashboardRow{SetRef: SetRef{SetID: "set-y", DefPath: "/def"}}

	// Open on failed task: menu offers O.
	failedTask := tasks.Task{ID: "02-b", File: "02-b.md", Status: "failed"}
	m, _, resetCalls, _ := detailOverrideModel(row, failedTask, nil, nil, nil)
	m = openTaskMenu(t, m)
	if got := taskMenuItemKeys(m.taskMenu); !slices.Contains(got, "O") {
		t.Fatalf("failed task menu = %v, want to contain O", got)
	}
	updated, cmd := m.Update(tea.KeyPressMsg{Code: 'O', Text: "O"})
	got := updated.(QueueDashboard)
	if cmd == nil {
		t.Fatal("O on failed: expected a command")
	}
	msg := cmd()
	if *resetCalls != 1 {
		t.Fatalf("resetCalls = %d, want 1", *resetCalls)
	}
	updated, _ = got.Update(msg)
	got = updated.(QueueDashboard)
	if !strings.Contains(got.detail.statusMsg, "open") {
		t.Fatalf("O confirmation = %q, want 'open'", got.detail.statusMsg)
	}

	// Open on skipped task: also offered.
	skippedTask := tasks.Task{ID: "03-c", File: "03-c.md", Status: "skipped"}
	m2, _, resetCalls2, _ := detailOverrideModel(row, skippedTask, nil, nil, nil)
	m2 = openTaskMenu(t, m2)
	if got := taskMenuItemKeys(m2.taskMenu); !slices.Contains(got, "O") {
		t.Fatalf("skipped task menu = %v, want to contain O", got)
	}
	_, cmd2 := m2.Update(tea.KeyPressMsg{Code: 'O', Text: "O"})
	if cmd2 == nil {
		t.Fatal("O on skipped: expected a command")
	}
	cmd2()
	if *resetCalls2 != 1 {
		t.Fatalf("O on skipped: resetCalls = %d, want 1", *resetCalls2)
	}

	// Open is NOT offered for an already-open task (CanReopen excludes open).
	openTask := tasks.Task{ID: "04-d", File: "04-d.md", Status: "open"}
	m3, _, resetCalls3, _ := detailOverrideModel(row, openTask, nil, nil, nil)
	m3 = openTaskMenu(t, m3)
	if got := taskMenuItemKeys(m3.taskMenu); slices.Contains(got, "O") {
		t.Fatalf("open task menu = %v, want NOT to contain O", got)
	}
	// Pressing O is inert while the menu is open and has no O item.
	_, cmd3 := m3.Update(tea.KeyPressMsg{Code: 'O', Text: "O"})
	if cmd3 != nil {
		t.Fatal("O on open task: expected no command")
	}
	if *resetCalls3 != 0 {
		t.Fatalf("O on open: resetCalls = %d, want 0", *resetCalls3)
	}
}

func TestDetailTaskMenuSkipVerb(t *testing.T) {
	row := DashboardRow{SetRef: SetRef{SetID: "set-z", DefPath: "/def"}}

	// Skip on open task: menu offers K.
	openTask := tasks.Task{ID: "04-d", File: "04-d.md", Status: "open"}
	m, _, _, skipCalls := detailOverrideModel(row, openTask, nil, nil, nil)
	m = openTaskMenu(t, m)
	if got := taskMenuItemKeys(m.taskMenu); !slices.Contains(got, "K") {
		t.Fatalf("open task menu = %v, want to contain K", got)
	}
	updated, cmd := m.Update(tea.KeyPressMsg{Code: 'K', Text: "K"})
	got := updated.(QueueDashboard)
	if cmd == nil {
		t.Fatal("K on open: expected a command")
	}
	msg := cmd()
	if *skipCalls != 1 {
		t.Fatalf("skipCalls = %d, want 1", *skipCalls)
	}
	updated, _ = got.Update(msg)
	got = updated.(QueueDashboard)
	if !strings.Contains(got.detail.statusMsg, "skip") {
		t.Fatalf("K confirmation = %q, want 'skip'", got.detail.statusMsg)
	}

	// Skip is NOT offered for a failed task (requires open).
	failedTask := tasks.Task{ID: "04-d", File: "04-d.md", Status: "failed"}
	m2, _, _, skipCalls2 := detailOverrideModel(row, failedTask, nil, nil, nil)
	m2 = openTaskMenu(t, m2)
	if got := taskMenuItemKeys(m2.taskMenu); slices.Contains(got, "K") {
		t.Fatalf("failed task menu = %v, want NOT to contain K", got)
	}
	_, cmd2 := m2.Update(tea.KeyPressMsg{Code: 'K', Text: "K"})
	if cmd2 != nil {
		t.Fatal("K on failed: expected no command")
	}
	if *skipCalls2 != 0 {
		t.Fatalf("K on failed: skipCalls = %d, want 0", *skipCalls2)
	}
}

// TestDetailTaskMenuDispatchViaEnter exercises j/k highlight + Enter dispatch.
func TestDetailTaskMenuDispatchViaEnter(t *testing.T) {
	row := DashboardRow{SetRef: SetRef{SetID: "set-enter", DefPath: "/def"}}
	failedTask := tasks.Task{ID: "02-b", File: "02-b.md", Status: "failed"}
	m, completeCalls, resetCalls, _ := detailOverrideModel(row, failedTask, nil, nil, nil)
	m = openTaskMenu(t, m)
	// Menu order for a failed task: complete (C), open (O). Highlight O via j.
	updated, _ := m.Update(tea.KeyPressMsg{Code: 'j', Text: "j"})
	got := updated.(QueueDashboard)
	if got.taskMenu.list.Cursor() != 1 {
		t.Fatalf("after j cursor = %d, want 1", got.taskMenu.list.Cursor())
	}
	updated, cmd := got.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	got = updated.(QueueDashboard)
	if cmd == nil {
		t.Fatal("Enter on highlighted O: expected a command")
	}
	cmd()
	if *resetCalls != 1 {
		t.Fatalf("resetCalls = %d, want 1", *resetCalls)
	}
	if *completeCalls != 0 {
		t.Fatalf("completeCalls = %d, want 0", *completeCalls)
	}
}

// TestDetailTaskMenuEscCloses verifies esc dismisses the menu without dispatch.
func TestDetailTaskMenuEscCloses(t *testing.T) {
	row := DashboardRow{SetRef: SetRef{SetID: "set-esc", DefPath: "/def"}}
	openTask := tasks.Task{ID: "01-a", File: "01-a.md", Status: "open"}
	m, completeCalls, _, skipCalls := detailOverrideModel(row, openTask, nil, nil, nil)
	m = openTaskMenu(t, m)
	updated, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	got := updated.(QueueDashboard)
	if got.taskMenu != nil {
		t.Fatal("esc should close the task menu")
	}
	if cmd != nil {
		t.Fatal("esc should not dispatch a command")
	}
	if got.detail == nil {
		t.Fatal("esc should keep the detail view open")
	}
	if *completeCalls != 0 || *skipCalls != 0 {
		t.Fatal("esc should not dispatch any verb")
	}
}

func TestDetailTaskMenuErrorSurfaced(t *testing.T) {
	row := DashboardRow{SetRef: SetRef{SetID: "set-err", DefPath: "/def"}}
	openTask := tasks.Task{ID: "01-a", File: "01-a.md", Status: "open"}
	someErr := errors.New("blocked by unsatisfied")
	m, _, _, _ := detailOverrideModel(row, openTask, someErr, nil, nil)

	m = openTaskMenu(t, m)
	updated, cmd := m.Update(tea.KeyPressMsg{Code: 'C', Text: "C"})
	got := updated.(QueueDashboard)
	msg := cmd()
	updated, _ = got.Update(msg)
	got = updated.(QueueDashboard)
	if !strings.Contains(got.detail.statusMsg, "error") {
		t.Fatalf("error not surfaced in statusMsg: %q", got.detail.statusMsg)
	}
}

func TestDetailViewActionsHintRendered(t *testing.T) {
	manifest := &tasks.Manifest{
		Valid: true,
		Tasks: []tasks.Task{{ID: "01-a", File: "01-a.md", Status: "open", Type: "AFK", Title: "A"}},
	}
	m := newQueueDashboard(&Deps{}, &config.Config{}, DashboardSnapshot{Rows: []DashboardRow{
		{Project: "pop", Status: "READY", cursorKey: "pop\x00set-render", SetRef: SetRef{SetID: "set-render"}},
	}})
	m.width = 80
	m.height = 12
	d := newDetailView(m.snap.Rows[0])
	d.syncManifest(manifest, nil)
	d.statusMsg = "completed 01-a"
	m.detail = d

	out := m.viewDetail()
	if !strings.Contains(out, "completed 01-a") {
		t.Fatalf("statusMsg not rendered:\n%s", out)
	}
	if !strings.Contains(out, "a actions") {
		t.Fatalf("hint line missing actions key:\n%s", out)
	}
}

// TestPeekTaskMenuOpensAndDispatches verifies `a` in the task text peek opens
// the task menu for the previewed task and dispatches a filtered verb.
func TestPeekTaskMenuOpensAndDispatches(t *testing.T) {
	row := DashboardRow{SetRef: SetRef{SetID: "set-peek", DefPath: "/def"}}
	failedTask := tasks.Task{ID: "02-b", File: "02-b.md", Status: "failed"}
	m, completeCalls, resetCalls, _ := detailOverrideModel(row, failedTask, nil, nil, nil)
	// Open a peek over the previewed task.
	m.detail.peek = &taskTextPeek{taskID: "02-b", text: "body\n"}

	updated, _ := m.Update(tea.KeyPressMsg{Code: 'a', Text: "a"})
	got := updated.(QueueDashboard)
	if got.taskMenu == nil {
		t.Fatal("a in peek: expected task menu to open")
	}
	if !got.taskMenu.inPeek {
		t.Fatal("peek-opened menu should be marked inPeek")
	}
	if keys := taskMenuItemKeys(got.taskMenu); !slices.Contains(keys, "O") || slices.Contains(keys, "K") {
		t.Fatalf("peek failed-task menu = %v, want O and not K", keys)
	}
	// The peek stays open beneath the menu.
	if got.detail.peek == nil {
		t.Fatal("peek should remain open while menu is up")
	}

	updated, cmd := got.Update(tea.KeyPressMsg{Code: 'O', Text: "O"})
	got = updated.(QueueDashboard)
	if got.taskMenu != nil {
		t.Fatal("O should close the menu")
	}
	if cmd == nil {
		t.Fatal("O in peek menu: expected a command")
	}
	cmd()
	if *resetCalls != 1 {
		t.Fatalf("resetCalls = %d, want 1", *resetCalls)
	}
	if *completeCalls != 0 {
		t.Fatalf("completeCalls = %d, want 0", *completeCalls)
	}
}

// TestPeekTaskMenuRendersOverlay verifies the peek view renders the menu verbs.
func TestPeekTaskMenuRendersOverlay(t *testing.T) {
	row := DashboardRow{SetRef: SetRef{SetID: "set-peek-render", DefPath: "/def"}}
	openTask := tasks.Task{ID: "01-a", File: "01-a.md", Status: "open"}
	m, _, _, _ := detailOverrideModel(row, openTask, nil, nil, nil)
	m.width = 120
	m.height = 14
	m.detail.peek = &taskTextPeek{taskID: "01-a", text: "body line\n"}

	updated, _ := m.Update(tea.KeyPressMsg{Code: 'a', Text: "a"})
	got := updated.(QueueDashboard)
	out := got.View().Content
	for _, want := range []string{"actions", "C  complete", "K  skip"} {
		if !strings.Contains(out, want) {
			t.Fatalf("rendered peek menu missing %q:\n%s", want, out)
		}
	}
}

// TestPeekFormerKeysInertWithoutMenu confirms C/O/K do nothing in the peek
// outside the menu (they act only through it).
func TestPeekFormerKeysInertWithoutMenu(t *testing.T) {
	row := DashboardRow{SetRef: SetRef{SetID: "set-peek-inert", DefPath: "/def"}}
	openTask := tasks.Task{ID: "01-a", File: "01-a.md", Status: "open"}
	m, completeCalls, _, skipCalls := detailOverrideModel(row, openTask, nil, nil, nil)
	m.detail.peek = &taskTextPeek{taskID: "01-a", text: "body\n"}

	for _, key := range []rune{'C', 'O', 'K'} {
		updated, cmd := m.Update(tea.KeyPressMsg{Code: key, Text: string(key)})
		got := updated.(QueueDashboard)
		if cmd != nil {
			t.Fatalf("%c in peek (no menu): expected no command", key)
		}
		if got.taskMenu != nil {
			t.Fatalf("%c in peek (no menu): should not open a menu", key)
		}
	}
	if *completeCalls != 0 || *skipCalls != 0 {
		t.Fatal("former direct keys should not dispatch in the peek")
	}
}

// TestDetailFormerKeysInertWithoutMenu confirms C/O/K do nothing in the detail
// list outside the menu.
func TestDetailFormerKeysInertWithoutMenu(t *testing.T) {
	row := DashboardRow{SetRef: SetRef{SetID: "set-inert", DefPath: "/def"}}
	openTask := tasks.Task{ID: "01-a", File: "01-a.md", Status: "open"}
	m, completeCalls, resetCalls, skipCalls := detailOverrideModel(row, openTask, nil, nil, nil)

	for _, key := range []rune{'C', 'O', 'K'} {
		updated, cmd := m.Update(tea.KeyPressMsg{Code: key, Text: string(key)})
		got := updated.(QueueDashboard)
		if cmd != nil {
			t.Fatalf("%c in detail (no menu): expected no command", key)
		}
		if got.taskMenu != nil {
			t.Fatalf("%c in detail (no menu): should not open a menu", key)
		}
	}
	if *completeCalls != 0 || *resetCalls != 0 || *skipCalls != 0 {
		t.Fatal("former direct keys should not dispatch in the detail list")
	}
}

// TestDetailTaskMenuRendersOverlay verifies the open menu's verbs render in the
// detail view, anchored under the cursored task.
func TestDetailTaskMenuRendersOverlay(t *testing.T) {
	row := DashboardRow{SetRef: SetRef{SetID: "set-render", DefPath: "/def"}}
	failedTask := tasks.Task{ID: "02-b", File: "02-b.md", Status: "failed", Type: "AFK", Title: "B"}
	m, _, _, _ := detailOverrideModel(row, failedTask, nil, nil, nil)
	m.width = 120
	m.height = 12
	m = openTaskMenu(t, m)
	out := m.View().Content
	for _, want := range []string{"actions", "C  complete", "O  open"} {
		if !strings.Contains(out, want) {
			t.Fatalf("rendered detail menu missing %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "K  skip") {
		t.Fatalf("failed task menu should not offer skip:\n%s", out)
	}
}

func TestMainListRuntimeShell(t *testing.T) {
	// runtimeShell opens the action menu and dispatches the shell verb (`O`).
	runtimeShell := func(m QueueDashboard) (QueueDashboard, tea.Cmd) {
		updated, _ := m.Update(tea.KeyPressMsg{Code: 'a', Text: "a"})
		got := updated.(QueueDashboard)
		if got.menu == nil {
			t.Fatal("a did not open the action menu")
		}
		updated, cmd := got.Update(tea.KeyPressMsg{Code: 'O', Text: "O"})
		return updated.(QueueDashboard), cmd
	}

	t.Run("O with empty runtimePath is no-op with statusMsg hint", func(t *testing.T) {
		row := DashboardRow{SetRef: SetRef{SetID: "set-x", DefPath: "/def", RuntimePath: ""}}
		m := newQueueDashboard(nil, nil, DashboardSnapshot{Rows: []DashboardRow{row}})
		got, cmd := runtimeShell(m)
		if cmd != nil {
			t.Fatal("O with empty runtimePath: expected no cmd")
		}
		if got.statusMsg == "" {
			t.Fatal("O with empty runtimePath: expected statusMsg hint")
		}
	})

	t.Run("O with whitespace-only runtimePath is no-op with statusMsg hint", func(t *testing.T) {
		row := DashboardRow{SetRef: SetRef{SetID: "set-y", DefPath: "/def", RuntimePath: "   "}}
		m := newQueueDashboard(nil, nil, DashboardSnapshot{Rows: []DashboardRow{row}})
		got, cmd := runtimeShell(m)
		if cmd != nil {
			t.Fatal("O with whitespace runtimePath: expected no cmd")
		}
		if got.statusMsg == "" {
			t.Fatal("O with whitespace runtimePath: expected statusMsg hint")
		}
	})

	t.Run("a with no rows does not open the menu", func(t *testing.T) {
		m := newQueueDashboard(nil, nil, DashboardSnapshot{})
		updated, cmd := m.Update(tea.KeyPressMsg{Code: 'a', Text: "a"})
		got := updated.(QueueDashboard)
		if cmd != nil {
			t.Fatal("a with no rows: expected no cmd")
		}
		if got.menu != nil {
			t.Fatal("a with no rows: menu must not open")
		}
		if got.statusMsg != "" {
			t.Fatalf("a with no rows: expected no statusMsg, got %q", got.statusMsg)
		}
	})

	t.Run("statusMsg hint rendered in view", func(t *testing.T) {
		row := DashboardRow{SetRef: SetRef{SetID: "set-z", DefPath: "/def", RuntimePath: ""}}
		m := newQueueDashboard(nil, nil, DashboardSnapshot{Rows: []DashboardRow{row}})
		m.statusMsg = "no checkout bound to this task set"
		v := m.View()
		if !strings.Contains(v.Content, "no checkout bound to this task set") {
			t.Fatalf("statusMsg not rendered in view:\n%s", v.Content)
		}
	})

	t.Run("menu offers the shell verb", func(t *testing.T) {
		row := DashboardRow{SetRef: SetRef{SetID: "set-hint", DefPath: "/def"}}
		m := newQueueDashboard(nil, nil, DashboardSnapshot{Rows: []DashboardRow{row}})
		updated, _ := m.Update(tea.KeyPressMsg{Code: 'a', Text: "a"})
		got := updated.(QueueDashboard)
		view := got.View().Content
		if !menuHasKey(got.menu, "O") {
			t.Fatalf("menu missing shell verb: %+v", got.menu)
		}
		if !strings.Contains(view, "O  shell") {
			t.Fatalf("menu view missing 'O  shell':\n%s", view)
		}
	})
}
