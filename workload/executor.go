package workload

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/glebglazov/pop/config"
	"github.com/glebglazov/pop/project"
)

// RunIssueOptions configures a single-issue execution.
type RunIssueOptions struct {
	ResolveInput
	PRDOverride   string
	IssueOverride string
	AgentPreset   string
	AgentCmd      string
	Yes           bool
	ConfirmIn     io.Reader
	ConfirmOut    io.Writer
	Output        io.Writer
}

// RunIssueResult is the outcome of a successful or declined run-issue.
type RunIssueResult struct {
	Selection  *Selection
	Refresh    *RefreshResult
	Declined   bool
	NoOp       bool
	CommitSHA  string
	AgentSummary string
}

// RunIssue executes one workload issue through an agent.
func RunIssue(opts RunIssueOptions) (*RunIssueResult, error) {
	return RunIssueWith(defaultDeps, project.DefaultDeps(), config.Load, opts)
}

// RunIssueWith executes one issue using injected dependencies.
func RunIssueWith(d *Deps, pd *project.Deps, loadConfig func(string) (*config.Config, error), opts RunIssueOptions) (*RunIssueResult, error) {
	if d.Runner == nil {
		d.Runner = RealCommandRunner{}
	}

	resolved, err := ResolvePathsWith(d, pd, loadConfig, opts.ResolveInput)
	if err != nil {
		return nil, exitErr(ExitSetup, "%v", err)
	}

	runtimePath, err := NormalizeProjectPathWith(d, resolved.ProjectPath)
	if err != nil {
		return nil, exitErr(ExitSetup, "%v", err)
	}

	statePath := DefaultStatePathWith(d)
	refresh, err := RefreshWith(d, resolved.DefinitionPath, statePath)
	if err != nil {
		return nil, exitErr(ExitSetup, "%v", err)
	}

	sel, err := SelectIssue(refresh, opts.PRDOverride, opts.IssueOverride)
	if err != nil {
		return nil, err
	}

	if err := requireCleanRuntime(d, runtimePath); err != nil {
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

	displayRows := cloneRows(refresh.Rows)
	MarkAutoPick(displayRows)
	MarkRunTarget(displayRows, sel.PRDID)
	displayRefresh := *refresh
	displayRefresh.Rows = displayRows

	fmt.Fprintln(out)
	Render(out, &displayRefresh)

	confirmed, err := confirmExecution(opts.ConfirmIn, confirmOut, opts.Yes)
	if err != nil {
		return nil, err
	}
	if !confirmed {
		return &RunIssueResult{Selection: sel, Refresh: refresh, Declined: true}, nil
	}

	prompt := BuildAgentPrompt(sel.IssuePath, sel.PRDPath, runtimePath)
	name, args, err := ResolveAgentCommand(opts.AgentPreset, opts.AgentCmd, prompt, runtimePath)
	if err != nil {
		return nil, exitErr(ExitSetup, "%v", err)
	}

	agentOut, exitCode, runErr := runAgent(d, runtimePath, out, name, args)
	if runErr != nil {
		return nil, exitErr(ExitOperational, "agent execution: %v", runErr)
	}
	if exitCode != 0 {
		return nil, exitErr(ExitOperational, "agent exited with status %d", exitCode)
	}

	issueData, err := d.FS.ReadFile(sel.IssuePath)
	if err != nil {
		return nil, exitErr(ExitOperational, "read issue markdown: %v", err)
	}

	assessment := AssessCompletion(agentOut, issueData)
	if !assessment.Complete {
		reason := assessment.FailedReason
		if reason == "" {
			reason = "agent output did not satisfy completion contract"
		}
		return nil, exitErr(ExitOperational, "%s", reason)
	}

	hasChanges, err := runtimeHasChanges(d, runtimePath)
	if err != nil {
		return nil, exitErr(ExitOperational, "check runtime changes: %v", err)
	}

	result := &RunIssueResult{
		Selection:    sel,
		Refresh:      refresh,
		AgentSummary: assessment.Summary,
	}

	if hasChanges {
		sha, err := createImplementationCommit(d, runtimePath, sel.PRDID, sel.IssueID, assessment.Summary)
		if err != nil {
			return nil, exitErr(ExitOperational, "implementation commit: %v", err)
		}
		result.CommitSHA = sha
	} else {
		result.NoOp = true
	}

	if err := finalizeIssueDone(d, sel, assessment.Summary); err != nil {
		return nil, exitErr(ExitOperational, "local bookkeeping: %v", err)
	}

	afterRefresh, err := RefreshWith(d, resolved.DefinitionPath, statePath)
	if err != nil {
		return nil, exitErr(ExitOperational, "refresh after completion: %v", err)
	}
	result.Refresh = afterRefresh

	if opts.Yes {
		printConciseSummary(out, result)
	} else {
		fmt.Fprintln(out)
		Render(out, afterRefresh)
	}

	return result, nil
}

func cloneRows(rows []Row) []Row {
	out := make([]Row, len(rows))
	copy(out, rows)
	return out
}

// NonInteractiveReader marks explicit non-interactive confirmation input (for tests and automation).
type NonInteractiveReader struct{}

func (NonInteractiveReader) Read([]byte) (int, error) { return 0, io.EOF }

func confirmExecution(in io.Reader, out io.Writer, yes bool) (bool, error) {
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
	fmt.Fprintf(out, "Run issue? [y/N]: ")
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

func requireCleanRuntime(d *Deps, runtimePath string) error {
	out, err := d.Git.CommandInDir(runtimePath, "status", "--porcelain")
	if err != nil {
		return exitErr(ExitSetup, "runtime git status: %v", err)
	}
	if strings.TrimSpace(out) != "" {
		return exitErr(ExitOperational, "runtime checkout is dirty; commit or stash changes before execution")
	}
	return nil
}

func runAgent(d *Deps, runtimePath string, liveOut io.Writer, name string, args []string) (string, int, error) {
	var capture bytes.Buffer
	mw := io.MultiWriter(liveOut, &capture)
	ctx := context.Background()
	exitCode, err := d.Runner.Run(ctx, runtimePath, mw, mw, name, args...)
	return capture.String(), exitCode, err
}

func runtimeHasChanges(d *Deps, runtimePath string) (bool, error) {
	out, err := d.Git.CommandInDir(runtimePath, "status", "--porcelain")
	if err != nil {
		return false, err
	}
	return strings.TrimSpace(out) != "", nil
}

func createImplementationCommit(d *Deps, runtimePath, prdID, issueID, summary string) (string, error) {
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
	subject := CommitSubject(prdID, issueID)
	if _, err := d.Git.CommandInDir(runtimePath, "commit", "-m", subject, "-m", summary); err != nil {
		return "", err
	}
	sha, err := d.Git.CommandInDir(runtimePath, "rev-parse", "HEAD")
	if err != nil {
		return "", err
	}
	return sha, nil
}

func finalizeIssueDone(d *Deps, sel *Selection, summary string) error {
	if err := AppendProgress(d, sel.Manifest.Dir, sel.IssueFile, "DONE", summary); err != nil {
		return err
	}
	sel.Manifest.Issues[sel.IssueIndex].Status = "done"
	return WriteManifestAtomic(d, sel.Manifest)
}

func printConciseSummary(w io.Writer, result *RunIssueResult) {
	fmt.Fprintf(w, "Completed %s/%s\n", result.Selection.PRDID, result.Selection.IssueID)
	if result.NoOp {
		fmt.Fprintln(w, "No implementation commit (verified no-op)")
	} else if result.CommitSHA != "" {
		fmt.Fprintf(w, "Implementation commit: %s\n", result.CommitSHA[:min(12, len(result.CommitSHA))])
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
