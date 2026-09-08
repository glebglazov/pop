package main

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/glebglazov/pop/tasks"
)

func TestPrepareCaseFromHistoricalTaskSet(t *testing.T) {
	repo := t.TempDir()
	runGit(t, repo, "init", "-q")
	runGit(t, repo, "config", "user.name", "Eval Test")
	runGit(t, repo, "config", "user.email", "eval@example.test")
	runGit(t, repo, "remote", "add", "origin", "https://example.test/project.git")
	writeFile(t, filepath.Join(repo, "go.mod"), "module example.test/project\n\ngo 1.23\n")
	writeFile(t, filepath.Join(repo, "AGENTS.md"), "# Standards\n")
	runGit(t, repo, "add", ".")
	runGit(t, repo, "commit", "-qm", "initial")
	parent := runGit(t, repo, "rev-parse", "HEAD")

	setID := "2026-09-08-fixture"
	t.Setenv("XDG_DATA_HOME", filepath.Join(t.TempDir(), "data"))
	d := tasks.DefaultDeps()
	identity, err := tasks.ResolveRepositoryIdentity(d, repo)
	if err != nil {
		t.Fatal(err)
	}
	setDir := filepath.Join(identity.TasksDir, setID)
	if err := os.MkdirAll(setDir, 0o755); err != nil {
		t.Fatal(err)
	}
	manifest := `{
  "verify": false,
  "base_commit": "historical",
  "human_completed": true,
  "tasks": [
    {"id":"01-build","file":"01-build.md","title":"Build it","type":"AFK","status":"done","blocked_by":[],"commit":{"sha":"old","subject":"old"}},
    {"id":"02-check","file":"02-check.md","title":"Check it","type":"AFK","status":"failed","blocked_by":["01-build","03-sign-off"],"failed_after":2},
    {"id":"03-sign-off","file":"03-sign-off.md","title":"Sign off","type":"HITL","status":"open","blocked_by":["02-check"]}
  ]
}`
	writeFile(t, filepath.Join(setDir, tasks.ManifestFileName), manifest)
	writeFile(t, filepath.Join(setDir, "01-build.md"), taskBody("Build it", "First behaviour"))
	writeFile(t, filepath.Join(setDir, "02-check.md"), taskBody("Check it", "Second behaviour"))
	writeFile(t, filepath.Join(setDir, "03-sign-off.md"), taskBody("Sign off", "Human approval"))

	writeFile(t, filepath.Join(repo, "one.go"), "package project\n")
	runGit(t, repo, "add", ".")
	runGit(t, repo, "commit", "-qm", "build\n\nPop-Task: "+setID+"/01-build")
	writeFile(t, filepath.Join(repo, "two.go"), "package project\n")
	runGit(t, repo, "add", ".")
	runGit(t, repo, "commit", "-qm", "check\n\nPop-Task: "+setID+"/02-check")
	writeFile(t, filepath.Join(repo, "refined.go"), "package project\n")
	runGit(t, repo, "add", ".")
	runGit(t, repo, "commit", "-qm", "refine\n\nPop-Refine: "+setID)
	last := runGit(t, repo, "rev-parse", "HEAD")

	outputRoot := filepath.Join(t.TempDir(), "cases")
	caseDir, err := prepareCase(prepareOptions{repositoryPath: repo, setID: setID, outputRoot: outputRoot})
	if err != nil {
		t.Fatal(err)
	}
	if caseDir != filepath.Join(outputRoot, setID) {
		t.Fatalf("Case path = %q", caseDir)
	}

	var gotCase caseManifest
	decodeJSONFile(t, filepath.Join(caseDir, caseManifestName), &gotCase)
	if gotCase.RepositoryURL != "https://example.test/project.git" || gotCase.ParentCommit != parent {
		t.Fatalf("Case manifest = %#v", gotCase)
	}
	if gotCase.ReferenceRange != parent+".."+last {
		t.Fatalf("reference range = %q", gotCase.ReferenceRange)
	}
	if strings.Join(gotCase.GateCommands, "|") != "go build ./...|go test ./..." {
		t.Fatalf("gate commands = %#v", gotCase.GateCommands)
	}
	if strings.Join(gotCase.StandardDocuments, "|") != "AGENTS.md" {
		t.Fatalf("standard documents = %#v", gotCase.StandardDocuments)
	}

	var gotTasks struct {
		Verify *bool        `json:"verify"`
		Tasks  []tasks.Task `json:"tasks"`
	}
	decodeJSONFile(t, filepath.Join(caseDir, "tasks", tasks.ManifestFileName), &gotTasks)
	if gotTasks.Verify == nil || *gotTasks.Verify || len(gotTasks.Tasks) != 2 {
		t.Fatalf("stripped manifest = %#v", gotTasks)
	}
	for _, task := range gotTasks.Tasks {
		if task.Type == "HITL" || task.Status != tasks.TaskOpen || task.Commit != nil || task.FailedAfter != nil {
			t.Fatalf("task was not reset: %#v", task)
		}
		for _, blocker := range task.BlockedBy {
			if blocker == "03-sign-off" {
				t.Fatalf("stripped blocker remains: %#v", task)
			}
		}
	}
	preparedSet := tasks.LoadManifest(tasks.DefaultDeps(), setID, filepath.Join(caseDir, "tasks", tasks.ManifestFileName))
	if !preparedSet.Valid {
		t.Fatalf("prepared Task set is not valid: %v", preparedSet.Errors)
	}
	if _, err := os.Stat(filepath.Join(caseDir, "tasks", "03-sign-off.md")); !os.IsNotExist(err) {
		t.Fatalf("HITL task file exists: %v", err)
	}
	spec := readFile(t, filepath.Join(caseDir, tasks.SpecFileName))
	if !strings.Contains(spec, "First behaviour") || !strings.Contains(spec, "Second behaviour") || strings.Contains(spec, "Human approval") {
		t.Fatalf("spec content:\n%s", spec)
	}
	acceptance := readFile(t, filepath.Join(caseDir, acceptanceName))
	if !strings.Contains(acceptance, "Status: not approved") || !strings.Contains(acceptance, "- [ ] First behaviour") || !strings.Contains(acceptance, "- [ ] Second behaviour") || strings.Contains(acceptance, "Human approval") {
		t.Fatalf("acceptance content:\n%s", acceptance)
	}

	if err := filepath.Walk(caseDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}
		content := readFile(t, path)
		if strings.Contains(content, repo) || strings.Contains(content, setDir) {
			t.Fatalf("%s contains a preparing-machine path", path)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

func taskBody(title, criterion string) string {
	return "## What to build\n\n" + title + "\n\n## Acceptance criteria\n\n- [x] " + criterion + "\n\n## Blocked by\n\n- None\n"
}

func runGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s: %v: %s", strings.Join(args, " "), err, output)
	}
	return strings.TrimSpace(string(output))
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func decodeJSONFile(t *testing.T, path string, value any) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(data, value); err != nil {
		t.Fatal(err)
	}
}
