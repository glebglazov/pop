package queue

import (
	"strings"
	"testing"

	"github.com/glebglazov/pop/internal/deps"
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
			got, ok := selectReadySet(tt.rows)
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
		ReadLock: func(runtimePath string) *tasks.RuntimeLockStatus {
			return liveLock(runtimePath)
		},
		Refresh: func(defPath string) (*tasks.RefreshResult, error) {
			refreshCalled = true
			return &tasks.RefreshResult{}, nil
		},
	}

	dec := decideProject(d, projectScan{Name: "proj", RuntimePath: "/co", DefinitionPath: "/def"})

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

	dec := decideProject(d, projectScan{Name: "proj", RuntimePath: "/co", DefinitionPath: "/def"})

	if dec.Busy || dec.Err != nil {
		t.Fatalf("idle project with ready work should be actionable, got %+v", dec)
	}
	if dec.TaskSetID != "top" {
		t.Fatalf("expected highest-priority ready set 'top', got %q", dec.TaskSetID)
	}
	if !dec.Actionable() {
		t.Fatalf("expected actionable decision, got %+v", dec)
	}
}

func TestDecideProjectNoReadySet(t *testing.T) {
	d := &Deps{
		ReadLock: func(runtimePath string) *tasks.RuntimeLockStatus { return idleLock(runtimePath) },
		Refresh: func(defPath string) (*tasks.RefreshResult, error) {
			return &tasks.RefreshResult{Rows: []tasks.Row{
				{ID: "done", Status: tasks.StatusDone, Priority: 5},
				{ID: "blocked", Status: tasks.StatusBlocked, Priority: 5},
			}}, nil
		},
	}

	dec := decideProject(d, projectScan{Name: "proj", RuntimePath: "/co", DefinitionPath: "/def"})

	if dec.Actionable() {
		t.Fatalf("a project with no ready set must not be actionable: %+v", dec)
	}
	if dec.Reason != "no ready set" {
		t.Fatalf("expected reason 'no ready set', got %q", dec.Reason)
	}
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
		Project:   "proj",
		TaskSetID: "2026-06-14-queue",
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
	if !strings.Contains(joined, "pop tasks implement 2026-06-14-queue --yes") {
		t.Fatalf("send-keys must run `pop tasks implement <set> --yes`: %v", sendKeys)
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
