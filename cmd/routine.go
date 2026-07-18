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

var routineAddSchedule string
var (
	routineAdd  = routine.Add
	routineList = routine.List
	routineFire = routine.Fire
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

var routineFireCmd = &cobra.Command{
	Use:   "fire <id>",
	Short: "Run a routine immediately in the foreground",
	Args:  cobra.ExactArgs(1),
	RunE:  runRoutineFire,
}

func init() {
	rootCmd.AddCommand(routineCmd)
	routineCmd.AddCommand(routineAddCmd)
	routineCmd.AddCommand(routineListCmd)
	routineCmd.AddCommand(routineFireCmd)
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

func runRoutineFire(cmd *cobra.Command, args []string) error {
	res, err := routineFire(args[0])
	if err != nil {
		return err
	}
	fmt.Fprintf(cmd.OutOrStdout(), "Routine %q finished with agent %s\n", res.RoutineID, res.AgentPreset)
	fmt.Fprintf(cmd.OutOrStdout(), "Report: %s\n", res.ReportPath)
	return nil
}
