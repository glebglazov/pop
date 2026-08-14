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

// executeTaskAttempts runs the retry loop for one task. The prompt is rebuilt
// per attempt (via the walk's invocation builder over basePrompt) so a retry can
// carry this task's own prior-attempt digest forward alongside set-wide
// remediation history and sibling briefs; attempt 1 runs those feeds only when
// they have content (ADR 0040/ADR 0154). Inside each attempt the walk steps
// through the preset's Effort tier: a model-scoped verdict restarts the attempt
// on the next entry without spending a try (ADR-0168).
func executeTaskAttempts(d *Deps, sel *Selection, runtimePath string, out, errOut io.Writer, basePrompt string, walk *effortModelWalk, maxTries int, timeout time.Duration, commitOverrides []string, retryDelays []time.Duration) (*RunTaskResult, error) {
	if errOut == nil {
		errOut = os.Stderr
	}
	display := outputFor(out)
	if pos, total := afkOrdinal(sel.Manifest, sel.TaskID); pos > 0 {
		display.line(ansiBold+ansiCyan, "━━ Running task %s/%s (%d/%d): %s", sel.TaskSetID, sel.TaskID, pos, total, sel.Task.Title)
	} else {
		display.line(ansiBold+ansiCyan, "━━ Running task %s/%s: %s", sel.TaskSetID, sel.TaskID, sel.Task.Title)
	}
	// Captured attempt streams written by this invocation, for the inline
	// breakdown when the task reaches a terminal state. Full history stays
	// with `pop tasks stream`.
	var streamPaths []string
	for attempt := 1; attempt <= maxTries; attempt++ {
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
			persist     func(rec *streamRecorder, attempt int, outcome, reason string, exitCode int)
			escalated   *AgentProceedVerdict
		)
		for {
			build, exhausted, err := walk.builder()
			if err != nil {
				return nil, taskExitErr(sel, ExitSetup, "%v", err)
			}
			if exhausted != nil {
				escalated = exhausted
				break
			}
			invocation, err = build(prompt)
			if err != nil {
				return nil, taskExitErr(sel, ExitSetup, "%v", err)
			}
			entry := invocation
			persist = func(rec *streamRecorder, attempt int, outcome, reason string, exitCode int) {
				if p := persistAttemptStream(d, errOut, sel, rec, entry.AgentPreset(), entry.RequestedAgent, attempt, outcome, reason, exitCode); p != "" {
					streamPaths = append(streamPaths, p)
				}
			}
			display.line(ansiDim, "   Attempt %d/%d · %s", attempt, maxTries, invocation.RequestedAgent)
			display.line(ansiDim, "── Agent output ────────────────────────────────────────")

			agentOut, attemptOut, err := runAgentAttempt(d, runtimePath, out, timeout, invocation)
			if err != nil {
				display.line(ansiRed, "✗ Agent failed to start for %s/%s", sel.TaskSetID, sel.TaskID)
				return nil, taskExitErr(sel, ExitOperational, "agent execution: %v", err)
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
				return nil, taskExitErr(sel, ExitOperational, "%v", err)
			}
			// The run is persisted as what the verdict finally condemned: a skip
			// when another tier entry takes over, an unusable agent when the
			// escalation hands the whole preset on.
			if stop != nil {
				persist(outcome.stream, attempt, streamOutcomeAgentUnusable, refused.Reason, outcome.exitCode)
				escalated = stop
				break
			}
			if p := persistSkippedAttemptStream(d, errOut, sel, outcome.stream, entry.AgentPreset(), entry.RequestedAgent, refused.Model, attempt, refused.Reason, outcome.exitCode); p != "" {
				streamPaths = append(streamPaths, p)
			}
			// The verdict is spent here — this attempt starts over on the tier's
			// next entry, and nothing downstream should see it.
			agentResult = AgentResult{}
			display.line(ansiDim, "   %s", refused.effortModelSkipMessage("Agent", walk.nextModel()))
		}
		if escalated != nil {
			return proceedVerdictResult(sel, *escalated), nil
		}
		if outcome.interrupted {
			persist(outcome.stream, attempt, streamOutcomeInterrupted, "", outcome.exitCode)
			return nil, taskExitErr(sel, ExitInterrupted, "interrupted")
		}
		if outcome.timedOut {
			timeoutReason := fmt.Sprintf("timed out after %s", timeout)
			persist(outcome.stream, attempt, streamOutcomeTimedOut, timeoutReason, outcome.exitCode)
			display.line(ansiRed, "✗ Attempt %d/%d timed out after %s", attempt, maxTries, timeout)
			// A timeout almost always means execution ran too long (an oversized
			// context window), not a doomed approach. The retry restarts from the
			// compact prior-attempt "continue" digest (ADR 0040), carried forward at
			// the top of the loop, rather than the bloated transcript — so a wait
			// adds nothing and the retry fires instantly. It consumes one slot of the
			// shared max_tries budget; only the final attempt finalizes Failed.
			if attempt < maxTries {
				display.line(ansiYellow, "↻ Retrying instantly with preserved changes...")
				continue
			}
			printAttemptBreakdown(d, out, streamPaths)
			summary := fmt.Sprintf("timed out after %s on attempt %d", timeout, attempt)
			if err := finalizeTaskFailed(d, sel, attempt, summary); err != nil {
				return nil, taskExitErr(sel, ExitOperational, "%v", err)
			}
			return nil, taskExitErr(sel, ExitOperational, "%s", summary)
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
			persist(outcome.stream, attempt, streamOutcomeTurnCapExhausted, reason, outcome.exitCode)
			display.line(ansiRed, "✗ Attempt %d/%d %s", attempt, maxTries, reason)
			if attempt < maxTries {
				display.line(ansiYellow, "↻ Retrying instantly with preserved changes...")
				continue
			}
			printAttemptBreakdown(d, out, streamPaths)
			summary := fmt.Sprintf("%s on attempt %d", reason, attempt)
			if err := finalizeTaskFailed(d, sel, attempt, summary); err != nil {
				return nil, taskExitErr(sel, ExitOperational, "%v", err)
			}
			return nil, taskExitErr(sel, ExitOperational, "%s", summary)
		}
		if outcome.runErr != nil {
			return nil, taskExitErr(sel, ExitOperational, "agent execution: %v", outcome.runErr)
		}
		if agentResult.ProceedVerdict != nil {
			v := stampDetectedVerdict(*agentResult.ProceedVerdict, invocation.AgentPreset(), invocation.PinnedModel())
			if _, ok := v.TimeHealing(); ok {
				v = v.WithResetAt(agentQuotaResetAt(v.Preset, v.Reason, time.Now()))
				persist(outcome.stream, attempt, streamOutcomeQuotaPaused, "", outcome.exitCode)
				display.line(ansiYellow, "Paused: agent quota exhausted for %s/%s", sel.TaskSetID, sel.TaskID)
				display.line(ansiYellow, "  %s", v.Reason)
			} else {
				persist(outcome.stream, attempt, streamOutcomeAgentUnusable, v.Reason, outcome.exitCode)
			}
			return proceedVerdictResult(sel, v), nil
		}

		taskData, err := d.FS.ReadFile(sel.TaskPath)
		if err != nil {
			return nil, taskExitErr(sel, ExitOperational, "read task markdown: %v", err)
		}

		assessment, reason := assessAttempt(agentResult.Output, outcome.exitCode, taskData)
		streamOutcome := streamOutcomeFailed
		if assessment.Complete {
			streamOutcome = streamOutcomeCompleted
		}
		persist(outcome.stream, attempt, streamOutcome, reason, outcome.exitCode)
		if assessment.Complete {
			result, err := completeSuccessfulTask(d, sel, runtimePath, assessment.Summary, commitOverrides)
			if err != nil {
				return nil, taskExitErr(sel, ExitOperational, "%v", err)
			}
			printConciseSummary(out, result)
			printAttemptBreakdown(d, out, streamPaths)
			return result, nil
		}

		display.line(ansiRed, "✗ Attempt %d/%d failed: %s", attempt, maxTries, reason)
		if attempt < maxTries {
			delay := attemptRetryDelay(retryDelays, attempt)
			if delay <= 0 {
				display.line(ansiYellow, "↻ Retrying with preserved changes...")
			} else if waitRetryDelay(d, out, delay) {
				return nil, taskExitErr(sel, ExitInterrupted, "interrupted")
			}
			continue
		}

		printAttemptBreakdown(d, out, streamPaths)
		summary := fmt.Sprintf("failed after %d attempts: %s", maxTries, reason)
		if err := finalizeTaskFailed(d, sel, maxTries, summary); err != nil {
			return nil, taskExitErr(sel, ExitOperational, "%v", err)
		}
		return nil, taskExitErr(sel, ExitOperational, "%s", summary)
	}
	return nil, taskExitErr(sel, ExitOperational, "unexpected attempt loop exit")
}

// executeTaskAttemptsWithAgentFallback walks the two nested lists a task's
// agents form: the Agent fallback presets, and inside each preset the Effort
// tier resolveSpec resolves. agentSpecs are the base specs, before Effort
// resolution — the tier is resolved per attempt so a recorded Effort model skip
// is filtered out of every resolution after the one that recorded it (ADR-0168).
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
		result, execErr := executeTaskAttempts(d, sel, runtimePath, out, errOut, basePrompt, walk, maxTries, timeout, commitOverrides, retryDelays)
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
	if len(unavailableResults) == 0 {
		return nil, taskExitErr(sel, ExitOperational, "no agent attempts were run")
	}
	return resolveAgentFallbackVerdict(sel, unavailableResults), nil
}

func proceedVerdictResult(sel *Selection, v AgentProceedVerdict) *RunTaskResult {
	result := &RunTaskResult{
		Selection:      sel,
		ProceedVerdict: &v,
	}
	if v.Kind == ProceedQuotaPause {
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

func assessAttempt(agentOut string, exitCode int, taskData []byte) (Assessment, string) {
	if exitCode != 0 {
		return Assessment{}, fmt.Sprintf("agent exited with status %d", exitCode)
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
		made, err := createImplementationCommit(d, runtimePath, implementationSubject(sel), summary, commitOverrides)
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

func createImplementationCommit(d *Deps, runtimePath, subject, summary string, commitOverrides []string) (*ImplementationCommit, error) {
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
	if _, err := d.Git.CommandInDir(runtimePath, commitGitArgs(commitOverrides, "commit", "-m", subject, "-m", summary)...); err != nil {
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
