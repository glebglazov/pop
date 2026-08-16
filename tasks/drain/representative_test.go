package drain

import (
	"github.com/glebglazov/pop/internal/queuetest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/glebglazov/pop/config"
	"github.com/glebglazov/pop/project"
	"github.com/glebglazov/pop/tasks"
)

// seedTaskStorage writes the repo.json marker and a task-set directory for the
// repository containing repoPath so Scan's storage-scoped partition (ADR-0060)
// discovers it. The set directory only needs to exist for discovery; callers that
// fake Refresh supply the set's rows separately.
func seedTaskStorage(t *testing.T, td *tasks.Deps, repoPath, setID string) {
	t.Helper()
	id, err := tasks.ResolveRepositoryIdentity(td, repoPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := tasks.EnsureStorage(td, id); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(id.TasksDir, setID), 0o755); err != nil {
		t.Fatal(err)
	}
}

// initNonBareRepoWithLinkedWorktrees creates a normal repo (the git main
// worktree) and adds n linked worktrees beside it.
func initNonBareRepoWithLinkedWorktrees(t *testing.T, n int) (string, []string) {
	t.Helper()
	main := initGitRepoWithBase(t)
	parent := t.TempDir()
	var linked []string
	for i := 0; i < n; i++ {
		wt := filepath.Join(parent, "linked"+string(rune('0'+i)))
		runGit(t, main, "worktree", "add", "--detach", wt, "HEAD")
		linked = append(linked, wt)
	}
	return main, linked
}

func repoDispatchDeps(t *testing.T, ready []tasks.Row, locks map[string]*tasks.RuntimeLockStatus) *Deps {
	t.Helper()
	return &Deps{
		Tasks:   queuetest.TasksDeps(t, true),
		Project: project.DefaultDeps(),
		ReadLock: func(runtimePath string) *tasks.RuntimeLockStatus {
			if locks != nil {
				if l, ok := locks[runtimePath]; ok {
					return l
				}
			}
			return queuetest.IdleLock(runtimePath)
		},
		Refresh: func(string) (*tasks.RefreshResult, error) {
			return &tasks.RefreshResult{Rows: ready}, nil
		},
	}
}

func scansForCheckouts(checkouts []string, defPath string) []projectScan {
	scans := make([]projectScan, 0, len(checkouts))
	for i, c := range checkouts {
		scans = append(scans, projectScan{
			Name:           "repo/wt" + string(rune('0'+i)),
			ProjectPath:    c,
			RuntimePath:    c,
			DefinitionPath: defPath,
		})
	}
	return scans
}

// trunkPtr states a repository's Trunk worktree as the path value it is.
func trunkPtr(path string) *config.TrunkPath {
	p := config.TrunkPath(path)
	return &p
}

func TestParseGitMainWorktree(t *testing.T) {
	bare := "worktree /repo/bare.git\nbare\n\nworktree /repo/bare.git/wt0\nHEAD abc\ndetached\n"
	if path, isBare := parseGitMainWorktree(bare); !isBare || path != "" {
		t.Fatalf("bare repo: got (%q, %v), want (\"\", true)", path, isBare)
	}

	nonBare := "worktree /repo/main\nHEAD abc\nbranch refs/heads/master\n\nworktree /repo/linked\nHEAD def\nbranch refs/heads/feature\n"
	if path, isBare := parseGitMainWorktree(nonBare); isBare || path != "/repo/main" {
		t.Fatalf("non-bare repo: got (%q, %v), want (/repo/main, false)", path, isBare)
	}
}

func TestDecideRepoDispatchesBareMultiWorktreeCollapsesToOneDrain(t *testing.T) {
	_, wts := queuetest.InitBareRepoWithWorktrees(t, 3)
	d := repoDispatchDeps(t, []tasks.Row{{ID: "top", Status: tasks.StatusReady, AutoDrain: true, Priority: 1}}, nil)

	// trunk override pins the repo's Trunk worktree to the first worktree.
	cfg := &config.Config{Repo: map[string]config.RepoOverrideConfig{
		wts[0]: {Trunk: trunkPtr(wts[0])},
	}}
	// The set is bound to the trunk checkout so it is Queue-drainable (ADR-0072);
	// this guards the multi-worktree collapse to a single drain, not the routing
	// default the integration target once provided (ADR-0070).
	bindSetInPlace(t, d, wts[0], "top")
	scans := scansForCheckouts(wts, "/def")

	decisions := decideRepoDispatches(d, cfg, scans, time.Now())

	var actionable []Decision
	for _, dec := range decisions {
		if dec.Actionable() {
			actionable = append(actionable, dec)
		}
		if dec.Reason == RepoScanReason {
			t.Fatalf("a repo with a trunk must not be skipped: %+v", dec)
		}
	}
	if len(actionable) != 1 {
		t.Fatalf("bare repo with 3 worktrees + 1 ready set: %d drain decisions, want exactly 1\n%+v", len(actionable), decisions)
	}
	if actionable[0].TaskSetID != "top" {
		t.Fatalf("drain set = %q, want top", actionable[0].TaskSetID)
	}
	if got := queuetest.Canon(t, d.Tasks, actionable[0].scan.RuntimePath); got != queuetest.Canon(t, d.Tasks, wts[0]) {
		t.Fatalf("drain routed to %q, want trunk checkout %q", got, queuetest.Canon(t, d.Tasks, wts[0]))
	}
}

// TestDecideRepoDispatchesExecutionRenameIsFatal proves the migration tripwire
// stays loud for a consuming command (ADR 0054): a queue_base→trunk rename,
// carried as a blocking "repo" finding, makes the queue's representative
// resolver fail fatally with the migration message rather than silently routing
// the drain elsewhere. The same finding is invisible to the project dashboard
// (covered in cmd/project_test.go).
func TestDecideRepoDispatchesExecutionRenameIsFatal(t *testing.T) {
	_, wts := queuetest.InitBareRepoWithWorktrees(t, 2)
	d := repoDispatchDeps(t, []tasks.Row{{ID: "top", Status: tasks.StatusReady, AutoDrain: true, Priority: 1}}, nil)
	scans := scansForCheckouts(wts, "/def")

	cfg := &config.Config{Findings: []config.Finding{{
		Path:    "repo",
		Message: "config.toml: [repo.\"/some/repo\"] queue_base was renamed to trunk",
	}}}

	decisions := decideRepoDispatches(d, cfg, scans, time.Now())

	if len(decisions) != 1 {
		t.Fatalf("execution rename: %d decisions, want 1 fatal\n%+v", len(decisions), decisions)
	}
	dec := decisions[0]
	if dec.Err == nil || !strings.Contains(dec.Err.Error(), "queue_base was renamed to trunk") {
		t.Fatalf("decision Err = %v, want queue_base rename migration message", dec.Err)
	}
	if dec.Actionable() {
		t.Fatalf("a repo poisoned by the execution rename must not be actionable: %+v", dec)
	}
}

// TestDecideRepoDispatchesBareManagedDirectiveIsConfigError asserts a `managed`
// worktree directive in a bare repo with no resolvable trunk is surfaced as a
// per-set config-class error (ADR-0059) rather than dispatched or folded into the
// generic "needs trunk" repo skip: the set is named, not actionable, and carries
// the directive fault for status — no drain, no crash-backoff.
func TestDecideRepoDispatchesBareManagedDirectiveIsConfigError(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", filepath.Join(t.TempDir(), "xdg"))
	_, wts := queuetest.InitBareRepoWithWorktrees(t, 2)
	d := repoDispatchDeps(t, []tasks.Row{{ID: "managed", Status: tasks.StatusReady, AutoDrain: true, Priority: 1}}, nil)

	id, err := tasks.ResolveRepositoryIdentity(d.Tasks, wts[0])
	if err != nil {
		t.Fatal(err)
	}
	canonDef, err := tasks.CanonicalDefinitionPathWith(d.Tasks, id.TasksDir)
	if err != nil {
		t.Fatal(err)
	}
	if err := tasks.UpdateGlobalStateWith(d.Tasks, tasks.StatePathFor(canonDef), func(s *tasks.GlobalState) error {
		s.Entry(canonDef).TaskSets = []tasks.RegisteredTaskSet{
			{ID: "managed", WorktreeIntent: &tasks.WorktreeDirective{Managed: true}},
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}

	scans := scansForCheckouts(wts, canonDef)
	decisions := decideRepoDispatches(d, &config.Config{}, scans, time.Now())

	var cfgErr *Decision
	for i := range decisions {
		if decisions[i].Actionable() {
			t.Fatalf("unsatisfiable managed directive must not be actionable: %+v", decisions[i])
		}
		if decisions[i].Reason == directiveConfigReason {
			cfgErr = &decisions[i]
		}
	}
	if cfgErr == nil {
		t.Fatalf("want a config-error decision for the managed directive, got %+v", decisions)
	}
	if cfgErr.BlockedSetID != "managed" {
		t.Fatalf("BlockedSetID = %q, want managed", cfgErr.BlockedSetID)
	}
	if !strings.Contains(cfgErr.ProjectConfigError, "Trunk") {
		t.Fatalf("ProjectConfigError = %q, want the no-resolvable-trunk fault", cfgErr.ProjectConfigError)
	}
}

func TestDecideRepoDispatchesBareWithoutBaseRefusesAndReports(t *testing.T) {
	_, wts := queuetest.InitBareRepoWithWorktrees(t, 2)
	d := repoDispatchDeps(t, []tasks.Row{{ID: "top", Status: tasks.StatusReady, AutoDrain: true, Priority: 1}}, nil)
	scans := scansForCheckouts(wts, "/def")

	decisions := decideRepoDispatches(d, &config.Config{}, scans, time.Now())

	if len(decisions) != 1 {
		t.Fatalf("bare repo without base: %d decisions, want 1 skip\n%+v", len(decisions), decisions)
	}
	dec := decisions[0]
	if dec.Actionable() {
		t.Fatalf("a refused repo must not be actionable: %+v", dec)
	}
	if dec.Reason != RepoScanReason {
		t.Fatalf("reason = %q, want %q", dec.Reason, RepoScanReason)
	}

	// The refusal is reported in status, never silently dropped.
	td := queuetest.DataDeps(t)
	snap, err := StatusFromDecisions(&Deps{Tasks: td}, decisions)
	if err != nil {
		t.Fatalf("status: %v", err)
	}
	snap.Tasks = queuetest.DataDeps(t)
	if len(snap.Skipped) != 1 || snap.Skipped[0].Reason != RepoScanReason {
		t.Fatalf("status Skipped = %+v, want one %q", snap.Skipped, RepoScanReason)
	}
	view := BuildRunView(snap, time.Now())
	if len(view.Skipped) != 1 || view.Skipped[0].Reason != RepoScanReason {
		t.Fatalf("run view Skipped = %+v, want one %q", view.Skipped, RepoScanReason)
	}
}

func TestDecideRepoDispatchesBindingRoutesRegardlessOfTrunkConfig(t *testing.T) {
	_, wts := queuetest.InitBareRepoWithWorktrees(t, 2)
	d := repoDispatchDeps(t, []tasks.Row{{ID: "top", Status: tasks.StatusReady, AutoDrain: true, Priority: 1}}, nil)

	// A per-set binding exists even though the bare repo has no Trunk configured:
	// the binding is the universal drain router.
	id, err := tasks.ResolveRepositoryIdentity(d.Tasks, wts[0])
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	queuetest.SeedBindingStore(t, d.Tasks, map[string]WorktreeBinding{
		SetScopedKey(repoIdentityKey(id), "top"): {RuntimePath: wts[1], Branch: "pop/top", Project: "repo"},
	})
	scans := scansForCheckouts(wts, "/def")

	decisions := decideRepoDispatches(d, &config.Config{}, scans, time.Now())

	var actionable []Decision
	for _, dec := range decisions {
		if dec.Reason == RepoScanReason {
			t.Fatalf("a bound set must route, not be skipped: %+v", dec)
		}
		if dec.Actionable() {
			actionable = append(actionable, dec)
		}
	}
	if len(actionable) != 1 || actionable[0].TaskSetID != "top" {
		t.Fatalf("bound bare repo: actionable = %+v, want one top drain", actionable)
	}
	if got := queuetest.Canon(t, d.Tasks, actionable[0].scan.RuntimePath); got != queuetest.Canon(t, d.Tasks, wts[1]) {
		t.Fatalf("drain routed to %q, want bound checkout %q", got, queuetest.Canon(t, d.Tasks, wts[1]))
	}
	// The pane goes to the session of the checkout the set is bound to, never to
	// the session of the repository the drain was decided from (ADR-0180).
	wantSession := project.CheckoutSessionNameWith(project.DefaultDeps(), wts[1])
	if actionable[0].scan.SessionName != wantSession {
		t.Fatalf("SessionName = %q, want bound checkout's session %q", actionable[0].scan.SessionName, wantSession)
	}
	originatingSession := project.CheckoutSessionNameWith(project.DefaultDeps(), wts[0])
	if actionable[0].scan.SessionName == originatingSession {
		t.Fatalf("SessionName must not be derived from the originating checkout %q", wts[0])
	}
}

func TestDecideRepoDispatchesNonBareRoutesToGitMainWorktree(t *testing.T) {
	main, linked := initNonBareRepoWithLinkedWorktrees(t, 2)
	d := repoDispatchDeps(t, []tasks.Row{{ID: "top", Status: tasks.StatusReady, AutoDrain: true, Priority: 1}}, nil)

	// Picker order lists a linked worktree first; with the set bound to the git
	// main worktree the drain must route there (ADR-0072).
	bindSetInPlace(t, d, main, "top")
	checkouts := []string{linked[0], linked[1], main}
	scans := scansForCheckouts(checkouts, "/def")

	decisions := decideRepoDispatches(d, &config.Config{}, scans, time.Now())

	var actionable []Decision
	for _, dec := range decisions {
		if dec.Actionable() {
			actionable = append(actionable, dec)
		}
		if dec.Reason == RepoScanReason {
			t.Fatalf("a non-bare repo has a git main worktree and must not be skipped: %+v", dec)
		}
	}
	if len(actionable) != 1 {
		t.Fatalf("non-bare repo with linked worktrees: %d drains, want exactly 1\n%+v", len(actionable), decisions)
	}
	if got := queuetest.Canon(t, d.Tasks, actionable[0].scan.RuntimePath); got != queuetest.Canon(t, d.Tasks, main) {
		t.Fatalf("drain routed to %q, want git main worktree %q", got, queuetest.Canon(t, d.Tasks, main))
	}
}

func TestScanCrossRepositoryFanOutPreserved(t *testing.T) {
	repoA := initGitRepoWithBase(t)
	repoB := initGitRepoWithBase(t)
	xdg := t.TempDir()
	t.Setenv("XDG_DATA_HOME", xdg)

	cfg := &config.Config{Projects: []config.ProjectEntry{{Path: repoA}, {Path: repoB}}}
	d := &Deps{
		Tasks:      queuetest.TasksDeps(t, true),
		Project:    project.DefaultDeps(),
		LoadConfig: func(string) (*config.Config, error) { return cfg, nil },
		ReadLock:   func(runtimePath string) *tasks.RuntimeLockStatus { return queuetest.IdleLock(runtimePath) },
		Refresh: func(string) (*tasks.RefreshResult, error) {
			return &tasks.RefreshResult{Rows: []tasks.Row{{ID: "top", Status: tasks.StatusReady, AutoDrain: true, Priority: 1}}}, nil
		},
	}
	// Both repos are registered (each carries a task-storage marker); Scan only
	// takes the decision path for repos with storage (ADR-0060).
	seedTaskStorage(t, d.Tasks, repoA, "top")
	seedTaskStorage(t, d.Tasks, repoB, "top")
	// Each set is bound in-place so it is Queue-drainable (ADR-0072); the
	// cross-repo fan-out is what this guards, not the routing default.
	bindSetInPlace(t, d, repoA, "top")
	bindSetInPlace(t, d, repoB, "top")

	decisions, err := Scan(d, cfg)
	if err != nil {
		t.Fatal(err)
	}
	actionable := 0
	for _, dec := range decisions {
		if dec.Actionable() {
			actionable++
		}
	}
	if actionable != 2 {
		t.Fatalf("two single-checkout repos: %d drains, want 2 (cross-repo fan-out preserved)\n%+v", actionable, decisions)
	}
}

// handoffFixture builds a repository with one task set bound to a linked worktree,
// plus the dashboard row a handoff verb is invoked on. The row carries the
// coordinates the live dashboard carries (project path and repo key), so the verbs
// take the fork-free bind context and the only session derivation left is the one
// under test.
func handoffFixture(t *testing.T, stem string) (d *Deps, cfg *config.Config, row DashboardRow, repo, bound string, rt *queuetest.RecordingTmux) {
	t.Helper()
	repo, setID, _ := queuetest.SetupSpawnRepo(t, stem, []queuetest.SpawnTask{
		{ID: "01-a", File: "01-a.md", Title: "A", Type: "AFK", Status: "done"},
	})
	bound = filepath.Join(t.TempDir(), stem+"-wt")
	runGit(t, repo, "worktree", "add", "--detach", bound, "HEAD")

	cfg = &config.Config{Projects: []config.ProjectEntry{{Path: repo}}}
	rt = queuetest.NewRecordingTmux(false, "0")
	d = &Deps{
		Tasks:      queuetest.TasksDeps(t, true),
		Project:    project.DefaultDeps(),
		Tmux:       rt,
		LoadConfig: func(string) (*config.Config, error) { return cfg, nil },
		ReadLock:   func(runtimePath string) *tasks.RuntimeLockStatus { return queuetest.IdleLock(runtimePath) },
	}
	repoKey, err := ResolveRepoKey(d, repo)
	if err != nil {
		t.Fatal(err)
	}
	queuetest.SeedBindingStore(t, d.Tasks, map[string]WorktreeBinding{
		SetScopedKey(repoKey, setID): {RuntimePath: bound, Branch: "detached", Project: filepath.Base(repo)},
	})
	row = DashboardRow{Project: "pop", ID: setID, ProjectPath: repo, RepoKey: repoKey, RuntimePath: bound}
	return d, cfg, row, repo, bound, rt
}

// TestHandoffVerbsTargetBoundCheckoutSession is ADR-0180 at every verb: drain,
// verify, assist, fold and the runtime shell all put their pane in the session of
// the checkout the set is bound to — created detached, since none exists yet — and
// none of them lands in the session of the project the operator invoked them from.
func TestHandoffVerbsTargetBoundCheckoutSession(t *testing.T) {
	verbs := map[string]func(*Deps, *config.Config, DashboardRow) (DashboardDrainResult, error){
		"drain":  LaunchDrain,
		"verify": LaunchVerify,
		"assist": LaunchAssist,
		"fold":   LaunchFold,
		"shell":  LaunchShell,
	}
	for name, launch := range verbs {
		t.Run(name, func(t *testing.T) {
			d, cfg, row, repo, bound, rt := handoffFixture(t, "locality-"+name)
			wantSession := project.CheckoutSessionNameWith(d.Project, bound)
			projectSession := project.CheckoutSessionNameWith(d.Project, repo)
			if wantSession == projectSession {
				t.Fatalf("fixture is not discriminating: bound and project sessions are both %q", wantSession)
			}

			result, err := launch(d, cfg, row)
			if err != nil {
				t.Fatalf("Launch %s: %v", name, err)
			}
			if result.Session != wantSession {
				t.Fatalf("%s session = %q, want the bound checkout's %q", name, result.Session, wantSession)
			}
			newSession, ok := rt.FindCommand("new-session")
			if !ok {
				t.Fatalf("%s: the bound checkout's session must be created detached; commands=%v", name, rt.Commands)
			}
			if len(newSession) != 3 || newSession[1] != wantSession {
				t.Fatalf("%s new-session = %v, want session %q", name, newSession, wantSession)
			}
		})
	}
}

// TestHandoffVerbsRefuseMissingBoundWorktree pins the other half of ADR-0180: with
// the pane's session derived from the bound checkout, a verb whose checkout is gone
// refuses rather than falling back to the trunk — the silent mislocation the ADR
// removes. The guard used to sit on drain alone.
func TestHandoffVerbsRefuseMissingBoundWorktree(t *testing.T) {
	verbs := map[string]func(*Deps, *config.Config, DashboardRow) (DashboardDrainResult, error){
		"drain":  LaunchDrain,
		"verify": LaunchVerify,
		"assist": LaunchAssist,
		"fold":   LaunchFold,
		"shell":  LaunchShell,
	}
	for name, launch := range verbs {
		t.Run(name, func(t *testing.T) {
			d, cfg, row, repo, bound, rt := handoffFixture(t, "missing-"+name)
			if err := os.RemoveAll(bound); err != nil {
				t.Fatal(err)
			}

			_, err := launch(d, cfg, row)
			if err == nil {
				t.Fatalf("%s: a missing bound worktree must refuse the verb", name)
			}
			if !strings.Contains(err.Error(), "bound worktree for "+row.ID+" is invalid") {
				t.Fatalf("%s refusal = %v, want the invalid-bound-worktree refusal", name, err)
			}
			if _, spawned := rt.FindCommand("new-session"); spawned {
				trunkSession := project.CheckoutSessionNameWith(d.Project, repo)
				t.Fatalf("%s opened a session (%q would be the trunk fallback) instead of refusing; commands=%v", name, trunkSession, rt.Commands)
			}
		})
	}
}
