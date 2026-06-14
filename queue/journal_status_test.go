package queue

import (
	"bytes"
	"os"
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
	snap := statusFromDecisions([]Decision{
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
			Project:   "waiting",
			TaskSetID: "set-ready",
			Reason:    "ready",
		},
		{
			Project: "idle",
			Reason:  "no ready set",
		},
	}, &DaemonState{Version: 1})

	var out bytes.Buffer
	RenderStatus(&out, snap)
	text := out.String()
	for _, want := range []string{
		"Picked-up sets:",
		"busy: set-busy pid=1234 since 2026-06-14T13:00:00Z",
		"waiting: waiting ready set set-ready",
		"idle: idle (no ready set)",
		`"version": 1`,
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("status output missing %q:\n%s", want, text)
		}
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
}

func TestRecordTerminalOutcomesReadsDrainOutcome(t *testing.T) {
	td := queueDataDeps(t)
	writtenAt := time.Date(2026, 6, 14, 14, 0, 0, 0, time.UTC)
	d := &Deps{
		Tasks: td,
		ReadOutcome: func(runtimePath string) (*tasks.DrainOutcomeRecord, error) {
			if runtimePath != "/runtime" {
				t.Fatalf("runtimePath = %q, want /runtime", runtimePath)
			}
			return &tasks.DrainOutcomeRecord{
				SetID:       "set-1",
				Outcome:     tasks.DrainOutcomeBlocked,
				RuntimePath: "/runtime",
				PID:         222,
				WrittenAt:   writtenAt,
			}, nil
		},
	}

	if err := recordTerminalOutcomes(d, &config.Config{}, []Decision{{
		Project: "pop",
		scan:    projectScan{RuntimePath: "/runtime"},
	}}); err != nil {
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
	if err := AppendJournalEntry(td, JournalEntry{
		Event:       JournalEventSpawn,
		Project:     "pop",
		SetID:       "set-crash",
		RuntimePath: "/runtime",
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
		scan:    projectScan{RuntimePath: "/runtime"},
	}}); err != nil {
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

func TestRecordTerminalOutcomesSetsQuotaCooldown(t *testing.T) {
	td := queueDataDeps(t)
	writtenAt := time.Date(2026, 6, 14, 14, 0, 0, 0, time.UTC)
	d := &Deps{
		Tasks: td,
		ReadOutcome: func(runtimePath string) (*tasks.DrainOutcomeRecord, error) {
			return &tasks.DrainOutcomeRecord{
				SetID:           "set-1",
				Outcome:         tasks.DrainOutcomeQuotaPaused,
				ExhaustedPreset: "codex",
				ExhaustedPinned: true,
				RuntimePath:     "/runtime",
				WrittenAt:       writtenAt,
			}, nil
		},
	}
	cfg := &config.Config{Queue: &config.QueueConfig{AgentQuotaRetryAfter: "30m"}}

	before := time.Now().UTC()
	if err := recordTerminalOutcomes(d, cfg, []Decision{{
		Project: "pop",
		scan:    projectScan{RuntimePath: "/runtime", DefinitionPath: "/def"},
	}}); err != nil {
		t.Fatalf("record outcomes: %v", err)
	}
	after := time.Now().UTC()

	state, err := ReadDaemonState(td)
	if err != nil {
		t.Fatalf("read state: %v", err)
	}
	until := state.AgentCooldowns["codex"]
	if until.Before(before.Add(30*time.Minute)) || until.After(after.Add(30*time.Minute+time.Second)) {
		t.Fatalf("cooldown until = %s, want about now+30m", until)
	}
	if got := state.SetBackoffs[setBackoffKey("/runtime", "set-1")]; !got.Equal(until) {
		t.Fatalf("set backoff = %s, want cooldown %s", got, until)
	}

	entries, err := ReadJournal(td)
	if err != nil {
		t.Fatalf("read journal: %v", err)
	}
	if len(entries) != 2 || entries[1].Event != JournalEventAgentCooldown || entries[1].Agent != "codex" {
		t.Fatalf("journal entries = %+v, want outcome then codex cooldown", entries)
	}
}

func TestRecordTerminalOutcomesDefaultQuotaDoesNotBackOffSet(t *testing.T) {
	td := queueDataDeps(t)
	d := &Deps{
		Tasks: td,
		ReadOutcome: func(runtimePath string) (*tasks.DrainOutcomeRecord, error) {
			return &tasks.DrainOutcomeRecord{
				SetID:           "set-1",
				Outcome:         tasks.DrainOutcomeQuotaPaused,
				ExhaustedPreset: "codex",
				RuntimePath:     "/runtime",
				WrittenAt:       time.Date(2026, 6, 14, 14, 0, 0, 0, time.UTC),
			}, nil
		},
	}

	if err := recordTerminalOutcomes(d, &config.Config{}, []Decision{{
		Project: "pop",
		scan:    projectScan{RuntimePath: "/runtime"},
	}}); err != nil {
		t.Fatalf("record outcomes: %v", err)
	}

	state, err := ReadDaemonState(td)
	if err != nil {
		t.Fatalf("read state: %v", err)
	}
	if state.AgentCooldowns["codex"].IsZero() {
		t.Fatalf("codex cooldown was not recorded: %+v", state.AgentCooldowns)
	}
	if got := state.SetBackoffs[setBackoffKey("/runtime", "set-1")]; !got.IsZero() {
		t.Fatalf("set backoff = %s, want none for rotating default quota pause", got)
	}
}
