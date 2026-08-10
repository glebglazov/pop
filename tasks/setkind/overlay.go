package setkind

import (
	"errors"
	"strings"
	"time"

	"github.com/glebglazov/pop/config"
	"github.com/glebglazov/pop/repogroup"
	"github.com/glebglazov/pop/tasks"
	"github.com/glebglazov/pop/tasks/binding"
	"github.com/glebglazov/pop/work"
)

// The per-pass volatile overlay: everything that changes between two reads of the
// same repo group. It is deliberately one store read per pass rather than one per
// container (see snapshot below).

func (d *Deps) now() time.Time {
	if d != nil && d.Now != nil {
		return d.Now()
	}
	return time.Now()
}

// config resolves the configuration this pass reads, loading it through the
// LoadConfig seam when the adapter was constructed without one.
func (d *Deps) config() *config.Config {
	if d.Config != nil {
		return d.Config
	}
	if d.LoadConfig != nil {
		cfg, _ := d.LoadConfig(config.DefaultConfigPath())
		return cfg
	}
	return nil
}

// refresh resolves the Refresh seam, defaulting to tasks.RefreshWith — or, when
// the active view preset wants archived rows, to the refresh that returns
// archived and active sets together (ADR-0197).
func (d *Deps) refresh(defPath string) (*tasks.RefreshResult, error) {
	if d.Refresh != nil {
		return d.Refresh(defPath)
	}
	statePath := tasks.StatePathFor(defPath)
	if tasks.PresetWantsArchived(d.viewPreset()) {
		return tasks.RefreshIncludingArchivedWith(d.Tasks, defPath, statePath)
	}
	return tasks.RefreshWith(d.Tasks, defPath, statePath)
}

// prepareGroups takes the write-side and store-side half of every group's
// refresh up front, serially, and pairs each group with it (ADR-0189). An
// injected Refresh seam owns the whole read, so there is nothing to prepare for
// it: each group is paired with nil, which every reader of a prepared value
// already answers for.
func (d *Deps) prepareGroups(groups []repogroup.Group) ([]groupLoad, error) {
	loads := make([]groupLoad, 0, len(groups))
	if d.Refresh != nil {
		for _, g := range groups {
			loads = append(loads, groupLoad{group: g})
		}
		return loads, nil
	}
	defPaths := make([]string, 0, len(groups))
	for _, g := range groups {
		defPaths = append(defPaths, g.DefPath)
	}
	prepared, err := tasks.PrepareRefreshes(d.Tasks, defPaths)
	if err != nil {
		return nil, err
	}
	for i, g := range groups {
		loads = append(loads, groupLoad{group: g, prepared: prepared[i]})
	}
	return loads, nil
}

// refreshPrepared reads one group's rows from its prepared half. This is the
// call a load fans out: with the seam injected it is the seam, and otherwise it
// is discovery plus manifest loading, which touch no store and write nothing.
func (d *Deps) refreshPrepared(prep *tasks.PreparedRefresh, defPath string) (*tasks.RefreshResult, error) {
	if d.Refresh != nil {
		return d.Refresh(defPath)
	}
	if tasks.PresetWantsArchived(d.viewPreset()) {
		return prep.RefreshIncludingArchived(d.Tasks)
	}
	return prep.Refresh(d.Tasks)
}

// worktreeIntentsFor is the seeded worktree directives for one group, served from
// the prepared registration when the load has one and read from the store when it
// does not (the injected-seam path, which prepares nothing).
func (d *Deps) worktreeIntentsFor(prep *tasks.PreparedRefresh, defPath string) map[string]*tasks.WorktreeDirective {
	if prep != nil {
		return prep.WorktreeIntents()
	}
	return worktreeIntents(d, defPath)
}

// rendersRow reports whether a refresh row survives this pass's view preset. It
// is the single answer both the verify-overlay narrowing and the container loop
// read, which is what makes "rendered" and "resolved" one set of rows
// (ADR-0189).
//
// The two readings agree even though the overlay sits between them: the overlay
// moves only a terminal row's status, and only away from DONE, so a row this
// predicate admits still passes after the overlay and a DONE row it rejects was
// never resolved and so still reads DONE.
func (d *Deps) rendersRow(row tasks.Row) bool {
	return tasks.MatchesPreset(tasks.RowViewFacts(row), d.viewPreset(), d.now())
}

// viewPreset returns the Work view preset this pass evaluates, falling back to
// the shipped active definition when unset (ADR-0197).
func (d *Deps) viewPreset() config.WorkViewPreset {
	if d != nil && strings.TrimSpace(d.ViewPreset.Name) != "" {
		return d.ViewPreset
	}
	if p, ok := config.ShippedWorkViewPreset("active"); ok {
		return p
	}
	return config.WorkViewPreset{}
}

// liveDrains resolves the LiveDrains seam, defaulting to tasks.LiveRunningDrains.
func (d *Deps) liveDrains() ([]tasks.RunningDrain, error) {
	if d.LiveDrains != nil {
		return d.LiveDrains()
	}
	return tasks.LiveRunningDrains(d.Tasks)
}

// probeDirective resolves the ProbeDirective seam, defaulting to a read-only
// binding.ProbeWorktreeDirective probe. It returns a config-class error message
// only for the two unsatisfiable-directive sentinels (ADR-0059); any other probe
// error yields "".
func (d *Deps) probeDirective(checkout, setID string) string {
	if d.ProbeDirective != nil {
		return d.ProbeDirective(checkout, setID)
	}
	err := binding.ProbeWorktreeDirective(d.Tasks, d.Project, d.config(), checkout, setID)
	if errors.Is(err, binding.ErrNoResolvableTrunk) || errors.Is(err, binding.ErrNamedWorktreeNotFound) {
		return err.Error()
	}
	return ""
}

// setBackoffLookup builds the per-set abnormal-backoff probe used to derive the
// parked fact. It reads each set's Drain history under the repository's common
// dir and applies the configured escalation schedule. Read errors and a missing
// store degrade to "not parked", never blocking the load.
func (d *Deps) setBackoffLookup(repoCommonDir string, delays []time.Duration, now time.Time) func(setID string) bool {
	if d == nil || d.Tasks == nil || strings.TrimSpace(repoCommonDir) == "" {
		return nil
	}
	return func(setID string) bool {
		info, err := tasks.ReadSetBackoff(d.Tasks, repoCommonDir, setID)
		if err != nil {
			return false
		}
		return setBackoffParked(info, delays, now)
	}
}

// setBackoffParked derives whether a set is parked from its Drain history
// (ADR-0055): with n consecutive abnormal terminals it is parked once n exceeds
// the retry schedule's length. A park-clear event newer than the most recent
// abnormal terminal lifts it. Only the parked fact is derived here; the dashboard
// does not surface the backoff until-instant.
func setBackoffParked(info tasks.SetBackoffInfo, delays []time.Duration, now time.Time) bool {
	n := info.ConsecutiveAbnormal
	if n == 0 {
		return false
	}
	if !info.ParkClearedAt.IsZero() && info.ParkClearedAt.After(info.LastAbnormalAt) {
		return false
	}
	return len(delays) == 0 || n > len(delays)
}

// snapshot is a single point-in-time read of pop.db taken once per load. Each
// container's binding, live-drain, and drain-pane lookup is served from these
// in-memory maps instead of reopening the store per container, so the whole view
// is one consistent snapshot and the store is opened a bounded number of times
// per pass.
type snapshot struct {
	bindings   map[string]binding.Binding
	liveDrains map[string]tasks.RunningDrain
	drainPanes map[string]tasks.DrainPane
}

// newSnapshot reads the volatile per-pass store state once: AllBindings, the
// live running drains (filtered to PID-alive), and the recorded drain panes. It
// is the single point at which a load touches pop.db for the overlay.
func newSnapshot(d *Deps) (*snapshot, error) {
	snap := &snapshot{
		bindings:   map[string]binding.Binding{},
		liveDrains: map[string]tasks.RunningDrain{},
		drainPanes: map[string]tasks.DrainPane{},
	}
	if d == nil || d.Tasks == nil {
		return snap, nil
	}
	bindings, err := binding.AllBindings(d.Tasks)
	if err != nil {
		return nil, err
	}
	for k, b := range bindings {
		snap.bindings[k] = b
	}
	drains, err := d.liveDrains()
	if err != nil {
		return nil, err
	}
	for _, dr := range drains {
		snap.liveDrains[dr.RuntimePath] = dr
	}
	panes, err := tasks.AllDrainPanes(d.Tasks)
	if err != nil {
		return nil, err
	}
	for k, p := range panes {
		snap.drainPanes[k] = p
	}
	return snap, nil
}

// liveDrainList is the pass's live Drains as a list, which is how attribution
// reads them: it asks which drain's checkout contains a directory, not which drain
// holds a known path.
func (s *snapshot) liveDrainList() []tasks.RunningDrain {
	if s == nil || len(s.liveDrains) == 0 {
		return nil
	}
	out := make([]tasks.RunningDrain, 0, len(s.liveDrains))
	for _, dr := range s.liveDrains {
		out = append(out, dr)
	}
	return out
}

// bindingFor returns the snapshot binding for (repoKey, setID).
func (s *snapshot) bindingFor(repoKey, setID string) (binding.Binding, bool) {
	b, ok := s.bindings[binding.ScopedKey(repoKey, setID)]
	return b, ok
}

// worktreeView is the resolved destination column for one container.
type worktreeView struct {
	label       string
	runtimePath string
	DestKind    work.DestKind
}

// worktreeIntents loads seeded worktree directives for one definition path in a
// single store read, keyed by set ID. The per-container destination column
// consults this map instead of reopening the store for each unbound set.
func worktreeIntents(d *Deps, defPath string) map[string]*tasks.WorktreeDirective {
	intents := map[string]*tasks.WorktreeDirective{}
	if d == nil || d.Tasks == nil {
		return intents
	}
	state, err := tasks.LoadGlobalStateWith(d.Tasks, tasks.StatePathFor(defPath))
	if err != nil {
		return intents
	}
	entry := state.Tasks[defPath]
	if entry == nil {
		return intents
	}
	for _, set := range entry.TaskSets {
		if set.WorktreeIntent != nil {
			intents[set.ID] = set.WorktreeIntent
		}
	}
	return intents
}

// worktree resolves the destination column per ADR-0070/0072: a bound set shows
// its branch plainly; an unbound set with a managed directive shows a [managed
// wt] badge; an unbound set with no directive shows needs bind; a Done set that
// still holds a managed binding shows [managed wt <branch>] as a clean-up
// reminder.
func worktree(d *Deps, snap *snapshot, intents map[string]*tasks.WorktreeDirective, repoKey, setID string, status tasks.TaskSetStatus, bnd binding.Binding, bound bool) worktreeView {
	if bound {
		branch := bnd.Branch
		if branch == "" {
			branch = repogroup.HeadBranch(d.Tasks, bnd.RuntimePath, "")
		}
		branch = formatBranch(branch)
		kind := work.DestBound
		if status == tasks.StatusDone && bnd.Provisioned {
			kind = work.DestDoneManagedBound
		}
		return worktreeView{label: branch, runtimePath: bnd.RuntimePath, DestKind: kind}
	}
	intent := intents[setID]
	if intent != nil && intent.Managed {
		return worktreeView{label: work.DestLabelManagedWt, DestKind: work.DestManagedDirective}
	}
	return worktreeView{label: work.DestLabelNeedsBind, DestKind: work.DestNeedsBind}
}

// formatBranch normalizes a branch name for the destination column.
func formatBranch(branch string) string {
	branch = strings.TrimSpace(branch)
	if branch == "" {
		return "detached"
	}
	return branch
}

// liveDrain reports whether a live (PID-alive) drain holds the set's checkout,
// reading the per-pass snapshot's live-drain map instead of reopening the runtime
// lock per container. It is the structured boolean the sort tier, header count,
// auto-drain silencing, and IN-PROGRESS refinement all key on (ADR-0111).
func liveDrain(snap *snapshot, repoKey, setID, runtimePath string) bool {
	paths := map[string]bool{}
	if runtimePath != "" {
		paths[runtimePath] = true
	}
	if b, ok := snap.bindingFor(repoKey, setID); ok && b.RuntimePath != "" {
		paths[b.RuntimePath] = true
	}
	for path := range paths {
		if dr, ok := snap.liveDrains[path]; ok && dr.SetID == setID {
			return true
		}
	}
	return false
}

// orphaned reports whether a set's Worktree binding points at a checkout that no
// longer exists on disk. Detection is a single cheap filesystem stat of the
// binding's runtime path — never a git subprocess. A set with no binding (or one
// with a blank runtime path) can never be orphaned.
func orphaned(d *Deps, bnd binding.Binding, hasBinding bool) bool {
	if !hasBinding {
		return false
	}
	path := strings.TrimSpace(bnd.RuntimePath)
	if path == "" {
		return false
	}
	if d == nil || d.Tasks == nil || d.Tasks.FS == nil {
		return false
	}
	_, err := d.Tasks.FS.Stat(path)
	return err != nil
}

// paneID returns the tmux pane recorded for a live drain of (repoKey, setID),
// empty if none was recorded.
func paneID(snap *snapshot, repoKey, setID string) string {
	if snap == nil || snap.drainPanes == nil {
		return ""
	}
	if pane, ok := snap.drainPanes[binding.ScopedKey(repoKey, setID)]; ok {
		return pane.PaneID
	}
	return ""
}

// groups resolves the repository groups this pass reads, through the injected
// seam when one was supplied.
func (d *Deps) groups(cfg *config.Config) ([]repogroup.Group, error) {
	if d.Groups != nil {
		return d.Groups()
	}
	return repogroup.Resolve(&repogroup.Deps{Tasks: d.Tasks, Project: d.Project}, cfg)
}
