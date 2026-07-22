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
Author one with pop routine new from any directory (git-backed or not).`,
}

var routineNewSchedule string
var routineNewRefineAgent string
var routineNewAgents []string
var routineNewEffort string
var routineEditSchedule string
var routineEditRefineAgent string
var routineEditAgents []string
var routineEditEffort string
var (
	routineNew              = routine.Add
	routineEdit             = routine.Edit
	routineConfigureRuntime = routine.ConfigureRuntime
	routineUpdateRuntime    = routine.UpdateRuntime
	routineRefine           = routine.Refine
	routineInteractive      = routine.Interactive
	routineList             = routine.List
	routineFire             = routine.Fire
	routinePause            = routine.Pause
	routineResume           = routine.Resume
	routineRuns             = routine.Runs
	routineDashboard        = dashboardshell.RunFromRoutine
)

var routineNewCmd = &cobra.Command{
	Use:   "new <id>",
	Short: "Scaffold a new routine from the current directory",
	Args:  cobra.ExactArgs(1),
	RunE:  runRoutineNew,
}

var routineEditCmd = &cobra.Command{
	Use:   "edit <id>",
	Short: "Edit a routine's prompt or schedule",
	Long: `Edit a routine's prompt or schedule.

Plain invocation drops into the Routine refinement session — a numbered menu to
fire test runs, view reports, edit the prompt, edit the schedule, and resume the
routine (interactive TTY only). With --schedule "<expr>" it rewrites the manifest
schedule directly and opens no session. --agent (repeatable) and --effort are
also direct writes; editing runtime config pauses the routine (reason changed).
The bound directory and id are fixed at creation.`,
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
	routineCmd.AddCommand(routineNewCmd)
	routineCmd.AddCommand(routineEditCmd)
	routineCmd.AddCommand(routineListCmd)
	routineCmd.AddCommand(routineFireCmd)
	routineCmd.AddCommand(routinePauseCmd)
	routineCmd.AddCommand(routineResumeCmd)
	routineCmd.AddCommand(routineRunsCmd)
	routineCmd.AddCommand(routineDashboardCmd)
	routineNewCmd.Flags().StringVar(&routineNewSchedule, "schedule", "", "routine schedule (optional; omit for a manual-fire-only routine): "+routine.ScheduleGrammar)
	routineNewCmd.Flags().StringArrayVar(&routineNewAgents, "agent", nil, "runtime agent preset for scheduled runs; repeat to define an ordered fallback list")
	routineNewCmd.Flags().StringVar(&routineNewEffort, "effort", "", "runtime model-strength tier: light, standard, or heavy (default standard)")
	routineNewCmd.Flags().StringVar(&routineNewRefineAgent, "refine-agent", "", "override the agent preset for the Routine refinement session")
	routineEditCmd.Flags().StringVar(&routineEditSchedule, "schedule", "", "new routine schedule: "+routine.ScheduleGrammar+"; skips the editor")
	routineEditCmd.Flags().StringArrayVar(&routineEditAgents, "agent", nil, "set the runtime agent preset list for scheduled runs; repeat for an ordered fallback list (direct write, pauses the routine)")
	routineEditCmd.Flags().StringVar(&routineEditEffort, "effort", "", "set the runtime model-strength tier: light, standard, or heavy (direct write, pauses the routine)")
	routineEditCmd.Flags().StringVar(&routineEditRefineAgent, "refine-agent", "", "override the agent preset for the Routine refinement session")
}

func runRoutineNew(cmd *cobra.Command, args []string) error {
	agentsSet := cmd.Flags().Changed("agent")
	effortSet := cmd.Flags().Changed("effort")
	res, err := routineNew(args[0], routineNewSchedule, "")
	if err != nil {
		return err
	}
	out := cmd.OutOrStdout()
	fmt.Fprintf(out, "Created routine %q at %s\n", res.ID, res.Dir)
	fmt.Fprintf(out, "Bound directory: %s\n", res.Manifest.BoundDirectory)
	fmt.Fprintf(out, "Schedule: %s\n", routine.ScheduleLabel(res.Manifest.Schedule))
	// Runtime agents/effort, when supplied, are direct validated writes onto the
	// freshly-scaffolded (created-paused) routine — no refinement gate involved.
	if agentsSet || effortSet {
		if _, err := routineConfigureRuntime(res.ID, routineNewAgents, agentsSet, routineNewEffort, effortSet); err != nil {
			return err
		}
	}
	// On a TTY, drop straight into the refinement session; a non-interactive new
	// just scaffolds paused and prints how to iterate manually.
	if routineInteractive() {
		return routineRefine(res.ID, routineNewRefineAgent)
	}
	fmt.Fprintf(out, "\nRoutine created paused. Iterate on its prompt, fire it manually with\n")
	fmt.Fprintf(out, "  pop routine fire %s\nuntil you are happy with the result, then arm it with\n", res.ID)
	fmt.Fprintf(out, "  pop routine resume %s\nThe first fire anchors the schedule.\n", res.ID)
	if !res.Manifest.IsScheduled() {
		fmt.Fprintf(out, "No schedule was set; the routine stays manual-fire-only until you set one with\n")
		fmt.Fprintf(out, "  pop routine edit %s --schedule \"<expr>\"\n", res.ID)
	}
	return nil
}

func runRoutineEdit(cmd *cobra.Command, args []string) error {
	scheduleSet := cmd.Flags().Changed("schedule")
	agentsSet := cmd.Flags().Changed("agent")
	effortSet := cmd.Flags().Changed("effort")
	// --schedule / --agent / --effort are direct, validated writes with no gate.
	if scheduleSet || agentsSet || effortSet {
		out := cmd.OutOrStdout()
		if scheduleSet {
			res, err := routineEdit(args[0], routineEditSchedule, true)
			if err != nil {
				return err
			}
			label := res.Schedule
			if label == "" {
				// An empty schedule was cleared to unscheduled (manual-only).
				label = "manual"
			}
			fmt.Fprintf(out, "Updated schedule for routine %q to %s\n", res.RoutineID, label)
		}
		// Editing runtime agents/effort is run-affecting: it pauses the routine
		// with reason `changed`.
		if agentsSet || effortSet {
			res, err := routineUpdateRuntime(args[0], routineEditAgents, agentsSet, routineEditEffort, effortSet)
			if err != nil {
				return err
			}
			fmt.Fprintf(out, "Updated runtime config for routine %q; paused (changed)\n", res.RoutineID)
		}
		return nil
	}
	// Bare edit opens the refinement session.
	return routineRefine(args[0], routineEditRefineAgent)
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
