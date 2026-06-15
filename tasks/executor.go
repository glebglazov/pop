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
	"github.com/glebglazov/pop/project"
)

const (
	DefaultMaxTries       = 3
	DefaultAttemptTimeout = 1 * time.Hour

	DirtyRuntimeContinue          DirtyRuntimeStrategy = "continue"
	DirtyRuntimeCommitAndContinue DirtyRuntimeStrategy = "commit-and-continue"
	DirtyRuntimeStashAndContinue  DirtyRuntimeStrategy = "stash-and-continue"
)

// DirtyRuntimeStrategy controls how a dirty runtime checkout is prepared for execution.
type DirtyRuntimeStrategy string

// Set validates and assigns a dirty-runtime strategy for Cobra flag parsing.
func (s *DirtyRuntimeStrategy) Set(value string) error {
	switch DirtyRuntimeStrategy(value) {
	case DirtyRuntimeContinue, DirtyRuntimeCommitAndContinue, DirtyRuntimeStashAndContinue:
		*s = DirtyRuntimeStrategy(value)
		return nil
	default:
		return fmt.Errorf("invalid dirty-runtime strategy %q; valid candidates: %s", value, strings.Join(ValidDirtyRuntimeStrategies(), ", "))
	}
}

func (s DirtyRuntimeStrategy) String() string { return string(s) }

func (s DirtyRuntimeStrategy) Type() string { return "dirty-runtime-strategy" }

// ValidDirtyRuntimeStrategies returns the accepted --allow-dirty values.
func ValidDirtyRuntimeStrategies() []string {
	return []string{
		string(DirtyRuntimeContinue),
		string(DirtyRuntimeCommitAndContinue),
		string(DirtyRuntimeStashAndContinue),
	}
}

// RunTaskOptions configures a single-task execution.
type RunTaskOptions struct {
	ResolveInput
	TaskPathOverride   string
	AgentPreset        string
	DefaultAgentPreset string
	// AgentExplicit reports the --agent flag was explicitly passed
	// (Flags().Changed), letting it override a task's `agent` key (ADR-0018).
	AgentExplicit bool
	AgentCmd      string
	AgentOutput   AgentOutputMode
	AllowDirty    DirtyRuntimeStrategy
	MaxTries      int
	Timeout       time.Duration
	Yes           bool
	ConfirmIn     io.Reader
	ConfirmOut    io.Writer
	Output        io.Writer
	// BindCheckout mirrors RunTaskSetOptions.BindCheckout for the single-task
	// path: it adopts the current checkout into the binding model (ADR-0036)
	// once the run has committed to its set and runtime checkout.
	BindCheckout func(setID, projectPath, runtimePath string) error
}

// RunTaskResult is the outcome of a successful or declined run-task.
type RunTaskResult struct {
	Selection   *Selection
	Refresh     *RefreshResult
	Declined    bool
	NoOp        bool
	QuotaPaused bool
	PauseReason string
	// PausePreset names the agent preset whose quota ran out, when QuotaPaused.
	PausePreset  string
	PauseResetAt time.Time
	CommitSHA    string
	AgentSummary string
}

type attemptOutcome struct {
	output      string
	exitCode    int
	timedOut    bool
	interrupted bool
	runErr      error
	stream      *streamRecorder
}

// RunTask executes one task task through an agent.
func RunTask(opts RunTaskOptions) (*RunTaskResult, error) {
	return RunTaskWith(defaultDeps, project.DefaultDeps(), config.Load, opts)
}

// RunTaskWith executes one task using injected dependencies.
func RunTaskWith(d *Deps, pd *project.Deps, loadConfig func(string) (*config.Config, error), opts RunTaskOptions) (*RunTaskResult, error) {
	if d.Runner == nil {
		d.Runner = RealCommandRunner{}
	}
	baseAgentPreset := resolveDefaultAgentPreset(opts.AgentPreset, opts.DefaultAgentPreset, opts.AgentExplicit)
	agentOutput := AgentOutputAuto
	if opts.AgentCmd == "" {
		var err error
		agentOutput, err = resolveAgentOutputMode(loadConfig, baseAgentPreset, opts.AgentOutput)
		if err != nil {
			return nil, exitErr(ExitSetup, "%v", err)
		}
	}
	if err := validateDirtyRuntimeStrategy(opts.AllowDirty); err != nil {
		return nil, exitErr(ExitSetup, "%v", err)
	}
	strategy := resolveDirtyRuntimeStrategy(opts.AllowDirty)

	// Resolve commit-config overrides up front (the lazy validation point) so a
	// malformed [workload.git] entry fails the drain hard before any commit —
	// including the dirty-runtime checkpoint, which commits earliest.
	commitOverrides, err := resolveCommitConfigOverrides(loadConfig)
	if err != nil {
		return nil, exitErr(ExitSetup, "%v", err)
	}

	resolved, err := ResolvePathsWith(d, pd, loadConfig, opts.ResolveInput)
	if err != nil {
		return nil, exitErr(ExitSetup, "%v", err)
	}

	runtimePath, err := ResolveRuntimePathWith(d, resolved.ProjectPath, opts.RuntimeOverride)
	if err != nil {
		return nil, exitErr(ExitSetup, "%v", err)
	}

	statePath := StatePathFor(resolved.DefinitionPath)
	refresh, err := RefreshWith(d, resolved.DefinitionPath, statePath)
	if err != nil {
		return nil, exitErr(ExitSetup, "%v", err)
	}

	taskSetID, taskID, err := ResolveTaskTarget(refresh, opts.TaskPathOverride)
	if err != nil {
		return nil, err
	}
	if err := RejectArchivedTaskSet(d, statePath, resolved.DefinitionPath, taskSetID); err != nil {
		return nil, err
	}

	sel, err := SelectTask(refresh, taskSetID, taskID)
	if err != nil {
		return nil, err
	}

	// Cross-checkout backstop: reject if this same (repo, set) is already live
	// in any other worktree of the repository. The per-checkout local lock
	// handles same-checkout conflicts; this closes the gap across checkouts.
	if err := CheckCrossCheckoutConflict(d, resolved.ProjectPath, runtimePath, sel.TaskSetID); err != nil {
		return nil, err
	}

	confirmOut := opts.ConfirmOut
	if confirmOut == nil {
		confirmOut = os.Stderr
	}
	out := opts.Output
	if out == nil {
		out = os.Stdout
	}

	lock, err := AcquireRuntimeLockForSet(d, runtimePath, sel.TaskSetID, confirmOut)
	if err != nil {
		return nil, err
	}
	defer lock.Release()

	// Adopt this checkout into the binding model (ADR-0036): worktree-locus runs
	// record a never-delete adopted binding; trunk-locus runs record nothing.
	if opts.BindCheckout != nil {
		if err := opts.BindCheckout(sel.TaskSetID, resolved.ProjectPath, runtimePath); err != nil {
			return nil, exitErr(ExitOperational, "bind checkout: %v", err)
		}
	}

	dirty, err := runtimeIsDirty(d, runtimePath)
	if err != nil {
		return nil, exitErr(ExitSetup, "runtime git status: %v", err)
	}

	displayRows := cloneRows(refresh.Rows)
	MarkAutoPick(displayRows)
	MarkRunTarget(displayRows, sel.TaskSetID)
	displayRefresh := *refresh
	displayRefresh.Rows = displayRows

	fmt.Fprintln(out)
	Render(out, &displayRefresh)

	if dirty {
		if err := reportDirtyRuntime(d, confirmOut, runtimePath, strategy); err != nil {
			return nil, exitErr(ExitSetup, "runtime git status: %v", err)
		}
	}

	confirmed, err := confirmExecution(opts.ConfirmIn, confirmOut, opts.Yes, taskConfirmPrompt)
	if err != nil {
		return nil, err
	}
	if !confirmed {
		return &RunTaskResult{Selection: sel, Refresh: refresh, Declined: true}, nil
	}

	if dirty {
		if err := applyDirtyRuntimeStrategy(d, runtimePath, sel.TaskSetID, sel.TaskID, strategy, commitOverrides, confirmOut); err != nil {
			return nil, taskExitErr(sel, ExitOperational, "dirty-runtime strategy: %v", err)
		}
	}

	agentSpec := resolveTaskAgentSpec(opts.AgentPreset, opts.DefaultAgentPreset, opts.AgentExplicit, opts.AgentCmd, sel.Task.Agent)
	if opts.AgentCmd == "" {
		effortConfig, err := loadConfigIfPresent(loadConfig)
		if err != nil {
			return nil, taskExitErr(sel, ExitSetup, "%v", err)
		}
		agentSpec = resolveTaskAgentSpecForEffortWithConfig(agentSpec, sel.Task.Effort, sel.Task.EffortExplicit, effortConfig)
	}
	if agentSpec != baseAgentPreset {
		agentOutput, err = resolveAgentOutputMode(loadConfig, agentSpec, opts.AgentOutput)
		if err != nil {
			return nil, taskExitErr(sel, ExitSetup, "%v", err)
		}
	}

	basePrompt := BuildAgentPrompt(sel.TaskPath, runtimePath)
	buildInvocation := func(prompt string) (*AgentInvocation, error) {
		return ResolveAgentInvocationWithMode(agentSpec, opts.AgentCmd, prompt, runtimePath, agentOutput)
	}

	maxTries := opts.MaxTries
	if maxTries <= 0 {
		maxTries = DefaultMaxTries
	}
	timeout := opts.Timeout
	if timeout <= 0 {
		timeout = DefaultAttemptTimeout
	}

	result, execErr := executeTaskAttempts(d, sel, runtimePath, out, confirmOut, basePrompt, buildInvocation, maxTries, timeout, commitOverrides)
	if execErr != nil {
		afterRefresh, refreshErr := RefreshWith(d, resolved.DefinitionPath, statePath)
		if refreshErr == nil && !opts.Yes {
			fmt.Fprintln(out)
			Render(out, afterRefresh)
		}
		return result, execErr
	}

	afterRefresh, err := RefreshWith(d, resolved.DefinitionPath, statePath)
	if err != nil {
		return nil, taskExitErr(sel, ExitOperational, "refresh after completion: %v", err)
	}
	result.Refresh = afterRefresh

	if !opts.Yes {
		fmt.Fprintln(out)
		Render(out, afterRefresh)
	}

	return result, nil
}

// executeTaskAttempts runs the retry loop for one task. The prompt is rebuilt
// per attempt (via buildInvocation over basePrompt) so a retry can carry this
// task's own prior-attempt digest forward; attempt 1 runs the base prompt
// unchanged (ADR 0023).
func executeTaskAttempts(d *Deps, sel *Selection, runtimePath string, out, errOut io.Writer, basePrompt string, buildInvocation func(prompt string) (*AgentInvocation, error), maxTries int, timeout time.Duration, commitOverrides []string) (*RunTaskResult, error) {
	if errOut == nil {
		errOut = os.Stderr
	}
	display := outputFor(out)
	display.line(ansiBold+ansiCyan, "━━ Running task %s/%s: %s", sel.TaskSetID, sel.TaskID, sel.Task.Title)
	// Captured attempt streams written by this invocation, for the inline
	// breakdown when the task reaches a terminal state. Full history stays
	// with `pop tasks timings`.
	var streamPaths []string
	for attempt := 1; attempt <= maxTries; attempt++ {
		prompt := basePrompt
		if attempt > 1 {
			// A retry carries two feeds forward so it converges instead of
			// repeating (ADR 0023): briefs of sibling tasks already completed in
			// the set (cross-task orientation), then this task's own prior-attempt
			// story. Both are harness-built, never a pointer to a raw stream (ADR
			// 0020).
			var carry strings.Builder
			if briefs := formatSiblingCompletedBriefs(d, sel.Manifest); briefs != "" {
				carry.WriteString("\n" + briefs)
			}
			if digest := buildPriorAttemptDigest(d, sel.Manifest.Dir, sel.TaskFile); digest != "" {
				carry.WriteString("\n" + digest)
			}
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
				return nil, taskExitErr(sel, ExitOperational, "%v", manualRepairErr(err))
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
			display.line(ansiYellow, "↻ Retrying with preserved changes...")
			continue
		}

		printAttemptBreakdown(d, out, streamPaths)
		summary := fmt.Sprintf("failed after %d attempts: %s", maxTries, reason)
		if err := finalizeTaskFailed(d, sel, maxTries, summary); err != nil {
			return nil, taskExitErr(sel, ExitOperational, "%v", manualRepairErr(err))
		}
		return nil, taskExitErr(sel, ExitOperational, "%s", summary)
	}
	return nil, taskExitErr(sel, ExitOperational, "unexpected attempt loop exit")
}

// resolveCommitConfigOverrides loads config and validates the commit-config
// overrides for the drain path. A nil loadConfig (or a load that fails to find
// a config file) yields no overrides — commits behave exactly as today. A
// malformed entry is returned as a hard error so the caller fails the drain.
func resolveCommitConfigOverrides(loadConfig func(string) (*config.Config, error)) ([]string, error) {
	if loadConfig == nil {
		return nil, nil
	}
	cfg, err := loadConfig(config.DefaultConfigPath())
	if err != nil {
		// A missing/unreadable config is not a drain-stopping error here; the
		// rest of the run already tolerates it. Only a present-but-malformed
		// override entry must fail hard, which ResolveCommitConfigOverrides does.
		return nil, nil
	}
	return cfg.ResolveCommitConfigOverrides()
}

func taskExitErr(sel *Selection, code int, format string, args ...any) *ExitError {
	return exitErr(code, "task %s/%s: %s", sel.TaskSetID, sel.TaskID, fmt.Sprintf(format, args...))
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

	if err := finalizeTaskDone(d, sel, summary); err != nil {
		return nil, manualRepairErr(err)
	}
	return result, nil
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

func finalizeTaskFailed(d *Deps, sel *Selection, attemptsStarted int, summary string) error {
	if err := AppendProgress(d, sel.Manifest.Dir, sel.TaskFile, "FAILED", summary); err != nil {
		return fmt.Errorf("append progress: %w", err)
	}
	sel.Manifest.Tasks[sel.TaskIndex].Status = "failed"
	failedAfter := attemptsStarted
	sel.Manifest.Tasks[sel.TaskIndex].FailedAfter = &failedAfter
	return WriteManifestAtomic(d, sel.Manifest)
}

func manualRepairErr(err error) *ExitError {
	return exitErr(ExitOperational, "local bookkeeping failed; manual repair required: %v", err)
}

func cloneRows(rows []Row) []Row {
	out := make([]Row, len(rows))
	copy(out, rows)
	return out
}

const taskConfirmPrompt = "Run task? [y/N]: "

// NonInteractiveReader marks explicit non-interactive confirmation input (for tests and automation).
type NonInteractiveReader struct{}

func (NonInteractiveReader) Read([]byte) (int, error) { return 0, io.EOF }

func confirmExecution(in io.Reader, out io.Writer, yes bool, prompt string) (bool, error) {
	if yes {
		return true, nil
	}
	if _, ok := in.(NonInteractiveReader); ok {
		return false, exitErr(ExitOperational, "non-interactive execution requires --yes or -y")
	}
	if in == nil {
		in = os.Stdin
	}
	interactive := in != os.Stdin || isInteractive(in)
	if !interactive {
		return false, exitErr(ExitOperational, "non-interactive execution requires --yes or -y")
	}
	if prompt == "" {
		prompt = taskConfirmPrompt
	}
	display := outputFor(out)
	fmt.Fprintf(display, "%s", display.styled(ansiCyan, prompt))
	var answer string
	if _, err := fmt.Fscanln(in, &answer); err != nil && err != io.EOF {
		return false, exitErr(ExitOperational, "read confirmation: %v", err)
	}
	answer = strings.ToLower(strings.TrimSpace(answer))
	return answer == "y" || answer == "yes", nil
}

func isInteractive(r io.Reader) bool {
	f, ok := r.(*os.File)
	if !ok {
		return false
	}
	info, err := f.Stat()
	if err != nil {
		return false
	}
	return (info.Mode() & os.ModeCharDevice) != 0
}

func runtimeIsDirty(d *Deps, runtimePath string) (bool, error) {
	out, err := d.Git.CommandInDir(runtimePath, "status", "--porcelain")
	if err != nil {
		return false, err
	}
	return strings.TrimSpace(out) != "", nil
}

// resolveDirtyRuntimeStrategy treats an unset strategy as the continue default.
func resolveDirtyRuntimeStrategy(strategy DirtyRuntimeStrategy) DirtyRuntimeStrategy {
	if strategy == "" {
		return DirtyRuntimeContinue
	}
	return strategy
}

func validateDirtyRuntimeStrategy(strategy DirtyRuntimeStrategy) error {
	if strategy == "" {
		return nil
	}
	var parsed DirtyRuntimeStrategy
	return parsed.Set(string(strategy))
}

// dirtyStrategyEffect describes, in one sentence, what the strategy will do to a
// dirty runtime checkout. Surfaced in the dirty-runtime confirmation.
func dirtyStrategyEffect(strategy DirtyRuntimeStrategy) string {
	switch strategy {
	case DirtyRuntimeCommitAndContinue:
		return "Strategy commit-and-continue: a checkpoint commit capturing this dirty state will be created before execution."
	case DirtyRuntimeStashAndContinue:
		return "Strategy stash-and-continue: tracked and untracked changes will be stashed before execution; restore the stash manually when ready."
	default:
		return "Strategy continue: execution proceeds without modifying these changes."
	}
}

// reportDirtyRuntime prints git status for the dirty runtime checkout followed by
// the chosen strategy's effect, so the operator can confirm with full context.
func reportDirtyRuntime(d *Deps, w io.Writer, runtimePath string, strategy DirtyRuntimeStrategy) error {
	status, err := d.Git.CommandInDir(runtimePath, "status")
	if err != nil {
		return err
	}
	out := outputFor(w)
	fmt.Fprintln(out)
	out.line(ansiYellow, "Runtime checkout has uncommitted changes:")
	fmt.Fprintln(out)
	fmt.Fprint(out, status)
	if !strings.HasSuffix(status, "\n") {
		fmt.Fprintln(out)
	}
	fmt.Fprintln(out)
	out.line(ansiYellow, "%s", dirtyStrategyEffect(strategy))
	return nil
}

func applyDirtyRuntimeStrategy(d *Deps, runtimePath, taskSetID, taskID string, strategy DirtyRuntimeStrategy, commitOverrides []string, out io.Writer) error {
	switch strategy {
	case DirtyRuntimeContinue:
		return nil
	case DirtyRuntimeCommitAndContinue:
		return checkpointDirtyRuntime(d, runtimePath, taskSetID, taskID, commitOverrides)
	case DirtyRuntimeStashAndContinue:
		return stashDirtyRuntime(d, runtimePath, out)
	default:
		return validateDirtyRuntimeStrategy(strategy)
	}
}

// commitGitArgs prepends `-c key=value` pairs (one per configured commit-config
// override) before a git subcommand's arguments. With no overrides it returns
// args unchanged, so unconfigured commits are byte-for-byte identical to today.
func commitGitArgs(overrides []string, args ...string) []string {
	if len(overrides) == 0 {
		return args
	}
	out := make([]string, 0, len(overrides)*2+len(args))
	for _, kv := range overrides {
		out = append(out, "-c", kv)
	}
	return append(out, args...)
}

func checkpointDirtyRuntime(d *Deps, runtimePath, taskSetID, taskID string, commitOverrides []string) error {
	if _, err := d.Git.CommandInDir(runtimePath, "add", "-A"); err != nil {
		return err
	}
	staged, err := d.Git.CommandInDir(runtimePath, "diff", "--cached", "--name-only")
	if err != nil {
		_, _ = d.Git.CommandInDir(runtimePath, "reset")
		return err
	}
	if strings.TrimSpace(staged) == "" {
		return nil
	}
	subject := DirtyCheckpointSubject(taskSetID, taskID)
	if _, err := d.Git.CommandInDir(runtimePath, commitGitArgs(commitOverrides, "commit", "-m", subject)...); err != nil {
		_, _ = d.Git.CommandInDir(runtimePath, "reset")
		return err
	}
	return nil
}

func stashDirtyRuntime(d *Deps, runtimePath string, out io.Writer) error {
	before, _ := d.Git.CommandInDir(runtimePath, "rev-parse", "--verify", "refs/stash")
	if _, err := d.Git.CommandInDir(runtimePath, "stash", "push", "--include-untracked"); err != nil {
		return err
	}
	after, err := d.Git.CommandInDir(runtimePath, "rev-parse", "--verify", "refs/stash")
	if err != nil || strings.TrimSpace(after) == strings.TrimSpace(before) {
		return nil
	}
	outputFor(out).line(ansiYellow, "Created stash: stash@{0}. Restore it manually when ready.")
	return nil
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

func finalizeTaskDone(d *Deps, sel *Selection, summary string) error {
	if err := AppendProgress(d, sel.Manifest.Dir, sel.TaskFile, "DONE", summary); err != nil {
		return err
	}
	sel.Manifest.Tasks[sel.TaskIndex].Status = "done"
	return WriteManifestAtomic(d, sel.Manifest)
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
