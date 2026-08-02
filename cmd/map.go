package cmd

import (
	"fmt"
	"io"
	"os"

	"github.com/glebglazov/pop/wayfinder"
	"github.com/spf13/cobra"
)

var mapStatusAll bool

var mapCmd = &cobra.Command{
	Use:   "map",
	Short: "Browse and manage Maps",
	Long: `Browse and manage Maps.

Maps live under the Task-storage maps/ directory as plain markdown: map.md plus
issues/ Decision tickets, with index.json holding ticket state. Charting ends
with ` + "`pop map register`" + `, which validates that manifest and makes the
Map registered Work.`,
}

var mapStatusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show map status",
	Args:  cobra.NoArgs,
	Run:   runMapStatus,
}

var mapShowCmd = &cobra.Command{
	Use:   "show MAP",
	Short: "Show one map in detail",
	Args:  cobra.ExactArgs(1),
	Run:   runMapShow,
}

// mapRegisterCmd deliberately carries no --managed flag: wayfinding writes
// nothing into the repository, so a Map has no checkout of its own and
// provisions no worktree (ADR-0172).
var mapRegisterCmd = &cobra.Command{
	Use:   "register MAP",
	Short: "End charting: validate a map's manifest and register it as Work",
	Args:  cobra.ExactArgs(1),
	Run:   runMapRegister,
}

var mapArchiveCmd = &cobra.Command{
	Use:   "archive MAP",
	Short: "Hide a map from default views",
	Args:  cobra.ExactArgs(1),
	Run:   runMapArchive,
}

var mapUnarchiveCmd = &cobra.Command{
	Use:   "unarchive MAP",
	Short: "Restore an archived map to default views",
	Args:  cobra.ExactArgs(1),
	Run:   runMapUnarchive,
}

func init() {
	rootCmd.AddCommand(mapCmd)
	mapCmd.AddCommand(mapStatusCmd)
	mapCmd.AddCommand(mapShowCmd)
	mapCmd.AddCommand(mapRegisterCmd)
	mapCmd.AddCommand(mapArchiveCmd)
	mapCmd.AddCommand(mapUnarchiveCmd)
	mapStatusCmd.Flags().BoolVar(&mapStatusAll, "all", false, "include done, abandoned, and archived maps")
}

func runMapStatus(cmd *cobra.Command, args []string) {
	err := runMapStatusWith(cmdLayerDeps().wayfinderDeps(), os.Stdout, mapStatusAll)
	handleTaskExit(err)
}

func runMapStatusWith(d *wayfinder.Deps, w io.Writer, includeAll bool) error {
	snap, err := wayfinder.BuildStatus(d, cmdLayerDeps().WorkDir(), includeAll)
	if err != nil {
		return err
	}
	return wayfinder.RenderStatus(w, snap)
}

func runMapShow(cmd *cobra.Command, args []string) {
	err := runMapShowWith(cmdLayerDeps().wayfinderDeps(), os.Stdout, args[0])
	handleTaskExit(err)
}

func runMapShowWith(d *wayfinder.Deps, w io.Writer, mapID string) error {
	return wayfinder.ShowWith(d, w, cmdLayerDeps().WorkDir(), mapID)
}

func runMapRegister(cmd *cobra.Command, args []string) {
	err := runMapRegisterWith(cmdLayerDeps().wayfinderDeps(), os.Stdout, args[0])
	handleTaskExit(err)
}

func runMapRegisterWith(d *wayfinder.Deps, w io.Writer, mapID string) error {
	result, err := wayfinder.RegisterMap(d, cmdLayerDeps().WorkDir(), mapID)
	if err != nil {
		return err
	}
	if result.AlreadyRegistered {
		fmt.Fprintf(w, "Map %s is already registered\n", result.MapID)
		return nil
	}
	fmt.Fprintf(w, "Registered map %s\n", result.MapID)
	return nil
}

func runMapArchive(cmd *cobra.Command, args []string) {
	err := runMapArchiveWith(cmdLayerDeps().wayfinderDeps(), os.Stdout, args[0])
	handleTaskExit(err)
}

func runMapArchiveWith(d *wayfinder.Deps, w io.Writer, mapID string) error {
	result, err := wayfinder.ArchiveMap(d, cmdLayerDeps().WorkDir(), mapID)
	if err != nil {
		return err
	}
	fmt.Fprintf(w, "Archived map %s\n", result.MapID)
	return nil
}

func runMapUnarchive(cmd *cobra.Command, args []string) {
	err := runMapUnarchiveWith(cmdLayerDeps().wayfinderDeps(), os.Stdout, args[0])
	handleTaskExit(err)
}

func runMapUnarchiveWith(d *wayfinder.Deps, w io.Writer, mapID string) error {
	result, err := wayfinder.UnarchiveMap(d, cmdLayerDeps().WorkDir(), mapID)
	if err != nil {
		return err
	}
	fmt.Fprintf(w, "Unarchived map %s\n", result.MapID)
	return nil
}
