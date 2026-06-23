package queue

import (
	"bytes"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/glebglazov/pop/config"
	"github.com/glebglazov/pop/project"
	"github.com/glebglazov/pop/tasks"
)

// initBareRepoWithWorktrees clones a committed source repo into a bare repo and
// adds n detached worktrees, returning the bare dir and the worktree paths. All
// worktrees share one Repository identity.
func initBareRepoWithWorktrees(t *testing.T, n int) (string, []string) {
	t.Helper()
	src := initMergeabilityRepo(t)
	parent := t.TempDir()
	runGit(t, parent, "clone", "--bare", src, "repo.git")
	bareDir := filepath.Join(parent, "repo.git")
	var wts []string
	for i := 0; i < n; i++ {
		wt := filepath.Join(parent, "wt"+string(rune('0'+i)))
		runGit(t, bareDir, "worktree", "add", "--detach", wt, "HEAD")
		wts = append(wts, wt)
	}
	return bareDir, wts
}

// initNonBareRepoWithLinkedWorktrees creates a normal repo (the git main
// worktree) and adds n linked worktrees beside it.
func initNonBareRepoWithLinkedWorktrees(t *testing.T, n int) (string, []string) {
	t.Helper()
	main := initMergeabilityRepo(t)
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
		Tasks:   queueTestTasksDeps(true),
		Project: project.DefaultDeps(),
		ReadLock: func(runtimePath string) *tasks.RuntimeLockStatus {
			if locks != nil {
				if l, ok := locks[runtimePath]; ok {
					return l
				}
			}
			return idleLock(runtimePath)
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

func ptrBool(b bool) *bool { return &b }

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
	_, wts := initBareRepoWithWorktrees(t, 3)
	d := repoDispatchDeps(t, []tasks.Row{{ID: "top", Status: tasks.StatusReady, AutoDrain: true, Priority: 1}}, nil)

	// trunk override pins the repo's Trunk worktree to the first worktree.
	cfg := &config.Config{Repo: map[string]config.RepoOverrideConfig{
		wts[0]: {Trunk: ptrBool(true)},
	}}
	scans := scansForCheckouts(wts, "/def")

	decisions := decideRepoDispatches(d, cfg, scans, &DaemonState{Version: 1}, time.Now())

	var actionable []Decision
	for _, dec := range decisions {
		if dec.Actionable() {
			actionable = append(actionable, dec)
		}
		if dec.Reason == repoScanReason {
			t.Fatalf("a repo with a trunk must not be skipped: %+v", dec)
		}
	}
	if len(actionable) != 1 {
		t.Fatalf("bare repo with 3 worktrees + 1 ready set: %d drain decisions, want exactly 1\n%+v", len(actionable), decisions)
	}
	if actionable[0].TaskSetID != "top" {
		t.Fatalf("drain set = %q, want top", actionable[0].TaskSetID)
	}
	if got := canon(t, d, actionable[0].scan.RuntimePath); got != canon(t, d, wts[0]) {
		t.Fatalf("drain routed to %q, want trunk checkout %q", got, canon(t, d, wts[0]))
	}
}

// TestDecideRepoDispatchesExecutionRenameIsFatal proves the migration tripwire
// stays loud for a consuming command (ADR 0054): a queue_base→trunk rename,
// carried as a blocking "repo" finding, makes the queue's representative
// resolver fail fatally with the migration message rather than silently routing
// the drain elsewhere. The same finding is invisible to the project dashboard
// (covered in cmd/project_test.go).
func TestDecideRepoDispatchesExecutionRenameIsFatal(t *testing.T) {
	_, wts := initBareRepoWithWorktrees(t, 2)
	d := repoDispatchDeps(t, []tasks.Row{{ID: "top", Status: tasks.StatusReady, AutoDrain: true, Priority: 1}}, nil)
	scans := scansForCheckouts(wts, "/def")

	cfg := &config.Config{Findings: []config.Finding{{
		Path:    "repo",
		Message: "config.toml: [repo.\"/some/repo\"] queue_base was renamed to trunk",
	}}}

	decisions := decideRepoDispatches(d, cfg, scans, &DaemonState{Version: 1}, time.Now())

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

func TestDecideRepoDispatchesBareWithoutBaseRefusesAndReports(t *testing.T) {
	_, wts := initBareRepoWithWorktrees(t, 2)
	d := repoDispatchDeps(t, []tasks.Row{{ID: "top", Status: tasks.StatusReady, AutoDrain: true, Priority: 1}}, nil)
	scans := scansForCheckouts(wts, "/def")

	decisions := decideRepoDispatches(d, &config.Config{}, scans, &DaemonState{Version: 1}, time.Now())

	if len(decisions) != 1 {
		t.Fatalf("bare repo without base: %d decisions, want 1 skip\n%+v", len(decisions), decisions)
	}
	dec := decisions[0]
	if dec.Actionable() {
		t.Fatalf("a refused repo must not be actionable: %+v", dec)
	}
	if dec.Reason != repoScanReason {
		t.Fatalf("reason = %q, want %q", dec.Reason, repoScanReason)
	}

	// The refusal is reported in status and run output, never silently dropped.
	td := queueDataDeps(t)
	snap, err := statusFromDecisions(&Deps{Tasks: td}, decisions, &DaemonState{Version: 1})
	if err != nil {
		t.Fatalf("status: %v", err)
	}
	snap.Tasks = queueDataDeps(t)
	if len(snap.Skipped) != 1 || snap.Skipped[0].Reason != repoScanReason {
		t.Fatalf("status Skipped = %+v, want one %q", snap.Skipped, repoScanReason)
	}
	var out bytes.Buffer
	RenderRunBaseline(&out, BuildRunView(snap, time.Now()))
	if !strings.Contains(out.String(), repoScanReason) {
		t.Fatalf("run output omits skip reason:\n%s", out.String())
	}
}

func TestDecideRepoDispatchesBindingRoutesRegardlessOfTrunkConfig(t *testing.T) {
	_, wts := initBareRepoWithWorktrees(t, 2)
	d := repoDispatchDeps(t, []tasks.Row{{ID: "top", Status: tasks.StatusReady, AutoDrain: true, Priority: 1}}, nil)

	// A per-set binding exists even though the bare repo has no Trunk configured:
	// the binding is the universal drain router.
	id, err := tasks.ResolveRepositoryIdentity(d.Tasks, wts[0])
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	seedBindingStore(t, d.Tasks, map[string]WorktreeBinding{
		setScopedKey(repoIdentityKey(id), "top"): {RuntimePath: wts[1], Branch: "pop/top", Project: "repo"},
	})
	state := &DaemonState{Version: 1}
	scans := scansForCheckouts(wts, "/def")

	decisions := decideRepoDispatches(d, &config.Config{}, scans, state, time.Now())

	var actionable []Decision
	for _, dec := range decisions {
		if dec.Reason == repoScanReason {
			t.Fatalf("a bound set must route, not be skipped: %+v", dec)
		}
		if dec.Actionable() {
			actionable = append(actionable, dec)
		}
	}
	if len(actionable) != 1 || actionable[0].TaskSetID != "top" {
		t.Fatalf("bound bare repo: actionable = %+v, want one top drain", actionable)
	}
	if got := canon(t, d, actionable[0].scan.RuntimePath); got != canon(t, d, wts[1]) {
		t.Fatalf("drain routed to %q, want bound checkout %q", got, canon(t, d, wts[1]))
	}
}

func TestDecideRepoDispatchesNonBareRoutesToGitMainWorktree(t *testing.T) {
	main, linked := initNonBareRepoWithLinkedWorktrees(t, 2)
	d := repoDispatchDeps(t, []tasks.Row{{ID: "top", Status: tasks.StatusReady, AutoDrain: true, Priority: 1}}, nil)

	// Picker order lists a linked worktree first; with no config the drain must
	// still route to the repo's git main worktree.
	checkouts := []string{linked[0], linked[1], main}
	scans := scansForCheckouts(checkouts, "/def")

	decisions := decideRepoDispatches(d, &config.Config{}, scans, &DaemonState{Version: 1}, time.Now())

	var actionable []Decision
	for _, dec := range decisions {
		if dec.Actionable() {
			actionable = append(actionable, dec)
		}
		if dec.Reason == repoScanReason {
			t.Fatalf("a non-bare repo has a git main worktree and must not be skipped: %+v", dec)
		}
	}
	if len(actionable) != 1 {
		t.Fatalf("non-bare repo with linked worktrees: %d drains, want exactly 1\n%+v", len(actionable), decisions)
	}
	if got := canon(t, d, actionable[0].scan.RuntimePath); got != canon(t, d, main) {
		t.Fatalf("drain routed to %q, want git main worktree %q", got, canon(t, d, main))
	}
}

func TestScanCrossRepositoryFanOutPreserved(t *testing.T) {
	repoA := initMergeabilityRepo(t)
	repoB := initMergeabilityRepo(t)
	xdg := t.TempDir()
	t.Setenv("XDG_DATA_HOME", xdg)

	cfg := &config.Config{Projects: []config.ProjectEntry{{Path: repoA}, {Path: repoB}}}
	d := &Deps{
		Tasks:      queueTestTasksDeps(true),
		Project:    project.DefaultDeps(),
		LoadConfig: func(string) (*config.Config, error) { return cfg, nil },
		ReadLock:   func(runtimePath string) *tasks.RuntimeLockStatus { return idleLock(runtimePath) },
		Refresh: func(string) (*tasks.RefreshResult, error) {
			return &tasks.RefreshResult{Rows: []tasks.Row{{ID: "top", Status: tasks.StatusReady, AutoDrain: true, Priority: 1}}}, nil
		},
	}

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

// canon canonicalizes a checkout path the way the scheduler does, for comparison.
func canon(t *testing.T, d *Deps, path string) string {
	t.Helper()
	c, err := canonicalCheckoutPath(d.Tasks, path)
	if err != nil {
		t.Fatalf("canonicalize %q: %v", path, err)
	}
	return c
}
