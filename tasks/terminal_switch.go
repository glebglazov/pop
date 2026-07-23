package tasks

import (
	"fmt"
)

// terminalDirective is the small instruction terminalStatus hands back to the
// drain loop, mirroring verifyPhase's directive so the loop skeleton reads as
// orchestration.
type terminalDirective int

const (
	// terminalContinue keeps draining: a gate handler cleared the terminal in
	// process (a HITL gate completed/deferred the blocking task, a Failed gate
	// re-ran or finished the failed task), so the loop re-Refreshes and picks the
	// next attempt.
	terminalContinue terminalDirective = iota
	// terminalReturn hands the returned (result, err) pair straight back to the
	// caller. The result is the run's result on the done/deferred/awaiting-approval
	// exits and nil on the blocked/failed/default exits, exactly as the inline
	// switch returned.
	terminalReturn
)

// terminalStatus dispatches the drain loop's terminal-status handling once the
// set has exhausted its runnable AFK work and the pre-approval Verifier phase has
// fallen through: the set stands at a terminal status (DONE / DEFERRED / BLOCKED
// / AWAITING-APPROVAL / FAILED). It records the disposition on the run's result,
// clears auto-drain and records the AUTO-DRAIN-CLEARED progress note on a
// drained DONE / AWAITING-APPROVAL, runs the HITL / Failed gate choreography, and
// returns a directive: terminalContinue when a gate was handled in process and
// the loop should keep draining, or terminalReturn with the exact (result, err)
// the loop should return. selErr is the SelectTaskInSet error the loop already
// holds; it is returned verbatim for a status outside the known terminal set.
func (r *implementRun) terminalStatus(currentRefresh *RefreshResult, row *Row, selErr error) (terminalDirective, *RunTaskSetResult, error) {
	d := r.d
	opts := r.opts
	out := r.out
	resolved := r.resolved
	taskSetID := r.taskSetID
	result := r.result

	switch row.Status {
	case StatusDone:
		result.TaskSetDone = true
		if row.AutoDrain {
			cleared, err := SetTaskSetAutoDrain(d, resolved.DefinitionPath, taskSetID, false)
			if err != nil {
				return terminalReturn, result, exitErr(ExitOperational, "clear auto-drain for task set %s: %v", taskSetID, err)
			}
			if cleared {
				fmt.Fprintf(out, "Auto-drain cleared for task set %s: all AFK tasks drained; status DONE.\n", taskSetID)
				if err := AppendSetProgress(d, currentRefresh.Manifests[taskSetID].Dir, "AUTO-DRAIN-CLEARED", "Auto-drain cleared: all AFK tasks drained; status DONE."); err != nil {
					return terminalReturn, result, exitErr(ExitOperational, "record auto-drain clear for task set %s: %v", taskSetID, err)
				}
			}
		}
		finishRunTaskSet(out, opts.Yes, result)
		return terminalReturn, result, nil
	case StatusDeferred:
		result.TaskSetDeferred = true
		result.SkippedTasks = SkippedTaskIDs(currentRefresh.Manifests[taskSetID])
		finishRunTaskSet(out, opts.Yes, result)
		return terminalReturn, result, nil
	case StatusBlocked, StatusAwaitingApproval:
		result.BlockedReason = row.BlockedReason
		if row.Status == StatusAwaitingApproval {
			result.TaskSetAwaitingApproval = true
			if row.AutoDrain {
				cleared, err := SetTaskSetAutoDrain(d, resolved.DefinitionPath, taskSetID, false)
				if err != nil {
					return terminalReturn, result, exitErr(ExitOperational, "clear auto-drain for task set %s: %v", taskSetID, err)
				}
				if cleared {
					fmt.Fprintf(out, "Auto-drain cleared for task set %s: all AFK tasks drained; status AWAITING-APPROVAL.\n", taskSetID)
					if err := AppendSetProgress(d, currentRefresh.Manifests[taskSetID].Dir, "AUTO-DRAIN-CLEARED", "Auto-drain cleared: all AFK tasks drained; status AWAITING-APPROVAL."); err != nil {
						return terminalReturn, result, exitErr(ExitOperational, "record auto-drain clear for task set %s: %v", taskSetID, err)
					}
				}
			}
		}
		if !opts.Yes {
			fmt.Fprintln(out)
			Render(out, currentRefresh)
		} else {
			printTaskSetSummary(out, result)
		}
		if hitl := BlockingHITLTask(currentRefresh.Manifests[taskSetID]); hitl != nil {
			handled, err := r.hitlGate(currentRefresh.Manifests[taskSetID], hitl)
			if err != nil {
				return terminalReturn, nil, err
			}
			if handled {
				return terminalContinue, nil, nil
			}
			if result.TaskSetAwaitingApproval {
				printTerminalHITLAdvice(d, out, taskSetID, currentRefresh.Manifests[taskSetID].Dir, hitl)
			} else {
				printHITLGateAdvice(d, out, taskSetID, currentRefresh.Manifests[taskSetID].Dir, hitl)
			}
		}
		if result.BlockedReason != "" {
			if result.TaskSetAwaitingApproval {
				return terminalReturn, result, exitErr(ExitNoRunnable, "Task set %q agents done — awaiting approval: %s", taskSetID, result.BlockedReason)
			}
			return terminalReturn, nil, exitErr(ExitNoRunnable, "Task set %q blocked: %s", taskSetID, result.BlockedReason)
		}
		return terminalReturn, nil, exitErr(ExitNoRunnable, "Task set %q has no eligible AFK task", taskSetID)
	case StatusFailed:
		if !opts.Yes {
			fmt.Fprintln(out)
			Render(out, currentRefresh)
		}
		m := currentRefresh.Manifests[taskSetID]
		handled, err := r.failedGate(m)
		if err != nil {
			return terminalReturn, nil, err
		}
		if handled {
			return terminalContinue, nil, nil
		}
		printFailedStopAdvice(out, taskSetID, m)
		return terminalReturn, nil, exitErr(ExitOperational, "Task set %q has failed tasks", taskSetID)
	default:
		return terminalReturn, nil, selErr
	}
}

// hitlGate runs the HITL gate choreography for the blocking task: park the
// Runtime execution lock (registering a checkout gate hold) only when the menu
// will actually prompt, run the interactive HITL menu over the run's gateEnv,
// then release the hold. It returns the handler's (handled, err) verbatim so the
// caller keeps draining on a handled disposition or prints advice and exits
// otherwise (ADR-0067/0100).
func (r *implementRun) hitlGate(m *Manifest, hitl *Task) (bool, error) {
	r.parkAtGate(m, hitl, false)
	rv := &reverifyGateContext{
		cfg:         r.plan.cfg,
		agents:      r.opts.VerifyAgents,
		effort:      r.opts.VerifyEffort,
		timeout:     r.timeout,
		runVerifier: r.opts.verifyRunner,
	}
	handled, err := handleInteractiveHITLGate(r.newGateEnv(), m, hitl, rv)
	r.releaseGateHold()
	return handled, err
}

// failedGate runs the Failed gate choreography: seed the shared prompt reader,
// park the Runtime execution lock (registering a checkout gate hold) only when
// the menu will prompt, run the interactive Failed menu over the run's gateEnv,
// then release the hold. It returns the handler's (handled, err) verbatim. Shared
// by the terminal-status FAILED case and the post-attempt failure path in the
// drain loop, which run the identical park → menu → release trio.
//
// The hold is claim-bearing (ADR-0135) iff the working tree is dirty at park
// time (uncommitted work another set would clobber). Dirtiness is snapshotted
// here — via the injected Git/FS deps, untracked files included — and recorded on
// the row; cleaning the tree mid-gate does not release the claim until the gate
// session ends. A read error is treated as not-dirty (a non-claiming hold), never
// blocking the gate on a git failure.
func (r *implementRun) failedGate(m *Manifest) (bool, error) {
	r.sharedPromptReader = ensurePromptReader(r.sharedPromptReader, r.opts.ConfirmIn, r.opts.Yes)
	dirty, _ := runtimeIsDirty(r.d, r.runtimePath)
	r.parkAtGate(m, FailedTask(m), dirty)
	handled, err := handleInteractiveFailedGate(r.newGateEnv(), m, FailedTask(m))
	r.releaseGateHold()
	return handled, err
}
