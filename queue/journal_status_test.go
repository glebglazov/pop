package queue

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"github.com/glebglazov/pop/internal/deps"
	"github.com/glebglazov/pop/store"
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
	// The store handle is now process-cached; close it at test end so it does not
	// outlive this test's temp data dir (test cleanup, per ADR-0118).
	t.Cleanup(func() { _ = d.CloseStore() })
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
				RuntimePath: "/runtime/set-busy",
				Locked:      true,
				Metadata: &tasks.RuntimeLockMetadata{
					PID:         1234,
					RuntimePath: "/runtime/set-busy",
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
	})
	if err != nil {
		t.Fatal(err)
	}
	snap.Tasks = td

	var out bytes.Buffer
	// Status is now Summary headline + dashboard table + Scan errors (ADR-0121);
	// this snapshot exercises the Summary roll-up and the trailing Scan errors
	// section (the table is fed the dashboard's rows by the command).
	RenderStatus(&out, snap, nil)
	text := out.String()
	for _, want := range []string{
		"Summary:",
		"Queue: 1 running, 1 queued",
		"Scan errors:",
		"idle: /repo/idle/.pop.toml: expected value",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("status output missing %q:\n%s", want, text)
		}
	}
	// The former per-bucket inventory sections are retired; only Summary, the
	// task-set table, and Scan errors remain.
	for _, omit := range []string{
		"Picked-up sets:",
		"Active worktrees:",
		"Queued ready sets:",
		"Blocked:",
		"Awaiting approval:",
		"Skipped repositories:",
		"other project: no ready work",
		"Daemon state:",
		`"version"`,
	} {
		if strings.Contains(text, omit) {
			t.Fatalf("status output should not contain retired section %q:\n%s", omit, text)
		}
	}
}

// TestBuildLogFromStore checks the Queue journal view is derived from Drain
// transitions, integration events, and park-clears in the store — there is no
// standalone journal file (ADR-0055).
func TestBuildLogFromStore(t *testing.T) {
	td := queueDataDeps(t)
	repo := initGitRepoWithBase(t)
	commonDir := testRepoCommonDir(t, td, repo)

	// A drain that quota-paused: contributes a spawn and a quota_paused outcome.
	h, err := tasks.BeginDrain(td, repo, "set-1", nil)
	if err != nil {
		t.Fatalf("BeginDrain: %v", err)
	}
	if err := h.Finish(store.StateQuotaPaused, "codex", false, time.Time{}); err != nil {
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

func TestRenderStatusShowsRecoveryWaiter(t *testing.T) {
	resetAt := time.Date(2026, 6, 15, 14, 0, 0, 0, time.UTC)
	td := queueDataDeps(t)
	snap, err := statusFromDecisions(&Deps{Tasks: td}, []Decision{{
		Project:      "pop",
		Reason:       "set waiting for quota recovery",
		BlockedSetID: "set-1",
		WaitUntil:    resetAt,
		Deferral:     SpawnDeferral{Reason: DeferQuotaRecovery, SetID: "set-1", Until: resetAt},
	}})
	if err != nil {
		t.Fatalf("status: %v", err)
	}
	snap.Tasks = td
	snap.RecoveryWaiters = map[string]tasks.RecoveryWaiter{
		"set-1": {
			SetID:       "set-1",
			Preset:      "codex",
			ResetAt:     resetAt,
			RuntimePath: "/runtime/set-1",
		},
	}

	var statusOut bytes.Buffer
	// A quota-recovery waiter counts as a blocked set in the Summary roll-up; the
	// former "Blocked:" inventory section (and its per-waiter detail) is retired —
	// blocked state now rides the dashboard row's STATUS cell (ADR-0121).
	RenderStatus(&statusOut, snap, nil)
	statusText := statusOut.String()
	if !strings.Contains(statusText, "Summary:") || !strings.Contains(statusText, "blocked") {
		t.Fatalf("status Summary should roll up the blocked waiter:\n%s", statusText)
	}
	for _, omit := range []string{"Blocked:", "waiting for quota recovery", "agent=codex"} {
		if strings.Contains(statusText, omit) {
			t.Fatalf("status output should not contain retired detail %q:\n%s", omit, statusText)
		}
	}
}

func TestRecoveryWaiterRunDeltaClearsWhenRemoved(t *testing.T) {
	resetAt := time.Date(2026, 6, 15, 14, 0, 0, 0, time.UTC)
	td := queueDataDeps(t)
	waiter := map[string]tasks.RecoveryWaiter{
		"set-1": {SetID: "set-1", Preset: "codex", ResetAt: resetAt, RuntimePath: "/runtime/set-1"},
	}
	blocked := BuildRunView(StatusSnapshot{
		Tasks:           td,
		RecoveryWaiters: waiter,
	}, time.Now())
	cleared := BuildRunView(StatusSnapshot{Tasks: td}, time.Now())
	lines := DiffRunView(&blocked, cleared)
	if len(lines) != 1 || !strings.Contains(lines[0], "quota recovery cleared") {
		t.Fatalf("recovery waiter cleared delta = %v", lines)
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

// seedAbnormalDrain records one abnormal (crashed) terminal Drain for a set,
// the unit the derived backoff/parking counts (ADR-0055). Only a genuine crash
// is abnormal; interrupted is a clean stop (ADR-0120).
func seedAbnormalDrain(t *testing.T, td *tasks.Deps, runtimePath, setID string) {
	t.Helper()
	h, err := tasks.BeginDrain(td, runtimePath, setID, nil)
	if err != nil {
		t.Fatalf("BeginDrain: %v", err)
	}
	if err := h.Finish(store.StateCrashed, "", false, time.Time{}); err != nil {
		t.Fatalf("Finish: %v", err)
	}
}

func TestCrashBackoffEscalatesThenParksFromDrainHistory(t *testing.T) {
	td := queueDataDeps(t)
	repo := initGitRepoWithBase(t)
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
}

// TestInterruptedTerminalDoesNotBackoffOrPark locks ADR-0120: repeated
// interrupted terminals are clean stops, so the derived backoff/park never
// escalates or parks the set — a manual interrupt clears Auto-drain, so there is
// no re-spawn thrash to throttle.
func TestInterruptedTerminalDoesNotBackoffOrPark(t *testing.T) {
	td := queueDataDeps(t)
	repo := initGitRepoWithBase(t)
	commonDir := testRepoCommonDir(t, td, repo)
	delays := []time.Duration{time.Minute}

	seedInterruptDrain := func() {
		h, err := tasks.BeginDrain(td, repo, "set-int", nil)
		if err != nil {
			t.Fatalf("BeginDrain: %v", err)
		}
		if err := h.Finish(store.StateInterrupted, "", false, time.Time{}); err != nil {
			t.Fatalf("Finish: %v", err)
		}
	}

	// Two interrupts would exceed the single-entry schedule if they counted as
	// abnormal — they must not.
	seedInterruptDrain()
	seedInterruptDrain()
	info, err := tasks.ReadSetBackoff(td, commonDir, "set-int")
	if err != nil {
		t.Fatalf("ReadSetBackoff: %v", err)
	}
	if info.ConsecutiveAbnormal != 0 {
		t.Fatalf("consecutive abnormal after interrupts = %d, want 0", info.ConsecutiveAbnormal)
	}
	if parked, until := setBackoffStatus(info, delays, time.Now().UTC()); parked || !until.IsZero() {
		t.Fatalf("interrupted set must stay spawnable, got (parked %v, until %s)", parked, until)
	}
}

func TestCleanTerminalResetsBackoffCountFromDrainHistory(t *testing.T) {
	td := queueDataDeps(t)
	repo := initGitRepoWithBase(t)
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
	if err := h.Finish(store.StateFinished, "", false, time.Time{}); err != nil {
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

func TestUnparkSetClearsPark(t *testing.T) {
	td := queueDataDeps(t)
	repo := initGitRepoWithBase(t)
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
	ref := SetRef{SetID: "set-1", RepoCommonDir: commonDir, RuntimePath: repo}
	if err := UnparkSet(d, ref); err != nil {
		t.Fatalf("UnparkSet: %v", err)
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
	repoKey := "test-repo"
	key := setScopedKey(repoKey, "set-1")
	td := queueDataDeps(t)
	seedBindingStore(t, td, map[string]WorktreeBinding{
		key: {
			Project:     "pop",
			RuntimePath: "/runtime/set-1",
			Branch:      "set-1",
		},
	})
	snap, err := statusFromDecisions(&Deps{Tasks: td}, []Decision{{
		Project:      "pop",
		Reason:       "set parked after repeated abnormal drain exits",
		BlockedSetID: "set-1",
		Deferral:     SpawnDeferral{Reason: DeferParked, SetID: "set-1"},
	}})
	if err != nil {
		t.Fatalf("status: %v", err)
	}
	snap.Tasks = td

	var statusOut bytes.Buffer
	// A parked set counts as blocked in the Summary roll-up; the retired
	// "Blocked:" / "Active worktrees:" inventory sections no longer render — the
	// dashboard row's STATUS cell carries the parked suffix (ADR-0121).
	RenderStatus(&statusOut, snap, nil)
	statusText := statusOut.String()
	if !strings.Contains(statusText, "Summary:") || !strings.Contains(statusText, "blocked") {
		t.Fatalf("status Summary should roll up the parked set:\n%s", statusText)
	}
	for _, omit := range []string{
		"Blocked:",
		"Active worktrees:",
		"test-repo: set-1 branch=set-1 at /runtime/set-1 — bound",
		"pop: set-1 parked",
		"Daemon state:",
		`"version"`,
		"set_crash_backoffs",
		"parked_sets",
	} {
		if strings.Contains(statusText, omit) {
			t.Fatalf("status output should not contain retired detail %q:\n%s", omit, statusText)
		}
	}
}
