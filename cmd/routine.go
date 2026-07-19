package cmd

import (
	"fmt"

	"github.com/glebglazov/pop/dashboardshell"
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
var routineAddAgent string
var routineEditSchedule string
var routineEditAgent string
var (
	routineAdd         = routine.Add
	routineEdit        = routine.Edit
	routineRefine      = routine.Refine
	routineInteractive = routine.Interactive
	routineList        = routine.List
	routineFire        = routine.Fire
	routinePause       = routine.Pause
	routineResume      = routine.Resume
	routineRuns        = routine.Runs
	routineDashboard   = dashboardshell.RunFromRoutine
)

var routineAddCmd = &cobra.Command{
	Use:   "add <id>",
	Short: "Scaffold a new routine from the current directory",
	Args:  cobra.ExactArgs(1),
	RunE:  runRoutineAdd,
}

var routineEditCmd = &cobra.Command{
	Use:   "edit <id>",
	Short: "Edit a routine's prompt or schedule",
	Long: `Edit a routine's prompt or schedule.

Plain invocation drops into the refinement gate — a numbered menu to fire test
runs, view reports, edit the prompt, edit the schedule, and resume the routine
(interactive TTY only). With --schedule "<expr>" it rewrites the manifest
schedule directly and opens no gate. The bound directory and id are fixed at
creation.`,
	Args: cobra.ExactArgs(1),
	RunE: runRoutineEdit,
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
	routineCmd.AddCommand(routineEditCmd)
	routineCmd.AddCommand(routineListCmd)
	routineCmd.AddCommand(routineFireCmd)
	routineCmd.AddCommand(routinePauseCmd)
	routineCmd.AddCommand(routineResumeCmd)
	routineCmd.AddCommand(routineRunsCmd)
	routineCmd.AddCommand(routineDashboardCmd)
	routineAddCmd.Flags().StringVar(&routineAddSchedule, "schedule", "", "routine schedule (\"every 6h\" or \"daily at 10:00\")")
	_ = routineAddCmd.MarkFlagRequired("schedule")
	routineAddCmd.Flags().StringVar(&routineAddAgent, "agent", "", "override the authoring-agent preset for the refinement gate session")
	routineEditCmd.Flags().StringVar(&routineEditSchedule, "schedule", "", "new routine schedule (\"every 6h\" or \"daily at 10:00\"); skips the editor")
	routineEditCmd.Flags().StringVar(&routineEditAgent, "agent", "", "override the authoring-agent preset for the refinement gate session")
}

func runRoutineAdd(cmd *cobra.Command, args []string) error {
	res, err := routineAdd(args[0], routineAddSchedule, "")
	if err != nil {
		return err
	}
	out := cmd.OutOrStdout()
	fmt.Fprintf(out, "Created routine %q at %s\n", res.ID, res.Dir)
	fmt.Fprintf(out, "Bound directory: %s\n", res.Manifest.BoundDirectory)
	fmt.Fprintf(out, "Schedule: %s\n", res.Manifest.Schedule)
	// On a TTY, drop straight into the refinement gate; a non-interactive add
	// just scaffolds paused and prints how to iterate manually.
	if routineInteractive() {
		return routineRefine(res.ID, routineAddAgent)
	}
	fmt.Fprintf(out, "\nRoutine created paused. Iterate on its prompt, fire it manually with\n")
	fmt.Fprintf(out, "  pop routine fire %s\nuntil you are happy with the result, then arm it with\n", res.ID)
	fmt.Fprintf(out, "  pop routine resume %s\nThe first fire anchors the schedule.\n", res.ID)
	return nil
}

func runRoutineEdit(cmd *cobra.Command, args []string) error {
	// --schedule keeps its direct, validated-write behavior with no gate.
	if cmd.Flags().Changed("schedule") {
		res, err := routineEdit(args[0], routineEditSchedule, true)
		if err != nil {
			return err
		}
		fmt.Fprintf(cmd.OutOrStdout(), "Updated schedule for routine %q to %s\n", res.RoutineID, res.Schedule)
		return nil
	}
	// Bare edit opens the refinement gate.
	return routineRefine(args[0], routineEditAgent)
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
