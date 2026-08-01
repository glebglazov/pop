package tasks

import (
	"bytes"
	"io"
	"strings"
	"testing"
	"time"
)

// A skipped attempt leaves a trace the stream lens can explain: its own outcome
// and the model that was refused, rather than a gap where the spawn happened
// (ADR-0168).
func TestStreamSurfacesASkippedRunAndNamesTheModel(t *testing.T) {
	env := streamFixture(t)
	d := env.deps()
	sel := &Selection{
		TaskSetID: "demo",
		TaskID:    "01-a",
		TaskFile:  "01-a.md",
		Manifest:  &Manifest{Dir: env.demoDir()},
	}
	start := time.Date(2026, 6, 10, 12, 0, 0, 0, time.UTC)
	rec := newStreamRecorder(io.Discard, fakeClock(start, 100*time.Millisecond))
	if _, err := rec.Write([]byte(`{"type":"system","subtype":"init"}` + "\n")); err != nil {
		t.Fatal(err)
	}
	rec.finish()

	if p := persistSkippedAttemptStream(d, io.Discard, sel, rec, "kimi", "kimi --model gated-model", "gated-model", 1, "does not have access to gated-model", 1); p == "" {
		t.Fatal("skipped attempt persisted nothing")
	}

	result, err := StreamWith(d, nil, nil, StreamOptions{
		ResolveInput: ResolveInput{CWD: env.root},
		Target:       "demo/01-a.md",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Tasks) != 1 || len(result.Tasks[0].Attempts) != 1 {
		t.Fatalf("stream result = %#v", result.Tasks)
	}
	timing := result.Tasks[0].Attempts[0].Timing
	if timing.Outcome != streamOutcomeModelSkipped {
		t.Fatalf("outcome = %q, want %s", timing.Outcome, streamOutcomeModelSkipped)
	}
	// The refusal never reached the model, so nothing in the events names it:
	// the recorded model is what keeps the row readable.
	if timing.ActualModel != "gated-model" {
		t.Fatalf("actual model = %q, want gated-model", timing.ActualModel)
	}

	var out bytes.Buffer
	RenderStream(&out, result, RenderStreamOptions{})
	rendered := out.String()
	if !strings.Contains(rendered, streamOutcomeModelSkipped) || !strings.Contains(rendered, "gated-model") {
		t.Fatalf("stream render names neither the skip nor the model:\n%s", rendered)
	}
}

// The dim line names both ends of the substitution, and falls back to the
// generic phrasing when the walk cannot name a successor.
func TestEffortModelSkipMessageNamesBothModels(t *testing.T) {
	v := NewModelRefusedVerdict("kimi", "gated-model", "gated", ProceedRecoveryPermanent)
	if got, want := v.effortModelSkipMessage("Agent", "runnable-model"), "Agent kimi cannot run gated-model; trying runnable-model instead"; got != want {
		t.Fatalf("effortModelSkipMessage = %q, want %q", got, want)
	}
	if got, want := v.effortModelSkipMessage("Agent", ""), v.fallThroughMessage("Agent"); got != want {
		t.Fatalf("unnamed successor = %q, want the generic %q", got, want)
	}
}

// Nothing about the task failed in a skipped run, so it teaches the next
// attempt nothing — it stays out of the prior-attempt digest the way a
// completed attempt does.
func TestDigestExcludesModelSkippedRun(t *testing.T) {
	d := realFSDeps()
	taskSetDir := t.TempDir()
	start := time.Date(2026, 6, 12, 10, 0, 0, 0, time.UTC)

	skipped := newStreamRecorder(io.Discard, fakeClock(start, 100*time.Millisecond))
	skipped.Write([]byte(`{"type":"assistant","message":{"content":[{"type":"text","text":"Refused before any work began."}]}}` + "\n"))
	skipped.finish()
	if _, _, err := writeCapturedRun(d, taskSetDir, "implement", "demo", "01-a", "01-a.md", skipped, "kimi", "kimi --model gated-model", "gated-model", 1, streamOutcomeModelSkipped, "does not have access to gated-model", 1, "", ""); err != nil {
		t.Fatal(err)
	}
	if digest := buildPriorAttemptDigest(d, taskSetDir, "01-a.md"); digest != "" {
		t.Fatalf("a skipped run poisoned the digest:\n%s", digest)
	}

	// The failing attempt that followed it still carries forward, alone.
	failed := newStreamRecorder(io.Discard, fakeClock(start.Add(time.Minute), 100*time.Millisecond))
	failed.Write([]byte(`{"type":"assistant","message":{"content":[{"type":"text","text":"Tried the retry loop."}]}}` + "\n"))
	failed.finish()
	writeTestNewFormatRun(t, d, taskSetDir, "01-a.md", failed, "claude", "claude", 1, streamOutcomeFailed, "missing TASK_COMPLETE sentinel", 0)

	digest := buildPriorAttemptDigest(d, taskSetDir, "01-a.md")
	if !strings.Contains(digest, "Tried the retry loop.") {
		t.Fatalf("the failed attempt is missing from the digest:\n%s", digest)
	}
	if strings.Contains(digest, "Refused before any work began.") {
		t.Fatalf("a skipped run poisoned the digest:\n%s", digest)
	}
}
