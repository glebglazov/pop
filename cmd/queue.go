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

var queueLogCmd = &cobra.Command{
	Use:   "log",
	Short: "Show recent queue journal history",
	Args:  cobra.NoArgs,
	RunE:  runQueueLog,
}

var queueIntegrateCmd = &cobra.Command{
	Use:   "integrate <set>",
	Short: "Merge a clean completed queue set into its working branch",
	Args:  cobra.ExactArgs(1),
	RunE:  runQueueIntegrate,
}

var queueAbandonCmd = &cobra.Command{
	Use:   "abandon <set>",
	Short: "Release a worktree binding without integrating",
	Args:  cobra.ExactArgs(1),
	RunE:  runQueueAbandon,
}

var queueBindWorktreeCmd = &cobra.Command{
	Use:   "bind-worktree <set>",
	Short: "Adopt the current checkout as the drain worktree for a set",
	Long: `Adopt the current checkout as the drain worktree for a set.

Run from inside the target checkout. Pop will drain the named set into this
checkout without deleting the directory on abandon or integration — only the
binding is forgotten. Use --force to re-point a set that is already bound
elsewhere.`,
	Args: cobra.ExactArgs(1),
	RunE: runQueueBindWorktree,
}

func init() {
	rootCmd.AddCommand(queueCmd)
	queueCmd.AddCommand(queueRunCmd)
	queueCmd.AddCommand(queueStatusCmd)
	queueCmd.AddCommand(queueLogCmd)
	queueCmd.AddCommand(queueIntegrateCmd)
	queueAbandonCmd.Flags().BoolVar(&queueAbandonYes, "yes", false, "Skip confirmation prompt")
	queueCmd.AddCommand(queueAbandonCmd)
	queueBindWorktreeCmd.Flags().BoolVar(&queueBindWorktreeForce, "force", false, "Re-point a set already bound elsewhere")
	queueCmd.AddCommand(queueBindWorktreeCmd)
}

var (
	queueConfigLoad        = config.Load
	queueRun               = queue.Run
	queueIntegrate         = queue.IntegrateWithOptions
	queueAbandon           = queue.AbandonWithOptions
	queueBindWorktree      = queue.BindWorktree
	queueAbandonYes        bool
	queueBindWorktreeForce bool
)

const queueLogLimit = 50

type queueRunConfig struct {
	Agents               []string
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
	if err := validateQueueAgents(resolved.Agents); err != nil {
		return queueRunConfig{}, err
	}
	agents := append([]string(nil), resolved.Agents...)
	if len(agents) == 0 {
		agents = []string{tasks.DefaultAgentPreset}
	}
	return queueRunConfig{
		Agents:               agents,
		PollInterval:         resolved.PollInterval,
		AgentQuotaRetryAfter: resolved.AgentQuotaRetryAfter,
		CrashRetryDelays:     append([]time.Duration(nil), resolved.CrashRetryDelays...),
	}, nil
}

func validateQueueAgents(agents []string) error {
	for i, agent := range agents {
		if _, err := tasks.ResolveAgentAdapter(agent); err != nil {
			return fmt.Errorf("[queue] agents[%d]: %w", i, err)
		}
	}
	return nil
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

func runQueueLog(cmd *cobra.Command, args []string) error {
	entries, err := queue.ReadJournal(tasks.DefaultDeps())
	if err != nil {
		return err
	}
	queue.RenderLog(os.Stdout, entries, queueLogLimit)
	return nil
}

func runQueueIntegrate(cmd *cobra.Command, args []string) error {
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
	_, err = queueIntegrate(d, cfg, args[0], os.Stdout, queue.IntegrationOptions{In: os.Stdin})
	return err
}

func runQueueAbandon(cmd *cobra.Command, args []string) error {
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
	_, err = queueAbandon(d, cfg, args[0], os.Stdout, queue.AbandonOptions{Yes: queueAbandonYes, In: os.Stdin})
	return err
}

func runQueueBindWorktree(cmd *cobra.Command, args []string) error {
	cfgPath := cfgFile
	if cfgPath == "" {
		cfgPath = config.DefaultConfigPath()
	}
	cfg, err := queueConfigLoad(cfgPath)
	if err != nil {
		return err
	}
	cwd, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("determine working directory: %w", err)
	}
	d := queue.DefaultDeps()
	d.LoadConfig = queueConfigLoad
	_, err = queueBindWorktree(d, cfg, args[0], cwd, queue.BindWorktreeOptions{Force: queueBindWorktreeForce}, os.Stdout)
	return err
}
