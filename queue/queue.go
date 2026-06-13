// Package queue implements `pop queue`, a parallel per-project supervisor that
// fans Task-set drains out across registered projects (ADR 0027). It is
// concurrent across projects and serial within each — per-project
// serialization falls out of the runtime execution lock for free, so the
// supervisor never coordinates within a project, it only ensures at most one
// drain per idle project.
package queue

import (
	"fmt"
	"sort"

	"github.com/glebglazov/pop/config"
	"github.com/glebglazov/pop/internal/deps"
	"github.com/glebglazov/pop/project"
	"github.com/glebglazov/pop/tasks"
)

// queueWindow is the tmux window name into which drains are spawned in each
// project's session. Finished panes are left in place (not auto-closed).
const queueWindow = "pop-queue"

// Deps holds the supervisor's external dependencies. Refresh and ReadLock are
// seams over the tasks package so the scan/selection logic can be unit-tested
// with mocked Task-set rows and lock state without driving the filesystem.
type Deps struct {
	Tasks      *tasks.Deps
	Project    *project.Deps
	Tmux       deps.Tmux
	LoadConfig func(string) (*config.Config, error)

	// Refresh returns the Task-set rows registered under a definition path.
	// Defaults to tasks.RefreshWith.
	Refresh func(defPath string) (*tasks.RefreshResult, error)
	// ReadLock returns the runtime execution lock status for a runtime
	// checkout. Defaults to tasks.ReadRuntimeLockStatus.
	ReadLock func(runtimePath string) *tasks.RuntimeLockStatus
}

// DefaultDeps returns supervisor dependencies backed by real implementations.
func DefaultDeps() *Deps {
	d := &Deps{
		Tasks:      tasks.DefaultDeps(),
		Project:    project.DefaultDeps(),
		Tmux:       deps.NewRealTmux(),
		LoadConfig: config.Load,
	}
	return d
}

// refresh resolves the Refresh seam, defaulting to tasks.RefreshWith.
func (d *Deps) refresh(defPath string) (*tasks.RefreshResult, error) {
	if d.Refresh != nil {
		return d.Refresh(defPath)
	}
	return tasks.RefreshWith(d.Tasks, defPath, tasks.StatePathFor(defPath))
}

// readLock resolves the ReadLock seam, defaulting to tasks.ReadRuntimeLockStatus.
func (d *Deps) readLock(runtimePath string) *tasks.RuntimeLockStatus {
	if d.ReadLock != nil {
		return d.ReadLock(runtimePath)
	}
	return tasks.ReadRuntimeLockStatus(d.Tasks, runtimePath)
}

// projectScan holds one registered project's resolved coordinates for a scan.
type projectScan struct {
	Name           string
	ProjectPath    string
	DefinitionPath string
	RuntimePath    string
	SessionName    string
}

// Decision is the supervisor's per-project outcome for one scan iteration.
type Decision struct {
	Project   string
	Busy      bool   // a live runtime lock ⇒ already executing, skip
	TaskSetID string // the drain to spawn; empty when nothing is actionable
	Reason    string // why no drain was spawned (busy, no-ready-set, error)
	Err       error
	scan      projectScan
}

// Actionable reports whether the decision selected a Task set to drain.
func (dec Decision) Actionable() bool {
	return dec.Err == nil && !dec.Busy && dec.TaskSetID != ""
}

// Scan resolves every registered project, derives its actionable drain (if
// any), and returns one Decision per project. It performs no tmux side effects.
func Scan(d *Deps, cfg *config.Config) ([]Decision, error) {
	projects, err := tasks.ListPickerProjectsWith(d.Project, cfg)
	if err != nil {
		return nil, err
	}

	decisions := make([]Decision, 0, len(projects))
	for _, p := range projects {
		scan, err := resolveScan(d, p)
		if err != nil {
			decisions = append(decisions, Decision{Project: p.Name, Err: err, Reason: "resolve"})
			continue
		}
		decisions = append(decisions, decideProject(d, scan))
	}
	return decisions, nil
}

// resolveScan derives a project's definition path, runtime checkout, and tmux
// session name from its picker-visible entry.
func resolveScan(d *Deps, p project.ExpandedProject) (projectScan, error) {
	resolved, err := tasks.ResolvePathsWith(d.Tasks, d.Project, d.LoadConfig, tasks.ResolveInput{Path: p.Path})
	if err != nil {
		return projectScan{}, err
	}
	runtimePath, err := tasks.ResolveRuntimePathWith(d.Tasks, resolved.ProjectPath, "")
	if err != nil {
		return projectScan{}, err
	}
	return projectScan{
		Name:           p.Name,
		ProjectPath:    resolved.ProjectPath,
		DefinitionPath: resolved.DefinitionPath,
		RuntimePath:    runtimePath,
		SessionName:    project.SessionNameWith(d.Project, resolved.ProjectPath),
	}, nil
}

// decideProject reads the runtime lock and Ready sets for one project. A live
// lock means the checkout is already executing, so the project is skipped
// (per-project serialization, ADR 0027); otherwise the highest-priority Ready
// non-Archived set is selected to drain.
func decideProject(d *Deps, scan projectScan) Decision {
	dec := Decision{Project: scan.Name, scan: scan}

	lock := d.readLock(scan.RuntimePath)
	if lock != nil && lock.Locked {
		dec.Busy = true
		dec.Reason = "busy"
		return dec
	}

	refresh, err := d.refresh(scan.DefinitionPath)
	if err != nil {
		dec.Err = err
		dec.Reason = "refresh"
		return dec
	}

	id, ok := selectReadySet(refresh.Rows)
	if !ok {
		dec.Reason = "no ready set"
		return dec
	}
	dec.TaskSetID = id
	return dec
}

// selectReadySet returns the highest-priority Ready set among refresh rows.
// RefreshWith returns only non-Archived sets, so Archived sets are already
// dropped here. Higher priority integers rank first; ties break by
// registration order, matching the status table's active-set ordering.
func selectReadySet(rows []tasks.Row) (string, bool) {
	var ready []tasks.Row
	for _, row := range rows {
		if row.Status == tasks.StatusReady {
			ready = append(ready, row)
		}
	}
	if len(ready) == 0 {
		return "", false
	}
	sort.SliceStable(ready, func(i, j int) bool {
		if ready[i].Priority != ready[j].Priority {
			return ready[i].Priority > ready[j].Priority
		}
		return ready[i].RegIndex < ready[j].RegIndex
	})
	return ready[0].ID, true
}

// Spawn launches the selected drain into a pane of the project's `pop-queue`
// window, creating the tmux session detached when absent and the window when
// absent. It is a no-op for non-actionable decisions.
func Spawn(d *Deps, dec Decision) error {
	if !dec.Actionable() {
		return nil
	}
	command := fmt.Sprintf("pop tasks implement %s --yes", dec.TaskSetID)
	return spawnDrain(d.Tmux, dec.scan.SessionName, dec.scan.ProjectPath, command)
}

// spawnDrain creates (if needed) the detached session and `pop-queue` window,
// then sends the drain command into a fresh pane. Existing finished panes are
// left in place; a new drain always lands in its own pane.
func spawnDrain(tmux deps.Tmux, session, dir, command string) error {
	if !tmux.HasSession(session) {
		if err := tmux.NewSession(session, dir); err != nil {
			return fmt.Errorf("create session %q: %w", session, err)
		}
	}

	var paneID string
	if !hasQueueWindow(tmux, session) {
		out, err := tmux.Command("new-window", "-d", "-P", "-F", "#{pane_id}", "-t", session, "-n", queueWindow, "-c", dir)
		if err != nil {
			return fmt.Errorf("create %s window: %w", queueWindow, err)
		}
		paneID = out
	} else {
		out, err := tmux.Command("split-window", "-d", "-P", "-F", "#{pane_id}", "-t", session+":"+queueWindow, "-c", dir)
		if err != nil {
			return fmt.Errorf("create drain pane: %w", err)
		}
		paneID = out
		if _, err := tmux.Command("select-layout", "-t", session+":"+queueWindow, "tiled"); err != nil {
			return fmt.Errorf("retile %s window: %w", queueWindow, err)
		}
	}

	if _, err := tmux.Command("send-keys", "-t", paneID, command, "Enter"); err != nil {
		return fmt.Errorf("send drain command: %w", err)
	}
	return nil
}

// hasQueueWindow reports whether the project's session already holds the
// `pop-queue` window.
func hasQueueWindow(tmux deps.Tmux, session string) bool {
	out, err := tmux.Command("list-windows", "-t", session, "-F", "#{window_name}")
	if err != nil {
		return false
	}
	for _, name := range splitLines(out) {
		if name == queueWindow {
			return true
		}
	}
	return false
}
