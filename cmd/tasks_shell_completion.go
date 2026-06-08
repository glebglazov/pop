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
	rootCmd.InitDefaultCompletionCmd()
}

func registerTaskShellCompletions() {
	registerTaskPathFlagCompletions()

	_ = taskCmd.RegisterFlagCompletionFunc("project", completeTaskProjects)

	_ = taskImplementCmd.RegisterFlagCompletionFunc("agent", completeTaskAgents)
	_ = taskImplementCmd.RegisterFlagCompletionFunc("agent-output", completeTaskAgentOutputs)

	taskSetPriorityCmd.ValidArgsFunction = completeTaskSetPriorityArgs
	taskImplementCmd.ValidArgsFunction = completeTaskImplementArgs
	taskResetTaskCmd.ValidArgsFunction = completeTaskTaskFileArgs
	taskCompleteTaskCmd.ValidArgsFunction = completeTaskTaskFileArgs
	taskSkipTaskCmd.ValidArgsFunction = completeTaskTaskFileArgs
	taskShowPathCmd.ValidArgsFunction = completeTaskShowPathArgs
}

func completeTaskShowPathArgs(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	if len(args) > 0 {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}
	ids, err := tasks.ListStoredTaskSetIDs(taskCompletionDeps(), "")
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
	targets, err := tasks.CompleteTaskTargetsWith(
		taskCompletionDeps(),
		taskCompletionProjectDeps(),
		taskCompletionConfigLoad,
		completionInputFromCmd(cmd),
		toComplete,
	)
	if err != nil {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}
	return filterShellCompletions(targets, toComplete), cobra.ShellCompDirectiveNoFileComp
}

func completeTaskAgents(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	return filterShellCompletions(tasks.ValidAgentPresets(), toComplete), cobra.ShellCompDirectiveNoFileComp
}

func completeTaskAgentOutputs(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	return filterShellCompletions(tasks.ValidAgentOutputModes(), toComplete), cobra.ShellCompDirectiveNoFileComp
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
	return completeTaskTaskTargets(cmd, args, toComplete)
}

func completeTaskTaskFileArgs(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	if len(args) > 0 {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}
	return completeTaskTaskTargets(cmd, args, toComplete)
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
