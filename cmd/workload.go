package cmd

import (
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"
	"time"

	"github.com/glebglazov/pop/config"
	"github.com/glebglazov/pop/project"
	"github.com/glebglazov/pop/workload"
	"github.com/spf13/cobra"
)

var (
	workloadProject     string
	workloadPath        string
	workloadDefPath     string
	workloadRuntimePath string
	workloadAgentPreset string
	workloadAgentCmd    string
	workloadAgentOutput workload.AgentOutputMode
	workloadRunYes      bool
	workloadAllowDirty  workload.DirtyRuntimeStrategy = workload.DirtyRuntimeContinue
	workloadMaxTries    int
	workloadTimeout     string
)

var workloadCmd = &cobra.Command{
	Use:   "workload",
	Short: "Discover and manage local Issue-set workloads",
}

var workloadStatusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show discovered Issue-set workloads and their statuses",
	Args:  cobra.NoArgs,
	RunE:  runWorkloadStatus,
}

var workloadSetPriorityCmd = &cobra.Command{
	Use:   "set-priority ISSUE_SET PRIORITY",
	Short: "Set a registered Issue-set priority",
	Args:  cobra.ExactArgs(2),
	RunE:  runWorkloadSetPriority,
}

var workloadRunIssueCmd = &cobra.Command{
	Use:   "run-issue [ISSUE_SET | ISSUE_SET/FILE.md]",
	Short: "Execute one eligible AFK issue through a coding agent",
	Args:  cobra.MaximumNArgs(1),
	Run:   runWorkloadRunIssue,
}

var workloadRunIssuesCmd = &cobra.Command{
	Use:   "run-issues [ISSUE_SET]",
	Short: "Sequentially drain eligible AFK issues from one Issue set",
	Args:  cobra.MaximumNArgs(1),
	Run:   runWorkloadRunIssues,
}

var workloadResetIssueCmd = &cobra.Command{
	Use:   "reset-issue ISSUE_SET/FILE.md",
	Short: "Reset one failed or skipped issue back to open",
	Args:  cobra.ExactArgs(1),
	Run:   runWorkloadResetIssue,
}

var workloadCompleteIssueCmd = &cobra.Command{
	Use:   "complete-issue ISSUE_SET/FILE.md",
	Short: "Manually mark one issue done without running an agent",
	Args:  cobra.ExactArgs(1),
	Run:   runWorkloadCompleteIssue,
}

var workloadSkipIssueCmd = &cobra.Command{
	Use:   "skip-issue ISSUE_SET/FILE.md",
	Short: "Defer one open issue to skipped, unblocking its dependents",
	Args:  cobra.ExactArgs(1),
	Run:   runWorkloadSkipIssue,
}

var workloadShowPathCmd = &cobra.Command{
	Use:   "show-path [ISSUE_SET]",
	Short: "Print this repository's Workload storage issues directory, creating it on demand",
	Args:  cobra.MaximumNArgs(1),
	Run:   runWorkloadShowPath,
}

func init() {
	rootCmd.AddCommand(workloadCmd)
	workloadCmd.AddCommand(workloadStatusCmd)
	workloadCmd.AddCommand(workloadSetPriorityCmd)
	workloadCmd.AddCommand(workloadRunIssueCmd)
	workloadCmd.AddCommand(workloadRunIssuesCmd)
	workloadCmd.AddCommand(workloadResetIssueCmd)
	workloadCmd.AddCommand(workloadCompleteIssueCmd)
	workloadCmd.AddCommand(workloadSkipIssueCmd)
	workloadCmd.AddCommand(workloadShowPathCmd)

	workloadCmd.PersistentFlags().StringVar(&workloadProject, "project", "", "Select project by exact picker-visible name")
	workloadCmd.PersistentFlags().StringVar(&workloadPath, "path", "", "Select project by path (normalized to git checkout root)")
	workloadCmd.PersistentFlags().StringVar(&workloadDefPath, "workload-definition-path", "", "Exact workload definition directory (not normalized to git root)")

	workloadRunIssueCmd.Flags().StringVar(&workloadRuntimePath, "workload-runtime-path", "", "Git checkout root for issue execution (normalized to checkout root)")
	workloadRunIssueCmd.Flags().Var(&workloadAllowDirty, "allow-dirty", "Dirty runtime strategy: continue (default), commit-and-continue, stash-and-continue")
	workloadRunIssueCmd.Flags().Lookup("allow-dirty").NoOptDefVal = string(workload.DirtyRuntimeContinue)
	workloadRunIssueCmd.Flags().StringVar(&workloadAgentPreset, "agent", "claude", "Agent preset: claude, opencode, cursor, codex, pi")
	workloadRunIssueCmd.Flags().StringVar(&workloadAgentCmd, "agent-cmd", "", "Trusted shell prefix; generated prompt passed as final positional argument")
	workloadRunIssueCmd.Flags().Var(&workloadAgentOutput, "agent-output", "Agent output mode: auto (default), text")
	workloadRunIssueCmd.Flags().IntVar(&workloadMaxTries, "max-tries", workload.DefaultMaxTries, "Maximum started attempts per issue")
	workloadRunIssueCmd.Flags().StringVar(&workloadTimeout, "timeout", "30m", "Maximum duration per attempt")
	workloadRunIssueCmd.Flags().BoolVarP(&workloadRunYes, "yes", "y", false, "Skip confirmation prompt")

	workloadRunIssuesCmd.Flags().StringVar(&workloadRuntimePath, "workload-runtime-path", "", "Git checkout root for issue execution (normalized to checkout root)")
	workloadRunIssuesCmd.Flags().Var(&workloadAllowDirty, "allow-dirty", "Dirty runtime strategy: continue (default), commit-and-continue, stash-and-continue")
	workloadRunIssuesCmd.Flags().Lookup("allow-dirty").NoOptDefVal = string(workload.DirtyRuntimeContinue)
	workloadRunIssuesCmd.Flags().StringVar(&workloadAgentPreset, "agent", "claude", "Agent preset: claude, opencode, cursor, codex, pi")
	workloadRunIssuesCmd.Flags().StringVar(&workloadAgentCmd, "agent-cmd", "", "Trusted shell prefix; generated prompt passed as final positional argument")
	workloadRunIssuesCmd.Flags().Var(&workloadAgentOutput, "agent-output", "Agent output mode: auto (default), text")
	workloadRunIssuesCmd.Flags().IntVar(&workloadMaxTries, "max-tries", workload.DefaultMaxTries, "Maximum started attempts per issue")
	workloadRunIssuesCmd.Flags().StringVar(&workloadTimeout, "timeout", "30m", "Maximum duration per attempt")
	workloadRunIssuesCmd.Flags().BoolVarP(&workloadRunYes, "yes", "y", false, "Skip confirmation prompt")
}

func workloadResolveInput() workload.ResolveInput {
	return workload.ResolveInput{
		ProjectName:        workloadProject,
		Path:               workloadPath,
		DefinitionOverride: workloadDefPath,
		RuntimeOverride:    workloadRuntimePath,
	}
}

func runWorkloadStatus(cmd *cobra.Command, args []string) error {
	return runWorkloadStatusWith(workload.DefaultDeps(), os.Stdout)
}

var workloadConfigLoad = func(path string) (*config.Config, error) {
	return config.Load(path)
}

func runWorkloadStatusWith(d *workload.Deps, w io.Writer) error {
	resolved, err := workload.ResolvePathsWith(d, workloadProjectDeps(), workloadConfigLoad, workloadResolveInput())
	if err != nil {
		return fmt.Errorf("workload status: %w", err)
	}

	result, err := workload.RefreshWith(d, resolved.DefinitionPath, workload.DefaultStatePathWith(d))
	if err != nil {
		return fmt.Errorf("workload status: %w", err)
	}

	if runtimePath, err := workload.ResolveRuntimePathWith(d, resolved.ProjectPath, ""); err == nil {
		result.RuntimeLock = workload.ReadRuntimeLockStatus(d, runtimePath)
	}

	workload.Render(w, result)
	return nil
}

func runWorkloadSetPriority(cmd *cobra.Command, args []string) error {
	return runWorkloadSetPriorityWith(workload.DefaultDeps(), os.Stdout, args[0], args[1])
}

func runWorkloadSetPriorityWith(d *workload.Deps, w io.Writer, issueSetID, priorityArg string) error {
	priority, err := strconv.Atoi(priorityArg)
	if err != nil {
		return fmt.Errorf("workload set-priority: invalid priority %q: %w", priorityArg, err)
	}

	result, err := workload.SetPriorityWith(d, workloadProjectDeps(), workloadConfigLoad, workloadResolveInput(), issueSetID, priority)
	if err != nil {
		return fmt.Errorf("workload set-priority: %w", err)
	}

	workload.RenderPriorityUpdate(w, result.IssueSetID, result.OldPriority, result.NewPriority)
	fmt.Fprintln(w)
	workload.Render(w, result.Refresh)
	return nil
}

func runWorkloadRunIssue(cmd *cobra.Command, args []string) {
	var issuePath string
	if len(args) > 0 {
		issuePath = args[0]
	}
	err := runWorkloadRunIssueWith(workload.DefaultDeps(), os.Stdout, os.Stderr, os.Stdin, issuePath)
	handleWorkloadExit(err)
}

func runWorkloadRunIssueWith(d *workload.Deps, stdout, stderr io.Writer, stdin io.Reader, issuePath string) error {
	timeout, err := time.ParseDuration(workloadTimeout)
	if err != nil {
		return fmt.Errorf("workload run-issue: invalid --timeout %q: %w", workloadTimeout, err)
	}
	_, err = workload.RunIssueWith(d, workloadProjectDeps(), workloadConfigLoad, workload.RunIssueOptions{
		ResolveInput:      workloadResolveInput(),
		IssuePathOverride: issuePath,
		AgentPreset:       workloadAgentPreset,
		AgentCmd:          workloadAgentCmd,
		AgentOutput:       workloadAgentOutput,
		AllowDirty:        workloadAllowDirty,
		MaxTries:          workloadMaxTries,
		Timeout:           timeout,
		Yes:               workloadRunYes,
		ConfirmIn:         stdin,
		ConfirmOut:        stderr,
		Output:            stdout,
	})
	return err
}

func runWorkloadRunIssues(cmd *cobra.Command, args []string) {
	var issueSetPath string
	if len(args) > 0 {
		issueSetPath = args[0]
	}
	err := runWorkloadRunIssuesWith(workload.DefaultDeps(), os.Stdout, os.Stderr, os.Stdin, issueSetPath)
	handleWorkloadExit(err)
}

func runWorkloadRunIssuesWith(d *workload.Deps, stdout, stderr io.Writer, stdin io.Reader, issueSetPath string) error {
	timeout, err := time.ParseDuration(workloadTimeout)
	if err != nil {
		return fmt.Errorf("workload run-issues: invalid --timeout %q: %w", workloadTimeout, err)
	}
	_, err = workload.RunIssueSetWith(d, workloadProjectDeps(), workloadConfigLoad, workload.RunIssueSetOptions{
		ResolveInput:     workloadResolveInput(),
		IssueSetOverride: issueSetPath,
		AgentPreset:      workloadAgentPreset,
		AgentCmd:         workloadAgentCmd,
		AgentOutput:      workloadAgentOutput,
		AllowDirty:       workloadAllowDirty,
		MaxTries:         workloadMaxTries,
		Timeout:          timeout,
		Yes:              workloadRunYes,
		ConfirmIn:        stdin,
		ConfirmOut:       stderr,
		Output:           stdout,
	})
	return err
}

func runWorkloadResetIssue(cmd *cobra.Command, args []string) {
	err := runWorkloadResetIssueWith(workload.DefaultDeps(), os.Stdout, args[0])
	handleWorkloadExit(err)
}

func runWorkloadResetIssueWith(d *workload.Deps, w io.Writer, issuePath string) error {
	result, err := workload.ResetIssueWith(d, workloadProjectDeps(), workloadConfigLoad, workload.ResetIssueOptions{
		ResolveInput: workloadResolveInput(),
		IssuePath:    issuePath,
	})
	if err != nil {
		return err
	}
	workload.RenderIssueReset(w, result.IssueSetID, result.IssueID)
	fmt.Fprintln(w)
	workload.Render(w, result.Refresh)
	return nil
}

func runWorkloadCompleteIssue(cmd *cobra.Command, args []string) {
	err := runWorkloadCompleteIssueWith(workload.DefaultDeps(), os.Stdout, args[0])
	handleWorkloadExit(err)
}

func runWorkloadCompleteIssueWith(d *workload.Deps, w io.Writer, issuePath string) error {
	result, err := workload.CompleteIssueWith(d, workloadProjectDeps(), workloadConfigLoad, workload.CompleteIssueOptions{
		ResolveInput: workloadResolveInput(),
		IssuePath:    issuePath,
	})
	if err != nil {
		return err
	}
	workload.RenderIssueComplete(w, result.IssueSetID, result.IssueID)
	fmt.Fprintln(w)
	workload.Render(w, result.Refresh)
	return nil
}

func runWorkloadSkipIssue(cmd *cobra.Command, args []string) {
	err := runWorkloadSkipIssueWith(workload.DefaultDeps(), os.Stdout, args[0])
	handleWorkloadExit(err)
}

func runWorkloadSkipIssueWith(d *workload.Deps, w io.Writer, issuePath string) error {
	result, err := workload.SkipIssueWith(d, workloadProjectDeps(), workloadConfigLoad, workload.SkipIssueOptions{
		ResolveInput: workloadResolveInput(),
		IssuePath:    issuePath,
	})
	if err != nil {
		return err
	}
	workload.RenderIssueSkip(w, result.IssueSetID, result.IssueID)
	fmt.Fprintln(w)
	workload.Render(w, result.Refresh)
	return nil
}

func runWorkloadShowPath(cmd *cobra.Command, args []string) {
	var issueSetID string
	if len(args) > 0 {
		issueSetID = args[0]
	}
	err := runWorkloadShowPathWith(workload.DefaultDeps(), os.Stdout, issueSetID)
	handleWorkloadExit(err)
}

func runWorkloadShowPathWith(d *workload.Deps, w io.Writer, issueSetID string) error {
	result, err := workload.ShowPath(d, "", issueSetID)
	if err != nil {
		return err
	}
	fmt.Fprintln(w, result.Path)
	return nil
}

func handleWorkloadExit(err error) {
	if err == nil {
		return
	}
	var exitErr *workload.ExitError
	if errors.As(err, &exitErr) {
		if exitErr.Err != nil {
			fmt.Fprintln(os.Stderr, exitErr.Err)
		}
		os.Exit(exitErr.Code)
	}
	fmt.Fprintln(os.Stderr, err)
	os.Exit(workload.ExitSetup)
}

func workloadProjectDeps() *project.Deps {
	return project.DefaultDeps()
}
