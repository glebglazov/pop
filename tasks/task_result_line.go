package tasks

import (
	"fmt"
	"io"
)

// taskEnding is how one task's turn in a Drain ended. It exists so the drain's
// per-task narration has a single vocabulary: every way a task can stop is one
// of these five, and each is worth exactly one Task result line.
type taskEnding int

const (
	// taskEndingDone is a completed task, whose Task result line carries the
	// implementation-commit detail beneath it.
	taskEndingDone taskEnding = iota
	// taskEndingFailed is an execution error that survived every retry and every
	// agent that could have absorbed it.
	taskEndingFailed
	// taskEndingOutOfAgents is an Agent fallback list that ran out with the task
	// left Open — nothing about the work failed, so a later drain retries it.
	taskEndingOutOfAgents
	// taskEndingQuotaPaused is a preset out of quota, named on the line because
	// which agent to wait for is the only thing the operator can act on.
	taskEndingQuotaPaused
	// taskEndingInterrupted is SIGINT tearing an attempt down mid-run.
	taskEndingInterrupted
)

// renderTaskResultLine prints the Task result line for one per-task ending: a
// glyph, the `<set>/<task>` reference, and the outcome word, the whole line
// colored by how the task ended. preset names the quota-exhausted agent and is
// read by no other ending. The line always prints; color follows the drain
// output layer's TTY and NO_COLOR handling.
func renderTaskResultLine(w io.Writer, setID, taskID string, ending taskEnding, preset string) {
	glyph, style, outcome := "✓", ansiGreen, "done"
	switch ending {
	case taskEndingFailed:
		glyph, style, outcome = "✗", ansiRed, "failed"
	case taskEndingOutOfAgents:
		glyph, style, outcome = "✗", ansiRed, "out of agents (left open)"
	case taskEndingQuotaPaused:
		glyph, style, outcome = "◌", ansiYellow, fmt.Sprintf("quota-paused (%s)", preset)
	case taskEndingInterrupted:
		glyph, style, outcome = "◌", ansiYellow, "interrupted"
	}
	outputFor(w).line(style, "%s %s/%s %s", glyph, setID, taskID, outcome)
}

// renderTaskDone prints the green Task result line for a completed task, with
// the commit it produced named beneath it: the line says the task is over, the
// detail says what it left in the Runtime.
func renderTaskDone(w io.Writer, result *RunTaskResult) {
	out := outputFor(w)
	renderTaskResultLine(out, result.Selection.TaskSetID, result.Selection.TaskID, taskEndingDone, "")
	printCommitDetail(out, result)
}

// taskEndingForExecError reads which ending an attempt-walk error was. An
// interrupt left the task Open with no transition written, and so did a walk
// exhausted by provider collapses alone (ADR-0231); everything else is the
// task's own failure.
func taskEndingForExecError(err error) taskEnding {
	if isInterrupted(err) {
		return taskEndingInterrupted
	}
	if fault, exhausted := exhaustedWalkFault(err); exhausted && fault.leavesTaskOpen() {
		return taskEndingOutOfAgents
	}
	return taskEndingFailed
}
