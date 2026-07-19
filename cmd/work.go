package cmd

import (
	"fmt"
	"io"
	"os"

	"github.com/glebglazov/pop/config"
	"github.com/glebglazov/pop/project"
	"github.com/glebglazov/pop/queue"
	"github.com/glebglazov/pop/tasks"
	"github.com/spf13/cobra"
)

var workCmd = &cobra.Command{
	Use:   "work",
	Short: "Cross-concept work surface for planning, maps, and task sets",
	Long: `Cross-concept work surface for planning, maps, and task sets.

The Work dashboard is the unified hands-on surface for ongoing work —
task sets and wayfinder maps — across registered projects. show-path
resolves this repository's Task-storage root — the directory holding
repo.json, tasks/, and wayfinder/ — for humans and planning skills alike.`,
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

// workDashboardIncludeDone backs the `--include-done` flag on the Work
// dashboard read surface (ADR-0121): off by default hides every DONE Task set.
var workDashboardIncludeDone bool

func init() {
	rootCmd.AddCommand(workCmd)
	workCmd.AddCommand(workShowPathCmd)
	workCmd.AddCommand(workDashboardCmd)
	workDashboardCmd.Flags().BoolVar(&workDashboardIncludeDone, "include-done", false, "include DONE task sets (hidden by default)")
}

func runWorkShowPath(cmd *cobra.Command, args []string) {
	err := runWorkShowPathWith(tasks.DefaultDeps(), os.Stdout)
	handleTaskExit(err)
}

func runWorkShowPathWith(d *tasks.Deps, w io.Writer) error {
	result, err := tasks.ShowStorageRoot(d, "")
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
	cfg, err := queueConfigLoad(cfgPath)
	if err != nil {
		return err
	}
	d := queue.DefaultDeps()
	d.LoadConfig = queueConfigLoad
	d.IncludeDone = workDashboardIncludeDone
	checkout, err := queueRunDashboard(d, cfg)
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
	ctx, err := project.DetectRepoContextFromPathWith(project.DefaultDeps(), checkout)
	if err != nil {
		return err
	}
	return openWorktreeWithShaping(defaultWorktreeShapeDeps(), ctx, checkout)
}
