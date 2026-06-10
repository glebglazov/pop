package cmd

import (
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/glebglazov/pop/config"
	"github.com/glebglazov/pop/project"
	"github.com/glebglazov/pop/tasks"
	"github.com/spf13/cobra"
)

var (
	taskProject     string
	taskPath        string
	taskDefPath     string
	taskRuntimePath string
	taskAgentPreset string
	taskAgentCmd    string
	taskAgentOutput tasks.AgentOutputMode
	taskRunYes      bool
	taskAllowDirty  tasks.DirtyRuntimeStrategy = tasks.DirtyRuntimeContinue
	taskMaxTries    int
	taskTimeout     string
)

var taskCmd = &cobra.Command{
	Use:   "tasks",
	Short: "Discover and manage local task sets",
}

var taskStatusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show discovered task sets and their statuses",
	Args:  cobra.NoArgs,
	RunE:  runTaskStatus,
}

var taskSetPriorityCmd = &cobra.Command{
	Use:   "set-priority TASK_SET PRIORITY",
	Short: "Set a registered task-set priority",
	Args:  cobra.ExactArgs(2),
	RunE:  runTaskSetPriority,
}

var taskImplementCmd = &cobra.Command{
	Use:   "implement [TASK_SET | TASK_SET/FILE.md]",
	Short: "Implement tasks through a coding agent: drain a task set, or run one targeted task",
	Args:  cobra.MaximumNArgs(1),
	Run:   runTaskImplement,
}

var taskResetTaskCmd = &cobra.Command{
	Use:   "open TASK_SET/FILE.md",
	Short: "Reset one failed or skipped task back to open",
	Args:  cobra.ExactArgs(1),
	Run:   runTaskResetTask,
}

var taskCompleteTaskCmd = &cobra.Command{
	Use:   "complete TASK_SET/FILE.md",
	Short: "Manually mark one task done without running an agent",
	Args:  cobra.ExactArgs(1),
	Run:   runTaskCompleteTask,
}

var taskSkipTaskCmd = &cobra.Command{
	Use:   "skip TASK_SET/FILE.md",
	Short: "Defer one open task to skipped, unblocking its dependents",
	Args:  cobra.ExactArgs(1),
	Run:   runTaskSkipTask,
}

var taskTimingsCmd = &cobra.Command{
	Use:   "timings TASK_SET[/FILE.md]",
	Short: "Show per-task attempt timings derived from captured attempt streams",
	Args:  cobra.ExactArgs(1),
	Run:   runTaskTimings,
}

var taskShowPathCmd = &cobra.Command{
	Use:   "show-path [TASK_SET]",
	Short: "Print this repository's task storage directory, creating it on demand",
	Args:  cobra.MaximumNArgs(1),
	Run:   runTaskShowPath,
}

var taskMigrateCmd = &cobra.Command{
	Use:   "migrate",
	Short: "Move legacy thoughts/issues task sets in this worktree into task storage",
	Args:  cobra.NoArgs,
	Run:   runTaskMigrate,
}

func init() {
	rootCmd.AddCommand(taskCmd)
	taskCmd.AddCommand(taskStatusCmd)
	taskCmd.AddCommand(taskSetPriorityCmd)
	taskCmd.AddCommand(taskImplementCmd)
	taskCmd.AddCommand(taskResetTaskCmd)
	taskCmd.AddCommand(taskCompleteTaskCmd)
	taskCmd.AddCommand(taskSkipTaskCmd)
	taskCmd.AddCommand(taskTimingsCmd)
	taskCmd.AddCommand(taskShowPathCmd)
	taskCmd.AddCommand(taskMigrateCmd)

	taskCmd.PersistentFlags().StringVar(&taskProject, "project", "", "Select project by exact picker-visible name")
	taskCmd.PersistentFlags().StringVar(&taskPath, "path", "", "Select project by path (normalized to git checkout root)")
	taskCmd.PersistentFlags().StringVar(&taskDefPath, "task-definition-path", "", "Exact task definition directory (not normalized to git root)")

	taskImplementCmd.Flags().StringVar(&taskRuntimePath, "task-runtime-path", "", "Git checkout root for task execution (normalized to checkout root)")
	taskImplementCmd.Flags().Var(&taskAllowDirty, "allow-dirty", "Dirty runtime strategy: continue (default), commit-and-continue, stash-and-continue")
	taskImplementCmd.Flags().Lookup("allow-dirty").NoOptDefVal = string(tasks.DirtyRuntimeContinue)
	taskImplementCmd.Flags().StringVar(&taskAgentPreset, "agent", "claude", "Agent preset (claude, opencode, cursor, codex, pi), optionally followed by extra agent args, e.g. \"claude --model opus4.8\"")
	taskImplementCmd.Flags().StringVar(&taskAgentCmd, "agent-cmd", "", "Trusted shell prefix; generated prompt passed as final positional argument")
	taskImplementCmd.Flags().Var(&taskAgentOutput, "agent-output", "Agent output mode: auto (default), text")
	taskImplementCmd.Flags().IntVar(&taskMaxTries, "max-tries", tasks.DefaultMaxTries, "Maximum started attempts per task")
	taskImplementCmd.Flags().StringVar(&taskTimeout, "timeout", "1h", "Maximum duration per attempt")
	taskImplementCmd.Flags().BoolVarP(&taskRunYes, "yes", "y", false, "Skip confirmation prompt")
}

func taskResolveInput() tasks.ResolveInput {
	return tasks.ResolveInput{
		ProjectName:        taskProject,
		Path:               taskPath,
		DefinitionOverride: taskDefPath,
		RuntimeOverride:    taskRuntimePath,
	}
}

func runTaskStatus(cmd *cobra.Command, args []string) error {
	return runTaskStatusWith(tasks.DefaultDeps(), os.Stdout)
}

var taskConfigLoad = func(path string) (*config.Config, error) {
	return config.Load(path)
}

func runTaskStatusWith(d *tasks.Deps, w io.Writer) error {
	resolved, err := tasks.ResolvePathsWith(d, taskProjectDeps(), taskConfigLoad, taskResolveInput())
	if err != nil {
		return fmt.Errorf("tasks status: %w", err)
	}

	result, err := tasks.RefreshWith(d, resolved.DefinitionPath, tasks.StatePathFor(resolved.DefinitionPath))
	if err != nil {
		return fmt.Errorf("tasks status: %w", err)
	}

	if runtimePath, err := tasks.ResolveRuntimePathWith(d, resolved.ProjectPath, ""); err == nil {
		result.RuntimeLock = tasks.ReadRuntimeLockStatus(d, runtimePath)
	}

	tasks.Render(w, result)
	return nil
}

func runTaskSetPriority(cmd *cobra.Command, args []string) error {
	return runTaskSetPriorityWith(tasks.DefaultDeps(), os.Stdout, args[0], args[1])
}

func runTaskSetPriorityWith(d *tasks.Deps, w io.Writer, taskSetID, priorityArg string) error {
	priority, err := strconv.Atoi(priorityArg)
	if err != nil {
		return fmt.Errorf("tasks set-priority: invalid priority %q: %w", priorityArg, err)
	}

	result, err := tasks.SetPriorityWith(d, taskProjectDeps(), taskConfigLoad, taskResolveInput(), taskSetID, priority)
	if err != nil {
		return fmt.Errorf("tasks set-priority: %w", err)
	}

	tasks.RenderPriorityUpdate(w, result.TaskSetID, result.OldPriority, result.NewPriority)
	fmt.Fprintln(w)
	tasks.Render(w, result.Refresh)
	return nil
}

func runTaskImplement(cmd *cobra.Command, args []string) {
	var target string
	if len(args) > 0 {
		target = args[0]
	}
	var err error
	if isTaskFileTarget(target) {
		err = runTaskRunTaskWith(tasks.DefaultDeps(), os.Stdout, os.Stderr, os.Stdin, target)
	} else {
		err = runTaskRunTasksWith(tasks.DefaultDeps(), os.Stdout, os.Stderr, os.Stdin, target)
	}
	handleTaskExit(err)
}

// isTaskFileTarget reports whether a Task target reference names a single task —
// a Task-set-relative file reference such as "<task-set>/<file>.md" — rather than
// a bare Task set identifier. The ".md" suffix is the discriminator: it is exactly
// the file-reference form, so a single task runs only when a file names it; a bare
// set identifier or an empty target (no argument) drains an auto-selected set.
// Malformed forms still route in and are rejected by the executor's own validation.
func isTaskFileTarget(target string) bool {
	return strings.HasSuffix(target, ".md")
}

func runTaskRunTaskWith(d *tasks.Deps, stdout, stderr io.Writer, stdin io.Reader, taskPath string) error {
	timeout, err := time.ParseDuration(taskTimeout)
	if err != nil {
		return fmt.Errorf("tasks implement: invalid --timeout %q: %w", taskTimeout, err)
	}
	_, err = tasks.RunTaskWith(d, taskProjectDeps(), taskConfigLoad, tasks.RunTaskOptions{
		ResolveInput:     taskResolveInput(),
		TaskPathOverride: taskPath,
		AgentPreset:      taskAgentPreset,
		AgentCmd:         taskAgentCmd,
		AgentOutput:      taskAgentOutput,
		AllowDirty:       taskAllowDirty,
		MaxTries:         taskMaxTries,
		Timeout:          timeout,
		Yes:              taskRunYes,
		ConfirmIn:        stdin,
		ConfirmOut:       stderr,
		Output:           stdout,
	})
	return err
}

func runTaskRunTasksWith(d *tasks.Deps, stdout, stderr io.Writer, stdin io.Reader, taskSetPath string) error {
	timeout, err := time.ParseDuration(taskTimeout)
	if err != nil {
		return fmt.Errorf("tasks implement: invalid --timeout %q: %w", taskTimeout, err)
	}
	_, err = tasks.RunTaskSetWith(d, taskProjectDeps(), taskConfigLoad, tasks.RunTaskSetOptions{
		ResolveInput:    taskResolveInput(),
		TaskSetOverride: taskSetPath,
		AgentPreset:     taskAgentPreset,
		AgentCmd:        taskAgentCmd,
		AgentOutput:     taskAgentOutput,
		AllowDirty:      taskAllowDirty,
		MaxTries:        taskMaxTries,
		Timeout:         timeout,
		Yes:             taskRunYes,
		ConfirmIn:       stdin,
		ConfirmOut:      stderr,
		Output:          stdout,
	})
	return err
}

func runTaskResetTask(cmd *cobra.Command, args []string) {
	err := runTaskResetTaskWith(tasks.DefaultDeps(), os.Stdout, args[0])
	handleTaskExit(err)
}

func runTaskResetTaskWith(d *tasks.Deps, w io.Writer, taskPath string) error {
	result, err := tasks.ResetTaskWith(d, taskProjectDeps(), taskConfigLoad, tasks.ResetTaskOptions{
		ResolveInput: taskResolveInput(),
		TaskPath:     taskPath,
	})
	if err != nil {
		return err
	}
	tasks.RenderTaskReset(w, result.TaskSetID, result.TaskID)
	fmt.Fprintln(w)
	tasks.Render(w, result.Refresh)
	return nil
}

func runTaskCompleteTask(cmd *cobra.Command, args []string) {
	err := runTaskCompleteTaskWith(tasks.DefaultDeps(), os.Stdout, args[0])
	handleTaskExit(err)
}

func runTaskCompleteTaskWith(d *tasks.Deps, w io.Writer, taskPath string) error {
	result, err := tasks.CompleteTaskWith(d, taskProjectDeps(), taskConfigLoad, tasks.CompleteTaskOptions{
		ResolveInput: taskResolveInput(),
		TaskPath:     taskPath,
	})
	if err != nil {
		return err
	}
	tasks.RenderTaskComplete(w, result.TaskSetID, result.TaskID)
	fmt.Fprintln(w)
	tasks.Render(w, result.Refresh)
	return nil
}

func runTaskSkipTask(cmd *cobra.Command, args []string) {
	err := runTaskSkipTaskWith(tasks.DefaultDeps(), os.Stdout, args[0])
	handleTaskExit(err)
}

func runTaskSkipTaskWith(d *tasks.Deps, w io.Writer, taskPath string) error {
	result, err := tasks.SkipTaskWith(d, taskProjectDeps(), taskConfigLoad, tasks.SkipTaskOptions{
		ResolveInput: taskResolveInput(),
		TaskPath:     taskPath,
	})
	if err != nil {
		return err
	}
	tasks.RenderTaskSkip(w, result.TaskSetID, result.TaskID)
	fmt.Fprintln(w)
	tasks.Render(w, result.Refresh)
	return nil
}

func runTaskTimings(cmd *cobra.Command, args []string) {
	err := runTaskTimingsWith(tasks.DefaultDeps(), os.Stdout, args[0])
	handleTaskExit(err)
}

func runTaskTimingsWith(d *tasks.Deps, w io.Writer, target string) error {
	result, err := tasks.TimingsWith(d, taskProjectDeps(), taskConfigLoad, tasks.TimingsOptions{
		ResolveInput: taskResolveInput(),
		Target:       target,
	})
	if err != nil {
		return err
	}
	tasks.RenderTimings(w, result)
	return nil
}

func runTaskShowPath(cmd *cobra.Command, args []string) {
	var taskSetID string
	if len(args) > 0 {
		taskSetID = args[0]
	}
	err := runTaskShowPathWith(tasks.DefaultDeps(), os.Stdout, taskSetID)
	handleTaskExit(err)
}

func runTaskShowPathWith(d *tasks.Deps, w io.Writer, taskSetID string) error {
	result, err := tasks.ShowPath(d, "", taskSetID)
	if err != nil {
		return err
	}
	fmt.Fprintln(w, result.Path)
	return nil
}

func runTaskMigrate(cmd *cobra.Command, args []string) {
	err := runTaskMigrateWith(tasks.DefaultDeps(), os.Stdout)
	handleTaskExit(err)
}

func runTaskMigrateWith(d *tasks.Deps, w io.Writer) error {
	result, err := tasks.Migrate(d, "")
	if err != nil {
		return err
	}
	tasks.RenderMigrate(w, result)
	return nil
}

func handleTaskExit(err error) {
	if err == nil {
		return
	}
	var exitErr *tasks.ExitError
	if errors.As(err, &exitErr) {
		if exitErr.Err != nil {
			fmt.Fprintln(os.Stderr, exitErr.Err)
		}
		os.Exit(exitErr.Code)
	}
	fmt.Fprintln(os.Stderr, err)
	os.Exit(tasks.ExitSetup)
}

func taskProjectDeps() *project.Deps {
	return project.DefaultDeps()
}
