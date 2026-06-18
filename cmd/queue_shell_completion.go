package cmd

import (
	"github.com/glebglazov/pop/queue"
	"github.com/spf13/cobra"
)

var queueCompletionDeps = func() *queue.Deps { return queue.DefaultDeps() }

func registerQueueShellCompletions() {
	queueAbandonCmd.ValidArgsFunction = completeQueueAbandonArgs
}

func completeQueueAbandonArgs(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	if len(args) > 0 {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}
	ids, err := queue.CompleteAbandonSetIDs(queueCompletionDeps())
	if err != nil {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}
	return filterShellCompletions(ids, toComplete), cobra.ShellCompDirectiveNoFileComp
}
