package tasks

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/glebglazov/pop/store"
)

// repoAndHead resolves the canonical repository identity and current HEAD for a
// fixture, so verdict seeding and invalidation tests key on the same facts the
// verify status path uses.
func repoAndHead(t *testing.T, d *Deps, repoRoot string) (string, string) {
	t.Helper()
	id, err := ResolveRepositoryIdentity(d, repoRoot)
	if err != nil {
		t.Fatalf("ResolveRepositoryIdentity: %v", err)
	}
	out, err := d.Git.CommandInDir(repoRoot, "rev-parse", "HEAD")
	if err != nil {
		t.Fatalf("rev-parse HEAD: %v", err)
	}
	return id.CommonDir, strings.TrimSpace(out)
}

// openStore opens the drain store for a fixture that has already had it created.
func openStore(t *testing.T, d *Deps) *store.Store {
	t.Helper()
	s, err := store.Open(DrainStorePathWith(d), func(int, string) bool { return true })
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	return s
}

// seedVerifyVerdict writes a verdict into the fixture's drain store.
func seedVerifyVerdict(t *testing.T, d *Deps, v store.VerifyVerdict) {
	t.Helper()
	s, err := openDrainStore(d)
	if err != nil {
		t.Fatalf("open drain store: %v", err)
	}
	defer func() { _ = d.CloseStore() }()
	if v.ComputedAt.IsZero() {
		v.ComputedAt = time.Unix(1, 0).UTC()
	}
	if err := s.PutVerifyVerdict(v); err != nil {
		t.Fatalf("PutVerifyVerdict: %v", err)
	}
}

// commitNewFile makes a new commit in repoRoot so HEAD moves; used to create a
// fresh work SHA for post-invalidation status derivation.
func commitNewFile(t *testing.T, repoRoot, name, content string) {
	t.Helper()
	path := filepath.Join(repoRoot, name)
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}
	if err := exec.Command("git", "-C", repoRoot, "add", name).Run(); err != nil {
		t.Fatalf("git add: %v", err)
	}
	if err := exec.Command("git", "-C", repoRoot, "commit", "-m", "move head").Run(); err != nil {
		t.Fatalf("git commit: %v", err)
	}
}

// TestResetTaskInvalidatesVerifyVerdicts: reopening a done task deletes every
// cached Verify verdict for the set, ending the verification episode.
func TestResetTaskInvalidatesVerifyVerdicts(t *testing.T) {
	env := setupCustomTaskFixture(t, []Task{
		{ID: "01-a", File: "01-a.md", Title: "A", Type: "AFK", Status: "done"},
	})
	d := env.deps()
	repo, _ := repoAndHead(t, d, env.root)
	seedVerifyVerdict(t, d, store.VerifyVerdict{Repo: repo, SetID: "demo", WorkSHA: "sha1", Verdict: "PASS"})
	seedVerifyVerdict(t, d, store.VerifyVerdict{Repo: repo, SetID: "demo", WorkSHA: "sha2", Verdict: "FIXABLE", Findings: "x"})

	_, err := ResetTaskWith(d, nil, nil, ResetTaskOptions{
		ResolveInput: ResolveInput{CWD: env.root},
		TaskPath:     env.demoTaskRef(t, "01-a.md"),
	})
	if err != nil {
		t.Fatalf("ResetTaskWith: %v", err)
	}

	s := openStore(t, d)
	defer func() { _ = s.Close() }()
	got, err := s.GetLatestPassVerifyVerdict(repo, "demo")
	if err != nil {
		t.Fatalf("GetLatestPassVerifyVerdict: %v", err)
	}
	if got != nil {
		t.Fatalf("expected verdicts invalidated, got %+v", got)
	}
}

// TestOpenTasksInvalidatesVerifyVerdicts: batch-reopening tasks deletes every
// cached Verify verdict for the set.
func TestOpenTasksInvalidatesVerifyVerdicts(t *testing.T) {
	env := setupCustomTaskFixture(t, []Task{
		{ID: "01-a", File: "01-a.md", Title: "A", Type: "AFK", Status: "done"},
		{ID: "02-b", File: "02-b.md", Title: "B", Type: "AFK", Status: "skipped"},
	})
	d := env.deps()
	repo, _ := repoAndHead(t, d, env.root)
	seedVerifyVerdict(t, d, store.VerifyVerdict{Repo: repo, SetID: "demo", WorkSHA: "sha1", Verdict: "PASS"})

	_, err := OpenTasksWith(d, nil, nil, OpenTasksOptions{
		ResolveInput:    ResolveInput{CWD: env.root},
		TaskSetTarget:   "demo",
		SelectedTaskIDs: []string{"01-a", "02-b"},
	})
	if err != nil {
		t.Fatalf("OpenTasksWith: %v", err)
	}

	s := openStore(t, d)
	defer func() { _ = s.Close() }()
	got, err := s.GetLatestPassVerifyVerdict(repo, "demo")
	if err != nil {
		t.Fatalf("GetLatestPassVerifyVerdict: %v", err)
	}
	if got != nil {
		t.Fatalf("expected verdicts invalidated, got %+v", got)
	}
}

// TestResetHITLTaskLeavesVerifyVerdicts: reopening a HITL task never ends the
// verification episode (ADR-0109) — the Verifier judges only done-AFK work
// (ADR-0102), so a cached PASS stands across a HITL reopen.
func TestResetHITLTaskLeavesVerifyVerdicts(t *testing.T) {
	env := setupCustomTaskFixture(t, []Task{
		{ID: "01-a", File: "01-a.md", Title: "A", Type: "AFK", Status: "done"},
		{ID: "02-h", File: "02-h.md", Title: "Sign off", Type: "HITL", Status: "done"},
	})
	d := env.deps()
	repo, _ := repoAndHead(t, d, env.root)
	seedVerifyVerdict(t, d, store.VerifyVerdict{Repo: repo, SetID: "demo", WorkSHA: "sha1", Verdict: "PASS"})

	_, err := ResetTaskWith(d, nil, nil, ResetTaskOptions{
		ResolveInput: ResolveInput{CWD: env.root},
		TaskPath:     env.demoTaskRef(t, "02-h.md"),
	})
	if err != nil {
		t.Fatalf("ResetTaskWith: %v", err)
	}

	s := openStore(t, d)
	defer func() { _ = s.Close() }()
	got, err := s.GetLatestPassVerifyVerdict(repo, "demo")
	if err != nil {
		t.Fatalf("GetLatestPassVerifyVerdict: %v", err)
	}
	if got == nil {
		t.Fatal("expected the cached PASS to survive a HITL reopen, got nil")
	}
}

// TestCompleteSkippedAFKTaskInvalidatesVerifyVerdicts: manually completing a
// skipped AFK task (skipped→done) ends the episode (ADR-0109), closing the hole
// where an unjudged done-AFK body would otherwise sit under an immunized PASS.
func TestCompleteSkippedAFKTaskInvalidatesVerifyVerdicts(t *testing.T) {
	env := setupCustomTaskFixture(t, []Task{
		{ID: "01-a", File: "01-a.md", Title: "A", Type: "AFK", Status: "skipped"},
	})
	d := env.deps()
	repo, _ := repoAndHead(t, d, env.root)
	seedVerifyVerdict(t, d, store.VerifyVerdict{Repo: repo, SetID: "demo", WorkSHA: "sha1", Verdict: "PASS"})

	if _, err := CompleteTaskWith(d, nil, nil, CompleteTaskOptions{
		ResolveInput: ResolveInput{CWD: env.root},
		TaskPath:     env.demoTaskRef(t, "01-a.md"),
	}); err != nil {
		t.Fatalf("CompleteTaskWith: %v", err)
	}

	s := openStore(t, d)
	defer func() { _ = s.Close() }()
	got, err := s.GetLatestPassVerifyVerdict(repo, "demo")
	if err != nil {
		t.Fatalf("GetLatestPassVerifyVerdict: %v", err)
	}
	if got != nil {
		t.Fatalf("expected verdicts invalidated on skipped→done, got %+v", got)
	}
}

// TestFinalizeTaskDoneAFKNoOpMidDrain: the executor's open→done routes through
// the same ADR-0109 invalidation rule as a manual completion — it is not
// special-cased — but is a no-op mid-drain. An open AFK task means no verified
// episode is in flight, so the set holds no cached verdict to clear; a
// co-resident set's verdict is untouched and the completion succeeds.
func TestFinalizeTaskDoneAFKNoOpMidDrain(t *testing.T) {
	env := setupCustomTaskFixture(t, []Task{
		{ID: "01-a", File: "01-a.md", Title: "A", Type: "AFK", Status: "open"},
	})
	d := env.deps()
	repo, _ := repoAndHead(t, d, env.root)
	// A live store holding an unrelated set's verdict — nothing for "demo".
	seedVerifyVerdict(t, d, store.VerifyVerdict{Repo: repo, SetID: "other", WorkSHA: "sha1", Verdict: "PASS"})

	refresh, err := RefreshWith(d, env.tasksDir, StatePathFor(env.tasksDir))
	if err != nil {
		t.Fatalf("RefreshWith: %v", err)
	}
	m := refresh.Manifests["demo"]
	if m == nil {
		t.Fatal("refresh missing demo manifest")
	}
	sel := &Selection{TaskSetID: "demo", TaskID: "01-a", Manifest: m}
	if err := finalizeTaskDone(d, sel, env.root, "done"); err != nil {
		t.Fatalf("finalizeTaskDone: %v", err)
	}

	s := openStore(t, d)
	defer func() { _ = s.Close() }()
	if got, err := s.GetLatestPassVerifyVerdict(repo, "demo"); err != nil {
		t.Fatalf("GetLatestPassVerifyVerdict(demo): %v", err)
	} else if got != nil {
		t.Fatalf("demo had no cached verdict mid-drain, got %+v", got)
	}
	if got, err := s.GetLatestPassVerifyVerdict(repo, "other"); err != nil {
		t.Fatalf("GetLatestPassVerifyVerdict(other): %v", err)
	} else if got == nil {
		t.Fatal("an unrelated set's verdict must survive the executor completion")
	}
}

// TestSpawnRemediationTaskInvalidatesVerifyVerdicts: creating a Remediation
// task deletes the cached Verify verdicts for the set, so the Verifier re-fires
// against the new work SHA after the remediation is drained.
func TestSpawnRemediationTaskInvalidatesVerifyVerdicts(t *testing.T) {
	d, m := setupDrainVerifyFixture(t, stubGit("sha1\n", "", ""), doneAFKSet(), nil)
	repo := "/repo/.git"
	seedVerifyVerdict(t, d, store.VerifyVerdict{Repo: repo, SetID: "demo", WorkSHA: "sha1", Verdict: "PASS"})

	_, err := spawnRemediationTask(d, m, repo, "sha1", "criterion 2 unmet", "", RemediationOriginAuto)
	if err != nil {
		t.Fatalf("spawnRemediationTask: %v", err)
	}

	s := openStore(t, d)
	defer func() { _ = s.Close() }()
	got, err := s.GetLatestPassVerifyVerdict(repo, "demo")
	if err != nil {
		t.Fatalf("GetLatestPassVerifyVerdict: %v", err)
	}
	if got != nil {
		t.Fatalf("expected verdicts invalidated, got %+v", got)
	}
}

// TestResetTaskInvalidationLeavesSetNeedsVerifyAfterHEADMoves: a terminal set
// with a cached PASS verdict is immunized against a moved HEAD; after reopening
// invalidates that PASS, the set (once completed again at the new SHA) derives
// NEEDS-VERIFY until a fresh PASS is recorded (ADR-0096).
func TestResetTaskInvalidationLeavesSetNeedsVerifyAfterHEADMoves(t *testing.T) {
	env := setupCustomTaskFixture(t, []Task{
		{ID: "01-a", File: "01-a.md", Title: "A", Type: "AFK", Status: "done"},
	})
	d := env.deps()
	repo, head := repoAndHead(t, d, env.root)
	seedVerifyVerdict(t, d, store.VerifyVerdict{Repo: repo, SetID: "demo", WorkSHA: head, Verdict: "PASS"})

	// Move HEAD so the old PASS no longer applies to the current work SHA.
	commitNewFile(t, env.root, "post-pass.txt", "x\n")

	// Reopen invalidates the cached PASS.
	_, err := ResetTaskWith(d, nil, nil, ResetTaskOptions{
		ResolveInput: ResolveInput{CWD: env.root},
		TaskPath:     env.demoTaskRef(t, "01-a.md"),
	})
	if err != nil {
		t.Fatalf("ResetTaskWith: %v", err)
	}

	// Complete the task again, returning the set to the terminal zone at the
	// new HEAD with no PASS in the store.
	if _, err := CompleteTaskWith(d, nil, nil, CompleteTaskOptions{
		ResolveInput: ResolveInput{CWD: env.root},
		TaskPath:     env.demoTaskRef(t, "01-a.md"),
	}); err != nil {
		t.Fatalf("CompleteTaskWith: %v", err)
	}

	refresh, err := RefreshWith(d, env.tasksDir, StatePathFor(env.tasksDir))
	if err != nil {
		t.Fatalf("RefreshWith: %v", err)
	}
	ApplyVerifyVerdicts(d, refresh, verifyEnabledConfig(), env.root)

	row := findRow(refresh, "demo")
	if row == nil {
		t.Fatal("refresh missing demo row")
	}
	if row.Status != StatusNeedsVerify {
		t.Fatalf("status = %q, want NEEDS-VERIFY", row.Status)
	}
}

// TestSpawnRemediationTaskInvalidationBestEffortNoStore: when no store exists,
// spawning a Remediation task with a repo still succeeds (invalidation is
// best-effort).
func TestSpawnRemediationTaskInvalidationBestEffortNoStore(t *testing.T) {
	d, m := setupDrainVerifyFixture(t, stubGit("sha1\n", "", ""), doneAFKSet(), nil)
	// repo is set but the store was never created; invalidation must not fail.
	_, err := spawnRemediationTask(d, m, "/repo/.git", "sha1", "criterion 2 unmet", "", RemediationOriginAuto)
	if err != nil {
		t.Fatalf("spawnRemediationTask with no store: %v", err)
	}
}
