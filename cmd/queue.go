package cmd

import (
	"errors"
	"fmt"
	"os"
	"os/signal"
	"syscall"

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
every registered project and spawns a drain (pop tasks implement <set> --yes)
into a pop-queue window in that project's tmux session for each idle project
with a Ready task set. Execution is concurrent across projects and serial
within each (enforced by the runtime execution lock). Ctrl-C stops the
supervisor; in-flight drains keep running in their panes.`,
}

var queueRunCmd = &cobra.Command{
	Use:   "run",
	Short: "Run the foreground supervisor loop",
	Args:  cobra.NoArgs,
	RunE:  runQueueRun,
}

func init() {
	rootCmd.AddCommand(queueCmd)
	queueCmd.AddCommand(queueRunCmd)
}

func runQueueRun(cmd *cobra.Command, args []string) error {
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)
	defer signal.Stop(sigCh)

	err := queue.Run(queue.DefaultDeps(), queue.PollInterval, os.Stdout, sigCh)
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
