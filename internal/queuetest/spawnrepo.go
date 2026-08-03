package queuetest

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/glebglazov/pop/tasks"
)

type SpawnTask struct {
	ID     string
	File   string
	Title  string
	Type   string
	Status string
}

func SetupSpawnRepo(t *testing.T, stem string, taskRows []SpawnTask) (repo, setID, agent string) {
	t.Helper()
	repo = t.TempDir()
	InitGitRepo(t, repo)
	xdg := filepath.Join(repo, ".xdg")
	t.Setenv("XDG_DATA_HOME", xdg)

	id, err := tasks.ResolveRepositoryIdentity(tasks.DefaultDeps(), repo)
	if err != nil {
		t.Fatal(err)
	}
	// A real repo with task sets always carries a repo.json storage marker
	// (EnsureStorage writes it on first task touch). This fixture writes task
	// files directly, so write the marker too — drain.Scan's storage-scoped partition
	// (ADR-0060) only takes the decision path for repos that have one.
	if err := tasks.EnsureStorage(tasks.DefaultDeps(), id); err != nil {
		t.Fatal(err)
	}
	tasksDir := id.TasksDir
	setDir := filepath.Join(tasksDir, stem)
	for _, task := range taskRows {
		WriteSpawnTaskMD(t, setDir, task.File)
	}
	WriteSpawnManifest(t, setDir, taskRows)
	statePath := tasks.StatePathFor(tasksDir)
	if _, err := tasks.RegisterWith(tasks.DefaultDeps(), tasksDir, statePath); err != nil {
		t.Fatal(err)
	}
	if _, err := tasks.ToggleAutoDrainWith(tasks.DefaultDeps(), tasksDir, statePath, stem); err != nil {
		t.Fatal(err)
	}

	agent = WriteSpawnTestAgent(t, repo)
	return repo, stem, agent
}

func InitGitRepo(t *testing.T, root string) {
	t.Helper()
	runGit(t, root, "init")
	runGit(t, root, "config", "user.email", "pop@example.test")
	runGit(t, root, "config", "user.name", "Pop Test")
	writeFile(t, filepath.Join(root, ".gitignore"), "thoughts/\n.agent/\n.xdg/\n")
	writeFile(t, filepath.Join(root, "README.md"), "# test\n")
	runGit(t, root, "add", "-A")
	runGit(t, root, "commit", "-m", "init")
}

func WriteSpawnTaskMD(t *testing.T, setDir, file string) {
	t.Helper()
	if err := os.MkdirAll(setDir, 0o755); err != nil {
		t.Fatal(err)
	}
	body := "## Acceptance criteria\n\n- [ ] ok\n"
	if err := os.WriteFile(filepath.Join(setDir, file), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func WriteSpawnManifest(t *testing.T, setDir string, taskRows []SpawnTask) {
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

func WriteSpawnTestAgent(t *testing.T, root string) string {
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
