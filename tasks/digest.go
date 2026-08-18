package tasks

import (
	"bytes"
	"compress/gzip"
	"encoding/json"
	"fmt"
	"io"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// Prior-attempt lessons. The contract lesson keeps a sound approach on track —
// the dominant real failure is a correct line of attack cut off before the
// completion sentinel (a backgrounded suite, a timeout), where "try a different
// angle" is exactly the wrong instruction (ADR 0040). The reassess lesson is
// for a crash or an empty session, where there is no approach to stand on.
// A contract failure keeps the approach but names what the contract wanted:
// the harness knows exactly which clause the attempt missed, and a lesson that
// withholds it sends the retry back over ground that was already sound.
const (
	lessonContinue        = "continue — your approach stood, finish and close out the sentinel"
	lessonUncheckedBoxes  = "continue — your approach stood and the work landed; the attempt failed only because the task file still had unticked acceptance boxes. Tick every `- [ ]` under \"Acceptance criteria\" to `- [x]` in the task file, then print the summary block and TASK_COMPLETE."
	lessonMissingSentinel = "continue — your approach stood; the attempt ended without any line opening on TASK_COMPLETE. Do the remaining work, then close out with the sentinel exactly as the prompt spells it: it starts its own line, and anything you add after it belongs on that line or below."
	lessonMissingSummary  = "continue — your approach stood; the attempt printed no usable SUMMARY_START…SUMMARY_END block. Close out with a non-empty summary block above TASK_COMPLETE."
	lessonReassess        = "reassess"
	lessonResume          = "resume — this attempt was cut off mid-flight (not a failure). The runtime checkout already holds the partial changes; read the uncommitted working-tree diff first and continue from it."
	// lessonTurnCapExhausted is the retry's whole reason to know a Turn cap
	// exists: the previous attempt ran out of turns rather than out of ideas, so
	// the approach stands and the budget is the thing to spend differently
	// (ADR-0190).
	lessonTurnCapExhausted = "resume — the previous attempt was cut short at its turn cap (not a failure): it ran out of turns before it could finish. Its changes are already in the runtime checkout; read the uncommitted working-tree diff first, continue from it, and spend your own turns on the remaining work rather than re-deriving what it did."
)

// priorAttempt is one in-scope prior attempt of the same task, summarized for
// the retry digest: its ordinal, the failure-type lesson derived from the
// footer, and a short tail of the approach narrative.
type priorAttempt struct {
	Attempt   int
	Lesson    string
	Narrative string
	sortKey   time.Time
}

// attemptLesson maps a footer's (outcome, reason, exitCode) to the failure-type
// lesson the next attempt should carry (ADR 0040). A timeout is a contract
// failure on a (presumed) sound approach, so it continues; a non-zero exit is a
// crash, so it reassesses; the harness-generated contract verdicts continue;
// anything else with a clean exit is the agent's own TASK_FAILED text, which
// pivots and carries that reason forward.
func attemptLesson(outcome, reason string, exitCode int) string {
	r := strings.TrimSpace(reason)
	switch {
	case outcome == streamOutcomeTurnCapExhausted:
		return lessonTurnCapExhausted
	case outcome == streamOutcomeInterrupted || outcome == streamOutcomeQuotaPaused || outcome == streamOutcomeAgentUnusable:
		return lessonResume
	case outcome == streamOutcomeTimedOut:
		return lessonContinue
	case exitCode != 0:
		return lessonReassess
	case isContractReason(r):
		return contractLesson(r)
	case r == "" || r == reasonEmptyOutput:
		return lessonReassess
	default:
		return "pivot/reassess: " + r
	}
}

// isContractReason reports whether a failure reason is one the harness produced
// from the completion contract (a missing sentinel/summary or unchecked
// acceptance) rather than the agent's own TASK_FAILED text. These are the
// finishing-line failures, so they keep the approach and continue.
func isContractReason(reason string) bool {
	switch reason {
	case reasonMissingSentinel, reasonMissingSummary, reasonUncheckedBoxes, reasonContractUnmet:
		return true
	}
	return false
}

// contractLesson picks the continue-lesson for one contract failure. Each
// harness-recorded reason has its own actionable form; anything else that
// reaches the contract bucket (the generic verdict, a reason recorded by an
// older pop) keeps the approach and carries its reason forward verbatim.
func contractLesson(reason string) string {
	switch reason {
	case reasonUncheckedBoxes:
		return lessonUncheckedBoxes
	case reasonMissingSentinel:
		return lessonMissingSentinel
	case reasonMissingSummary:
		return lessonMissingSummary
	default:
		return lessonContinue + " — the attempt failed the completion contract: " + reason
	}
}

// buildPriorAttemptDigest derives the prompt section that carries this task's
// own prior-attempt story into a retry (ADR 0040). It reads the task's Captured
// runs (ADR-0094) and legacy attempt stream files, scopes them to attempts
// since the latest Open-task reset (a human reopens precisely because the
// prior line of attack was abandoned), and renders a failure-type lesson plus
// a short approach narrative per attempt. Returns "" when there is nothing to
// carry — the caller injects it only on attempt > 1, and the agent never sees
// a raw stream file (ADR 0020).
func buildPriorAttemptDigest(d *Deps, taskSetDir, taskFile string) string {
	runs, err := listTaskRuns(d, taskSetDir, taskFile)
	if err != nil {
		return ""
	}
	cut := latestResetTime(d, taskSetDir, taskFile)

	var attempts []priorAttempt
	for _, run := range runs {
		// Drop attempts from the abandoned line of attack: a human reopen marks
		// everything up to its RESET timestamp stale.
		if !cut.IsZero() && !run.meta.StartTime.After(cut) {
			continue
		}
		// Captured attempts from failed, timed-out, interrupted, and quota-
		// paused runs all feed a retry, and so does one cut short at its Turn cap:
		// the retry needs told that the work stopped for want of turns rather than
		// for want of an approach (ADR-0190). Completed attempts are intentionally
		// excluded: they have no lesson to teach (ADR 0040/ADR 0089). So is a
		// model-skipped run: the provider refused the model before the agent
		// said anything about the task, so carrying it forward would teach the
		// next attempt a lesson about a failure that never happened (ADR-0168).
		if run.meta.Outcome != streamOutcomeFailed && run.meta.Outcome != streamOutcomeTimedOut &&
			run.meta.Outcome != streamOutcomeInterrupted && run.meta.Outcome != streamOutcomeQuotaPaused &&
			run.meta.Outcome != streamOutcomeAgentUnusable && run.meta.Outcome != streamOutcomeTurnCapExhausted {
			continue
		}
		attempts = append(attempts, priorAttempt{
			Attempt:   run.meta.Attempt,
			Lesson:    attemptLesson(run.meta.Outcome, run.meta.Reason, run.meta.ExitCode),
			Narrative: attemptNarrative(run.meta.Agent, run.events),
			sortKey:   run.meta.StartTime,
		})
	}
	if len(attempts) == 0 {
		return ""
	}
	sort.SliceStable(attempts, func(i, j int) bool { return attempts[i].sortKey.Before(attempts[j].sortKey) })
	return formatPriorAttemptDigest(attempts)
}

// latestResetTime returns the timestamp of the most recent Open-task reset for
// one task from the Progress record, or the zero time when the task has no
// recorded reset. It is the cut for the prior-attempt digest's since-last-reset
// scoping (ADR 0040).
func latestResetTime(d *Deps, taskSetDir, taskFile string) time.Time {
	data, err := d.FS.ReadFile(filepath.Join(taskSetDir, "progress.txt"))
	if err != nil {
		return time.Time{}
	}
	var latest time.Time
	for _, rec := range parseProgressRecords(string(data)) {
		if rec.File != taskFile || rec.Outcome != "RESET" {
			continue
		}
		t, err := time.Parse(time.RFC3339, rec.Timestamp)
		if err != nil {
			continue
		}
		if t.After(latest) {
			latest = t
		}
	}
	return latest
}

// attemptNarrative renders the approach narrative for one attempt — its
// assistant text plus tool-use ticks — from the stored stream events, reusing
// the agent's live line renderer so the digest reads like the attempt did. Only
// the tail is kept (the agent's final words plus a short run-up), enough to show
// where the attempt left off without replaying the whole session.
func attemptNarrative(agent string, events []streamEventRecord) string {
	render := lineRendererFor(presetAutoFormat(agent), false)
	var lines []string
	for _, ev := range events {
		if render == nil {
			if line := strings.TrimSpace(ev.Raw); line != "" {
				lines = append(lines, line)
			}
			continue
		}
		rendered, handled := render([]byte(ev.Raw))
		if !handled {
			if line := strings.TrimSpace(ev.Raw); line != "" {
				lines = append(lines, line)
			}
			continue
		}
		if rendered == "" {
			continue
		}
		for _, l := range strings.Split(strings.TrimRight(rendered, "\n"), "\n") {
			if strings.TrimSpace(l) != "" {
				lines = append(lines, l)
			}
		}
	}
	const tailLines = 12
	if len(lines) > tailLines {
		lines = lines[len(lines)-tailLines:]
	}
	return strings.Join(lines, "\n")
}

// formatPriorAttemptDigest renders the digest section appended to the worker
// prompt on a retry. Attempts read most-recent-last so the freshest lesson is
// closest to the task instructions.
func formatPriorAttemptDigest(attempts []priorAttempt) string {
	var b strings.Builder
	b.WriteString("\n")
	b.WriteString("Prior attempts on THIS task (most recent last). They ran on the runtime\n")
	b.WriteString("checkout you have now, so build on them rather than rediscovering from\n")
	b.WriteString("scratch. The lesson on each says whether the approach stood:\n\n")
	for _, a := range attempts {
		fmt.Fprintf(&b, "Attempt %d — %s\n", a.Attempt, a.Lesson)
		for _, line := range strings.Split(a.Narrative, "\n") {
			if strings.TrimSpace(line) == "" {
				continue
			}
			fmt.Fprintf(&b, "  %s\n", line)
		}
		b.WriteString("\n")
	}
	return strings.TrimRight(b.String(), "\n") + "\n"
}

// parseAttemptStream decodes one gzipped Captured attempt stream into its
// header, events, and footer. It is the digest's read path over the same
// substrate the timing lens reads (ADR 0016); unlike readAttemptTiming it keeps
// the raw events, which the narrative needs.
func parseAttemptStream(data []byte) (streamHeaderRecord, []streamEventRecord, streamFooterRecord, error) {
	var (
		header streamHeaderRecord
		footer streamFooterRecord
		events []streamEventRecord
	)
	zr, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		return header, nil, footer, err
	}
	jsonl, err := io.ReadAll(zr)
	if err != nil {
		return header, nil, footer, err
	}
	if err := zr.Close(); err != nil {
		return header, nil, footer, err
	}
	var hasHeader, hasFooter bool
	for _, line := range bytes.Split(jsonl, []byte("\n")) {
		if len(bytes.TrimSpace(line)) == 0 {
			continue
		}
		var probe struct {
			Type string `json:"type"`
		}
		if err := json.Unmarshal(line, &probe); err != nil {
			return header, nil, footer, fmt.Errorf("parse record: %w", err)
		}
		switch probe.Type {
		case "header":
			if err := json.Unmarshal(line, &header); err != nil {
				return header, nil, footer, fmt.Errorf("parse header: %w", err)
			}
			hasHeader = true
		case "footer":
			if err := json.Unmarshal(line, &footer); err != nil {
				return header, nil, footer, fmt.Errorf("parse footer: %w", err)
			}
			hasFooter = true
		case "event":
			var ev streamEventRecord
			if err := json.Unmarshal(line, &ev); err != nil {
				return header, nil, footer, fmt.Errorf("parse event: %w", err)
			}
			events = append(events, ev)
		}
	}
	if !hasHeader {
		return header, nil, footer, fmt.Errorf("missing header record")
	}
	if !hasFooter {
		return header, nil, footer, fmt.Errorf("missing footer record")
	}
	return header, events, footer, nil
}
