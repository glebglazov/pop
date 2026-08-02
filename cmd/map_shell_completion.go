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
	for _, cmd := range []*cobra.Command{mapShowCmd, mapRegisterCmd, mapArchiveCmd} {
		cmd.ValidArgsFunction = completeMapArgs
	}
	mapUnarchiveCmd.ValidArgsFunction = completeArchivedMapArgs
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
