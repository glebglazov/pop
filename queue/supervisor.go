package queue

import (
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/glebglazov/pop/config"
	"github.com/glebglazov/pop/project"
	"github.com/glebglazov/pop/tasks"
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
	if err := recordTerminalOutcomes(d, cfg, decisions); err != nil {
		fmt.Fprintf(out, "queue: journal outcomes: %v\n", err)
	} else {
		decisions, err = Scan(d, cfg)
		if err != nil {
			fmt.Fprintf(out, "queue: scan: %v\n", err)
			return
		}
	}

	inPlaceFallbackSpawned := map[string]bool{}
	for _, dec := range decisions {
		if err := journalAgentNotes(d, dec); err != nil {
			fmt.Fprintf(out, "queue: %s: journal agent notes: %v\n", dec.Project, err)
		}
		switch {
		case dec.Err != nil:
			fmt.Fprintf(out, "queue: %s: %v\n", dec.Project, dec.Err)
		case dec.Actionable():
			originalRuntimePath := dec.scan.RuntimePath
			dec = prepareWorktreeDrain(d, out, dec)
			if dec.WorktreeReady && dec.scan.RuntimePath == originalRuntimePath {
				if inPlaceFallbackSpawned[dec.Project] {
					fmt.Fprintf(out, "queue: %s: skip in-place fallback for %s; another set already fell back this tick\n", dec.Project, dec.TaskSetID)
					continue
				}
				inPlaceFallbackSpawned[dec.Project] = true
			}
			if err := Spawn(d, dec); err != nil {
				fmt.Fprintf(out, "queue: %s: spawn %s: %v\n", dec.Project, dec.TaskSetID, err)
				continue
			}
			if dec.DefaultAgent != "" {
				if err := AppendJournalEntry(d.Tasks, JournalEntry{
					Event:       JournalEventAgentSwitch,
					Project:     dec.Project,
					SetID:       dec.TaskSetID,
					RuntimePath: dec.scan.RuntimePath,
					Agent:       dec.DefaultAgent,
					Source:      "supervisor",
				}); err != nil {
					fmt.Fprintf(out, "queue: %s: journal agent switch %s: %v\n", dec.Project, dec.TaskSetID, err)
				}
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

func prepareWorktreeDrain(d *Deps, out io.Writer, dec Decision) Decision {
	if !dec.Actionable() || !dec.WorktreeReady {
		return dec
	}
	wt, err := provisionWorktree(d, dec.scan.ProjectPath, dec.TaskSetID)
	if err != nil {
		fmt.Fprintf(out, "queue: %s: provision worktree for %s: %v; falling back to in-place drain\n", dec.Project, dec.TaskSetID, err)
		return dec
	}
	dec.scan.ProjectPath = wt.Path
	dec.scan.RuntimePath = wt.Path
	pd := d.Project
	if pd == nil {
		pd = project.DefaultDeps()
	}
	dec.scan.SessionName = project.SessionNameWith(pd, wt.Path)
	return dec
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

func recordTerminalOutcomes(d *Deps, cfg *config.Config, decisions []Decision) error {
	entries, err := ReadJournal(d.Tasks)
	if err != nil {
		return err
	}
	qcfg, err := resolvedQueueConfig(cfg)
	if err != nil {
		return err
	}
	state, err := EnsureDaemonState(d.Tasks)
	if err != nil {
		return err
	}
	for _, dec := range decisions {
		if dec.Busy || dec.scan.RuntimePath == "" {
			continue
		}
		runtimes := terminalOutcomeRuntimes(entries, dec)
		for _, runtime := range runtimes {
			rec, err := d.readOutcome(runtime.RuntimePath)
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
					if err := applyTerminalOutcomeState(d, state, qcfg, dec.Project, rec.RuntimePath, rec.SetID, rec.Outcome); err != nil {
						return err
					}
					if rec.Outcome == tasks.DrainOutcomeDone {
						merge, err := d.computeMergeability(runtime.WorkingPath, rec.RuntimePath)
						if err != nil {
							return err
						}
						merge.Project = dec.Project
						merge.RuntimePath = rec.RuntimePath
						merge.SetID = rec.SetID
						if err := recordMergeability(d, state, merge); err != nil {
							return err
						}
						mergeEvent := JournalEntry{
							Event:       JournalEventMergeability,
							Project:     dec.Project,
							SetID:       rec.SetID,
							RuntimePath: rec.RuntimePath,
							MergeStatus: merge.Status,
							Target:      merge.Target,
							SourceRef:   merge.Source,
							Source:      "supervisor",
						}
						if err := AppendJournalEntry(d.Tasks, mergeEvent); err != nil {
							return err
						}
						entries = append(entries, mergeEvent)
						if merge.Status == MergeabilityClean && autoMergeCleanEnabled(d, runtime.WorkingPath) {
							if _, err := integrateCleanSet(d, cfg, setBackoffKey(rec.RuntimePath, rec.SetID), merge, io.Discard, "auto"); err != nil {
								return err
							}
						}
					}
					if rec.Outcome == tasks.DrainOutcomeQuotaPaused && rec.ExhaustedPreset != "" {
						until := time.Now().UTC().Add(qcfg.AgentQuotaRetryAfter)
						if state.AgentCooldowns == nil {
							state.AgentCooldowns = map[string]time.Time{}
						}
						state.AgentCooldowns[rec.ExhaustedPreset] = until
						if rec.ExhaustedPinned {
							if state.SetBackoffs == nil {
								state.SetBackoffs = map[string]time.Time{}
							}
							state.SetBackoffs[setBackoffKey(rec.RuntimePath, rec.SetID)] = until
						}
						if err := WriteDaemonState(d.Tasks, state); err != nil {
							return err
						}
						cooldown := JournalEntry{
							Event:       JournalEventAgentCooldown,
							Project:     dec.Project,
							SetID:       rec.SetID,
							RuntimePath: rec.RuntimePath,
							Agent:       rec.ExhaustedPreset,
							Reason:      "quota pause",
							Until:       until,
							Source:      "supervisor",
						}
						if err := AppendJournalEntry(d.Tasks, cooldown); err != nil {
							return err
						}
						entries = append(entries, cooldown)
					}
				}
			}
		}
		for _, entry := range entries {
			if entry.Project != dec.Project || entry.RuntimePath != dec.scan.RuntimePath || entry.Event != JournalEventSpawn {
				continue
			}
			if journalHasOpenSpawn(entries, entry.Project, entry.RuntimePath, entry.SetID) {
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
				if err := applyTerminalOutcomeState(d, state, qcfg, entry.Project, entry.RuntimePath, entry.SetID, DrainOutcomeCrashed); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

type terminalRuntime struct {
	RuntimePath string
	WorkingPath string
}

func terminalOutcomeRuntimes(entries []JournalEntry, dec Decision) []terminalRuntime {
	seen := map[string]bool{}
	add := func(out *[]terminalRuntime, runtimePath string) {
		if runtimePath == "" || seen[runtimePath] {
			return
		}
		seen[runtimePath] = true
		workingPath := dec.scan.ProjectPath
		if workingPath == "" {
			workingPath = dec.scan.RuntimePath
		}
		*out = append(*out, terminalRuntime{RuntimePath: runtimePath, WorkingPath: workingPath})
	}
	var out []terminalRuntime
	add(&out, dec.scan.RuntimePath)
	for _, entry := range entries {
		if entry.Project != dec.Project || entry.Event != JournalEventSpawn || entry.RuntimePath == "" {
			continue
		}
		if journalHasOpenSpawn(entries, entry.Project, entry.RuntimePath, entry.SetID) {
			add(&out, entry.RuntimePath)
		}
	}
	return out
}

func recordMergeability(d *Deps, state *DaemonState, rec MergeabilityRecord) error {
	if state == nil || rec.RuntimePath == "" || rec.SetID == "" {
		return nil
	}
	if rec.CheckedAt.IsZero() {
		rec.CheckedAt = time.Now().UTC()
	}
	if state.Mergeability == nil {
		state.Mergeability = map[string]MergeabilityRecord{}
	}
	state.Mergeability[setBackoffKey(rec.RuntimePath, rec.SetID)] = rec
	return WriteDaemonState(d.Tasks, state)
}

func autoMergeCleanEnabled(d *Deps, repoRoot string) bool {
	cfg, err := loadRepoConfig(d, repoRoot)
	return err == nil && cfg.AutoMergeClean
}

func applyTerminalOutcomeState(d *Deps, state *DaemonState, qcfg config.ResolvedQueueConfig, project, runtimePath, setID string, outcome tasks.DrainOutcome) error {
	if state == nil || runtimePath == "" || setID == "" {
		return nil
	}
	key := setBackoffKey(runtimePath, setID)
	if drainOutcomeAbnormal(outcome) {
		if state.SetCrashCounts == nil {
			state.SetCrashCounts = map[string]int{}
		}
		count := state.SetCrashCounts[key] + 1
		state.SetCrashCounts[key] = count

		if len(qcfg.CrashRetryDelays) == 0 || count > len(qcfg.CrashRetryDelays) {
			if state.SetCrashBackoffs != nil {
				delete(state.SetCrashBackoffs, key)
			}
			if state.ParkedSets == nil {
				state.ParkedSets = map[string]ParkedSet{}
			}
			state.ParkedSets[key] = ParkedSet{
				RuntimePath:              runtimePath,
				SetID:                    setID,
				ParkedAt:                 time.Now().UTC(),
				Reason:                   fmt.Sprintf("exhausted %d crash retry delay(s)", len(qcfg.CrashRetryDelays)),
				ConsecutiveAbnormalExits: count,
			}
			if err := WriteDaemonState(d.Tasks, state); err != nil {
				return err
			}
			return AppendJournalEntry(d.Tasks, JournalEntry{
				Event:       JournalEventSetParked,
				Project:     project,
				SetID:       setID,
				RuntimePath: runtimePath,
				Reason:      "repeated abnormal drain exits",
				Source:      "supervisor",
			})
		}

		if state.SetCrashBackoffs == nil {
			state.SetCrashBackoffs = map[string]time.Time{}
		}
		state.SetCrashBackoffs[key] = time.Now().UTC().Add(qcfg.CrashRetryDelays[count-1])
		if state.ParkedSets != nil {
			delete(state.ParkedSets, key)
		}
		return WriteDaemonState(d.Tasks, state)
	}

	resetCrashState(state, key)
	return WriteDaemonState(d.Tasks, state)
}

func resetCrashState(state *DaemonState, key string) {
	if state.SetCrashCounts != nil {
		delete(state.SetCrashCounts, key)
	}
	if state.SetCrashBackoffs != nil {
		delete(state.SetCrashBackoffs, key)
	}
	if state.ParkedSets != nil {
		delete(state.ParkedSets, key)
	}
}

func drainOutcomeAbnormal(outcome tasks.DrainOutcome) bool {
	return outcome == DrainOutcomeCrashed || outcome.Abnormal()
}

func journalAgentNotes(d *Deps, dec Decision) error {
	for _, note := range dec.AgentNotes {
		event := JournalEventAgentUnavailable
		if note.Event == "agent_cooling" {
			event = JournalEventAgentCooldown
		}
		if err := AppendJournalEntry(d.Tasks, JournalEntry{
			Event:       event,
			Project:     dec.Project,
			SetID:       dec.TaskSetID,
			RuntimePath: dec.scan.RuntimePath,
			Agent:       note.Agent,
			Reason:      note.Reason,
			Until:       note.Until,
			Source:      "supervisor",
		}); err != nil {
			return err
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
