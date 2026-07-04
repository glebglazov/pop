package cmd

import (
	"bytes"
	"compress/gzip"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/glebglazov/pop/config"
	"github.com/glebglazov/pop/tasks"
	"github.com/glebglazov/pop/tasks/binding"
	"github.com/glebglazov/pop/ui"
	"github.com/spf13/cobra"
)

// registrationSnapshot returns a stable string of the registration metadata
// stored for a definition path, read back through the store, so a test can
// assert that a cancelled operation wrote nothing.
func registrationSnapshot(t *testing.T, defPath string) string {
	t.Helper()
	canon, err := tasks.CanonicalDefinitionPath(defPath)
	if err != nil {
		t.Fatal(err)
	}
	state, err := tasks.LoadGlobalState(tasks.StatePathFor(canon))
	if err != nil {
		t.Fatal(err)
	}
	entry := state.Tasks[canon]
	if entry == nil {
		return ""
	}
	return fmt.Sprintf("%+v", entry.TaskSets)
}

func TestTaskStatusExitSuccessWithMalformedRows(t *testing.T) {
	root := t.TempDir()
	initGitRepoCmd(t, root)
	t.Setenv("XDG_DATA_HOME", filepath.Join(root, ".xdg"))
	taskDir := filepath.Join(cmdTasksDir(t, root), "bad")
	if err := os.MkdirAll(taskDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(taskDir, "index.json"), []byte(`not json`), 0o644); err != nil {
		t.Fatal(err)
	}

	taskProject = ""
	taskPath = ""
	taskDefPath = ""
	t.Cleanup(func() {
		taskProject = ""
		taskPath = ""
		taskDefPath = ""
	})

	oldWd, _ := os.Getwd()
	if err := os.Chdir(root); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(oldWd) })

	d := tasks.DefaultDeps()
	var buf bytes.Buffer
	if err := runTaskStatusWith(d, &buf, ""); err != nil {
		t.Fatalf("status should succeed: %v", err)
	}
	if buf.Len() == 0 {
		t.Fatal("expected output")
	}
}

func TestTaskStatusUnreadableDiscoveryFails(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("chmod tests unreliable as root")
	}
	root := t.TempDir()
	initGitRepoCmd(t, root)
	t.Setenv("XDG_DATA_HOME", filepath.Join(root, ".xdg"))
	taskDir := cmdTasksDir(t, root)
	if err := os.MkdirAll(taskDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(taskDir, 0o000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(taskDir, 0o755) })

	taskProject = ""
	taskPath = ""
	taskDefPath = ""
	t.Cleanup(func() {
		taskProject = ""
		taskPath = ""
		taskDefPath = ""
	})

	oldWd, _ := os.Getwd()
	if err := os.Chdir(root); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(oldWd) })

	err := runTaskStatusWith(tasks.DefaultDeps(), &bytes.Buffer{}, "")
	if err == nil {
		t.Fatal("expected setup failure")
	}
}

func TestTaskSetPriorityRefreshesTable(t *testing.T) {
	root := t.TempDir()
	taskProject = ""
	taskPath = ""
	taskDefPath = ""
	t.Cleanup(func() {
		taskProject = ""
		taskPath = ""
		taskDefPath = ""
	})

	initGitRepoCmd(t, root)
	t.Setenv("XDG_DATA_HOME", filepath.Join(root, ".xdg"))
	tasksDir := cmdTasksDir(t, root)
	taskDir := filepath.Join(tasksDir, "feature")
	if err := os.MkdirAll(taskDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(taskDir, "01-a.md"), []byte("## Acceptance criteria\n\n- [ ] ok\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	manifest := `{"tasks":[{"id":"01-a","file":"01-a.md","title":"A","type":"AFK","status":"open"}]}`
	if err := os.WriteFile(filepath.Join(taskDir, "index.json"), []byte(manifest), 0o644); err != nil {
		t.Fatal(err)
	}

	oldWd, _ := os.Getwd()
	if err := os.Chdir(root); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(oldWd) })

	if _, err := tasks.RegisterWith(tasks.DefaultDeps(), tasksDir, tasks.DefaultStatePath()); err != nil {
		t.Fatal(err)
	}

	var buf bytes.Buffer
	if err := runTaskSetPriorityWith(tasks.DefaultDeps(), &buf, "feature", "7"); err != nil {
		t.Fatalf("set-priority failed: %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, "Updated priority for feature: 0 -> 7") {
		t.Fatalf("missing change report:\n%s", out)
	}
	if !strings.Contains(out, "7 NEXT") {
		t.Fatalf("missing refreshed table with NEXT:\n%s", out)
	}
}

func TestTaskArchiveCommandsAndArchivedStatus(t *testing.T) {
	root := t.TempDir()
	resetTaskFlags()
	t.Cleanup(resetTaskFlags)

	initGitRepoCmd(t, root)
	t.Setenv("XDG_DATA_HOME", filepath.Join(root, ".xdg"))
	tasksDir := cmdTasksDir(t, root)
	writeTaskThoughts(t, tasksDir, "alpha")
	writeTaskThoughts(t, tasksDir, "beta")

	oldWd, _ := os.Getwd()
	if err := os.Chdir(root); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(oldWd) })

	if _, err := tasks.RegisterWith(tasks.DefaultDeps(), tasksDir, tasks.StatePathFor(tasksDir)); err != nil {
		t.Fatal(err)
	}

	var archiveOut bytes.Buffer
	if err := runTaskArchiveWith(tasks.DefaultDeps(), &archiveOut, "alpha"); err != nil {
		t.Fatalf("archive failed: %v", err)
	}
	if !strings.Contains(archiveOut.String(), "Archived task set alpha") {
		t.Fatalf("missing archive report:\n%s", archiveOut.String())
	}

	var defaultOut bytes.Buffer
	taskStatusArchived = false
	if err := runTaskStatusWith(tasks.DefaultDeps(), &defaultOut, ""); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(defaultOut.String(), "alpha") || !strings.Contains(defaultOut.String(), "beta") {
		t.Fatalf("default status wrong:\n%s", defaultOut.String())
	}
	if !strings.Contains(defaultOut.String(), "pop tasks status --archived") {
		t.Fatalf("default status missing archive hint:\n%s", defaultOut.String())
	}

	var archivedOut bytes.Buffer
	taskStatusArchived = true
	if err := runTaskStatusWith(tasks.DefaultDeps(), &archivedOut, ""); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(archivedOut.String(), "alpha") || strings.Contains(archivedOut.String(), "beta") {
		t.Fatalf("archived status wrong:\n%s", archivedOut.String())
	}

	taskStatusArchived = false
	var unarchiveOut bytes.Buffer
	if err := runTaskUnarchiveWith(tasks.DefaultDeps(), &unarchiveOut, "alpha"); err != nil {
		t.Fatalf("unarchive failed: %v", err)
	}
	if !strings.Contains(unarchiveOut.String(), "Unarchived task set alpha") {
		t.Fatalf("missing unarchive report:\n%s", unarchiveOut.String())
	}
}

// TestTaskRegisterActivatesAndStatusStaysPure verifies the ADR-0061 contract:
// a freshly authored set is inert under reads (status writes nothing, shows
// nothing), and `pop tasks register` is the deliberate writer that activates it.
func TestTaskRegisterActivatesAndStatusStaysPure(t *testing.T) {
	root := t.TempDir()
	resetTaskFlags()
	t.Cleanup(resetTaskFlags)

	initGitRepoCmd(t, root)
	t.Setenv("XDG_DATA_HOME", filepath.Join(root, ".xdg"))
	tasksDir := cmdTasksDir(t, root)
	writeTaskThoughts(t, tasksDir, "draft")

	oldWd, _ := os.Getwd()
	if err := os.Chdir(root); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(oldWd) })

	// A read does not register: status shows no sets and Task state stays empty.
	var beforeOut bytes.Buffer
	if err := runTaskStatusWith(tasks.DefaultDeps(), &beforeOut, ""); err != nil {
		t.Fatalf("status failed: %v", err)
	}
	if strings.Contains(beforeOut.String(), "draft") {
		t.Fatalf("unregistered set leaked into status:\n%s", beforeOut.String())
	}
	if snap := registrationSnapshot(t, tasksDir); snap != "" {
		t.Fatalf("read registered a set: %q", snap)
	}

	// register activates it and prints the resulting status.
	var regOut bytes.Buffer
	if err := runTaskRegisterWith(tasks.DefaultDeps(), &regOut, ""); err != nil {
		t.Fatalf("register failed: %v", err)
	}
	if !strings.Contains(regOut.String(), "draft") {
		t.Fatalf("register output missing the activated set:\n%s", regOut.String())
	}
	if snap := registrationSnapshot(t, tasksDir); !strings.Contains(snap, "draft") {
		t.Fatalf("register did not persist registration: %q", snap)
	}

	// After registration, status (a read) shows it without re-registering.
	var afterOut bytes.Buffer
	if err := runTaskStatusWith(tasks.DefaultDeps(), &afterOut, ""); err != nil {
		t.Fatalf("status after register failed: %v", err)
	}
	if !strings.Contains(afterOut.String(), "draft") {
		t.Fatalf("status after register missing the set:\n%s", afterOut.String())
	}
}

func TestTaskArchiveSelectionPrechecksDoneOnlyAndCancelWritesNothing(t *testing.T) {
	root := t.TempDir()
	resetTaskFlags()
	t.Cleanup(resetTaskFlags)

	initGitRepoCmd(t, root)
	t.Setenv("XDG_DATA_HOME", filepath.Join(root, ".xdg"))
	tasksDir := cmdTasksDir(t, root)
	writeTaskThoughtsWithStatus(t, tasksDir, "done", "done")
	writeTaskThoughtsWithStatus(t, tasksDir, "ready", "open")
	if _, err := tasks.RegisterWith(tasks.DefaultDeps(), tasksDir, tasks.StatePathFor(tasksDir)); err != nil {
		t.Fatal(err)
	}

	oldWd, _ := os.Getwd()
	if err := os.Chdir(root); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(oldWd) })
	stubCompleteInteractive(t, true)

	var items []ui.MultiSelectItem
	stubCompleteSelect(t, ui.MultiSelectResult{Confirmed: false}, &items)
	before := registrationSnapshot(t, tasksDir)

	var stdout bytes.Buffer
	if err := runTaskArchiveSelectionWith(tasks.DefaultDeps(), &stdout, strings.NewReader(""), false); err != nil {
		t.Fatalf("archive picker cancel failed: %v", err)
	}
	if stdout.Len() != 0 {
		t.Fatalf("cancel should render nothing:\n%s", stdout.String())
	}
	if len(items) != 2 {
		t.Fatalf("items = %d, want done and ready: %+v", len(items), items)
	}
	if !items[0].Checked || items[1].Checked {
		t.Fatalf("prechecked policy wrong: %+v", items)
	}
	if !strings.Contains(items[0].Label, "[DONE]") || !strings.Contains(items[0].Label, "done") {
		t.Fatalf("done row label missing id/status: %+v", items[0])
	}
	if !strings.Contains(items[1].Label, "[READY]") || !strings.Contains(items[1].Label, "ready") {
		t.Fatalf("ready row label missing id/status: %+v", items[1])
	}
	after := registrationSnapshot(t, tasksDir)
	if before != after {
		t.Fatalf("cancel must not write:\nbefore:%s\nafter:%s", before, after)
	}
}

func TestTaskArchiveSelectionConfirmArchivesBatch(t *testing.T) {
	root := t.TempDir()
	resetTaskFlags()
	t.Cleanup(resetTaskFlags)

	initGitRepoCmd(t, root)
	t.Setenv("XDG_DATA_HOME", filepath.Join(root, ".xdg"))
	tasksDir := cmdTasksDir(t, root)
	writeTaskThoughtsWithStatus(t, tasksDir, "done", "done")
	writeTaskThoughtsWithStatus(t, tasksDir, "ready", "open")
	if _, err := tasks.RegisterWith(tasks.DefaultDeps(), tasksDir, tasks.StatePathFor(tasksDir)); err != nil {
		t.Fatal(err)
	}

	oldWd, _ := os.Getwd()
	if err := os.Chdir(root); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(oldWd) })
	stubCompleteInteractive(t, true)
	stubCompleteSelect(t, ui.MultiSelectResult{Confirmed: true, Checked: []int{0, 1}}, nil)

	var stdout bytes.Buffer
	if err := runTaskArchiveSelectionWith(tasks.DefaultDeps(), &stdout, strings.NewReader(""), false); err != nil {
		t.Fatalf("archive picker confirm failed: %v", err)
	}
	out := stdout.String()
	if !strings.Contains(out, "Archived task sets done, ready") {
		t.Fatalf("missing batch archive report:\n%s", out)
	}
	active, err := tasks.RefreshWith(tasks.DefaultDeps(), tasksDir, tasks.StatePathFor(tasksDir))
	if err != nil {
		t.Fatal(err)
	}
	if len(active.Rows) != 0 {
		t.Fatalf("active rows = %#v, want none", active.Rows)
	}
}

func TestTaskUnarchiveSelectionListsArchivedOnlyUncheckedAndCancelWritesNothing(t *testing.T) {
	root := t.TempDir()
	resetTaskFlags()
	t.Cleanup(resetTaskFlags)

	initGitRepoCmd(t, root)
	t.Setenv("XDG_DATA_HOME", filepath.Join(root, ".xdg"))
	tasksDir := cmdTasksDir(t, root)
	writeTaskThoughtsWithStatus(t, tasksDir, "archived", "open")
	writeTaskThoughtsWithStatus(t, tasksDir, "active", "open")
	if _, err := tasks.RegisterWith(tasks.DefaultDeps(), tasksDir, tasks.StatePathFor(tasksDir)); err != nil {
		t.Fatal(err)
	}
	if _, err := tasks.ArchiveTaskSetWith(tasks.DefaultDeps(), nil, nil, tasks.ResolveInput{DefinitionOverride: tasksDir, CWD: root}, "archived"); err != nil {
		t.Fatal(err)
	}

	oldWd, _ := os.Getwd()
	if err := os.Chdir(root); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(oldWd) })
	stubCompleteInteractive(t, true)

	var items []ui.MultiSelectItem
	stubCompleteSelect(t, ui.MultiSelectResult{Confirmed: false}, &items)
	before := registrationSnapshot(t, tasksDir)

	var stdout bytes.Buffer
	if err := runTaskUnarchiveSelectionWith(tasks.DefaultDeps(), &stdout, strings.NewReader("")); err != nil {
		t.Fatalf("unarchive picker cancel failed: %v", err)
	}
	if stdout.Len() != 0 {
		t.Fatalf("cancel should render nothing:\n%s", stdout.String())
	}
	if len(items) != 1 {
		t.Fatalf("items = %d, want archived only: %+v", len(items), items)
	}
	if items[0].Checked {
		t.Fatalf("unarchive picker should start unchecked: %+v", items)
	}
	if !strings.Contains(items[0].Label, "[READY]") || !strings.Contains(items[0].Label, "archived") {
		t.Fatalf("archived row label missing id/status: %+v", items[0])
	}
	if strings.Contains(items[0].Label, "active") {
		t.Fatalf("active row leaked into unarchive picker: %+v", items)
	}
	after := registrationSnapshot(t, tasksDir)
	if before != after {
		t.Fatalf("cancel must not write:\nbefore:%s\nafter:%s", before, after)
	}
}

func TestTaskUnarchiveSelectionConfirmRestoresBatch(t *testing.T) {
	root := t.TempDir()
	resetTaskFlags()
	t.Cleanup(resetTaskFlags)

	initGitRepoCmd(t, root)
	t.Setenv("XDG_DATA_HOME", filepath.Join(root, ".xdg"))
	tasksDir := cmdTasksDir(t, root)
	writeTaskThoughtsWithStatus(t, tasksDir, "one", "open")
	writeTaskThoughtsWithStatus(t, tasksDir, "two", "open")
	if _, err := tasks.RegisterWith(tasks.DefaultDeps(), tasksDir, tasks.StatePathFor(tasksDir)); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"one", "two"} {
		if _, err := tasks.ArchiveTaskSetWith(tasks.DefaultDeps(), nil, nil, tasks.ResolveInput{DefinitionOverride: tasksDir, CWD: root}, id); err != nil {
			t.Fatal(err)
		}
	}

	oldWd, _ := os.Getwd()
	if err := os.Chdir(root); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(oldWd) })
	stubCompleteInteractive(t, true)
	stubCompleteSelect(t, ui.MultiSelectResult{Confirmed: true, Checked: []int{0, 1}}, nil)

	var stdout bytes.Buffer
	if err := runTaskUnarchiveSelectionWith(tasks.DefaultDeps(), &stdout, strings.NewReader("")); err != nil {
		t.Fatalf("unarchive picker confirm failed: %v", err)
	}
	out := stdout.String()
	if !strings.Contains(out, "Unarchived task sets one, two") {
		t.Fatalf("missing batch unarchive report:\n%s", out)
	}
	active, err := tasks.RefreshWith(tasks.DefaultDeps(), tasksDir, tasks.StatePathFor(tasksDir))
	if err != nil {
		t.Fatal(err)
	}
	if len(active.Rows) != 2 {
		t.Fatalf("active rows = %#v, want both restored", active.Rows)
	}
}

func TestTaskArchiveYesArchivesDoneOnly(t *testing.T) {
	root := t.TempDir()
	resetTaskFlags()
	t.Cleanup(resetTaskFlags)

	initGitRepoCmd(t, root)
	t.Setenv("XDG_DATA_HOME", filepath.Join(root, ".xdg"))
	tasksDir := cmdTasksDir(t, root)
	writeTaskThoughtsWithStatus(t, tasksDir, "done", "done")
	writeTaskThoughtsWithStatus(t, tasksDir, "ready", "open")
	if _, err := tasks.RegisterWith(tasks.DefaultDeps(), tasksDir, tasks.StatePathFor(tasksDir)); err != nil {
		t.Fatal(err)
	}

	oldWd, _ := os.Getwd()
	if err := os.Chdir(root); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(oldWd) })

	var stdout bytes.Buffer
	if err := runTaskArchiveSelectionWith(tasks.DefaultDeps(), &stdout, strings.NewReader(""), true); err != nil {
		t.Fatalf("--yes archive failed: %v", err)
	}
	if !strings.Contains(stdout.String(), "Archived task set done") {
		t.Fatalf("missing done archive report:\n%s", stdout.String())
	}
	active, err := tasks.RefreshWith(tasks.DefaultDeps(), tasksDir, tasks.StatePathFor(tasksDir))
	if err != nil {
		t.Fatal(err)
	}
	if len(active.Rows) != 1 || active.Rows[0].ID != "ready" {
		t.Fatalf("--yes should leave only ready active: %#v", active.Rows)
	}
}

func TestTaskArchiveYesNoDoneNoop(t *testing.T) {
	root := t.TempDir()
	resetTaskFlags()
	t.Cleanup(resetTaskFlags)

	initGitRepoCmd(t, root)
	t.Setenv("XDG_DATA_HOME", filepath.Join(root, ".xdg"))
	tasksDir := cmdTasksDir(t, root)
	writeTaskThoughtsWithStatus(t, tasksDir, "ready", "open")
	if _, err := tasks.RegisterWith(tasks.DefaultDeps(), tasksDir, tasks.StatePathFor(tasksDir)); err != nil {
		t.Fatal(err)
	}

	oldWd, _ := os.Getwd()
	if err := os.Chdir(root); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(oldWd) })

	before, _ := os.ReadFile(tasks.StatePathFor(tasksDir))
	var stdout bytes.Buffer
	if err := runTaskArchiveSelectionWith(tasks.DefaultDeps(), &stdout, strings.NewReader(""), true); err != nil {
		t.Fatalf("--yes zero done should be clean: %v", err)
	}
	if !strings.Contains(stdout.String(), "No done task sets to archive.") {
		t.Fatalf("missing no-op message:\n%s", stdout.String())
	}
	after, _ := os.ReadFile(tasks.StatePathFor(tasksDir))
	if string(before) != string(after) {
		t.Fatalf("zero-done --yes must not write:\nbefore:%s\nafter:%s", before, after)
	}
}

func TestTaskArchiveManagedWorktreeConfirmAndDecline(t *testing.T) {
	root := t.TempDir()
	resetTaskFlags()
	t.Cleanup(resetTaskFlags)

	initGitRepoCmd(t, root)
	t.Setenv("XDG_DATA_HOME", filepath.Join(root, ".xdg"))
	tasksDir := cmdTasksDir(t, root)
	writeTaskThoughtsWithStatus(t, tasksDir, "managed-done", "done")
	if _, err := tasks.RegisterWith(tasks.DefaultDeps(), tasksDir, tasks.StatePathFor(tasksDir)); err != nil {
		t.Fatal(err)
	}

	wt := cmdArchiveTestWorktree(t, root, "managed-branch")
	td := tasks.DefaultDeps()
	id, err := tasks.ResolveRepositoryIdentity(td, root)
	if err != nil {
		t.Fatal(err)
	}
	store := &binding.Store{}
	store.Put(binding.Key(id, "managed-done"), binding.Binding{
		RuntimePath: wt,
		Branch:      "managed-branch",
		Project:     filepath.Base(root),
		Provisioned: true,
	})
	if err := binding.Save(td, store); err != nil {
		t.Fatal(err)
	}

	oldWd, _ := os.Getwd()
	if err := os.Chdir(root); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(oldWd) })

	origLoad := taskConfigLoad
	taskConfigLoad = func(string) (*config.Config, error) {
		return &config.Config{Projects: []config.ProjectEntry{{Path: root}}}, nil
	}
	t.Cleanup(func() { taskConfigLoad = origLoad })

	var declined bytes.Buffer
	if err := runTaskArchiveWithConfirm(td, &declined, strings.NewReader("n\n"), false, "managed-done"); err != nil {
		t.Fatalf("declined archive: %v", err)
	}
	if strings.Contains(declined.String(), "Archived task set") {
		t.Fatalf("decline should not archive:\n%s", declined.String())
	}
	if _, err := os.Stat(wt); err != nil {
		t.Fatalf("worktree should remain after decline: %v", err)
	}

	var confirmed bytes.Buffer
	if err := runTaskArchiveWithConfirm(td, &confirmed, strings.NewReader("y\n"), false, "managed-done"); err != nil {
		t.Fatalf("confirmed archive: %v", err)
	}
	if !strings.Contains(confirmed.String(), "Archived task set managed-done") {
		t.Fatalf("missing archive report:\n%s", confirmed.String())
	}
	if _, err := os.Stat(wt); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("worktree should be removed after confirm")
	}
}

func TestTaskArchiveManagedWorktreeYesAndMetadataOnly(t *testing.T) {
	root := t.TempDir()
	resetTaskFlags()
	t.Cleanup(resetTaskFlags)

	initGitRepoCmd(t, root)
	t.Setenv("XDG_DATA_HOME", filepath.Join(root, ".xdg"))
	tasksDir := cmdTasksDir(t, root)
	writeTaskThoughtsWithStatus(t, tasksDir, "managed-yes", "done")
	writeTaskThoughtsWithStatus(t, tasksDir, "adopted-done", "done")
	if _, err := tasks.RegisterWith(tasks.DefaultDeps(), tasksDir, tasks.StatePathFor(tasksDir)); err != nil {
		t.Fatal(err)
	}

	managedWT := cmdArchiveTestWorktree(t, root, "managed-yes-branch")
	adoptedWT := cmdArchiveTestWorktree(t, root, "adopted-branch")
	td := tasks.DefaultDeps()
	id, err := tasks.ResolveRepositoryIdentity(td, root)
	if err != nil {
		t.Fatal(err)
	}
	store := &binding.Store{}
	store.Put(binding.Key(id, "managed-yes"), binding.Binding{
		RuntimePath: managedWT,
		Branch:      "managed-yes-branch",
		Project:     filepath.Base(root),
		Provisioned: true,
	})
	store.Put(binding.Key(id, "adopted-done"), binding.Binding{
		RuntimePath: adoptedWT,
		Branch:      "adopted-branch",
		Project:     filepath.Base(root),
		Provisioned: false,
	})
	if err := binding.Save(td, store); err != nil {
		t.Fatal(err)
	}

	oldWd, _ := os.Getwd()
	if err := os.Chdir(root); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(oldWd) })

	origLoad := taskConfigLoad
	taskConfigLoad = func(string) (*config.Config, error) {
		return &config.Config{Projects: []config.ProjectEntry{{Path: root}}}, nil
	}
	t.Cleanup(func() { taskConfigLoad = origLoad })

	var managedOut bytes.Buffer
	if err := runTaskArchiveWithConfirm(td, &managedOut, tasks.NonInteractiveReader{}, true, "managed-yes"); err != nil {
		t.Fatalf("--yes managed archive: %v", err)
	}
	if _, err := os.Stat(managedWT); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("managed worktree should be removed with --yes")
	}

	var adoptedOut bytes.Buffer
	if err := runTaskArchiveWithConfirm(td, &adoptedOut, tasks.NonInteractiveReader{}, false, "adopted-done"); err != nil {
		t.Fatalf("adopted archive: %v", err)
	}
	if _, err := os.Stat(adoptedWT); err != nil {
		t.Fatalf("adopted worktree must remain: %v", err)
	}
	if _, err := tasks.UnarchiveTaskSetWith(td, nil, taskConfigLoad, tasks.ResolveInput{DefinitionOverride: tasksDir, CWD: root}, "adopted-done"); err != nil {
		t.Fatalf("unarchive adopted: %v", err)
	}
}

func cmdArchiveTestWorktree(t *testing.T, repo, branch string) string {
	t.Helper()
	cmd := exec.Command("git", "-C", repo, "commit", "--allow-empty", "-m", "base")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("initial commit: %v\n%s", err, out)
	}
	wt := filepath.Join(t.TempDir(), "wt-"+branch)
	cmd = exec.Command("git", "-C", repo, "worktree", "add", "-b", branch, wt, "HEAD")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("worktree add: %v\n%s", err, out)
	}
	return wt
}

func TestTaskArchiveNoArgNonInteractiveRejected(t *testing.T) {
	root := t.TempDir()
	resetTaskFlags()
	t.Cleanup(resetTaskFlags)

	initGitRepoCmd(t, root)
	t.Setenv("XDG_DATA_HOME", filepath.Join(root, ".xdg"))
	tasksDir := cmdTasksDir(t, root)
	writeTaskThoughtsWithStatus(t, tasksDir, "done", "done")
	if _, err := tasks.RegisterWith(tasks.DefaultDeps(), tasksDir, tasks.StatePathFor(tasksDir)); err != nil {
		t.Fatal(err)
	}

	oldWd, _ := os.Getwd()
	if err := os.Chdir(root); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(oldWd) })
	stubCompleteInteractive(t, false)

	err := runTaskArchiveSelectionWith(tasks.DefaultDeps(), &bytes.Buffer{}, strings.NewReader(""), false)
	if err == nil {
		t.Fatal("no-arg non-interactive archive should error")
	}
	ee, ok := err.(*tasks.ExitError)
	if !ok || ee.Code != tasks.ExitOperational {
		t.Fatalf("err = %v, want ExitOperational", err)
	}
	if !strings.Contains(err.Error(), "--yes") || !strings.Contains(err.Error(), "bare identifier") {
		t.Fatalf("err should point to --yes or a bare identifier: %v", err)
	}
}

func TestTaskUnarchiveNoArgNonInteractiveRejected(t *testing.T) {
	root := t.TempDir()
	resetTaskFlags()
	t.Cleanup(resetTaskFlags)

	initGitRepoCmd(t, root)
	t.Setenv("XDG_DATA_HOME", filepath.Join(root, ".xdg"))
	tasksDir := cmdTasksDir(t, root)
	writeTaskThoughtsWithStatus(t, tasksDir, "demo", "open")
	if _, err := tasks.RegisterWith(tasks.DefaultDeps(), tasksDir, tasks.StatePathFor(tasksDir)); err != nil {
		t.Fatal(err)
	}
	if _, err := tasks.ArchiveTaskSetWith(tasks.DefaultDeps(), nil, nil, tasks.ResolveInput{DefinitionOverride: tasksDir, CWD: root}, "demo"); err != nil {
		t.Fatal(err)
	}

	oldWd, _ := os.Getwd()
	if err := os.Chdir(root); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(oldWd) })
	stubCompleteInteractive(t, false)

	err := runTaskUnarchiveSelectionWith(tasks.DefaultDeps(), &bytes.Buffer{}, strings.NewReader(""))
	if err == nil {
		t.Fatal("no-arg non-interactive unarchive should error")
	}
	ee, ok := err.(*tasks.ExitError)
	if !ok || ee.Code != tasks.ExitOperational {
		t.Fatalf("err = %v, want ExitOperational", err)
	}
	if !strings.Contains(err.Error(), "bare identifier") || !strings.Contains(err.Error(), "pop tasks unarchive <task-set>") {
		t.Fatalf("err should point to the bare identifier form: %v", err)
	}
}

func TestTaskActionVerbsRejectArchivedTargets(t *testing.T) {
	root := setupRunTaskCmdFixture(t)
	agent := writeRunTaskFakeAgent(t, root)

	resetTaskFlags()
	taskAgentCmd = agent
	t.Cleanup(resetTaskFlags)

	tasksDir := cmdTasksDir(t, root)
	if _, err := tasks.RegisterWith(tasks.DefaultDeps(), tasksDir, tasks.StatePathFor(tasksDir)); err != nil {
		t.Fatal(err)
	}
	if _, err := tasks.ArchiveTaskSetWith(tasks.DefaultDeps(), nil, nil, tasks.ResolveInput{}, "demo"); err != nil {
		t.Fatal(err)
	}

	cases := []struct {
		name string
		run  func() error
	}{
		{"implement set", func() error {
			return runTaskRunTasksWith(tasks.DefaultDeps(), &bytes.Buffer{}, io.Discard, strings.NewReader("n\n"), "demo", false)
		}},
		{"implement task", func() error {
			return runTaskRunTaskWith(tasks.DefaultDeps(), &bytes.Buffer{}, io.Discard, strings.NewReader("n\n"), "demo/01-a.md", false)
		}},
		{"open task", func() error {
			return runTaskResetTaskWith(tasks.DefaultDeps(), &bytes.Buffer{}, "demo/01-a.md")
		}},
		{"open set", func() error {
			return runTaskOpenTasksWith(tasks.DefaultDeps(), &bytes.Buffer{}, strings.NewReader(""), "demo")
		}},
		{"complete task", func() error {
			return runTaskCompleteTaskWith(tasks.DefaultDeps(), &bytes.Buffer{}, "demo/01-a.md")
		}},
		{"complete set", func() error {
			return runTaskCompleteTasksWith(tasks.DefaultDeps(), &bytes.Buffer{}, strings.NewReader(""), "demo")
		}},
		{"skip task", func() error {
			return runTaskSkipTaskWith(tasks.DefaultDeps(), &bytes.Buffer{}, "demo/01-a.md")
		}},
		{"skip set", func() error {
			return runTaskSkipTasksWith(tasks.DefaultDeps(), &bytes.Buffer{}, strings.NewReader(""), "demo")
		}},
		{"set-priority", func() error {
			return runTaskSetPriorityWith(tasks.DefaultDeps(), &bytes.Buffer{}, "demo", "4")
		}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.run()
			var ee *tasks.ExitError
			if !errors.As(err, &ee) || ee.Code == 0 {
				t.Fatalf("err = %v", err)
			}
			if !strings.Contains(err.Error(), "pop tasks unarchive demo") || !strings.Contains(err.Error(), "first") {
				t.Fatalf("missing unarchive-first guidance: %v", err)
			}
		})
	}
}

func TestTaskSnapshotVerbsAcceptArchivedTargets(t *testing.T) {
	root := setupRunTaskCmdFixture(t)

	resetTaskFlags()
	taskExportOutput = filepath.Join(root, "archived-demo.tar.gz")
	t.Cleanup(resetTaskFlags)

	tasksDir := cmdTasksDir(t, root)
	if _, err := tasks.RegisterWith(tasks.DefaultDeps(), tasksDir, tasks.StatePathFor(tasksDir)); err != nil {
		t.Fatal(err)
	}
	if _, err := tasks.ArchiveTaskSetWith(tasks.DefaultDeps(), nil, nil, tasks.ResolveInput{}, "demo"); err != nil {
		t.Fatal(err)
	}

	t.Run("timings", func(t *testing.T) {
		var buf bytes.Buffer
		if err := runTaskTimingsWith(tasks.DefaultDeps(), &buf, "demo"); err != nil {
			t.Fatalf("timings: %v", err)
		}
		if !strings.Contains(buf.String(), "demo") {
			t.Fatalf("timings output missing task set:\n%s", buf.String())
		}
	})

	t.Run("show-path", func(t *testing.T) {
		var buf bytes.Buffer
		if err := runTaskShowPathWith(tasks.DefaultDeps(), &buf, "demo"); err != nil {
			t.Fatalf("show-path: %v", err)
		}
		if !strings.Contains(buf.String(), filepath.Join("tasks", "demo")) {
			t.Fatalf("show-path output = %q", buf.String())
		}
	})

	t.Run("export", func(t *testing.T) {
		var buf bytes.Buffer
		if err := runTaskExportWith(tasks.DefaultDeps(), &buf, []string{"demo"}); err != nil {
			t.Fatalf("export: %v", err)
		}
		if _, err := os.Stat(strings.TrimSpace(buf.String())); err != nil {
			t.Fatalf("exported archive missing: %v", err)
		}
	})
}

func TestTaskStatusUsesDefinitionOverride(t *testing.T) {
	root := t.TempDir()
	defDir := filepath.Join(root, "planning")
	writeTaskThoughts(t, defDir, "a")

	taskProject = ""
	taskPath = root
	taskDefPath = defDir
	t.Cleanup(func() {
		taskProject = ""
		taskPath = ""
		taskDefPath = ""
	})

	t.Setenv("XDG_DATA_HOME", root)
	var buf bytes.Buffer
	if err := runTaskStatusWith(tasks.DefaultDeps(), &buf, ""); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(buf.String(), "a") {
		t.Fatalf("expected PRD in output:\n%s", buf.String())
	}
}

func TestTaskResolveByProjectName(t *testing.T) {
	root := t.TempDir()
	projectDir := filepath.Join(root, "svc")
	if err := os.MkdirAll(projectDir, 0o755); err != nil {
		t.Fatal(err)
	}
	initGitRepoCmd(t, projectDir)
	t.Setenv("XDG_DATA_HOME", filepath.Join(root, ".xdg"))
	writeTaskThoughts(t, cmdTasksDir(t, projectDir), "svc")

	cfgPath := filepath.Join(root, "config.toml")
	if err := os.WriteFile(cfgPath, []byte("projects = [{ path = \""+projectDir+"\" }]\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	origLoad := taskConfigLoad
	taskConfigLoad = func(path string) (*config.Config, error) {
		return config.Load(cfgPath)
	}
	t.Cleanup(func() { taskConfigLoad = origLoad })

	taskProject = "svc"
	taskPath = ""
	taskDefPath = ""
	t.Cleanup(func() {
		taskProject = ""
		taskPath = ""
		taskDefPath = ""
	})

	var buf bytes.Buffer
	if err := runTaskStatusWith(tasks.DefaultDeps(), &buf, ""); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(buf.String(), "svc") {
		t.Fatalf("expected PRD in output:\n%s", buf.String())
	}
}

// cmdTasksDir resolves the Task storage tasks directory for a repository checkout.
// XDG_DATA_HOME must already be set so the location is deterministic.
func cmdTasksDir(t *testing.T, repoRoot string) string {
	t.Helper()
	id, err := tasks.ResolveRepositoryIdentity(tasks.DefaultDeps(), repoRoot)
	if err != nil {
		t.Fatalf("resolve storage: %v", err)
	}
	return id.TasksDir
}

// writeTaskThoughts creates a minimal valid Task set under tasksDir/<stem>.
func writeTaskThoughts(t *testing.T, tasksDir, stem string) {
	t.Helper()
	taskDir := filepath.Join(tasksDir, stem)
	if err := os.MkdirAll(taskDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(taskDir, "01-a.md"), []byte("## Acceptance criteria\n\n- [ ] ok\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	manifest := `{"tasks":[{"id":"01-a","file":"01-a.md","title":"A","type":"AFK","status":"open"}]}`
	if err := os.WriteFile(filepath.Join(taskDir, "index.json"), []byte(manifest), 0o644); err != nil {
		t.Fatal(err)
	}
}

func writeTaskThoughtsWithStatus(t *testing.T, tasksDir, stem, status string) {
	t.Helper()
	taskDir := filepath.Join(tasksDir, stem)
	if err := os.MkdirAll(taskDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(taskDir, "01-a.md"), []byte("## Acceptance criteria\n\n- [ ] ok\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	manifest := fmt.Sprintf(`{"tasks":[{"id":"01-a","file":"01-a.md","title":"A","type":"AFK","status":%q}]}`, status)
	if err := os.WriteFile(filepath.Join(taskDir, "index.json"), []byte(manifest), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestTaskStatusShowsRuntimeLock(t *testing.T) {
	root := t.TempDir()
	initGitRepoCmd(t, root)
	t.Setenv("XDG_DATA_HOME", filepath.Join(root, ".xdg"))
	tasksDir := cmdTasksDir(t, root)
	writeTaskThoughts(t, tasksDir, "demo")
	if _, err := tasks.RegisterWith(tasks.DefaultDeps(), tasksDir, tasks.DefaultStatePath()); err != nil {
		t.Fatal(err)
	}

	d := tasks.DefaultDeps()
	runtimePath, err := tasks.ResolveRuntimePathWith(d, root, "")
	if err != nil {
		t.Fatal(err)
	}
	lock, err := tasks.AcquireRuntimeLockForSet(d, runtimePath, "demo", io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = lock.Release() })

	taskProject = ""
	taskPath = ""
	taskDefPath = ""
	t.Cleanup(resetTaskFlags)

	oldWd, _ := os.Getwd()
	if err := os.Chdir(root); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(oldWd) })

	var buf bytes.Buffer
	if err := runTaskStatusWith(d, &buf, ""); err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	if !strings.Contains(out, "Runtime execution lock") || !strings.Contains(out, "PID") {
		t.Fatalf("missing lock rendering:\n%s", out)
	}
}

// TestTaskStatusSurfacesUnsatisfiableWorktreeDirective asserts `pop tasks status`
// shows a `name` worktree directive that names a worktree absent on this machine
// as a config/registration-class error on the set (ADR-0059), without provisioning
// or draining anything.
func TestTaskStatusSurfacesUnsatisfiableWorktreeDirective(t *testing.T) {
	root := t.TempDir()
	initGitRepoCmd(t, root)
	t.Setenv("XDG_DATA_HOME", filepath.Join(root, ".xdg"))
	tasksDir := cmdTasksDir(t, root)
	writeTaskThoughts(t, tasksDir, "demo")
	d := tasks.DefaultDeps()
	if _, err := tasks.RegisterWith(d, tasksDir, tasks.DefaultStatePath()); err != nil {
		t.Fatal(err)
	}

	canon, err := tasks.CanonicalDefinitionPathWith(d, tasksDir)
	if err != nil {
		t.Fatal(err)
	}
	if err := tasks.UpdateGlobalStateWith(d, tasks.StatePathFor(canon), func(s *tasks.GlobalState) error {
		entry := s.Tasks[canon]
		for i := range entry.TaskSets {
			if entry.TaskSets[i].ID == "demo" {
				entry.TaskSets[i].WorktreeIntent = &tasks.WorktreeDirective{Name: "absent"}
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}

	taskProject = ""
	taskPath = ""
	taskDefPath = ""
	t.Cleanup(resetTaskFlags)

	oldWd, _ := os.Getwd()
	if err := os.Chdir(root); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(oldWd) })

	var buf bytes.Buffer
	if err := runTaskStatusWith(d, &buf, ""); err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	if !strings.Contains(out, "Config errors:") {
		t.Fatalf("missing config-error diagnostics:\n%s", out)
	}
	if !strings.Contains(out, "no worktree of that name") {
		t.Fatalf("missing the named-directive fault:\n%s", out)
	}
}

func TestTaskStatusSetArgDrillsIn(t *testing.T) {
	root := t.TempDir()
	initGitRepoCmd(t, root)
	t.Setenv("XDG_DATA_HOME", filepath.Join(root, ".xdg"))
	tasksDir := cmdTasksDir(t, root)
	writeTaskThoughts(t, tasksDir, "alpha")
	writeTaskThoughts(t, tasksDir, "beta")
	if _, err := tasks.RegisterWith(tasks.DefaultDeps(), tasksDir, tasks.DefaultStatePath()); err != nil {
		t.Fatal(err)
	}

	taskProject = ""
	taskPath = ""
	taskDefPath = ""
	t.Cleanup(resetTaskFlags)

	oldWd, _ := os.Getwd()
	if err := os.Chdir(root); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(oldWd) })

	var buf bytes.Buffer
	if err := runTaskStatusWith(tasks.DefaultDeps(), &buf, "alpha"); err != nil {
		t.Fatalf("drill-in should succeed: %v", err)
	}
	out := buf.String()
	// Per-task table, not the all-sets overview.
	if strings.Contains(out, "TASK SET") {
		t.Fatalf("expected per-task breakdown, got overview:\n%s", out)
	}
	for _, want := range []string{"alpha", "STATUS", "TYPE", "ID", "TITLE", "BLOCKED-BY", "01-a"} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %q in breakdown:\n%s", want, out)
		}
	}
	// Scoped to the named set only.
	if strings.Contains(out, "beta") {
		t.Fatalf("breakdown leaked another set:\n%s", out)
	}
}

func TestTaskStatusUnknownSetArgErrors(t *testing.T) {
	root := t.TempDir()
	initGitRepoCmd(t, root)
	t.Setenv("XDG_DATA_HOME", filepath.Join(root, ".xdg"))
	tasksDir := cmdTasksDir(t, root)
	writeTaskThoughts(t, tasksDir, "alpha")
	if _, err := tasks.RegisterWith(tasks.DefaultDeps(), tasksDir, tasks.DefaultStatePath()); err != nil {
		t.Fatal(err)
	}

	taskProject = ""
	taskPath = ""
	taskDefPath = ""
	t.Cleanup(resetTaskFlags)

	oldWd, _ := os.Getwd()
	if err := os.Chdir(root); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(oldWd) })

	err := runTaskStatusWith(tasks.DefaultDeps(), &bytes.Buffer{}, "nope")
	if err == nil {
		t.Fatal("expected error for unknown set")
	}
	// The error lists the valid identifiers so a typo becomes the answer.
	if !strings.Contains(err.Error(), "alpha") {
		t.Fatalf("error should list valid ids: %v", err)
	}
}

func initGitRepoCmd(t *testing.T, root string) {
	t.Helper()
	cmd := exec.Command("git", "init")
	cmd.Dir = root
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git init: %v\n%s", err, out)
	}
	for _, args := range [][]string{
		{"config", "user.email", "test@test"},
		{"config", "user.name", "test"},
	} {
		c := exec.Command("git", args...)
		c.Dir = root
		if out, err := c.CombinedOutput(); err != nil {
			t.Fatal(err, string(out))
		}
	}
}

func TestHandleTaskExitMapsCodes(t *testing.T) {
	tests := []struct {
		err  error
		code int
	}{
		{nil, 0},
		{&tasks.ExitError{Code: tasks.ExitNoRunnable, Err: fmt.Errorf("no work")}, tasks.ExitNoRunnable},
		{&tasks.ExitError{Code: tasks.ExitInterrupted, Err: fmt.Errorf("interrupted")}, tasks.ExitInterrupted},
	}
	for _, tt := range tests {
		if tt.err == nil {
			continue
		}
		var ee *tasks.ExitError
		if !errors.As(tt.err, &ee) || ee.Code != tt.code {
			t.Fatalf("code = %v, want %d", tt.err, tt.code)
		}
	}
}

func TestRunTaskCmdDeclinedIsSuccess(t *testing.T) {
	root := setupRunTaskCmdFixture(t)
	agent := writeRunTaskFakeAgent(t, root)

	taskProject = ""
	taskPath = ""
	taskDefPath = ""
	taskAgentPreset = ""
	taskAgentCmd = agent
	taskRunYes = false
	t.Cleanup(resetTaskFlags)

	var stdout bytes.Buffer
	err := runTaskRunTaskWith(tasks.DefaultDeps(), &stdout, io.Discard, strings.NewReader("n\n"), "", false)
	if err != nil {
		t.Fatalf("declined should succeed: %v", err)
	}
	if !strings.Contains(stdout.String(), "RUN") {
		t.Fatalf("missing pre-run table:\n%s", stdout.String())
	}
	_ = root
}

func TestRunTasksCmdStartsWithoutAFKConsent(t *testing.T) {
	root := setupRunTaskCmdFixture(t)
	agent := writeRunTaskFakeAgent(t, root)

	resetTaskFlags()
	taskAgentCmd = agent
	t.Cleanup(resetTaskFlags)

	var stdout bytes.Buffer
	err := runTaskRunTasksWith(tasks.DefaultDeps(), &stdout, io.Discard, strings.NewReader("n\n"), "", false)
	if err != nil {
		t.Fatalf("set drain should proceed without AFK consent: %v", err)
	}
	if !strings.Contains(stdout.String(), "RUN") {
		t.Fatalf("missing pre-run table:\n%s", stdout.String())
	}
	if strings.Contains(stdout.String(), "Run AFK tasks in this Task set?") {
		t.Fatalf("set drain must not ask for AFK consent:\n%s", stdout.String())
	}
	if !strings.Contains(stdout.String(), "✓ Completed demo/01-a") {
		t.Fatalf("expected set to drain:\n%s", stdout.String())
	}
	_ = root
}

// TestRunTasksCmdUnboundDrainsInCurrentCheckout asserts a default (non-inline)
// whole-set run does not auto-provision a managed worktree: it drains inline in
// the current checkout (routing never provisions — ADR-0052) and persists a
// default (adopted, never-provisioned) Worktree binding to that checkout so later
// drains resume there (ADR-0062).
func TestRunTasksCmdUnboundDrainsInCurrentCheckout(t *testing.T) {
	root := setupRunTaskCmdFixture(t)
	agent := writeRunTaskFakeAgent(t, root)

	resetTaskFlags()
	taskAgentCmd = agent
	t.Cleanup(resetTaskFlags)

	if err := runTaskRunTasksWith(tasks.DefaultDeps(), &bytes.Buffer{}, io.Discard, strings.NewReader("n\n"), "demo", false); err != nil {
		t.Fatalf("run task set: %v", err)
	}

	store, err := binding.Load(tasks.DefaultDeps())
	if err != nil {
		t.Fatal(err)
	}
	id, err := tasks.ResolveRepositoryIdentity(tasks.DefaultDeps(), root)
	if err != nil {
		t.Fatal(err)
	}
	b, ok := store.Get(binding.Key(id, "demo"))
	if !ok {
		t.Fatalf("expected a default binding for the unbound run")
	}
	wantRoot, _ := filepath.EvalSymlinks(root)
	gotBound, _ := filepath.EvalSymlinks(b.RuntimePath)
	if gotBound != wantRoot {
		t.Fatalf("default binding RuntimePath = %q, want current checkout %q", gotBound, wantRoot)
	}
	if b.Provisioned {
		t.Fatalf("default binding must be adopted (Provisioned=false), got %+v", b)
	}
}

// TestRunTasksCmdInWorktreeProvisionsAndBinds asserts `--in-worktree` forks a
// managed worktree off the current checkout and records a provisioned Worktree
// binding (ADR-0072).
func TestRunTasksCmdInWorktreeProvisionsAndBinds(t *testing.T) {
	root := setupRunTaskCmdFixture(t)
	agent := writeRunTaskFakeAgent(t, root)

	resetTaskFlags()
	taskAgentCmd = agent
	taskInWorktree = true
	t.Cleanup(resetTaskFlags)

	if err := runTaskRunTasksWith(tasks.DefaultDeps(), &bytes.Buffer{}, io.Discard, strings.NewReader("n\n"), "demo", false); err != nil {
		t.Fatalf("run task set: %v", err)
	}

	store, err := binding.Load(tasks.DefaultDeps())
	if err != nil {
		t.Fatal(err)
	}
	id, err := tasks.ResolveRepositoryIdentity(tasks.DefaultDeps(), root)
	if err != nil {
		t.Fatal(err)
	}
	b, ok := store.Get(binding.Key(id, "demo"))
	if !ok {
		t.Fatalf("expected provisioned worktree binding for --in-worktree run")
	}
	if !b.Provisioned {
		t.Fatalf("binding = %+v, want Provisioned=true", b)
	}
	wantRoot, _ := filepath.EvalSymlinks(root)
	gotRuntime, _ := filepath.EvalSymlinks(b.RuntimePath)
	if gotRuntime == wantRoot {
		t.Fatalf("provisioned worktree = current checkout %q, want a distinct managed worktree", wantRoot)
	}
}

// TestRunTasksCmdInWorktreeRejectsBoundSet asserts `--in-worktree` on an
// already-bound set is rejected with guidance to unbind first.
func TestRunTasksCmdInWorktreeRejectsBoundSet(t *testing.T) {
	root := setupRunTaskCmdFixture(t)
	agent := writeRunTaskFakeAgent(t, root)

	wt := filepath.Join(t.TempDir(), "existing-wt")
	if out, err := exec.Command("git", "-C", root, "worktree", "add", "-b", "feature", wt, "HEAD").CombinedOutput(); err != nil {
		t.Fatalf("worktree add: %v\n%s", err, out)
	}
	id, err := tasks.ResolveRepositoryIdentity(tasks.DefaultDeps(), root)
	if err != nil {
		t.Fatal(err)
	}
	store := &binding.Store{}
	store.Put(binding.Key(id, "demo"), binding.Adopt(wt, "feature", ""))
	if err := binding.Save(tasks.DefaultDeps(), store); err != nil {
		t.Fatal(err)
	}

	resetTaskFlags()
	taskAgentCmd = agent
	taskInWorktree = true
	t.Cleanup(resetTaskFlags)

	err = runTaskRunTasksWith(tasks.DefaultDeps(), &bytes.Buffer{}, io.Discard, strings.NewReader("n\n"), "demo", false)
	if err == nil || !strings.Contains(err.Error(), "already bound") || !strings.Contains(err.Error(), "unbind-worktree") {
		t.Fatalf("err = %v, want already-bound rejection with unbind guidance", err)
	}
}

func TestRunTasksCmdRejectsRelativeTaskSetPath(t *testing.T) {
	root := setupRunTaskCmdFixture(t)
	resetTaskFlags()
	t.Cleanup(resetTaskFlags)

	err := runTaskRunTasksWith(tasks.DefaultDeps(), &bytes.Buffer{}, io.Discard, strings.NewReader("n\n"), relTo(t, root, runTaskCmdDemoDir(t, root)), false)
	if err == nil || !strings.Contains(err.Error(), "invalid target") || !strings.Contains(err.Error(), "valid: demo") {
		t.Fatalf("relative Task set path error = %v", err)
	}
}

func TestRunTaskCmdRejectsRelativeTaskPath(t *testing.T) {
	root := setupRunTaskCmdFixture(t)
	resetTaskFlags()
	t.Cleanup(resetTaskFlags)

	err := runTaskRunTaskWith(tasks.DefaultDeps(), &bytes.Buffer{}, io.Discard, strings.NewReader("n\n"), relTo(t, root, filepath.Join(runTaskCmdDemoDir(t, root), "01-a.md")), false)
	if err == nil || !strings.Contains(err.Error(), "invalid target") || !strings.Contains(err.Error(), "valid: demo") {
		t.Fatalf("relative task path error = %v", err)
	}
}

func TestRunTaskCmdTargetsTaskSetRelativeFile(t *testing.T) {
	root := setupRunTaskCmdFixture(t)
	resetTaskFlags()
	t.Cleanup(resetTaskFlags)

	err := runTaskRunTaskWith(tasks.DefaultDeps(), &bytes.Buffer{}, io.Discard, strings.NewReader("n\n"), "demo/01-a.md", false)
	if err != nil {
		t.Fatalf("task-set-relative file failed: %v", err)
	}
	_ = root
}

func TestRunTaskCmdTargetsTaskSetIdentifier(t *testing.T) {
	root := setupRunTaskCmdFixture(t)
	resetTaskFlags()
	t.Cleanup(resetTaskFlags)

	err := runTaskRunTaskWith(tasks.DefaultDeps(), &bytes.Buffer{}, io.Discard, strings.NewReader("n\n"), "demo", false)
	if err != nil {
		t.Fatalf("Task set identifier failed: %v", err)
	}
	_ = root
}

func TestRunTaskCmdRejectsInvalidTaskTargets(t *testing.T) {
	root := setupRunTaskCmdFixture(t)
	resetTaskFlags()
	t.Cleanup(resetTaskFlags)

	err := runTaskRunTaskWith(tasks.DefaultDeps(), &bytes.Buffer{}, io.Discard, strings.NewReader("n\n"), "01-a", false)
	if err == nil || !strings.Contains(err.Error(), "valid: demo") {
		t.Fatalf("bare task ID error = %v", err)
	}

	err = runTaskRunTaskWith(tasks.DefaultDeps(), &bytes.Buffer{}, io.Discard, strings.NewReader("n\n"), "01-a.md", false)
	if err == nil || !strings.Contains(err.Error(), "bare filenames") {
		t.Fatalf("bare filename error = %v", err)
	}

	err = runTaskRunTaskWith(tasks.DefaultDeps(), &bytes.Buffer{}, io.Discard, strings.NewReader("n\n"), filepath.Join(runTaskCmdDemoDir(t, root), "01-a.md"), false)
	if err == nil || !strings.Contains(err.Error(), "absolute paths") {
		t.Fatalf("absolute path error = %v", err)
	}
}

func TestImplementCmdRejectsMoreThanOnePositional(t *testing.T) {
	err := taskImplementCmd.Args(taskImplementCmd, []string{"one", "two"})
	if err == nil {
		t.Fatal("expected usage error")
	}
}

func TestImplementTimeoutDefaultMatchesAttemptTimeout(t *testing.T) {
	// The flag default is a clean literal ("1h") for pretty help text, while the
	// executor's zero-value fallback is the DefaultAttemptTimeout constant. They
	// are independent sources; this guards them against drift.
	def := taskImplementCmd.Flags().Lookup("timeout").DefValue
	got, err := time.ParseDuration(def)
	if err != nil {
		t.Fatalf("flag default %q does not parse: %v", def, err)
	}
	if got != tasks.DefaultAttemptTimeout {
		t.Errorf("flag default %q = %v, want DefaultAttemptTimeout %v", def, got, tasks.DefaultAttemptTimeout)
	}
}

func TestImplementDispatchByTargetShape(t *testing.T) {
	// A ".md" target is a Task-set-relative file reference (single task); a bare
	// identifier or empty target (no argument) drains an auto-selected set.
	cases := []struct {
		target   string
		wantFile bool
	}{
		{"", false},
		{"demo", false},
		{"thoughts/issues/live-agent-smoke", false},
		{"demo/01-a.md", true},
		{"2026-06-08-feature/03-x.md", true},
	}
	for _, c := range cases {
		if got := isTaskFileTarget(c.target); got != c.wantFile {
			t.Errorf("isTaskFileTarget(%q) = %v, want %v", c.target, got, c.wantFile)
		}
	}
}

func TestResetTaskCmdRequiresOnePositional(t *testing.T) {
	for _, args := range [][]string{nil, {"one", "two"}} {
		if err := taskResetTaskCmd.Args(taskResetTaskCmd, args); err == nil {
			t.Fatalf("args %v should fail as a usage error", args)
		}
	}
}

func TestResetTaskCmdTargetsTaskSetRelativeFile(t *testing.T) {
	root := setupRunTaskCmdFixture(t)
	resetTaskFlags()
	t.Cleanup(resetTaskFlags)

	manifestPath := filepath.Join(runTaskCmdDemoDir(t, root), "index.json")
	manifest := `{"tasks":[{"id":"01-a","file":"01-a.md","title":"A","type":"AFK","status":"failed","failed_after":2}]}`
	if err := os.WriteFile(manifestPath, []byte(manifest), 0o644); err != nil {
		t.Fatal(err)
	}

	var stdout bytes.Buffer
	if err := runTaskResetTaskWith(tasks.DefaultDeps(), &stdout, "demo/01-a.md"); err != nil {
		t.Fatalf("task-set-relative file failed: %v", err)
	}
	if !strings.Contains(stdout.String(), "Reset task demo/01-a to open") {
		t.Fatalf("missing canonical success output:\n%s", stdout.String())
	}
	_ = root
}

func TestResetTaskCmdRejectsBareIdentifier(t *testing.T) {
	root := setupRunTaskCmdFixture(t)
	resetTaskFlags()
	t.Cleanup(resetTaskFlags)

	err := runTaskResetTaskWith(tasks.DefaultDeps(), &bytes.Buffer{}, "demo")
	if err == nil || !strings.Contains(err.Error(), "<task-set>/<file>.md") {
		t.Fatalf("bare identifier error = %v", err)
	}
	_ = root
}

func TestCompleteTaskCmdRequiresOnePositional(t *testing.T) {
	for _, args := range [][]string{nil, {"one", "two"}} {
		if err := taskCompleteTaskCmd.Args(taskCompleteTaskCmd, args); err == nil {
			t.Fatalf("args %v should fail as a usage error", args)
		}
	}
}

func TestCompleteTaskCmdTargetsTaskSetRelativeFile(t *testing.T) {
	root := setupRunTaskCmdFixture(t)
	resetTaskFlags()
	t.Cleanup(resetTaskFlags)

	var stdout bytes.Buffer
	if err := runTaskCompleteTaskWith(tasks.DefaultDeps(), &stdout, "demo/01-a.md"); err != nil {
		t.Fatalf("task-set-relative file failed: %v", err)
	}
	if !strings.Contains(stdout.String(), "Completed task demo/01-a") {
		t.Fatalf("missing canonical success output:\n%s", stdout.String())
	}
	manifestPath := filepath.Join(runTaskCmdDemoDir(t, root), "index.json")
	data, err := os.ReadFile(manifestPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), `"status": "done"`) {
		t.Fatalf("task not marked done:\n%s", data)
	}
}

func TestRunTasksCmdRejectsTaskSetRelativeFile(t *testing.T) {
	root := setupRunTaskCmdFixture(t)
	resetTaskFlags()
	t.Cleanup(resetTaskFlags)

	err := runTaskRunTasksWith(tasks.DefaultDeps(), &bytes.Buffer{}, io.Discard, strings.NewReader("n\n"), "demo/01-a.md", false)
	if err == nil || !strings.Contains(err.Error(), "bare task set identifier") {
		t.Fatalf("file reference error = %v", err)
	}
	_ = root
}

func TestRunTasksCmdTargetsTaskSetIdentifier(t *testing.T) {
	root := setupRunTaskCmdFixture(t)
	agent := writeRunTaskFakeAgent(t, root)
	resetTaskFlags()
	taskAgentCmd = agent
	t.Cleanup(resetTaskFlags)

	err := runTaskRunTasksWith(tasks.DefaultDeps(), &bytes.Buffer{}, io.Discard, strings.NewReader("n\n"), "demo", false)
	if err != nil {
		t.Fatalf("Task set identifier failed: %v", err)
	}
	_ = root
}

func TestRunTasksCmdRejectsAbsoluteTaskSetPath(t *testing.T) {
	root := setupRunTaskCmdFixture(t)
	resetTaskFlags()
	t.Cleanup(resetTaskFlags)

	err := runTaskRunTasksWith(tasks.DefaultDeps(), &bytes.Buffer{}, io.Discard, strings.NewReader("n\n"), runTaskCmdDemoDir(t, root), false)
	if err == nil || !strings.Contains(err.Error(), "absolute paths") {
		t.Fatalf("absolute path error = %v", err)
	}
}

func TestTaskCommandSurfaceUsesTaskSetVocabulary(t *testing.T) {
	names := map[string]*cobra.Command{}
	for _, c := range taskCmd.Commands() {
		names[c.Name()] = c
	}

	if _, ok := names["implement"]; !ok {
		t.Fatal("implement command is not registered")
	}
	// run and drain merged into the single implement verb (ADR 0015).
	if _, ok := names["run"]; ok {
		t.Fatal("removed run verb is still registered")
	}
	if _, ok := names["drain"]; ok {
		t.Fatal("removed drain verb is still registered")
	}
	if _, ok := names["run-prd"]; ok {
		t.Fatal("removed run-prd alias is still registered")
	}

	if names["open"] == nil {
		t.Fatal("open command is not registered")
	}
	// The pre-rename --issue-set / --issue flags were removed; assert by their
	// legacy names that they stay gone.
	if names["open"].Flags().Lookup("issue-set") != nil {
		t.Fatal("open still exposes removed --issue-set flag")
	}
	if names["open"].Flags().Lookup("issue") != nil {
		t.Fatal("open still exposes removed --issue flag")
	}
	if names["implement"].Flags().Lookup("issue-set") != nil {
		t.Fatal("implement still exposes removed --issue-set flag")
	}
	if names["implement"].Flags().Lookup("issue") != nil {
		t.Fatal("implement still exposes removed --issue flag")
	}
}

func TestTaskAllowDirtyFlagAcceptsOptionalStrategies(t *testing.T) {
	t.Cleanup(resetTaskFlags)
	for _, command := range []*cobra.Command{taskImplementCmd} {
		flag := command.Flags().Lookup("allow-dirty")
		if flag == nil {
			t.Fatalf("%s missing --allow-dirty", command.Name())
		}
		if flag.NoOptDefVal != string(tasks.DirtyRuntimeContinue) {
			t.Fatalf("%s bare --allow-dirty = %q", command.Name(), flag.NoOptDefVal)
		}
		if err := command.Flags().Parse([]string{"--allow-dirty"}); err != nil {
			t.Fatalf("%s rejected bare --allow-dirty: %v", command.Name(), err)
		}
		if taskAllowDirty != tasks.DirtyRuntimeContinue {
			t.Fatalf("%s bare --allow-dirty parsed as %q", command.Name(), taskAllowDirty)
		}
		for _, strategy := range tasks.ValidDirtyRuntimeStrategies() {
			if err := command.Flags().Parse([]string{"--allow-dirty=" + strategy}); err != nil {
				t.Fatalf("%s rejected %q: %v", command.Name(), strategy, err)
			}
		}
		err := command.Flags().Parse([]string{"--allow-dirty=invalid"})
		if err == nil || !strings.Contains(err.Error(), "continue, commit-and-continue, stash-and-continue") {
			t.Fatalf("%s invalid strategy error = %v", command.Name(), err)
		}
	}
}

func TestRunTaskCmdNonInteractiveFails(t *testing.T) {
	root := setupRunTaskCmdFixture(t)
	agent := writeRunTaskFakeAgent(t, root)

	resetTaskFlags()
	taskAgentCmd = agent
	t.Cleanup(resetTaskFlags)

	err := runTaskRunTaskWith(tasks.DefaultDeps(), &bytes.Buffer{}, io.Discard, tasks.NonInteractiveReader{}, "", false)
	var ee *tasks.ExitError
	if !errors.As(err, &ee) || ee.Code != tasks.ExitOperational {
		t.Fatalf("err = %v", err)
	}
	_ = root
}

func resetTaskFlags() {
	taskProject = ""
	taskPath = ""
	taskDefPath = ""
	taskRuntimePath = ""
	taskStatusArchived = false
	taskAgentPreset = ""
	taskAgentPresets = nil
	taskAgentCmd = ""
	taskAgentOutput = ""
	taskRunYes = false
	taskInWorktree = false
	taskAllowDirty = tasks.DirtyRuntimeContinue
	taskExportOutput = ""
	taskImportAs = ""
	taskStreamFull = false
	taskStreamRaw = false
	taskStreamLast = false
}

func setupRunTaskCmdFixture(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	oldWd, _ := os.Getwd()
	if err := os.Chdir(root); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(oldWd) })

	cmd := exec.Command("git", "init")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git init: %v\n%s", err, out)
	}
	for _, args := range [][]string{
		{"config", "user.email", "test@test"},
		{"config", "user.name", "test"},
	} {
		if out, err := exec.Command("git", args...).CombinedOutput(); err != nil {
			t.Fatal(err, string(out))
		}
	}
	writeFileCmd(t, filepath.Join(root, ".gitignore"), ".agent/\n.xdg/\n")
	writeFileCmd(t, filepath.Join(root, "README.md"), "# test\n")
	if out, err := exec.Command("git", "add", "-A").CombinedOutput(); err != nil {
		t.Fatal(err, string(out))
	}
	if out, err := exec.Command("git", "commit", "-m", "init").CombinedOutput(); err != nil {
		t.Fatal(err, string(out))
	}

	t.Setenv("XDG_DATA_HOME", filepath.Join(root, ".xdg"))
	tasksDir := cmdTasksDir(t, root)
	writeTaskThoughts(t, tasksDir, "demo")
	if _, err := tasks.RegisterWith(tasks.DefaultDeps(), tasksDir, tasks.DefaultStatePath()); err != nil {
		t.Fatal(err)
	}
	return root
}

// runTaskCmdDemoDir returns the storage directory of the fixture's "demo" Task set.
func runTaskCmdDemoDir(t *testing.T, root string) string {
	t.Helper()
	return filepath.Join(cmdTasksDir(t, root), "demo")
}

// relTo returns a relative path from base to target, failing the test on error.
func relTo(t *testing.T, base, target string) string {
	t.Helper()
	rel, err := filepath.Rel(base, target)
	if err != nil {
		t.Fatal(err)
	}
	return rel
}

func writeRunTaskFakeAgent(t *testing.T, root string) string {
	t.Helper()
	dir := filepath.Join(root, ".agent")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "fake-agent.sh")
	script := "#!/bin/sh\nTASK=$(printf '%s' \"$1\" | sed -n 's|^You are implementing the task at: ||p' | head -1)\n" +
		"if [ -n \"$TASK\" ] && [ -f \"$TASK\" ]; then sed -i '' 's/- \\[ \\]/- [x]/g' \"$TASK\" 2>/dev/null || sed -i 's/- \\[ \\]/- [x]/g' \"$TASK\"; fi\n" +
		"printf 'SUMMARY_START\\ncmd test\\nSUMMARY_END\\nTASK_COMPLETE\\n'\n"
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}

func writeFileCmd(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// TestRunTasksCmdQuotaPauseExitsQuotaPaused pins the machine-readable signal a
// supervisor reads: a drain that stops on an agent quota pause exits with the
// dedicated ExitQuotaPaused code rather than a clean ExitSuccess.
func TestRunTasksCmdQuotaPauseExitsQuotaPaused(t *testing.T) {
	root := setupRunTaskCmdFixture(t)
	installClaudeQuotaAgentCmd(t, root)

	resetTaskFlags()
	taskAgentPreset = "claude"
	taskRunYes = true
	t.Cleanup(resetTaskFlags)

	err := runTaskRunTasksWith(tasks.DefaultDeps(), &bytes.Buffer{}, io.Discard, tasks.NonInteractiveReader{}, "demo", true)
	var ee *tasks.ExitError
	if !errors.As(err, &ee) {
		t.Fatalf("err = %v, want *tasks.ExitError", err)
	}
	if ee.Code != tasks.ExitQuotaPaused {
		t.Fatalf("exit code = %d, want ExitQuotaPaused (%d)", ee.Code, tasks.ExitQuotaPaused)
	}
}

func installClaudeQuotaAgentCmd(t *testing.T, root string) {
	t.Helper()
	dir := filepath.Join(root, ".agent-bin")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	script := "#!/bin/sh\n" +
		"printf '%s\\n' '{\"type\":\"result\",\"subtype\":\"error_during_execution\",\"result\":\"You'\"'\"'ve hit your weekly limit · resets Mon 12:00am\"}'\n"
	bin := filepath.Join(dir, "claude")
	if err := os.WriteFile(bin, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
}

// TestImplementAgentFlagExplicitness pins the distinction between the built-in
// fallback and an explicitly supplied --agent fallback list.
func TestImplementAgentFlagExplicitness(t *testing.T) {
	f := taskImplementCmd.Flags().Lookup("agent")
	if f == nil {
		t.Fatal("agent flag not registered")
	}
	if f.Changed {
		t.Fatal("defaulted agent flag must not report Changed")
	}
	if err := taskImplementCmd.Flags().Set("agent", "claude"); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		f.Changed = false
		_ = f.Value.Set(f.DefValue)
	})
	if !taskImplementCmd.Flags().Changed("agent") {
		t.Fatal("explicitly passed agent flag must report Changed even at the default value")
	}
}

func TestVerifierSteeringFlagsRegistered(t *testing.T) {
	// `pop tasks verify` accepts repeatable --agent and an --effort override.
	agent := taskVerifyCmd.Flags().Lookup("agent")
	if agent == nil {
		t.Fatal("tasks verify --agent flag not registered")
	}
	if agent.Value.Type() != "stringArray" {
		t.Fatalf("tasks verify --agent type = %q, want stringArray (repeatable)", agent.Value.Type())
	}
	if taskVerifyCmd.Flags().Lookup("effort") == nil {
		t.Fatal("tasks verify --effort flag not registered")
	}

	// `pop tasks implement` accepts repeatable --verify-agent and --verify-effort.
	verifyAgent := taskImplementCmd.Flags().Lookup("verify-agent")
	if verifyAgent == nil {
		t.Fatal("tasks implement --verify-agent flag not registered")
	}
	if verifyAgent.Value.Type() != "stringArray" {
		t.Fatalf("tasks implement --verify-agent type = %q, want stringArray (repeatable)", verifyAgent.Value.Type())
	}
	if taskImplementCmd.Flags().Lookup("verify-effort") == nil {
		t.Fatal("tasks implement --verify-effort flag not registered")
	}
}

func TestTaskExportImportRoundtripCmd(t *testing.T) {
	root := t.TempDir()
	initGitRepoCmd(t, root)
	t.Setenv("XDG_DATA_HOME", filepath.Join(root, ".xdg"))
	tasksDir := cmdTasksDir(t, root)
	const setID = "2026-06-01-user-auth"
	writeTaskThoughts(t, tasksDir, setID)

	taskProject = ""
	taskPath = ""
	taskDefPath = ""
	taskExportOutput = ""
	taskImportAs = ""
	t.Cleanup(resetTaskFlags)

	oldWd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(root); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(oldWd) })

	d := tasks.DefaultDeps()
	var exportBuf bytes.Buffer
	if err := runTaskExportWith(d, &exportBuf, []string{setID}); err != nil {
		t.Fatalf("export: %v", err)
	}
	archivePath := strings.TrimSpace(exportBuf.String())
	if _, err := os.Stat(archivePath); err != nil {
		t.Fatalf("archive missing: %v", err)
	}

	dstRoot := t.TempDir()
	initGitRepoCmd(t, dstRoot)
	t.Setenv("XDG_DATA_HOME", filepath.Join(dstRoot, ".xdg"))
	oldWd2, _ := os.Getwd()
	if err := os.Chdir(dstRoot); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(oldWd2) })

	var importBuf bytes.Buffer
	if err := runTaskImportWith(d, &importBuf, archivePath); err != nil {
		t.Fatalf("import: %v", err)
	}
	importedPath := strings.TrimSpace(importBuf.String())
	if _, err := os.Stat(filepath.Join(importedPath, "index.json")); err != nil {
		t.Fatalf("imported set missing manifest: %v", err)
	}
}

// writeStreamData writes a gzipped attempt stream file at dir/name with
// the given header, events, and footer records.
func writeStreamData(t *testing.T, dir, name string, agent string, attempt int, start time.Time, outcome string, durationMS int64) {
	t.Helper()
	var jsonl bytes.Buffer
	enc := json.NewEncoder(&jsonl)
	if err := enc.Encode(map[string]any{
		"type": "header", "agent": agent, "attempt": attempt, "start_time": start.UTC().Format(time.RFC3339),
	}); err != nil {
		t.Fatal(err)
	}
	if err := enc.Encode(map[string]any{
		"type": "event", "at_ms": int64(5), "raw": `{"type":"system","subtype":"init"}`,
	}); err != nil {
		t.Fatal(err)
	}
	if err := enc.Encode(map[string]any{
		"type": "event", "at_ms": int64(100), "raw": `{"type":"assistant","message":{"content":[{"type":"text","text":"hello"}]}}`,
	}); err != nil {
		t.Fatal(err)
	}
	if err := enc.Encode(map[string]any{
		"type": "footer", "outcome": outcome, "duration_ms": durationMS,
	}); err != nil {
		t.Fatal(err)
	}

	var gz bytes.Buffer
	zw := gzip.NewWriter(&gz)
	if _, err := zw.Write(jsonl.Bytes()); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, name), gz.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
}

// setupStreamCmdFixture creates a git repo with one registered task set
// ("demo") and writes one attempt stream for the task, returning the root.
func setupStreamCmdFixture(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	oldWd, _ := os.Getwd()
	if err := os.Chdir(root); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(oldWd) })

	initGitRepoCmd(t, root)
	t.Setenv("XDG_DATA_HOME", filepath.Join(root, ".xdg"))
	tasksDir := cmdTasksDir(t, root)
	writeTaskThoughts(t, tasksDir, "demo")
	if _, err := tasks.RegisterWith(tasks.DefaultDeps(), tasksDir, tasks.StatePathFor(tasksDir)); err != nil {
		t.Fatal(err)
	}

	// Write stream data for the single task.
	streamDir := filepath.Join(tasksDir, "demo", "streams", "01-a")
	base := time.Date(2026, 6, 10, 12, 0, 0, 0, time.UTC)
	writeStreamData(t, streamDir, "attempt-001.jsonl.gz", "claude", 1, base, "completed", 60_000)

	return root
}

// TestTaskStreamNonTTYBypassesPager verifies that when stdout is not
// interactive (piped/redirected), the stream output is written directly
// to the provided writer without passing through a pager, for both the
// rendered path and the --raw path.
func TestTaskStreamNonTTYBypassesPager(t *testing.T) {
	setupStreamCmdFixture(t)
	resetTaskFlags()
	t.Cleanup(resetTaskFlags)

	// Rendered output through a buffer (pipded path) — pager must not intervene.
	var buf bytes.Buffer
	if err := runTaskStreamWith(tasks.DefaultDeps(), &buf, "demo"); err != nil {
		t.Fatalf("runTaskStreamWith: %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, "demo") {
		t.Fatalf("expected rendered output, got:\n%s", out)
	}
	if !strings.Contains(out, "claude") {
		t.Fatalf("expected agent info in output:\n%s", out)
	}
	if !strings.Contains(out, "completed") {
		t.Fatalf("expected outcome in output:\n%s", out)
	}
	if !strings.Contains(out, "hello") {
		t.Fatalf("expected event text in output:\n%s", out)
	}
}

func TestTaskStreamRawNonTTYBypassesPager(t *testing.T) {
	setupStreamCmdFixture(t)
	resetTaskFlags()
	t.Cleanup(resetTaskFlags)

	// --raw output through a buffer (pipded path) — pager must not intervene.
	taskStreamRaw = true
	var buf bytes.Buffer
	if err := runTaskStreamWith(tasks.DefaultDeps(), &buf, "demo"); err != nil {
		t.Fatalf("runTaskStreamWith (--raw): %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, `"agent":"claude"`) {
		t.Fatalf("expected raw JSONL with agent, got:\n%s", out)
	}
	if !strings.Contains(out, `"attempt":1`) {
		t.Fatalf("expected raw JSONL with attempt, got:\n%s", out)
	}
}

func TestTaskStreamTTYPipesThroughPager(t *testing.T) {
	setupStreamCmdFixture(t)
	resetTaskFlags()
	t.Cleanup(resetTaskFlags)

	origInteractive := taskStdoutInteractive
	origPager := taskOpenPager
	t.Cleanup(func() {
		taskStdoutInteractive = origInteractive
		taskOpenPager = origPager
	})

	taskStdoutInteractive = func() bool { return true }

	// Mock pager that captures output into a buffer.
	var pagerBuf bytes.Buffer
	taskOpenPager = func() (io.WriteCloser, func() error, error) {
		return &nopWriteCloser{&pagerBuf}, func() error { return nil }, nil
	}

	runTaskStream(taskStreamCmd, []string{"demo"})

	if pagerBuf.Len() == 0 {
		t.Fatal("expected output through pager, but pager buffer is empty")
	}
	if !strings.Contains(pagerBuf.String(), "hello") {
		t.Fatalf("expected event text in pager output, got:\n%s", pagerBuf.String())
	}
}

func TestTaskStreamRawTTYPipesThroughPager(t *testing.T) {
	setupStreamCmdFixture(t)
	resetTaskFlags()
	t.Cleanup(resetTaskFlags)

	origInteractive := taskStdoutInteractive
	origPager := taskOpenPager
	t.Cleanup(func() {
		taskStdoutInteractive = origInteractive
		taskOpenPager = origPager
	})

	taskStdoutInteractive = func() bool { return true }

	var pagerBuf bytes.Buffer
	taskOpenPager = func() (io.WriteCloser, func() error, error) {
		return &nopWriteCloser{&pagerBuf}, func() error { return nil }, nil
	}

	taskStreamRaw = true
	runTaskStream(taskStreamCmd, []string{"demo"})

	if pagerBuf.Len() == 0 {
		t.Fatal("expected --raw output through pager, but pager buffer is empty")
	}
	if !strings.Contains(pagerBuf.String(), `"agent":"claude"`) {
		t.Fatalf("expected raw JSONL in pager output, got:\n%s", pagerBuf.String())
	}
}

// nopWriteCloser wraps a byte buffer as an io.WriteCloser.
type nopWriteCloser struct {
	*bytes.Buffer
}

func (nopWriteCloser) Close() error { return nil }
