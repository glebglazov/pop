package cmd

import (
	"fmt"
	"io"
	"os"

	"github.com/glebglazov/pop/config"
	"github.com/glebglazov/pop/project"
	"github.com/glebglazov/pop/tasks"
	"github.com/spf13/cobra"
)

var workCmd = &cobra.Command{
	Use:   "work",
	Short: "Cross-concept work surface for planning, maps, and task sets",
	Long: `Cross-concept work surface for planning, maps, and task sets.

The Work dashboard is the unified hands-on surface for ongoing work across
registered projects, in two pages toggled with v: task sets and maps, and
routines. show-path resolves this repository's Task-storage root — the
directory holding repo.json, tasks/, and maps/ — for humans and planning
skills alike.

pop work daemon starts a foreground supervisor that, every poll interval, asks
every advanceable Work kind what it can advance and dispatches it: a drain (pop
tasks implement <set>) for each idle project with a Ready task set, a fire for
each due routine. Execution is concurrent across projects and serial within
each (enforced by the runtime execution lock). Ctrl-C stops the supervisor;
in-flight drains keep running in their panes. status reports what the daemon
can advance — task sets, then routines — and log replays what it did.`,
}

var workShowPathCmd = &cobra.Command{
	Use:   "show-path",
	Short: "Print this repository's Task-storage root, creating it on demand",
	Args:  cobra.NoArgs,
	Run:   runWorkShowPath,
}

var workDashboardCmd = &cobra.Command{
	Use:   "dashboard",
	Short: "Open the work dashboard",
	Args:  cobra.NoArgs,
	RunE:  runWorkDashboard,
}

// workDashboardIncludeDone backs the deprecated `--include-done` flag on the
// Work dashboard — an alias for seeding the `all` view preset (ADR-0197).
var workDashboardIncludeDone bool

func init() {
	rootCmd.AddCommand(workCmd)
	workCmd.AddCommand(workShowPathCmd)
	workCmd.AddCommand(workDashboardCmd)
	workDashboardCmd.Flags().BoolVar(&workDashboardIncludeDone, "include-done", false, "deprecated: alias for seeding the all view preset")
}

func runWorkShowPath(cmd *cobra.Command, args []string) {
	err := runWorkShowPathWith(cmdLayerDeps().tasksDeps(), os.Stdout)
	handleTaskExit(err)
}

func runWorkShowPathWith(d *tasks.Deps, w io.Writer) error {
	result, err := tasks.ShowStorageRoot(d, cmdLayerDeps().WorkDir())
	if err != nil {
		return err
	}
	fmt.Fprintln(w, result.Path)
	return nil
}

func runWorkDashboard(cmd *cobra.Command, args []string) error {
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
	preset, err := resolveWorkStatusPreset(cfg, "", workDashboardIncludeDone, cmd.ErrOrStderr())
	if err != nil {
		return err
	}
	d.ViewPreset = preset
	checkout, err := workRunDashboard(d, cfg)
	if err != nil {
		return err
	}
	if checkout == "" {
		return nil
	}
	// Ctrl-g on a bound row: open that checkout through the shared workbench-aware
	// open helper (task 02) — birth-time shaping when the session is absent, else
	// flat attach (ADR-0075). Because a managed worktree's session usually already
	// exists, this attaches to the running session.
	//
	// Force the tmux switch: unlike `pop worktree`, the work command exposes no
	// -s/--switch flag, so the shared flat-open path (handleWorktreeSelect) would
	// otherwise print the path instead of switching. The dashboard has already
	// quit here — the only sensible action is to attach, never echo the path.
	switchSession = true
	ctx, err := project.DetectRepoContextFromPathWith(cmdLayerDeps().projectDeps(), checkout)
	if err != nil {
		return err
	}
	return openWorktreeWithShaping(defaultWorktreeShapeDeps(), ctx, checkout)
}
