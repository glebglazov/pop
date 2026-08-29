package tasks

import (
	"bytes"
	"io"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/glebglazov/pop/config"
	"github.com/glebglazov/pop/store"
)

// refineEnabledConfig switches on both drain phases the refine step sits behind:
// refine runs after verification, so a test that pins the order needs both.
func refineEnabledConfig() *config.Config {
	return &config.Config{Work: &config.WorkConfig{
		Verify: &config.VerifyConfig{Enabled: true},
		Refine: &config.RefineConfig{Enabled: true},
	}}
}

// setupDrainRefineFixture is setupRunTaskSetFixture with the set's base commit
// recorded, so the Work diff view resolves a commit range and the refine pass has
// something it is willing to judge.
func setupDrainRefineFixture(t *testing.T, tasks []Task) *runTaskSetFixture {
	t.Helper()
	env := setupRunTaskSetFixture(t, "demo", tasks)
	_, _, head := runtimeHead(t, env.deps(), env.root)
	writeManifestWithSetKeys(t, filepath.Join(env.tasksDir, "demo"), tasks, map[string]any{"base_commit": head})
	return env
}

// signOffSet is a set whose agent work is finished and whose only remaining task
// is the human sign-off. Draining it arrives at AFK quiescence immediately, and
// completing the HITL task at the gate arrives there a second time — the two
// arrivals a Refine episode has to tell apart.
func signOffSet() []Task {
	return []Task{
		{ID: "01-a", File: "01-a.md", Title: "A", Type: "AFK", Status: "done"},
		{ID: "02-h", File: "02-h.md", Title: "Sign off", Type: "HITL", Status: "open"},
	}
}

func refineDocuments(t *testing.T, env *runTaskSetFixture) []string {
	t.Helper()
	return listRefineFiles(t, filepath.Join(env.tasksDir, "demo"))
}

// TestDrainRefinesOnceAfterVerifyAndBeforeTheTerminalSwitch drives the whole
// step: an enabled drain refines after the verify phase and before the terminal
// switch hands the set to a human, and the second arrival at quiescence in the
// same episode refines nothing.
func TestDrainRefinesOnceAfterVerifyAndBeforeTheTerminalSwitch(t *testing.T) {
	t.Parallel()
	env := setupDrainRefineFixture(t, signOffSet())
	var phases []string

	var buf bytes.Buffer
	opts := env.runTaskSetOpts(false, "", &buf)
	opts.TaskSetOverride = "demo"
	opts.ConfirmIn = strings.NewReader("2\n") // Complete the sign-off at the gate.
	opts.ConfirmOut = &buf
	opts.verifyRunner = func(string) (string, error) {
		phases = append(phases, "verify")
		return "VERDICT: PASS\n", nil
	}
	opts.refineRunner = func(string) (string, string, error) {
		phases = append(phases, "refine")
		return "## Naming\n\nthe helper reads as a noun", "claude", nil
	}

	result, err := RunTaskSetWith(env.deps(), nil, func(string) (*config.Config, error) {
		return refineEnabledConfig(), nil
	}, opts)
	if err != nil {
		t.Fatalf("RunTaskSetWith: %v", err)
	}
	if !result.TaskSetDone {
		t.Fatalf("result = %+v, want the completed sign-off to reach DONE", result)
	}

	// Ordering: the Verifier judged the set before the Refiner described it, and
	// the second arrival at quiescence spawned neither.
	if strings.Join(phases, ",") != "verify,refine" {
		t.Fatalf("phases = %v, want the refine pass after exactly one verification", phases)
	}
	if docs := refineDocuments(t, env); len(docs) != 1 {
		t.Fatalf("refine documents = %v, want exactly one", docs)
	}

	// Placement: the report is written before the terminal switch opens the
	// sign-off gate, so the human deciding is looking at a report of the tree
	// they are approving.
	out := buf.String()
	refine := strings.Index(out, "━━ Refine for demo")
	verdict := strings.Index(out, "━━ Verify verdict for demo")
	gate := strings.Index(out, "Complete task")
	if refine < 0 || verdict < 0 || gate < 0 {
		t.Fatalf("output missing a phase (verdict=%d refine=%d gate=%d):\n%s", verdict, refine, gate, out)
	}
	if !(verdict < refine && refine < gate) {
		t.Fatalf("phases out of order (verdict=%d refine=%d gate=%d):\n%s", verdict, refine, gate, out)
	}

	// Refine spawns no work, which is why its episode needs no carve-out: the
	// set's tasks are the ones it started with.
	m := LoadManifest(env.deps(), "demo", filepath.Join(env.tasksDir, "demo", "index.json"))
	if len(m.Tasks) != 2 {
		t.Fatalf("refine pass changed the set's task list: %+v", m.Tasks)
	}
}

// TestDrainReachesTheSameTerminalStatusWithRefineOnAndOff: refine never gates,
// so the only observable difference an enabled group makes is that a report
// exists. A disabled group spawns nothing at all.
func TestDrainReachesTheSameTerminalStatusWithRefineOnAndOff(t *testing.T) {
	t.Parallel()

	drain := func(t *testing.T, cfg *config.Config, refine func(string) (string, string, error)) (*RunTaskSetResult, *runTaskSetFixture) {
		t.Helper()
		env := setupDrainRefineFixture(t, signOffSet())
		var buf bytes.Buffer
		opts := env.runTaskSetOpts(false, "", &buf)
		opts.TaskSetOverride = "demo"
		opts.ConfirmIn = strings.NewReader("2\n")
		opts.ConfirmOut = &buf
		opts.verifyRunner = func(string) (string, error) { return "VERDICT: PASS\n", nil }
		opts.refineRunner = refine
		result, err := RunTaskSetWith(env.deps(), nil, func(string) (*config.Config, error) { return cfg, nil }, opts)
		if err != nil {
			t.Fatalf("RunTaskSetWith: %v", err)
		}
		return result, env
	}

	off, offEnv := drain(t, verifyEnabledConfig(), func(string) (string, string, error) {
		t.Fatal("a disabled [work.refine] must spawn no Refiner")
		return "", "", nil
	})
	on, onEnv := drain(t, refineEnabledConfig(), func(string) (string, string, error) {
		return "## Fine", "claude", nil
	})

	if off.TaskSetDone != on.TaskSetDone ||
		off.TaskSetAwaitingApproval != on.TaskSetAwaitingApproval ||
		off.TaskSetVerifyFailed != on.TaskSetVerifyFailed {
		t.Fatalf("terminal status differs with refine enabled:\n off = %+v\n on  = %+v", off, on)
	}
	if docs := refineDocuments(t, offEnv); len(docs) != 0 {
		t.Fatalf("a disabled group wrote %v", docs)
	}
	if docs := refineDocuments(t, onEnv); len(docs) != 1 {
		t.Fatalf("an enabled group wrote %v, want one document", docs)
	}
}

// TestDrainSkipsRefineForAnOptedOutSet: `"refine": false` declines the drain's
// Refine for one set while the group stays enabled for every other — the
// Verifier's per-set opt-out, key for key. The set reaches the same terminal
// status it would have reached refined, because refine gates nothing.
func TestDrainSkipsRefineForAnOptedOutSet(t *testing.T) {
	t.Parallel()
	env := setupRunTaskSetFixture(t, "demo", signOffSet())
	_, _, head := runtimeHead(t, env.deps(), env.root)
	writeManifestWithSetKeys(t, filepath.Join(env.tasksDir, "demo"), signOffSet(),
		map[string]any{"base_commit": head, "refine": false})

	var buf bytes.Buffer
	opts := env.runTaskSetOpts(false, "", &buf)
	opts.TaskSetOverride = "demo"
	opts.ConfirmIn = strings.NewReader("2\n") // Complete the sign-off at the gate.
	opts.ConfirmOut = &buf
	opts.verifyRunner = func(string) (string, error) { return "VERDICT: PASS\n", nil }
	opts.refineRunner = func(string) (string, string, error) {
		t.Fatal("an opted-out set must spawn no Refiner")
		return "", "", nil
	}

	result, err := RunTaskSetWith(env.deps(), nil, func(string) (*config.Config, error) {
		return refineEnabledConfig(), nil
	}, opts)
	if err != nil {
		t.Fatalf("RunTaskSetWith: %v", err)
	}
	if !result.TaskSetDone {
		t.Fatalf("result = %+v, want the completed sign-off to reach DONE", result)
	}
	if docs := refineDocuments(t, env); len(docs) != 0 {
		t.Fatalf("an opted-out set wrote %v", docs)
	}
}

// TestRefineRunIsCapturedUnderItsOwnPhase: the Refiner's invocation is filed as
// a Captured run under the `refine` phase — the seam the shared fallback walk
// calls — and the lenses that read those runs give it a row of its own rather
// than folding a set-level run into implement spend.
func TestRefineRunIsCapturedUnderItsOwnPhase(t *testing.T) {
	t.Parallel()
	d, _, setDir := setupRefineFixture(t, time.Date(2026, 8, 16, 12, 0, 0, 0, time.UTC))
	m := LoadManifest(d, "demo", filepath.Join(setDir, "index.json"))

	invocation, err := ResolveAgentInvocationWithMode("claude", "", "prompt", "/rt", AgentOutputAuto)
	if err != nil {
		t.Fatalf("resolve invocation: %v", err)
	}
	rec := newStreamRecorder(io.Discard, fakeClock(time.Date(2026, 8, 16, 12, 0, 0, 0, time.UTC), 100*time.Millisecond))
	role := refinerRole(d, io.Discard, setDir, "demo", "sha1")
	role.persistAnswer(rec, invocation, 1, streamOutcomeCompleted, "", 0, "## Naming")

	runs, err := listSetRuns(d, setDir)
	if err != nil {
		t.Fatalf("listSetRuns: %v", err)
	}
	if len(runs) != 1 {
		t.Fatalf("captured runs = %d, want the Refiner's one", len(runs))
	}
	meta := runs[0].meta
	if meta.Phase != "refine" || meta.TaskSetID != "demo" || meta.WorkSHA != "sha1" {
		t.Fatalf("run meta = %+v, want a set-level refine run at sha1", meta)
	}
	if meta.Verdict != "" || meta.TaskFile != "" {
		t.Fatalf("run meta = %+v, want no verdict and no task file", meta)
	}

	// The spend lens gives it its own row and its own bucket: a refine pass is not
	// implement spend, and pretending otherwise would overstate what a task cost.
	breakdown, err := buildSpendSetBreakdown(d, "demo", m, loadRateTableForSpend(d), nil)
	if err != nil {
		t.Fatalf("buildSpendSetBreakdown: %v", err)
	}
	if breakdown.RefineRunCount != 1 || breakdown.ImplementRunCount != 0 {
		t.Fatalf("buckets = refine %d / implement %d, want the run counted as refine",
			breakdown.RefineRunCount, breakdown.ImplementRunCount)
	}
	if len(breakdown.Rows) != 1 || breakdown.Rows[0].TaskID != "refine" {
		t.Fatalf("rows = %+v, want one refine row", breakdown.Rows)
	}

	// The stream lens groups it as a set-level run rather than failing to name a
	// task file it does not have.
	streams, err := readSetAttemptStreams(d, m, true)
	if err != nil {
		t.Fatalf("readSetAttemptStreams: %v", err)
	}
	if len(streams) != 1 || streams[0].TaskID != "refine" {
		t.Fatalf("streams = %+v, want the refine pass's own stream", streams)
	}
}

// TestRefineEpisodeArmsOnNewDoneAFKWork covers the episode rule itself: what
// disarms automatic Refine, and what brings it back.
func TestRefineEpisodeArmsOnNewDoneAFKWork(t *testing.T) {
	t.Parallel()
	refined := []Task{{ID: "01-a", File: "01-a.md", Title: "A", Type: "AFK", Status: "done"}}

	tests := []struct {
		name  string
		tasks []Task
		want  bool
	}{
		{
			name:  "the composition just refined",
			tasks: refined,
			want:  false,
		},
		{
			name: "a human sign-off landing beside it",
			tasks: append(append([]Task{}, refined...),
				Task{ID: "02-h", File: "02-h.md", Title: "Sign off", Type: "HITL", Status: "done"}),
			want: false,
		},
		{
			name: "an unfinished task appearing",
			tasks: append(append([]Task{}, refined...),
				Task{ID: "02-b", File: "02-b.md", Title: "B", Type: "AFK", Status: "open"}),
			want: false,
		},
		{
			name: "a verify-spawned remediation task landing",
			tasks: append(append([]Task{}, refined...),
				Task{ID: "02-remediation", File: "02-remediation.md", Title: "Fix findings", Type: "AFK", Status: "done"}),
			want: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			d, defPath, _ := setupRefineFixture(t, time.Date(2026, 8, 16, 12, 0, 0, 0, time.UTC))
			setupManifest(t, defPath, "demo", tt.tasks)
			m := LoadManifest(d, "demo", filepath.Join(defPath, "demo", "index.json"))

			recordRefineEpisode(d, nil, refineEpisodeRecord("/repo/.git", "demo", "sha1", refineComposition(&Manifest{Tasks: refined}), "/refine/refine.md", time.Now()))
			defer func() { _ = d.CloseStore() }()

			if got := refineEpisodeArmed(d, "/repo/.git", "demo", refineComposition(m)); got != tt.want {
				t.Fatalf("armed = %v, want %v", got, tt.want)
			}
		})
	}
}

// TestRefineEpisodeArmedWithoutARecordOrWork: a set nobody has refined is
// armed, and a set with no finished agent work never is — there is nothing whose
// standards a refine pass could judge.
func TestRefineEpisodeArmedWithoutARecordOrWork(t *testing.T) {
	t.Parallel()
	d := newTestDeps(t)
	if !refineEpisodeArmed(d, "/repo/.git", "demo", "01-a") {
		t.Fatal("an unrefined set must be armed")
	}
	if refineEpisodeArmed(d, "/repo/.git", "demo", "") {
		t.Fatal("a set with no done AFK work must never arm refine")
	}
}

// TestHandRefineIgnoresTheEpisode: `pop tasks refine <set>` is a human asking,
// so it runs at a composition the drain has already refined — and records the
// episode itself, because the rule is about the work having been refined.
func TestHandRefineIgnoresTheEpisode(t *testing.T) {
	t.Parallel()
	d, defPath, setDir := setupRefineFixture(t, time.Date(2026, 8, 16, 12, 0, 0, 0, time.UTC))
	m := LoadManifest(d, "demo", filepath.Join(setDir, "index.json"))
	composition := refineComposition(m)
	recordRefineEpisode(d, nil, refineEpisodeRecord("/repo/.git", "demo", "sha1", composition, "/refine/old.md", time.Now()))
	defer func() { _ = d.CloseStore() }()

	opts := refineOpts(defPath, &bytes.Buffer{}, func(string) (string, string, error) {
		return "## Still worth saying", "claude", nil
	})
	opts.Repo = "/repo/.git"
	res, err := refineResolvedSet(d, nil, opts)
	if err != nil {
		t.Fatalf("a hand refine pass must run regardless of the episode: %v", err)
	}
	if refineEpisodeArmed(d, "/repo/.git", "demo", composition) {
		t.Fatal("a hand refine pass must disarm automatic re-refine of the same work")
	}
	s, err := openDrainStore(d)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	episode, err := s.GetRefineEpisode("/repo/.git", "demo")
	if err != nil || episode == nil {
		t.Fatalf("episode = %+v, %v", episode, err)
	}
	if episode.Document != res.Path {
		t.Fatalf("episode document = %q, want the document just written %q", episode.Document, res.Path)
	}
	if episode.WorkSHA == "" {
		t.Fatal("the episode must record the commit the document was written against")
	}
}

// TestRefineEpisodeStoreRoundTrip pins the one thing the row must do: the latest
// report for a set replaces the previous one rather than accumulating.
func TestRefineEpisodeStoreRoundTrip(t *testing.T) {
	t.Parallel()
	d := newTestDeps(t)
	s, err := openDrainStore(d)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer func() { _ = d.CloseStore() }()

	for _, composition := range []string{"01-a", "01-a\n02-b"} {
		if err := s.PutRefineEpisode(store.RefineEpisode{
			Repo: "/repo/.git", SetID: "demo", WorkSHA: "sha", Composition: composition,
			Document: "/refine/" + composition + ".md", RefinedAt: time.Unix(1, 0).UTC(),
		}); err != nil {
			t.Fatalf("PutRefineEpisode: %v", err)
		}
	}
	episode, err := s.GetRefineEpisode("/repo/.git", "demo")
	if err != nil || episode == nil {
		t.Fatalf("episode = %+v, %v", episode, err)
	}
	if episode.Composition != "01-a\n02-b" {
		t.Fatalf("composition = %q, want the latest report's", episode.Composition)
	}
}
