package cmd

import (
	"github.com/glebglazov/pop/wayfinder"
	"github.com/spf13/cobra"
)

// registerMapShellCompletions wires positional completion for the `pop map`
// family. Every verb takes exactly one bare map identifier, so completion offers
// candidates for the first positional only. It mirrors the Task-set rule: the
// filter narrows completion, never resolution — `unarchive` offers only archived
// Maps, and every other verb offers only the visible ones, but explicitly typing
// either still resolves.
func registerMapShellCompletions() {
	for _, cmd := range []*cobra.Command{mapShowCmd, mapRegisterCmd, mapArchiveCmd, mapNextCmd, mapArriveCmd, mapOpenCmd} {
		cmd.ValidArgsFunction = completeMapArgs
	}
	mapUnarchiveCmd.ValidArgsFunction = completeArchivedMapArgs
	for _, cmd := range []*cobra.Command{mapClaimCmd, mapResolveCmd, mapOutOfScopeCmd} {
		cmd.ValidArgsFunction = completeMapTicketArgs
	}
}

// completeMapTicketArgs completes `pop map claim`: a map id first, then the
// tickets of that map still worth claiming. A resolved ticket is never offered —
// claiming one is refused, so offering it would only produce an error.
func completeMapTicketArgs(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	switch len(args) {
	case 0:
		return completeMapIDs(args, toComplete, false)
	case 1:
		d := cmdLayerDeps()
		ids := wayfinder.CompletionTicketIDs(d.wayfinderDeps(), d.WorkDir(), args[0])
		return filterShellCompletions(ids, toComplete), cobra.ShellCompDirectiveNoFileComp
	default:
		return nil, cobra.ShellCompDirectiveNoFileComp
	}
}

func completeMapArgs(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	return completeMapIDs(args, toComplete, false)
}

func completeArchivedMapArgs(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	return completeMapIDs(args, toComplete, true)
}

func completeMapIDs(args []string, toComplete string, archivedOnly bool) ([]string, cobra.ShellCompDirective) {
	if len(args) > 0 {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}
	d := cmdLayerDeps()
	ids := wayfinder.CompletionMapIDs(d.wayfinderDeps(), d.WorkDir(), archivedOnly)
	return filterShellCompletions(ids, toComplete), cobra.ShellCompDirectiveNoFileComp
}
