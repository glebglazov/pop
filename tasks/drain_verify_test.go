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

// TestDrainVerifyPhaseStaleCachedVerdictReRuns: a verdict stored at a different
// (stale) work SHA misses the cache, so the Verifier runs and records a fresh
// verdict at the current SHA.
func TestDrainVerifyPhaseStaleCachedVerdictReRuns(t *testing.T) {
	d, m := setupDrainVerifyFixture(t, stubGit("shaNEW\n", "", ""), doneAFKSet(), nil)
	seedVerdict(t, d, store.VerifyVerdict{Repo: "/repo/.git", SetID: "demo", WorkSHA: "shaOLD", Verdict: "PASS"})
	called := false
	status, verdict, err := drainVerifyPhase(d, nil, verifyCoreOptions{
		Repo: "/repo/.git", RuntimePath: "/rt", SetID: "demo", Output: &bytes.Buffer{},
		runVerifier: func(string) (string, error) { called = true; return "VERDICT: NEEDS-HUMAN\nFINDINGS: x\n", nil },
	}, m, StatusDone)
	if err != nil {
		t.Fatalf("drainVerifyPhase: %v", err)
	}
	if !called {
		t.Fatal("Verifier not invoked despite a stale (different-SHA) cached verdict")
	}
	if status != StatusVerifyFailed || verdict == nil || verdict.Verdict != "NEEDS-HUMAN" {
		t.Fatalf("status/verdict = %q/%+v, want VERIFY-FAILED/NEEDS-HUMAN", status, verdict)
	}
	if stored := readStoredVerdict(t, d, "/repo/.git", "demo", "shaNEW"); stored == nil || stored.Verdict != "NEEDS-HUMAN" {
		t.Fatalf("stored verdict at shaNEW = %+v, want NEEDS-HUMAN", stored)
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

// TestRunTaskSetVerifyCachedPassReachesDone: with verification enabled and a
// PASS verdict already cached at the current work SHA, the drain reuses it and
// reaches DONE without re-invoking the Verifier (a re-drain does not loop).
func TestRunTaskSetVerifyCachedPassReachesDone(t *testing.T) {
	env := setupRunTaskSetFixture(t, "demo", openAFKSet())
	agent := writeFakeAgent(t, env.root, fakeAgentConfig{checkTask: true, summary: "done"})
	d := env.deps()
	repo, _, head := runtimeHead(t, d, env.root)
	seedVerdict(t, d, store.VerifyVerdict{Repo: repo, SetID: "demo", WorkSHA: head, Verdict: "PASS"})

	var buf bytes.Buffer
	opts := env.runTaskSetOpts(true, agent, &buf)
	opts.TaskSetOverride = "demo"

	result, err := RunTaskSetWith(d, nil, func(string) (*config.Config, error) {
		return verifyEnabledConfig(), nil
	}, opts)
	if err != nil {
		t.Fatalf("RunTaskSetWith: %v", err)
	}
	if !result.TaskSetDone {
		t.Fatalf("result = %+v, want TaskSetDone from cached PASS", result)
	}
}

// TestRunTaskSetVerifyCachedNeedsHumanParks: with verification enabled and a
// NEEDS-HUMAN verdict cached at the current work SHA, the drain parks the set
// cleanly as VERIFY-FAILED (ExitNoRunnable, no crash).
func TestRunTaskSetVerifyCachedNeedsHumanParks(t *testing.T) {
	env := setupRunTaskSetFixture(t, "demo", openAFKSet())
	agent := writeFakeAgent(t, env.root, fakeAgentConfig{checkTask: true, summary: "done"})
	d := env.deps()
	d.ProcessAlive = func(pid int) bool { return pid == os.Getpid() }
	repo, runtimePath, head := runtimeHead(t, d, env.root)
	seedVerdict(t, d, store.VerifyVerdict{
		Repo: repo, SetID: "demo", WorkSHA: head, Verdict: "NEEDS-HUMAN", Findings: "the spec is ambiguous",
	})

	var buf bytes.Buffer
	opts := env.runTaskSetOpts(true, agent, &buf)
	opts.TaskSetOverride = "demo"

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
	rec, err := ReadDrainOutcome(d, runtimePath)
	if err != nil {
		t.Fatalf("read drain outcome: %v", err)
	}
	if rec.Outcome != DrainOutcomeVerifyFailed {
		t.Fatalf("park outcome = %q, want %q", rec.Outcome, DrainOutcomeVerifyFailed)
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
	defer func() { _ = handle.Finish(DrainOutcomeFinished, "", false, time.Time{}) }()

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
