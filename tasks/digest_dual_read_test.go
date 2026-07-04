package tasks

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// Helper to write a new-format captured run for testing.
// taskSetDir is the task-set directory, taskFile is the task file name,
// and rec holds the recorded events.
func writeTestNewFormatRun(t *testing.T, d *Deps, taskSetDir, taskFile string, rec *streamRecorder, agent, requestedAgent string, attempt int, outcome, reason string, exitCode int) {
	t.Helper()
	stem := strings.TrimSuffix(taskFile, filepath.Ext(taskFile))
	sel := &Selection{
		TaskSetID: "demo",
		TaskID:    stem,
		TaskFile:  taskFile,
		Manifest:  &Manifest{Dir: taskSetDir},
	}
	if _, _, err := writeCapturedRun(d, taskSetDir, "implement", sel.TaskSetID, sel.TaskID, sel.TaskFile, rec, agent, requestedAgent, attempt, outcome, reason, exitCode, "", ""); err != nil {
		t.Fatal(err)
	}
}

// writeTestLegacyRun writes a legacy-format attempt stream file for testing.
func writeTestLegacyRun(t *testing.T, taskSetDir, taskFile, agent string, attempt int, start time.Time, outcome string, reason string, exitCode int, events []streamEventRecord) {
	t.Helper()
	if events == nil {
		events = []streamEventRecord{
			{Type: "event", AtMS: 5, Raw: `{"type":"system","subtype":"init"}`},
		}
	}
	dur := 30_000
	writeTimingStreamRecords(t, taskStreamDir(taskSetDir, taskFile), "attempt-001.jsonl.gz",
		streamHeaderRecord{Type: "header", Agent: agent, Attempt: attempt, StartTime: start.UTC()},
		events,
		streamFooterRecord{Type: "footer", Outcome: outcome, DurationMS: int64(dur), Reason: reason, ExitCode: exitCode})
}

func TestDigestReadsNewFormatRunOnly(t *testing.T) {
	d := realFSDeps()
	taskSetDir := t.TempDir()
	start := time.Date(2026, 6, 12, 10, 0, 0, 0, time.UTC)

	// Write a new-format captured run (failed outcome, missing sentinel →
	// continue lesson).
	rec := newStreamRecorder(io.Discard, fakeClock(start, 100*time.Millisecond))
	if _, err := rec.Write([]byte(`{"type":"assistant","message":{"content":[{"type":"text","text":"I tried to fix the flaky test by adding a retry loop."}]}}` + "\n")); err != nil {
		t.Fatal(err)
	}
	if _, err := rec.Write([]byte(`{"type":"assistant","message":{"content":[{"type":"tool_use","id":"t1","name":"Bash","input":{"command":"go test -count=1 ./pkg"}}]}}` + "\n")); err != nil {
		t.Fatal(err)
	}
	rec.finish()

	writeTestNewFormatRun(t, d, taskSetDir, "01-a.md", rec, "claude", "claude", 1, streamOutcomeFailed, "missing TASK_COMPLETE sentinel", 0)

	digest := buildPriorAttemptDigest(d, taskSetDir, "01-a.md")
	if digest == "" {
		t.Fatal("expected a digest from a new-format captured run, got empty")
	}
	if !strings.Contains(digest, "Attempt 1") {
		t.Fatal("expected Attempt 1 in digest")
	}
	if !strings.Contains(digest, lessonContinue) {
		t.Fatalf("expected continue lesson in digest, got:\n%s", digest)
	}
	if !strings.Contains(digest, "I tried to fix the flaky test") {
		t.Fatalf("expected narrative text in digest, got:\n%s", digest)
	}
}

func TestDigestMixedLegacyAndNewFormat(t *testing.T) {
	d := realFSDeps()
	taskSetDir := t.TempDir()

	base := time.Date(2026, 6, 12, 10, 0, 0, 0, time.UTC)

	// Legacy failed run at base+0min
	writeTestLegacyRun(t, taskSetDir, "01-a.md", "claude", 1, base, streamOutcomeFailed, "missing TASK_COMPLETE sentinel", 0,
		[]streamEventRecord{
			{Type: "event", AtMS: 100, Raw: `{"type":"assistant","message":{"content":[{"type":"text","text":"Legacy approach: patching the config parser."}]}}`},
		})

	// New-format failed run at base+5min
	rec := newStreamRecorder(io.Discard, fakeClock(base.Add(5*time.Minute), 100*time.Millisecond))
	rec.Write([]byte(`{"type":"assistant","message":{"content":[{"type":"text","text":"New approach: rewriting the validation logic."}]}}` + "\n"))
	rec.finish()
	writeTestNewFormatRun(t, d, taskSetDir, "01-a.md", rec, "claude", "claude", 2, streamOutcomeFailed, "missing TASK_COMPLETE sentinel", 0)

	digest := buildPriorAttemptDigest(d, taskSetDir, "01-a.md")
	if digest == "" {
		t.Fatal("expected a digest from mixed sources, got empty")
	}

	// Both attempts should appear in the digest.
	if !strings.Contains(digest, "Attempt 1") {
		t.Fatalf("expected Attempt 1 (legacy) in digest, got:\n%s", digest)
	}
	if !strings.Contains(digest, "Attempt 2") {
		t.Fatalf("expected Attempt 2 (new-format) in digest, got:\n%s", digest)
	}

	// Both narratives should be present.
	if !strings.Contains(digest, "Legacy approach: patching the config parser.") {
		t.Fatalf("expected legacy narrative in digest, got:\n%s", digest)
	}
	if !strings.Contains(digest, "New approach: rewriting the validation logic.") {
		t.Fatalf("expected new-format narrative in digest, got:\n%s", digest)
	}
}

func TestDigestMixedSourcesRespectsResetCut(t *testing.T) {
	d := realFSDeps()
	taskSetDir := t.TempDir()

	base := time.Date(2026, 6, 12, 10, 0, 0, 0, time.UTC)
	resetAt := time.Date(2026, 6, 12, 11, 0, 0, 0, time.UTC)
	postReset := time.Date(2026, 6, 12, 12, 0, 0, 0, time.UTC)

	// Pre-reset legacy run.
	writeTestLegacyRun(t, taskSetDir, "01-a.md", "claude", 1, base, streamOutcomeFailed, "wrong direction", 0,
		[]streamEventRecord{
			{Type: "event", AtMS: 100, Raw: `{"type":"assistant","message":{"content":[{"type":"text","text":"Pre-reset legacy approach."}]}}`},
		})

	// Pre-reset new-format run.
	rec := newStreamRecorder(io.Discard, fakeClock(base.Add(30*time.Minute), 100*time.Millisecond))
	rec.Write([]byte(`{"type":"assistant","message":{"content":[{"type":"text","text":"Pre-reset new-format approach."}]}}` + "\n"))
	rec.finish()
	writeTestNewFormatRun(t, d, taskSetDir, "01-a.md", rec, "claude", "claude", 2, streamOutcomeFailed, "missing TASK_COMPLETE sentinel", 0)

	// Post-reset new-format run.
	rec2 := newStreamRecorder(io.Discard, fakeClock(postReset, 100*time.Millisecond))
	rec2.Write([]byte(`{"type":"assistant","message":{"content":[{"type":"text","text":"Post-reset fresh approach."}]}}` + "\n"))
	rec2.finish()
	writeTestNewFormatRun(t, d, taskSetDir, "01-a.md", rec2, "claude", "claude", 3, streamOutcomeFailed, "missing TASK_COMPLETE sentinel", 0)

	// Write progress.txt with a RESET at resetAt.
	progress := resetAt.Format(time.RFC3339) + " [01-a.md] RESET\nreset demo/01-a to open (was failed)\n---\n"
	if err := os.WriteFile(filepath.Join(taskSetDir, "progress.txt"), []byte(progress), 0o644); err != nil {
		t.Fatal(err)
	}

	digest := buildPriorAttemptDigest(d, taskSetDir, "01-a.md")

	// Pre-reset attempts should be excluded.
	if strings.Contains(digest, "Pre-reset legacy approach") {
		t.Fatal("pre-reset legacy run should be excluded from digest")
	}
	if strings.Contains(digest, "Pre-reset new-format approach") {
		t.Fatal("pre-reset new-format run should be excluded from digest")
	}

	// Post-reset attempt should be included.
	if !strings.Contains(digest, "Post-reset fresh approach") {
		t.Fatalf("post-reset new-format run should be included in digest, got:\n%s", digest)
	}
	if !strings.Contains(digest, "Attempt 3") {
		t.Fatalf("expected Attempt 3 (post-reset) in digest, got:\n%s", digest)
	}
}

func TestDigestNewFormatInterruptedRun(t *testing.T) {
	d := realFSDeps()
	taskSetDir := t.TempDir()
	start := time.Date(2026, 6, 12, 10, 0, 0, 0, time.UTC)

	// New-format interrupted run.
	rec := newStreamRecorder(io.Discard, fakeClock(start, 100*time.Millisecond))
	rec.Write([]byte(`{"type":"assistant","message":{"content":[{"type":"text","text":"Partial edit of the config file."}]}}` + "\n"))
	rec.Write([]byte(`{"type":"assistant","message":{"content":[{"type":"text","text":"Still working on the fix."}]}}` + "\n"))
	rec.finish()
	writeTestNewFormatRun(t, d, taskSetDir, "01-a.md", rec, "claude", "claude", 1, streamOutcomeInterrupted, "", 143)

	digest := buildPriorAttemptDigest(d, taskSetDir, "01-a.md")
	if digest == "" {
		t.Fatal("expected a digest from a new-format interrupted run, got empty")
	}
	if !strings.Contains(digest, "Attempt 1") {
		t.Fatal("expected Attempt 1 in digest")
	}
	if !strings.Contains(digest, lessonResume) {
		t.Fatalf("expected resume lesson in digest, got:\n%s", digest)
	}
	if !strings.Contains(digest, "Still working on the fix.") {
		t.Fatalf("expected narrative in digest, got:\n%s", digest)
	}
}

func TestDigestNewFormatTimedOutRun(t *testing.T) {
	d := realFSDeps()
	taskSetDir := t.TempDir()
	start := time.Date(2026, 6, 12, 10, 0, 0, 0, time.UTC)

	// New-format timed-out run.
	rec := newStreamRecorder(io.Discard, fakeClock(start, 100*time.Millisecond))
	rec.Write([]byte(`{"type":"assistant","message":{"content":[{"type":"text","text":"The suite is still running, polling again."}]}}` + "\n"))
	rec.finish()
	writeTestNewFormatRun(t, d, taskSetDir, "01-a.md", rec, "claude", "claude", 1, streamOutcomeTimedOut, "timed out after 1h0m0s", -1)

	digest := buildPriorAttemptDigest(d, taskSetDir, "01-a.md")
	if digest == "" {
		t.Fatal("expected a digest from a new-format timed-out run, got empty")
	}
	if !strings.Contains(digest, "Attempt 1") {
		t.Fatal("expected Attempt 1 in digest")
	}
	if !strings.Contains(digest, lessonContinue) {
		t.Fatalf("expected continue lesson for timed-out run, got:\n%s", digest)
	}
	if !strings.Contains(digest, "The suite is still running, polling again.") {
		t.Fatalf("expected narrative in digest, got:\n%s", digest)
	}
}
