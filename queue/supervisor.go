package queue

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/glebglazov/pop/config"
	"github.com/glebglazov/pop/tasks"
	"github.com/glebglazov/pop/tasks/binding"
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
	if _, err := d.LoadConfig(config.DefaultConfigPath()); err != nil {
		return err
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
	if err := recordPinnedQuotaCooldowns(d, cfg, decisions); err != nil {
		fmt.Fprintf(out, "queue: record pinned quota cooldowns: %v\n", err)
	} else {
		decisions, err = Scan(d, cfg)
		if err != nil {
			runOut.emitScanError(out, fmt.Sprintf("queue: scan: %v", err))
			return
		}
	}

	if snap, err := BuildStatus(d, cfg); err == nil {
		preSpawn := BuildRunView(snap, time.Now())
		runOut.emitViewTransition(out, preSpawn, nil)
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
				continue
			}
			if err := recordDrainPane(d, dec, spawn.PaneID, "supervisor"); err != nil {
				fmt.Fprintf(out, "queue: %s: record drain pane %s: %v\n", dec.Project, dec.TaskSetID, err)
			}
			fmt.Fprintf(out, "queue: %s: spawned drain for %s\n", dec.Project, dec.TaskSetID)
		}
	}

	if snap, err := BuildStatus(d, cfg); err == nil {
		runOut.emitPostSpawnView(out, BuildRunView(snap, time.Now()))
	}
}

// prepareWorktreeDrain routes a worktree-ready actionable drain to its checkout.
// A bound set resumes at its bound worktree; an unbound set stays on the
// representative checkout — routing never provisions, so the repo's unbound
// Ready sets all land on one checkout and serialize on its runtime execution
// lock instead of fanning into separate worktrees (ADR-0052).
func prepareWorktreeDrain(d *Deps, out io.Writer, dec Decision) Decision {
	if !dec.Actionable() || !dec.WorktreeReady {
		return dec
	}
	route, err := binding.RouteDrainCheckout(binding.RouteDrainCheckoutRequest{
		TD:              d.Tasks,
		CurrentCheckout: dec.scan.ProjectPath,
		SetID:           dec.TaskSetID,
		Trigger:         binding.TriggerQueueSpawn,
	})
	if err != nil {
		if errors.Is(err, binding.ErrBoundWorktreeInvalid) {
			fmt.Fprintf(out, "queue: %s: bound worktree for %s is invalid (%v); repair git state or run `pop tasks unbind-worktree`\n", dec.Project, dec.TaskSetID, err)
			dec.TaskSetID = ""
			dec.Reason = "bound worktree invalid"
			return dec
		}
		fmt.Fprintf(out, "queue: %s: route drain for %s: %v\n", dec.Project, dec.TaskSetID, err)
		dec.TaskSetID = ""
		dec.Reason = "route"
		return dec
	}
	dec.scan.ProjectPath = route.RuntimePath
	dec.scan.RuntimePath = route.RuntimePath
	return dec
}

func validateBoundWorktree(d *Deps, projectPath string, b WorktreeBinding) error {
	return binding.ValidateBoundWorktree(d.Tasks, projectPath, b)
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

// recordPinnedQuotaCooldowns records the per-set pinned-agent quota cooldown for
// any idle set whose latest terminal Drain quota-paused on a pinned agent. A
// pinned set cannot fall back to another agent, so it must wait out the pinned
// preset's reset; that wait is a daemon-state SetBackoff keyed by scoped key
// (the agent-global cooldown in the store covers the fallback chain — a
// different axis, ADR-0055/0056). The cooldown instant is derived from the
// Drain's terminal (its reset instant, or its finish time plus the configured
// retry-after) so re-observing the same terminal across ticks is idempotent: an
// elapsed cooldown is never re-written, so it stops blocking the set.
func recordPinnedQuotaCooldowns(d *Deps, cfg *config.Config, decisions []Decision) error {
	qcfg, err := resolvedQueueConfig(cfg)
	if err != nil {
		return err
	}
	state, err := EnsureDaemonState(d.Tasks)
	if err != nil {
		return err
	}
	now := time.Now().UTC()
	changed := false
	for _, dec := range decisions {
		if dec.Busy || dec.scan.RuntimePath == "" {
			continue
		}
		rec, err := d.readOutcome(dec.scan.RuntimePath)
		if err != nil || rec == nil || rec.SetID == "" {
			continue
		}
		if rec.Outcome != tasks.DrainOutcomeQuotaPaused || !rec.ExhaustedPinned || rec.ExhaustedPreset == "" {
			continue
		}
		until := agentQuotaCooldownUntil(rec.ExhaustedResetAt, rec.WrittenAt, qcfg.AgentQuotaRetryAfter)
		if !until.After(now) {
			continue
		}
		scopedKey, err := scopedKeyForPaths(d, dec.scan.ProjectPath, rec.RuntimePath, rec.SetID)
		if err != nil {
			return err
		}
		if state.SetBackoffs == nil {
			state.SetBackoffs = map[string]time.Time{}
		}
		if existing, ok := state.SetBackoffs[scopedKey]; ok && existing.Equal(until) {
			continue
		}
		state.SetBackoffs[scopedKey] = until
		changed = true
	}
	if changed {
		return WriteDaemonState(d.Tasks, state)
	}
	return nil
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
