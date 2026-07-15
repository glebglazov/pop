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
	runErr      error
	stream      *streamRecorder
}

// buildAgentInvocationFactory returns the per-agentSpec invocation builder
// shared by both drain entry points (RunTaskWith and runSelectedTask): the base
// preset reuses the already-resolved agentOutput, while any other preset in the
// fallback chain re-resolves its own output mode.
func buildAgentInvocationFactory(loadConfig func(string) (*config.Config, error), runtimePath, baseAgentPreset, agentCmd string, agentOutput, optAgentOutput AgentOutputMode) func(agentSpec string) (func(string) (*AgentInvocation, error), error) {
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
			return ResolveAgentInvocationWithMode(agentSpec, agentCmd, prompt, runtimePath, attemptOutput)
		}, nil
	}
}

func taskExitErr(sel *Selection, code int, format string, args ...any) *ExitError {
	return exitErr(code, "task %s/%s: %s", sel.TaskSetID, sel.TaskID, fmt.Sprintf(format, args...))
}

// executeTaskAttempts runs the retry loop for one task. The prompt is rebuilt
// per attempt (via buildInvocation over basePrompt) so a retry can carry this
// task's own prior-attempt digest forward; attempt 1 runs the base prompt
// unchanged (ADR 0040).
func executeTaskAttempts(d *Deps, sel *Selection, runtimePath string, out, errOut io.Writer, basePrompt string, buildInvocation func(prompt string) (*AgentInvocation, error), maxTries int, timeout time.Duration, commitOverrides []string, retryDelays []time.Duration) (*RunTaskResult, error) {
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
		// Carry two harness-built feeds forward whenever they have content so
		// a retry converges instead of repeating (ADR 0040/ADR 0089): briefs of
		// sibling tasks already completed in the set (cross-task orientation),
		// then this task's own prior-attempt story. They fire on attempt 1 when
		// non-empty, which is how a resumed interrupted/quota-paused task sees
		// its own context immediately. Both are always harness-built, never a
		// pointer to a raw stream (ADR 0020).
		var carry strings.Builder
		if briefs := formatSiblingCompletedBriefs(d, sel.Manifest); briefs != "" {
			carry.WriteString("\n" + briefs)
		}
		if digest := buildPriorAttemptDigest(d, sel.Manifest.Dir, sel.TaskFile); digest != "" {
			carry.WriteString("\n" + digest)
		}
		if carry.Len() > 0 {
			prompt = basePrompt + carry.String()
		}
		invocation, err := buildInvocation(prompt)
		if err != nil {
			return nil, taskExitErr(sel, ExitSetup, "%v", err)
		}
		persist := func(rec *streamRecorder, attempt int, outcome, reason string, exitCode int) {
			if p := persistAttemptStream(d, errOut, sel, rec, invocation.AgentPreset(), invocation.RequestedAgent, attempt, outcome, reason, exitCode); p != "" {
				streamPaths = append(streamPaths, p)
			}
		}
		display.line(ansiDim, "   Attempt %d/%d · %s", attempt, maxTries, invocation.RequestedAgent)
		display.line(ansiDim, "── Agent output ────────────────────────────────────────")

		agentOut, outcome, err := runAgentAttempt(d, runtimePath, out, timeout, invocation)
		if err != nil {
			display.line(ansiRed, "✗ Agent failed to start for %s/%s", sel.TaskSetID, sel.TaskID)
			return nil, taskExitErr(sel, ExitOperational, "agent execution: %v", err)
		}
		if outcome.timedOut {
			display.line(ansiDim, "── Agent killed (timeout) for %s/%s ───────────────────", sel.TaskSetID, sel.TaskID)
		} else {
			display.line(ansiDim, "── Agent finished for %s/%s ───────────────────────────", sel.TaskSetID, sel.TaskID)
		}
		if outcome.interrupted {
			persist(outcome.stream, attempt, streamOutcomeInterrupted, "", outcome.exitCode)
			return nil, taskExitErr(sel, ExitInterrupted, "interrupted")
		}
		if outcome.timedOut {
			timeoutReason := fmt.Sprintf("timed out after %s", timeout)
			persist(outcome.stream, attempt, streamOutcomeTimedOut, timeoutReason, outcome.exitCode)
			summary := fmt.Sprintf("timed out after %s on attempt %d", timeout, attempt)
			display.line(ansiRed, "✗ Attempt %d/%d timed out after %s", attempt, maxTries, timeout)
			printAttemptBreakdown(d, out, streamPaths)
			if err := finalizeTaskFailed(d, sel, attempt, summary); err != nil {
				return nil, taskExitErr(sel, ExitOperational, "%v", err)
			}
			return nil, taskExitErr(sel, ExitOperational, "%s", summary)
		}
		if outcome.runErr != nil {
			return nil, taskExitErr(sel, ExitOperational, "agent execution: %v", outcome.runErr)
		}
		agentResult := invocation.NormalizeOutput(agentOut)
		if agentResult.QuotaPause != nil {
			pause := *agentResult.QuotaPause
			pause.ResetAt = agentQuotaResetAt(invocation.AgentPreset(), pause.Reason, time.Now())
			persist(outcome.stream, attempt, streamOutcomeQuotaPaused, "", outcome.exitCode)
			display.line(ansiYellow, "Paused: agent quota exhausted for %s/%s", sel.TaskSetID, sel.TaskID)
			display.line(ansiYellow, "  %s", pause.Reason)
			return &RunTaskResult{
				Selection:    sel,
				QuotaPaused:  true,
				PauseReason:  pause.Reason,
				PausePreset:  invocation.AgentPreset(),
				PauseResetAt: pause.ResetAt,
			}, nil
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
			} else if waitRetryDelay(out, delay) {
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

func executeTaskAttemptsWithAgentFallback(d *Deps, sel *Selection, runtimePath string, out, errOut io.Writer, basePrompt string, agentSpecs []string, buildForAgent func(agentSpec string) (func(prompt string) (*AgentInvocation, error), error), maxTries int, timeout time.Duration, commitOverrides []string, agentQuotaRetryAfter time.Duration, retryDelays []time.Duration) (*RunTaskResult, error) {
	cooldowns, err := readAgentCooldowns(d)
	if err != nil {
		return nil, taskExitErr(sel, ExitOperational, "%v", err)
	}
	activeCooldowns := activeAgentCooldowns(cooldowns, time.Now())
	specs := nonEmptyAgentSpecs(agentSpecs, DefaultAgentPreset)
	var quotaResults []*RunTaskResult
	for i, agentSpec := range specs {
		preset, err := AgentPresetName(agentSpec)
		if err != nil {
			return nil, taskExitErr(sel, ExitSetup, "%v", err)
		}
		if until, cooling := activeCooldowns[preset]; cooling {
			quotaResults = append(quotaResults, &RunTaskResult{
				Selection:    sel,
				QuotaPaused:  true,
				PauseReason:  fmt.Sprintf("agent quota cooldown until %s", until.UTC().Format(time.RFC3339)),
				PausePreset:  preset,
				PauseResetAt: until,
			})
			continue
		}
		buildInvocation, err := buildForAgent(agentSpec)
		if err != nil {
			return nil, taskExitErr(sel, ExitSetup, "%v", err)
		}
		result, execErr := executeTaskAttempts(d, sel, runtimePath, out, errOut, basePrompt, buildInvocation, maxTries, timeout, commitOverrides, retryDelays)
		if execErr != nil || result == nil || !result.QuotaPaused {
			return result, execErr
		}
		until := agentQuotaCooldownUntil(result.PauseResetAt, time.Now(), agentQuotaRetryAfter)
		if err := updateAgentCooldown(d, result.PausePreset, until); err != nil {
			return nil, taskExitErr(sel, ExitOperational, "%v", err)
		}
		activeCooldowns[result.PausePreset] = until
		quotaResults = append(quotaResults, result)
		if i+1 < len(specs) && out != nil {
			outputFor(out).line(ansiDim, "   Agent %s quota-paused; trying next", result.PausePreset)
		}
	}
	if len(quotaResults) == 0 {
		return nil, taskExitErr(sel, ExitOperational, "no agent attempts were run")
	}
	return earliestQuotaPauseResult(quotaResults), nil
}

func earliestQuotaPauseResult(results []*RunTaskResult) *RunTaskResult {
	var best *RunTaskResult
	for _, result := range results {
		if result == nil {
			continue
		}
		if best == nil {
			best = result
			continue
		}
		if result.PauseResetAt.IsZero() {
			continue
		}
		if best.PauseResetAt.IsZero() || result.PauseResetAt.Before(best.PauseResetAt) {
			best = result
		}
	}
	return best
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
		reason = "agent output did not satisfy completion contract"
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

	if hasChanges {
		sha, err := createImplementationCommit(d, runtimePath, sel.TaskSetID, sel.TaskID, summary, commitOverrides)
		if err != nil {
			return nil, exitErr(ExitOperational, "implementation commit: %v", err)
		}
		result.CommitSHA = sha
	} else {
		result.NoOp = true
	}

	if err := finalizeTaskDone(d, sel, runtimePath, summary); err != nil {
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

func createImplementationCommit(d *Deps, runtimePath, taskSetID, taskID, summary string, commitOverrides []string) (string, error) {
	if _, err := d.Git.CommandInDir(runtimePath, "add", "-A"); err != nil {
		return "", err
	}
	staged, err := d.Git.CommandInDir(runtimePath, "diff", "--cached", "--name-only")
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(staged) == "" {
		return "", nil
	}
	subject := CommitSubject(taskSetID, taskID)
	if _, err := d.Git.CommandInDir(runtimePath, commitGitArgs(commitOverrides, "commit", "-m", subject, "-m", summary)...); err != nil {
		return "", err
	}
	sha, err := d.Git.CommandInDir(runtimePath, "rev-parse", "HEAD")
	if err != nil {
		return "", err
	}
	return sha, nil
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

func finalizeTaskDone(d *Deps, sel *Selection, runtimePath, summary string) error {
	// Route the open→done write through the Task-transition chokepoint as
	// Executor; the chokepoint owns the DONE progress record, clearing the
	// attempt count under its uniform rule, and the atomic manifest write. This
	// open→done flows through the same ADR-0109 invalidation rule as a manual
	// completion — a no-op mid-drain, since the set has no cached verdicts until
	// it goes fully done.
	return ApplyTransitions(d, sel.Manifest, runtimePath, []TransitionOp{{
		TaskID:  sel.TaskID,
		To:      TaskDone,
		Actor:   ActorExecutor,
		Marker:  "DONE",
		Summary: summary,
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

func runAgentAttempt(d *Deps, runtimePath string, liveOut io.Writer, timeout time.Duration, invocation *AgentInvocation) (string, *attemptOutcome, error) {
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

	proc, err := d.Runner.Start(ctx, runtimePath, agentOut, agentOut, invocation.Name, invocation.Args...)
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
	}

	raw := capture.String()
	// Formats rendered live already streamed to liveOut; only the silently
	// captured formats still need the post-hoc dump.
	if invocation.OutputFormat != AgentOutputPlain && liveWriter == nil {
		if normalized := invocation.NormalizeOutput(raw); normalized.QuotaPause == nil {
			invocation.RenderOutput(liveOut, raw)
		}
	}
	return raw, outcome, nil
}
