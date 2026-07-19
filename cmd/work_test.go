package cmd

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/glebglazov/pop/internal/deps"
	"github.com/glebglazov/pop/tasks"
)

func TestWorkCommandTree(t *testing.T) {
	if _, _, err := rootCmd.Find([]string{"work", "show-path"}); err != nil {
		t.Fatalf("Find([work show-path]): %v", err)
	}
}

func TestWorkHelpDescribesCrossConceptSurface(t *testing.T) {
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
	for _, want := range []string{"Cross-concept", "show-path", "tasks/", "wayfinder/"} {
		if !strings.Contains(help, want) {
			t.Fatalf("work help missing %q:\n%s", want, help)
		}
	}
}

func TestWorkShowPathCreatesStorageRoot(t *testing.T) {
	root := t.TempDir()
	dataHome := filepath.Join(root, "data")
	commonDir := filepath.Join(root, "repo", ".git")
	if err := os.MkdirAll(commonDir, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("XDG_DATA_HOME", dataHome)
	oldWd, _ := os.Getwd()
	if err := os.Chdir(filepath.Join(root, "repo")); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(oldWd) })

	d := &tasks.Deps{
		FS: deps.NewRealFileSystem(),
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
	root := t.TempDir()
	dataHome := filepath.Join(root, "data")
	t.Setenv("XDG_DATA_HOME", dataHome)
	d := &tasks.Deps{
		FS: deps.NewRealFileSystem(),
		Git: &deps.MockGit{
			CommandInDirFunc: func(dir string, args ...string) (string, error) {
				return "", errors.New("fatal: not a git repository")
			},
		},
		Runner: tasks.RealCommandRunner{},
	}

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
