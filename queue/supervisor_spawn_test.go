package queue

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/glebglazov/pop/config"
	"github.com/glebglazov/pop/project"
	"github.com/glebglazov/pop/tasks"
)

// TestSupervisorSpawnPlainImplementDrain verifies the supervisor spawns plain
// `pop tasks implement <set>` (no --yes), that AFK work starts without a
// per-drain consent prompt, and that a simulated HITL block still shows the
// interactive gate menu when stdin is a TTY.
func TestSupervisorSpawnPlainImplementDrain(t *testing.T) {
	repo, setID, agent := setupSupervisorSpawnRepo(t, "queue-drain", []spawnTestTask{
		{ID: "01-a", File: "01-a.md", Title: "A", Type: "AFK", Status: "open"},
		{ID: "02-hitl", File: "02-hitl.md", Title: "Review", Type: "HITL", Status: "open"},
	})

	cfg := &config.Config{Projects: []config.ProjectEntry{{Path: repo}}}
	rt := newRecordingTmux(false, "0")
	td := queueTestTasksDeps(true)
	d := &Deps{
		Tasks:      td,
		Project:    project.DefaultDeps(),
		Tmux:       rt,
		LoadConfig: func(string) (*config.Config, error) { return cfg, nil },
	}

	var supervisorOut bytes.Buffer
	tick(d, &supervisorOut, newRunOutputState())

	spawnCmd, ok := extractSpawnCommand(rt)
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
	entries, err := ReadJournal(td)
	if err != nil {
		t.Fatalf("read journal: %v", err)
	}
	spawns := 0
	for _, entry := range entries {
		switch entry.Event {
		case JournalEventSpawn:
			spawns++
		case JournalEventSpawnFailed:
			t.Fatalf("successful spawn must not write spawn_failed: %+v", entry)
		}
	}
	if spawns != 1 {
		t.Fatalf("successful spawn entries = %d, want 1; journal=%+v", spawns, entries)
	}

	var confirmOut bytes.Buffer
	var drainOut bytes.Buffer
	opts := tasks.RunTaskSetOptions{
		ResolveInput:       tasks.ResolveInput{CWD: repo},
		TaskSetOverride:    setID,
		DefaultAgentPreset: "claude",
		AgentCmd:           agent,
		Yes:                false,
		ConfirmIn:          strings.NewReader("4\n"),
		ConfirmOut:         &confirmOut,
		Output:             &drainOut,
	}
	_, err = tasks.RunTaskSetWith(td, project.DefaultDeps(), d.LoadConfig, opts)
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
		"4. Exit",
		"Choose [1]:",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("HITL gate menu missing %q:\n%s", want, out)
		}
	}
}

func TestSupervisorWorktreeDrainTargetsProjectSessionWithCheckoutCWD(t *testing.T) {
	repo, setID, _ := setupSupervisorSpawnRepo(t, "worktree-drain", []spawnTestTask{
		{ID: "01-a", File: "01-a.md", Title: "A", Type: "AFK", Status: "open"},
	})
	if err := os.WriteFile(filepath.Join(repo, ".pop.toml"), []byte("worktree_ready = true\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg := &config.Config{Projects: []config.ProjectEntry{{Path: repo}}}
	rt := newRecordingTmux(false, "0")
	td := queueTestTasksDeps(true)
	d := &Deps{
		Tasks:      td,
		Project:    project.DefaultDeps(),
		Tmux:       rt,
		LoadConfig: func(string) (*config.Config, error) { return cfg, nil },
	}

	var supervisorOut bytes.Buffer
	tick(d, &supervisorOut, newRunOutputState())

	wantSession := project.SessionNameWith(project.DefaultDeps(), repo)
	newSession, ok := rt.findCommand("new-session")
	if !ok {
		t.Fatal("expected originating project session to be created when absent")
	}
	if len(newSession) != 3 || newSession[1] != wantSession {
		t.Fatalf("new-session = %v, want session %q", newSession, wantSession)
	}
	checkout := newSession[2]
	if checkout == repo || !strings.Contains(checkout, filepath.Join("pop", "queue", "worktrees")) {
		t.Fatalf("new-session cwd = %q, want provisioned worktree checkout", checkout)
	}

	assertSplitIntoWindow(t, rt, wantSession+":0", checkout)
	spawnCmd, ok := extractSpawnCommand(rt)
	if !ok {
		t.Fatal("supervisor tick must spawn a drain command")
	}
	if !strings.Contains(spawnCmd, "pop tasks implement "+setID) || !strings.Contains(spawnCmd, "--task-runtime-path "+checkout) {
		t.Fatalf("spawn command = %q, want set and checkout runtime override %q", spawnCmd, checkout)
	}
	worktreeSession := project.SessionNameWith(project.DefaultDeps(), checkout)
	if worktreeSession != wantSession && newSession[1] == worktreeSession {
		t.Fatalf("new-session must not target worktree-derived session %q: %v", worktreeSession, newSession)
	}
	if !strings.Contains(supervisorOut.String(), "spawned drain for "+setID) {
		t.Fatalf("supervisor output missing spawn line:\n%s", supervisorOut.String())
	}
}

func TestSupervisorTickJournalsSpawnFailure(t *testing.T) {
	repo, setID, _ := setupSupervisorSpawnRepo(t, "spawn-fails", []spawnTestTask{
		{ID: "01-a", File: "01-a.md", Title: "A", Type: "AFK", Status: "open"},
	})

	cfg := &config.Config{Projects: []config.ProjectEntry{{Path: repo}}}
	td := queueTestTasksDeps(true)
	rt := newRecordingTmux(false, "0")
	rt.CommandFunc = func(args ...string) (string, error) {
		rt.commands = append(rt.commands, args)
		if len(args) > 0 && args[0] == "list-windows" {
			return "0", nil
		}
		if len(args) > 0 && args[0] == "split-window" {
			return "", errors.New("tmux refused pane")
		}
		return "", nil
	}
	d := &Deps{
		Tasks:      td,
		Project:    project.DefaultDeps(),
		Tmux:       rt,
		LoadConfig: func(string) (*config.Config, error) { return cfg, nil },
	}

	var out bytes.Buffer
	tick(d, &out, newRunOutputState())

	if !strings.Contains(out.String(), "spawn "+setID+": create drain pane: tmux refused pane") {
		t.Fatalf("supervisor output missing spawn failure:\n%s", out.String())
	}
	entries, err := ReadJournal(td)
	if err != nil {
		t.Fatalf("read journal: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("journal entries = %+v, want one spawn_failed entry", entries)
	}
	wantRuntimePath, err := filepath.EvalSymlinks(repo)
	if err != nil {
		t.Fatalf("canonicalize repo: %v", err)
	}
	got := entries[0]
	if got.Event != JournalEventSpawnFailed || got.Project == "" || got.SetID != setID || got.RuntimePath != wantRuntimePath || got.Source != "supervisor" {
		t.Fatalf("spawn_failed entry = %+v", got)
	}
	if got.Reason != "create drain pane: tmux refused pane" {
		t.Fatalf("spawn_failed reason = %q", got.Reason)
	}
}

type spawnTestTask struct {
	ID     string
	File   string
	Title  string
	Type   string
	Status string
}

func setupSupervisorSpawnRepo(t *testing.T, stem string, taskRows []spawnTestTask) (repo, setID, agent string) {
	t.Helper()
	repo = t.TempDir()
	spawnInitGitRepo(t, repo)
	xdg := filepath.Join(repo, ".xdg")
	t.Setenv("XDG_DATA_HOME", xdg)

	id, err := tasks.ResolveRepositoryIdentity(tasks.DefaultDeps(), repo)
	if err != nil {
		t.Fatal(err)
	}
	tasksDir := id.TasksDir
	setDir := filepath.Join(tasksDir, stem)
	for _, task := range taskRows {
		writeSpawnTaskMD(t, setDir, task.File)
	}
	writeSpawnManifest(t, setDir, taskRows)
	if _, err := tasks.RefreshWith(tasks.DefaultDeps(), tasksDir, tasks.DefaultStatePath()); err != nil {
		t.Fatal(err)
	}

	agent = writeSpawnTestAgent(t, repo)
	return repo, stem, agent
}

func spawnInitGitRepo(t *testing.T, root string) {
	t.Helper()
	runGit(t, root, "init")
	runGit(t, root, "config", "user.email", "pop@example.test")
	runGit(t, root, "config", "user.name", "Pop Test")
	writeFile(t, filepath.Join(root, ".gitignore"), "thoughts/\n.agent/\n.xdg/\n")
	writeFile(t, filepath.Join(root, "README.md"), "# test\n")
	runGit(t, root, "add", "-A")
	runGit(t, root, "commit", "-m", "init")
}

func writeSpawnTaskMD(t *testing.T, setDir, file string) {
	t.Helper()
	if err := os.MkdirAll(setDir, 0o755); err != nil {
		t.Fatal(err)
	}
	body := "## Acceptance criteria\n\n- [ ] ok\n"
	if err := os.WriteFile(filepath.Join(setDir, file), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func writeSpawnManifest(t *testing.T, setDir string, taskRows []spawnTestTask) {
	t.Helper()
	payload := map[string]any{"tasks": taskRows}
	data, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(setDir, "index.json"), data, 0o644); err != nil {
		t.Fatal(err)
	}
}

func writeSpawnTestAgent(t *testing.T, root string) string {
	t.Helper()
	path := filepath.Join(root, ".agent", "fake-agent.sh")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	script := "#!/bin/sh\n" +
		"TASK=$(printf '%s' \"$1\" | sed -n 's|^You are implementing the task at: ||p' | head -1)\n" +
		"if [ -n \"$TASK\" ] && [ -f \"$TASK\" ]; then\n" +
		"  sed -i '' 's/- \\[ \\]/- [x]/g' \"$TASK\" 2>/dev/null || sed -i 's/- \\[ \\]/- [x]/g' \"$TASK\"\n" +
		"fi\n" +
		"printf 'SUMMARY_START\\nok\\nSUMMARY_END\\nTASK_COMPLETE\\n'\n"
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}

func extractSpawnCommand(rt *recordingTmux) (string, bool) {
	sendKeys, ok := rt.findCommand("send-keys")
	if !ok {
		return "", false
	}
	for i, arg := range sendKeys {
		if strings.HasPrefix(arg, "pop tasks implement ") {
			return arg, true
		}
		if i > 0 && sendKeys[i-1] == "-t" && strings.HasPrefix(arg, "pop tasks implement ") {
			return arg, true
		}
	}
	joined := strings.Join(sendKeys, " ")
	if idx := strings.Index(joined, "pop tasks implement "); idx >= 0 {
		cmd := joined[idx:]
		if end := strings.Index(cmd, " Enter"); end >= 0 {
			cmd = cmd[:end]
		}
		return cmd, true
	}
	return "", false
}
