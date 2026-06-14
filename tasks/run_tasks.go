package tasks

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/glebglazov/pop/config"
	"github.com/glebglazov/pop/project"
)

// RunTaskSetOptions configures sequential Task-set draining.
type RunTaskSetOptions struct {
	ResolveInput
	TaskSetOverride    string
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
}

// RunTaskSetResult is the outcome of a run-tasks invocation.
type RunTaskSetResult struct {
	TaskSetID       string
	Completed       []*RunTaskResult
	Refresh         *RefreshResult
	Declined        bool
	TaskSetDone     bool
	TaskSetDeferred bool
	SkippedTasks    []string
	BlockedReason   string
	QuotaPaused     bool
	PauseReason     string
	// PausePreset names the agent preset whose quota ran out, when QuotaPaused.
	PausePreset      string
	PausePinnedAgent bool
}

// RunTaskSet drains one Task set sequentially through eligible AFK tasks.
func RunTaskSet(opts RunTaskSetOptions) (*RunTaskSetResult, error) {
	return RunTaskSetWith(defaultDeps, project.DefaultDeps(), config.Load, opts)
}

// RunTaskSetWith drains one Task set using injected dependencies.
func RunTaskSetWith(d *Deps, pd *project.Deps, loadConfig func(string) (*config.Config, error), opts RunTaskSetOptions) (result *RunTaskSetResult, err error) {
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
	// including the per-task dirty-runtime checkpoint, which commits earliest.
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

	taskSetOverride, err := ResolveTaskSetTarget(refresh, opts.TaskSetOverride)
	if err != nil {
		return nil, err
	}
	if taskSetOverride != "" {
		if err := RejectArchivedTaskSet(d, statePath, resolved.DefinitionPath, taskSetOverride); err != nil {
			return nil, err
		}
	}

	taskSetID, hitlFallback, err := SelectTaskSet(refresh, taskSetOverride)
	if err != nil {
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

	lock, err := AcquireRuntimeLockForSet(d, runtimePath, taskSetID, confirmOut)
	if err != nil {
		return nil, err
	}
	defer lock.Release()

	// The lock is held, so this process owns the drain: record how it ends on
	// every exit path below (including a panic-free crash bubbling up as err) so
	// the supervisor can read the outcome without parsing human output. A
	// declined run writes no record.
	defer func() {
		if rec, ok := drainOutcomeFor(taskSetID, runtimePath, result, err); ok {
			_ = WriteDrainOutcome(d, rec)
		}
	}()

	dirty, err := runtimeIsDirty(d, runtimePath)
	if err != nil {
		return nil, exitErr(ExitSetup, "runtime git status: %v", err)
	}

	displayRows := cloneRows(refresh.Rows)
	MarkAutoPick(displayRows)
	MarkRunTarget(displayRows, taskSetID)
	displayRefresh := *refresh
	displayRefresh.Rows = displayRows

	if hitlFallback {
		outputFor(out).line(ansiYellow, "No runnable AFK work")
	}

	fmt.Fprintln(out)
	Render(out, &displayRefresh)

	if m := displayRefresh.Manifests[taskSetID]; m != nil {
		RenderTaskList(out, taskSetID, m)
	}

	if dirty {
		if err := reportDirtyRuntime(d, confirmOut, runtimePath, strategy); err != nil {
			return nil, exitErr(ExitSetup, "runtime git status: %v", err)
		}
	}

	initialGate := selectedTaskSetStartsAtHITLGate(refresh, taskSetID) ||
		selectedTaskSetStartsAtFailedGate(refresh, taskSetID)
	var sharedPromptReader *bufio.Reader
	if initialGate {
		sharedPromptReader = ensurePromptReader(sharedPromptReader, opts.ConfirmIn, opts.Yes)
	}

	maxTries := opts.MaxTries
	if maxTries <= 0 {
		maxTries = DefaultMaxTries
	}
	timeout := opts.Timeout
	if timeout <= 0 {
		timeout = DefaultAttemptTimeout
	}

	result = &RunTaskSetResult{TaskSetID: taskSetID}
	dirtyStrategyApplied := false

	for {
		currentRefresh, err := RefreshWith(d, resolved.DefinitionPath, statePath)
		if err != nil {
			return nil, exitErr(ExitOperational, "refresh before task selection: %v", err)
		}

		sel, selErr := SelectTaskInSet(currentRefresh, taskSetID)
		if selErr != nil {
			row := findRow(currentRefresh, taskSetID)
			if row == nil {
				return nil, selErr
			}
			result.Refresh = currentRefresh
			switch row.Status {
			case StatusDone:
				result.TaskSetDone = true
				finishRunTaskSet(out, opts.Yes, result)
				return result, nil
			case StatusDeferred:
				result.TaskSetDeferred = true
				result.SkippedTasks = SkippedTaskIDs(currentRefresh.Manifests[taskSetID])
				finishRunTaskSet(out, opts.Yes, result)
				return result, nil
			case StatusBlocked:
				result.BlockedReason = row.BlockedReason
				if !opts.Yes {
					fmt.Fprintln(out)
					Render(out, currentRefresh)
				} else {
					printTaskSetSummary(out, result)
				}
				if hitl := BlockingHITLTask(currentRefresh.Manifests[taskSetID]); hitl != nil {
					handled, err := handleInteractiveHITLGate(d, out, opts.ConfirmIn, sharedPromptReader, opts.Yes, opts.AgentPreset, opts.AgentCmd, opts.CWD, runtimePath, resolved.DefinitionPath, statePath, taskSetID, currentRefresh.Manifests[taskSetID], hitl)
					if err != nil {
						return nil, err
					}
					if handled {
						continue
					}
					printHITLGateAdvice(d, out, taskSetID, currentRefresh.Manifests[taskSetID].Dir, hitl)
				}
				if result.BlockedReason != "" {
					return nil, exitErr(ExitNoRunnable, "Task set %q blocked: %s", taskSetID, result.BlockedReason)
				}
				return nil, exitErr(ExitNoRunnable, "Task set %q has no eligible AFK task", taskSetID)
			case StatusFailed:
				if !opts.Yes {
					fmt.Fprintln(out)
					Render(out, currentRefresh)
				}
				m := currentRefresh.Manifests[taskSetID]
				sharedPromptReader = ensurePromptReader(sharedPromptReader, opts.ConfirmIn, opts.Yes)
				handled, err := handleInteractiveFailedGate(d, out, opts.ConfirmIn, sharedPromptReader, opts.Yes, opts.AgentPreset, opts.AgentCmd, opts.CWD, runtimePath, resolved.DefinitionPath, statePath, taskSetID, m, FailedTask(m))
				if err != nil {
					return nil, err
				}
				if handled {
					continue
				}
				printFailedStopAdvice(out, taskSetID, m)
				return nil, exitErr(ExitOperational, "Task set %q has failed tasks", taskSetID)
			default:
				return nil, selErr
			}
		}

		if dirty && !dirtyStrategyApplied {
			if err := applyDirtyRuntimeStrategy(d, runtimePath, sel.TaskSetID, sel.TaskID, strategy, commitOverrides, confirmOut); err != nil {
				return nil, taskExitErr(sel, ExitOperational, "dirty-runtime strategy: %v", err)
			}
			dirtyStrategyApplied = true
		}

		agentSpec := resolveTaskAgentSpec(opts.AgentPreset, opts.DefaultAgentPreset, opts.AgentExplicit, opts.AgentCmd, sel.Task.Agent)
		if opts.AgentCmd == "" {
			effortConfig, err := loadConfigIfPresent(loadConfig)
			if err != nil {
				return nil, taskExitErr(sel, ExitSetup, "%v", err)
			}
			agentSpec = resolveTaskAgentSpecForEffortWithConfig(agentSpec, sel.Task.Effort, sel.Task.EffortExplicit, effortConfig)
		}
		attemptOutput := agentOutput
		if agentSpec != baseAgentPreset {
			attemptOutput, err = resolveAgentOutputMode(loadConfig, agentSpec, opts.AgentOutput)
			if err != nil {
				return nil, taskExitErr(sel, ExitSetup, "%v", err)
			}
		}

		basePrompt := BuildAgentPrompt(sel.TaskPath, runtimePath)
		buildInvocation := func(prompt string) (*AgentInvocation, error) {
			return ResolveAgentInvocationWithMode(agentSpec, opts.AgentCmd, prompt, runtimePath, attemptOutput)
		}

		taskResult, execErr := executeTaskAttempts(d, sel, runtimePath, out, confirmOut, basePrompt, buildInvocation, maxTries, timeout, commitOverrides)
		if execErr != nil {
			afterRefresh, refreshErr := RefreshWith(d, resolved.DefinitionPath, statePath)
			if refreshErr == nil {
				result.Refresh = afterRefresh
				if !opts.Yes {
					fmt.Fprintln(out)
					Render(out, afterRefresh)
				}
				m := afterRefresh.Manifests[taskSetID]
				sharedPromptReader = ensurePromptReader(sharedPromptReader, opts.ConfirmIn, opts.Yes)
				handled, gateErr := handleInteractiveFailedGate(d, out, opts.ConfirmIn, sharedPromptReader, opts.Yes, opts.AgentPreset, opts.AgentCmd, opts.CWD, runtimePath, resolved.DefinitionPath, statePath, taskSetID, m, FailedTask(m))
				if gateErr != nil {
					return result, gateErr
				}
				if handled {
					continue
				}
				printFailedStopAdvice(out, taskSetID, m)
			}
			return result, execErr
		}
		if taskResult.QuotaPaused {
			result.QuotaPaused = true
			result.PauseReason = taskResult.PauseReason
			result.PausePreset = taskResult.PausePreset
			result.PausePinnedAgent = taskPinMatchesPreset(sel.Task.Agent, taskResult.PausePreset)
			result.Refresh = currentRefresh
			printTaskSetSummary(out, result)
			return result, nil
		}

		result.Completed = append(result.Completed, taskResult)
	}
}

func finishRunTaskSet(out io.Writer, yes bool, result *RunTaskSetResult) {
	if yes {
		printTaskSetSummary(out, result)
		return
	}
	fmt.Fprintln(out)
	Render(out, result.Refresh)
	if result.TaskSetDeferred {
		fmt.Fprintln(out, deferralMessage(result))
	}
}

func selectedTaskSetStartsAtHITLGate(refresh *RefreshResult, taskSetID string) bool {
	row := findRow(refresh, taskSetID)
	return row != nil && row.Status == StatusBlocked && BlockingHITLTask(refresh.Manifests[taskSetID]) != nil
}

// selectedTaskSetStartsAtFailedGate reports whether draining re-enters an
// already-Failed set, so the run goes straight to the Failed gate instead of
// asking for AFK consent first (the set has no immediately runnable task).
func selectedTaskSetStartsAtFailedGate(refresh *RefreshResult, taskSetID string) bool {
	row := findRow(refresh, taskSetID)
	return row != nil && row.Status == StatusFailed
}

// ensurePromptReader returns a single bufio.Reader reused across every gate
// prompt in one run. Reusing one reader matters: a fresh bufio.Reader buffers
// ahead on its first read, so making a new one per gate would swallow the input
// queued for later gates. Returns nil — and the caller falls back to static
// advice — when prompting is impossible (--yes or a non-interactive input).
func ensurePromptReader(existing *bufio.Reader, in io.Reader, yes bool) *bufio.Reader {
	if existing != nil {
		return existing
	}
	if yes || !canPrompt(in) {
		return nil
	}
	if in == nil {
		in = os.Stdin
	}
	return bufio.NewReader(in)
}

func deferralMessage(result *RunTaskSetResult) string {
	if len(result.SkippedTasks) > 0 {
		return fmt.Sprintf("Task set %s deferred: skipped %s", result.TaskSetID, strings.Join(result.SkippedTasks, ", "))
	}
	return fmt.Sprintf("Task set %s deferred", result.TaskSetID)
}

func printTaskSetSummary(w io.Writer, result *RunTaskSetResult) {
	out := outputFor(w)
	if result.TaskSetDone {
		out.line(ansiGreen, "✓ Completed task set %s (%d task(s))", result.TaskSetID, len(result.Completed))
		return
	}
	if result.TaskSetDeferred {
		out.line(ansiYellow, "%s", deferralMessage(result))
		return
	}
	if result.BlockedReason != "" {
		out.line(ansiYellow, "Task set %s blocked: %s", result.TaskSetID, result.BlockedReason)
		return
	}
	if result.QuotaPaused {
		out.line(ansiYellow, "Task set %s paused after %d completed task(s): agent quota exhausted", result.TaskSetID, len(result.Completed))
		return
	}
	fmt.Fprintf(out, "Task set %s stopped after %d task(s)\n", result.TaskSetID, len(result.Completed))
}

type hitlGateAction int

const (
	hitlGateExit hitlGateAction = iota
	hitlGateComplete
	hitlGateAssist
	hitlGateDefer
)

func handleInteractiveHITLGate(d *Deps, out io.Writer, in io.Reader, reader *bufio.Reader, yes bool, agentPreset, agentCmd, cwd, runtimePath, definitionPath, statePath, taskSetID string, m *Manifest, hitl *Task) (bool, error) {
	if yes || !canPrompt(in) || m == nil || hitl == nil {
		return false, nil
	}
	if in == nil {
		in = os.Stdin
	}
	if reader == nil {
		reader = bufio.NewReader(in)
	}

	prompt := BuildHITLAssistancePrompt(d, taskSetID, m, *hitl, runtimePath)
	body := gateTaskBody(d, m, hitl)
	invocation, err := ResolveAgentAssistanceInvocation(agentPreset, agentCmd, prompt, runtimePath)
	if err != nil {
		return false, exitErr(ExitSetup, "%v", err)
	}

	for {
		action, err := promptHITLGateAction(out, reader, taskSetID, hitl, body, invocation)
		if err != nil {
			return true, err
		}
		switch action {
		case hitlGateComplete:
			result, err := CompleteTaskWith(d, nil, nil, CompleteTaskOptions{ResolveInput: ResolveInput{CWD: cwd}, TaskPath: taskPathHint(taskSetID, hitl.File)})
			if err != nil {
				return true, err
			}
			RenderTaskComplete(out, result.TaskSetID, result.TaskID)
			return true, nil
		case hitlGateAssist:
			fmt.Fprintf(outputFor(out), "Starting HITL assistance: %s\n", invocation.Display)
			exitCode, err := runAttendedAssistanceCommand(d, in, runtimePath, out, invocation)
			if err != nil {
				fmt.Fprintf(outputFor(out), "Could not start HITL assistance: %v\n", err)
				continue
			}
			if exitCode != 0 {
				fmt.Fprintf(outputFor(out), "HITL assistance exited with status %d; refreshing Task set.\n", exitCode)
			}
			afterRefresh, err := RefreshWith(d, definitionPath, statePath)
			if err != nil {
				return true, exitErr(ExitOperational, "refresh after HITL assistance: %v", err)
			}
			afterManifest := afterRefresh.Manifests[taskSetID]
			if BlockingHITLTask(afterManifest) == nil {
				return true, nil
			}
			m = afterManifest
			prompt = BuildHITLAssistancePrompt(d, taskSetID, m, *BlockingHITLTask(m), runtimePath)
			invocation, err = ResolveAgentAssistanceInvocation(agentPreset, agentCmd, prompt, runtimePath)
			if err != nil {
				return true, exitErr(ExitSetup, "%v", err)
			}
			hitl = BlockingHITLTask(m)
			body = gateTaskBody(d, m, hitl)
		case hitlGateDefer:
			result, err := SkipTaskWith(d, nil, nil, SkipTaskOptions{ResolveInput: ResolveInput{CWD: cwd}, TaskPath: taskPathHint(taskSetID, hitl.File)})
			if err != nil {
				return true, err
			}
			RenderTaskSkip(out, result.TaskSetID, result.TaskID)
			return true, nil
		case hitlGateExit:
			return false, nil
		}
	}
}

// runAttendedAssistanceCommand runs the attended assistance agent. stdin must be
// the raw input source (the *os.File terminal), NOT the bufio.Reader used for
// gate prompts: os/exec only inherits a child's controlling terminal when
// cmd.Stdin is an *os.File. Handing it any other io.Reader makes exec splice a
// pipe instead, so a TTY-requiring agent (e.g. codex) fails immediately with
// "stdin is not a terminal".
func runAttendedAssistanceCommand(d *Deps, stdin io.Reader, runtimePath string, out io.Writer, invocation *AgentAssistanceInvocation) (int, error) {
	if attended, ok := d.Runner.(AttendedCommandRunner); ok {
		return attended.RunAttended(context.Background(), runtimePath, stdin, out, out, invocation.Command.Name, invocation.Command.Args...)
	}
	return d.Runner.Run(context.Background(), runtimePath, out, out, invocation.Command.Name, invocation.Command.Args...)
}

func canPrompt(in io.Reader) bool {
	if _, ok := in.(NonInteractiveReader); ok {
		return false
	}
	if in == nil {
		return isInteractive(os.Stdin)
	}
	return in != os.Stdin || isInteractive(in)
}

// gateTaskBody returns the raw task file body for inline display at a gate, or
// "" when it cannot be read. The agent prompt carries the body regardless; this
// is the copy the human reads before electing to act on the task by hand.
func gateTaskBody(d *Deps, m *Manifest, task *Task) string {
	if d == nil || m == nil || task == nil {
		return ""
	}
	fs := d.FS
	if fs == nil {
		fs = DefaultDeps().FS
	}
	data, err := fs.ReadFile(filepath.Join(m.Dir, task.File))
	if err != nil {
		return ""
	}
	return strings.TrimRight(string(data), "\n")
}

// renderGateTaskBody prints the blocking task's full body above a gate menu so a
// human electing to finish by hand can see every action point in place.
func renderGateTaskBody(display *output, taskFile, body string) {
	if body == "" {
		return
	}
	heading := fmt.Sprintf("--- %s ---", taskFile)
	fmt.Fprintln(display)
	fmt.Fprintln(display, heading)
	fmt.Fprintln(display, body)
	fmt.Fprintln(display, strings.Repeat("-", len(heading)))
}

func promptHITLGateAction(out io.Writer, reader *bufio.Reader, taskSetID string, hitl *Task, body string, invocation *AgentAssistanceInvocation) (hitlGateAction, error) {
	display := outputFor(out)
	fmt.Fprintln(display)
	display.line(ansiYellow, "Human-blocked: %s/%s needs human work before the set can continue.", taskSetID, hitl.ID)
	renderGateTaskBody(display, hitl.File, body)
	fmt.Fprintln(display, "  1. Get agent assistance (default)")
	if invocation != nil {
		fmt.Fprintf(display, "     %s\n", invocation.Display)
		if invocation.Detail != "" {
			fmt.Fprintf(display, "     %s\n", invocation.Detail)
		}
	}
	fmt.Fprintln(display, "  2. Complete task")
	fmt.Fprintln(display, "  3. Defer task")
	fmt.Fprintln(display, "  4. Exit")
	fmt.Fprintf(display, "%s", display.styled(ansiCyan, "Choose [1]: "))

	answer, err := readPromptLine(reader, "4")
	if err != nil {
		return hitlGateExit, err
	}
	switch strings.ToLower(strings.TrimSpace(answer)) {
	case "", "1":
		return hitlGateAssist, nil
	case "2":
		return hitlGateComplete, nil
	case "3":
		return hitlGateDefer, nil
	case "4", "q", "quit", "exit":
		return hitlGateExit, nil
	default:
		fmt.Fprintln(display, "Choose 1, 2, 3, or 4.")
		return promptHITLGateAction(out, reader, taskSetID, hitl, body, invocation)
	}
}

// readPromptLine reads one menu selection. eofDefault is returned when the
// input source closes with nothing pending, so a closed pipe resolves to a
// definite choice (each gate passes the number of its Exit option) instead of
// looping forever on empty reads.
func readPromptLine(reader *bufio.Reader, eofDefault string) (string, error) {
	answer, err := reader.ReadString('\n')
	if err != nil && err != io.EOF {
		return "", exitErr(ExitOperational, "read gate selection: %v", err)
	}
	if err == io.EOF && answer == "" {
		return eofDefault, nil
	}
	return strings.TrimRight(answer, "\r\n"), nil
}

type failedGateAction int

const (
	failedGateExit failedGateAction = iota
	failedGateRerun
	failedGateAssist
	failedGateComplete
)

// handleInteractiveFailedGate is the interactive counterpart to
// printFailedStopAdvice: it offers the same recovery paths as a numbered menu
// at both points where draining stops on a failed task. Returns (true, nil)
// when the caller should keep draining in-process — Re-run reset the task to
// open, Finish-by-hand marked it done — and (false, nil) when it should fall
// back to the static advice and exit with operational failure (Exit chosen, or
// the prompt cannot run under --yes / a non-interactive input).
func handleInteractiveFailedGate(d *Deps, out io.Writer, in io.Reader, reader *bufio.Reader, yes bool, agentPreset, agentCmd, cwd, runtimePath, definitionPath, statePath, taskSetID string, m *Manifest, failed *Task) (bool, error) {
	if yes || !canPrompt(in) || m == nil || failed == nil {
		return false, nil
	}
	if in == nil {
		in = os.Stdin
	}
	if reader == nil {
		reader = bufio.NewReader(in)
	}

	prompt := BuildFailedAssistancePrompt(d, taskSetID, m, *failed, runtimePath)
	body := gateTaskBody(d, m, failed)
	invocation, err := ResolveAgentAssistanceInvocation(agentPreset, agentCmd, prompt, runtimePath)
	if err != nil {
		return false, exitErr(ExitSetup, "%v", err)
	}

	for {
		action, err := promptFailedGateAction(out, reader, taskSetID, failed, body, invocation)
		if err != nil {
			return true, err
		}
		switch action {
		case failedGateRerun:
			result, err := ResetTaskWith(d, nil, nil, ResetTaskOptions{ResolveInput: ResolveInput{CWD: cwd}, TaskPath: taskPathHint(taskSetID, failed.File)})
			if err != nil {
				return true, err
			}
			RenderTaskReset(out, result.TaskSetID, result.TaskID)
			return true, nil
		case failedGateAssist:
			fmt.Fprintf(outputFor(out), "Starting Failed assistance: %s\n", invocation.Display)
			exitCode, err := runAttendedAssistanceCommand(d, in, runtimePath, out, invocation)
			if err != nil {
				fmt.Fprintf(outputFor(out), "Could not start Failed assistance: %v\n", err)
				continue
			}
			if exitCode != 0 {
				fmt.Fprintf(outputFor(out), "Failed assistance exited with status %d; refreshing Task set.\n", exitCode)
			}
			afterRefresh, err := RefreshWith(d, definitionPath, statePath)
			if err != nil {
				return true, exitErr(ExitOperational, "refresh after Failed assistance: %v", err)
			}
			afterManifest := afterRefresh.Manifests[taskSetID]
			// The assist agent does not change task state on its own, so the task
			// is still failed: refresh, then re-show the Failed gate. If the human
			// did override state during the session, fall through to normal
			// draining.
			if FailedTask(afterManifest) == nil {
				return true, nil
			}
			m = afterManifest
			failed = FailedTask(m)
			prompt = BuildFailedAssistancePrompt(d, taskSetID, m, *failed, runtimePath)
			body = gateTaskBody(d, m, failed)
			invocation, err = ResolveAgentAssistanceInvocation(agentPreset, agentCmd, prompt, runtimePath)
			if err != nil {
				return true, exitErr(ExitSetup, "%v", err)
			}
		case failedGateComplete:
			result, err := CompleteTaskWith(d, nil, nil, CompleteTaskOptions{ResolveInput: ResolveInput{CWD: cwd}, TaskPath: taskPathHint(taskSetID, failed.File)})
			if err != nil {
				return true, err
			}
			RenderTaskComplete(out, result.TaskSetID, result.TaskID)
			return true, nil
		case failedGateExit:
			return false, nil
		}
	}
}

func promptFailedGateAction(out io.Writer, reader *bufio.Reader, taskSetID string, failed *Task, body string, invocation *AgentAssistanceInvocation) (failedGateAction, error) {
	display := outputFor(out)
	fmt.Fprintln(display)
	display.line(ansiRed, "Failed: %s/%s failed before the set could continue.", taskSetID, failed.ID)
	renderGateTaskBody(display, failed.File, body)
	fmt.Fprintln(display, "  1. Re-run (default)")
	fmt.Fprintln(display, "  2. Agent assistance")
	if invocation != nil {
		fmt.Fprintf(display, "     %s\n", invocation.Display)
		if invocation.Detail != "" {
			fmt.Fprintf(display, "     %s\n", invocation.Detail)
		}
	}
	fmt.Fprintln(display, "  3. Finish by hand")
	fmt.Fprintln(display, "  4. Exit")
	fmt.Fprintf(display, "%s", display.styled(ansiCyan, "Choose [1]: "))

	answer, err := readPromptLine(reader, "4")
	if err != nil {
		return failedGateExit, err
	}
	switch strings.ToLower(strings.TrimSpace(answer)) {
	case "", "1":
		return failedGateRerun, nil
	case "2":
		return failedGateAssist, nil
	case "3":
		return failedGateComplete, nil
	case "4", "q", "quit", "exit":
		return failedGateExit, nil
	default:
		fmt.Fprintln(display, "Choose 1, 2, 3, or 4.")
		return promptFailedGateAction(out, reader, taskSetID, failed, body, invocation)
	}
}
