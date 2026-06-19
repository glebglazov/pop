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

func TestDashboardSKeyOpensStatusModalAndClosePreservesCursor(t *testing.T) {
	m := newDashboardModel(&Deps{}, &config.Config{}, DashboardSnapshot{Rows: []DashboardRow{
		{Project: "pop", SetID: "first", Status: "READY", cursorKey: "pop\x00first"},
		{Project: "pop", SetID: "second", Status: "READY", cursorKey: "pop\x00second"},
	}})
	m.cursor = 1

	updated, cmd := m.Update(tea.KeyPressMsg{Code: 's', Text: "s"})
	got := updated.(dashboardModel)
	if got.status == nil || !got.status.loading || got.status.row.SetID != "second" {
		t.Fatalf("status modal = %+v, want loading modal for second", got.status)
	}
	if cmd == nil {
		t.Fatalf("s key did not return a status-loading command")
	}

	updated, cmd = got.Update(tea.KeyPressMsg{Code: 'q', Text: "q"})
	got = updated.(dashboardModel)
	if cmd != nil {
		t.Fatalf("q in status modal should close modal without quitting")
	}
	if got.status != nil || got.cursor != 1 {
		t.Fatalf("after close: status=%+v cursor=%d, want nil and 1", got.status, got.cursor)
	}
}

func TestDashboardStatusModalRendersNormalTaskDetail(t *testing.T) {
	d := &Deps{Refresh: func(string) (*tasks.RefreshResult, error) {
		return &tasks.RefreshResult{
			Rows: []tasks.Row{{ID: "set-normal", Status: tasks.StatusReady, Progress: "1/2 done, 1 open"}},
			Manifests: map[string]*tasks.Manifest{"set-normal": {
				Valid: true,
				Tasks: []tasks.Task{
					{ID: "01-a", File: "01-a.md", Title: "First", Type: "AFK", Status: "done"},
					{ID: "02-b", File: "02-b.md", Title: "Second", Type: "AFK", Status: "open", BlockedBy: []string{"01-a"}},
				},
			}},
		}, nil
	}}
	lines, err := DashboardStatusDetailLines(d, DashboardRow{SetID: "set-normal"})
	if err != nil {
		t.Fatalf("DashboardStatusDetailLines: %v", err)
	}
	modal := &dashboardStatusModal{row: DashboardRow{SetID: "set-normal"}, lines: lines}
	var rendered strings.Builder
	renderDashboardStatusModal(&rendered, modal, 20)
	out := rendered.String()

	for _, want := range []string{"Task status: set-normal", "set-normal  [READY]  1/2 done, 1 open", "STATUS", "01-a", "02-b", "01-a"} {
		if !strings.Contains(out, want) {
			t.Fatalf("rendered modal missing %q:\n%s", want, out)
		}
	}
	if strings.Index(out, "01-a") > strings.Index(out, "02-b") {
		t.Fatalf("tasks out of manifest order:\n%s", out)
	}
}

func TestDashboardStatusModalRendersMalformedDiagnostics(t *testing.T) {
	d := &Deps{Refresh: func(string) (*tasks.RefreshResult, error) {
		return &tasks.RefreshResult{
			Rows: []tasks.Row{{
				ID:               "set-broken",
				Status:           tasks.StatusMalformed,
				MalformedSummary: "bad manifest",
				DetailErrors:     []string{"task \"01-a\": invalid status \"wat\""},
			}},
			Manifests: map[string]*tasks.Manifest{"set-broken": {
				Valid:  false,
				Errors: []string{"task \"01-a\": invalid status \"wat\""},
			}},
		}, nil
	}}
	lines, err := DashboardStatusDetailLines(d, DashboardRow{SetID: "set-broken"})
	if err != nil {
		t.Fatalf("DashboardStatusDetailLines: %v", err)
	}
	modal := &dashboardStatusModal{row: DashboardRow{SetID: "set-broken"}, lines: lines}
	var rendered strings.Builder
	renderDashboardStatusModal(&rendered, modal, 20)
	out := rendered.String()

	if !strings.Contains(out, "set-broken  [MALFORMED]") || !strings.Contains(out, "malformed manifest") || !strings.Contains(out, "invalid status") {
		t.Fatalf("expected malformed diagnostics:\n%s", out)
	}
	if strings.Contains(out, "STATUS") {
		t.Fatalf("malformed modal should not print a task table:\n%s", out)
	}
}

func TestDashboardStatusModalRendersMissingDiagnostic(t *testing.T) {
	d := &Deps{Refresh: func(string) (*tasks.RefreshResult, error) {
		return &tasks.RefreshResult{
			Rows:      []tasks.Row{{ID: "set-missing", Status: tasks.StatusMissing}},
			Manifests: map[string]*tasks.Manifest{},
		}, nil
	}}
	lines, err := DashboardStatusDetailLines(d, DashboardRow{SetID: "set-missing"})
	if err != nil {
		t.Fatalf("DashboardStatusDetailLines: %v", err)
	}
	modal := &dashboardStatusModal{row: DashboardRow{SetID: "set-missing"}, lines: lines}
	var rendered strings.Builder
	renderDashboardStatusModal(&rendered, modal, 20)
	out := rendered.String()

	if !strings.Contains(out, "set-missing  [MISSING]") || !strings.Contains(out, "registered task set missing") {
		t.Fatalf("expected missing diagnostic:\n%s", out)
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
