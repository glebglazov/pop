package cmd

import (
	"errors"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/glebglazov/pop/config"
	"github.com/glebglazov/pop/dashboardshell"
	"github.com/glebglazov/pop/queue"
	"github.com/glebglazov/pop/tasks"
	"github.com/spf13/cobra"
)

// queueCmd is the `pop queue` command group. Bare `pop queue` prints help and
// never launches the supervisor — supervising is started only by the explicit
// `pop queue run` subcommand (ADR 0027).
var queueCmd = &cobra.Command{
	Use:   "queue",
	Short: "Supervise Task-set drains across registered projects",
	Long: `Supervise Task-set drains across registered projects.

pop queue run starts a foreground supervisor that, every poll interval, scans
every registered project and spawns a drain (pop tasks implement <set>) into
the project's pop-queue tmux window for each idle project with a Ready task
set. Execution is concurrent across projects and serial within each (enforced
by the runtime execution lock). Ctrl-C stops the supervisor; in-flight drains
keep running in their panes.`,
}

var queueRunCmd = &cobra.Command{
	Use:   "run",
	Short: "Run the foreground supervisor loop",
	Args:  cobra.NoArgs,
	RunE:  runQueueRun,
}

var queueStatusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show queue status from on-disk state",
	Args:  cobra.NoArgs,
	RunE:  runQueueStatus,
}

// Deprecated: use `pop work dashboard` instead. Hidden alias for existing
// keybindings. TODO: remove at next major release.
var queueDashboardCmd = &cobra.Command{
	Use:    "dashboard",
	Short:  "Open the work dashboard (alias for work dashboard)",
	Hidden: true,
	Args:   cobra.NoArgs,
	RunE:   runWorkDashboard,
}

var queueLogCmd = &cobra.Command{
	Use:   "log",
	Short: "Show recent queue journal history",
	Args:  cobra.NoArgs,
	RunE:  runQueueLog,
}

// queueStatusIncludeDone backs the `--include-done` flag on `pop queue status`.
// The Work dashboard owns the same flag on `pop work dashboard`; the hidden
// `pop queue dashboard` alias shares workDashboardIncludeDone.
var queueStatusIncludeDone bool

func init() {
	rootCmd.AddCommand(queueCmd)
	queueCmd.AddCommand(queueRunCmd)
	queueCmd.AddCommand(queueStatusCmd)
	queueCmd.AddCommand(queueDashboardCmd)
	queueCmd.AddCommand(queueLogCmd)

	queueStatusCmd.Flags().BoolVar(&queueStatusIncludeDone, "include-done", false, "include DONE task sets (hidden by default)")
	queueDashboardCmd.Flags().BoolVar(&workDashboardIncludeDone, "include-done", false, "include DONE task sets (hidden by default)")
}

var (
	queueConfigLoad     = config.Load
	queueRun            = queue.Run
	queueBuildStatus    = queue.BuildStatus
	queueBuildDashboard = queue.BuildDashboard
	queueRunDashboard   = dashboardshell.RunFromQueue
)

const queueLogLimit = 50

func runQueueRun(cmd *cobra.Command, args []string) error {
	cfgPath := cfgFile
	if cfgPath == "" {
		cfgPath = config.DefaultConfigPath()
	}
	cfg, err := queueConfigLoad(cfgPath)
	if err != nil {
		return err
	}
	resolved, err := cfg.ResolveQueue()
	if err != nil {
		return err
	}

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)
	defer signal.Stop(sigCh)

	err = queueRun(queue.DefaultDeps(), resolved.PollInterval, os.Stdout, sigCh)
	if err != nil {
		var exitErr *tasks.ExitError
		if errors.As(err, &exitErr) {
			if exitErr.Err != nil {
				fmt.Fprintln(os.Stderr, exitErr.Err)
			}
			os.Exit(exitErr.Code)
		}
		return err
	}
	return nil
}

func runQueueStatus(cmd *cobra.Command, args []string) error {
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
	d.IncludeDone = queueStatusIncludeDone
	snap, err := queueBuildStatus(d, cfg)
	if err != nil {
		return err
	}
	// The task-set table is the Work dashboard's rows (ADR-0121): status and the
	// dashboard share one row builder and one comparator, so BuildDashboard yields
	// the same rows, filter, and sort the dashboard renders.
	dash, err := queueBuildDashboard(d, cfg)
	if err != nil {
		return err
	}
	queue.RenderStatus(os.Stdout, snap, dash.Rows)
	return nil
}

func runQueueLog(cmd *cobra.Command, args []string) error {
	events, err := queue.BuildLog(tasks.DefaultDeps())
	if err != nil {
		return err
	}
	queue.RenderLog(os.Stdout, events, queueLogLimit)
	return nil
}
