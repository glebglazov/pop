// Package queue implements `pop queue`, a parallel per-project supervisor that
// fans Task-set drains out across registered projects (ADR 0027). It is
// concurrent across projects and serial within each — per-project
// serialization falls out of the Drain's transactional mutual exclusion in the
// global store for free (ADR-0055), so the supervisor never coordinates within a
// project, it only ensures at most one drain per idle project.
package queue

import (
	"fmt"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/glebglazov/pop/tasks/binding"
	"github.com/glebglazov/pop/tasks/integration"
	"github.com/glebglazov/pop/config"
	"github.com/glebglazov/pop/internal/deps"
	"github.com/glebglazov/pop/project"
	"github.com/glebglazov/pop/tasks"
)

const drainWindowName = "pop-queue"

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
	// Reconcile runs the opportunistic crash-detection pass before a read,
	// transitioning dead-PID running Drains to crashed. Defaults to
	// tasks.ReconcileDrains.
	Reconcile func() (int, error)
	// ComputeMergeability dry-runs merging a completed runtime branch into the
	// working checkout. Defaults to git merge-tree.
	ComputeMergeability func(workingPath, runtimePath string) (MergeabilityRecord, error)
	// ToggleAutoDrain flips a registered Task-set auto-drain bit in Task state.
	// Defaults to tasks.ToggleAutoDrainWith.
	ToggleAutoDrain func(defPath, statePath, setID string) (*tasks.AutoDrainResult, error)
	// AcquireRuntimeLock serializes human-triggered integration with normal
	// runtime execution. Defaults to tasks.AcquireRuntimeLock.
	AcquireRuntimeLock func(runtimePath string) (runtimeLock, error)
	// Now returns the current time. Defaults to time.Now.
	Now func() time.Time

	// CompleteDetailTask, ResetDetailTask, SkipDetailTask are seams for the
	// detail-view override keys (C/O/K). Each defaults to the corresponding
	// tasks.*With function resolved with the Deps' Tasks, Project, and LoadConfig.
	CompleteDetailTask func(defPath, taskPath string) error
	ResetDetailTask    func(defPath, taskPath string) error
	SkipDetailTask     func(defPath, taskPath string) error
}

type runtimeLock interface {
	Release() error
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

// reconcile runs the opportunistic crash-detection pass before a read pass,
// healing dead-PID running Drains into crashed (ADR-0055). It defaults to
// tasks.ReconcileDrains. The result count is advisory; reconciliation never
// blocks a read, so a reconcile error is swallowed (the read still reflects the
// pre-reconcile truth, which is no worse than before this pass existed).
func (d *Deps) reconcile() {
	if d.Reconcile != nil {
		_, _ = d.Reconcile()
		return
	}
	_, _ = tasks.ReconcileDrains(d.Tasks)
}

func (d *Deps) computeMergeability(workingPath, runtimePath string) (MergeabilityRecord, error) {
	if d.ComputeMergeability != nil {
		return d.ComputeMergeability(workingPath, runtimePath)
	}
	rec, err := integration.Compute(d.Tasks, workingPath, runtimePath)
	return mergeabilityRecordFromIntegration(rec), err
}

func (d *Deps) toggleAutoDrain(defPath, statePath, setID string) (*tasks.AutoDrainResult, error) {
	if d.ToggleAutoDrain != nil {
		return d.ToggleAutoDrain(defPath, statePath, setID)
	}
	return tasks.ToggleAutoDrainWith(d.Tasks, defPath, statePath, setID)
}

func (d *Deps) acquireRuntimeLock(runtimePath string) (runtimeLock, error) {
	if d.AcquireRuntimeLock != nil {
		return d.AcquireRuntimeLock(runtimePath)
	}
	return tasks.AcquireRuntimeLock(d.Tasks, runtimePath, nil)
}

func (d *Deps) resolveInput(defPath string) tasks.ResolveInput {
	return tasks.ResolveInput{DefinitionOverride: defPath, CWD: defPath}
}

func (d *Deps) loadConfig() func(string) (*config.Config, error) {
	if d.LoadConfig != nil {
		return d.LoadConfig
	}
	return config.Load
}

func (d *Deps) projectDeps() *project.Deps {
	if d.Project != nil {
		return d.Project
	}
	return project.DefaultDeps()
}

func (d *Deps) completeDetailTask(defPath, taskPath string) error {
	if d.CompleteDetailTask != nil {
		return d.CompleteDetailTask(defPath, taskPath)
	}
	td := d.Tasks
	if td == nil {
		td = tasks.DefaultDeps()
	}
	result, err := tasks.CompleteTaskWith(td, d.projectDeps(), d.loadConfig(), tasks.CompleteTaskOptions{
		ResolveInput: d.resolveInput(defPath),
		TaskPath:     taskPath,
	})
	if err != nil {
		return err
	}
	// Record Mergeability when this completion concluded a worktree-bound set,
	// so the dashboard backlog shows a merge verdict rather than "unknown"
	// (ADR-0051). Best-effort and silent: the completion already succeeded,
	// Mergeability is recomputed at integrate time, and the dashboard runs an
	// alt-screen TUI where a stderr warning would corrupt the display.
	id := integration.DefaultDeps()
	id.Tasks = td
	_ = integration.RecordCompletionMergeability(id, result.ProjectPath, result.TaskSetID, result.Refresh)
	return nil
}

func (d *Deps) resetDetailTask(defPath, taskPath string) error {
	if d.ResetDetailTask != nil {
		return d.ResetDetailTask(defPath, taskPath)
	}
	td := d.Tasks
	if td == nil {
		td = tasks.DefaultDeps()
	}
	_, err := tasks.ResetTaskWith(td, d.projectDeps(), d.loadConfig(), tasks.ResetTaskOptions{
		ResolveInput: d.resolveInput(defPath),
		TaskPath:     taskPath,
	})
	return err
}

func (d *Deps) skipDetailTask(defPath, taskPath string) error {
	if d.SkipDetailTask != nil {
		return d.SkipDetailTask(defPath, taskPath)
	}
	td := d.Tasks
	if td == nil {
		td = tasks.DefaultDeps()
	}
	_, err := tasks.SkipTaskWith(td, d.projectDeps(), d.loadConfig(), tasks.SkipTaskOptions{
		ResolveInput: d.resolveInput(defPath),
		TaskPath:     taskPath,
	})
	return err
}

// projectScan holds one registered project's resolved coordinates for a scan.
type projectScan struct {
	Name           string
	ProjectPath    string
	DefinitionPath string
	RuntimePath    string
	SessionName    string
	// RepoKey is the repository identity prefix (basename-shortHash) resolved
	// once during scan path resolution. Carrying it lets the decision phase reuse
	// it instead of re-forking `git rev-parse --git-common-dir` per group.
	RepoKey string
	// RepoCommonDir is the repository's canonical git common directory — the
	// Drain row's repo key. The decision phase queries per-set abnormal-terminal
	// history (Queue backoff/parking) by it (ADR-0055).
	RepoCommonDir string
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
	WaitUntil          time.Time
	WorktreeReady      bool
	ProjectConfigError string
	// UnverifiedSetID is the first Task-set in UNVERIFIED state (AFK done, terminal
	// HITL gate awaits human sign-off). Empty when the project has no unverified set.
	UnverifiedSetID string
	// BlockedSetID names the set whose abnormal-backoff or parking produced
	// Reason. It lets the dashboard attribute a backoff/park to a specific set
	// without reading any persisted flag (ADR-0055).
	BlockedSetID string
	Err          error
	scan         projectScan
	lockStatus   *tasks.RuntimeLockStatus
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

// Scan resolves every registered project, collapses the checkouts that share a
// Repository identity into one scheduling unit (ADR-0035), derives each repo's
// actionable drain(s), and returns the Decisions for this scan. A repository is
// scheduled at most once per Ready set regardless of how many worktrees it
// expands into. Non-worktree-ready repos still return at most one actionable
// Decision; worktree-ready repos may return one busy Decision per live worktree
// drain plus one actionable Decision per Ready set not already running. It
// performs no tmux side effects.
func Scan(d *Deps, cfg *config.Config) ([]Decision, error) {
	// Reconcile-then-read: heal dead-PID running Drains into crashed before the
	// lock/outcome reads below project from them (ADR-0055). This covers
	// `pop queue status` (BuildStatus → Scan) and each daemon tick (tick → Scan).
	d.reconcile()
	projects, err := tasks.ListPickerProjectsWith(d.Project, cfg)
	if err != nil {
		return nil, err
	}
	if _, err := resolvedQueueConfig(cfg); err != nil {
		return nil, err
	}
	state, err := EnsureDaemonState(d.Tasks)
	if err != nil {
		return nil, err
	}
	now := d.now().UTC()

	// Memoize idempotent git reads for this scan. resolveScan and
	// decideRepoDispatches resolve the same repository coordinates repeatedly
	// (path normalization per project, identity per project and again per group),
	// re-forking git for the same directories. Wrap a shallow copy of the deps so
	// the caller's git is untouched, then serve those repeated reads from cache.
	if d.Tasks != nil && d.Tasks.Git != nil {
		scanDeps := *d
		tasksDeps := *d.Tasks
		tasksDeps.Git = newScanGitCache(d.Tasks.Git)
		scanDeps.Tasks = &tasksDeps
		d = &scanDeps
	}

	// Each project's scan resolution and each repo group's decision run several
	// read-only git subprocesses; with many registered checkouts the serial cost
	// dominates wall-clock. Both phases are concurrency-safe — resolveScan and
	// decideRepoDispatches only read the shared DaemonState and Deps — so fan
	// them out across a bounded worker pool while preserving deterministic order.
	sem := make(chan struct{}, scanConcurrency())

	// Phase 1: resolve every project's scan concurrently. A terminal decision
	// (resolve error or out-of-scope) is recorded in place of a scan.
	type scanResult struct {
		scan projectScan
		dec  *Decision
	}
	results := make([]scanResult, len(projects))
	var wg sync.WaitGroup
	for i, p := range projects {
		wg.Add(1)
		sem <- struct{}{}
		go func(idx int, p project.ExpandedProject) {
			defer wg.Done()
			defer func() { <-sem }()
			scan, err := resolveScan(d, p)
			if err != nil {
				if outsideQueueScopeResolveError(err) {
					results[idx] = scanResult{dec: &Decision{Project: p.Name, Reason: "no ready set"}}
					return
				}
				results[idx] = scanResult{dec: &Decision{Project: p.Name, Err: err, Reason: "resolve"}}
				return
			}
			results[idx] = scanResult{scan: scan}
		}(i, p)
	}
	wg.Wait()

	// Group picker Projects by Repository identity. All checkouts of one repo
	// share a Task storage definition path, so it keys the group: a bare repo's
	// many worktrees collapse to a single scheduling unit.
	var order []string
	groups := map[string][]projectScan{}
	decisions := make([]Decision, 0, len(projects))
	for _, r := range results {
		if r.dec != nil {
			decisions = append(decisions, *r.dec)
			continue
		}
		if _, ok := groups[r.scan.DefinitionPath]; !ok {
			order = append(order, r.scan.DefinitionPath)
		}
		groups[r.scan.DefinitionPath] = append(groups[r.scan.DefinitionPath], r.scan)
	}

	// Phase 2: decide each repo group concurrently, preserving group order.
	groupDecisions := make([][]Decision, len(order))
	var wg2 sync.WaitGroup
	for i, key := range order {
		wg2.Add(1)
		sem <- struct{}{}
		go func(idx int, key string) {
			defer wg2.Done()
			defer func() { <-sem }()
			groupDecisions[idx] = decideRepoDispatches(d, cfg, groups[key], state, now)
		}(i, key)
	}
	wg2.Wait()
	for _, gd := range groupDecisions {
		decisions = append(decisions, gd...)
	}
	return decisions, nil
}

// scanConcurrency bounds the worker pool used to resolve project scans and decide
// repo groups in parallel. The work is git-subprocess (I/O) bound, so it oversubscribes
// the CPU count; the cap keeps a large project list from spawning hundreds of
// simultaneous git processes.
func scanConcurrency() int {
	n := runtime.NumCPU() * 4
	if n < 4 {
		n = 4
	}
	if n > 32 {
		n = 32
	}
	return n
}

// outsideQueueScopeResolveError reports whether resolveScan failed because the
// project has no git checkout. Such projects are picker Projects but outside
// Queue scope — they have no Repository identity and therefore no Task storage.
func outsideQueueScopeResolveError(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "not inside a git repository") ||
		strings.Contains(msg, "is not a git checkout")
}

func resolvedQueueConfig(cfg *config.Config) (config.ResolvedQueueConfig, error) {
	qcfg, err := cfg.ResolveQueue()
	if err != nil {
		return config.ResolvedQueueConfig{}, err
	}
	return qcfg, nil
}

// resolveScan derives a project's definition path, runtime checkout, and tmux
// session name from its picker-visible entry.
func resolveScan(d *Deps, p project.ExpandedProject) (projectScan, error) {
	// ResolveScanPaths derives the project root and definition path in a single
	// git invocation. The runtime checkout equals the project root here (the
	// queue never overrides it), so no separate runtime-path resolution is needed.
	resolved, id, err := tasks.ResolveScanPaths(d.Tasks, p.Path)
	if err != nil {
		return projectScan{}, err
	}
	return projectScan{
		Name:           p.Name,
		ProjectPath:    resolved.ProjectPath,
		DefinitionPath: resolved.DefinitionPath,
		RuntimePath:    resolved.ProjectPath,
		SessionName:    project.SessionNameWith(d.Project, resolved.ProjectPath),
		RepoKey:        repoIdentityKey(id),
		RepoCommonDir:  id.CommonDir,
	}, nil
}

// scanRepoKey returns the repository key resolved during scan path resolution,
// falling back to a git lookup for callers (tests, the spawn path) that build a
// projectScan without it.
func scanRepoKey(d *Deps, scan projectScan) (string, error) {
	if scan.RepoKey != "" {
		return scan.RepoKey, nil
	}
	return resolveRepoKey(d, scan.ProjectPath)
}

// scanRepoCommonDir returns the repository's canonical git common directory (the
// Drain row's repo key for backoff/parking history), reusing the value resolved
// during scan path resolution and falling back to a git lookup for callers
// (tests, the spawn path) that build a projectScan without it.
func scanRepoCommonDir(d *Deps, scan projectScan) string {
	if scan.RepoCommonDir != "" {
		return scan.RepoCommonDir
	}
	if d == nil || d.Tasks == nil || strings.TrimSpace(scan.ProjectPath) == "" {
		return ""
	}
	id, err := tasks.ResolveRepositoryIdentity(d.Tasks, scan.ProjectPath)
	if err != nil {
		return ""
	}
	return id.CommonDir
}

// decideProject reads the runtime lock and Ready sets for one project and
// returns the first Decision. It is retained for tests and callers that need the
// v1 single-decision view; Scan uses decideProjectDispatches to expose
// worktree-ready multi-set fan-out.
func decideProject(d *Deps, scan projectScan, state *DaemonState, now time.Time) Decision {
	decisions := decideProjectDispatches(d, scan, nil, state, now)
	if len(decisions) == 0 {
		return Decision{Project: scan.Name, scan: scan, Reason: "no ready set"}
	}
	return decisions[0]
}

// decideProjectDispatches reads runtime locks and Ready sets for one project.
// One live checkout lock makes the project busy; otherwise the
// highest-priority Ready set is selected. A project with an explicit
// WorktreeReady Decision keeps live worktree drains as per-checkout busy
// Decisions but may still dispatch other Ready sets into fresh worktrees.
func decideProjectDispatches(d *Deps, scan projectScan, delays []time.Duration, state *DaemonState, now time.Time) []Decision {
	dec := Decision{Project: scan.Name, scan: scan}
	dec.WorktreeReady, dec.ProjectConfigError = readRepoConfig(d, scan.ProjectPath)

	var decisions []Decision
	runningSets := map[string]bool{}
	if dec.WorktreeReady {
		openSpawns, err := liveOpenSpawns(d, dec.Project)
		if err != nil {
			dec.Err = err
			dec.Reason = "journal"
			return []Decision{dec}
		}
		for _, open := range openSpawns {
			if open.Lock == nil || !open.Lock.Locked {
				continue
			}
			busy := dec
			busy.Busy = true
			busy.Reason = "busy"
			busy.TaskSetID = open.Entry.SetID
			busy.lockStatus = open.Lock
			busy.scan.RuntimePath = open.Entry.RuntimePath
			decisions = append(decisions, busy)
			runningSets[open.Entry.SetID] = true
		}
	}

	lock := d.readLock(scan.RuntimePath)
	dec.lockStatus = lock
	// When the current checkout has been adopted into the worktree binding model
	// (ADR-0036), its runtime path equals an open spawn's runtime path that the
	// openSpawns loop above already reported as busy. Treating that lock as a v1
	// in-place lock here would both double-count the live drain and short-circuit
	// dispatch of the repo's other Ready sets into fresh worktrees, so fall
	// through to the dispatch logic instead.
	adoptedSpawn := dec.WorktreeReady && lock != nil && lock.Metadata != nil && runningSets[lock.Metadata.SetID]
	if lock != nil && lock.Locked && !adoptedSpawn {
		dec.Busy = true
		dec.Reason = "busy"
		if lock.Metadata != nil && lock.Metadata.SetID != "" {
			dec.TaskSetID = lock.Metadata.SetID
			runningSets[lock.Metadata.SetID] = true
		}
		decisions = append(decisions, dec)
		return decisions
	}
	if adoptedSpawn {
		// The dispatch baseline must not carry the adopted spawn's live lock.
		dec.lockStatus = nil
	}

	refresh, err := d.refresh(scan.DefinitionPath)
	if err != nil {
		dec.Err = err
		dec.Reason = "refresh"
		return appendOrOnly(decisions, dec)
	}

	repoKey, err := scanRepoKey(d, scan)
	if err != nil {
		dec.Err = err
		dec.Reason = "repo"
		return appendOrOnly(decisions, dec)
	}

	backoff := d.setBackoffLookup(scanRepoCommonDir(d, scan), delays, now)
	ids, waitUntil, waitReason, blockedID, ok := selectReadySets(refresh, repoKey, state, backoff, now)
	if !ok {
		if !waitUntil.IsZero() {
			dec.Reason = waitReason
			dec.WaitUntil = waitUntil
			dec.BlockedSetID = blockedID
		} else if waitReason != "" {
			dec.Reason = waitReason
			dec.BlockedSetID = blockedID
		} else if id := firstUnverifiedSetID(refresh.Rows); id != "" {
			dec.Reason = "awaiting verification"
			dec.UnverifiedSetID = id
		} else {
			dec.Reason = "no ready set"
		}
		return appendOrOnly(decisions, dec)
	}
	if !dec.WorktreeReady && len(ids) > 1 {
		ids = ids[:1]
	}
	for _, id := range ids {
		if runningSets[id] {
			continue
		}
		action := dec
		action.TaskSetID = id
		decisions = append(decisions, action)
	}
	if len(decisions) == 0 {
		dec.Reason = "ready set already running"
		return []Decision{dec}
	}
	return decisions
}

func appendOrOnly(decisions []Decision, dec Decision) []Decision {
	if len(decisions) > 0 {
		return decisions
	}
	return []Decision{dec}
}

type liveOpenSpawn struct {
	Entry JournalEntry
	Lock  *tasks.RuntimeLockStatus
}

func liveOpenSpawns(d *Deps, projectName string) ([]liveOpenSpawn, error) {
	if d == nil || d.Tasks == nil {
		return nil, nil
	}
	entries, err := ReadJournal(d.Tasks)
	if err != nil {
		return nil, err
	}
	seen := map[string]bool{}
	var out []liveOpenSpawn
	for i := len(entries) - 1; i >= 0; i-- {
		entry := entries[i]
		if entry.Project != projectName || entry.Event != JournalEventSpawn || entry.RuntimePath == "" || entry.SetID == "" {
			continue
		}
		if !journalHasOpenSpawn(entries, entry.Project, entry.RuntimePath, entry.SetID) {
			continue
		}
		key := entry.RuntimePath + "\x00" + entry.SetID
		if seen[key] {
			continue
		}
		seen[key] = true
		lock := d.readLock(entry.RuntimePath)
		// Under the adopt-current-checkout model several sets share one runtime
		// path, so a held lock alone does not prove THIS set is the live drain —
		// only the set named in the lock metadata is. Requiring the match drops
		// stale open-spawns left by drains that died without journaling an
		// outcome, which would otherwise borrow the live lock of another set and
		// surface as a duplicate picked-up line.
		if lock != nil && lock.Locked && lock.Metadata != nil && lock.Metadata.SetID == entry.SetID {
			out = append(out, liveOpenSpawn{Entry: entry, Lock: lock})
		}
	}
	for i, j := 0, len(out)-1; i < j; i, j = i+1, j-1 {
		out[i], out[j] = out[j], out[i]
	}
	return out, nil
}

func readRepoConfig(d *Deps, repoRoot string) (bool, string) {
	_, err := loadRepoConfig(d, repoRoot)
	if err != nil {
		return false, err.Error()
	}
	return false, ""
}

func loadRepoConfig(d *Deps, repoRoot string) (config.RepoConfig, error) {
	pd := d.Project
	if pd == nil || pd.FS == nil {
		pd = project.DefaultDeps()
	}
	return config.LoadRepoConfigWith(&config.Deps{FS: pd.FS}, repoRoot)
}

// selectReadySetID returns the highest-priority Auto-drain Ready set among
// refresh rows. RefreshWith returns only non-Archived sets, so Archived sets are
// already dropped here. Higher priority integers rank first; ties break by
// registration order, matching the status table's active-set ordering.
func selectReadySetID(rows []tasks.Row) (string, bool) {
	var ready []tasks.Row
	for _, row := range rows {
		if row.Status == tasks.StatusReady && row.AutoDrain {
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

// firstUnverifiedSetID returns the ID of the first Task-set in UNVERIFIED state
// (all AFK work done/skipped, only a terminal HITL gate remains). Empty when none.
func firstUnverifiedSetID(rows []tasks.Row) string {
	for _, row := range rows {
		if row.Status == tasks.StatusUnverified {
			return row.ID
		}
	}
	return ""
}

func selectReadySet(refresh *tasks.RefreshResult, repoKey string, state *DaemonState, backoff setBackoffFunc, now time.Time) (string, time.Time, string, bool) {
	ids, waitUntil, reason, _, ok := selectReadySets(refresh, repoKey, state, backoff, now)
	if !ok || len(ids) == 0 {
		return "", waitUntil, reason, false
	}
	return ids[0], time.Time{}, "", true
}

// setBackoffFunc reports a set's abnormal-derived Queue eligibility: parked
// reports whether repeated abnormal terminals have parked it (skip indefinitely
// until a human unparks); a non-zero until is the instant it next becomes
// spawnable after an escalating backoff. Both are derived from Drain history, so
// a nil func (tests, callers without a store) means "always spawnable".
type setBackoffFunc func(setID string) (parked bool, until time.Time)

func selectReadySets(refresh *tasks.RefreshResult, repoKey string, state *DaemonState, backoff setBackoffFunc, now time.Time) ([]string, time.Time, string, string, bool) {
	if refresh == nil {
		return nil, time.Time{}, "", "", false
	}
	var ready []tasks.Row
	for _, row := range refresh.Rows {
		if row.Status == tasks.StatusReady && row.AutoDrain {
			ready = append(ready, row)
		}
	}
	if len(ready) == 0 {
		return nil, time.Time{}, "", "", false
	}
	sort.SliceStable(ready, func(i, j int) bool {
		if ready[i].Priority != ready[j].Priority {
			return ready[i].Priority > ready[j].Priority
		}
		return ready[i].RegIndex < ready[j].RegIndex
	})
	var earliest time.Time
	var parkedID, backoffID, quotaID string
	var ids []string
	for _, row := range ready {
		if backoff != nil {
			if parked, until := backoff(row.ID); parked {
				if parkedID == "" {
					parkedID = row.ID
				}
				continue
			} else if !until.IsZero() {
				if backoffID == "" {
					backoffID = row.ID
				}
				if earliest.IsZero() || until.Before(earliest) {
					earliest = until
				}
				continue
			}
		}
		if until := setBackoffUntil(state, repoKey, row.ID, now); !until.IsZero() {
			if quotaID == "" {
				quotaID = row.ID
			}
			if earliest.IsZero() || until.Before(earliest) {
				earliest = until
			}
			continue
		}
		ids = append(ids, row.ID)
	}
	if len(ids) > 0 {
		return ids, time.Time{}, "", "", true
	}
	switch {
	case !earliest.IsZero() && backoffID != "":
		return nil, earliest, "set backed off after abnormal drain exit", backoffID, false
	case !earliest.IsZero() && quotaID != "":
		return nil, earliest, "set backed off for pinned agent cooldown", quotaID, false
	case parkedID != "":
		return nil, time.Time{}, "set parked after repeated abnormal drain exits", parkedID, false
	default:
		return nil, time.Time{}, "no ready set", "", false
	}
}

func setBackoffUntil(state *DaemonState, repoKey, setID string, now time.Time) time.Time {
	if state == nil || state.SetBackoffs == nil {
		return time.Time{}
	}
	until := state.SetBackoffs[setScopedKey(repoKey, setID)]
	if until.IsZero() || !until.After(now) {
		return time.Time{}
	}
	return until
}

// setBackoffStatus derives a set's abnormal-driven Queue eligibility from its
// Drain history (ADR-0055): with n consecutive abnormal terminals it is parked
// once n exceeds the retry schedule's length, otherwise it backs off until the
// most recent abnormal terminal plus the nth escalating delay. A park-clear
// event newer than that terminal lifts both backoff and park. No timer or flag
// is persisted — the history is the source of truth.
func setBackoffStatus(info tasks.SetBackoffInfo, delays []time.Duration, now time.Time) (parked bool, until time.Time) {
	n := info.ConsecutiveAbnormal
	if n == 0 {
		return false, time.Time{}
	}
	if !info.ParkClearedAt.IsZero() && info.ParkClearedAt.After(info.LastAbnormalAt) {
		return false, time.Time{}
	}
	if len(delays) == 0 || n > len(delays) {
		return true, time.Time{}
	}
	candidate := info.LastAbnormalAt.Add(delays[n-1])
	if candidate.After(now) {
		return false, candidate
	}
	return false, time.Time{}
}

// setBackoffLookup builds the per-set abnormal-backoff probe used during
// dispatch. It reads each set's Drain history under the repository's common dir
// and applies the configured escalation schedule. Read errors and a missing
// store degrade to "spawnable", never blocking dispatch on a transient store
// problem.
func (d *Deps) setBackoffLookup(repoCommonDir string, delays []time.Duration, now time.Time) setBackoffFunc {
	if d == nil || d.Tasks == nil || strings.TrimSpace(repoCommonDir) == "" {
		return nil
	}
	return func(setID string) (bool, time.Time) {
		info, err := tasks.ReadSetBackoff(d.Tasks, repoCommonDir, setID)
		if err != nil {
			return false, time.Time{}
		}
		return setBackoffStatus(info, delays, now)
	}
}

func resolveRepoKey(d *Deps, projectPath string) (string, error) {
	if d == nil || d.Tasks == nil {
		return "", fmt.Errorf("missing task dependencies")
	}
	id, err := tasks.ResolveRepositoryIdentity(d.Tasks, projectPath)
	if err != nil {
		return "", err
	}
	return repoIdentityKey(id), nil
}

func scopedKeyForPaths(d *Deps, projectPath, runtimePath, setID string) (string, error) {
	repoKey := repoIdentityFromWorktreePath(runtimePath)
	if repoKey == "" {
		rk, err := resolveRepoKey(d, projectPath)
		if err != nil {
			return "", err
		}
		return setScopedKey(rk, setID), nil
	}
	return setScopedKey(repoKey, setID), nil
}

// provisionWorktree is the Queue's adapter over the binding module's
// provisioner. The worktree directory tree lives under the queue data dir; the
// binding module owns the `git worktree add` and path-layout details.
func provisionWorktree(d *Deps, projectPath, setID string) (provisionedWorktree, error) {
	if d == nil || d.Tasks == nil {
		return provisionedWorktree{}, fmt.Errorf("missing task dependencies")
	}
	worktreesRoot := filepath.Join(QueueDataDir(d.Tasks), "worktrees")
	b, err := binding.ProvisionWorktree(d.Tasks, worktreesRoot, projectPath, setID, d.now())
	if err != nil {
		return provisionedWorktree{}, err
	}
	return provisionedWorktree{Path: b.RuntimePath, Branch: b.Branch}, nil
}

// Spawn launches the selected drain into a new pane of the project's tmux
// session, creating the session detached when absent. It is a no-op for
// non-actionable decisions.
func Spawn(d *Deps, dec Decision) error {
	_, err := SpawnWithResult(d, dec)
	return err
}

type SpawnResult struct {
	PaneID string
}

func SpawnWithResult(d *Deps, dec Decision) (SpawnResult, error) {
	if !dec.Actionable() {
		return SpawnResult{}, nil
	}
	command := fmt.Sprintf("pop tasks implement %s", shellQuote(dec.TaskSetID))
	if dec.WorktreeReady && dec.scan.RuntimePath != "" {
		command += " --task-runtime-path " + shellQuote(dec.scan.RuntimePath)
	}
	paneID, err := spawnDrain(d.Tmux, dec.scan.SessionName, dec.scan.ProjectPath, dec.TaskSetID, command)
	return SpawnResult{PaneID: paneID}, err
}

func recordDrainPane(d *Deps, dec Decision, paneID, source string) error {
	if d == nil || d.Tasks == nil || paneID == "" || dec.TaskSetID == "" {
		return nil
	}
	key, err := scopedKeyForPaths(d, dec.scan.ProjectPath, dec.scan.RuntimePath, dec.TaskSetID)
	if err != nil {
		return err
	}
	state, err := EnsureDaemonState(d.Tasks)
	if err != nil {
		return err
	}
	if state.DrainPanes == nil {
		state.DrainPanes = map[string]DrainPane{}
	}
	state.DrainPanes[key] = DrainPane{
		Project:     dec.Project,
		RuntimePath: dec.scan.RuntimePath,
		SetID:       dec.TaskSetID,
		PaneID:      paneID,
		RecordedAt:  d.now().UTC(),
		Source:      source,
	}
	return WriteDaemonState(d.Tasks, state)
}

// spawnDrain creates (if needed) the detached session and shared queue window,
// then sends the drain command to this set's existing pane or a freshly split
// tagged pane.
func spawnDrain(tmux deps.Tmux, session, dir, setID, command string) (string, error) {
	if !tmux.HasSession(session) {
		if err := tmux.NewSession(session, dir); err != nil {
			return "", fmt.Errorf("create session %q: %w", session, err)
		}
	}

	windowTarget, freshPaneID, err := resolveDrainWindowTarget(tmux, session, dir)
	if err != nil {
		return "", err
	}

	paneID, err := findDrainPaneForSet(tmux, windowTarget, setID)
	if err != nil {
		return "", err
	}
	if paneID != "" {
		if _, err := tmux.Command("send-keys", "-t", paneID, command, "Enter"); err != nil {
			return "", fmt.Errorf("send drain command: %w", err)
		}
		return paneID, nil
	}

	if freshPaneID != "" {
		// The queue window was just created; reuse its initial pane instead of
		// splitting, so a fresh window holds a single drain pane.
		paneID = freshPaneID
	} else {
		out, err := tmux.Command("split-window", "-d", "-P", "-F", "#{pane_id}", "-t", windowTarget, "-c", dir)
		if err != nil {
			return "", fmt.Errorf("create drain pane: %w", err)
		}
		paneID = strings.TrimSpace(out)
		if paneID == "" {
			return "", fmt.Errorf("create drain pane: tmux returned no pane id")
		}
		if _, err := tmux.Command("select-layout", "-t", windowTarget, "tiled"); err != nil {
			return "", fmt.Errorf("retile drain window: %w", err)
		}
	}

	if _, err := tmux.Command("set-option", "-p", "-t", paneID, "@pop_set", setID); err != nil {
		return "", fmt.Errorf("tag drain pane: %w", err)
	}
	if _, err := tmux.Command("send-keys", "-t", paneID, command, "Enter"); err != nil {
		return "", fmt.Errorf("send drain command: %w", err)
	}
	return paneID, nil
}

func findDrainPaneForSet(tmux deps.Tmux, windowTarget, setID string) (string, error) {
	out, err := tmux.Command("list-panes", "-t", windowTarget, "-F", "#{@pop_set} #{pane_id}")
	if err != nil {
		return "", fmt.Errorf("list drain panes in %q: %w", windowTarget, err)
	}
	for _, line := range splitLines(out) {
		tag, paneID, ok := parseDrainPaneTagLine(line)
		if ok && tag == setID {
			return paneID, nil
		}
	}
	return "", nil
}

func parseDrainPaneTagLine(line string) (tag, paneID string, ok bool) {
	line = strings.TrimSpace(line)
	idx := strings.LastIndex(line, " %")
	if idx < 0 {
		return "", "", false
	}
	tag = strings.TrimSpace(line[:idx])
	paneID = strings.TrimSpace(line[idx+1:])
	return tag, paneID, tag != "" && paneID != ""
}

// resolveDrainWindowTarget returns the queue window target, creating it when
// absent. When it creates the window, it also returns the id of the window's
// initial pane (started in dir) so the caller can reuse it instead of splitting
// a second pane; the pane id is empty when the window already existed.
func resolveDrainWindowTarget(tmux deps.Tmux, session, dir string) (target, freshPaneID string, err error) {
	target = session + ":" + drainWindowName
	out, err := tmux.Command("list-windows", "-t", session, "-F", "#{window_name}")
	if err != nil {
		return "", "", fmt.Errorf("list windows in %q: %w", session, err)
	}
	for _, line := range splitLines(out) {
		if line == drainWindowName {
			return target, "", nil
		}
	}
	out, err = tmux.Command("new-window", "-d", "-a", "-P", "-F", "#{pane_id}", "-t", session, "-n", drainWindowName, "-c", dir)
	if err != nil {
		return "", "", fmt.Errorf("create queue window in %q: %w", session, err)
	}
	freshPaneID = strings.TrimSpace(out)
	if freshPaneID == "" {
		return "", "", fmt.Errorf("create queue window in %q: tmux returned no pane id", session)
	}
	return target, freshPaneID, nil
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
