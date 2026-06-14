// Package queue implements `pop queue`, a parallel per-project supervisor that
// fans Task-set drains out across registered projects (ADR 0027). It is
// concurrent across projects and serial within each — per-project
// serialization falls out of the runtime execution lock for free, so the
// supervisor never coordinates within a project, it only ensures at most one
// drain per idle project.
package queue

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"time"

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
	// ReadOutcome returns the latest terminal drain outcome for a runtime
	// checkout. Defaults to tasks.ReadDrainOutcome.
	ReadOutcome func(runtimePath string) (*tasks.DrainOutcomeRecord, error)
	// Now returns the current time. Defaults to time.Now.
	Now func() time.Time
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

// readOutcome resolves the ReadOutcome seam, defaulting to tasks.ReadDrainOutcome.
func (d *Deps) readOutcome(runtimePath string) (*tasks.DrainOutcomeRecord, error) {
	if d.ReadOutcome != nil {
		return d.ReadOutcome(runtimePath)
	}
	return tasks.ReadDrainOutcome(d.Tasks, runtimePath)
}

// projectScan holds one registered project's resolved coordinates for a scan.
type projectScan struct {
	Name           string
	ProjectPath    string
	DefinitionPath string
	RuntimePath    string
	SessionName    string
}

type provisionedWorktree struct {
	Path   string
	Branch string
}

// Decision is the supervisor's per-project outcome for one scan iteration.
type Decision struct {
	Project            string
	Busy               bool   // a live runtime lock ⇒ already executing, skip
	TaskSetID          string // the drain to spawn; empty when nothing is actionable
	Reason             string // why no drain was spawned (busy, no-ready-set, error)
	DefaultAgent       string
	WaitUntil          time.Time
	AgentNotes         []AgentNote
	WorktreeReady      bool
	ProjectConfigError string
	Err                error
	scan               projectScan
	lockStatus         *tasks.RuntimeLockStatus
}

type AgentNote struct {
	Event  string
	Agent  string
	Reason string
	Until  time.Time
}

// Actionable reports whether the decision selected a Task set to drain.
func (dec Decision) Actionable() bool {
	return dec.Err == nil && !dec.Busy && dec.TaskSetID != ""
}

func (d *Deps) now() time.Time {
	if d != nil && d.Now != nil {
		return d.Now()
	}
	return time.Now()
}

// Scan resolves every registered project, derives its actionable drain (if
// any), and returns one Decision per project. It performs no tmux side effects.
func Scan(d *Deps, cfg *config.Config) ([]Decision, error) {
	projects, err := tasks.ListPickerProjectsWith(d.Project, cfg)
	if err != nil {
		return nil, err
	}
	qcfg, err := resolvedQueueConfig(cfg)
	if err != nil {
		return nil, err
	}
	state, err := EnsureDaemonState(d.Tasks)
	if err != nil {
		return nil, err
	}
	now := d.now().UTC()

	decisions := make([]Decision, 0, len(projects))
	for _, p := range projects {
		scan, err := resolveScan(d, p)
		if err != nil {
			decisions = append(decisions, Decision{Project: p.Name, Err: err, Reason: "resolve"})
			continue
		}
		decisions = append(decisions, decideProject(d, scan, qcfg.Agents, state, now))
	}
	return decisions, nil
}

func resolvedQueueConfig(cfg *config.Config) (config.ResolvedQueueConfig, error) {
	qcfg, err := cfg.ResolveQueue()
	if err != nil {
		return config.ResolvedQueueConfig{}, err
	}
	if len(qcfg.Agents) == 0 {
		qcfg.Agents = []string{tasks.DefaultAgentPreset}
	}
	return qcfg, nil
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
func decideProject(d *Deps, scan projectScan, agents []string, state *DaemonState, now time.Time) Decision {
	dec := Decision{Project: scan.Name, scan: scan}
	dec.WorktreeReady, dec.ProjectConfigError = readRepoConfig(d, scan.ProjectPath)

	if dec.WorktreeReady {
		openSpawn, openLock, err := liveOpenSpawn(d, dec.Project)
		if err != nil {
			dec.Err = err
			dec.Reason = "journal"
			return dec
		}
		if openLock != nil && openLock.Locked {
			dec.Busy = true
			dec.Reason = "busy"
			dec.TaskSetID = openSpawn.SetID
			dec.lockStatus = openLock
			return dec
		}
	}

	lock := d.readLock(scan.RuntimePath)
	dec.lockStatus = lock
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

	id, waitUntil, waitReason, ok := selectReadySet(refresh, scan.RuntimePath, state, now)
	if !ok {
		if !waitUntil.IsZero() {
			dec.Reason = waitReason
			dec.WaitUntil = waitUntil
		} else if waitReason != "" {
			dec.Reason = waitReason
		} else {
			dec.Reason = "no ready set"
		}
		return dec
	}
	defaultAgent, waitUntil, notes, ok := selectDefaultAgent(d, agents, state, now)
	dec.AgentNotes = notes
	if !ok {
		if waitUntil.IsZero() {
			dec.Reason = "no available agent"
		} else {
			dec.Reason = "all agents cooling"
		}
		dec.WaitUntil = waitUntil
		return dec
	}
	dec.TaskSetID = id
	dec.DefaultAgent = defaultAgent
	return dec
}

func liveOpenSpawn(d *Deps, projectName string) (JournalEntry, *tasks.RuntimeLockStatus, error) {
	if d == nil || d.Tasks == nil {
		return JournalEntry{}, nil, nil
	}
	entries, err := ReadJournal(d.Tasks)
	if err != nil {
		return JournalEntry{}, nil, err
	}
	for i := len(entries) - 1; i >= 0; i-- {
		entry := entries[i]
		if entry.Project != projectName || entry.Event != JournalEventSpawn || entry.RuntimePath == "" || entry.SetID == "" {
			continue
		}
		if !journalHasOpenSpawn(entries, entry.Project, entry.RuntimePath, entry.SetID) {
			continue
		}
		lock := d.readLock(entry.RuntimePath)
		if lock != nil && lock.Locked {
			return entry, lock, nil
		}
	}
	return JournalEntry{}, nil, nil
}

func readRepoConfig(d *Deps, repoRoot string) (bool, string) {
	pd := d.Project
	if pd == nil || pd.FS == nil {
		pd = project.DefaultDeps()
	}
	cfg, err := config.LoadRepoConfigWith(&config.Deps{FS: pd.FS}, repoRoot)
	if err != nil {
		return false, err.Error()
	}
	return cfg.WorktreeReady, ""
}

// selectReadySet returns the highest-priority Ready set among refresh rows.
// RefreshWith returns only non-Archived sets, so Archived sets are already
// dropped here. Higher priority integers rank first; ties break by
// registration order, matching the status table's active-set ordering.
func selectReadySetID(rows []tasks.Row) (string, bool) {
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

func selectReadySet(refresh *tasks.RefreshResult, runtimePath string, state *DaemonState, now time.Time) (string, time.Time, string, bool) {
	if refresh == nil {
		return "", time.Time{}, "", false
	}
	var ready []tasks.Row
	for _, row := range refresh.Rows {
		if row.Status == tasks.StatusReady {
			ready = append(ready, row)
		}
	}
	if len(ready) == 0 {
		return "", time.Time{}, "", false
	}
	sort.SliceStable(ready, func(i, j int) bool {
		if ready[i].Priority != ready[j].Priority {
			return ready[i].Priority > ready[j].Priority
		}
		return ready[i].RegIndex < ready[j].RegIndex
	})
	var earliest time.Time
	var skippedParked bool
	var skippedBackoff bool
	var skippedQuota bool
	for _, row := range ready {
		if setParked(state, runtimePath, row.ID) {
			skippedParked = true
			continue
		}
		if until := setCrashBackoffUntil(state, runtimePath, row.ID, now); !until.IsZero() {
			skippedBackoff = true
			if earliest.IsZero() || until.Before(earliest) {
				earliest = until
			}
			continue
		}
		if until := setBackoffUntil(state, runtimePath, row.ID, now); !until.IsZero() {
			skippedQuota = true
			if earliest.IsZero() || until.Before(earliest) {
				earliest = until
			}
			continue
		}
		return row.ID, time.Time{}, "", true
	}
	switch {
	case !earliest.IsZero() && skippedBackoff:
		return "", earliest, "set backed off after abnormal drain exit", false
	case !earliest.IsZero() && skippedQuota:
		return "", earliest, "set backed off for pinned agent cooldown", false
	case skippedParked:
		return "", time.Time{}, "set parked after repeated abnormal drain exits", false
	default:
		return "", time.Time{}, "no ready set", false
	}
}

func setBackoffUntil(state *DaemonState, runtimePath, setID string, now time.Time) time.Time {
	if state == nil || state.SetBackoffs == nil {
		return time.Time{}
	}
	until := state.SetBackoffs[setBackoffKey(runtimePath, setID)]
	if until.IsZero() || !until.After(now) {
		return time.Time{}
	}
	return until
}

func setCrashBackoffUntil(state *DaemonState, runtimePath, setID string, now time.Time) time.Time {
	if state == nil || state.SetCrashBackoffs == nil {
		return time.Time{}
	}
	until := state.SetCrashBackoffs[setBackoffKey(runtimePath, setID)]
	if until.IsZero() || !until.After(now) {
		return time.Time{}
	}
	return until
}

func setParked(state *DaemonState, runtimePath, setID string) bool {
	if state == nil || state.ParkedSets == nil {
		return false
	}
	_, ok := state.ParkedSets[setBackoffKey(runtimePath, setID)]
	return ok
}

func setBackoffKey(runtimePath, setID string) string {
	return runtimePath + "\x00" + setID
}

func selectDefaultAgent(d *Deps, agents []string, state *DaemonState, now time.Time) (string, time.Time, []AgentNote, bool) {
	var notes []AgentNote
	var earliest time.Time
	for _, agent := range agents {
		preset, err := tasks.AgentPresetName(agent)
		if err != nil {
			notes = append(notes, AgentNote{Event: "agent_unavailable", Agent: agent, Reason: err.Error()})
			continue
		}
		until := agentCooldownUntil(state, preset, now)
		if !until.IsZero() {
			notes = append(notes, AgentNote{Event: "agent_cooling", Agent: preset, Reason: "quota cooldown", Until: until})
			if earliest.IsZero() || until.Before(earliest) {
				earliest = until
			}
			continue
		}
		if !agentBinaryAvailable(d, preset) {
			notes = append(notes, AgentNote{Event: "agent_unavailable", Agent: preset, Reason: "binary not found on PATH"})
			continue
		}
		return agent, time.Time{}, notes, true
	}
	return "", earliest, notes, false
}

func agentCooldownUntil(state *DaemonState, preset string, now time.Time) time.Time {
	if state == nil || state.AgentCooldowns == nil {
		return time.Time{}
	}
	until := state.AgentCooldowns[preset]
	if until.IsZero() || !until.After(now) {
		return time.Time{}
	}
	return until
}

func agentBinaryAvailable(d *Deps, preset string) bool {
	adapter, err := tasks.ResolveAgentAdapter(preset)
	if err != nil {
		return false
	}
	lookPath := tasks.DefaultDeps().LookPath
	if d != nil && d.Tasks != nil && d.Tasks.LookPath != nil {
		lookPath = d.Tasks.LookPath
	}
	_, err = lookPath(tasks.AgentBinary(adapter))
	return err == nil
}

func provisionWorktree(d *Deps, projectPath, setID string) (provisionedWorktree, error) {
	if d == nil || d.Tasks == nil {
		return provisionedWorktree{}, fmt.Errorf("missing task dependencies")
	}
	id, err := tasks.ResolveRepositoryIdentity(d.Tasks, projectPath)
	if err != nil {
		return provisionedWorktree{}, err
	}
	safeSet := safeWorktreeComponent(setID)
	stamp := d.now().UTC().Format("20060102T150405Z")
	branch := fmt.Sprintf("pop/%s/%s", safeSet, stamp)
	path := filepath.Join(QueueDataDir(d.Tasks), "worktrees", id.Basename+"-"+id.ShortHash, safeSet+"-"+stamp)
	if err := d.Tasks.FS.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return provisionedWorktree{}, fmt.Errorf("create worktree parent: %w", err)
	}
	if _, err := d.Tasks.Git.CommandInDir(projectPath, "worktree", "add", "-b", branch, path, "HEAD"); err != nil {
		return provisionedWorktree{}, fmt.Errorf("git worktree add: %w", err)
	}
	return provisionedWorktree{Path: path, Branch: branch}, nil
}

func safeWorktreeComponent(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return "set"
	}
	var b strings.Builder
	lastDash := false
	for _, r := range strings.ToLower(s) {
		ok := (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9')
		if ok {
			b.WriteRune(r)
			lastDash = false
			continue
		}
		if !lastDash {
			b.WriteByte('-')
			lastDash = true
		}
	}
	out := strings.Trim(b.String(), "-")
	if out == "" {
		return "set"
	}
	return out
}

// Spawn launches the selected drain into a pane of the project's `pop-queue`
// window, creating the tmux session detached when absent and the window when
// absent. It is a no-op for non-actionable decisions.
func Spawn(d *Deps, dec Decision) error {
	if !dec.Actionable() {
		return nil
	}
	command := fmt.Sprintf("pop tasks implement %s --yes", shellQuote(dec.TaskSetID))
	if dec.WorktreeReady && dec.scan.RuntimePath != "" {
		command += " --task-runtime-path " + shellQuote(dec.scan.RuntimePath)
	}
	if dec.DefaultAgent != "" {
		command += " --default-agent " + shellQuote(dec.DefaultAgent)
	}
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

func shellQuote(s string) string {
	if s == "" {
		return "''"
	}
	for _, r := range s {
		switch r {
		case ' ', '\t', '\n', '\'', '"', '\\', '$', '`', '!', '&', '|', ';', '(', ')', '<', '>':
			return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
		}
	}
	return s
}
