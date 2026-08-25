package dashboard

import (
	"github.com/glebglazov/pop/internal/queuetest"
	"github.com/glebglazov/pop/tasks/drain"
	"path/filepath"
	"strings"
	"testing"

	"github.com/glebglazov/pop/project"
	"github.com/glebglazov/pop/tasks"
	"github.com/glebglazov/pop/tasks/setkind"
)

// TestDashboardVerifyVerbConditionalInclusion asserts the `v` verify verb is
// offered on exactly the rows a verdict can still move — the unverified and
// verify-failed marks, whatever status carries them — and is absent for every
// unmarked row, including while a live drain holds the set: the pane it opens
// waits for the checkout instead of being withheld (ADR-0238).
func TestDashboardVerifyVerbConditionalInclusion(t *testing.T) {
	has := func(row DashboardRow) bool {
		for _, item := range dashboardMenuItems(testKinds(), row) {
			if item.key == "V" && item.verb == setkind.VerbVerify {
				return true
			}
		}
		return false
	}

	// A human-completed set reads DONE and still owes a verdict — the verb must
	// follow the mark, not the status, or verification would be skipped for exactly
	// the set that deferred it.
	eligible := []DashboardRow{
		{ID: "s", RawStatus: tasks.StatusNeedsVerify, VerifyMark: tasks.VerifyMarkUnverified},
		{ID: "s", RawStatus: tasks.StatusVerifyFailed, VerifyMark: tasks.VerifyMarkFailed},
		{ID: "s", RawStatus: tasks.StatusDone, VerifyMark: tasks.VerifyMarkUnverified},
		{ID: "s", RawStatus: tasks.StatusDone, VerifyMark: tasks.VerifyMarkFailed},
		{ID: "s", RawStatus: tasks.StatusAwaitingApproval, VerifyMark: tasks.VerifyMarkUnverified},
	}
	for _, row := range eligible {
		if !has(row) {
			t.Fatalf("verify verb missing on a %s row marked %q", row.RawStatus, row.VerifyMark)
		}
	}

	ineligible := []tasks.TaskSetStatus{
		tasks.StatusReady, tasks.StatusFailed, tasks.StatusDone,
		tasks.StatusBlocked, tasks.StatusAwaitingApproval, tasks.StatusDeferred,
	}
	for _, st := range ineligible {
		if has(DashboardRow{ID: "s", RawStatus: st}) {
			t.Fatalf("verify verb present on an unmarked %s row", st)
		}
	}
	// A cleared set carries no work a verdict could move.
	if has(DashboardRow{ID: "s", RawStatus: tasks.StatusDone, VerifyMark: tasks.VerifyMarkVerified}) {
		t.Fatal("verify verb present on a verified DONE row")
	}

	// A live drain no longer withholds the verb: the Verifier takes the checkout
	// like anything else, so pressing it queues the pane behind the drain rather
	// than doing nothing (ADR-0238).
	for _, row := range eligible {
		row.LiveDrain = true
		if !has(row) {
			t.Fatalf("verify verb missing on a live-drained %s row", row.RawStatus)
		}
	}
}

// extractVerifySpawnCommand pulls the `pop tasks verify ...` command out of the
// send-keys the verify spawn issued into the set's pane.
func extractVerifySpawnCommand(rt *queuetest.RecordingTmux) (string, bool) {
	sendKeys, ok := rt.FindCommand("send-keys")
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

// TestDashboardVerifyLaunchPinsRuntimePath asserts drain.LaunchVerify spawns a pane whose
// command pins the row's runtime path via --task-runtime-path, and records no drain
// lock, spawn intent, or DrainPane: the claim belongs to the verify the pane runs,
// which takes and releases it itself (ADR-0238), not to the launcher.
func TestDashboardVerifyLaunchPinsRuntimePath(t *testing.T) {
	repo, setID, _ := queuetest.SetupSpawnRepo(t, "verify-pinned", []queuetest.SpawnTask{
		{ID: "01-a", File: "01-a.md", Title: "A", Type: "AFK", Status: "done"},
	})
	d, cfg, row, rt := dashboardLaunchFixture(t, repo, setID)
	row.RuntimePath = repo
	row.RawStatus = tasks.StatusNeedsVerify

	if _, err := drain.LaunchVerify(d, cfg, row); err != nil {
		t.Fatalf("drain.LaunchVerify: %v", err)
	}

	cmd, ok := extractVerifySpawnCommand(rt)
	if !ok {
		t.Fatalf("no verify spawn command recorded; commands=%v", rt.Commands)
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
	repo, setID, _ := queuetest.SetupSpawnRepo(t, "verify-plain", []queuetest.SpawnTask{
		{ID: "01-a", File: "01-a.md", Title: "A", Type: "AFK", Status: "done"},
	})
	d, cfg, row, rt := dashboardLaunchFixture(t, repo, setID)
	row.RawStatus = tasks.StatusVerifyFailed
	// RuntimePath left blank: no resolvable checkout.

	if _, err := drain.LaunchVerify(d, cfg, row); err != nil {
		t.Fatalf("drain.LaunchVerify: %v", err)
	}

	cmd, ok := extractVerifySpawnCommand(rt)
	if !ok {
		t.Fatalf("no verify spawn command recorded; commands=%v", rt.Commands)
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

// TestDashboardLaunchVerifyBoundCheckoutUsesCheckoutSession asserts verify panes
// for a bound non-trunk checkout open in that checkout's own session with the
// binding as cwd — never the originating project's session (ADR-0180).
func TestDashboardLaunchVerifyBoundCheckoutUsesCheckoutSession(t *testing.T) {
	repo, setID, _ := queuetest.SetupSpawnRepo(t, "verify-bound", []queuetest.SpawnTask{
		{ID: "01-a", File: "01-a.md", Title: "A", Type: "AFK", Status: "done"},
	})
	bound := filepath.Join(t.TempDir(), "verify-bound-wt")
	runGit(t, repo, "worktree", "add", "--detach", bound, "HEAD")
	d, cfg, row, rt := dashboardLaunchFixture(t, repo, setID)
	repoKey, err := drain.ResolveRepoKey(d, repo)
	if err != nil {
		t.Fatal(err)
	}
	queuetest.SeedBindingStore(t, d.Tasks, map[string]drain.WorktreeBinding{
		drain.SetScopedKey(repoKey, setID): {RuntimePath: bound, Branch: "verify-bound", Project: "pop", Provisioned: false},
	})
	row.RuntimePath = bound
	row.ProjectPath = repo
	row.RawStatus = tasks.StatusNeedsVerify

	if _, err := drain.LaunchVerify(d, cfg, row); err != nil {
		t.Fatalf("drain.LaunchVerify: %v", err)
	}
	assertSetPaneCheckoutSessionAndCwd(t, rt, repo, bound)
	assertVerifyRecordsNothing(t, d, repo)
}

// assertSetPaneCheckoutSessionAndCwd pins ADR-0180 at the verb: the pane's session
// is the one named after the checkout the set is bound to — created detached here,
// since the fixture's tmux has no session live — and never the originating
// project's.
func assertSetPaneCheckoutSessionAndCwd(t *testing.T, rt *queuetest.RecordingTmux, repo, checkout string) {
	t.Helper()
	wantSession := project.CheckoutSessionNameWith(project.DefaultDeps(), checkout)
	newSession, ok := rt.FindCommand("new-session")
	if !ok {
		t.Fatalf("expected the checkout's session to be created detached; commands=%v", rt.Commands)
	}
	if len(newSession) != 3 || newSession[1] != wantSession {
		t.Fatalf("new-session = %v, want session %q", newSession, wantSession)
	}
	if got := newSession[2]; canonPath(t, got) != canonPath(t, checkout) {
		t.Fatalf("new-session cwd = %q, want checkout %q", got, checkout)
	}
	projectSession := project.CheckoutSessionNameWith(project.DefaultDeps(), repo)
	if projectSession != wantSession && newSession[1] == projectSession {
		t.Fatalf("must not target the originating project session %q", projectSession)
	}
	newWindow, ok := rt.FindCommand("new-window")
	if !ok {
		t.Fatalf("expected pop-work window; commands=%v", rt.Commands)
	}
	cwd := newWindowCwd(newWindow)
	if cwd == "" {
		t.Fatalf("new-window missing -c cwd: %v", newWindow)
	}
	if canonPath(t, cwd) != canonPath(t, checkout) {
		t.Fatalf("new-window cwd = %q, want checkout %q", cwd, checkout)
	}
}

func newWindowCwd(args []string) string {
	for i := 0; i < len(args)-1; i++ {
		if args[i] == "-c" {
			return args[i+1]
		}
	}
	return ""
}

func canonPath(t *testing.T, path string) string {
	t.Helper()
	got, err := filepath.EvalSymlinks(path)
	if err != nil {
		return filepath.Clean(path)
	}
	return got
}

// assertVerifyRecordsNothing checks the verify *spawn* left no drain lock, spawn
// intent, or DrainPane behind — the row's ● indicator must stay dark. What the
// spawned command then takes for itself is the Verifier's own claim, released
// when its verdict is in hand.
func assertVerifyRecordsNothing(t *testing.T, d *drain.Deps, repo string) {
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
