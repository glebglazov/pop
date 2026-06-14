package queue

import (
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/glebglazov/pop/config"
)

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

	if _, err := EnsureDaemonState(d.Tasks); err != nil {
		return err
	}
	cfg, err := d.LoadConfig(config.DefaultConfigPath())
	if err != nil {
		return err
	}
	if err := ReconcileInFlight(d, cfg); err != nil {
		fmt.Fprintf(out, "queue: reconcile in-flight drains: %v\n", err)
	}

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
	if err := recordTerminalOutcomes(d, decisions); err != nil {
		fmt.Fprintf(out, "queue: journal outcomes: %v\n", err)
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
			if err := AppendJournalEntry(d.Tasks, JournalEntry{
				Event:       JournalEventSpawn,
				Project:     dec.Project,
				SetID:       dec.TaskSetID,
				RuntimePath: dec.scan.RuntimePath,
				Source:      "supervisor",
			}); err != nil {
				fmt.Fprintf(out, "queue: %s: journal spawn %s: %v\n", dec.Project, dec.TaskSetID, err)
			}
			fmt.Fprintf(out, "queue: %s: spawned drain for %s\n", dec.Project, dec.TaskSetID)
		}
	}
}

// ReconcileInFlight records open spawn entries for live runtime locks observed
// when a supervisor starts after a restart.
func ReconcileInFlight(d *Deps, cfg *config.Config) error {
	decisions, err := Scan(d, cfg)
	if err != nil {
		return err
	}
	entries, err := ReadJournal(d.Tasks)
	if err != nil {
		return err
	}
	for _, dec := range decisions {
		if !dec.Busy || dec.lockStatus == nil || dec.lockStatus.Metadata == nil || dec.lockStatus.Metadata.SetID == "" {
			continue
		}
		meta := dec.lockStatus.Metadata
		if journalHasOpenSpawn(entries, dec.Project, meta.RuntimePath, meta.SetID) {
			continue
		}
		if err := AppendJournalEntry(d.Tasks, JournalEntry{
			Event:       JournalEventSpawn,
			Project:     dec.Project,
			SetID:       meta.SetID,
			RuntimePath: meta.RuntimePath,
			PID:         meta.PID,
			Source:      "reconcile",
		}); err != nil {
			return err
		}
	}
	return nil
}

func recordTerminalOutcomes(d *Deps, decisions []Decision) error {
	entries, err := ReadJournal(d.Tasks)
	if err != nil {
		return err
	}
	for _, dec := range decisions {
		if dec.Busy || dec.scan.RuntimePath == "" {
			continue
		}
		rec, err := d.readOutcome(dec.scan.RuntimePath)
		if err == nil && rec != nil && rec.SetID != "" {
			if !journalHasOutcome(entries, dec.Project, rec.RuntimePath, rec.SetID, rec.Outcome, rec.WrittenAt) {
				ts := rec.WrittenAt
				entry := JournalEntry{
					Timestamp:   ts,
					Event:       JournalEventOutcome,
					Project:     dec.Project,
					SetID:       rec.SetID,
					RuntimePath: rec.RuntimePath,
					Outcome:     rec.Outcome,
					PID:         rec.PID,
				}
				if err := AppendJournalEntry(d.Tasks, entry); err != nil {
					return err
				}
				entries = append(entries, entry)
			}
		}
		for _, entry := range entries {
			if entry.Project != dec.Project || entry.RuntimePath != dec.scan.RuntimePath || entry.Event != JournalEventSpawn {
				continue
			}
			if journalHasOpenSpawn(entries, entry.Project, entry.RuntimePath, entry.SetID) && !journalHasOutcome(entries, entry.Project, entry.RuntimePath, entry.SetID, DrainOutcomeCrashed, time.Time{}) {
				outcome := JournalEntry{
					Event:       JournalEventOutcome,
					Project:     entry.Project,
					SetID:       entry.SetID,
					RuntimePath: entry.RuntimePath,
					Outcome:     DrainOutcomeCrashed,
				}
				if err := AppendJournalEntry(d.Tasks, outcome); err != nil {
					return err
				}
				entries = append(entries, outcome)
			}
		}
	}
	return nil
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
