package queue

import (
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/glebglazov/pop/config"
)

// PollInterval is the supervisor's scan cadence. Hardcoded for this slice;
// configuration comes later.
const PollInterval = 60 * time.Second

// Run starts the foreground supervisor loop: it acquires the single-instance
// lock, then every poll interval scans every registered project and spawns a
// drain into tmux for each idle project with a Ready set. It returns when a
// signal arrives on sigCh (graceful shutdown) — in-flight drains are
// tmux-owned panes and keep running. A second `pop queue run` while one holds
// the lock is refused before the loop starts.
func Run(d *Deps, interval time.Duration, out io.Writer, sigCh <-chan os.Signal) error {
	lock, err := AcquireSupervisorLock(d.Tasks)
	if err != nil {
		return err
	}
	defer func() { _ = lock.Release() }()

	fmt.Fprintf(out, "pop queue supervisor started (PID %d); poll every %s. Ctrl-C to stop.\n", os.Getpid(), interval)

	for {
		tick(d, out)

		select {
		case <-sigCh:
			fmt.Fprintln(out, "\nShutting down supervisor; in-flight drains keep running in their panes.")
			return nil
		case <-time.After(interval):
		}
	}
}

// tick performs one scan-and-spawn pass across all registered projects. Errors
// resolving or spawning a single project are reported and skipped; one bad
// project never halts the supervisor.
func tick(d *Deps, out io.Writer) {
	cfg, err := d.LoadConfig(config.DefaultConfigPath())
	if err != nil {
		fmt.Fprintf(out, "queue: load config: %v\n", err)
		return
	}

	decisions, err := Scan(d, cfg)
	if err != nil {
		fmt.Fprintf(out, "queue: scan: %v\n", err)
		return
	}

	for _, dec := range decisions {
		switch {
		case dec.Err != nil:
			fmt.Fprintf(out, "queue: %s: %v\n", dec.Project, dec.Err)
		case dec.Actionable():
			if err := Spawn(d, dec); err != nil {
				fmt.Fprintf(out, "queue: %s: spawn %s: %v\n", dec.Project, dec.TaskSetID, err)
				continue
			}
			fmt.Fprintf(out, "queue: %s: spawned drain for %s\n", dec.Project, dec.TaskSetID)
		}
	}
}

// splitLines splits tmux output into non-empty lines.
func splitLines(s string) []string {
	var lines []string
	for _, line := range strings.Split(s, "\n") {
		if strings.TrimSpace(line) != "" {
			lines = append(lines, line)
		}
	}
	return lines
}
