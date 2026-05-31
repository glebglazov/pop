package cmd

import (
	"strings"

	"github.com/glebglazov/pop/config"
	"github.com/glebglazov/pop/project"
	"github.com/glebglazov/pop/workload"
	"github.com/spf13/cobra"
)

var (
	workloadCompletionDeps       = func() *workload.Deps { return workload.DefaultDeps() }
	workloadCompletionProjectDeps = func() *project.Deps { return project.DefaultDeps() }
	workloadCompletionConfigLoad  = func(path string) (*config.Config, error) { return config.Load(path) }
)

func init() {
	registerWorkloadShellCompletions()
	rootCmd.InitDefaultCompletionCmd()
}

func registerWorkloadShellCompletions() {
	registerWorkloadPathFlagCompletions()

	_ = workloadCmd.RegisterFlagCompletionFunc("project", completeWorkloadProjects)

	for _, cmd := range []*cobra.Command{
		workloadRunIssueCmd,
		workloadRunIssuesCmd,
		workloadResetIssueCmd,
	} {
		_ = cmd.RegisterFlagCompletionFunc("issue-set", completeWorkloadIssueSets)
	}
	_ = workloadRunIssueCmd.RegisterFlagCompletionFunc("issue", completeWorkloadIssues)
	_ = workloadResetIssueCmd.RegisterFlagCompletionFunc("issue", completeWorkloadIssues)

	for _, cmd := range []*cobra.Command{workloadRunIssueCmd, workloadRunIssuesCmd} {
		_ = cmd.RegisterFlagCompletionFunc("agent", completeWorkloadAgents)
	}

	workloadSetPriorityCmd.ValidArgsFunction = completeWorkloadSetPriorityArgs
}

func registerWorkloadPathFlagCompletions() {
	_ = workloadCmd.MarkPersistentFlagDirname("path")
	_ = workloadCmd.MarkPersistentFlagDirname("workload-definition-path")
	_ = workloadRunIssueCmd.MarkFlagDirname("workload-runtime-path")
	_ = workloadRunIssuesCmd.MarkFlagDirname("workload-runtime-path")
}

func completeWorkloadProjects(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	names, err := workload.CompleteProjectNamesWith(
		workloadCompletionDeps(),
		workloadCompletionProjectDeps(),
		workloadCompletionConfigLoad,
	)
	if err != nil {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}
	return filterShellCompletions(names, toComplete), cobra.ShellCompDirectiveNoFileComp
}

func completeWorkloadIssueSets(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	stems, err := workload.CompleteIssueSetIDsWith(
		workloadCompletionDeps(),
		workloadCompletionProjectDeps(),
		workloadCompletionConfigLoad,
		completionInputFromCmd(cmd),
	)
	if err != nil {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}
	return filterShellCompletions(stems, toComplete), cobra.ShellCompDirectiveNoFileComp
}

func completeWorkloadIssues(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	ids, err := workload.CompleteIssueIDsWith(
		workloadCompletionDeps(),
		workloadCompletionProjectDeps(),
		workloadCompletionConfigLoad,
		completionInputFromCmd(cmd),
	)
	if err != nil {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}
	return filterShellCompletions(ids, toComplete), cobra.ShellCompDirectiveNoFileComp
}

func completeWorkloadAgents(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	return filterShellCompletions(workload.ValidAgentPresets(), toComplete), cobra.ShellCompDirectiveNoFileComp
}

func completeWorkloadSetPriorityArgs(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	if len(args) > 0 {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}
	return completeWorkloadIssueSets(cmd, args, toComplete)
}

func completionInputFromCmd(cmd *cobra.Command) workload.CompletionInput {
	return workload.CompletionInput{
		ProjectName:        lookupWorkloadFlag(cmd, "project"),
		Path:               lookupWorkloadFlag(cmd, "path"),
		DefinitionOverride: lookupWorkloadFlag(cmd, "workload-definition-path"),
		IssueSet:           lookupWorkloadFlag(cmd, "issue-set"),
	}
}

func lookupWorkloadFlag(cmd *cobra.Command, name string) string {
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
