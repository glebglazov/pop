package tasks

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/glebglazov/pop/config"
	"github.com/glebglazov/pop/project"
)

const taskSetConfirmPrompt = "Run AFK tasks in this Task set? [y/N]: "

// RunTaskSetOptions configures sequential Task-set draining.
type RunTaskSetOptions struct {
	ResolveInput
	TaskSetOverride string
	AgentPreset     string
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
}

// RunTaskSet drains one Task set sequentially through eligible AFK tasks.
func RunTaskSet(opts RunTaskSetOptions) (*RunTaskSetResult, error) {
	return RunTaskSetWith(defaultDeps, project.DefaultDeps(), config.Load, opts)
}

// RunTaskSetWith drains one Task set using injected dependencies.
func RunTaskSetWith(d *Deps, pd *project.Deps, loadConfig func(string) (*config.Config, error), opts RunTaskSetOptions) (*RunTaskSetResult, error) {
	if d.Runner == nil {
		d.Runner = RealCommandRunner{}
	}
	agentOutput := AgentOutputAuto
	if opts.AgentCmd == "" {
		var err error
		agentOutput, err = resolveAgentOutputMode(loadConfig, opts.AgentPreset, opts.AgentOutput)
		if err != nil {
			return nil, exitErr(ExitSetup, "%v", err)
		}
	}
	if err := validateDirtyRuntimeStrategy(opts.AllowDirty); err != nil {
		return nil, exitErr(ExitSetup, "%v", err)
	}
	strategy := resolveDirtyRuntimeStrategy(opts.AllowDirty)

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

	taskSetID, hitlFallback, err := SelectTaskSet(refresh, taskSetOverride)
	if err != nil {
		return nil, err
	}

	dirty, err := runtimeIsDirty(d, runtimePath)
	if err != nil {
		return nil, exitErr(ExitSetup, "runtime git status: %v", err)
	}

	confirmOut := opts.ConfirmOut
	if confirmOut == nil {
		confirmOut = os.Stderr
	}
	out := opts.Output
	if out == nil {
		out = os.Stdout
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

	initialHITLGate := selectedTaskSetStartsAtHITLGate(refresh, taskSetID)
	var sharedPromptReader *bufio.Reader
	if initialHITLGate && !opts.Yes && canPrompt(opts.ConfirmIn) {
		promptIn := opts.ConfirmIn
		if promptIn == nil {
			promptIn = os.Stdin
		}
		sharedPromptReader = bufio.NewReader(promptIn)
	}

	afkConsentConfirmed := opts.Yes
	if !initialHITLGate {
		confirmed, err := confirmExecution(opts.ConfirmIn, confirmOut, opts.Yes, taskSetConfirmPrompt)
		if err != nil {
			return nil, err
		}
		if !confirmed {
			return &RunTaskSetResult{TaskSetID: taskSetID, Refresh: refresh, Declined: true}, nil
		}
		afkConsentConfirmed = true
	}

	lock, err := AcquireRuntimeLock(d, runtimePath, confirmOut)
	if err != nil {
		return nil, err
	}
	defer lock.Release()

	maxTries := opts.MaxTries
	if maxTries <= 0 {
		maxTries = DefaultMaxTries
	}
	timeout := opts.Timeout
	if timeout <= 0 {
		timeout = DefaultAttemptTimeout
	}

	result := &RunTaskSetResult{TaskSetID: taskSetID}
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
				printFailedStopAdvice(out, taskSetID, currentRefresh.Manifests[taskSetID])
				return nil, exitErr(ExitOperational, "Task set %q has failed tasks", taskSetID)
			default:
				return nil, selErr
			}
		}

		if !afkConsentConfirmed {
			confirmIn := opts.ConfirmIn
			if sharedPromptReader != nil {
				confirmIn = sharedPromptReader
			}
			confirmed, err := confirmExecution(confirmIn, confirmOut, opts.Yes, taskSetConfirmPrompt)
			if err != nil {
				return nil, err
			}
			if !confirmed {
				result.Refresh = currentRefresh
				result.Declined = true
				return result, nil
			}
			afkConsentConfirmed = true
		}

		if dirty && !dirtyStrategyApplied {
			if err := applyDirtyRuntimeStrategy(d, runtimePath, sel.TaskSetID, sel.TaskID, strategy, confirmOut); err != nil {
				return nil, taskExitErr(sel, ExitOperational, "dirty-runtime strategy: %v", err)
			}
			dirtyStrategyApplied = true
		}

		agentSpec := resolveTaskAgentSpec(opts.AgentPreset, opts.AgentExplicit, opts.AgentCmd, sel.Task.Agent)
		attemptOutput := agentOutput
		if agentSpec != opts.AgentPreset {
			attemptOutput, err = resolveAgentOutputMode(loadConfig, agentSpec, opts.AgentOutput)
			if err != nil {
				return nil, taskExitErr(sel, ExitSetup, "%v", err)
			}
		}

		prompt := BuildAgentPrompt(sel.TaskPath, runtimePath)
		invocation, err := ResolveAgentInvocationWithMode(agentSpec, opts.AgentCmd, prompt, runtimePath, attemptOutput)
		if err != nil {
			return nil, taskExitErr(sel, ExitSetup, "%v", err)
		}

		taskResult, execErr := executeTaskAttempts(d, sel, runtimePath, out, confirmOut, invocation, maxTries, timeout)
		if execErr != nil {
			afterRefresh, refreshErr := RefreshWith(d, resolved.DefinitionPath, statePath)
			if refreshErr == nil {
				result.Refresh = afterRefresh
				if !opts.Yes {
					fmt.Fprintln(out)
					Render(out, afterRefresh)
				}
				printFailedStopAdvice(out, taskSetID, afterRefresh.Manifests[taskSetID])
			}
			return result, execErr
		}
		if taskResult.QuotaPaused {
			result.QuotaPaused = true
			result.PauseReason = taskResult.PauseReason
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
	if reader == nil {
		if in == nil {
			in = os.Stdin
		}
		reader = bufio.NewReader(in)
	}

	prompt := BuildHITLAssistancePrompt(d, taskSetID, m, *hitl, runtimePath)
	invocation, err := ResolveAgentAssistanceInvocation(agentPreset, agentCmd, prompt, runtimePath)
	if err != nil {
		return false, exitErr(ExitSetup, "%v", err)
	}

	for {
		action, err := promptHITLGateAction(out, reader, taskSetID, hitl, invocation)
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
			exitCode, err := runHITLAssistanceCommand(d, reader, runtimePath, out, invocation)
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

func runHITLAssistanceCommand(d *Deps, stdin io.Reader, runtimePath string, out io.Writer, invocation *AgentAssistanceInvocation) (int, error) {
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

func promptHITLGateAction(out io.Writer, reader *bufio.Reader, taskSetID string, hitl *Task, invocation *AgentAssistanceInvocation) (hitlGateAction, error) {
	display := outputFor(out)
	fmt.Fprintln(display)
	display.line(ansiYellow, "Human-blocked: %s/%s needs human work before the set can continue.", taskSetID, hitl.ID)
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

	answer, err := readPromptLine(reader)
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
		return promptHITLGateAction(out, reader, taskSetID, hitl, invocation)
	}
}

func readPromptLine(reader *bufio.Reader) (string, error) {
	answer, err := reader.ReadString('\n')
	if err != nil && err != io.EOF {
		return "", exitErr(ExitOperational, "read HITL gate selection: %v", err)
	}
	if err == io.EOF && answer == "" {
		return "4", nil
	}
	return strings.TrimRight(answer, "\r\n"), nil
}
