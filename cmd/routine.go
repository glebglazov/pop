package cmd

import (
	"fmt"

	"github.com/glebglazov/pop/routine"
	"github.com/spf13/cobra"
)

var routineCmd = &cobra.Command{
	Use:   "routine",
	Short: "Manage recurring unattended agent routines",
	Long: `Manage recurring unattended agent routines.

Routines are directory-bound schedules that fire agent runs over time.
Author one with pop routine add from any directory (git-backed or not).`,
}

var (
	routineAddSchedule string
	routineAdd         = routine.Add
	routineList        = routine.List
)

var routineAddCmd = &cobra.Command{
	Use:   "add <id>",
	Short: "Scaffold a new routine from the current directory",
	Args:  cobra.ExactArgs(1),
	RunE:  runRoutineAdd,
}

var routineListCmd = &cobra.Command{
	Use:   "list",
	Short: "List configured routines",
	Args:  cobra.NoArgs,
	RunE:  runRoutineList,
}

func init() {
	rootCmd.AddCommand(routineCmd)
	routineCmd.AddCommand(routineAddCmd)
	routineCmd.AddCommand(routineListCmd)
	routineAddCmd.Flags().StringVar(&routineAddSchedule, "schedule", "", "routine schedule (\"every 6h\" or \"daily at 10:00\")")
	_ = routineAddCmd.MarkFlagRequired("schedule")
}

func runRoutineAdd(cmd *cobra.Command, args []string) error {
	res, err := routineAdd(args[0], routineAddSchedule, "")
	if err != nil {
		return err
	}
	fmt.Fprintf(cmd.OutOrStdout(), "Created routine %q at %s\n", res.ID, res.Dir)
	fmt.Fprintf(cmd.OutOrStdout(), "Bound directory: %s\n", res.Manifest.BoundDirectory)
	fmt.Fprintf(cmd.OutOrStdout(), "Schedule: %s\n", res.Manifest.Schedule)
	return nil
}

func runRoutineList(cmd *cobra.Command, args []string) error {
	return routineList(cmd.OutOrStdout())
}
