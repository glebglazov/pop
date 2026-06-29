package queue

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/glebglazov/pop/config"
	"github.com/glebglazov/pop/tasks"
)

func TestAbandonRefusesWhileBusy(t *testing.T) {
	repo := initGitRepoWithBase(t)
	wt := filepath.Join(t.TempDir(), "set-busy")
	runGit(t, repo, "worktree", "add", "-b", "set-busy", wt, "HEAD")

	td := queueDataDeps(t)
	key := testScopedKey(t, repo, "set-1")
	seedBindingStore(t, td, map[string]WorktreeBinding{
		key: {
			RuntimePath: wt,
			Branch:      "set-busy",
			Project:     filepath.Base(repo),
		},
	})

	d := &Deps{
		Tasks: td,
		ReadLock: func(runtimePath string) *tasks.RuntimeLockStatus {
			return liveLock(runtimePath)
		},
	}
	cfg := &config.Config{Projects: []config.ProjectEntry{{Path: repo}}}

	_, err := AbandonWithOptions(d, cfg, "set-1", io.Discard, AbandonOptions{Yes: true, In: tasks.NonInteractiveReader{}})
	if err == nil || !strings.Contains(err.Error(), "refusing unbind") {
		t.Fatalf("err = %v, want refuse while busy", err)
	}

	afterBindings := loadBindingStore(t, td)
	if len(afterBindings) != 1 {
		t.Fatalf("binding state = %+v, want retained while busy", afterBindings)
	}
	if _, err := os.Stat(wt); err != nil {
		t.Fatalf("worktree should still exist: %v", err)
	}
}

func TestAbandonSuccessfulPreservesTaskStatus(t *testing.T) {
	repo := initGitRepoWithBase(t)
	td := queueDataDeps(t)
	tasksDir := setupAbandonTaskManifest(t, repo, tasks.StatusDone)
	beforeManifest := mustReadFile(t, filepath.Join(tasksDir, "set-1", "index.json"))
	wt := filepath.Join(t.TempDir(), "set-done")
	runGit(t, repo, "worktree", "add", "-b", "set-done", wt, "HEAD")
	writeFile(t, filepath.Join(wt, "set.txt"), "set\n")
	runGit(t, wt, "add", "set.txt")
	runGit(t, wt, "commit", "-m", "set change")

	key := testScopedKey(t, repo, "set-1")
	seedBindingStore(t, td, map[string]WorktreeBinding{
		key: {
			RuntimePath: wt,
			Branch:      "set-done",
			Project:     filepath.Base(repo),
			Provisioned: true,
		},
	})

	d := &Deps{Tasks: td}
	cfg := &config.Config{Projects: []config.ProjectEntry{{Path: repo}}}

	var out bytes.Buffer
	got, err := AbandonWithOptions(d, cfg, "set-1", &out, AbandonOptions{Yes: true, In: tasks.NonInteractiveReader{}})
	if err != nil {
		t.Fatalf("abandon: %v", err)
	}
	if got.Noop {
		t.Fatalf("result = %+v, want success", got)
	}
	if _, err := os.Stat(wt); err != nil {
		t.Fatalf("worktree should be retained after unbind: %v", err)
	}
	if branch := runGitOutput(t, repo, "branch", "--list", "set-done"); strings.TrimSpace(branch) == "" {
		t.Fatalf("branch should still exist after unbind")
	}

	afterBindings := loadBindingStore(t, td)
	if len(afterBindings) != 0 {
		t.Fatalf("bindings = %+v, want cleared", afterBindings)
	}

	afterManifest := mustReadFile(t, filepath.Join(tasksDir, "set-1", "index.json"))
	if string(beforeManifest) != string(afterManifest) {
		t.Fatalf("manifest changed:\nbefore:%s\nafter:%s", beforeManifest, afterManifest)
	}

	if !strings.Contains(out.String(), "Unbound set-1") || !strings.Contains(out.String(), "retained") {
		t.Fatalf("output = %q, want clear unbind message with retained checkout", out.String())
	}
}

func TestAbandonNoopWhenUnbound(t *testing.T) {
	td := queueDataDeps(t)
	if _, err := EnsureDaemonState(td); err != nil {
		t.Fatalf("ensure state: %v", err)
	}
	d := &Deps{
		Tasks: td,
		ReadLock: func(runtimePath string) *tasks.RuntimeLockStatus {
			t.Fatalf("unbound abandon must not read runtime lock")
			return nil
		},
	}

	var out bytes.Buffer
	got, err := AbandonWithOptions(d, &config.Config{}, "set-1", &out, AbandonOptions{Yes: true, In: tasks.NonInteractiveReader{}})
	if err != nil {
		t.Fatalf("abandon: %v", err)
	}
	if !got.Noop {
		t.Fatalf("result = %+v, want noop", got)
	}
	if !strings.Contains(out.String(), "no worktree binding") {
		t.Fatalf("output = %q, want clear no-op message", out.String())
	}
}

func TestAbandonDoneSetRequiresConfirmationUnlessYes(t *testing.T) {
	repo := initGitRepoWithBase(t)
	td := queueDataDeps(t)
	wt := filepath.Join(t.TempDir(), "set-done")
	runGit(t, repo, "worktree", "add", "-b", "set-done", wt, "HEAD")

	key := testScopedKey(t, repo, "set-1")
	seedBindingStore(t, td, map[string]WorktreeBinding{
		key: {
			RuntimePath: wt,
			Branch:      "set-done",
			Project:     filepath.Base(repo),
			Provisioned: true,
		},
	})
	// A Done task status is the confirm trigger now that integration is gone
	// (ADR-0070): unbinding it would quietly forget completed work.
	d := &Deps{Tasks: td, Refresh: func(string) (*tasks.RefreshResult, error) {
		return &tasks.RefreshResult{Rows: []tasks.Row{{ID: "set-1", Status: tasks.StatusDone}}}, nil
	}}
	cfg := &config.Config{Projects: []config.ProjectEntry{{Path: repo}}}

	_, err := AbandonWithOptions(d, cfg, "set-1", io.Discard, AbandonOptions{In: tasks.NonInteractiveReader{}})
	if err == nil || !strings.Contains(err.Error(), "requires --yes") {
		t.Fatalf("err = %v, want non-interactive confirmation refusal", err)
	}

	var declined bytes.Buffer
	got, err := AbandonWithOptions(d, cfg, "set-1", &declined, AbandonOptions{In: strings.NewReader("n\n")})
	if err != nil {
		t.Fatalf("declined abandon: %v", err)
	}
	if !got.Noop || !strings.Contains(declined.String(), "cancelled") {
		t.Fatalf("declined result = %+v output = %q", got, declined.String())
	}
	if _, err := os.Stat(wt); err != nil {
		t.Fatalf("worktree should remain after decline: %v", err)
	}

	var confirmed bytes.Buffer
	got, err = AbandonWithOptions(d, cfg, "set-1", &confirmed, AbandonOptions{In: strings.NewReader("y\n")})
	if err != nil {
		t.Fatalf("confirmed abandon: %v", err)
	}
	if got.Noop {
		t.Fatalf("confirmed result = %+v, want success", got)
	}
	if len(loadBindingStore(t, td)) != 0 {
		t.Fatalf("bindings = %+v, want cleared", loadBindingStore(t, td))
	}
}

func setupAbandonTaskManifest(t *testing.T, repo string, status tasks.TaskSetStatus) string {
	t.Helper()
	tasksDir := filepath.Join(repo, "tasks")
	if err := os.MkdirAll(filepath.Join(tasksDir, "set-1"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(tasksDir, "set-1", "01-a.md"), "## Acceptance criteria\n\n- [x] done\n")
	manifest := `{
  "tasks": [
    {"id": "01-a", "file": "01-a.md", "title": "A", "type": "AFK", "status": "` + strings.ToLower(string(status)) + `"}
  ]
}`
	writeFile(t, filepath.Join(tasksDir, "set-1", "index.json"), manifest)
	if _, err := tasks.RegisterWith(tasks.DefaultDeps(), tasksDir, tasks.StatePathFor(tasksDir)); err != nil {
		t.Fatalf("refresh: %v", err)
	}
	return tasksDir
}
