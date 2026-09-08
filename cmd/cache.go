package cmd

import (
	"fmt"
	"io"

	"github.com/glebglazov/pop/tasks"
	"github.com/spf13/cobra"
)

var cacheCmd = &cobra.Command{
	Use:   "cache",
	Short: "Manage pop's machine-local cache",
	Long: `Manage pop's machine-local cache of derived answers.

Nothing in the cache is authoritative — every entry is re-validated against
its source before it is served — so throwing it away only costs recomputation.`,
}

var cacheClearCmd = &cobra.Command{
	Use:   "clear",
	Short: "Delete the machine-local cache database",
	Long: `Deletes the machine-local cache database, so the next access builds a
fresh one.

Safe to run while pop is running: an open handle is dropped as soon as the
file underneath it goes. Clearing an already-absent cache succeeds.`,
	Args: cobra.NoArgs,
	RunE: runCacheClear,
}

func init() {
	rootCmd.AddCommand(cacheCmd)
	cacheCmd.AddCommand(cacheClearCmd)
}

func runCacheClear(cmd *cobra.Command, args []string) error {
	return runCacheClearWith(cmdLayerDeps().tasksDeps(), cmd.OutOrStdout())
}

func runCacheClearWith(d *tasks.Deps, out io.Writer) error {
	path, removed, err := tasks.RemoveCacheDB(d)
	if err != nil {
		return err
	}
	if !removed {
		fmt.Fprintf(out, "No cache database at %s\n", path)
		return nil
	}
	fmt.Fprintf(out, "Removed %s\n", path)
	return nil
}
