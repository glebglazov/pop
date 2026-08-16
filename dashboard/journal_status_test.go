package dashboard

import (
	"bytes"
	"github.com/glebglazov/pop/internal/queuetest"
	"github.com/glebglazov/pop/tasks/drain"
	"strings"
	"testing"
	"time"

	"github.com/glebglazov/pop/tasks"
)

func TestRenderStatusFromLocksAndState(t *testing.T) {
	started := time.Date(2026, 6, 14, 13, 0, 0, 0, time.UTC)
	td := queuetest.DataDeps(t)
	snap, err := drain.StatusFromDecisions(&drain.Deps{Tasks: td}, []drain.Decision{
		{
			Project: "busy",
			Busy:    true,
			LockStatus: &tasks.RuntimeLockStatus{
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
			Project:   "waiting",
			TaskSetID: "set-ready",
			Reason:    "ready",
		},
		{
			Project:            "idle",
			Reason:             "no ready set",
			ProjectConfigError: "/repo/idle/.pop/config.toml: expected value",
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	snap.Tasks = td

	var out bytes.Buffer
	// Status is now Summary headline + dashboard table + drain.Scan errors (ADR-0121);
	// this snapshot exercises the Summary roll-up and the trailing drain.Scan errors
	// section (the table is fed the dashboard's rows by the command).
	RenderStatus(&out, snap, StatusTables{TaskSets: StatusTable{Kinds: (&drain.Deps{}).WorkKinds(nil), Rows: nil}})
	text := out.String()
	for _, want := range []string{
		"Summary:",
		"Work: 1 running, 1 queued",
		"Scan errors:",
		"idle: /repo/idle/.pop/config.toml: expected value",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("status output missing %q:\n%s", want, text)
		}
	}
	// The former per-bucket inventory sections are retired; only Summary, the
	// task-set table, and drain.Scan errors remain.
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

func TestRenderStatusShowsRecoveryWaiter(t *testing.T) {
	resetAt := time.Date(2026, 6, 15, 14, 0, 0, 0, time.UTC)
	td := queuetest.DataDeps(t)
	snap, err := drain.StatusFromDecisions(&drain.Deps{Tasks: td}, []drain.Decision{{
		Project:      "pop",
		Reason:       "set waiting for quota recovery",
		BlockedSetID: "set-1",
		WaitUntil:    resetAt,
		Deferral:     drain.SpawnDeferral{Reason: drain.DeferQuotaRecovery, SetID: "set-1", Until: resetAt},
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
	RenderStatus(&statusOut, snap, StatusTables{TaskSets: StatusTable{Kinds: (&drain.Deps{}).WorkKinds(nil), Rows: nil}})
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
	td := queuetest.DataDeps(t)
	waiter := map[string]tasks.RecoveryWaiter{
		"set-1": {SetID: "set-1", Preset: "codex", ResetAt: resetAt, RuntimePath: "/runtime/set-1"},
	}
	blocked := drain.BuildRunView(drain.StatusSnapshot{
		Tasks:           td,
		RecoveryWaiters: waiter,
	}, time.Now())
	cleared := drain.BuildRunView(drain.StatusSnapshot{Tasks: td}, time.Now())
	lines := drain.DiffRunView(&blocked, cleared)
	if len(lines) != 1 || !strings.Contains(lines[0], "quota recovery cleared") {
		t.Fatalf("recovery waiter cleared delta = %v", lines)
	}
}

func TestRenderStatusShowsCrashBackoffAndPark(t *testing.T) {
	repoKey := "test-repo"
	key := drain.SetScopedKey(repoKey, "set-1")
	td := queuetest.DataDeps(t)
	queuetest.SeedBindingStore(t, td, map[string]drain.WorktreeBinding{
		key: {
			Project:     "pop",
			RuntimePath: "/runtime/set-1",
			Branch:      "set-1",
		},
	})
	snap, err := drain.StatusFromDecisions(&drain.Deps{Tasks: td}, []drain.Decision{{
		Project:      "pop",
		Reason:       "set parked after repeated abnormal drain exits",
		BlockedSetID: "set-1",
		Deferral:     drain.SpawnDeferral{Reason: drain.DeferParked, SetID: "set-1"},
	}})
	if err != nil {
		t.Fatalf("status: %v", err)
	}
	snap.Tasks = td

	var statusOut bytes.Buffer
	// A parked set counts as blocked in the Summary roll-up; the retired
	// "Blocked:" / "Active worktrees:" inventory sections no longer render — the
	// dashboard row's STATUS cell carries the parked suffix (ADR-0121).
	RenderStatus(&statusOut, snap, StatusTables{TaskSets: StatusTable{Kinds: (&drain.Deps{}).WorkKinds(nil), Rows: nil}})
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
