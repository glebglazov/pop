package tasks

import (
	"bytes"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/glebglazov/pop/config"
	"github.com/glebglazov/pop/project"
)

// registeredTaskSetsFor returns the registrations stored for a definition path,
// read back through the store-backed loader.
func registeredTaskSetsFor(t *testing.T, d *Deps, defPath string) []RegisteredTaskSet {
	t.Helper()
	state, err := LoadGlobalStateWith(d, StatePathFor(defPath))
	if err != nil {
		t.Fatalf("load registration: %v", err)
	}
	entry := state.Tasks[defPath]
	if entry == nil {
		return nil
	}
	return entry.TaskSets
}

func completionCWD(root string) CompletionInput {
	return CompletionInput{CWD: root}
}

func TestCompleteTaskSetIDsFromDiscovery(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	tasksDir, d := setupCompletionRepo(t, root)
	writeCompletionTaskSet(t, tasksDir, "alpha")
	writeCompletionTaskSet(t, tasksDir, "beta")

	stems, err := CompleteTaskSetIDsWith(d, project.DefaultDeps(), config.Load, completionCWD(root), "")
	if err != nil {
		t.Fatal(err)
	}
	if len(stems) != 2 || stems[0] != "alpha" || stems[1] != "beta" {
		t.Fatalf("stems = %#v", stems)
	}
}

func TestCompleteTaskTargetsOffersIdentifiersAndSetRelativeFiles(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	tasksDir, d := setupCompletionRepo(t, root)
	writeCompletionFixture(t, tasksDir, "feature", []Task{
		{ID: "01-a", File: "01-a.md", Title: "A", Type: "AFK", Status: "open"},
		{ID: "02-b", File: "02-b.md", Title: "B", Type: "AFK", Status: "done"},
	})

	ids, err := CompleteTaskTargetsWith(d, project.DefaultDeps(), config.Load, completionCWD(root), "")
	if err != nil {
		t.Fatal(err)
	}
	if len(ids) != 1 || ids[0] != "feature/" {
		t.Fatalf("identifiers = %#v", ids)
	}

	files, err := CompleteTaskTargetsWith(d, project.DefaultDeps(), config.Load, completionCWD(root), "feature/")
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 2 || files[0] != "feature/01-a.md" || files[1] != "feature/02-b.md" {
		t.Fatalf("set-relative files = %#v", files)
	}
}

func TestCompleteActionableTaskTargetsOmitsDoneSetsAndTasks(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	tasksDir, d := setupCompletionRepo(t, root)
	writeCompletionFixture(t, tasksDir, "archived", []Task{
		{ID: "01-a", File: "01-a.md", Title: "A", Type: "AFK", Status: "done"},
	})
	writeCompletionFixture(t, tasksDir, "feature", []Task{
		{ID: "01-a", File: "01-a.md", Title: "A", Type: "AFK", Status: "open"},
		{ID: "02-b", File: "02-b.md", Title: "B", Type: "AFK", Status: "done"},
	})

	ids, err := CompleteActionableTaskTargetsWith(d, project.DefaultDeps(), config.Load, completionCWD(root), "")
	if err != nil {
		t.Fatal(err)
	}
	if len(ids) != 1 || ids[0] != "feature/" {
		t.Fatalf("identifiers = %#v", ids)
	}

	files, err := CompleteActionableTaskTargetsWith(d, project.DefaultDeps(), config.Load, completionCWD(root), "feature/")
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 1 || files[0] != "feature/01-a.md" {
		t.Fatalf("set-relative files = %#v", files)
	}

	// The unfiltered variant (stream) still offers the Done set and done task.
	all, err := CompleteTaskTargetsWith(d, project.DefaultDeps(), config.Load, completionCWD(root), "")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(all, ",") != "archived/,feature/" {
		t.Fatalf("unfiltered identifiers = %#v", all)
	}
}

func TestCompletionsFilterArchivedTaskSets(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	tasksDir, d := setupCompletionRepo(t, root)
	writeCompletionFixture(t, tasksDir, "active", []Task{
		{ID: "01-a", File: "01-a.md", Title: "A", Type: "AFK", Status: "open"},
	})
	writeCompletionFixture(t, tasksDir, "archived", []Task{
		{ID: "01-a", File: "01-a.md", Title: "A", Type: "AFK", Status: "open"},
	})

	if _, err := RegisterWith(d, tasksDir, StatePathFor(tasksDir)); err != nil {
		t.Fatal(err)
	}
	if _, err := ArchiveTaskSetWith(d, nil, nil, ResolveInput{CWD: root}, "archived"); err != nil {
		t.Fatal(err)
	}

	ids, err := CompleteTaskSetIDsWith(d, project.DefaultDeps(), config.Load, completionCWD(root), "")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(ids, ",") != "active" {
		t.Fatalf("active ids = %#v", ids)
	}

	targets, err := CompleteTaskTargetsWith(d, project.DefaultDeps(), config.Load, completionCWD(root), "")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(targets, ",") != "active/" {
		t.Fatalf("snapshot targets = %#v", targets)
	}

	actionable, err := CompleteActionableTaskTargetsWith(d, project.DefaultDeps(), config.Load, completionCWD(root), "")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(actionable, ",") != "active/" {
		t.Fatalf("actionable targets = %#v", actionable)
	}

	archived, err := CompleteArchivedTaskSetIDsWith(d, project.DefaultDeps(), config.Load, completionCWD(root), "")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(archived, ",") != "archived" {
		t.Fatalf("archived ids = %#v", archived)
	}
}

func TestCompleteExportTaskSetIDsOrdersNewestFirst(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	tasksDir, d := setupCompletionRepo(t, root)
	writeCompletionTaskSet(t, tasksDir, "2026-06-01-alpha")
	writeCompletionTaskSet(t, tasksDir, "2026-06-15-beta")
	writeCompletionTaskSet(t, tasksDir, "2026-07-01-gamma")

	ids, err := CompleteExportTaskSetIDsWith(d, project.DefaultDeps(), config.Load, completionCWD(root), nil, "")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(ids, ",") != "2026-07-01-gamma,2026-06-15-beta,2026-06-01-alpha" {
		t.Fatalf("export ids = %#v (want newest-first)", ids)
	}
}

func TestCompleteExportTaskSetIDsExcludesAlreadyChosen(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	tasksDir, d := setupCompletionRepo(t, root)
	writeCompletionTaskSet(t, tasksDir, "2026-06-01-alpha")
	writeCompletionTaskSet(t, tasksDir, "2026-06-15-beta")
	writeCompletionTaskSet(t, tasksDir, "2026-07-01-gamma")

	ids, err := CompleteExportTaskSetIDsWith(d, project.DefaultDeps(), config.Load, completionCWD(root), []string{"2026-07-01-gamma", "2026-06-01-alpha"}, "")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(ids, ",") != "2026-06-15-beta" {
		t.Fatalf("export ids = %#v (want only the unchosen set)", ids)
	}
}

func TestCompleteExportTaskSetIDsOmitsArchived(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	tasksDir, d := setupCompletionRepo(t, root)
	writeCompletionTaskSet(t, tasksDir, "2026-06-01-active")
	writeCompletionTaskSet(t, tasksDir, "2026-07-01-archived")

	if _, err := RegisterWith(d, tasksDir, StatePathFor(tasksDir)); err != nil {
		t.Fatal(err)
	}
	if _, err := ArchiveTaskSetWith(d, nil, nil, ResolveInput{CWD: root}, "2026-07-01-archived"); err != nil {
		t.Fatal(err)
	}

	ids, err := CompleteExportTaskSetIDsWith(d, project.DefaultDeps(), config.Load, completionCWD(root), nil, "")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(ids, ",") != "2026-06-01-active" {
		t.Fatalf("export ids = %#v (want archived omitted)", ids)
	}
}

func TestCompleteProjectNamesUsesPickerVisibleNames(t *testing.T) {
	t.Parallel()
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
	t.Parallel()
	root := t.TempDir()
	initGitRepo(t, root)
	d := newTestDeps(t)
	tasksDir := storageTasksDir(t, d, root)
	writeCompletionTaskSet(t, tasksDir, "existing")
	writeCompletionTaskSet(t, tasksDir, "new-prd")

	dataHome := popDataDirWith(d)
	statePath := filepath.Join(dataHome, "state.json")
	canon, err := CanonicalDefinitionPath(tasksDir)
	if err != nil {
		t.Fatal(err)
	}

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
	before := registeredTaskSetsFor(t, d, canon)

	var notices bytes.Buffer
	d.NoticeOut = &notices

	stems, err := CompleteTaskSetIDsWith(d, project.DefaultDeps(), config.Load, completionCWD(root), "")
	if err != nil {
		t.Fatal(err)
	}
	if len(stems) != 2 {
		t.Fatalf("stems = %#v", stems)
	}

	after := registeredTaskSetsFor(t, d, canon)
	if !reflect.DeepEqual(after, before) {
		t.Fatalf("state mutated:\nbefore=%#v\nafter=%#v", before, after)
	}
	if _, err := os.Stat(filepath.Join(dataHome, "pop", "workloads-state.json")); !os.IsNotExist(err) {
		t.Fatal("expected no default state file write")
	}
	if _, err := os.Stat(statePath); !os.IsNotExist(err) {
		t.Fatalf("retired state.json was written: stat err = %v", err)
	}
	if notices.Len() != 0 {
		t.Fatalf("unexpected notices: %q", notices.String())
	}
}

func TestCompletionUnreadableDiscoveryReturnsEmptyWithoutError(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("chmod tests unreliable as root")
	}
	t.Parallel()
	root := t.TempDir()
	tasksDir, d := setupCompletionRepo(t, root)
	writeCompletionTaskSet(t, tasksDir, "a")
	if err := os.Chmod(tasksDir, 0o000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(tasksDir, 0o755) })

	stems, err := CompleteTaskSetIDsWith(d, project.DefaultDeps(), config.Load, completionCWD(root), "")
	if err != nil {
		t.Fatal(err)
	}
	if len(stems) != 0 {
		t.Fatalf("stems = %#v", stems)
	}
}

// setupCompletionRepo initializes a git repo at root with isolated Deps and
// returns the repository's Task storage tasks directory.
func setupCompletionRepo(t *testing.T, root string) (string, *Deps) {
	t.Helper()
	initGitRepo(t, root)
	d := newTestDeps(t)
	return storageTasksDir(t, d, root), d
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
	t.Parallel()
	d := newTestDeps(t)
	root := t.TempDir()
	defDir := filepath.Join(root, "planning")
	writeCompletionTaskSet(t, defDir, "planned")

	stems, err := CompleteTaskSetIDsWith(d, project.DefaultDeps(), config.Load, CompletionInput{
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
	t.Parallel()
	root := t.TempDir()
	tasksDir, d := setupCompletionRepo(t, root)
	writeCompletionFixture(t, tasksDir, "one", []Task{
		{ID: "01-a", File: "01-a.md", Title: "A", Type: "AFK", Status: "open"},
	})
	writeCompletionFixture(t, tasksDir, "two", []Task{
		{ID: "99-z", File: "99-z.md", Title: "Z", Type: "AFK", Status: "open"},
	})

	files, err := CompleteTaskTargetsWith(d, project.DefaultDeps(), config.Load, completionCWD(root), "two/")
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 1 || files[0] != "two/99-z.md" {
		t.Fatalf("files = %#v", files)
	}
}

func TestCompleteProjectNamesMissingConfigIsEmpty(t *testing.T) {
	t.Parallel()
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
	t.Parallel()
	root := t.TempDir()
	tasksDir, d := setupCompletionRepo(t, root)
	writeCompletionFixture(t, tasksDir, "demo", []Task{
		{ID: "01-a", File: "01-a.md", Title: "A", Type: "AFK", Status: "open"},
	})

	_, _ = CompleteTaskSetIDsWith(d, project.DefaultDeps(), config.Load, completionCWD(root), "")
	_, _ = CompleteTaskTargetsWith(d, project.DefaultDeps(), config.Load, completionCWD(root), "demo/")

	progressPath := filepath.Join(tasksDir, "demo", "progress.txt")
	if _, err := os.Stat(progressPath); !os.IsNotExist(err) {
		t.Fatal("completion should not create progress.txt")
	}
}

func TestCompleteTaskSetIDsDoesNotRegisterInStateFile(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	tasksDir, d := setupCompletionRepo(t, root)
	writeCompletionTaskSet(t, tasksDir, "fresh")

	if _, err := CompleteTaskSetIDsWith(d, project.DefaultDeps(), config.Load, completionCWD(root), ""); err != nil {
		t.Fatal(err)
	}
	statePath := DefaultStatePathWith(d)
	if _, err := os.Stat(statePath); !os.IsNotExist(err) {
		t.Fatalf("expected no state file at %s", statePath)
	}
}

func TestCompleteTaskSetIDsSorted(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	tasksDir, d := setupCompletionRepo(t, root)
	for _, stem := range []string{"charlie", "alpha", "bravo"} {
		writeCompletionTaskSet(t, tasksDir, stem)
	}

	stems, err := CompleteTaskSetIDsWith(d, project.DefaultDeps(), config.Load, completionCWD(root), "")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(stems, ",") != "alpha,bravo,charlie" {
		t.Fatalf("stems = %#v", stems)
	}
}
