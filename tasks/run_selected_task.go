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
	// exec error (after the Failed gate falls through to advice, or straight away
	// when the walk left the task Open), a quota-recovery wait error, or a clean
	// quota-pause exit with the pause fields populated — exactly as the inline
	// branch returned.
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
// original exec error. An exec error from a walk exhausted by provider collapses
// skips that choreography: the task is still Open, so there is no failure to
// dispose of and the drain simply stops (ADR-0231). A quota pause parks and waits for recovery (ADR-0100); a
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
	buildForAgent := buildAgentInvocationFactory(loadConfig, runtimePath, baseAgentPreset, opts.AgentCmd, agentOutput, opts.AgentOutput, r.turnCap)

	resolveSpec := newEffortSpecResolver(opts.AgentCmd, sel.Task.Effort, sel.Task.EffortExplicit, cfg)
	taskResult, execErr := executeTaskAttemptsWithAgentFallback(d, sel, runtimePath, out, confirmOut, basePrompt, baseAgentPresets, resolveSpec, buildForAgent, maxTries, timeout, commitOverrides, agentQuotaRetryAfter, retryDelays, r.agentProbeMemo)
	if execErr != nil {
		// The chokepoint sees every ending, so the Task result line is printed here
		// rather than in the branches below: the gates that follow can keep the
		// drain going, but this task's turn is over either way.
		renderTaskResultLine(out, sel.TaskSetID, sel.TaskID, taskEndingForExecError(execErr), "")
		afterRefresh, refreshErr := RefreshWith(d, resolved.DefinitionPath, statePath)
		if refreshErr == nil {
			result.Refresh = afterRefresh
			if !opts.Yes {
				fmt.Fprintln(out)
				Render(out, afterRefresh)
			}
			m := afterRefresh.Manifests[taskSetID]
			if isInterrupted(execErr) {
				// SIGINT tore the attempt down mid-run (ADR-0163): the task is still
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
			if fault, exhausted := exhaustedWalkFault(execErr); exhausted && fault.leavesTaskOpen() {
				// The agent list ran out on provider collapses alone, so the walk
				// left the task Open: there is no failure for the Failed gate to
				// dispose of, and the drain stops for Work supervision to pick the
				// task up once the machine is awake and the network is back
				// (ADR-0231).
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
	if taskResult.ProceedVerdict != nil {
		u := taskResult.ProceedVerdict
		th, ok := u.TimeHealing()
		if !ok {
			// Every configured preset is human-healing unavailable: exit setup with
			// each preset's provider diagnostic; never enter recovery wait (ADR-0153).
			presets := taskResult.UnavailablePresets
			if len(presets) == 0 {
				presets = []AgentProceedVerdict{*u}
			}
			result.ProceedVerdict = &presets[0]
			// Carry the no-op fact up to the set-level result: the drain's own
			// finalize reads it there, and a walk that started no agent records a
			// Drain ending saying so rather than looking like a clean finish
			// (ADR-0231).
			result.NoAgentStarted = taskResult.NoAgentStarted
			// Not one preset could run, so the walk left the task exactly as it was:
			// the same ending as an exhausted agent list, told the same way.
			renderTaskResultLine(out, sel.TaskSetID, sel.TaskID, taskEndingOutOfAgents, "")
			return runTaskReturn, result, taskExitErr(sel, ExitSetup, "%s", humanHealingStopMessage(sel, taskResult.NoAgentStarted, presets))
		}
		// Quota recovery wait (ADR-0100): instead of exiting with ExitQuotaPaused,
		// park the drain, register a recovery waiter, and poll until the preset's
		// cooldown elapses and a recovery turn is acquired. Both foreground and
		// unattended drains enter the wait loop.
		// Said before the recovery wait, which can outlast the operator's patience:
		// the line is what tells them which agent the drain is waiting on.
		renderTaskResultLine(out, sel.TaskSetID, sel.TaskID, taskEndingQuotaPaused, u.Preset)
		priority := 0
		if row := findRow(currentRefresh, taskSetID); row != nil {
			priority = row.Priority
		}
		regFailed, waitErr := ParkAndWaitForQuotaRecovery(d, &r.drain, taskSetID, u.Preset, th, runtimePath, priority, out, r.ensureDrain)
		if waitErr != nil {
			return runTaskReturn, result, waitErr
		}
		if regFailed {
			result.ProceedVerdict = u
			result.QuotaPaused = true
			result.PauseReason = u.Reason
			result.PausePreset = u.Preset
			result.PauseResetAt = th.ResetAt
			result.Refresh = currentRefresh
			printTaskSetSummary(out, result)
			return runTaskReturn, result, nil
		}
		return runTaskContinue, nil, nil
	}

	renderTaskDone(out, taskResult)
	result.Completed = append(result.Completed, taskResult)
	return runTaskContinue, nil, nil
}
