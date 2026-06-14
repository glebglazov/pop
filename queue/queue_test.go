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

	"github.com/glebglazov/pop/internal/deps"
	"github.com/glebglazov/pop/project"
	"github.com/glebglazov/pop/tasks"
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
				{ID: "only", Status: tasks.StatusReady, Priority: 0},
			},
			want: "only",
			ok:   true,
		},
		{
			name: "highest priority wins, non-ready ignored",
			rows: []tasks.Row{
				{ID: "low", Status: tasks.StatusReady, Priority: 1, RegIndex: 0},
				{ID: "blocked-high", Status: tasks.StatusBlocked, Priority: 100, RegIndex: 1},
				{ID: "high", Status: tasks.StatusReady, Priority: 50, RegIndex: 2},
				{ID: "mid", Status: tasks.StatusReady, Priority: 10, RegIndex: 3},
			},
			want: "high",
			ok:   true,
		},
		{
			name: "priority tie breaks by registration order",
			rows: []tasks.Row{
				{ID: "second", Status: tasks.StatusReady, Priority: 5, RegIndex: 4},
				{ID: "first", Status: tasks.StatusReady, Priority: 5, RegIndex: 1},
			},
			want: "first",
			ok:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := selectReadySetID(tt.rows)
			if ok != tt.ok || got != tt.want {
				t.Fatalf("selectReadySet = (%q, %v), want (%q, %v)", got, ok, tt.want, tt.ok)
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

func TestDecideProjectIdleSkip(t *testing.T) {
	refreshCalled := false
	d := &Deps{
		Tasks: queueTestTasksDeps(true),
		ReadLock: func(runtimePath string) *tasks.RuntimeLockStatus {
			return liveLock(runtimePath)
		},
		Refresh: func(defPath string) (*tasks.RefreshResult, error) {
			refreshCalled = true
			return &tasks.RefreshResult{}, nil
		},
	}

	dec := decideProject(d, projectScan{Name: "proj", RuntimePath: "/co", DefinitionPath: "/def"}, []string{"claude"}, &DaemonState{Version: 1}, time.Now())

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
		Tasks: queueTestTasksDeps(true),
		ReadLock: func(runtimePath string) *tasks.RuntimeLockStatus {
			return idleLock(runtimePath)
		},
		Refresh: func(defPath string) (*tasks.RefreshResult, error) {
			return &tasks.RefreshResult{Rows: []tasks.Row{
				{ID: "low", Status: tasks.StatusReady, Priority: 1, RegIndex: 0},
				{ID: "top", Status: tasks.StatusReady, Priority: 99, RegIndex: 1},
				{ID: "blocked", Status: tasks.StatusBlocked, Priority: 100, RegIndex: 2},
			}}, nil
		},
	}

	dec := decideProject(d, projectScan{Name: "proj", RuntimePath: "/co", DefinitionPath: "/def"}, []string{"claude"}, &DaemonState{Version: 1}, time.Now())

	if dec.Busy || dec.Err != nil {
		t.Fatalf("idle project with ready work should be actionable, got %+v", dec)
	}
	if dec.TaskSetID != "top" {
		t.Fatalf("expected highest-priority ready set 'top', got %q", dec.TaskSetID)
	}
	if dec.DefaultAgent != "claude" {
		t.Fatalf("default agent = %q, want claude", dec.DefaultAgent)
	}
	if !dec.Actionable() {
		t.Fatalf("expected actionable decision, got %+v", dec)
	}
}

func TestDecideProjectNoReadySet(t *testing.T) {
	d := &Deps{
		Tasks:    queueTestTasksDeps(true),
		ReadLock: func(runtimePath string) *tasks.RuntimeLockStatus { return idleLock(runtimePath) },
		Refresh: func(defPath string) (*tasks.RefreshResult, error) {
			return &tasks.RefreshResult{Rows: []tasks.Row{
				{ID: "done", Status: tasks.StatusDone, Priority: 5},
				{ID: "blocked", Status: tasks.StatusBlocked, Priority: 5},
			}}, nil
		},
	}

	dec := decideProject(d, projectScan{Name: "proj", RuntimePath: "/co", DefinitionPath: "/def"}, []string{"claude"}, &DaemonState{Version: 1}, time.Now())

	if dec.Actionable() {
		t.Fatalf("a project with no ready set must not be actionable: %+v", dec)
	}
	if dec.Reason != "no ready set" {
		t.Fatalf("expected reason 'no ready set', got %q", dec.Reason)
	}
}

func TestDecideProjectReadsRepoWorktreeReady(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, ".pop.toml"), []byte("worktree_ready = true\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	d := &Deps{
		Tasks:   queueTestTasksDeps(true),
		Project: &project.Deps{FS: deps.NewRealFileSystem()},
		ReadLock: func(runtimePath string) *tasks.RuntimeLockStatus {
			return idleLock(runtimePath)
		},
		Refresh: func(defPath string) (*tasks.RefreshResult, error) {
			return &tasks.RefreshResult{}, nil
		},
	}

	dec := decideProject(d, projectScan{Name: "proj", ProjectPath: root, RuntimePath: root, DefinitionPath: root}, []string{"claude"}, &DaemonState{Version: 1}, time.Now())

	if !dec.WorktreeReady {
		t.Fatalf("WorktreeReady = false, want true: %+v", dec)
	}
	if dec.ProjectConfigError != "" {
		t.Fatalf("ProjectConfigError = %q, want empty", dec.ProjectConfigError)
	}
}

func TestDecideProjectMalformedRepoConfigReportsAndDegrades(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, ".pop.toml"), []byte("worktree_ready =\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	d := &Deps{
		Tasks:   queueTestTasksDeps(true),
		Project: &project.Deps{FS: deps.NewRealFileSystem()},
		ReadLock: func(runtimePath string) *tasks.RuntimeLockStatus {
			return idleLock(runtimePath)
		},
		Refresh: func(defPath string) (*tasks.RefreshResult, error) {
			return &tasks.RefreshResult{}, nil
		},
	}

	dec := decideProject(d, projectScan{Name: "proj", ProjectPath: root, RuntimePath: root, DefinitionPath: root}, []string{"claude"}, &DaemonState{Version: 1}, time.Now())

	if dec.WorktreeReady {
		t.Fatalf("malformed .pop.toml must degrade to not worktree-ready: %+v", dec)
	}
	if !strings.Contains(dec.ProjectConfigError, ".pop.toml") {
		t.Fatalf("ProjectConfigError = %q, want .pop.toml parse error", dec.ProjectConfigError)
	}
}

func TestDecideProjectWorktreeReadyTreatsLiveSpawnAsBusy(t *testing.T) {
	xdg := t.TempDir()
	t.Setenv("XDG_DATA_HOME", xdg)
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, ".pop.toml"), []byte("worktree_ready = true\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	d := &Deps{
		Tasks:   queueTestTasksDeps(true),
		Project: &project.Deps{FS: deps.NewRealFileSystem()},
		ReadLock: func(runtimePath string) *tasks.RuntimeLockStatus {
			if runtimePath == "/pop/worktrees/repo/set" {
				return liveLock(runtimePath)
			}
			return idleLock(runtimePath)
		},
		Refresh: func(defPath string) (*tasks.RefreshResult, error) {
			return &tasks.RefreshResult{Rows: []tasks.Row{
				{ID: "ready", Status: tasks.StatusReady, Priority: 10, RegIndex: 0},
			}}, nil
		},
	}
	if err := AppendJournalEntry(d.Tasks, JournalEntry{
		Event:       JournalEventSpawn,
		Project:     "proj",
		SetID:       "ready",
		RuntimePath: "/pop/worktrees/repo/set",
	}); err != nil {
		t.Fatal(err)
	}

	dec := decideProject(d, projectScan{Name: "proj", ProjectPath: root, RuntimePath: root, DefinitionPath: root}, []string{"claude"}, &DaemonState{Version: 1}, time.Now())

	if !dec.Busy || dec.TaskSetID != "ready" {
		t.Fatalf("expected live worktree spawn to make project busy, got %+v", dec)
	}
	if dec.lockStatus == nil || dec.lockStatus.RuntimePath != "/pop/worktrees/repo/set" {
		t.Fatalf("lockStatus = %+v, want worktree lock", dec.lockStatus)
	}
}

func TestDecideProjectDispatchesWorktreeReadyReadySetsConcurrently(t *testing.T) {
	xdg := t.TempDir()
	t.Setenv("XDG_DATA_HOME", xdg)
	root := initMergeabilityRepo(t)
	if err := os.WriteFile(filepath.Join(root, ".pop.toml"), []byte("worktree_ready = true\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	td := queueDataDeps(t)
	td.LookPath = func(file string) (string, error) { return "/bin/" + file, nil }
	d := &Deps{
		Tasks:   td,
		Project: &project.Deps{FS: deps.NewRealFileSystem()},
		ReadLock: func(runtimePath string) *tasks.RuntimeLockStatus {
			if runtimePath == "/pop/worktrees/repo/set-a" {
				lock := liveLock(runtimePath)
				lock.Metadata.SetID = "set-a"
				return lock
			}
			return idleLock(runtimePath)
		},
		Refresh: func(defPath string) (*tasks.RefreshResult, error) {
			return &tasks.RefreshResult{Rows: []tasks.Row{
				{ID: "set-a", Status: tasks.StatusReady, Priority: 30, RegIndex: 0},
				{ID: "set-b", Status: tasks.StatusReady, Priority: 20, RegIndex: 1},
				{ID: "set-c", Status: tasks.StatusReady, Priority: 10, RegIndex: 2},
				{ID: "blocked", Status: tasks.StatusBlocked, Priority: 100, RegIndex: 3},
			}}, nil
		},
	}
	if err := AppendJournalEntry(td, JournalEntry{
		Event:       JournalEventSpawn,
		Project:     "proj",
		SetID:       "set-a",
		RuntimePath: "/pop/worktrees/repo/set-a",
	}); err != nil {
		t.Fatal(err)
	}

	decisions := decideProjectDispatches(d, projectScan{Name: "proj", ProjectPath: root, RuntimePath: root, DefinitionPath: root}, []string{"claude"}, &DaemonState{Version: 1}, time.Now())

	var busy []string
	var actionable []string
	for _, dec := range decisions {
		if dec.Busy {
			busy = append(busy, dec.TaskSetID)
		}
		if dec.Actionable() {
			actionable = append(actionable, dec.TaskSetID)
			if !dec.WorktreeReady {
				t.Fatalf("actionable worktree decision lost WorktreeReady: %+v", dec)
			}
		}
	}
	if !reflect.DeepEqual(busy, []string{"set-a"}) {
		t.Fatalf("busy sets = %#v, want set-a", busy)
	}
	if !reflect.DeepEqual(actionable, []string{"set-b", "set-c"}) {
		t.Fatalf("actionable sets = %#v, want set-b,set-c", actionable)
	}
}

func TestDecideProjectDispatchesNonWorktreeReadyKeepsSingleInPlaceDrain(t *testing.T) {
	d := &Deps{
		Tasks:    queueTestTasksDeps(true),
		ReadLock: func(runtimePath string) *tasks.RuntimeLockStatus { return idleLock(runtimePath) },
		Refresh: func(defPath string) (*tasks.RefreshResult, error) {
			return &tasks.RefreshResult{Rows: []tasks.Row{
				{ID: "top", Status: tasks.StatusReady, Priority: 30, RegIndex: 0},
				{ID: "next", Status: tasks.StatusReady, Priority: 20, RegIndex: 1},
			}}, nil
		},
	}

	decisions := decideProjectDispatches(d, projectScan{Name: "proj", RuntimePath: "/co", DefinitionPath: "/def"}, []string{"claude"}, &DaemonState{Version: 1}, time.Now())

	if len(decisions) != 1 || !decisions[0].Actionable() || decisions[0].TaskSetID != "top" {
		t.Fatalf("non-worktree-ready dispatches = %+v, want one in-place top drain", decisions)
	}
}

func TestSelectDefaultAgentSkipsMissingAndCooled(t *testing.T) {
	now := time.Date(2026, 6, 14, 12, 0, 0, 0, time.UTC)
	d := &Deps{Tasks: queueTestTasksDeps(false)}
	d.Tasks.LookPath = func(file string) (string, error) {
		if file == "codex" {
			return "/bin/codex", nil
		}
		return "", fmt.Errorf("missing %s", file)
	}
	state := &DaemonState{Version: 1, AgentCooldowns: map[string]time.Time{
		"opencode": now.Add(time.Hour),
	}}

	agent, _, notes, ok := selectDefaultAgent(d, []string{"claude", "opencode", "codex"}, state, now)
	if !ok || agent != "codex" {
		t.Fatalf("selectDefaultAgent = (%q, %v), want codex,true; notes=%+v", agent, ok, notes)
	}
	if len(notes) != 2 {
		t.Fatalf("notes = %+v, want missing claude and cooling opencode", notes)
	}
}

func TestSelectDefaultAgentAllCooledWaits(t *testing.T) {
	now := time.Date(2026, 6, 14, 12, 0, 0, 0, time.UTC)
	first := now.Add(10 * time.Minute)
	second := now.Add(time.Hour)
	d := &Deps{Tasks: queueTestTasksDeps(true)}
	state := &DaemonState{Version: 1, AgentCooldowns: map[string]time.Time{
		"claude": first,
		"codex":  second,
	}}

	agent, waitUntil, _, ok := selectDefaultAgent(d, []string{"claude", "codex"}, state, now)
	if ok || agent != "" || !waitUntil.Equal(first) {
		t.Fatalf("selectDefaultAgent = (%q,%s,%v), want wait until %s", agent, waitUntil, ok, first)
	}
}

func TestSelectReadySetSkipsBackedOffPinnedSet(t *testing.T) {
	now := time.Date(2026, 6, 14, 12, 0, 0, 0, time.UTC)
	refresh := &tasks.RefreshResult{Rows: []tasks.Row{
		{ID: "pinned", Status: tasks.StatusReady, Priority: 100, RegIndex: 0},
		{ID: "fallback", Status: tasks.StatusReady, Priority: 1, RegIndex: 1},
	}}
	repoKey := "test-repo"
	state := &DaemonState{Version: 1, SetBackoffs: map[string]time.Time{
		setScopedKey(repoKey, "pinned"): now.Add(time.Hour),
	}}

	id, _, _, ok := selectReadySet(refresh, repoKey, state, now)
	if !ok || id != "fallback" {
		t.Fatalf("selectReadySet = (%q,%v), want fallback,true", id, ok)
	}
}

func TestSelectReadySetSkipsCrashBackoffUntilElapsed(t *testing.T) {
	now := time.Date(2026, 6, 14, 12, 0, 0, 0, time.UTC)
	refresh := &tasks.RefreshResult{Rows: []tasks.Row{
		{ID: "crashy", Status: tasks.StatusReady, Priority: 100, RegIndex: 0},
	}}
	repoKey := "test-repo"
	state := &DaemonState{Version: 1, SetCrashBackoffs: map[string]time.Time{
		setScopedKey(repoKey, "crashy"): now.Add(time.Minute),
	}}

	id, until, reason, ok := selectReadySet(refresh, repoKey, state, now)
	if ok || id != "" || !until.Equal(now.Add(time.Minute)) || reason != "set backed off after abnormal drain exit" {
		t.Fatalf("selectReadySet during backoff = (%q,%s,%q,%v)", id, until, reason, ok)
	}

	id, _, _, ok = selectReadySet(refresh, repoKey, state, now.Add(2*time.Minute))
	if !ok || id != "crashy" {
		t.Fatalf("selectReadySet after backoff = (%q,%v), want crashy,true", id, ok)
	}
}

func TestSelectReadySetSkipsParkedSet(t *testing.T) {
	now := time.Date(2026, 6, 14, 12, 0, 0, 0, time.UTC)
	refresh := &tasks.RefreshResult{Rows: []tasks.Row{
		{ID: "parked", Status: tasks.StatusReady, Priority: 100, RegIndex: 0},
	}}
	repoKey := "test-repo"
	state := &DaemonState{Version: 1, ParkedSets: map[string]ParkedSet{
		setScopedKey(repoKey, "parked"): {RuntimePath: "/runtime", SetID: "parked", ParkedAt: now},
	}}

	id, until, reason, ok := selectReadySet(refresh, repoKey, state, now)
	if ok || id != "" || !until.IsZero() || reason != "set parked after repeated abnormal drain exits" {
		t.Fatalf("selectReadySet parked = (%q,%s,%q,%v)", id, until, reason, ok)
	}
}

func queueTestTasksDeps(allFound bool) *tasks.Deps {
	d := tasks.DefaultDeps()
	d.LookPath = func(file string) (string, error) {
		if allFound {
			return "/bin/" + file, nil
		}
		return "", fmt.Errorf("missing %s", file)
	}
	return d
}

// recordingTmux captures tmux invocations so spawn behavior can be asserted.
type recordingTmux struct {
	deps.MockTmux
	commands [][]string
}

func newRecordingTmux(hasSession bool, windowIndices string) *recordingTmux {
	rt := &recordingTmux{}
	rt.HasSessionFunc = func(name string) bool { return hasSession }
	rt.NewSessionFunc = func(name, dir string) error {
		rt.commands = append(rt.commands, []string{"new-session", name, dir})
		return nil
	}
	rt.CommandFunc = func(args ...string) (string, error) {
		rt.commands = append(rt.commands, args)
		if len(args) > 0 && args[0] == "list-windows" {
			return windowIndices, nil
		}
		if len(args) > 0 && args[0] == "new-window" {
			return "0", nil
		}
		if len(args) > 0 && args[0] == "split-window" {
			return "%7", nil
		}
		return "", nil
	}
	return rt
}

func (rt *recordingTmux) findCommand(verb string) ([]string, bool) {
	for _, c := range rt.commands {
		if len(c) > 0 && c[0] == verb {
			return c, true
		}
	}
	return nil, false
}

func actionableDecision() Decision {
	return Decision{
		Project:      "proj",
		TaskSetID:    "2026-06-14-queue",
		DefaultAgent: "codex",
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

func TestPrepareWorktreeDrainOverridesRuntimePath(t *testing.T) {
	now := time.Date(2026, 6, 14, 9, 8, 7, 0, time.UTC)
	d := worktreeProvisionDeps(t, now, nil)
	dec := actionableDecision()
	dec.WorktreeReady = true
	dec.scan.RuntimePath = "/repo"
	dec.scan.ProjectPath = "/repo"

	got := prepareWorktreeDrain(d, &bytes.Buffer{}, dec)

	if got.scan.RuntimePath == "/repo" || got.scan.RuntimePath == "" {
		t.Fatalf("RuntimePath was not overridden: %+v", got.scan)
	}
	if got.scan.ProjectPath != got.scan.RuntimePath {
		t.Fatalf("ProjectPath = %q, RuntimePath = %q; want worktree checkout for both", got.scan.ProjectPath, got.scan.RuntimePath)
	}
	if got.scan.SessionName == dec.scan.SessionName {
		t.Fatalf("SessionName was not recalculated for worktree: %q", got.scan.SessionName)
	}
}

func TestPrepareWorktreeDrainNonReadyStaysInPlace(t *testing.T) {
	d := worktreeProvisionDeps(t, time.Date(2026, 6, 14, 9, 8, 7, 0, time.UTC), fmt.Errorf("must not provision"))
	dec := actionableDecision()
	dec.WorktreeReady = false
	dec.scan.RuntimePath = "/repo"
	dec.scan.ProjectPath = "/repo"

	got := prepareWorktreeDrain(d, &bytes.Buffer{}, dec)

	if got.scan.RuntimePath != "/repo" || got.scan.ProjectPath != "/repo" {
		t.Fatalf("non-worktree-ready project must stay in-place, got %+v", got.scan)
	}
}

func TestPrepareWorktreeDrainProvisionFailureFallsBackInPlace(t *testing.T) {
	d := worktreeProvisionDeps(t, time.Date(2026, 6, 14, 9, 8, 7, 0, time.UTC), fmt.Errorf("boom"))
	dec := actionableDecision()
	dec.WorktreeReady = true
	dec.scan.RuntimePath = "/repo"
	dec.scan.ProjectPath = "/repo"
	var out bytes.Buffer

	got := prepareWorktreeDrain(d, &out, dec)

	if got.scan.RuntimePath != "/repo" || got.scan.ProjectPath != "/repo" {
		t.Fatalf("failed provisioning must fall back in-place, got %+v", got.scan)
	}
	if !strings.Contains(out.String(), "falling back to in-place drain") {
		t.Fatalf("fallback was not reported: %q", out.String())
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
		if reflect.DeepEqual(args, []string{"worktree", "list", "--porcelain"}) {
			return "worktree " + boundPath + "\n", nil
		}
		if reflect.DeepEqual(args, []string{"rev-parse", "--show-toplevel"}) && dir == boundPath {
			return boundPath, nil
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
	state := &DaemonState{
		Version: 1,
		WorktreeBindings: map[string]WorktreeBinding{
			setScopedKey(repoKey, "2026-06-14-queue"): {
				RuntimePath: boundPath,
				Branch:      "pop/2026-06-14-queue/20260614T090807Z",
				Project:     "proj",
			},
		},
	}
	if err := WriteDaemonState(d.Tasks, state); err != nil {
		t.Fatal(err)
	}

	dec := actionableDecision()
	dec.WorktreeReady = true
	dec.scan.RuntimePath = "/repo"
	dec.scan.ProjectPath = "/repo"

	got := prepareWorktreeDrain(d, &bytes.Buffer{}, dec)

	if worktreeAddCalls != 0 {
		t.Fatalf("git worktree add calls = %d, want 0 when binding is valid", worktreeAddCalls)
	}
	if got.scan.RuntimePath != boundPath || got.scan.ProjectPath != boundPath {
		t.Fatalf("expected bound checkout %+v, got %+v", boundPath, got.scan)
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
	state := &DaemonState{
		Version: 1,
		WorktreeBindings: map[string]WorktreeBinding{
			setScopedKey(repoKey, "2026-06-14-queue"): {
				RuntimePath: missingPath,
				Branch:      "pop/2026-06-14-queue/20260614T090807Z",
				Project:     "proj",
			},
		},
	}
	if err := WriteDaemonState(d.Tasks, state); err != nil {
		t.Fatal(err)
	}

	dec := actionableDecision()
	dec.WorktreeReady = true
	dec.scan.RuntimePath = "/repo"
	dec.scan.ProjectPath = "/repo"
	var out bytes.Buffer

	got := prepareWorktreeDrain(d, &out, dec)

	if got.Actionable() {
		t.Fatalf("invalid binding must refuse spawn, got actionable %+v", got)
	}
	if !strings.Contains(out.String(), "pop queue abandon") {
		t.Fatalf("output must mention abandon: %q", out.String())
	}
	if got.scan.RuntimePath != "/repo" {
		t.Fatalf("must not fall back in-place, got runtime %q", got.scan.RuntimePath)
	}
}

func TestSpawnCreatesSessionAndSplitsMainWindow(t *testing.T) {
	rt := newRecordingTmux(false, "0")
	d := &Deps{Tmux: rt}

	if err := Spawn(d, actionableDecision()); err != nil {
		t.Fatalf("Spawn: %v", err)
	}

	if _, ok := rt.findCommand("new-session"); !ok {
		t.Fatal("expected a detached session to be created when absent")
	}
	if _, ok := rt.findCommand("new-window"); ok {
		t.Fatal("must not create a separate queue window")
	}
	assertSplitIntoWindow(t, rt, "proj-session:0", "/checkout")
	assertSendKeys(t, rt)
}

func TestSpawnWorktreeDrainPassesRuntimeOverrideAndUsesWorktreeDir(t *testing.T) {
	rt := newRecordingTmux(false, "0")
	dec := actionableDecision()
	dec.WorktreeReady = true
	dec.scan.ProjectPath = "/pop/worktrees/repo/set"
	dec.scan.RuntimePath = "/pop/worktrees/repo/set"
	d := &Deps{Tmux: rt}

	if err := Spawn(d, dec); err != nil {
		t.Fatalf("Spawn: %v", err)
	}

	splitWindow, ok := rt.findCommand("split-window")
	if !ok {
		t.Fatal("expected a drain pane in the main window")
	}
	if !argsContain(splitWindow, "-c", "/pop/worktrees/repo/set") {
		t.Fatalf("split-window must start in the worktree checkout: %v", splitWindow)
	}
	sendKeys, ok := rt.findCommand("send-keys")
	if !ok {
		t.Fatal("expected drain command")
	}
	joined := strings.Join(sendKeys, " ")
	if !strings.Contains(joined, "--task-runtime-path /pop/worktrees/repo/set") {
		t.Fatalf("send-keys must pass runtime override: %v", sendKeys)
	}
}

func TestSpawnSplitsWhenSessionExists(t *testing.T) {
	rt := newRecordingTmux(true, "0\n1")
	d := &Deps{Tmux: rt}

	if err := Spawn(d, actionableDecision()); err != nil {
		t.Fatalf("Spawn: %v", err)
	}

	if _, ok := rt.findCommand("new-session"); ok {
		t.Fatal("must not create a session that already exists")
	}
	if _, ok := rt.findCommand("new-window"); ok {
		t.Fatal("must not create a new window when the session already exists")
	}
	assertSplitIntoWindow(t, rt, "proj-session:0", "/checkout")
	assertSendKeys(t, rt)
}

func TestSpawnSplitsWhenWindowZeroMissing(t *testing.T) {
	rt := newRecordingTmux(true, "1")
	d := &Deps{Tmux: rt}

	if err := Spawn(d, actionableDecision()); err != nil {
		t.Fatalf("Spawn: %v", err)
	}

	assertSplitIntoWindow(t, rt, "proj-session:1", "/checkout")
	assertSendKeys(t, rt)
}

func TestResolveDrainWindowTargetCreatesWindowWhenSessionEmpty(t *testing.T) {
	rt := newRecordingTmux(true, "")
	target, err := resolveDrainWindowTarget(rt, "pop")
	if err != nil {
		t.Fatalf("resolveDrainWindowTarget: %v", err)
	}
	if target != "pop:0" {
		t.Fatalf("target = %q, want pop:0", target)
	}
	if _, ok := rt.findCommand("new-window"); !ok {
		t.Fatal("expected new-window when session has no windows")
	}
}

func TestSpawnNonActionableNoOp(t *testing.T) {
	rt := newRecordingTmux(false, "")
	d := &Deps{Tmux: rt}

	if err := Spawn(d, Decision{Project: "busy", Busy: true}); err != nil {
		t.Fatalf("Spawn: %v", err)
	}
	if len(rt.commands) != 0 {
		t.Fatalf("non-actionable decision must touch tmux 0 times, got %v", rt.commands)
	}
}

func assertSplitIntoWindow(t *testing.T, rt *recordingTmux, windowTarget, dir string) {
	t.Helper()
	splitWindow, ok := rt.findCommand("split-window")
	if !ok {
		t.Fatal("expected a new pane to be split into the drain window")
	}
	if !argsContain(splitWindow, "-t", windowTarget) {
		t.Fatalf("split-window must target %q: %v", windowTarget, splitWindow)
	}
	if !argsContain(splitWindow, "-c", dir) {
		t.Fatalf("split-window must start in %q: %v", dir, splitWindow)
	}
	layout, ok := rt.findCommand("select-layout")
	if !ok {
		t.Fatal("expected the drain window to be retiled after split")
	}
	if !argsContain(layout, "-t", windowTarget) {
		t.Fatalf("select-layout must target %q: %v", windowTarget, layout)
	}
}

func assertSendKeys(t *testing.T, rt *recordingTmux) {
	t.Helper()
	sendKeys, ok := rt.findCommand("send-keys")
	if !ok {
		t.Fatal("expected the drain command to be sent into the pane")
	}
	joined := strings.Join(sendKeys, " ")
	if strings.Contains(joined, "--yes") {
		t.Fatalf("send-keys must not pass --yes for queue spawns: %v", sendKeys)
	}
	if !strings.Contains(joined, "pop tasks implement 2026-06-14-queue --default-agent codex") {
		t.Fatalf("send-keys must run plain `pop tasks implement <set> --default-agent <agent>`: %v", sendKeys)
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
	wtPath := filepath.Join("/xdg", "pop", "queue", "worktrees", "repo-"+repoHashForTest(t, filepath.Join("/repo", ".git")), "2026-06-14-queue")
	return &Deps{
		Tasks: &tasks.Deps{
			FS: &deps.MockFileSystem{
				GetenvFunc:       func(key string) string { return "/xdg" },
				EvalSymlinksFunc: func(path string) (string, error) { return path, nil },
				MkdirAllFunc:     func(path string, perm os.FileMode) error { return nil },
			},
			Git: &deps.MockGit{CommandInDirFunc: func(dir string, args ...string) (string, error) {
				if reflect.DeepEqual(args, []string{"rev-parse", "--git-common-dir"}) {
					return filepath.Join("/repo", ".git"), nil
				}
				if reflect.DeepEqual(args, []string{"rev-parse", "--show-toplevel"}) && dir == wtPath {
					return wtPath, nil
				}
				return "", addErr
			}},
		},
		Project: &project.Deps{
			FS: &deps.MockFileSystem{
				StatFunc: func(path string) (os.FileInfo, error) {
					return nil, os.ErrNotExist
				},
			},
			Git: &deps.MockGit{CommandInDirFunc: func(dir string, args ...string) (string, error) {
				if reflect.DeepEqual(args, []string{"rev-parse", "--git-common-dir"}) {
					return ".git", nil
				}
				if reflect.DeepEqual(args, []string{"rev-parse", "--show-toplevel"}) {
					return dir, nil
				}
				return "", nil
			}},
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
