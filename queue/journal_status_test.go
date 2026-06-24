package queue

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/glebglazov/pop/config"
	"github.com/glebglazov/pop/internal/deps"
	"github.com/glebglazov/pop/tasks"
)

func queueDataDeps(t *testing.T) *tasks.Deps {
	t.Helper()
	dir := t.TempDir()
	real := deps.NewRealFileSystem()
	d := tasks.DefaultDeps()
	d.FS = &deps.MockFileSystem{
		GetenvFunc: func(key string) string {
			if key == "XDG_DATA_HOME" {
				return dir
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

func TestJournalAppendRead(t *testing.T) {
	d := queueDataDeps(t)
	ts := time.Date(2026, 6, 14, 12, 0, 0, 0, time.UTC)

	if err := AppendJournalEntry(d, JournalEntry{
		Timestamp:   ts,
		Event:       JournalEventSpawn,
		Project:     "pop",
		SetID:       "set-1",
		RuntimePath: "/runtime",
		Source:      "supervisor",
	}); err != nil {
		t.Fatalf("append spawn: %v", err)
	}
	if err := AppendJournalEntry(d, JournalEntry{
		Timestamp:   ts.Add(time.Minute),
		Event:       JournalEventOutcome,
		Project:     "pop",
		SetID:       "set-1",
		RuntimePath: "/runtime",
		Outcome:     tasks.DrainOutcomeDone,
	}); err != nil {
		t.Fatalf("append outcome: %v", err)
	}

	got, err := ReadJournal(d)
	if err != nil {
		t.Fatalf("read journal: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("journal entries = %d, want 2", len(got))
	}
	if got[0].Event != JournalEventSpawn || got[0].SetID != "set-1" {
		t.Fatalf("first entry = %+v", got[0])
	}
	if got[1].Event != JournalEventOutcome || got[1].Outcome != tasks.DrainOutcomeDone {
		t.Fatalf("second entry = %+v", got[1])
	}
	if _, err := os.Stat(JournalPath(d)); err != nil {
		t.Fatalf("journal should exist on disk: %v", err)
	}
}

func TestRenderStatusFromLocksAndState(t *testing.T) {
	started := time.Date(2026, 6, 14, 13, 0, 0, 0, time.UTC)
	td := queueDataDeps(t)
	snap, err := statusFromDecisions(&Deps{Tasks: td}, []Decision{
		{
			Project: "busy",
			Busy:    true,
			lockStatus: &tasks.RuntimeLockStatus{
				RuntimePath: "/runtime/busy",
				Locked:      true,
				Metadata: &tasks.RuntimeLockMetadata{
					PID:         1234,
					RuntimePath: "/runtime/busy",
					StartedAt:   started,
					SetID:       "set-busy",
				},
			},
		},
		{
			Project:       "waiting",
			TaskSetID:     "set-ready",
			Reason:        "ready",
			WorktreeReady: true,
		},
		{
			Project:            "idle",
			Reason:             "no ready set",
			ProjectConfigError: "/repo/idle/.pop.toml: expected value",
		},
	}, &DaemonState{Version: 1})
	if err != nil {
		t.Fatal(err)
	}
	snap.Tasks = td

	var out bytes.Buffer
	RenderStatus(&out, snap)
	text := out.String()
	for _, want := range []string{
		"Summary:",
		"Picked-up sets:",
		"Active worktrees:",
		"busy: set-busy pid=1234 since 2026-06-14T13:00:00Z",
		"Queued ready sets:",
		"waiting [worktree-ready]: waiting ready set set-ready",
		"Queue: 1 running, 1 queued",
		"Integration: none awaiting",
		"Scan errors:",
		"idle: /repo/idle/.pop.toml: expected value",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("status output missing %q:\n%s", want, text)
		}
	}
	for _, omit := range []string{"Daemon state:", `"version"`} {
		if strings.Contains(text, omit) {
			t.Fatalf("status output should not contain %q:\n%s", omit, text)
		}
	}
	if strings.Contains(text, "other project: no ready work") {
		t.Fatalf("config error project should be a scan error, not collapsed idle:\n%s", text)
	}
}

func TestRenderLogFromSampleJournal(t *testing.T) {
	entries := []JournalEntry{
		{
			Timestamp: time.Date(2026, 6, 14, 12, 0, 0, 0, time.UTC),
			Event:     JournalEventSpawn,
			Project:   "pop",
			SetID:     "set-1",
			Source:    "supervisor",
		},
		{
			Timestamp: time.Date(2026, 6, 14, 12, 5, 0, 0, time.UTC),
			Event:     JournalEventOutcome,
			Project:   "pop",
			SetID:     "set-1",
			Outcome:   tasks.DrainOutcomeQuotaPaused,
		},
		{
			Timestamp: time.Date(2026, 6, 14, 12, 6, 0, 0, time.UTC),
			Event:     JournalEventSpawnFailed,
			Project:   "pop",
			SetID:     "set-2",
			Reason:    "create drain pane: tmux refused pane",
		},
	}

	var out bytes.Buffer
	RenderLog(&out, entries, 50)
	text := out.String()
	if !strings.Contains(text, "2026-06-14T12:00:00Z pop set-1 spawned source=supervisor") {
		t.Fatalf("log output missing spawn:\n%s", text)
	}
	if !strings.Contains(text, "2026-06-14T12:05:00Z pop set-1 outcome=quota_paused") {
		t.Fatalf("log output missing outcome:\n%s", text)
	}
	if !strings.Contains(text, "2026-06-14T12:06:00Z pop set-2 spawn_failed reason=create drain pane: tmux refused pane") {
		t.Fatalf("log output missing spawn failure:\n%s", text)
	}
}

func TestRecordTerminalOutcomesReadsDrainOutcome(t *testing.T) {
	td := queueDataDeps(t)
	repo := initMergeabilityRepo(t)
	writtenAt := time.Date(2026, 6, 14, 14, 0, 0, 0, time.UTC)
	d := &Deps{
		Tasks: td,
		ReadOutcome: func(runtimePath string) (*tasks.DrainOutcomeRecord, error) {
			if runtimePath != repo {
				t.Fatalf("runtimePath = %q, want %q", runtimePath, repo)
			}
			return &tasks.DrainOutcomeRecord{
				SetID:       "set-1",
				Outcome:     tasks.DrainOutcomeBlocked,
				RuntimePath: repo,
				PID:         222,
				WrittenAt:   writtenAt,
			}, nil
		},
	}

	if err := recordTerminalOutcomes(d, &config.Config{}, []Decision{{
		Project: "pop",
		scan:    projectScan{ProjectPath: repo, RuntimePath: repo},
	}}, nil); err != nil {
		t.Fatalf("record outcomes: %v", err)
	}

	entries, err := ReadJournal(td)
	if err != nil {
		t.Fatalf("read journal: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("entries = %d, want 1", len(entries))
	}
	got := entries[0]
	if got.Event != JournalEventOutcome || got.Outcome != tasks.DrainOutcomeBlocked || got.SetID != "set-1" {
		t.Fatalf("outcome entry = %+v", got)
	}
	if !got.Timestamp.Equal(writtenAt) {
		t.Fatalf("timestamp = %s, want %s", got.Timestamp, writtenAt)
	}
}

func TestRecordTerminalOutcomesInfersCrashForOpenSpawnWithoutOutcome(t *testing.T) {
	td := queueDataDeps(t)
	repo := initMergeabilityRepo(t)
	if err := AppendJournalEntry(td, JournalEntry{
		Event:       JournalEventSpawn,
		Project:     "pop",
		SetID:       "set-crash",
		RuntimePath: repo,
		Source:      "supervisor",
	}); err != nil {
		t.Fatalf("append spawn: %v", err)
	}
	d := &Deps{
		Tasks: td,
		ReadOutcome: func(runtimePath string) (*tasks.DrainOutcomeRecord, error) {
			return nil, os.ErrNotExist
		},
	}

	if err := recordTerminalOutcomes(d, &config.Config{}, []Decision{{
		Project: "pop",
		scan:    projectScan{ProjectPath: repo, RuntimePath: repo},
	}}, nil); err != nil {
		t.Fatalf("record outcomes: %v", err)
	}

	entries, err := ReadJournal(td)
	if err != nil {
		t.Fatalf("read journal: %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("entries = %d, want 2", len(entries))
	}
	got := entries[1]
	if got.Event != JournalEventOutcome || got.Outcome != DrainOutcomeCrashed || got.SetID != "set-crash" {
		t.Fatalf("crash entry = %+v", got)
	}
}

func TestRecordTerminalOutcomesBacksOffPinnedQuotaSet(t *testing.T) {
	td := queueDataDeps(t)
	repo := initMergeabilityRepo(t)
	key := testScopedKeyFor(t, td, repo, repo, "set-1")
	writtenAt := time.Date(2026, 6, 14, 14, 0, 0, 0, time.UTC)
	d := &Deps{
		Tasks: td,
		ReadOutcome: func(runtimePath string) (*tasks.DrainOutcomeRecord, error) {
			return &tasks.DrainOutcomeRecord{
				SetID:           "set-1",
				Outcome:         tasks.DrainOutcomeQuotaPaused,
				ExhaustedPreset: "codex",
				ExhaustedPinned: true,
				RuntimePath:     repo,
				WrittenAt:       writtenAt,
			}, nil
		},
	}
	cfg := &config.Config{Queue: &config.QueueConfig{AgentQuotaRetryAfter: "30m"}}

	before := time.Now().UTC()
	if err := recordTerminalOutcomes(d, cfg, []Decision{{
		Project: "pop",
		scan:    projectScan{ProjectPath: repo, RuntimePath: repo, DefinitionPath: "/def"},
	}}, nil); err != nil {
		t.Fatalf("record outcomes: %v", err)
	}
	after := time.Now().UTC()

	state, err := ReadDaemonState(td)
	if err != nil {
		t.Fatalf("read state: %v", err)
	}
	until := state.SetBackoffs[key]
	if until.Before(before.Add(30*time.Minute)) || until.After(after.Add(30*time.Minute+time.Second)) {
		t.Fatalf("set backoff = %s, want about now+30m", until)
	}

	entries, err := ReadJournal(td)
	if err != nil {
		t.Fatalf("read journal: %v", err)
	}
	if len(entries) != 2 || entries[1].Event != JournalEventAgentCooldown || entries[1].Agent != "codex" {
		t.Fatalf("journal entries = %+v, want outcome then codex cooldown", entries)
	}
	cooldowns, err := tasks.ActiveAgentCooldownsWith(td, after)
	if err != nil {
		t.Fatalf("read cooldown store: %v", err)
	}
	if len(cooldowns) != 0 {
		t.Fatalf("queue must not write task cooldown store, got %+v", cooldowns)
	}
}

func TestRecordTerminalOutcomesBacksOffPinnedQuotaSetFromResetAt(t *testing.T) {
	td := queueDataDeps(t)
	repo := initMergeabilityRepo(t)
	key := testScopedKeyFor(t, td, repo, repo, "set-1")
	resetAt := time.Now().UTC().Add(45 * time.Minute).Truncate(time.Second)
	d := &Deps{
		Tasks: td,
		ReadOutcome: func(runtimePath string) (*tasks.DrainOutcomeRecord, error) {
			return &tasks.DrainOutcomeRecord{
				SetID:            "set-1",
				Outcome:          tasks.DrainOutcomeQuotaPaused,
				ExhaustedPreset:  "codex",
				ExhaustedPinned:  true,
				ExhaustedResetAt: resetAt,
				RuntimePath:      repo,
				WrittenAt:        time.Date(2026, 6, 14, 14, 0, 0, 0, time.UTC),
			}, nil
		},
	}
	cfg := &config.Config{Queue: &config.QueueConfig{AgentQuotaRetryAfter: "30m"}}

	if err := recordTerminalOutcomes(d, cfg, []Decision{{
		Project: "pop",
		scan:    projectScan{ProjectPath: repo, RuntimePath: repo, DefinitionPath: "/def"},
	}}, nil); err != nil {
		t.Fatalf("record outcomes: %v", err)
	}

	state, err := ReadDaemonState(td)
	if err != nil {
		t.Fatalf("read state: %v", err)
	}
	want := resetAt.Add(2 * time.Minute)
	if got := state.SetBackoffs[key]; !got.Equal(want) {
		t.Fatalf("set backoff = %s, want reset+2m %s", got, want)
	}
}

func TestAgentQuotaCooldownUntilPolicy(t *testing.T) {
	now := time.Date(2026, 6, 15, 12, 0, 0, 0, time.UTC)
	fallback := 30 * time.Minute

	tests := []struct {
		name    string
		resetAt time.Time
		want    time.Time
	}{
		{name: "zero fallback", resetAt: time.Time{}, want: now.Add(fallback)},
		{name: "past fallback", resetAt: now.Add(-time.Second), want: now.Add(fallback)},
		{name: "too far fallback", resetAt: now.Add(8*24*time.Hour + time.Second), want: now.Add(fallback)},
		{name: "sane reset with skew", resetAt: now.Add(time.Hour), want: now.Add(time.Hour + 2*time.Minute)},
		{name: "eight days exactly with skew", resetAt: now.Add(8 * 24 * time.Hour), want: now.Add(8*24*time.Hour + 2*time.Minute)},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := agentQuotaCooldownUntil(tc.resetAt, now, fallback); !got.Equal(tc.want) {
				t.Fatalf("cooldown = %s, want %s", got, tc.want)
			}
		})
	}
}

func TestRecordTerminalOutcomesDefaultQuotaDoesNotBackOffSet(t *testing.T) {
	td := queueDataDeps(t)
	repo := initMergeabilityRepo(t)
	key := testScopedKeyFor(t, td, repo, repo, "set-1")
	d := &Deps{
		Tasks: td,
		ReadOutcome: func(runtimePath string) (*tasks.DrainOutcomeRecord, error) {
			return &tasks.DrainOutcomeRecord{
				SetID:           "set-1",
				Outcome:         tasks.DrainOutcomeQuotaPaused,
				ExhaustedPreset: "codex",
				RuntimePath:     repo,
				WrittenAt:       time.Date(2026, 6, 14, 14, 0, 0, 0, time.UTC),
			}, nil
		},
	}

	if err := recordTerminalOutcomes(d, &config.Config{}, []Decision{{
		Project: "pop",
		scan:    projectScan{ProjectPath: repo, RuntimePath: repo},
	}}, nil); err != nil {
		t.Fatalf("record outcomes: %v", err)
	}

	state, err := ReadDaemonState(td)
	if err != nil {
		t.Fatalf("read state: %v", err)
	}
	if got := state.SetBackoffs[key]; !got.IsZero() {
		t.Fatalf("set backoff = %s, want none for rotating default quota pause", got)
	}
	cooldowns, err := tasks.ActiveAgentCooldownsWith(td, time.Now())
	if err != nil {
		t.Fatalf("read cooldown store: %v", err)
	}
	if len(cooldowns) != 0 {
		t.Fatalf("queue must not write task cooldown store, got %+v", cooldowns)
	}
}

func TestDrainOutcomeAbnormalClassification(t *testing.T) {
	if !drainOutcomeAbnormal(DrainOutcomeCrashed) {
		t.Fatal("crashed outcome must be abnormal")
	}
	if !drainOutcomeAbnormal(tasks.DrainOutcomeInterrupted) {
		t.Fatal("interrupted outcome must be abnormal")
	}
	for _, outcome := range []tasks.DrainOutcome{
		tasks.DrainOutcomeDone,
		tasks.DrainOutcomeFailed,
		tasks.DrainOutcomeBlocked,
		tasks.DrainOutcomeDeferred,
		tasks.DrainOutcomeQuotaPaused,
	} {
		if drainOutcomeAbnormal(outcome) {
			t.Fatalf("%s must be classified as clean", outcome)
		}
	}
}

// testRepoCommonDir resolves the Drain row's repo key (the canonical git common
// dir) for a checkout, matching what BeginDrain records.
func testRepoCommonDir(t *testing.T, td *tasks.Deps, path string) string {
	t.Helper()
	id, err := tasks.ResolveRepositoryIdentity(td, path)
	if err != nil {
		t.Fatalf("resolve repository identity: %v", err)
	}
	return id.CommonDir
}

// seedAbnormalDrain records one abnormal (interrupted) terminal Drain for a set,
// the unit the derived backoff/parking counts (ADR-0055).
func seedAbnormalDrain(t *testing.T, td *tasks.Deps, runtimePath, setID string) {
	t.Helper()
	h, err := tasks.BeginDrain(td, runtimePath, setID, nil)
	if err != nil {
		t.Fatalf("BeginDrain: %v", err)
	}
	if err := h.Finish(tasks.DrainOutcomeInterrupted, "", false, time.Time{}); err != nil {
		t.Fatalf("Finish: %v", err)
	}
}

func TestCrashBackoffEscalatesThenParksFromDrainHistory(t *testing.T) {
	td := queueDataDeps(t)
	repo := initMergeabilityRepo(t)
	commonDir := testRepoCommonDir(t, td, repo)
	delays := []time.Duration{time.Minute, 5 * time.Minute}

	// First abnormal terminal → backoff one delay from the terminal instant.
	seedAbnormalDrain(t, td, repo, "set-crash")
	info, err := tasks.ReadSetBackoff(td, commonDir, "set-crash")
	if err != nil {
		t.Fatalf("ReadSetBackoff: %v", err)
	}
	if info.ConsecutiveAbnormal != 1 {
		t.Fatalf("consecutive abnormal = %d, want 1", info.ConsecutiveAbnormal)
	}
	if parked, until := setBackoffStatus(info, delays, info.LastAbnormalAt); parked || !until.Equal(info.LastAbnormalAt.Add(time.Minute)) {
		t.Fatalf("first backoff = (parked %v, until %s), want until+1m", parked, until)
	}

	// Second abnormal terminal → escalates to the second delay.
	seedAbnormalDrain(t, td, repo, "set-crash")
	info, _ = tasks.ReadSetBackoff(td, commonDir, "set-crash")
	if info.ConsecutiveAbnormal != 2 {
		t.Fatalf("consecutive abnormal = %d, want 2", info.ConsecutiveAbnormal)
	}
	if parked, until := setBackoffStatus(info, delays, info.LastAbnormalAt); parked || !until.Equal(info.LastAbnormalAt.Add(5*time.Minute)) {
		t.Fatalf("second backoff = (parked %v, until %s), want until+5m", parked, until)
	}

	// Third abnormal terminal exhausts the schedule (park threshold = len+1).
	seedAbnormalDrain(t, td, repo, "set-crash")
	info, _ = tasks.ReadSetBackoff(td, commonDir, "set-crash")
	if info.ConsecutiveAbnormal != 3 {
		t.Fatalf("consecutive abnormal = %d, want 3", info.ConsecutiveAbnormal)
	}
	if parked, _ := setBackoffStatus(info, delays, info.LastAbnormalAt); !parked {
		t.Fatalf("third abnormal terminal must park the set")
	}

	// The supervisor journals the park exactly once on the crossing terminal.
	d := &Deps{
		Tasks: td,
		ReadOutcome: func(runtimePath string) (*tasks.DrainOutcomeRecord, error) {
			return nil, os.ErrNotExist
		},
	}
	cfg := &config.Config{Queue: &config.QueueConfig{CrashRetryDelays: []string{"1m", "5m"}}}
	if err := AppendJournalEntry(td, JournalEntry{
		Event:       JournalEventSpawn,
		Project:     "pop",
		SetID:       "set-crash",
		RuntimePath: repo,
		Source:      "supervisor",
	}); err != nil {
		t.Fatalf("append spawn: %v", err)
	}
	if err := recordTerminalOutcomes(d, cfg, []Decision{{
		Project: "pop",
		scan:    projectScan{ProjectPath: repo, RuntimePath: repo},
	}}, nil); err != nil {
		t.Fatalf("record outcomes: %v", err)
	}

	entries, err := ReadJournal(td)
	if err != nil {
		t.Fatalf("read journal: %v", err)
	}
	var parked int
	for _, entry := range entries {
		if entry.Event == JournalEventSetParked && entry.SetID == "set-crash" {
			parked++
		}
	}
	if parked != 1 {
		t.Fatalf("park journal events = %d, want 1; entries=%+v", parked, entries)
	}

	// No abnormal-backoff or park flags are persisted in daemon state (ADR-0055).
	state, err := ReadDaemonState(td)
	if err != nil {
		t.Fatalf("read state: %v", err)
	}
	data, _ := td.FS.ReadFile(DaemonStatePath(td))
	for _, omit := range []string{"set_crash_backoffs", "set_crash_counts", "parked_sets"} {
		if strings.Contains(string(data), omit) {
			t.Fatalf("daemon state must not persist %q: %s", omit, data)
		}
	}
	_ = state
}

func TestCleanTerminalResetsBackoffCountFromDrainHistory(t *testing.T) {
	td := queueDataDeps(t)
	repo := initMergeabilityRepo(t)
	commonDir := testRepoCommonDir(t, td, repo)

	seedAbnormalDrain(t, td, repo, "set-1")
	seedAbnormalDrain(t, td, repo, "set-1")
	info, err := tasks.ReadSetBackoff(td, commonDir, "set-1")
	if err != nil {
		t.Fatalf("ReadSetBackoff: %v", err)
	}
	if info.ConsecutiveAbnormal != 2 {
		t.Fatalf("consecutive abnormal = %d, want 2 before clean terminal", info.ConsecutiveAbnormal)
	}

	// A clean (finished) terminal breaks the abnormal run, resetting the count
	// for free — no stored counter to clear.
	h, err := tasks.BeginDrain(td, repo, "set-1", nil)
	if err != nil {
		t.Fatalf("BeginDrain: %v", err)
	}
	if err := h.Finish(tasks.DrainOutcomeFinished, "", false, time.Time{}); err != nil {
		t.Fatalf("Finish: %v", err)
	}
	info, _ = tasks.ReadSetBackoff(td, commonDir, "set-1")
	if info.ConsecutiveAbnormal != 0 {
		t.Fatalf("consecutive abnormal after clean terminal = %d, want 0", info.ConsecutiveAbnormal)
	}
	if parked, until := setBackoffStatus(info, []time.Duration{time.Minute}, time.Now().UTC()); parked || !until.IsZero() {
		t.Fatalf("clean terminal must leave the set spawnable, got (parked %v, until %s)", parked, until)
	}
}

func TestRecordTerminalOutcomesDoneRecordsMergeability(t *testing.T) {
	td := queueDataDeps(t)
	repo := initMergeabilityRepo(t)
	wt := filepath.Join(t.TempDir(), "set-1")
	runGit(t, repo, "worktree", "add", "-b", "set-1", wt, "HEAD")
	key := testScopedKeyFor(t, td, repo, wt, "set-1")
	if err := AppendJournalEntry(td, JournalEntry{
		Event:       JournalEventSpawn,
		Project:     "pop",
		SetID:       "set-1",
		RuntimePath: wt,
		Source:      "supervisor",
	}); err != nil {
		t.Fatalf("append spawn: %v", err)
	}
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
				WrittenAt:   time.Date(2026, 6, 14, 14, 0, 0, 0, time.UTC),
			}, nil
		},
		ComputeMergeability: func(workingPath, runtimePath string) (MergeabilityRecord, error) {
			if workingPath != repo || runtimePath != wt {
				t.Fatalf("mergeability paths = %q %q, want %q %q", workingPath, runtimePath, repo, wt)
			}
			return MergeabilityRecord{Status: MergeabilityClean, Target: "main", Source: "set"}, nil
		},
	}

	if err := recordTerminalOutcomes(d, &config.Config{}, []Decision{{
		Project: "pop",
		scan:    projectScan{ProjectPath: repo, RuntimePath: repo},
	}}, nil); err != nil {
		t.Fatalf("record outcomes: %v", err)
	}

	got := loadMergeabilityStore(t, td)[key]
	if got.Status != MergeabilityClean || got.Project != "pop" || got.SetID != "set-1" {
		t.Fatalf("mergeability state = %+v", got)
	}
	entries, err := ReadJournal(td)
	if err != nil {
		t.Fatalf("read journal: %v", err)
	}
	if len(entries) != 3 || entries[2].Event != JournalEventMergeability || entries[2].MergeStatus != MergeabilityClean {
		t.Fatalf("journal entries = %+v, want spawn/outcome/mergeability", entries)
	}
}

func TestRecordTerminalOutcomesDoneSkipsMergeabilityForTrunkDrain(t *testing.T) {
	td := queueDataDeps(t)
	repo := initMergeabilityRepo(t)
	d := &Deps{
		Tasks: td,
		ReadOutcome: func(runtimePath string) (*tasks.DrainOutcomeRecord, error) {
			if runtimePath != repo {
				return nil, os.ErrNotExist
			}
			return &tasks.DrainOutcomeRecord{
				SetID:       "set-trunk",
				Outcome:     tasks.DrainOutcomeDone,
				RuntimePath: repo,
				WrittenAt:   time.Date(2026, 6, 14, 14, 0, 0, 0, time.UTC),
			}, nil
		},
		ComputeMergeability: func(workingPath, runtimePath string) (MergeabilityRecord, error) {
			t.Fatalf("mergeability should not be computed for trunk drain: %q %q", workingPath, runtimePath)
			return MergeabilityRecord{}, nil
		},
	}

	if err := recordTerminalOutcomes(d, &config.Config{}, []Decision{{
		Project: "pop",
		scan:    projectScan{ProjectPath: repo, RuntimePath: repo},
	}}, nil); err != nil {
		t.Fatalf("record outcomes: %v", err)
	}

	if got := loadMergeabilityStore(t, td); len(got) != 0 {
		t.Fatalf("mergeability state = %+v, want empty", got)
	}
	entries, err := ReadJournal(td)
	if err != nil {
		t.Fatalf("read journal: %v", err)
	}
	if len(entries) != 1 || entries[0].Event != JournalEventOutcome {
		t.Fatalf("journal entries = %+v, want outcome only", entries)
	}
}

func TestUnparkDashboardRowClearsPark(t *testing.T) {
	td := queueDataDeps(t)
	repo := initMergeabilityRepo(t)
	commonDir := testRepoCommonDir(t, td, repo)
	delays := []time.Duration{time.Minute}

	// Two abnormal terminals exceed the single-entry schedule and park the set.
	seedAbnormalDrain(t, td, repo, "set-1")
	seedAbnormalDrain(t, td, repo, "set-1")
	info, err := tasks.ReadSetBackoff(td, commonDir, "set-1")
	if err != nil {
		t.Fatalf("ReadSetBackoff: %v", err)
	}
	if parked, _ := setBackoffStatus(info, delays, time.Now().UTC()); !parked {
		t.Fatal("set should be parked before unpark")
	}

	d := &Deps{Tasks: td}
	row := DashboardRow{SetID: "set-1", repoCommonDir: commonDir, runtimePath: repo}
	if err := UnparkDashboardRow(d, row); err != nil {
		t.Fatalf("UnparkDashboardRow: %v", err)
	}

	info, _ = tasks.ReadSetBackoff(td, commonDir, "set-1")
	if info.ParkClearedAt.IsZero() {
		t.Fatal("park-clear event was not recorded")
	}
	if parked, until := setBackoffStatus(info, delays, time.Now().UTC()); parked || !until.IsZero() {
		t.Fatalf("set should be spawnable after unpark, got (parked %v, until %s)", parked, until)
	}
}

func TestRenderStatusAndLogShowCrashBackoffAndPark(t *testing.T) {
	now := time.Date(2026, 6, 14, 12, 0, 0, 0, time.UTC)
	repoKey := "test-repo"
	key := setScopedKey(repoKey, "set-1")
	td := queueDataDeps(t)
	seedBindingStore(t, td, map[string]WorktreeBinding{
		key: {
			Project:     "pop",
			RuntimePath: "/runtime",
			Branch:      "set-1",
		},
	})
	seedMergeabilityStore(t, td, map[string]MergeabilityRecord{
		key: {
			Project:     "pop",
			RuntimePath: "/runtime",
			SetID:       "set-1",
			Status:      MergeabilityConflicts,
			CheckedAt:   now,
		},
	})
	snap, err := statusFromDecisions(&Deps{Tasks: td}, []Decision{{
		Project:      "pop",
		Reason:       "set parked after repeated abnormal drain exits",
		BlockedSetID: "set-1",
	}}, &DaemonState{Version: 1})
	if err != nil {
		t.Fatalf("status: %v", err)
	}
	snap.Tasks = td

	var statusOut bytes.Buffer
	RenderStatus(&statusOut, snap)
	statusText := statusOut.String()
	for _, want := range []string{
		"set-1",
		"Blocked:",
		"Awaiting integration:",
		"Active worktrees:",
		"pop: set-1 branch=set-1 at /runtime — conflicts",
		"pop: set-1 parked",
		"integrate: pop tasks integrate set-1",
	} {
		if !strings.Contains(statusText, want) {
			t.Fatalf("status output missing %q:\n%s", want, statusText)
		}
	}
	for _, omit := range []string{"Daemon state:", `"version"`, "set_crash_backoffs", "parked_sets"} {
		if strings.Contains(statusText, omit) {
			t.Fatalf("status output should not contain %q:\n%s", omit, statusText)
		}
	}

	var logOut bytes.Buffer
	RenderLog(&logOut, []JournalEntry{{
		Timestamp: now,
		Event:     JournalEventSetParked,
		Project:   "pop",
		SetID:     "set-1",
		Reason:    "repeated abnormal drain exits",
	}}, 50)
	if !strings.Contains(logOut.String(), "pop set-1 parked reason=repeated abnormal drain exits") {
		t.Fatalf("log output missing park event:\n%s", logOut.String())
	}
}
