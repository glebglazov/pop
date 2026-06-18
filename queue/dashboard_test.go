package queue

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/glebglazov/pop/config"
	"github.com/glebglazov/pop/internal/deps"
	"github.com/glebglazov/pop/project"
	"github.com/glebglazov/pop/tasks"
)

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
	state := &DaemonState{Version: 1, Mergeability: map[string]MergeabilityRecord{
		setScopedKey("repo-key", "done-integrating"): {RuntimePath: "/repo/done", SetID: "done-integrating", Status: MergeabilityClean},
	}}
	scans := []projectScan{{Name: "pop", ProjectPath: "/repo/main", RuntimePath: "/repo/main", DefinitionPath: "/def", RepoKey: "repo-key"}}

	got, err := dashboardRowsForRepo(d, &config.Config{}, state, scans)
	if err != nil {
		t.Fatal(err)
	}
	var ids []string
	for _, row := range got {
		ids = append(ids, row.SetID)
	}
	want := []string{"ready", "failed", "blocked", "deferred", "missing", "malformed", "done-integrating"}
	if !reflect.DeepEqual(ids, want) {
		t.Fatalf("ids = %v, want %v", ids, want)
	}
}

func TestDashboardSortOrder(t *testing.T) {
	rows := []DashboardRow{
		{Project: "zeta", SetID: "2026-01-01-old"},
		{Project: "alpha", SetID: "2026-01-01-old"},
		{Project: "alpha", SetID: "2026-06-18-new"},
	}
	sortDashboardRows(rows)
	got := []string{rows[0].Project + "/" + rows[0].SetID, rows[1].Project + "/" + rows[1].SetID, rows[2].Project + "/" + rows[2].SetID}
	want := []string{"alpha/2026-06-18-new", "alpha/2026-01-01-old", "zeta/2026-01-01-old"}
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
	state := &DaemonState{
		Version: 1,
		Mergeability: map[string]MergeabilityRecord{
			setScopedKey("repo-key", "done"): {RuntimePath: "/repo/done", SetID: "done", Status: MergeabilityConflicts},
		},
		WorktreeBindings: map[string]WorktreeBinding{
			setScopedKey("repo-key", "done"):  {RuntimePath: "/repo/done", Branch: "done-branch"},
			setScopedKey("repo-key", "bound"): {RuntimePath: "/repo/bound", Branch: "bound-branch"},
		},
	}
	scans := []projectScan{{Name: "pop", ProjectPath: "/repo/main", RuntimePath: "/repo/main", DefinitionPath: "/def", RepoKey: "repo-key"}}

	got, err := dashboardRowsForRepo(d, &config.Config{}, state, scans)
	if err != nil {
		t.Fatal(err)
	}
	byID := map[string]DashboardRow{}
	for _, row := range got {
		byID[row.SetID] = row
	}
	if byID["done"].Status != "DONE · conflicts" || byID["done"].Worktree != "/repo/done (done-branch)" {
		t.Fatalf("done row = %+v", byID["done"])
	}
	if byID["ready"].Status != "READY" || byID["ready"].Worktree != "/repo/main (main)" {
		t.Fatalf("ready row = %+v", byID["ready"])
	}
	if byID["bound"].Worktree != "/repo/bound (bound-branch)" {
		t.Fatalf("bound row = %+v", byID["bound"])
	}
}

func TestDashboardNoBaseWorktree(t *testing.T) {
	d := dashboardTestDeps(t, []tasks.Row{{ID: "missing", Status: tasks.StatusMissing}}, nil)
	scans := []projectScan{{Name: "bare", ProjectPath: "/repo/bare.git", RuntimePath: "/repo/bare.git", DefinitionPath: "/def", RepoKey: "bare-key"}}

	got, err := dashboardRowsForRepo(d, &config.Config{}, &DaemonState{Version: 1}, scans)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Worktree != "(no base)" {
		t.Fatalf("rows = %+v, want no base", got)
	}
}

func TestDashboardPickedUpIndicator(t *testing.T) {
	locks := map[string]*tasks.RuntimeLockStatus{
		"/repo/main": {
			RuntimePath: "/repo/main",
			Locked:      true,
			Metadata:    &tasks.RuntimeLockMetadata{PID: 123, RuntimePath: "/repo/main", SetID: "ready"},
		},
	}
	d := dashboardTestDeps(t, []tasks.Row{
		{ID: "ready", Status: tasks.StatusReady, AutoDrain: true},
		{ID: "other", Status: tasks.StatusReady, AutoDrain: true},
	}, locks)
	scans := []projectScan{{Name: "pop", ProjectPath: "/repo/main", RuntimePath: "/repo/main", DefinitionPath: "/def", RepoKey: "repo-key"}}

	got, err := dashboardRowsForRepo(d, &config.Config{}, &DaemonState{Version: 1}, scans)
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

func TestDashboardAutoDrainBadgeAndToggle(t *testing.T) {
	rows := []DashboardRow{
		{Project: "pop", SetID: "marked", Status: "READY", Worktree: "/repo/main (main)", AutoDrain: true},
		{Project: "pop", SetID: "plain", Status: "READY", Worktree: "/repo/main (main)"},
	}
	var rendered strings.Builder
	renderDashboardTable(&rendered, rows, 0)
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
	m := newDashboardModel(d, &config.Config{}, DashboardSnapshot{Rows: []DashboardRow{{
		Project: "pop", SetID: "plain", Status: "READY", Worktree: "/repo/main (main)",
		defPath: "/repo/tasks", statePath: "/repo/state.json", cursorKey: "pop\x00plain",
	}}})
	updated, cmd := m.Update(tea.KeyPressMsg{Code: 'a', Text: "a"})
	got := updated.(dashboardModel)
	if !got.snap.Rows[0].AutoDrain {
		t.Fatalf("toggle did not update badge immediately: %+v", got.snap.Rows[0])
	}
	msg := cmd().(dashboardToggleMsg)
	updated, _ = got.Update(msg)
	got = updated.(dashboardModel)
	if toggledDef != "/repo/tasks" || toggledState != "/repo/state.json" || toggledSet != "plain" {
		t.Fatalf("toggle target = (%q, %q, %q)", toggledDef, toggledState, toggledSet)
	}
	if !got.snap.Rows[0].AutoDrain || got.err != nil {
		t.Fatalf("toggle result = row %+v err %v", got.snap.Rows[0], got.err)
	}
}

func TestDashboardLaunchDrainRoutesPlainQueueBaseAndRecordsPane(t *testing.T) {
	repo, setID, _ := setupSupervisorSpawnRepo(t, "plain-drain", []spawnTestTask{
		{ID: "01-a", File: "01-a.md", Title: "A", Type: "AFK", Status: "open"},
	})
	d, cfg, row, rt := dashboardLaunchFixture(t, repo, setID)

	result, err := LaunchDashboardDrain(d, cfg, row)
	if err != nil {
		t.Fatalf("LaunchDashboardDrain: %v", err)
	}
	if canon(t, d, result.RuntimePath) != canon(t, d, repo) {
		t.Fatalf("runtime = %q, want queue base %q", result.RuntimePath, repo)
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
	if err := WriteDaemonState(d.Tasks, &DaemonState{Version: 1, WorktreeBindings: map[string]WorktreeBinding{
		setScopedKey(repoKey, setID): {RuntimePath: bound, Branch: "bound", Project: "pop", Provisioned: false},
	}}); err != nil {
		t.Fatal(err)
	}

	result, err := LaunchDashboardDrain(d, cfg, row)
	if err != nil {
		t.Fatalf("LaunchDashboardDrain: %v", err)
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

func TestDashboardLaunchDrainProvisionsManagedWorktreeWhenReady(t *testing.T) {
	repo, setID, _ := setupSupervisorSpawnRepo(t, "managed-drain", []spawnTestTask{
		{ID: "01-a", File: "01-a.md", Title: "A", Type: "AFK", Status: "open"},
	})
	if err := os.WriteFile(filepath.Join(repo, ".pop.toml"), []byte("worktree_ready = true\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	d, cfg, row, rt := dashboardLaunchFixture(t, repo, setID)

	result, err := LaunchDashboardDrain(d, cfg, row)
	if err != nil {
		t.Fatalf("LaunchDashboardDrain: %v", err)
	}
	if result.RuntimePath == repo || !strings.Contains(result.RuntimePath, filepath.Join("pop", "queue", "worktrees")) {
		t.Fatalf("runtime = %q, want managed queue worktree", result.RuntimePath)
	}
	cmd, ok := extractSpawnCommand(rt)
	if !ok || !strings.Contains(cmd, "--task-runtime-path "+result.RuntimePath) {
		t.Fatalf("spawn command = %q, want runtime override %q", cmd, result.RuntimePath)
	}
	state, err := ReadDaemonState(d.Tasks)
	if err != nil {
		t.Fatal(err)
	}
	repoKey, err := resolveRepoKey(d, repo)
	if err != nil {
		t.Fatal(err)
	}
	binding := state.WorktreeBindings[setScopedKey(repoKey, setID)]
	if binding.RuntimePath != result.RuntimePath || !binding.Provisioned {
		t.Fatalf("binding = %+v, want managed worktree %q", binding, result.RuntimePath)
	}
	assertDashboardPaneMapping(t, d, repo, setID, "%3", "dashboard")
}

func TestDashboardLaunchDrainRefusesBareWithoutQueueBase(t *testing.T) {
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
	if _, err := tasks.RefreshWith(tasks.DefaultDeps(), id.TasksDir, tasks.StatePathFor(id.TasksDir)); err != nil {
		t.Fatal(err)
	}
	d, cfg, row, rt := dashboardLaunchFixture(t, checkout, setID)

	_, err = LaunchDashboardDrain(d, cfg, row)
	if err == nil || !strings.Contains(err.Error(), repoScanReason) {
		t.Fatalf("LaunchDashboardDrain err = %v, want %q", err, repoScanReason)
	}
	if len(rt.commands) != 0 {
		t.Fatalf("bare no-base refusal must not touch tmux, got %v", rt.commands)
	}
}

func TestDashboardPreviewDrainPaneAndNoOp(t *testing.T) {
	rt := newRecordingTmux(true, drainWindowName)
	d := &Deps{Tmux: rt}
	if err := PreviewDashboardDrain(d, DashboardRow{paneID: "%9"}); err != nil {
		t.Fatalf("PreviewDashboardDrain: %v", err)
	}
	if _, ok := rt.findCommand("select-pane"); !ok {
		t.Fatal("expected select-pane")
	}
	switchClient, ok := rt.findCommand("switch-client")
	if !ok || !argsContain(switchClient, "-t", "%9") {
		t.Fatalf("switch-client = %v, want pane %%9", switchClient)
	}
	rt.commands = nil
	if err := PreviewDashboardDrain(d, DashboardRow{}); err != nil {
		t.Fatalf("PreviewDashboardDrain no-op: %v", err)
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
	cfg := &config.Config{Projects: []config.ProjectEntry{{Path: repo}}}
	rt := newRecordingTmux(false, "0")
	td := queueTestTasksDeps(true)
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
	row := DashboardRow{Project: "pop", SetID: setID, defPath: scan.DefinitionPath, statePath: tasks.StatePathFor(id.TasksDir)}
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
