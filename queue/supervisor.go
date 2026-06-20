package queue

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/glebglazov/pop/config"
	"github.com/glebglazov/pop/tasks"
)

const agentQuotaResetSkew = 2 * time.Minute
const maxAgentQuotaResetHorizon = 8 * 24 * time.Hour

// Run starts the foreground supervisor loop: it acquires the single-instance
// lock, then every poll interval scans every registered project and spawns a
// drain into tmux for each idle project with a Ready set. It returns when a
// signal arrives on sigCh (graceful shutdown) — in-flight drains are
// tmux-owned panes and keep running. A second `pop queue run` while one holds
// the lock is refused before the loop starts.
func Run(d *Deps, interval time.Duration, out io.Writer, sigCh <-chan os.Signal) error {
	out, supervisorLog, err := supervisorOutput(d.Tasks, out)
	if err != nil {
		return err
	}
	defer supervisorLog.Close()

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

	output := newRunOutputState()
	for {
		tick(d, out, output)

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
func tick(d *Deps, out io.Writer, runOut *runOutputState) {
	cfg, err := d.LoadConfig(config.DefaultConfigPath())
	if err != nil {
		runOut.emitScanError(out, fmt.Sprintf("queue: load config: %v", err))
		return
	}

	decisions, err := Scan(d, cfg)
	if err != nil {
		runOut.emitScanError(out, fmt.Sprintf("queue: scan: %v", err))
		return
	}
	runOut.lastScan = ""
	var eventLines []string
	if err := recordTerminalOutcomes(d, cfg, decisions, &eventLines); err != nil {
		fmt.Fprintf(out, "queue: journal outcomes: %v\n", err)
	} else {
		decisions, err = Scan(d, cfg)
		if err != nil {
			runOut.emitScanError(out, fmt.Sprintf("queue: scan: %v", err))
			return
		}
	}

	if snap, err := BuildStatus(d, cfg); err == nil {
		preSpawn := BuildRunView(snap, time.Now())
		runOut.emitViewTransition(out, preSpawn, eventLines)
	} else {
		fmt.Fprintf(out, "queue: status: %v\n", err)
	}

	inPlaceFallbackSpawned := map[string]bool{}
	for _, dec := range decisions {
		switch {
		case dec.Err != nil:
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
			spawn, err := SpawnWithResult(d, dec)
			if err != nil {
				fmt.Fprintf(out, "queue: %s: spawn %s: %v\n", dec.Project, dec.TaskSetID, err)
				if journalErr := AppendJournalEntry(d.Tasks, JournalEntry{
					Event:       JournalEventSpawnFailed,
					Project:     dec.Project,
					SetID:       dec.TaskSetID,
					RuntimePath: dec.scan.RuntimePath,
					Source:      "supervisor",
					Reason:      err.Error(),
				}); journalErr != nil {
					fmt.Fprintf(out, "queue: %s: journal spawn failure %s: %v\n", dec.Project, dec.TaskSetID, journalErr)
				}
				continue
			}
			if err := recordDrainPane(d, dec, spawn.PaneID, "supervisor"); err != nil {
				fmt.Fprintf(out, "queue: %s: record drain pane %s: %v\n", dec.Project, dec.TaskSetID, err)
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

	if snap, err := BuildStatus(d, cfg); err == nil {
		runOut.emitPostSpawnView(out, BuildRunView(snap, time.Now()))
	}
}

func prepareWorktreeDrain(d *Deps, out io.Writer, dec Decision) Decision {
	if !dec.Actionable() || !dec.WorktreeReady {
		return dec
	}
	state, err := EnsureDaemonState(d.Tasks)
	if err != nil {
		fmt.Fprintf(out, "queue: %s: load daemon state for %s: %v\n", dec.Project, dec.TaskSetID, err)
		dec.TaskSetID = ""
		dec.Reason = "daemon state"
		return dec
	}
	repoKey, err := resolveRepoKey(d, dec.scan.ProjectPath)
	if err != nil {
		fmt.Fprintf(out, "queue: %s: resolve repository for %s: %v\n", dec.Project, dec.TaskSetID, err)
		dec.TaskSetID = ""
		dec.Reason = "repo"
		return dec
	}
	key := setScopedKey(repoKey, dec.TaskSetID)
	if binding, ok := state.WorktreeBindings[key]; ok {
		if err := validateBoundWorktree(d, dec.scan.ProjectPath, binding); err != nil {
			fmt.Fprintf(out, "queue: %s: bound worktree for %s is invalid (%v); repair git state or run `pop tasks unbind-worktree`\n", dec.Project, dec.TaskSetID, err)
			dec.TaskSetID = ""
			dec.Reason = "bound worktree invalid"
			return dec
		}
		dec.scan.ProjectPath = binding.RuntimePath
		dec.scan.RuntimePath = binding.RuntimePath
		return dec
	}
	wt, err := provisionWorktree(d, dec.scan.ProjectPath, dec.TaskSetID)
	if err != nil {
		fmt.Fprintf(out, "queue: %s: provision worktree for %s: %v; falling back to in-place drain\n", dec.Project, dec.TaskSetID, err)
		return dec
	}
	if state.WorktreeBindings == nil {
		state.WorktreeBindings = map[string]WorktreeBinding{}
	}
	state.WorktreeBindings[key] = WorktreeBinding{
		RuntimePath: wt.Path,
		Branch:      wt.Branch,
		Project:     dec.Project,
		Provisioned: true,
	}
	if err := WriteDaemonState(d.Tasks, state); err != nil {
		fmt.Fprintf(out, "queue: %s: record worktree binding for %s: %v\n", dec.Project, dec.TaskSetID, err)
		dec.TaskSetID = ""
		dec.Reason = "daemon state"
		return dec
	}
	dec.scan.ProjectPath = wt.Path
	dec.scan.RuntimePath = wt.Path
	return dec
}

func validateBoundWorktree(d *Deps, projectPath string, binding WorktreeBinding) error {
	if d == nil || d.Tasks == nil {
		return fmt.Errorf("missing task dependencies")
	}
	path := strings.TrimSpace(binding.RuntimePath)
	if path == "" {
		return fmt.Errorf("binding has no runtime path")
	}
	if _, err := d.Tasks.FS.Stat(path); err != nil {
		return fmt.Errorf("checkout missing: %w", err)
	}
	registered, err := worktreeRegistered(d, projectPath, path)
	if err != nil {
		return err
	}
	if !registered {
		return fmt.Errorf("checkout %s is not registered with git", path)
	}
	return nil
}

func worktreeRegistered(d *Deps, projectPath, checkoutPath string) (bool, error) {
	out, err := d.Tasks.Git.CommandInDir(projectPath, "worktree", "list", "--porcelain")
	if err != nil {
		return false, fmt.Errorf("list worktrees: %w", err)
	}
	canonCheckout, err := canonicalCheckoutPath(d.Tasks, checkoutPath)
	if err != nil {
		return false, fmt.Errorf("canonicalize checkout: %w", err)
	}
	for _, line := range strings.Split(out, "\n") {
		if !strings.HasPrefix(line, "worktree ") {
			continue
		}
		wtPath := strings.TrimSpace(strings.TrimPrefix(line, "worktree "))
		canonWT, err := canonicalCheckoutPath(d.Tasks, wtPath)
		if err != nil {
			continue
		}
		if canonWT == canonCheckout {
			return true, nil
		}
	}
	return false, nil
}

func canonicalCheckoutPath(d *tasks.Deps, path string) (string, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	return d.FS.EvalSymlinks(abs)
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

func recordTerminalOutcomes(d *Deps, cfg *config.Config, decisions []Decision, eventLines *[]string) error {
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
					appendRunEvent(eventLines, formatOutcomeDelta(dec.Project, rec.SetID, rec.Outcome))
					if err := applyTerminalOutcomeState(d, state, qcfg, dec.Project, dec.scan.ProjectPath, rec.RuntimePath, rec.SetID, rec.Outcome, eventLines); err != nil {
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
						if err := recordMergeability(d, state, dec.scan.ProjectPath, merge); err != nil {
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
							scopedKey, err := scopedKeyForPaths(d, dec.scan.ProjectPath, rec.RuntimePath, rec.SetID)
							if err != nil {
								return err
							}
							if _, err := integrateCleanSet(d, cfg, scopedKey, merge, io.Discard, "auto"); err != nil {
								return err
							}
						}
					}
					if rec.Outcome == tasks.DrainOutcomeQuotaPaused && rec.ExhaustedPreset != "" {
						until := agentQuotaCooldownUntil(rec.ExhaustedResetAt, time.Now().UTC(), qcfg.AgentQuotaRetryAfter)
						if rec.ExhaustedPinned {
							scopedKey, err := scopedKeyForPaths(d, dec.scan.ProjectPath, rec.RuntimePath, rec.SetID)
							if err != nil {
								return err
							}
							if state.SetBackoffs == nil {
								state.SetBackoffs = map[string]time.Time{}
							}
							state.SetBackoffs[scopedKey] = until
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
				appendRunEvent(eventLines, formatOutcomeDelta(entry.Project, entry.SetID, DrainOutcomeCrashed))
				if err := applyTerminalOutcomeState(d, state, qcfg, entry.Project, dec.scan.ProjectPath, entry.RuntimePath, entry.SetID, DrainOutcomeCrashed, eventLines); err != nil {
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

func recordMergeability(d *Deps, state *DaemonState, projectPath string, rec MergeabilityRecord) error {
	if state == nil || rec.RuntimePath == "" || rec.SetID == "" {
		return nil
	}
	if rec.CheckedAt.IsZero() {
		rec.CheckedAt = time.Now().UTC()
	}
	scopedKey, err := scopedKeyForPaths(d, projectPath, rec.RuntimePath, rec.SetID)
	if err != nil {
		return err
	}
	if state.Mergeability == nil {
		state.Mergeability = map[string]MergeabilityRecord{}
	}
	state.Mergeability[scopedKey] = rec
	return WriteDaemonState(d.Tasks, state)
}

func autoMergeCleanEnabled(d *Deps, repoRoot string) bool {
	cfg, err := loadRepoConfig(d, repoRoot)
	return err == nil && cfg.AutoMergeClean
}

func applyTerminalOutcomeState(d *Deps, state *DaemonState, qcfg config.ResolvedQueueConfig, project, projectPath, runtimePath, setID string, outcome tasks.DrainOutcome, eventLines *[]string) error {
	if state == nil || runtimePath == "" || setID == "" {
		return nil
	}
	scopedKey, err := scopedKeyForPaths(d, projectPath, runtimePath, setID)
	if err != nil {
		return err
	}
	key := scopedKey
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
			appendRunEvent(eventLines, fmt.Sprintf("queue: %s: %s parked reason=%s", project, setID, "repeated abnormal drain exits"))
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

func appendRunEvent(lines *[]string, line string) {
	if lines == nil || line == "" {
		return
	}
	*lines = append(*lines, line)
}

func agentQuotaCooldownUntil(resetAt, now time.Time, fallback time.Duration) time.Time {
	now = now.UTC()
	if resetAt.IsZero() {
		return now.Add(fallback)
	}
	resetAt = resetAt.UTC()
	if !resetAt.After(now) || resetAt.Sub(now) > maxAgentQuotaResetHorizon {
		return now.Add(fallback)
	}
	return resetAt.Add(agentQuotaResetSkew)
}

func drainOutcomeAbnormal(outcome tasks.DrainOutcome) bool {
	return outcome == DrainOutcomeCrashed || outcome.Abnormal()
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
