// Package setkind is the Task-set Work kind: the adapter that makes a task set
// comply with `work.Kind`. It lives kind-side of the seam — `work` imports no
// kind — and one level down from `tasks` only because the adapter needs
// tasks/binding, which imports tasks; nothing else about it belongs anywhere but
// beside the kind it speaks for.
//
// Everything here was `work`'s snapshot builder before the seam existed: the
// fork-free static resolution per repository group (now repogroup), the single
// per-build pop.db read, and the per-set derivation of the destination column,
// live-drain, orphan, park and config-error facts (ADR-0143, ADR-0060).
package setkind

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/glebglazov/pop/config"
	"github.com/glebglazov/pop/project"
	"github.com/glebglazov/pop/repogroup"
	"github.com/glebglazov/pop/tasks"
	"github.com/glebglazov/pop/tasks/binding"
	"github.com/glebglazov/pop/work"
	"github.com/glebglazov/pop/work/ref"
)

// Deps holds the Task-set kind's external dependencies. It borrows the
// process-cached Execution-state store handle through Tasks (ADR-0140): every
// store touch goes through a tasks/binding accessor in if-exists mode, so a pure
// read never materialises a database and the handle is never closed here. The
// seams mirror the ones the Work snapshot builder carried, so injected test
// doubles keep working after the move behind the seam.
type Deps struct {
	Tasks      *tasks.Deps
	Project    *project.Deps
	LoadConfig func(string) (*config.Config, error)

	// Config is the resolved configuration this adapter was constructed with. Left
	// nil it is loaded through LoadConfig on first use — the kind is constructed
	// with its dependencies captured, so the config it reads is the caller's.
	Config *config.Config

	// IncludeDone is the Done-inclusion view flag (ADR-0121): DONE sets are hidden
	// unless this is true.
	IncludeDone bool

	// Groups resolves the repository groups to scan for task sets. Defaults to
	// repogroup.Resolve over Tasks and Project — injectable because a test wants to
	// name its groups rather than lay out a machine, and because a caller building
	// several kinds can hand them all one resolution per build.
	Groups func() ([]repogroup.Group, error)

	// Refresh returns the Task-set rows registered under a definition path.
	// Defaults to tasks.RefreshWith.
	Refresh func(defPath string) (*tasks.RefreshResult, error)
	// LiveDrains returns every running Drain whose owning process is still alive.
	// The load reads it once per pass into its snapshot. Defaults to
	// tasks.LiveRunningDrains.
	LiveDrains func() ([]tasks.RunningDrain, error)
	// Now returns the current time. Defaults to time.Now.
	Now func() time.Time
	// ProbeDirective reports a config/registration-class error message when a
	// Ready set's worktree directive cannot be satisfied (ADR-0059) — read-only.
	// Empty means satisfiable or no directive. Defaults to a
	// binding.ProbeWorktreeDirective probe surfacing only the two directive
	// sentinels.
	ProbeDirective func(checkout, setID string) string
}

// DefaultDeps returns Task-set kind dependencies backed by real implementations.
func DefaultDeps() *Deps {
	return &Deps{
		Tasks:      tasks.DefaultDeps(),
		Project:    project.DefaultDeps(),
		LoadConfig: config.Load,
	}
}

// Kind is the Task-set Work kind.
type Kind struct {
	d *Deps
}

// New returns the Task-set kind over d. A nil d resolves to DefaultDeps.
func New(d *Deps) *Kind {
	if d == nil {
		d = DefaultDeps()
	}
	if d.Tasks == nil {
		d.Tasks = tasks.DefaultDeps()
	}
	if d.Project == nil {
		d.Project = project.DefaultDeps()
	}
	return &Kind{d: d}
}

// ID is the closed enum's task-set member.
func (k *Kind) ID() work.KindID { return ref.KindTaskSet }

// Load reads every registered task set worth showing, as Work containers. It is
// a pure read — the crash-detection pass it used to run before reading is the
// Work supervisor's explicit phase now, so rendering a task set writes nothing —
// and it forks no git (identity, integration target and branch all derive from
// each repo's repo.json marker plus config, ADR-0060).
func (k *Kind) Load() ([]work.Container, error) {
	d := k.d
	cfg := d.config()
	groups, err := d.groups(cfg)
	if err != nil {
		return nil, err
	}
	if len(groups) == 0 {
		return nil, nil
	}
	var delays []time.Duration
	if qcfg, qerr := cfg.ResolveWorkDaemon(); qerr == nil {
		delays = qcfg.CrashRetryDelays
	}
	snap, err := newSnapshot(d)
	if err != nil {
		return nil, err
	}
	now := d.now().UTC()
	var containers []work.Container
	for _, g := range groups {
		got, err := containersFromGroup(d, cfg, snap, delays, now, g)
		if err != nil {
			return nil, err
		}
		containers = append(containers, got...)
	}
	return containers, nil
}

// Less orders two task-set containers by the shared Queue surface comparator
// (ADR-0121) over their row projections — the same total order `pop work
// dashboard` and `pop work status` have always read, ported wholesale rather
// than re-derived, so the seam's ordering is that comparator and not a second one.
func (k *Kind) Less(a, b work.Container) bool {
	return tasks.WorkRowLess(a, b)
}

// StatusCell composes a task set's STATUS cell: the display label with the
// READY→IN PROGRESS refinement, then the verified-at, auto-drain, orphaned,
// parked and config-error suffixes in that fixed order (ADR-0108, ADR-0111). The
// composition itself is the Task-set model's, ported here unchanged.
func (k *Kind) StatusCell(c work.Container) []work.StatusSegment {
	return tasks.WorkRowStatusSegments(c)
}

// Columns are the Task-set page's column headers. They are the Work dashboard's
// long-standing five — the trailing one deliberately unlabelled, since the
// per-activity glyph cluster under it has no name a header could carry. A page
// whose primary kind is this one reads under them, and a Map on that page fills
// the same cells.
func (k *Kind) Columns() []string {
	return TaskSetColumns()
}

// TaskSetColumns is the Task-set column header row, exported because a kind that
// fills these cells rather than its own must head its page with the identical
// list — a copy in the other kind would be a second source of one header.
func TaskSetColumns() []string {
	return []string{"PROJECT", "TASK SET", "STATUS", "WORKTREE", ""}
}

// ModelSkips reports the Effort model skips in force (ADR-0168). They are
// machine-global rather than per-container, which is why the kind reports them
// through the snapshot's footnote extension rather than as a container field: the
// footer explains a ladder walking its tail regardless of which rows are on
// screen, so it holds even for a machine with no renderable repo group.
func (k *Kind) ModelSkips() ([]work.ModelSkip, error) {
	rows, err := tasks.ActiveAgentModelCooldownsWith(k.d.Tasks, k.d.now().UTC())
	if err != nil {
		return nil, err
	}
	skips := make([]work.ModelSkip, 0, len(rows))
	for _, row := range rows {
		skips = append(skips, work.ModelSkip{Preset: row.Preset, Model: row.Model, Until: row.Until})
	}
	sort.Slice(skips, func(i, j int) bool {
		if skips[i].Preset != skips[j].Preset {
			return skips[i].Preset < skips[j].Preset
		}
		return skips[i].Model < skips[j].Model
	})
	if len(skips) == 0 {
		return nil, nil
	}
	return skips, nil
}

// Summary returns the Task-set kind's header phrases: the set count always, then
// the ready, running and auto-drain tallies when non-zero. The counts read the
// row projection because they are this kind's own facts — there is no shared
// status taxonomy to count against.
func (k *Kind) Summary(containers []work.Container) []string {
	ready, running, autoDrain := 0, 0, 0
	for _, c := range containers {
		if c.RawStatus == tasks.StatusReady {
			ready++
		}
		if c.LiveDrain {
			running++
		}
		if work.AutoDrainWaiting(c) {
			autoDrain++
		}
	}
	phrases := []string{work.CountPhrase(len(containers), "task set", "task sets")}
	if ready > 0 {
		phrases = append(phrases, work.CountPhrase(ready, "ready", "ready"))
	}
	if running > 0 {
		phrases = append(phrases, work.CountPhrase(running, "running", "running"))
	}
	if autoDrain > 0 {
		phrases = append(phrases, work.CountPhrase(autoDrain, "auto-drain", "auto-drain"))
	}
	return phrases
}

// containersForGroup renders one repo group's containers from a fully resolved
// group plus the current volatile overlay, taking the single per-pass store
// snapshot. It is the seam tests drive with a hand-built group, mirroring what
// Load does per group after resolving them fork-free from markers (ADR-0143). The
// kind's public surface stays containers-out.
func containersForGroup(d *Deps, cfg *config.Config, g repogroup.Group) ([]work.Container, error) {
	var delays []time.Duration
	if qcfg, qerr := cfg.ResolveWorkDaemon(); qerr == nil {
		delays = qcfg.CrashRetryDelays
	}
	snap, err := newSnapshot(d)
	if err != nil {
		return nil, err
	}
	return containersFromGroup(d, cfg, snap, delays, d.now().UTC(), g)
}

// containersFromGroup builds a repo group's containers from its static resolution
// plus the current volatile state: task statuses (refresh), runtime locks, and
// daemon-state columns. It forks no git — the static side is marker/config
// derived (ADR-0060) and this overlay is cheap file/store reads.
func containersFromGroup(d *Deps, cfg *config.Config, snap *snapshot, delays []time.Duration, now time.Time, g repogroup.Group) ([]work.Container, error) {
	refresh, err := d.refresh(g.DefPath)
	if err != nil {
		return nil, err
	}
	tasks.ApplyVerifyVerdictsWith(d.Tasks, refresh, cfg, func(setID string) string {
		return binding.RuntimeForSet(snap.bindings, g.RepoKey, setID)
	})
	intents := worktreeIntents(d, g.DefPath)
	backoff := d.setBackoffLookup(g.RepoCommonDir, delays, now)
	var containers []work.Container
	for _, taskRow := range refresh.Rows {
		bnd, hasBinding := snap.bindingFor(g.RepoKey, taskRow.ID)
		bound := hasBinding && strings.TrimSpace(bnd.RuntimePath) != ""
		doneStillManagedBound := taskRow.Status == tasks.StatusDone && bound && bnd.Provisioned
		orphanedSet := orphaned(d, bnd, hasBinding)
		if !tasks.ShowRow(taskRow, d.IncludeDone) {
			continue
		}
		wt := worktree(d, snap, intents, g.RepoKey, taskRow.ID, taskRow.Status, bnd, bound)
		parked := false
		if backoff != nil {
			parked = backoff(taskRow.ID)
		}
		liveDrainSet := liveDrain(snap, g.RepoKey, taskRow.ID, wt.runtimePath)
		// A live drain lights the trailing ● indicator (ADR-0111); parked and
		// config-error ride the STATUS cell. The mutual exclusion the retired
		// single-string DRAIN cell enforced is preserved by gating the config-error
		// probe on a set that is neither live-drained nor parked.
		configErr := ""
		if !liveDrainSet && !parked {
			if g.ConfigError != "" && !hasBinding {
				// Bare repo with no declared trunk: an unbound set has no integration
				// target to route to (ADR-0060). A bound set is still drainable via its
				// binding, so it is left untouched.
				configErr = g.ConfigError
			} else if taskRow.Status == tasks.StatusReady {
				// An unsatisfiable worktree directive is a static config defect
				// (ADR-0059). Read the registration intent first (a store read, no git);
				// only a set that carries a directive pays the read-only probe.
				if intent, _ := tasks.RegisteredWorktreeIntent(d.Tasks, g.DefPath, taskRow.ID); intent != nil {
					if msg := d.probeDirective(g.CheckoutPath(), taskRow.ID); msg != "" {
						configErr = msg
					}
				}
			}
		}
		container := work.Container{
			Kind:                  ref.KindTaskSet,
			ID:                    taskRow.ID,
			Project:               g.ProjectName,
			RawStatus:             taskRow.Status,
			AutoDrain:             taskRow.AutoDrain,
			DefPath:               g.DefPath,
			StatePath:             g.StatePath,
			RepoKey:               g.RepoKey,
			RepoCommonDir:         g.RepoCommonDir,
			ProjectPath:           g.CheckoutPath(),
			RuntimePath:           wt.runtimePath,
			DoneStillManagedBound: doneStillManagedBound,
			Parked:                parked,
			ConfigError:           configErr,
			Orphaned:              orphanedSet,
			Bound:                 bound,
			PaneID:                paneID(snap, g.RepoKey, taskRow.ID),
			LiveDrain:             liveDrainSet,
			Started:               taskRow.Started,
			VerifiedAtSHA:         taskRow.VerifiedAtSHA,
			VerifiedAtDrifted:     taskRow.VerifiedAtDrifted,
			VerifyMark:            taskRow.VerifyMark,
			Worktree:              wt.label,
			CursorKey:             g.ProjectName + "\x00" + taskRow.ID,
			DestKind:              wt.DestKind,
			Items:                 itemsFor(refresh, taskRow.ID),
			Headline:              taskRow.Progress,
		}
		container.Broken, container.BrokenReason = brokenFor(refresh, taskRow.ID)
		container.Status = tasks.WorkRowStatusLabel(container)
		container.Checkout = shellDir(container)
		containers = append(containers, container)
	}
	return containers, nil
}

// itemsFor projects a set's manifest tasks onto Work items. The manifest is
// already parsed by the refresh the statuses came from, so the items cost nothing
// extra to carry.
func itemsFor(refresh *tasks.RefreshResult, setID string) []work.Item {
	return ItemsFromManifest(manifestFor(refresh, setID))
}

// ItemsFromManifest projects one parsed manifest's tasks onto Work items. It is
// exported because it is the whole of how a task becomes an item — a caller that
// has a manifest in hand and wants the items a set would carry must get the same
// projection, not a second one that drifts.
func ItemsFromManifest(m *tasks.Manifest) []work.Item {
	if m == nil {
		return nil
	}
	items := make([]work.Item, 0, len(m.Tasks))
	for _, task := range m.Tasks {
		items = append(items, work.Item{
			ID:          task.ID,
			Title:       task.Title,
			Type:        task.Type,
			Status:      string(task.Status),
			StatusLabel: taskStatusLabel(task),
			Blocked:     len(task.BlockedBy) > 0 && task.Status == tasks.TaskOpen,
			BlockedBy:   task.BlockedBy,
			File:        filepath.Join(m.Dir, task.File),
		})
	}
	return items
}

// taskStatusLabel is the display embellishment a task status carries beyond its
// word: a failed task folds its retry count into the label (`failed(2)`), which
// is the whole of the difference between a task's status and how it reads.
func taskStatusLabel(task tasks.Task) string {
	if task.Status == tasks.TaskFailed && task.FailedAfter != nil {
		return fmt.Sprintf("failed(%d)", *task.FailedAfter)
	}
	return ""
}

// brokenFor reports a set whose definition could not be read as a set: a
// manifest that failed validation. The reason is the manifest's own diagnostics,
// which is what a reader needs to fix it.
func brokenFor(refresh *tasks.RefreshResult, setID string) (bool, string) {
	m := manifestFor(refresh, setID)
	if m == nil || m.Valid {
		return false, ""
	}
	return true, strings.Join(m.Errors, "; ")
}

// manifestFor returns the parsed manifest a refresh already holds for setID.
func manifestFor(refresh *tasks.RefreshResult, setID string) *tasks.Manifest {
	if refresh == nil || refresh.Manifests == nil {
		return nil
	}
	return refresh.Manifests[setID]
}

// shellDir is the directory a shell or handoff verb runs in for a set: its bound
// runtime checkout, and nothing else. An unbound set resolves none — a shell
// "in" a set that has no checkout of its own would land in the shared
// integration target, which is not where work on that set belongs.
func shellDir(row work.Container) string {
	return strings.TrimSpace(row.RuntimePath)
}
