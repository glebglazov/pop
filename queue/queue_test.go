package queue

import (
	"fmt"
	"os"
	"path/filepath"
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
	state := &DaemonState{Version: 1, SetBackoffs: map[string]time.Time{
		setBackoffKey("/runtime", "pinned"): now.Add(time.Hour),
	}}

	id, _, _, ok := selectReadySet(refresh, "/runtime", state, now)
	if !ok || id != "fallback" {
		t.Fatalf("selectReadySet = (%q,%v), want fallback,true", id, ok)
	}
}

func TestSelectReadySetSkipsCrashBackoffUntilElapsed(t *testing.T) {
	now := time.Date(2026, 6, 14, 12, 0, 0, 0, time.UTC)
	refresh := &tasks.RefreshResult{Rows: []tasks.Row{
		{ID: "crashy", Status: tasks.StatusReady, Priority: 100, RegIndex: 0},
	}}
	state := &DaemonState{Version: 1, SetCrashBackoffs: map[string]time.Time{
		setBackoffKey("/runtime", "crashy"): now.Add(time.Minute),
	}}

	id, until, reason, ok := selectReadySet(refresh, "/runtime", state, now)
	if ok || id != "" || !until.Equal(now.Add(time.Minute)) || reason != "set backed off after abnormal drain exit" {
		t.Fatalf("selectReadySet during backoff = (%q,%s,%q,%v)", id, until, reason, ok)
	}

	id, _, _, ok = selectReadySet(refresh, "/runtime", state, now.Add(2*time.Minute))
	if !ok || id != "crashy" {
		t.Fatalf("selectReadySet after backoff = (%q,%v), want crashy,true", id, ok)
	}
}

func TestSelectReadySetSkipsParkedSet(t *testing.T) {
	now := time.Date(2026, 6, 14, 12, 0, 0, 0, time.UTC)
	refresh := &tasks.RefreshResult{Rows: []tasks.Row{
		{ID: "parked", Status: tasks.StatusReady, Priority: 100, RegIndex: 0},
	}}
	state := &DaemonState{Version: 1, ParkedSets: map[string]ParkedSet{
		setBackoffKey("/runtime", "parked"): {RuntimePath: "/runtime", SetID: "parked", ParkedAt: now},
	}}

	id, until, reason, ok := selectReadySet(refresh, "/runtime", state, now)
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

func newRecordingTmux(hasSession bool, windows string) *recordingTmux {
	rt := &recordingTmux{}
	rt.HasSessionFunc = func(name string) bool { return hasSession }
	rt.NewSessionFunc = func(name, dir string) error {
		rt.commands = append(rt.commands, []string{"new-session", name, dir})
		return nil
	}
	rt.CommandFunc = func(args ...string) (string, error) {
		rt.commands = append(rt.commands, args)
		if len(args) > 0 && args[0] == "list-windows" {
			return windows, nil
		}
		if len(args) > 0 && (args[0] == "new-window" || args[0] == "split-window") {
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

func TestSpawnCreatesSessionAndWindow(t *testing.T) {
	rt := newRecordingTmux(false, "")
	d := &Deps{Tmux: rt}

	if err := Spawn(d, actionableDecision()); err != nil {
		t.Fatalf("Spawn: %v", err)
	}

	if _, ok := rt.findCommand("new-session"); !ok {
		t.Fatal("expected a detached session to be created when absent")
	}
	newWindow, ok := rt.findCommand("new-window")
	if !ok {
		t.Fatal("expected the pop-queue window to be created when absent")
	}
	if !argsContain(newWindow, "-n", queueWindow) {
		t.Fatalf("new-window must target the %q window: %v", queueWindow, newWindow)
	}
	assertSendKeys(t, rt)
}

func TestSpawnSplitsWhenWindowExists(t *testing.T) {
	rt := newRecordingTmux(true, "main\npop-queue")
	d := &Deps{Tmux: rt}

	if err := Spawn(d, actionableDecision()); err != nil {
		t.Fatalf("Spawn: %v", err)
	}

	if _, ok := rt.findCommand("new-session"); ok {
		t.Fatal("must not create a session that already exists")
	}
	if _, ok := rt.findCommand("new-window"); ok {
		t.Fatal("must not recreate an existing pop-queue window")
	}
	if _, ok := rt.findCommand("split-window"); !ok {
		t.Fatal("expected a new pane to be split into the existing pop-queue window")
	}
	assertSendKeys(t, rt)
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

func assertSendKeys(t *testing.T, rt *recordingTmux) {
	t.Helper()
	sendKeys, ok := rt.findCommand("send-keys")
	if !ok {
		t.Fatal("expected the drain command to be sent into the pane")
	}
	joined := strings.Join(sendKeys, " ")
	if !strings.Contains(joined, "pop tasks implement 2026-06-14-queue --yes --default-agent codex") {
		t.Fatalf("send-keys must run `pop tasks implement <set> --yes --default-agent <agent>`: %v", sendKeys)
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
