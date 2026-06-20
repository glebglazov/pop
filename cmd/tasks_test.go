package cmd

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/glebglazov/pop/binding"
	"github.com/glebglazov/pop/config"
	"github.com/glebglazov/pop/internal/deps"
	"github.com/glebglazov/pop/queue"
	"github.com/glebglazov/pop/tasks"
	"github.com/glebglazov/pop/ui"
	"github.com/spf13/cobra"
)

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

	if _, err := tasks.RefreshWith(tasks.DefaultDeps(), tasksDir, tasks.DefaultStatePath()); err != nil {
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
	if !strings.Contains(out, "7 AUTO") {
		t.Fatalf("missing refreshed table with AUTO:\n%s", out)
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

	if _, err := tasks.RefreshWith(tasks.DefaultDeps(), tasksDir, tasks.StatePathFor(tasksDir)); err != nil {
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

func TestTaskArchiveSelectionPrechecksDoneOnlyAndCancelWritesNothing(t *testing.T) {
	root := t.TempDir()
	resetTaskFlags()
	t.Cleanup(resetTaskFlags)

	initGitRepoCmd(t, root)
	t.Setenv("XDG_DATA_HOME", filepath.Join(root, ".xdg"))
	tasksDir := cmdTasksDir(t, root)
	writeTaskThoughtsWithStatus(t, tasksDir, "done", "done")
	writeTaskThoughtsWithStatus(t, tasksDir, "ready", "open")
	if _, err := tasks.RefreshWith(tasks.DefaultDeps(), tasksDir, tasks.StatePathFor(tasksDir)); err != nil {
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
	before, err := os.ReadFile(tasks.StatePathFor(tasksDir))
	if err != nil {
		t.Fatal(err)
	}

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
	after, err := os.ReadFile(tasks.StatePathFor(tasksDir))
	if err != nil {
		t.Fatal(err)
	}
	if string(before) != string(after) {
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
	if _, err := tasks.RefreshWith(tasks.DefaultDeps(), tasksDir, tasks.StatePathFor(tasksDir)); err != nil {
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
	if _, err := tasks.RefreshWith(tasks.DefaultDeps(), tasksDir, tasks.StatePathFor(tasksDir)); err != nil {
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
	before, err := os.ReadFile(tasks.StatePathFor(tasksDir))
	if err != nil {
		t.Fatal(err)
	}

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
	after, err := os.ReadFile(tasks.StatePathFor(tasksDir))
	if err != nil {
		t.Fatal(err)
	}
	if string(before) != string(after) {
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
	if _, err := tasks.RefreshWith(tasks.DefaultDeps(), tasksDir, tasks.StatePathFor(tasksDir)); err != nil {
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
	if _, err := tasks.RefreshWith(tasks.DefaultDeps(), tasksDir, tasks.StatePathFor(tasksDir)); err != nil {
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
	if _, err := tasks.RefreshWith(tasks.DefaultDeps(), tasksDir, tasks.StatePathFor(tasksDir)); err != nil {
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

func TestTaskArchiveNoArgNonInteractiveRejected(t *testing.T) {
	root := t.TempDir()
	resetTaskFlags()
	t.Cleanup(resetTaskFlags)

	initGitRepoCmd(t, root)
	t.Setenv("XDG_DATA_HOME", filepath.Join(root, ".xdg"))
	tasksDir := cmdTasksDir(t, root)
	writeTaskThoughtsWithStatus(t, tasksDir, "done", "done")
	if _, err := tasks.RefreshWith(tasks.DefaultDeps(), tasksDir, tasks.StatePathFor(tasksDir)); err != nil {
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
	if _, err := tasks.RefreshWith(tasks.DefaultDeps(), tasksDir, tasks.StatePathFor(tasksDir)); err != nil {
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
	if _, err := tasks.RefreshWith(tasks.DefaultDeps(), tasksDir, tasks.StatePathFor(tasksDir)); err != nil {
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
	if _, err := tasks.RefreshWith(tasks.DefaultDeps(), tasksDir, tasks.StatePathFor(tasksDir)); err != nil {
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
		if err := runTaskExportWith(tasks.DefaultDeps(), &buf, "demo"); err != nil {
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
	if _, err := tasks.RefreshWith(tasks.DefaultDeps(), tasksDir, tasks.DefaultStatePath()); err != nil {
		t.Fatal(err)
	}

	d := tasks.DefaultDeps()
	runtimePath, err := tasks.ResolveRuntimePathWith(d, root, "")
	if err != nil {
		t.Fatal(err)
	}
	lock, err := tasks.AcquireRuntimeLock(d, runtimePath, io.Discard)
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

func TestTaskStatusSetArgDrillsIn(t *testing.T) {
	root := t.TempDir()
	initGitRepoCmd(t, root)
	t.Setenv("XDG_DATA_HOME", filepath.Join(root, ".xdg"))
	tasksDir := cmdTasksDir(t, root)
	writeTaskThoughts(t, tasksDir, "alpha")
	writeTaskThoughts(t, tasksDir, "beta")
	if _, err := tasks.RefreshWith(tasks.DefaultDeps(), tasksDir, tasks.DefaultStatePath()); err != nil {
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
	if _, err := tasks.RefreshWith(tasks.DefaultDeps(), tasksDir, tasks.DefaultStatePath()); err != nil {
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
	if !strings.Contains(stdout.String(), "AUTO RUN") {
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
	if !strings.Contains(stdout.String(), "AUTO RUN") {
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
	taskStatusArchived = false
	taskAgentPreset = ""
	taskAgentPresets = nil
	taskAgentCmd = ""
	taskAgentOutput = ""
	taskRunYes = false
	taskAllowDirty = tasks.DirtyRuntimeContinue
	taskExportOutput = ""
	taskImportAs = ""
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
	if _, err := tasks.RefreshWith(tasks.DefaultDeps(), tasksDir, tasks.DefaultStatePath()); err != nil {
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

	err := runTaskRunTasksWith(tasks.DefaultDeps(), &bytes.Buffer{}, io.Discard, tasks.NonInteractiveReader{}, "demo", false)
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

// TestImplementAgentFlagExplicitness pins the contract behind per-task agent
// resolution (ADR-0018): a bare defaulted --agent does not report Changed, so
// only an explicitly passed flag overrides a task's `agent` key.
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
	if err := runTaskExportWith(d, &exportBuf, setID); err != nil {
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

// offerIntegrationTestDataDeps returns task Deps whose data dir is redirected
// to a fresh temp dir so daemon state reads and writes stay within the test
// sandbox. The XDG_DATA_HOME is set on the test's environment so that
// offerImplementIntegration (which calls queue.DefaultDeps()) picks up the
// same directory through the env var.
func offerIntegrationTestDataDeps(t *testing.T) *tasks.Deps {
	t.Helper()
	xdg := t.TempDir()
	t.Setenv("XDG_DATA_HOME", xdg)
	real := deps.NewRealFileSystem()
	d := tasks.DefaultDeps()
	d.FS = &deps.MockFileSystem{
		GetenvFunc: func(key string) string {
			if key == "XDG_DATA_HOME" {
				return xdg
			}
			return ""
		},
		ReadFileFunc:  real.ReadFile,
		WriteFileFunc: real.WriteFile,
		MkdirAllFunc:  real.MkdirAll,
		RenameFunc:    real.Rename,
		RemoveAllFunc: real.RemoveAll,
	}
	return d
}

// offerIntegrationRunGit runs a git command in dir, failing the test on error.
func offerIntegrationRunGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git -C %s %v: %v\n%s", dir, args, err, out)
	}
}

// setupOfferIntegrationWorktree creates a bare-enough git repo with one linked
// worktree and writes a mergeability record into the daemon state. Returns the
// repo path (main working tree), worktree path, and the set ID so callers can
// drive offerImplementIntegration.
func setupOfferIntegrationWorktree(t *testing.T, td *tasks.Deps, status string) (repo, wt, setID string) {
	t.Helper()
	repo = t.TempDir()
	offerIntegrationRunGit(t, repo, "init")
	offerIntegrationRunGit(t, repo, "config", "user.email", "pop@example.test")
	offerIntegrationRunGit(t, repo, "config", "user.name", "Pop Test")
	if err := os.WriteFile(filepath.Join(repo, "base.txt"), []byte("base\n"), 0o644); err != nil {
		t.Fatalf("write base: %v", err)
	}
	offerIntegrationRunGit(t, repo, "add", "base.txt")
	offerIntegrationRunGit(t, repo, "commit", "-m", "base")

	setID = "test-set-1"
	wt = filepath.Join(t.TempDir(), "wt-feature")
	offerIntegrationRunGit(t, repo, "worktree", "add", "-b", "feature", wt, "HEAD")

	if err := os.WriteFile(filepath.Join(wt, "feature.txt"), []byte("feature\n"), 0o644); err != nil {
		t.Fatalf("write feature: %v", err)
	}
	offerIntegrationRunGit(t, wt, "add", "feature.txt")
	offerIntegrationRunGit(t, wt, "commit", "-m", "feature work")

	// Write the binding so IntegrateWithOptions can find the working path.
	bstore, _ := binding.Load(td)
	id, err := tasks.ResolveRepositoryIdentity(td, repo)
	if err != nil {
		t.Fatalf("repo identity: %v", err)
	}
	key := binding.Key(id, setID)
	bstore.Put(key, binding.Adopt(wt, "feature", ""))
	if err := binding.Save(td, bstore); err != nil {
		t.Fatalf("save binding: %v", err)
	}

	// Write a mergeability record via the daemon state.
	state := &queue.DaemonState{
		Version: 1,
		Mergeability: map[string]queue.MergeabilityRecord{
			key: {
				RuntimePath: wt,
				SetID:       setID,
				Status:      status,
			},
		},
		WorktreeBindings: map[string]queue.WorktreeBinding{
			key: {RuntimePath: wt, Branch: "feature", Provisioned: false},
		},
	}
	if err := queue.WriteDaemonState(td, state); err != nil {
		t.Fatalf("write state: %v", err)
	}
	return repo, wt, setID
}

// TestOfferImplementIntegrationNonInteractiveSkips verifies the offer is
// suppressed when stdin is not a TTY.
func TestOfferImplementIntegrationNonInteractiveSkips(t *testing.T) {
	resetTaskFlags()
	t.Cleanup(resetTaskFlags)

	orig := taskStdinInteractive
	taskStdinInteractive = func(io.Reader) bool { return false }
	t.Cleanup(func() { taskStdinInteractive = orig })

	result := &tasks.RunTaskSetResult{TaskSetID: "demo", TaskSetDone: true, RuntimePath: "/some/wt"}
	var out bytes.Buffer
	offerImplementIntegration(tasks.DefaultDeps(), result, strings.NewReader("y\n"), &out)
	if out.Len() != 0 {
		t.Fatalf("expected no output for non-interactive stdin, got: %q", out.String())
	}
}

// TestOfferImplementIntegrationYesFlagTrunkDrainSkips verifies that --yes
// with no mergeability record (trunk drain) produces no output and no
// integration (ADR-0036).
func TestOfferImplementIntegrationYesFlagTrunkDrainSkips(t *testing.T) {
	resetTaskFlags()
	taskRunYes = true
	t.Cleanup(resetTaskFlags)

	orig := taskStdinInteractive
	taskStdinInteractive = func(io.Reader) bool { return true }
	t.Cleanup(func() { taskStdinInteractive = orig })

	result := &tasks.RunTaskSetResult{TaskSetID: "demo", TaskSetDone: true, RuntimePath: "/some/wt"}
	var out bytes.Buffer
	offerImplementIntegration(tasks.DefaultDeps(), result, strings.NewReader("y\n"), &out)
	if out.Len() != 0 {
		t.Fatalf("expected no output when --yes is set on trunk drain, got: %q", out.String())
	}
}

// TestOfferImplementIntegrationYesNoAutoMergeCleanCleanParks verifies that
// --yes with a clean mergeability but no auto_merge_clean opt-in parks the
// set in the Integration backlog without integrating (ADR-0036).
func TestOfferImplementIntegrationYesNoAutoMergeCleanCleanParks(t *testing.T) {
	td := offerIntegrationTestDataDeps(t)
	repo, wt, setID := setupOfferIntegrationWorktree(t, td, queue.MergeabilityClean)
	resetTaskFlags()
	taskRunYes = true
	t.Cleanup(resetTaskFlags)

	orig := taskStdinInteractive
	taskStdinInteractive = func(io.Reader) bool { return true }
	t.Cleanup(func() { taskStdinInteractive = orig })

	result := &tasks.RunTaskSetResult{
		TaskSetID:   setID,
		TaskSetDone: true,
		RuntimePath: wt,
		ProjectPath: repo,
	}
	var out bytes.Buffer
	offerImplementIntegration(td, result, strings.NewReader(""), &out)

	if out.Len() != 0 {
		t.Fatalf("expected no output (parked), got: %q", out.String())
	}
	// Mergeability record must remain in state (not integrated).
	qd := queue.DefaultDeps()
	qd.Tasks = td
	_, ok, err := queue.LookupMergeability(qd, setID)
	if err != nil {
		t.Fatalf("lookup mergeability: %v", err)
	}
	if !ok {
		t.Fatal("mergeability record cleared: set must remain in backlog when auto_merge_clean is not set")
	}
}

// TestOfferImplementIntegrationYesNoAutoMergeCleanConflictsParks verifies
// that --yes with conflicts and no auto_merge_clean parks in the backlog.
func TestOfferImplementIntegrationYesNoAutoMergeCleanConflictsParks(t *testing.T) {
	td := offerIntegrationTestDataDeps(t)
	repo, wt, setID := setupOfferIntegrationWorktree(t, td, queue.MergeabilityConflicts)
	resetTaskFlags()
	taskRunYes = true
	t.Cleanup(resetTaskFlags)

	orig := taskStdinInteractive
	taskStdinInteractive = func(io.Reader) bool { return true }
	t.Cleanup(func() { taskStdinInteractive = orig })

	result := &tasks.RunTaskSetResult{
		TaskSetID:   setID,
		TaskSetDone: true,
		RuntimePath: wt,
		ProjectPath: repo,
	}
	var out bytes.Buffer
	offerImplementIntegration(td, result, strings.NewReader(""), &out)

	if out.Len() != 0 {
		t.Fatalf("expected no output (parked), got: %q", out.String())
	}
	qd := queue.DefaultDeps()
	qd.Tasks = td
	_, ok, err := queue.LookupMergeability(qd, setID)
	if err != nil {
		t.Fatalf("lookup mergeability: %v", err)
	}
	if !ok {
		t.Fatal("mergeability record cleared: conflicting set must remain in backlog")
	}
}

// TestOfferImplementIntegrationYesAutoMergeCleanCleanIntegrates verifies that
// --yes with a clean set and auto_merge_clean=true integrates automatically
// without prompting (ADR-0036).
func TestOfferImplementIntegrationYesAutoMergeCleanCleanIntegrates(t *testing.T) {
	td := offerIntegrationTestDataDeps(t)
	repo, wt, setID := setupOfferIntegrationWorktree(t, td, queue.MergeabilityClean)
	resetTaskFlags()
	taskRunYes = true
	t.Cleanup(resetTaskFlags)

	if err := os.WriteFile(filepath.Join(repo, ".pop.toml"), []byte("auto_merge_clean = true\n"), 0o644); err != nil {
		t.Fatalf("write .pop.toml: %v", err)
	}

	origLoad := taskConfigLoad
	taskConfigLoad = func(_ string) (*config.Config, error) {
		return &config.Config{Projects: []config.ProjectEntry{{Path: repo}}}, nil
	}
	t.Cleanup(func() { taskConfigLoad = origLoad })

	orig := taskStdinInteractive
	taskStdinInteractive = func(io.Reader) bool { return true }
	t.Cleanup(func() { taskStdinInteractive = orig })

	result := &tasks.RunTaskSetResult{
		TaskSetID:   setID,
		TaskSetDone: true,
		RuntimePath: wt,
		ProjectPath: repo,
	}
	var out bytes.Buffer
	offerImplementIntegration(td, result, strings.NewReader(""), &out)

	// Integration: feature.txt must appear in the main repo.
	if _, err := os.Stat(filepath.Join(repo, "feature.txt")); err != nil {
		t.Fatalf("feature.txt missing from main repo after auto-integration: %v", err)
	}
	// Mergeability record must be cleared from state.
	qd := queue.DefaultDeps()
	qd.Tasks = td
	_, ok, err := queue.LookupMergeability(qd, setID)
	if err != nil {
		t.Fatalf("lookup mergeability: %v", err)
	}
	if ok {
		t.Fatal("mergeability record not cleared: set must be removed from backlog after auto-integration")
	}
}

// TestOfferImplementIntegrationYesAutoMergeCleanConflictsParks verifies that
// --yes with auto_merge_clean=true still parks conflicting sets in the backlog
// (ADR-0036: conflicts never auto-integrate regardless of flag).
func TestOfferImplementIntegrationYesAutoMergeCleanConflictsParks(t *testing.T) {
	td := offerIntegrationTestDataDeps(t)
	repo, wt, setID := setupOfferIntegrationWorktree(t, td, queue.MergeabilityConflicts)
	resetTaskFlags()
	taskRunYes = true
	t.Cleanup(resetTaskFlags)

	if err := os.WriteFile(filepath.Join(repo, ".pop.toml"), []byte("auto_merge_clean = true\n"), 0o644); err != nil {
		t.Fatalf("write .pop.toml: %v", err)
	}

	origLoad := taskConfigLoad
	taskConfigLoad = func(_ string) (*config.Config, error) {
		return &config.Config{Projects: []config.ProjectEntry{{Path: repo}}}, nil
	}
	t.Cleanup(func() { taskConfigLoad = origLoad })

	orig := taskStdinInteractive
	taskStdinInteractive = func(io.Reader) bool { return true }
	t.Cleanup(func() { taskStdinInteractive = orig })

	result := &tasks.RunTaskSetResult{
		TaskSetID:   setID,
		TaskSetDone: true,
		RuntimePath: wt,
		ProjectPath: repo,
	}
	var out bytes.Buffer
	offerImplementIntegration(td, result, strings.NewReader(""), &out)

	if out.Len() != 0 {
		t.Fatalf("expected no output for conflicting set, got: %q", out.String())
	}
	qd := queue.DefaultDeps()
	qd.Tasks = td
	rec, ok, err := queue.LookupMergeability(qd, setID)
	if err != nil {
		t.Fatalf("lookup mergeability: %v", err)
	}
	if !ok {
		t.Fatal("mergeability record cleared: conflicting set must remain in backlog even with auto_merge_clean")
	}
	if rec.Status != queue.MergeabilityConflicts {
		t.Fatalf("mergeability status = %q, want conflicts", rec.Status)
	}
}

// TestOfferImplementIntegrationTrunkDrainNoOffer verifies trunk drains (no
// mergeability record) produce no integration prompt.
func TestOfferImplementIntegrationTrunkDrainNoOffer(t *testing.T) {
	td := offerIntegrationTestDataDeps(t)
	resetTaskFlags()
	t.Cleanup(resetTaskFlags)

	orig := taskStdinInteractive
	taskStdinInteractive = func(io.Reader) bool { return true }
	t.Cleanup(func() { taskStdinInteractive = orig })

	repo := t.TempDir()
	offerIntegrationRunGit(t, repo, "init")
	offerIntegrationRunGit(t, repo, "config", "user.email", "pop@example.test")
	offerIntegrationRunGit(t, repo, "config", "user.name", "Pop Test")
	offerIntegrationRunGit(t, repo, "commit", "--allow-empty", "-m", "base")

	// No mergeability record written: trunk drain.
	result := &tasks.RunTaskSetResult{
		TaskSetID:   "demo",
		TaskSetDone: true,
		RuntimePath: repo,
	}
	var out bytes.Buffer
	offerImplementIntegration(td, result, strings.NewReader("y\n"), &out)
	if out.Len() != 0 {
		t.Fatalf("trunk drain must produce no integration offer, got: %q", out.String())
	}
}

// TestOfferImplementIntegrationCleanPromptDeclined verifies a clean merge
// offer that the user declines does not trigger integration.
func TestOfferImplementIntegrationCleanPromptDeclined(t *testing.T) {
	td := offerIntegrationTestDataDeps(t)
	repo, wt, setID := setupOfferIntegrationWorktree(t, td, queue.MergeabilityClean)
	resetTaskFlags()
	t.Cleanup(resetTaskFlags)

	orig := taskStdinInteractive
	taskStdinInteractive = func(io.Reader) bool { return true }
	t.Cleanup(func() { taskStdinInteractive = orig })

	result := &tasks.RunTaskSetResult{
		TaskSetID:   setID,
		TaskSetDone: true,
		RuntimePath: wt,
		ProjectPath: repo,
	}
	var out bytes.Buffer
	offerImplementIntegration(td, result, strings.NewReader("n\n"), &out)

	outStr := out.String()
	if !strings.Contains(outStr, "Integrate") {
		t.Fatalf("expected Integrate prompt, got: %q", outStr)
	}
	if !strings.Contains(outStr, "merges clean") {
		t.Fatalf("expected 'merges clean' in prompt, got: %q", outStr)
	}
	// Declined: worktree should still exist.
	if _, err := os.Stat(wt); err != nil {
		t.Fatalf("worktree should not be removed on decline: %v", err)
	}
}

// TestOfferImplementIntegrationCleanPromptShowsBranch verifies the working
// branch name appears in the integration offer.
func TestOfferImplementIntegrationCleanPromptShowsBranch(t *testing.T) {
	td := offerIntegrationTestDataDeps(t)
	repo, wt, setID := setupOfferIntegrationWorktree(t, td, queue.MergeabilityClean)
	resetTaskFlags()
	t.Cleanup(resetTaskFlags)

	orig := taskStdinInteractive
	taskStdinInteractive = func(io.Reader) bool { return true }
	t.Cleanup(func() { taskStdinInteractive = orig })

	result := &tasks.RunTaskSetResult{
		TaskSetID:   setID,
		TaskSetDone: true,
		RuntimePath: wt,
		ProjectPath: repo,
	}
	var out bytes.Buffer
	offerImplementIntegration(td, result, strings.NewReader("n\n"), &out)

	outStr := out.String()
	// The main worktree's branch (master or main depending on git config) should appear.
	if !strings.Contains(outStr, "Integrate "+setID+" into ") {
		t.Fatalf("expected branch name in integrate offer, got: %q", outStr)
	}
}

// TestOfferImplementIntegrationConflictsInPrompt verifies a conflicting merge
// shows "has conflicts" in the integration offer.
func TestOfferImplementIntegrationConflictsInPrompt(t *testing.T) {
	td := offerIntegrationTestDataDeps(t)
	repo, wt, setID := setupOfferIntegrationWorktree(t, td, queue.MergeabilityConflicts)
	resetTaskFlags()
	t.Cleanup(resetTaskFlags)

	orig := taskStdinInteractive
	taskStdinInteractive = func(io.Reader) bool { return true }
	t.Cleanup(func() { taskStdinInteractive = orig })

	result := &tasks.RunTaskSetResult{
		TaskSetID:   setID,
		TaskSetDone: true,
		RuntimePath: wt,
		ProjectPath: repo,
	}
	var out bytes.Buffer
	offerImplementIntegration(td, result, strings.NewReader("n\n"), &out)

	outStr := out.String()
	if !strings.Contains(outStr, "has conflicts") {
		t.Fatalf("expected 'has conflicts' in prompt for conflicting merge, got: %q", outStr)
	}
}

// TestOfferImplementIntegrationNotDoneSkips verifies the offer is not shown
// when the set did not reach Done (e.g. stopped mid-drain).
func TestOfferImplementIntegrationNotDoneSkips(t *testing.T) {
	resetTaskFlags()
	t.Cleanup(resetTaskFlags)

	orig := taskStdinInteractive
	taskStdinInteractive = func(io.Reader) bool { return true }
	t.Cleanup(func() { taskStdinInteractive = orig })

	result := &tasks.RunTaskSetResult{
		TaskSetID:   "demo",
		TaskSetDone: false,
		RuntimePath: "/some/wt",
	}
	var out bytes.Buffer
	offerImplementIntegration(tasks.DefaultDeps(), result, strings.NewReader("y\n"), &out)
	if out.Len() != 0 {
		t.Fatalf("expected no output when set not Done, got: %q", out.String())
	}
}
