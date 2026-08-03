package cmd

import (
	"fmt"

	"github.com/glebglazov/pop/config"
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

// routineNewOpts holds one invocation's flag values. Each command constructor
// binds its own struct, so parallel tests never share flag state.
type routineNewOpts struct {
	schedule    string
	agents      []string
	effort      string
	refineAgent string
}

type routineEditOpts struct {
	schedule    string
	agents      []string
	effort      string
	refineAgent string
}

func newRoutineNewCmd() *cobra.Command {
	o := &routineNewOpts{}
	cmd := &cobra.Command{
		Use:   "new <id>",
		Short: "Scaffold a new routine from the current directory",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runRoutineNew(cmd, args, o)
		},
	}
	cmd.Flags().StringVar(&o.schedule, "schedule", "", "routine schedule (optional; omit for a manual-fire-only routine): "+routine.ScheduleGrammar)
	cmd.Flags().StringArrayVar(&o.agents, "agent", nil, "runtime agent preset for scheduled runs; repeat to define an ordered fallback list")
	cmd.Flags().StringVar(&o.effort, "effort", "", "runtime model-strength tier: light, standard, or heavy (default standard)")
	cmd.Flags().StringVar(&o.refineAgent, "refine-agent", "", "override the agent preset for the Routine refinement session")
	return cmd
}

func newRoutineEditCmd() *cobra.Command {
	o := &routineEditOpts{}
	cmd := &cobra.Command{
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
		RunE: func(cmd *cobra.Command, args []string) error {
			return runRoutineEdit(cmd, args, o)
		},
	}
	cmd.Flags().StringVar(&o.schedule, "schedule", "", "new routine schedule: "+routine.ScheduleGrammar+"; skips the editor")
	cmd.Flags().StringArrayVar(&o.agents, "agent", nil, "set the runtime agent preset list for scheduled runs; repeat for an ordered fallback list (direct write, pauses the routine)")
	cmd.Flags().StringVar(&o.effort, "effort", "", "set the runtime model-strength tier: light, standard, or heavy (direct write, pauses the routine)")
	cmd.Flags().StringVar(&o.refineAgent, "refine-agent", "", "override the agent preset for the Routine refinement session")
	return cmd
}

func newRoutineListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List configured routines",
		Args:  cobra.NoArgs,
		RunE:  runRoutineList,
	}
}

func newRoutineFireCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "fire <id>",
		Short: "Run a routine immediately in the foreground",
		Args:  cobra.ExactArgs(1),
		RunE:  runRoutineFire,
	}
}

func newRoutinePauseCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "pause <id>",
		Short: "Suspend scheduled firing for a routine",
		Args:  cobra.ExactArgs(1),
		RunE:  runRoutinePause,
	}
}

func newRoutineResumeCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "resume <id>",
		Short: "Resume scheduled firing for a paused routine",
		Args:  cobra.ExactArgs(1),
		RunE:  runRoutineResume,
	}
}

func newRoutineRunsCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "runs <id>",
		Short: "List a routine's run history",
		Args:  cobra.ExactArgs(1),
		RunE:  runRoutineRuns,
	}
}

func newRoutineHandoffCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "handoff <id>",
		Short: "Print a continuation prompt assembled from a routine's artifacts",
		Long: `Print a continuation prompt for a fresh agent session, assembled from a
routine's artifacts (its prompt, latest run report, memory directory, and bound
directory). The prompt bakes in no task of its own — pipe it into another agent
and follow up with the task you want done, e.g. "fix all the bugs this routine
found".`,
		Args: cobra.ExactArgs(1),
		RunE: runRoutineHandoff,
	}
}

func newRoutineDashboardCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "dashboard",
		Short: "Open the Work dashboard on its routines page",
		Long: `Open the Work dashboard on its routines page.

Every routine is listed, ordered by how close it is to where you are standing:
this checkout first (project routines included), then another checkout of this
project, then everything else. Press v to switch to the task sets and maps page
and back.`,
		Args: cobra.NoArgs,
		RunE: runRoutineDashboard,
	}
}

func newRoutineMigrateManifestsCmd() *cobra.Command {
	return &cobra.Command{
		Use:    "migrate-manifests",
		Short:  "One-shot migration of legacy manifest.json routines to frontmatter + state.json",
		Long:   "Migrates every routine still carrying a legacy manifest.json (pre-ADR-0139) to the split format: schedule/agents/effort move into prompt.md frontmatter and machine state moves into state.json. Idempotent — already-migrated routines are left untouched — and conservative — a directory it cannot parse is reported and skipped. Meant to be run once by the machine owner.",
		Hidden: true,
		Args:   cobra.NoArgs,
		RunE:   runRoutineMigrateManifests,
	}
}

func init() {
	rootCmd.AddCommand(routineCmd)
	routineCmd.AddCommand(newRoutineNewCmd())
	routineCmd.AddCommand(newRoutineEditCmd())
	routineCmd.AddCommand(newRoutineListCmd())
	routineCmd.AddCommand(newRoutineFireCmd())
	routineCmd.AddCommand(newRoutinePauseCmd())
	routineCmd.AddCommand(newRoutineResumeCmd())
	routineCmd.AddCommand(newRoutineRunsCmd())
	routineCmd.AddCommand(newRoutineHandoffCmd())
	routineCmd.AddCommand(newRoutineDashboardCmd())
	routineCmd.AddCommand(newRoutineMigrateManifestsCmd())
}

func runRoutineNew(cmd *cobra.Command, args []string, o *routineNewOpts) error {
	d := cmdLayerDeps().routineDeps()
	agentsSet := cmd.Flags().Changed("agent")
	effortSet := cmd.Flags().Changed("effort")
	res, err := routine.AddWith(d, args[0], o.schedule, cmdLayerDeps().WorkDir())
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
		if _, err := routine.ConfigureRuntimeWith(d, res.ID, o.agents, agentsSet, o.effort, effortSet); err != nil {
			return err
		}
	}
	// On a TTY, drop straight into the refinement session; a non-interactive new
	// just scaffolds paused and prints how to iterate manually.
	if routine.InteractiveWith(d) {
		return routine.RefineWith(d, res.ID, o.refineAgent)
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

func runRoutineEdit(cmd *cobra.Command, args []string, o *routineEditOpts) error {
	d := cmdLayerDeps().routineDeps()
	scheduleSet := cmd.Flags().Changed("schedule")
	agentsSet := cmd.Flags().Changed("agent")
	effortSet := cmd.Flags().Changed("effort")
	// --schedule / --agent / --effort are direct, validated writes with no gate.
	if scheduleSet || agentsSet || effortSet {
		out := cmd.OutOrStdout()
		if scheduleSet {
			res, err := routine.EditWith(d, args[0], o.schedule, true)
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
			res, err := routine.UpdateRuntimeWith(d, args[0], o.agents, agentsSet, o.effort, effortSet)
			if err != nil {
				return err
			}
			// An authored routine pauses (reason changed) on a runtime edit; a
			// Project routine has no pause state, so its committed file is rewritten
			// in place without pausing (ADR-0138).
			if res.Paused {
				fmt.Fprintf(out, "Updated runtime config for routine %q; paused (changed)\n", res.RoutineID)
			} else {
				fmt.Fprintf(out, "Updated runtime config for routine %q\n", res.RoutineID)
			}
		}
		return nil
	}
	// Bare edit opens the refinement session.
	return routine.RefineWith(d, args[0], o.refineAgent)
}

func runRoutineList(cmd *cobra.Command, args []string) error {
	return routine.ListWith(cmdLayerDeps().routineDeps(), cmd.OutOrStdout())
}

func runRoutineFire(cmd *cobra.Command, args []string) error {
	res, err := routine.FireWith(cmdLayerDeps().routineDeps(), args[0])
	if err != nil {
		return err
	}
	fmt.Fprintf(cmd.OutOrStdout(), "Routine %q finished with agent %s\n", res.RoutineID, res.AgentPreset)
	fmt.Fprintf(cmd.OutOrStdout(), "Report: %s\n", res.ReportPath)
	return nil
}

func runRoutinePause(cmd *cobra.Command, args []string) error {
	res, err := routine.PauseWith(cmdLayerDeps().routineDeps(), args[0])
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
	res, err := routine.ResumeWith(cmdLayerDeps().routineDeps(), args[0])
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
	return routine.RunsWith(cmdLayerDeps().routineDeps(), args[0], cmd.OutOrStdout())
}

func runRoutineHandoff(cmd *cobra.Command, args []string) error {
	return routine.HandoffWith(cmdLayerDeps().routineDeps(), args[0], cmd.OutOrStdout())
}

// routineRunDashboard is the Work dashboard opened on its Routine page. There is
// no Routine TUI behind this any more: the verb is an entry onto page B of the
// one dashboard, so a `v` from there lands on Task sets and Maps.
var routineRunDashboard = dashboardshell.RunFromRoutine

func runRoutineDashboard(cmd *cobra.Command, args []string) error {
	cfgPath := cfgFile
	if cfgPath == "" {
		cfgPath = config.DefaultConfigPath()
	}
	cfg, err := workConfigLoad(cfgPath)
	if err != nil {
		return err
	}
	d := cmdLayerDeps().queueDeps()
	d.LoadConfig = workConfigLoad
	return routineRunDashboard(d, cfg)
}

func runRoutineMigrateManifests(cmd *cobra.Command, args []string) error {
	return routine.MigrateManifestsWith(cmdLayerDeps().routineDeps(), cmd.OutOrStdout())
}
