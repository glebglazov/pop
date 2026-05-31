package workload

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
	DefaultAttemptTimeout = 30 * time.Minute

	DirtyRuntimeReject            DirtyRuntimeStrategy = ""
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

// RunIssueOptions configures a single-issue execution.
type RunIssueOptions struct {
	ResolveInput
	IssuePathOverride string
	AgentPreset       string
	AgentCmd          string
	AllowDirty        DirtyRuntimeStrategy
	MaxTries          int
	Timeout           time.Duration
	Yes               bool
	ConfirmIn         io.Reader
	ConfirmOut        io.Writer
	Output            io.Writer
}

// RunIssueResult is the outcome of a successful or declined run-issue.
type RunIssueResult struct {
	Selection    *Selection
	Refresh      *RefreshResult
	Declined     bool
	NoOp         bool
	CommitSHA    string
	AgentSummary string
}

type attemptOutcome struct {
	output      string
	exitCode    int
	timedOut    bool
	interrupted bool
	runErr      error
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
	if err := validateDirtyRuntimeStrategy(opts.AllowDirty); err != nil {
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

	statePath := DefaultStatePathWith(d)
	refresh, err := RefreshWith(d, resolved.DefinitionPath, statePath)
	if err != nil {
		return nil, exitErr(ExitSetup, "%v", err)
	}

	issueSetID, issueID, err := ResolveIssueTarget(d, refresh, opts.CWD, opts.IssuePathOverride)
	if err != nil {
		return nil, err
	}

	sel, err := SelectIssue(refresh, issueSetID, issueID)
	if err != nil {
		return nil, err
	}

	dirty, err := runtimeIsDirty(d, runtimePath)
	if err != nil {
		return nil, exitErr(ExitSetup, "runtime git status: %v", err)
	}
	if dirty && opts.AllowDirty == DirtyRuntimeReject {
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

	if dirty {
		warnDirtyRuntimeStrategy(confirmOut, opts.AllowDirty)
	}

	displayRows := cloneRows(refresh.Rows)
	MarkAutoPick(displayRows)
	MarkRunTarget(displayRows, sel.IssueSetID)
	displayRefresh := *refresh
	displayRefresh.Rows = displayRows

	fmt.Fprintln(out)
	Render(out, &displayRefresh)

	confirmed, err := confirmExecution(opts.ConfirmIn, confirmOut, opts.Yes, issueConfirmPrompt)
	if err != nil {
		return nil, err
	}
	if !confirmed {
		return &RunIssueResult{Selection: sel, Refresh: refresh, Declined: true}, nil
	}

	lock, err := AcquireRuntimeLock(d, runtimePath, confirmOut)
	if err != nil {
		return nil, err
	}
	defer lock.Release()

	if dirty {
		if err := applyDirtyRuntimeStrategy(d, runtimePath, sel.IssueSetID, sel.IssueID, opts.AllowDirty, confirmOut); err != nil {
			return nil, exitErr(ExitOperational, "dirty-runtime strategy: %v", err)
		}
	}

	prompt := BuildAgentPrompt(sel.IssuePath, runtimePath)
	name, args, err := ResolveAgentCommand(opts.AgentPreset, opts.AgentCmd, prompt, runtimePath)
	if err != nil {
		return nil, exitErr(ExitSetup, "%v", err)
	}

	maxTries := opts.MaxTries
	if maxTries <= 0 {
		maxTries = DefaultMaxTries
	}
	timeout := opts.Timeout
	if timeout <= 0 {
		timeout = DefaultAttemptTimeout
	}

	result, execErr := executeIssueAttempts(d, sel, runtimePath, out, name, args, maxTries, timeout)
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

func executeIssueAttempts(d *Deps, sel *Selection, runtimePath string, out io.Writer, name string, args []string, maxTries int, timeout time.Duration) (*RunIssueResult, error) {
	for attempt := 1; attempt <= maxTries; attempt++ {
		fmt.Fprintf(out, "Attempt %d/%d\n", attempt, maxTries)

		agentOut, outcome, err := runAgentAttempt(d, runtimePath, out, timeout, name, args...)
		if err != nil {
			return nil, exitErr(ExitOperational, "agent execution: %v", err)
		}
		if outcome.interrupted {
			return nil, exitErr(ExitInterrupted, "interrupted")
		}
		if outcome.timedOut {
			summary := fmt.Sprintf("timed out after %s on attempt %d", timeout, attempt)
			if err := finalizeIssueFailed(d, sel, attempt, summary); err != nil {
				return nil, manualRepairErr(err)
			}
			return nil, exitErr(ExitOperational, "issue timed out after %d started attempt(s)", attempt)
		}
		if outcome.runErr != nil {
			return nil, exitErr(ExitOperational, "agent execution: %v", outcome.runErr)
		}

		issueData, err := d.FS.ReadFile(sel.IssuePath)
		if err != nil {
			return nil, exitErr(ExitOperational, "read issue markdown: %v", err)
		}

		assessment, reason := assessAttempt(agentOut, outcome.exitCode, issueData)
		if assessment.Complete {
			return completeSuccessfulIssue(d, sel, runtimePath, assessment.Summary)
		}

		fmt.Fprintf(out, "Attempt %d failed: %s\n", attempt, reason)
		if attempt < maxTries {
			fmt.Fprintln(out, "Retrying with preserved changes...")
			continue
		}

		summary := fmt.Sprintf("failed after %d attempts: %s", maxTries, reason)
		if err := finalizeIssueFailed(d, sel, maxTries, summary); err != nil {
			return nil, manualRepairErr(err)
		}
		return nil, exitErr(ExitOperational, "%s", summary)
	}
	return nil, exitErr(ExitOperational, "unexpected attempt loop exit")
}

func assessAttempt(agentOut string, exitCode int, issueData []byte) (Assessment, string) {
	if exitCode != 0 {
		return Assessment{}, fmt.Sprintf("agent exited with status %d", exitCode)
	}
	assessment := AssessCompletion(agentOut, issueData)
	if assessment.Complete {
		return assessment, ""
	}
	reason := assessment.FailedReason
	if reason == "" {
		reason = "agent output did not satisfy completion contract"
	}
	return assessment, reason
}

func completeSuccessfulIssue(d *Deps, sel *Selection, runtimePath, summary string) (*RunIssueResult, error) {
	hasChanges, err := runtimeHasChanges(d, runtimePath)
	if err != nil {
		return nil, exitErr(ExitOperational, "check runtime changes: %v", err)
	}

	result := &RunIssueResult{
		Selection:    sel,
		AgentSummary: summary,
	}

	if hasChanges {
		sha, err := createImplementationCommit(d, runtimePath, sel.IssueSetID, sel.IssueID, summary)
		if err != nil {
			return nil, exitErr(ExitOperational, "implementation commit: %v", err)
		}
		result.CommitSHA = sha
	} else {
		result.NoOp = true
	}

	if err := finalizeIssueDone(d, sel, summary); err != nil {
		return nil, manualRepairErr(err)
	}
	return result, nil
}

func runAgentAttempt(d *Deps, runtimePath string, liveOut io.Writer, timeout time.Duration, name string, args ...string) (string, *attemptOutcome, error) {
	var capture bytes.Buffer
	mw := io.MultiWriter(liveOut, &capture)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	proc, err := d.Runner.Start(ctx, runtimePath, mw, mw, name, args...)
	if err != nil {
		return "", nil, err
	}

	outcome := &attemptOutcome{}
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	defer signal.Stop(sigCh)

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

	return capture.String(), outcome, nil
}

func finalizeIssueFailed(d *Deps, sel *Selection, attemptsStarted int, summary string) error {
	if err := AppendProgress(d, sel.Manifest.Dir, sel.IssueFile, "FAILED", summary); err != nil {
		return fmt.Errorf("append progress: %w", err)
	}
	sel.Manifest.Issues[sel.IssueIndex].Status = "failed"
	failedAfter := attemptsStarted
	sel.Manifest.Issues[sel.IssueIndex].FailedAfter = &failedAfter
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

const issueConfirmPrompt = "Run issue? [y/N]: "

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
		prompt = issueConfirmPrompt
	}
	fmt.Fprintf(out, "%s", prompt)
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

func validateDirtyRuntimeStrategy(strategy DirtyRuntimeStrategy) error {
	if strategy == DirtyRuntimeReject {
		return nil
	}
	var parsed DirtyRuntimeStrategy
	return parsed.Set(string(strategy))
}

func warnDirtyRuntimeStrategy(w io.Writer, strategy DirtyRuntimeStrategy) {
	switch strategy {
	case DirtyRuntimeContinue:
		fmt.Fprintln(w, "Warning: runtime checkout has uncommitted changes; continuing without modifying the baseline.")
	case DirtyRuntimeCommitAndContinue:
		fmt.Fprintln(w, "Warning: runtime checkout has uncommitted changes; a capturing dirty state checkpoint commit will be created before execution.")
	case DirtyRuntimeStashAndContinue:
		fmt.Fprintln(w, "Warning: runtime checkout has uncommitted changes; tracked and untracked changes will be stashed before execution. Restore the stash manually when ready.")
	}
}

func applyDirtyRuntimeStrategy(d *Deps, runtimePath, issueSetID, issueID string, strategy DirtyRuntimeStrategy, out io.Writer) error {
	switch strategy {
	case DirtyRuntimeContinue:
		return nil
	case DirtyRuntimeCommitAndContinue:
		return checkpointDirtyRuntime(d, runtimePath, issueSetID, issueID)
	case DirtyRuntimeStashAndContinue:
		return stashDirtyRuntime(d, runtimePath, out)
	default:
		return validateDirtyRuntimeStrategy(strategy)
	}
}

func checkpointDirtyRuntime(d *Deps, runtimePath, issueSetID, issueID string) error {
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
	subject := DirtyCheckpointSubject(issueSetID, issueID)
	if _, err := d.Git.CommandInDir(runtimePath, "commit", "-m", subject); err != nil {
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
	fmt.Fprintln(out, "Created stash: stash@{0}. Restore it manually when ready.")
	return nil
}

func runtimeHasChanges(d *Deps, runtimePath string) (bool, error) {
	out, err := d.Git.CommandInDir(runtimePath, "status", "--porcelain")
	if err != nil {
		return false, err
	}
	return strings.TrimSpace(out) != "", nil
}

func createImplementationCommit(d *Deps, runtimePath, issueSetID, issueID, summary string) (string, error) {
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
	subject := CommitSubject(issueSetID, issueID)
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
	fmt.Fprintf(w, "Completed %s/%s\n", result.Selection.IssueSetID, result.Selection.IssueID)
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
