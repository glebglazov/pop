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
	routineAdd       = routine.Add
	routineList      = routine.List
	routineFire      = routine.Fire
	routinePause     = routine.Pause
	routineResume    = routine.Resume
	routineRuns      = routine.Runs
	routineDashboard = routine.RunDashboard
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

var routinePauseCmd = &cobra.Command{
	Use:   "pause <id>",
	Short: "Suspend scheduled firing for a routine",
	Args:  cobra.ExactArgs(1),
	RunE:  runRoutinePause,
}

var routineResumeCmd = &cobra.Command{
	Use:   "resume <id>",
	Short: "Resume scheduled firing for a paused routine",
	Args:  cobra.ExactArgs(1),
	RunE:  runRoutineResume,
}

var routineRunsCmd = &cobra.Command{
	Use:   "runs <id>",
	Short: "List a routine's run history",
	Args:  cobra.ExactArgs(1),
	RunE:  runRoutineRuns,
}

var routineDashboardCmd = &cobra.Command{
	Use:   "dashboard",
	Short: "Open the interactive routines dashboard",
	Args:  cobra.NoArgs,
	RunE:  runRoutineDashboard,
}

func init() {
	rootCmd.AddCommand(routineCmd)
	routineCmd.AddCommand(routineAddCmd)
	routineCmd.AddCommand(routineListCmd)
	routineCmd.AddCommand(routineFireCmd)
	routineCmd.AddCommand(routinePauseCmd)
	routineCmd.AddCommand(routineResumeCmd)
	routineCmd.AddCommand(routineRunsCmd)
	routineCmd.AddCommand(routineDashboardCmd)
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

func runRoutinePause(cmd *cobra.Command, args []string) error {
	res, err := routinePause(args[0])
	if err != nil {
		return err
	}
	if res.AlreadyPaused {
		fmt.Fprintf(cmd.OutOrStdout(), "Routine %q is already paused\n", res.RoutineID)
		return nil
	}
	fmt.Fprintf(cmd.OutOrStdout(), "Paused routine %q\n", res.RoutineID)
	return nil
}

func runRoutineResume(cmd *cobra.Command, args []string) error {
	res, err := routineResume(args[0])
	if err != nil {
		return err
	}
	if res.NotPaused {
		fmt.Fprintf(cmd.OutOrStdout(), "Routine %q is not paused\n", res.RoutineID)
		return nil
	}
	fmt.Fprintf(cmd.OutOrStdout(), "Resumed routine %q\n", res.RoutineID)
	return nil
}

func runRoutineRuns(cmd *cobra.Command, args []string) error {
	return routineRuns(args[0], cmd.OutOrStdout())
}

func runRoutineDashboard(cmd *cobra.Command, args []string) error {
	return routineDashboard(routine.DefaultDeps())
}
