package queue

import (
	"bytes"
	"context"
	"errors"
	"io"
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
	key := testScopedKey(t, repo, "set-1")
	seedMergeabilityStore(t, td, map[string]MergeabilityRecord{
			key: {
				Project:     filepath.Base(repo),
				RuntimePath: wt,
				SetID:       "set-1",
				Status:      MergeabilityClean,
				CheckedAt:   time.Date(2026, 6, 14, 12, 0, 0, 0, time.UTC),
			},
		})
	seedBindingStore(t, td, map[string]WorktreeBinding{
		key: integrationWorktreeBinding(t, repo, wt, "set-clean"),
	})
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

	if len(loadMergeabilityStore(t, td)) != 0 {
		t.Fatalf("mergeability state = %+v, want cleared", loadMergeabilityStore(t, td))
	}
	if len(loadBindingStore(t, td)) != 0 {
		t.Fatalf("worktree bindings = %+v, want cleared", loadBindingStore(t, td))
	}
	snap, err := statusFromDecisions(&Deps{Tasks: td}, nil, &DaemonState{Version: 1})
	if err != nil {
		t.Fatalf("status: %v", err)
	}
	if len(snap.AwaitingIntegration) != 0 {
		t.Fatalf("awaiting integration = %+v, want empty", snap.AwaitingIntegration)
	}
	events, err := tasks.IntegrationEventsForSet(td, "set-1")
	if err != nil {
		t.Fatalf("read integration events: %v", err)
	}
	if len(events) != 1 || events[0].SetID != "set-1" {
		t.Fatalf("integration events = %+v, want one for set-1", events)
	}
	if !strings.Contains(out.String(), "integrated set-1") {
		t.Fatalf("output = %q, want clear integration message", out.String())
	}
}

func TestIntegrateConflictSetRefuses(t *testing.T) {
	repo, wt, rec := setupConflictingIntegration(t)
	td := queueDataDeps(t)
	key := testScopedKey(t, repo, "set-1")
	seedMergeabilityStore(t, td, map[string]MergeabilityRecord{
			key: rec,
		})
	seedBindingStore(t, td, map[string]WorktreeBinding{
		key: integrationWorktreeBinding(t, repo, wt, "set-conflict"),
	})
	d := &Deps{
		Tasks: td,
		AcquireRuntimeLock: func(runtimePath string) (runtimeLock, error) {
			t.Fatalf("conflict integration must not acquire runtime lock")
			return nil, nil
		},
	}

	var out bytes.Buffer
	got, err := Integrate(d, &config.Config{Projects: []config.ProjectEntry{{Path: repo}}}, "set-1", &out)
	if err != nil {
		t.Fatalf("integrate: %v", err)
	}
	if !got.Kept || got.Outcome != "declined" {
		t.Fatalf("result = %+v, want declined kept conflict", got)
	}
	if !strings.Contains(out.String(), "has merge conflicts") || !strings.Contains(out.String(), "Get agent assistance") {
		t.Fatalf("output = %q, want surfaced conflict with assistance offer", out.String())
	}
	if len(loadMergeabilityStore(t, td)) != 1 {
		t.Fatalf("mergeability state = %+v, want retained", loadMergeabilityStore(t, td))
	}
	if len(loadBindingStore(t, td)) != 1 {
		t.Fatalf("worktree bindings = %+v, want retained", loadBindingStore(t, td))
	}
}

func TestIntegrateConflictDeclinedKeepsWorktreeBranchAndState(t *testing.T) {
	repo, wt, rec := setupConflictingIntegration(t)
	td := queueDataDeps(t)
	key := testScopedKey(t, repo, "set-1")
	seedMergeabilityStore(t, td, map[string]MergeabilityRecord{key: rec})
	seedBindingStore(t, td, map[string]WorktreeBinding{
		key: integrationWorktreeBinding(t, repo, wt, "set-conflict"),
	})
	d := &Deps{
		Tasks: td,
		AcquireRuntimeLock: func(runtimePath string) (runtimeLock, error) {
			t.Fatalf("declined conflict must not acquire runtime lock")
			return nil, nil
		},
	}

	var out bytes.Buffer
	got, err := IntegrateWithOptions(d, &config.Config{Projects: []config.ProjectEntry{{Path: repo}}}, "set-1", &out, IntegrationOptions{In: strings.NewReader("2\n")})
	if err != nil {
		t.Fatalf("integrate: %v", err)
	}
	if !got.Kept || got.Outcome != "declined" {
		t.Fatalf("result = %+v, want declined kept", got)
	}
	if _, err := os.Stat(wt); err != nil {
		t.Fatalf("worktree should be kept: %v", err)
	}
	if branch := strings.TrimSpace(runGitOutput(t, repo, "branch", "--list", "set-conflict")); branch == "" {
		t.Fatal("set branch should be kept")
	}
	if len(loadMergeabilityStore(t, td)) != 1 {
		t.Fatalf("mergeability state = %+v, want retained", loadMergeabilityStore(t, td))
	}
	if len(loadBindingStore(t, td)) != 1 {
		t.Fatalf("worktree bindings = %+v, want retained", loadBindingStore(t, td))
	}
	// A declined conflict never integrates, so no durable integration event lands.
	events, err := tasks.IntegrationEventsForSet(td, "set-1")
	if err != nil {
		t.Fatalf("read integration events: %v", err)
	}
	if len(events) != 0 {
		t.Fatalf("integration events = %+v, want none for declined conflict", events)
	}
}

func TestIntegrateConflictUnresolvedKeepsWorktreeBinding(t *testing.T) {
	repo, wt, rec := setupConflictingIntegration(t)
	td := queueDataDeps(t)
	td.Runner = &noopConflictRunner{}
	key := testScopedKey(t, repo, "set-1")
	seedMergeabilityStore(t, td, map[string]MergeabilityRecord{key: rec})
	seedBindingStore(t, td, map[string]WorktreeBinding{
		key: integrationWorktreeBinding(t, repo, wt, "set-conflict"),
	})
	d := &Deps{
		Tasks: td,
		AcquireRuntimeLock: func(runtimePath string) (runtimeLock, error) {
			return tasks.AcquireRuntimeLock(td, runtimePath, nil)
		},
	}

	var out bytes.Buffer
	got, err := IntegrateWithOptions(d, &config.Config{Projects: []config.ProjectEntry{{Path: repo}}}, "set-1", &out, IntegrationOptions{In: strings.NewReader("\n"), AgentPreset: "claude"})
	if err != nil {
		t.Fatalf("integrate: %v", err)
	}
	if !got.Kept || got.Outcome != "unresolved" {
		t.Fatalf("result = %+v, want unresolved kept conflict", got)
	}
	if _, err := os.Stat(wt); err != nil {
		t.Fatalf("worktree should be kept: %v", err)
	}
	if len(loadMergeabilityStore(t, td)) != 1 {
		t.Fatalf("mergeability state = %+v, want retained", loadMergeabilityStore(t, td))
	}
	if len(loadBindingStore(t, td)) != 1 {
		t.Fatalf("worktree bindings = %+v, want retained", loadBindingStore(t, td))
	}
}

func TestIntegrateConflictUsesConfigDefaultAgent(t *testing.T) {
	repo, wt, rec := setupConflictingIntegration(t)
	td := queueDataDeps(t)
	runner := &conflictResolutionRunner{t: t, resolvedText: "resolved by agent\n"}
	td.Runner = runner
	key := testScopedKey(t, repo, "set-1")
	seedMergeabilityStore(t, td, map[string]MergeabilityRecord{key: rec})
	seedBindingStore(t, td, map[string]WorktreeBinding{
		key: integrationWorktreeBinding(t, repo, wt, "set-conflict"),
	})
	d := &Deps{
		Tasks: td,
		AcquireRuntimeLock: func(runtimePath string) (runtimeLock, error) {
			return tasks.AcquireRuntimeLock(td, runtimePath, nil)
		},
	}
	cfg := &config.Config{
		Projects: []config.ProjectEntry{{Path: repo}},
		Task:     &config.TaskConfig{DefaultAgents: []string{"codex", "claude"}},
	}

	var out bytes.Buffer
	got, err := IntegrateWithOptions(d, cfg, "set-1", &out, IntegrationOptions{In: strings.NewReader("\n")})
	if err != nil {
		t.Fatalf("integrate: %v", err)
	}
	if got.Outcome != "resolved" {
		t.Fatalf("result = %+v, want resolved integration", got)
	}
	if runner.name != "codex" {
		t.Fatalf("agent = %q, want codex from default_agents[0]", runner.name)
	}
}

func TestIntegrateConflictClaudeFallbackWhenNoConfigAgents(t *testing.T) {
	repo, wt, rec := setupConflictingIntegration(t)
	td := queueDataDeps(t)
	runner := &conflictResolutionRunner{t: t, resolvedText: "resolved by agent\n"}
	td.Runner = runner
	key := testScopedKey(t, repo, "set-1")
	seedMergeabilityStore(t, td, map[string]MergeabilityRecord{key: rec})
	seedBindingStore(t, td, map[string]WorktreeBinding{
		key: integrationWorktreeBinding(t, repo, wt, "set-conflict"),
	})
	d := &Deps{
		Tasks: td,
		AcquireRuntimeLock: func(runtimePath string) (runtimeLock, error) {
			return tasks.AcquireRuntimeLock(td, runtimePath, nil)
		},
	}

	var out bytes.Buffer
	got, err := IntegrateWithOptions(d, &config.Config{Projects: []config.ProjectEntry{{Path: repo}}}, "set-1", &out, IntegrationOptions{In: strings.NewReader("\n")})
	if err != nil {
		t.Fatalf("integrate: %v", err)
	}
	if got.Outcome != "resolved" {
		t.Fatalf("result = %+v, want resolved integration", got)
	}
	if runner.name != "claude" {
		t.Fatalf("agent = %q, want claude fallback", runner.name)
	}
}

func TestIntegrateConflictAttendedResolutionMergesAndTearsDown(t *testing.T) {
	repo, wt, rec := setupConflictingIntegration(t)
	td := queueDataDeps(t)
	runner := &conflictResolutionRunner{t: t, resolvedText: "resolved by agent\n"}
	td.Runner = runner
	key := testScopedKey(t, repo, "set-1")
	seedMergeabilityStore(t, td, map[string]MergeabilityRecord{key: rec})
	seedBindingStore(t, td, map[string]WorktreeBinding{
		key: integrationWorktreeBinding(t, repo, wt, "set-conflict"),
	})
	var lockedRuntime string
	d := &Deps{
		Tasks: td,
		AcquireRuntimeLock: func(runtimePath string) (runtimeLock, error) {
			lockedRuntime = runtimePath
			return tasks.AcquireRuntimeLock(td, runtimePath, nil)
		},
	}

	var out bytes.Buffer
	got, err := IntegrateWithOptions(d, &config.Config{Projects: []config.ProjectEntry{{Path: repo}}}, "set-1", &out, IntegrationOptions{In: strings.NewReader("\n"), AgentPreset: "claude"})
	if err != nil {
		t.Fatalf("integrate: %v", err)
	}
	if got.Kept || got.Outcome != "resolved" || got.Branch != "set-conflict" {
		t.Fatalf("result = %+v, want resolved integration", got)
	}
	canonicalRepo, err := filepath.EvalSymlinks(repo)
	if err != nil {
		t.Fatalf("canonical repo: %v", err)
	}
	if lockedRuntime != canonicalRepo {
		t.Fatalf("locked runtime = %q, want %q", lockedRuntime, canonicalRepo)
	}
	if runner.calls != 1 || runner.dir != canonicalRepo || runner.name != "claude" {
		t.Fatalf("runner = calls %d dir %q name %q", runner.calls, runner.dir, runner.name)
	}
	if len(runner.args) == 0 || !strings.Contains(runner.args[len(runner.args)-1], "Pop queue integration conflict") {
		t.Fatalf("agent prompt args = %#v", runner.args)
	}
	if got := string(mustReadFile(t, filepath.Join(repo, "shared.txt"))); got != "resolved by agent\n" {
		t.Fatalf("merged file = %q", got)
	}
	if _, err := os.Stat(wt); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("worktree stat err = %v, want not exist", err)
	}
	if branch := strings.TrimSpace(runGitOutput(t, repo, "branch", "--list", "set-conflict")); branch != "" {
		t.Fatalf("branch still exists: %q", branch)
	}
	if len(loadMergeabilityStore(t, td)) != 0 {
		t.Fatalf("mergeability state = %+v, want cleared", loadMergeabilityStore(t, td))
	}
	if len(loadBindingStore(t, td)) != 0 {
		t.Fatalf("worktree bindings = %+v, want cleared", loadBindingStore(t, td))
	}
	// Resolving the conflict and merging records a durable integration event.
	events, err := tasks.IntegrationEventsForSet(td, "set-1")
	if err != nil {
		t.Fatalf("read integration events: %v", err)
	}
	if len(events) != 1 || events[0].SetID != "set-1" {
		t.Fatalf("integration events = %+v, want one for set-1", events)
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
	events, err := tasks.IntegrationEventsForSet(td, "set-1")
	if err != nil {
		t.Fatalf("read integration events: %v", err)
	}
	if len(events) != 0 {
		t.Fatalf("integration events = %+v, want none for no-op", events)
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

func integrationWorktreeBinding(t *testing.T, repo, wt, branch string) WorktreeBinding {
	t.Helper()
	return WorktreeBinding{
		RuntimePath: wt,
		Branch:      branch,
		Project:     filepath.Base(repo),
		Provisioned: true,
	}
}

func setupConflictingIntegration(t *testing.T) (string, string, MergeabilityRecord) {
	t.Helper()
	repo := initMergeabilityRepo(t)
	wt := filepath.Join(t.TempDir(), "set-conflict")
	runGit(t, repo, "worktree", "add", "-b", "set-conflict", wt, "HEAD")
	writeFile(t, filepath.Join(wt, "shared.txt"), "set branch\n")
	runGit(t, wt, "add", "shared.txt")
	runGit(t, wt, "commit", "-m", "set edits shared")
	writeFile(t, filepath.Join(repo, "shared.txt"), "working branch\n")
	runGit(t, repo, "add", "shared.txt")
	runGit(t, repo, "commit", "-m", "working edits shared")
	rec, err := (&Deps{Tasks: tasks.DefaultDeps()}).computeMergeability(repo, wt)
	if err != nil {
		t.Fatalf("compute mergeability: %v", err)
	}
	rec.Project = filepath.Base(repo)
	rec.RuntimePath = wt
	rec.SetID = "set-1"
	return repo, wt, rec
}

func mustReadFile(t *testing.T, path string) []byte {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return data
}

type noopConflictRunner struct{}

func (noopConflictRunner) Run(ctx context.Context, dir string, stdout, stderr io.Writer, name string, args ...string) (int, error) {
	return 0, nil
}

func (noopConflictRunner) RunAttended(ctx context.Context, dir string, stdin io.Reader, stdout, stderr io.Writer, name string, args ...string) (int, error) {
	return 0, nil
}

func (noopConflictRunner) Start(ctx context.Context, dir string, stdout, stderr io.Writer, name string, args ...string) (*tasks.ManagedProcess, error) {
	return nil, errors.New("unexpected Start call")
}

type conflictResolutionRunner struct {
	t            *testing.T
	resolvedText string
	calls        int
	dir          string
	name         string
	args         []string
}

func (r *conflictResolutionRunner) Run(ctx context.Context, dir string, stdout, stderr io.Writer, name string, args ...string) (int, error) {
	return r.RunAttended(ctx, dir, nil, stdout, stderr, name, args...)
}

func (r *conflictResolutionRunner) RunAttended(ctx context.Context, dir string, stdin io.Reader, stdout, stderr io.Writer, name string, args ...string) (int, error) {
	r.calls++
	r.dir = dir
	r.name = name
	r.args = append([]string(nil), args...)
	writeFile(r.t, filepath.Join(dir, "shared.txt"), r.resolvedText)
	return 0, nil
}

func (r *conflictResolutionRunner) Start(ctx context.Context, dir string, stdout, stderr io.Writer, name string, args ...string) (*tasks.ManagedProcess, error) {
	return nil, errors.New("unexpected Start call")
}
