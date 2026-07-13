package tasks

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/glebglazov/pop/config"
	"github.com/glebglazov/pop/internal/deps"
	"github.com/glebglazov/pop/store"
)

// setupDrainVerifyFixture writes a "demo" set with the given tasks (and optional
// set-level keys, e.g. {"verify": false}) under a temp definition path with the
// data dir isolated, then loads and returns the validated manifest. It is the
// drain-phase counterpart to setupVerifyFixture: drainVerifyPhase takes the
// manifest directly rather than re-discovering it.
func setupDrainVerifyFixture(t *testing.T, git *deps.MockGit, tasks []Task, setKeys map[string]any) (*Deps, *Manifest) {
	t.Helper()
	root := t.TempDir()
	t.Setenv("XDG_DATA_HOME", filepath.Join(root, ".xdg"))
	taskDir := filepath.Join(root, "tasks", "demo")
	for _, task := range tasks {
		writeTaskMD(t, taskDir, task.File, "## Acceptance criteria\n\n- [ ] ok\n")
	}
	writeManifestWithSetKeys(t, taskDir, tasks, setKeys)
	d := &Deps{FS: deps.NewRealFileSystem(), Git: git}
	m := LoadManifest(d, "demo", filepath.Join(taskDir, "index.json"))
	if !m.Valid {
		t.Fatalf("manifest invalid: %v", m.Errors)
	}
	return d, m
}

// seedVerdict writes a verdict directly into the Drain store so a cache-first
// read can find it.
func seedVerdict(t *testing.T, d *Deps, v store.VerifyVerdict) {
	t.Helper()
	s, err := openDrainStore(d)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer func() { _ = s.Close() }()
	if v.ComputedAt.IsZero() {
		v.ComputedAt = time.Unix(1, 0).UTC()
	}
	if err := s.PutVerifyVerdict(v); err != nil {
		t.Fatalf("PutVerifyVerdict: %v", err)
	}
}

func doneAFKSet() []Task {
	return []Task{{ID: "01-a", File: "01-a.md", Title: "A", Type: "AFK", Status: "done"}}
}

// openAFKSet is a single open AFK task: draining it to completion exhausts the
// set in-loop and lands on the pre-approval Verifier phase. An explicitly
// targeted already-DONE set is refused before the loop, so the verify phase is
// only reachable by draining open work to exhaustion.
func openAFKSet() []Task {
	return []Task{{ID: "01-a", File: "01-a.md", Title: "A", Type: "AFK", Status: "open"}}
}

func terminalHITLSet() []Task {
	return []Task{
		{ID: "01-a", File: "01-a.md", Title: "A", Type: "AFK", Status: "done"},
		{ID: "02-h", File: "02-h.md", Title: "Sign off", Type: "HITL", Status: "open"},
	}
}

// TestBuildVerifierPromptScopesToDoneAFK: the Verifier prompt carries only the
// set's `done` AFK tasks (ADR-0102). Open/not-`done` AFK tasks and HITL tasks of
// any status are omitted from the judged criteria — an agent cannot judge a
// human sign-off, and a not-yet-run task is not an unmet criterion.
func TestBuildVerifierPromptScopesToDoneAFK(t *testing.T) {
	mixed := []Task{
		{ID: "01-afk-done", File: "01-afk-done.md", Title: "Done AFK", Type: "AFK", Status: "done"},
		{ID: "02-afk-open", File: "02-afk-open.md", Title: "Open AFK", Type: "AFK", Status: "open"},
		{ID: "03-hitl-open", File: "03-hitl-open.md", Title: "Sign off", Type: "HITL", Status: "open"},
		{ID: "04-hitl-done", File: "04-hitl-done.md", Title: "Approved", Type: "HITL", Status: "done"},
	}
	d, m := setupDrainVerifyFixture(t, stubGit("sha1\n", "", ""), mixed, nil)

	prompt := buildVerifierPrompt(d, m, "sha1", "", "")

	if !strings.Contains(prompt, "01-afk-done") {
		t.Fatalf("prompt must include the done AFK task:\n%s", prompt)
	}
	for _, omitted := range []string{"02-afk-open", "03-hitl-open", "04-hitl-done", "[HITL]"} {
		if strings.Contains(prompt, omitted) {
			t.Fatalf("prompt must omit %q (only done AFK work is judged):\n%s", omitted, prompt)
		}
	}
}

// TestDrainVerifyPhasePassReachesDone: a PASS verdict on a pure-AFK exhausted
// set lets the drain reach DONE, and the verdict is recorded at the work SHA.
func TestDrainVerifyPhasePassReachesDone(t *testing.T) {
	d, m := setupDrainVerifyFixture(t, stubGit("sha1\n", "", ""), doneAFKSet(), nil)
	called := false
	status, verdict, err := drainVerifyPhase(d, nil, verifyCoreOptions{
		Repo: "/repo/.git", RuntimePath: "/rt", SetID: "demo", Output: &bytes.Buffer{},
		runVerifier: func(string) (string, error) { called = true; return "VERDICT: PASS\n", nil },
	}, m, StatusDone)
	if err != nil {
		t.Fatalf("drainVerifyPhase: %v", err)
	}
	if !called {
		t.Fatal("Verifier was not invoked on a cache miss")
	}
	if status != StatusDone {
		t.Fatalf("status = %q, want DONE", status)
	}
	if verdict == nil || verdict.Verdict != "PASS" {
		t.Fatalf("verdict = %+v, want PASS", verdict)
	}
	if stored := readStoredVerdict(t, d, "/repo/.git", "demo", "sha1"); stored == nil || stored.Verdict != "PASS" {
		t.Fatalf("stored verdict = %+v, want PASS at sha1", stored)
	}
}

// TestDrainVerifyPhasePassReachesAwaitingApproval: a PASS verdict on a set whose
// only remaining work is a terminal HITL approval keeps it at AWAITING-APPROVAL.
func TestDrainVerifyPhasePassReachesAwaitingApproval(t *testing.T) {
	d, m := setupDrainVerifyFixture(t, stubGit("sha1\n", "", ""), terminalHITLSet(), nil)
	if base := DeriveStatus(m); base != StatusAwaitingApproval {
		t.Fatalf("fixture base status = %q, want AWAITING-APPROVAL", base)
	}
	status, _, err := drainVerifyPhase(d, nil, verifyCoreOptions{
		Repo: "/repo/.git", RuntimePath: "/rt", SetID: "demo", Output: &bytes.Buffer{},
		runVerifier: func(string) (string, error) { return "VERDICT: PASS\n", nil },
	}, m, StatusAwaitingApproval)
	if err != nil {
		t.Fatalf("drainVerifyPhase: %v", err)
	}
	if status != StatusAwaitingApproval {
		t.Fatalf("status = %q, want AWAITING-APPROVAL", status)
	}
}

// TestDrainVerifyPhaseNonPassParks: a FIXABLE or NEEDS-HUMAN verdict resolves to
// VERIFY-FAILED, parking the set, with the findings carried through.
func TestDrainVerifyPhaseNonPassParks(t *testing.T) {
	for _, tc := range []struct {
		name    string
		raw     string
		verdict string
	}{
		{"fixable", "VERDICT: FIXABLE\nFINDINGS: criterion 2 unmet\n", "FIXABLE"},
		{"needs-human", "VERDICT: NEEDS-HUMAN\nFINDINGS: the spec is ambiguous\n", "NEEDS-HUMAN"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d, m := setupDrainVerifyFixture(t, stubGit("sha1\n", "", ""), doneAFKSet(), nil)
			status, verdict, err := drainVerifyPhase(d, nil, verifyCoreOptions{
				Repo: "/repo/.git", RuntimePath: "/rt", SetID: "demo", Output: &bytes.Buffer{},
				runVerifier: func(string) (string, error) { return tc.raw, nil },
			}, m, StatusDone)
			if err != nil {
				t.Fatalf("drainVerifyPhase: %v", err)
			}
			if status != StatusVerifyFailed {
				t.Fatalf("status = %q, want VERIFY-FAILED", status)
			}
			if verdict == nil || verdict.Verdict != tc.verdict {
				t.Fatalf("verdict = %+v, want %s", verdict, tc.verdict)
			}
			if verdict.Findings == "" {
				t.Fatal("findings should carry the Verifier's reasons")
			}
		})
	}
}

// TestDrainVerifyPhaseReusesCachedVerdict: a verdict already stored at the
// current work SHA is reused, so the Verifier is not re-invoked — a re-drain at
// unchanged work does not loop.
func TestDrainVerifyPhaseReusesCachedVerdict(t *testing.T) {
	d, m := setupDrainVerifyFixture(t, stubGit("sha1\n", "", ""), doneAFKSet(), nil)
	seedVerdict(t, d, store.VerifyVerdict{Repo: "/repo/.git", SetID: "demo", WorkSHA: "sha1", Verdict: "PASS"})
	status, verdict, err := drainVerifyPhase(d, nil, verifyCoreOptions{
		Repo: "/repo/.git", RuntimePath: "/rt", SetID: "demo", Output: &bytes.Buffer{},
		runVerifier: func(string) (string, error) {
			t.Fatal("Verifier re-invoked despite a cached verdict")
			return "", nil
		},
	}, m, StatusDone)
	if err != nil {
		t.Fatalf("drainVerifyPhase: %v", err)
	}
	if status != StatusDone || verdict == nil || verdict.Verdict != "PASS" {
		t.Fatalf("status/verdict = %q/%+v, want DONE/PASS from cache", status, verdict)
	}
}

// TestDrainVerifyPhaseStaleNonPassReRuns: a non-PASS verdict stored at a
// different (stale) work SHA does not immunize the set, so the Verifier runs
// and records a fresh verdict at the current SHA.
func TestDrainVerifyPhaseStaleNonPassReRuns(t *testing.T) {
	d, m := setupDrainVerifyFixture(t, stubGit("shaNEW\n", "", ""), doneAFKSet(), nil)
	seedVerdict(t, d, store.VerifyVerdict{Repo: "/repo/.git", SetID: "demo", WorkSHA: "shaOLD", Verdict: "NEEDS-HUMAN", Findings: "stale"})
	called := false
	status, verdict, err := drainVerifyPhase(d, nil, verifyCoreOptions{
		Repo: "/repo/.git", RuntimePath: "/rt", SetID: "demo", Output: &bytes.Buffer{},
		runVerifier: func(string) (string, error) { called = true; return "VERDICT: NEEDS-HUMAN\nFINDINGS: x\n", nil },
	}, m, StatusDone)
	if err != nil {
		t.Fatalf("drainVerifyPhase: %v", err)
	}
	if !called {
		t.Fatal("Verifier not invoked despite a stale non-PASS cached verdict")
	}
	if status != StatusVerifyFailed || verdict == nil || verdict.Verdict != "NEEDS-HUMAN" {
		t.Fatalf("status/verdict = %q/%+v, want VERIFY-FAILED/NEEDS-HUMAN", status, verdict)
	}
	if stored := readStoredVerdict(t, d, "/repo/.git", "demo", "shaNEW"); stored == nil || stored.Verdict != "NEEDS-HUMAN" {
		t.Fatalf("stored verdict at shaNEW = %+v, want NEEDS-HUMAN", stored)
	}
}

// TestDrainVerifyPhaseImmunizingPassAtOldSHANoRun: a PASS verdict stored at an
// older work SHA immunizes the set (ADR-0096), so the drain reuses it without
// re-invoking the Verifier even though HEAD has moved on.
func TestDrainVerifyPhaseImmunizingPassAtOldSHANoRun(t *testing.T) {
	d, m := setupDrainVerifyFixture(t, stubGit("shaNEW\n", "", ""), doneAFKSet(), nil)
	seedVerdict(t, d, store.VerifyVerdict{Repo: "/repo/.git", SetID: "demo", WorkSHA: "shaOLD", Verdict: "PASS"})
	status, verdict, err := drainVerifyPhase(d, nil, verifyCoreOptions{
		Repo: "/repo/.git", RuntimePath: "/rt", SetID: "demo", Output: &bytes.Buffer{},
		runVerifier: func(string) (string, error) {
			t.Fatal("Verifier re-invoked despite an immunizing PASS at an older SHA")
			return "", nil
		},
	}, m, StatusDone)
	if err != nil {
		t.Fatalf("drainVerifyPhase: %v", err)
	}
	if status != StatusDone || verdict == nil || verdict.Verdict != "PASS" || verdict.WorkSHA != "shaOLD" {
		t.Fatalf("status/verdict = %q/%+v, want DONE/PASS at shaOLD", status, verdict)
	}
	// No fresh verdict is written at the new SHA; the immunizing PASS remains.
	if stored := readStoredVerdict(t, d, "/repo/.git", "demo", "shaNEW"); stored != nil {
		t.Fatalf("unexpected stored verdict at shaNEW: %+v", stored)
	}
}

// TestDrainVerifyPhaseAfterInvalidationRunsAgain: invalidating the verify
// verdicts for a set (e.g., on reopen/remediation, ADR-0096) removes the
// immunizing PASS, so the next terminal drain re-invokes the Verifier.
func TestDrainVerifyPhaseAfterInvalidationRunsAgain(t *testing.T) {
	d, m := setupDrainVerifyFixture(t, stubGit("shaNEW\n", "", ""), doneAFKSet(), nil)
	seedVerdict(t, d, store.VerifyVerdict{Repo: "/repo/.git", SetID: "demo", WorkSHA: "shaOLD", Verdict: "PASS"})

	s, err := openDrainStore(d)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	if err := s.InvalidateVerifyVerdicts("/repo/.git", "demo"); err != nil {
		t.Fatalf("InvalidateVerifyVerdicts: %v", err)
	}
	_ = s.Close()

	called := false
	status, verdict, err := drainVerifyPhase(d, nil, verifyCoreOptions{
		Repo: "/repo/.git", RuntimePath: "/rt", SetID: "demo", Output: &bytes.Buffer{},
		runVerifier: func(string) (string, error) { called = true; return "VERDICT: PASS\n", nil },
	}, m, StatusDone)
	if err != nil {
		t.Fatalf("drainVerifyPhase: %v", err)
	}
	if !called {
		t.Fatal("Verifier not invoked after invalidation removed the immunizing PASS")
	}
	if status != StatusDone || verdict == nil || verdict.Verdict != "PASS" || verdict.WorkSHA != "shaNEW" {
		t.Fatalf("status/verdict = %q/%+v, want DONE/PASS at shaNEW", status, verdict)
	}
}

// TestDrainVerifyPhaseNonTerminalPassthrough: a status outside the terminal zone
// is returned unchanged with no verdict and no Verifier call.
func TestDrainVerifyPhaseNonTerminalPassthrough(t *testing.T) {
	d, m := setupDrainVerifyFixture(t, stubGit("sha1\n", "", ""), doneAFKSet(), nil)
	status, verdict, err := drainVerifyPhase(d, nil, verifyCoreOptions{
		Repo: "/repo/.git", RuntimePath: "/rt", SetID: "demo", Output: &bytes.Buffer{},
		runVerifier: func(string) (string, error) {
			t.Fatal("Verifier invoked for a non-terminal status")
			return "", nil
		},
	}, m, StatusBlocked)
	if err != nil {
		t.Fatalf("drainVerifyPhase: %v", err)
	}
	if status != StatusBlocked || verdict != nil {
		t.Fatalf("status/verdict = %q/%+v, want BLOCKED/nil passthrough", status, verdict)
	}
}

func TestManifestVerifyOptedOut(t *testing.T) {
	tests := []struct {
		name    string
		setKeys map[string]any
		want    bool
	}{
		{"absent participates", nil, false},
		{"false opts out", map[string]any{"verify": false}, true},
		{"true participates", map[string]any{"verify": true}, false},
		{"malformed participates", map[string]any{"verify": "nope"}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, m := setupDrainVerifyFixture(t, stubGit("sha1\n", "", ""), doneAFKSet(), tt.setKeys)
			if got := m.VerifyOptedOut(); got != tt.want {
				t.Fatalf("VerifyOptedOut() = %v, want %v", got, tt.want)
			}
		})
	}
}

// verifyEnabledConfig returns a config with Agent verification switched on.
func verifyEnabledConfig() *config.Config {
	return &config.Config{Task: &config.TasksConfig{Verify: &config.VerifyConfig{Enabled: true}}}
}

// setupRunTaskSetFixtureWithKeys mirrors setupRunTaskSetFixture but writes
// set-level manifest keys (e.g. {"verify": false}).
func setupRunTaskSetFixtureWithKeys(t *testing.T, stem string, tasks []Task, setKeys map[string]any) *runTaskSetFixture {
	t.Helper()
	root := t.TempDir()
	initExecutorGitRepo(t, root)
	t.Setenv("XDG_DATA_HOME", filepath.Join(root, ".xdg"))
	tasksDir := storageTasksDir(t, root)
	taskDir := filepath.Join(tasksDir, stem)
	for _, task := range tasks {
		writeTaskMD(t, taskDir, task.File, "## Acceptance criteria\n\n- [ ] ok\n")
	}
	writeManifestWithSetKeys(t, taskDir, tasks, setKeys)
	if _, err := RegisterWith(DefaultDeps(), tasksDir, DefaultStatePath()); err != nil {
		t.Fatal(err)
	}
	return &runTaskSetFixture{root: root, tasksDir: tasksDir}
}

// runtimeHead resolves the drain's runtime checkout, its repository identity,
// and current HEAD — the coordinates a Verify verdict is keyed by.
func runtimeHead(t *testing.T, d *Deps, root string) (repo, runtimePath, head string) {
	t.Helper()
	runtimePath, err := ResolveRuntimePathWith(d, root, "")
	if err != nil {
		t.Fatalf("resolve runtime path: %v", err)
	}
	id, err := ResolveRepositoryIdentity(d, runtimePath)
	if err != nil {
		t.Fatalf("resolve repository identity: %v", err)
	}
	out, err := d.Git.CommandInDir(runtimePath, "rev-parse", "HEAD")
	if err != nil {
		t.Fatalf("rev-parse HEAD: %v", err)
	}
	return id.CommonDir, runtimePath, strings.TrimSpace(out)
}

// TestRunTaskSetDisabledVerificationReachesDone: with verification off (the
// default), a fully-drained pure-AFK set reaches DONE without any Verifier —
// exactly as before the feature (criterion: disabled → DONE).
func TestRunTaskSetDisabledVerificationReachesDone(t *testing.T) {
	env := setupRunTaskSetFixture(t, "demo", openAFKSet())
	agent := writeFakeAgent(t, env.root, fakeAgentConfig{checkTask: true, summary: "done"})
	var buf bytes.Buffer
	opts := env.runTaskSetOpts(true, agent, &buf)
	opts.TaskSetOverride = "demo"

	result, err := RunTaskSetWith(env.deps(), nil, nil, opts)
	if err != nil {
		t.Fatalf("RunTaskSetWith: %v", err)
	}
	if !result.TaskSetDone {
		t.Fatalf("result = %+v, want TaskSetDone", result)
	}
}

// TestRunTaskSetVerifyOptOutReachesDone: with verification enabled but the set
// opting out via "verify": false, the drain skips verification and reaches DONE
// on AFK-exhaustion (no Verifier is invoked).
func TestRunTaskSetVerifyOptOutReachesDone(t *testing.T) {
	env := setupRunTaskSetFixtureWithKeys(t, "demo", openAFKSet(), map[string]any{"verify": false})
	agent := writeFakeAgent(t, env.root, fakeAgentConfig{checkTask: true, summary: "done"})
	var buf bytes.Buffer
	opts := env.runTaskSetOpts(true, agent, &buf)
	opts.TaskSetOverride = "demo"

	result, err := RunTaskSetWith(env.deps(), nil, func(string) (*config.Config, error) {
		return verifyEnabledConfig(), nil
	}, opts)
	if err != nil {
		t.Fatalf("RunTaskSetWith: %v", err)
	}
	if !result.TaskSetDone || result.TaskSetVerifyFailed {
		t.Fatalf("result = %+v, want TaskSetDone with no verify failure", result)
	}
}

// TestRunTaskSetVerifyPassReachesDone: with verification enabled, draining the
// open set to exhaustion runs the pre-approval Verifier fresh (an open AFK task
// means no prior episode's verdict may be reused — ADR-0109); a PASS reaches
// DONE. Cache reuse at unchanged work is exercised at the drainVerifyPhase level
// (TestDrainVerifyPhaseReusesCachedVerdict).
func TestRunTaskSetVerifyPassReachesDone(t *testing.T) {
	env := setupRunTaskSetFixture(t, "demo", openAFKSet())
	agent := writeFakeAgent(t, env.root, fakeAgentConfig{checkTask: true, summary: "done"})
	d := env.deps()

	var buf bytes.Buffer
	opts := env.runTaskSetOpts(true, agent, &buf)
	opts.TaskSetOverride = "demo"
	opts.verifyRunner = func(string) (string, error) { return "VERDICT: PASS\n", nil }

	result, err := RunTaskSetWith(d, nil, func(string) (*config.Config, error) {
		return verifyEnabledConfig(), nil
	}, opts)
	if err != nil {
		t.Fatalf("RunTaskSetWith: %v", err)
	}
	if !result.TaskSetDone {
		t.Fatalf("result = %+v, want TaskSetDone from a PASS verdict", result)
	}
}

// TestRunTaskSetVerifyNeedsHumanParks: with verification enabled, draining the
// open set to exhaustion runs the pre-approval Verifier fresh (ADR-0109 — an
// open AFK task carries no reusable verdict); a NEEDS-HUMAN verdict parks the
// set cleanly as VERIFY-FAILED (ExitNoRunnable, no crash).
func TestRunTaskSetVerifyNeedsHumanParks(t *testing.T) {
	env := setupRunTaskSetFixture(t, "demo", openAFKSet())
	agent := writeFakeAgent(t, env.root, fakeAgentConfig{checkTask: true, summary: "done"})
	d := env.deps()
	d.ProcessAlive = func(pid int) bool { return pid == os.Getpid() }
	_, runtimePath, _ := runtimeHead(t, d, env.root)

	var buf bytes.Buffer
	opts := env.runTaskSetOpts(true, agent, &buf)
	opts.TaskSetOverride = "demo"
	opts.verifyRunner = func(string) (string, error) {
		return "VERDICT: NEEDS-HUMAN\nFINDINGS: the spec is ambiguous\n", nil
	}

	result, err := RunTaskSetWith(d, nil, func(string) (*config.Config, error) {
		return verifyEnabledConfig(), nil
	}, opts)
	assertExitCode(t, err, ExitNoRunnable)
	if result == nil || !result.TaskSetVerifyFailed {
		t.Fatalf("result = %+v, want TaskSetVerifyFailed", result)
	}
	if result.TaskSetDone {
		t.Fatal("a NEEDS-HUMAN verdict must not reach DONE")
	}
	if !strings.Contains(result.VerifyFindings, "ambiguous") {
		t.Fatalf("VerifyFindings = %q, want the Verifier's reasons", result.VerifyFindings)
	}
	// The drain stopped cleanly on a NEEDS-HUMAN verdict: it records the
	// verify_failed terminal (ADR-0087), not a crash.
	rec := latestTerminalDrain(t, d, runtimePath)
	if rec == nil {
		t.Fatal("no terminal drain recorded")
	}
	if rec.State != store.StateVerifyFailed {
		t.Fatalf("park outcome = %q, want %q", rec.State, store.StateVerifyFailed)
	}
}

// TestDrainVerifyPhaseWritesWhileDrainHeld: on a cache miss the phase persists a
// fresh verdict on a second store connection while the drain's own connection is
// still open (BeginDrain) — the concurrent-connection write the live drain makes.
// It confirms WAL + busy_timeout let the write land rather than deadlock.
func TestDrainVerifyPhaseWritesWhileDrainHeld(t *testing.T) {
	env := setupRunTaskSetFixture(t, "demo", doneAFKSet())
	d := env.deps()
	d.ProcessAlive = func(pid int) bool { return pid == os.Getpid() }
	repo, runtimePath, head := runtimeHead(t, d, env.root)
	m := LoadManifest(d, "demo", filepath.Join(env.tasksDir, "demo", "index.json"))

	handle, err := BeginDrain(d, runtimePath, "demo", &bytes.Buffer{})
	if err != nil {
		t.Fatalf("BeginDrain: %v", err)
	}
	defer func() { _ = handle.Finish(store.StateFinished, "", false, time.Time{}) }()

	status, verdict, err := drainVerifyPhase(d, nil, verifyCoreOptions{
		Repo: repo, RuntimePath: runtimePath, SetID: "demo", Output: &bytes.Buffer{},
		runVerifier: func(string) (string, error) { return "VERDICT: PASS\n", nil },
	}, m, StatusDone)
	if err != nil {
		t.Fatalf("drainVerifyPhase while drain held: %v", err)
	}
	if status != StatusDone || verdict == nil || verdict.Verdict != "PASS" {
		t.Fatalf("status/verdict = %q/%+v, want DONE/PASS", status, verdict)
	}
	if stored := readStoredVerdict(t, d, repo, "demo", head); stored == nil || stored.Verdict != "PASS" {
		t.Fatalf("stored verdict = %+v, want PASS at head", stored)
	}
}

// TestRunTaskSetHITLGateOffersReverify: with verification enabled and the set at
// the AWAITING-APPROVAL HITL gate (a PASS verdict let the terminal status
// stand), the gate menu offers a Re-verify option, and choosing it force-runs
// the Verifier again against the current work (bypassing the SHA cache) before
// returning to the menu (ADR-0012).
func TestRunTaskSetHITLGateOffersReverify(t *testing.T) {
	env := setupRunTaskSetFixture(t, "demo", []Task{
		{ID: "01-a", File: "01-a.md", Title: "A", Type: "AFK", Status: "open"},
		{ID: "02-hitl", File: "02-hitl.md", Title: "Review", Type: "HITL", Status: "open"},
	})
	agent := writeFakeAgent(t, env.root, fakeAgentConfig{checkTask: true, summary: "first done"})
	d := env.deps()
	d.ProcessAlive = func(pid int) bool { return pid == os.Getpid() }

	calls := 0
	verify := func(string) (string, error) {
		calls++
		return "VERDICT: PASS\n", nil
	}

	var buf bytes.Buffer
	opts := env.runTaskSetOpts(false, agent, &buf)
	opts.verifyRunner = verify
	// Re-verify (5), then exit (0).
	opts.ConfirmIn = strings.NewReader("5\n0\n")

	_, err := RunTaskSetWith(d, nil, func(string) (*config.Config, error) {
		return verifyEnabledConfig(), nil
	}, opts)
	assertExitCode(t, err, ExitNoRunnable)

	out := buf.String()
	if !strings.Contains(out, "5. Re-verify") {
		t.Fatalf("HITL gate menu missing Re-verify option:\n%s", out)
	}
	// The Verifier ran at least twice: once in the drain's pre-approval phase
	// (PASS → reaches the gate) and once more for the forced re-verify.
	if calls < 2 {
		t.Fatalf("verifier calls = %d, want >= 2 (drain phase + forced re-verify)", calls)
	}
}

// TestRunTaskSetHITLGateReverifyRefreshesLabel: when a forced re-verify at the
// gate comes back non-PASS, the set's rendered state refreshes to VERIFY-FAILED
// and control returns to the gate menu (still offering Re-verify), so a human
// can keep iterating without a fresh drain (ADR-0012).
func TestRunTaskSetHITLGateReverifyRefreshesLabel(t *testing.T) {
	env := setupRunTaskSetFixture(t, "demo", []Task{
		{ID: "01-a", File: "01-a.md", Title: "A", Type: "AFK", Status: "open"},
		{ID: "02-hitl", File: "02-hitl.md", Title: "Review", Type: "HITL", Status: "open"},
	})
	agent := writeFakeAgent(t, env.root, fakeAgentConfig{checkTask: true, summary: "first done"})
	d := env.deps()
	d.ProcessAlive = func(pid int) bool { return pid == os.Getpid() }

	calls := 0
	verify := func(string) (string, error) {
		calls++
		if calls == 1 {
			// The drain's pre-approval phase passes, so the set reaches the gate.
			return "VERDICT: PASS\n", nil
		}
		// The forced re-verify now finds a problem the human must resolve.
		return "VERDICT: NEEDS-HUMAN\nFINDINGS: criterion 1 regressed\n", nil
	}

	var buf bytes.Buffer
	opts := env.runTaskSetOpts(false, agent, &buf)
	opts.verifyRunner = verify
	// Re-verify (5), then exit (0).
	opts.ConfirmIn = strings.NewReader("5\n0\n")

	_, err := RunTaskSetWith(d, nil, func(string) (*config.Config, error) {
		return verifyEnabledConfig(), nil
	}, opts)
	assertExitCode(t, err, ExitNoRunnable)

	out := buf.String()
	if !strings.Contains(out, "VERIFY-FAILED") {
		t.Fatalf("re-verify NEEDS-HUMAN verdict must refresh the label to VERIFY-FAILED:\n%s", out)
	}
	// The gate re-displayed after the re-verify (two Choose [1]: prompts).
	if strings.Count(out, "Choose [1]:") < 2 {
		t.Fatalf("gate must re-display after re-verify:\n%s", out)
	}
	if calls != 2 {
		t.Fatalf("verifier calls = %d, want 2 (drain phase + one forced re-verify)", calls)
	}
}

// TestRunTaskSetHITLGateHidesReverifyWhenDisabled: with verification off, the
// HITL gate menu omits the Re-verify option entirely — the force-verify path is
// gated by the same config opt-in as the rest of the feature (ADR-0086).
func TestRunTaskSetHITLGateHidesReverifyWhenDisabled(t *testing.T) {
	env := setupRunTaskSetFixture(t, "demo", []Task{
		{ID: "01-a", File: "01-a.md", Title: "A", Type: "AFK", Status: "open"},
		{ID: "02-hitl", File: "02-hitl.md", Title: "Review", Type: "HITL", Status: "open"},
	})
	agent := writeFakeAgent(t, env.root, fakeAgentConfig{checkTask: true, summary: "first done"})

	var buf bytes.Buffer
	opts := env.runTaskSetOpts(false, agent, &buf)
	opts.ConfirmIn = strings.NewReader("0\n")

	_, err := RunTaskSetWith(env.deps(), nil, nil, opts)
	assertExitCode(t, err, ExitNoRunnable)

	out := buf.String()
	if strings.Contains(out, "Re-verify") {
		t.Fatalf("HITL gate must not offer Re-verify when verification is disabled:\n%s", out)
	}
}

// TestRunTaskSetOpenHITLScopedVerifyReachesGate: with verification enabled, a
// set whose AFK work is done and whose only remaining task is an open terminal
// HITL sign-off verifies PASS on the AFK work and reaches the HITL gate — no
// premature VERIFY-FAILED return (ADR-0102). The verifier here returns
// NEEDS-HUMAN if it is ever shown the HITL task, so the set reaching the gate is
// a direct consequence of the prompt being scoped to done AFK work only.
func TestRunTaskSetOpenHITLScopedVerifyReachesGate(t *testing.T) {
	env := setupRunTaskSetFixture(t, "demo", []Task{
		{ID: "01-a", File: "01-a.md", Title: "A", Type: "AFK", Status: "open"},
		{ID: "02-hitl", File: "02-hitl.md", Title: "Sign off", Type: "HITL", Status: "open"},
	})
	agent := writeFakeAgent(t, env.root, fakeAgentConfig{checkTask: true, summary: "done"})
	d := env.deps()
	d.ProcessAlive = func(pid int) bool { return pid == os.Getpid() }

	// The verdict depends on the prompt: a scoped prompt (done AFK only) PASSes;
	// an unscoped prompt showing the open HITL task would deadlock at NEEDS-HUMAN.
	verify := func(prompt string) (string, error) {
		if strings.Contains(prompt, "[HITL]") {
			return "VERDICT: NEEDS-HUMAN\nFINDINGS: an agent cannot judge a human sign-off\n", nil
		}
		return "VERDICT: PASS\n", nil
	}

	var buf bytes.Buffer
	opts := env.runTaskSetOpts(false, agent, &buf)
	opts.verifyRunner = verify
	// Exit at the gate menu.
	opts.ConfirmIn = strings.NewReader("0\n")

	result, err := RunTaskSetWith(d, nil, func(string) (*config.Config, error) {
		return verifyEnabledConfig(), nil
	}, opts)
	assertExitCode(t, err, ExitNoRunnable)

	if result == nil || !result.TaskSetAwaitingApproval {
		t.Fatalf("result = %+v, want TaskSetAwaitingApproval (reached the HITL gate)", result)
	}
	if result.TaskSetVerifyFailed {
		t.Fatalf("scoped PASS must not park the set as VERIFY-FAILED:\n%s", buf.String())
	}
	repo, _, head := runtimeHead(t, d, env.root)
	if stored := readStoredVerdict(t, d, repo, "demo", head); stored == nil || stored.Verdict != "PASS" {
		t.Fatalf("stored verdict = %+v, want PASS at the current work SHA", stored)
	}
}

func TestVerifyEnabledGate(t *testing.T) {
	if verifyEnabled(nil) {
		t.Fatal("nil config should be disabled")
	}
	if verifyEnabled(&config.Config{}) {
		t.Fatal("empty config should be disabled")
	}
	if verifyEnabled(&config.Config{Task: &config.TasksConfig{Verify: &config.VerifyConfig{Enabled: false}}}) {
		t.Fatal("enabled=false should be disabled")
	}
	if !verifyEnabled(verifyEnabledConfig()) {
		t.Fatal("enabled=true should be enabled")
	}
}

// TestDrainVerifyPhaseQuotaPausePropagates: when every Verifier agent is
// quota-paused, the phase returns a quota pause error without persisting a
// NEEDS-HUMAN verdict.
func TestDrainVerifyPhaseQuotaPausePropagates(t *testing.T) {
	d, m := setupDrainVerifyFixture(t, stubGit("sha1\n", "", ""), doneAFKSet(), nil)
	_, _, err := drainVerifyPhase(d, nil, verifyCoreOptions{
		Repo: "/repo/.git", RuntimePath: "/rt", SetID: "demo", Output: &bytes.Buffer{},
		runVerifier: func(string) (string, error) {
			return "", newVerifyQuotaPause(VerifyQuotaPause{
				Preset:  "claude",
				ResetAt: time.Now().Add(-time.Hour),
				Reason:  "quota exhausted",
			})
		},
	}, m, StatusDone)
	if err == nil {
		t.Fatal("expected quota pause error")
	}
	if _, ok := AsVerifyQuotaPause(err); !ok {
		t.Fatalf("expected VerifyQuotaPause, got %v", err)
	}
	s, ok, storeErr := openDrainStoreIfExists(d)
	if storeErr != nil {
		t.Fatalf("open store: %v", storeErr)
	}
	if ok {
		defer func() { _ = s.Close() }()
		if v, err := s.GetVerifyVerdict("/repo/.git", "demo", "sha1"); err != nil || v != nil {
			t.Fatalf("quota pause must not store a verdict: v=%+v err=%v", v, err)
		}
	}
}

// TestRunTaskSetVerifyQuotaPauseRecoversAndCompletes: verifier quota exhaustion
// enters recovery wait; after the cooldown elapses the drain re-enters at verify
// only and can complete without re-running finished tasks.
func TestRunTaskSetVerifyQuotaPauseRecoversAndCompletes(t *testing.T) {
	env := setupRunTaskSetFixture(t, "demo", openAFKSet())
	agent := writeFakeAgent(t, env.root, fakeAgentConfig{checkTask: true, summary: "done"})
	d := env.deps()
	d.ProcessAlive = func(pid int) bool { return pid == os.Getpid() }

	calls := 0
	verify := func(string) (string, error) {
		calls++
		if calls == 1 {
			return "", newVerifyQuotaPause(VerifyQuotaPause{
				Preset:  "claude",
				ResetAt: time.Now().Add(-time.Hour),
				Reason:  "verifier quota exhausted",
			})
		}
		return "VERDICT: PASS\n", nil
	}

	var buf bytes.Buffer
	opts := env.runTaskSetOpts(true, agent, &buf)
	opts.TaskSetOverride = "demo"
	opts.verifyRunner = verify

	result, err := RunTaskSetWith(d, nil, func(string) (*config.Config, error) {
		return verifyEnabledConfig(), nil
	}, opts)
	if err != nil {
		t.Fatalf("RunTaskSetWith: %v", err)
	}
	if !result.TaskSetDone {
		t.Fatalf("result = %+v, want TaskSetDone", result)
	}
	if calls != 2 {
		t.Fatalf("verifier calls = %d, want 2 (quota pause then resume)", calls)
	}
	repo, _, head := runtimeHead(t, d, env.root)
	if stored := readStoredVerdict(t, d, repo, "demo", head); stored == nil || stored.Verdict != "PASS" {
		t.Fatalf("stored verdict = %+v, want PASS", stored)
	}
}

// twoDoneAFKSet is a set enlarged past a one-task PASS: two done AFK tasks, as
// after a new AFK task was added by a direct manifest edit (e.g. a HITL assist
// session) and drained to completion.
func twoDoneAFKSet() []Task {
	return []Task{
		{ID: "01-a", File: "01-a.md", Title: "A", Type: "AFK", Status: "done"},
		{ID: "02-b", File: "02-b.md", Title: "B", Type: "AFK", Status: "done"},
	}
}

// TestDrainVerifyPhaseScopeGrowthInvalidatesStalePass: an AFK task added to a
// set that already holds a PASS (recorded at the smaller scope) invalidates the
// stale PASS, so the enlarged set is re-verified at the new work SHA rather than
// coasting on ADR-0096 idempotency (ADR-0101).
func TestDrainVerifyPhaseScopeGrowthInvalidatesStalePass(t *testing.T) {
	d, m := setupDrainVerifyFixture(t, stubGit("shaNEW\n", "", ""), twoDoneAFKSet(), nil)
	// The PASS was recorded when the set had a single AFK task (scope 1).
	seedVerdict(t, d, store.VerifyVerdict{Repo: "/repo/.git", SetID: "demo", WorkSHA: "shaOLD", Verdict: "PASS", Scope: 1})

	called := false
	status, verdict, err := drainVerifyPhase(d, nil, verifyCoreOptions{
		Repo: "/repo/.git", RuntimePath: "/rt", SetID: "demo", Output: &bytes.Buffer{},
		runVerifier: func(string) (string, error) { called = true; return "VERDICT: PASS\n", nil },
	}, m, StatusDone)
	if err != nil {
		t.Fatalf("drainVerifyPhase: %v", err)
	}
	if !called {
		t.Fatal("Verifier not re-invoked despite the set growing a new AFK task past the PASS scope")
	}
	if status != StatusDone || verdict == nil || verdict.Verdict != "PASS" {
		t.Fatalf("status/verdict = %q/%+v, want DONE/PASS from the re-verify", status, verdict)
	}
	// The fresh verdict is recorded at the new SHA carrying the enlarged scope.
	stored := readStoredVerdict(t, d, "/repo/.git", "demo", "shaNEW")
	if stored == nil || stored.Verdict != "PASS" || stored.Scope != 2 {
		t.Fatalf("stored verdict at shaNEW = %+v, want PASS with scope 2", stored)
	}
	// The stale PASS at the smaller scope was invalidated, not left to immunize.
	if old := readStoredVerdict(t, d, "/repo/.git", "demo", "shaOLD"); old != nil {
		t.Fatalf("stale PASS at shaOLD survived scope growth: %+v", old)
	}
}

// TestDrainVerifyPhaseUnchangedScopeStillImmunizes: a commit that only moves the
// work SHA without adding a task (scope unchanged) still coasts on the immunizing
// PASS, so the Verifier is not re-invoked — ADR-0096 is not regressed by the
// scope-growth check (ADR-0101).
func TestDrainVerifyPhaseUnchangedScopeStillImmunizes(t *testing.T) {
	d, m := setupDrainVerifyFixture(t, stubGit("shaNEW\n", "", ""), twoDoneAFKSet(), nil)
	// The PASS was recorded at the same scope the set still has (2 AFK tasks).
	seedVerdict(t, d, store.VerifyVerdict{Repo: "/repo/.git", SetID: "demo", WorkSHA: "shaOLD", Verdict: "PASS", Scope: 2})

	status, verdict, err := drainVerifyPhase(d, nil, verifyCoreOptions{
		Repo: "/repo/.git", RuntimePath: "/rt", SetID: "demo", Output: &bytes.Buffer{},
		runVerifier: func(string) (string, error) {
			t.Fatal("Verifier re-invoked despite an unchanged scope (incidental SHA move)")
			return "", nil
		},
	}, m, StatusDone)
	if err != nil {
		t.Fatalf("drainVerifyPhase: %v", err)
	}
	if status != StatusDone || verdict == nil || verdict.Verdict != "PASS" || verdict.WorkSHA != "shaOLD" {
		t.Fatalf("status/verdict = %q/%+v, want DONE/PASS at shaOLD from the immunizing cache", status, verdict)
	}
}

// TestDrainVerifyPhaseAcceptedPassDerivesVerified: a human-authored PASS
// (ADR-0103) at the current work SHA is an ordinary PASS row, so the cache-first
// read reuses it and the set derives verified without re-invoking the Verifier —
// exactly as an agent PASS does.
func TestDrainVerifyPhaseAcceptedPassDerivesVerified(t *testing.T) {
	d, m := setupDrainVerifyFixture(t, stubGit("shaACC\n", "", ""), doneAFKSet(), nil)
	seedVerdict(t, d, store.VerifyVerdict{
		Repo: "/repo/.git", SetID: "demo", WorkSHA: "shaACC", Verdict: "PASS",
		Scope: 1, HumanAuthored: true, Note: "known non-issue",
	})

	status, verdict, err := drainVerifyPhase(d, nil, verifyCoreOptions{
		Repo: "/repo/.git", RuntimePath: "/rt", SetID: "demo", Output: &bytes.Buffer{},
		runVerifier: func(string) (string, error) {
			t.Fatal("Verifier re-invoked despite a human-authored PASS at the current SHA")
			return "", nil
		},
	}, m, StatusDone)
	if err != nil {
		t.Fatalf("drainVerifyPhase: %v", err)
	}
	if status != StatusDone || verdict == nil || verdict.Verdict != "PASS" || !verdict.HumanAuthored {
		t.Fatalf("status/verdict = %q/%+v, want DONE/human-authored PASS", status, verdict)
	}
}

// TestDrainVerifyPhaseScopeGrowthInvalidatesAcceptedPassAndForwardFeedsNote:
// growing the AFK scope invalidates a human-authored PASS just like any PASS
// (ADR-0101) — the stale accept no longer immunizes and the Verifier re-fires at
// the new SHA. The accepted note is captured before the row is deleted and folded
// into that fresh Verifier prompt as context (ADR-0103), so the known non-issue
// is not re-flagged while a real regression could still fail.
func TestDrainVerifyPhaseScopeGrowthInvalidatesAcceptedPassAndForwardFeedsNote(t *testing.T) {
	d, m := setupDrainVerifyFixture(t, stubGit("shaNEW\n", "", ""), twoDoneAFKSet(), nil)
	// A human accepted the set when it held a single AFK task (scope 1).
	seedVerdict(t, d, store.VerifyVerdict{
		Repo: "/repo/.git", SetID: "demo", WorkSHA: "shaOLD", Verdict: "PASS",
		Scope: 1, HumanAuthored: true, Note: "the extra allocation is deliberate",
	})

	var gotPrompt string
	status, verdict, err := drainVerifyPhase(d, nil, verifyCoreOptions{
		Repo: "/repo/.git", RuntimePath: "/rt", SetID: "demo", Output: &bytes.Buffer{},
		runVerifier: func(prompt string) (string, error) { gotPrompt = prompt; return "VERDICT: PASS\n", nil },
	}, m, StatusDone)
	if err != nil {
		t.Fatalf("drainVerifyPhase: %v", err)
	}
	if status != StatusDone || verdict == nil || verdict.Verdict != "PASS" {
		t.Fatalf("status/verdict = %q/%+v, want DONE/PASS from the re-verify", status, verdict)
	}
	// The invalidated accept no longer immunizes: its row is gone and the fresh
	// verdict is agent-authored at the new SHA carrying the enlarged scope.
	if old := readStoredVerdict(t, d, "/repo/.git", "demo", "shaOLD"); old != nil {
		t.Fatalf("stale accepted PASS at shaOLD survived scope growth: %+v", old)
	}
	fresh := readStoredVerdict(t, d, "/repo/.git", "demo", "shaNEW")
	if fresh == nil || fresh.Verdict != "PASS" || fresh.Scope != 2 || fresh.HumanAuthored {
		t.Fatalf("stored verdict at shaNEW = %+v, want agent-authored PASS with scope 2", fresh)
	}
	// The accepted note fed forward into the fresh Verifier prompt as context.
	if !strings.Contains(gotPrompt, "the extra allocation is deliberate") {
		t.Fatalf("re-verify prompt must forward-feed the accepted note:\n%s", gotPrompt)
	}
	if !strings.Contains(gotPrompt, "Prior human note") {
		t.Fatalf("re-verify prompt must frame the note as a prior-human-note context section:\n%s", gotPrompt)
	}
}
