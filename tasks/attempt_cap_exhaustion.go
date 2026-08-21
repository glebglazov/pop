package tasks

import (
	"errors"
	"fmt"
	"io"
	"strings"
)

// attemptFault says whose failure a spent Task retry cap was. It is the one
// thing the ending of an exhausted Agent fallback walk turns on: a provider that
// fell over says nothing about whether the work can be done, while an agent that
// ran cleanly and left the contract unmet says exactly that (ADR-0231).
type attemptFault int

const (
	// faultProvider is a last attempt that exited non-zero. Every non-zero exit
	// ever captured on this machine — 49 of them across two months — was the
	// provider falling over: a connection closed mid-response, a laptop asleep,
	// an overloaded server, a capped account. None was the task's fault, and all
	// of them are conditions a later drain finds gone, so the task stays Open.
	faultProvider attemptFault = iota
	// faultContract is an agent that ran to a clean exit and still did not
	// satisfy the task's contract — unchecked acceptance criteria, a missing
	// summary, a blocker it reported itself. This is the only side of the exit
	// status the corpus has task faults on, and it is the ending a human is for.
	faultContract
	// faultOverrun is an attempt that ran out of room rather than out of ability:
	// a Task attempt timeout, or an agent that stopped itself at its Turn cap.
	// Several agents' worth of that is evidence about the task's size, which no
	// later drain fixes — what it needs is the task split.
	faultOverrun
)

// leavesTaskOpen reports whether an exhausted walk that ended on this fault
// leaves the task alone. Only a provider collapse does: nothing about the work
// failed, so charging the task for it would take a Failed disposition to a human
// who can do nothing about it and stop **Work supervision** from ever retrying.
func (f attemptFault) leavesTaskOpen() bool { return f == faultProvider }

// attemptFaultForExit reads a finished attempt's exit status the one way the
// corpus supports: non-zero is the provider, zero is the work (ADR-0231).
func attemptFaultForExit(exitCode int) attemptFault {
	if exitCode != 0 {
		return faultProvider
	}
	return faultContract
}

// attemptCapExhaustion is a preset that spent its whole Task retry cap without
// finishing. It carries the ending the retry loop would once have written on the
// spot, because whether this is the task's ending at all turns on something only
// the Agent fallback walk knows: whether the list has another agent left
// (ADR-0231).
type attemptCapExhaustion struct {
	preset string
	// attempts is how many tries this preset itself spent, which is the cap
	// unless the cap was reached by a shorter route.
	attempts int
	// fault is whose failure the last attempt was, and so which ending the task
	// gets if this preset turns out to be the last one.
	fault attemptFault
	// reason is what the last attempt ended on, in the provider's own words
	// wherever it left any: the sentence the fall-through line shows the
	// operator and the prior-attempt digest carries to the next agent.
	reason string
	// summary is the progress line this ending writes if no agent is left,
	// already phrased against the task's own attempt count rather than this
	// preset's share of it.
	summary string
}

func (e attemptCapExhaustion) fallThroughMessage(role string) string {
	return retryCapFallThroughMessage(role, e.preset, e.attempts, e.reason)
}

// retryCapFallThroughMessage is the dim line printed when a preset spent its
// whole Task retry cap without finishing and the turn passes to the next agent.
// It reads like a proceed verdict's fall-through on purpose: to the operator the
// two are one event — this agent is not going to do the work and the next one is
// up — and the only thing worth saying differently is that a spent cap, rather
// than a refusal, is what ended the turn. role is the caller's word for what it
// is walking past, so every Work group reports the fall-through in its own voice
// (ADR-0231).
func retryCapFallThroughMessage(role, preset string, attempts int, reason string) string {
	spent := fmt.Sprintf("spent its %d attempts", attempts)
	if attempts == 1 {
		spent = "spent its only attempt"
	}
	msg := fmt.Sprintf("%s %s %s without finishing", role, preset, spent)
	if reason = strings.TrimSpace(reason); reason != "" {
		msg += fmt.Sprintf(" (last: %s)", clampAgentDiagnostic(reason))
	}
	return msg + "; trying next"
}

// exhaustedWalkError is the ExitError an exhausted Agent fallback walk stops on,
// carrying the ending that produced it. The drain-loop branch above the walk
// reads the fault rather than re-deriving it from the message: a walk exhausted
// by provider collapses left the task Open, so there is no failure for the
// Failed gate to dispose of (ADR-0231).
type exhaustedWalkError struct {
	fault attemptFault
	// preset is the agent that spent the last retry cap — the one a human
	// reading the journal looks at first, and the reason the Drain row records an
	// agent beside the exhausted-walk ending.
	preset string
	err    *ExitError
}

func (e *exhaustedWalkError) Error() string { return e.err.Error() }

func (e *exhaustedWalkError) Unwrap() error { return e.err }

// exhaustedWalkFault reports the ending an exhausted walk stopped on, and false
// for every other error.
func exhaustedWalkFault(err error) (attemptFault, bool) {
	var stop *exhaustedWalkError
	if errors.As(err, &stop) {
		return stop.fault, true
	}
	return 0, false
}

// exhaustedWalkPreset reports the agent that spent the last retry cap of an
// exhausted walk, and false for every other error. It is what lets the Drain
// row — and so the journal — name an agent for a stop whose exit reason is an
// ordinary clean finish.
func exhaustedWalkPreset(err error) (string, bool) {
	var stop *exhaustedWalkError
	if errors.As(err, &stop) {
		return stop.preset, true
	}
	return "", false
}

// disposeExhaustedWalk writes the ending of a task whose Agent fallback list ran
// out and returns the error the drain stops on. attempts is the task's own
// attempt count across every preset that had a turn.
//
// The three endings differ in what they leave behind, not in how loudly they say
// it: a provider collapse leaves the task Open for the next drain, a clean run
// that missed the contract leaves it Failed for a human, and a task that overran
// every agent leaves it Failed with the one piece of advice retrying cannot
// deliver — split it (ADR-0231).
func disposeExhaustedWalk(d *Deps, sel *Selection, out io.Writer, spent attemptCapExhaustion, attempts int) error {
	display := outputFor(out)
	display.line(ansiRed, "✗ Out of agents for %s/%s after %d attempts", sel.TaskSetID, sel.TaskID, attempts)
	if reason := strings.TrimSpace(spent.reason); reason != "" {
		display.line(ansiRed, "   Last: %s", clampAgentDiagnostic(reason))
	}
	if spent.fault.leavesTaskOpen() {
		display.line(ansiDim, "   Every agent died on the provider's side, which is never the task's fault: %s/%s stays open and a later drain retries it.", sel.TaskSetID, sel.TaskID)
		return &exhaustedWalkError{
			fault:  spent.fault,
			preset: spent.preset,
			err:   taskExitErr(sel, ExitOperational, "every agent failed on the provider's side, task left open (last: %s)", clampAgentDiagnostic(spent.reason)),
		}
	}
	if spent.fault == faultOverrun {
		display.line(ansiYellow, "   Every agent ran out of room rather than out of ability: split the task rather than re-running it.")
	}
	if err := finalizeTaskFailed(d, sel, attempts, spent.summary); err != nil {
		return taskExitErr(sel, ExitOperational, "%v", err)
	}
	return &exhaustedWalkError{
		fault:  spent.fault,
		preset: spent.preset,
		err:    taskExitErr(sel, ExitOperational, "%s", spent.summary),
	}
}

// humanHealingStopMessage is what a walk that never got past its presets' own
// refusals reports. When not one of them ran a Task attempt the walk is a no-op
// rather than an exhaustion — nothing was attempted, so nothing failed and the
// task is exactly as it was — and saying so is the difference between an
// operator reading "fix your logins" and reading "this task failed" (ADR-0231).
func humanHealingStopMessage(sel *Selection, noAgentStarted bool, presets []AgentProceedVerdict) string {
	if !noAgentStarted {
		return formatHumanHealingExhaustionMessage(presets)
	}
	var b strings.Builder
	fmt.Fprintf(&b, "no agent could be started, so %s/%s was not attempted and is unchanged:", sel.TaskSetID, sel.TaskID)
	if len(presets) == 0 {
		return strings.TrimSuffix(b.String(), ":")
	}
	for _, v := range presets {
		fmt.Fprintf(&b, "\n  %s: %q", v.Preset, v.Reason)
	}
	return b.String()
}
