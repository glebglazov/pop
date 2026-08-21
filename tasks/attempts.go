package tasks

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/glebglazov/pop/config"
)

type attemptOutcome struct {
	output      string
	exitCode    int
	timedOut    bool
	interrupted bool
	// turnCapExhausted is set when the agent's own stream and exit status say it
	// stopped because it reached its Turn cap (ADR-0190). Only an adapter that
	// declares the ending recognisable can set it, and only an implementation
	// attempt is ever capped, so a Verifier's outcome carries it never.
	turnCapExhausted bool
	runErr           error
	stream           *streamRecorder
}

// turnCapExhaustedReason phrases why an attempt stopped, naming the bound when
// pop is the one that set it. A hand-written cap leaves pop no number to name,
// so the sentence names the cap without one rather than inventing it.
func turnCapExhaustedReason(turnCap int) string {
	if turnCap > 0 {
		return fmt.Sprintf("stopped at its %d-turn cap", turnCap)
	}
	return "stopped at its turn cap"
}

// buildAgentInvocationFactory returns the per-agentSpec invocation builder
// shared by both drain entry points (RunTaskWith and runSelectedTask): the base
// preset reuses the already-resolved agentOutput, while any other preset in the
// fallback chain re-resolves its own output mode. Every command it builds is an
// implementation attempt, so the repository's turnCap rides along — 0 when the
// repository declares none, and ignored by a preset that cannot be told to cap
// turns (ADR-0190).
func buildAgentInvocationFactory(loadConfig func(string) (*config.Config, error), runtimePath, baseAgentPreset, agentCmd string, agentOutput, optAgentOutput AgentOutputMode, turnCap int) func(agentSpec string) (func(string) (*AgentInvocation, error), error) {
	return func(agentSpec string) (func(string) (*AgentInvocation, error), error) {
		attemptOutput := agentOutput
		if agentSpec != baseAgentPreset {
			var err error
			attemptOutput, err = resolveAgentOutputMode(loadConfig, agentSpec, optAgentOutput)
			if err != nil {
				return nil, err
			}
		}
		return func(prompt string) (*AgentInvocation, error) {
			return ResolveImplementAgentInvocation(agentSpec, agentCmd, prompt, runtimePath, attemptOutput, turnCap)
		}, nil
	}
}

func taskExitErr(sel *Selection, code int, format string, args ...any) *ExitError {
	return exitErr(code, "task %s/%s: %s", sel.TaskSetID, sel.TaskID, fmt.Sprintf(format, args...))
}

// taskAttemptLedger is what one task's attempts add up to across the whole Agent
// fallback list: how many tries have been started, and where each was captured.
// It belongs to the walk rather than to a preset because both numbers are facts
// about the task — the digest handed to a later agent reads "Attempt 4" without
// caring which CLI ran attempts 1 to 3, and the breakdown printed when the task
// reaches a terminal state covers every agent that had a turn (ADR-0231). The
// per-preset Task retry cap is counted separately, inside the retry loop.
type taskAttemptLedger struct {
	attempts int
	streams  []string
}

// spendTry claims the next task-wide attempt ordinal. It is called once per try,
// not once per invocation: an Effort model skip restarts the try on the tier's
// next entry, and both runs are the same attempt of the task (ADR-0168).
func (l *taskAttemptLedger) spendTry() int {
	l.attempts++
	return l.attempts
}

func (l *taskAttemptLedger) record(path string) {
	if path != "" {
		l.streams = append(l.streams, path)
	}
}

// executeTaskAttempts runs the retry loop for one task on one preset. The prompt
// is rebuilt per attempt (via the walk's invocation builder over basePrompt) so a
// retry can carry this task's own prior-attempt digest forward alongside set-wide
// remediation history and sibling briefs; attempt 1 runs those feeds only when
// they have content (ADR 0040/ADR 0154). Inside each attempt the walk steps
// through the preset's Effort tier: a model-scoped verdict restarts the attempt
// on the next entry without spending a try (ADR-0168).
//
// It reports a spent Task retry cap as an attemptCapExhaustion rather than
// writing the task's ending itself: only the Agent fallback walk knows whether
// another agent is left to try, and a preset that has run out of tries has said
// nothing about whether the task can be done (ADR-0231). ledger carries the
// attempt ordinals and captured streams across those presets, so a task's
// attempts are numbered once for the whole walk.
func executeTaskAttempts(d *Deps, sel *Selection, runtimePath string, out, errOut io.Writer, basePrompt string, walk *effortModelWalk, maxTries int, timeout time.Duration, commitOverrides []string, retryDelays []time.Duration, ledger *taskAttemptLedger) (*RunTaskResult, *attemptCapExhaustion, error) {
	if errOut == nil {
		errOut = os.Stderr
	}
	display := outputFor(out)
	if pos, total := afkOrdinal(sel.Manifest, sel.TaskID); pos > 0 {
		display.line(ansiBold+ansiCyan, "━━ Running task %s/%s (%d/%d): %s", sel.TaskSetID, sel.TaskID, pos, total, sel.Task.Title)
	} else {
		display.line(ansiBold+ansiCyan, "━━ Running task %s/%s: %s", sel.TaskSetID, sel.TaskID, sel.Task.Title)
	}
	for attempt := 1; attempt <= maxTries; attempt++ {
		// The task's own ordinal for this try, claimed below by the first
		// invocation that actually runs. It is the preset's own attempt number
		// only while this is the first preset to get a turn.
		taskAttempt := 0
		prompt := basePrompt
		// Carry harness-built feeds forward whenever they have content so a
		// retry converges instead of repeating (ADR 0040/ADR 0089/ADR 0154):
		// set-wide remediation history (cross-task, capped self-reports),
		// briefs of sibling tasks already completed in the set (cross-task
		// orientation), then this task's own prior-attempt story. They fire
		// on attempt 1 when non-empty, which is how a resumed interrupted/
		// quota-paused task sees its own context immediately. All are always
		// harness-built, never a pointer to a raw stream (ADR 0020). The
		// remediation history channel is not fused with the prior-attempt
		// digest (ADR-0154).
		var carry strings.Builder
		if history := formatRemediationHistoryBlock(d, sel.Manifest); history != "" {
			carry.WriteString("\n" + history)
		}
		if briefs := formatSiblingCompletedBriefs(d, sel.Manifest); briefs != "" {
			carry.WriteString("\n" + briefs)
		}
		if digest := buildPriorAttemptDigest(d, sel.Manifest.Dir, sel.TaskFile); digest != "" {
			carry.WriteString("\n" + digest)
		}
		if carry.Len() > 0 {
			prompt = basePrompt + carry.String()
		}
		// One attempt, walked across the Effort tier: a model-scoped verdict
		// retires the entry it just used and restarts here on the next one, so
		// the try is spent only once the agent answers about the task rather
		// than about its model (ADR-0168).
		var (
			invocation  *AgentInvocation
			outcome     *attemptOutcome
			agentResult AgentResult
			persist     func(rec *streamRecorder, outcome, reason string, exitCode int)
			escalated   *AgentProceedVerdict
		)
		for {
			build, exhausted, err := walk.builder()
			if err != nil {
				return nil, nil, taskExitErr(sel, ExitSetup, "%v", err)
			}
			if exhausted != nil {
				escalated = exhausted
				break
			}
			invocation, err = build(prompt)
			if err != nil {
				return nil, nil, taskExitErr(sel, ExitSetup, "%v", err)
			}
			entry := invocation
			if taskAttempt == 0 {
				// A try whose Effort tier had nothing left to invoke never became
				// an attempt of the task, so it claims no ordinal; every
				// invocation this try makes past the first is the same attempt
				// walking the tier (ADR-0168).
				taskAttempt = ledger.spendTry()
			}
			persist = func(rec *streamRecorder, outcome, reason string, exitCode int) {
				ledger.record(persistAttemptStream(d, errOut, sel, rec, entry.AgentPreset(), entry.RequestedAgent, taskAttempt, outcome, reason, exitCode))
			}
			display.line(ansiDim, "   Attempt %d/%d · %s", attempt, maxTries, invocation.RequestedAgent)
			display.line(ansiDim, "── Agent output ────────────────────────────────────────")

			agentOut, attemptOut, err := runAgentAttempt(d, runtimePath, out, timeout, invocation)
			if err != nil {
				display.line(ansiRed, "✗ Agent failed to start for %s/%s", sel.TaskSetID, sel.TaskID)
				return nil, nil, taskExitErr(sel, ExitOperational, "agent execution: %v", err)
			}
			outcome = attemptOut
			if outcome.timedOut {
				display.line(ansiDim, "── Agent killed (timeout) for %s/%s ───────────────────", sel.TaskSetID, sel.TaskID)
			} else {
				display.line(ansiDim, "── Agent finished for %s/%s ───────────────────────────", sel.TaskSetID, sel.TaskID)
			}
			if outcome.interrupted || outcome.timedOut || outcome.turnCapExhausted || outcome.runErr != nil {
				break
			}
			agentResult = invocation.NormalizeOutput(agentOut)
			if agentResult.ProceedVerdict == nil || agentResult.ProceedVerdict.Scope != ProceedScopeModel {
				break
			}
			refused := stampDetectedVerdict(*agentResult.ProceedVerdict, invocation.AgentPreset(), invocation.PinnedModel())
			stop, err := walk.retire(refused)
			if err != nil {
				return nil, nil, taskExitErr(sel, ExitOperational, "%v", err)
			}
			// The run is persisted as what the verdict finally condemned: a skip
			// when another tier entry takes over, an unusable agent when the
			// escalation hands the whole preset on.
			if stop != nil {
				persist(outcome.stream, streamOutcomeAgentUnusable, refused.Reason, outcome.exitCode)
				escalated = stop
				break
			}
			ledger.record(persistSkippedAttemptStream(d, errOut, sel, outcome.stream, entry.AgentPreset(), entry.RequestedAgent, refused.Model, taskAttempt, refused.Reason, outcome.exitCode))
			// The verdict is spent here — this attempt starts over on the tier's
			// next entry, and nothing downstream should see it.
			agentResult = AgentResult{}
			display.line(ansiDim, "   %s", refused.effortModelSkipMessage("Agent", walk.nextModel()))
		}
		if escalated != nil {
			return proceedVerdictResult(sel, *escalated), nil, nil
		}
		if outcome.interrupted {
			persist(outcome.stream, streamOutcomeInterrupted, "", outcome.exitCode)
			return nil, nil, taskExitErr(sel, ExitInterrupted, "interrupted")
		}
		if outcome.timedOut {
			timeoutReason := fmt.Sprintf("timed out after %s", timeout)
			persist(outcome.stream, streamOutcomeTimedOut, timeoutReason, outcome.exitCode)
			display.line(ansiRed, "✗ Attempt %d/%d timed out after %s", attempt, maxTries, timeout)
			// A timeout almost always means execution ran too long (an oversized
			// context window), not a doomed approach. The retry restarts from the
			// compact prior-attempt "continue" digest (ADR 0040), carried forward at
			// the top of the loop, rather than the bloated transcript — so a wait
			// adds nothing and the retry fires instantly. It consumes one slot of the
			// shared max_tries budget; only the final attempt ends the preset's turn.
			if attempt < maxTries {
				display.line(ansiYellow, "↻ Retrying instantly with preserved changes...")
				continue
			}
			// The cap is spent on timeouts alone. That is real evidence the work
			// does not fit in one attempt, but it is evidence about this preset:
			// another agent may have room this one did not, so the walk is left to
			// decide whether the task has run out of agents (ADR-0231).
			return nil, &attemptCapExhaustion{
				preset:   walk.preset,
				attempts: attempt,
				fault:    faultOverrun,
				reason:   timeoutReason,
				summary:  fmt.Sprintf("timed out after %s on attempt %d", timeout, taskAttempt),
			}, nil
		}
		if outcome.turnCapExhausted {
			// The agent stopped itself at the repository's bound, so the attempt
			// answered nothing about the task and everything it committed is on
			// disk. That spends the try — unlike an Effort model skip — and the
			// digest carried forward at the top of this loop tells the next attempt
			// it was cut short, which is the whole point of recording the ending
			// separately (ADR-0190 decision 6). Like a timeout, there is nothing to
			// wait for: the retry fires instantly on the compact digest.
			reason := turnCapExhaustedReason(invocation.turnCap)
			persist(outcome.stream, streamOutcomeTurnCapExhausted, reason, outcome.exitCode)
			display.line(ansiRed, "✗ Attempt %d/%d %s", attempt, maxTries, reason)
			if attempt < maxTries {
				display.line(ansiYellow, "↻ Retrying instantly with preserved changes...")
				continue
			}
			return nil, &attemptCapExhaustion{
				preset:   walk.preset,
				attempts: attempt,
				fault:    faultOverrun,
				reason:   reason,
				summary:  fmt.Sprintf("%s on attempt %d", reason, taskAttempt),
			}, nil
		}
		if outcome.runErr != nil {
			return nil, nil, taskExitErr(sel, ExitOperational, "agent execution: %v", outcome.runErr)
		}
		if agentResult.ProceedVerdict != nil {
			v := stampDetectedVerdict(*agentResult.ProceedVerdict, invocation.AgentPreset(), invocation.PinnedModel())
			if _, ok := v.TimeHealing(); ok {
				v = resolveProceedResetAt(v, time.Now())
				persist(outcome.stream, streamOutcomeQuotaPaused, "", outcome.exitCode)
				display.line(ansiYellow, "Paused: agent quota exhausted for %s/%s", sel.TaskSetID, sel.TaskID)
				display.line(ansiYellow, "  %s", v.Reason)
			} else {
				persist(outcome.stream, streamOutcomeAgentUnusable, v.Reason, outcome.exitCode)
			}
			return proceedVerdictResult(sel, v), nil, nil
		}

		taskData, err := d.FS.ReadFile(sel.TaskPath)
		if err != nil {
			return nil, nil, taskExitErr(sel, ExitOperational, "read task markdown: %v", err)
		}

		assessment, reason := assessAttempt(agentResult.Output, outcome.exitCode, taskData)
		streamOutcome := streamOutcomeFailed
		if assessment.Complete {
			streamOutcome = streamOutcomeCompleted
		}
		persist(outcome.stream, streamOutcome, reason, outcome.exitCode)
		if assessment.Complete {
			result, err := completeSuccessfulTask(d, sel, runtimePath, assessment.Summary, commitOverrides)
			if err != nil {
				return nil, nil, taskExitErr(sel, ExitOperational, "%v", err)
			}
			printConciseSummary(out, result)
			// Every attempt the task had, including those an earlier agent in the
			// fallback list spent before handing the turn on.
			printAttemptBreakdown(d, out, ledger.streams)
			return result, nil, nil
		}

		display.line(ansiRed, "✗ Attempt %d/%d failed: %s", attempt, maxTries, reason)
		if attempt < maxTries {
			delay := attemptRetryDelay(retryDelays, attempt)
			if delay <= 0 {
				display.line(ansiYellow, "↻ Retrying with preserved changes...")
			} else if waitRetryDelay(d, out, delay) {
				return nil, nil, taskExitErr(sel, ExitInterrupted, "interrupted")
			}
			continue
		}

		// This preset is out of tries. Whether the task is out of agents is the
		// walk's to answer (ADR-0231).
		return nil, &attemptCapExhaustion{
			preset:   walk.preset,
			attempts: attempt,
			fault:    attemptFaultForExit(outcome.exitCode),
			reason:   reason,
			summary:  fmt.Sprintf("failed after %d attempts: %s", taskAttempt, reason),
		}, nil
	}
	return nil, nil, taskExitErr(sel, ExitOperational, "unexpected attempt loop exit")
}

// executeTaskAttemptsWithAgentFallback walks the two nested lists a task's
// agents form: the Agent fallback presets, and inside each preset the Effort
// tier resolveSpec resolves. agentSpecs are the base specs, before Effort
// resolution — the tier is resolved per attempt so a recorded Effort model skip
// is filtered out of every resolution after the one that recorded it (ADR-0168).
//
// Every way a preset can fail to do the work advances the walk: a preset-scoped
// proceed verdict, and equally a preset that spent its whole Task retry cap
// without finishing. The walk ends only when the list ends, so it is the only
// place that knows a task has run out of agents — and therefore the place that
// writes the task's Failed ending (ADR-0231).
func executeTaskAttemptsWithAgentFallback(d *Deps, sel *Selection, runtimePath string, out, errOut io.Writer, basePrompt string, agentSpecs []string, resolveSpec effortSpecResolver, buildForAgent func(agentSpec string) (func(prompt string) (*AgentInvocation, error), error), maxTries int, timeout time.Duration, commitOverrides []string, agentQuotaRetryAfter time.Duration, retryDelays []time.Duration, probeMemo *agentAvailabilityProbeMemo) (*RunTaskResult, error) {
	cooldowns, err := readAgentCooldowns(d)
	if err != nil {
		return nil, taskExitErr(sel, ExitOperational, "%v", err)
	}
	activeCooldowns := activeAgentCooldowns(cooldowns, time.Now())
	skips, err := loadEffortModelSkips(d, time.Now())
	if err != nil {
		return nil, taskExitErr(sel, ExitOperational, "%v", err)
	}
	specs := nonEmptyAgentSpecs(agentSpecs, DefaultAgentPreset)
	var unavailableResults []*RunTaskResult
	// The last preset to spend its Task retry cap, held in case the list runs
	// out: its ending becomes the task's, and until then it is only a reason to
	// try the next agent (ADR-0231).
	var spentCap *attemptCapExhaustion
	ledger := &taskAttemptLedger{}
	for i, agentSpec := range specs {
		preset, err := AgentPresetName(agentSpec)
		if err != nil {
			return nil, taskExitErr(sel, ExitSetup, "%v", err)
		}
		if until, cooling := activeCooldowns[preset]; cooling {
			v := NewQuotaPauseVerdict(preset, fmt.Sprintf("agent quota cooldown until %s", until.UTC().Format(time.RFC3339)), until)
			unavailableResults = append(unavailableResults, proceedVerdictResult(sel, v))
			continue
		}
		if !agentBinaryAvailable(d, preset) {
			v := NewMissingBinaryVerdict(preset, "binary not found on PATH")
			if i+1 < len(specs) && out != nil {
				outputFor(out).line(ansiDim, "   %s", v.fallThroughMessage("Agent"))
			}
			unavailableResults = append(unavailableResults, proceedVerdictResult(sel, v))
			continue
		}
		if v := probeMemo.checkProceedVerdict(d, runtimePath, preset); v != nil {
			if i+1 < len(specs) && out != nil {
				outputFor(out).line(ansiDim, "   %s", v.fallThroughMessage("Agent"))
			}
			unavailableResults = append(unavailableResults, proceedVerdictResult(sel, *v))
			continue
		}
		walk := &effortModelWalk{d: d, preset: preset, baseSpec: agentSpec, resolve: resolveSpec, skips: skips, build: buildForAgent}
		result, spent, execErr := executeTaskAttempts(d, sel, runtimePath, out, errOut, basePrompt, walk, maxTries, timeout, commitOverrides, retryDelays, ledger)
		if spent != nil {
			spentCap = spent
			if i+1 < len(specs) && out != nil {
				outputFor(out).line(ansiDim, "   %s", spent.fallThroughMessage("Agent"))
			}
			continue
		}
		if execErr != nil || result == nil || result.ProceedVerdict == nil {
			return result, execErr
		}
		v := *result.ProceedVerdict
		// Only a preset-scoped verdict reaches the preset cooldown store; a
		// model-scoped one leaves the CLI running fine (ADR-0168).
		if th, ok := v.TimeHealing(); ok && v.Scope == ProceedScopePreset {
			until := agentQuotaCooldownUntil(th.ResetAt, time.Now(), agentQuotaRetryAfter)
			if err := updateAgentCooldown(d, v.Preset, until); err != nil {
				return nil, taskExitErr(sel, ExitOperational, "%v", err)
			}
			activeCooldowns[v.Preset] = until
		}
		if v.Scope == ProceedScopePreset && i+1 < len(specs) && out != nil {
			outputFor(out).line(ansiDim, "   %s", v.fallThroughMessage("Agent"))
		}
		unavailableResults = append(unavailableResults, result)
	}
	verdict := resolveAgentFallbackVerdict(sel, unavailableResults)
	if verdict != nil && ledger.attempts == 0 {
		// Not one preset got as far as invoking an agent: every one was cooling,
		// capped, unauthenticated or absent from PATH. Nothing was attempted, so
		// nothing failed, and the caller reports the stop as the no-op it is
		// rather than as an exhausted list (ADR-0231).
		verdict.NoAgentStarted = true
	}
	// A time-healing verdict outranks a spent cap: the preset it condemns comes
	// back on its own, so parking the drain until it does and retrying the task
	// then is a better ending than declaring the task failed while an agent that
	// was never really tried is about to be usable again.
	if verdict != nil && verdict.ProceedVerdict != nil {
		if _, ok := verdict.ProceedVerdict.TimeHealing(); ok {
			return verdict, nil
		}
	}
	if spentCap != nil {
		// Every agent has had its turn and none finished, so this walk is where
		// the task's ending is written. Which ending that is follows how the last
		// agent stopped, and the drain stops either way — per-drain rather than
		// per-task, because the next task would be handed the same dead list
		// (ADR-0231).
		printAttemptBreakdown(d, out, ledger.streams)
		return nil, disposeExhaustedWalk(d, sel, out, *spentCap, ledger.attempts)
	}
	if verdict == nil {
		return nil, taskExitErr(sel, ExitSetup, "%s", humanHealingStopMessage(sel, true, nil))
	}
	return verdict, nil
}

func proceedVerdictResult(sel *Selection, v AgentProceedVerdict) *RunTaskResult {
	result := &RunTaskResult{
		Selection:      sel,
		ProceedVerdict: &v,
	}
	if v.pausesUntilReset() {
		result.QuotaPaused = true
		result.PauseReason = v.Reason
		result.PausePreset = v.Preset
		if th, ok := v.TimeHealing(); ok {
			result.PauseResetAt = th.ResetAt
		}
	}
	return result
}

func resolveAgentFallbackVerdict(sel *Selection, results []*RunTaskResult) *RunTaskResult {
	if len(results) == 0 {
		return nil
	}
	if best := earliestTimeHealingVerdict(results); best != nil {
		if _, ok := best.ProceedVerdict.TimeHealing(); ok {
			return best
		}
	}
	var presets []AgentProceedVerdict
	for _, result := range results {
		if result == nil || result.ProceedVerdict == nil {
			continue
		}
		presets = append(presets, *result.ProceedVerdict)
	}
	if len(presets) == 0 {
		for _, result := range results {
			if result != nil {
				return result
			}
		}
		return nil
	}
	out := proceedVerdictResult(sel, presets[0])
	out.UnavailablePresets = presets
	return out
}

func earliestTimeHealingVerdict(results []*RunTaskResult) *RunTaskResult {
	var best *RunTaskResult
	var bestReset time.Time
	for _, result := range results {
		if result == nil || result.ProceedVerdict == nil {
			continue
		}
		th, ok := result.ProceedVerdict.TimeHealing()
		if !ok {
			continue
		}
		if best == nil {
			best = result
			bestReset = th.ResetAt
			continue
		}
		if th.ResetAt.IsZero() {
			continue
		}
		if bestReset.IsZero() || th.ResetAt.Before(bestReset) {
			best = result
			bestReset = th.ResetAt
		}
	}
	if best != nil {
		return best
	}
	for _, result := range results {
		if result != nil {
			return result
		}
	}
	return nil
}

// agentDiagnosticMaxRunes bounds a provider's sentence to what a one-line
// progress record and a prompt digest can carry. Every wording observed is far
// shorter; the cap exists so an agent that dies mid-paragraph cannot turn one
// failure into a wall of text on three surfaces at once.
const agentDiagnosticMaxRunes = 200

// agentExitDiagnostic says why an attempt that exited non-zero stopped, in the
// provider's own words whenever it left any. Non-zero exit has only ever been
// the provider falling over — a sleeping laptop, a dropped connection, a capped
// account — and the sentence naming which one arrives last, after whatever work
// the agent had already done, so the capture's final line is the diagnostic
// (ADR-0231). A capture that said nothing keeps the exit code, because a failure
// with no reason at all is worse than a thin one.
func agentExitDiagnostic(agentOut string, exitCode int) string {
	exitReason := fmt.Sprintf("agent exited with status %d", exitCode)
	trimmed := strings.TrimRight(agentOut, " \t\r\n")
	// A capture that closes out on the contract has no provider tail: the agent
	// got its own last word in, and whatever killed it afterwards said nothing.
	// Its own TASK_FAILED text is the exception — that sentence is why it stopped.
	if closeOut, ok := closeOutLine(splitNonEmptyLines(trimmed)); ok {
		if !strings.HasPrefix(closeOut, failedSentinel) {
			return exitReason
		}
		if reported := strings.TrimSpace(strings.TrimPrefix(closeOut, failedSentinel)); reported != "" {
			return clampAgentDiagnostic(reported)
		}
		return exitReason
	}
	diagnostic := lastNonEmptyLine(trimmed)
	if diagnostic == "" {
		return exitReason
	}
	return clampAgentDiagnostic(diagnostic)
}

func clampAgentDiagnostic(diagnostic string) string {
	if runes := []rune(diagnostic); len(runes) > agentDiagnosticMaxRunes {
		return strings.TrimSpace(string(runes[:agentDiagnosticMaxRunes])) + "…"
	}
	return diagnostic
}

func assessAttempt(agentOut string, exitCode int, taskData []byte) (Assessment, string) {
	if exitCode != 0 {
		return Assessment{}, agentExitDiagnostic(agentOut, exitCode)
	}
	assessment := AssessCompletion(agentOut, taskData)
	if assessment.Complete {
		return assessment, ""
	}
	reason := assessment.FailedReason
	if reason == "" {
		reason = reasonContractUnmet
	}
	return assessment, reason
}

func completeSuccessfulTask(d *Deps, sel *Selection, runtimePath, summary string, commitOverrides []string) (*RunTaskResult, error) {
	hasChanges, err := runtimeHasChanges(d, runtimePath)
	if err != nil {
		return nil, exitErr(ExitOperational, "check runtime changes: %v", err)
	}

	result := &RunTaskResult{
		Selection:    sel,
		AgentSummary: summary,
	}

	var commit *ImplementationCommit
	if hasChanges {
		made, err := createImplementationCommit(d, runtimePath, sel, summary, commitOverrides)
		if err != nil {
			return nil, exitErr(ExitOperational, "implementation commit: %v", err)
		}
		commit = made
		if made != nil {
			result.CommitSHA = made.SHA
		}
	} else {
		result.NoOp = true
	}

	if err := finalizeTaskDone(d, sel, runtimePath, summary, commit); err != nil {
		return nil, err
	}
	return result, nil
}

func runtimeHasChanges(d *Deps, runtimePath string) (bool, error) {
	out, err := d.Git.CommandInDir(runtimePath, "status", "--porcelain")
	if err != nil {
		return false, err
	}
	return strings.TrimSpace(out) != "", nil
}

// ImplementationCommit is the commit an attempt just made, in the shape the
// manifest needs to reconstruct the set's commit range later (ADR-0207): the
// commit itself, and the parent that becomes the Set base commit when this is
// the set's first one.
type ImplementationCommit struct {
	SHA string
	// Subject is the subject line as handed to git, kept verbatim so a later
	// fixed-string search of history finds this commit again after a rebase has
	// changed its SHA.
	Subject string
	// Parent is the commit's first parent, empty when the commit is the
	// repository's own root commit and therefore has none.
	Parent string
}

// implementationSubject picks the subject this task's commit is written under:
// the Planned commit subject the manifest carries, used verbatim, or pop's
// built-in default format when the set was planned without one (ADR-0207).
// Resolving it here rather than inside the commit keeps the commit a pure
// git operation and leaves one place that knows the fallback rule.
func implementationSubject(sel *Selection) string {
	if sel.Manifest != nil {
		for _, task := range sel.Manifest.Tasks {
			if task.ID == sel.TaskID && strings.TrimSpace(task.CommitSubject) != "" {
				return task.CommitSubject
			}
		}
	}
	return CommitSubject(sel.TaskSetID, sel.TaskID)
}

// createImplementationCommit writes the task's work as one commit. It takes the
// whole Selection rather than a finished message so the Task trailer cannot be
// forgotten by a caller: subject and trailer are both derived here, and every
// task commit therefore carries the trailer (ADR-0216).
func createImplementationCommit(d *Deps, runtimePath string, sel *Selection, summary string, commitOverrides []string) (*ImplementationCommit, error) {
	if _, err := d.Git.CommandInDir(runtimePath, "add", "-A"); err != nil {
		return nil, err
	}
	staged, err := d.Git.CommandInDir(runtimePath, "diff", "--cached", "--name-only")
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(staged) == "" {
		return nil, nil
	}
	// A third `-m` puts the trailer in a paragraph of its own, which is what makes
	// git read it as a trailer rather than as the summary's last line of prose.
	subject := implementationSubject(sel)
	trailer := TaskTrailer(sel.TaskSetID, sel.TaskID)
	if _, err := d.Git.CommandInDir(runtimePath, commitGitArgs(commitOverrides, "commit", "-m", subject, "-m", summary, "-m", trailer)...); err != nil {
		return nil, err
	}
	// One call answers both questions: `--parents` prints the new commit's SHA
	// followed by its parents, so a root commit — which has none, and for which
	// `rev-parse HEAD^` would simply fail — is a line with a single field rather
	// than an error to tell apart from a real git failure.
	out, err := d.Git.CommandInDir(runtimePath, "rev-list", "--parents", "-n", "1", "HEAD")
	if err != nil {
		return nil, err
	}
	fields := strings.Fields(out)
	if len(fields) == 0 {
		return nil, nil
	}
	made := &ImplementationCommit{SHA: fields[0], Subject: subject}
	if len(fields) > 1 {
		made.Parent = fields[1]
	}
	return made, nil
}

func finalizeTaskFailed(d *Deps, sel *Selection, attemptsStarted int, summary string) error {
	// Route the open→failed write through the Task-transition chokepoint as
	// Executor; the chokepoint owns the FAILED progress record, the attempt-count
	// bookkeeping (set on →failed), and the atomic manifest write. open→failed
	// never touches the verification episode (ADR-0109 fires only on →open/→done),
	// so no project path is needed for invalidation.
	return ApplyTransitions(d, sel.Manifest, "", []TransitionOp{{
		TaskID:       sel.TaskID,
		To:           TaskFailed,
		Actor:        ActorExecutor,
		Marker:       "FAILED",
		Summary:      summary,
		AttemptCount: attemptsStarted,
	}})
}

func finalizeTaskDone(d *Deps, sel *Selection, runtimePath, summary string, commit *ImplementationCommit) error {
	// Route the open→done write through the Task-transition chokepoint as
	// Executor; the chokepoint owns the DONE progress record, clearing the
	// attempt count under its uniform rule, and the atomic manifest write. This
	// open→done flows through the same ADR-0109 invalidation rule as a manual
	// completion — a no-op mid-drain, since the set has no cached verdicts until
	// it goes fully done.
	// The commit rides along so its SHA, subject and the set's base land in the
	// same atomic manifest write as the →done status: a crash can leave a commit
	// with no record, but never a record of a task that is not done.
	return ApplyTransitions(d, sel.Manifest, runtimePath, []TransitionOp{{
		TaskID:  sel.TaskID,
		To:      TaskDone,
		Actor:   ActorExecutor,
		Marker:  "DONE",
		Summary: summary,
		Commit:  commit,
	}})
}

func printConciseSummary(w io.Writer, result *RunTaskResult) {
	out := outputFor(w)
	out.line(ansiGreen, "✓ Completed %s/%s", result.Selection.TaskSetID, result.Selection.TaskID)
	if result.NoOp {
		fmt.Fprintln(out, "  No implementation commit (verified no-op)")
	} else if result.CommitSHA != "" {
		fmt.Fprintf(out, "  Implementation commit: %s\n", result.CommitSHA[:min(12, len(result.CommitSHA))])
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// startAgentInvocation spawns one resolved agent command, taking the env-aware
// runner path when the invocation carries environment entries. Both of the
// agent's streams go to one writer, which is what interleaves kimi's stderr
// (its Bash tool echoes there) into the same capture as its stream-json.
func startAgentInvocation(ctx context.Context, runner CommandRunner, runtimePath string, agentOut io.Writer, invocation *AgentInvocation) (*ManagedProcess, error) {
	if len(invocation.Env) == 0 {
		return runner.Start(ctx, runtimePath, agentOut, agentOut, invocation.Name, invocation.Args...)
	}
	envRunner, ok := runner.(EnvCommandRunner)
	if !ok {
		return nil, fmt.Errorf("agent %s needs invocation environment (%s) that command runner %T cannot carry", invocation.Name, strings.Join(invocation.Env, " "), runner)
	}
	return envRunner.StartWithEnv(ctx, runtimePath, invocation.Env, agentOut, agentOut, invocation.Name, invocation.Args...)
}

func runAgentAttempt(d *Deps, runtimePath string, liveOut io.Writer, timeout time.Duration, invocation *AgentInvocation) (string, *attemptOutcome, error) {
	// The prompt leaves argv here, for the length of this attempt only: argv has
	// an execve ceiling a generated prompt can exceed, a file has none.
	if err := invocation.spillPrompt(); err != nil {
		return "", nil, err
	}
	defer invocation.cleanupPrompt()

	var capture bytes.Buffer
	var agentOut io.Writer = &capture
	var recorder *streamRecorder
	var liveWriter *liveRenderWriter
	if invocation.OutputFormat == AgentOutputPlain {
		// Plain-output and custom-command attempts have no structured events
		// and are not recorded (ADR 0016).
		agentOut = io.MultiWriter(liveOut, &capture)
	} else {
		recorder = newStreamRecorder(&capture, time.Now)
		agentOut = recorder
		if render := lineRendererFor(invocation.OutputFormat, outputFor(liveOut).color); render != nil {
			liveWriter = newLiveRenderWriter(liveOut, recorder, render, time.Now)
			agentOut = liveWriter
		}
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Install the handler before the agent starts so a signal arriving while
	// the agent is already running can never hit the default (fatal) action.
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	defer signal.Stop(sigCh)

	proc, err := startAgentInvocation(ctx, d.Runner, runtimePath, agentOut, invocation)
	if err != nil {
		return "", nil, err
	}

	outcome := &attemptOutcome{}

	done := make(chan waitResult, 1)
	go func() {
		code, waitErr := proc.Wait()
		done <- waitResult{exitCode: code, err: waitErr}
	}()

	timeoutCh := time.After(timeout)

	waitForDone := func() {
		r := <-done
		outcome.exitCode = r.exitCode
		if r.err != nil && r.exitCode == 0 {
			outcome.runErr = r.err
		}
	}

	select {
	case sig := <-sigCh:
		_ = sig
		outcome.interrupted = true
		terminateProcessGroup(proc, syscall.SIGTERM)
		grace := time.NewTimer(signalGracePeriod)
		select {
		case <-done:
			grace.Stop()
		case <-grace.C:
			terminateProcessGroup(proc, syscall.SIGKILL)
			<-done
		}
	case <-timeoutCh:
		outcome.timedOut = true
		terminateProcessGroup(proc, syscall.SIGKILL)
		waitForDone()
	case r := <-done:
		outcome.exitCode = r.exitCode
		if r.err != nil && r.exitCode == 0 {
			outcome.runErr = r.err
		}
	}

	if liveWriter != nil {
		liveWriter.Flush()
	}
	if recorder != nil {
		recorder.finish()
		outcome.stream = recorder
		// Read the ending out of what the agent said and how it exited, through the
		// adapter's declared exhaustion capability — never off a turn count
		// (ADR-0190). A killed attempt is pop's own doing and is already named by
		// its own outcome, so it is not re-read as the agent stopping itself.
		if !outcome.interrupted && !outcome.timedOut {
			outcome.turnCapExhausted = attemptExhaustedTurnCap(invocation.AgentPreset(), recorder.events, outcome.exitCode)
		}
	}

	raw := capture.String()
	// Formats rendered live already streamed to liveOut; only the silently
	// captured formats still need the post-hoc dump.
	if invocation.OutputFormat != AgentOutputPlain && liveWriter == nil {
		if normalized := invocation.NormalizeOutput(raw); normalized.ProceedVerdict == nil {
			invocation.RenderOutput(liveOut, raw)
		}
	}
	return raw, outcome, nil
}
