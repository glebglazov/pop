package tasks

import (
	"bytes"
	"context"
	"errors"
	"io"
	"strings"
	"testing"
	"time"
)

// scriptedReviewRun is one invocation the scripted Reviewer runner replays: the
// stream lines it prints, how the process ends, and whether it outlives the
// attempt timeout — the three endings a Reviewer attempt is judged on.
type scriptedReviewRun struct {
	output   string
	exitCode int
	runErr   error
	// hang keeps the process alive this long, so an attempt timeout fires first
	// and the prose already on the stream is what a timed-out run left behind.
	hang time.Duration
}

type scriptedReviewRunner struct {
	runs  []scriptedReviewRun
	calls int
}

func (r *scriptedReviewRunner) Run(ctx context.Context, dir string, stdout, stderr io.Writer, name string, args ...string) (int, error) {
	proc, err := r.Start(ctx, dir, stdout, stderr, name, args...)
	if err != nil {
		return 1, err
	}
	return proc.Wait()
}

func (r *scriptedReviewRunner) Start(ctx context.Context, dir string, stdout, stderr io.Writer, name string, args ...string) (*ManagedProcess, error) {
	r.calls++
	run := scriptedReviewRun{}
	if r.calls <= len(r.runs) {
		run = r.runs[r.calls-1]
	}
	for _, line := range strings.Split(run.output, "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		if _, err := stdout.Write([]byte(line + "\n")); err != nil {
			return nil, err
		}
	}
	proc := &ManagedProcess{done: make(chan waitResult, 1)}
	result := waitResult{exitCode: run.exitCode, err: run.runErr}
	if run.hang <= 0 {
		proc.done <- result
		return proc, nil
	}
	go func() {
		time.Sleep(run.hang)
		proc.done <- result
	}()
	return proc, nil
}

func (r *scriptedReviewRunner) StartWithEnv(ctx context.Context, dir string, env []string, stdout, stderr io.Writer, name string, args ...string) (*ManagedProcess, error) {
	return r.Start(ctx, dir, stdout, stderr, name, args...)
}

// claudeReviewStream is a claude JSON stream carrying body as the run's result.
func claudeReviewStream(body string) string {
	return `{"type":"system","subtype":"init"}` + "\n" +
		`{"type":"result","subtype":"success","result":"` + body + `"}`
}

func reviewRunnerDeps(t *testing.T, runs ...scriptedReviewRun) (*Deps, *scriptedReviewRunner) {
	t.Helper()
	runner := &scriptedReviewRunner{runs: runs}
	d := newTestDeps(t)
	d.Git = stubGit("sha1\n", "", "")
	d.LookPath = func(string) (string, error) { return "/bin/claude", nil }
	d.Runner = &probeNeutralRunner{inner: runner}
	return d, runner
}

func runScriptedReviewer(t *testing.T, d *Deps, taskSetDir string, timeout time.Duration) (string, error) {
	t.Helper()
	var out bytes.Buffer
	body, _, err := runConfiguredReviewer(d, nil, verifierSelection{
		Agents: []string{"claude"}, Effort: "heavy",
	}, taskSetDir, "demo", "sha1", "/rt", "prompt", &out, timeout, nil)
	return body, err
}

// TestReviewerRetryEligibilityMatchesTheVerifiers pins the two roles' rules side
// by side: they agree on every ending, and part on the one thing that is a
// format question — prose with no verdict in it is the Reviewer's whole answer
// and the Verifier's failure to answer.
func TestReviewerRetryEligibilityMatchesTheVerifiers(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name         string
		outcome      *attemptOutcome
		raw          string
		wantReviewer bool
		wantVerifier bool
	}{
		{
			name:         "a clean run with prose",
			outcome:      &attemptOutcome{},
			raw:          "## Naming\nThe helper names read well.",
			wantReviewer: false,
			wantVerifier: true,
		},
		{
			name:         "a clean run with nothing to write down",
			outcome:      &attemptOutcome{},
			raw:          "   \n",
			wantReviewer: true,
			wantVerifier: true,
		},
		{
			name:         "a timeout that left half a document",
			outcome:      &attemptOutcome{timedOut: true},
			raw:          "## Naming\nThe helper na",
			wantReviewer: true,
			wantVerifier: true,
		},
		{
			name:         "an agent that could not run",
			outcome:      &attemptOutcome{runErr: errors.New("agent crashed")},
			raw:          "## Naming",
			wantReviewer: true,
			wantVerifier: true,
		},
		{
			name:         "a non-zero exit",
			outcome:      &attemptOutcome{exitCode: 2},
			raw:          "## Naming",
			wantReviewer: true,
			wantVerifier: true,
		},
		{
			name:         "a timeout after a parsed verdict",
			outcome:      &attemptOutcome{timedOut: true},
			raw:          "VERDICT: PASS\nFINDINGS:\n",
			wantReviewer: true,
			wantVerifier: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := reviewAttemptRetryEligible(tc.outcome, tc.raw); got != tc.wantReviewer {
				t.Fatalf("reviewAttemptRetryEligible = %v, want %v", got, tc.wantReviewer)
			}
			if got := verifyAttemptRetryEligible(tc.outcome, tc.raw); got != tc.wantVerifier {
				t.Fatalf("verifyAttemptRetryEligible = %v, want %v", got, tc.wantVerifier)
			}
		})
	}
}

// TestReviewerRetriesATimedOutAttemptAndKeepsItsProse: a Reviewer that hangs
// past the attempt timeout is retried, and the fragment it left is never handed
// back as the set's document — the caller gets an error, so nothing supersedes
// the review the set already had.
func TestReviewerRetriesATimedOutAttemptAndKeepsItsProse(t *testing.T) {
	t.Parallel()
	taskSetDir := t.TempDir()
	hung := scriptedReviewRun{output: claudeReviewStream("## Naming\\nThe helper na"), hang: 150 * time.Millisecond}
	d, runner := reviewRunnerDeps(t, hung, hung, hung)

	body, err := runScriptedReviewer(t, d, taskSetDir, 20*time.Millisecond)
	if err == nil {
		t.Fatalf("a Reviewer that only ever timed out returned a document: %q", body)
	}
	if body != "" {
		t.Fatalf("body = %q, want nothing written from a timed-out attempt", body)
	}
	if runner.calls != 3 {
		t.Fatalf("agent invocations = %d, want the default cap of 3 tries", runner.calls)
	}
	if pairs := listRunFilePairs(t, capturedRunsDir(taskSetDir)); len(pairs) != 3 {
		t.Fatalf("captured runs = %d, want each timed-out attempt filed", len(pairs))
	}
}

// TestReviewerRetriesAFailedRunThenTakesTheCleanDocument: a run error and a
// non-zero exit are retried exactly as a timeout is, and the first run that
// reaches its own ending with prose is the answer — "any prose is an answer"
// still holds for a completed run.
func TestReviewerRetriesAFailedRunThenTakesTheCleanDocument(t *testing.T) {
	t.Parallel()
	taskSetDir := t.TempDir()
	d, runner := reviewRunnerDeps(t,
		scriptedReviewRun{output: claudeReviewStream("## Naming"), runErr: errors.New("agent crashed")},
		scriptedReviewRun{output: claudeReviewStream("## Naming"), exitCode: 2},
		scriptedReviewRun{output: claudeReviewStream("## Naming\\nThe helper names read well.")},
	)

	body, err := runScriptedReviewer(t, d, taskSetDir, time.Minute)
	if err != nil {
		t.Fatalf("runConfiguredReviewer: %v", err)
	}
	if !strings.Contains(body, "The helper names read well.") {
		t.Fatalf("body = %q, want the completed run's document", body)
	}
	if runner.calls != 3 {
		t.Fatalf("agent invocations = %d, want two failures then the answer", runner.calls)
	}
}

// TestReviewerTakesACleanRunOnTheFirstTry keeps the common path pinned: one
// completed run with prose spends one try and is the document.
func TestReviewerTakesACleanRunOnTheFirstTry(t *testing.T) {
	t.Parallel()
	taskSetDir := t.TempDir()
	d, runner := reviewRunnerDeps(t, scriptedReviewRun{output: claudeReviewStream("## Naming\\nAll good.")})

	body, err := runScriptedReviewer(t, d, taskSetDir, time.Minute)
	if err != nil {
		t.Fatalf("runConfiguredReviewer: %v", err)
	}
	if !strings.Contains(body, "All good.") {
		t.Fatalf("body = %q, want the completed run's document", body)
	}
	if runner.calls != 1 {
		t.Fatalf("agent invocations = %d, want one", runner.calls)
	}
}
