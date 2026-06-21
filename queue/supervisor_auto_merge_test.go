package queue

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/glebglazov/pop/config"
	"github.com/glebglazov/pop/project"
	"github.com/glebglazov/pop/tasks"
)

func TestRecordTerminalOutcomesAutoMergeCleanEnabledIntegratesAndTearsDown(t *testing.T) {
	repo := initMergeabilityRepo(t)
	if err := os.WriteFile(filepath.Join(repo, ".pop.toml"), []byte("auto_merge_clean = true\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	wt := filepath.Join(t.TempDir(), "set-clean")
	runGit(t, repo, "worktree", "add", "-b", "set-clean", wt, "HEAD")
	writeFile(t, filepath.Join(wt, "set.txt"), "set\n")
	runGit(t, wt, "add", "set.txt")
	runGit(t, wt, "commit", "-m", "set change")

	td := queueDataDeps(t)
	cfg := &config.Config{Projects: []config.ProjectEntry{{Path: repo}}}
	projectName := queueProjectName(t, cfg)
	if err := AppendJournalEntry(td, JournalEntry{
		Event:       JournalEventSpawn,
		Project:     projectName,
		SetID:       "set-1",
		RuntimePath: wt,
		Source:      "supervisor",
	}); err != nil {
		t.Fatalf("append spawn: %v", err)
	}
	var lockedRuntime string
	d := &Deps{
		Tasks: td,
		ReadOutcome: func(runtimePath string) (*tasks.DrainOutcomeRecord, error) {
			if runtimePath != wt {
				return nil, os.ErrNotExist
			}
			return &tasks.DrainOutcomeRecord{
				SetID:       "set-1",
				Outcome:     tasks.DrainOutcomeDone,
				RuntimePath: wt,
				WrittenAt:   time.Date(2026, 6, 14, 15, 0, 0, 0, time.UTC),
			}, nil
		},
		AcquireRuntimeLock: func(runtimePath string) (runtimeLock, error) {
			lockedRuntime = runtimePath
			return tasks.AcquireRuntimeLock(td, runtimePath, nil)
		},
	}

	if err := recordTerminalOutcomes(d, cfg, []Decision{{
		Project: projectName,
		scan:    projectScan{ProjectPath: repo, RuntimePath: repo},
	}}, nil); err != nil {
		t.Fatalf("record outcomes: %v", err)
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
	if _, err := os.Stat(wt); !os.IsNotExist(err) {
		t.Fatalf("worktree stat err = %v, want not exist", err)
	}
	if branch := strings.TrimSpace(runGitOutput(t, repo, "branch", "--list", "set-clean")); branch != "" {
		t.Fatalf("branch still exists: %q", branch)
	}
	if len(loadMergeabilityStore(t, td)) != 0 {
		t.Fatalf("mergeability state = %+v, want cleared", loadMergeabilityStore(t, td))
	}
	entries, err := ReadJournal(td)
	if err != nil {
		t.Fatalf("read journal: %v", err)
	}
	if !journalContainsSource(entries, JournalEventIntegrated, "auto") {
		t.Fatalf("journal entries = %+v, want auto integrated event", entries)
	}
}

func TestRecordTerminalOutcomesAutoMergeCleanDefaultOffWaits(t *testing.T) {
	repo := initMergeabilityRepo(t)
	wt := filepath.Join(t.TempDir(), "set-clean")
	runGit(t, repo, "worktree", "add", "-b", "set-clean", wt, "HEAD")
	writeFile(t, filepath.Join(wt, "set.txt"), "set\n")
	runGit(t, wt, "add", "set.txt")
	runGit(t, wt, "commit", "-m", "set change")

	td := queueDataDeps(t)
	cfg := &config.Config{Projects: []config.ProjectEntry{{Path: repo}}}
	projectName := queueProjectName(t, cfg)
	if err := AppendJournalEntry(td, JournalEntry{Event: JournalEventSpawn, Project: projectName, SetID: "set-1", RuntimePath: wt}); err != nil {
		t.Fatalf("append spawn: %v", err)
	}
	d := &Deps{
		Tasks: td,
		ReadOutcome: func(runtimePath string) (*tasks.DrainOutcomeRecord, error) {
			if runtimePath != wt {
				return nil, os.ErrNotExist
			}
			return &tasks.DrainOutcomeRecord{SetID: "set-1", Outcome: tasks.DrainOutcomeDone, RuntimePath: wt, WrittenAt: time.Date(2026, 6, 14, 15, 0, 0, 0, time.UTC)}, nil
		},
		AcquireRuntimeLock: func(runtimePath string) (runtimeLock, error) {
			t.Fatalf("default-off clean set must wait for human integration")
			return nil, nil
		},
	}

	if err := recordTerminalOutcomes(d, cfg, []Decision{{
		Project: projectName,
		scan:    projectScan{ProjectPath: repo, RuntimePath: repo},
	}}, nil); err != nil {
		t.Fatalf("record outcomes: %v", err)
	}

	if _, err := os.Stat(wt); err != nil {
		t.Fatalf("worktree should remain awaiting integration: %v", err)
	}
	key := testScopedKeyFor(t, td, repo, wt, "set-1")
	if got := loadMergeabilityStore(t, td)[key]; got.Status != MergeabilityClean {
		t.Fatalf("mergeability state = %+v, want clean awaiting integration", loadMergeabilityStore(t, td))
	}
	entries, err := ReadJournal(td)
	if err != nil {
		t.Fatalf("read journal: %v", err)
	}
	if journalContainsSource(entries, JournalEventIntegrated, "auto") {
		t.Fatalf("journal entries = %+v, want no auto integration", entries)
	}
}

func TestRecordTerminalOutcomesAutoMergeCleanDoesNotIntegrateConflicts(t *testing.T) {
	repo, wt, _ := setupConflictingIntegration(t)
	if err := os.WriteFile(filepath.Join(repo, ".pop.toml"), []byte("auto_merge_clean = true\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	td := queueDataDeps(t)
	cfg := &config.Config{Projects: []config.ProjectEntry{{Path: repo}}}
	projectName := queueProjectName(t, cfg)
	if err := AppendJournalEntry(td, JournalEntry{Event: JournalEventSpawn, Project: projectName, SetID: "set-1", RuntimePath: wt}); err != nil {
		t.Fatalf("append spawn: %v", err)
	}
	d := &Deps{
		Tasks: td,
		ReadOutcome: func(runtimePath string) (*tasks.DrainOutcomeRecord, error) {
			if runtimePath != wt {
				return nil, os.ErrNotExist
			}
			return &tasks.DrainOutcomeRecord{SetID: "set-1", Outcome: tasks.DrainOutcomeDone, RuntimePath: wt, WrittenAt: time.Date(2026, 6, 14, 15, 0, 0, 0, time.UTC)}, nil
		},
		AcquireRuntimeLock: func(runtimePath string) (runtimeLock, error) {
			t.Fatalf("conflicting set must never be auto-integrated")
			return nil, nil
		},
	}

	if err := recordTerminalOutcomes(d, cfg, []Decision{{
		Project: projectName,
		scan:    projectScan{ProjectPath: repo, RuntimePath: repo},
	}}, nil); err != nil {
		t.Fatalf("record outcomes: %v", err)
	}

	if _, err := os.Stat(wt); err != nil {
		t.Fatalf("conflicted worktree should remain: %v", err)
	}
	if branch := strings.TrimSpace(runGitOutput(t, repo, "branch", "--list", "set-conflict")); branch == "" {
		t.Fatal("conflicted set branch should remain")
	}
	key := testScopedKeyFor(t, td, repo, wt, "set-1")
	if got := loadMergeabilityStore(t, td)[key]; got.Status != MergeabilityConflicts {
		t.Fatalf("mergeability state = %+v, want conflicts awaiting attended path", loadMergeabilityStore(t, td))
	}
	entries, err := ReadJournal(td)
	if err != nil {
		t.Fatalf("read journal: %v", err)
	}
	if journalContainsSource(entries, JournalEventIntegrated, "auto") {
		t.Fatalf("journal entries = %+v, want no auto integration", entries)
	}
}

func journalContainsSource(entries []JournalEntry, event, source string) bool {
	for _, entry := range entries {
		if entry.Event == event && entry.Source == source {
			return true
		}
	}
	return false
}

func queueProjectName(t *testing.T, cfg *config.Config) string {
	t.Helper()
	projects, err := tasks.ListPickerProjectsWith(project.DefaultDeps(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	if len(projects) != 1 {
		t.Fatalf("projects = %+v, want exactly one", projects)
	}
	return projects[0].Name
}
