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

	if _, err := d.LoadConfig(config.DefaultConfigPath()); err != nil {
		return err
	}

	fmt.Fprintf(out, "pop queue supervisor started (PID %d); poll every %s. Ctrl-C to stop.\n", os.Getpid(), interval)
	tasks.WarnProcStartUnsupported(out)

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

	if snap, err := BuildStatus(d, cfg); err == nil {
		preSpawn := BuildRunView(snap, time.Now())
		runOut.emitViewTransition(out, preSpawn, nil)
	} else {
		fmt.Fprintf(out, "queue: status: %v\n", err)
	}

	// Per-checkout dispatch (ADR-0070/0072): each drain routes to its own
	// checkout, so several of a repo's Ready sets dispatch in one tick. Two drains
	// routed to the *same* checkout in one tick would race two implements into it,
	// so the first wins and the rest defer to a later tick — distinct bound and
	// freshly provisioned managed worktrees each dispatch.
	spawnedCheckouts := map[string]bool{}
	var spawned []PickedUpSet
	for _, dec := range decisions {
		switch {
		case dec.Err != nil:
		case dec.Actionable():
			repoLabel := repoLabelFromScan(dec.scan)
			dec = prepareWorktreeDrain(d, out, dec)
			if !dec.Actionable() {
				continue // routing refused (invalid binding, provision failure); already reported.
			}
			if path := dec.scan.RuntimePath; path != "" {
				if spawnedCheckouts[path] {
					fmt.Fprintf(out, "queue: %s: skip %s; another set already dispatched to %s this tick\n", repoLabel, dec.TaskSetID, path)
					continue
				}
				spawnedCheckouts[path] = true
			}
			spawn, err := SpawnWithResult(d, dec)
			if err != nil {
				fmt.Fprintf(out, "queue: %s: spawn %s: %v\n", repoLabel, dec.TaskSetID, err)
				continue
			}
			if err := recordDrainPane(d, dec, spawn.PaneID, "supervisor"); err != nil {
				fmt.Fprintf(out, "queue: %s: record drain pane %s: %v\n", repoLabel, dec.TaskSetID, err)
			}
			label := statusProjectLabel(repoLabel, dec.ProjectConfigError)
			fmt.Fprintf(out, "queue: %s: spawned drain for %s\n", label, dec.TaskSetID)
			spawned = append(spawned, PickedUpSet{Project: dec.Project, RepoLabel: repoLabel, SetID: dec.TaskSetID, ProjectConfigError: dec.ProjectConfigError})
		}
	}

	if snap, err := BuildStatus(d, cfg); err == nil {
		// A just-spawned drain has not yet acquired its runtime lock, so the
		// post-spawn scan still lists its set as Ready, not Running. Seed the
		// spawned sets into the swallow snapshot so next tick's view diff does
		// not re-announce them as freshly "spawned drain" (they were already
		// reported imperatively above). This seeding is display-only — the
		// double-spawn window is closed against the store by the spawn-intent
		// guard, not by this view patch (see seedSpawnedRunning).
		runOut.emitPostSpawnView(out, seedSpawnedRunning(BuildRunView(snap, time.Now()), spawned))
	}

	tickRoutines(d, out)
}

// prepareWorktreeDrain routes an actionable drain to its checkout (ADR-0070/0072).
// Routing runs unconditionally: a bound set resumes at its bound worktree, and an
// unbound set carrying a managed intent provisions a worktree forked from the
// Trunk worktree and binds it — the one place routing provisions. Unbound sets
// with no intent never reach here: the queue-drainable check faults them before
// dispatch, so routing has no in-place fallback to invent a checkout.
func prepareWorktreeDrain(d *Deps, out io.Writer, dec Decision) Decision {
	if !dec.Actionable() {
		return dec
	}
	var cfg *config.Config
	if d.LoadConfig != nil {
		cfg, _ = d.LoadConfig(config.DefaultConfigPath())
	}
	route, err := binding.RouteDrainCheckout(binding.RouteDrainCheckoutRequest{
		TD:              d.Tasks,
		PD:              d.Project,
		Config:          cfg,
		Now:             d.now(),
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
	dec.pinRuntimePath = true
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
