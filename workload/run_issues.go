package workload

import (
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/glebglazov/pop/config"
	"github.com/glebglazov/pop/project"
)

const issueSetConfirmPrompt = "Run Issue set? [y/N]: "

// RunIssueSetOptions configures sequential Issue-set draining.
type RunIssueSetOptions struct {
	ResolveInput
	IssueSetOverride string
	AgentPreset      string
	AgentCmd         string
	AgentOutput      AgentOutputMode
	AllowDirty       DirtyRuntimeStrategy
	MaxTries         int
	Timeout          time.Duration
	Yes              bool
	ConfirmIn        io.Reader
	ConfirmOut       io.Writer
	Output           io.Writer
}

// RunIssueSetResult is the outcome of a run-issues invocation.
type RunIssueSetResult struct {
	IssueSetID       string
	Completed        []*RunIssueResult
	Refresh          *RefreshResult
	Declined         bool
	IssueSetDone     bool
	IssueSetDeferred bool
	SkippedIssues    []string
	BlockedReason    string
	QuotaPaused      bool
	PauseReason      string
}

// RunIssueSet drains one Issue set sequentially through eligible AFK issues.
func RunIssueSet(opts RunIssueSetOptions) (*RunIssueSetResult, error) {
	return RunIssueSetWith(defaultDeps, project.DefaultDeps(), config.Load, opts)
}

// RunIssueSetWith drains one Issue set using injected dependencies.
func RunIssueSetWith(d *Deps, pd *project.Deps, loadConfig func(string) (*config.Config, error), opts RunIssueSetOptions) (*RunIssueSetResult, error) {
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

	statePath := DefaultStatePathWith(d)
	refresh, err := RefreshWith(d, resolved.DefinitionPath, statePath)
	if err != nil {
		return nil, exitErr(ExitSetup, "%v", err)
	}

	issueSetOverride, err := ResolveIssueSetTarget(d, refresh, opts.CWD, opts.IssueSetOverride)
	if err != nil {
		return nil, err
	}

	issueSetID, err := SelectIssueSet(refresh, issueSetOverride)
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
	MarkRunTarget(displayRows, issueSetID)
	displayRefresh := *refresh
	displayRefresh.Rows = displayRows

	fmt.Fprintln(out)
	Render(out, &displayRefresh)

	if m := displayRefresh.Manifests[issueSetID]; m != nil {
		RenderIssueList(out, issueSetID, m)
	}

	if dirty {
		if err := reportDirtyRuntime(d, confirmOut, runtimePath, strategy); err != nil {
			return nil, exitErr(ExitSetup, "runtime git status: %v", err)
		}
	}

	confirmed, err := confirmExecution(opts.ConfirmIn, confirmOut, opts.Yes, issueSetConfirmPrompt)
	if err != nil {
		return nil, err
	}
	if !confirmed {
		return &RunIssueSetResult{IssueSetID: issueSetID, Refresh: refresh, Declined: true}, nil
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

	result := &RunIssueSetResult{IssueSetID: issueSetID}
	dirtyStrategyApplied := false

	for {
		currentRefresh, err := RefreshWith(d, resolved.DefinitionPath, statePath)
		if err != nil {
			return nil, exitErr(ExitOperational, "refresh before issue selection: %v", err)
		}

		sel, selErr := SelectIssueInSet(currentRefresh, issueSetID)
		if selErr != nil {
			row := findRow(currentRefresh, issueSetID)
			if row == nil {
				return nil, selErr
			}
			result.Refresh = currentRefresh
			switch row.Status {
			case StatusDone:
				result.IssueSetDone = true
				finishRunIssueSet(out, opts.Yes, result)
				return result, nil
			case StatusDeferred:
				result.IssueSetDeferred = true
				result.SkippedIssues = SkippedIssueIDs(currentRefresh.Manifests[issueSetID])
				finishRunIssueSet(out, opts.Yes, result)
				return result, nil
			case StatusBlocked:
				result.BlockedReason = row.BlockedReason
				if !opts.Yes {
					fmt.Fprintln(out)
					Render(out, currentRefresh)
				} else {
					printIssueSetSummary(out, result)
				}
				if hitl := BlockingHITLIssue(currentRefresh.Manifests[issueSetID]); hitl != nil {
					printHITLGateAdvice(d, out, issueSetID, currentRefresh.Manifests[issueSetID].Dir, hitl)
				}
				if result.BlockedReason != "" {
					return nil, exitErr(ExitNoRunnable, "Issue set %q blocked: %s", issueSetID, result.BlockedReason)
				}
				return nil, exitErr(ExitNoRunnable, "Issue set %q has no eligible AFK issue", issueSetID)
			case StatusFailed:
				if !opts.Yes {
					fmt.Fprintln(out)
					Render(out, currentRefresh)
				}
				printFailedStopAdvice(out, issueSetID, currentRefresh.Manifests[issueSetID])
				return nil, exitErr(ExitOperational, "Issue set %q has failed issues", issueSetID)
			default:
				return nil, selErr
			}
		}

		if dirty && !dirtyStrategyApplied {
			if err := applyDirtyRuntimeStrategy(d, runtimePath, sel.IssueSetID, sel.IssueID, strategy, confirmOut); err != nil {
				return nil, issueExitErr(sel, ExitOperational, "dirty-runtime strategy: %v", err)
			}
			dirtyStrategyApplied = true
		}

		prompt := BuildAgentPrompt(sel.IssuePath, runtimePath)
		invocation, err := ResolveAgentInvocationWithMode(opts.AgentPreset, opts.AgentCmd, prompt, runtimePath, agentOutput)
		if err != nil {
			return nil, issueExitErr(sel, ExitSetup, "%v", err)
		}

		issueResult, execErr := executeIssueAttempts(d, sel, runtimePath, out, invocation, maxTries, timeout)
		if execErr != nil {
			afterRefresh, refreshErr := RefreshWith(d, resolved.DefinitionPath, statePath)
			if refreshErr == nil {
				result.Refresh = afterRefresh
				if !opts.Yes {
					fmt.Fprintln(out)
					Render(out, afterRefresh)
				}
				printFailedStopAdvice(out, issueSetID, afterRefresh.Manifests[issueSetID])
			}
			return result, execErr
		}
		if issueResult.QuotaPaused {
			result.QuotaPaused = true
			result.PauseReason = issueResult.PauseReason
			result.Refresh = currentRefresh
			printIssueSetSummary(out, result)
			return result, nil
		}

		result.Completed = append(result.Completed, issueResult)
	}
}

func finishRunIssueSet(out io.Writer, yes bool, result *RunIssueSetResult) {
	if yes {
		printIssueSetSummary(out, result)
		return
	}
	fmt.Fprintln(out)
	Render(out, result.Refresh)
	if result.IssueSetDeferred {
		fmt.Fprintln(out, deferralMessage(result))
	}
}

func deferralMessage(result *RunIssueSetResult) string {
	if len(result.SkippedIssues) > 0 {
		return fmt.Sprintf("Issue set %s deferred: skipped %s", result.IssueSetID, strings.Join(result.SkippedIssues, ", "))
	}
	return fmt.Sprintf("Issue set %s deferred", result.IssueSetID)
}

func printIssueSetSummary(w io.Writer, result *RunIssueSetResult) {
	out := outputFor(w)
	if result.IssueSetDone {
		out.line(ansiGreen, "✓ Completed Issue set %s (%d issue(s))", result.IssueSetID, len(result.Completed))
		return
	}
	if result.IssueSetDeferred {
		out.line(ansiYellow, "%s", deferralMessage(result))
		return
	}
	if result.BlockedReason != "" {
		out.line(ansiYellow, "Issue set %s blocked: %s", result.IssueSetID, result.BlockedReason)
		return
	}
	if result.QuotaPaused {
		out.line(ansiYellow, "Issue set %s paused after %d completed issue(s): agent quota exhausted", result.IssueSetID, len(result.Completed))
		return
	}
	fmt.Fprintf(out, "Issue set %s stopped after %d issue(s)\n", result.IssueSetID, len(result.Completed))
}
