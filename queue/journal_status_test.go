package queue

import (
	"bytes"
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
	// Pin the *real* process environment at the same temp dir too: helpers that
	// reach the store through tasks.DefaultDeps() (e.g. RefreshWith in
	// setupAbandonTaskManifest) resolve XDG_DATA_HOME from real env, not from the
	// mock seam below. Without this they would write into ~/.local/share/pop and
	// pollute the developer's machine-global store (slice 01).
	t.Setenv("XDG_DATA_HOME", dir)
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

// TestBuildLogFromStore checks the Queue journal view is derived from Drain
// transitions, integration events, and park-clears in the store — there is no
// standalone journal file (ADR-0055).
func TestBuildLogFromStore(t *testing.T) {
	td := queueDataDeps(t)
	repo := initMergeabilityRepo(t)
	commonDir := testRepoCommonDir(t, td, repo)

	// A drain that quota-paused: contributes a spawn and a quota_paused outcome.
	h, err := tasks.BeginDrain(td, repo, "set-1", nil)
	if err != nil {
		t.Fatalf("BeginDrain: %v", err)
	}
	if err := h.Finish(tasks.DrainOutcomeQuotaPaused, "codex", false, time.Time{}); err != nil {
		t.Fatalf("Finish: %v", err)
	}
	// A park-clear (unpark) event.
	if err := tasks.RecordParkClear(td, commonDir, "set-1"); err != nil {
		t.Fatalf("RecordParkClear: %v", err)
	}
	// An integration event.
	if err := tasks.RecordIntegrationEvent(td, tasks.IntegrationEvent{
		ScopedKey: "k", SetID: "set-2", Project: "pop", BaseRef: "main", BranchSHA: "abc",
	}); err != nil {
		t.Fatalf("RecordIntegrationEvent: %v", err)
	}

	events, err := BuildLog(td)
	if err != nil {
		t.Fatalf("BuildLog: %v", err)
	}
	var out bytes.Buffer
	RenderLog(&out, events, 50)
	text := out.String()
	for _, want := range []string{
		"set-1 spawned",
		"set-1 quota_paused agent=codex",
		"set-1 unparked",
		"set-2 integrated base=main",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("log output missing %q:\n%s", want, text)
		}
	}
}

func TestRecordPinnedQuotaCooldownsBacksOffPinnedQuotaSet(t *testing.T) {
	td := queueDataDeps(t)
	repo := initMergeabilityRepo(t)
	key := testScopedKeyFor(t, td, repo, repo, "set-1")
	writtenAt := time.Now().UTC()
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

	if err := recordPinnedQuotaCooldowns(d, cfg, []Decision{{
		Project: "pop",
		scan:    projectScan{ProjectPath: repo, RuntimePath: repo, DefinitionPath: "/def"},
	}}); err != nil {
		t.Fatalf("record cooldowns: %v", err)
	}

	state, err := ReadDaemonState(td)
	if err != nil {
		t.Fatalf("read state: %v", err)
	}
	want := writtenAt.Add(30 * time.Minute)
	if got := state.SetBackoffs[key]; !got.Equal(want) {
		t.Fatalf("set backoff = %s, want finishedAt+30m %s", got, want)
	}

	// The queue must not write the agent-global cooldown store — the executor
	// fallback owns that axis (ADR-0055/0056).
	cooldowns, err := tasks.ActiveAgentCooldownsWith(td, time.Now())
	if err != nil {
		t.Fatalf("read cooldown store: %v", err)
	}
	if len(cooldowns) != 0 {
		t.Fatalf("queue must not write agent cooldown store, got %+v", cooldowns)
	}
}

func TestRecordPinnedQuotaCooldownsFromResetAt(t *testing.T) {
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
				WrittenAt:        time.Now().UTC(),
			}, nil
		},
	}
	cfg := &config.Config{Queue: &config.QueueConfig{AgentQuotaRetryAfter: "30m"}}

	if err := recordPinnedQuotaCooldowns(d, cfg, []Decision{{
		Project: "pop",
		scan:    projectScan{ProjectPath: repo, RuntimePath: repo, DefinitionPath: "/def"},
	}}); err != nil {
		t.Fatalf("record cooldowns: %v", err)
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

// TestRecordPinnedQuotaCooldownsIdempotent checks re-observing the same
// quota_paused terminal across ticks does not drift the cooldown — the instant
// is derived from the Drain's finish time, so a second pass writes the same value
// and, once elapsed, never re-blocks the set.
func TestRecordPinnedQuotaCooldownsIdempotent(t *testing.T) {
	td := queueDataDeps(t)
	repo := initMergeabilityRepo(t)
	key := testScopedKeyFor(t, td, repo, repo, "set-1")
	writtenAt := time.Now().UTC()
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
	dec := []Decision{{Project: "pop", scan: projectScan{ProjectPath: repo, RuntimePath: repo}}}

	if err := recordPinnedQuotaCooldowns(d, cfg, dec); err != nil {
		t.Fatalf("first pass: %v", err)
	}
	first := mustReadState(t, td).SetBackoffs[key]
	if err := recordPinnedQuotaCooldowns(d, cfg, dec); err != nil {
		t.Fatalf("second pass: %v", err)
	}
	second := mustReadState(t, td).SetBackoffs[key]
	if !first.Equal(second) {
		t.Fatalf("cooldown drifted across ticks: %s then %s", first, second)
	}
}

func mustReadState(t *testing.T, td *tasks.Deps) *DaemonState {
	t.Helper()
	state, err := ReadDaemonState(td)
	if err != nil {
		t.Fatalf("read state: %v", err)
	}
	return state
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

func TestRecordPinnedQuotaCooldownsDefaultQuotaDoesNotBackOffSet(t *testing.T) {
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
				WrittenAt:       time.Now().UTC(),
			}, nil
		},
	}

	if err := recordPinnedQuotaCooldowns(d, &config.Config{}, []Decision{{
		Project: "pop",
		scan:    projectScan{ProjectPath: repo, RuntimePath: repo},
	}}); err != nil {
		t.Fatalf("record cooldowns: %v", err)
	}

	state, err := ReadDaemonState(td)
	if err != nil {
		t.Fatalf("read state: %v", err)
	}
	if got := state.SetBackoffs[key]; !got.IsZero() {
		t.Fatalf("set backoff = %s, want none for rotating default quota pause", got)
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

	// No abnormal-backoff or park flags are persisted in daemon state (ADR-0055).
	data, _ := td.FS.ReadFile(DaemonStatePath(td))
	for _, omit := range []string{"set_crash_backoffs", "set_crash_counts", "parked_sets"} {
		if strings.Contains(string(data), omit) {
			t.Fatalf("daemon state must not persist %q: %s", omit, data)
		}
	}
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

func TestRenderStatusShowsCrashBackoffAndPark(t *testing.T) {
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
}
