package setkind

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/glebglazov/pop/config"
	"github.com/glebglazov/pop/internal/deps"
	"github.com/glebglazov/pop/project"
	"github.com/glebglazov/pop/repogroup"
	"github.com/glebglazov/pop/store"
	"github.com/glebglazov/pop/tasks"
	"github.com/glebglazov/pop/tasks/binding"
	"github.com/glebglazov/pop/work"
)

// These are the container-assembly derivation tests, relocated with the loader
// they drive when the Work seam took shape: the kind's public surface is
// kinds-in, containers-out, so the unexported containersForGroup seam and its
// fixtures live beside the adapter. Assertions read each container's row
// projection, which is what the dashboard still renders.

// rowsForStatic renders one hand-built repo group's containers and hands back
// their row projections — the shape these tests were written against.
func rowsForStatic(d *Deps, cfg *config.Config, g repogroup.Group) ([]work.Container, error) {
	containers, err := containersForGroup(d, cfg, g)
	if err != nil {
		return nil, err
	}
	rows := make([]work.Container, 0, len(containers))
	for _, c := range containers {
		rows = append(rows, c)
	}
	return rows, nil
}

func mkdirDrainStoreDir(t *testing.T, td *tasks.Deps) {
	t.Helper()
	dir := filepath.Dir(tasks.DrainStorePathWith(td))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("MkdirAll drain store dir: %v", err)
	}
}

// workDataDeps returns tasks.Deps backed by a temp XDG data dir so store touches
// stay isolated per test (mirrors queue's queueDataDeps).
func workDataDeps(t *testing.T) *tasks.Deps {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("XDG_DATA_HOME", dir)
	real := deps.NewRealFileSystem()
	d := tasks.DefaultDeps()
	d.FS = &deps.MockFileSystem{
		GetenvFunc: func(key string) string {
			if key == "XDG_DATA_HOME" {
				return dir
			}
			return ""
		},
		ReadFileFunc:  real.ReadFile,
		WriteFileFunc: real.WriteFile,
		MkdirAllFunc:  real.MkdirAll,
		RenameFunc:    real.Rename,
		RemoveAllFunc: real.RemoveAll,
	}
	t.Cleanup(func() { _ = d.CloseStore() })
	return d
}

func testDeps(t *testing.T, rows []tasks.Row) *Deps {
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
			return "", errors.New("unexpected git command: " + strings.Join(args, " "))
		},
	}
	tasksDeps := &tasks.Deps{FS: fs, Git: git}
	t.Cleanup(func() { _ = tasksDeps.CloseStore() })
	return &Deps{
		Tasks:   tasksDeps,
		Project: &project.Deps{FS: fs, Git: git},
		Refresh: func(string) (*tasks.RefreshResult, error) {
			return &tasks.RefreshResult{Rows: rows}, nil
		},
	}
}

func seedBindingStore(t *testing.T, td *tasks.Deps, bindings map[string]binding.Binding) {
	t.Helper()
	existing, err := binding.AllBindings(td)
	if err != nil {
		t.Fatal(err)
	}
	for key := range existing {
		if err := binding.Delete(td, key); err != nil {
			t.Fatal(err)
		}
	}
	for key, b := range bindings {
		if err := binding.Put(td, key, b); err != nil {
			t.Fatal(err)
		}
	}
}

// scanFixture is the queue projectScan's minimal derivation slice the fixtures
// carry (the fork-free static resolution reads only these).
type scanFixture struct {
	Name           string
	ProjectPath    string
	RuntimePath    string
	DefinitionPath string
	RepoKey        string
	RepoCommonDir  string
}

// staticForScan builds the repogroup.Group that fork-free marker resolution would
// produce for a synthetic single-scan repo group, so tests express the
// integration target (rep) and its branch directly. A bare repo has no
// integration target: rep is nil.
func staticForScan(scan scanFixture, repBranch string, bare bool) repogroup.Group {
	var rep *repogroup.Checkout
	if !bare {
		rep = &repogroup.Checkout{Name: scan.Name, ProjectPath: scan.ProjectPath, RuntimePath: scan.RuntimePath}
	}
	storageDir := ""
	if scan.DefinitionPath != "" {
		storageDir = filepath.Dir(scan.DefinitionPath)
	}
	return repogroup.Group{
		DefPath:       scan.DefinitionPath,
		StatePath:     tasks.StatePathFor(scan.DefinitionPath),
		StorageDir:    storageDir,
		RepoKey:       scan.RepoKey,
		RepoCommonDir: scan.RepoCommonDir,
		ProjectName:   scan.Name,
		Rep:           rep,
		Branch:        repBranch,
		Bare:          bare,
	}
}
func TestBuildRowsVerifyFailedStatus(t *testing.T) {
	enabled := &config.Config{Work: &config.WorkConfig{Verify: &config.VerifyConfig{Enabled: true}}}
	doneManifest := &tasks.Manifest{
		Valid: true,
		Tasks: []tasks.Task{{ID: "01-a", File: "01-a.md", Type: "AFK", Status: "done"}},
	}
	rows := []tasks.Row{{ID: "demo", Status: tasks.StatusDone}}
	td := workDataDeps(t)
	d := testDeps(t, rows)
	// The subject here is the VERIFY-FAILED status cell, so the row has to be on
	// screen: a DONE row hidden by the filter is never asked for its verdict
	// (ADR-0189), which TestVerdictsResolveOnlyForRenderedRows covers instead.
	d.ViewPreset, _ = config.ShippedWorkViewPreset("all")
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
	runtime := t.TempDir()
	seedBindingStore(t, td, map[string]binding.Binding{
		binding.ScopedKey("repo-key", "demo"): {RuntimePath: runtime, Branch: "main"},
	})
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

	scan := scanFixture{
		Name:           "pop",
		ProjectPath:    "/repo/main",
		DefinitionPath: "/def",
		RepoKey:        "repo-key",
		RepoCommonDir:  "/repo/.git",
	}
	got, err := rowsForStatic(d, enabled, staticForScan(scan, "main", false))
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("rows = %+v, want one", got)
	}
	if got[0].RawStatus != tasks.StatusVerifyFailed {
		t.Fatalf("RawStatus = %q, want VERIFY-FAILED", got[0].RawStatus)
	}
	if cell := tasks.WorkRowStatusCell(got[0]); !strings.HasPrefix(cell, "VERIFY-FAILED") {
		t.Fatalf("Status = %q, want VERIFY-FAILED prefix", cell)
	}
}

// TestBuildRowsUnplacedSkipsTrunkVerdict confirms an unbound Done set keeps its
// manifest status and empty RuntimePath — a trunk HEAD verdict must not stand in
// for an unplaced set (ADR-0147).
func TestBuildRowsUnplacedSkipsTrunkVerdict(t *testing.T) {
	enabled := &config.Config{Work: &config.WorkConfig{Verify: &config.VerifyConfig{Enabled: true}}}
	doneManifest := &tasks.Manifest{
		Valid: true,
		Tasks: []tasks.Task{{ID: "01-a", File: "01-a.md", Type: "AFK", Status: "done"}},
	}
	rows := []tasks.Row{{ID: "unplaced", Status: tasks.StatusDone}}
	td := workDataDeps(t)
	d := testDeps(t, rows)
	d.ViewPreset, _ = config.ShippedWorkViewPreset("all")
	d.Tasks = td
	d.Refresh = func(string) (*tasks.RefreshResult, error) {
		return &tasks.RefreshResult{
			Rows:      rows,
			Manifests: map[string]*tasks.Manifest{"unplaced": doneManifest},
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
		Repo: "/repo/.git", SetID: "unplaced", WorkSHA: "shaCUR", Verdict: "NEEDS-HUMAN", Findings: "trunk-only",
	}); err != nil {
		t.Fatalf("PutVerifyVerdict: %v", err)
	}
	_ = s.Close()

	scan := scanFixture{
		Name:           "pop",
		ProjectPath:    "/repo/main",
		DefinitionPath: "/def",
		RepoKey:        "repo-key",
		RepoCommonDir:  "/repo/.git",
	}
	got, err := rowsForStatic(d, enabled, staticForScan(scan, "main", false))
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("rows = %+v, want one", got)
	}
	row := got[0]
	if row.Bound || row.RuntimePath != "" {
		t.Fatalf("unplaced row Bound=%v RuntimePath=%q, want unbound with no checkout", row.Bound, row.RuntimePath)
	}
	if row.DestKind != work.DestNeedsBind || row.Worktree != work.DestLabelNeedsBind {
		t.Fatalf("unplaced dest = kind=%v label=%q, want needs-bind", row.DestKind, row.Worktree)
	}
	if row.RawStatus != tasks.StatusDone {
		t.Fatalf("RawStatus = %q, want DONE (trunk verdict must not overlay an unplaced set)", row.RawStatus)
	}
}

func TestShowRuleFiltering(t *testing.T) {
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
	d := testDeps(t, rows)
	d.Tasks = withDataDir(t, d.Tasks)
	seedBindingStore(t, d.Tasks, map[string]binding.Binding{
		binding.ScopedKey("repo-key", "done-integrating"): {RuntimePath: "/repo/done", Branch: "done-branch", Provisioned: true},
	})
	scan := scanFixture{Name: "pop", ProjectPath: "/repo/main", RuntimePath: "/repo/main", DefinitionPath: "/def", RepoKey: "repo-key"}

	got, err := rowsForStatic(d, &config.Config{}, staticForScan(scan, "main", false))
	if err != nil {
		t.Fatal(err)
	}
	var ids []string
	for _, row := range got {
		ids = append(ids, row.ID)
	}
	want := []string{"ready", "failed", "blocked", "deferred", "missing", "malformed", "done-integrating"}
	if !reflect.DeepEqual(ids, want) {
		t.Fatalf("default ids = %v, want %v (folded DONE hidden; unfolded DONE shown)", ids, want)
	}

	d.ViewPreset, _ = config.ShippedWorkViewPreset("all")
	got, err = rowsForStatic(d, &config.Config{}, staticForScan(scan, "main", false))
	if err != nil {
		t.Fatal(err)
	}
	ids = nil
	byID := map[string]work.Container{}
	for _, row := range got {
		ids = append(ids, row.ID)
		byID[row.ID] = row
	}
	wantInclude := []string{"ready", "failed", "blocked", "deferred", "missing", "malformed", "done-integrating", "done-concluded"}
	if !reflect.DeepEqual(ids, wantInclude) {
		t.Fatalf("include-done ids = %v, want %v", ids, wantInclude)
	}
	for _, id := range []string{"done-integrating", "done-concluded"} {
		if got := byID[id]; !strings.HasPrefix(tasks.WorkRowStatusCell(got), "DONE") {
			t.Fatalf("%s row = %+v, want DONE", id, got)
		}
	}
	if !byID["done-integrating"].DoneStillManagedBound {
		t.Fatalf("done-integrating should record DoneStillManagedBound")
	}
}

func TestColumnDerivation(t *testing.T) {
	rows := []tasks.Row{
		{ID: "done", Status: tasks.StatusDone},
		{ID: "ready", Status: tasks.StatusReady, AutoDrain: true},
		{ID: "bound", Status: tasks.StatusBlocked},
	}
	d := testDeps(t, rows)
	d.Tasks = withDataDir(t, d.Tasks)
	seedBindingStore(t, d.Tasks, map[string]binding.Binding{
		binding.ScopedKey("repo-key", "done"):  {RuntimePath: "/repo/done", Branch: "done-branch", Provisioned: true},
		binding.ScopedKey("repo-key", "bound"): {RuntimePath: "/repo/bound", Branch: "bound-branch"},
	})
	scan := scanFixture{Name: "pop", ProjectPath: "/repo/main", RuntimePath: "/repo/main", DefinitionPath: "/def", RepoKey: "repo-key"}

	d.ViewPreset, _ = config.ShippedWorkViewPreset("all")
	got, err := rowsForStatic(d, &config.Config{}, staticForScan(scan, "main", false))
	if err != nil {
		t.Fatal(err)
	}
	byID := map[string]work.Container{}
	for _, row := range got {
		byID[row.ID] = row
	}
	if !strings.HasPrefix(tasks.WorkRowStatusCell(byID["done"]), "DONE") || byID["done"].Worktree != "done-branch" || byID["done"].DestKind != work.DestDoneManagedBound {
		t.Fatalf("done row = %+v", byID["done"])
	}
	if !strings.HasPrefix(tasks.WorkRowStatusCell(byID["ready"]), "READY") || byID["ready"].Worktree != work.DestLabelNeedsBind || byID["ready"].DestKind != work.DestNeedsBind {
		t.Fatalf("ready row = %+v", byID["ready"])
	}
	if byID["bound"].Worktree != "bound-branch" || byID["bound"].DestKind != work.DestBound {
		t.Fatalf("bound row = %+v", byID["bound"])
	}
}

func TestNoBaseWorktree(t *testing.T) {
	d := testDeps(t, []tasks.Row{{ID: "missing", Status: tasks.StatusMissing}})
	scan := scanFixture{Name: "bare", ProjectPath: "/repo/bare.git", RuntimePath: "/repo/bare.git", DefinitionPath: "/def", RepoKey: "bare-key"}

	got, err := rowsForStatic(d, &config.Config{}, staticForScan(scan, "", true))
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Worktree != work.DestLabelNeedsBind || got[0].DestKind != work.DestNeedsBind {
		t.Fatalf("rows = %+v, want needs bind", got)
	}
}

func TestPickedUpIndicator(t *testing.T) {
	d := testDeps(t, []tasks.Row{
		{ID: "ready", Status: tasks.StatusReady, AutoDrain: true},
		{ID: "other", Status: tasks.StatusReady, AutoDrain: true},
	})
	d.LiveDrains = func() ([]tasks.RunningDrain, error) {
		return []tasks.RunningDrain{
			{RuntimePath: "/repo/bound", SetID: "ready", PID: 123},
		}, nil
	}
	d.Tasks = withDataDir(t, d.Tasks)
	seedBindingStore(t, d.Tasks, map[string]binding.Binding{
		binding.ScopedKey("repo-key", "ready"): {RuntimePath: "/repo/bound", Branch: "ready-branch"},
	})
	scan := scanFixture{Name: "pop", ProjectPath: "/repo/main", RuntimePath: "/repo/main", DefinitionPath: "/def", RepoKey: "repo-key"}

	got, err := rowsForStatic(d, &config.Config{}, staticForScan(scan, "main", false))
	if err != nil {
		t.Fatal(err)
	}
	byID := map[string]work.Container{}
	for _, row := range got {
		byID[row.ID] = row
	}
	if !byID["ready"].LiveDrain {
		t.Fatalf("ready LiveDrain = false, want true (held by a live drain)")
	}
	if byID["other"].LiveDrain {
		t.Fatalf("other LiveDrain = true, want false")
	}
}

// TestOrphanedIndicator covers the three orphaned-detection cases: a set whose
// bound checkout is missing on disk is orphaned; a set whose bound checkout still
// stats present is not; and a set with no binding can never be orphaned.
// Detection is a filesystem stat only — the mocked Git would error on any
// command, so this also asserts the build adds no git subprocess.
func TestOrphanedIndicator(t *testing.T) {
	rows := []tasks.Row{
		{ID: "present", Status: tasks.StatusBlocked},
		{ID: "missing", Status: tasks.StatusBlocked},
		{ID: "unbound", Status: tasks.StatusBlocked},
	}
	d := testDeps(t, rows)
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
	seedBindingStore(t, d.Tasks, map[string]binding.Binding{
		binding.ScopedKey("repo-key", "present"): {RuntimePath: presentPath, Branch: "present-branch"},
		binding.ScopedKey("repo-key", "missing"): {RuntimePath: "/repo/gone", Branch: "missing-branch"},
	})
	scan := scanFixture{Name: "pop", ProjectPath: "/repo/main", RuntimePath: "/repo/main", DefinitionPath: "/def", RepoKey: "repo-key"}

	got, err := rowsForStatic(d, &config.Config{}, staticForScan(scan, "main", false))
	if err != nil {
		t.Fatal(err)
	}
	byID := map[string]work.Container{}
	for _, row := range got {
		byID[row.ID] = row
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
	// The orphaned set's STATUS cell carries the suffix; the others do not.
	if !strings.Contains(tasks.WorkRowStatusCell(byID["missing"]), "· orphaned") {
		t.Fatalf("orphaned suffix missing from status cell: %q", tasks.WorkRowStatusCell(byID["missing"]))
	}
	if strings.Contains(tasks.WorkRowStatusCell(byID["present"]), "orphaned") || strings.Contains(tasks.WorkRowStatusCell(byID["unbound"]), "orphaned") {
		t.Fatalf("non-orphaned rows must not carry the suffix")
	}
}

// TestBareWithoutTrunkRendersConfigError covers ADR-0060's bare-without-trunk
// rule: an unbound set in such a repo shows a config-class error as a STATUS
// suffix and needs bind for its worktree, derived fork-free (no git probe).
func TestBareWithoutTrunkRendersConfigError(t *testing.T) {
	d := testDeps(t, []tasks.Row{{ID: "ready", Status: tasks.StatusReady, AutoDrain: true}})
	d.LiveDrains = func() ([]tasks.RunningDrain, error) { return nil, nil }
	st := repogroup.Group{
		DefPath:     "/def",
		StatePath:   tasks.StatePathFor("/def"),
		RepoKey:     "bare-key",
		ProjectName: "bare",
		Rep:         nil,
		Bare:        true,
		ConfigError: repogroup.ScanReason,
	}
	got, err := rowsForStatic(d, &config.Config{}, st)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("rows = %+v, want one", got)
	}
	if got[0].LiveDrain {
		t.Fatalf("row LiveDrain = true, want false (config error row is not a live drain)")
	}
	if got[0].Worktree != work.DestLabelNeedsBind {
		t.Fatalf("row = %+v, want worktree %q", got[0], work.DestLabelNeedsBind)
	}
	wantSuffix := "· config error: " + repogroup.ScanReason
	if status := tasks.WorkRowStatusCell(got[0]); !strings.Contains(status, wantSuffix) {
		t.Fatalf("status = %q, want config error suffix %q", status, wantSuffix)
	}
}

// TestBranchColumnSources covers ADR-0070/0072 destination rules: a bound set
// shows its binding-row branch plainly; an unbound set with no directive shows
// needs bind.
func TestBranchColumnSources(t *testing.T) {
	d := testDeps(t, []tasks.Row{
		{ID: "bound", Status: tasks.StatusBlocked},
		{ID: "unbound", Status: tasks.StatusReady, AutoDrain: true},
	})
	d.LiveDrains = func() ([]tasks.RunningDrain, error) { return nil, nil }
	d.Tasks = withDataDir(t, d.Tasks)
	seedBindingStore(t, d.Tasks, map[string]binding.Binding{
		binding.ScopedKey("repo-key", "bound"): {RuntimePath: "/repo/bound", Branch: "bound-branch"},
	})

	st := repogroup.Group{
		DefPath:     "/def",
		StatePath:   tasks.StatePathFor("/def"),
		RepoKey:     "repo-key",
		ProjectName: "pop",
		Rep:         &repogroup.Checkout{Name: "pop", ProjectPath: "/repo/main", RuntimePath: "/repo/main"},
		Branch:      "trunk-branch",
		Bare:        false,
	}
	got, err := rowsForStatic(d, &config.Config{}, st)
	if err != nil {
		t.Fatal(err)
	}
	byID := map[string]work.Container{}
	for _, row := range got {
		byID[row.ID] = row
	}
	if byID["bound"].Worktree != "bound-branch" || byID["bound"].DestKind != work.DestBound {
		t.Fatalf("bound worktree = %+v, want binding-row branch", byID["bound"])
	}
	if byID["unbound"].Worktree != work.DestLabelNeedsBind || byID["unbound"].DestKind != work.DestNeedsBind {
		t.Fatalf("unbound worktree = %+v, want needs bind", byID["unbound"])
	}
}

func TestManagedDirectiveDestColumn(t *testing.T) {
	rows := []tasks.Row{
		{ID: "managed", Status: tasks.StatusReady, AutoDrain: true},
	}
	d := testDeps(t, rows)
	d.Tasks = withDataDir(t, d.Tasks)
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
	scan := scanFixture{Name: "pop", ProjectPath: "/repo/main", RuntimePath: "/repo/main", DefinitionPath: defPath, RepoKey: "repo-key"}

	got, err := rowsForStatic(d, &config.Config{}, staticForScan(scan, "main", false))
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("rows = %+v, want one managed-directive row", got)
	}
	if got[0].Worktree != work.DestLabelManagedWt || got[0].DestKind != work.DestManagedDirective {
		t.Fatalf("managed row = %+v, want [managed wt] badge", got[0])
	}
	if label := work.WorktreeLabel(got[0].DestKind, got[0].Worktree); label != work.DestLabelManagedWt {
		t.Fatalf("worktree label = %q, want %q", label, work.DestLabelManagedWt)
	}
}

// TestDoneHiddenUniformly pins the ADR-0121 uniform DONE hide: a DONE set is
// omitted by default whether its Worktree binding is adopted or managed. Done
// TestDoneVisibleWhenUnfolded pins ADR-0197's active preset: a DONE set that
// still holds a managed binding (unfolded) stays on the default view as the
// teardown reminder; an adopted DONE binding is not unfolded and is hidden;
// the all preset still reveals every DONE row including folded ones.
func TestDoneVisibleWhenUnfolded(t *testing.T) {
	rows := []tasks.Row{
		{ID: "done-adopted", Status: tasks.StatusDone},
		{ID: "done-managed", Status: tasks.StatusDone},
		{ID: "done-folded", Status: tasks.StatusDone},
	}
	d := testDeps(t, rows)
	d.Tasks = withDataDir(t, d.Tasks)
	seedBindingStore(t, d.Tasks, map[string]binding.Binding{
		binding.ScopedKey("repo-key", "done-adopted"): {RuntimePath: "/repo/adopted", Branch: "adopted-branch"},
		binding.ScopedKey("repo-key", "done-managed"): {RuntimePath: "/repo/managed", Branch: "managed-branch", Provisioned: true},
	})
	scan := scanFixture{Name: "pop", ProjectPath: "/repo/main", RuntimePath: "/repo/main", DefinitionPath: "/def", RepoKey: "repo-key"}

	got, err := rowsForStatic(d, &config.Config{}, staticForScan(scan, "main", false))
	if err != nil {
		t.Fatal(err)
	}
	byID := map[string]work.Container{}
	for _, row := range got {
		byID[row.ID] = row
	}
	if _, ok := byID["done-folded"]; ok {
		t.Fatalf("folded DONE shown by active: %+v", got)
	}
	if _, ok := byID["done-adopted"]; ok {
		t.Fatalf("adopted DONE shown by active (not unfolded): %+v", got)
	}
	if row, ok := byID["done-managed"]; !ok {
		t.Fatal("managed Done binding should stay visible under active")
	} else if row.DestKind != work.DestDoneManagedBound || row.Worktree != "managed-branch" {
		t.Fatalf("done-managed row = %+v", row)
	} else if !row.Provisioned || !strings.Contains(tasks.WorkRowStatusCell(row), tasks.UnfoldedMark) {
		t.Fatalf("done-managed should read unfolded: %+v status=%q", row, tasks.WorkRowStatusCell(row))
	}

	d.ViewPreset, _ = config.ShippedWorkViewPreset("all")
	got, err = rowsForStatic(d, &config.Config{}, staticForScan(scan, "main", false))
	if err != nil {
		t.Fatal(err)
	}
	byID = map[string]work.Container{}
	for _, row := range got {
		byID[row.ID] = row
	}
	if _, ok := byID["done-folded"]; !ok {
		t.Fatal("folded DONE should be revealed with preset all")
	}
	if row, ok := byID["done-adopted"]; !ok {
		t.Fatal("adopted Done binding should be revealed with preset all")
	} else if row.Worktree != "adopted-branch" {
		t.Fatalf("done-adopted row = %+v", row)
	} else if strings.Contains(tasks.WorkRowStatusCell(row), tasks.UnfoldedMark) {
		t.Fatalf("adopted DONE must not read unfolded: %q", tasks.WorkRowStatusCell(row))
	}
	if row, ok := byID["done-managed"]; !ok {
		t.Fatal("managed Done binding should be revealed with preset all")
	} else if row.DestKind != work.DestDoneManagedBound || row.Worktree != "managed-branch" {
		t.Fatalf("done-managed row = %+v", row)
	}
}

func TestNeedsBindLabel(t *testing.T) {
	d := testDeps(t, []tasks.Row{{ID: "plain", Status: tasks.StatusReady}})
	scan := scanFixture{Name: "pop", ProjectPath: "/repo/main", RuntimePath: "/repo/main", DefinitionPath: "/def", RepoKey: "repo-key"}
	got, err := rowsForStatic(d, &config.Config{}, staticForScan(scan, "main", false))
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("rows = %+v, want one", got)
	}
	if label := work.WorktreeLabel(got[0].DestKind, got[0].Worktree); !strings.Contains(label, "needs bind") {
		t.Fatalf("worktree label = %q, want needs bind", label)
	}
}

// withDataDir rewraps a tasks.Deps' mock FS with a temp XDG data dir and real
// file ops so the binding store can be seeded and read, preserving the original
// symlink/stat seams. It mirrors the FS-swap the relocated tests did inline.
func withDataDir(t *testing.T, td *tasks.Deps) *tasks.Deps {
	t.Helper()
	dataHome := t.TempDir()
	real := deps.NewRealFileSystem()
	origFS := td.FS.(*deps.MockFileSystem)
	td.FS = &deps.MockFileSystem{
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
	return td
}

// TestLessThreadsPresetSort proves the Task-set kind's Less is the shared
// WorkRowLess comparator with ViewPreset.Sort threaded through (ADR-0197):
// dashboard BuildSnapshot and pop work status therefore order identically.
func TestLessThreadsPresetSort(t *testing.T) {
	older := work.Container{Project: "p", ID: "2026-01-01-old", RawStatus: tasks.StatusBlocked}
	newer := work.Container{Project: "p", ID: "2026-06-01-new", RawStatus: tasks.StatusReady}
	live := work.Container{Project: "p", ID: "2026-02-01-run", RawStatus: tasks.StatusDone, LiveDrain: true}

	statusKind := New(&Deps{})
	if !statusKind.Less(newer, older) {
		// Under the status scheme READY floats above BLOCKED regardless of date.
		t.Fatal("empty-sort Less: READY should precede BLOCKED")
	}
	if !statusKind.Less(live, older) {
		// Under the status scheme a live-drained DONE set reads IN PROGRESS, which is
		// the leading band — the band, not a tier, is what puts it first.
		t.Fatal("empty-sort Less: IN PROGRESS should precede BLOCKED")
	}

	recent, _ := config.ShippedWorkViewPreset("recent-30d")
	recencyKind := New(&Deps{ViewPreset: recent})
	if !recencyKind.Less(newer, older) || recencyKind.Less(older, newer) {
		t.Fatal("created_desc Less: newer id must precede older")
	}
	if recencyKind.Less(live, newer) {
		t.Fatal("created_desc Less: a live drain is ordered by its date, not lifted")
	}
	// Kind.Less and SortWorkRows must agree — one comparator.
	rows := []work.Container{older, newer, live}
	tasks.SortWorkRows(rows, recent.Sort)
	if rows[0].ID != newer.ID || rows[1].ID != live.ID || rows[2].ID != older.ID {
		t.Fatalf("SortWorkRows(%q) = %v/%v/%v, want newer/live/older", recent.Sort, rows[0].ID, rows[1].ID, rows[2].ID)
	}
	if !recencyKind.Less(rows[0], rows[1]) || !recencyKind.Less(rows[1], rows[2]) {
		t.Fatal("Kind.Less disagrees with SortWorkRows under recent-30d")
	}
}
