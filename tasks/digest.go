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
// angle" is exactly the wrong instruction (ADR 0023). The reassess lesson is
// for a crash or an empty session, where there is no approach to stand on.
const (
	lessonContinue = "continue — your approach stood, finish and close out the sentinel"
	lessonReassess = "reassess"
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
// lesson the next attempt should carry (ADR 0023). A timeout is a contract
// failure on a (presumed) sound approach, so it continues; a non-zero exit is a
// crash, so it reassesses; the harness-generated contract verdicts continue;
// anything else with a clean exit is the agent's own TASK_FAILED text, which
// pivots and carries that reason forward.
func attemptLesson(outcome, reason string, exitCode int) string {
	r := strings.TrimSpace(reason)
	switch {
	case outcome == streamOutcomeTimedOut:
		return lessonContinue
	case exitCode != 0:
		return lessonReassess
	case isContractReason(r):
		return lessonContinue
	case r == "" || r == "empty agent output":
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
	case "missing TASK_COMPLETE sentinel",
		"missing or empty summary block",
		"acceptance criteria not all checked",
		"agent output did not satisfy completion contract":
		return true
	}
	return false
}

// buildPriorAttemptDigest derives the prompt section that carries this task's
// own prior-attempt story into a retry (ADR 0023). It reads the task's Captured
// attempt stream files, scopes them to attempts since the latest Open-task
// reset (a human reopens precisely because the prior line of attack was
// abandoned), and renders a failure-type lesson plus a short approach narrative
// per attempt. Returns "" when there is nothing to carry — the caller injects
// it only on attempt > 1, and the agent never sees a raw stream file (ADR 0020).
func buildPriorAttemptDigest(d *Deps, taskSetDir, taskFile string) string {
	dir := taskStreamDir(taskSetDir, taskFile)
	entries, err := d.FS.ReadDir(dir)
	if err != nil {
		return ""
	}
	cut := latestResetTime(d, taskSetDir, taskFile)

	var attempts []priorAttempt
	for _, e := range entries {
		if !attemptStreamNamePattern.MatchString(e.Name()) {
			continue
		}
		data, err := d.FS.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			continue
		}
		header, events, footer, err := parseAttemptStream(data)
		if err != nil {
			continue
		}
		// Drop attempts from the abandoned line of attack: a human reopen marks
		// everything up to its RESET timestamp stale.
		if !cut.IsZero() && !header.StartTime.After(cut) {
			continue
		}
		// Only failures teach a retry; completed/interrupted/quota-paused
		// attempts are not lessons about the approach.
		if footer.Outcome != streamOutcomeFailed && footer.Outcome != streamOutcomeTimedOut {
			continue
		}
		attempts = append(attempts, priorAttempt{
			Attempt:   header.Attempt,
			Lesson:    attemptLesson(footer.Outcome, footer.Reason, footer.ExitCode),
			Narrative: attemptNarrative(header.Agent, events),
			sortKey:   header.StartTime,
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
// scoping (ADR 0023).
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
