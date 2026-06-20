package cmd

import (
	"errors"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/glebglazov/pop/config"
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

var queueDashboardCmd = &cobra.Command{
	Use:   "dashboard",
	Short: "Open the read-only queue dashboard",
	Args:  cobra.NoArgs,
	RunE:  runQueueDashboard,
}

var queueLogCmd = &cobra.Command{
	Use:   "log",
	Short: "Show recent queue journal history",
	Args:  cobra.NoArgs,
	RunE:  runQueueLog,
}

func init() {
	rootCmd.AddCommand(queueCmd)
	queueCmd.AddCommand(queueRunCmd)
	queueCmd.AddCommand(queueStatusCmd)
	queueCmd.AddCommand(queueDashboardCmd)
	queueCmd.AddCommand(queueLogCmd)
}

var (
	queueConfigLoad = config.Load
	queueRun        = queue.Run
)

const queueLogLimit = 50

type queueRunConfig struct {
	PollInterval         time.Duration
	AgentQuotaRetryAfter time.Duration
	CrashRetryDelays     []time.Duration
}

func resolveQueueRunConfig(loadConfig func(string) (*config.Config, error), path string) (queueRunConfig, error) {
	cfg, err := loadConfig(path)
	if err != nil {
		return queueRunConfig{}, err
	}
	resolved, err := cfg.ResolveQueue()
	if err != nil {
		return queueRunConfig{}, err
	}
	return queueRunConfig{
		PollInterval:         resolved.PollInterval,
		AgentQuotaRetryAfter: resolved.AgentQuotaRetryAfter,
		CrashRetryDelays:     append([]time.Duration(nil), resolved.CrashRetryDelays...),
	}, nil
}

func runQueueRun(cmd *cobra.Command, args []string) error {
	cfgPath := cfgFile
	if cfgPath == "" {
		cfgPath = config.DefaultConfigPath()
	}
	qcfg, err := resolveQueueRunConfig(queueConfigLoad, cfgPath)
	if err != nil {
		return err
	}

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)
	defer signal.Stop(sigCh)

	err = queueRun(queue.DefaultDeps(), qcfg.PollInterval, os.Stdout, sigCh)
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
	snap, err := queue.BuildStatus(d, cfg)
	if err != nil {
		return err
	}
	queue.RenderStatus(os.Stdout, snap)
	return nil
}

func runQueueDashboard(cmd *cobra.Command, args []string) error {
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
	return queue.RunDashboard(d, cfg)
}

func runQueueLog(cmd *cobra.Command, args []string) error {
	entries, err := queue.ReadJournal(tasks.DefaultDeps())
	if err != nil {
		return err
	}
	queue.RenderLog(os.Stdout, entries, queueLogLimit)
	return nil
}
