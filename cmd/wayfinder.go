package cmd

import (
	"fmt"
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

var wayfinderShowCmd = &cobra.Command{
	Use:   "show MAP",
	Short: "Show one wayfinder map in detail",
	Args:  cobra.ExactArgs(1),
	Run:   runWayfinderShow,
}

var wayfinderArchiveCmd = &cobra.Command{
	Use:   "archive MAP",
	Short: "Hide a wayfinder map from default views",
	Args:  cobra.ExactArgs(1),
	Run:   runWayfinderArchive,
}

var wayfinderUnarchiveCmd = &cobra.Command{
	Use:   "unarchive MAP",
	Short: "Restore an archived wayfinder map to default views",
	Args:  cobra.ExactArgs(1),
	Run:   runWayfinderUnarchive,
}

func init() {
	rootCmd.AddCommand(wayfinderCmd)
	wayfinderCmd.AddCommand(wayfinderStatusCmd)
	wayfinderCmd.AddCommand(wayfinderShowCmd)
	wayfinderCmd.AddCommand(wayfinderArchiveCmd)
	wayfinderCmd.AddCommand(wayfinderUnarchiveCmd)
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

func runWayfinderShow(cmd *cobra.Command, args []string) {
	err := runWayfinderShowWith(wayfinder.DefaultDeps(), os.Stdout, args[0])
	handleTaskExit(err)
}

func runWayfinderShowWith(d *wayfinder.Deps, w io.Writer, mapID string) error {
	return wayfinder.ShowWith(d, w, "", mapID)
}

func runWayfinderArchive(cmd *cobra.Command, args []string) {
	err := runWayfinderArchiveWith(wayfinder.DefaultDeps(), os.Stdout, args[0])
	handleTaskExit(err)
}

func runWayfinderArchiveWith(d *wayfinder.Deps, w io.Writer, mapID string) error {
	result, err := wayfinder.ArchiveMap(d, "", mapID)
	if err != nil {
		return err
	}
	fmt.Fprintf(w, "Archived wayfinder map %s\n", result.MapID)
	return nil
}

func runWayfinderUnarchive(cmd *cobra.Command, args []string) {
	err := runWayfinderUnarchiveWith(wayfinder.DefaultDeps(), os.Stdout, args[0])
	handleTaskExit(err)
}

func runWayfinderUnarchiveWith(d *wayfinder.Deps, w io.Writer, mapID string) error {
	result, err := wayfinder.UnarchiveMap(d, "", mapID)
	if err != nil {
		return err
	}
	fmt.Fprintf(w, "Unarchived wayfinder map %s\n", result.MapID)
	return nil
}
