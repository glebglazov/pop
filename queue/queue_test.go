package queue

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/glebglazov/pop/config"
	"github.com/glebglazov/pop/internal/deps"
	tmuxmod "github.com/glebglazov/pop/internal/tmux"
	"github.com/glebglazov/pop/internal/tmux/tmuxtest"
	"github.com/glebglazov/pop/project"
	"github.com/glebglazov/pop/tasks"
	"github.com/glebglazov/pop/tasks/binding"
)

func TestSelectReadySet(t *testing.T) {
	tests := []struct {
		name string
		rows []tasks.Row
		want string
		ok   bool
	}{
		{
			name: "no rows",
			rows: nil,
			ok:   false,
		},
		{
			name: "no ready rows",
			rows: []tasks.Row{
				{ID: "a", Status: tasks.StatusBlocked, Priority: 9},
				{ID: "b", Status: tasks.StatusDone, Priority: 8},
				{ID: "c", Status: tasks.StatusFailed, Priority: 7},
			},
			ok: false,
		},
		{
			name: "single ready row",
			rows: []tasks.Row{
				{ID: "only", Status: tasks.StatusReady, AutoDrain: true, Priority: 0},
			},
			want: "only",
			ok:   true,
		},
		{
			name: "highest priority wins, non-ready ignored",
			rows: []tasks.Row{
				{ID: "low", Status: tasks.StatusReady, AutoDrain: true, Priority: 1, RegIndex: 0},
				{ID: "blocked-high", Status: tasks.StatusBlocked, Priority: 100, RegIndex: 1},
				{ID: "high", Status: tasks.StatusReady, AutoDrain: true, Priority: 50, RegIndex: 2},
				{ID: "mid", Status: tasks.StatusReady, AutoDrain: true, Priority: 10, RegIndex: 3},
			},
			want: "high",
			ok:   true,
		},
		{
			name: "priority tie breaks by registration order",
			rows: []tasks.Row{
				{ID: "second", Status: tasks.StatusReady, AutoDrain: true, Priority: 5, RegIndex: 4},
				{ID: "first", Status: tasks.StatusReady, AutoDrain: true, Priority: 5, RegIndex: 1},
			},
			want: "first",
			ok:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ids, _, ok := selectReadySets(&tasks.RefreshResult{Rows: tt.rows}, nil, nil, nil)
			got := ""
			if ok && len(ids) > 0 {
				got = ids[0]
			}
			if ok != tt.ok || got != tt.want {
				t.Fatalf("selectReadySets = (%q, %v), want (%q, %v)", got, ok, tt.want, tt.ok)
			}
		})
	}
}

// liveLock returns a runtime-lock status that reads as a live (busy) lock.
func liveLock(path string) *tasks.RuntimeLockStatus {
	return &tasks.RuntimeLockStatus{
		RuntimePath: path,
		Locked:      true,
		Metadata:    &tasks.RuntimeLockMetadata{PID: 4242, RuntimePath: path},
	}
}

func idleLock(path string) *tasks.RuntimeLockStatus {
	return &tasks.RuntimeLockStatus{RuntimePath: path}
}

func TestScanSkipsNonGitProjectsOutsideQueueScope(t *testing.T) {
	gitRepo := t.TempDir()
	spawnInitGitRepo(t, gitRepo)
	nonGit := t.TempDir()

	cfg := &config.Config{
		Projects: []config.ProjectEntry{
			{Path: gitRepo},
			{Path: nonGit},
		},
	}
	td := queueTestTasksDeps(t, true)
	d := &Deps{
		Tasks:      td,
		Project:    project.DefaultDeps(),
		LoadConfig: func(string) (*config.Config, error) { return cfg, nil },
		ReadLock:   func(runtimePath string) *tasks.RuntimeLockStatus { return idleLock(runtimePath) },
		Refresh:    func(defPath string) (*tasks.RefreshResult, error) { return &tasks.RefreshResult{}, nil },
	}

	decisions, err := Scan(d, cfg)
	if err != nil {
		t.Fatal(err)
	}

	var gitDec, nonGitDec *Decision
	for i := range decisions {
		switch decisions[i].Project {
		case filepath.Base(gitRepo):
			gitDec = &decisions[i]
		case filepath.Base(nonGit):
			nonGitDec = &decisions[i]
		}
	}
	if gitDec == nil {
		t.Fatal("expected decision for git project")
	}
	if nonGitDec == nil {
		t.Fatal("expected decision for non-git project")
	}
	if nonGitDec.Err != nil {
		t.Fatalf("non-git project must not be a scan error: %v", nonGitDec.Err)
	}
	if nonGitDec.Reason != "no ready set" {
		t.Fatalf("non-git project Reason = %q, want no ready set", nonGitDec.Reason)
	}

	snap, err := statusFromDecisions(&Deps{Tasks: td}, decisions)
	if err != nil {
		t.Fatalf("status: %v", err)
	}
	snap.Tasks = queueDataDeps(t)
	view := BuildRunView(snap, time.Now())
	if len(view.ScanErrors) != 0 {
		t.Fatalf("ScanErrors = %v, want none", view.ScanErrors)
	}
	if view.IdleCount != 2 {
		t.Fatalf("IdleCount = %d, want 2 (both projects have no ready sets)", view.IdleCount)
	}
}

func TestDecideProjectIdleSkip(t *testing.T) {
	refreshCalled := false
	d := &Deps{
		Tasks: queueTestTasksDeps(t, true),
		ReadLock: func(runtimePath string) *tasks.RuntimeLockStatus {
			return liveLock(runtimePath)
		},
		Refresh: func(defPath string) (*tasks.RefreshResult, error) {
			refreshCalled = true
			return &tasks.RefreshResult{}, nil
		},
	}

	dec := decideProject(d, projectScan{Name: "proj", RuntimePath: "/co", DefinitionPath: "/def"}, time.Now())

	if !dec.Busy {
		t.Fatalf("expected Busy decision for a live lock, got %+v", dec)
	}
	if dec.Actionable() {
		t.Fatalf("a busy project must not be actionable: %+v", dec)
	}
	if dec.TaskSetID != "" {
		t.Fatalf("a busy project must select no set, got %q", dec.TaskSetID)
	}
	if refreshCalled {
		t.Fatal("a live lock must short-circuit before refreshing Task sets")
	}
}

func TestDecideProjectSelectsHighestPriority(t *testing.T) {
	d := &Deps{
		Tasks: queueTestTasksDeps(t, true),
		ReadLock: func(runtimePath string) *tasks.RuntimeLockStatus {
			return idleLock(runtimePath)
		},
		Refresh: func(defPath string) (*tasks.RefreshResult, error) {
			return &tasks.RefreshResult{Rows: []tasks.Row{
				{ID: "low", Status: tasks.StatusReady, AutoDrain: true, Priority: 1, RegIndex: 0},
				{ID: "top", Status: tasks.StatusReady, AutoDrain: true, Priority: 99, RegIndex: 1},
				{ID: "blocked", Status: tasks.StatusBlocked, Priority: 100, RegIndex: 2},
			}}, nil
		},
	}

	dec := decideProject(d, projectScan{Name: "proj", RuntimePath: "/co", DefinitionPath: "/def"}, time.Now())

	if dec.Busy || dec.Err != nil {
		t.Fatalf("idle project with ready work should not be busy/errored, got %+v", dec)
	}
	// The highest-priority ready set is selected first, but unbound and carrying
	// no worktree directive it is not Queue-drainable (ADR-0070/0072): it is
	// surfaced as needs-bind rather than dispatched.
	if dec.Reason != needsBindReason || dec.BlockedSetID != "top" {
		t.Fatalf("expected needs-bind skip for highest-priority set 'top', got %+v", dec)
	}
	if dec.Actionable() {
		t.Fatalf("an unbound no-directive set must not be actionable: %+v", dec)
	}
}

func TestDecideProjectSelectsOnlyAutoDrainReadySets(t *testing.T) {
	d := &Deps{
		Tasks:    queueTestTasksDeps(t, true),
		ReadLock: func(runtimePath string) *tasks.RuntimeLockStatus { return idleLock(runtimePath) },
		Refresh: func(defPath string) (*tasks.RefreshResult, error) {
			return &tasks.RefreshResult{Rows: []tasks.Row{
				{ID: "unmarked", Status: tasks.StatusReady, Priority: 100, RegIndex: 0},
				{ID: "marked", Status: tasks.StatusReady, AutoDrain: true, Priority: 1, RegIndex: 1},
			}}, nil
		},
	}

	dec := decideProject(d, projectScan{Name: "proj", RuntimePath: "/co", DefinitionPath: "/def"}, time.Now())

	// Only the auto-drain set is a Queue candidate; unbound and directive-free it
	// surfaces as needs-bind rather than dispatching (ADR-0070/0072).
	if dec.Reason != needsBindReason || dec.BlockedSetID != "marked" {
		t.Fatalf("want needs-bind skip for auto-drain set 'marked', got %+v", dec)
	}

	d.Refresh = func(defPath string) (*tasks.RefreshResult, error) {
		return &tasks.RefreshResult{Rows: []tasks.Row{
			{ID: "unmarked", Status: tasks.StatusReady, Priority: 100, RegIndex: 0},
		}}, nil
	}
	dec = decideProject(d, projectScan{Name: "proj", RuntimePath: "/co", DefinitionPath: "/def"}, time.Now())
	if dec.Actionable() || dec.Reason != "no ready set" {
		t.Fatalf("unmarked ready set should be skipped, got %+v", dec)
	}
}

func TestDecideProjectNoReadySet(t *testing.T) {
	d := &Deps{
		Tasks:    queueTestTasksDeps(t, true),
		ReadLock: func(runtimePath string) *tasks.RuntimeLockStatus { return idleLock(runtimePath) },
		Refresh: func(defPath string) (*tasks.RefreshResult, error) {
			return &tasks.RefreshResult{Rows: []tasks.Row{
				{ID: "done", Status: tasks.StatusDone, Priority: 5},
				{ID: "blocked", Status: tasks.StatusBlocked, Priority: 5},
			}}, nil
		},
	}

	dec := decideProject(d, projectScan{Name: "proj", RuntimePath: "/co", DefinitionPath: "/def"}, time.Now())

	if dec.Actionable() {
		t.Fatalf("a project with no ready set must not be actionable: %+v", dec)
	}
	if dec.Reason != "no ready set" {
		t.Fatalf("expected reason 'no ready set', got %q", dec.Reason)
	}
}

func TestDecideProjectRetiredConfigKeyCausesConfigError(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".pop"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".pop", "config.toml"), []byte("worktree_ready = true\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	d := &Deps{
		Tasks:   queueTestTasksDeps(t, true),
		Project: &project.Deps{FS: deps.NewRealFileSystem()},
		ReadLock: func(runtimePath string) *tasks.RuntimeLockStatus {
			return idleLock(runtimePath)
		},
		Refresh: func(defPath string) (*tasks.RefreshResult, error) {
			return &tasks.RefreshResult{}, nil
		},
	}

	dec := decideProject(d, projectScan{Name: "proj", ProjectPath: root, RuntimePath: root, DefinitionPath: root}, time.Now())

	if dec.ProjectConfigError == "" {
		t.Fatalf("worktree_ready in .pop/config.toml must cause ProjectConfigError, got empty: %+v", dec)
	}
	if !strings.Contains(dec.ProjectConfigError, "worktree_ready was removed") {
		t.Fatalf("ProjectConfigError = %q, want 'worktree_ready was removed'", dec.ProjectConfigError)
	}
}

func TestDecideProjectMalformedRepoConfigReportsAndDegrades(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".pop"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".pop", "config.toml"), []byte("worktree_ready =\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	d := &Deps{
		Tasks:   queueTestTasksDeps(t, true),
		Project: &project.Deps{FS: deps.NewRealFileSystem()},
		ReadLock: func(runtimePath string) *tasks.RuntimeLockStatus {
			return idleLock(runtimePath)
		},
		Refresh: func(defPath string) (*tasks.RefreshResult, error) {
			return &tasks.RefreshResult{}, nil
		},
	}

	dec := decideProject(d, projectScan{Name: "proj", ProjectPath: root, RuntimePath: root, DefinitionPath: root}, time.Now())

	if !strings.Contains(dec.ProjectConfigError, ".pop/config.toml") {
		t.Fatalf("ProjectConfigError = %q, want .pop/config.toml parse error", dec.ProjectConfigError)
	}
}

func TestLiveOpenSpawnsExcludesStaleSpawnOnSharedCheckout(t *testing.T) {
	// Under the adopt-current-checkout model several sets share one runtime path.
	// A drain killed without journaling an outcome leaves a stale open-spawn; its
	// SetID no longer matches the live lock's metadata, so it must not be reported
	// as running (which would borrow the live set's lock and surface as a
	// duplicate picked-up line).
	xdg := t.TempDir()
	t.Setenv("XDG_DATA_HOME", xdg)
	root := initGitRepoWithBase(t)
	td := queueDataDeps(t)
	td.LookPath = func(file string) (string, error) { return "/bin/" + file, nil }
	d := &Deps{
		Tasks:   td,
		Project: &project.Deps{FS: deps.NewRealFileSystem()},
		ReadLock: func(runtimePath string) *tasks.RuntimeLockStatus {
			if runtimePath == root {
				lock := liveLock(runtimePath)
				lock.Metadata.SetID = "live"
				return lock
			}
			return idleLock(runtimePath)
		},
		Refresh: func(defPath string) (*tasks.RefreshResult, error) {
			return &tasks.RefreshResult{Rows: []tasks.Row{
				{ID: "live", Status: tasks.StatusReady, AutoDrain: true, Priority: 20, RegIndex: 0},
			}}, nil
		},
	}
	// Only "live" holds the runtime lock (a live running Drain); the busy
	// detection must report that set alone and never a set with no live drain.
	decisions := decideProjectDispatches(d, projectScan{Name: "proj", ProjectPath: root, RuntimePath: root, DefinitionPath: root}, nil, nil, time.Now())

	var busy []string
	for _, dec := range decisions {
		if dec.Busy {
			busy = append(busy, dec.TaskSetID)
		}
	}
	if !reflect.DeepEqual(busy, []string{"live"}) {
		t.Fatalf("busy sets = %#v, want only the live lock holder (stale spawn excluded)", busy)
	}
}

func hasActionable(decisions []Decision, setID string) bool {
	for _, dec := range decisions {
		if dec.Actionable() && dec.TaskSetID == setID {
			return true
		}
	}
	return false
}

// TestPendingSpawnIntentBlocksFastRePoll is the double-spawn-window guard: between
// the supervisor sending an implement into a pane and that drain reaching
// BeginDrain the store has no running Drain row, so a fast re-poll would re-select
// the same set and send a second implement. The durable spawn-intent record — not
// in-memory view seeding — closes the window. decideProjectDispatches carries no
// run view, so a pass through it exercises the store guard alone.
func TestPendingSpawnIntentBlocksFastRePoll(t *testing.T) {
	root := initGitRepoWithBase(t)
	td := queueDataDeps(t)
	td.LookPath = func(file string) (string, error) { return "/bin/" + file, nil }
	d := &Deps{
		Tasks:    td,
		Project:  &project.Deps{FS: deps.NewRealFileSystem()},
		ReadLock: func(runtimePath string) *tasks.RuntimeLockStatus { return idleLock(runtimePath) },
		Refresh: func(defPath string) (*tasks.RefreshResult, error) {
			return &tasks.RefreshResult{Rows: []tasks.Row{
				{ID: "set-a", Status: tasks.StatusReady, AutoDrain: true, Priority: 10, RegIndex: 0},
			}}, nil
		},
	}
	bindSetInPlace(t, d, root, "set-a")
	scan := projectScan{Name: "proj", ProjectPath: root, RuntimePath: root, DefinitionPath: root}

	// First poll: the set is idle and Queue-drainable, so it dispatches.
	if first := decideProjectDispatches(d, scan, nil, nil, time.Now()); !hasActionable(first, "set-a") {
		t.Fatalf("first poll must dispatch set-a: %+v", first)
	}

	// The supervisor records the spawn intent before sending the drain command.
	if err := tasks.RecordSpawnIntent(td, root, "set-a"); err != nil {
		t.Fatalf("RecordSpawnIntent: %v", err)
	}

	// Second poll before the drain reaches BeginDrain: no running row exists yet,
	// but the live pending-spawn marker makes the set read busy, so it is NOT
	// re-dispatched — closing the window against durable state.
	second := decideProjectDispatches(d, scan, nil, nil, time.Now())
	if hasActionable(second, "set-a") {
		t.Fatalf("fast re-poll re-dispatched set-a despite a pending spawn intent: %+v", second)
	}
}

// scanGitGuard fails the test if Scan forks any git command. The fork-free
// partition (ADR-0060) classifies a project lacking a task-storage marker as
// idle/no-tasks from its name alone, so a status scan over such projects forks no
// git at all — neither identity resolution nor the integration target.
type scanGitGuard struct{ t *testing.T }

func (g *scanGitGuard) Command(args ...string) (string, error) {
	g.t.Errorf("Scan forked git: git %s", strings.Join(args, " "))
	return "", nil
}

func (g *scanGitGuard) CommandInDir(dir string, args ...string) (string, error) {
	g.t.Errorf("Scan forked git in %q: git %s", dir, strings.Join(args, " "))
	return "", nil
}

// TestScanForksNoGitForProjectsWithoutTaskStorage is the ADR-0060 guard for the
// status read path: a scan over configured projects that carry no task-storage
// marker forks zero git (a guard git fails the test on any invocation), yet every
// project still appears as idle/no-tasks — the full-fleet listing is preserved.
func TestScanForksNoGitForProjectsWithoutTaskStorage(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", filepath.Join(t.TempDir(), "xdg"))
	projA := t.TempDir()
	projB := t.TempDir()

	cfg := &config.Config{Projects: []config.ProjectEntry{{Path: projA}, {Path: projB}}}
	guard := &scanGitGuard{t: t}
	td := queueTestTasksDeps(t, true)
	td.Git = guard
	pd := project.DefaultDeps()
	pd.Git = guard
	d := &Deps{
		Tasks:      td,
		Project:    pd,
		LoadConfig: func(string) (*config.Config, error) { return cfg, nil },
	}

	decisions, err := Scan(d, cfg)
	if err != nil {
		t.Fatal(err)
	}
	if len(decisions) != 2 {
		t.Fatalf("decisions = %+v, want one idle decision per configured project", decisions)
	}
	for _, dec := range decisions {
		if dec.Actionable() || dec.Busy || dec.Err != nil || dec.Reason != "no ready set" {
			t.Fatalf("a project lacking task storage must be idle/no-tasks: %+v", dec)
		}
	}

	snap, err := statusFromDecisions(&Deps{Tasks: queueDataDeps(t)}, decisions)
	if err != nil {
		t.Fatalf("status: %v", err)
	}
	snap.Tasks = queueDataDeps(t)
	if view := BuildRunView(snap, time.Now()); view.IdleCount != 2 {
		t.Fatalf("IdleCount = %d, want 2 (both projects listed as idle)", view.IdleCount)
	}
}

// TestScanRegisteredReadySetIsDispatchable is the ADR-0060 guard for the daemon
// path: a created and registered Ready set in a task-storage repo still resolves
// to an Actionable decision, and its spawn session name is identical to the value
// the old git-resolved path produced — so the marker-based partition leaves
// scheduling decisions unchanged for repos with task storage.
func TestScanRegisteredReadySetIsDispatchable(t *testing.T) {
	repo, setID, _ := setupSupervisorSpawnRepo(t, "scan-dispatch", []spawnTestTask{
		{ID: "01-a", File: "01-a.md", Title: "A", Type: "AFK", Status: "open"},
	})
	cfg := &config.Config{Projects: []config.ProjectEntry{{Path: repo}}}
	d := &Deps{
		Tasks:      queueTestTasksDeps(t, true),
		Project:    project.DefaultDeps(),
		LoadConfig: func(string) (*config.Config, error) { return cfg, nil },
	}
	bindSetInPlace(t, d, repo, setID)

	decisions, err := Scan(d, cfg)
	if err != nil {
		t.Fatal(err)
	}
	var actionable *Decision
	for i := range decisions {
		if decisions[i].Actionable() {
			actionable = &decisions[i]
		}
	}
	if actionable == nil {
		t.Fatalf("a created+registered ready set must be dispatchable: %+v", decisions)
	}
	if actionable.TaskSetID != setID {
		t.Fatalf("dispatchable set = %q, want %q", actionable.TaskSetID, setID)
	}
	want := project.SessionNameWith(project.DefaultDeps(), repo)
	if actionable.scan.SessionName != want {
		t.Fatalf("spawn session name = %q, want %q (unchanged from the git-resolved path)", actionable.scan.SessionName, want)
	}
}

func TestScanTreatsDrainAtHITLGateRuntimeLockAsBusy(t *testing.T) {
	xdg := t.TempDir()
	t.Setenv("XDG_DATA_HOME", xdg)
	root := t.TempDir()
	runGit(t, root, "init")

	td := tasks.DefaultDeps()
	td.ProcessAlive = func(pid int) bool { return pid == os.Getpid() }
	// A live drain implies a registered repo; Scan only inspects locks for repos
	// with a task-storage marker (ADR-0060), so seed one for this checkout.
	seedTaskStorage(t, td, root, "hitl-set")
	runtimePath, err := tasks.ResolveRuntimePathWith(td, root, "")
	if err != nil {
		t.Fatal(err)
	}
	lock, err := tasks.AcquireRuntimeLockForSet(td, runtimePath, "hitl-set", &bytes.Buffer{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = lock.Release() })

	d := DefaultDeps()
	d.Tasks = td
	cfg := &config.Config{Projects: []config.ProjectEntry{{Path: root}}}

	decisions, err := Scan(d, cfg)
	if err != nil {
		t.Fatal(err)
	}
	if len(decisions) != 1 {
		t.Fatalf("decisions = %+v, want one busy decision", decisions)
	}
	dec := decisions[0]
	if !dec.Busy || dec.Actionable() {
		t.Fatalf("HITL-gated live drain must be busy and non-actionable, got %+v", dec)
	}
	if dec.TaskSetID != "hitl-set" {
		t.Fatalf("busy TaskSetID = %q, want hitl-set", dec.TaskSetID)
	}
}

func TestDecideProjectDispatchesReportsEachUnboundSetNeedsBind(t *testing.T) {
	d := &Deps{
		Tasks:    queueTestTasksDeps(t, true),
		ReadLock: func(runtimePath string) *tasks.RuntimeLockStatus { return idleLock(runtimePath) },
		Refresh: func(defPath string) (*tasks.RefreshResult, error) {
			return &tasks.RefreshResult{Rows: []tasks.Row{
				{ID: "top", Status: tasks.StatusReady, AutoDrain: true, Priority: 30, RegIndex: 0},
				{ID: "next", Status: tasks.StatusReady, AutoDrain: true, Priority: 20, RegIndex: 1},
			}}, nil
		},
	}

	decisions := decideProjectDispatches(d, projectScan{Name: "proj", RuntimePath: "/co", DefinitionPath: "/def"}, nil, nil, time.Now())

	// Routing runs per set with no single-dispatch truncation (ADR-0070/0072):
	// every Ready set is evaluated, and each unbound, directive-free set surfaces
	// as its own needs-bind fault rather than being dispatched in-place.
	if len(decisions) != 2 {
		t.Fatalf("dispatches = %+v, want one needs-bind skip per unbound set", decisions)
	}
	for i, want := range []string{"top", "next"} {
		dec := decisions[i]
		if dec.Actionable() || dec.Reason != needsBindReason || dec.BlockedSetID != want {
			t.Fatalf("decision[%d] = %+v, want needs-bind skip for %s", i, dec, want)
		}
	}
}

// TestDecideProjectDispatchesWithholdsUnsatisfiableDirective asserts a Ready set
// whose worktree directive is unsatisfiable is withheld as a non-actionable
// config-class error (ADR-0059): TaskSetID cleared so no drain is dispatched,
// Reason marks the directive fault, BlockedSetID names the set, and the error is
// carried for status. The daemon spawns only Actionable decisions, so this is the
// no-drain / no-churn / no-crash-backoff guarantee at the decision seam.
func TestDecideProjectDispatchesWithholdsUnsatisfiableDirective(t *testing.T) {
	d := &Deps{
		Tasks:    queueTestTasksDeps(t, true),
		ReadLock: func(runtimePath string) *tasks.RuntimeLockStatus { return idleLock(runtimePath) },
		Refresh: func(defPath string) (*tasks.RefreshResult, error) {
			return &tasks.RefreshResult{Rows: []tasks.Row{
				{ID: "managed", Status: tasks.StatusReady, AutoDrain: true, Priority: 30, RegIndex: 0},
			}}, nil
		},
		ProbeDirective: func(checkout, setID string) string {
			if setID == "managed" {
				return "managed worktree directive: no resolvable Trunk worktree"
			}
			return ""
		},
	}

	decisions := decideProjectDispatches(d, projectScan{Name: "proj", RuntimePath: "/co", DefinitionPath: "/def"}, nil, nil, time.Now())

	if len(decisions) != 1 {
		t.Fatalf("decisions = %+v, want one config-error decision", decisions)
	}
	dec := decisions[0]
	if dec.Actionable() {
		t.Fatalf("unsatisfiable directive must not be actionable: %+v", dec)
	}
	if dec.Reason != directiveConfigReason {
		t.Fatalf("reason = %q, want %q", dec.Reason, directiveConfigReason)
	}
	if dec.BlockedSetID != "managed" {
		t.Fatalf("BlockedSetID = %q, want managed", dec.BlockedSetID)
	}
	if !strings.Contains(dec.ProjectConfigError, "managed") || !strings.Contains(dec.ProjectConfigError, "Trunk") {
		t.Fatalf("ProjectConfigError = %q, want the directive fault", dec.ProjectConfigError)
	}
}

// TestUnsatisfiableDirectiveSurfacesInStatusNotBackoff asserts the withheld
// config-error decision surfaces the same way a project config error does — in
// the run view's scan-error channel — and never as a queued drain or a backoff
// timer (a static defect is not a runtime crash, ADR-0059).
func TestUnsatisfiableDirectiveSurfacesInStatusNotBackoff(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", filepath.Join(t.TempDir(), "xdg"))
	td := tasks.DefaultDeps()
	d := &Deps{
		Tasks:    td,
		ReadLock: func(runtimePath string) *tasks.RuntimeLockStatus { return idleLock(runtimePath) },
		Refresh: func(defPath string) (*tasks.RefreshResult, error) {
			return &tasks.RefreshResult{Rows: []tasks.Row{
				{ID: "named", Status: tasks.StatusReady, AutoDrain: true, Priority: 30, RegIndex: 0},
			}}, nil
		},
		ProbeDirective: func(checkout, setID string) string {
			return `named worktree directive: no worktree of that name on this machine: "absent"`
		},
	}

	decisions := decideProjectDispatches(d, projectScan{Name: "proj", RuntimePath: "/co", DefinitionPath: "/def"}, nil, nil, time.Now())

	snap, err := statusFromDecisions(d, decisions)
	if err != nil {
		t.Fatalf("status: %v", err)
	}
	snap.Tasks = td
	view := BuildRunView(snap, time.Now())

	msg, ok := view.ScanErrors["proj"]
	if !ok || !strings.Contains(msg, "named") {
		t.Fatalf("ScanErrors[proj] = %q (ok=%v), want the directive config error", msg, ok)
	}
	for _, q := range view.Queued {
		if q.ReadySet == "named" {
			t.Fatalf("unsatisfiable set must not be queued for drain: %+v", q)
		}
	}
	for _, b := range view.Blocked {
		if b.SetID == "named" {
			t.Fatalf("unsatisfiable set must not appear blocked/backed-off: %+v", b)
		}
	}
}

// firstReadySet is a test convenience over the single selectReadySets selector,
// returning only its top pick the way the retired selectReadySet wrapper read:
// the highest-priority spawnable set, or the wait instant/reason on none.
func firstReadySet(refresh *tasks.RefreshResult, backoff setBackoffFunc, recoveryWaiters map[string]tasks.RecoveryWaiter) (string, time.Time, string, bool) {
	ids, deferral, ok := selectReadySets(refresh, backoff, recoveryWaiters, nil)
	if !ok || len(ids) == 0 {
		return "", deferral.Until, deferral.Message(), false
	}
	return ids[0], time.Time{}, "", true
}

func TestSelectReadySetSkipsRecoveryWaiter(t *testing.T) {
	refresh := &tasks.RefreshResult{Rows: []tasks.Row{
		{ID: "waiting", Status: tasks.StatusReady, AutoDrain: true, Priority: 100, RegIndex: 0},
		{ID: "fallback", Status: tasks.StatusReady, AutoDrain: true, Priority: 1, RegIndex: 1},
	}}
	recoveryWaiters := map[string]tasks.RecoveryWaiter{
		"waiting": {
			SetID:   "waiting",
			Preset:  "codex",
			ResetAt: time.Date(2026, 6, 14, 13, 0, 0, 0, time.UTC),
		},
	}

	id, _, _, ok := firstReadySet(refresh, nil, recoveryWaiters)
	if !ok || id != "fallback" {
		t.Fatalf("selectReadySet = (%q,%v), want fallback,true", id, ok)
	}
}

// backoffFor returns a setBackoffFunc deriving each named set's status from
// synthetic Drain history, exercising the same setBackoffStatus the production
// store-backed lookup feeds (ADR-0055).
func backoffFor(history map[string]tasks.SetBackoffInfo, delays []time.Duration, now time.Time) setBackoffFunc {
	return func(setID string) (bool, time.Time) {
		return setBackoffStatus(history[setID], delays, now)
	}
}

func TestSelectReadySetSkipsCrashBackoffUntilElapsed(t *testing.T) {
	now := time.Date(2026, 6, 14, 12, 0, 0, 0, time.UTC)
	refresh := &tasks.RefreshResult{Rows: []tasks.Row{
		{ID: "crashy", Status: tasks.StatusReady, AutoDrain: true, Priority: 100, RegIndex: 0},
	}}
	delays := []time.Duration{time.Minute}
	history := map[string]tasks.SetBackoffInfo{
		"crashy": {ConsecutiveAbnormal: 1, LastAbnormalAt: now},
	}

	id, until, reason, ok := firstReadySet(refresh, backoffFor(history, delays, now), nil)
	if ok || id != "" || !until.Equal(now.Add(time.Minute)) || reason != "set backed off after abnormal drain exit" {
		t.Fatalf("selectReadySet during backoff = (%q,%s,%q,%v)", id, until, reason, ok)
	}

	later := now.Add(2 * time.Minute)
	id, _, _, ok = firstReadySet(refresh, backoffFor(history, delays, later), nil)
	if !ok || id != "crashy" {
		t.Fatalf("selectReadySet after backoff = (%q,%v), want crashy,true", id, ok)
	}
}

func TestSelectReadySetSkipsParkedSet(t *testing.T) {
	now := time.Date(2026, 6, 14, 12, 0, 0, 0, time.UTC)
	refresh := &tasks.RefreshResult{Rows: []tasks.Row{
		{ID: "parked", Status: tasks.StatusReady, AutoDrain: true, Priority: 100, RegIndex: 0},
	}}
	// n=2 consecutive abnormal terminals exceeds the single-entry retry schedule
	// (park threshold = len+1), so the set is parked.
	delays := []time.Duration{time.Minute}
	history := map[string]tasks.SetBackoffInfo{
		"parked": {ConsecutiveAbnormal: 2, LastAbnormalAt: now},
	}

	id, until, reason, ok := firstReadySet(refresh, backoffFor(history, delays, now), nil)
	if ok || id != "" || !until.IsZero() || reason != "set parked after repeated abnormal drain exits" {
		t.Fatalf("selectReadySet parked = (%q,%s,%q,%v)", id, until, reason, ok)
	}

	// A park-clear newer than the latest abnormal terminal lifts the park.
	history["parked"] = tasks.SetBackoffInfo{ConsecutiveAbnormal: 2, LastAbnormalAt: now, ParkClearedAt: now.Add(time.Second)}
	id, _, _, ok = firstReadySet(refresh, backoffFor(history, delays, now), nil)
	if !ok || id != "parked" {
		t.Fatalf("selectReadySet after park-clear = (%q,%v), want parked,true", id, ok)
	}
}

func queueTestTasksDeps(t *testing.T, allFound bool) *tasks.Deps {
	t.Helper()
	// Default isolation (slice 01): point the data dir at a temp location so any
	// store touch lands in a throwaway dir, never the developer's real
	// machine-global store. Only set one when the caller hasn't already isolated
	// XDG_DATA_HOME (e.g. setupSupervisorSpawnRepo pins it to repo/.xdg and seeds
	// the store there) — clobbering it would hide the seeded rows. The
	// guardTestStorePath backstop panics if isolation is ever missed entirely.
	if os.Getenv("XDG_DATA_HOME") == "" {
		t.Setenv("XDG_DATA_HOME", t.TempDir())
	}
	d := tasks.DefaultDeps()
	d.LookPath = func(file string) (string, error) {
		if allFound {
			return "/bin/" + file, nil
		}
		return "", fmt.Errorf("missing %s", file)
	}
	// The store handle is now process-cached; close it at test end so it does not
	// outlive this test's temp data dir (test cleanup, per ADR-0118).
	t.Cleanup(func() { _ = d.CloseStore() })
	return d
}

// recordingTmux captures tmux subcommands so spawn behaviour can be asserted by
// the non-spawn tests that only need a placeholder tmux. It embeds the shared
// stateful fake for the verbs it does not override and replays the old
// session-agnostic seeded behaviour (windowNames + paneList). Dedicated spawn
// tests use a plain tmuxtest.Fake and assert on pane state instead.
type recordingTmux struct {
	*tmuxtest.Fake
	commands      [][]string
	hasSession    bool
	windowNames   map[string]bool
	paneList      string
	splitErr      error
	nextSplitPane int
}

func newRecordingTmux(hasSession bool, windowNames string) *recordingTmux {
	rt := &recordingTmux{Fake: &tmuxtest.Fake{}, hasSession: hasSession, windowNames: map[string]bool{}}
	for _, w := range strings.Split(windowNames, "\n") {
		if w != "" {
			rt.windowNames[w] = true
		}
	}
	return rt
}

func (rt *recordingTmux) record(args ...string) { rt.commands = append(rt.commands, args) }

func (rt *recordingTmux) HasSession(string) bool { return rt.hasSession }

func (rt *recordingTmux) NewSession(name, dir string) error {
	rt.record("new-session", name, dir)
	return nil
}

func (rt *recordingTmux) WindowExists(session, name string) (bool, error) {
	rt.record("list-windows", "-t", session)
	return rt.windowNames[name], nil
}

func (rt *recordingTmux) NewWindow(session, name, dir string) (string, error) {
	rt.record("new-window", "-d", "-P", "-t", session, "-n", name, "-c", dir)
	return "%3", nil
}

func (rt *recordingTmux) SplitWindow(session, name, dir string) (string, error) {
	rt.record("split-window", "-d", "-P", "-t", session+":"+name, "-c", dir)
	if rt.splitErr != nil {
		return "", rt.splitErr
	}
	if rt.nextSplitPane == 0 {
		rt.nextSplitPane = 7
	}
	paneID := fmt.Sprintf("%%%d", rt.nextSplitPane)
	rt.nextSplitPane++
	return paneID, nil
}

func (rt *recordingTmux) RetileWindow(session, name string) error {
	rt.record("select-layout", "-t", session+":"+name, "tiled")
	return nil
}

func (rt *recordingTmux) WindowPanes(session, name string) ([]string, error) {
	rt.record("list-panes", "-t", session+":"+name)
	return recorderPaneIDs(rt.paneList), nil
}

func (rt *recordingTmux) FindTaggedPane(session string, tag tmuxmod.PaneTag, value string) (string, error) {
	rt.record("list-panes", "-t", session+":"+drainWindowName)
	for paneID, tags := range rt.PaneTagValues {
		if tags[tag] == value {
			return paneID, nil
		}
	}
	return "", nil
}

func (rt *recordingTmux) TagPane(paneID string, tag tmuxmod.PaneTag, value string) error {
	rt.record("set-option", "-p", "-t", paneID, value)
	return rt.Fake.TagPane(paneID, tag, value)
}

func (rt *recordingTmux) SelectPane(paneID string) error {
	rt.record("select-pane", "-t", paneID)
	return nil
}

func (rt *recordingTmux) SwitchClient(target string) error {
	rt.record("switch-client", "-t", target)
	return nil
}

func (rt *recordingTmux) SendKeys(paneID string, keys ...string) error {
	rt.record(append([]string{"send-keys", "-t", paneID}, keys...)...)
	return nil
}

func recorderPaneIDs(list string) []string {
	var ids []string
	for _, line := range strings.Split(list, "\n") {
		if id := strings.TrimSpace(line); id != "" {
			ids = append(ids, id)
		}
	}
	return ids
}

func recorderTaggedPane(list, value string) string {
	for _, line := range strings.Split(list, "\n") {
		line = strings.TrimSpace(line)
		idx := strings.LastIndex(line, " %")
		if idx < 0 {
			continue
		}
		if strings.TrimSpace(line[:idx]) == value {
			return strings.TrimSpace(line[idx+1:])
		}
	}
	return ""
}

func (rt *recordingTmux) findCommand(verb string) ([]string, bool) {
	for _, c := range rt.commands {
		if len(c) > 0 && c[0] == verb {
			return c, true
		}
	}
	return nil, false
}

func (rt *recordingTmux) countCommand(verb string) int {
	var n int
	for _, c := range rt.commands {
		if len(c) > 0 && c[0] == verb {
			n++
		}
	}
	return n
}

func actionableDecision() Decision {
	return Decision{
		Project:   "proj",
		TaskSetID: "2026-06-14-queue",
		scan: projectScan{
			ProjectPath: "/checkout",
			SessionName: "proj-session",
		},
	}
}

func TestProvisionWorktreeAddsFreshBranchFromHead(t *testing.T) {
	now := time.Date(2026, 6, 14, 9, 8, 7, 0, time.UTC)
	var gotDir string
	var gotArgs []string
	d := &Deps{
		Tasks: &tasks.Deps{
			FS: &deps.MockFileSystem{
				GetenvFunc:       func(key string) string { return "/xdg" },
				EvalSymlinksFunc: func(path string) (string, error) { return path, nil },
				MkdirAllFunc: func(path string, perm os.FileMode) error {
					return nil
				},
			},
			Git: &deps.MockGit{CommandInDirFunc: func(dir string, args ...string) (string, error) {
				if reflect.DeepEqual(args, []string{"rev-parse", "--git-common-dir"}) {
					return filepath.Join("/repo", ".git"), nil
				}
				gotDir = dir
				gotArgs = append([]string(nil), args...)
				return "", nil
			}},
		},
		Now: func() time.Time { return now },
	}

	wt, err := provisionWorktree(d, "/repo", "Set With Spaces")
	if err != nil {
		t.Fatalf("provisionWorktree: %v", err)
	}

	wantBranch := "pop/set-with-spaces/20260614T090807Z"
	wantPath := filepath.Join("/xdg", "pop", "queue", "worktrees", "repo-"+repoHashForTest(t, filepath.Join("/repo", ".git")), "set-with-spaces")
	if wt.Branch != wantBranch || wt.Path != wantPath {
		t.Fatalf("provisioned = %+v, want branch %q path %q", wt, wantBranch, wantPath)
	}
	if gotDir != "/repo" {
		t.Fatalf("git worktree add dir = %q, want /repo", gotDir)
	}
	wantArgs := []string{"worktree", "add", "-b", wantBranch, wantPath, "HEAD"}
	if !reflect.DeepEqual(gotArgs, wantArgs) {
		t.Fatalf("git args = %#v, want %#v", gotArgs, wantArgs)
	}
}

func TestPrepareWorktreeDrainReusesBindingWithoutWorktreeAdd(t *testing.T) {
	xdg := t.TempDir()
	t.Setenv("XDG_DATA_HOME", xdg)
	now := time.Date(2026, 6, 14, 9, 8, 7, 0, time.UTC)
	repoHash := repoHashForTest(t, filepath.Join("/repo", ".git"))
	boundPath := filepath.Join(xdg, "pop", "queue", "worktrees", "repo-"+repoHash, "2026-06-14-queue")
	worktreeAddCalls := 0
	real := deps.NewRealFileSystem()
	d := worktreeProvisionDeps(t, now, nil)
	d.Tasks.FS = &deps.MockFileSystem{
		GetenvFunc:       func(key string) string { return xdg },
		EvalSymlinksFunc: real.EvalSymlinks,
		MkdirAllFunc:     real.MkdirAll,
		WriteFileFunc:    real.WriteFile,
		ReadFileFunc:     real.ReadFile,
		RenameFunc:       real.Rename,
		StatFunc:         real.Stat,
	}
	d.Tasks.Git = &deps.MockGit{CommandInDirFunc: func(dir string, args ...string) (string, error) {
		if reflect.DeepEqual(args, []string{"rev-parse", "--git-common-dir"}) {
			return filepath.Join("/repo", ".git"), nil
		}
		if reflect.DeepEqual(args, []string{"rev-parse", "--show-toplevel"}) {
			if dir == "/repo" || dir == boundPath {
				return dir, nil
			}
		}
		if reflect.DeepEqual(args, []string{"worktree", "list", "--porcelain"}) {
			return "worktree " + boundPath + "\n", nil
		}
		if len(args) >= 2 && args[0] == "worktree" && args[1] == "add" {
			worktreeAddCalls++
		}
		return "", nil
	}}
	repoKey, err := resolveRepoKey(d, "/repo")
	if err != nil {
		t.Fatal(err)
	}
	if err := real.MkdirAll(boundPath, 0o755); err != nil {
		t.Fatal(err)
	}
	seedBindingStore(t, d.Tasks, map[string]WorktreeBinding{
		setScopedKey(repoKey, "2026-06-14-queue"): {
			RuntimePath: boundPath,
			Branch:      "pop/2026-06-14-queue/20260614T090807Z",
			Project:     "proj",
		},
	})

	dec := actionableDecision()
	dec.scan.RuntimePath = "/repo"
	dec.scan.ProjectPath = "/repo"

	got, _ := prepareWorktreeDrain(d, dec)

	if worktreeAddCalls != 0 {
		t.Fatalf("git worktree add calls = %d, want 0 when binding is valid", worktreeAddCalls)
	}
	if got.scan.RuntimePath != boundPath || got.scan.ProjectPath != boundPath {
		t.Fatalf("expected bound checkout %+v, got %+v", boundPath, got.scan)
	}
	if got.scan.SessionName != dec.scan.SessionName {
		t.Fatalf("SessionName = %q, want originating project session %q", got.scan.SessionName, dec.scan.SessionName)
	}
}

func TestPrepareWorktreeDrainRefusesInvalidBinding(t *testing.T) {
	xdg := t.TempDir()
	t.Setenv("XDG_DATA_HOME", xdg)
	now := time.Date(2026, 6, 14, 9, 8, 7, 0, time.UTC)
	repoHash := repoHashForTest(t, filepath.Join("/repo", ".git"))
	missingPath := filepath.Join(xdg, "pop", "queue", "worktrees", "repo-"+repoHash, "2026-06-14-queue")
	real := deps.NewRealFileSystem()
	d := worktreeProvisionDeps(t, now, nil)
	d.Tasks.FS = &deps.MockFileSystem{
		GetenvFunc:       func(key string) string { return xdg },
		EvalSymlinksFunc: real.EvalSymlinks,
		MkdirAllFunc:     real.MkdirAll,
		WriteFileFunc:    real.WriteFile,
		ReadFileFunc:     real.ReadFile,
		RenameFunc:       real.Rename,
		StatFunc: func(path string) (os.FileInfo, error) {
			return nil, os.ErrNotExist
		},
	}
	repoKey, err := resolveRepoKey(d, "/repo")
	if err != nil {
		t.Fatal(err)
	}
	seedBindingStore(t, d.Tasks, map[string]WorktreeBinding{
		setScopedKey(repoKey, "2026-06-14-queue"): {
			RuntimePath: missingPath,
			Branch:      "pop/2026-06-14-queue/20260614T090807Z",
			Project:     "proj",
		},
	})

	dec := actionableDecision()
	dec.scan.RuntimePath = "/repo"
	dec.scan.ProjectPath = "/repo"

	got, refusal := prepareWorktreeDrain(d, dec)

	if got.Actionable() {
		t.Fatalf("invalid binding must refuse spawn, got actionable %+v", got)
	}
	if !strings.Contains(refusal, "pop tasks unbind-worktree") {
		t.Fatalf("refusal must mention unbind: %q", refusal)
	}
	if got.scan.RuntimePath != "/repo" {
		t.Fatalf("must not fall back in-place, got runtime %q", got.scan.RuntimePath)
	}
}

// The drain-spawn tests drive the tmux.EnsureTaggedPane composite through a
// plain stateful fake and assert on the resulting pane state — the window the
// pane lives in, its @pop_set tag, and the command sent — rather than on tmux
// argument arrays (ADR-0142).

// soleDrainPane returns the single pane in session's drain window, failing when
// the count is not one.
func soleDrainPane(t *testing.T, f *tmuxtest.Fake, session string) string {
	t.Helper()
	panes := f.Windows[session][drainWindowName]
	if len(panes) != 1 {
		t.Fatalf("drain window %s:%s panes = %v, want exactly one", session, drainWindowName, panes)
	}
	return panes[0]
}

func assertFakeTagged(t *testing.T, f *tmuxtest.Fake, pane, setID string) {
	t.Helper()
	if got := f.PaneTagValues[pane][tmuxmod.TagSet]; got != setID {
		t.Fatalf("pane %s @pop_set = %q, want %q", pane, got, setID)
	}
}

func assertFakeSentImplement(t *testing.T, f *tmuxtest.Fake, pane, setID string) {
	t.Helper()
	joined := strings.Join(f.SentCommands[pane], " || ")
	if !strings.Contains(joined, "pop tasks implement "+setID) {
		t.Fatalf("pane %s sent %q, want plain `pop tasks implement %s`", pane, joined, setID)
	}
	if strings.Contains(joined, "--yes") || strings.Contains(joined, "--agent") {
		t.Fatalf("queue spawn must not inject --yes/--agent flags: %q", joined)
	}
}

func TestSpawnCreatesQueueWindowWhenAbsent(t *testing.T) {
	f := &tmuxtest.Fake{}
	d := &Deps{Tmux: f}

	if err := Spawn(d, actionableDecision()); err != nil {
		t.Fatalf("Spawn: %v", err)
	}

	if f.Live["proj-session"] != "/checkout" {
		t.Fatalf("Live[proj-session] = %q, want /checkout (detached session created)", f.Live["proj-session"])
	}
	pane := soleDrainPane(t, f, "proj-session")
	assertFakeTagged(t, f, pane, "2026-06-14-queue")
	assertFakeSentImplement(t, f, pane, "2026-06-14-queue")
	if len(f.WindowRetiled) != 0 {
		t.Fatalf("a fresh single-pane drain window must not be retiled, got %v", f.WindowRetiled)
	}
}

func TestSpawnWorktreeDrainPassesRuntimeOverrideAndUsesWorktreeDir(t *testing.T) {
	f := &tmuxtest.Fake{}
	dec := actionableDecision()
	dec.pinRuntimePath = true
	dec.scan.ProjectPath = "/pop/worktrees/repo/set"
	dec.scan.RuntimePath = "/pop/worktrees/repo/set"
	d := &Deps{Tmux: f}

	if err := Spawn(d, dec); err != nil {
		t.Fatalf("Spawn: %v", err)
	}

	// The originating project session hosts the drain, rooted at the worktree
	// checkout — never a worktree-derived session name.
	if f.Live["proj-session"] != "/pop/worktrees/repo/set" {
		t.Fatalf("Live[proj-session] = %q, want worktree cwd", f.Live["proj-session"])
	}
	pane := soleDrainPane(t, f, "proj-session")
	assertFakeTagged(t, f, pane, "2026-06-14-queue")
	joined := strings.Join(f.SentCommands[pane], " || ")
	if !strings.Contains(joined, "--task-runtime-path /pop/worktrees/repo/set") {
		t.Fatalf("drain command %q must pass the runtime override", joined)
	}
}

func TestSpawnReusesQueueWindowWhenSessionExists(t *testing.T) {
	// Session and drain window already exist, with an untagged sibling pane: the
	// spawn splits and tags a fresh pane and retiles rather than creating either.
	f := &tmuxtest.Fake{
		Live:    map[string]string{"proj-session": "/checkout"},
		Windows: map[string]map[string][]string{"proj-session": {drainWindowName: {"%1"}}},
	}
	d := &Deps{Tmux: f}

	if err := Spawn(d, actionableDecision()); err != nil {
		t.Fatalf("Spawn: %v", err)
	}

	panes := f.Windows["proj-session"][drainWindowName]
	if len(panes) != 2 {
		t.Fatalf("drain window panes = %v, want the sibling plus one split pane", panes)
	}
	newPane := panes[1]
	assertFakeTagged(t, f, newPane, "2026-06-14-queue")
	assertFakeSentImplement(t, f, newPane, "2026-06-14-queue")
	if want := "proj-session:" + drainWindowName; len(f.WindowRetiled) != 1 || f.WindowRetiled[0] != want {
		t.Fatalf("WindowRetiled = %v, want [%s] (split window retiled)", f.WindowRetiled, want)
	}
}

func TestSpawnDoesNotTargetLowestIndexWindow(t *testing.T) {
	// Sibling numeric windows exist but no drain window: the spawn creates the
	// named pop-queue window rather than landing in an existing numeric window.
	f := &tmuxtest.Fake{
		Live:    map[string]string{"proj-session": "/checkout"},
		Windows: map[string]map[string][]string{"proj-session": {"0": {"%1"}, "1": {"%2"}}},
	}
	d := &Deps{Tmux: f}

	if err := Spawn(d, actionableDecision()); err != nil {
		t.Fatalf("Spawn: %v", err)
	}

	pane := soleDrainPane(t, f, "proj-session")
	assertFakeTagged(t, f, pane, "2026-06-14-queue")
	assertFakeSentImplement(t, f, pane, "2026-06-14-queue")
	if len(f.WindowRetiled) != 0 {
		t.Fatalf("a fresh single-pane drain window must not be retiled, got %v", f.WindowRetiled)
	}
}

func TestSpawnReusesExistingPaneForSameSet(t *testing.T) {
	// A second spawn for the same set reuses the already-tagged pane: no second
	// pane is split, no re-tag, and the command is sent into the same pane twice.
	f := &tmuxtest.Fake{}
	d := &Deps{Tmux: f}

	if err := Spawn(d, actionableDecision()); err != nil {
		t.Fatalf("first Spawn: %v", err)
	}
	pane := soleDrainPane(t, f, "proj-session")

	if err := Spawn(d, actionableDecision()); err != nil {
		t.Fatalf("second Spawn: %v", err)
	}

	if got := soleDrainPane(t, f, "proj-session"); got != pane {
		t.Fatalf("second spawn used pane %q, want the reused pane %q (no new split)", got, pane)
	}
	if got := f.SentCommands[pane]; len(got) != 2 {
		t.Fatalf("pane %s received %d commands, want 2 (one per spawn)", pane, len(got))
	}
	if len(f.WindowRetiled) != 0 {
		t.Fatalf("reusing a pane must not split or retile, got retiles %v", f.WindowRetiled)
	}
	if len(f.Respawned) != 0 {
		t.Fatalf("same checkout must not respawn the pane, got %v", f.Respawned)
	}
}

func TestSpawnReusesPaneCorrectsDirectory(t *testing.T) {
	f := &tmuxtest.Fake{}
	d := &Deps{Tmux: f}

	first := actionableDecision()
	if err := Spawn(d, first); err != nil {
		t.Fatalf("first Spawn: %v", err)
	}
	pane := soleDrainPane(t, f, "proj-session")

	second := actionableDecision()
	second.pinRuntimePath = true
	second.scan.ProjectPath = "/pop/worktrees/repo/set"
	second.scan.RuntimePath = "/pop/worktrees/repo/set"
	if err := Spawn(d, second); err != nil {
		t.Fatalf("second Spawn: %v", err)
	}
	if got := soleDrainPane(t, f, "proj-session"); got != pane {
		t.Fatalf("second spawn used pane %q, want reused %q", got, pane)
	}
	if got := f.Respawned[pane]; got != "/pop/worktrees/repo/set" {
		t.Fatalf("Respawned[%s] = %q, want worktree checkout", pane, got)
	}
	joined := strings.Join(f.SentCommands[pane], " || ")
	if !strings.Contains(joined, "--task-runtime-path /pop/worktrees/repo/set") {
		t.Fatalf("drain command %q must pass the runtime override", joined)
	}
}

func TestSpawnNonActionableNoOp(t *testing.T) {
	f := &tmuxtest.Fake{}
	d := &Deps{Tmux: f}

	if err := Spawn(d, Decision{Project: "busy", Busy: true}); err != nil {
		t.Fatalf("Spawn: %v", err)
	}
	if len(f.Live) != 0 || len(f.Windows) != 0 || len(f.SentCommands) != 0 {
		t.Fatalf("non-actionable decision must not touch tmux: live=%v windows=%v sent=%v", f.Live, f.Windows, f.SentCommands)
	}
}

// assertReusesFreshPane checks the supervisor spawn reused a freshly created
// single-pane drain window (no split, no retile) and sent the drain command
// into it. Still recorder-based because the supervisor tick tests assert on the
// recorded subcommands.
func assertReusesFreshPane(t *testing.T, rt *recordingTmux, paneID string) {
	t.Helper()
	if _, ok := rt.findCommand("split-window"); ok {
		t.Fatal("must reuse the freshly created window's pane, not split a second pane")
	}
	if _, ok := rt.findCommand("select-layout"); ok {
		t.Fatal("must not retile a single-pane drain window")
	}
	sendKeys, ok := rt.findCommand("send-keys")
	if !ok {
		t.Fatal("expected the drain command to be sent into the pane")
	}
	if !argsContain(sendKeys, "-t", paneID) {
		t.Fatalf("send-keys must target reused pane %s: %v", paneID, sendKeys)
	}
}

func argsContain(args []string, want ...string) bool {
	for i := 0; i+len(want) <= len(args); i++ {
		match := true
		for j, w := range want {
			if args[i+j] != w {
				match = false
				break
			}
		}
		if match {
			return true
		}
	}
	return false
}

func worktreeProvisionDeps(t *testing.T, now time.Time, addErr error) *Deps {
	t.Helper()
	dataHome := t.TempDir()
	wtPath := filepath.Join(dataHome, "pop", "queue", "worktrees", "repo-"+repoHashForTest(t, filepath.Join("/repo", ".git")), "2026-06-14-queue")
	real := deps.NewRealFileSystem()
	gitMock := func(dir string, args ...string) (string, error) {
		switch {
		case reflect.DeepEqual(args, []string{"rev-parse", "--git-common-dir"}):
			return filepath.Join("/repo", ".git"), nil
		case reflect.DeepEqual(args, []string{"rev-parse", "--git-dir"}):
			if dir == "/repo" {
				return filepath.Join("/repo", ".git"), nil
			}
			if dir == wtPath {
				return filepath.Join("/repo", ".git", "worktrees", "linked"), nil
			}
		case reflect.DeepEqual(args, []string{"rev-parse", "--show-toplevel"}):
			if dir == "/repo" || dir == wtPath {
				return dir, nil
			}
		case reflect.DeepEqual(args, []string{"worktree", "list", "--porcelain"}):
			return "worktree /repo\nHEAD abc\nbranch refs/heads/main\n\n", nil
		case reflect.DeepEqual(args, []string{"branch", "--show-current"}):
			return "main", nil
		case len(args) >= 2 && args[0] == "worktree" && args[1] == "add":
			return "", addErr
		}
		if addErr != nil {
			return "", addErr
		}
		return "", nil
	}
	return &Deps{
		Tasks: &tasks.Deps{
			FS: &deps.MockFileSystem{
				GetenvFunc: func(key string) string {
					if key == "XDG_DATA_HOME" {
						return dataHome
					}
					return ""
				},
				EvalSymlinksFunc: func(path string) (string, error) { return path, nil },
				MkdirAllFunc:     real.MkdirAll,
				WriteFileFunc:    real.WriteFile,
				ReadFileFunc:     real.ReadFile,
				RenameFunc:       real.Rename,
			},
			Git: &deps.MockGit{CommandInDirFunc: gitMock},
		},
		Project: &project.Deps{
			FS: &deps.MockFileSystem{
				StatFunc: func(path string) (os.FileInfo, error) {
					return nil, os.ErrNotExist
				},
			},
			Git: &deps.MockGit{CommandInDirFunc: gitMock},
		},
		Now: func() time.Time { return now },
	}
}

func repoHashForTest(t *testing.T, commonDir string) string {
	t.Helper()
	id, err := tasks.ResolveRepositoryIdentity(&tasks.Deps{
		FS: &deps.MockFileSystem{
			GetenvFunc:       func(key string) string { return "/xdg" },
			EvalSymlinksFunc: func(path string) (string, error) { return path, nil },
		},
		Git: &deps.MockGit{CommandInDirFunc: func(dir string, args ...string) (string, error) {
			return commonDir, nil
		}},
	}, "/repo")
	if err != nil {
		t.Fatal(err)
	}
	return id.ShortHash
}

func testScopedKey(t *testing.T, repoPath, setID string) string {
	return testScopedKeyFor(t, queueDataDeps(t), repoPath, repoPath, setID)
}

func testScopedKeyFor(t *testing.T, td *tasks.Deps, projectPath, runtimePath, setID string) string {
	t.Helper()
	key, err := scopedKeyForPaths(&Deps{Tasks: td}, projectPath, runtimePath, setID)
	if err != nil {
		t.Fatal(err)
	}
	return key
}

func seedBindingStore(t *testing.T, td *tasks.Deps, bindings map[string]WorktreeBinding) {
	t.Helper()
	// Replace the whole set the tests reason about: clear any rows a prior seed
	// left, then write the given ones. An empty map therefore clears the store.
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

func loadBindingStore(t *testing.T, td *tasks.Deps) map[string]WorktreeBinding {
	t.Helper()
	all, err := binding.AllBindings(td)
	if err != nil {
		t.Fatal(err)
	}
	return all
}
