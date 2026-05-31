package workload

import (
	"fmt"
	"io"
	"os"
	"time"

	"github.com/glebglazov/pop/config"
	"github.com/glebglazov/pop/project"
)

const prdConfirmPrompt = "Run PRD? [y/N]: "

// RunPRDOptions configures sequential PRD draining.
type RunPRDOptions struct {
	ResolveInput
	PRDOverride string
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

// RunPRDResult is the outcome of a run-prd invocation.
type RunPRDResult struct {
	PRDID         string
	Completed     []*RunIssueResult
	Refresh       *RefreshResult
	Declined      bool
	PRDDone       bool
	BlockedReason string
}

// RunPRD drains one PRD sequentially through eligible AFK issues.
func RunPRD(opts RunPRDOptions) (*RunPRDResult, error) {
	return RunPRDWith(defaultDeps, project.DefaultDeps(), config.Load, opts)
}

// RunPRDWith drains one PRD using injected dependencies.
func RunPRDWith(d *Deps, pd *project.Deps, loadConfig func(string) (*config.Config, error), opts RunPRDOptions) (*RunPRDResult, error) {
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

	prdID, err := SelectPRD(refresh, opts.PRDOverride)
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
	MarkRunTarget(displayRows, prdID)
	displayRefresh := *refresh
	displayRefresh.Rows = displayRows

	fmt.Fprintln(out)
	Render(out, &displayRefresh)

	confirmed, err := confirmExecution(opts.ConfirmIn, confirmOut, opts.Yes, prdConfirmPrompt)
	if err != nil {
		return nil, err
	}
	if !confirmed {
		return &RunPRDResult{PRDID: prdID, Refresh: refresh, Declined: true}, nil
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

	result := &RunPRDResult{PRDID: prdID}
	dirtyCheckpointed := false

	for {
		currentRefresh, err := RefreshWith(d, resolved.DefinitionPath, statePath)
		if err != nil {
			return nil, exitErr(ExitOperational, "refresh before issue selection: %v", err)
		}

		sel, selErr := SelectIssueInPRD(currentRefresh, prdID)
		if selErr != nil {
			row := findRow(currentRefresh, prdID)
			if row == nil {
				return nil, selErr
			}
			result.Refresh = currentRefresh
			switch row.Status {
			case StatusDone:
				result.PRDDone = true
				finishRunPRD(out, opts.Yes, result)
				return result, nil
			case StatusBlocked:
				result.BlockedReason = row.BlockedReason
				if !opts.Yes {
					fmt.Fprintln(out)
					Render(out, currentRefresh)
				} else {
					printPRDSummary(out, result)
				}
				if result.BlockedReason != "" {
					return nil, exitErr(ExitNoRunnable, "PRD %q blocked: %s", prdID, result.BlockedReason)
				}
				return nil, exitErr(ExitNoRunnable, "PRD %q has no eligible AFK issue", prdID)
			case StatusFailed:
				if !opts.Yes {
					fmt.Fprintln(out)
					Render(out, currentRefresh)
				}
				return nil, exitErr(ExitOperational, "PRD %q has failed issues", prdID)
			default:
				return nil, selErr
			}
		}

		if dirty && opts.AllowDirty && !dirtyCheckpointed {
			if err := checkpointDirtyRuntime(d, runtimePath, sel.PRDID, sel.IssueID); err != nil {
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

func finishRunPRD(out io.Writer, yes bool, result *RunPRDResult) {
	if yes {
		printPRDSummary(out, result)
		return
	}
	fmt.Fprintln(out)
	Render(out, result.Refresh)
}

func printPRDSummary(w io.Writer, result *RunPRDResult) {
	if result.PRDDone {
		fmt.Fprintf(w, "Completed PRD %s (%d issue(s))\n", result.PRDID, len(result.Completed))
		return
	}
	if result.BlockedReason != "" {
		fmt.Fprintf(w, "PRD %s blocked: %s\n", result.PRDID, result.BlockedReason)
		return
	}
	fmt.Fprintf(w, "PRD %s stopped after %d issue(s)\n", result.PRDID, len(result.Completed))
}
