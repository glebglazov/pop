package tasks

import (
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/glebglazov/pop/config"
	"github.com/glebglazov/pop/project"
)

const taskSetConfirmPrompt = "Run Task set? [y/N]: "

// RunTaskSetOptions configures sequential Task-set draining.
type RunTaskSetOptions struct {
	ResolveInput
	TaskSetOverride string
	AgentPreset     string
	AgentCmd        string
	AgentOutput     AgentOutputMode
	AllowDirty      DirtyRuntimeStrategy
	MaxTries        int
	Timeout         time.Duration
	Yes             bool
	ConfirmIn       io.Reader
	ConfirmOut      io.Writer
	Output          io.Writer
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

	taskSetID, err := SelectTaskSet(refresh, taskSetOverride)
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

	confirmed, err := confirmExecution(opts.ConfirmIn, confirmOut, opts.Yes, taskSetConfirmPrompt)
	if err != nil {
		return nil, err
	}
	if !confirmed {
		return &RunTaskSetResult{TaskSetID: taskSetID, Refresh: refresh, Declined: true}, nil
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

		if dirty && !dirtyStrategyApplied {
			if err := applyDirtyRuntimeStrategy(d, runtimePath, sel.TaskSetID, sel.TaskID, strategy, confirmOut); err != nil {
				return nil, taskExitErr(sel, ExitOperational, "dirty-runtime strategy: %v", err)
			}
			dirtyStrategyApplied = true
		}

		prompt := BuildAgentPrompt(sel.TaskPath, runtimePath)
		invocation, err := ResolveAgentInvocationWithMode(opts.AgentPreset, opts.AgentCmd, prompt, runtimePath, agentOutput)
		if err != nil {
			return nil, taskExitErr(sel, ExitSetup, "%v", err)
		}

		taskResult, execErr := executeTaskAttempts(d, sel, runtimePath, out, invocation, maxTries, timeout)
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
