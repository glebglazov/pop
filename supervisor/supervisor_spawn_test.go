package supervisor

import (
	"bytes"
	"errors"
	"github.com/glebglazov/pop/internal/queuetest"
	"github.com/glebglazov/pop/tasks/drain"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/glebglazov/pop/config"
	"github.com/glebglazov/pop/project"
	"github.com/glebglazov/pop/tasks"
)

// TestSupervisorSpawnPlainImplementDrain verifies the supervisor spawns plain
// `pop tasks implement <set>` (no --yes), that AFK work starts without a
// per-drain consent prompt, and that a simulated HITL block still shows the
// interactive gate menu when stdin is a TTY.
func TestSupervisorSpawnPlainImplementDrain(t *testing.T) {
	repo, setID, agent := queuetest.SetupSpawnRepo(t, "queue-drain", []queuetest.SpawnTask{
		{ID: "01-a", File: "01-a.md", Title: "A", Type: "AFK", Status: "open"},
		{ID: "02-hitl", File: "02-hitl.md", Title: "Review", Type: "HITL", Status: "open"},
	})

	cfg := &config.Config{Projects: []config.ProjectEntry{{Path: repo}}}
	rt := queuetest.NewRecordingTmux(false, "0")
	td := queuetest.TasksDeps(t, true)
	d := &drain.Deps{
		Tasks:      td,
		Project:    project.DefaultDeps(),
		Tmux:       rt,
		LoadConfig: func(string) (*config.Config, error) { return cfg, nil },
	}
	bindSetInPlace(t, d, repo, setID)

	var supervisorOut bytes.Buffer
	tick(d, &supervisorOut, newRunOutputState())

	spawnCmd, ok := queuetest.ExtractSpawnCommand(rt)
	if !ok {
		t.Fatal("supervisor tick must spawn a drain command")
	}
	if strings.Contains(spawnCmd, "--yes") {
		t.Fatalf("spawn command must not include --yes: %q", spawnCmd)
	}
	if !strings.Contains(spawnCmd, "pop tasks implement "+setID) {
		t.Fatalf("spawn command = %q, want plain implement for %q", spawnCmd, setID)
	}
	if !strings.Contains(supervisorOut.String(), "spawned drain for "+setID) {
		t.Fatalf("supervisor output missing spawn line:\n%s", supervisorOut.String())
	}

	var confirmOut bytes.Buffer
	var drainOut bytes.Buffer
	opts := tasks.RunTaskSetOptions{
		ResolveInput:    tasks.ResolveInput{CWD: repo},
		TaskSetOverride: setID,
		AgentCmd:        agent,
		Yes:             false,
		ConfirmIn:       strings.NewReader("0\n"),
		ConfirmOut:      &confirmOut,
		Output:          &drainOut,
	}
	_, err := tasks.RunTaskSetWith(td, project.DefaultDeps(), d.LoadConfig, opts)
	if err == nil {
		t.Fatal("expected exit after choosing Exit at the HITL gate")
	}
	var exitErr *tasks.ExitError
	if !errors.As(err, &exitErr) || exitErr.Code != tasks.ExitNoRunnable {
		t.Fatalf("HITL exit = %v, want ExitNoRunnable", err)
	}

	out := drainOut.String()
	if strings.Contains(confirmOut.String(), "Run AFK tasks in this Task set?") {
		t.Fatalf("queue spawn must not ask for AFK consent:\n%s", confirmOut.String())
	}
	if !strings.Contains(out, "✓ Completed queue-drain/01-a") {
		t.Fatalf("AFK task should run without a start prompt:\n%s", out)
	}
	for _, want := range []string{
		"1. Get agent assistance (default)",
		"2. Complete task",
		"3. Defer task",
		"0. Exit",
		"Choose [1]:",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("HITL gate menu missing %q:\n%s", want, out)
		}
	}
}

func TestSupervisorSkipsSpawnWithActiveRecoveryWaiter(t *testing.T) {
	repo, setID, _ := queuetest.SetupSpawnRepo(t, "quota-wait", []queuetest.SpawnTask{
		{ID: "01-a", File: "01-a.md", Title: "A", Type: "AFK", Status: "open"},
	})

	cfg := &config.Config{Projects: []config.ProjectEntry{{Path: repo}}}
	rt := queuetest.NewRecordingTmux(false, "0")
	td := queuetest.TasksDeps(t, true)
	d := &drain.Deps{
		Tasks:      td,
		Project:    project.DefaultDeps(),
		Tmux:       rt,
		LoadConfig: func(string) (*config.Config, error) { return cfg, nil },
	}
	bindSetInPlace(t, d, repo, setID)

	resetAt := time.Now().UTC().Add(time.Hour).Truncate(time.Second)
	if _, err := tasks.RegisterRecoveryWaiter(td, tasks.RecoveryWaiter{
		SetID:       setID,
		Preset:      "codex",
		ResetAt:     resetAt,
		RuntimePath: repo,
	}); err != nil {
		t.Fatalf("RegisterRecoveryWaiter: %v", err)
	}

	var supervisorOut bytes.Buffer
	tick(d, &supervisorOut, newRunOutputState())

	if _, ok := queuetest.ExtractSpawnCommand(rt); ok {
		t.Fatalf("supervisor must not spawn while recovery waiter is active, got commands: %v", rt.Commands)
	}
	out := supervisorOut.String()
	if !strings.Contains(out, "waiting for quota recovery") {
		t.Fatalf("supervisor output missing recovery wait status:\n%s", out)
	}
}

func TestSupervisorWorktreeDrainTargetsProjectSessionWithCheckoutCWD(t *testing.T) {
	repo, setID, _ := queuetest.SetupSpawnRepo(t, "worktree-drain", []queuetest.SpawnTask{
		{ID: "01-a", File: "01-a.md", Title: "A", Type: "AFK", Status: "open"},
	})

	cfg := &config.Config{Projects: []config.ProjectEntry{{Path: repo}}}
	rt := queuetest.NewRecordingTmux(false, "0")
	td := queuetest.TasksDeps(t, true)
	d := &drain.Deps{
		Tasks:      td,
		Project:    project.DefaultDeps(),
		Tmux:       rt,
		LoadConfig: func(string) (*config.Config, error) { return cfg, nil },
	}
	bindSetInPlace(t, d, repo, setID)

	var supervisorOut bytes.Buffer
	tick(d, &supervisorOut, newRunOutputState())

	wantSession := project.SessionNameWith(project.DefaultDeps(), repo)
	newSession, ok := rt.FindCommand("new-session")
	if !ok {
		t.Fatal("expected originating project session to be created when absent")
	}
	if len(newSession) != 3 || newSession[1] != wantSession {
		t.Fatalf("new-session = %v, want session %q", newSession, wantSession)
	}
	checkout := newSession[2]
	wantCheckout, _ := filepath.EvalSymlinks(repo)
	gotCheckout, _ := filepath.EvalSymlinks(checkout)
	if gotCheckout != wantCheckout || strings.Contains(checkout, filepath.Join("pop", "queue", "worktrees")) {
		t.Fatalf("new-session cwd = %q, want current checkout %q with no provisioned worktree (ADR-0052)", checkout, repo)
	}

	queuetest.AssertReusesFreshPane(t, rt, "%3")
	newWindow, ok := rt.FindCommand("new-window")
	if !ok {
		t.Fatal("expected a queue window to host the drain pane")
	}
	if !queuetest.ArgsContain(newWindow, "-c", checkout) {
		t.Fatalf("new-window must start in worktree checkout %q: %v", checkout, newWindow)
	}
	spawnCmd, ok := queuetest.ExtractSpawnCommand(rt)
	if !ok {
		t.Fatal("supervisor tick must spawn a drain command")
	}
	if !strings.Contains(spawnCmd, "pop tasks implement "+setID) {
		t.Fatalf("spawn command = %q, want implement command for set %q", spawnCmd, setID)
	}
	worktreeSession := project.SessionNameWith(project.DefaultDeps(), checkout)
	if worktreeSession != wantSession && newSession[1] == worktreeSession {
		t.Fatalf("new-session must not target worktree-derived session %q: %v", worktreeSession, newSession)
	}
	if !strings.Contains(supervisorOut.String(), "spawned drain for "+setID) {
		t.Fatalf("supervisor output missing spawn line:\n%s", supervisorOut.String())
	}
}

func TestSupervisorTickReportsSpawnFailure(t *testing.T) {
	repo, setID, _ := queuetest.SetupSpawnRepo(t, "spawn-fails", []queuetest.SpawnTask{
		{ID: "01-a", File: "01-a.md", Title: "A", Type: "AFK", Status: "open"},
	})

	cfg := &config.Config{Projects: []config.ProjectEntry{{Path: repo}}}
	td := queuetest.TasksDeps(t, true)
	// The drain window already exists (session present) with an untagged pane, so
	// the spawn takes the split path — and the split is made to fail.
	rt := queuetest.NewRecordingTmux(true, drain.DrainWindowName)
	rt.PaneList = "%1"
	rt.SplitErr = errors.New("tmux refused pane")
	d := &drain.Deps{
		Tasks:      td,
		Project:    project.DefaultDeps(),
		Tmux:       rt,
		LoadConfig: func(string) (*config.Config, error) { return cfg, nil },
	}
	bindSetInPlace(t, d, repo, setID)

	var out bytes.Buffer
	tick(d, &out, newRunOutputState())

	if !strings.Contains(out.String(), "spawn "+setID+":") || !strings.Contains(out.String(), "tmux refused pane") {
		t.Fatalf("supervisor output missing spawn failure:\n%s", out.String())
	}
}
