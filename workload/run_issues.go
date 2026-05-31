package workload

import (
	"fmt"
	"io"
	"os"
	"time"

	"github.com/glebglazov/pop/config"
	"github.com/glebglazov/pop/project"
)

const issueSetConfirmPrompt = "Run Issue set? [y/N]: "

// RunIssueSetOptions configures sequential Issue-set draining.
type RunIssueSetOptions struct {
	ResolveInput
	IssueSetOverride string
	AgentPreset   string
	AgentCmd      string
	AllowDirty    bool
	MaxTries      int
	Timeout       time.Duration
	Yes           bool
	ConfirmIn     io.Reader
	ConfirmOut    io.Writer
	Output        io.Writer
}

// RunIssueSetResult is the outcome of a run-issues invocation.
type RunIssueSetResult struct {
	IssueSetID         string
	Completed     []*RunIssueResult
	Refresh       *RefreshResult
	Declined      bool
	IssueSetDone       bool
	BlockedReason string
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

	issueSetID, err := SelectIssueSet(refresh, opts.IssueSetOverride)
	if err != nil {
		return nil, err
	}

	dirty, err := runtimeIsDirty(d, runtimePath)
	if err != nil {
		return nil, exitErr(ExitSetup, "runtime git status: %v", err)
	}
	if dirty && !opts.AllowDirty {
		return nil, exitErr(ExitOperational, "runtime checkout is dirty; commit or stash changes before execution")
	}

	confirmOut := opts.ConfirmOut
	if confirmOut == nil {
		confirmOut = os.Stderr
	}
	out := opts.Output
	if out == nil {
		out = os.Stdout
	}

	if dirty && opts.AllowDirty {
		fmt.Fprintln(confirmOut, "Warning: runtime checkout has uncommitted changes; a capturing dirty state checkpoint commit will be created before execution.")
	}

	displayRows := cloneRows(refresh.Rows)
	MarkAutoPick(displayRows)
	MarkRunTarget(displayRows, issueSetID)
	displayRefresh := *refresh
	displayRefresh.Rows = displayRows

	fmt.Fprintln(out)
	Render(out, &displayRefresh)

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
	dirtyCheckpointed := false

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
			case StatusBlocked:
				result.BlockedReason = row.BlockedReason
				if !opts.Yes {
					fmt.Fprintln(out)
					Render(out, currentRefresh)
				} else {
					printIssueSetSummary(out, result)
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
				return nil, exitErr(ExitOperational, "Issue set %q has failed issues", issueSetID)
			default:
				return nil, selErr
			}
		}

		if dirty && opts.AllowDirty && !dirtyCheckpointed {
			if err := checkpointDirtyRuntime(d, runtimePath, sel.IssueSetID, sel.IssueID); err != nil {
				return nil, exitErr(ExitOperational, "dirty-state checkpoint: %v", err)
			}
			dirtyCheckpointed = true
		}

		prompt := BuildAgentPrompt(sel.IssuePath, runtimePath)
		name, args, err := ResolveAgentCommand(opts.AgentPreset, opts.AgentCmd, prompt, runtimePath)
		if err != nil {
			return nil, exitErr(ExitSetup, "%v", err)
		}

		issueResult, execErr := executeIssueAttempts(d, sel, runtimePath, out, name, args, maxTries, timeout)
		if execErr != nil {
			afterRefresh, refreshErr := RefreshWith(d, resolved.DefinitionPath, statePath)
			if refreshErr == nil {
				result.Refresh = afterRefresh
				if !opts.Yes {
					fmt.Fprintln(out)
					Render(out, afterRefresh)
				}
			}
			return result, execErr
		}

		result.Completed = append(result.Completed, issueResult)
		if opts.Yes {
			printConciseSummary(out, issueResult)
		}
	}
}

func finishRunIssueSet(out io.Writer, yes bool, result *RunIssueSetResult) {
	if yes {
		printIssueSetSummary(out, result)
		return
	}
	fmt.Fprintln(out)
	Render(out, result.Refresh)
}

func printIssueSetSummary(w io.Writer, result *RunIssueSetResult) {
	if result.IssueSetDone {
		fmt.Fprintf(w, "Completed Issue set %s (%d issue(s))\n", result.IssueSetID, len(result.Completed))
		return
	}
	if result.BlockedReason != "" {
		fmt.Fprintf(w, "Issue set %s blocked: %s\n", result.IssueSetID, result.BlockedReason)
		return
	}
	fmt.Fprintf(w, "Issue set %s stopped after %d issue(s)\n", result.IssueSetID, len(result.Completed))
}
