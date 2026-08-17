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

// reviewEnabledConfig switches on both drain phases the review step sits behind:
// review runs after verification, so a test that pins the order needs both.
func reviewEnabledConfig() *config.Config {
	return &config.Config{Work: &config.WorkConfig{
		Verify: &config.VerifyConfig{Enabled: true},
		Review: &config.ReviewConfig{Enabled: true},
	}}
}

// setupDrainReviewFixture is setupRunTaskSetFixture with the set's base commit
// recorded, so the Work diff view resolves a commit range and the review has
// something it is willing to judge.
func setupDrainReviewFixture(t *testing.T, tasks []Task) *runTaskSetFixture {
	t.Helper()
	env := setupRunTaskSetFixture(t, "demo", tasks)
	_, _, head := runtimeHead(t, env.deps(), env.root)
	writeManifestWithSetKeys(t, filepath.Join(env.tasksDir, "demo"), tasks, map[string]any{"base_commit": head})
	return env
}

// signOffSet is a set whose agent work is finished and whose only remaining task
// is the human sign-off. Draining it arrives at AFK quiescence immediately, and
// completing the HITL task at the gate arrives there a second time — the two
// arrivals a Review episode has to tell apart.
func signOffSet() []Task {
	return []Task{
		{ID: "01-a", File: "01-a.md", Title: "A", Type: "AFK", Status: "done"},
		{ID: "02-h", File: "02-h.md", Title: "Sign off", Type: "HITL", Status: "open"},
	}
}

func reviewDocuments(t *testing.T, env *runTaskSetFixture) []string {
	t.Helper()
	return listReviewFiles(t, filepath.Join(env.tasksDir, "demo"))
}

// TestDrainReviewsOnceAfterVerifyAndBeforeTheTerminalSwitch drives the whole
// step: an enabled drain reviews after the verify phase and before the terminal
// switch hands the set to a human, and the second arrival at quiescence in the
// same episode reviews nothing.
func TestDrainReviewsOnceAfterVerifyAndBeforeTheTerminalSwitch(t *testing.T) {
	t.Parallel()
	env := setupDrainReviewFixture(t, signOffSet())
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
	opts.reviewRunner = func(string) (string, string, error) {
		phases = append(phases, "review")
		return "## Naming\n\nthe helper reads as a noun", "claude", nil
	}

	result, err := RunTaskSetWith(env.deps(), nil, func(string) (*config.Config, error) {
		return reviewEnabledConfig(), nil
	}, opts)
	if err != nil {
		t.Fatalf("RunTaskSetWith: %v", err)
	}
	if !result.TaskSetDone {
		t.Fatalf("result = %+v, want the completed sign-off to reach DONE", result)
	}

	// Ordering: the Verifier judged the set before the Reviewer described it, and
	// the second arrival at quiescence spawned neither.
	if strings.Join(phases, ",") != "verify,review" {
		t.Fatalf("phases = %v, want the review after exactly one verification", phases)
	}
	if docs := reviewDocuments(t, env); len(docs) != 1 {
		t.Fatalf("review documents = %v, want exactly one", docs)
	}

	// Placement: the document is written before the terminal switch opens the
	// sign-off gate, so the human deciding is looking at a review of the tree
	// they are approving.
	out := buf.String()
	review := strings.Index(out, "━━ Code review for demo")
	verdict := strings.Index(out, "━━ Verify verdict for demo")
	gate := strings.Index(out, "Complete task")
	if review < 0 || verdict < 0 || gate < 0 {
		t.Fatalf("output missing a phase (verdict=%d review=%d gate=%d):\n%s", verdict, review, gate, out)
	}
	if !(verdict < review && review < gate) {
		t.Fatalf("phases out of order (verdict=%d review=%d gate=%d):\n%s", verdict, review, gate, out)
	}

	// Review spawns no work, which is why its episode needs no carve-out: the
	// set's tasks are the ones it started with.
	m := LoadManifest(env.deps(), "demo", filepath.Join(env.tasksDir, "demo", "index.json"))
	if len(m.Tasks) != 2 {
		t.Fatalf("review changed the set's task list: %+v", m.Tasks)
	}
}

// TestDrainReachesTheSameTerminalStatusWithReviewOnAndOff: review never gates,
// so the only observable difference an enabled group makes is that a document
// exists. A disabled group spawns nothing at all.
func TestDrainReachesTheSameTerminalStatusWithReviewOnAndOff(t *testing.T) {
	t.Parallel()

	drain := func(t *testing.T, cfg *config.Config, review func(string) (string, string, error)) (*RunTaskSetResult, *runTaskSetFixture) {
		t.Helper()
		env := setupDrainReviewFixture(t, signOffSet())
		var buf bytes.Buffer
		opts := env.runTaskSetOpts(false, "", &buf)
		opts.TaskSetOverride = "demo"
		opts.ConfirmIn = strings.NewReader("2\n")
		opts.ConfirmOut = &buf
		opts.verifyRunner = func(string) (string, error) { return "VERDICT: PASS\n", nil }
		opts.reviewRunner = review
		result, err := RunTaskSetWith(env.deps(), nil, func(string) (*config.Config, error) { return cfg, nil }, opts)
		if err != nil {
			t.Fatalf("RunTaskSetWith: %v", err)
		}
		return result, env
	}

	off, offEnv := drain(t, verifyEnabledConfig(), func(string) (string, string, error) {
		t.Fatal("a disabled [work.review] must spawn no Reviewer")
		return "", "", nil
	})
	on, onEnv := drain(t, reviewEnabledConfig(), func(string) (string, string, error) {
		return "## Fine", "claude", nil
	})

	if off.TaskSetDone != on.TaskSetDone ||
		off.TaskSetAwaitingApproval != on.TaskSetAwaitingApproval ||
		off.TaskSetVerifyFailed != on.TaskSetVerifyFailed {
		t.Fatalf("terminal status differs with review enabled:\n off = %+v\n on  = %+v", off, on)
	}
	if docs := reviewDocuments(t, offEnv); len(docs) != 0 {
		t.Fatalf("a disabled group wrote %v", docs)
	}
	if docs := reviewDocuments(t, onEnv); len(docs) != 1 {
		t.Fatalf("an enabled group wrote %v, want one document", docs)
	}
}

// TestDrainSkipsReviewForAnOptedOutSet: `"review": false` declines the drain's
// Code review for one set while the group stays enabled for every other — the
// Verifier's per-set opt-out, key for key. The set reaches the same terminal
// status it would have reached reviewed, because review gates nothing.
func TestDrainSkipsReviewForAnOptedOutSet(t *testing.T) {
	t.Parallel()
	env := setupRunTaskSetFixture(t, "demo", signOffSet())
	_, _, head := runtimeHead(t, env.deps(), env.root)
	writeManifestWithSetKeys(t, filepath.Join(env.tasksDir, "demo"), signOffSet(),
		map[string]any{"base_commit": head, "review": false})

	var buf bytes.Buffer
	opts := env.runTaskSetOpts(false, "", &buf)
	opts.TaskSetOverride = "demo"
	opts.ConfirmIn = strings.NewReader("2\n") // Complete the sign-off at the gate.
	opts.ConfirmOut = &buf
	opts.verifyRunner = func(string) (string, error) { return "VERDICT: PASS\n", nil }
	opts.reviewRunner = func(string) (string, string, error) {
		t.Fatal("an opted-out set must spawn no Reviewer")
		return "", "", nil
	}

	result, err := RunTaskSetWith(env.deps(), nil, func(string) (*config.Config, error) {
		return reviewEnabledConfig(), nil
	}, opts)
	if err != nil {
		t.Fatalf("RunTaskSetWith: %v", err)
	}
	if !result.TaskSetDone {
		t.Fatalf("result = %+v, want the completed sign-off to reach DONE", result)
	}
	if docs := reviewDocuments(t, env); len(docs) != 0 {
		t.Fatalf("an opted-out set wrote %v", docs)
	}
}

// TestReviewRunIsCapturedUnderItsOwnPhase: the Reviewer's invocation is filed as
// a Captured run under the `review` phase — the seam the shared fallback walk
// calls — and the lenses that read those runs give it a row of its own rather
// than folding a set-level run into implement spend.
func TestReviewRunIsCapturedUnderItsOwnPhase(t *testing.T) {
	t.Parallel()
	d, _, setDir := setupReviewFixture(t, time.Date(2026, 8, 16, 12, 0, 0, 0, time.UTC))
	m := LoadManifest(d, "demo", filepath.Join(setDir, "index.json"))

	invocation, err := ResolveAgentInvocationWithMode("claude", "", "prompt", "/rt", AgentOutputAuto)
	if err != nil {
		t.Fatalf("resolve invocation: %v", err)
	}
	rec := newStreamRecorder(io.Discard, fakeClock(time.Date(2026, 8, 16, 12, 0, 0, 0, time.UTC), 100*time.Millisecond))
	role := reviewerRole(d, io.Discard, setDir, "demo", "sha1")
	role.persistAnswer(rec, invocation, 1, streamOutcomeCompleted, "", 0, "## Naming")

	runs, err := listSetRuns(d, setDir)
	if err != nil {
		t.Fatalf("listSetRuns: %v", err)
	}
	if len(runs) != 1 {
		t.Fatalf("captured runs = %d, want the Reviewer's one", len(runs))
	}
	meta := runs[0].meta
	if meta.Phase != "review" || meta.TaskSetID != "demo" || meta.WorkSHA != "sha1" {
		t.Fatalf("run meta = %+v, want a set-level review run at sha1", meta)
	}
	if meta.Verdict != "" || meta.TaskFile != "" {
		t.Fatalf("run meta = %+v, want no verdict and no task file", meta)
	}

	// The spend lens gives it its own row and its own bucket: a review is not
	// implement spend, and pretending otherwise would overstate what a task cost.
	breakdown, err := buildSpendSetBreakdown(d, "demo", m)
	if err != nil {
		t.Fatalf("buildSpendSetBreakdown: %v", err)
	}
	if breakdown.ReviewRunCount != 1 || breakdown.ImplementRunCount != 0 {
		t.Fatalf("buckets = review %d / implement %d, want the run counted as review",
			breakdown.ReviewRunCount, breakdown.ImplementRunCount)
	}
	if len(breakdown.Rows) != 1 || breakdown.Rows[0].TaskID != "review" {
		t.Fatalf("rows = %+v, want one review row", breakdown.Rows)
	}

	// The stream lens groups it as a set-level run rather than failing to name a
	// task file it does not have.
	streams, err := readSetAttemptStreams(d, m, true)
	if err != nil {
		t.Fatalf("readSetAttemptStreams: %v", err)
	}
	if len(streams) != 1 || streams[0].TaskID != "review" {
		t.Fatalf("streams = %+v, want the review's own stream", streams)
	}
}

// TestReviewEpisodeArmsOnNewDoneAFKWork covers the episode rule itself: what
// disarms automatic review, and what brings it back.
func TestReviewEpisodeArmsOnNewDoneAFKWork(t *testing.T) {
	t.Parallel()
	reviewed := []Task{{ID: "01-a", File: "01-a.md", Title: "A", Type: "AFK", Status: "done"}}

	tests := []struct {
		name  string
		tasks []Task
		want  bool
	}{
		{
			name:  "the composition just reviewed",
			tasks: reviewed,
			want:  false,
		},
		{
			name: "a human sign-off landing beside it",
			tasks: append(append([]Task{}, reviewed...),
				Task{ID: "02-h", File: "02-h.md", Title: "Sign off", Type: "HITL", Status: "done"}),
			want: false,
		},
		{
			name: "an unfinished task appearing",
			tasks: append(append([]Task{}, reviewed...),
				Task{ID: "02-b", File: "02-b.md", Title: "B", Type: "AFK", Status: "open"}),
			want: false,
		},
		{
			name: "a verify-spawned remediation task landing",
			tasks: append(append([]Task{}, reviewed...),
				Task{ID: "02-remediation", File: "02-remediation.md", Title: "Fix findings", Type: "AFK", Status: "done"}),
			want: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			d, defPath, _ := setupReviewFixture(t, time.Date(2026, 8, 16, 12, 0, 0, 0, time.UTC))
			setupManifest(t, defPath, "demo", tt.tasks)
			m := LoadManifest(d, "demo", filepath.Join(defPath, "demo", "index.json"))

			recordReviewEpisode(d, nil, reviewEpisodeRecord("/repo/.git", "demo", "sha1", reviewComposition(&Manifest{Tasks: reviewed}), "/reviews/review.md", time.Now()))
			defer func() { _ = d.CloseStore() }()

			if got := reviewEpisodeArmed(d, "/repo/.git", "demo", reviewComposition(m)); got != tt.want {
				t.Fatalf("armed = %v, want %v", got, tt.want)
			}
		})
	}
}

// TestReviewEpisodeArmedWithoutARecordOrWork: a set nobody has reviewed is
// armed, and a set with no finished agent work never is — there is nothing whose
// standards a review could judge.
func TestReviewEpisodeArmedWithoutARecordOrWork(t *testing.T) {
	t.Parallel()
	d := newTestDeps(t)
	if !reviewEpisodeArmed(d, "/repo/.git", "demo", "01-a") {
		t.Fatal("an unreviewed set must be armed")
	}
	if reviewEpisodeArmed(d, "/repo/.git", "demo", "") {
		t.Fatal("a set with no done AFK work must never arm review")
	}
}

// TestHandReviewIgnoresTheEpisode: `pop tasks review <set>` is a human asking,
// so it runs at a composition the drain has already reviewed — and records the
// episode itself, because the rule is about the work having been reviewed.
func TestHandReviewIgnoresTheEpisode(t *testing.T) {
	t.Parallel()
	d, defPath, setDir := setupReviewFixture(t, time.Date(2026, 8, 16, 12, 0, 0, 0, time.UTC))
	m := LoadManifest(d, "demo", filepath.Join(setDir, "index.json"))
	composition := reviewComposition(m)
	recordReviewEpisode(d, nil, reviewEpisodeRecord("/repo/.git", "demo", "sha1", composition, "/reviews/old.md", time.Now()))
	defer func() { _ = d.CloseStore() }()

	opts := reviewOpts(defPath, &bytes.Buffer{}, func(string) (string, string, error) {
		return "## Still worth saying", "claude", nil
	})
	opts.Repo = "/repo/.git"
	res, err := reviewResolvedSet(d, nil, opts)
	if err != nil {
		t.Fatalf("a hand review must run regardless of the episode: %v", err)
	}
	if reviewEpisodeArmed(d, "/repo/.git", "demo", composition) {
		t.Fatal("a hand review must disarm automatic re-review of the same work")
	}
	s, err := openDrainStore(d)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	episode, err := s.GetReviewEpisode("/repo/.git", "demo")
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

// TestReviewEpisodeStoreRoundTrip pins the one thing the row must do: the latest
// review for a set replaces the previous one rather than accumulating.
func TestReviewEpisodeStoreRoundTrip(t *testing.T) {
	t.Parallel()
	d := newTestDeps(t)
	s, err := openDrainStore(d)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer func() { _ = d.CloseStore() }()

	for _, composition := range []string{"01-a", "01-a\n02-b"} {
		if err := s.PutReviewEpisode(store.ReviewEpisode{
			Repo: "/repo/.git", SetID: "demo", WorkSHA: "sha", Composition: composition,
			Document: "/reviews/" + composition + ".md", ReviewedAt: time.Unix(1, 0).UTC(),
		}); err != nil {
			t.Fatalf("PutReviewEpisode: %v", err)
		}
	}
	episode, err := s.GetReviewEpisode("/repo/.git", "demo")
	if err != nil || episode == nil {
		t.Fatalf("episode = %+v, %v", episode, err)
	}
	if episode.Composition != "01-a\n02-b" {
		t.Fatalf("composition = %q, want the latest review's", episode.Composition)
	}
}
