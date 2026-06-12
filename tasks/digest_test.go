package tasks

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestAttemptLessonMapsEachOutcomeReasonClass(t *testing.T) {
	cases := []struct {
		name     string
		outcome  string
		reason   string
		exitCode int
		want     string
	}{
		{"timeout is a contract failure on a sound approach", streamOutcomeTimedOut, "timed out after 1h0m0s", -1, lessonContinue},
		{"missing sentinel continues", streamOutcomeFailed, "missing TASK_COMPLETE sentinel", 0, lessonContinue},
		{"missing summary continues", streamOutcomeFailed, "missing or empty summary block", 0, lessonContinue},
		{"unchecked acceptance continues", streamOutcomeFailed, "acceptance criteria not all checked", 0, lessonContinue},
		{"generic contract verdict continues", streamOutcomeFailed, "agent output did not satisfy completion contract", 0, lessonContinue},
		{"crash exit reassesses", streamOutcomeFailed, "agent exited with status 2", 2, lessonReassess},
		{"empty output reassesses", streamOutcomeFailed, "empty agent output", 0, lessonReassess},
		{"no reason reassesses", streamOutcomeFailed, "", 0, lessonReassess},
		{"agent TASK_FAILED pivots with its reason", streamOutcomeFailed, "schema migration is incompatible", 0, "pivot/reassess: schema migration is incompatible"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := attemptLesson(tc.outcome, tc.reason, tc.exitCode); got != tc.want {
				t.Fatalf("attemptLesson(%q,%q,%d) = %q; want %q", tc.outcome, tc.reason, tc.exitCode, got, tc.want)
			}
		})
	}
}

// claudeAssistantEvent builds a stream event carrying one assistant text block,
// mirroring the stream-json shape the live renderer parses.
func claudeAssistantEvent(atMS int64, text string) streamEventRecord {
	return streamEventRecord{Type: "event", AtMS: atMS, Raw: `{"type":"assistant","message":{"content":[{"type":"text","text":` + jsonString(text) + `}]}}`}
}

func claudeToolEvent(atMS int64, name, cmd string) streamEventRecord {
	return streamEventRecord{Type: "event", AtMS: atMS, Raw: `{"type":"assistant","message":{"content":[{"type":"tool_use","id":"t1","name":` + jsonString(name) + `,"input":{"command":` + jsonString(cmd) + `}}]}}`}
}

func TestBuildPriorAttemptDigestNarrativeAndTag(t *testing.T) {
	dir := t.TempDir()
	streamDir := taskStreamDir(dir, "01-a.md")
	start := time.Date(2026, 6, 12, 10, 0, 0, 0, time.UTC)
	writeTimingStreamRecords(t, streamDir, "attempt-001.jsonl.gz",
		streamHeaderRecord{Type: "header", Agent: "claude", Attempt: 1, StartTime: start},
		[]streamEventRecord{
			claudeAssistantEvent(10, "Reading the failing suite to find the flake."),
			claudeToolEvent(20, "Bash", "go test ./pkg -run TestFlaky"),
			claudeAssistantEvent(30, "Suite is still running; polling again."),
		},
		streamFooterRecord{Type: "footer", Outcome: streamOutcomeTimedOut, DurationMS: 3600000, Reason: "timed out after 1h0m0s", ExitCode: -1})

	digest := buildPriorAttemptDigest(defaultDeps, dir, "01-a.md")
	if digest == "" {
		t.Fatal("expected a digest for a prior timed-out attempt")
	}
	if !strings.Contains(digest, "Attempt 1 — "+lessonContinue) {
		t.Fatalf("expected continue lesson on attempt 1, got:\n%s", digest)
	}
	for _, want := range []string{
		"Suite is still running; polling again.",
		"→ Bash go test ./pkg -run TestFlaky",
	} {
		if !strings.Contains(digest, want) {
			t.Fatalf("expected narrative to contain %q, got:\n%s", want, digest)
		}
	}
}

func TestBuildPriorAttemptDigestScopesSinceLastReset(t *testing.T) {
	dir := t.TempDir()
	streamDir := taskStreamDir(dir, "01-a.md")

	preReset := time.Date(2026, 6, 12, 9, 0, 0, 0, time.UTC)
	resetAt := time.Date(2026, 6, 12, 10, 0, 0, 0, time.UTC)
	postReset := time.Date(2026, 6, 12, 11, 0, 0, 0, time.UTC)

	writeTimingStreamRecords(t, streamDir, "attempt-001.jsonl.gz",
		streamHeaderRecord{Type: "header", Agent: "claude", Attempt: 1, StartTime: preReset},
		[]streamEventRecord{claudeAssistantEvent(10, "Abandoned line of attack from before the reset.")},
		streamFooterRecord{Type: "footer", Outcome: streamOutcomeFailed, DurationMS: 100, Reason: "wrong direction entirely", ExitCode: 0})
	writeTimingStreamRecords(t, streamDir, "attempt-002.jsonl.gz",
		streamHeaderRecord{Type: "header", Agent: "claude", Attempt: 2, StartTime: postReset},
		[]streamEventRecord{claudeAssistantEvent(10, "Fresh approach after the human reopened the task.")},
		streamFooterRecord{Type: "footer", Outcome: streamOutcomeFailed, DurationMS: 100, Reason: "missing TASK_COMPLETE sentinel", ExitCode: 0})

	// A RESET in the Progress record cuts off everything up to its timestamp.
	progress := resetAt.Format(time.RFC3339) + " [01-a.md] RESET\nreset demo/01-a to open (was failed)\n---\n"
	if err := os.WriteFile(filepath.Join(dir, "progress.txt"), []byte(progress), 0o644); err != nil {
		t.Fatal(err)
	}

	digest := buildPriorAttemptDigest(defaultDeps, dir, "01-a.md")
	if strings.Contains(digest, "Abandoned line of attack") || strings.Contains(digest, "Attempt 1") {
		t.Fatalf("pre-reset attempt should be excluded, got:\n%s", digest)
	}
	if !strings.Contains(digest, "Fresh approach after the human reopened") {
		t.Fatalf("post-reset attempt should be included, got:\n%s", digest)
	}
	if !strings.Contains(digest, "Attempt 2 — "+lessonContinue) {
		t.Fatalf("expected post-reset attempt 2 with continue lesson, got:\n%s", digest)
	}
}

func TestBuildPriorAttemptDigestEmptyWhenNoStreams(t *testing.T) {
	dir := t.TempDir()
	if digest := buildPriorAttemptDigest(defaultDeps, dir, "01-a.md"); digest != "" {
		t.Fatalf("expected empty digest with no streams, got:\n%s", digest)
	}
}
