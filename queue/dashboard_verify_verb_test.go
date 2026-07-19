package queue

import (
	"strings"
	"testing"

	"github.com/glebglazov/pop/tasks"
)

// TestDashboardVerifyVerbConditionalInclusion asserts the `v` verify verb (ADR-0123)
// is offered only on rows a verdict can still move — NEEDS-VERIFY and VERIFY-FAILED —
// and is absent for every other status and whenever a live drain holds the set.
func TestDashboardVerifyVerbConditionalInclusion(t *testing.T) {
	has := func(row DashboardRow) bool {
		for _, item := range dashboardMenuItems(row) {
			if item.key == "v" && item.action == menuActionVerify {
				return true
			}
		}
		return false
	}

	eligible := []tasks.TaskSetStatus{tasks.StatusNeedsVerify, tasks.StatusVerifyFailed}
	for _, st := range eligible {
		if !has(DashboardRow{SetRef: SetRef{SetID: "s", RawStatus: st}}) {
			t.Fatalf("verify verb missing on a %s row", st)
		}
	}

	ineligible := []tasks.TaskSetStatus{
		tasks.StatusReady, tasks.StatusFailed, tasks.StatusDone,
		tasks.StatusBlocked, tasks.StatusAwaitingApproval, tasks.StatusDeferred,
	}
	for _, st := range ineligible {
		if has(DashboardRow{SetRef: SetRef{SetID: "s", RawStatus: st}}) {
			t.Fatalf("verify verb present on a %s row", st)
		}
	}

	// A live drain hides the verb even on an otherwise-eligible row: a plain verify
	// is not quiescence-gated, so the running drain verifies itself.
	for _, st := range eligible {
		if has(DashboardRow{SetRef: SetRef{SetID: "s", RawStatus: st, LiveDrain: true}}) {
			t.Fatalf("verify verb present on a live-drained %s row", st)
		}
	}
}

// extractVerifySpawnCommand pulls the `pop tasks verify ...` command out of the
// send-keys the verify spawn issued into the set's pane.
func extractVerifySpawnCommand(rt *recordingTmux) (string, bool) {
	sendKeys, ok := rt.findCommand("send-keys")
	if !ok {
		return "", false
	}
	joined := strings.Join(sendKeys, " ")
	idx := strings.Index(joined, "pop tasks verify ")
	if idx < 0 {
		return "", false
	}
	cmd := joined[idx:]
	if end := strings.Index(cmd, " Enter"); end >= 0 {
		cmd = cmd[:end]
	}
	return cmd, true
}

// TestDashboardVerifyLaunchPinsRuntimePath asserts LaunchVerify spawns a pane whose
// command pins the row's runtime path via --task-runtime-path, and records no drain
// lock, spawn intent, or DrainPane (verify is not a drain — ADR-0123).
func TestDashboardVerifyLaunchPinsRuntimePath(t *testing.T) {
	repo, setID, _ := setupSupervisorSpawnRepo(t, "verify-pinned", []spawnTestTask{
		{ID: "01-a", File: "01-a.md", Title: "A", Type: "AFK", Status: "done"},
	})
	d, cfg, row, rt := dashboardLaunchFixture(t, repo, setID)
	row.RuntimePath = repo
	row.RawStatus = tasks.StatusNeedsVerify

	if _, err := LaunchVerify(d, cfg, row.SetRef); err != nil {
		t.Fatalf("LaunchVerify: %v", err)
	}

	cmd, ok := extractVerifySpawnCommand(rt)
	if !ok {
		t.Fatalf("no verify spawn command recorded; commands=%v", rt.commands)
	}
	want := "pop tasks verify " + setID + " --task-runtime-path " + repo
	if cmd != want {
		t.Fatalf("verify command = %q, want %q", cmd, want)
	}

	assertVerifyRecordsNothing(t, d, repo)
}

// TestDashboardVerifyLaunchOmitsFlagWithoutRuntimePath asserts a row with no
// resolvable runtime path spawns a plain `pop tasks verify <set>` (the flag is
// omitted; pop tasks verify defaults to the project root).
func TestDashboardVerifyLaunchOmitsFlagWithoutRuntimePath(t *testing.T) {
	repo, setID, _ := setupSupervisorSpawnRepo(t, "verify-plain", []spawnTestTask{
		{ID: "01-a", File: "01-a.md", Title: "A", Type: "AFK", Status: "done"},
	})
	d, cfg, row, rt := dashboardLaunchFixture(t, repo, setID)
	row.RawStatus = tasks.StatusVerifyFailed
	// RuntimePath left blank: no resolvable checkout.

	if _, err := LaunchVerify(d, cfg, row.SetRef); err != nil {
		t.Fatalf("LaunchVerify: %v", err)
	}

	cmd, ok := extractVerifySpawnCommand(rt)
	if !ok {
		t.Fatalf("no verify spawn command recorded; commands=%v", rt.commands)
	}
	want := "pop tasks verify " + setID
	if cmd != want {
		t.Fatalf("verify command = %q, want %q (no --task-runtime-path)", cmd, want)
	}
	if strings.Contains(cmd, "--task-runtime-path") {
		t.Fatalf("verify command pinned a runtime path without one: %q", cmd)
	}

	assertVerifyRecordsNothing(t, d, repo)
}

// assertVerifyRecordsNothing checks the verify spawn left no drain lock, spawn
// intent, or DrainPane behind — the row's ● indicator must stay dark and `p`
// (preview) must not reach the verify pane.
func assertVerifyRecordsNothing(t *testing.T, d *Deps, repo string) {
	t.Helper()
	panes, err := tasks.AllDrainPanes(d.Tasks)
	if err != nil {
		t.Fatal(err)
	}
	if len(panes) != 0 {
		t.Fatalf("verify recorded a DrainPane: %+v", panes)
	}
	drains, err := tasks.LiveRunningDrains(d.Tasks)
	if err != nil {
		t.Fatal(err)
	}
	if len(drains) != 0 {
		t.Fatalf("verify recorded a running-drain lock: %+v", drains)
	}
	id, err := tasks.ResolveRepositoryIdentity(d.Tasks, repo)
	if err != nil {
		t.Fatal(err)
	}
	intents, err := tasks.PendingSpawns(d.Tasks, id.CommonDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(intents) != 0 {
		t.Fatalf("verify recorded a spawn intent: %+v", intents)
	}
}
