package cmd

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/glebglazov/pop/internal/deps"
	"github.com/glebglazov/pop/tasks"
)

func TestWorkCommandTree(t *testing.T) {
	t.Parallel()
	for _, path := range [][]string{
		{"work", "show-path"},
		{"work", "dashboard"},
		{"work", "daemon"},
		{"work", "status"},
		{"work", "log"},
	} {
		if _, _, err := rootCmd.Find(path); err != nil {
			t.Fatalf("Find(%v): %v", path, err)
		}
	}
}

func TestWorkHelpDescribesCrossConceptSurface(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	workCmd.SetOut(&buf)
	workCmd.SetErr(&buf)
	t.Cleanup(func() {
		workCmd.SetOut(nil)
		workCmd.SetErr(nil)
	})
	if err := workCmd.Help(); err != nil {
		t.Fatal(err)
	}
	help := buf.String()
	for _, want := range []string{"Cross-concept", "Work dashboard", "show-path", "tasks/", "maps/", "pop work daemon", "Ctrl-C"} {
		if !strings.Contains(help, want) {
			t.Fatalf("work help missing %q:\n%s", want, help)
		}
	}
}

func TestWorkShowPathCreatesStorageRoot(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	dataHome := filepath.Join(root, "data")
	repoDir := filepath.Join(root, "repo")
	commonDir := filepath.Join(repoDir, ".git")
	if err := os.MkdirAll(commonDir, 0o755); err != nil {
		t.Fatal(err)
	}
	cd := newTestCmdDeps(t, repoDir, dataHome, "")
	cd.Tasks = &tasks.Deps{
		FS: cd.FS,
		Git: &deps.MockGit{
			CommandInDirFunc: func(dir string, args ...string) (string, error) {
				if len(args) >= 2 && args[0] == "rev-parse" && args[1] == "--git-common-dir" {
					return commonDir, nil
				}
				return "", nil
			},
		},
		Runner: tasks.RealCommandRunner{},
	}
	setCmdLayerDeps(t, cd)
	d := cd.tasksDeps()

	var workBuf bytes.Buffer
	if err := runWorkShowPathWith(d, &workBuf); err != nil {
		t.Fatalf("work show-path: %v", err)
	}
	storageRoot := strings.TrimSpace(workBuf.String())
	if filepath.Base(storageRoot) == "tasks" {
		t.Fatalf("work show-path printed tasks dir %q, want storage root", storageRoot)
	}

	markerPath := filepath.Join(storageRoot, "repo.json")
	if _, err := os.Stat(markerPath); err != nil {
		t.Fatalf("repo.json not created at storage root: %v", err)
	}
	tasksDir := filepath.Join(storageRoot, "tasks")
	if info, err := os.Stat(tasksDir); err != nil || !info.IsDir() {
		t.Fatalf("tasks/ not created under storage root: %v", err)
	}

	var tasksBuf bytes.Buffer
	if err := runTaskShowPathWith(d, &tasksBuf, ""); err != nil {
		t.Fatalf("tasks show-path: %v", err)
	}
	if got := strings.TrimSpace(tasksBuf.String()); got != tasksDir {
		t.Fatalf("tasks show-path = %q, want %q (= work show-path/tasks)", got, tasksDir)
	}
}

func TestWorkShowPathOutsideGitRepo(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	dataHome := filepath.Join(root, "data")
	cd := newTestCmdDeps(t, root, dataHome, "")
	cd.Tasks = &tasks.Deps{
		FS: cd.FS,
		Git: &deps.MockGit{
			CommandInDirFunc: func(dir string, args ...string) (string, error) {
				return "", errors.New("fatal: not a git repository")
			},
		},
		Runner: tasks.RealCommandRunner{},
	}
	setCmdLayerDeps(t, cd)
	d := cd.tasksDeps()

	workErr := runWorkShowPathWith(d, &bytes.Buffer{})
	tasksErr := runTaskShowPathWith(d, &bytes.Buffer{}, "")
	if workErr == nil || tasksErr == nil {
		t.Fatal("expected errors outside git repository")
	}
	var workExit, tasksExit *tasks.ExitError
	if !errors.As(workErr, &workExit) || workExit.Code == 0 {
		t.Fatalf("work show-path error = %v, want non-zero ExitError", workErr)
	}
	if !errors.As(tasksErr, &tasksExit) || tasksExit.Code == 0 {
		t.Fatalf("tasks show-path error = %v, want non-zero ExitError", tasksErr)
	}
	if workExit.Code != tasksExit.Code {
		t.Fatalf("exit codes differ: work=%d tasks=%d", workExit.Code, tasksExit.Code)
	}
}

func TestWorkDashboardUsesWorkHandler(t *testing.T) {
	t.Parallel()
	got, _, err := rootCmd.Find([]string{"work", "dashboard"})
	if err != nil {
		t.Fatalf("Find([work dashboard]): %v", err)
	}
	if got != workDashboardCmd {
		t.Fatalf("Find([work dashboard]) = %q, want work dashboard command", got.CommandPath())
	}
	if got.RunE == nil {
		t.Fatal("work dashboard missing RunE")
	}
}

// TestQueueCommandFamilyIsGone pins the hard cut: `pop queue` and every
// subcommand it carried — including the hidden `dashboard` alias — are deleted
// with no alias left behind, and the three verbs that survived live under `work`.
func TestQueueCommandFamilyIsGone(t *testing.T) {
	t.Parallel()
	for _, path := range [][]string{
		{"queue"},
		{"queue", "run"},
		{"queue", "status"},
		{"queue", "log"},
		{"queue", "dashboard"},
	} {
		got, _, _ := rootCmd.Find(path)
		if strings.HasPrefix(got.CommandPath(), "pop queue") {
			t.Fatalf("Find(%v) resolved to %q; pop queue is deleted, not aliased", path, got.CommandPath())
		}
	}
	for _, c := range rootCmd.Commands() {
		if c.Name() == "queue" || c.HasAlias("queue") {
			t.Fatalf("root still carries a %q command", "queue")
		}
	}
}

// TestWorkSubcommandsAreTheWholeSurface pins the verb set: the two read surfaces
// plus the three former Queue verbs, and no service-management verb — the daemon
// is foreground and Ctrl-C is stop, so there is nothing to start, stop or install.
func TestWorkSubcommandsAreTheWholeSurface(t *testing.T) {
	t.Parallel()
	var got []string
	for _, c := range workCmd.Commands() {
		got = append(got, c.Name())
	}
	sort.Strings(got)
	want := []string{"dashboard", "daemon", "log", "show-path", "status"}
	sort.Strings(want)
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("pop work subcommands = %v, want %v", got, want)
	}
	if workDaemonCmd.Hidden {
		t.Fatal("pop work daemon must not be hidden")
	}
}
