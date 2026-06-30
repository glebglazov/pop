package queue

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/glebglazov/pop/config"
	"github.com/glebglazov/pop/internal/deps"
	"github.com/glebglazov/pop/project"
	"github.com/glebglazov/pop/tasks"
	"github.com/glebglazov/pop/tasks/binding"
	"github.com/glebglazov/pop/ui"
)

const dashboardPollInterval = 2 * time.Second

// dashboardDrainPickedUp is the Drain-column value for a set whose checkout is
// held by a live drain (running / Picked-up). It is the tier-1 sort signal.
const dashboardDrainPickedUp = "picked up"

// DashboardRow is one read-only Queue dashboard table row.
type DashboardRow struct {
	Project string
	SetID   string
	// Status is the rendered display label (e.g. "IN PROGRESS", "DONE · merged").
	// Logic that needs the schedulability fact must read RawStatus, never parse
	// this string.
	Status string
	// RawStatus is the underlying derived Task-set status, kept for counts and
	// comparisons so display relabels never leak into logic.
	RawStatus tasks.TaskSetStatus
	Worktree  string
	Drain     string
	AutoDrain bool

	defPath            string
	statePath          string
	cursorKey          string
	repoKey            string
	repoCommonDir      string
	projectPath        string
	runtimePath        string
	paneID         string
	// doneStillManagedBound is true when a Done set still holds a pop-provisioned
	// (managed) Worktree binding. The dashboard keeps such a row visible as a
	// clean-up reminder until archived or unbound (ADR-0070).
	doneStillManagedBound bool
	// destKind selects how the destination column is styled; Worktree holds the
	// plain label (branch name, "[managed wt]", or "needs bind").
	destKind dashboardDestKind
	// parked is true when the set's repeated abnormal terminals have parked it
	// (derived from Drain history); unpark writes a park-clear event (ADR-0055).
	parked bool
	// orphaned is true when the set's Worktree binding points at a checkout that
	// no longer exists on disk. Like Picked-up, it is a derived per-build fact
	// (a cheap filesystem stat, never a git fork), not a persisted status, and is
	// orthogonal to Task-set status — a set of any status may be orphaned. A set
	// with no binding can never be orphaned.
	orphaned bool
	// bound is true when the set holds a Worktree binding with a non-blank runtime
	// path — the dedicated-checkout fact the action menu gates unbind on. Derived
	// per-build from the binding snapshot (no git fork), mirroring dashboardSetBound.
	bound bool
}

// DashboardSnapshot is the data model for `pop queue dashboard`.
type DashboardSnapshot struct {
	Rows []DashboardRow
}

// dashboardRepoStatic holds one repo group's static resolution: the repository
// coordinates and integration target, all derived fork-free from the repo.json
// marker's common directory and config (ADR-0060). The dashboard recomputes only
// the volatile overlay (task statuses, locks, daemon state) per poll.
type dashboardRepoStatic struct {
	defPath       string
	statePath     string
	repoKey       string
	repoCommonDir string
	projectName   string
	rep           *projectScan
	repBranch     string
	bare          bool
	// configErr is non-empty when the repository cannot resolve an integration
	// target from config — a bare repo with no declared trunk (ADR-0060/0059). Its
	// sets render this as a config-class error rather than forking git.
	configErr string
}

// dashboardSnapshot is a single point-in-time read of pop.db taken once per
// dashboard build. Each rendered row used to reopen pop.db several times
// (bindings thrice, the drain lock once) every poll; the per-row overlay now
// consults these in-memory maps instead, so the whole view is one consistent
// snapshot and the store is opened a bounded number of times per build, not per
// row. It holds every binding keyed by scoped key, plus the live (PID-alive)
// running drains keyed by runtime path.
type dashboardSnapshot struct {
	bindings   map[string]WorktreeBinding
	liveDrains map[string]tasks.RunningDrain
}

// newDashboardSnapshot reads the volatile per-build store state once: AllBindings
// and the live running drains (RunningDrains filtered to PID-alive in memory). It
// is the single point at which a build touches pop.db for the overlay.
func newDashboardSnapshot(d *Deps) (*dashboardSnapshot, error) {
	snap := &dashboardSnapshot{
		bindings:   map[string]WorktreeBinding{},
		liveDrains: map[string]tasks.RunningDrain{},
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
	return snap, nil
}

// bindingFor returns the snapshot binding for (repoKey, setID), the
// snapshot-backed equivalent of bindingForSet.
func (s *dashboardSnapshot) bindingFor(repoKey, setID string) (WorktreeBinding, bool) {
	b, ok := s.bindings[setScopedKey(repoKey, setID)]
	return b, ok
}

// BuildDashboard derives the Queue dashboard rows from registered projects and
// on-disk task/queue state. It is read-only except for the same refresh
// auto-registration behavior used by `pop queue status`.
//
// The static side — repository identity, integration target, and branch — is
// derived fork-free from each repo's repo.json marker and config (ADR-0060), so
// a build forks no git for those coordinates; only mergeability (SHA-gated, in
// reconcile) remains a git cost, and only for repos with task storage.
func BuildDashboard(d *Deps, cfg *config.Config) (DashboardSnapshot, error) {
	if d == nil {
		d = DefaultDeps()
	}
	if d.Tasks == nil {
		d.Tasks = tasks.DefaultDeps()
	}
	if d.Project == nil {
		d.Project = project.DefaultDeps()
	}
	// Reconcile-then-read: heal dead-PID running Drains into crashed before the
	// volatile overlay below reads locks from them (ADR-0055). A foreground drain
	// that crashed is healed by whoever next opens the dashboard.
	d.reconcile()
	state, err := EnsureDaemonState(d.Tasks)
	if err != nil {
		return DashboardSnapshot{}, err
	}
	projects, err := tasks.ListPickerProjectsWith(d.Project, cfg)
	if err != nil {
		return DashboardSnapshot{}, err
	}

	// Resolve each renderable repo group's static coordinates fork-free from its
	// marker plus config. The dashboard can only ever render task sets, which live
	// in per-repo Task storage, so only repositories with a storage marker that
	// also intersect config contribute statics (ADR-0042 intersection). Identity,
	// integration target, and branch all derive from the marker's common directory
	// and config — no `rev-parse`, no `worktree list`, no `branch --show-current`.
	statics, err := dashboardRepoStatics(d, cfg, projects)
	if err != nil {
		return DashboardSnapshot{}, err
	}
	if len(statics) == 0 {
		return DashboardSnapshot{}, nil
	}

	// Volatile overlay: task statuses, locks, and daemon-state-derived columns
	// are re-read every poll so the view stays live, but these are cheap file
	// and store reads — the static side above forks no git to begin with.
	var delays []time.Duration
	if qcfg, qerr := resolvedQueueConfig(cfg); qerr == nil {
		delays = qcfg.CrashRetryDelays
	}
	now := d.now().UTC()
	// One pop.db read per build, not per row: every row's binding, mergeability,
	// and live-drain lookup is served from this snapshot.
	snap, err := newDashboardSnapshot(d)
	if err != nil {
		return DashboardSnapshot{}, err
	}
	var rows []DashboardRow
	for _, st := range statics {
		groupRows, err := dashboardRowsFromStatic(d, snap, state, delays, now, st)
		if err != nil {
			return DashboardSnapshot{}, err
		}
		rows = append(rows, groupRows...)
	}
	sortDashboardRows(rows)
	return DashboardSnapshot{Rows: rows}, nil
}

// dashboardRepoStatics resolves every renderable repo group's static coordinates
// fork-free (ADR-0060). It iterates the repositories that have a Task storage
// marker on disk — the only repos that can contribute rows — and, for each,
// pairs it with the config projects whose checkout nests under (or contains) its
// working-tree root. A repository with no matching config project is dropped
// (ADR-0042's config intersection). Identity, paths, integration target, and
// branch are all derived from the marker's common directory plus config: no git.
func dashboardRepoStatics(d *Deps, cfg *config.Config, projects []project.ExpandedProject) ([]dashboardRepoStatic, error) {
	repos, err := tasks.ListTaskStorageRepos(d.Tasks)
	if err != nil {
		return nil, err
	}
	if len(repos) == 0 {
		return nil, nil
	}

	// Canonicalize each candidate project path once (cheap symlink eval, never a
	// git fork) for path-nesting comparison against each repo's root.
	type candidate struct {
		p     project.ExpandedProject
		canon string
	}
	cands := make([]candidate, 0, len(projects))
	for _, p := range projects {
		canon, err := canonicalCheckoutPath(d.Tasks, p.Path)
		if err != nil {
			canon = p.Path
		}
		cands = append(cands, candidate{p: p, canon: canon})
	}

	statics := make([]dashboardRepoStatic, 0, len(repos))
	for _, repo := range repos {
		root := storageRepoRoot(d.Tasks, repo.RepositoryPath)
		var scans []projectScan
		for _, c := range cands {
			if pathWithinOrEqual(c.canon, root) || pathWithinOrEqual(root, c.canon) {
				// SessionName is left unset: deriving it forks `git rev-parse` and the
				// build path never reads it (rendered rows carry no session, and
				// bind/drain sub-actions recompute it from the row's project path).
				scans = append(scans, projectScan{
					Name:        c.p.Name,
					ProjectPath: c.canon,
					RuntimePath: c.canon,
				})
			}
		}
		if len(scans) == 0 {
			// Registered storage but not in config: dropped by the intersection.
			continue
		}
		st, err := dashboardRepoStaticFromMarker(d, cfg, repo.RepositoryPath, scans)
		if err != nil {
			return nil, err
		}
		statics = append(statics, st)
	}
	return statics, nil
}

// dashboardRepoStaticFromMarker derives one repo group's static coordinates from
// its marker's common directory and config, forking no git (ADR-0060): identity
// and paths come from identityFromCommonDir (sha256 + path ops), the integration
// target from dashboardRepresentative (config trunk or, for a non-bare repo, the
// parent of the common directory), and the branch from a HEAD file read. A bare
// repo with no declared trunk carries a config-class error on configErr instead.
func dashboardRepoStaticFromMarker(d *Deps, cfg *config.Config, commonDir string, scans []projectScan) (dashboardRepoStatic, error) {
	id, err := tasks.IdentityFromCommonDir(d.Tasks, commonDir)
	if err != nil {
		return dashboardRepoStatic{}, err
	}
	defPath, err := tasks.CanonicalDefinitionPathWith(d.Tasks, id.TasksDir)
	if err != nil {
		return dashboardRepoStatic{}, err
	}

	rep, bare, err := dashboardRepresentative(d, cfg, id.CommonDir, scans)
	if err != nil {
		return dashboardRepoStatic{}, err
	}
	repBranch := ""
	configErr := ""
	switch {
	case rep != nil:
		repBranch = headBranchFromCheckout(d.Tasks, rep.ProjectPath, id.CommonDir)
	case bare:
		// Bare repo with no declared trunk: there is no integration target to fork
		// for. Surface a config-class error on its sets (ADR-0060/0059).
		configErr = repoScanReason
	}

	return dashboardRepoStatic{
		defPath:       defPath,
		statePath:     tasks.StatePathFor(defPath),
		repoKey:       repoIdentityKey(id),
		repoCommonDir: id.CommonDir,
		projectName:   repoName(scans, rep),
		rep:           rep,
		repBranch:     repBranch,
		bare:          bare,
		configErr:     configErr,
	}, nil
}

// dashboardRepresentative resolves a repo group's integration target without
// forking git (ADR-0060): a per-checkout `trunk = true` override wins (bare or
// not), else a non-bare repo's target is the main worktree — the parent of the
// common directory — and a bare repo with no declared trunk has none (bare=true,
// rep=nil). A renamed execution key surfaces as a fatal config finding, matching
// resolveRepresentative's contract.
func dashboardRepresentative(d *Deps, cfg *config.Config, commonDir string, scans []projectScan) (*projectScan, bool, error) {
	if cfg != nil && len(scans) > 0 {
		if _, err := resolveRepoConfigFor(d, cfg, scans[0].ProjectPath); err != nil {
			var f config.Finding
			if errors.As(err, &f) {
				return nil, false, err
			}
		}
	}

	// 1. explicit trunk = true checkout (config-only, no git).
	for i := range scans {
		rc, err := resolveRepoConfigFor(d, cfg, scans[i].ProjectPath)
		if err == nil && rc.Trunk {
			return &scans[i], false, nil
		}
	}

	// 2. non-bare repo → main worktree = parent of the common directory. A normal
	// repo's common dir is `<root>/.git`; only that layout has a derivable main
	// worktree fork-free. Anything else (`.bare`, top-level bare) is bare.
	if filepath.Base(commonDir) == ".git" {
		return dashboardScanForCheckout(d, scans, filepath.Dir(commonDir)), false, nil
	}

	// 3. bare repo with no declared trunk → no integration target.
	return nil, true, nil
}

// dashboardScanForCheckout returns the scan whose checkout canonicalizes to
// checkoutPath, or synthesizes one (fork-free) when the target — e.g. a main
// worktree that is not itself a picker Project — is not among the group's scans.
func dashboardScanForCheckout(d *Deps, scans []projectScan, checkoutPath string) *projectScan {
	canon, err := canonicalCheckoutPath(d.Tasks, checkoutPath)
	if err != nil {
		canon = checkoutPath
	}
	for i := range scans {
		if c, err := canonicalCheckoutPath(d.Tasks, scans[i].ProjectPath); err == nil && c == canon {
			return &scans[i]
		}
	}
	name := ""
	if len(scans) > 0 {
		name = scans[0].Name
	}
	// SessionName is left unset for the same reason as in dashboardRepoStatics:
	// deriving it forks git and the build path never reads it.
	return &projectScan{
		Name:        name,
		ProjectPath: canon,
		RuntimePath: canon,
	}
}

// headBranchFromCheckout reads a checkout's current branch from its HEAD file —
// no `git branch --show-current` (ADR-0060). It resolves the checkout's git
// directory (a `.git` directory for a main worktree, or the `gitdir:` pointer in
// a linked worktree's `.git` file), falling back to commonDir, then parses
// `ref: refs/heads/<branch>`. A detached HEAD or any read failure yields "".
func headBranchFromCheckout(d *tasks.Deps, checkout, commonDir string) string {
	gitDir := ""
	if strings.TrimSpace(checkout) != "" {
		dotGit := filepath.Join(checkout, ".git")
		if info, err := d.FS.Stat(dotGit); err == nil {
			if info.IsDir() {
				gitDir = dotGit
			} else if data, rerr := d.FS.ReadFile(dotGit); rerr == nil {
				line := strings.TrimSpace(string(data))
				if p := strings.TrimPrefix(line, "gitdir:"); p != line {
					p = strings.TrimSpace(p)
					if !filepath.IsAbs(p) {
						p = filepath.Join(checkout, p)
					}
					gitDir = filepath.Clean(p)
				}
			}
		}
	}
	if gitDir == "" {
		gitDir = commonDir
	}
	if strings.TrimSpace(gitDir) == "" {
		return ""
	}
	data, err := d.FS.ReadFile(filepath.Join(gitDir, "HEAD"))
	if err != nil {
		return ""
	}
	head := strings.TrimSpace(string(data))
	if ref := strings.TrimPrefix(head, "ref: refs/heads/"); ref != head {
		return strings.TrimSpace(ref)
	}
	return ""
}

// storageRepoRoot derives a repository's working-tree root from the canonical
// git common directory recorded in its marker: a normal repo's common dir is
// `<root>/.git` and a bare-with-worktrees layout's is `<root>/.bare`, so the
// root is the parent; a top-level bare repo's common dir is the repo dir itself.
func storageRepoRoot(d *tasks.Deps, commonDir string) string {
	root := commonDir
	switch filepath.Base(commonDir) {
	case ".git", ".bare":
		root = filepath.Dir(commonDir)
	}
	if canon, err := canonicalCheckoutPath(d, root); err == nil {
		return canon
	}
	return root
}

// pathWithinOrEqual reports whether p is base or a descendant of base.
func pathWithinOrEqual(p, base string) bool {
	return p == base || strings.HasPrefix(p, base+string(filepath.Separator))
}

// Queue dashboard membership tiers, in precedence order. A row lands in the
// first tier it qualifies for, so an orphaned + auto-drain set sorts under the
// auto-drain tier (auto-drain is checked before orphaned).
const (
	dashboardTierRunning  = iota // live drain holds the checkout (Picked-up)
	dashboardTierAutoDrain        // auto-drain enabled
	dashboardTierOrphaned         // Worktree binding points at a missing checkout
	dashboardTierRest             // everything else
)

// dashboardSortTier returns a row's membership tier (see the dashboardTier*
// constants). The order of these checks encodes the precedence: a row that is
// both orphaned and auto-drain qualifies for the auto-drain tier first.
func dashboardSortTier(r DashboardRow) int {
	switch {
	case r.Drain == dashboardDrainPickedUp:
		return dashboardTierRunning
	case r.AutoDrain:
		return dashboardTierAutoDrain
	case r.orphaned:
		return dashboardTierOrphaned
	default:
		return dashboardTierRest
	}
}

// dashboardStatusRank orders statuses within a single project for the "the
// rest" tier (Reading A): normal rows first, then UNVERIFIED, then DONE sink to
// that project's bottom. The sink is per-project, not global — a project's own
// terminal statuses cluster at that project's bottom.
func dashboardStatusRank(s tasks.TaskSetStatus) int {
	switch s {
	case tasks.StatusUnverified:
		return 1
	case tasks.StatusDone:
		return 2
	default:
		return 0
	}
}

// sortDashboardRows applies the agreed total order: rows fall into membership
// tiers (running, auto-drain, orphaned, the rest); within a tier they group by
// project name ascending; within a project in "the rest" UNVERIFIED then DONE
// sink to the bottom; and the global tiebreak is SetID descending.
func sortDashboardRows(rows []DashboardRow) {
	sort.SliceStable(rows, func(i, j int) bool {
		a, b := rows[i], rows[j]
		if ta, tb := dashboardSortTier(a), dashboardSortTier(b); ta != tb {
			return ta < tb
		}
		if a.Project != b.Project {
			return a.Project < b.Project
		}
		// Status sink applies only within "the rest"; tiers 1–3 go straight to
		// the SetID tiebreak after project name.
		if dashboardSortTier(a) == dashboardTierRest {
			if ra, rb := dashboardStatusRank(a.RawStatus), dashboardStatusRank(b.RawStatus); ra != rb {
				return ra < rb
			}
		}
		return a.SetID > b.SetID
	})
}

// dashboardRowsForStatic renders one repo group's rows from a fully resolved
// static plus the current volatile overlay (statuses, locks, daemon state),
// taking the single per-build store snapshot. It is the seam tests use to drive
// dashboardRowsFromStatic with a hand-built static, mirroring what BuildDashboard
// does per group after deriving statics fork-free from markers.
func dashboardRowsForStatic(d *Deps, cfg *config.Config, state *DaemonState, st dashboardRepoStatic) ([]DashboardRow, error) {
	var delays []time.Duration
	if qcfg, qerr := resolvedQueueConfig(cfg); qerr == nil {
		delays = qcfg.CrashRetryDelays
	}
	snap, err := newDashboardSnapshot(d)
	if err != nil {
		return nil, err
	}
	return dashboardRowsFromStatic(d, snap, state, delays, d.now().UTC(), st)
}

// dashboardRowsFromStatic builds a repo group's rows from its static resolution
// plus the current volatile state: task statuses (refresh), runtime locks, and
// daemon-state columns. It forks no git — the static side is marker/config
// derived (ADR-0060) and this overlay is cheap file/store reads.
func dashboardRowsFromStatic(d *Deps, snap *dashboardSnapshot, state *DaemonState, delays []time.Duration, now time.Time, st dashboardRepoStatic) ([]DashboardRow, error) {
	refresh, err := d.refresh(st.defPath)
	if err != nil {
		return nil, err
	}
	intents := dashboardWorktreeIntents(d, st.defPath)
	backoff := d.setBackoffLookup(st.repoCommonDir, delays, now)
	var rows []DashboardRow
	for _, taskRow := range refresh.Rows {
		// A Done set keeps its dashboard row only while it still holds a managed
		// (pop-provisioned) Worktree binding, as a clean-up reminder (ADR-0070).
		bnd, hasBinding := snap.bindingFor(st.repoKey, taskRow.ID)
		bound := hasBinding && strings.TrimSpace(bnd.RuntimePath) != ""
		doneStillManagedBound := taskRow.Status == tasks.StatusDone && bound && bnd.Provisioned
		orphaned := dashboardOrphaned(d, bnd, hasBinding)
		if !dashboardShowRow(taskRow, doneStillManagedBound) {
			continue
		}
		wt := dashboardWorktree(d, snap, intents, st.repoKey, taskRow.ID, taskRow.Status, bnd, bound)
		parked := false
		if backoff != nil {
			parked, _ = backoff(taskRow.ID)
		}
		drain := dashboardDrain(snap, state, st.repoKey, taskRow.ID, wt.runtimePath)
		if parked {
			drain = "parked"
		} else if st.configErr != "" && !hasBinding && drain == "" {
			// Bare repo with no declared trunk: an unbound set has no integration
			// target to route to (ADR-0060). Surface the config-class error derived
			// fork-free during static resolution, never a git probe. A bound set is
			// still drainable via its binding, so it is left untouched.
			drain = "config error: " + st.configErr
		} else if drain == "" && taskRow.Status == tasks.StatusReady {
			// An unsatisfiable worktree directive is a static config defect, not a
			// runtime crash (ADR-0059): show it on the set so the operator fixes the
			// environment. Read the registration intent first (a store read, no git);
			// only a set that actually carries a directive pays the read-only probe
			// (which forks git to resolve the trunk/worktree), so the no-directive
			// common path — and the dashboard's cached rebuild — forks no git.
			if intent, _ := tasks.RegisteredWorktreeIntent(d.Tasks, st.defPath, taskRow.ID); intent != nil {
				if msg := d.probeDirective(staticProjectPath(st), taskRow.ID); msg != "" {
					drain = "config error: " + msg
				}
			}
		}
		status := dashboardStatus(taskRow)
		rows = append(rows, DashboardRow{
			Project:        st.projectName,
			SetID:          taskRow.ID,
			Status:         status,
			RawStatus:      taskRow.Status,
			Worktree:       wt.label,
			Drain:          drain,
			AutoDrain:      taskRow.AutoDrain,
			defPath:        st.defPath,
			statePath:      st.statePath,
			cursorKey:      st.projectName + "\x00" + taskRow.ID,
			repoKey:        st.repoKey,
			repoCommonDir:  st.repoCommonDir,
			projectPath:    staticProjectPath(st),
			runtimePath:    wt.runtimePath,
			paneID:         dashboardPaneID(state, st.repoKey, taskRow.ID),
			doneStillManagedBound: doneStillManagedBound,
			parked:                parked,
			orphaned:              orphaned,
			bound:                 bound,
			destKind:              wt.destKind,
		})
	}
	return rows, nil
}

func dashboardShowRow(row tasks.Row, doneStillManagedBound bool) bool {
	return row.Status != tasks.StatusDone || doneStillManagedBound
}

// staticProjectPath returns the repo group's representative checkout, the path
// every bind/drain sub-action runs git against. It is empty only for a bare
// repo with no resolvable representative, in which case bind falls back to a
// full project scan.
func staticProjectPath(st dashboardRepoStatic) string {
	if st.rep == nil {
		return ""
	}
	return st.rep.ProjectPath
}

func dashboardStatus(row tasks.Row) string {
	return tasks.StatusLabel(row)
}

type dashboardDestKind int

const (
	dashboardDestBound dashboardDestKind = iota
	dashboardDestManagedDirective
	dashboardDestNeedsBind
	dashboardDestDoneManagedBound
)

// dashboardManagedWtStyle colors the [managed wt] destination badge.
var dashboardManagedWtStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("39"))

type dashboardWorktreeView struct {
	label       string
	runtimePath string
	destKind    dashboardDestKind
}

const (
	dashboardDestLabelManagedWt = "[managed wt]"
	dashboardDestLabelNeedsBind = "needs bind"
)

// dashboardWorktreeIntents loads seeded worktree directives for one definition
// path in a single store read, keyed by set ID. The per-row destination column
// consults this map instead of reopening the store for each unbound row.
func dashboardWorktreeIntents(d *Deps, defPath string) map[string]*tasks.WorktreeDirective {
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

// dashboardWorktree resolves the destination column per ADR-0070/0072: a bound
// set shows its branch plainly; an unbound set with a managed directive shows a
// [managed wt] badge (the Queue will provision on drain); an unbound set with no
// directive shows dim needs bind (the Queue will not drain it). A Done set that
// still holds a managed binding shows [managed wt <branch>] as a clean-up reminder.
func dashboardWorktree(d *Deps, snap *dashboardSnapshot, intents map[string]*tasks.WorktreeDirective, repoKey, setID string, status tasks.TaskSetStatus, bnd WorktreeBinding, bound bool) dashboardWorktreeView {
	if bound {
		branch := bnd.Branch
		if branch == "" {
			branch = headBranchFromCheckout(d.Tasks, bnd.RuntimePath, "")
		}
		branch = formatDashboardBranch(branch)
		kind := dashboardDestBound
		if status == tasks.StatusDone && bnd.Provisioned {
			kind = dashboardDestDoneManagedBound
		}
		return dashboardWorktreeView{label: branch, runtimePath: bnd.RuntimePath, destKind: kind}
	}
	intent := intents[setID]
	if intent != nil && intent.Managed {
		return dashboardWorktreeView{label: dashboardDestLabelManagedWt, destKind: dashboardDestManagedDirective}
	}
	return dashboardWorktreeView{label: dashboardDestLabelNeedsBind, destKind: dashboardDestNeedsBind}
}

// formatDashboardBranch normalizes a branch name for the destination column.
func formatDashboardBranch(branch string) string {
	branch = strings.TrimSpace(branch)
	if branch == "" {
		return "detached"
	}
	return branch
}

// renderDashboardDest applies destination-column styling to the plain label.
func renderDashboardDest(kind dashboardDestKind, label string) string {
	switch kind {
	case dashboardDestManagedDirective:
		return dashboardManagedWtStyle.Render(dashboardDestLabelManagedWt)
	case dashboardDestNeedsBind:
		return ui.HintStyle.Render(dashboardDestLabelNeedsBind)
	case dashboardDestDoneManagedBound:
		return dashboardManagedWtStyle.Render("[managed wt " + label + "]")
	default:
		return label
	}
}

// dashboardDrain reports "picked up" when a live drain holds the set's checkout,
// reading the per-build snapshot's live-drain map (one RunningDrains read plus
// in-memory PID-liveness) instead of reopening the runtime lock per row. The
// snapshot keys live drains by runtime path; a drain at the set's checkout (or
// its bound checkout) whose SetID matches is the same fact ReadRuntimeLockStatus
// derived per row before.
func dashboardDrain(snap *dashboardSnapshot, state *DaemonState, repoKey, setID, runtimePath string) string {
	paths := map[string]bool{}
	if runtimePath != "" {
		paths[runtimePath] = true
	}
	if b, ok := snap.bindingFor(repoKey, setID); ok && b.RuntimePath != "" {
		paths[b.RuntimePath] = true
	}
	_ = state
	for path := range paths {
		if dr, ok := snap.liveDrains[path]; ok && dr.SetID == setID {
			return dashboardDrainPickedUp
		}
	}
	return ""
}

// dashboardOrphaned reports whether a set's Worktree binding points at a
// checkout that no longer exists on disk. Detection is a single cheap
// filesystem stat of the binding's runtime path — never a git subprocess — so
// the fork-free dashboard build stays fork-free. A set with no binding (or one
// with a blank runtime path) can never be orphaned; a binding whose runtime
// path still stats present is not orphaned.
func dashboardOrphaned(d *Deps, bnd WorktreeBinding, hasBinding bool) bool {
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

func dashboardPaneID(state *DaemonState, repoKey, setID string) string {
	if state == nil || state.DrainPanes == nil {
		return ""
	}
	if pane, ok := state.DrainPanes[setScopedKey(repoKey, setID)]; ok {
		return pane.PaneID
	}
	return ""
}

type dashboardTickMsg struct{}
type dashboardRowsMsg struct {
	snap DashboardSnapshot
	err  error
}
type dashboardToggleMsg struct {
	key       string
	autoDrain bool
	err       error
}
type dashboardDrainMsg struct {
	err error
}
type dashboardUnparkMsg struct {
	setID string
	err   error
}
type dashboardArchiveMsg struct {
	setID string
	err   error
}
type dashboardPreviewMsg struct {
	err error
}
type dashboardDetailMsg struct {
	dashRow  DashboardRow
	manifest *tasks.Manifest
	taskRow  *tasks.Row
	err      error
}

type dashboardTaskTextMsg struct {
	taskID string
	path   string
	text   string
	err    error
}
type dashboardBindListMsg struct {
	row     DashboardRow
	entries []dashboardBindEntry
	err     error
}
type dashboardBindRefsMsg struct {
	refs []string
	err  error
}
type dashboardBindMsg struct {
	err error
}
type dashboardDrainListMsg struct {
	row     DashboardRow
	entries []dashboardDrainEntry
	err     error
}
type dashboardAbandonMsg struct {
	err error
}
type dashboardDetailOverrideMsg struct {
	taskID string
	verb   string // "complete", "open", or "skip" (for confirmation text)
	err    error
}

type dashboardBindStage int

const (
	dashboardBindStageWorktree dashboardBindStage = iota
	dashboardBindStageBaseRef
	dashboardBindStageName
)

type dashboardBindEntry struct {
	Label  string
	Path   string
	Branch string
	Create bool
}

type dashboardBindModal struct {
	row     DashboardRow
	stage   dashboardBindStage
	entries []dashboardBindEntry
	refs    []string
	cursor  int
	baseRef string
	name    string
	loading bool
}

// dashboardDrainTargetKind identifies one Drain target picker option (ADR-0052).
type dashboardDrainTargetKind int

const (
	// drainTargetWorktree adopts an existing non-managed, unbound worktree.
	drainTargetWorktree dashboardDrainTargetKind = iota
	// drainTargetNewManaged provisions a managed worktree forked from the trunk.
	drainTargetNewManaged
	// drainTargetTrunk drains inline in the trunk worktree with no binding.
	drainTargetTrunk
)

type dashboardDrainEntry struct {
	Label  string
	Kind   dashboardDrainTargetKind
	Path   string // adopt target checkout (drainTargetWorktree only)
	Branch string
}

// dashboardDrainModal is the Drain target picker shown when `i` is pressed on an
// unbound set: pick an existing worktree to adopt, a new managed worktree to
// provision off the trunk (the default cursor), or the trunk itself — then bind
// (or stay unbound for trunk) and drain in one action. A bound set skips the
// picker and resumes in its binding (ADR-0052).
type dashboardDrainModal struct {
	row     DashboardRow
	entries []dashboardDrainEntry
	cursor  int
	loading bool
}

type dashboardAbandonModal struct {
	row     DashboardRow
	loading bool
}

// dashboardMenuAction identifies the verb a menu item dispatches.
type dashboardMenuAction int

const (
	menuActionDrain dashboardMenuAction = iota
	menuActionBind
	menuActionUnbind
	menuActionAutoDrain
	menuActionPreview
	menuActionUnpark
	menuActionShell
	menuActionArchive
)

// dashboardMenuItem is one verb in the action menu overlay: the flat shortcut
// letter it keeps, the label shown beside it, and the verb it dispatches.
type dashboardMenuItem struct {
	key    string
	label  string
	action dashboardMenuAction
}

// dashboardMenu is the layered action overlay opened with `a` over the focused
// row. It carries the snapshot of the row it was opened on and the verbs
// applicable to that row, with a highlight cursor for j/k + Enter selection.
type dashboardMenu struct {
	row    DashboardRow
	items  []dashboardMenuItem
	cursor int
}

// dashboardMenuItems returns the verbs applicable to row, in a stable order.
// Conditional verbs are filtered to the row's context: unbind only for bound
// rows, auto-drain only for non-orphaned rows, and unpark only for parked rows.
// Drain, bind, preview, the runtime shell, and archive apply to every row
// regardless of status.
func dashboardMenuItems(row DashboardRow) []dashboardMenuItem {
	items := []dashboardMenuItem{
		{key: "i", label: "drain", action: menuActionDrain},
	}
	items = append(items, dashboardMenuItem{key: "b", label: "bind worktree", action: menuActionBind})
	if row.bound {
		items = append(items, dashboardMenuItem{key: "U", label: "unbind worktree", action: menuActionUnbind})
	}
	if !row.orphaned {
		items = append(items, dashboardMenuItem{key: "d", label: "auto-drain", action: menuActionAutoDrain})
	}
	items = append(items, dashboardMenuItem{key: "p", label: "preview", action: menuActionPreview})
	if row.parked {
		items = append(items, dashboardMenuItem{key: "P", label: "unpark", action: menuActionUnpark})
	}
	items = append(items, dashboardMenuItem{key: "O", label: "shell", action: menuActionShell})
	items = append(items, dashboardMenuItem{key: "A", label: "archive", action: menuActionArchive})
	return items
}

// taskMenuItem is one verb in the task-level action menu: the flat shortcut
// letter it keeps (also the verb code passed to applyDetailOverride) and the
// label shown beside it.
type taskMenuItem struct {
	key   string
	label string
}

// taskMenu is the action overlay opened with `a` over a single task — in the
// task-set detail view (over the cursored task) or the task text peek (over the
// previewed task). It carries the task snapshot and the verbs applicable to that
// task's status, with a highlight cursor for j/k + Enter selection. inPeek marks
// which view it was opened from so the renderer can place it correctly.
type taskMenu struct {
	task   tasks.Task
	items  []taskMenuItem
	cursor int
	inPeek bool
}

// taskMenuItems returns the task verbs applicable to task, filtered to its
// status: Complete for open/failed/skipped (anything not already done), Open for
// any non-open task (done/failed/skipped, mirroring CanReopen), and Skip for
// open. A done task yields Open.
func taskMenuItems(task tasks.Task) []taskMenuItem {
	var items []taskMenuItem
	if task.Status != "done" {
		items = append(items, taskMenuItem{key: "C", label: "complete"})
	}
	if tasks.CanReopen(task.Status) {
		items = append(items, taskMenuItem{key: "O", label: "open"})
	}
	if task.Status == "open" {
		items = append(items, taskMenuItem{key: "K", label: "skip"})
	}
	return items
}

// detailView is the full-screen task-set detail that replaces the table. The
// cursor is pinned by task ID so it survives manifest refreshes.
type detailView struct {
	row      DashboardRow
	manifest *tasks.Manifest
	taskRow  *tasks.Row
	cursorID string
	loading  bool
	err      error
	peek     *taskTextPeek
	// statusMsg is a transient one-line message shown above the hint bar.
	// Set to a hint on invalid transition; set to confirmation on success.
	statusMsg string
}

type taskTextPeek struct {
	taskID  string
	path    string
	text    string
	loading bool
	err     error
	scroll  int
}

// taskByID returns the manifest task with the given ID, or false if absent.
func (d *detailView) taskByID(id string) (tasks.Task, bool) {
	if d.manifest == nil {
		return tasks.Task{}, false
	}
	for _, t := range d.manifest.Tasks {
		if t.ID == id {
			return t, true
		}
	}
	return tasks.Task{}, false
}

// cursorIndex returns the index of the cursor task in the manifest, or 0.
func (d *detailView) cursorIndex() int {
	if d.manifest == nil {
		return 0
	}
	for i, t := range d.manifest.Tasks {
		if t.ID == d.cursorID {
			return i
		}
	}
	return 0
}

// moveCursor moves the cursor by delta, clamped to valid task indices.
func (d *detailView) moveCursor(delta int) {
	if d.manifest == nil || len(d.manifest.Tasks) == 0 {
		return
	}
	idx := d.cursorIndex() + delta
	if idx < 0 {
		idx = 0
	}
	if idx >= len(d.manifest.Tasks) {
		idx = len(d.manifest.Tasks) - 1
	}
	d.cursorID = d.manifest.Tasks[idx].ID
}

// syncManifest updates the manifest on a tick refresh, keeping cursor on the
// same task ID when possible.
func (d *detailView) syncManifest(m *tasks.Manifest, row *tasks.Row) {
	d.manifest = m
	d.taskRow = row
	if m == nil || len(m.Tasks) == 0 {
		d.cursorID = ""
		return
	}
	for _, t := range m.Tasks {
		if t.ID == d.cursorID {
			return
		}
	}
	d.cursorID = m.Tasks[0].ID
}

type QueueDashboard struct {
	d         *Deps
	cfg       *config.Config
	snap      DashboardSnapshot
	allRows   []DashboardRow // source of truth; snap.Rows is the filtered view
	cursor    int
	err       error
	width     int
	height    int
	bind      *dashboardBindModal
	drainPick *dashboardDrainModal
	abandon   *dashboardAbandonModal
	detail    *detailView
	menu      *dashboardMenu
	taskMenu  *taskMenu

	filterMode  bool
	filterInput textinput.Model
	pendingG    bool
	statusMsg   string
}

func newQueueDashboard(d *Deps, cfg *config.Config, snap DashboardSnapshot) QueueDashboard {
	if d == nil {
		d = DefaultDeps()
	}
	return QueueDashboard{d: d, cfg: cfg, snap: snap, allRows: snap.Rows}
}

func (m QueueDashboard) Init() tea.Cmd {
	return dashboardTick()
}

func (m QueueDashboard) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		if m.bind != nil {
			m.pendingG = false
			return m.updateBindModal(msg)
		}
		if m.drainPick != nil {
			m.pendingG = false
			return m.updateDrainModal(msg)
		}
		if m.abandon != nil {
			m.pendingG = false
			return m.updateAbandonModal(msg)
		}
		if m.detail != nil {
			return m.updateDetailView(msg)
		}
		if m.menu != nil {
			m.pendingG = false
			return m.updateMenu(msg)
		}
		if m.filterMode {
			m.pendingG = false
			return m.updateFilterMode(msg)
		}
		if msg.String() == "g" {
			if m.pendingG {
				m.pendingG = false
				if len(m.snap.Rows) > 0 {
					m.cursor = 0
				}
			} else {
				m.pendingG = true
			}
			return m, nil
		}
		m.pendingG = false
		switch msg.String() {
		case "ctrl+c", "esc", "h", "left":
			return m, tea.Quit
		case "/":
			m.filterMode = true
			ti := textinput.New()
			ti.Prompt = "> "
			ti.Focus()
			m.filterInput = ti
			return m, nil
		case "j", "down":
			if len(m.snap.Rows) > 0 && m.cursor < len(m.snap.Rows)-1 {
				m.cursor++
			}
		case "k", "up":
			if len(m.snap.Rows) > 0 && m.cursor > 0 {
				m.cursor--
			}
		case "G":
			if len(m.snap.Rows) > 0 {
				m.cursor = len(m.snap.Rows) - 1
			}
		case "a":
			if len(m.snap.Rows) == 0 || m.cursor < 0 || m.cursor >= len(m.snap.Rows) {
				return m, nil
			}
			row := m.snap.Rows[m.cursor]
			m.menu = &dashboardMenu{row: row, items: dashboardMenuItems(row)}
			m.err = nil
			m.statusMsg = ""
			return m, nil
		case "l", "enter":
			if len(m.snap.Rows) == 0 || m.cursor < 0 || m.cursor >= len(m.snap.Rows) {
				return m, nil
			}
			m.err = nil
			row := m.snap.Rows[m.cursor]
			m.detail = &detailView{row: row, loading: true}
			return m, m.loadDetail(row)
		}
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
	case dashboardTickMsg:
		cmds := []tea.Cmd{dashboardTick(), m.reload()}
		if m.detail != nil {
			cmds = append(cmds, m.loadDetail(m.detail.row))
		}
		return m, tea.Batch(cmds...)
	case dashboardRowsMsg:
		oldKey := ""
		if m.cursor >= 0 && m.cursor < len(m.snap.Rows) {
			oldKey = m.snap.Rows[m.cursor].cursorKey
		}
		m.err = msg.err
		if msg.err == nil {
			m.allRows = msg.snap.Rows
			m.snap = msg.snap
			if m.filterMode {
				m.snap.Rows = filterDashboardRows(m.allRows, m.filterInput.Value())
			}
			m.cursor = dashboardCursorAfterReload(m.snap.Rows, oldKey, m.cursor)
			if m.detail != nil {
				for _, row := range m.snap.Rows {
					if row.cursorKey == m.detail.row.cursorKey {
						m.detail.row = row
						break
					}
				}
			}
		}
	case dashboardDetailMsg:
		if m.detail == nil {
			return m, nil
		}
		if msg.err != nil {
			m.detail.loading = false
			m.detail.err = msg.err
			return m, nil
		}
		m.detail.syncManifest(msg.manifest, msg.taskRow)
		m.detail.loading = false
		m.detail.err = nil
		return m, nil
	case dashboardToggleMsg:
		if msg.err != nil {
			m.err = msg.err
			return m, m.reload()
		}
		for i := range m.snap.Rows {
			if m.snap.Rows[i].cursorKey == msg.key {
				m.snap.Rows[i].AutoDrain = msg.autoDrain
				break
			}
		}
	case dashboardDrainMsg:
		m.drainPick = nil
		if msg.err != nil {
			m.err = msg.err
		}
		return m, m.reload()
	case dashboardUnparkMsg:
		if msg.err != nil {
			m.err = msg.err
		} else {
			m.statusMsg = fmt.Sprintf("%s unparked", msg.setID)
		}
		return m, m.reload()
	case dashboardArchiveMsg:
		if msg.err != nil {
			m.err = msg.err
		} else {
			m.statusMsg = fmt.Sprintf("%s archived", msg.setID)
		}
		return m, m.reload()
	case dashboardDrainListMsg:
		if msg.err != nil {
			m.err = msg.err
			return m, nil
		}
		if len(msg.entries) == 0 {
			m.err = fmt.Errorf("no drain target available for %s", msg.row.SetID)
			return m, nil
		}
		m.drainPick = &dashboardDrainModal{row: msg.row, entries: msg.entries, cursor: defaultDrainCursor(msg.entries)}
		return m, nil
	case dashboardPreviewMsg:
		if msg.err != nil {
			m.err = msg.err
		}
	case dashboardBindListMsg:
		if msg.err != nil {
			m.err = msg.err
			m.bind = nil
			return m, nil
		}
		m.bind = &dashboardBindModal{row: msg.row, entries: msg.entries}
	case dashboardBindRefsMsg:
		if m.bind == nil {
			return m, nil
		}
		if msg.err != nil {
			m.err = msg.err
			m.bind = nil
			return m, nil
		}
		m.bind.stage = dashboardBindStageBaseRef
		m.bind.refs = msg.refs
		m.bind.cursor = 0
		m.bind.loading = false
	case dashboardBindMsg:
		if msg.err != nil {
			m.err = msg.err
			m.bind = nil
			return m, m.reload()
		}
		m.bind = nil
		return m, m.reload()
	case dashboardAbandonMsg:
		if msg.err != nil {
			m.err = msg.err
			m.abandon = nil
			return m, m.reload()
		}
		m.abandon = nil
		return m, m.reload()
	case dashboardDetailOverrideMsg:
		if m.detail == nil {
			return m, nil
		}
		if msg.err != nil {
			m.detail.statusMsg = fmt.Sprintf("error: %v", msg.err)
		} else {
			m.detail.statusMsg = fmt.Sprintf("%s applied to %s", msg.verb, msg.taskID)
		}
		return m, m.loadDetail(m.detail.row)
	case dashboardTaskTextMsg:
		if m.detail == nil || m.detail.peek == nil {
			return m, nil
		}
		m.detail.peek.loading = false
		m.detail.peek.taskID = msg.taskID
		m.detail.peek.path = msg.path
		m.detail.peek.text = msg.text
		m.detail.peek.err = msg.err
	}
	return m, nil
}

func (m QueueDashboard) updateBindModal(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc", "ctrl+c":
		m.bind = nil
		return m, nil
	case "j", "down":
		m.moveBindCursor(1)
		return m, nil
	case "k", "up":
		m.moveBindCursor(-1)
		return m, nil
	case "backspace":
		if m.bind.stage == dashboardBindStageName && len(m.bind.name) > 0 {
			m.bind.name = m.bind.name[:len(m.bind.name)-1]
		}
		return m, nil
	case "enter":
		return m.confirmBindModal()
	}
	if m.bind.stage == dashboardBindStageName {
		if s := msg.String(); len([]rune(s)) == 1 {
			m.bind.name += s
		}
	}
	return m, nil
}

func (m QueueDashboard) updateAbandonModal(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc", "ctrl+c", "n":
		m.abandon = nil
		return m, nil
	case "enter", "y":
		if m.abandon == nil || m.abandon.loading {
			return m, nil
		}
		m.abandon.loading = true
		return m, m.abandonWorktree(m.abandon.row)
	}
	return m, nil
}

// updateMenu drives the action overlay: esc/ctrl+c close it, j/k move the
// highlight, Enter runs the highlighted verb, and any matching verb letter runs
// that verb directly. Non-matching keys are inert while the menu is open.
func (m QueueDashboard) updateMenu(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.menu == nil {
		return m, nil
	}
	switch msg.String() {
	case "esc", "ctrl+c":
		m.menu = nil
		return m, nil
	case "j", "down":
		m.moveMenuCursor(1)
		return m, nil
	case "k", "up":
		m.moveMenuCursor(-1)
		return m, nil
	case "enter":
		return m.invokeMenuItem(m.menu.cursor)
	}
	for i, item := range m.menu.items {
		if msg.String() == item.key {
			return m.invokeMenuItem(i)
		}
	}
	return m, nil
}

func (m *QueueDashboard) moveMenuCursor(delta int) {
	if m.menu == nil {
		return
	}
	n := len(m.menu.items)
	if n == 0 {
		return
	}
	m.menu.cursor += delta
	if m.menu.cursor < 0 {
		m.menu.cursor = n - 1
	}
	if m.menu.cursor >= n {
		m.menu.cursor = 0
	}
}

// invokeMenuItem closes the menu and dispatches the verb at idx against the row
// the menu was opened on.
func (m QueueDashboard) invokeMenuItem(idx int) (tea.Model, tea.Cmd) {
	if m.menu == nil || idx < 0 || idx >= len(m.menu.items) {
		return m, nil
	}
	item := m.menu.items[idx]
	row := m.menu.row
	m.menu = nil
	return m.dispatchMenuAction(item.action, row)
}

// dispatchMenuAction runs the verb. The conditional guards mirror
// dashboardMenuItems' context filtering — an item present in the menu always
// passes its guard, but the guards keep dispatch self-contained.
func (m QueueDashboard) dispatchMenuAction(action dashboardMenuAction, row DashboardRow) (tea.Model, tea.Cmd) {
	m.err = nil
	switch action {
	case menuActionDrain:
		return m, m.launchDrain(row)
	case menuActionBind:
		m.bind = &dashboardBindModal{row: row, loading: true}
		return m, m.loadBindWorktrees(row)
	case menuActionUnbind:
		if !row.bound {
			return m, nil
		}
		m.abandon = &dashboardAbandonModal{row: row}
		return m, nil
	case menuActionAutoDrain:
		if row.orphaned {
			return m, nil
		}
		if m.cursor >= 0 && m.cursor < len(m.snap.Rows) {
			m.snap.Rows[m.cursor].AutoDrain = !m.snap.Rows[m.cursor].AutoDrain
		}
		return m, m.toggleAutoDrain(row)
	case menuActionPreview:
		return m, m.previewDrain(row)
	case menuActionUnpark:
		if !row.parked {
			m.statusMsg = "task set is not parked"
			return m, nil
		}
		m.statusMsg = ""
		return m, m.unparkSet(row)
	case menuActionShell:
		if strings.TrimSpace(row.runtimePath) == "" {
			m.statusMsg = "no checkout bound to this task set"
			return m, nil
		}
		m.statusMsg = ""
		shell := os.Getenv("SHELL")
		if shell == "" {
			shell = "/bin/sh"
		}
		cmd := exec.Command(shell)
		cmd.Dir = row.runtimePath
		return m, tea.ExecProcess(cmd, nil)
	case menuActionArchive:
		m.statusMsg = ""
		return m, m.archiveSet(row)
	}
	return m, nil
}

func (m QueueDashboard) updateDetailView(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.taskMenu != nil {
		m.pendingG = false
		return m.updateTaskMenu(msg)
	}
	if m.detail != nil && m.detail.peek != nil {
		if msg.String() == "g" {
			if m.pendingG {
				m.pendingG = false
				m.detail.peek.scroll = 0
			} else {
				m.pendingG = true
			}
			return m, nil
		}
		m.pendingG = false
		switch msg.String() {
		case "esc", "h", "left":
			m.detail.peek = nil
		case "ctrl+c":
			return m, tea.Quit
		case "j", "down":
			m.moveTaskTextPeek(1)
		case "k", "up":
			m.moveTaskTextPeek(-1)
		case "ctrl+d":
			m.moveTaskTextPeek(halfPageDelta(m.taskTextPeekPageSize()))
		case "ctrl+u":
			m.moveTaskTextPeek(-halfPageDelta(m.taskTextPeekPageSize()))
		case "G":
			m.detail.peek.scroll = m.maxTaskTextPeekScroll()
		case "a":
			task, ok := m.detail.taskByID(m.detail.peek.taskID)
			if !ok {
				return m, nil
			}
			items := taskMenuItems(task)
			if len(items) == 0 {
				return m, nil
			}
			m.taskMenu = &taskMenu{task: task, items: items, inPeek: true}
		}
		return m, nil
	}
	if msg.String() == "g" {
		if m.pendingG {
			m.pendingG = false
			if m.detail != nil && m.detail.manifest != nil && len(m.detail.manifest.Tasks) > 0 {
				m.detail.cursorID = m.detail.manifest.Tasks[0].ID
			}
		} else {
			m.pendingG = true
		}
		return m, nil
	}
	m.pendingG = false
	switch msg.String() {
	case "esc", "h", "left":
		m.detail = nil
		return m, nil
	case "ctrl+c":
		return m, tea.Quit
	case "j", "down":
		if m.detail != nil {
			m.detail.moveCursor(1)
		}
	case "k", "up":
		if m.detail != nil {
			m.detail.moveCursor(-1)
		}
	case "G":
		if m.detail != nil && m.detail.manifest != nil && len(m.detail.manifest.Tasks) > 0 {
			m.detail.cursorID = m.detail.manifest.Tasks[len(m.detail.manifest.Tasks)-1].ID
		}
	case "l", "enter":
		if m.detail == nil || m.detail.loading || m.detail.manifest == nil {
			return m, nil
		}
		idx := m.detail.cursorIndex()
		if idx < 0 || idx >= len(m.detail.manifest.Tasks) {
			return m, nil
		}
		task := m.detail.manifest.Tasks[idx]
		m.detail.peek = &taskTextPeek{taskID: task.ID, loading: true}
		return m, m.loadTaskText(m.detail.manifest, task)
	case "a":
		if m.detail == nil || m.detail.loading || m.detail.manifest == nil {
			return m, nil
		}
		idx := m.detail.cursorIndex()
		if idx < 0 || idx >= len(m.detail.manifest.Tasks) {
			return m, nil
		}
		task := m.detail.manifest.Tasks[idx]
		items := taskMenuItems(task)
		if len(items) == 0 {
			return m, nil
		}
		m.detail.statusMsg = ""
		m.taskMenu = &taskMenu{task: task, items: items}
		return m, nil
	}
	return m, nil
}

// updateTaskMenu drives the task-level action overlay: esc/ctrl+c close it, j/k
// move the highlight, Enter runs the highlighted verb, and any matching verb
// letter runs that verb directly. Non-matching keys are inert while open.
func (m QueueDashboard) updateTaskMenu(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.taskMenu == nil {
		return m, nil
	}
	switch msg.String() {
	case "esc", "ctrl+c":
		m.taskMenu = nil
		return m, nil
	case "j", "down":
		m.moveTaskMenuCursor(1)
		return m, nil
	case "k", "up":
		m.moveTaskMenuCursor(-1)
		return m, nil
	case "enter":
		return m.invokeTaskMenuItem(m.taskMenu.cursor)
	}
	for i, item := range m.taskMenu.items {
		if msg.String() == item.key {
			return m.invokeTaskMenuItem(i)
		}
	}
	return m, nil
}

func (m *QueueDashboard) moveTaskMenuCursor(delta int) {
	if m.taskMenu == nil {
		return
	}
	n := len(m.taskMenu.items)
	if n == 0 {
		return
	}
	m.taskMenu.cursor += delta
	if m.taskMenu.cursor < 0 {
		m.taskMenu.cursor = n - 1
	}
	if m.taskMenu.cursor >= n {
		m.taskMenu.cursor = 0
	}
}

// invokeTaskMenuItem closes the menu and dispatches the verb at idx against the
// task the menu was opened on. The items are pre-filtered to valid transitions
// (taskMenuItems), so the verb applies without a separate confirmation.
func (m QueueDashboard) invokeTaskMenuItem(idx int) (tea.Model, tea.Cmd) {
	if m.taskMenu == nil || idx < 0 || idx >= len(m.taskMenu.items) {
		return m, nil
	}
	if m.detail == nil {
		m.taskMenu = nil
		return m, nil
	}
	item := m.taskMenu.items[idx]
	task := m.taskMenu.task
	m.taskMenu = nil
	m.detail.statusMsg = ""
	return m, m.applyDetailOverride(m.detail.row, task, item.key)
}

// applyDetailOverride dispatches the C/O/K override verb to the appropriate
// tasks.*With function via the Deps seam.
func (m QueueDashboard) applyDetailOverride(row DashboardRow, task tasks.Task, verb string) tea.Cmd {
	d := m.d
	if d == nil {
		d = DefaultDeps()
	}
	taskPath := row.SetID + "/" + task.File
	return func() tea.Msg {
		var err error
		switch verb {
		case "C":
			err = d.completeDetailTask(row.defPath, taskPath)
		case "O":
			err = d.resetDetailTask(row.defPath, taskPath)
		case "K":
			err = d.skipDetailTask(row.defPath, taskPath)
		}
		verbName := map[string]string{"C": "complete", "O": "open", "K": "skip"}[verb]
		return dashboardDetailOverrideMsg{taskID: task.ID, verb: verbName, err: err}
	}
}

func (m QueueDashboard) updateFilterMode(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "ctrl+c":
		return m, tea.Quit
	case "esc":
		m.filterMode = false
		m.filterInput = textinput.Model{}
		m.snap.Rows = m.allRows
		m.cursor = dashboardCursorAfterReload(m.snap.Rows, "", m.cursor)
		return m, nil
	case "j", "down":
		if len(m.snap.Rows) > 0 && m.cursor < len(m.snap.Rows)-1 {
			m.cursor++
		}
		return m, nil
	case "k", "up":
		if len(m.snap.Rows) > 0 && m.cursor > 0 {
			m.cursor--
		}
		return m, nil
	default:
		oldKey := ""
		if m.cursor >= 0 && m.cursor < len(m.snap.Rows) {
			oldKey = m.snap.Rows[m.cursor].cursorKey
		}
		var cmd tea.Cmd
		m.filterInput, cmd = m.filterInput.Update(msg)
		m.snap.Rows = filterDashboardRows(m.allRows, m.filterInput.Value())
		m.cursor = dashboardCursorAfterReload(m.snap.Rows, oldKey, m.cursor)
		return m, cmd
	}
}

func (m QueueDashboard) moveTaskTextPeek(delta int) {
	if m.detail == nil || m.detail.peek == nil || delta == 0 {
		return
	}
	m.detail.peek.scroll += delta
	if m.detail.peek.scroll < 0 {
		m.detail.peek.scroll = 0
	}
	if maxScroll := m.maxTaskTextPeekScroll(); m.detail.peek.scroll > maxScroll {
		m.detail.peek.scroll = maxScroll
	}
}

func (m QueueDashboard) maxTaskTextPeekScroll() int {
	if m.detail == nil || m.detail.peek == nil {
		return 0
	}
	lines := taskTextPeekLines(m.detail.peek.text)
	maxScroll := len(lines) - m.taskTextPeekPageSize()
	if maxScroll < 0 {
		return 0
	}
	return maxScroll
}

func (m QueueDashboard) taskTextPeekPageSize() int {
	if m.detail == nil || m.detail.peek == nil {
		return 1
	}
	return taskTextPeekPageSize(m.height, m.detail.peek.path)
}

// filterDashboardRows returns rows whose Project or SetID contain query
// (case-insensitive). Returns allRows unchanged when query is empty.
func filterDashboardRows(rows []DashboardRow, query string) []DashboardRow {
	if query == "" {
		return rows
	}
	q := strings.ToLower(query)
	var filtered []DashboardRow
	for _, row := range rows {
		if strings.Contains(strings.ToLower(row.Project), q) ||
			strings.Contains(strings.ToLower(row.SetID), q) {
			filtered = append(filtered, row)
		}
	}
	return filtered
}

func (m *QueueDashboard) moveBindCursor(delta int) {
	if m.bind == nil {
		return
	}
	limit := len(m.bind.entries)
	if m.bind.stage == dashboardBindStageBaseRef {
		limit = len(m.bind.refs)
	}
	if limit == 0 {
		return
	}
	m.bind.cursor += delta
	if m.bind.cursor < 0 {
		m.bind.cursor = limit - 1
	}
	if m.bind.cursor >= limit {
		m.bind.cursor = 0
	}
}

func (m QueueDashboard) confirmBindModal() (tea.Model, tea.Cmd) {
	if m.bind == nil || m.bind.loading {
		return m, nil
	}
	switch m.bind.stage {
	case dashboardBindStageWorktree:
		if len(m.bind.entries) == 0 || m.bind.cursor < 0 || m.bind.cursor >= len(m.bind.entries) {
			return m, nil
		}
		entry := m.bind.entries[m.bind.cursor]
		if entry.Create {
			m.bind.loading = true
			return m, m.loadBindRefs(m.bind.row)
		}
		m.bind.loading = true
		return m, m.adoptBindWorktree(m.bind.row, entry.Path)
	case dashboardBindStageBaseRef:
		if len(m.bind.refs) == 0 || m.bind.cursor < 0 || m.bind.cursor >= len(m.bind.refs) {
			return m, nil
		}
		m.bind.baseRef = m.bind.refs[m.bind.cursor]
		m.bind.stage = dashboardBindStageName
		m.bind.cursor = 0
		return m, nil
	case dashboardBindStageName:
		name := strings.TrimSpace(m.bind.name)
		if name == "" {
			m.err = fmt.Errorf("worktree name is required")
			return m, nil
		}
		m.bind.loading = true
		return m, m.createBindWorktree(m.bind.row, m.bind.baseRef, name)
	}
	return m, nil
}

func (m QueueDashboard) reload() tea.Cmd {
	return func() tea.Msg {
		snap, err := BuildDashboard(m.d, m.cfg)
		return dashboardRowsMsg{snap: snap, err: err}
	}
}

// unparkSet handles the `P` key: it writes a durable park-clear event for the
// selected parked set so the daemon may auto-spawn it again (ADR-0055).
func (m QueueDashboard) unparkSet(row DashboardRow) tea.Cmd {
	return func() tea.Msg {
		err := UnparkDashboardRow(m.d, row)
		return dashboardUnparkMsg{setID: row.SetID, err: err}
	}
}

// UnparkDashboardRow clears the park on a dashboard row's Task set by appending a
// park-clear event keyed by the repository's common dir and set id. The row must
// carry a resolved common dir (parked rows always do).
func UnparkDashboardRow(d *Deps, row DashboardRow) error {
	if d == nil || d.Tasks == nil {
		return fmt.Errorf("missing task dependencies")
	}
	commonDir := row.repoCommonDir
	if strings.TrimSpace(commonDir) == "" {
		id, err := tasks.ResolveRepositoryIdentity(d.Tasks, row.runtimePath)
		if err != nil {
			return err
		}
		commonDir = id.CommonDir
	}
	return tasks.RecordParkClear(d.Tasks, commonDir, row.SetID)
}

// archiveSet sets the reversible archived flag on the cursored set through the
// existing archive flag-write path. It touches only Task state, leaving the
// set's Worktree binding intact; the archived row drops out on the next build,
// which excludes Archived sets. Archiving is fully reversible, so no
// confirmation is required (ADR cleanup path for Done and Orphaned sets alike).
func (m QueueDashboard) archiveSet(row DashboardRow) tea.Cmd {
	return func() tea.Msg {
		err := m.d.archiveSet(row.defPath, row.SetID)
		return dashboardArchiveMsg{setID: row.SetID, err: err}
	}
}

func (m QueueDashboard) toggleAutoDrain(row DashboardRow) tea.Cmd {
	return func() tea.Msg {
		result, err := m.d.toggleAutoDrain(row.defPath, row.statePath, row.SetID)
		if err != nil {
			return dashboardToggleMsg{key: row.cursorKey, err: err}
		}
		return dashboardToggleMsg{key: row.cursorKey, autoDrain: result.AutoDrain}
	}
}

// launchDrain handles the `i` key. A set that already holds a Worktree binding
// resumes in it immediately (no picker). An unbound set opens the Drain target
// picker so the operator chooses where the drain lands (ADR-0052).
func (m QueueDashboard) launchDrain(row DashboardRow) tea.Cmd {
	return func() tea.Msg {
		bound, err := dashboardSetBound(m.d, m.cfg, row)
		if err != nil {
			return dashboardDrainListMsg{row: row, err: err}
		}
		if bound {
			_, err := LaunchDashboardDrain(m.d, m.cfg, row)
			return dashboardDrainMsg{err: err}
		}
		entries, err := DashboardDrainTargetEntries(m.d, m.cfg, row)
		return dashboardDrainListMsg{row: row, entries: entries, err: err}
	}
}

func (m QueueDashboard) updateDrainModal(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc", "ctrl+c":
		m.drainPick = nil
		return m, nil
	case "j", "down":
		m.moveDrainCursor(1)
		return m, nil
	case "k", "up":
		m.moveDrainCursor(-1)
		return m, nil
	case "enter":
		return m.confirmDrainModal()
	}
	return m, nil
}

func (m *QueueDashboard) moveDrainCursor(delta int) {
	if m.drainPick == nil {
		return
	}
	n := len(m.drainPick.entries)
	if n == 0 {
		return
	}
	m.drainPick.cursor += delta
	if m.drainPick.cursor < 0 {
		m.drainPick.cursor = n - 1
	}
	if m.drainPick.cursor >= n {
		m.drainPick.cursor = 0
	}
}

func (m QueueDashboard) confirmDrainModal() (tea.Model, tea.Cmd) {
	if m.drainPick == nil || m.drainPick.loading {
		return m, nil
	}
	if m.drainPick.cursor < 0 || m.drainPick.cursor >= len(m.drainPick.entries) {
		return m, nil
	}
	entry := m.drainPick.entries[m.drainPick.cursor]
	row := m.drainPick.row
	m.drainPick.loading = true
	return m, m.launchDrainTarget(row, entry)
}

// launchDrainTarget binds the chosen target (adopt, provision, or leave unbound
// for trunk) and drains in one action.
func (m QueueDashboard) launchDrainTarget(row DashboardRow, target dashboardDrainEntry) tea.Cmd {
	return func() tea.Msg {
		_, err := LaunchDashboardDrainTarget(m.d, m.cfg, row, target)
		return dashboardDrainMsg{err: err}
	}
}

// defaultDrainCursor positions the picker on "new managed worktree" — the
// frictionless default that provisions an isolated checkout. It falls back to the
// first entry when no trunk is resolvable (the option is absent).
func defaultDrainCursor(entries []dashboardDrainEntry) int {
	for i, e := range entries {
		if e.Kind == drainTargetNewManaged {
			return i
		}
	}
	return 0
}

func (m QueueDashboard) previewDrain(row DashboardRow) tea.Cmd {
	return func() tea.Msg {
		err := PreviewDashboardDrain(m.d, row)
		return dashboardPreviewMsg{err: err}
	}
}

func (m QueueDashboard) loadDetail(row DashboardRow) tea.Cmd {
	return func() tea.Msg {
		d := m.d
		if d == nil {
			d = DefaultDeps()
		}
		if d.Tasks == nil {
			d.Tasks = tasks.DefaultDeps()
		}
		refresh, err := d.refresh(row.defPath)
		if err != nil {
			return dashboardDetailMsg{dashRow: row, err: err}
		}
		taskRow := tasks.FindRow(refresh, row.SetID)
		manifest := refresh.Manifests[row.SetID]
		return dashboardDetailMsg{dashRow: row, manifest: manifest, taskRow: taskRow}
	}
}

func (m QueueDashboard) loadTaskText(manifest *tasks.Manifest, task tasks.Task) tea.Cmd {
	return func() tea.Msg {
		if manifest == nil {
			return dashboardTaskTextMsg{taskID: task.ID, err: fmt.Errorf("manifest not loaded")}
		}
		d := m.d
		if d == nil {
			d = DefaultDeps()
		}
		if d.Tasks == nil {
			d.Tasks = tasks.DefaultDeps()
		}
		path := filepath.Join(manifest.Dir, task.File)
		data, err := d.Tasks.FS.ReadFile(path)
		if err != nil {
			return dashboardTaskTextMsg{taskID: task.ID, path: path, err: err}
		}
		return dashboardTaskTextMsg{taskID: task.ID, path: path, text: string(data)}
	}
}

func (m QueueDashboard) loadBindWorktrees(row DashboardRow) tea.Cmd {
	return func() tea.Msg {
		entries, err := DashboardBindWorktreeEntries(m.d, m.cfg, row)
		return dashboardBindListMsg{row: row, entries: entries, err: err}
	}
}

// DashboardStatusDetailLines renders the same per-set task status detail as
// `pop tasks status <set>` for a dashboard row.
func DashboardStatusDetailLines(d *Deps, row DashboardRow) ([]string, error) {
	if d == nil {
		d = DefaultDeps()
	}
	if d.Tasks == nil {
		d.Tasks = tasks.DefaultDeps()
	}
	refresh, err := d.refresh(row.defPath)
	if err != nil {
		return nil, err
	}
	detailRow := tasks.FindRow(refresh, row.SetID)
	var buf bytes.Buffer
	tasks.RenderTaskSetDetail(&buf, row.SetID, detailRow, refresh.Manifests[row.SetID])
	text := strings.TrimRight(buf.String(), "\n")
	if text == "" {
		text = fmt.Sprintf("%s: no status detail available", row.SetID)
	}
	lines := strings.Split(text, "\n")
	if strings.TrimSpace(row.runtimePath) != "" {
		lines = append([]string{"checkout: " + row.runtimePath, ""}, lines...)
	}
	return lines, nil
}

func (m QueueDashboard) loadBindRefs(row DashboardRow) tea.Cmd {
	return func() tea.Msg {
		refs, err := DashboardBindBaseRefs(m.d, m.cfg, row)
		return dashboardBindRefsMsg{refs: refs, err: err}
	}
}

func (m QueueDashboard) adoptBindWorktree(row DashboardRow, checkoutPath string) tea.Cmd {
	return func() tea.Msg {
		_, err := DashboardAdoptWorktree(m.d, m.cfg, row, checkoutPath)
		return dashboardBindMsg{err: err}
	}
}

func (m QueueDashboard) createBindWorktree(row DashboardRow, baseRef, name string) tea.Cmd {
	return func() tea.Msg {
		_, err := DashboardCreateWorktree(m.d, m.cfg, row, baseRef, name)
		return dashboardBindMsg{err: err}
	}
}

func (m QueueDashboard) abandonWorktree(row DashboardRow) tea.Cmd {
	return func() tea.Msg {
		_, err := DashboardUnbindWorktree(m.d, m.cfg, row)
		return dashboardAbandonMsg{err: err}
	}
}

type DashboardDrainResult struct {
	PaneID      string
	RuntimePath string
}

// LaunchDashboardDrain manually launches the highlighted dashboard row through
// the same Queue provisioning and tmux spawn path used by the supervisor.
func LaunchDashboardDrain(d *Deps, cfg *config.Config, row DashboardRow) (DashboardDrainResult, error) {
	if d == nil {
		d = DefaultDeps()
	}
	if d.Tasks == nil {
		d.Tasks = tasks.DefaultDeps()
	}
	if d.Project == nil {
		d.Project = project.DefaultDeps()
	}
	scans, err := dashboardScansForDefinition(d, cfg, row.defPath)
	if err != nil {
		return DashboardDrainResult{}, err
	}
	if len(scans) == 0 {
		return DashboardDrainResult{}, fmt.Errorf("task set %s is no longer in a registered queue project", row.SetID)
	}
	repoKey, err := scanRepoKey(d, scans[0])
	if err != nil {
		return DashboardDrainResult{}, err
	}
	rep, bare, err := resolveRepresentative(d, cfg, scans)
	if err != nil {
		return DashboardDrainResult{}, err
	}
	dec := Decision{
		Project:   repoName(scans, rep),
		TaskSetID: row.SetID,
	}
	if b, ok := bindingForSet(d.Tasks, repoKey, row.SetID); ok && strings.TrimSpace(b.RuntimePath) != "" {
		if err := validateBoundWorktree(d, scans[0].ProjectPath, b); err != nil {
			return DashboardDrainResult{}, fmt.Errorf("bound worktree for %s is invalid (%v); repair git state or run `pop tasks unbind-worktree`", row.SetID, err)
		}
		worktreeReady, configErr := readRepoConfig(d, scans[0].ProjectPath)
		if configErr != "" {
			worktreeReady = false
		}
		sessionName := project.SessionNameWith(d.Project, b.RuntimePath)
		if worktreeReady && rep != nil {
			sessionName = rep.SessionName
		}
		dec.WorktreeReady = worktreeReady
		dec.scan = projectScan{
			Name:           dec.Project,
			ProjectPath:    b.RuntimePath,
			DefinitionPath: scans[0].DefinitionPath,
			RuntimePath:    b.RuntimePath,
			SessionName:    sessionName,
			RepoKey:        repoKey,
		}
	} else {
		if rep == nil {
			if bare {
				return DashboardDrainResult{}, fmt.Errorf("%s", repoScanReason)
			}
			return DashboardDrainResult{}, fmt.Errorf("no Trunk worktree configured; set trunk = true in a global [repo.\"<path>\"] block")
		}
		dec.scan = *rep
		dec.WorktreeReady, _ = readRepoConfig(d, rep.ProjectPath)
		if dec.WorktreeReady {
			dec = prepareWorktreeDrain(d, io.Discard, dec)
			if !dec.Actionable() {
				return DashboardDrainResult{}, fmt.Errorf("%s", dec.Reason)
			}
		}
	}

	spawn, err := SpawnWithResult(d, dec)
	if err != nil {
		return DashboardDrainResult{}, err
	}
	if err := recordDrainPane(d, dec, spawn.PaneID, "dashboard"); err != nil {
		return DashboardDrainResult{}, err
	}
	return DashboardDrainResult{PaneID: spawn.PaneID, RuntimePath: dec.scan.RuntimePath}, nil
}

func dashboardScansForDefinition(d *Deps, cfg *config.Config, defPath string) ([]projectScan, error) {
	projects, err := tasks.ListPickerProjectsWith(d.Project, cfg)
	if err != nil {
		return nil, err
	}
	var scans []projectScan
	for _, p := range projects {
		scan, err := resolveScan(d, p)
		if err != nil {
			if outsideQueueScopeResolveError(err) {
				continue
			}
			return nil, err
		}
		if scan.DefinitionPath == defPath {
			scans = append(scans, scan)
		}
	}
	return scans, nil
}

// PreviewDashboardDrain switches the active tmux client to the pane associated
// with the highlighted row. Rows without a recorded pane intentionally no-op.
func PreviewDashboardDrain(d *Deps, row DashboardRow) error {
	if strings.TrimSpace(row.paneID) == "" {
		return nil
	}
	if d == nil {
		d = DefaultDeps()
	}
	if d.Tmux == nil {
		d.Tmux = deps.NewRealTmux()
	}
	if _, err := d.Tmux.Command("select-pane", "-t", row.paneID); err != nil {
		return err
	}
	_, err := d.Tmux.Command("switch-client", "-t", row.paneID)
	return err
}

// DashboardUnbindWorktree releases the highlighted set's worktree binding
// through the same unbind implementation used by `pop tasks unbind-worktree`.
// The dashboard supplies its own inline confirmation, so the command-level
// prompt is skipped here.
func DashboardUnbindWorktree(d *Deps, cfg *config.Config, row DashboardRow) (AbandonResult, error) {
	key := ""
	if strings.TrimSpace(row.repoKey) != "" {
		key = setScopedKey(row.repoKey, row.SetID)
	}
	return AbandonBindingWithOptions(d, cfg, key, row.SetID, io.Discard, AbandonOptions{Yes: true, In: tasks.NonInteractiveReader{}})
}

// DashboardBindWorktreeEntries returns the inline bind picker entries for the
// highlighted dashboard row: every existing worktree in the row's repository,
// followed by the pop-native creation entry.
func DashboardBindWorktreeEntries(d *Deps, cfg *config.Config, row DashboardRow) ([]dashboardBindEntry, error) {
	scans, _, err := dashboardBindContext(d, cfg, row)
	if err != nil {
		return nil, err
	}
	out, err := d.Tasks.Git.CommandInDir(scans[0].ProjectPath, "worktree", "list", "--porcelain")
	if err != nil {
		return nil, fmt.Errorf("list worktrees: %w", err)
	}
	worktrees := parseDashboardWorktrees(out)
	entries := make([]dashboardBindEntry, 0, len(worktrees)+1)
	for _, wt := range worktrees {
		label := wt.Name
		if wt.Branch != "" {
			label = fmt.Sprintf("%s (%s)", wt.Name, wt.Branch)
		}
		entries = append(entries, dashboardBindEntry{Label: label, Path: wt.Path, Branch: wt.Branch})
	}
	entries = append(entries, dashboardBindEntry{Label: "＋ Create new worktree", Create: true})
	return entries, nil
}

// DashboardBindBaseRefs lists local and remote branch refs for the create-new
// flow, with main/master variants first.
func DashboardBindBaseRefs(d *Deps, cfg *config.Config, row DashboardRow) ([]string, error) {
	scans, _, err := dashboardBindContext(d, cfg, row)
	if err != nil {
		return nil, err
	}
	out, err := d.Tasks.Git.CommandInDir(scans[0].ProjectPath, "for-each-ref", "--format=%(refname:short)", "refs/heads", "refs/remotes")
	if err != nil {
		return nil, fmt.Errorf("list base refs: %w", err)
	}
	refs := parseDashboardBaseRefs(out)
	if len(refs) == 0 {
		return nil, fmt.Errorf("no local or remote branches found")
	}
	return refs, nil
}

// DashboardAdoptWorktree binds row.SetID to an existing checkout. The dashboard
// action is deliberate, so idle re-pointing uses Force without a second prompt.
func DashboardAdoptWorktree(d *Deps, cfg *config.Config, row DashboardRow, checkoutPath string) (BindWorktreeResult, error) {
	if err := refuseDashboardBindWhileLocked(d, row); err != nil {
		return BindWorktreeResult{}, err
	}
	return BindWorktree(d, cfg, row.SetID, checkoutPath, BindWorktreeOptions{Force: true}, io.Discard)
}

type DashboardCreateWorktreeResult struct {
	SetID       string
	RuntimePath string
	Branch      string
	BaseRef     string
}

// DashboardCreateWorktree creates a pop-managed worktree on a fresh branch and
// records a provisioned binding. It never opens or attaches a tmux session.
func DashboardCreateWorktree(d *Deps, cfg *config.Config, row DashboardRow, baseRef, name string) (DashboardCreateWorktreeResult, error) {
	baseRef = strings.TrimSpace(baseRef)
	name = strings.TrimSpace(name)
	if baseRef == "" {
		return DashboardCreateWorktreeResult{}, fmt.Errorf("base ref is required")
	}
	if name == "" {
		return DashboardCreateWorktreeResult{}, fmt.Errorf("worktree name is required")
	}
	scans, repoKey, err := dashboardBindContext(d, cfg, row)
	if err != nil {
		return DashboardCreateWorktreeResult{}, err
	}
	if err := refuseDashboardBindWhileLocked(d, row); err != nil {
		return DashboardCreateWorktreeResult{}, err
	}
	branch := name
	path := filepath.Join(QueueDataDir(d.Tasks), "worktrees", repoKey, binding.SafeComponent(name))
	if err := d.Tasks.FS.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return DashboardCreateWorktreeResult{}, fmt.Errorf("create worktree parent: %w", err)
	}
	if _, err := d.Tasks.Git.CommandInDir(scans[0].ProjectPath, "worktree", "add", "-b", branch, path, baseRef); err != nil {
		return DashboardCreateWorktreeResult{}, fmt.Errorf("git worktree add: %w", err)
	}
	proj := repoName(scans, nil)
	if rep, _, err := resolveRepresentative(d, cfg, scans); err == nil {
		proj = repoName(scans, rep)
	}
	key := setScopedKey(repoKey, row.SetID)
	if err := binding.Put(d.Tasks, key, binding.Binding{RuntimePath: path, Branch: branch, Project: proj, Provisioned: true}); err != nil {
		return DashboardCreateWorktreeResult{}, err
	}
	return DashboardCreateWorktreeResult{SetID: row.SetID, RuntimePath: path, Branch: branch, BaseRef: baseRef}, nil
}

// dashboardSetBound reports whether the row's set already holds a Worktree
// binding. The Drain target picker only opens for unbound sets; a bound set
// resumes in its binding (ADR-0052).
func dashboardSetBound(d *Deps, cfg *config.Config, row DashboardRow) (bool, error) {
	d = ensureQueueDeps(d)
	repoKey := row.repoKey
	if repoKey == "" {
		_, rk, err := dashboardBindContext(d, cfg, row)
		if err != nil {
			return false, err
		}
		repoKey = rk
	}
	b, ok := bindingForSet(d.Tasks, repoKey, row.SetID)
	return ok && strings.TrimSpace(b.RuntimePath) != "", nil
}

// DashboardDrainTargetEntries builds the Drain target picker options for an
// unbound set (ADR-0052), in order: the repo's existing non-managed, unbound
// worktrees (adopt), "new managed worktree" (provision off the trunk), then the
// trunk itself (drain inline). The trunk-dependent options are omitted when no
// trunk resolves (an unconfigured bare repo). Managed worktrees, the trunk, and
// any worktree already bound to another set are excluded from the adopt list to
// preserve the 1:1 checkout↔set mapping.
func DashboardDrainTargetEntries(d *Deps, cfg *config.Config, row DashboardRow) ([]dashboardDrainEntry, error) {
	scans, _, err := dashboardBindContext(d, cfg, row)
	if err != nil {
		return nil, err
	}
	projectPath := scans[0].ProjectPath

	trunkPath, bare, trunkErr := binding.ResolveTrunkPath(d.Tasks, cfg, projectPath)
	hasTrunk := trunkErr == nil && !bare && strings.TrimSpace(trunkPath) != ""
	canonTrunk := ""
	if hasTrunk {
		canonTrunk = bestEffortCanon(d, trunkPath)
	}

	out, err := d.Tasks.Git.CommandInDir(projectPath, "worktree", "list", "--porcelain")
	if err != nil {
		return nil, fmt.Errorf("list worktrees: %w", err)
	}
	worktrees := parseDashboardWorktrees(out)
	bound, err := boundCheckoutPaths(d)
	if err != nil {
		return nil, err
	}
	managedRoot := bestEffortCanon(d, binding.ManagedWorktreesRoot(d.Tasks))

	var entries []dashboardDrainEntry
	for _, wt := range worktrees {
		canon := bestEffortCanon(d, wt.Path)
		if hasTrunk && canon == canonTrunk {
			continue // the trunk is offered as its own option
		}
		if pathUnder(canon, managedRoot) {
			continue // a pop-managed worktree
		}
		if bound[canon] {
			continue // already bound to another set (1:1 checkout↔set)
		}
		label := wt.Name
		if wt.Branch != "" {
			label = fmt.Sprintf("%s (%s)", wt.Name, wt.Branch)
		}
		entries = append(entries, dashboardDrainEntry{Label: label, Kind: drainTargetWorktree, Path: wt.Path, Branch: wt.Branch})
	}
	if hasTrunk {
		entries = append(entries, dashboardDrainEntry{Label: "＋ New managed worktree (fork from trunk)", Kind: drainTargetNewManaged})
		entries = append(entries, dashboardDrainEntry{Label: "Trunk worktree (drain inline)", Kind: drainTargetTrunk, Path: trunkPath})
	}
	return entries, nil
}

// LaunchDashboardDrainTarget binds the chosen Drain target picker option and
// drains in one action (ADR-0052): an existing worktree is adopted, "new managed
// worktree" provisions a managed checkout forked from the trunk, and trunk leaves
// the set unbound so LaunchDashboardDrain routes it to the trunk. Once bound (or
// for trunk, immediately), it reuses LaunchDashboardDrain to spawn the drain.
func LaunchDashboardDrainTarget(d *Deps, cfg *config.Config, row DashboardRow, target dashboardDrainEntry) (DashboardDrainResult, error) {
	switch target.Kind {
	case drainTargetWorktree:
		if _, err := DashboardAdoptWorktree(d, cfg, row, target.Path); err != nil {
			return DashboardDrainResult{}, err
		}
	case drainTargetNewManaged:
		if _, err := DashboardProvisionManagedWorktree(d, cfg, row); err != nil {
			return DashboardDrainResult{}, err
		}
	case drainTargetTrunk:
		// Leave the set unbound: LaunchDashboardDrain routes to the representative
		// checkout (the trunk) and records no binding — a trunk drain is inline.
	default:
		return DashboardDrainResult{}, fmt.Errorf("unknown drain target")
	}
	return LaunchDashboardDrain(d, cfg, row)
}

// DashboardProvisionManagedWorktree provisions a pop-managed worktree forked
// from the Trunk worktree's HEAD and records a provisioned binding, reusing the
// shared provisioning path (ADR-0052). It refuses a repo with no resolvable
// trunk and never opens or attaches a tmux session.
func DashboardProvisionManagedWorktree(d *Deps, cfg *config.Config, row DashboardRow) (DashboardCreateWorktreeResult, error) {
	scans, repoKey, err := dashboardBindContext(d, cfg, row)
	if err != nil {
		return DashboardCreateWorktreeResult{}, err
	}
	if err := refuseDashboardBindWhileLocked(d, row); err != nil {
		return DashboardCreateWorktreeResult{}, err
	}
	if b, ok := bindingForSet(d.Tasks, repoKey, row.SetID); ok && strings.TrimSpace(b.RuntimePath) != "" {
		return DashboardCreateWorktreeResult{}, fmt.Errorf("task set %s is already bound; unbind first to retarget", row.SetID)
	}
	trunkPath, bare, err := binding.ResolveTrunkPath(d.Tasks, cfg, scans[0].ProjectPath)
	if err != nil {
		return DashboardCreateWorktreeResult{}, err
	}
	if bare || strings.TrimSpace(trunkPath) == "" {
		return DashboardCreateWorktreeResult{}, fmt.Errorf("no Trunk worktree configured; set trunk = true in a global [repo.\"<path>\"] block")
	}
	b, err := binding.ProvisionWorktree(d.Tasks, binding.ManagedWorktreesRoot(d.Tasks), trunkPath, row.SetID, d.now())
	if err != nil {
		return DashboardCreateWorktreeResult{}, err
	}
	proj := repoName(scans, nil)
	if rep, _, repErr := resolveRepresentative(d, cfg, scans); repErr == nil {
		proj = repoName(scans, rep)
	}
	b.Project = proj
	key := setScopedKey(repoKey, row.SetID)
	if err := binding.Put(d.Tasks, key, b); err != nil {
		return DashboardCreateWorktreeResult{}, err
	}
	return DashboardCreateWorktreeResult{SetID: row.SetID, RuntimePath: b.RuntimePath, Branch: b.Branch}, nil
}

// boundCheckoutPaths returns the canonicalized set of every checkout currently
// bound to a set, across all repos. The Drain target picker excludes these from
// its adopt list so a checkout never binds to two sets at once.
func boundCheckoutPaths(d *Deps) (map[string]bool, error) {
	bindings, err := binding.AllBindings(d.Tasks)
	if err != nil {
		return nil, err
	}
	out := map[string]bool{}
	for _, b := range bindings {
		path := strings.TrimSpace(b.RuntimePath)
		if path == "" {
			continue
		}
		out[bestEffortCanon(d, path)] = true
	}
	return out, nil
}

// bestEffortCanon canonicalizes path for reliable comparison, falling back to a
// cleaned absolute path when the target does not exist (so EvalSymlinks fails).
func bestEffortCanon(d *Deps, path string) string {
	if c, err := canonicalCheckoutPath(d.Tasks, path); err == nil {
		return c
	}
	if abs, err := filepath.Abs(path); err == nil {
		return filepath.Clean(abs)
	}
	return filepath.Clean(path)
}

// pathUnder reports whether path is root or lives beneath it. Both arguments are
// expected to be canonicalized.
func pathUnder(path, root string) bool {
	if root == "" {
		return false
	}
	if path == root {
		return true
	}
	return strings.HasPrefix(path, root+string(filepath.Separator))
}

func dashboardBindContext(d *Deps, cfg *config.Config, row DashboardRow) ([]projectScan, string, error) {
	if d == nil {
		d = DefaultDeps()
	}
	if d.Tasks == nil {
		d.Tasks = tasks.DefaultDeps()
	}
	if d.Project == nil {
		d.Project = project.DefaultDeps()
	}
	// Fast path: a row built by the live dashboard already carries its repo
	// group's resolved coordinates (the integration target checkout and repo
	// key), derived fork-free at build time (ADR-0060). Every bind/drain
	// sub-action consumes only scans[0].ProjectPath and the repo key, so reuse
	// them directly instead of re-forking `git rev-parse` across every registered
	// project — the sequential rescan that left the inline bind picker stuck on
	// "loading...".
	if row.projectPath != "" && row.repoKey != "" {
		scan := projectScan{
			Name:           row.Project,
			ProjectPath:    row.projectPath,
			DefinitionPath: row.defPath,
			RuntimePath:    row.projectPath,
			SessionName:    project.SessionNameWith(d.Project, row.projectPath),
			RepoKey:        row.repoKey,
		}
		return []projectScan{scan}, row.repoKey, nil
	}
	scans, err := dashboardScansForDefinition(d, cfg, row.defPath)
	if err != nil {
		return nil, "", err
	}
	if len(scans) == 0 {
		return nil, "", fmt.Errorf("task set %s is no longer in a registered queue project", row.SetID)
	}
	repoKey := row.repoKey
	if repoKey == "" {
		repoKey, err = scanRepoKey(d, scans[0])
		if err != nil {
			return nil, "", err
		}
	}
	return scans, repoKey, nil
}

func refuseDashboardBindWhileLocked(d *Deps, row DashboardRow) error {
	if d == nil {
		d = DefaultDeps()
	}
	if d.Tasks == nil {
		d.Tasks = tasks.DefaultDeps()
	}
	paths := map[string]bool{}
	if strings.TrimSpace(row.runtimePath) != "" {
		paths[row.runtimePath] = true
	}
	if row.repoKey != "" {
		if b, ok := bindingForSet(d.Tasks, row.repoKey, row.SetID); ok && b.RuntimePath != "" {
			paths[b.RuntimePath] = true
		}
	}
	for path := range paths {
		lock := d.readLock(path)
		if lock == nil || !lock.Locked {
			continue
		}
		if lock.Metadata == nil || lock.Metadata.SetID == "" || lock.Metadata.SetID == row.SetID {
			return fmt.Errorf("refusing bind-worktree: %s is currently executing", row.SetID)
		}
	}
	return nil
}

func parseDashboardWorktrees(output string) []project.Worktree {
	var worktrees []project.Worktree
	var current project.Worktree
	isBare := false
	for _, line := range strings.Split(output, "\n") {
		switch {
		case strings.HasPrefix(line, "worktree "):
			current.Path = strings.TrimPrefix(line, "worktree ")
			current.Name = filepath.Base(current.Path)
		case strings.HasPrefix(line, "branch "):
			current.Branch = strings.TrimPrefix(strings.TrimPrefix(line, "branch "), "refs/heads/")
		case line == "detached":
			current.Branch = "detached"
		case line == "bare":
			isBare = true
		case line == "":
			if current.Path != "" && !isBare {
				worktrees = append(worktrees, current)
			}
			current = project.Worktree{}
			isBare = false
		}
	}
	if current.Path != "" && !isBare {
		worktrees = append(worktrees, current)
	}
	return worktrees
}

func parseDashboardBaseRefs(output string) []string {
	seen := map[string]bool{}
	var refs []string
	for _, line := range strings.Split(output, "\n") {
		ref := strings.TrimSpace(line)
		if ref == "" || strings.HasSuffix(ref, "/HEAD") || seen[ref] {
			continue
		}
		seen[ref] = true
		refs = append(refs, ref)
	}
	sort.SliceStable(refs, func(i, j int) bool {
		ri, rj := dashboardBaseRefRank(refs[i]), dashboardBaseRefRank(refs[j])
		if ri != rj {
			return ri < rj
		}
		return refs[i] < refs[j]
	})
	return refs
}

func dashboardBaseRefRank(ref string) int {
	switch ref {
	case "main":
		return 0
	case "master":
		return 1
	}
	if strings.HasSuffix(ref, "/main") {
		return 2
	}
	if strings.HasSuffix(ref, "/master") {
		return 3
	}
	return 4
}

func dashboardTick() tea.Cmd {
	return tea.Tick(dashboardPollInterval, func(time.Time) tea.Msg { return dashboardTickMsg{} })
}

func dashboardCursorAfterReload(rows []DashboardRow, key string, previous int) int {
	if len(rows) == 0 {
		return 0
	}
	if key != "" {
		for i, row := range rows {
			if row.cursorKey == key {
				return i
			}
		}
	}
	if previous >= len(rows) {
		return len(rows) - 1
	}
	if previous < 0 {
		return 0
	}
	return previous
}

func (m QueueDashboard) View() tea.View {
	if m.detail != nil {
		content := m.viewDetail()
		v := tea.NewView(content)
		v.AltScreen = true
		return v
	}
	var body strings.Builder
	if m.err != nil {
		fmt.Fprintf(&body, "refresh error: %v\n", m.err)
	}
	if len(m.snap.Rows) == 0 {
		if m.filterMode {
			fmt.Fprintln(&body, "No matching task sets.")
		} else {
			fmt.Fprintln(&body, "No queue-actionable task sets.")
		}
		hint := "h/esc quit"
		if m.filterMode {
			ui.WriteInputBox(&body, m.width, m.filterInput.View())
			hint = "esc clear filter"
		}
		writeDashboardFooter(&body, m.height, ui.HintStyle.Render(hint))
		content := body.String()
		v := tea.NewView(content)
		v.AltScreen = true
		return v
	}
	fmt.Fprintf(&body, "Queue · %s\n", dashboardSummary(m.snap.Rows))
	fmt.Fprintln(&body)
	if m.menu != nil {
		renderDashboardTableWithMenu(&body, m.snap.Rows, m.cursor, m.width, m.height, m.menu)
	} else {
		renderDashboardTable(&body, m.snap.Rows, m.cursor, m.width)
	}
	hint := "j/k move · gg/G top/bottom · l/enter status · a actions · / filter · h/esc quit"
	footer := true
	if m.menu != nil {
		hint = "j/k move · enter/letter run · esc close"
	} else if m.bind != nil {
		renderDashboardBindModal(&body, m.bind)
		footer = false
	} else if m.drainPick != nil {
		renderDashboardDrainModal(&body, m.drainPick)
		footer = false
	} else if m.abandon != nil {
		renderDashboardAbandonModal(&body, m.abandon)
		footer = false
	} else if m.filterMode {
		ui.WriteInputBox(&body, m.width, m.filterInput.View())
		hint = "esc clear filter · j/k navigate"
	}
	if footer {
		if m.statusMsg != "" {
			fmt.Fprintf(&body, "  %s\n", m.statusMsg)
		}
		writeDashboardFooter(&body, m.height, ui.HintStyle.Render(hint))
	}
	content := body.String()
	v := tea.NewView(content)
	v.AltScreen = true
	return v
}

func dashboardSummary(rows []DashboardRow) string {
	total := len(rows)
	ready := 0
	running := 0
	autoDrain := 0
	for _, row := range rows {
		if row.RawStatus == tasks.StatusReady {
			ready++
		}
		if strings.TrimSpace(row.Drain) != "" {
			running++
		}
		if row.AutoDrain {
			autoDrain++
		}
	}
	parts := []string{countPhrase(total, "task set", "task sets")}
	if ready > 0 {
		parts = append(parts, countPhrase(ready, "ready", "ready"))
	}
	if running > 0 {
		parts = append(parts, countPhrase(running, "running", "running"))
	}
	if autoDrain > 0 {
		parts = append(parts, countPhrase(autoDrain, "auto-drain", "auto-drain"))
	}
	return strings.Join(parts, " · ")
}

func countPhrase(n int, singular, plural string) string {
	if n == 1 {
		return fmt.Sprintf("1 %s", singular)
	}
	return fmt.Sprintf("%d %s", n, plural)
}

// viewDetail renders the full-screen task-set detail view.
func (m QueueDashboard) viewDetail() string {
	var b strings.Builder
	d := m.detail
	if d.peek != nil {
		renderTaskTextPeek(&b, d, m.height, m.width, m.taskMenu)
		return b.String()
	}
	if d.loading {
		fmt.Fprintf(&b, "Loading %s...\n", d.row.SetID)
		writeDashboardFooter(&b, m.height, ui.HintStyle.Render("  h/esc back"))
		return b.String()
	}
	if d.err != nil {
		fmt.Fprintf(&b, "error loading %s: %v\n", d.row.SetID, d.err)
		writeDashboardFooter(&b, m.height, ui.HintStyle.Render("  h/esc back"))
		return b.String()
	}
	renderDetailContent(&b, d, m.height, m.width, m.taskMenu)
	return b.String()
}

// renderDetailContent renders the task list with cursor indicators for the
// detail view. The cursor is on the task identified by detailView.cursorID.
func renderDetailContent(b *strings.Builder, d *detailView, height, width int, menu *taskMenu) {
	manifest := d.manifest
	taskRow := d.taskRow

	status := tasks.DeriveStatus(manifest)
	label := string(status)
	progress := ""
	if taskRow != nil {
		status = taskRow.Status
		label = tasks.StatusLabel(*taskRow)
		progress = taskRow.Progress
	}

	header := fmt.Sprintf("Task · %s  [%s]", d.row.SetID, label)
	if progress != "" {
		header += "  " + progress
	}
	fmt.Fprintln(b, header)

	if status == tasks.StatusMissing {
		fmt.Fprintln(b, "  registered task set missing")
		writeDashboardFooter(b, height, ui.HintStyle.Render("  h/esc back"))
		return
	}
	if manifest == nil || !manifest.Valid {
		fmt.Fprintln(b, "  malformed manifest")
		if manifest != nil {
			for _, e := range manifest.Errors {
				fmt.Fprintf(b, "  - %s\n", e)
			}
		}
		writeDashboardFooter(b, height, ui.HintStyle.Render("  h/esc back"))
		return
	}

	fmt.Fprintln(b)

	const (
		stW    = 10
		tyW    = 4
		titleW = 40
	)
	idW := len("ID")
	for _, t := range manifest.Tasks {
		if len(t.ID) > idW {
			idW = len(t.ID)
		}
	}

	fmt.Fprintf(b, "  %-*s  %-*s  %-*s  %-*s  %s\n",
		stW, "STATUS", tyW, "TYPE", idW, "ID", titleW, "TITLE", "BLOCKED-BY")
	fmt.Fprintf(b, "  %-*s  %-*s  %-*s  %-*s  %s\n",
		stW, strings.Repeat("-", stW),
		tyW, strings.Repeat("-", tyW),
		idW, strings.Repeat("-", idW),
		titleW, strings.Repeat("-", titleW),
		strings.Repeat("-", 12))

	cursorIdx := d.cursorIndex()
	var menuLines []string
	placeBelow := true
	if menu != nil && !menu.inPeek {
		menuLines = taskMenuLines(menu, width)
		placeBelow = dashboardMenuPlaceBelow(cursorIdx, len(menuLines), height)
	}
	writeMenu := func() {
		for _, ml := range menuLines {
			fmt.Fprintf(b, "%s\n", ml)
		}
	}
	for i, t := range manifest.Tasks {
		if menuLines != nil && i == cursorIdx && !placeBelow {
			writeMenu()
		}
		prefix := "  "
		if i == cursorIdx {
			prefix = ui.IndicatorStyle.Render("█") + " "
		}
		title := t.Title
		if len(title) > titleW {
			title = title[:titleW-3] + "..."
		}
		blockedBy := "-"
		if len(t.BlockedBy) > 0 {
			blockedBy = strings.Join(t.BlockedBy, ", ")
		}
		statusCell := t.Status
		if t.Status == "failed" && t.FailedAfter != nil {
			statusCell = fmt.Sprintf("failed(%d)", *t.FailedAfter)
		}
		line := fmt.Sprintf("%-*s  %-*s  %-*s  %-*s  %s",
			stW, statusCell, tyW, t.Type, idW, t.ID, titleW, title, blockedBy)
		fmt.Fprintf(b, "%s%s\n", prefix, line)
		if menuLines != nil && i == cursorIdx && placeBelow {
			writeMenu()
		}
	}

	fmt.Fprintln(b)
	if d.statusMsg != "" {
		fmt.Fprintf(b, "  %s\n", d.statusMsg)
	}
	hint := "  j/k · gg/G top/bottom · l/enter peek · a actions · h/esc back"
	if menu != nil {
		hint = "  j/k move · enter/letter run · esc close"
	}
	writeDashboardFooter(b, height, ui.HintStyle.Render(hint))
}

// taskMenuLines renders the task-level action overlay as a block of lines,
// indented to nest under the cursored task, with the highlighted item carrying
// the shared cursor block. The first line is a dimmed "actions" caption. It
// mirrors dashboardMenuLines (the set-view overlay) for a consistent look.
func taskMenuLines(menu *taskMenu, width int) []string {
	if menu == nil {
		return nil
	}
	lines := []string{ui.TruncateString("    "+ui.HintStyle.Render("actions"), width)}
	for i, item := range menu.items {
		marker := "  "
		if i == menu.cursor {
			marker = ui.IndicatorStyle.Render("█") + " "
		}
		line := fmt.Sprintf("    %s%s  %s", marker, item.key, item.label)
		lines = append(lines, ui.TruncateString(line, width))
	}
	return lines
}

func renderTaskTextPeek(b *strings.Builder, d *detailView, height, width int, menu *taskMenu) {
	p := d.peek
	header := d.row.SetID
	if p.taskID != "" {
		header += " / " + p.taskID
	}
	fmt.Fprintln(b, header)
	if menu != nil && menu.inPeek {
		for _, ml := range taskMenuLines(menu, width) {
			fmt.Fprintln(b, ml)
		}
	}
	if p.loading {
		fmt.Fprintln(b, "  loading task text...")
		writeDashboardFooter(b, height, ui.HintStyle.Render("  h/esc back"))
		return
	}
	if p.err != nil {
		fmt.Fprintf(b, "  error loading task text: %v\n", p.err)
		if p.path != "" {
			fmt.Fprintf(b, "  %s\n", p.path)
		}
		writeDashboardFooter(b, height, ui.HintStyle.Render("  h/esc back"))
		return
	}
	if p.path != "" {
		fmt.Fprintf(b, "  %s\n\n", p.path)
	}
	lines := taskTextPeekLines(p.text)
	pageSize := taskTextPeekPageSize(height, p.path)
	maxScroll := len(lines) - pageSize
	if maxScroll < 0 {
		maxScroll = 0
	}
	if p.scroll > maxScroll {
		p.scroll = maxScroll
	}
	if p.scroll < 0 {
		p.scroll = 0
	}
	if len(lines) == 0 {
		fmt.Fprintln(b, "  (empty task file)")
	} else {
		end := p.scroll + pageSize
		if end > len(lines) {
			end = len(lines)
		}
		for _, line := range lines[p.scroll:end] {
			fmt.Fprintln(b, ui.TruncateString(line, width))
		}
	}
	fmt.Fprintln(b)
	position := ""
	if maxScroll > 0 {
		position = fmt.Sprintf(" · %d/%d", p.scroll+1, len(lines))
	}
	hint := "  j/k · C-d/C-u · gg/G · a actions · h/esc back" + position
	if menu != nil && menu.inPeek {
		hint = "  j/k move · enter/letter run · esc close"
	}
	writeDashboardFooter(b, height, ui.HintStyle.Render(hint))
}

func taskTextPeekLines(text string) []string {
	if strings.TrimSpace(text) == "" {
		return nil
	}
	lines := strings.Split(text, "\n")
	if len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	return lines
}

func taskTextPeekPageSize(height int, path string) int {
	if height <= 0 {
		height = 20
	}
	pathLines := 0
	if path != "" {
		pathLines = 2
	}
	pageSize := height - 1 /* title */ - 1 /* header */ - pathLines - 1 /* hint */
	if pageSize < 1 {
		return 1
	}
	return pageSize
}

func halfPageDelta(pageSize int) int {
	if pageSize <= 1 {
		return 1
	}
	return pageSize / 2
}

func renderDashboardBindModal(w io.Writer, modal *dashboardBindModal) {
	if modal == nil {
		return
	}
	fmt.Fprintln(w, "Bind worktree")
	if modal.loading {
		fmt.Fprintln(w, "  loading...")
		return
	}
	switch modal.stage {
	case dashboardBindStageWorktree:
		for i, entry := range modal.entries {
			prefix := "  "
			if i == modal.cursor {
				prefix = ui.IndicatorStyle.Render("█") + " "
			}
			label := entry.Label
			if label == "" {
				label = entry.Path
			}
			fmt.Fprintf(w, "%s%s\n", prefix, label)
		}
		fmt.Fprint(w, ui.HintStyle.Render("enter select · esc cancel"))
	case dashboardBindStageBaseRef:
		fmt.Fprintln(w, "Base ref")
		for i, ref := range modal.refs {
			prefix := "  "
			if i == modal.cursor {
				prefix = ui.IndicatorStyle.Render("█") + " "
			}
			fmt.Fprintf(w, "%s%s\n", prefix, ref)
		}
		fmt.Fprint(w, ui.HintStyle.Render("enter select · esc cancel"))
	case dashboardBindStageName:
		fmt.Fprintf(w, "Base: %s\n", modal.baseRef)
		fmt.Fprintf(w, "Name: %s\n", modal.name)
		fmt.Fprint(w, ui.HintStyle.Render("enter create · esc cancel"))
	}
}

func renderDashboardDrainModal(w io.Writer, modal *dashboardDrainModal) {
	if modal == nil {
		return
	}
	fmt.Fprintf(w, "Drain target for %s\n", modal.row.SetID)
	if modal.loading {
		fmt.Fprintln(w, "  draining...")
		return
	}
	for i, entry := range modal.entries {
		prefix := "  "
		if i == modal.cursor {
			prefix = ui.IndicatorStyle.Render("█") + " "
		}
		label := entry.Label
		if label == "" {
			label = entry.Path
		}
		fmt.Fprintf(w, "%s%s\n", prefix, label)
	}
	fmt.Fprint(w, ui.HintStyle.Render("enter drain · esc cancel"))
}

func renderDashboardAbandonModal(w io.Writer, modal *dashboardAbandonModal) {
	if modal == nil {
		return
	}
	fmt.Fprintf(w, "Unbind worktree for %s\n", modal.row.SetID)
	if modal.loading {
		fmt.Fprintln(w, "  unbinding...")
		return
	}
	fmt.Fprintln(w, "This releases the binding without integrating. Task statuses are unchanged.")
	fmt.Fprint(w, ui.HintStyle.Render("enter/y confirm · n/esc cancel"))
}

func renderDashboardTable(w io.Writer, rows []DashboardRow, cursor, width int) {
	renderDashboardTableWithMenu(w, rows, cursor, width, 0, nil)
}

// renderDashboardTableWithMenu renders the task-set table and, when menu is
// non-nil, splices the action overlay in next to the cursored row: below it by
// default, flipping above when the cursor sits too low for the menu to fit
// beneath it within height (dashboardMenuPlaceBelow).
func renderDashboardTableWithMenu(w io.Writer, rows []DashboardRow, cursor, width, height int, menu *dashboardMenu) {
	headers := []string{"PROJECT", "TASK SET", "STATUS", "WORKTREE", "DRAIN", ""}
	widths := []int{len(headers[0]), len(headers[1]), len(headers[2]), len(headers[3]), len(headers[4])}
	widths = append(widths, len(headers[5]))
	for _, row := range rows {
		values := dashboardRowValues(row)
		for i, v := range values {
			if n := lipgloss.Width(v); n > widths[i] {
				widths[i] = n
			}
		}
	}
	fmt.Fprintf(w, "%s\n", ui.TruncateString("  "+dashboardTableLine(headers, widths), width))
	fmt.Fprintf(w, "%s\n", ui.TruncateString("  "+dashboardTableSeparator(widths), width))

	var menuLines []string
	placeBelow := true
	if menu != nil {
		menuLines = dashboardMenuLines(menu, width)
		placeBelow = dashboardMenuPlaceBelow(cursor, len(menuLines), height)
	}
	writeMenu := func() {
		for _, ml := range menuLines {
			fmt.Fprintf(w, "%s\n", ml)
		}
	}
	for i, row := range rows {
		if menu != nil && i == cursor && !placeBelow {
			writeMenu()
		}
		var prefix string
		if i == cursor {
			prefix = ui.IndicatorStyle.Render("█") + " "
		} else {
			prefix = "  "
		}
		line := ui.TruncateString(prefix+dashboardTableLine(dashboardRowValues(row), widths), width)
		fmt.Fprintf(w, "%s\n", line)
		if menu != nil && i == cursor && placeBelow {
			writeMenu()
		}
	}
}

// dashboardTableTopOffset is the number of lines above the first table row in
// the dashboard view: the summary line, a blank, the header, and the separator.
const dashboardTableTopOffset = 4

// dashboardMenuPlaceBelow reports whether the action menu of menuHeight lines
// should render below the cursor row (true) or flip above it (false). It flips
// above only when the cursor sits low enough that the menu would not fit beneath
// it within the viewport. A non-positive height (no WindowSizeMsg yet) keeps the
// menu below.
func dashboardMenuPlaceBelow(cursor, menuHeight, height int) bool {
	if height <= 0 {
		return true
	}
	linesBelowCursor := height - 1 - dashboardTableTopOffset - cursor
	return linesBelowCursor >= menuHeight
}

// dashboardMenuLines renders the action overlay as a block of lines indented to
// nest under the cursored row, with the highlighted item carrying the shared
// cursor block. The first line is a dimmed "actions" caption.
func dashboardMenuLines(menu *dashboardMenu, width int) []string {
	if menu == nil {
		return nil
	}
	lines := []string{ui.TruncateString("    "+ui.HintStyle.Render("actions"), width)}
	for i, item := range menu.items {
		marker := "  "
		if i == menu.cursor {
			marker = ui.IndicatorStyle.Render("█") + " "
		}
		line := fmt.Sprintf("    %s%s  %s", marker, item.key, item.label)
		lines = append(lines, ui.TruncateString(line, width))
	}
	return lines
}

func writeDashboardFooter(b *strings.Builder, height int, hint string) {
	if height > 0 {
		lines := strings.Count(b.String(), "\n")
		for lines < height-1 {
			b.WriteByte('\n')
			lines++
		}
		if b.Len() > 0 && !strings.HasSuffix(b.String(), "\n") {
			b.WriteByte('\n')
		}
	} else if b.Len() > 0 {
		b.WriteByte('\n')
	}
	fmt.Fprint(b, hint)
}


func dashboardRowValues(row DashboardRow) []string {
	var badges []string
	if row.orphaned {
		badges = append(badges, "orphaned")
	}
	if row.AutoDrain {
		badges = append(badges, "Auto-drain")
	}
	return []string{row.Project, row.SetID, row.Status, renderDashboardDest(row.destKind, row.Worktree), row.Drain, strings.Join(badges, " ")}
}

func dashboardTableLine(values []string, widths []int) string {
	parts := make([]string, len(values))
	for i, v := range values {
		parts[i] = padDashboardCell(v, widths[i])
	}
	return strings.Join(parts, "  ")
}

func dashboardTableSeparator(widths []int) string {
	parts := make([]string, len(widths))
	for i, width := range widths {
		parts[i] = strings.Repeat("-", width)
	}
	return strings.Join(parts, "  ")
}

func padDashboardCell(s string, width int) string {
	if pad := width - lipgloss.Width(s); pad > 0 {
		return s + strings.Repeat(" ", pad)
	}
	return s
}

// RunDashboard opens the read-only Queue dashboard TUI.
func RunDashboard(d *Deps, cfg *config.Config) error {
	m := newQueueDashboard(d, cfg, DashboardSnapshot{})
	snap, err := BuildDashboard(d, cfg)
	if err != nil {
		return err
	}
	m.snap = snap
	program := tea.NewProgram(m)
	_, err = program.Run()
	return err
}
