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
	if byID["done"].Status != "DONE · conflicts" || byID["done"].Worktree != "↳ done-branch" {
		t.Fatalf("done row = %+v", byID["done"])
	}
	if byID["ready"].Status != "READY" || byID["ready"].Worktree != "main" {
		t.Fatalf("ready row = %+v", byID["ready"])
	}
	if byID["bound"].Worktree != "↳ bound-branch" {
		t.Fatalf("bound row = %+v", byID["bound"])
	}
	if !DashboardRowCanIntegrate(byID["done"]) {
		t.Fatalf("done row should be integration-enabled: %+v", byID["done"])
	}
	if DashboardRowCanIntegrate(byID["ready"]) || DashboardRowCanIntegrate(byID["bound"]) {
		t.Fatalf("non-integration rows should not be integration-enabled: ready=%+v bound=%+v", byID["ready"], byID["bound"])
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

func TestDashboardBKeyOpensBindModal(t *testing.T) {
	m := newDashboardModel(&Deps{}, &config.Config{}, DashboardSnapshot{Rows: []DashboardRow{{
		Project: "pop", SetID: "set-bind", Status: "READY", Worktree: "/repo/main (main)",
		defPath: "/repo/tasks", statePath: "/repo/state.json", cursorKey: "pop\x00set-bind",
	}}})
	updated, cmd := m.Update(tea.KeyPressMsg{Code: 'b', Text: "b"})
	got := updated.(dashboardModel)
	if got.bind == nil || !got.bind.loading || got.bind.row.SetID != "set-bind" {
		t.Fatalf("bind modal = %+v, want loading modal for set-bind", got.bind)
	}
	if cmd == nil {
		t.Fatalf("b key did not return a worktree-loading command")
	}
}

func TestDashboardSKeyOpensDetailViewAndClosePreservesCursor(t *testing.T) {
	m := newDashboardModel(&Deps{}, &config.Config{}, DashboardSnapshot{Rows: []DashboardRow{
		{Project: "pop", SetID: "first", Status: "READY", cursorKey: "pop\x00first"},
		{Project: "pop", SetID: "second", Status: "READY", cursorKey: "pop\x00second"},
	}})
	m.cursor = 1

	// Enter: s opens detail view for the focused row.
	updated, cmd := m.Update(tea.KeyPressMsg{Code: 's', Text: "s"})
	got := updated.(dashboardModel)
	if got.detail == nil || !got.detail.loading || got.detail.row.SetID != "second" {
		t.Fatalf("detail view = %+v, want loading for second", got.detail)
	}
	if cmd == nil {
		t.Fatalf("s key did not return a loading command")
	}
	// View must be the detail view (no table).
	view := got.View().Content
	if strings.Contains(view, "project") && strings.Contains(view, "task set") {
		t.Fatalf("view should not show the table when detail is open:\n%s", view)
	}

	// Exit: q closes detail, returns to queue table, cursor unchanged.
	updated, cmd = got.Update(tea.KeyPressMsg{Code: 'q', Text: "q"})
	got = updated.(dashboardModel)
	if cmd != nil {
		t.Fatalf("q in detail view should close without quitting")
	}
	if got.detail != nil || got.cursor != 1 {
		t.Fatalf("after close: detail=%+v cursor=%d, want nil and 1", got.detail, got.cursor)
	}

	// Exit via esc also works.
	m2 := newDashboardModel(&Deps{}, &config.Config{}, DashboardSnapshot{Rows: []DashboardRow{
		{Project: "pop", SetID: "alpha", Status: "READY", cursorKey: "pop\x00alpha"},
	}})
	updated, _ = m2.Update(tea.KeyPressMsg{Code: 's', Text: "s"})
	updated, cmd = updated.(dashboardModel).Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	if cmd != nil || updated.(dashboardModel).detail != nil {
		t.Fatalf("esc should close detail view without quitting")
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
	d := &detailView{
		row:      DashboardRow{SetID: "set-normal"},
		manifest: manifest,
		taskRow:  taskRow,
		cursorID: "01-a",
	}
	var rendered strings.Builder
	renderDetailContent(&rendered, d)
	out := rendered.String()

	for _, want := range []string{"set-normal  [READY]  1/2 done, 1 open", "STATUS", "01-a", "02-b", "01-a"} {
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

	d := &detailView{
		row:      DashboardRow{SetID: "set-x"},
		manifest: manifest1,
		cursorID: "02-b",
	}

	// Cursor is at index 1 before refresh.
	if got := d.cursorIndex(); got != 1 {
		t.Fatalf("cursorIndex = %d, want 1", got)
	}

	// After refresh the manifest has the same task ID at index 0.
	d.syncManifest(manifest2, nil)

	// Cursor ID is preserved; index changed because order changed.
	if d.cursorID != "02-b" {
		t.Fatalf("cursorID = %q, want 02-b", d.cursorID)
	}
	if got := d.cursorIndex(); got != 0 {
		t.Fatalf("cursorIndex after refresh = %d, want 0", got)
	}

	// When cursor ID disappears, falls back to first task.
	manifest3 := &tasks.Manifest{
		Valid: true,
		Tasks: []tasks.Task{
			{ID: "03-c", Type: "AFK", Status: "open"},
		},
	}
	d.syncManifest(manifest3, nil)
	if d.cursorID != "03-c" {
		t.Fatalf("cursorID after disappear = %q, want 03-c", d.cursorID)
	}
}

func TestDashboardDetailViewJKNavigation(t *testing.T) {
	manifest := &tasks.Manifest{
		Valid: true,
		Tasks: []tasks.Task{
			{ID: "01-a", Type: "AFK", Status: "done"},
			{ID: "02-b", Type: "AFK", Status: "open"},
			{ID: "03-c", Type: "AFK", Status: "open"},
		},
	}
	m := newDashboardModel(&Deps{}, &config.Config{}, DashboardSnapshot{Rows: []DashboardRow{
		{Project: "pop", SetID: "set-nav", Status: "READY", cursorKey: "pop\x00set-nav"},
	}})
	m.detail = &detailView{row: m.snap.Rows[0], manifest: manifest, cursorID: "01-a"}

	// j moves cursor down.
	updated, _ := m.Update(tea.KeyPressMsg{Code: 'j', Text: "j"})
	got := updated.(dashboardModel)
	if got.detail.cursorID != "02-b" {
		t.Fatalf("after j: cursorID = %q, want 02-b", got.detail.cursorID)
	}

	// k moves cursor up.
	updated, _ = got.Update(tea.KeyPressMsg{Code: 'k', Text: "k"})
	got = updated.(dashboardModel)
	if got.detail.cursorID != "01-a" {
		t.Fatalf("after k: cursorID = %q, want 01-a", got.detail.cursorID)
	}

	// j/k clamp at boundaries.
	updated, _ = got.Update(tea.KeyPressMsg{Code: 'k', Text: "k"})
	got = updated.(dashboardModel)
	if got.detail.cursorID != "01-a" {
		t.Fatalf("k at top should clamp: cursorID = %q, want 01-a", got.detail.cursorID)
	}
}

func TestDashboardIKeyOnlyEnabledForIntegrationBacklog(t *testing.T) {
	m := newDashboardModel(&Deps{}, &config.Config{}, DashboardSnapshot{Rows: []DashboardRow{
		{Project: "pop", SetID: "ready", Status: "READY"},
		{Project: "pop", SetID: "done", Status: "DONE · clean", integrationBacklog: true},
	}})
	updated, cmd := m.Update(tea.KeyPressMsg{Code: 'I', Text: "I"})
	if cmd != nil {
		t.Fatalf("I key on non-integration row returned command")
	}
	got := updated.(dashboardModel)
	got.cursor = 1
	_, cmd = got.Update(tea.KeyPressMsg{Code: 'I', Text: "I"})
	if cmd == nil {
		t.Fatalf("I key on integration backlog row returned nil command")
	}
}

func TestDashboardLaunchIntegrateDispatchesExistingWizardAndSwitchesPane(t *testing.T) {
	repo, setID, _ := setupSupervisorSpawnRepo(t, "dashboard-integrate", []spawnTestTask{
		{ID: "01-a", File: "01-a.md", Title: "A", Type: "AFK", Status: "done"},
	})
	d, cfg, row, rt := dashboardLaunchFixture(t, repo, setID)
	row.integrationBacklog = true

	result, err := LaunchDashboardIntegrate(d, cfg, row)
	if err != nil {
		t.Fatalf("LaunchDashboardIntegrate: %v", err)
	}
	if result.PaneID != "%3" {
		t.Fatalf("pane = %q, want fresh queue pane %%3", result.PaneID)
	}
	sendKeys, ok := rt.findCommand("send-keys")
	if !ok {
		t.Fatal("expected integrate command to be sent")
	}
	if got := strings.Join(sendKeys, " "); !strings.Contains(got, "pop tasks integrate "+setID) {
		t.Fatalf("send-keys = %v, want existing integrate entry point", sendKeys)
	}
	if _, ok := rt.findCommand("select-pane"); !ok {
		t.Fatal("expected integrate pane to be selected")
	}
	switchClient, ok := rt.findCommand("switch-client")
	if !ok || !argsContain(switchClient, "-t", "%3") {
		t.Fatalf("switch-client = %v, want pane %%3", switchClient)
	}
}

func TestDashboardBaseRefsMainMasterFirst(t *testing.T) {
	got := parseDashboardBaseRefs("feature\norigin/master\norigin/HEAD\nmaster\norigin/main\nmain\n")
	want := []string{"main", "master", "origin/main", "origin/master", "feature"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("refs = %v, want %v", got, want)
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

func TestDashboardBindPickerListsAndAdoptsExistingWorktree(t *testing.T) {
	repo, setID, _ := setupSupervisorSpawnRepo(t, "bind-existing", []spawnTestTask{
		{ID: "01-a", File: "01-a.md", Title: "A", Type: "AFK", Status: "open"},
	})
	wt1 := filepath.Join(t.TempDir(), "existing-one")
	wt2 := filepath.Join(t.TempDir(), "existing-two")
	runGit(t, repo, "worktree", "add", "-b", "existing-one", wt1, "HEAD")
	runGit(t, repo, "worktree", "add", "-b", "existing-two", wt2, "HEAD")
	d, cfg, row, _ := dashboardLaunchFixture(t, repo, setID)

	entries, err := DashboardBindWorktreeEntries(d, cfg, row)
	if err != nil {
		t.Fatalf("DashboardBindWorktreeEntries: %v", err)
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

	got, err := DashboardAdoptWorktree(d, cfg, row, wt1)
	if err != nil {
		t.Fatalf("DashboardAdoptWorktree: %v", err)
	}
	if got.RuntimePath != wt1 || got.Branch != "existing-one" {
		t.Fatalf("adopt result = %+v, want %s existing-one", got, wt1)
	}

	repointed, err := DashboardAdoptWorktree(d, cfg, row, wt2)
	if err != nil {
		t.Fatalf("idle re-point should not require force prompt: %v", err)
	}
	if !repointed.Replaced || repointed.RuntimePath != wt2 {
		t.Fatalf("repoint result = %+v, want replaced binding to %s", repointed, wt2)
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
	if binding.RuntimePath != wt2 || binding.Provisioned {
		t.Fatalf("binding = %+v, want adopted %s", binding, wt2)
	}
	snap, err := BuildDashboard(d, cfg)
	if err != nil {
		t.Fatalf("BuildDashboard: %v", err)
	}
	if len(snap.Rows) == 0 || snap.Rows[0].Worktree != "↳ existing-two" {
		t.Fatalf("dashboard rows = %+v, want worktree column updated", snap.Rows)
	}
}

func TestDashboardCreateWorktreeManagedFreshBranchNoSession(t *testing.T) {
	repo, setID, _ := setupSupervisorSpawnRepo(t, "bind-create", []spawnTestTask{
		{ID: "01-a", File: "01-a.md", Title: "A", Type: "AFK", Status: "open"},
	})
	d, cfg, row, rt := dashboardLaunchFixture(t, repo, setID)

	refs, err := DashboardBindBaseRefs(d, cfg, row)
	if err != nil {
		t.Fatalf("DashboardBindBaseRefs: %v", err)
	}
	if len(refs) == 0 || refs[0] != "main" {
		t.Fatalf("refs = %v, want main first", refs)
	}

	got, err := DashboardCreateWorktree(d, cfg, row, "main", "fresh-dashboard-branch")
	if err != nil {
		t.Fatalf("DashboardCreateWorktree: %v", err)
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
	state, err := ReadDaemonState(d.Tasks)
	if err != nil {
		t.Fatal(err)
	}
	repoKey, err := resolveRepoKey(d, repo)
	if err != nil {
		t.Fatal(err)
	}
	binding := state.WorktreeBindings[setScopedKey(repoKey, setID)]
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
	row.repoKey = repoKey
	row.runtimePath = locked
	if err := WriteDaemonState(d.Tasks, &DaemonState{Version: 1, WorktreeBindings: map[string]WorktreeBinding{
		setScopedKey(repoKey, setID): {RuntimePath: locked, Branch: "locked-branch", Project: "pop", Provisioned: false},
	}}); err != nil {
		t.Fatal(err)
	}
	d.ReadLock = func(runtimePath string) *tasks.RuntimeLockStatus {
		if runtimePath == locked {
			lock := liveLock(runtimePath)
			lock.Metadata.SetID = setID
			return lock
		}
		return idleLock(runtimePath)
	}

	_, err = DashboardAdoptWorktree(d, cfg, row, target)
	if err == nil || !strings.Contains(err.Error(), "currently executing") {
		t.Fatalf("DashboardAdoptWorktree err = %v, want live-lock refusal", err)
	}
	after, err := ReadDaemonState(d.Tasks)
	if err != nil {
		t.Fatal(err)
	}
	if got := after.WorktreeBindings[setScopedKey(repoKey, setID)].RuntimePath; got != locked {
		t.Fatalf("binding runtime = %q, want unchanged %q", got, locked)
	}
}

func TestDashboardUKeyRequiresInlineConfirmBeforeUnbind(t *testing.T) {
	m := newDashboardModel(&Deps{}, &config.Config{}, DashboardSnapshot{Rows: []DashboardRow{{
		Project: "pop", SetID: "set-unbind", Status: "FAILED", Worktree: "/repo/bound (branch)",
		defPath: "/repo/tasks", statePath: "/repo/state.json", cursorKey: "pop\x00set-unbind",
	}}})

	updated, cmd := m.Update(tea.KeyPressMsg{Code: 'U', Text: "U"})
	got := updated.(dashboardModel)
	if cmd != nil {
		t.Fatalf("U key returned command before confirmation")
	}
	if got.abandon == nil || got.abandon.row.SetID != "set-unbind" {
		t.Fatalf("abandon modal = %+v, want set-unbind", got.abandon)
	}
	if !strings.Contains(got.View().Content, "Unbind worktree for set-unbind") {
		t.Fatalf("view missing unbind modal:\n%s", got.View().Content)
	}

	updated, cmd = got.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	got = updated.(dashboardModel)
	if cmd == nil {
		t.Fatalf("confirm did not return unbind command")
	}
	if got.abandon == nil || !got.abandon.loading {
		t.Fatalf("abandon modal after confirm = %+v, want loading", got.abandon)
	}

	updated, cmd = m.Update(tea.KeyPressMsg{Code: 'U', Text: "U"})
	got = updated.(dashboardModel)
	updated, cmd = got.Update(tea.KeyPressMsg{Code: 'n', Text: "n"})
	got = updated.(dashboardModel)
	if cmd != nil || got.abandon != nil {
		t.Fatalf("cancel should close modal without command: modal=%+v cmd=%v", got.abandon, cmd)
	}
}

func TestDashboardUnbindManagedTearsDownAndRefreshShowsQueueBase(t *testing.T) {
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
	row.repoKey = repoKey
	row.runtimePath = wt
	if err := WriteDaemonState(d.Tasks, &DaemonState{Version: 1, WorktreeBindings: map[string]WorktreeBinding{
		setScopedKey(repoKey, setID): {RuntimePath: wt, Branch: "managed-unbind", Project: filepath.Base(repo), Provisioned: true},
	}}); err != nil {
		t.Fatal(err)
	}

	got, err := DashboardUnbindWorktree(d, cfg, row)
	if err != nil {
		t.Fatalf("DashboardUnbindWorktree: %v", err)
	}
	if got.Noop {
		t.Fatalf("unbind result = %+v, want success", got)
	}
	if _, err := os.Stat(wt); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("managed checkout stat err = %v, want not exist", err)
	}
	if branch := runGitOutput(t, repo, "branch", "--list", "managed-unbind"); strings.TrimSpace(branch) != "" {
		t.Fatalf("managed branch still exists: %q", branch)
	}
	after, err := ReadDaemonState(d.Tasks)
	if err != nil {
		t.Fatal(err)
	}
	if len(after.WorktreeBindings) != 0 {
		t.Fatalf("bindings = %+v, want cleared", after.WorktreeBindings)
	}
	afterManifest := mustReadFile(t, filepath.Join(id.TasksDir, setID, "index.json"))
	if string(beforeManifest) != string(afterManifest) {
		t.Fatalf("manifest changed:\nbefore:%s\nafter:%s", beforeManifest, afterManifest)
	}
	snap, err := BuildDashboard(d, cfg)
	if err != nil {
		t.Fatalf("BuildDashboard: %v", err)
	}
	if len(snap.Rows) == 0 || canon(t, d, snap.Rows[0].runtimePath) != canon(t, d, repo) || snap.Rows[0].Worktree != "main" {
		t.Fatalf("dashboard rows = %+v, want queue base worktree", snap.Rows)
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
	row.repoKey = repoKey
	row.runtimePath = wt
	if err := WriteDaemonState(d.Tasks, &DaemonState{Version: 1, WorktreeBindings: map[string]WorktreeBinding{
		setScopedKey(repoKey, setID): {RuntimePath: wt, Branch: "adopted-unbind", Project: filepath.Base(repo), Provisioned: false},
	}}); err != nil {
		t.Fatal(err)
	}

	got, err := DashboardUnbindWorktree(d, cfg, row)
	if err != nil {
		t.Fatalf("DashboardUnbindWorktree: %v", err)
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
	after, err := ReadDaemonState(d.Tasks)
	if err != nil {
		t.Fatal(err)
	}
	if len(after.WorktreeBindings) != 0 {
		t.Fatalf("bindings = %+v, want cleared", after.WorktreeBindings)
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
	row.repoKey = repoKey
	row.runtimePath = wt
	if err := WriteDaemonState(d.Tasks, &DaemonState{Version: 1, WorktreeBindings: map[string]WorktreeBinding{
		setScopedKey(repoKey, setID): {RuntimePath: wt, Branch: "locked-unbind", Project: filepath.Base(repo), Provisioned: false},
	}}); err != nil {
		t.Fatal(err)
	}
	d.ReadLock = func(runtimePath string) *tasks.RuntimeLockStatus {
		if runtimePath == wt {
			lock := liveLock(runtimePath)
			lock.Metadata.SetID = setID
			return lock
		}
		return idleLock(runtimePath)
	}

	_, err = DashboardUnbindWorktree(d, cfg, row)
	if err == nil || !strings.Contains(err.Error(), "refusing unbind") {
		t.Fatalf("DashboardUnbindWorktree err = %v, want live-lock refusal", err)
	}
	after, err := ReadDaemonState(d.Tasks)
	if err != nil {
		t.Fatal(err)
	}
	if got := after.WorktreeBindings[setScopedKey(repoKey, setID)].RuntimePath; got != wt {
		t.Fatalf("binding runtime = %q, want unchanged %q", got, wt)
	}

	d.ReadLock = func(runtimePath string) *tasks.RuntimeLockStatus {
		t.Fatalf("no-binding unbind must not read runtime lock")
		return nil
	}
	if err := WriteDaemonState(d.Tasks, &DaemonState{Version: 1, WorktreeBindings: map[string]WorktreeBinding{}}); err != nil {
		t.Fatal(err)
	}
	got, err := DashboardUnbindWorktree(d, cfg, row)
	if err != nil {
		t.Fatalf("no-binding DashboardUnbindWorktree: %v", err)
	}
	if !got.Noop {
		t.Fatalf("no-binding result = %+v, want noop", got)
	}
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
	// A real repo with task sets always carries a repo.json storage marker
	// (EnsureStorage writes it on first task touch); the spawn fixture writes
	// task files directly and skips it, so write it here so BuildDashboard's
	// storage-scoped discovery sees this repo.
	if err := tasks.EnsureStorage(tasks.DefaultDeps(), id); err != nil {
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

func filterTestModel() dashboardModel {
	rows := []DashboardRow{
		{Project: "alpha", SetID: "set-one", Status: "READY", cursorKey: "alpha\x00set-one"},
		{Project: "beta", SetID: "set-two", Status: "READY", cursorKey: "beta\x00set-two"},
		{Project: "gamma", SetID: "feature", Status: "FAILED", cursorKey: "gamma\x00feature"},
	}
	m := newDashboardModel(&Deps{}, &config.Config{}, DashboardSnapshot{Rows: rows})
	m.cursor = 2
	return m
}

func TestDashboardFilterMode_SlashEntersFilterMode(t *testing.T) {
	m := filterTestModel()
	updated, _ := m.Update(tea.KeyPressMsg{Code: '/', Text: "/"})
	got := updated.(dashboardModel)
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
	m = updated.(dashboardModel)
	// Type a filter
	updated, _ = m.Update(tea.KeyPressMsg{Code: 'b', Text: "b"})
	m = updated.(dashboardModel)
	if len(m.snap.Rows) != 1 {
		t.Fatalf("after 'b' filter: rows = %d, want 1", len(m.snap.Rows))
	}
	// Esc exits filter mode and restores all rows
	updated, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	got := updated.(dashboardModel)
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
	m = updated.(dashboardModel)

	// Type "alpha" — matches Project "alpha"
	for _, ch := range "alpha" {
		updated, _ = m.Update(tea.KeyPressMsg{Code: ch, Text: string(ch)})
		m = updated.(dashboardModel)
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
	m = updated.(dashboardModel)

	// "feature" matches SetID "feature" in project "gamma"
	for _, ch := range "feature" {
		updated, _ = m.Update(tea.KeyPressMsg{Code: ch, Text: string(ch)})
		m = updated.(dashboardModel)
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
	m.cursor = 2 // on gamma/feature
	updated, _ := m.Update(tea.KeyPressMsg{Code: '/', Text: "/"})
	m = updated.(dashboardModel)

	// Type "alpha" — only alpha/set-one matches; cursor must move within bounds
	for _, ch := range "alpha" {
		updated, _ = m.Update(tea.KeyPressMsg{Code: ch, Text: string(ch)})
		m = updated.(dashboardModel)
	}
	if m.cursor < 0 || m.cursor >= len(m.snap.Rows) {
		t.Fatalf("cursor = %d, out of bounds for %d filtered rows", m.cursor, len(m.snap.Rows))
	}
}

func TestDashboardFilterMode_NavigationWorksInsideFilter(t *testing.T) {
	m := filterTestModel()
	updated, _ := m.Update(tea.KeyPressMsg{Code: '/', Text: "/"})
	m = updated.(dashboardModel)
	// Type "set" to match two rows
	for _, ch := range "set" {
		updated, _ = m.Update(tea.KeyPressMsg{Code: ch, Text: string(ch)})
		m = updated.(dashboardModel)
	}
	if len(m.snap.Rows) != 2 {
		t.Fatalf("after 'set': rows = %d, want 2", len(m.snap.Rows))
	}
	m.cursor = 0
	updated, _ = m.Update(tea.KeyPressMsg{Code: 'j', Text: "j"})
	got := updated.(dashboardModel)
	if got.cursor != 1 {
		t.Fatalf("j in filter mode: cursor = %d, want 1", got.cursor)
	}
	updated, _ = got.Update(tea.KeyPressMsg{Code: 'k', Text: "k"})
	got = updated.(dashboardModel)
	if got.cursor != 0 {
		t.Fatalf("k in filter mode: cursor = %d, want 0", got.cursor)
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
		{Project: "alpha", SetID: "set-one", Status: "READY", cursorKey: "alpha\x00set-one",
			defPath: "/def", statePath: "/state"},
	}
	m := newDashboardModel(d, &config.Config{}, DashboardSnapshot{Rows: rows})
	m.cursor = 0
	// Enter filter mode
	updated, _ := m.Update(tea.KeyPressMsg{Code: '/', Text: "/"})
	m = updated.(dashboardModel)
	// Action keys should NOT trigger actions — they go to the filter input
	for _, key := range []tea.KeyPressMsg{
		{Code: 'i', Text: "i"},
		{Code: 'I', Text: "I"},
		{Code: 'b', Text: "b"},
		{Code: 'U', Text: "U"},
		{Code: 'a', Text: "a"},
		{Code: 'p', Text: "p"},
		{Code: 's', Text: "s"},
	} {
		updated, _ = m.Update(key)
		m = updated.(dashboardModel)
	}
	if called {
		t.Fatal("bare-letter actions must be inert in filter mode")
	}
	if m.bind != nil || m.abandon != nil || m.detail != nil {
		t.Fatal("modals must not open while in filter mode")
	}
}

func TestDashboardFilterMode_QKeyGoesToInputNotQuit(t *testing.T) {
	m := filterTestModel()
	updated, _ := m.Update(tea.KeyPressMsg{Code: '/', Text: "/"})
	m = updated.(dashboardModel)
	// 'q' in filter mode goes to the input box, not quit
	updated, _ = m.Update(tea.KeyPressMsg{Code: 'q', Text: "q"})
	got := updated.(dashboardModel)
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
	m = updated.(dashboardModel)
	for _, ch := range "alpha" {
		updated, _ = m.Update(tea.KeyPressMsg{Code: ch, Text: string(ch)})
		m = updated.(dashboardModel)
	}
	if len(m.snap.Rows) != 1 {
		t.Fatalf("before reload: rows = %d, want 1", len(m.snap.Rows))
	}

	// Simulate a reload with new rows that still include alpha
	newRows := []DashboardRow{
		{Project: "alpha", SetID: "set-one", Status: "BLOCKED", cursorKey: "alpha\x00set-one"},
		{Project: "beta", SetID: "set-two", Status: "READY", cursorKey: "beta\x00set-two"},
		{Project: "delta", SetID: "alpha-task", Status: "READY", cursorKey: "delta\x00alpha-task"},
	}
	updated, _ = m.Update(dashboardRowsMsg{snap: DashboardSnapshot{Rows: newRows}})
	got := updated.(dashboardModel)

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
		{Project: "alpha", SetID: "set-one"},
		{Project: "beta", SetID: "set-two"},
		{Project: "gamma", SetID: "feature"},
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
