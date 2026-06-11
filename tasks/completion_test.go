package tasks

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/glebglazov/pop/config"
	"github.com/glebglazov/pop/project"
)

func TestCompleteTaskSetIDsFromDiscovery(t *testing.T) {
	root := t.TempDir()
	tasksDir := setupCompletionRepo(t, root)
	writeCompletionTaskSet(t, tasksDir, "alpha")
	writeCompletionTaskSet(t, tasksDir, "beta")

	oldWd, _ := os.Getwd()
	if err := os.Chdir(root); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(oldWd) })

	stems, err := CompleteTaskSetIDs(CompletionInput{}, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(stems) != 2 || stems[0] != "alpha" || stems[1] != "beta" {
		t.Fatalf("stems = %#v", stems)
	}
}

func TestCompleteTaskTargetsOffersIdentifiersAndSetRelativeFiles(t *testing.T) {
	root := t.TempDir()
	tasksDir := setupCompletionRepo(t, root)
	writeCompletionFixture(t, tasksDir, "feature", []Task{
		{ID: "01-a", File: "01-a.md", Title: "A", Type: "AFK", Status: "open"},
		{ID: "02-b", File: "02-b.md", Title: "B", Type: "AFK", Status: "done"},
	})

	oldWd, _ := os.Getwd()
	if err := os.Chdir(root); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(oldWd) })

	ids, err := CompleteTaskTargets(CompletionInput{}, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(ids) != 1 || ids[0] != "feature/" {
		t.Fatalf("identifiers = %#v", ids)
	}

	files, err := CompleteTaskTargets(CompletionInput{}, "feature/")
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 2 || files[0] != "feature/01-a.md" || files[1] != "feature/02-b.md" {
		t.Fatalf("set-relative files = %#v", files)
	}
}

func TestCompleteActionableTaskTargetsOmitsDoneSetsAndTasks(t *testing.T) {
	root := t.TempDir()
	tasksDir := setupCompletionRepo(t, root)
	writeCompletionFixture(t, tasksDir, "archived", []Task{
		{ID: "01-a", File: "01-a.md", Title: "A", Type: "AFK", Status: "done"},
	})
	writeCompletionFixture(t, tasksDir, "feature", []Task{
		{ID: "01-a", File: "01-a.md", Title: "A", Type: "AFK", Status: "open"},
		{ID: "02-b", File: "02-b.md", Title: "B", Type: "AFK", Status: "done"},
	})

	oldWd, _ := os.Getwd()
	if err := os.Chdir(root); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(oldWd) })

	ids, err := CompleteActionableTaskTargets(CompletionInput{}, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(ids) != 1 || ids[0] != "feature/" {
		t.Fatalf("identifiers = %#v", ids)
	}

	files, err := CompleteActionableTaskTargets(CompletionInput{}, "feature/")
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 1 || files[0] != "feature/01-a.md" {
		t.Fatalf("set-relative files = %#v", files)
	}

	// The unfiltered variant (timings) still offers the Done set and done task.
	all, err := CompleteTaskTargets(CompletionInput{}, "")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(all, ",") != "archived/,feature/" {
		t.Fatalf("unfiltered identifiers = %#v", all)
	}
}

func TestCompleteProjectNamesUsesPickerVisibleNames(t *testing.T) {
	root := t.TempDir()
	projectDir := filepath.Join(root, "svc")
	if err := os.MkdirAll(projectDir, 0o755); err != nil {
		t.Fatal(err)
	}
	writeCompletionTaskSet(t, projectDir, "svc")

	cfgPath := filepath.Join(root, "config.toml")
	if err := os.WriteFile(cfgPath, []byte("projects = [{ path = \""+projectDir+"\" }]\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	names, err := CompleteProjectNamesWith(DefaultDeps(), project.DefaultDeps(), func(string) (*config.Config, error) {
		return config.Load(cfgPath)
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(names) != 1 || names[0] != "svc" {
		t.Fatalf("names = %#v", names)
	}
}

func TestCompletionDoesNotPersistTaskState(t *testing.T) {
	root := t.TempDir()
	initGitRepo(t, root)
	t.Setenv("XDG_DATA_HOME", root)
	tasksDir := storageTasksDir(t, root)
	writeCompletionTaskSet(t, tasksDir, "existing")
	writeCompletionTaskSet(t, tasksDir, "new-prd")

	statePath := filepath.Join(root, "state.json")
	canon, err := CanonicalDefinitionPath(tasksDir)
	if err != nil {
		t.Fatal(err)
	}

	d := DefaultDeps()
	seed := &GlobalState{
		Version: StateVersion,
		Tasks: map[string]*TaskEntry{
			canon: {TaskSets: []RegisteredTaskSet{{ID: "existing", Priority: 0}}},
		},
		path: statePath,
	}
	if err := seed.SaveWith(d); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatal(err)
	}

	oldWd, _ := os.Getwd()
	if err := os.Chdir(root); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(oldWd) })

	var notices bytes.Buffer
	d.NoticeOut = &notices

	stems, err := CompleteTaskSetIDsWith(d, project.DefaultDeps(), config.Load, CompletionInput{}, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(stems) != 2 {
		t.Fatalf("stems = %#v", stems)
	}

	after, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != string(before) {
		t.Fatalf("state mutated:\nbefore=%q\nafter=%q", before, after)
	}
	if _, err := os.Stat(filepath.Join(root, "pop", "workloads-state.json")); !os.IsNotExist(err) {
		t.Fatal("expected no default state file write")
	}
	if notices.Len() != 0 {
		t.Fatalf("unexpected notices: %q", notices.String())
	}
}

func TestCompletionUnreadableDiscoveryReturnsEmptyWithoutError(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("chmod tests unreliable as root")
	}
	root := t.TempDir()
	tasksDir := setupCompletionRepo(t, root)
	writeCompletionTaskSet(t, tasksDir, "a")
	if err := os.Chmod(tasksDir, 0o000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(tasksDir, 0o755) })

	oldWd, _ := os.Getwd()
	if err := os.Chdir(root); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(oldWd) })

	stems, err := CompleteTaskSetIDs(CompletionInput{}, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(stems) != 0 {
		t.Fatalf("stems = %#v", stems)
	}
}

// setupCompletionRepo initializes a git repo at root, points XDG at it, and
// returns the repository's Task storage tasks directory.
func setupCompletionRepo(t *testing.T, root string) string {
	t.Helper()
	initGitRepo(t, root)
	t.Setenv("XDG_DATA_HOME", filepath.Join(root, ".xdg"))
	return storageTasksDir(t, root)
}

// writeCompletionTaskSet creates a minimal valid Task set (no PRD pairing required).
func writeCompletionTaskSet(t *testing.T, tasksDir, stem string) {
	t.Helper()
	writeCompletionFixture(t, tasksDir, stem, []Task{
		{ID: "01-a", File: "01-a.md", Title: "A", Type: "AFK", Status: "open"},
	})
}

func writeCompletionFixture(t *testing.T, tasksDir, stem string, tasks []Task) {
	t.Helper()
	taskDir := filepath.Join(tasksDir, stem)
	for _, task := range tasks {
		path := filepath.Join(taskDir, task.File)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte("## Acceptance criteria\n\n- [ ] ok\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	writeManifest(t, taskDir, tasks)
}

func TestCompleteTaskSetIDsUsesDefinitionOverride(t *testing.T) {
	root := t.TempDir()
	defDir := filepath.Join(root, "planning")
	writeCompletionTaskSet(t, defDir, "planned")

	stems, err := CompleteTaskSetIDs(CompletionInput{
		Path:               root,
		DefinitionOverride: defDir,
	}, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(stems) != 1 || stems[0] != "planned" {
		t.Fatalf("stems = %#v", stems)
	}
}

func TestCompleteTaskTargetsScopedToSelectedTaskSet(t *testing.T) {
	root := t.TempDir()
	tasksDir := setupCompletionRepo(t, root)
	writeCompletionFixture(t, tasksDir, "one", []Task{
		{ID: "01-a", File: "01-a.md", Title: "A", Type: "AFK", Status: "open"},
	})
	writeCompletionFixture(t, tasksDir, "two", []Task{
		{ID: "99-z", File: "99-z.md", Title: "Z", Type: "AFK", Status: "open"},
	})

	oldWd, _ := os.Getwd()
	if err := os.Chdir(root); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(oldWd) })

	files, err := CompleteTaskTargets(CompletionInput{}, "two/")
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 1 || files[0] != "two/99-z.md" {
		t.Fatalf("files = %#v", files)
	}
}

func TestCompleteProjectNamesMissingConfigIsEmpty(t *testing.T) {
	names, err := CompleteProjectNamesWith(DefaultDeps(), project.DefaultDeps(), func(string) (*config.Config, error) {
		return nil, os.ErrNotExist
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(names) != 0 {
		t.Fatalf("names = %#v", names)
	}
}

func TestCompletionNeverWritesProgress(t *testing.T) {
	root := t.TempDir()
	tasksDir := setupCompletionRepo(t, root)
	writeCompletionFixture(t, tasksDir, "demo", []Task{
		{ID: "01-a", File: "01-a.md", Title: "A", Type: "AFK", Status: "open"},
	})

	oldWd, _ := os.Getwd()
	if err := os.Chdir(root); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(oldWd) })

	_, _ = CompleteTaskSetIDs(CompletionInput{}, "")
	_, _ = CompleteTaskTargets(CompletionInput{}, "demo/")

	progressPath := filepath.Join(tasksDir, "demo", "progress.txt")
	if _, err := os.Stat(progressPath); !os.IsNotExist(err) {
		t.Fatal("completion should not create progress.txt")
	}
}

func TestCompleteTaskSetIDsDoesNotRegisterInStateFile(t *testing.T) {
	root := t.TempDir()
	tasksDir := setupCompletionRepo(t, root)
	writeCompletionTaskSet(t, tasksDir, "fresh")

	oldWd, _ := os.Getwd()
	if err := os.Chdir(root); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(oldWd) })

	if _, err := CompleteTaskSetIDs(CompletionInput{}, ""); err != nil {
		t.Fatal(err)
	}
	statePath := DefaultStatePath()
	if _, err := os.Stat(statePath); !os.IsNotExist(err) {
		t.Fatalf("expected no state file at %s", statePath)
	}
}

func TestCompleteTaskSetIDsSorted(t *testing.T) {
	root := t.TempDir()
	tasksDir := setupCompletionRepo(t, root)
	for _, stem := range []string{"charlie", "alpha", "bravo"} {
		writeCompletionTaskSet(t, tasksDir, stem)
	}

	oldWd, _ := os.Getwd()
	if err := os.Chdir(root); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(oldWd) })

	stems, err := CompleteTaskSetIDs(CompletionInput{}, "")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(stems, ",") != "alpha,bravo,charlie" {
		t.Fatalf("stems = %#v", stems)
	}
}
