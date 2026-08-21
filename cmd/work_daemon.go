package cmd

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/glebglazov/pop/config"
	"github.com/glebglazov/pop/dashboard"
	"github.com/glebglazov/pop/dashboardshell"
	"github.com/glebglazov/pop/supervisor"
	"github.com/glebglazov/pop/tasks"
	"github.com/glebglazov/pop/tasks/drain"
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

// workLogSevere backs `--severe` on `pop work log`: the short list a human wants
// on coming back to the machine — only the events that spent an agent's whole
// retry budget or a drain's whole agent list (ADR-0231).
var workLogSevere bool

// workLogSince backs `--severe`'s window. It defaults to a day because that is
// the span of an unattended night.
var workLogSince time.Duration

// workStatusIncludeDone backs the deprecated `--include-done` flag on
// `pop work status` — an alias for `--preset all` (ADR-0197).
var workStatusIncludeDone bool

// workStatusPreset backs the `--preset` flag on `pop work status`. Empty means
// the configured default preset.
var workStatusPreset string

func init() {
	workCmd.AddCommand(workDaemonCmd)
	workCmd.AddCommand(workStatusCmd)
	workCmd.AddCommand(workLogCmd)

	workLogCmd.Flags().BoolVar(&workLogSevere, "severe", false, "Show only severe events — an agent that burned its whole retry cap without finishing, a drain that spent its whole agent list, or one that could not start an agent at all")
	workLogCmd.Flags().DurationVar(&workLogSince, "since", 24*time.Hour, "Window the --severe listing covers")

	workStatusCmd.Flags().StringVar(&workStatusPreset, "preset", "", "Work view preset name (default: first configured preset)")
	workStatusCmd.Flags().BoolVar(&workStatusIncludeDone, "include-done", false, "deprecated: alias for --preset all")
	_ = workStatusCmd.RegisterFlagCompletionFunc("preset", completeWorkStatusPreset)
}

// completeWorkStatusPreset lists the resolved Work view preset roster: the
// user's configured presets, or the shipped roster when none are declared.
func completeWorkStatusPreset(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	cfgPath := cfgFile
	if cfgPath == "" {
		cfgPath = config.DefaultConfigPath()
	}
	cfg, err := workConfigLoad(cfgPath)
	if err != nil {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}
	return filterShellCompletions(cfg.WorkViewPresetNames(), toComplete), cobra.ShellCompDirectiveNoFileComp
}

var (
	workConfigLoad  = config.Load
	supervisorRun   = supervisor.Run
	workBuildStatus = drain.BuildStatus
	// workBuildStatusTables builds the two tables `pop work status` prints. The
	// builder lives in the dashboard package because the daemon's run baseline
	// prints the same two tables through it; the var is the test seam.
	workBuildStatusTables = dashboard.BuildStatusTables
	workRunDashboard      = dashboardshell.RunFromQueue
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

	d := cmdLayerDeps().queueDeps()
	// The daemon's run baseline is the shipped active definition on every
	// machine — never the human's configured default (ADR-0197).
	if p, ok := config.ShippedWorkViewPreset("active"); ok {
		d.ViewPreset = p
	}
	err = supervisorRun(d, resolved.PollInterval, os.Stdout, sigCh)
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
	preset, err := resolveWorkStatusPreset(cfg, workStatusPreset, workStatusIncludeDone, cmd.ErrOrStderr())
	if err != nil {
		return err
	}
	d := cmdLayerDeps().queueDeps()
	d.LoadConfig = workConfigLoad
	d.ViewPreset = preset
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

// resolveWorkStatusPreset picks the Work view preset for `pop work status`
// (ADR-0197): `--include-done` is a deprecated alias for `--preset all`; an
// explicit `--preset` names an entry in the resolved roster; otherwise the
// configured default (first entry) is used.
func resolveWorkStatusPreset(cfg *config.Config, presetName string, includeDone bool, warn io.Writer) (config.WorkViewPreset, error) {
	if includeDone {
		if warn != nil {
			fmt.Fprintln(warn, "warning: --include-done is deprecated; use --preset all")
		}
		if p, ok := config.ShippedWorkViewPreset("all"); ok {
			return p, nil
		}
		if cfg != nil {
			if p, ok := cfg.WorkViewPresetNamed("all"); ok {
				return p, nil
			}
		}
		return config.WorkViewPreset{Name: "all", WorkViewPresetFilter: config.WorkViewPresetFilter{Archived: config.ArchivedInclude}}, nil
	}
	if name := strings.TrimSpace(presetName); name != "" {
		if cfg == nil {
			cfg = &config.Config{}
		}
		if p, ok := cfg.WorkViewPresetNamed(name); ok {
			return p, nil
		}
		names := cfg.WorkViewPresetNames()
		return config.WorkViewPreset{}, fmt.Errorf("unknown work view preset %q (available: %s)", name, strings.Join(names, ", "))
	}
	if cfg == nil {
		cfg = &config.Config{}
	}
	return cfg.DefaultWorkViewPreset(), nil
}

func runWorkLog(cmd *cobra.Command, args []string) error {
	events, err := supervisor.BuildLog(cmdLayerDeps().tasksDeps())
	if err != nil {
		return err
	}
	if workLogSevere {
		supervisor.RenderSevereLog(cmdOut(cmd), events, workLogSince, time.Now())
		return nil
	}
	supervisor.RenderLog(cmdOut(cmd), events, workLogLimit)
	return nil
}
