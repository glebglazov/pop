package queue

import (
	"bytes"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/glebglazov/pop/config"
	"github.com/glebglazov/pop/tasks"
)

func TestIntegrateCleanSetMergesAndTearsDown(t *testing.T) {
	repo := initMergeabilityRepo(t)
	wt := filepath.Join(t.TempDir(), "set-clean")
	runGit(t, repo, "worktree", "add", "-b", "set-clean", wt, "HEAD")
	writeFile(t, filepath.Join(wt, "set.txt"), "set\n")
	runGit(t, wt, "add", "set.txt")
	runGit(t, wt, "commit", "-m", "set change")

	td := queueDataDeps(t)
	state := &DaemonState{
		Version: 1,
		Mergeability: map[string]MergeabilityRecord{
			setBackoffKey(wt, "set-1"): {
				Project:     filepath.Base(repo),
				RuntimePath: wt,
				SetID:       "set-1",
				Status:      MergeabilityClean,
				CheckedAt:   time.Date(2026, 6, 14, 12, 0, 0, 0, time.UTC),
			},
		},
	}
	if err := WriteDaemonState(td, state); err != nil {
		t.Fatalf("write state: %v", err)
	}
	var lockedRuntime string
	d := &Deps{
		Tasks: td,
		AcquireRuntimeLock: func(runtimePath string) (runtimeLock, error) {
			lockedRuntime = runtimePath
			return tasks.AcquireRuntimeLock(td, runtimePath, nil)
		},
	}
	cfg := &config.Config{Projects: []config.ProjectEntry{{Path: repo}}}

	var out bytes.Buffer
	got, err := Integrate(d, cfg, "set-1", &out)
	if err != nil {
		t.Fatalf("integrate: %v", err)
	}
	if got.Noop || got.Branch != "set-clean" {
		t.Fatalf("result = %+v, want branch set-clean", got)
	}
	canonicalRepo, err := filepath.EvalSymlinks(repo)
	if err != nil {
		t.Fatalf("canonical repo: %v", err)
	}
	if lockedRuntime != canonicalRepo {
		t.Fatalf("locked runtime = %q, want working checkout %q", lockedRuntime, canonicalRepo)
	}
	if _, err := os.Stat(filepath.Join(repo, "set.txt")); err != nil {
		t.Fatalf("merged file missing from working branch: %v", err)
	}
	if _, err := os.Stat(wt); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("worktree stat err = %v, want not exist", err)
	}
	if branch := runGitOutput(t, repo, "branch", "--list", "set-clean"); strings.TrimSpace(branch) != "" {
		t.Fatalf("branch still exists: %q", branch)
	}

	after, err := ReadDaemonState(td)
	if err != nil {
		t.Fatalf("read state: %v", err)
	}
	if len(after.Mergeability) != 0 {
		t.Fatalf("mergeability state = %+v, want cleared", after.Mergeability)
	}
	snap := statusFromDecisions(nil, after)
	if len(snap.AwaitingIntegration) != 0 {
		t.Fatalf("awaiting integration = %+v, want empty", snap.AwaitingIntegration)
	}
	entries, err := ReadJournal(td)
	if err != nil {
		t.Fatalf("read journal: %v", err)
	}
	if len(entries) != 1 || entries[0].Event != JournalEventIntegrated || entries[0].SetID != "set-1" {
		t.Fatalf("journal entries = %+v, want integrated event", entries)
	}
	if !strings.Contains(out.String(), "integrated set-1") {
		t.Fatalf("output = %q, want clear integration message", out.String())
	}
}

func TestIntegrateConflictSetRefuses(t *testing.T) {
	td := queueDataDeps(t)
	state := &DaemonState{
		Version: 1,
		Mergeability: map[string]MergeabilityRecord{
			setBackoffKey("/worktree", "set-1"): {
				Project:     "pop",
				RuntimePath: "/worktree",
				SetID:       "set-1",
				Status:      MergeabilityConflicts,
			},
		},
	}
	if err := WriteDaemonState(td, state); err != nil {
		t.Fatalf("write state: %v", err)
	}
	d := &Deps{
		Tasks: td,
		AcquireRuntimeLock: func(runtimePath string) (runtimeLock, error) {
			t.Fatalf("conflict integration must not acquire runtime lock")
			return nil, nil
		},
	}

	_, err := Integrate(d, &config.Config{}, "set-1", nil)
	if err == nil {
		t.Fatal("expected conflict refusal")
	}
	if !strings.Contains(err.Error(), "has conflicts") || !strings.Contains(err.Error(), "deferred to conflict handling") {
		t.Fatalf("error = %q", err)
	}
	after, err := ReadDaemonState(td)
	if err != nil {
		t.Fatalf("read state: %v", err)
	}
	if len(after.Mergeability) != 1 {
		t.Fatalf("mergeability state = %+v, want retained", after.Mergeability)
	}
}

func TestIntegrateAlreadyIntegratedNoop(t *testing.T) {
	td := queueDataDeps(t)
	if _, err := EnsureDaemonState(td); err != nil {
		t.Fatalf("ensure state: %v", err)
	}
	d := &Deps{
		Tasks: td,
		AcquireRuntimeLock: func(runtimePath string) (runtimeLock, error) {
			t.Fatalf("no-op integration must not acquire runtime lock")
			return nil, nil
		},
	}

	var out bytes.Buffer
	got, err := Integrate(d, &config.Config{}, "set-1", &out)
	if err != nil {
		t.Fatalf("integrate: %v", err)
	}
	if !got.Noop {
		t.Fatalf("result = %+v, want noop", got)
	}
	if !strings.Contains(out.String(), "already integrated") {
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

func runGitOutput(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git -C %s %v: %v\n%s", dir, args, err, out)
	}
	return string(out)
}
