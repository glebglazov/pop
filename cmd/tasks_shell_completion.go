package cmd

import (
	"strings"

	"github.com/glebglazov/pop/config"
	"github.com/glebglazov/pop/project"
	"github.com/glebglazov/pop/tasks"
	"github.com/spf13/cobra"
)

var (
	taskCompletionDeps        = func() *tasks.Deps { return tasks.DefaultDeps() }
	taskCompletionProjectDeps = func() *project.Deps { return project.DefaultDeps() }
	taskCompletionConfigLoad  = func(path string) (*config.Config, error) { return config.Load(path) }
)

func init() {
	registerTaskShellCompletions()
	registerQueueShellCompletions()
	rootCmd.InitDefaultCompletionCmd()
}

func registerTaskShellCompletions() {
	registerTaskPathFlagCompletions()

	_ = taskCmd.RegisterFlagCompletionFunc("project", completeTaskProjects)

	_ = taskImplementCmd.RegisterFlagCompletionFunc("agent", completeTaskAgents)
	_ = taskImplementCmd.RegisterFlagCompletionFunc("default-agent", completeTaskAgents)
	_ = taskImplementCmd.RegisterFlagCompletionFunc("agent-output", completeTaskAgentOutputs)

	taskStatusCmd.ValidArgsFunction = completeTaskStatusArgs
	taskArchiveCmd.ValidArgsFunction = completeTaskArchiveArgs
	taskUnarchiveCmd.ValidArgsFunction = completeTaskUnarchiveArgs
	taskSetPriorityCmd.ValidArgsFunction = completeTaskSetPriorityArgs
	taskImplementCmd.ValidArgsFunction = completeTaskImplementArgs
	taskResetTaskCmd.ValidArgsFunction = completeTaskTaskFileArgs
	taskCompleteTaskCmd.ValidArgsFunction = completeTaskTaskFileArgs
	taskSkipTaskCmd.ValidArgsFunction = completeTaskTaskFileArgs
	taskTimingsCmd.ValidArgsFunction = completeTaskTimingsArgs
	taskShowPathCmd.ValidArgsFunction = completeTaskShowPathArgs
	taskExportCmd.ValidArgsFunction = completeTaskExportArgs
}

func completeTaskShowPathArgs(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	if len(args) > 0 {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}
	ids, err := tasks.CompleteTaskSetIDsWith(
		taskCompletionDeps(),
		taskCompletionProjectDeps(),
		taskCompletionConfigLoad,
		completionInputFromCmd(cmd),
		toComplete,
	)
	if err != nil {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}
	return filterShellCompletions(ids, toComplete), cobra.ShellCompDirectiveNoFileComp
}

func completeTaskExportArgs(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	if len(args) > 0 {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}
	ids, err := tasks.CompleteTaskSetIDsWith(
		taskCompletionDeps(),
		taskCompletionProjectDeps(),
		taskCompletionConfigLoad,
		completionInputFromCmd(cmd),
		toComplete,
	)
	if err != nil {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}
	return filterShellCompletions(ids, toComplete), cobra.ShellCompDirectiveNoFileComp
}

func registerTaskPathFlagCompletions() {
	_ = taskCmd.MarkPersistentFlagDirname("path")
	_ = taskCmd.MarkPersistentFlagDirname("task-definition-path")
	_ = taskImplementCmd.MarkFlagDirname("task-runtime-path")
}

func completeTaskProjects(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	names, err := tasks.CompleteProjectNamesWith(
		taskCompletionDeps(),
		taskCompletionProjectDeps(),
		taskCompletionConfigLoad,
	)
	if err != nil {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}
	return filterShellCompletions(names, toComplete), cobra.ShellCompDirectiveNoFileComp
}

func completeTaskTaskSets(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	stems, err := tasks.CompleteTaskSetIDsWith(
		taskCompletionDeps(),
		taskCompletionProjectDeps(),
		taskCompletionConfigLoad,
		completionInputFromCmd(cmd),
		toComplete,
	)
	if err != nil {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}
	return filterShellCompletions(stems, toComplete), cobra.ShellCompDirectiveNoFileComp
}

func completeTaskTaskTargets(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	return completeTaskTargetCandidates(cmd, toComplete, tasks.CompleteTaskTargetsWith)
}

// completeActionableTaskTargets omits Done sets and done tasks; implement and
// the override verbs use it, while timings keeps the unfiltered list.
func completeActionableTaskTargets(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	return completeTaskTargetCandidates(cmd, toComplete, tasks.CompleteActionableTaskTargetsWith)
}

func completeTaskTargetCandidates(cmd *cobra.Command, toComplete string, list func(*tasks.Deps, *project.Deps, func(string) (*config.Config, error), tasks.CompletionInput, string) ([]string, error)) ([]string, cobra.ShellCompDirective) {
	targets, err := list(
		taskCompletionDeps(),
		taskCompletionProjectDeps(),
		taskCompletionConfigLoad,
		completionInputFromCmd(cmd),
		toComplete,
	)
	if err != nil {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}
	filtered := filterShellCompletions(targets, toComplete)
	if strings.Contains(toComplete, "/") {
		// File stage: a slash is already present, so candidates are
		// <task-set>/<file>.md. Complete them with a normal trailing space.
		return filtered, cobra.ShellCompDirectiveNoFileComp
	}
	// Set stage: candidates are <task-set>/. Keep the cursor on the slash
	// (no trailing space) so the operator can drill straight into a file,
	// while <task-set>/ itself remains a valid whole-set target.
	return filtered, cobra.ShellCompDirectiveNoSpace | cobra.ShellCompDirectiveNoFileComp
}

func completeTaskAgents(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	return filterShellCompletions(tasks.ValidAgentPresets(), toComplete), cobra.ShellCompDirectiveNoFileComp
}

func completeTaskAgentOutputs(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	return filterShellCompletions(tasks.ValidAgentOutputModes(), toComplete), cobra.ShellCompDirectiveNoFileComp
}

// completeTaskStatusArgs completes the optional set argument to `tasks status`.
// status is set-only and read-only, so it offers bare set identifiers with a
// normal trailing space (no <task-set>/ slash drill) and keeps Done sets — the
// finished set you most often confirm.
func completeTaskStatusArgs(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	if len(args) > 0 {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}
	return completeTaskTaskSets(cmd, args, toComplete)
}

func completeTaskArchiveArgs(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	if len(args) > 0 {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}
	return completeTaskTaskSets(cmd, args, toComplete)
}

func completeTaskUnarchiveArgs(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	if len(args) > 0 {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}
	ids, err := tasks.CompleteArchivedTaskSetIDsWith(
		taskCompletionDeps(),
		taskCompletionProjectDeps(),
		taskCompletionConfigLoad,
		completionInputFromCmd(cmd),
		toComplete,
	)
	if err != nil {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}
	return filterShellCompletions(ids, toComplete), cobra.ShellCompDirectiveNoFileComp
}

func completeTaskSetPriorityArgs(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	if len(args) > 0 {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}
	return completeTaskTaskSets(cmd, args, toComplete)
}

func completeTaskImplementArgs(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	if len(args) > 0 {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}
	return completeActionableTaskTargets(cmd, args, toComplete)
}

func completeTaskTimingsArgs(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	if len(args) > 0 {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}
	return completeTaskTaskTargets(cmd, args, toComplete)
}

func completeTaskTaskFileArgs(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	if len(args) > 0 {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}
	return completeActionableTaskTargets(cmd, args, toComplete)
}

func completionInputFromCmd(cmd *cobra.Command) tasks.CompletionInput {
	return tasks.CompletionInput{
		ProjectName:        lookupTaskFlag(cmd, "project"),
		Path:               lookupTaskFlag(cmd, "path"),
		DefinitionOverride: lookupTaskFlag(cmd, "task-definition-path"),
	}
}

func lookupTaskFlag(cmd *cobra.Command, name string) string {
	for c := cmd; c != nil; c = c.Parent() {
		if f := c.Flags().Lookup(name); f != nil && f.Changed {
			val, _ := c.Flags().GetString(name)
			return val
		}
		if f := c.PersistentFlags().Lookup(name); f != nil && f.Changed {
			val, _ := c.PersistentFlags().GetString(name)
			return val
		}
	}
	return ""
}

func filterShellCompletions(items []string, toComplete string) []string {
	if toComplete == "" {
		out := make([]string, len(items))
		copy(out, items)
		return out
	}
	var out []string
	for _, item := range items {
		if strings.HasPrefix(item, toComplete) {
			out = append(out, item)
		}
	}
	return out
}
