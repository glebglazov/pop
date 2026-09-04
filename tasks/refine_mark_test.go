package tasks

import (
	"encoding/json"
	"io"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// recordRefinePass files one Refiner invocation the way the shared fallback walk
// files it — the seam is the role's own, so what these tests read back is what a
// real pass leaves behind. An interrupted attempt yields no answer and takes the
// walk's other persist path.
func recordRefinePass(t *testing.T, d *Deps, setDir string, at time.Time, streamOutcome, answer string) {
	t.Helper()
	invocation, err := ResolveAgentInvocationWithMode("claude", "", "prompt", "/rt", AgentOutputAuto)
	if err != nil {
		t.Fatalf("resolve invocation: %v", err)
	}
	rec := newStreamRecorder(io.Discard, fakeClock(at, 100*time.Millisecond))
	role := refinerRole(d, io.Discard, setDir, "demo", "sha1")
	if streamOutcome == streamOutcomeInterrupted {
		role.persist(rec, invocation, 1, streamOutcome, "", 0)
		return
	}
	role.persistAnswer(rec, invocation, 1, streamOutcome, "", 0, answer)
}

func refineReply(outcome string) string {
	return "REFINE-OUTCOME: " + outcome + "\n\n## Fixed\n\nNothing needed fixing.\n"
}

// TestRefineMarkSaysWhetherTheStandardWasApplied drives every way a pass can end
// through the run pop files for it, and asks the read-side resolution what a
// human at a gate would be told (ADR-0260). The four reasons a set is not
// refined are each recoverable beside the one mark.
func TestRefineMarkSaysWhetherTheStandardWasApplied(t *testing.T) {
	t.Parallel()
	at := time.Date(2026, 9, 5, 12, 0, 0, 0, time.UTC)
	for _, tc := range []struct {
		name string
		// stream is the run's own ending, empty for a set no pass ever ran on.
		stream   string
		answer   string
		wantMark RefineMark
		wantWhy  UnrefinedReason
	}{
		{name: "no pass has ever run", wantMark: RefineMarkNotRefined, wantWhy: UnrefinedNeverRan},
		{name: "refined", stream: streamOutcomeCompleted, answer: refineReply("refined"), wantMark: RefineMarkRefined, wantWhy: UnrefinedReasonNone},
		{name: "the Refiner reported no outcome", stream: streamOutcomeCompleted, answer: "## Fixed\n\nNothing.\n", wantMark: RefineMarkRefined, wantWhy: UnrefinedReasonNone},
		{name: "the gate was already red", stream: streamOutcomeCompleted, answer: refineReply("gate-blocked"), wantMark: RefineMarkNotRefined, wantWhy: UnrefinedGateBlocked},
		{name: "the pass gave up", stream: streamOutcomeCompleted, answer: refineReply("abandoned"), wantMark: RefineMarkNotRefined, wantWhy: UnrefinedAbandoned},
		{name: "the human interrupted it", stream: streamOutcomeInterrupted, wantMark: RefineMarkNotRefined, wantWhy: UnrefinedInterrupted},
		{name: "it ran clean and produced nothing", stream: streamOutcomeCompleted, wantMark: RefineMarkNotRefined, wantWhy: UnrefinedInterrupted},
		// Prose from an attempt that died is never published as a report, so the
		// outcome that prose claims says nothing about the changeset.
		{name: "it died holding half a report", stream: streamOutcomeTimedOut, answer: refineReply("refined"), wantMark: RefineMarkNotRefined, wantWhy: UnrefinedInterrupted},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d, _, setDir := setupRefineFixture(t, at)
			if tc.stream != "" {
				recordRefinePass(t, d, setDir, at, tc.stream, tc.answer)
			}
			m := LoadManifest(d, "demo", filepath.Join(setDir, "index.json"))
			got := ResolveRefineMark(d, refineEnabledConfig(), m)
			if got.Mark != tc.wantMark || got.Reason != tc.wantWhy {
				t.Fatalf("refine mark = %q/%q, want %q/%q", got.Mark, got.Reason, tc.wantMark, tc.wantWhy)
			}
		})
	}
}

// TestRefineMarkFollowsTheLatestPass: a set is refined when the pass that ran
// last refined it, whichever way the ones before it ended, and a later failure
// takes the mark away again.
func TestRefineMarkFollowsTheLatestPass(t *testing.T) {
	t.Parallel()
	first := time.Date(2026, 9, 5, 12, 0, 0, 0, time.UTC)
	second := first.Add(time.Hour)

	d, _, setDir := setupRefineFixture(t, first)
	recordRefinePass(t, d, setDir, first, streamOutcomeInterrupted, "")
	recordRefinePass(t, d, setDir, second, streamOutcomeCompleted, refineReply("refined"))
	m := LoadManifest(d, "demo", filepath.Join(setDir, "index.json"))
	if got := ResolveRefineMark(d, refineEnabledConfig(), m); got.Mark != RefineMarkRefined {
		t.Fatalf("refine mark after a retry that refined = %q/%q, want refined", got.Mark, got.Reason)
	}

	d2, _, setDir2 := setupRefineFixture(t, first)
	recordRefinePass(t, d2, setDir2, first, streamOutcomeCompleted, refineReply("refined"))
	recordRefinePass(t, d2, setDir2, second, streamOutcomeCompleted, refineReply("gate-blocked"))
	m2 := LoadManifest(d2, "demo", filepath.Join(setDir2, "index.json"))
	got := ResolveRefineMark(d2, refineEnabledConfig(), m2)
	if got.Mark != RefineMarkNotRefined || got.Reason != UnrefinedGateBlocked {
		t.Fatalf("refine mark after a gate-blocked pass = %q/%q, want not-refined/gate-blocked", got.Mark, got.Reason)
	}
}

// TestRefineMarkIsAbsentWhereRefineDoesNotApply: the absent mark is a third
// answer, not a quiet "not refined". A set nothing was going to refine is not a
// set that failed to be refined.
func TestRefineMarkIsAbsentWhereRefineDoesNotApply(t *testing.T) {
	t.Parallel()
	at := time.Date(2026, 9, 5, 12, 0, 0, 0, time.UTC)
	d, _, setDir := setupRefineFixture(t, at)
	m := LoadManifest(d, "demo", filepath.Join(setDir, "index.json"))

	if got := ResolveRefineMark(d, nil, m); got.Mark != RefineMarkNone {
		t.Fatalf("refine disabled: mark = %q, want none", got.Mark)
	}

	optedOut := LoadManifest(d, "demo", filepath.Join(setDir, "index.json"))
	optedOut.Unknown = map[string]json.RawMessage{"refine": json.RawMessage("false")}
	if got := ResolveRefineMark(d, refineEnabledConfig(), optedOut); got.Mark != RefineMarkNone {
		t.Fatalf("opted out: mark = %q, want none", got.Mark)
	}

	unfinished := LoadManifest(d, "demo", filepath.Join(setDir, "index.json"))
	unfinished.Tasks[0].Status = TaskOpen
	if got := ResolveRefineMark(d, refineEnabledConfig(), unfinished); got.Mark != RefineMarkNone {
		t.Fatalf("unfinished set: mark = %q, want none", got.Mark)
	}
}

// TestSignOffGateReportsTheRefineMark: the human at the sign-off gate is told
// whether the set was refined, is told in words when the reason was a gate that
// was already red — and is refused nothing on account of either (ADR-0260
// decisions 3 and 6).
func TestSignOffGateReportsTheRefineMark(t *testing.T) {
	t.Parallel()
	at := time.Date(2026, 9, 5, 12, 0, 0, 0, time.UTC)

	d, m := hitlFixture(t)
	recordRefinePass(t, d, m.Dir, at, streamOutcomeCompleted, refineReply("gate-blocked"))
	gate, action := hitlGateOutputWithConfig(t, d, m, refineEnabledConfig(), "2\n")
	if !strings.Contains(gate, "Not refined: the scoped gate was already red when the pass began") {
		t.Fatalf("gate does not say the gate was red:\n%s", gate)
	}
	if action != hitlGateComplete {
		t.Fatalf("gate action = %v, want the human's own choice to stand", action)
	}

	refined, mRefined := hitlFixture(t)
	recordRefinePass(t, refined, mRefined.Dir, at, streamOutcomeCompleted, refineReply("refined"))
	gate, _ = hitlGateOutputWithConfig(t, refined, mRefined, refineEnabledConfig(), "0\n")
	if !strings.Contains(gate, "\U0001F4DD Refined") {
		t.Fatalf("gate does not report the set as refined:\n%s", gate)
	}

	never, mNever := hitlFixture(t)
	gate, _ = hitlGateOutputWithConfig(t, never, mNever, refineEnabledConfig(), "0\n")
	if !strings.Contains(gate, "\U0001F4DD Not refined") || strings.Contains(gate, "already red") {
		t.Fatalf("gate does not report an unrefined set plainly:\n%s", gate)
	}
}
