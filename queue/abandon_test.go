package queue

import (
	"bytes"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/glebglazov/pop/config"
	"github.com/glebglazov/pop/tasks"
)

func TestAbandonRefusesWhileBusy(t *testing.T) {
	repo := initMergeabilityRepo(t)
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
	repo := initMergeabilityRepo(t)
	tasksDir := setupAbandonTaskManifest(t, repo, tasks.StatusDone)
	beforeManifest := mustReadFile(t, filepath.Join(tasksDir, "set-1", "index.json"))
	wt := filepath.Join(t.TempDir(), "set-done")
	runGit(t, repo, "worktree", "add", "-b", "set-done", wt, "HEAD")
	writeFile(t, filepath.Join(wt, "set.txt"), "set\n")
	runGit(t, wt, "add", "set.txt")
	runGit(t, wt, "commit", "-m", "set change")

	td := queueDataDeps(t)
	key := testScopedKey(t, repo, "set-1")
	seedBindingStore(t, td, map[string]WorktreeBinding{
		key: {
			RuntimePath: wt,
			Branch:      "set-done",
			Project:     filepath.Base(repo),
			Provisioned: true,
		},
	})
	seedMergeabilityStore(t, td, map[string]MergeabilityRecord{
		key: {
			Project:     filepath.Base(repo),
			RuntimePath: wt,
			SetID:       "set-1",
			Status:      MergeabilityClean,
			CheckedAt:   time.Date(2026, 6, 14, 12, 0, 0, 0, time.UTC),
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
	if _, err := os.Stat(wt); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("worktree stat err = %v, want not exist", err)
	}
	if branch := runGitOutput(t, repo, "branch", "--list", "set-done"); strings.TrimSpace(branch) != "" {
		t.Fatalf("branch still exists: %q", branch)
	}

	afterBindings := loadBindingStore(t, td)
	if len(afterBindings) != 0 {
		t.Fatalf("bindings = %+v, want cleared", afterBindings)
	}
	if len(loadMergeabilityStore(t, td)) != 0 {
		t.Fatalf("mergeability = %+v, want cleared", loadMergeabilityStore(t, td))
	}

	afterManifest := mustReadFile(t, filepath.Join(tasksDir, "set-1", "index.json"))
	if string(beforeManifest) != string(afterManifest) {
		t.Fatalf("manifest changed:\nbefore:%s\nafter:%s", beforeManifest, afterManifest)
	}

	entries, err := ReadJournal(td)
	if err != nil {
		t.Fatalf("read journal: %v", err)
	}
	if len(entries) != 1 || entries[0].Event != JournalEventAbandoned || entries[0].SetID != "set-1" {
		t.Fatalf("journal entries = %+v, want abandoned event", entries)
	}
	if !strings.Contains(out.String(), "Unbound set-1") {
		t.Fatalf("output = %q, want clear unbind message", out.String())
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
	entries, err := ReadJournal(td)
	if err != nil {
		t.Fatalf("read journal: %v", err)
	}
	if len(entries) != 0 {
		t.Fatalf("journal entries = %+v, want none for no-op", entries)
	}
}

func TestAbandonDoneSetRequiresConfirmationUnlessYes(t *testing.T) {
	repo := initMergeabilityRepo(t)
	wt := filepath.Join(t.TempDir(), "set-done")
	runGit(t, repo, "worktree", "add", "-b", "set-done", wt, "HEAD")

	td := queueDataDeps(t)
	key := testScopedKey(t, repo, "set-1")
	seedBindingStore(t, td, map[string]WorktreeBinding{
		key: {
			RuntimePath: wt,
			Branch:      "set-done",
			Project:     filepath.Base(repo),
			Provisioned: true,
		},
	})
	seedMergeabilityStore(t, td, map[string]MergeabilityRecord{
		key: {
			Project:     filepath.Base(repo),
			RuntimePath: wt,
			SetID:       "set-1",
			Status:      MergeabilityClean,
			CheckedAt:   time.Date(2026, 6, 14, 12, 0, 0, 0, time.UTC),
		},
	})
	d := &Deps{Tasks: td}
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
	if len(loadMergeabilityStore(t, td)) != 0 || len(loadBindingStore(t, td)) != 0 {
		t.Fatalf("mergeability = %+v bindings = %+v, want cleared", loadMergeabilityStore(t, td), loadBindingStore(t, td))
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
	if _, err := tasks.RefreshWith(tasks.DefaultDeps(), tasksDir, tasks.StatePathFor(tasksDir)); err != nil {
		t.Fatalf("refresh: %v", err)
	}
	return tasksDir
}
