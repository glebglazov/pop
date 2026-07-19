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
	"charm.land/lipgloss/v2"
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
	tasksDeps := &tasks.Deps{FS: fs, Git: git}
	// The store handle is now process-cached; close it at test end so it does not
	// outlive this test's temp data dir (test cleanup, per ADR-0118).
	t.Cleanup(func() { _ = tasksDeps.CloseStore() })
	return &Deps{
		Tasks:   tasksDeps,
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
	storageDir := ""
	if scan.DefinitionPath != "" {
		storageDir = filepath.Dir(scan.DefinitionPath)
	}
	return dashboardRepoStatic{
		defPath:       scan.DefinitionPath,
		statePath:     tasks.StatePathFor(scan.DefinitionPath),
		storageDir:    storageDir,
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
	s, err := store.Open(tasks.DrainStorePathWith(td), func(int, string) bool { return true })
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
	got, err := dashboardRowsForStatic(d, enabled, staticForScan(scan, "main", false))
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("rows = %+v, want one", got)
	}
	if got[0].RawStatus != tasks.StatusVerifyFailed {
		t.Fatalf("RawStatus = %q, want VERIFY-FAILED", got[0].RawStatus)
	}
	if dashboardStatusCell(got[0]) != "VERIFY-FAILED" {
		t.Fatalf("Status = %q, want VERIFY-FAILED", dashboardStatusCell(got[0]))
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
	// Done inclusion (ADR-0121): DONE sets are hidden uniformly by default, even
	// one that still holds a managed (pop-provisioned) Worktree binding — the old
	// teardown-reminder carve-out is retired. done-integrating carries a managed
	// binding, done-concluded none; both stay hidden unless Done inclusion is on.
	seedBindingStore(t, d.Tasks, map[string]WorktreeBinding{
		setScopedKey("repo-key", "done-integrating"): {RuntimePath: "/repo/done", Branch: "done-branch", Provisioned: true},
	})
	scan := projectScan{Name: "pop", ProjectPath: "/repo/main", RuntimePath: "/repo/main", DefinitionPath: "/def", RepoKey: "repo-key"}

	got, err := dashboardRowsForStatic(d, &config.Config{}, staticForScan(scan, "main", false))
	if err != nil {
		t.Fatal(err)
	}
	var ids []string
	for _, row := range got {
		ids = append(ids, row.SetID)
	}
	want := []string{"ready", "failed", "blocked", "deferred", "missing", "malformed"}
	if !reflect.DeepEqual(ids, want) {
		t.Fatalf("default ids = %v, want %v (both DONE sets hidden)", ids, want)
	}

	// With Done inclusion on, both DONE sets — including the managed-bound one —
	// are revealed, and each still renders as DONE.
	d.IncludeDone = true
	got, err = dashboardRowsForStatic(d, &config.Config{}, staticForScan(scan, "main", false))
	if err != nil {
		t.Fatal(err)
	}
	ids = nil
	byID := map[string]DashboardRow{}
	for _, row := range got {
		ids = append(ids, row.SetID)
		byID[row.SetID] = row
	}
	wantInclude := []string{"ready", "failed", "blocked", "deferred", "missing", "malformed", "done-integrating", "done-concluded"}
	if !reflect.DeepEqual(ids, wantInclude) {
		t.Fatalf("include-done ids = %v, want %v", ids, wantInclude)
	}
	for _, id := range []string{"done-integrating", "done-concluded"} {
		if got := byID[id]; !strings.HasPrefix(dashboardStatusCell(got), "DONE") {
			t.Fatalf("%s row = %+v, want DONE", id, got)
		}
	}
	if !byID["done-integrating"].DoneStillManagedBound {
		t.Fatalf("done-integrating should record DoneStillManagedBound")
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

// TestDashboardTieredSortOrder drives the full Queue surface order (ADR-0121)
// across a mixed fixture that exercises every membership tier and the status
// scheme. Tier precedence is running → auto-drain → orphaned → the rest; the
// orphaned + auto-drain set lands in the auto-drain tier; within the rest tier
// the status scheme floats the IN PROGRESS band, then the READY band (both
// cross-project), then every remaining status per-project by the explicit
// status order; SetID descending is the tiebreak throughout.
func TestDashboardTieredSortOrder(t *testing.T) {
	rows := []DashboardRow{
		// Rest tier, rest band — alphabetically-early project with a needs-you status.
		{Project: "alpha", SetRef: SetRef{SetID: "2026-02-01-blk", RawStatus: tasks.StatusBlocked}},
		// Rest tier, READY band — floats above alpha's BLOCKED even though bravo sorts later.
		{Project: "bravo", SetRef: SetRef{SetID: "2026-02-02-rdy", RawStatus: tasks.StatusReady}},
		{Project: "alpha", SetRef: SetRef{SetID: "2026-02-03-rdy", RawStatus: tasks.StatusReady}},
		// Rest tier, IN PROGRESS band (started READY) — floats above the READY band.
		{Project: "bravo", Started: true, SetRef: SetRef{SetID: "2026-02-04-inp", RawStatus: tasks.StatusReady}},
		// Rest tier, rest band — DONE and AWAITING-APPROVAL, project-first then status order.
		{Project: "bravo", SetRef: SetRef{SetID: "2026-02-05-done", RawStatus: tasks.StatusDone}},
		{Project: "charlie", SetRef: SetRef{SetID: "2026-02-06-aa", RawStatus: tasks.StatusAwaitingApproval}},
		// Orphaned tier.
		{Project: "zoo", SetRef: SetRef{SetID: "2026-04-01-orph", RawStatus: tasks.StatusReady, Orphaned: true}},
		// Auto-drain tier — the orphaned+auto-drain set belongs here, not orphaned.
		{Project: "kilo", SetRef: SetRef{SetID: "2026-05-01-ad", RawStatus: tasks.StatusReady, AutoDrain: true}},
		{Project: "kilo", SetRef: SetRef{SetID: "2026-05-02-ado", RawStatus: tasks.StatusReady, AutoDrain: true, Orphaned: true}},
		// Running tier — highest precedence even over an auto-drain BLOCKED set.
		{Project: "delta", SetRef: SetRef{SetID: "2026-06-01-run", RawStatus: tasks.StatusBlocked, AutoDrain: true, LiveDrain: true}},
	}
	sortDashboardRows(rows)
	got := make([]string, len(rows))
	for i, r := range rows {
		got[i] = r.Project + "/" + r.SetID
	}
	want := []string{
		// Tier 1: running (floats above the whole status scheme, BLOCKED and all).
		"delta/2026-06-01-run",
		// Tier 2: auto-drain, SetID descending.
		"kilo/2026-05-02-ado",
		"kilo/2026-05-01-ad",
		// Tier 3: orphaned.
		"zoo/2026-04-01-orph",
		// Tier 4: the rest, status scheme.
		"bravo/2026-02-04-inp",  // IN PROGRESS band (started READY) floats first
		"alpha/2026-02-03-rdy",  // READY band, cross-project: alpha before bravo
		"bravo/2026-02-02-rdy",  // READY band
		"alpha/2026-02-01-blk",  // rest band, project-first: alpha BLOCKED
		"bravo/2026-02-05-done", // rest band: bravo DONE
		"charlie/2026-02-06-aa", // rest band: charlie AWAITING-APPROVAL
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("order = %v, want %v", got, want)
	}
}

// TestDashboardReadyBandInterleavesProjects proves the READY band reads
// cross-project (ADR-0121): every READY row floats above the rest band
// regardless of project, rather than each project's rows clustering together.
func TestDashboardReadyBandInterleavesProjects(t *testing.T) {
	rows := []DashboardRow{
		{Project: "alpha", SetRef: SetRef{SetID: "2026-01-01-blk", RawStatus: tasks.StatusBlocked}},
		{Project: "bravo", SetRef: SetRef{SetID: "2026-01-02-rdy", RawStatus: tasks.StatusReady}},
		{Project: "alpha", SetRef: SetRef{SetID: "2026-01-03-rdy", RawStatus: tasks.StatusReady}},
		{Project: "bravo", SetRef: SetRef{SetID: "2026-01-04-blk", RawStatus: tasks.StatusBlocked}},
	}
	sortDashboardRows(rows)
	got := make([]string, len(rows))
	for i, r := range rows {
		got[i] = r.Project + "/" + r.SetID
	}
	// READY band (cross-project, Project asc) first, then the rest band. If the
	// order were project-grouped it would read alpha/rdy, alpha/blk, bravo/rdy,
	// bravo/blk instead.
	want := []string{
		"alpha/2026-01-03-rdy",
		"bravo/2026-01-02-rdy",
		"alpha/2026-01-01-blk",
		"bravo/2026-01-04-blk",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("order = %v, want %v", got, want)
	}
}

// TestDashboardBandKeysOnDisplayedLabel proves a row's band is keyed on its
// displayed label, not its raw status (ADR-0121): a started READY set renders
// as IN PROGRESS and sorts in the IN PROGRESS band, floating above a plain
// READY set even though both carry raw status READY and the IN PROGRESS row's
// project sorts later alphabetically.
func TestDashboardBandKeysOnDisplayedLabel(t *testing.T) {
	rows := []DashboardRow{
		{Project: "alpha", SetRef: SetRef{SetID: "2026-01-01-rdy", RawStatus: tasks.StatusReady}},
		{Project: "zeta", Started: true, SetRef: SetRef{SetID: "2026-01-02-inp", RawStatus: tasks.StatusReady}},
	}
	sortDashboardRows(rows)
	got := []string{rows[0].Project + "/" + rows[0].SetID, rows[1].Project + "/" + rows[1].SetID}
	want := []string{"zeta/2026-01-02-inp", "alpha/2026-01-01-rdy"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("order = %v, want %v", got, want)
	}
}

// TestRenderStatusMirrorsDashboardRows proves `pop queue status` renders the
// same rows in the same order as the Work dashboard (ADR-0121): both key on one
// row builder (dashboardRowsForStatic) and one comparator (sortDashboardRows),
// so the static status table's TASK SET order equals the sorted dashboard rows'
// order, under the Summary headline and without any retired inventory section.
func TestRenderStatusMirrorsDashboardRows(t *testing.T) {
	rows := []tasks.Row{
		{ID: "2026-01-01-rdy", Status: tasks.StatusReady},
		{ID: "2026-01-02-blk", Status: tasks.StatusBlocked},
		{ID: "2026-01-03-aa", Status: tasks.StatusAwaitingApproval},
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
	scan := projectScan{Name: "pop", ProjectPath: "/repo/main", RuntimePath: "/repo/main", DefinitionPath: "/def", RepoKey: "repo-key"}

	// The shared row builder produces one repo group's rows; the shared comparator
	// orders the full set exactly as BuildDashboard does. The status table must
	// render these same rows in this same order.
	got, err := dashboardRowsForStatic(d, &config.Config{}, staticForScan(scan, "main", false))
	if err != nil {
		t.Fatal(err)
	}
	sortDashboardRows(got)
	wantOrder := make([]string, len(got))
	for i, r := range got {
		wantOrder[i] = r.SetID
	}
	// The READY band floats above the rest band; the rest band orders
	// AWAITING-APPROVAL before BLOCKED. Guard against a fixture that stopped
	// exercising the comparator.
	if !reflect.DeepEqual(wantOrder, []string{"2026-01-01-rdy", "2026-01-03-aa", "2026-01-02-blk"}) {
		t.Fatalf("shared comparator order = %v, unexpected fixture", wantOrder)
	}

	var out strings.Builder
	RenderStatus(&out, StatusSnapshot{Tasks: d.Tasks}, got)
	text := out.String()

	if !strings.Contains(text, "Summary:") {
		t.Fatalf("status missing Summary headline:\n%s", text)
	}
	for _, header := range []string{"PROJECT", "TASK SET", "STATUS", "WORKTREE"} {
		if !strings.Contains(text, header) {
			t.Fatalf("status table missing %q column header:\n%s", header, text)
		}
	}
	// The status table's TASK SET column must appear in the shared-comparator
	// order — the dashboard's row set/order, rendered statically.
	prev := -1
	for _, id := range wantOrder {
		idx := strings.Index(text, id)
		if idx < 0 {
			t.Fatalf("status table missing set %q:\n%s", id, text)
		}
		if idx <= prev {
			t.Fatalf("status table order breaks dashboard order at %q:\n%s", id, text)
		}
		prev = idx
	}
	for _, omit := range []string{
		"Picked-up sets:",
		"Active worktrees:",
		"Queued ready sets:",
		"Blocked:",
		"Awaiting approval:",
		"Skipped repositories:",
	} {
		if strings.Contains(text, omit) {
			t.Fatalf("status output should not contain retired section %q:\n%s", omit, text)
		}
	}
}

// TestRenderStatusTableColumnsAndIndicator pins the static status table's
// column composition (ADR-0121): the STATUS cell recomposes IN PROGRESS from a
// live-drained READY set, the trailing ● live-drain indicator survives, and the
// WORKTREE destination cells render as plain (ANSI-free) labels.
func TestRenderStatusTableColumnsAndIndicator(t *testing.T) {
	td := queueDataDeps(t)
	rows := []DashboardRow{
		{Project: "alpha", Started: true, SetRef: SetRef{SetID: "2026-03-01-inp", RawStatus: tasks.StatusReady, LiveDrain: true}, destKind: dashboardDestManagedDirective},
		{Project: "bravo", SetRef: SetRef{SetID: "2026-03-02-blk", RawStatus: tasks.StatusBlocked}, destKind: dashboardDestNeedsBind},
	}
	sortDashboardRows(rows)

	var out strings.Builder
	RenderStatus(&out, StatusSnapshot{Tasks: td}, rows)
	text := out.String()

	for _, want := range []string{
		"IN PROGRESS",           // live-drained READY → IN PROGRESS
		dashboardLiveDrainGlyph, // trailing live-drain indicator
		dashboardDestLabelManagedWt,
		dashboardDestLabelNeedsBind,
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("status table missing %q:\n%s", want, text)
		}
	}
	// Live-drain (running tier) floats above the rest-band BLOCKED set.
	if strings.Index(text, "2026-03-01-inp") >= strings.Index(text, "2026-03-02-blk") {
		t.Fatalf("running tier should float above BLOCKED:\n%s", text)
	}
	if strings.Contains(text, "\x1b[") {
		t.Fatalf("status table must be plain text (no ANSI):\n%q", text)
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
	seedBindingStore(t, d.Tasks, map[string]WorktreeBinding{
		setScopedKey("repo-key", "done"):  {RuntimePath: "/repo/done", Branch: "done-branch", Provisioned: true},
		setScopedKey("repo-key", "bound"): {RuntimePath: "/repo/bound", Branch: "bound-branch"},
	})
	scan := projectScan{Name: "pop", ProjectPath: "/repo/main", RuntimePath: "/repo/main", DefinitionPath: "/def", RepoKey: "repo-key"}

	// Reveal the DONE row so its column derivation stays under test (ADR-0121
	// hides DONE by default).
	d.IncludeDone = true
	got, err := dashboardRowsForStatic(d, &config.Config{}, staticForScan(scan, "main", false))
	if err != nil {
		t.Fatal(err)
	}
	byID := map[string]DashboardRow{}
	for _, row := range got {
		byID[row.SetID] = row
	}
	if !strings.HasPrefix(dashboardStatusCell(byID["done"]), "DONE") || byID["done"].Worktree != "done-branch" || byID["done"].destKind != dashboardDestDoneManagedBound {
		t.Fatalf("done row = %+v", byID["done"])
	}
	if !strings.HasPrefix(dashboardStatusCell(byID["ready"]), "READY") || byID["ready"].Worktree != dashboardDestLabelNeedsBind || byID["ready"].destKind != dashboardDestNeedsBind {
		t.Fatalf("ready row = %+v", byID["ready"])
	}
	if byID["bound"].Worktree != "bound-branch" || byID["bound"].destKind != dashboardDestBound {
		t.Fatalf("bound row = %+v", byID["bound"])
	}
}

func TestDashboardNoBaseWorktree(t *testing.T) {
	d := dashboardTestDeps(t, []tasks.Row{{ID: "missing", Status: tasks.StatusMissing}}, nil)
	scan := projectScan{Name: "bare", ProjectPath: "/repo/bare.git", RuntimePath: "/repo/bare.git", DefinitionPath: "/def", RepoKey: "bare-key"}

	got, err := dashboardRowsForStatic(d, &config.Config{}, staticForScan(scan, "", true))
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

	got, err := dashboardRowsForStatic(d, &config.Config{}, staticForScan(scan, "main", false))
	if err != nil {
		t.Fatal(err)
	}
	byID := map[string]DashboardRow{}
	for _, row := range got {
		byID[row.SetID] = row
	}
	if !byID["ready"].LiveDrain {
		t.Fatalf("ready LiveDrain = false, want true (held by a live drain)")
	}
	if byID["other"].LiveDrain {
		t.Fatalf("other LiveDrain = true, want false")
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

	got, err := dashboardRowsForStatic(d, &config.Config{}, staticForScan(scan, "main", false))
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
	// The orphaned set must render its status suffix; the present/unbound sets must not.
	var rendered strings.Builder
	renderDashboardTable(&rendered, []DashboardRow{byID["missing"]}, 0, 120, 20)
	if !strings.Contains(rendered.String(), "· orphaned") {
		t.Fatalf("orphaned suffix missing from row render:\n%s", rendered.String())
	}
}

func TestDashboardAutoDrainBadgeAndToggle(t *testing.T) {
	rows := []DashboardRow{
		{Project: "pop", Worktree: "/repo/main (main)", SetRef: SetRef{RawStatus: tasks.StatusReady, SetID: "marked", AutoDrain: true}},
		{Project: "pop", Worktree: "/repo/main (main)", SetRef: SetRef{RawStatus: tasks.StatusReady, SetID: "plain"}},
	}
	var rendered strings.Builder
	renderDashboardTable(&rendered, rows, 0, 0, 20)
	if !strings.Contains(rendered.String(), "AD") {
		t.Fatalf("missing auto-drain flag:\n%s", rendered.String())
	}

	var toggledDef, toggledState, toggledSet string
	d := &Deps{
		ToggleAutoDrain: func(defPath, statePath, setID string) (*tasks.AutoDrainResult, error) {
			toggledDef, toggledState, toggledSet = defPath, statePath, setID
			return &tasks.AutoDrainResult{TaskSetID: setID, AutoDrain: true}, nil
		},
	}
	m := newQueueDashboard(d, &config.Config{}, DashboardSnapshot{Rows: []DashboardRow{{Project: "pop", Worktree: "/repo/main (main)", cursorKey: "pop\x00plain", SetRef: SetRef{RawStatus: tasks.StatusReady, SetID: "plain", DefPath: "/repo/tasks", StatePath: "/repo/state.json"}}}})
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

// TestDashboardAutoDrainToggleReflectsInRowAndCount proves the render-time STATUS
// composition (ADR-0108): a simulated auto-drain toggle updates the per-row `·
// auto-drain` marker and the header's auto-drain count together on the very next
// View pass — no dashboardTickMsg / dashboardRowsMsg poll is fed between the
// toggle and the render. Before the toggle neither the row cell nor the summary
// mentions auto-drain; after it, both do.
func TestDashboardAutoDrainToggleReflectsInRowAndCount(t *testing.T) {
	d := &Deps{
		ToggleAutoDrain: func(defPath, statePath, setID string) (*tasks.AutoDrainResult, error) {
			return &tasks.AutoDrainResult{TaskSetID: setID, AutoDrain: true}, nil
		},
	}
	m := newQueueDashboard(d, &config.Config{}, DashboardSnapshot{Rows: []DashboardRow{
		{Project: "pop", Worktree: "main", cursorKey: "pop\x00plain", SetRef: SetRef{RawStatus: tasks.StatusReady, SetID: "plain", DefPath: "/repo/tasks", StatePath: "/repo/state.json"}},
	}})
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 20})
	m = updated.(QueueDashboard)

	// Baseline: the READY row carries no auto-drain marker and the summary carries
	// no auto-drain count.
	before := m.View().Content
	if strings.Contains(before, "auto-drain") {
		t.Fatalf("baseline view should not mention auto-drain:\n%s", before)
	}

	// Toggle auto-drain on via the action menu (`a` opens, `a` dispatches).
	updated, _ = m.Update(tea.KeyPressMsg{Code: 'a', Text: "a"})
	m = updated.(QueueDashboard)
	updated, _ = m.Update(tea.KeyPressMsg{Code: 'a', Text: "a"})
	m = updated.(QueueDashboard)

	// The same View pass — no poll tick — must reflect the toggle in both places.
	after := m.View().Content
	// The base label is bucket-colored; the auto-drain marker follows it plainly.
	if !strings.Contains(after, "\x1b[34mREADY\x1b[m · auto-drain") {
		t.Fatalf("row cell missing auto-drain marker after toggle:\n%s", after)
	}
	if !strings.Contains(after, "1 auto-drain") {
		t.Fatalf("summary count missing auto-drain after toggle:\n%s", after)
	}
}

// TestDashboardAutoDrainWaitingMarkerAndCount pins the ADR-0108 auto-drain
// display rules: the `· auto-drain` marker and the header tally both key on the
// same waiting predicate (consented AND not Picked-up). A consented+idle set
// shows the marker and is counted; a consented set held by a live drain
// (LiveDrain) hides the marker and drops out of the count while its persisted
// AutoDrain bit stays true; a non-consented set has neither.
func TestDashboardAutoDrainWaitingMarkerAndCount(t *testing.T) {
	idle := DashboardRow{SetRef: SetRef{RawStatus: tasks.StatusReady, AutoDrain: true}}
	pickedUp := DashboardRow{SetRef: SetRef{RawStatus: tasks.StatusReady, AutoDrain: true, LiveDrain: true}}
	plain := DashboardRow{SetRef: SetRef{RawStatus: tasks.StatusReady}}

	// Per-row marker.
	if got := dashboardStatusCell(idle); !strings.Contains(got, "· auto-drain") {
		t.Errorf("consented+idle marker: got %q, want auto-drain suffix", got)
	}
	if got := dashboardStatusCell(pickedUp); strings.Contains(got, "auto-drain") {
		t.Errorf("consented+picked-up marker: got %q, want no auto-drain suffix", got)
	}
	if got := dashboardStatusCell(plain); strings.Contains(got, "auto-drain") {
		t.Errorf("not-consented marker: got %q, want no auto-drain suffix", got)
	}

	// Silencing is display-only: the persisted consent bit is untouched.
	if !pickedUp.AutoDrain {
		t.Error("Picked-up silencing mutated the persisted AutoDrain bit")
	}

	// Header count — waiting-only. idle counts, pickedUp does not, plain does not.
	summary := dashboardSummary([]DashboardRow{idle, pickedUp, plain})
	if !strings.Contains(summary, "1 auto-drain") {
		t.Errorf("summary count: got %q, want exactly 1 auto-drain (waiting-only)", summary)
	}

	// Marker and count agree: both driven by the shared predicate.
	if dashboardAutoDrainWaiting(idle) != strings.Contains(dashboardStatusCell(idle), "· auto-drain") {
		t.Error("idle: predicate and marker disagree")
	}
	if dashboardAutoDrainWaiting(pickedUp) != strings.Contains(dashboardStatusCell(pickedUp), "· auto-drain") {
		t.Error("picked-up: predicate and marker disagree")
	}
}

func TestDashboardBKeyOpensBindModal(t *testing.T) {
	m := newQueueDashboard(&Deps{}, &config.Config{}, DashboardSnapshot{Rows: []DashboardRow{{Project: "pop", Worktree: "/repo/main (main)", cursorKey: "pop\x00set-bind", SetRef: SetRef{RawStatus: tasks.StatusReady, SetID: "set-bind", DefPath: "/repo/tasks", StatePath: "/repo/state.json"}}}})
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
		{Project: "pop", cursorKey: "pop\x00set", SetRef: SetRef{RawStatus: tasks.StatusReady, SetID: "set", RuntimePath: "/repo/wt"}},
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
		m := newQueueDashboard(&Deps{}, &config.Config{}, DashboardSnapshot{Rows: []DashboardRow{{Project: "pop", Worktree: "/repo/wt (main)", cursorKey: "pop\x00set", SetRef: SetRef{RawStatus: tasks.StatusReady, SetID: "set", DefPath: "/repo/tasks", StatePath: "/repo/state.json", RuntimePath: "/repo/wt", Bound: true, Parked: true}}}})
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
		return newQueueDashboard(&Deps{}, &config.Config{}, DashboardSnapshot{Rows: []DashboardRow{{Project: "pop", Worktree: "/repo/wt (main)", cursorKey: "pop\x00set", SetRef: SetRef{RawStatus: tasks.StatusReady, SetID: "set", DefPath: "/repo/tasks", StatePath: "/repo/state.json", RuntimePath: "/repo/wt", Bound: true}}}})
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
	m := newQueueDashboard(d, &config.Config{}, DashboardSnapshot{Rows: []DashboardRow{{Project: "pop", Worktree: "/repo/wt (main)", cursorKey: "pop\x00set", SetRef: SetRef{RawStatus: tasks.StatusDone, SetID: "set", DefPath: "/repo/tasks", StatePath: "/repo/state.json", RuntimePath: "/repo/wt", Bound: true}}}})

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
	m := newQueueDashboard(d, &config.Config{}, DashboardSnapshot{Rows: []DashboardRow{{Project: "proj", cursorKey: "proj\x00set-1", SetRef: SetRef{RawStatus: tasks.StatusDone, SetID: "set-1", DefPath: tasksDir, StatePath: statePath, RuntimePath: "/repo/wt", Bound: true}}}})

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

	// dashboardMenuPlaceBelowTwoLine: each logical row consumes two physical
	// lines, so the available space below the cursor is halved.
	if !dashboardMenuPlaceBelowTwoLine(0, 6, 24) {
		t.Fatal("two-line menu should render below a top-of-list cursor")
	}
	if dashboardMenuPlaceBelowTwoLine(8, 6, 24) {
		t.Fatal("two-line menu should flip above a low cursor (each row is two lines)")
	}
	if !dashboardMenuPlaceBelowTwoLine(5, 6, 0) {
		t.Fatal("two-line menu should default below when height is unknown")
	}

	rows := make([]DashboardRow, 20)
	for i := range rows {
		id := fmt.Sprintf("set-%02d", i)
		rows[i] = DashboardRow{Project: "pop", cursorKey: "pop\x00" + id, SetRef: SetRef{RawStatus: tasks.StatusReady, SetID: id}}
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
		{Project: "pop", cursorKey: "pop\x00first", SetRef: SetRef{RawStatus: tasks.StatusReady, SetID: "first"}},
		{Project: "pop", cursorKey: "pop\x00second", SetRef: SetRef{RawStatus: tasks.StatusReady, SetID: "second"}},
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
		{Project: "pop", cursorKey: "pop\x00alpha", SetRef: SetRef{RawStatus: tasks.StatusReady, SetID: "alpha"}},
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
				{Project: "pop", cursorKey: "pop\x00target", SetRef: SetRef{RawStatus: tasks.StatusReady, SetID: "target"}},
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

func TestDashboardCtrlGOpensBoundCheckout(t *testing.T) {
	// Bound row: Ctrl-g surfaces its checkout path and quits so the command layer
	// can run the workbench-aware open after the TUI exits (task 02).
	m := newQueueDashboard(&Deps{}, &config.Config{}, DashboardSnapshot{Rows: []DashboardRow{
		{Project: "pop", cursorKey: "pop\x00set", SetRef: SetRef{RawStatus: tasks.StatusReady, SetID: "set", RuntimePath: "/repo/wt", Bound: true}},
	}})

	updated, cmd := m.Update(tea.KeyPressMsg{Code: 'g', Mod: tea.ModCtrl})
	got := updated.(QueueDashboard)
	if got.openCheckout != "/repo/wt" {
		t.Fatalf("openCheckout = %q, want /repo/wt", got.openCheckout)
	}
	if cmd == nil {
		t.Fatal("Ctrl-g on bound row: expected a quit command")
	}
	if _, ok := cmd().(tea.QuitMsg); !ok {
		t.Fatalf("Ctrl-g command = %T, want tea.QuitMsg", cmd())
	}
	if got.statusMsg != "" {
		t.Fatalf("statusMsg = %q, want empty on bound open", got.statusMsg)
	}
}

func TestDashboardCtrlGUnboundRowShowsStatusAndDoesNotQuit(t *testing.T) {
	// Unbound row (no RuntimePath): Ctrl-g shows an inline status message and keeps
	// the dashboard running — no quit, no surfaced checkout (task 02).
	m := newQueueDashboard(&Deps{}, &config.Config{}, DashboardSnapshot{Rows: []DashboardRow{
		{Project: "pop", cursorKey: "pop\x00unbound", SetRef: SetRef{RawStatus: tasks.StatusReady, SetID: "unbound"}},
	}})

	updated, cmd := m.Update(tea.KeyPressMsg{Code: 'g', Mod: tea.ModCtrl})
	got := updated.(QueueDashboard)
	if cmd != nil {
		t.Fatalf("Ctrl-g on unbound row should not quit, got cmd %T", cmd())
	}
	if got.openCheckout != "" {
		t.Fatalf("openCheckout = %q, want empty on unbound row", got.openCheckout)
	}
	if got.statusMsg != "no checkout bound to this task set" {
		t.Fatalf("statusMsg = %q, want 'no checkout bound to this task set'", got.statusMsg)
	}
}

func TestDashboardViewUsesTaskTableHeaderAndBottomShortcutLegend(t *testing.T) {
	m := newQueueDashboard(&Deps{}, &config.Config{}, DashboardSnapshot{Rows: []DashboardRow{
		{Project: "pop", Worktree: "main", cursorKey: "pop\x00set", SetRef: SetRef{SetID: "set", RawStatus: tasks.StatusReady, AutoDrain: true, LiveDrain: true}},
		{Project: "pop", Worktree: "main", cursorKey: "pop\x00done", SetRef: SetRef{SetID: "done", RawStatus: tasks.StatusDone}},
	}})
	m.width = 120
	m.height = 8

	view := m.View().Content
	if strings.Contains(view, "Queue dashboard") {
		t.Fatalf("task-set list should use summary instead of dashboard title:\n%s", view)
	}
	// The auto-drain set here is Picked-up, so per ADR-0108 it drops out of the
	// waiting-only auto-drain tally (the live-drain indicator already signals it).
	if !strings.Contains(view, "Queue · 2 task sets · 1 ready · 1 running") {
		t.Fatalf("task-set list should render useful summary:\n%s", view)
	}
	if strings.Contains(view, "auto-drain") {
		t.Fatalf("Picked-up auto-drain set should not surface an auto-drain marker/count:\n%s", view)
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
		rows[i] = DashboardRow{Project: "pop", cursorKey: "pop\x00" + id, SetRef: SetRef{RawStatus: tasks.StatusReady, SetID: id}}
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

func TestDashboardTableFitsTerminalWidth(t *testing.T) {
	// Wide row content on a narrow pane must not spill horizontally. Auto-drain
	// and orphaned suffixes ride inside STATUS now that FLAGS is gone, so a
	// pane tight enough to shrink STATUS may truncate them — this test only
	// asserts the table never spills past termW; suffix visibility at
	// generous widths is covered by TestDashboardStatusSuffixesRender.
	row := DashboardRow{
		Project:       "very-long-project-name-here",
		VerifiedAtSHA: "abcdef123456",
		Worktree:      "feature/super-long-branch-name-for-testing",
		cursorKey:     "pop\x00set1",
		SetRef: SetRef{
			SetID:       "set1",
			RawStatus:   tasks.StatusAwaitingApproval,
			AutoDrain:   true,
			Orphaned:    true,
			ConfigError: "no trunk worktree configured",
		},
	}
	for _, termW := range []int{40, 60, 80} {
		t.Run(fmt.Sprintf("width=%d", termW), func(t *testing.T) {
			m := newQueueDashboard(&Deps{}, &config.Config{}, DashboardSnapshot{Rows: []DashboardRow{row}})
			updated, _ := m.Update(tea.WindowSizeMsg{Width: termW, Height: 20})
			m = updated.(QueueDashboard)
			view := m.View().Content
			for _, line := range dashboardTestTableLines(view) {
				if got := lipgloss.Width(line); got > termW {
					t.Fatalf("table line width %d exceeds terminal width %d:\n%q", got, termW, line)
				}
			}
		})
	}
}

func TestDashboardFitColumnWidths(t *testing.T) {
	natural := []int{20, 30, 40, 25, 1} // PROJECT, TASK SET, STATUS, WORKTREE, indicator
	fitted := dashboardFitColumnWidths(natural, 50)
	if dashboardTableLineWidth(fitted) > 50 {
		t.Fatalf("fitted line width %d exceeds budget 50: %v", dashboardTableLineWidth(fitted), fitted)
	}
}

func dashboardTestTableLines(view string) []string {
	var lines []string
	inTable := false
	for _, line := range strings.Split(view, "\n") {
		singleHeader := strings.Contains(line, "PROJECT") && strings.Contains(line, "STATUS")
		twoLineHeader := strings.Contains(line, "TASK SET") && strings.Contains(line, "WORKTREE")
		if singleHeader || twoLineHeader {
			inTable = true
		}
		if !inTable {
			continue
		}
		if strings.Contains(line, "j/k move") || strings.Contains(line, "h/esc quit") {
			break
		}
		lines = append(lines, line)
	}
	return lines
}

func TestDashboardTwoLineMode(t *testing.T) {
	short := DashboardRow{SetRef: SetRef{SetID: "short-id"}}
	long := DashboardRow{SetRef: SetRef{SetID: strings.Repeat("a", 37)}}

	// roomy is a pane height at/above the floor; two-line decisions apply.
	const roomy = 20

	if !dashboardTwoLineMode([]DashboardRow{short}, 40, roomy) {
		t.Fatalf("narrow terminal (40 cols) should activate two-line mode")
	}
	if !dashboardTwoLineMode([]DashboardRow{short}, 119, roomy) {
		t.Fatalf("terminal just below threshold (119 cols) should activate two-line mode")
	}
	if dashboardTwoLineMode([]DashboardRow{short}, 120, roomy) {
		t.Fatalf("terminal at threshold (120 cols) with short ids should stay single-line")
	}
	if !dashboardTwoLineMode([]DashboardRow{short, long}, 120, roomy) {
		t.Fatalf("one long set id should activate two-line mode for all rows")
	}
	if !dashboardTwoLineMode([]DashboardRow{long, short}, 140, roomy) {
		t.Fatalf("long set id should activate two-line mode even on wide terminals")
	}
}

func TestDashboardTwoLineModeHeightGate(t *testing.T) {
	short := DashboardRow{SetRef: SetRef{SetID: "short-id"}}
	long := DashboardRow{SetRef: SetRef{SetID: strings.Repeat("a", 37)}}

	// Below the height floor, neither a narrow terminal nor a long set id may
	// activate two-line mode: a short popup stays single-line for row density.
	const short_pane = dashboardTwoLineHeightFloor - 1
	if dashboardTwoLineMode([]DashboardRow{short}, 40, short_pane) {
		t.Fatalf("narrow terminal below height floor should stay single-line")
	}
	if dashboardTwoLineMode([]DashboardRow{long, short}, 200, short_pane) {
		t.Fatalf("long set id below height floor should stay single-line")
	}

	// Exactly at the floor the pane is roomy again.
	if !dashboardTwoLineMode([]DashboardRow{short}, 40, dashboardTwoLineHeightFloor) {
		t.Fatalf("narrow terminal at the height floor should activate two-line mode")
	}
}

func TestDashboardTwoLineRowLine1ShowsIndicatorProjectSetIDWorktree(t *testing.T) {
	row := DashboardRow{
		Project:   "pop",
		Worktree:  "main",
		SetRef:    SetRef{SetID: "2026-07-05-queue-dashboard-two-line", RawStatus: tasks.StatusReady, LiveDrain: true},
		cursorKey: "pop\x00set",
	}
	widths := dashboardTwoLineFitWidths(dashboardTwoLineNaturalWidths([]DashboardRow{row}), 120)
	line1 := dashboardTwoLineRowLine1(row, widths)

	// Line 1 carries PROJECT, TASK SET, WORKTREE and the trailing live-drain
	// indicator; STATUS lives on line 2. A live drain lights the ● (ADR-0111).
	for _, want := range []string{dashboardLiveDrainGlyph, "pop", row.SetID, "main"} {
		if !strings.Contains(line1, want) {
			t.Fatalf("two-line row line 1 missing expected value %q: %q", want, line1)
		}
	}
	if strings.Contains(line1, "picked up") {
		t.Fatalf("two-line row line 1 must not carry the retired DRAIN string: %q", line1)
	}
	if strings.Contains(line1, "READY") {
		t.Fatalf("two-line row line 1 must not contain the status: %q", line1)
	}
}

func TestDashboardTwoLineRowLine2ShowsStatusUnderTaskSet(t *testing.T) {
	row := DashboardRow{
		Project: "pop",
		SetRef:  SetRef{SetID: "2026-07-05-queue-dashboard-two-line", RawStatus: tasks.StatusReady},
		Started: true,
	}
	widths := dashboardTwoLineFitWidths(dashboardTwoLineNaturalWidths([]DashboardRow{row}), 120)
	line2 := dashboardTwoLineRowLine2(row, widths)

	// The base label is bucket-colored (started READY → blue "IN PROGRESS").
	wantStatus := dashboardStatusCellStyled(row)
	if strings.TrimLeft(line2, " ") != wantStatus {
		t.Fatalf("two-line row line 2 = %q, want the status %q (indented)", line2, wantStatus)
	}
	// STATUS must be indented to start under the TASK SET column, i.e. past the
	// PROJECT column and its separator.
	wantIndent := dashboardTwoLineStatusIndent(widths)
	if got := len(line2) - len(strings.TrimLeft(line2, " ")); got != wantIndent {
		t.Fatalf("two-line row line 2 indent = %d, want %d (under TASK SET): %q", got, wantIndent, line2)
	}
	if strings.Contains(line2, row.SetID) {
		t.Fatalf("two-line row line 2 must not contain the set id: %q", line2)
	}
}

func TestDashboardTwoLineRowsFitTerminalWidth(t *testing.T) {
	cases := []struct {
		termW int
		setID string
	}{
		// Narrow widths activate two-line mode regardless of set id length.
		{40, "set"},
		{60, "set"},
		// At the width threshold, a long set id is required to keep two-line
		// mode active; pick one that still fits within the 80-column budget.
		{80, "2026-07-05-queue-dashboard-two-line-mode"},
	}
	for _, tc := range cases {
		t.Run(fmt.Sprintf("width=%d", tc.termW), func(t *testing.T) {
			row := DashboardRow{
				Project:   "pop",
				Worktree:  "main",
				SetRef:    SetRef{SetID: tc.setID, RawStatus: tasks.StatusReady},
				cursorKey: "pop\x00" + tc.setID,
			}
			m := newQueueDashboard(&Deps{}, &config.Config{}, DashboardSnapshot{Rows: []DashboardRow{row}})
			updated, _ := m.Update(tea.WindowSizeMsg{Width: tc.termW, Height: 20})
			m = updated.(QueueDashboard)
			if !dashboardTwoLineMode(m.snap.Rows, m.width, m.height) {
				t.Fatalf("expected two-line mode at width %d", tc.termW)
			}
			view := m.View().Content
			for _, line := range dashboardTestTableLines(view) {
				if got := lipgloss.Width(line); got > tc.termW {
					t.Fatalf("table line width %d exceeds terminal width %d:\n%q", got, tc.termW, line)
				}
			}
		})
	}
}

func TestDashboardTwoLineSingleLineLayoutUnchanged(t *testing.T) {
	row := DashboardRow{
		Project:   "pop",
		Worktree:  "main",
		SetRef:    SetRef{SetID: "set1", RawStatus: tasks.StatusReady},
		cursorKey: "pop\x00set1",
	}
	m := newQueueDashboard(&Deps{}, &config.Config{}, DashboardSnapshot{Rows: []DashboardRow{row}})
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 20})
	m = updated.(QueueDashboard)
	if dashboardTwoLineMode(m.snap.Rows, m.width, m.height) {
		t.Fatalf("wide terminal with short ids should not activate two-line mode")
	}
	view := m.View().Content
	for _, want := range []string{"PROJECT  TASK SET  STATUS", "pop      set1"} {
		if !strings.Contains(view, want) {
			t.Fatalf("single-line layout missing %q:\n%s", want, view)
		}
	}
}

// TestDashboardLiveIndicator pins the trailing live-drain indicator (ADR-0111): a
// PID-alive drain lights a single ● in the house working colour, an idle row
// shows a blank cell, and the plain (width-measurement) form carries no ANSI.
func TestDashboardLiveIndicator(t *testing.T) {
	live := DashboardRow{SetRef: SetRef{RawStatus: tasks.StatusReady, LiveDrain: true}}
	idle := DashboardRow{SetRef: SetRef{RawStatus: tasks.StatusReady}}

	if got := dashboardLiveIndicator(idle, true); got != "" {
		t.Fatalf("idle indicator = %q, want blank", got)
	}
	if got := dashboardLiveIndicator(idle, false); got != "" {
		t.Fatalf("idle indicator (plain) = %q, want blank", got)
	}

	plain := dashboardLiveIndicator(live, false)
	if plain != dashboardLiveDrainGlyph {
		t.Fatalf("live indicator (plain) = %q, want %q", plain, dashboardLiveDrainGlyph)
	}
	styled := dashboardLiveIndicator(live, true)
	if !strings.Contains(styled, dashboardLiveDrainGlyph) {
		t.Fatalf("live indicator (styled) = %q, want it to contain %q", styled, dashboardLiveDrainGlyph)
	}
	if styled == plain {
		t.Fatalf("styled indicator = %q, want ANSI styling distinct from the plain glyph", styled)
	}
	if w := lipgloss.Width(styled); w != 1 {
		t.Fatalf("styled indicator width = %d, want 1 (one-character cell)", w)
	}
}

// TestDashboardLiveIndicatorAcrossStatuses confirms the indicator is driven by
// LiveDrain regardless of STATUS (ADR-0111): an AWAITING-APPROVAL row with a live
// paused agent carries the ● and keeps its own status label unrefined.
func TestDashboardLiveIndicatorAcrossStatuses(t *testing.T) {
	for _, status := range []tasks.TaskSetStatus{
		tasks.StatusReady, tasks.StatusAwaitingApproval, tasks.StatusNeedsVerify, tasks.StatusBlocked,
	} {
		row := DashboardRow{SetRef: SetRef{RawStatus: status, LiveDrain: true}}
		if got := dashboardLiveIndicator(row, false); got != dashboardLiveDrainGlyph {
			t.Fatalf("status %s live indicator = %q, want %q", status, got, dashboardLiveDrainGlyph)
		}
		// The indicator never rewrites the status label (only READY refines).
		if status != tasks.StatusReady {
			if got := dashboardStatusLabel(row); got != string(status) {
				t.Fatalf("status %s label = %q, want unchanged (indicator does not refine)", status, got)
			}
		}
	}
}

// TestDashboardSingleLineDropsDrainColumnKeepsIndicator pins the retired DRAIN
// column and the trailing indicator on the single-line layout (ADR-0111): the
// header carries no DRAIN, the column order is PROJECT/TASK SET/STATUS/WORKTREE/
// indicator, and a live row carries the ● glyph.
func TestDashboardSingleLineDropsDrainColumnKeepsIndicator(t *testing.T) {
	rows := []DashboardRow{
		{Project: "pop", Worktree: "main", cursorKey: "pop\x00live", SetRef: SetRef{SetID: "live", RawStatus: tasks.StatusReady, LiveDrain: true}},
		{Project: "pop", Worktree: "main", cursorKey: "pop\x00idle", SetRef: SetRef{SetID: "idle", RawStatus: tasks.StatusDone}},
	}
	m := newQueueDashboard(&Deps{}, &config.Config{}, DashboardSnapshot{Rows: rows})
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 20})
	m = updated.(QueueDashboard)
	if dashboardTwoLineMode(m.snap.Rows, m.width, m.height) {
		t.Fatalf("wide terminal with short ids should stay single-line")
	}
	view := m.View().Content
	if strings.Contains(view, "DRAIN") {
		t.Fatalf("single-line view must not carry the retired DRAIN column:\n%s", view)
	}
	// Column order: PROJECT, TASK SET, STATUS, WORKTREE — left to right, then the
	// blank-header indicator column trails (no DRAIN anywhere).
	header := strings.Split(view, "\n")[dashboardTestLineIndex(strings.Split(view, "\n"), "PROJECT")]
	iProject := strings.Index(header, "PROJECT")
	iSet := strings.Index(header, "TASK SET")
	iStatus := strings.Index(header, "STATUS")
	iWorktree := strings.Index(header, "WORKTREE")
	if !(iProject >= 0 && iProject < iSet && iSet < iStatus && iStatus < iWorktree) {
		t.Fatalf("single-line header column order wrong: %q", header)
	}
	// The live row carries the ● glyph; the done row does not.
	lines := strings.Split(view, "\n")
	liveIdx := dashboardTestLineIndex(lines, "live")
	if liveIdx < 0 {
		t.Fatalf("live row missing from view:\n%s", view)
	}
	if !strings.Contains(lines[liveIdx], dashboardLiveDrainGlyph) {
		t.Fatalf("live row missing ● indicator: %q", lines[liveIdx])
	}
	doneIdx := dashboardTestLineIndex(lines, "idle")
	if doneIdx < 0 {
		t.Fatalf("idle row missing from view:\n%s", view)
	}
	if strings.Contains(lines[doneIdx], dashboardLiveDrainGlyph) {
		t.Fatalf("idle row must not show the ● indicator: %q", lines[doneIdx])
	}
}

// TestDashboardNarrowPaneKeepsIndicator confirms the fixed-width indicator is
// never dropped by elastic width fitting even when the pane is very narrow
// (ADR-0111): the ● still appears in a live row's rendered cells.
func TestDashboardNarrowPaneKeepsIndicator(t *testing.T) {
	rows := []DashboardRow{
		{Project: "a-really-long-project-name", Worktree: "some-long-branch", cursorKey: "a\x00live",
			SetRef: SetRef{SetID: "live", RawStatus: tasks.StatusReady, LiveDrain: true}},
	}
	natural := dashboardColumnWidths(rows)
	fitted := dashboardFitColumnWidths(natural, 20)
	if fitted[dashboardColIndicator] < 1 {
		t.Fatalf("indicator column width = %d, want >= 1 (never dropped)", fitted[dashboardColIndicator])
	}
	line := dashboardTableLine(dashboardRowValues(rows[0]), fitted)
	if !strings.Contains(line, dashboardLiveDrainGlyph) {
		t.Fatalf("narrow-pane live row missing ● indicator: %q", line)
	}
}

func TestDashboardDetailViewOmitsTitleAndUsesBottomShortcutLegend(t *testing.T) {
	manifest := &tasks.Manifest{
		Valid: true,
		Tasks: []tasks.Task{{ID: "01-a", File: "01-a.md", Title: "First", Type: "AFK", Status: "open"}},
	}
	m := newQueueDashboard(&Deps{}, &config.Config{}, DashboardSnapshot{Rows: []DashboardRow{
		{Project: "pop", cursorKey: "pop\x00set-normal", SetRef: SetRef{RawStatus: tasks.StatusReady, SetID: "set-normal"}},
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
		{Project: "pop", cursorKey: "pop\x00set-long", SetRef: SetRef{RawStatus: tasks.StatusReady, SetID: "set-long"}},
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

// TestDashboardStatusAppendsVerifiedAtSHA confirms the main table STATUS column
// appends a yellow `verified @ <shortSHA>` suffix when the row carries one.
func TestDashboardStatusAppendsVerifiedAtSHA(t *testing.T) {
	row := DashboardRow{VerifiedAtSHA: "abcdef1234567890", SetRef: SetRef{RawStatus: tasks.StatusAwaitingApproval}}
	got := dashboardStatusCellStyled(row)
	if !strings.Contains(got, "AWAITING-APPROVAL") {
		t.Fatalf("status label missing: %q", got)
	}
	if !strings.Contains(got, "verified @ abcdef123456") {
		t.Fatalf("verified suffix missing: %q", got)
	}
	// The suffix should carry ANSI yellow (same as pop tasks status Details).
	if !strings.Contains(got, "\x1b[33m") {
		t.Fatalf("verified suffix should be yellow: %q", got)
	}

	// No suffix when VerifiedAtSHA is empty.
	plain := DashboardRow{SetRef: SetRef{RawStatus: tasks.StatusAwaitingApproval}}
	if got := dashboardStatusCellStyled(plain); strings.Contains(got, "verified @") {
		t.Fatalf("plain status should not contain suffix: %q", got)
	}
}

// TestDashboardStatusBucketColors confirms the base status label is colored by
// semantic bucket, and only the base token — the label carries the bucket ANSI
// while width measurement (dashboardStatusCell) stays plain.
func TestDashboardStatusBucketColors(t *testing.T) {
	cases := []struct {
		name    string
		status  tasks.TaskSetStatus
		started bool
		ansi    string // expected bucket color prefixing the base label
	}{
		{"DONE green", tasks.StatusDone, false, "\x1b[32m"},
		{"READY blue", tasks.StatusReady, false, "\x1b[34m"},
		{"IN PROGRESS blue", tasks.StatusReady, true, "\x1b[34m"},
		{"NEEDS-VERIFY yellow", tasks.StatusNeedsVerify, false, "\x1b[33m"},
		{"AWAITING-APPROVAL yellow", tasks.StatusAwaitingApproval, false, "\x1b[33m"},
		{"BLOCKED yellow", tasks.StatusBlocked, false, "\x1b[33m"},
		{"FAILED red", tasks.StatusFailed, false, "\x1b[31m"},
		{"VERIFY-FAILED red", tasks.StatusVerifyFailed, false, "\x1b[31m"},
		{"MALFORMED red", tasks.StatusMalformed, false, "\x1b[31m"},
		{"MISSING red", tasks.StatusMissing, false, "\x1b[31m"},
		{"DEFERRED faint", tasks.StatusDeferred, false, "\x1b[2m"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			row := DashboardRow{SetRef: SetRef{RawStatus: c.status}, Started: c.started}
			styled := dashboardStatusCellStyled(row)
			label := dashboardStatusLabel(row)
			// The bucket ANSI must wrap the base label token.
			if !strings.Contains(styled, c.ansi+label) {
				t.Fatalf("styled label = %q, want bucket %q on %q", styled, c.ansi, label)
			}
			// Width measurement stays plain: no ANSI in the un-styled cell.
			if plain := dashboardStatusCell(row); strings.Contains(plain, "\x1b[") {
				t.Fatalf("plain cell should carry no ANSI: %q", plain)
			}
		})
	}
}

// TestDashboardStatusBucketColorOnlyBaseToken confirms styling colors only the
// base label, leaving suffixes to their own styling: the auto-drain/orphaned
// suffixes stay plain and the label ANSI resets before them.
func TestDashboardStatusBucketColorOnlyBaseToken(t *testing.T) {
	row := DashboardRow{SetRef: SetRef{RawStatus: tasks.StatusBlocked, AutoDrain: true, Orphaned: true}}
	styled := dashboardStatusCellStyled(row)
	// Base label is yellow and reset before the suffixes.
	if !strings.Contains(styled, "\x1b[33mBLOCKED\x1b[m") {
		t.Fatalf("base BLOCKED token should be yellow-and-reset: %q", styled)
	}
	// Suffixes carry no color of the base bucket.
	if !strings.Contains(styled, "· auto-drain") || !strings.Contains(styled, "· orphaned") {
		t.Fatalf("suffixes missing: %q", styled)
	}
	if strings.Contains(styled, "\x1b[33m· auto-drain") || strings.Contains(styled, "\x1b[33m· orphaned") {
		t.Fatalf("suffixes should not inherit base bucket color: %q", styled)
	}
}

// TestDashboardLiveDrainRefinesReadyToInProgress covers ADR-0111's live-drain
// trigger for the STATUS label: a READY set held by a live drain reads "IN
// PROGRESS" even with zero done tasks; a started READY set (≥1 done, no live
// drain) still reads "IN PROGRESS"; an idle READY set stays "READY"; and a live
// drain coinciding with a non-READY status leaves that status' label untouched —
// needs-you outranks liveness.
func TestDashboardLiveDrainRefinesReadyToInProgress(t *testing.T) {
	liveReady := DashboardRow{SetRef: SetRef{RawStatus: tasks.StatusReady, LiveDrain: true}}
	if got := dashboardStatusLabel(liveReady); got != "IN PROGRESS" {
		t.Errorf("live READY label = %q, want IN PROGRESS", got)
	}
	startedReady := DashboardRow{SetRef: SetRef{RawStatus: tasks.StatusReady}, Started: true}
	if got := dashboardStatusLabel(startedReady); got != "IN PROGRESS" {
		t.Errorf("started READY label = %q, want IN PROGRESS", got)
	}
	idleReady := DashboardRow{SetRef: SetRef{RawStatus: tasks.StatusReady}}
	if got := dashboardStatusLabel(idleReady); got != string(tasks.StatusReady) {
		t.Errorf("idle READY label = %q, want READY", got)
	}
	// The refinement is READY-only: a live drain on a non-READY set keeps its
	// real label.
	for _, status := range []tasks.TaskSetStatus{tasks.StatusAwaitingApproval, tasks.StatusNeedsVerify, tasks.StatusBlocked} {
		row := DashboardRow{SetRef: SetRef{RawStatus: status, LiveDrain: true}}
		if got := dashboardStatusLabel(row); got != string(status) {
			t.Errorf("live %s label = %q, want %s (needs-you outranks liveness)", status, got, status)
		}
	}
}

// TestDashboardSummaryRunningCountsLiveDrainsOnly confirms the header "N running"
// tally counts live-drain rows only (ADR-0111): a parked or config-error row is
// not live-drained, so it no longer inflates the count as it did when the tally
// keyed on any non-blank DRAIN.
func TestDashboardSummaryRunningCountsLiveDrainsOnly(t *testing.T) {
	rows := []DashboardRow{
		{SetRef: SetRef{RawStatus: tasks.StatusReady, LiveDrain: true}},
		{SetRef: SetRef{RawStatus: tasks.StatusReady, Parked: true}},
		{SetRef: SetRef{RawStatus: tasks.StatusReady, ConfigError: "no trunk"}},
	}
	summary := dashboardSummary(rows)
	if !strings.Contains(summary, "1 running") {
		t.Errorf("summary = %q, want exactly 1 running (live-drain rows only)", summary)
	}
}

// TestDashboardDetailHeaderIncludesVerifiedAtSHA confirms the detail view header
// includes the yellow suffix inside the status brackets when applicable.
func TestDashboardDetailHeaderIncludesVerifiedAtSHA(t *testing.T) {
	header := detailHeader("demo", "AWAITING-APPROVAL", "1/1 done", "abcdef1234567890")
	if !strings.Contains(header, "Task · demo") {
		t.Fatalf("header missing set prefix: %q", header)
	}
	if !strings.Contains(header, "[AWAITING-APPROVAL") {
		t.Fatalf("header missing status bracket: %q", header)
	}
	if !strings.Contains(header, "verified @ abcdef123456") {
		t.Fatalf("header missing verified suffix: %q", header)
	}
	if !strings.Contains(header, "\x1b[33m") {
		t.Fatalf("verified suffix should be yellow: %q", header)
	}

	plain := detailHeader("demo", "AWAITING-APPROVAL", "1/1 done", "")
	if strings.Contains(plain, "verified @") {
		t.Fatalf("plain header should not contain suffix: %q", plain)
	}
}

// TestDashboardTableRendersVerifiedAtSHA confirms a row produced through the
// dashboard status pipeline renders the suffix in the STATUS column.
func TestDashboardTableRendersVerifiedAtSHA(t *testing.T) {
	row := DashboardRow{
		Project:       "pop",
		VerifiedAtSHA: "abcdef1234567890",
		Worktree:      "main",
		cursorKey:     "pop\x00set",
		SetRef:        SetRef{SetID: "set", RawStatus: tasks.StatusDone},
	}
	m := newQueueDashboard(&Deps{}, &config.Config{}, DashboardSnapshot{Rows: []DashboardRow{row}})
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 8})
	m = updated.(QueueDashboard)
	view := m.View().Content
	if !strings.Contains(view, "DONE") {
		t.Fatalf("view missing DONE status:\n%s", view)
	}
	if !strings.Contains(view, "verified @ abcdef123456") {
		t.Fatalf("view missing verified suffix:\n%s", view)
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
		{Project: "pop", cursorKey: "pop\x00set-bind", SetRef: SetRef{RawStatus: tasks.StatusReady, SetID: "set-bind"}},
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
		{Project: "pop", cursorKey: "pop\x00set-drain", SetRef: SetRef{RawStatus: tasks.StatusReady, SetID: "set-drain"}},
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
		{Project: "pop", cursorKey: "pop\x00set", SetRef: SetRef{RawStatus: tasks.StatusReady, SetID: "set"}},
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
		{Project: "pop", cursorKey: "pop\x00set-peek", SetRef: SetRef{RawStatus: tasks.StatusReady, SetID: "set-peek"}},
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
		{Project: "pop", cursorKey: "pop\x00set-peek", SetRef: SetRef{RawStatus: tasks.StatusReady, SetID: "set-peek"}},
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
		{Project: "pop", cursorKey: "pop\x00set-scroll", SetRef: SetRef{RawStatus: tasks.StatusReady, SetID: "set-scroll"}},
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
		{Project: "pop", cursorKey: "pop\x00first", SetRef: SetRef{RawStatus: tasks.StatusReady, SetID: "first"}},
		{Project: "pop", cursorKey: "pop\x00second", SetRef: SetRef{RawStatus: tasks.StatusReady, SetID: "second"}},
		{Project: "pop", cursorKey: "pop\x00third", SetRef: SetRef{RawStatus: tasks.StatusReady, SetID: "third"}},
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
		{Project: "pop", cursorKey: "pop\x00a", SetRef: SetRef{RawStatus: tasks.StatusReady, SetID: "a"}},
		{Project: "pop", cursorKey: "pop\x00b", SetRef: SetRef{RawStatus: tasks.StatusReady, SetID: "b"}},
		{Project: "pop", cursorKey: "pop\x00c", SetRef: SetRef{RawStatus: tasks.StatusReady, SetID: "c"}},
	}})
	m.list.SetCursor(2) // on "c"

	// A tick reload delivers the same sets reordered; the cursor must follow "c".
	reordered := []DashboardRow{
		{Project: "pop", cursorKey: "pop\x00c", SetRef: SetRef{RawStatus: tasks.StatusReady, SetID: "c"}},
		{Project: "pop", cursorKey: "pop\x00a", SetRef: SetRef{RawStatus: tasks.StatusReady, SetID: "a"}},
		{Project: "pop", cursorKey: "pop\x00b", SetRef: SetRef{RawStatus: tasks.StatusReady, SetID: "b"}},
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
		{Project: "pop", cursorKey: "pop\x00set-normal", SetRef: SetRef{RawStatus: tasks.StatusReady, SetID: "set-normal"}},
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
		{Project: "pop", cursorKey: "pop\x00set-nav", SetRef: SetRef{RawStatus: tasks.StatusReady, SetID: "set-nav"}},
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

// TestDashboardShowsUnsatisfiableWorktreeDirective asserts the Work dashboard
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
	if row.LiveDrain {
		t.Fatalf("LiveDrain = true, want false (config error row is not a live drain)")
	}
	status := dashboardStatusCell(*row)
	if !strings.Contains(status, "· config error:") || !strings.Contains(status, "no worktree of that name") {
		t.Fatalf("status = %q, want a config error suffix for the unsatisfiable named directive", status)
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
// config-class error as a STATUS suffix and needs bind for its worktree,
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
	got, err := dashboardRowsForStatic(d, &config.Config{}, st)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("rows = %+v, want one", got)
	}
	if got[0].LiveDrain {
		t.Fatalf("row LiveDrain = true, want false (config error row is not a live drain)")
	}
	if got[0].Worktree != dashboardDestLabelNeedsBind {
		t.Fatalf("row = %+v, want worktree %q", got[0], dashboardDestLabelNeedsBind)
	}
	wantSuffix := "· config error: " + repoScanReason
	if status := dashboardStatusCell(got[0]); !strings.Contains(status, wantSuffix) {
		t.Fatalf("status = %q, want config error suffix %q", status, wantSuffix)
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
	got, err := dashboardRowsForStatic(d, &config.Config{}, st)
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

	got, err := dashboardRowsForStatic(d, &config.Config{}, staticForScan(scan, "main", false))
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
	renderDashboardTable(&rendered, got, 0, 0, 20)
	out := rendered.String()
	if !strings.Contains(out, "[managed wt]") {
		t.Fatalf("render missing [managed wt] badge:\n%s", out)
	}
	if strings.Contains(out, "↳") {
		t.Fatalf("render must not contain worktree marker glyph:\n%s", out)
	}
}

// TestDashboardDoneHiddenUniformly pins the ADR-0121 uniform DONE hide: a DONE
// set is omitted by default whether its Worktree binding is adopted or managed
// (the old managed-worktree carve-out is retired). Done inclusion reveals both,
// and the managed one still carries its clean-up destKind.
func TestDashboardDoneHiddenUniformly(t *testing.T) {
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

	got, err := dashboardRowsForStatic(d, &config.Config{}, staticForScan(scan, "main", false))
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Fatalf("default rows = %+v, want both DONE sets hidden", got)
	}

	// Done inclusion reveals both DONE sets regardless of binding kind.
	d.IncludeDone = true
	got, err = dashboardRowsForStatic(d, &config.Config{}, staticForScan(scan, "main", false))
	if err != nil {
		t.Fatal(err)
	}
	byID := map[string]DashboardRow{}
	for _, row := range got {
		byID[row.SetID] = row
	}
	if row, ok := byID["done-adopted"]; !ok {
		t.Fatal("adopted Done binding should be revealed with include-done")
	} else if row.Worktree != "adopted-branch" {
		t.Fatalf("done-adopted row = %+v", row)
	}
	if row, ok := byID["done-managed"]; !ok {
		t.Fatal("managed Done binding should be revealed with include-done")
	} else if row.destKind != dashboardDestDoneManagedBound || row.Worktree != "managed-branch" {
		t.Fatalf("done-managed row = %+v", row)
	}
}

func TestDashboardNeedsBindRenderedDim(t *testing.T) {
	d := dashboardTestDeps(t, []tasks.Row{{ID: "plain", Status: tasks.StatusReady}}, nil)
	scan := projectScan{Name: "pop", ProjectPath: "/repo/main", RuntimePath: "/repo/main", DefinitionPath: "/def", RepoKey: "repo-key"}
	got, err := dashboardRowsForStatic(d, &config.Config{}, staticForScan(scan, "main", false))
	if err != nil {
		t.Fatal(err)
	}
	var rendered strings.Builder
	renderDashboardTable(&rendered, got, 0, 0, 20)
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

func TestCreateWorktreeManagedFreshBranchNoSession(t *testing.T) {
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
	m := newQueueDashboard(&Deps{}, &config.Config{}, DashboardSnapshot{Rows: []DashboardRow{{Project: "pop", Worktree: "/repo/bound (branch)", cursorKey: "pop\x00set-unbind", SetRef: SetRef{RawStatus: tasks.StatusFailed, SetID: "set-unbind", DefPath: "/repo/tasks", StatePath: "/repo/state.json", Bound: true}}}})

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
	panes, err := tasks.AllDrainPanes(d.Tasks)
	if err != nil {
		t.Fatal(err)
	}
	repoKey, err := resolveRepoKey(d, repo)
	if err != nil {
		t.Fatal(err)
	}
	pane := panes[setScopedKey(repoKey, setID)]
	if pane.PaneID != paneID || pane.SetID != setID || pane.Source != source {
		t.Fatalf("pane mapping = %+v, want pane=%s set=%s source=%s", pane, paneID, setID, source)
	}
}

func filterTestModel() QueueDashboard {
	rows := []DashboardRow{
		{Project: "alpha", cursorKey: "alpha\x00set-one", SetRef: SetRef{RawStatus: tasks.StatusReady, SetID: "set-one"}},
		{Project: "beta", cursorKey: "beta\x00set-two", SetRef: SetRef{RawStatus: tasks.StatusReady, SetID: "set-two"}},
		{Project: "gamma", cursorKey: "gamma\x00feature", SetRef: SetRef{RawStatus: tasks.StatusFailed, SetID: "feature"}},
	}
	m := newQueueDashboard(&Deps{}, &config.Config{}, DashboardSnapshot{Rows: rows})
	m.list.SetCursor(2)
	return m
}

// filterMenuTestModel builds a model with two non-done rows plus one DONE row,
// with Done inclusion off (the launch default), so allRows/snap.Rows initially
// hide the DONE set — the state the filter menu's Show-done toggle flips.
func filterMenuTestModel() QueueDashboard {
	rows := []DashboardRow{
		{Project: "alpha", cursorKey: "alpha\x00set-one", SetRef: SetRef{RawStatus: tasks.StatusReady, SetID: "set-one"}},
		{Project: "beta", cursorKey: "beta\x00set-two", SetRef: SetRef{RawStatus: tasks.StatusFailed, SetID: "set-two"}},
	}
	m := newQueueDashboard(&Deps{}, &config.Config{}, DashboardSnapshot{Rows: rows})
	m.width = 120
	m.height = 20
	return m
}

// doneRow is the DONE task set the reload delivers once Show-done is toggled on.
func doneRow() DashboardRow {
	return DashboardRow{Project: "gamma", cursorKey: "gamma\x00done-set", SetRef: SetRef{RawStatus: tasks.StatusDone, SetID: "done-set"}}
}

func TestDashboardFilterMenuOpenAndClose(t *testing.T) {
	m := filterMenuTestModel()

	// `f` opens the filter modal without dispatching a command.
	updated, cmd := m.Update(tea.KeyPressMsg{Code: 'f', Text: "f"})
	got := updated.(QueueDashboard)
	if got.filter == nil {
		t.Fatal("f did not open the filter menu")
	}
	if cmd != nil {
		t.Fatal("opening the filter menu should not dispatch a command")
	}
	if got.filterMode {
		t.Fatal("f must not enter the fuzzy filter mode")
	}
	view := got.View().Content
	for _, want := range []string{"filters", "show done", "[ ]", "enter/space toggle · esc close"} {
		if !strings.Contains(view, want) {
			t.Fatalf("filter menu view missing %q:\n%s", want, view)
		}
	}

	// `esc` closes the overlay without quitting.
	updated, cmd = got.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	got = updated.(QueueDashboard)
	if got.filter != nil {
		t.Fatal("esc did not close the filter menu")
	}
	if cmd != nil {
		t.Fatal("closing the filter menu should not quit or dispatch")
	}

	// `f` reopens and `f` again closes (sibling-modal toggle).
	updated, _ = got.Update(tea.KeyPressMsg{Code: 'f', Text: "f"})
	got = updated.(QueueDashboard)
	if got.filter == nil {
		t.Fatal("f did not reopen the filter menu")
	}
	updated, _ = got.Update(tea.KeyPressMsg{Code: 'f', Text: "f"})
	got = updated.(QueueDashboard)
	if got.filter != nil {
		t.Fatal("second f did not close the filter menu")
	}
}

func TestDashboardFilterMenuShowDoneTogglesLive(t *testing.T) {
	m := filterMenuTestModel()
	if len(m.snap.Rows) != 2 {
		t.Fatalf("initial rows = %d, want 2 (done hidden)", len(m.snap.Rows))
	}

	// Open the menu and flip Show done on via its letter shortcut.
	updated, _ := m.Update(tea.KeyPressMsg{Code: 'f', Text: "f"})
	m = updated.(QueueDashboard)
	updated, cmd := m.Update(tea.KeyPressMsg{Code: 'd', Text: "d"})
	m = updated.(QueueDashboard)
	if !m.d.IncludeDone {
		t.Fatal("toggling Show done did not set IncludeDone")
	}
	if cmd == nil {
		t.Fatal("toggling Show done must trigger a rebuild")
	}
	if m.filter == nil {
		t.Fatal("toggle should leave the filter menu open")
	}
	if !strings.Contains(m.View().Content, "[x]") {
		t.Fatalf("checkbox should render checked after toggle-on:\n%s", m.View().Content)
	}

	// Simulate the reload the toggle triggered: BuildDashboard now includes the
	// DONE set and re-sorts (proven by task 02's BuildDashboard tests). The
	// rebuilt rows flow into the view.
	updated, _ = m.Update(dashboardRowsMsg{snap: DashboardSnapshot{Rows: []DashboardRow{
		{Project: "alpha", cursorKey: "alpha\x00set-one", SetRef: SetRef{RawStatus: tasks.StatusReady, SetID: "set-one"}},
		{Project: "beta", cursorKey: "beta\x00set-two", SetRef: SetRef{RawStatus: tasks.StatusFailed, SetID: "set-two"}},
		doneRow(),
	}}})
	m = updated.(QueueDashboard)
	if len(m.snap.Rows) != 3 {
		t.Fatalf("after toggle-on reload: rows = %d, want 3", len(m.snap.Rows))
	}
	if !strings.Contains(m.View().Content, "done-set") {
		t.Fatalf("DONE set should be visible after toggle-on:\n%s", m.View().Content)
	}

	// Flip Show done back off: the flag clears and the rebuilt (done-excluded)
	// rows hide the DONE set again.
	updated, _ = m.Update(tea.KeyPressMsg{Code: 'd', Text: "d"})
	m = updated.(QueueDashboard)
	if m.d.IncludeDone {
		t.Fatal("toggling Show done again did not clear IncludeDone")
	}
	if strings.Contains(m.View().Content, "[x]") {
		t.Fatalf("checkbox should render unchecked after toggle-off:\n%s", m.View().Content)
	}
	updated, _ = m.Update(dashboardRowsMsg{snap: DashboardSnapshot{Rows: []DashboardRow{
		{Project: "alpha", cursorKey: "alpha\x00set-one", SetRef: SetRef{RawStatus: tasks.StatusReady, SetID: "set-one"}},
		{Project: "beta", cursorKey: "beta\x00set-two", SetRef: SetRef{RawStatus: tasks.StatusFailed, SetID: "set-two"}},
	}}})
	m = updated.(QueueDashboard)
	if len(m.snap.Rows) != 2 {
		t.Fatalf("after toggle-off reload: rows = %d, want 2", len(m.snap.Rows))
	}
	if strings.Contains(m.View().Content, "done-set") {
		t.Fatalf("DONE set should be hidden after toggle-off:\n%s", m.View().Content)
	}
}

func TestDashboardFilterMenuEnterTogglesHighlighted(t *testing.T) {
	m := filterMenuTestModel()
	updated, _ := m.Update(tea.KeyPressMsg{Code: 'f', Text: "f"})
	m = updated.(QueueDashboard)
	// Enter flips the highlighted toggle just like its letter shortcut.
	updated, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	got := updated.(QueueDashboard)
	if !got.d.IncludeDone {
		t.Fatal("enter did not toggle the highlighted filter")
	}
	if cmd == nil {
		t.Fatal("enter toggle must trigger a rebuild")
	}
}

func TestDashboardFilterMenuSeedsFromIncludeDone(t *testing.T) {
	// `--include-done` seeds IncludeDone true at launch; the menu opens checked.
	m := newQueueDashboard(&Deps{IncludeDone: true}, &config.Config{}, DashboardSnapshot{Rows: []DashboardRow{
		{Project: "alpha", cursorKey: "alpha\x00set-one", SetRef: SetRef{RawStatus: tasks.StatusReady, SetID: "set-one"}},
	}})
	m.width = 120
	m.height = 20
	updated, _ := m.Update(tea.KeyPressMsg{Code: 'f', Text: "f"})
	got := updated.(QueueDashboard)
	if !strings.Contains(got.View().Content, "[x]") {
		t.Fatalf("Show done should seed checked from --include-done:\n%s", got.View().Content)
	}
}

func TestDashboardFilterMenuIndependentOfSlash(t *testing.T) {
	// `/` fuzzy filtering is a distinct concept from the filter menu. Opening the
	// menu never enters fuzzy mode, and a rebuild the menu triggers preserves an
	// active fuzzy query.
	m := filterTestModel()

	// A fuzzy query narrows the visible rows.
	updated, _ := m.Update(tea.KeyPressMsg{Code: '/', Text: "/"})
	m = updated.(QueueDashboard)
	for _, ch := range "beta" {
		updated, _ = m.Update(tea.KeyPressMsg{Code: ch, Text: string(ch)})
		m = updated.(QueueDashboard)
	}
	if len(m.snap.Rows) != 1 {
		t.Fatalf("after 'beta' query: rows = %d, want 1", len(m.snap.Rows))
	}
	if m.filter != nil {
		t.Fatal("fuzzy filtering must not open the filter menu")
	}

	// A rebuild triggered while the fuzzy query is active (as the Show-done toggle
	// would trigger) re-applies the query rather than dropping it.
	updated, _ = m.Update(dashboardRowsMsg{snap: DashboardSnapshot{Rows: []DashboardRow{
		{Project: "alpha", cursorKey: "alpha\x00set-one", SetRef: SetRef{RawStatus: tasks.StatusReady, SetID: "set-one"}},
		{Project: "beta", cursorKey: "beta\x00set-two", SetRef: SetRef{RawStatus: tasks.StatusFailed, SetID: "set-two"}},
		{Project: "gamma", cursorKey: "gamma\x00feature", SetRef: SetRef{RawStatus: tasks.StatusFailed, SetID: "feature"}},
	}}})
	m = updated.(QueueDashboard)
	if !m.filterMode {
		t.Fatal("rebuild dropped the fuzzy filter mode")
	}
	if len(m.snap.Rows) != 1 || m.snap.Rows[0].Project != "beta" {
		t.Fatalf("rebuild did not preserve the 'beta' query: rows = %d", len(m.snap.Rows))
	}

	// The `f` menu opens independently once out of fuzzy mode.
	updated, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	m = updated.(QueueDashboard)
	updated, _ = m.Update(tea.KeyPressMsg{Code: 'f', Text: "f"})
	got := updated.(QueueDashboard)
	if got.filter == nil {
		t.Fatal("f did not open the filter menu after clearing the fuzzy query")
	}
	if got.filterMode {
		t.Fatal("opening the filter menu must not re-enter fuzzy mode")
	}
}

func TestDashboardFilterMenuBlocksViewToggle(t *testing.T) {
	m := filterMenuTestModel()
	if !m.ViewToggleAllowed() {
		t.Fatal("view toggle should be allowed on the plain main list")
	}
	updated, _ := m.Update(tea.KeyPressMsg{Code: 'f', Text: "f"})
	got := updated.(QueueDashboard)
	if got.ViewToggleAllowed() {
		t.Fatal("view toggle must be blocked while the filter menu is open")
	}
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
		{Project: "alpha", cursorKey: "alpha\x00set-one", SetRef: SetRef{RawStatus: tasks.StatusReady, SetID: "set-one", DefPath: "/def", StatePath: "/state"}},
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
		{Project: "alpha", cursorKey: "alpha\x00set-one", SetRef: SetRef{RawStatus: tasks.StatusBlocked, SetID: "set-one"}},
		{Project: "beta", cursorKey: "beta\x00set-two", SetRef: SetRef{RawStatus: tasks.StatusReady, SetID: "set-two"}},
		{Project: "delta", cursorKey: "delta\x00alpha-task", SetRef: SetRef{RawStatus: tasks.StatusReady, SetID: "alpha-task"}},
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
		{Project: "pop", cursorKey: "pop\x00set-render", SetRef: SetRef{RawStatus: tasks.StatusReady, SetID: "set-render"}},
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

func TestQueueDashboardHelpOverlay(t *testing.T) {
	ctrlH := tea.KeyPressMsg{Code: 'h', Mod: tea.ModCtrl}

	t.Run("C-h opens help in main list", func(t *testing.T) {
		m := newQueueDashboard(nil, nil, DashboardSnapshot{})
		updated, _ := m.Update(ctrlH)
		got := updated.(QueueDashboard)
		if !got.showHelp {
			t.Error("C-h should open help overlay")
		}
	})

	t.Run("second C-h closes help", func(t *testing.T) {
		m := newQueueDashboard(nil, nil, DashboardSnapshot{})
		updated, _ := m.Update(ctrlH)
		got := updated.(QueueDashboard)
		if !got.showHelp {
			t.Fatal("first C-h should open help")
		}
		updated, _ = got.Update(ctrlH)
		got = updated.(QueueDashboard)
		if got.showHelp {
			t.Error("second C-h should close help")
		}
	})

	t.Run("Esc closes help", func(t *testing.T) {
		m := newQueueDashboard(nil, nil, DashboardSnapshot{})
		updated, _ := m.Update(ctrlH)
		got := updated.(QueueDashboard)
		if !got.showHelp {
			t.Fatal("C-h should open help")
		}
		updated, _ = got.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
		got = updated.(QueueDashboard)
		if got.showHelp {
			t.Error("Esc should close help")
		}
	})

	t.Run("help swallows other keys when open", func(t *testing.T) {
		m := newQueueDashboard(nil, nil, DashboardSnapshot{})
		updated, _ := m.Update(ctrlH)
		got := updated.(QueueDashboard)
		if !got.showHelp {
			t.Fatal("C-h should open help")
		}
		// Try pressing 'j' which would normally move cursor
		updated, _ = got.Update(tea.KeyPressMsg{Code: 'j', Text: "j"})
		got = updated.(QueueDashboard)
		if !got.showHelp {
			t.Error("help should remain open when other keys pressed")
		}
	})

	t.Run("help works in filter mode", func(t *testing.T) {
		m := newQueueDashboard(nil, nil, DashboardSnapshot{})
		updated, _ := m.Update(tea.KeyPressMsg{Code: '/', Text: "/"})
		got := updated.(QueueDashboard)
		if !got.filterMode {
			t.Fatal("/ should enter filter mode")
		}
		updated, _ = got.Update(ctrlH)
		got = updated.(QueueDashboard)
		if !got.showHelp {
			t.Error("C-h should open help in filter mode")
		}
	})

	t.Run("help works in detail view", func(t *testing.T) {
		m := newQueueDashboard(nil, nil, DashboardSnapshot{})
		m.detail = &detailView{}
		updated, _ := m.Update(ctrlH)
		got := updated.(QueueDashboard)
		if !got.showHelp {
			t.Error("C-h should open help in detail view")
		}
	})

	t.Run("help works in peek mode", func(t *testing.T) {
		m := newQueueDashboard(nil, nil, DashboardSnapshot{})
		m.detail = &detailView{peek: &taskTextPeek{}}
		updated, _ := m.Update(ctrlH)
		got := updated.(QueueDashboard)
		if !got.showHelp {
			t.Error("C-h should open help in peek mode")
		}
	})

	t.Run("help works in action menu", func(t *testing.T) {
		m := newQueueDashboard(nil, nil, DashboardSnapshot{})
		m.menu = &dashboardMenu{}
		updated, _ := m.Update(ctrlH)
		got := updated.(QueueDashboard)
		if !got.showHelp {
			t.Error("C-h should open help in action menu")
		}
	})

	t.Run("help works in task menu", func(t *testing.T) {
		m := newQueueDashboard(nil, nil, DashboardSnapshot{})
		m.taskMenu = &taskMenu{}
		updated, _ := m.Update(ctrlH)
		got := updated.(QueueDashboard)
		if !got.showHelp {
			t.Error("C-h should open help in task menu")
		}
	})

	t.Run("help works in bind modal", func(t *testing.T) {
		m := newQueueDashboard(nil, nil, DashboardSnapshot{})
		m.bind = &dashboardBindModal{}
		updated, _ := m.Update(ctrlH)
		got := updated.(QueueDashboard)
		if !got.showHelp {
			t.Error("C-h should open help in bind modal")
		}
	})

	t.Run("help works in drain picker", func(t *testing.T) {
		m := newQueueDashboard(nil, nil, DashboardSnapshot{})
		m.drainPick = &dashboardDrainModal{}
		updated, _ := m.Update(ctrlH)
		got := updated.(QueueDashboard)
		if !got.showHelp {
			t.Error("C-h should open help in drain picker")
		}
	})

	t.Run("help works in abandon modal", func(t *testing.T) {
		m := newQueueDashboard(nil, nil, DashboardSnapshot{})
		m.abandon = &dashboardAbandonModal{}
		updated, _ := m.Update(ctrlH)
		got := updated.(QueueDashboard)
		if !got.showHelp {
			t.Error("C-h should open help in abandon modal")
		}
	})

	t.Run("F1 does nothing", func(t *testing.T) {
		m := newQueueDashboard(nil, nil, DashboardSnapshot{})
		updated, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyF1})
		got := updated.(QueueDashboard)
		if got.showHelp {
			t.Error("F1 should not open help")
		}
	})
}

func TestQueueDashboardHelpContent(t *testing.T) {
	t.Run("main list shows main bindings", func(t *testing.T) {
		m := newQueueDashboard(nil, nil, DashboardSnapshot{})
		entries := m.helpEntries()
		if len(entries) == 0 {
			t.Fatal("main list should have help entries")
		}
		// Check for key bindings
		found := map[string]bool{}
		for _, e := range entries {
			found[e.Key] = true
		}
		required := []string{"j/k", "gg", "G", "l/enter", "a", "/", "h/esc"}
		for _, key := range required {
			if !found[key] {
				t.Errorf("main list help missing key: %s", key)
			}
		}
	})

	t.Run("filter mode shows filter bindings", func(t *testing.T) {
		m := newQueueDashboard(nil, nil, DashboardSnapshot{})
		m.filterMode = true
		entries := m.helpEntries()
		found := map[string]bool{}
		for _, e := range entries {
			found[e.Key] = true
		}
		if !found["typing"] {
			t.Error("filter mode help missing 'typing'")
		}
		if !found["j/k"] {
			t.Error("filter mode help missing 'j/k'")
		}
		if !found["esc"] {
			t.Error("filter mode help missing 'esc'")
		}
	})

	t.Run("detail view shows detail bindings", func(t *testing.T) {
		m := newQueueDashboard(nil, nil, DashboardSnapshot{})
		m.detail = &detailView{}
		entries := m.helpEntries()
		found := map[string]bool{}
		for _, e := range entries {
			found[e.Key] = true
		}
		if !found["l/enter"] {
			t.Error("detail view help missing 'l/enter'")
		}
		if !found["a"] {
			t.Error("detail view help missing 'a'")
		}
	})

	t.Run("peek mode shows peek bindings", func(t *testing.T) {
		m := newQueueDashboard(nil, nil, DashboardSnapshot{})
		m.detail = &detailView{peek: &taskTextPeek{}}
		entries := m.helpEntries()
		found := map[string]bool{}
		for _, e := range entries {
			found[e.Key] = true
		}
		if !found["ctrl+d"] {
			t.Error("peek mode help missing 'ctrl+d'")
		}
		if !found["ctrl+u"] {
			t.Error("peek mode help missing 'ctrl+u'")
		}
	})

	t.Run("action menu shows menu verbs", func(t *testing.T) {
		m := newQueueDashboard(nil, nil, DashboardSnapshot{})
		m.menu = &dashboardMenu{}
		entries := m.helpEntries()
		found := map[string]bool{}
		for _, e := range entries {
			found[e.Key] = true
		}
		// Should show menu-specific verbs
		if !found["i"] {
			t.Error("action menu help missing 'i' (drain)")
		}
		if !found["b"] {
			t.Error("action menu help missing 'b' (bind)")
		}
		if !found["esc"] {
			t.Error("action menu help missing 'esc'")
		}
	})

	t.Run("task menu shows task verbs", func(t *testing.T) {
		m := newQueueDashboard(nil, nil, DashboardSnapshot{})
		m.taskMenu = &taskMenu{}
		entries := m.helpEntries()
		found := map[string]bool{}
		for _, e := range entries {
			found[e.Key] = true
		}
		if !found["C"] {
			t.Error("task menu help missing 'C' (complete)")
		}
		if !found["O"] {
			t.Error("task menu help missing 'O' (open)")
		}
		if !found["K"] {
			t.Error("task menu help missing 'K' (skip)")
		}
	})

	t.Run("bind modal shows bind bindings", func(t *testing.T) {
		m := newQueueDashboard(nil, nil, DashboardSnapshot{})
		m.bind = &dashboardBindModal{}
		entries := m.helpEntries()
		found := map[string]bool{}
		for _, e := range entries {
			found[e.Key] = true
		}
		if !found["j/k"] {
			t.Error("bind modal help missing 'j/k'")
		}
		if !found["enter"] {
			t.Error("bind modal help missing 'enter'")
		}
	})

	t.Run("drain picker shows picker bindings", func(t *testing.T) {
		m := newQueueDashboard(nil, nil, DashboardSnapshot{})
		m.drainPick = &dashboardDrainModal{}
		entries := m.helpEntries()
		found := map[string]bool{}
		for _, e := range entries {
			found[e.Key] = true
		}
		if !found["j/k"] {
			t.Error("drain picker help missing 'j/k'")
		}
		if !found["enter"] {
			t.Error("drain picker help missing 'enter'")
		}
	})

	t.Run("abandon modal shows abandon bindings", func(t *testing.T) {
		m := newQueueDashboard(nil, nil, DashboardSnapshot{})
		m.abandon = &dashboardAbandonModal{}
		entries := m.helpEntries()
		found := map[string]bool{}
		for _, e := range entries {
			found[e.Key] = true
		}
		if !found["y/enter"] {
			t.Error("abandon modal help missing 'y/enter'")
		}
		if !found["n/esc"] {
			t.Error("abandon modal help missing 'n/esc'")
		}
	})
}

func TestQueueDashboardHelpRendering(t *testing.T) {
	m := newQueueDashboard(nil, nil, DashboardSnapshot{})
	m.width = 80
	m.height = 24
	m.showHelp = true
	view := m.View()

	// Check that help overlay is rendered
	if !strings.Contains(view.Content, "Help") {
		t.Error("help overlay should contain 'Help' title")
	}
	if !strings.Contains(view.Content, "C-h toggle") {
		t.Error("help overlay should contain 'C-h toggle' footer")
	}
	if !strings.Contains(view.Content, "Esc close") {
		t.Error("help overlay should contain 'Esc close' footer")
	}
}

func TestQueueDashboardHelpFooterHint(t *testing.T) {
	m := newQueueDashboard(nil, nil, DashboardSnapshot{})
	hint := m.mainHint()
	if !strings.Contains(hint, "C-h help") {
		t.Error("main footer hint should include 'C-h help'")
	}
}

// TestDashboardMainViewTwoLineIntegration wires two-line mode into the default
// Frame + List path: with a long set id every row renders two physical lines —
// the "PROJECT · SETID" identity (plus WORKTREE) on line 1 and STATUS on
// line 2 — and the view clamps to the terminal height instead of overflowing.
func TestDashboardMainViewTwoLineIntegration(t *testing.T) {
	longID := strings.Repeat("a", 37)
	rows := []DashboardRow{
		{Project: "pop", Worktree: "main", cursorKey: "pop\x00" + longID, SetRef: SetRef{RawStatus: tasks.StatusReady, SetID: longID}},
		{Project: "pop", Worktree: "main", cursorKey: "pop\x00bbb", SetRef: SetRef{RawStatus: tasks.StatusDone, SetID: "bbb"}},
	}
	m := newQueueDashboard(&Deps{}, &config.Config{}, DashboardSnapshot{Rows: rows})
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 80, Height: 20})
	m = updated.(QueueDashboard)

	if !dashboardTwoLineMode(m.snap.Rows, m.width, m.height) {
		t.Fatalf("expected two-line mode with a long set id")
	}
	if m.list.LinesPerItem() != 2 {
		t.Fatalf("LinesPerItem = %d, want 2", m.list.LinesPerItem())
	}

	view := m.View().Content
	lines := strings.Split(view, "\n")
	if got, want := len(lines), m.height; got != want {
		t.Fatalf("view line count = %d, want %d (clamped to terminal height):\n%s", got, want, view)
	}

	// The line-1 header labels the identity and WORKTREE columns (the DRAIN column
	// is retired, ADR-0111); the line-2 header labels STATUS.
	headerIdx := dashboardTestLineIndex(lines, "TASK SET")
	if headerIdx < 0 {
		t.Fatalf("two-line header missing TASK SET:\n%s", view)
	}
	header := lines[headerIdx]
	for _, want := range []string{"TASK SET", "WORKTREE"} {
		if !strings.Contains(header, want) {
			t.Fatalf("two-line line-1 header missing %q:\n%s", want, view)
		}
	}
	if strings.Contains(header, "DRAIN") {
		t.Fatalf("two-line line-1 header must not carry the retired DRAIN column:\n%s", view)
	}
	if !strings.Contains(lines[headerIdx+1], "STATUS") {
		t.Fatalf("two-line line-2 header missing STATUS:\n%s", view)
	}

	// The data row's first physical line carries PROJECT and the set id; STATUS
	// appears on the following physical line, not line 1.
	idIdx := dashboardTestLineIndex(lines, longID)
	if idIdx < 0 {
		t.Fatalf("set id %q missing from view:\n%s", longID, view)
	}
	if !strings.Contains(lines[idIdx], "pop") {
		t.Fatalf("row line 1 must carry the project:\n%s", view)
	}
	if strings.Contains(lines[idIdx], "READY") {
		t.Fatalf("row line 1 must not contain the status:\n%s", view)
	}
	if !strings.Contains(lines[idIdx+1], "READY") {
		t.Fatalf("row line 2 must contain the status READY:\n%s", view)
	}
}

// TestDashboardTwoLineCursorMovesByLogicalRow confirms that j/k/gg/G move the
// cursor one logical task-set row at a time even though each row renders two
// physical terminal lines.
func TestDashboardTwoLineCursorMovesByLogicalRow(t *testing.T) {
	longID := strings.Repeat("a", 37)
	rows := make([]DashboardRow, 5)
	for i := range rows {
		id := fmt.Sprintf("%s-%d", longID, i)
		rows[i] = DashboardRow{Project: "pop", cursorKey: "pop\x00" + id, SetRef: SetRef{RawStatus: tasks.StatusReady, SetID: id}}
	}
	m := newQueueDashboard(&Deps{}, &config.Config{}, DashboardSnapshot{Rows: rows})
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 40, Height: 20})
	m = updated.(QueueDashboard)

	if m.list.LinesPerItem() != 2 {
		t.Fatalf("LinesPerItem = %d, want 2", m.list.LinesPerItem())
	}

	assertCursor := func(name string, want int) {
		t.Helper()
		if m.list.Cursor() != want {
			t.Fatalf("%s: cursor = %d, want %d", name, m.list.Cursor(), want)
		}
	}

	updated, _ = m.Update(tea.KeyPressMsg{Code: 'j', Text: "j"})
	m = updated.(QueueDashboard)
	assertCursor("j", 1)

	updated, _ = m.Update(tea.KeyPressMsg{Code: 'j', Text: "j"})
	m = updated.(QueueDashboard)
	assertCursor("jj", 2)

	updated, _ = m.Update(tea.KeyPressMsg{Code: 'k', Text: "k"})
	m = updated.(QueueDashboard)
	assertCursor("jjk", 1)

	updated, _ = m.Update(tea.KeyPressMsg{Code: 'G', Text: "G"})
	m = updated.(QueueDashboard)
	assertCursor("G", len(rows)-1)

	updated, _ = m.Update(tea.KeyPressMsg{Code: 'g', Text: "g"})
	m = updated.(QueueDashboard)
	assertCursor("first g", len(rows)-1)
	updated, _ = m.Update(tea.KeyPressMsg{Code: 'g', Text: "g"})
	m = updated.(QueueDashboard)
	assertCursor("gg", 0)
}

// TestDashboardTwoLineClampsToBodyHeight asserts that many rows on a short
// terminal do not overflow the viewport in two-line mode. Each logical item
// consumes two physical lines, so the visible logical row count is halved, but
// the total rendered line count still equals the terminal height.
func TestDashboardTwoLineClampsToBodyHeight(t *testing.T) {
	longID := strings.Repeat("a", 37)
	rows := make([]DashboardRow, 40)
	for i := range rows {
		id := fmt.Sprintf("%s-%02d", longID, i)
		rows[i] = DashboardRow{Project: "pop", cursorKey: "pop\x00" + id, SetRef: SetRef{RawStatus: tasks.StatusReady, SetID: id}}
	}
	m := newQueueDashboard(&Deps{}, &config.Config{}, DashboardSnapshot{Rows: rows})
	// Height at the two-line floor (16): roomy enough for two-line mode, still
	// short enough that 40 rows overflow and must clamp to the viewport.
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 40, Height: 16})
	m = updated.(QueueDashboard)

	if m.list.LinesPerItem() != 2 {
		t.Fatalf("LinesPerItem = %d, want 2", m.list.LinesPerItem())
	}

	view := m.View().Content
	lines := strings.Split(view, "\n")
	if got, want := len(lines), m.height; got != want {
		t.Fatalf("view line count = %d, want %d (clamped to body height):\n%s", got, want, view)
	}

	// In two-line mode the chrome consumes an extra line for the second header
	// (blank, line-1 header, line-2 STATUS header, separator), and each logical
	// row occupies two physical lines.
	bodyH := m.frameSpec().BodyHeight(m.height) - dashboardTwoLineChromeLines
	visible := m.list.VisibleRows()
	if got := len(visible); got != bodyH {
		t.Fatalf("List VisibleRows = %d, want %d", got, bodyH)
	}
	selected, ok := m.list.Selected()
	if !ok {
		t.Fatal("expected a selected row")
	}
	if selected.SetID != rows[0].SetID {
		t.Fatalf("selected = %q, want %q", selected.SetID, rows[0].SetID)
	}
}

// TestDashboardShortPaneCollapsesToSingleLine asserts that a pane below the
// two-line height floor renders single-line rows even when the terminal is
// narrow and a set id is long — a short tmux popup trades id completeness for
// visible-row density (ADR-0107). The collapse must hold in both the main body
// and the action-menu overlay.
func TestDashboardShortPaneCollapsesToSingleLine(t *testing.T) {
	longID := strings.Repeat("a", 37)
	rows := []DashboardRow{
		{Project: "pop", Worktree: "main", cursorKey: "pop\x00" + longID, SetRef: SetRef{RawStatus: tasks.StatusReady, SetID: longID}},
		{Project: "pop", Worktree: "main", cursorKey: "pop\x00bbb", SetRef: SetRef{RawStatus: tasks.StatusDone, SetID: "bbb"}},
	}
	m := newQueueDashboard(&Deps{}, &config.Config{}, DashboardSnapshot{Rows: rows})
	// A wide pane (width >= 120) with a long id would force two-line mode were the
	// pane roomy — the id, not the width, is the trigger. But the height is one
	// row below the floor, so the table stays single-line. The width is wide
	// enough that the single-line header renders in full for the assertions.
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 130, Height: dashboardTwoLineHeightFloor - 1})
	m = updated.(QueueDashboard)

	if dashboardTwoLineMode(m.snap.Rows, m.width, m.height) {
		t.Fatalf("short pane must not activate two-line mode")
	}
	if m.list.LinesPerItem() != 1 {
		t.Fatalf("LinesPerItem = %d, want 1 on a short pane", m.list.LinesPerItem())
	}
	// In single-line mode the id and its status share one physical line; in
	// two-line mode the status would sit on the following line instead.
	assertSingleLineRow := func(view, label string) {
		t.Helper()
		lines := strings.Split(view, "\n")
		idx := dashboardTestLineIndex(lines, longID)
		if idx < 0 {
			t.Fatalf("%s: set id %q missing from render:\n%s", label, longID, view)
		}
		if !strings.Contains(lines[idx], "READY") {
			t.Fatalf("%s: status must share the id's line in single-line mode:\n%s", label, view)
		}
	}
	assertSingleLineRow(m.View().Content, "main body")

	// The action-menu overlay must share the same single-line decision.
	updated, _ = m.Update(tea.KeyPressMsg{Code: 'a', Text: "a"})
	m = updated.(QueueDashboard)
	if m.menu == nil {
		t.Fatal("a did not open the action menu")
	}
	assertSingleLineRow(m.View().Content, "menu overlay")
}

// TestDashboardMenuTwoLineOverlay verifies that opening the action menu (`a`)
// on a narrow pane renders the table rows in two-line mode and anchors the menu
// relative to the cursor's two-line block.
func TestDashboardMenuTwoLineOverlay(t *testing.T) {
	longID := strings.Repeat("a", 37)
	rows := []DashboardRow{
		{Project: "pop", Worktree: "main", cursorKey: "pop\x00" + longID, SetRef: SetRef{RawStatus: tasks.StatusReady, SetID: longID}},
		{Project: "pop", Worktree: "main", cursorKey: "pop\x00bbb", SetRef: SetRef{RawStatus: tasks.StatusDone, SetID: "bbb"}},
	}
	m := newQueueDashboard(&Deps{}, &config.Config{}, DashboardSnapshot{Rows: rows})
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	m = updated.(QueueDashboard)

	updated, _ = m.Update(tea.KeyPressMsg{Code: 'a', Text: "a"})
	m = updated.(QueueDashboard)
	if m.menu == nil {
		t.Fatal("a did not open the action menu")
	}

	view := m.View().Content
	lines := strings.Split(view, "\n")

	if !dashboardTwoLineMode(m.snap.Rows, m.width, m.height) {
		t.Fatalf("expected two-line mode with a long set id")
	}

	// The set id rides on line 1 (the identity); STATUS follows on line 2.
	idx := dashboardTestLineIndex(lines, longID)
	if idx < 0 {
		t.Fatalf("set id %q missing from menu overlay:\n%s", longID, view)
	}
	if !strings.Contains(lines[idx+1], "READY") {
		t.Fatalf("row line 2 must contain the status:\n%s", view)
	}

	// The menu must be rendered with the "actions" caption.
	if !strings.Contains(view, "actions") {
		t.Fatalf("menu caption not rendered:\n%s", view)
	}

	// No rendered line may exceed the terminal width.
	for i, line := range lines {
		if lipgloss.Width(line) > m.width {
			t.Fatalf("line %d exceeds terminal width (%d > %d): %q", i, lipgloss.Width(line), m.width, line)
		}
	}
}

// TestDashboardBindModalTwoLineOverlay verifies that the bind modal renders the
// table above its body in two-line mode on a narrow pane without spilling past
// the terminal width.
func TestDashboardBindModalTwoLineOverlay(t *testing.T) {
	longID := strings.Repeat("a", 37)
	rows := []DashboardRow{
		{Project: "pop", Worktree: "main", cursorKey: "pop\x00" + longID, SetRef: SetRef{RawStatus: tasks.StatusReady, SetID: longID}},
	}
	m := newQueueDashboard(&Deps{}, &config.Config{}, DashboardSnapshot{Rows: rows})
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 80, Height: 20})
	m = updated.(QueueDashboard)

	// Inject a bind modal directly so the test does not depend on filesystem/git.
	m.bind = &dashboardBindModal{
		row:   rows[0],
		stage: dashboardBindStageWorktree,
		list: newBindEntryList([]dashboardBindEntry{
			{Label: "existing worktree"},
			{Label: "create new"},
		}),
	}

	view := m.View().Content
	lines := strings.Split(view, "\n")

	if !dashboardTwoLineMode(m.snap.Rows, m.width, m.height) {
		t.Fatalf("expected two-line mode with a long set id")
	}

	// The set id rides on line 1 (the identity); STATUS follows on line 2.
	idx := dashboardTestLineIndex(lines, longID)
	if idx < 0 {
		t.Fatalf("set id %q missing from bind modal overlay:\n%s", longID, view)
	}
	if !strings.Contains(lines[idx+1], "READY") {
		t.Fatalf("row line 2 must contain the status:\n%s", view)
	}

	// No rendered line may exceed the terminal width (no horizontal spill).
	for i, line := range lines {
		if lipgloss.Width(line) > m.width {
			t.Fatalf("line %d exceeds terminal width (%d > %d): %q", i, lipgloss.Width(line), m.width, line)
		}
	}

	// The modal body must be rendered below the table.
	if !strings.Contains(view, "Bind worktree") {
		t.Fatalf("bind modal body not rendered:\n%s", view)
	}
}

// TestDashboardDrainModalTwoLineOverlay verifies that the drain target modal
// renders the table above its body in two-line mode on a narrow pane.
func TestDashboardDrainModalTwoLineOverlay(t *testing.T) {
	longID := strings.Repeat("a", 37)
	rows := []DashboardRow{
		{Project: "pop", Worktree: "main", cursorKey: "pop\x00" + longID, SetRef: SetRef{RawStatus: tasks.StatusReady, SetID: longID}},
	}
	m := newQueueDashboard(&Deps{}, &config.Config{}, DashboardSnapshot{Rows: rows})
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 80, Height: 20})
	m = updated.(QueueDashboard)

	m.drainPick = newDashboardDrainModal(rows[0], []dashboardDrainEntry{
		{Label: "new managed worktree"},
		{Label: "trunk"},
	})

	view := m.View().Content
	lines := strings.Split(view, "\n")

	if !dashboardTwoLineMode(m.snap.Rows, m.width, m.height) {
		t.Fatalf("expected two-line mode with a long set id")
	}

	idx := dashboardTestLineIndex(lines, longID)
	if idx < 0 {
		t.Fatalf("set id %q missing from drain modal overlay:\n%s", longID, view)
	}
	if !strings.Contains(lines[idx+1], "READY") {
		t.Fatalf("row line 2 must contain the status:\n%s", view)
	}

	for i, line := range lines {
		if lipgloss.Width(line) > m.width {
			t.Fatalf("line %d exceeds terminal width (%d > %d): %q", i, lipgloss.Width(line), m.width, line)
		}
	}

	if !strings.Contains(view, "Drain target") {
		t.Fatalf("drain modal body not rendered:\n%s", view)
	}
}

// TestDashboardFilterReevaluatesTwoLineMode verifies that filter mode re-evaluates
// two-line mode against the filtered row set: starting with a mix that triggers
// two-line mode and filtering down to short ids deactivates it; clearing the
// filter reactivates it.
func TestDashboardFilterReevaluatesTwoLineMode(t *testing.T) {
	longID := strings.Repeat("a", 37)
	rows := []DashboardRow{
		{Project: "pop", Worktree: "main", cursorKey: "pop\x00short", SetRef: SetRef{RawStatus: tasks.StatusReady, SetID: "short"}},
		{Project: "pop", Worktree: "main", cursorKey: "pop\x00" + longID, SetRef: SetRef{RawStatus: tasks.StatusReady, SetID: longID}},
	}
	m := newQueueDashboard(&Deps{}, &config.Config{}, DashboardSnapshot{Rows: rows})
	// Width at the forced-fit threshold (120): only the long set id, not width,
	// may trigger two-line mode, so filtering it away must drop back to one line.
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 20})
	m = updated.(QueueDashboard)

	if !dashboardTwoLineMode(m.snap.Rows, m.width, m.height) {
		t.Fatalf("expected two-line mode initially because of the long set id")
	}
	if m.list.LinesPerItem() != 2 {
		t.Fatalf("LinesPerItem = %d, want 2 before filter", m.list.LinesPerItem())
	}

	// Enter filter mode and type a query that matches only the short id.
	updated, _ = m.Update(tea.KeyPressMsg{Code: '/', Text: "/"})
	m = updated.(QueueDashboard)
	if !m.filterMode {
		t.Fatal("/ did not enter filter mode")
	}
	updated, _ = m.Update(tea.KeyPressMsg{Code: 's', Text: "s"})
	m = updated.(QueueDashboard)
	updated, _ = m.Update(tea.KeyPressMsg{Code: 'h', Text: "h"})
	m = updated.(QueueDashboard)
	updated, _ = m.Update(tea.KeyPressMsg{Code: 'o', Text: "o"})
	m = updated.(QueueDashboard)
	updated, _ = m.Update(tea.KeyPressMsg{Code: 'r', Text: "r"})
	m = updated.(QueueDashboard)
	updated, _ = m.Update(tea.KeyPressMsg{Code: 't', Text: "t"})
	m = updated.(QueueDashboard)

	if len(m.snap.Rows) != 1 {
		t.Fatalf("filtered rows = %d, want 1", len(m.snap.Rows))
	}
	if dashboardTwoLineMode(m.snap.Rows, m.width, m.height) {
		t.Fatalf("expected single-line mode after filtering to short id")
	}

	// A render must update LinesPerItem to match the filtered rows.
	_ = m.View()
	if m.list.LinesPerItem() != 1 {
		t.Fatalf("LinesPerItem = %d, want 1 after filtering to short id", m.list.LinesPerItem())
	}

	// Clear the filter: two-line mode must return because the long id is back.
	updated, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	m = updated.(QueueDashboard)
	if m.filterMode {
		t.Fatal("esc did not clear filter mode")
	}
	if !dashboardTwoLineMode(m.snap.Rows, m.width, m.height) {
		t.Fatalf("expected two-line mode after clearing filter")
	}
	_ = m.View()
	if m.list.LinesPerItem() != 2 {
		t.Fatalf("LinesPerItem = %d, want 2 after clearing filter", m.list.LinesPerItem())
	}
}

// TestDashboardStatusAppendsAutoDrain confirms the status-label assembly appends
// a plain-text ` · auto-drain` suffix for an auto-drain row (after the yellow
// verify suffix), and nothing for a non-auto-drain row.
func TestDashboardStatusAppendsAutoDrain(t *testing.T) {
	ad := dashboardStatusCellStyled(DashboardRow{SetRef: SetRef{RawStatus: tasks.StatusReady, AutoDrain: true}})
	// The base label is bucket-colored (READY → blue); the marker follows plainly.
	if !strings.Contains(ad, "\x1b[34mREADY\x1b[m · auto-drain") {
		t.Fatalf("auto-drain suffix missing/misplaced: %q", ad)
	}
	if plain := dashboardStatusCellStyled(DashboardRow{SetRef: SetRef{RawStatus: tasks.StatusReady}}); strings.Contains(plain, "auto-drain") {
		t.Fatalf("non-auto-drain row should not carry suffix: %q", plain)
	}

	// The yellow verify suffix must still render and precede the uncolored
	// auto-drain suffix: <label> · verified @ <sha> · auto-drain.
	ordered := dashboardStatusCellStyled(DashboardRow{VerifiedAtSHA: "abcdef1234567890", SetRef: SetRef{RawStatus: tasks.StatusAwaitingApproval, AutoDrain: true}})
	vIdx := strings.Index(ordered, "verified @")
	aIdx := strings.Index(ordered, "auto-drain")
	if vIdx < 0 || aIdx < 0 || vIdx > aIdx {
		t.Fatalf("verify suffix must precede auto-drain: %q", ordered)
	}
	if !strings.Contains(ordered, "\x1b[33m") {
		t.Fatalf("verify suffix should stay yellow: %q", ordered)
	}
	if !strings.Contains(ordered, " · auto-drain") {
		t.Fatalf("auto-drain suffix should be uncolored plain text: %q", ordered)
	}
}

// TestDashboardStatusSuffixesRender drives the fork-free build so a bound-but-
// missing checkout appends ` · orphaned` at the row-assembly site, an auto-drain
// row appends ` · auto-drain`, and a row that is both shows them ordered
// `... · auto-drain · orphaned` — surfaced in both single-line and two-line
// render modes off the one precomputed status string.
func TestDashboardStatusSuffixesRender(t *testing.T) {
	rows := []tasks.Row{
		{ID: "ad", Status: tasks.StatusBlocked, AutoDrain: true},
		{ID: "orph", Status: tasks.StatusBlocked},
		{ID: "both", Status: tasks.StatusBlocked, AutoDrain: true},
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
		setScopedKey("repo-key", "ad"):   {RuntimePath: presentPath, Branch: "ad-branch"},
		setScopedKey("repo-key", "orph"): {RuntimePath: "/repo/gone", Branch: "orph-branch"},
		setScopedKey("repo-key", "both"): {RuntimePath: "/repo/gone2", Branch: "both-branch"},
	})
	scan := projectScan{Name: "pop", ProjectPath: "/repo/main", RuntimePath: "/repo/main", DefinitionPath: "/def", RepoKey: "repo-key"}

	got, err := dashboardRowsForStatic(d, &config.Config{}, staticForScan(scan, "main", false))
	if err != nil {
		t.Fatal(err)
	}
	byID := map[string]DashboardRow{}
	for _, row := range got {
		byID[row.SetID] = row
	}

	if s := dashboardStatusCell(byID["ad"]); !strings.Contains(s, " · auto-drain") || strings.Contains(s, "orphaned") {
		t.Fatalf("auto-drain row status = %q", s)
	}
	if s := dashboardStatusCell(byID["orph"]); !strings.Contains(s, " · orphaned") || strings.Contains(s, "auto-drain") {
		t.Fatalf("orphaned row status = %q", s)
	}
	if s := dashboardStatusCell(byID["both"]); !strings.Contains(s, " · auto-drain · orphaned") {
		t.Fatalf("both row status = %q", s)
	}

	// Both render modes read the same precomputed status; widths are wide enough
	// that no truncation clips the suffixes. Column order: PROJECT, TASK SET,
	// STATUS (index 2, given the width), WORKTREE, indicator.
	widths := []int{20, 20, 60, 20, 20}
	single := dashboardTableLine(dashboardRowValues(byID["both"]), widths)
	if !strings.Contains(single, "· auto-drain · orphaned") {
		t.Fatalf("single-line render missing suffixes:\n%s", single)
	}
	twoLine := dashboardTwoLineRowLine2(byID["both"], []int{10, 10, 10, 10})
	if !strings.Contains(twoLine, "· auto-drain · orphaned") {
		t.Fatalf("two-line render missing suffixes:\n%s", twoLine)
	}
}

// TestDashboardParkedAndConfigErrorSuffixes covers ADR-0111's relocation of the
// old DRAIN `parked` / `config error: <msg>` values onto the STATUS cell: each
// renders as a plain ` · parked` / ` · config error: <msg>` suffix in every
// status and both row layouts, the DRAIN string no longer carries either, the
// two compose cleanly after verified/auto-drain/orphaned, and the styled cell's
// measured width matches the plain form (no ANSI leaks into column math).
func TestDashboardParkedAndConfigErrorSuffixes(t *testing.T) {
	// Column order: PROJECT, TASK SET, STATUS, WORKTREE, indicator — STATUS (index
	// 2) is given ample width so its suffixes are not truncated at render.
	statusW := []int{10, 10, 80, 10, 10}

	for _, status := range []tasks.TaskSetStatus{tasks.StatusReady, tasks.StatusBlocked, tasks.StatusAwaitingApproval, tasks.StatusFailed} {
		row := DashboardRow{SetRef: SetRef{RawStatus: status, Parked: true}}
		if s := dashboardStatusCell(row); !strings.Contains(s, " · parked") {
			t.Fatalf("status %s parked cell = %q, want a · parked suffix", status, s)
		}
		single := dashboardTableLine(dashboardRowValues(row), statusW)
		if !strings.Contains(single, "· parked") {
			t.Fatalf("status %s single-line parked render missing suffix:\n%s", status, single)
		}
		twoLine := dashboardTwoLineRowLine2(row, []int{10, 10, 10, 10})
		if !strings.Contains(twoLine, "· parked") {
			t.Fatalf("status %s two-line parked render missing suffix:\n%s", status, twoLine)
		}
	}

	const msg = "no trunk worktree configured"
	ce := DashboardRow{SetRef: SetRef{RawStatus: tasks.StatusReady, ConfigError: msg}}
	if s := dashboardStatusCell(ce); !strings.Contains(s, " · config error: "+msg) {
		t.Fatalf("config-error cell = %q, want a · config error suffix", s)
	}
	if ce.LiveDrain {
		t.Fatalf("config-error row LiveDrain = true, want false (config error is not a live drain)")
	}
	single := dashboardTableLine(dashboardRowValues(ce), statusW)
	if !strings.Contains(single, "· config error: "+msg) {
		t.Fatalf("single-line config-error render missing suffix:\n%s", single)
	}
	twoLine := dashboardTwoLineRowLine2(ce, []int{10, 10, 10, 10})
	if !strings.Contains(twoLine, "· config error: "+msg) {
		t.Fatalf("two-line config-error render missing suffix:\n%s", twoLine)
	}

	// Multi-suffix compose: verified @, auto-drain, orphaned, parked, config error
	// all appear in a fixed order, and the styled cell measures the same width as
	// the plain cell (the two new suffixes stay ANSI-free).
	multi := DashboardRow{
		VerifiedAtSHA: "abcdef123456",
		SetRef: SetRef{
			RawStatus:   tasks.StatusReady,
			AutoDrain:   true,
			Orphaned:    true,
			Parked:      true,
			ConfigError: "no trunk",
		},
	}
	plain := dashboardStatusCell(multi)
	wantOrder := "· verified @ abcdef123456 · auto-drain · orphaned · parked · config error: no trunk"
	if !strings.Contains(plain, wantOrder) {
		t.Fatalf("multi-suffix cell = %q, want ordered suffixes %q", plain, wantOrder)
	}
	if styled := dashboardStatusCellStyled(multi); lipgloss.Width(styled) != lipgloss.Width(plain) {
		t.Fatalf("styled width %d != plain width %d (ANSI leaked into width math)", lipgloss.Width(styled), lipgloss.Width(plain))
	}
}

// withWayfinderMaps overlays wayfinder map folders onto a dashboard test Deps'
// mock FS so dashboardRowsForStatic can discover Map rows alongside Task sets.
func withWayfinderMaps(t *testing.T, d *Deps, storageDir string, files map[string]string) {
	t.Helper()
	fs := d.Tasks.FS.(*deps.MockFileSystem)
	origReadFile := fs.ReadFileFunc
	origReadDir := fs.ReadDirFunc
	fs.ReadDirFunc = func(path string) ([]os.DirEntry, error) {
		entries := mapDirEntries(path, files)
		if entries != nil {
			return entries, nil
		}
		if origReadDir != nil {
			return origReadDir(path)
		}
		return nil, os.ErrNotExist
	}
	fs.ReadFileFunc = func(path string) ([]byte, error) {
		if content, ok := files[path]; ok {
			return []byte(content), nil
		}
		if origReadFile != nil {
			return origReadFile(path)
		}
		return nil, os.ErrNotExist
	}
	_ = storageDir // storageDir is encoded in the file paths callers pass
}

func mapDirEntries(path string, files map[string]string) []os.DirEntry {
	children := map[string]bool{}
	for filePath := range files {
		if !strings.HasPrefix(filePath, path+string(os.PathSeparator)) && filePath != path {
			continue
		}
		rel := strings.TrimPrefix(filePath, path+string(os.PathSeparator))
		if rel == "" || rel == filePath {
			continue
		}
		parts := strings.Split(rel, string(os.PathSeparator))
		name := parts[0]
		children[name] = len(parts) > 1 || children[name]
	}
	if len(children) == 0 {
		return nil
	}
	var out []os.DirEntry
	for name, isDir := range children {
		out = append(out, deps.MockDirEntry{NameVal: name, IsDirVal: isDir})
	}
	return out
}

func TestDashboardMapRowsMixedAndFiltered(t *testing.T) {
	storageDir := "/data/repos/repo-aaaa"
	tasksDir := filepath.Join(storageDir, "tasks")
	activeMap := filepath.Join(storageDir, "wayfinder", "2026-07-01-active")
	doneMap := filepath.Join(storageDir, "wayfinder", "2026-07-02-done")
	abandonedMap := filepath.Join(storageDir, "wayfinder", "2026-07-03-abandoned")
	archivedMap := filepath.Join(storageDir, "wayfinder", "2026-07-04-archived")
	files := map[string]string{
		filepath.Join(activeMap, "map.md"): "Status: active\n\n## Destination\nShip it\n",
		filepath.Join(activeMap, "issues", "01-research.md"): "" +
			"Type: research\nStatus: open\n\n# Q\n",
		filepath.Join(activeMap, "issues", "02-blocked.md"): "" +
			"Type: research\nStatus: open\nBlocked by: 01\n\n# Q\n",
		filepath.Join(doneMap, "map.md"):                   "Status: done\n\n## Destination\nDone\n",
		filepath.Join(abandonedMap, "map.md"):              "Status: abandoned\n\n## Destination\nNope\n",
		filepath.Join(archivedMap, "map.md"):               "Status: active\n\n## Destination\nHidden\n",
		filepath.Join(storageDir, "wayfinder-archive.json"): `{"archived":["2026-07-04-archived"]}`,
	}

	rows := []tasks.Row{
		{ID: "2026-07-01-set-a", Status: tasks.StatusBlocked},
		{ID: "2026-07-01-set-b", Status: tasks.StatusReady},
	}
	d := dashboardTestDeps(t, rows, nil)
	withWayfinderMaps(t, d, storageDir, files)

	scan := projectScan{
		Name: "pop", ProjectPath: "/repo/main", RuntimePath: "/repo/main",
		DefinitionPath: tasksDir, RepoKey: "repo-key",
	}
	got, err := dashboardRowsForStatic(d, &config.Config{}, staticForScan(scan, "main", false))
	if err != nil {
		t.Fatal(err)
	}
	sortDashboardRows(got)

	var ids []string
	byID := map[string]DashboardRow{}
	for _, r := range got {
		ids = append(ids, r.SetID)
		byID[r.SetID] = r
	}
	// Active map appears; done/abandoned/archived do not.
	if !slices.Contains(ids, "2026-07-01-active") {
		t.Fatalf("missing active map row; got %v", ids)
	}
	for _, hidden := range []string{"2026-07-02-done", "2026-07-03-abandoned", "2026-07-04-archived"} {
		if slices.Contains(ids, hidden) {
			t.Fatalf("hidden map %q still present: %v", hidden, ids)
		}
	}
	// Both task sets present alongside the map.
	if !slices.Contains(ids, "2026-07-01-set-a") || !slices.Contains(ids, "2026-07-01-set-b") {
		t.Fatalf("missing task-set rows; got %v", ids)
	}

	mapRow := byID["2026-07-01-active"]
	if !mapRow.IsMap {
		t.Fatal("active map row IsMap = false")
	}
	if mapRow.Project != "pop" {
		t.Fatalf("map PROJECT = %q, want pop", mapRow.Project)
	}
	if mapRow.Worktree != "" {
		t.Fatalf("map WORKTREE = %q, want blank", mapRow.Worktree)
	}
	wantStatus := "WAYFINDING · 2 open / 1 frontier"
	if got := dashboardStatusCell(mapRow); got != wantStatus {
		t.Fatalf("map STATUS = %q, want %q", got, wantStatus)
	}

	// READY floats above rest; within the rest band the project groups map + blocked.
	wantOrder := []string{"2026-07-01-set-b", "2026-07-01-set-a", "2026-07-01-active"}
	if !reflect.DeepEqual(ids, wantOrder) {
		t.Fatalf("interleave order = %v, want %v", ids, wantOrder)
	}
}

func TestDashboardMapRowsInterleaveByProject(t *testing.T) {
	rows := []DashboardRow{
		{Project: "bravo", SetRef: SetRef{SetID: "2026-02-01-set", RawStatus: tasks.StatusBlocked}},
		{Project: "alpha", IsMap: true, SetRef: SetRef{SetID: "2026-02-01-map"}, MapOpen: 1, MapFrontier: 1},
		{Project: "alpha", SetRef: SetRef{SetID: "2026-02-01-set", RawStatus: tasks.StatusBlocked}},
		{Project: "bravo", IsMap: true, SetRef: SetRef{SetID: "2026-02-01-map"}, MapOpen: 0, MapFrontier: 0},
	}
	sortDashboardRows(rows)
	got := make([]string, len(rows))
	for i, r := range rows {
		kind := "set"
		if r.IsMap {
			kind = "map"
		}
		got[i] = r.Project + "/" + kind + "/" + r.SetID
	}
	// Rest band is per-project: alpha's map+set together, then bravo's.
	want := []string{
		"alpha/set/2026-02-01-set",
		"alpha/map/2026-02-01-map",
		"bravo/set/2026-02-01-set",
		"bravo/map/2026-02-01-map",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("project interleave = %v, want %v", got, want)
	}
}

func TestDashboardMapRowQueueVerbsInert(t *testing.T) {
	mapRow := DashboardRow{
		Project: "pop", IsMap: true, cursorKey: "pop\x00map\x00demo",
		SetRef: SetRef{SetID: "demo"}, MapOpen: 1, MapFrontier: 1,
	}
	setRow := DashboardRow{
		Project: "pop", cursorKey: "pop\x00set",
		SetRef: SetRef{RawStatus: tasks.StatusReady, SetID: "set", DefPath: "/repo/tasks", StatePath: "/repo/state.json"},
	}
	m := newQueueDashboard(&Deps{}, &config.Config{}, DashboardSnapshot{Rows: []DashboardRow{mapRow, setRow}})
	m.width, m.height = 120, 40
	m.list.SetCursor(0)

	if items := dashboardMenuItems(mapRow); len(items) != 0 {
		t.Fatalf("map menu items = %v, want empty", items)
	}
	updated, _ := m.Update(tea.KeyPressMsg{Code: 'a', Text: "a"})
	got := updated.(QueueDashboard)
	if got.menu != nil {
		t.Fatal("a on map row opened action menu")
	}

	// Former direct bind/unbind keys stay inert at top level on map rows too.
	for _, key := range []string{"b", "U"} {
		updated, _ = got.Update(tea.KeyPressMsg{Code: []rune(key)[0], Text: key})
		got = updated.(QueueDashboard)
		if got.bind != nil || got.abandon != nil || got.menu != nil {
			t.Fatalf("%s on map row opened a modal", key)
		}
	}

	// Set rows still open the action menu on a.
	got.list.SetCursor(1)
	updated, _ = got.Update(tea.KeyPressMsg{Code: 'a', Text: "a"})
	got = updated.(QueueDashboard)
	if got.menu == nil {
		t.Fatal("a on set row did not open action menu")
	}
}

func TestDashboardMapRowTwoLineRender(t *testing.T) {
	row := DashboardRow{
		Project: "pop", IsMap: true, SetRef: SetRef{SetID: "2026-07-01-wayfinding-map"},
		MapOpen: 3, MapFrontier: 2,
	}
	widths := dashboardTwoLineFitWidths(dashboardTwoLineNaturalWidths([]DashboardRow{row}), 120)
	line1 := dashboardTwoLineRowLine1(row, widths)
	line2 := dashboardTwoLineRowLine2(row, widths)
	if !strings.Contains(line1, "pop") || !strings.Contains(line1, "2026-07-01-wayfinding-map") {
		t.Fatalf("two-line line1 missing project/map id: %q", line1)
	}
	// WORKTREE blank: line1 should not carry needs-bind.
	if strings.Contains(line1, dashboardDestLabelNeedsBind) {
		t.Fatalf("two-line line1 shows needs bind for map: %q", line1)
	}
	if !strings.Contains(line2, "3 open / 2 frontier") {
		t.Fatalf("two-line line2 STATUS = %q", line2)
	}
	plain := dashboardStatusCell(row)
	if plain != "WAYFINDING · 3 open / 2 frontier" {
		t.Fatalf("map STATUS cell = %q", plain)
	}
	wantIndent := dashboardTwoLineStatusIndent(widths)
	if got := len(line2) - len(strings.TrimLeft(line2, " ")); got < wantIndent {
		t.Fatalf("two-line line2 indent = %d, want >= %d: %q", got, wantIndent, line2)
	}
}
