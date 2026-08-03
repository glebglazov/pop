package cmd

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"syscall"

	"github.com/glebglazov/pop/config"
	"github.com/glebglazov/pop/dashboard"
	"github.com/glebglazov/pop/dashboardshell"
	"github.com/glebglazov/pop/supervisor"
	"github.com/glebglazov/pop/tasks"
	"github.com/glebglazov/pop/tasks/drain"
	"github.com/glebglazov/pop/work"
	"github.com/spf13/cobra"
)

// The three daemon-facing `pop work` verbs. `pop queue` is gone with no alias:
// the supervisor advances every Work kind, so the command that runs it, the one
// that reports what it can advance, and the one that replays what it did all live
// under `work`.

// workDaemonCmd runs the supervisor in the foreground. It is deliberately the
// only way to start one (ADR-0027): no picker path auto-starts it, and there are
// no service-management verbs — the operator parks it in a pane and Ctrl-C stops
// it.
var workDaemonCmd = &cobra.Command{
	Use:   "daemon",
	Short: "Run the foreground supervisor loop",
	Args:  cobra.NoArgs,
	RunE:  runWorkDaemon,
}

var workStatusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show what the daemon can advance, from on-disk state",
	Args:  cobra.NoArgs,
	RunE:  runWorkStatus,
}

var workLogCmd = &cobra.Command{
	Use:   "log",
	Short: "Show recent daemon journal history",
	Args:  cobra.NoArgs,
	RunE:  runWorkLog,
}

// workStatusIncludeDone backs the `--include-done` flag on `pop work status`.
// The Work dashboard owns the same flag on `pop work dashboard`.
var workStatusIncludeDone bool

func init() {
	workCmd.AddCommand(workDaemonCmd)
	workCmd.AddCommand(workStatusCmd)
	workCmd.AddCommand(workLogCmd)

	workStatusCmd.Flags().BoolVar(&workStatusIncludeDone, "include-done", false, "include DONE task sets (hidden by default)")
}

var (
	workConfigLoad  = config.Load
	supervisorRun   = supervisor.Run
	workBuildStatus = drain.BuildStatus
	// workBuildStatusTables builds the two tables `pop work status` prints through
	// the work data core (ADR-0143): the command surface is a consumer of
	// work.BuildSnapshot, so status renders the same rows the dashboard's two pages
	// derive — page A's, then page B's.
	workBuildStatusTables = func(d *drain.Deps, cfg *config.Config) (dashboard.StatusTables, error) {
		setKinds := d.WorkKinds(cfg)
		sets, err := work.BuildSnapshot(setKinds)
		if err != nil {
			return dashboard.StatusTables{}, err
		}
		routineKinds := d.RoutinePageKinds(cfg)
		routines, err := work.BuildSnapshot(routineKinds)
		if err != nil {
			return dashboard.StatusTables{}, err
		}
		return dashboard.StatusTables{
			TaskSets: dashboard.StatusTable{Kinds: setKinds, Rows: sets.Containers},
			Routines: dashboard.StatusTable{Kinds: routineKinds, Rows: routines.Containers},
		}, nil
	}
	workRunDashboard = dashboardshell.RunFromQueue
)

const workLogLimit = 50

// cmdOut is the writer a read verb prints to: the command's own, so a test reads
// what the operator would see, falling back to stdout for a direct call.
func cmdOut(cmd *cobra.Command) io.Writer {
	if cmd == nil {
		return os.Stdout
	}
	return cmd.OutOrStdout()
}

func runWorkDaemon(cmd *cobra.Command, args []string) error {
	cfgPath := cfgFile
	if cfgPath == "" {
		cfgPath = config.DefaultConfigPath()
	}
	cfg, err := workConfigLoad(cfgPath)
	if err != nil {
		return err
	}
	resolved, err := cfg.ResolveWorkDaemon()
	if err != nil {
		return err
	}

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)
	defer signal.Stop(sigCh)

	err = supervisorRun(cmdLayerDeps().queueDeps(), resolved.PollInterval, os.Stdout, sigCh)
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

func runWorkStatus(cmd *cobra.Command, args []string) error {
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
	d.IncludeDone = workStatusIncludeDone
	// The verb is one load across two builders — the status snapshot and the two
	// tables — so the git memo is derived once here and threaded through both.
	// Each builder memoizes for itself as well; nesting is free, and what it buys
	// is that the common dir the tables resolve is the fork the snapshot already
	// paid for.
	d = d.WithGitMemo()
	snap, err := workBuildStatus(d, cfg)
	if err != nil {
		return err
	}
	// The task-set table is the Work dashboard's rows (ADR-0121): status and the
	// dashboard share one row builder and one comparator, so the page snapshots
	// yield the same rows, filter, and sort the dashboard renders. Map rows are the
	// one deliberate exception, dropped by the render because a Map never advances.
	tables, err := workBuildStatusTables(d, cfg)
	if err != nil {
		return err
	}
	dashboard.RenderStatus(cmdOut(cmd), snap, tables)
	return nil
}

func runWorkLog(cmd *cobra.Command, args []string) error {
	events, err := supervisor.BuildLog(cmdLayerDeps().tasksDeps())
	if err != nil {
		return err
	}
	supervisor.RenderLog(cmdOut(cmd), events, workLogLimit)
	return nil
}
