package work

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/glebglazov/pop/config"
	"github.com/glebglazov/pop/internal/deps"
	"github.com/glebglazov/pop/project"
	"github.com/glebglazov/pop/store"
	"github.com/glebglazov/pop/tasks"
	"github.com/glebglazov/pop/tasks/binding"
)

// These are the row-assembly derivation tests relocated in-package with the
// snapshot builder (ADR-0143): the public surface is snapshot-in, rows-out, so
// the unexported rowsForStatic / repoStaticFromMarker seams and their fixtures
// live here rather than queue-side.

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

// staticForScan builds the repoStatic that fork-free marker resolution would
// produce for a synthetic single-scan repo group, so tests express the
// integration target (rep) and its branch directly. A bare repo has no
// integration target: rep is nil.
func staticForScan(scan scanFixture, repBranch string, bare bool) repoStatic {
	var rep *repoScan
	if !bare {
		rep = &repoScan{Name: scan.Name, ProjectPath: scan.ProjectPath, RuntimePath: scan.RuntimePath}
	}
	storageDir := ""
	if scan.DefinitionPath != "" {
		storageDir = filepath.Dir(scan.DefinitionPath)
	}
	return repoStatic{
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

// TestBuildRowsVerifyFailedStatus confirms the build applies the same SHA-gated
// Verify overlay as `pop tasks status`, not manifest status alone.
func TestBuildRowsVerifyFailedStatus(t *testing.T) {
	enabled := &config.Config{Task: &config.TasksConfig{Verify: &config.VerifyConfig{Enabled: true}}}
	doneManifest := &tasks.Manifest{
		Valid: true,
		Tasks: []tasks.Task{{ID: "01-a", File: "01-a.md", Type: "AFK", Status: "done"}},
	}
	rows := []tasks.Row{{ID: "demo", Status: tasks.StatusDone}}
	td := workDataDeps(t)
	d := testDeps(t, rows)
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
	if StatusCell(got[0]) != "VERIFY-FAILED" {
		t.Fatalf("Status = %q, want VERIFY-FAILED", StatusCell(got[0]))
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
		ids = append(ids, row.SetID)
	}
	want := []string{"ready", "failed", "blocked", "deferred", "missing", "malformed"}
	if !reflect.DeepEqual(ids, want) {
		t.Fatalf("default ids = %v, want %v (both DONE sets hidden)", ids, want)
	}

	d.IncludeDone = true
	got, err = rowsForStatic(d, &config.Config{}, staticForScan(scan, "main", false))
	if err != nil {
		t.Fatal(err)
	}
	ids = nil
	byID := map[string]Row{}
	for _, row := range got {
		ids = append(ids, row.SetID)
		byID[row.SetID] = row
	}
	wantInclude := []string{"ready", "failed", "blocked", "deferred", "missing", "malformed", "done-integrating", "done-concluded"}
	if !reflect.DeepEqual(ids, wantInclude) {
		t.Fatalf("include-done ids = %v, want %v", ids, wantInclude)
	}
	for _, id := range []string{"done-integrating", "done-concluded"} {
		if got := byID[id]; !strings.HasPrefix(StatusCell(got), "DONE") {
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

	d.IncludeDone = true
	got, err := rowsForStatic(d, &config.Config{}, staticForScan(scan, "main", false))
	if err != nil {
		t.Fatal(err)
	}
	byID := map[string]Row{}
	for _, row := range got {
		byID[row.SetID] = row
	}
	if !strings.HasPrefix(StatusCell(byID["done"]), "DONE") || byID["done"].Worktree != "done-branch" || byID["done"].DestKind != DestDoneManagedBound {
		t.Fatalf("done row = %+v", byID["done"])
	}
	if !strings.HasPrefix(StatusCell(byID["ready"]), "READY") || byID["ready"].Worktree != DestLabelNeedsBind || byID["ready"].DestKind != DestNeedsBind {
		t.Fatalf("ready row = %+v", byID["ready"])
	}
	if byID["bound"].Worktree != "bound-branch" || byID["bound"].DestKind != DestBound {
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
	if len(got) != 1 || got[0].Worktree != DestLabelNeedsBind || got[0].DestKind != DestNeedsBind {
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
	byID := map[string]Row{}
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
	byID := map[string]Row{}
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
	// The orphaned set's STATUS cell carries the suffix; the others do not.
	if !strings.Contains(StatusCell(byID["missing"]), "· orphaned") {
		t.Fatalf("orphaned suffix missing from status cell: %q", StatusCell(byID["missing"]))
	}
	if strings.Contains(StatusCell(byID["present"]), "orphaned") || strings.Contains(StatusCell(byID["unbound"]), "orphaned") {
		t.Fatalf("non-orphaned rows must not carry the suffix")
	}
}

func dashboardBoolPtr(b bool) *bool { return &b }

// TestIntegrationTargetDerivedForkFree covers ADR-0060's integration target rules
// without forking git (a guard git fails the test on any static command): a
// non-bare repo's target is the main worktree (parent of the common dir) and
// needs no config; a bare repo's target is its config trunk; and a bare repo
// without a declared trunk surfaces a config-class error.
func TestIntegrationTargetDerivedForkFree(t *testing.T) {
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
		guard := &deps.MockGit{
			CommandInDirFunc: func(_ string, args ...string) (string, error) {
				return "", errors.New("unexpected git: " + strings.Join(args, " "))
			},
		}
		return &Deps{Tasks: &tasks.Deps{FS: fs, Git: guard}, Project: &project.Deps{FS: fs, Git: guard}}
	}

	t.Run("non-bare resolves target with no config", func(t *testing.T) {
		d := mkDeps()
		scans := []repoScan{{Name: "repo", ProjectPath: "/repo", RuntimePath: "/repo"}}
		st, err := repoStaticFromMarker(d, &config.Config{}, "/repo/.git", scans)
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
		scans := []repoScan{
			{Name: "repo/feat", ProjectPath: "/repo/feat", RuntimePath: "/repo/feat"},
			{Name: "repo/main", ProjectPath: "/repo/main", RuntimePath: "/repo/main"},
		}
		st, err := repoStaticFromMarker(d, cfg, "/repo/.bare", scans)
		if err != nil {
			t.Fatal(err)
		}
		if st.configErr != "" || st.rep == nil || st.rep.ProjectPath != "/repo/main" {
			t.Fatalf("bare+trunk static = %+v rep = %+v", st, st.rep)
		}
	})

	t.Run("bare without trunk surfaces config error", func(t *testing.T) {
		d := mkDeps()
		scans := []repoScan{{Name: "repo/feat", ProjectPath: "/repo/feat", RuntimePath: "/repo/feat"}}
		st, err := repoStaticFromMarker(d, &config.Config{}, "/repo/.bare", scans)
		if err != nil {
			t.Fatal(err)
		}
		if st.rep != nil || !st.bare || st.configErr == "" {
			t.Fatalf("bare-no-trunk static = %+v rep = %+v", st, st.rep)
		}
	})
}

// TestBareWithoutTrunkRendersConfigError covers ADR-0060's bare-without-trunk
// rule: an unbound set in such a repo shows a config-class error as a STATUS
// suffix and needs bind for its worktree, derived fork-free (no git probe).
func TestBareWithoutTrunkRendersConfigError(t *testing.T) {
	d := testDeps(t, []tasks.Row{{ID: "ready", Status: tasks.StatusReady, AutoDrain: true}})
	d.LiveDrains = func() ([]tasks.RunningDrain, error) { return nil, nil }
	st := repoStatic{
		defPath:     "/def",
		statePath:   tasks.StatePathFor("/def"),
		repoKey:     "bare-key",
		projectName: "bare",
		rep:         nil,
		bare:        true,
		configErr:   repoScanReason,
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
	if got[0].Worktree != DestLabelNeedsBind {
		t.Fatalf("row = %+v, want worktree %q", got[0], DestLabelNeedsBind)
	}
	wantSuffix := "· config error: " + repoScanReason
	if status := StatusCell(got[0]); !strings.Contains(status, wantSuffix) {
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

	st := repoStatic{
		defPath:     "/def",
		statePath:   tasks.StatePathFor("/def"),
		repoKey:     "repo-key",
		projectName: "pop",
		rep:         &repoScan{Name: "pop", ProjectPath: "/repo/main", RuntimePath: "/repo/main"},
		repBranch:   "trunk-branch",
		bare:        false,
	}
	got, err := rowsForStatic(d, &config.Config{}, st)
	if err != nil {
		t.Fatal(err)
	}
	byID := map[string]Row{}
	for _, row := range got {
		byID[row.SetID] = row
	}
	if byID["bound"].Worktree != "bound-branch" || byID["bound"].DestKind != DestBound {
		t.Fatalf("bound worktree = %+v, want binding-row branch", byID["bound"])
	}
	if byID["unbound"].Worktree != DestLabelNeedsBind || byID["unbound"].DestKind != DestNeedsBind {
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
	if got[0].Worktree != DestLabelManagedWt || got[0].DestKind != DestManagedDirective {
		t.Fatalf("managed row = %+v, want [managed wt] badge", got[0])
	}
	if label := WorktreeLabel(got[0].DestKind, got[0].Worktree); label != DestLabelManagedWt {
		t.Fatalf("worktree label = %q, want %q", label, DestLabelManagedWt)
	}
}

// TestDoneHiddenUniformly pins the ADR-0121 uniform DONE hide: a DONE set is
// omitted by default whether its Worktree binding is adopted or managed. Done
// inclusion reveals both, and the managed one still carries its clean-up
// DestKind.
func TestDoneHiddenUniformly(t *testing.T) {
	rows := []tasks.Row{
		{ID: "done-adopted", Status: tasks.StatusDone},
		{ID: "done-managed", Status: tasks.StatusDone},
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
	if len(got) != 0 {
		t.Fatalf("default rows = %+v, want both DONE sets hidden", got)
	}

	d.IncludeDone = true
	got, err = rowsForStatic(d, &config.Config{}, staticForScan(scan, "main", false))
	if err != nil {
		t.Fatal(err)
	}
	byID := map[string]Row{}
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
	} else if row.DestKind != DestDoneManagedBound || row.Worktree != "managed-branch" {
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
	if label := WorktreeLabel(got[0].DestKind, got[0].Worktree); !strings.Contains(label, "needs bind") {
		t.Fatalf("worktree label = %q, want needs bind", label)
	}
}

// TestHeadBranchFromCheckout covers ADR-0060's fork-free branch read: a main
// worktree's branch is parsed from <checkout>/.git/HEAD, a linked worktree's via
// its `.git` gitdir pointer, a detached HEAD yields "", and the common-dir
// fallback applies when there is no `.git` entry.
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

func TestMapRowsMixedAndFiltered(t *testing.T) {
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
		filepath.Join(doneMap, "map.md"):                    "Status: done\n\n## Destination\nDone\n",
		filepath.Join(abandonedMap, "map.md"):               "Status: abandoned\n\n## Destination\nNope\n",
		filepath.Join(archivedMap, "map.md"):                "Status: active\n\n## Destination\nHidden\n",
		filepath.Join(storageDir, "wayfinder-archive.json"): `{"archived":["2026-07-04-archived"]}`,
	}

	rows := []tasks.Row{
		{ID: "2026-07-01-set-a", Status: tasks.StatusBlocked},
		{ID: "2026-07-01-set-b", Status: tasks.StatusReady},
	}
	d := testDeps(t, rows)
	withWayfinderMaps(t, d, storageDir, files)

	scan := scanFixture{
		Name: "pop", ProjectPath: "/repo/main", RuntimePath: "/repo/main",
		DefinitionPath: tasksDir, RepoKey: "repo-key",
	}
	got, err := rowsForStatic(d, &config.Config{}, staticForScan(scan, "main", false))
	if err != nil {
		t.Fatal(err)
	}
	SortRows(got)

	var ids []string
	byID := map[string]Row{}
	for _, r := range got {
		ids = append(ids, r.SetID)
		byID[r.SetID] = r
	}
	if !slices.Contains(ids, "2026-07-01-active") {
		t.Fatalf("missing active map row; got %v", ids)
	}
	for _, hidden := range []string{"2026-07-02-done", "2026-07-03-abandoned", "2026-07-04-archived"} {
		if slices.Contains(ids, hidden) {
			t.Fatalf("hidden map %q still present: %v", hidden, ids)
		}
	}
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
	if got := StatusCell(mapRow); got != wantStatus {
		t.Fatalf("map STATUS = %q, want %q", got, wantStatus)
	}

	wantOrder := []string{"2026-07-01-set-b", "2026-07-01-set-a", "2026-07-01-active"}
	if !reflect.DeepEqual(ids, wantOrder) {
		t.Fatalf("interleave order = %v, want %v", ids, wantOrder)
	}
}

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
	_ = storageDir
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
