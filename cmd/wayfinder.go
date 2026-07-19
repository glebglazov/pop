package cmd

import (
	"io"
	"os"

	"github.com/glebglazov/pop/wayfinder"
	"github.com/spf13/cobra"
)

var wayfinderStatusAll bool

var wayfinderCmd = &cobra.Command{
	Use:   "wayfinder",
	Short: "Browse and manage Wayfinder maps",
	Long: `Browse and manage Wayfinder maps.

Maps live under the Task-storage wayfinder/ directory as plain markdown:
map.md plus issues/ decision tickets.`,
}

var wayfinderStatusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show wayfinder map status",
	Args:  cobra.NoArgs,
	Run:   runWayfinderStatus,
}

func init() {
	rootCmd.AddCommand(wayfinderCmd)
	wayfinderCmd.AddCommand(wayfinderStatusCmd)
	wayfinderStatusCmd.Flags().BoolVar(&wayfinderStatusAll, "all", false, "include done, abandoned, and archived maps")
}

func runWayfinderStatus(cmd *cobra.Command, args []string) {
	err := runWayfinderStatusWith(wayfinder.DefaultDeps(), os.Stdout, wayfinderStatusAll)
	handleTaskExit(err)
}

func runWayfinderStatusWith(d *wayfinder.Deps, w io.Writer, includeAll bool) error {
	snap, err := wayfinder.BuildStatus(d, "", includeAll)
	if err != nil {
		return err
	}
	return wayfinder.RenderStatus(w, snap)
}
