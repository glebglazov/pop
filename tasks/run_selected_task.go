package tasks

import (
	"fmt"
)

// runTaskDirective is the small instruction runSelectedTask hands back to the
// drain loop, mirroring verifyPhase / terminalStatus so the loop skeleton reads
// as orchestration: the branch runs one selected task and tells the loop whether
// to keep draining or return.
type runTaskDirective int

const (
	// runTaskContinue keeps draining: the attempt completed and was accumulated, a
	// post-failure Failed gate cleared the failure in process, or a quota-recovery
	// wait resumed. The loop re-Refreshes and picks the next attempt.
	runTaskContinue runTaskDirective = iota
	// runTaskReturn hands the returned (result, err) pair straight back to the
	// caller — a Drain collision, a dirty-strategy failure, the exhausted-attempts
	// exec error (after the Failed gate falls through to advice), a quota-recovery
	// wait error, or a clean quota-pause exit with the pause fields populated —
	// exactly as the inline branch returned.
	runTaskReturn
)

// runSelectedTask runs the drain loop's task-execution branch: the path taken
// once SelectTaskInSet has picked an eligible AFK task. It (re-)acquires the
// Runtime execution lock for the contiguous run of attempts that starts here
// (ADR-0067), applies the dirty-runtime strategy at most once per Implement run,
// pre-seeds the pane's Topic from the task Title (ADR-0058), builds the agent
// prompt and the per-agent invocation builder, and runs the attempts through
// executeTaskAttemptsWithAgentFallback. On an exec error it refreshes, renders,
// and runs the Failed gate choreography — a handled disposition keeps draining
// (runTaskContinue), otherwise it prints the static stop advice and returns the
// original exec error. A quota pause parks and waits for recovery (ADR-0100); a
// failed waiter registration exits cleanly with the pause fields populated on the
// result, while a recovered wait keeps draining. A completed attempt is appended
// to the result and the loop continues.
//
// It mutates the run's Drain / result / dirtyStrategyApplied state through the
// receiver so the drain loop and the deferred finalize see the latest values.
// The returned result is the run's result on the returning paths that carry it
// (a Drain collision, the exec error, the quota-recovery wait error, the clean
// quota-pause exit) and nil on the dirty-strategy failure, exactly as the inline
// branch returned; runTaskContinue carries nil.
func (r *implementRun) runSelectedTask(currentRefresh *RefreshResult, sel *Selection) (runTaskDirective, *RunTaskSetResult, error) {
	d := r.d
	opts := r.opts
	loadConfig := r.loadConfig
	cfg := r.plan.cfg
	baseAgentPresets := r.plan.baseAgentPresets
	baseAgentPreset := r.plan.baseAgentPreset
	agentOutput := r.plan.agentOutput
	strategy := r.plan.strategy
	commitOverrides := r.plan.commitOverrides
	agentQuotaRetryAfter := r.plan.agentQuotaRetryAfter
	resolved := r.resolved
	runtimePath := r.runtimePath
	statePath := r.statePath
	taskSetID := r.taskSetID
	confirmOut := r.confirmOut
	out := r.out
	maxTries := r.maxTries
	retryDelays := r.retryDelays
	timeout := r.timeout
	result := r.result

	// An eligible AFK task is about to run: (re-)acquire the Runtime execution
	// lock for the contiguous run of attempts that starts here (ADR-0067). First
	// iteration is a no-op (the opening BeginDrain still holds); after a gate park
	// this is a fresh BeginDrain, and a collision refuses cleanly without touching
	// manifest state.
	if err := r.ensureDrain(); err != nil {
		return runTaskReturn, result, err
	}

	if r.dirty && !r.dirtyStrategyApplied {
		if err := applyDirtyRuntimeStrategy(d, runtimePath, sel.TaskSetID, sel.TaskID, strategy, commitOverrides, confirmOut); err != nil {
			return runTaskReturn, nil, taskExitErr(sel, ExitOperational, "dirty-runtime strategy: %v", err)
		}
		r.dirtyStrategyApplied = true
	}

	// Pre-seed the pane's Topic from this task's Title before its first agent
	// prompt (ADR-0058); the hook guards on the existing @pop_topic, so the first
	// task in the set wins and the derive hook no-ops thereafter.
	if opts.PreSeedTopic != nil {
		opts.PreSeedTopic(sel.Task.Title)
	}

	basePrompt := BuildAgentPrompt(sel.TaskPath, runtimePath)
	buildForAgent := buildAgentInvocationFactory(loadConfig, runtimePath, baseAgentPreset, opts.AgentCmd, agentOutput, opts.AgentOutput)

	agentSpecs := resolveTaskAgentSpecs(baseAgentPresets, opts.AgentCmd, sel.Task.Effort, sel.Task.EffortExplicit, cfg)
	taskResult, execErr := executeTaskAttemptsWithAgentFallback(d, sel, runtimePath, out, confirmOut, basePrompt, agentSpecs, buildForAgent, maxTries, timeout, commitOverrides, agentQuotaRetryAfter, retryDelays)
	if execErr != nil {
		afterRefresh, refreshErr := RefreshWith(d, resolved.DefinitionPath, statePath)
		if refreshErr == nil {
			result.Refresh = afterRefresh
			if !opts.Yes {
				fmt.Fprintln(out)
				Render(out, afterRefresh)
			}
			m := afterRefresh.Manifests[taskSetID]
			if isInterrupted(execErr) {
				// SIGINT tore the attempt down mid-run (ADR-0119): the task is still
				// open (the interrupt path writes no failed/done transition), so present
				// the interrupt gate rather than the Failed gate. Continue re-acquires
				// the lock and re-runs the interrupted task, keeping the drain going;
				// Exit (or a non-promptable run) falls through to the interrupted
				// terminal preserved by the normal finalize.
				cont, gateErr := r.interruptGate(m, findTaskInManifest(m, sel.TaskID))
				if gateErr != nil {
					return runTaskReturn, result, gateErr
				}
				if cont {
					return runTaskContinue, nil, nil
				}
				return runTaskReturn, result, execErr
			}
			// Park the Runtime execution lock before the post-failure Failed gate
			// menu so it runs lock-free (ADR-0067).
			handled, gateErr := r.failedGate(m)
			if gateErr != nil {
				return runTaskReturn, result, gateErr
			}
			if handled {
				return runTaskContinue, nil, nil
			}
			printFailedStopAdvice(out, taskSetID, m)
		}
		return runTaskReturn, result, execErr
	}
	if taskResult.QuotaPaused {
		// Quota recovery wait (ADR-0100): instead of exiting with ExitQuotaPaused,
		// park the drain, register a recovery waiter, and poll until the preset's
		// cooldown elapses and a recovery turn is acquired. Both foreground and
		// unattended drains enter the wait loop.
		priority := 0
		if row := findRow(currentRefresh, taskSetID); row != nil {
			priority = row.Priority
		}
		regFailed, waitErr := ParkAndWaitForQuotaRecovery(d, &r.drain, taskSetID, taskResult.PausePreset, taskResult.PauseResetAt, runtimePath, priority, out, r.ensureDrain)
		if waitErr != nil {
			return runTaskReturn, result, waitErr
		}
		if regFailed {
			result.QuotaPaused = true
			result.PauseReason = taskResult.PauseReason
			result.PausePreset = taskResult.PausePreset
			result.PauseResetAt = taskResult.PauseResetAt
			result.Refresh = currentRefresh
			printTaskSetSummary(out, result)
			return runTaskReturn, result, nil
		}
		return runTaskContinue, nil, nil
	}

	result.Completed = append(result.Completed, taskResult)
	return runTaskContinue, nil, nil
}
