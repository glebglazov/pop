package queue

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/glebglazov/pop/config"
	"github.com/glebglazov/pop/project"
	"github.com/glebglazov/pop/tasks"
	"github.com/glebglazov/pop/tasks/binding"
	"github.com/glebglazov/pop/ui"
)

const dashboardPollInterval = 2 * time.Second

// SetRef holds the resolved, fork-free coordinates of one registered Task set
// that the Queue write-path acts on, plus the per-build derived facts the
// write-path branches on. Nothing re-resolves these; they are carried,
// honoring the fork-free build (ADR-0060).
type SetRef struct {
	DefPath, StatePath, SetID string
	RepoKey, RepoCommonDir    string
	ProjectPath, RuntimePath  string
	// ProjectName is the pre-resolved project label (dashboardRepoStatic.projectName),
	// carried so the adopt path can skip DetectProject's per-project git fan-out
	// (ADR-0060).
	ProjectName string
	// Parked is true when the set's repeated abnormal terminals have parked it
	// (derived from Drain history); unpark writes a park-clear event (ADR-0055).
	// Bound is true when the set holds a Worktree binding with a non-blank
	// runtime path — the dedicated-checkout fact the action menu gates unbind
	// on. Derived per-build from the binding snapshot (no git fork), mirroring
	// dashboardSetBound.
	// Orphaned is true when the set's Worktree binding points at a checkout
	// that no longer exists on disk. Like Picked-up, it is a derived per-build
	// fact (a cheap filesystem stat, never a git fork), not a persisted
	// status, and is orthogonal to Task-set status — a set of any status may
	// be orphaned. A set with no binding can never be orphaned.
	Parked, Bound, Orphaned bool
	AutoDrain               bool
	// ConfigError is the message for a config-class defect that keeps the set
	// from routing to an integration target — a bare repo with no declared trunk
	// or an unsatisfiable worktree directive (ADR-0059/0060). Non-blank only when
	// the set is neither live-drained nor parked, preserving the mutual exclusion
	// the retired single-string DRAIN cell enforced. Rendered as the plain
	// ` · config error: <msg>` STATUS suffix (ADR-0111).
	ConfigError string
	// RawStatus is the underlying derived Task-set status, kept for counts and
	// comparisons so display relabels never leak into logic.
	RawStatus tasks.TaskSetStatus
	// DoneStillManagedBound is true when a Done set still holds a
	// pop-provisioned (managed) Worktree binding. The dashboard keeps such a
	// row visible as a clean-up reminder until archived or unbound (ADR-0070).
	DoneStillManagedBound bool
	// PaneID is the tmux pane recorded for a live drain of this set, empty if
	// none was recorded. It is the fact PreviewDrain branches on.
	PaneID string
	// LiveDrain is true when a live (PID-alive) Runtime execution lock holds
	// this set's checkout — the structured fact that replaced the retired DRAIN
	// column (ADR-0111). It lights the trailing ● live-drain indicator across every
	// status, and drives Sort's running tier, the header "N running" count, the
	// auto-drain suffix silencing (ADR-0108), and the READY→IN PROGRESS
	// refinement. Derived per-build from the live-drain snapshot, never a git fork.
	LiveDrain bool
}

// DashboardRow is one read-only Queue dashboard table row.
type DashboardRow struct {
	SetRef

	Project string
	// Started mirrors tasks.Row.Started: a started READY set renders as
	// "IN PROGRESS". It is a presentational input to the render-time STATUS
	// composition (dashboardStatusCell), never a schedulability fact — logic keys
	// on RawStatus.
	Started bool
	// VerifiedAtSHA mirrors tasks.Row.VerifiedAtSHA: the short SHA of the
	// immunizing PASS verdict, rendered as a yellow "verified @ <sha>" suffix when
	// non-empty. It is carried on the row so the STATUS cell is composed at render
	// time from live fields instead of a pre-baked string (ADR-0108).
	VerifiedAtSHA string
	Worktree      string

	cursorKey string
	// destKind selects how the destination column is styled; Worktree holds the
	// plain label (branch name, "[managed wt]", or "needs bind").
	destKind dashboardDestKind
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
// row. It holds every binding keyed by scoped key, the live (PID-alive) running
// drains keyed by runtime path, and the recorded drain panes keyed by scoped key.
type dashboardSnapshot struct {
	bindings   map[string]WorktreeBinding
	liveDrains map[string]tasks.RunningDrain
	drainPanes map[string]tasks.DrainPane
}

// newDashboardSnapshot reads the volatile per-build store state once: AllBindings,
// the live running drains (RunningDrains filtered to PID-alive in memory), and the
// recorded drain panes. It is the single point at which a build touches pop.db for
// the overlay.
func newDashboardSnapshot(d *Deps) (*dashboardSnapshot, error) {
	snap := &dashboardSnapshot{
		bindings:   map[string]WorktreeBinding{},
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
		groupRows, err := dashboardRowsFromStatic(d, cfg, snap, delays, now, st)
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
					Name:         c.p.Name,
					ProjectLabel: c.p.ProjectLabel,
					ProjectPath:  c.canon,
					RuntimePath:  c.canon,
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
	name, label := "", ""
	if len(scans) > 0 {
		name = scans[0].Name
		label = scans[0].ProjectLabel
	}
	// SessionName is left unset for the same reason as in dashboardRepoStatics:
	// deriving it forks git and the build path never reads it.
	return &projectScan{
		Name:         name,
		ProjectLabel: label,
		ProjectPath:  canon,
		RuntimePath:  canon,
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
	dashboardTierRunning   = iota // live drain holds the checkout (Picked-up)
	dashboardTierAutoDrain        // auto-drain enabled
	dashboardTierOrphaned         // Worktree binding points at a missing checkout
	dashboardTierRest             // everything else
)

// dashboardSortTier returns a row's membership tier (see the dashboardTier*
// constants). The order of these checks encodes the precedence: a row that is
// both orphaned and auto-drain qualifies for the auto-drain tier first.
func dashboardSortTier(r DashboardRow) int {
	switch {
	case r.LiveDrain:
		return dashboardTierRunning
	case r.AutoDrain:
		return dashboardTierAutoDrain
	case r.Orphaned:
		return dashboardTierOrphaned
	default:
		return dashboardTierRest
	}
}

// Queue surface status bands (ADR-0121). A row's band is keyed on its DISPLAYED
// label, not its raw status: an IN PROGRESS row (a started or live-drained READY
// set) sorts in the IN PROGRESS band even though its raw status is READY. The
// IN PROGRESS and READY bands float running/ready work across projects; every
// other status reads per-project in dashboardBandRest.
const (
	dashboardBandInProgress = iota // displayed label "IN PROGRESS"
	dashboardBandReady             // displayed label "READY"
	dashboardBandRest              // every other displayed status
)

// dashboardStatusBand returns a row's status band, keyed on its displayed label
// so the READY→IN PROGRESS refinement (dashboardStatusLabel) lands in the
// IN PROGRESS band rather than the READY band.
func dashboardStatusBand(r DashboardRow) int {
	switch dashboardStatusLabel(r) {
	case "IN PROGRESS":
		return dashboardBandInProgress
	case string(tasks.StatusReady):
		return dashboardBandReady
	default:
		return dashboardBandRest
	}
}

// dashboardStatusOrder is the explicit intra-project ordering for the
// dashboardBandRest band (ADR-0121): the "needs-you" statuses first, then the
// problem bucket, then the shelved/terminal statuses, then the structural
// defects. MISSING and MALFORMED share the last rank.
func dashboardStatusOrder(s tasks.TaskSetStatus) int {
	switch s {
	case tasks.StatusAwaitingApproval:
		return 0
	case tasks.StatusNeedsVerify:
		return 1
	case tasks.StatusVerifyFailed:
		return 2
	case tasks.StatusFailed:
		return 3
	case tasks.StatusBlocked:
		return 4
	case tasks.StatusDeferred:
		return 5
	case tasks.StatusDone:
		return 6
	case tasks.StatusMissing, tasks.StatusMalformed:
		return 7
	default:
		return 8
	}
}

// queueRowLess is the shared Queue surface comparator (ADR-0121), the single
// source of the total order both `pop queue dashboard` and `pop queue status`
// read. Rows float by membership tier (live-drain → auto-drain → orphaned),
// then fall through to the status scheme: the IN PROGRESS and READY bands read
// cross-project (Project asc, then SetID desc), and every remaining status
// reads per-project (Project asc, then the explicit status order, then SetID
// desc). Bands key on the displayed label, so a started or live-drained READY
// set sorts as IN PROGRESS even though its raw status is READY. The membership
// tiers float above the whole status scheme — an auto-drain BLOCKED set
// outranks a plain IN PROGRESS set — and fall through to the same band/status/
// SetID tiebreak within a tier.
func queueRowLess(a, b DashboardRow) bool {
	if ta, tb := dashboardSortTier(a), dashboardSortTier(b); ta != tb {
		return ta < tb
	}
	ba, bb := dashboardStatusBand(a), dashboardStatusBand(b)
	if ba != bb {
		return ba < bb
	}
	if a.Project != b.Project {
		return a.Project < b.Project
	}
	// The explicit status order breaks ties only within dashboardBandRest; the
	// IN PROGRESS and READY bands are single-status, so they go straight to the
	// SetID tiebreak after project name.
	if ba == dashboardBandRest {
		if ra, rb := dashboardStatusOrder(a.RawStatus), dashboardStatusOrder(b.RawStatus); ra != rb {
			return ra < rb
		}
	}
	return a.SetID > b.SetID
}

// sortDashboardRows applies the shared Queue surface order (queueRowLess) to a
// dashboard build's rows.
func sortDashboardRows(rows []DashboardRow) {
	sort.SliceStable(rows, func(i, j int) bool {
		return queueRowLess(rows[i], rows[j])
	})
}

// dashboardRowsForStatic renders one repo group's rows from a fully resolved
// static plus the current volatile overlay (statuses, locks, daemon state),
// taking the single per-build store snapshot. It is the seam tests use to drive
// dashboardRowsFromStatic with a hand-built static, mirroring what BuildDashboard
// does per group after deriving statics fork-free from markers.
func dashboardRowsForStatic(d *Deps, cfg *config.Config, st dashboardRepoStatic) ([]DashboardRow, error) {
	var delays []time.Duration
	if qcfg, qerr := resolvedQueueConfig(cfg); qerr == nil {
		delays = qcfg.CrashRetryDelays
	}
	snap, err := newDashboardSnapshot(d)
	if err != nil {
		return nil, err
	}
	return dashboardRowsFromStatic(d, cfg, snap, delays, d.now().UTC(), st)
}

// dashboardRowsFromStatic builds a repo group's rows from its static resolution
// plus the current volatile state: task statuses (refresh), runtime locks, and
// daemon-state columns. It forks no git — the static side is marker/config
// derived (ADR-0060) and this overlay is cheap file/store reads.
func dashboardRowsFromStatic(d *Deps, cfg *config.Config, snap *dashboardSnapshot, delays []time.Duration, now time.Time, st dashboardRepoStatic) ([]DashboardRow, error) {
	refresh, err := d.refresh(st.defPath)
	if err != nil {
		return nil, err
	}
	if cfg == nil && d.LoadConfig != nil {
		cfg, _ = d.LoadConfig(config.DefaultConfigPath())
	}
	tasks.ApplyVerifyVerdictsWith(d.Tasks, refresh, cfg, func(setID string) string {
		return binding.RuntimeForSet(snap.bindings, st.repoKey, setID, staticProjectPath(st))
	})
	intents := dashboardWorktreeIntents(d, st.defPath)
	backoff := d.setBackoffLookup(st.repoCommonDir, delays, now)
	var rows []DashboardRow
	for _, taskRow := range refresh.Rows {
		// Done sets are hidden uniformly unless Done inclusion is on (ADR-0121):
		// the old carve-out that kept a DONE set visible while it still held a
		// managed (pop-provisioned) Worktree binding is retired — teardown stays
		// gated at Archive. DoneStillManagedBound is still recorded on the row so a
		// revealed (`--include-done`) DONE row can be labelled/styled from the fact.
		bnd, hasBinding := snap.bindingFor(st.repoKey, taskRow.ID)
		bound := hasBinding && strings.TrimSpace(bnd.RuntimePath) != ""
		doneStillManagedBound := taskRow.Status == tasks.StatusDone && bound && bnd.Provisioned
		orphaned := dashboardOrphaned(d, bnd, hasBinding)
		if !dashboardShowRow(taskRow, d.IncludeDone) {
			continue
		}
		wt := dashboardWorktree(d, snap, intents, st.repoKey, taskRow.ID, taskRow.Status, bnd, bound)
		parked := false
		if backoff != nil {
			parked, _ = backoff(taskRow.ID)
		}
		liveDrain := dashboardLiveDrain(snap, st.repoKey, taskRow.ID, wt.runtimePath)
		// A live drain lights the trailing ● indicator (ADR-0111); parked and
		// config-error ride the STATUS cell as ` · parked` / ` · config error: <msg>`
		// suffixes. The mutual exclusion the retired single-string DRAIN cell
		// enforced is preserved by gating the config-error probe on a set that is
		// neither live-drained nor parked.
		configErr := ""
		if !liveDrain && !parked {
			if st.configErr != "" && !hasBinding {
				// Bare repo with no declared trunk: an unbound set has no integration
				// target to route to (ADR-0060). Surface the config-class error derived
				// fork-free during static resolution, never a git probe. A bound set is
				// still drainable via its binding, so it is left untouched.
				configErr = st.configErr
			} else if taskRow.Status == tasks.StatusReady {
				// An unsatisfiable worktree directive is a static config defect, not a
				// runtime crash (ADR-0059): show it on the set so the operator fixes the
				// environment. Read the registration intent first (a store read, no git);
				// only a set that actually carries a directive pays the read-only probe
				// (which forks git to resolve the trunk/worktree), so the no-directive
				// common path — and the dashboard's cached rebuild — forks no git.
				if intent, _ := tasks.RegisteredWorktreeIntent(d.Tasks, st.defPath, taskRow.ID); intent != nil {
					if msg := d.probeDirective(staticProjectPath(st), taskRow.ID); msg != "" {
						configErr = msg
					}
				}
			}
		}
		rows = append(rows, DashboardRow{
			SetRef: SetRef{
				SetID:                 taskRow.ID,
				RawStatus:             taskRow.Status,
				AutoDrain:             taskRow.AutoDrain,
				DefPath:               st.defPath,
				StatePath:             st.statePath,
				RepoKey:               st.repoKey,
				RepoCommonDir:         st.repoCommonDir,
				ProjectPath:           staticProjectPath(st),
				ProjectName:           st.projectName,
				RuntimePath:           wt.runtimePath,
				DoneStillManagedBound: doneStillManagedBound,
				Parked:                parked,
				ConfigError:           configErr,
				Orphaned:              orphaned,
				Bound:                 bound,
				PaneID:                dashboardPaneID(snap, st.repoKey, taskRow.ID),
				LiveDrain:             liveDrain,
			},
			Project:       st.projectName,
			Started:       taskRow.Started,
			VerifiedAtSHA: taskRow.VerifiedAtSHA,
			Worktree:      wt.label,
			cursorKey:     st.projectName + "\x00" + taskRow.ID,
			destKind:      wt.destKind,
		})
	}
	return rows, nil
}

// dashboardShowRow is the shared Done-inclusion row filter (ADR-0121). Every
// non-DONE set always shows; a DONE set shows only when Done inclusion is on.
// The filter is uniform — a DONE set holding a managed Worktree binding is no
// longer carved out (teardown stays gated at Archive).
func dashboardShowRow(row tasks.Row, includeDone bool) bool {
	return includeDone || row.Status != tasks.StatusDone
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

// dashboardStatusLabel reproduces tasks.StatusLabel from a dashboard row's live
// fields, extending the READY refinement with the live-drain trigger (ADR-0111):
// a READY set shows "IN PROGRESS" when it is started (≥1 done) OR held by a live
// drain; every other row shows its raw status. The refinement is READY-only — a
// live drain coinciding with a non-READY status leaves that label untouched
// (needs-you outranks liveness). It reads RawStatus/Started/LiveDrain so the
// label is recomposed on each render pass rather than baked in at row-build time.
func dashboardStatusLabel(row DashboardRow) string {
	if row.RawStatus == tasks.StatusReady && (row.Started || row.LiveDrain) {
		return "IN PROGRESS"
	}
	return tasks.StatusLabel(tasks.Row{Status: row.RawStatus, Started: row.Started})
}

// dashboardStatusCell composes a row's STATUS cell from its live fields — the
// single source of truth every render path and the header count read (ADR-0108).
// It returns the plain, un-styled text: the display label followed by the
// verified-at, auto-drain and orphaned suffixes in that fixed order. Column
// width-fitting measures this plain form, so no ANSI leaks into column math;
// dashboardStatusCellStyled layers styling for the rendered output. Because the
// cell is derived from the row on each View pass, any action that mutates a row
// field (the auto-drain toggle, a drain kick) updates the cell and the header
// count together on the same render.
func dashboardStatusCell(row DashboardRow) string {
	return dashboardComposeStatus(row, false)
}

// dashboardStatusCellStyled is dashboardStatusCell with the immunized
// "verified @ <sha>" token rendered yellow for display (same ANSI as pop tasks
// status Details). The styling is layered only here so width measurement stays
// ANSI-free.
func dashboardStatusCellStyled(row DashboardRow) string {
	return dashboardComposeStatus(row, true)
}

// dashboardComposeStatus assembles the STATUS cell from live row fields. When
// styled, the verified-at token carries ANSI yellow; the auto-drain, orphaned,
// parked, and config-error suffixes are always plain text.
func dashboardComposeStatus(row DashboardRow, styled bool) string {
	label := dashboardStatusLabel(row)
	if styled {
		if st, ok := dashboardStatusBucketStyle[label]; ok {
			label = st.Render(label)
		}
	}
	if row.VerifiedAtSHA != "" {
		verified := "verified @ " + row.VerifiedAtSHA
		if styled {
			verified = dashboardVerifiedAtStyle.Render(verified)
		}
		label += " · " + verified
	}
	if dashboardAutoDrainWaiting(row) {
		label += " · auto-drain"
	}
	if row.Orphaned {
		label += " · orphaned"
	}
	// Parked and config-error relocated off the DRAIN string onto the STATUS cell
	// (ADR-0111). Both are uncoloured plain text, so they never leak ANSI into the
	// width-measured (unstyled) form; they trail the auto-drain/orphaned suffixes
	// in a fixed order.
	if row.Parked {
		label += " · parked"
	}
	if row.ConfigError != "" {
		label += " · config error: " + row.ConfigError
	}
	return label
}

// dashboardAutoDrainWaiting reports whether a set's auto-drain consent should
// surface as "waiting to be picked up" — the single predicate the per-row
// marker and the header tally both read (ADR-0108). A consented set counts and
// shows the marker only while it is not Picked-up; once a live drain holds the
// checkout (row.LiveDrain) the IN-PROGRESS refinement already signals the
// activity, so the marker is silenced and the set drops out of the "still needs
// picking up" count. The persisted consent bit is untouched — this is
// display-only.
func dashboardAutoDrainWaiting(row DashboardRow) bool {
	return row.AutoDrain && !row.LiveDrain
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

// dashboardLiveDrainGlyph is the trailing live-drain indicator: a single ● shown
// on any row whose runtime lock is PID-alive, regardless of STATUS (ADR-0111).
// It is the cue that `p` (preview the working pane) can reach a pane.
const dashboardLiveDrainGlyph = "●"

// dashboardLiveDrainStyle colors the live-drain indicator in the house working
// colour — the same red (ui.ColorWorking, 256-color 196) the Monitor dashboard
// paints active panes. Working shares the "hot" red with unread/attention;
// motion (Monitor's spinner) not hue separates them.
var dashboardLiveDrainStyle = lipgloss.NewStyle().Foreground(ui.ColorWorking)

// dashboardLiveIndicator returns the trailing indicator cell: the ● glyph when a
// live drain holds the checkout, blank otherwise. When styled the glyph carries
// the house working colour; the plain form feeds width measurement so no ANSI
// reaches column math.
func dashboardLiveIndicator(row DashboardRow, styled bool) string {
	if !row.LiveDrain {
		return ""
	}
	if styled {
		return dashboardLiveDrainStyle.Render(dashboardLiveDrainGlyph)
	}
	return dashboardLiveDrainGlyph
}

// dashboardVerifiedAtStyle colors the immunized "verified @ <shortSHA>" suffix
// (same ANSI yellow as pop tasks status Details output).
var dashboardVerifiedAtStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("3"))

// dashboardStatusBucketStyle maps a base status label to its semantic bucket
// color. Only the base label token is colored here; the verified@/auto-drain/
// orphaned suffixes keep their own styling, so this is applied to the label
// before suffixes are appended in dashboardComposeStatus. The map is keyed by
// the display label, so "IN PROGRESS" (the started-READY refinement) shares
// READY's blue bucket.
//
// Bucket rationale (from the grilling session):
//   - green  DONE — terminal success.
//   - blue   READY / IN PROGRESS — in-flight work, nothing wrong.
//   - yellow NEEDS-VERIFY / AWAITING-APPROVAL / BLOCKED — "needs-you": each
//     waits on a human decision. BLOCKED is a needs-you gate, not a failure,
//     so it sits with the amber attention bucket rather than red.
//   - red    FAILED / VERIFY-FAILED / MALFORMED / MISSING — the problem bucket;
//     MALFORMED (bad task file) and MISSING (no manifest) fold in here as
//     structural problems alongside outright failures.
//   - faint  DEFERRED — intentionally shelved, dimmed to recede.
//
// The mapping is trivially reversible, so no ADR backs it.
var dashboardStatusBucketStyle = map[string]lipgloss.Style{
	string(tasks.StatusDone):             lipgloss.NewStyle().Foreground(lipgloss.Color("2")),
	string(tasks.StatusReady):            lipgloss.NewStyle().Foreground(lipgloss.Color("4")),
	"IN PROGRESS":                        lipgloss.NewStyle().Foreground(lipgloss.Color("4")),
	string(tasks.StatusNeedsVerify):      lipgloss.NewStyle().Foreground(lipgloss.Color("3")),
	string(tasks.StatusAwaitingApproval): lipgloss.NewStyle().Foreground(lipgloss.Color("3")),
	string(tasks.StatusBlocked):          lipgloss.NewStyle().Foreground(lipgloss.Color("3")),
	string(tasks.StatusFailed):           lipgloss.NewStyle().Foreground(lipgloss.Color("1")),
	string(tasks.StatusVerifyFailed):     lipgloss.NewStyle().Foreground(lipgloss.Color("1")),
	string(tasks.StatusMalformed):        lipgloss.NewStyle().Foreground(lipgloss.Color("1")),
	string(tasks.StatusMissing):          lipgloss.NewStyle().Foreground(lipgloss.Color("1")),
	string(tasks.StatusDeferred):         lipgloss.NewStyle().Faint(true),
}

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

// dashboardLiveDrain reports whether a live (PID-alive) drain holds the set's
// checkout, reading the per-build snapshot's live-drain map (one RunningDrains
// read plus in-memory PID-liveness) instead of reopening the runtime lock per
// row. The snapshot keys live drains by runtime path; a drain at the set's
// checkout (or its bound checkout) whose SetID matches is the same fact
// ReadRuntimeLockStatus derived per row before. It is the structured boolean
// the sort tier, header count, auto-drain silencing, and IN-PROGRESS refinement
// all key on (ADR-0111).
func dashboardLiveDrain(snap *dashboardSnapshot, repoKey, setID, runtimePath string) bool {
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

func dashboardPaneID(snap *dashboardSnapshot, repoKey, setID string) string {
	if snap == nil || snap.drainPanes == nil {
		return ""
	}
	if pane, ok := snap.drainPanes[setScopedKey(repoKey, setID)]; ok {
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
	row   DashboardRow
	stage dashboardBindStage
	// list drives the worktree-pick and base-ref-pick stages (both wrapping).
	// Base refs are held as entries with only Label set. The name stage is a
	// plain text input and does not use the list.
	list    *ui.List[dashboardBindEntry]
	baseRef string
	name    string
	loading bool
}

// bindEntryCell renders one bind-modal row: the worktree label (falling back to
// the checkout path) or, in the base-ref stage, the ref held in Label.
func bindEntryCell(e dashboardBindEntry, _ ui.RowState) string {
	if e.Label != "" {
		return e.Label
	}
	return e.Path
}

// newBindEntryList builds the wrapping list backing a bind-modal list stage.
func newBindEntryList(entries []dashboardBindEntry) *ui.List[dashboardBindEntry] {
	return ui.NewList(entries, ui.Opts[dashboardBindEntry]{
		Wrap: true,
		Cell: bindEntryCell,
	})
}

// bindRefEntries wraps base refs as bind entries so the base-ref stage reuses
// the same wrapping list as the worktree-pick stage.
func bindRefEntries(refs []string) []dashboardBindEntry {
	entries := make([]dashboardBindEntry, len(refs))
	for i, ref := range refs {
		entries[i] = dashboardBindEntry{Label: ref}
	}
	return entries
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
	list    *ui.List[dashboardDrainEntry]
	loading bool
}

// newDashboardDrainModal builds the Drain target picker with a wrapping list,
// positioning the cursor on "new managed worktree" — the frictionless default.
func newDashboardDrainModal(row DashboardRow, entries []dashboardDrainEntry) *dashboardDrainModal {
	list := ui.NewList(entries, ui.Opts[dashboardDrainEntry]{
		Wrap: true,
		Cell: func(e dashboardDrainEntry, _ ui.RowState) string {
			if e.Label != "" {
				return e.Label
			}
			return e.Path
		},
	})
	list.SetCursor(defaultDrainCursor(entries))
	return &dashboardDrainModal{row: row, list: list}
}

type dashboardAbandonModal struct {
	row     DashboardRow
	loading bool
}

// dashboardMenuAction identifies the verb a menu item dispatches.
type dashboardMenuAction int

const (
	menuActionDrain dashboardMenuAction = iota
	menuActionVerify
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
// applicable to that row on a ui.List whose cursor drives j/k + Enter selection.
type dashboardMenu struct {
	row  DashboardRow
	list *ui.List[dashboardMenuItem]
}

// dashboardMenuItems returns the verbs applicable to row, in a stable order.
// Conditional verbs are filtered to the row's context: verify only for
// NEEDS-VERIFY / VERIFY-FAILED rows with no live drain, unbind only for bound
// rows, auto-drain only for non-orphaned rows, and unpark only for parked rows.
// Drain, bind, preview, the runtime shell, and archive apply to every row
// regardless of status.
func dashboardMenuItems(row DashboardRow) []dashboardMenuItem {
	items := []dashboardMenuItem{
		{key: "i", label: "drain", action: menuActionDrain},
	}
	// Verify is the lighter, explicit Verifier force (ADR-0123): offered only on
	// rows a verdict can move (NEEDS-VERIFY / VERIFY-FAILED) and hidden while a
	// live drain holds the set — a plain verify is not quiescence-gated, so the
	// running drain verifies itself instead.
	if dashboardVerifyEligible(row) {
		items = append(items, dashboardMenuItem{key: "v", label: "verify", action: menuActionVerify})
	}
	items = append(items, dashboardMenuItem{key: "b", label: "bind worktree", action: menuActionBind})
	if row.Bound {
		items = append(items, dashboardMenuItem{key: "U", label: "unbind worktree", action: menuActionUnbind})
	}
	if !row.Orphaned {
		items = append(items, dashboardMenuItem{key: "a", label: "auto-drain", action: menuActionAutoDrain})
	}
	items = append(items, dashboardMenuItem{key: "p", label: "preview", action: menuActionPreview})
	if row.Parked {
		items = append(items, dashboardMenuItem{key: "P", label: "unpark", action: menuActionUnpark})
	}
	items = append(items, dashboardMenuItem{key: "O", label: "shell", action: menuActionShell})
	items = append(items, dashboardMenuItem{key: "A", label: "archive", action: menuActionArchive})
	return items
}

// dashboardVerifyEligible reports whether the verify verb applies to row: a set
// a verdict can still move (NEEDS-VERIFY or VERIFY-FAILED) that no live drain
// holds (ADR-0123). It is the single guard shared by the menu (inclusion) and
// dispatch (self-containment).
func dashboardVerifyEligible(row DashboardRow) bool {
	if row.LiveDrain {
		return false
	}
	return row.RawStatus == tasks.StatusNeedsVerify || row.RawStatus == tasks.StatusVerifyFailed
}

// newDashboardMenu opens the action overlay on row, wrapping its verbs in a
// ui.List with j/k wrap-around navigation.
func newDashboardMenu(row DashboardRow) *dashboardMenu {
	return &dashboardMenu{
		row:  row,
		list: ui.NewList(dashboardMenuItems(row), ui.Opts[dashboardMenuItem]{Wrap: true}),
	}
}

// dashboardFilterToggle identifies one row-inclusion view filter the filter
// menu flips. Today the menu carries a single toggle (Show done, wired to the
// ADR-0121 Done-inclusion flag); the enum and the item list are the extension
// point for future inclusion filters (by status, by project).
type dashboardFilterToggle int

const (
	filterToggleShowDone dashboardFilterToggle = iota
)

// dashboardFilterItem is one toggle in the filter menu: the flat shortcut letter
// it keeps, the label shown beside its checkbox, and the view filter it flips.
type dashboardFilterItem struct {
	key    string
	label  string
	toggle dashboardFilterToggle
}

// dashboardFilterMenu is the modal opened with `f` over the Queue dashboard. It
// is a sibling of the `a` action menu but holds row-inclusion toggles rather
// than row verbs, so it is not anchored to the cursored row. The toggle state
// lives on the model (m.d.IncludeDone), not the menu — the menu only renders it
// and dispatches flips — so the checkbox reflects the live view every frame.
type dashboardFilterMenu struct {
	list *ui.List[dashboardFilterItem]
}

// dashboardFilterItems returns the inclusion toggles, in a stable order. New
// inclusion filters append here.
func dashboardFilterItems() []dashboardFilterItem {
	return []dashboardFilterItem{
		{key: "d", label: "show done", toggle: filterToggleShowDone},
	}
}

// newDashboardFilterMenu opens the filter modal with j/k wrap-around navigation.
func newDashboardFilterMenu() *dashboardFilterMenu {
	return &dashboardFilterMenu{
		list: ui.NewList(dashboardFilterItems(), ui.Opts[dashboardFilterItem]{Wrap: true}),
	}
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
// task's status on a ui.List whose cursor drives j/k + Enter selection. inPeek
// marks which view it was opened from so the renderer can place it correctly.
type taskMenu struct {
	task   tasks.Task
	list   *ui.List[taskMenuItem]
	inPeek bool
}

// taskMenuItems returns the task verbs applicable to task, filtered to its
// status: Complete for open/failed/skipped (anything not already done), Open for
// any non-open task (done/failed/skipped, mirroring CanReopen), and Skip for
// open. A done task yields Open.
func taskMenuItems(task tasks.Task) []taskMenuItem {
	var items []taskMenuItem
	if task.Status != tasks.TaskDone {
		items = append(items, taskMenuItem{key: "C", label: "complete"})
	}
	if tasks.CanReopen(task.Status) {
		items = append(items, taskMenuItem{key: "O", label: "open"})
	}
	if task.Status == tasks.TaskOpen {
		items = append(items, taskMenuItem{key: "K", label: "skip"})
	}
	return items
}

// newTaskMenu wraps pre-filtered task verbs in a ui.List with j/k wrap-around
// navigation. inPeek records which detail view opened it for placement.
func newTaskMenu(task tasks.Task, items []taskMenuItem, inPeek bool) *taskMenu {
	return &taskMenu{
		task:   task,
		list:   ui.NewList(items, ui.Opts[taskMenuItem]{Wrap: true}),
		inPeek: inPeek,
	}
}

// detailView is the full-screen task-set detail that replaces the table. Its
// task list is a ui.List keyed by task ID, so the cursor survives a manifest
// refresh — ReplaceItems re-anchors it by key (ADR-0079).
type detailView struct {
	row      DashboardRow
	manifest *tasks.Manifest
	taskRow  *tasks.Row
	list     *ui.List[tasks.Task]
	cols     *detailColumns
	loading  bool
	err      error
	peek     *taskTextPeek
	// statusMsg is a transient one-line message shown above the hint bar.
	// Set to a hint on invalid transition; set to confirmation on success.
	statusMsg string
}

// detailColumns holds the detail task list's ID-column width, precomputed over the
// manifest tasks (the status/type/title columns are fixed). The List's Cell closure
// closes over a pointer to it so a manifest refresh updates the width in place,
// matching the house pattern (dashboardColumns / pickerCell).
type detailColumns struct {
	idW int
}

// Detail task-table column widths. Status, type, and title are fixed; the ID
// column grows to the widest task ID (floored at the "ID" header).
const (
	detailStatusW = 10
	detailTypeW   = 4
	detailTitleW  = 40
)

// detailTableChromeLines is the number of body lines above the detail List rows:
// the blank line under the header, the column header, and the separator.
const detailTableChromeLines = 3

// newDetailView builds a loading detail view for row with an empty task List keyed
// by task ID. The manifest arrives via syncManifest once loaded.
func newDetailView(row DashboardRow) *detailView {
	cols := &detailColumns{idW: len("ID")}
	d := &detailView{row: row, loading: true, cols: cols}
	d.list = ui.NewList([]tasks.Task{}, ui.Opts[tasks.Task]{
		Key:    func(t tasks.Task) string { return t.ID },
		Anchor: ui.AnchorTop,
		Cell: func(t tasks.Task, _ ui.RowState) string {
			return detailTaskLine(t, cols.idW)
		},
	})
	return d
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

// syncManifest updates the manifest on load or a tick refresh, feeding the tasks
// to the List (which re-anchors the cursor by task ID) and recomputing the ID
// column width. It clears the loading/error state, since a synced manifest is a
// completed load.
func (d *detailView) syncManifest(m *tasks.Manifest, row *tasks.Row) {
	d.manifest = m
	d.taskRow = row
	d.loading = false
	d.err = nil
	var items []tasks.Task
	if m != nil {
		items = m.Tasks
	}
	d.cols.idW = detailIDWidth(items)
	d.list.ReplaceItems(items)
}

// dashboardColumns holds the task-set table's natural column widths (derived from
// content) and the fitted widths clamped to the terminal budget. The List's Cell
// closure closes over a pointer to it so a reload or filter can update widths in
// place without rebuilding the list — matching the house pattern of pickerCell
// closing over its picker.
type dashboardColumns struct {
	natural []int
	widths  []int
	width   int
}

type QueueDashboard struct {
	d         *Deps
	cfg       *config.Config
	snap      DashboardSnapshot
	allRows   []DashboardRow // source of truth; snap.Rows is the filtered view
	list      *ui.List[DashboardRow]
	cols      *dashboardColumns
	err       error
	width     int
	height    int
	bind      *dashboardBindModal
	drainPick *dashboardDrainModal
	abandon   *dashboardAbandonModal
	detail    *detailView
	menu      *dashboardMenu
	taskMenu  *taskMenu
	filter    *dashboardFilterMenu

	filterMode  bool
	filterInput ui.TextField
	pendingG    bool
	statusMsg   string
	showHelp    bool
	// openCheckout is the bound checkout path chosen with Ctrl-g on the main
	// list. It is set alongside a tea.Quit so RunDashboard can surface it out of
	// the program and the command layer runs the workbench-aware open after the
	// TUI exits (task 02).
	openCheckout string
}

// TestDashboardRow builds a minimal dashboard row for tests outside the queue
// package. The cursor key mirrors production derivation from project and set ID.
func TestDashboardRow(project, setID string, ref SetRef) DashboardRow {
	if ref.SetID == "" {
		ref.SetID = setID
	}
	return DashboardRow{
		Project:   project,
		cursorKey: project + "\x00" + ref.SetID,
		SetRef:    ref,
	}
}

// NewDashboard constructs a Queue dashboard model from a snapshot.
func NewDashboard(d *Deps, cfg *config.Config, snap DashboardSnapshot) QueueDashboard {
	return newQueueDashboard(d, cfg, snap)
}

func newQueueDashboard(d *Deps, cfg *config.Config, snap DashboardSnapshot) QueueDashboard {
	if d == nil {
		d = DefaultDeps()
	}
	cols := &dashboardColumns{}
	cols.syncNatural(snap.Rows)
	var list *ui.List[DashboardRow]
	list = ui.NewList(snap.Rows, ui.Opts[DashboardRow]{
		Key:    func(r DashboardRow) string { return r.cursorKey },
		Anchor: ui.AnchorTop,
		Cell: func(r DashboardRow, rs ui.RowState) string {
			budget := dashboardListCellBudget(cols.width)
			if list.LinesPerItem() == 2 {
				line1Widths := dashboardTwoLineFitWidths(dashboardTwoLineNaturalWidths(list.Items()), budget)
				if rs.LineIndex == 1 {
					return ui.TruncateString(dashboardTwoLineRowLine2(r, line1Widths), budget)
				}
				return ui.TruncateString(dashboardTwoLineRowLine1(r, line1Widths), budget)
			}
			return ui.TruncateString(dashboardTableLine(dashboardRowValues(r), cols.widths), budget)
		},
	})
	return QueueDashboard{d: d, cfg: cfg, snap: snap, allRows: snap.Rows, list: list, cols: cols}
}

const (
	dashboardColProject = iota
	dashboardColSetID
	dashboardColStatus
	dashboardColWorktree
	dashboardColIndicator
)

const dashboardColSep = 2

// dashboardColShrinkOrder lists elastic columns in shrink priority: WORKTREE
// gives way first. The trailing live-drain indicator is fixed-width and absent
// here, so narrow-pane fitting never drops it (ADR-0111).
var dashboardColShrinkOrder = []int{
	dashboardColWorktree,
	dashboardColStatus,
	dashboardColSetID,
	dashboardColProject,
}

// dashboardTableHeaders is the fixed column header row. The trailing column is
// the live-drain indicator: an empty header over a ● / blank cell, so no label
// sits above the glyph.
func dashboardTableHeaders() []string {
	return []string{"PROJECT", "TASK SET", "STATUS", "WORKTREE", ""}
}

// dashboardColumnWidths precomputes each column's natural width over the full row
// set, floored at the header label width.
func dashboardColumnWidths(rows []DashboardRow) []int {
	headers := dashboardTableHeaders()
	widths := make([]int, len(headers))
	for i, h := range headers {
		widths[i] = len(h)
	}
	for _, row := range rows {
		for i, v := range dashboardRowNaturalValues(row) {
			if n := lipgloss.Width(v); n > widths[i] {
				widths[i] = n
			}
		}
	}
	return widths
}

func dashboardTableLineWidth(widths []int) int {
	if len(widths) == 0 {
		return 0
	}
	total := 0
	for _, w := range widths {
		total += w
	}
	return total + dashboardColSep*(len(widths)-1)
}

// dashboardFitColumnWidths shrinks elastic columns until the table fits budget.
// When budget is still exceeded after shrinking, cells are truncated at render
// time via padDashboardCell.
func dashboardFitColumnWidths(natural []int, budget int) []int {
	if budget <= 0 || len(natural) == 0 {
		return append([]int(nil), natural...)
	}
	widths := append([]int(nil), natural...)
	headers := dashboardTableHeaders()
	mins := make([]int, len(headers))
	for i, h := range headers {
		mins[i] = len(h)
	}
	for dashboardTableLineWidth(widths) > budget {
		shrunk := false
		for _, col := range dashboardColShrinkOrder {
			if widths[col] > mins[col] {
				widths[col]--
				shrunk = true
				break
			}
		}
		if !shrunk {
			break
		}
	}
	return widths
}

// dashboardListCellBudget is the visible width available to a List row's table
// cells after the List's 2-char cursor / pad prefix.
func dashboardListCellBudget(termWidth int) int {
	if termWidth > 2 {
		return termWidth - 2
	}
	return termWidth
}

// dashboardTableBodyBudget is the visible width available to a table line that
// carries a 2-char body indent ("  " prefix before the cells).
func dashboardTableBodyBudget(termWidth int) int {
	if termWidth > 2 {
		return termWidth - 2
	}
	return termWidth
}

func (c *dashboardColumns) syncNatural(rows []DashboardRow) {
	c.natural = dashboardColumnWidths(rows)
	c.refit()
}

func (c *dashboardColumns) refit() {
	c.widths = dashboardFitColumnWidths(c.natural, dashboardListCellBudget(c.width))
}

func dashboardTableWidthsForRows(rows []DashboardRow, termWidth int) []int {
	return dashboardFitColumnWidths(dashboardColumnWidths(rows), dashboardTableBodyBudget(termWidth))
}

const (
	dashboardTwoLineWidthThreshold = 120
	dashboardTwoLineSetIDThreshold = 36
	// dashboardTwoLineHeightFloor is the pane-height floor below which the table
	// stays single-line regardless of width or set-id length (ADR-0107). In a
	// short tmux popup, visible-row density beats id completeness.
	dashboardTwoLineHeightFloor = 16
)

// dashboardTwoLineMode reports whether the Queue dashboard should render each
// row on two lines. Two-line mode is height-gated (ADR-0107): it engages only
// when the pane is roomy (termHeight >= dashboardTwoLineHeightFloor). When
// roomy, it activates if the terminal is narrow (< 120 columns) or any visible
// Task set identifier is long (> 36 characters). Below the height floor every
// row stays single-line. When active, every row uses the same two-line shape
// (uniform height).
func dashboardTwoLineMode(rows []DashboardRow, termWidth, termHeight int) bool {
	if termHeight < dashboardTwoLineHeightFloor {
		return false
	}
	if termWidth < dashboardTwoLineWidthThreshold {
		return true
	}
	for _, row := range rows {
		if len(row.SetID) > dashboardTwoLineSetIDThreshold {
			return true
		}
	}
	return false
}

// dashboardTwoLineHeaders returns the line-1 column headers for two-line mode:
// PROJECT, TASK SET, WORKTREE, and the trailing live-drain indicator (empty
// header). STATUS is rendered on line 2, indented to sit under the TASK SET
// column (see dashboardTwoLineStatusHeader).
func dashboardTwoLineHeaders() []string {
	return []string{"PROJECT", "TASK SET", "WORKTREE", ""}
}

// Line-1 column indices for two-line mode.
const (
	dashboardTwoLineColProject = iota
	dashboardTwoLineColSetID
	dashboardTwoLineColWorktree
	dashboardTwoLineColIndicator
)

// dashboardTwoLineStatusIndent is the leading padding for the line-2 STATUS cell
// so it aligns under the TASK SET column, past the PROJECT column and its
// separator.
func dashboardTwoLineStatusIndent(line1Widths []int) int {
	if len(line1Widths) <= dashboardTwoLineColProject {
		return 0
	}
	return line1Widths[dashboardTwoLineColProject] + dashboardColSep
}

// dashboardTwoLineStatusHeader renders the line-2 header: STATUS indented under
// the TASK SET column.
func dashboardTwoLineStatusHeader(line1Widths []int) string {
	return strings.Repeat(" ", dashboardTwoLineStatusIndent(line1Widths)) + "STATUS"
}

// dashboardTwoLineRowValuesLine1 returns the cell values for line 1 of a two-line
// row: PROJECT, TASK SET (the set id), WORKTREE, and the trailing live-drain
// indicator.
func dashboardTwoLineRowValuesLine1(row DashboardRow) []string {
	return []string{
		row.Project,
		row.SetID,
		renderDashboardDest(row.destKind, row.Worktree),
		dashboardLiveIndicator(row, true),
	}
}

// dashboardTwoLineNaturalWidths returns the natural widths of the line-1 columns
// in two-line mode, floored at the header label width.
func dashboardTwoLineNaturalWidths(rows []DashboardRow) []int {
	headers := dashboardTwoLineHeaders()
	widths := make([]int, len(headers))
	for i, h := range headers {
		widths[i] = len(h)
	}
	for _, row := range rows {
		for i, v := range dashboardTwoLineRowValuesLine1(row) {
			if n := lipgloss.Width(v); n > widths[i] {
				widths[i] = n
			}
		}
	}
	return widths
}

// dashboardTwoLineTableLineWidth returns the rendered width of a line-1 row or
// header given the two-line column widths.
func dashboardTwoLineTableLineWidth(widths []int) int {
	if len(widths) == 0 {
		return 0
	}
	total := 0
	for _, w := range widths {
		total += w
	}
	return total + dashboardColSep*(len(widths)-1)
}

// dashboardTwoLineColShrinkOrder lists elastic line-1 columns in shrink
// priority: WORKTREE gives way first, then PROJECT, so the TASK SET set id keeps
// as much width as possible and only truncates as a last resort. The trailing
// live-drain indicator is fixed-width and absent here, so it is never dropped.
var dashboardTwoLineColShrinkOrder = []int{
	dashboardTwoLineColWorktree,
	dashboardTwoLineColProject,
	dashboardTwoLineColSetID,
}

// dashboardTwoLineFitWidths shrinks the line-1 columns until the row fits budget.
func dashboardTwoLineFitWidths(natural []int, budget int) []int {
	if budget <= 0 || len(natural) == 0 {
		return append([]int(nil), natural...)
	}
	widths := append([]int(nil), natural...)
	headers := dashboardTwoLineHeaders()
	mins := make([]int, len(headers))
	for i, h := range headers {
		mins[i] = len(h)
	}
	for dashboardTwoLineTableLineWidth(widths) > budget {
		shrunk := false
		for _, col := range dashboardTwoLineColShrinkOrder {
			if widths[col] > mins[col] {
				widths[col]--
				shrunk = true
				break
			}
		}
		if !shrunk {
			break
		}
	}
	return widths
}

// dashboardTwoLineTableHeader renders the two-line mode line-1 header.
func dashboardTwoLineTableHeader(widths []int) string {
	return dashboardTableLine(dashboardTwoLineHeaders(), widths)
}

// dashboardTwoLineTableSeparator renders the two-line mode line-1 separator.
func dashboardTwoLineTableSeparator(widths []int) string {
	return dashboardTableSeparator(widths)
}

// dashboardTwoLineRowLine1 renders the padded line-1 cells of a two-line row.
func dashboardTwoLineRowLine1(row DashboardRow, widths []int) string {
	return dashboardTableLine(dashboardTwoLineRowValuesLine1(row), widths)
}

// dashboardTwoLineRowLine2 renders line 2 of a two-line row: the STATUS value,
// indented to sit under the TASK SET column on line 1. The List (and the bespoke
// overlay path) supply the two-space gutter on top of this indent.
func dashboardTwoLineRowLine2(row DashboardRow, line1Widths []int) string {
	return strings.Repeat(" ", dashboardTwoLineStatusIndent(line1Widths)) + dashboardStatusCellStyled(row)
}

// dashboardTableChromeLines is the number of body lines above the List rows in
// single-line mode: the blank line under the summary header, the column header,
// and the separator.
const dashboardTableChromeLines = 3

// dashboardTwoLineChromeLines is the chrome height in two-line mode: the blank
// line, the line-1 (PROJECT/TASK SET/WORKTREE) header, the line-2 (STATUS)
// header, and the separator.
const dashboardTwoLineChromeLines = dashboardTableChromeLines + 1

// dashboardChromeLines returns the chrome height above the List rows for the
// current render mode.
func (m QueueDashboard) dashboardChromeLines() int {
	if dashboardTwoLineMode(m.snap.Rows, m.width, m.height) {
		return dashboardTwoLineChromeLines
	}
	return dashboardTableChromeLines
}

// syncListRows feeds the current filtered rows to the List (re-anchoring the
// cursor by cursorKey) and recomputes the column widths over them.
func (m QueueDashboard) syncListRows() {
	m.list.ReplaceItems(m.snap.Rows)
	m.cols.syncNatural(m.snap.Rows)
}

// resizeMainList sizes the List to the body budget the Frame leaves, minus the
// table's own header chrome, so the table clamps to the terminal instead of
// overflowing. In two-line mode each List item renders two terminal lines, so
// the List's LinesPerItem is set to 2 and the physical body budget is unchanged.
func (m QueueDashboard) resizeMainList() {
	listH := m.frameSpec().BodyHeight(m.height) - m.dashboardChromeLines()
	if listH < 1 {
		listH = 1
	}
	if dashboardTwoLineMode(m.snap.Rows, m.width, m.height) {
		m.list.SetLinesPerItem(2)
	} else {
		m.list.SetLinesPerItem(1)
	}
	m.list.Resize(listH)
}

// ViewToggleAllowed reports whether v may switch to the Routine dashboard.
func (m QueueDashboard) ViewToggleAllowed() bool {
	return m.bind == nil && m.drainPick == nil && m.abandon == nil &&
		m.detail == nil && m.menu == nil && m.taskMenu == nil && m.filter == nil
}

// OpenCheckout returns the checkout path chosen with Ctrl-g before quit.
func (m QueueDashboard) OpenCheckout() string {
	return m.openCheckout
}

// ListCursor exposes the main-list cursor index for tests.
func (m QueueDashboard) ListCursor() int {
	return m.list.Cursor()
}

// FilterActive reports whether the main-list filter is engaged.
func (m QueueDashboard) FilterActive() bool {
	return m.filterMode
}

func (m QueueDashboard) Init() tea.Cmd {
	return dashboardTick()
}

func (m QueueDashboard) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		if kpm, ok := msg.(tea.KeyPressMsg); ok {
			if ui.ToggleHelp(&m.showHelp, kpm) {
				return m, nil
			}
			if m.showHelp {
				return m, nil
			}
		}
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
		if m.filter != nil {
			m.pendingG = false
			return m.updateFilterMenu(msg)
		}
		if m.filterMode {
			m.pendingG = false
			return m.updateFilterMode(msg)
		}
		if msg.String() == "g" {
			if m.pendingG {
				m.pendingG = false
				m.list.SetCursor(0)
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
			m.filterInput = ui.NewTextField()
			return m, nil
		case "j", "down":
			m.list.MoveDown()
		case "k", "up":
			m.list.MoveUp()
		case "G":
			m.list.SetCursor(len(m.snap.Rows) - 1)
		case "ctrl+g":
			// Open the highlighted row's bound checkout in pop (task 02). A row
			// with a bound checkout surfaces its path on quit so the command
			// layer runs the workbench-aware open after the TUI exits; a row with
			// no checkout shows an inline status and keeps the dashboard running
			// (mirroring the shell action).
			row, ok := m.list.Selected()
			if !ok {
				return m, nil
			}
			if strings.TrimSpace(row.RuntimePath) == "" {
				m.statusMsg = "no checkout bound to this task set"
				return m, nil
			}
			m.statusMsg = ""
			m.openCheckout = row.RuntimePath
			return m, tea.Quit
		case "a":
			row, ok := m.list.Selected()
			if !ok {
				return m, nil
			}
			m.menu = newDashboardMenu(row)
			m.err = nil
			m.statusMsg = ""
			return m, nil
		case "f":
			// Open the row-inclusion filter menu (ADR-0121). Unlike `/` (a transient
			// fuzzy query over the already-included rows) this modal flips which rows
			// are included at all; the two are independent concepts.
			m.filter = newDashboardFilterMenu()
			m.err = nil
			m.statusMsg = ""
			return m, nil
		case "l", "enter":
			row, ok := m.list.Selected()
			if !ok {
				return m, nil
			}
			m.err = nil
			m.detail = newDetailView(row)
			return m, m.loadDetail(row)
		}
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.cols.width = msg.Width
		m.cols.refit()
		m.resizeMainList()
	case dashboardTickMsg:
		cmds := []tea.Cmd{dashboardTick(), m.reload()}
		if m.detail != nil {
			cmds = append(cmds, m.loadDetail(m.detail.row))
		}
		return m, tea.Batch(cmds...)
	case dashboardRowsMsg:
		m.err = msg.err
		if msg.err == nil {
			m.allRows = msg.snap.Rows
			m.snap = msg.snap
			if m.filterMode {
				m.snap.Rows = filterDashboardRows(m.allRows, m.filterInput.Value())
			}
			m.syncListRows()
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
		m.cols.syncNatural(m.snap.Rows)
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
		m.drainPick = newDashboardDrainModal(msg.row, msg.entries)
		return m, nil
	case dashboardPreviewMsg:
		if msg.err != nil {
			m.err = msg.err
			return m, nil
		}
		// Preview switched the tmux client to the drain's working pane; the
		// dashboard has handed off attention, so close it rather than leaving it
		// stranded behind the pane the operator now looks at.
		return m, tea.Quit
	case dashboardBindListMsg:
		if msg.err != nil {
			m.err = msg.err
			m.bind = nil
			return m, nil
		}
		m.bind = &dashboardBindModal{row: msg.row, list: newBindEntryList(msg.entries)}
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
		m.bind.list = newBindEntryList(bindRefEntries(msg.refs))
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
		if m.bind.stage != dashboardBindStageName && m.bind.list != nil {
			m.bind.list.MoveDown()
		}
		return m, nil
	case "k", "up":
		if m.bind.stage != dashboardBindStageName && m.bind.list != nil {
			m.bind.list.MoveUp()
		}
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
		m.menu.list.MoveDown()
		return m, nil
	case "k", "up":
		m.menu.list.MoveUp()
		return m, nil
	case "enter":
		return m.invokeMenuItem(m.menu.list.Cursor())
	}
	for i, item := range m.menu.list.Items() {
		if msg.String() == item.key {
			return m.invokeMenuItem(i)
		}
	}
	return m, nil
}

// invokeMenuItem closes the menu and dispatches the verb at idx against the row
// the menu was opened on.
func (m QueueDashboard) invokeMenuItem(idx int) (tea.Model, tea.Cmd) {
	if m.menu == nil {
		return m, nil
	}
	items := m.menu.list.Items()
	if idx < 0 || idx >= len(items) {
		return m, nil
	}
	item := items[idx]
	row := m.menu.row
	m.menu = nil
	return m.dispatchMenuAction(item.action, row)
}

// updateFilterMenu drives the row-inclusion filter modal: esc/ctrl+c/f close it,
// j/k move the highlight, Enter/space toggles the highlighted filter, and any
// matching toggle letter flips that filter directly. The menu stays open across
// a toggle so the checkbox flip is visible and successive toggles are cheap;
// non-matching keys are inert while it is open (v is gated off by
// ViewToggleAllowed, so it lands here and is ignored).
func (m QueueDashboard) updateFilterMenu(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.filter == nil {
		return m, nil
	}
	switch msg.String() {
	case "esc", "ctrl+c", "f":
		m.filter = nil
		return m, nil
	case "j", "down":
		m.filter.list.MoveDown()
		return m, nil
	case "k", "up":
		m.filter.list.MoveUp()
		return m, nil
	case "enter", "space":
		return m.invokeFilterItem(m.filter.list.Cursor())
	}
	for i, item := range m.filter.list.Items() {
		if msg.String() == item.key {
			return m.invokeFilterItem(i)
		}
	}
	return m, nil
}

// invokeFilterItem flips the inclusion filter at idx and rebuilds the row set.
// The menu stays open. Flipping mutates the session view flag on the model's
// Deps (m.d.IncludeDone) and returns a reload: BuildDashboard re-derives the
// rows honoring the new flag and re-sorts them (ADR-0121), and the reload's
// dashboardRowsMsg re-applies any active `/` fuzzy query, so the two filters
// stay independent. The flag is session-only — a fresh Deps on relaunch resets
// it to the launch seed (`--include-done`).
func (m QueueDashboard) invokeFilterItem(idx int) (tea.Model, tea.Cmd) {
	if m.filter == nil {
		return m, nil
	}
	items := m.filter.list.Items()
	if idx < 0 || idx >= len(items) {
		return m, nil
	}
	switch items[idx].toggle {
	case filterToggleShowDone:
		m.d.IncludeDone = !m.d.IncludeDone
		return m, m.reload()
	}
	return m, nil
}

// filterToggleOn reports the current on/off state of an inclusion filter, read
// from the live view flags so the menu checkbox tracks the actual view.
func (m QueueDashboard) filterToggleOn(toggle dashboardFilterToggle) bool {
	switch toggle {
	case filterToggleShowDone:
		return m.d != nil && m.d.IncludeDone
	}
	return false
}

// dispatchMenuAction runs the verb. The conditional guards mirror
// dashboardMenuItems' context filtering — an item present in the menu always
// passes its guard, but the guards keep dispatch self-contained.
func (m QueueDashboard) dispatchMenuAction(action dashboardMenuAction, row DashboardRow) (tea.Model, tea.Cmd) {
	m.err = nil
	switch action {
	case menuActionDrain:
		return m, m.launchDrain(row)
	case menuActionVerify:
		if !dashboardVerifyEligible(row) {
			return m, nil
		}
		m.statusMsg = ""
		return m, m.launchVerify(row)
	case menuActionBind:
		m.bind = &dashboardBindModal{row: row, loading: true}
		return m, m.loadBindWorktrees(row)
	case menuActionUnbind:
		if !row.Bound {
			return m, nil
		}
		m.abandon = &dashboardAbandonModal{row: row}
		return m, nil
	case menuActionAutoDrain:
		if row.Orphaned {
			return m, nil
		}
		for i := range m.snap.Rows {
			if m.snap.Rows[i].cursorKey == row.cursorKey {
				m.snap.Rows[i].AutoDrain = !m.snap.Rows[i].AutoDrain
				break
			}
		}
		m.cols.syncNatural(m.snap.Rows)
		return m, m.toggleAutoDrain(row)
	case menuActionPreview:
		if strings.TrimSpace(row.PaneID) == "" {
			m.statusMsg = "no working pane to preview"
			return m, nil
		}
		m.statusMsg = ""
		return m, m.previewDrain(row)
	case menuActionUnpark:
		if !row.Parked {
			m.statusMsg = "task set is not parked"
			return m, nil
		}
		m.statusMsg = ""
		return m, m.unparkSet(row)
	case menuActionShell:
		if strings.TrimSpace(row.RuntimePath) == "" {
			m.statusMsg = "no checkout bound to this task set"
			return m, nil
		}
		m.statusMsg = ""
		shell := os.Getenv("SHELL")
		if shell == "" {
			shell = "/bin/sh"
		}
		cmd := exec.Command(shell)
		cmd.Dir = row.RuntimePath
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
			m.taskMenu = newTaskMenu(task, items, true)
		}
		return m, nil
	}
	if msg.String() == "g" {
		if m.pendingG {
			m.pendingG = false
			if m.detail != nil {
				m.detail.list.SetCursor(0)
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
	case "ctrl+g":
		// Open the detail's task set checkout in pop, mirroring the main-list
		// Ctrl-g: surface the bound checkout on quit so the command layer runs the
		// workbench-aware open after the TUI exits; an unbound set shows an inline
		// status and keeps the dashboard running.
		if m.detail == nil {
			return m, nil
		}
		if strings.TrimSpace(m.detail.row.RuntimePath) == "" {
			m.detail.statusMsg = "no checkout bound to this task set"
			return m, nil
		}
		m.detail.statusMsg = ""
		m.openCheckout = m.detail.row.RuntimePath
		return m, tea.Quit
	case "j", "down":
		if m.detail != nil {
			m.detail.list.MoveDown()
		}
	case "k", "up":
		if m.detail != nil {
			m.detail.list.MoveUp()
		}
	case "G":
		if m.detail != nil && m.detail.manifest != nil {
			m.detail.list.SetCursor(len(m.detail.manifest.Tasks) - 1)
		}
	case "l", "enter":
		if m.detail == nil || m.detail.loading {
			return m, nil
		}
		task, ok := m.detail.list.Selected()
		if !ok {
			return m, nil
		}
		m.detail.peek = &taskTextPeek{taskID: task.ID, loading: true}
		return m, m.loadTaskText(m.detail.manifest, task)
	case "a":
		if m.detail == nil || m.detail.loading {
			return m, nil
		}
		task, ok := m.detail.list.Selected()
		if !ok {
			return m, nil
		}
		items := taskMenuItems(task)
		if len(items) == 0 {
			return m, nil
		}
		m.detail.statusMsg = ""
		m.taskMenu = newTaskMenu(task, items, false)
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
		m.taskMenu.list.MoveDown()
		return m, nil
	case "k", "up":
		m.taskMenu.list.MoveUp()
		return m, nil
	case "enter":
		return m.invokeTaskMenuItem(m.taskMenu.list.Cursor())
	}
	for i, item := range m.taskMenu.list.Items() {
		if msg.String() == item.key {
			return m.invokeTaskMenuItem(i)
		}
	}
	return m, nil
}

// invokeTaskMenuItem closes the menu and dispatches the verb at idx against the
// task the menu was opened on. The items are pre-filtered to valid transitions
// (taskMenuItems), so the verb applies without a separate confirmation.
func (m QueueDashboard) invokeTaskMenuItem(idx int) (tea.Model, tea.Cmd) {
	if m.taskMenu == nil {
		return m, nil
	}
	items := m.taskMenu.list.Items()
	if idx < 0 || idx >= len(items) {
		return m, nil
	}
	if m.detail == nil {
		m.taskMenu = nil
		return m, nil
	}
	item := items[idx]
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
			err = d.completeDetailTask(row.DefPath, taskPath)
		case "O":
			err = d.resetDetailTask(row.DefPath, taskPath)
		case "K":
			err = d.skipDetailTask(row.DefPath, taskPath)
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
		m.filterInput = ui.TextField{}
		m.snap.Rows = m.allRows
		m.syncListRows()
		return m, nil
	case "j", "down":
		m.list.MoveDown()
		return m, nil
	case "k", "up":
		m.list.MoveUp()
		return m, nil
	default:
		m.filterInput.Update(msg)
		m.snap.Rows = filterDashboardRows(m.allRows, m.filterInput.Value())
		m.syncListRows()
		return m, nil
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

func (m QueueDashboard) confirmBindModal() (tea.Model, tea.Cmd) {
	if m.bind == nil || m.bind.loading {
		return m, nil
	}
	switch m.bind.stage {
	case dashboardBindStageWorktree:
		entry, ok := m.bind.list.Selected()
		if !ok {
			return m, nil
		}
		if entry.Create {
			m.bind.loading = true
			return m, m.loadBindRefs(m.bind.row)
		}
		m.bind.loading = true
		return m, m.adoptBindWorktree(m.bind.row, entry.Path)
	case dashboardBindStageBaseRef:
		entry, ok := m.bind.list.Selected()
		if !ok {
			return m, nil
		}
		m.bind.baseRef = entry.Label
		m.bind.stage = dashboardBindStageName
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
		err := UnparkSet(m.d, row.SetRef)
		return dashboardUnparkMsg{setID: row.SetID, err: err}
	}
}

// archiveSet sets the reversible archived flag on the cursored set through the
// existing archive flag-write path. It touches only Task state, leaving the
// set's Worktree binding intact; the archived row drops out on the next build,
// which excludes Archived sets. Archiving is fully reversible, so no
// confirmation is required (ADR cleanup path for Done and Orphaned sets alike).
func (m QueueDashboard) archiveSet(row DashboardRow) tea.Cmd {
	return func() tea.Msg {
		err := m.d.archiveSet(row.DefPath, row.SetID)
		return dashboardArchiveMsg{setID: row.SetID, err: err}
	}
}

func (m QueueDashboard) toggleAutoDrain(row DashboardRow) tea.Cmd {
	return func() tea.Msg {
		result, err := m.d.toggleAutoDrain(row.DefPath, row.StatePath, row.SetID)
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
		bound, err := dashboardSetBound(m.d, m.cfg, row.SetRef)
		if err != nil {
			return dashboardDrainListMsg{row: row, err: err}
		}
		if bound {
			_, err := LaunchDrain(m.d, m.cfg, row.SetRef)
			return dashboardDrainMsg{err: err}
		}
		entries, err := DrainTargetEntries(m.d, m.cfg, row.SetRef)
		return dashboardDrainListMsg{row: row, entries: entries, err: err}
	}
}

func (m QueueDashboard) updateDrainModal(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc", "ctrl+c":
		m.drainPick = nil
		return m, nil
	case "j", "down":
		if m.drainPick.list != nil {
			m.drainPick.list.MoveDown()
		}
		return m, nil
	case "k", "up":
		if m.drainPick.list != nil {
			m.drainPick.list.MoveUp()
		}
		return m, nil
	case "enter":
		return m.confirmDrainModal()
	}
	return m, nil
}

func (m QueueDashboard) confirmDrainModal() (tea.Model, tea.Cmd) {
	if m.drainPick == nil || m.drainPick.loading {
		return m, nil
	}
	entry, ok := m.drainPick.list.Selected()
	if !ok {
		return m, nil
	}
	row := m.drainPick.row
	m.drainPick.loading = true
	return m, m.launchDrainTarget(row, entry)
}

// launchDrainTarget binds the chosen target (adopt, provision, or leave unbound
// for trunk) and drains in one action.
func (m QueueDashboard) launchDrainTarget(row DashboardRow, target dashboardDrainEntry) tea.Cmd {
	return func() tea.Msg {
		_, err := LaunchDrainTarget(m.d, m.cfg, row.SetRef, target)
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

// launchVerify spawns a Verifier pane on the focused set (ADR-0123). It records
// no lock, spawn intent, or DrainPane — verify is not a drain — so the verdict
// surfaces through the next poll's ApplyVerifyVerdicts re-derivation via the
// reload dashboardDrainMsg drives.
func (m QueueDashboard) launchVerify(row DashboardRow) tea.Cmd {
	return func() tea.Msg {
		_, err := LaunchVerify(m.d, m.cfg, row.SetRef)
		return dashboardDrainMsg{err: err}
	}
}

func (m QueueDashboard) previewDrain(row DashboardRow) tea.Cmd {
	return func() tea.Msg {
		err := PreviewDrain(m.d, row.SetRef)
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
		refresh, err := d.refresh(row.DefPath)
		if err != nil {
			return dashboardDetailMsg{dashRow: row, err: err}
		}
		snap, _ := newDashboardSnapshot(d)
		var bindings map[string]WorktreeBinding
		if snap != nil {
			bindings = snap.bindings
		}
		var cfg *config.Config
		if d.LoadConfig != nil {
			cfg, _ = d.LoadConfig(config.DefaultConfigPath())
		}
		tasks.ApplyVerifyVerdictsWith(d.Tasks, refresh, cfg, func(setID string) string {
			return binding.RuntimeForSet(bindings, row.RepoKey, setID, row.ProjectPath)
		})
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
		entries, err := BindWorktreeEntries(m.d, m.cfg, row.SetRef)
		return dashboardBindListMsg{row: row, entries: entries, err: err}
	}
}

func (m QueueDashboard) loadBindRefs(row DashboardRow) tea.Cmd {
	return func() tea.Msg {
		refs, err := BindBaseRefs(m.d, m.cfg, row.SetRef)
		return dashboardBindRefsMsg{refs: refs, err: err}
	}
}

func (m QueueDashboard) adoptBindWorktree(row DashboardRow, checkoutPath string) tea.Cmd {
	return func() tea.Msg {
		_, err := AdoptWorktree(m.d, m.cfg, row.SetRef, checkoutPath)
		return dashboardBindMsg{err: err}
	}
}

func (m QueueDashboard) createBindWorktree(row DashboardRow, baseRef, name string) tea.Cmd {
	return func() tea.Msg {
		_, err := CreateWorktree(m.d, m.cfg, row.SetRef, baseRef, name)
		return dashboardBindMsg{err: err}
	}
}

func (m QueueDashboard) abandonWorktree(row DashboardRow) tea.Cmd {
	return func() tea.Msg {
		_, err := UnbindWorktree(m.d, m.cfg, row.SetRef)
		return dashboardAbandonMsg{err: err}
	}
}

// dashboardSetBound reports whether the row's set already holds a Worktree
// binding. The Drain target picker only opens for unbound sets; a bound set
// resumes in its binding (ADR-0052).
func dashboardSetBound(d *Deps, cfg *config.Config, ref SetRef) (bool, error) {
	d = ensureQueueDeps(d)
	repoKey := ref.RepoKey
	if repoKey == "" {
		_, rk, err := dashboardBindContext(d, cfg, ref)
		if err != nil {
			return false, err
		}
		repoKey = rk
	}
	b, ok := bindingForSet(d.Tasks, repoKey, ref.SetID)
	return ok && strings.TrimSpace(b.RuntimePath) != "", nil
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

func (m QueueDashboard) helpEntries() []ui.HelpEntry {
	// Determine current mode and return contextual help entries
	switch {
	case m.bind != nil:
		// Bind modal - contextual based on stage
		switch m.bind.stage {
		case dashboardBindStageWorktree:
			return []ui.HelpEntry{
				{Key: "j/k", Desc: "navigate worktrees"},
				{Key: "enter", Desc: "select"},
				{Key: "esc", Desc: "cancel"},
			}
		case dashboardBindStageBaseRef:
			return []ui.HelpEntry{
				{Key: "j/k", Desc: "navigate refs"},
				{Key: "enter", Desc: "select base ref"},
				{Key: "esc", Desc: "cancel"},
			}
		case dashboardBindStageName:
			return []ui.HelpEntry{
				{Key: "typing", Desc: "enter worktree name"},
				{Key: "backspace", Desc: "delete character"},
				{Key: "enter", Desc: "create worktree"},
				{Key: "esc", Desc: "cancel"},
			}
		}
	case m.drainPick != nil:
		// Drain picker modal
		return []ui.HelpEntry{
			{Key: "j/k", Desc: "navigate targets"},
			{Key: "enter", Desc: "drain to selected"},
			{Key: "esc", Desc: "cancel"},
		}
	case m.abandon != nil:
		// Abandon/unbind confirmation modal
		return []ui.HelpEntry{
			{Key: "y/enter", Desc: "confirm unbind"},
			{Key: "n/esc", Desc: "cancel"},
		}
	case m.taskMenu != nil:
		// Task-level action menu (in detail or peek)
		return []ui.HelpEntry{
			{Key: "C", Desc: "complete task"},
			{Key: "O", Desc: "open/reopen task"},
			{Key: "K", Desc: "skip task"},
			{Key: "j/k", Desc: "navigate"},
			{Key: "enter", Desc: "run action"},
			{Key: "esc", Desc: "close menu"},
		}
	case m.menu != nil:
		// Dashboard action menu
		return []ui.HelpEntry{
			{Key: "i", Desc: "drain"},
			{Key: "b", Desc: "bind worktree"},
			{Key: "U", Desc: "unbind worktree"},
			{Key: "a", Desc: "toggle auto-drain"},
			{Key: "p", Desc: "preview drain"},
			{Key: "P", Desc: "unpark"},
			{Key: "O", Desc: "shell"},
			{Key: "A", Desc: "archive"},
			{Key: "j/k", Desc: "navigate"},
			{Key: "enter", Desc: "run action"},
			{Key: "esc", Desc: "close menu"},
		}
	case m.filter != nil:
		// Row-inclusion filter menu
		return []ui.HelpEntry{
			{Key: "d", Desc: "toggle show done"},
			{Key: "j/k", Desc: "navigate"},
			{Key: "enter/space", Desc: "toggle filter"},
			{Key: "esc", Desc: "close menu"},
		}
	case m.detail != nil && m.detail.peek != nil:
		// Detail peek view
		return []ui.HelpEntry{
			{Key: "j/k", Desc: "scroll line"},
			{Key: "ctrl+d", Desc: "page down"},
			{Key: "ctrl+u", Desc: "page up"},
			{Key: "gg", Desc: "top"},
			{Key: "G", Desc: "bottom"},
			{Key: "a", Desc: "task actions"},
			{Key: "h/esc", Desc: "close peek"},
		}
	case m.detail != nil:
		// Detail view (task list)
		return []ui.HelpEntry{
			{Key: "j/k", Desc: "navigate tasks"},
			{Key: "gg", Desc: "first task"},
			{Key: "G", Desc: "last task"},
			{Key: "l/enter", Desc: "peek task text"},
			{Key: "a", Desc: "task actions"},
			{Key: "ctrl+g", Desc: "open worktree"},
			{Key: "h/esc", Desc: "back to list"},
		}
	case m.filterMode:
		// Filter mode
		return []ui.HelpEntry{
			{Key: "typing", Desc: "filter rows"},
			{Key: "j/k", Desc: "navigate filtered"},
			{Key: "v", Desc: "routines view"},
			{Key: "esc", Desc: "clear filter"},
		}
	default:
		// Main list view
		return []ui.HelpEntry{
			{Key: "j/k", Desc: "navigate"},
			{Key: "gg", Desc: "first row"},
			{Key: "G", Desc: "last row"},
			{Key: "l/enter", Desc: "open detail"},
			{Key: "a", Desc: "action menu"},
			{Key: "ctrl+g", Desc: "open worktree"},
			{Key: "/", Desc: "filter"},
			{Key: "f", Desc: "filter menu"},
			{Key: "v", Desc: "routines view"},
			{Key: "h/esc", Desc: "quit"},
		}
	}
	return nil
}

func (m QueueDashboard) View() tea.View {
	if m.showHelp {
		title := "Help · Queue"
		if m.filterMode {
			title = "Help · Queue · filter"
		} else if m.detail != nil && m.detail.peek != nil {
			title = "Help · Queue · peek"
		} else if m.detail != nil {
			title = "Help · Queue · detail"
		} else if m.menu != nil {
			title = "Help · Queue · action menu"
		} else if m.filter != nil {
			title = "Help · Queue · filter menu"
		} else if m.taskMenu != nil {
			title = "Help · Queue · task menu"
		} else if m.bind != nil {
			title = "Help · Queue · bind"
		} else if m.drainPick != nil {
			title = "Queue · drain"
		} else if m.abandon != nil {
			title = "Queue · unbind"
		}
		content := ui.RenderHelpOverlay(title, m.helpEntries(), m.width, m.height)
		v := tea.NewView(content)
		v.AltScreen = true
		return v
	}

	if m.detail != nil {
		content := m.viewDetail()
		v := tea.NewView(content)
		v.AltScreen = true
		return v
	}
	m.cols.width = m.width
	m.cols.refit()
	m.resizeMainList()

	var content string
	switch {
	case m.menu != nil:
		content = m.viewWithMenu()
	case m.filter != nil:
		content = m.viewWithFilterMenu()
	case m.bind != nil || m.drainPick != nil || m.abandon != nil:
		content = m.viewWithModal()
	default:
		content = m.frameSpec().Render(m.mainBody())
	}
	v := tea.NewView(content)
	v.AltScreen = true
	return v
}

// frameSpec builds the Frame describing the main task-set view's chrome: the
// Queue · N summary (Header), the filter input (InputBox), the refresh error
// (Warnings), the transient statusMsg (Status), and the footer hint (Hints). The
// same Frame drives both the body-height budget and the render, so the reserved
// line count can never drift from what is drawn (ADR-0079).
func (m QueueDashboard) frameSpec() ui.Frame {
	var warnings []string
	if m.err != nil {
		warnings = append(warnings, fmt.Sprintf("refresh error: %v", m.err))
	}
	header := ""
	if len(m.snap.Rows) > 0 {
		header = "Queue · " + dashboardSummary(m.snap.Rows)
	}
	inputBox := ""
	if m.filterMode {
		inputBox = m.filterInput.View()
	}
	return ui.Frame{
		Width:    m.width,
		TermH:    m.height,
		Header:   header,
		InputBox: inputBox,
		Warnings: warnings,
		Status:   m.statusMsg,
		Hints:    m.mainHint(),
	}
}

// mainHint returns the footer hint for the main (non-modal, non-menu) view.
func (m QueueDashboard) mainHint() string {
	if len(m.snap.Rows) == 0 {
		if m.filterMode {
			return "esc clear filter · v routines · C-h help"
		}
		return "v routines · C-h help · h/esc quit"
	}
	if m.filterMode {
		return "esc clear filter · j/k navigate · v routines · C-h help"
	}
	return "j/k move · gg/G top/bottom · l/enter status · a actions · / filter · f filters · v routines · C-h help · h/esc quit"
}

// mainBody renders the table body (a blank line, the column header, the
// separator, then the List's scroll window) or the empty-state message. It is
// the body the Frame composes its chrome around.
func (m QueueDashboard) mainBody() string {
	if len(m.snap.Rows) == 0 {
		if m.filterMode {
			return "No matching task sets."
		}
		return "No queue-actionable task sets."
	}
	var parts []string
	if dashboardTwoLineMode(m.snap.Rows, m.width, m.height) {
		line1Widths := dashboardTwoLineFitWidths(dashboardTwoLineNaturalWidths(m.snap.Rows), dashboardTableBodyBudget(m.width))
		parts = []string{
			"",
			ui.TruncateString("  "+dashboardTwoLineTableHeader(line1Widths), m.width),
			ui.TruncateString("  "+dashboardTwoLineStatusHeader(line1Widths), m.width),
			ui.TruncateString("  "+dashboardTwoLineTableSeparator(line1Widths), m.width),
		}
	} else {
		parts = []string{
			"",
			ui.TruncateString("  "+dashboardTableLine(dashboardTableHeaders(), m.cols.widths), m.width),
			ui.TruncateString("  "+dashboardTableSeparator(m.cols.widths), m.width),
		}
	}
	parts = append(parts, m.list.VisibleRows()...)
	return strings.Join(parts, "\n")
}

// viewWithMenu renders the action-menu overlay: the summary, the full table with
// the menu spliced next to the cursored row (bespoke overlay placement, ADR-0079),
// and the menu footer. The menu overlay's own body is ported onto List in a later
// slice; here it keeps the current rendering with the cursor read from the List.
func (m QueueDashboard) viewWithMenu() string {
	var body strings.Builder
	if m.err != nil {
		fmt.Fprintf(&body, "refresh error: %v\n", m.err)
	}
	fmt.Fprintf(&body, "Queue · %s\n", dashboardSummary(m.snap.Rows))
	fmt.Fprintln(&body)
	renderDashboardTableWithMenu(&body, m.snap.Rows, m.list.Cursor(), m.width, m.height, m.menu)
	if m.statusMsg != "" {
		fmt.Fprintf(&body, "  %s\n", m.statusMsg)
	}
	writeDashboardFooter(&body, m.height, ui.HintStyle.Render("j/k move · enter/letter run · esc close"))
	return body.String()
}

// viewWithFilterMenu renders the row-inclusion filter modal: the summary, the
// full table, and the filter toggles below it, replacing the footer. It mirrors
// viewWithMenu's chrome — a sibling modal — but the menu is not row-anchored, so
// it sits below the table rather than splicing next to the cursor.
func (m QueueDashboard) viewWithFilterMenu() string {
	var body strings.Builder
	if m.err != nil {
		fmt.Fprintf(&body, "refresh error: %v\n", m.err)
	}
	fmt.Fprintf(&body, "Queue · %s\n", dashboardSummary(m.snap.Rows))
	fmt.Fprintln(&body)
	renderDashboardTable(&body, m.snap.Rows, m.list.Cursor(), m.width, m.height)
	for _, ml := range m.dashboardFilterMenuLines() {
		fmt.Fprintf(&body, "%s\n", ml)
	}
	writeDashboardFooter(&body, m.height, ui.HintStyle.Render("j/k move · enter/space toggle · esc close"))
	return body.String()
}

// dashboardFilterMenuLines renders the filter overlay as a block of lines: a
// dimmed "filters" caption, then one checkbox line per toggle with the
// highlighted item carrying the shared cursor block. The checkbox state is read
// live from the model's view flags (filterToggleOn), so it always reflects the
// current view.
func (m QueueDashboard) dashboardFilterMenuLines() []string {
	if m.filter == nil {
		return nil
	}
	lines := []string{ui.TruncateString("    "+ui.HintStyle.Render("filters"), m.width)}
	cursor := m.filter.list.Cursor()
	for i, item := range m.filter.list.Items() {
		marker := "  "
		if i == cursor {
			marker = ui.IndicatorStyle.Render("█") + " "
		}
		box := "[ ]"
		if m.filterToggleOn(item.toggle) {
			box = "[x]"
		}
		line := fmt.Sprintf("    %s%s %s %s", marker, item.key, box, item.label)
		lines = append(lines, ui.TruncateString(line, m.width))
	}
	return lines
}

// viewWithModal renders the summary, the full table, and the active modal below
// it, replacing the footer. The modal bodies are ported onto List in a later
// slice; here they keep the current bespoke rendering (ADR-0079).
func (m QueueDashboard) viewWithModal() string {
	var body strings.Builder
	if m.err != nil {
		fmt.Fprintf(&body, "refresh error: %v\n", m.err)
	}
	fmt.Fprintf(&body, "Queue · %s\n", dashboardSummary(m.snap.Rows))
	fmt.Fprintln(&body)
	renderDashboardTable(&body, m.snap.Rows, m.list.Cursor(), m.width, m.height)
	// avail is the number of body lines left for the modal below the table, so
	// its scroll window clamps long worktree/ref lists instead of overflowing.
	// A non-positive avail (no WindowSizeMsg yet) means "don't clamp".
	avail := 0
	if m.height > 0 {
		avail = m.height - strings.Count(body.String(), "\n")
	}
	switch {
	case m.bind != nil:
		renderDashboardBindModal(&body, m.bind, avail, m.width)
	case m.drainPick != nil:
		renderDashboardDrainModal(&body, m.drainPick, avail, m.width)
	case m.abandon != nil:
		renderDashboardAbandonModal(&body, m.abandon, m.width)
	}
	return body.String()
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
		if row.LiveDrain {
			running++
		}
		if dashboardAutoDrainWaiting(row) {
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

// viewDetail renders the full-screen task-set detail view. The task text peek
// (ADR-0079) and the task action-menu overlay keep their bespoke rendering; the
// loading, error, and content states compose through a Frame with the task list on
// ui.List.
func (m QueueDashboard) viewDetail() string {
	d := m.detail
	if d.peek != nil {
		var b strings.Builder
		renderTaskTextPeek(&b, d, m.height, m.width, m.taskMenu)
		return b.String()
	}
	if m.taskMenu != nil {
		var b strings.Builder
		renderDetailContent(&b, d, m.height, m.width, m.taskMenu)
		return b.String()
	}
	frame, body := m.detailFrame()
	return frame.Render(body)
}

// detailFrame builds the Frame and body for the non-menu detail states: loading,
// error, missing/malformed manifest, and the task-list content. The same Frame
// drives the body-height budget and the render (ADR-0079); the content body's List
// is sized to the budget the Frame leaves minus the table's own chrome, so the list
// clamps to the terminal instead of rendering every task.
func (m QueueDashboard) detailFrame() (ui.Frame, string) {
	d := m.detail
	const backHint = "h/esc back"
	if d.loading {
		return ui.Frame{Width: m.width, TermH: m.height, Hints: backHint}, fmt.Sprintf("Loading %s...", d.row.SetID)
	}
	if d.err != nil {
		return ui.Frame{Width: m.width, TermH: m.height, Hints: backHint}, fmt.Sprintf("error loading %s: %v", d.row.SetID, d.err)
	}

	manifest := d.manifest
	status := tasks.DeriveStatus(manifest)
	label := string(status)
	progress := ""
	verifiedSHA := ""
	if d.taskRow != nil {
		status = d.taskRow.Status
		label = tasks.StatusLabel(*d.taskRow)
		progress = d.taskRow.Progress
		verifiedSHA = d.taskRow.VerifiedAtSHA
	}
	header := detailHeader(d.row.SetID, label, progress, verifiedSHA)

	if status == tasks.StatusMissing {
		return ui.Frame{Width: m.width, TermH: m.height, Header: header, Hints: backHint}, "  registered task set missing"
	}
	if manifest == nil || !manifest.Valid {
		lines := []string{"  malformed manifest"}
		if manifest != nil {
			for _, e := range manifest.Errors {
				lines = append(lines, "  - "+e)
			}
		}
		return ui.Frame{Width: m.width, TermH: m.height, Header: header, Hints: backHint}, strings.Join(lines, "\n")
	}

	frame := ui.Frame{
		Width:  m.width,
		TermH:  m.height,
		Header: header,
		Status: d.statusMsg,
		Hints:  "j/k · gg/G top/bottom · l/enter peek · a actions · h/esc back",
	}
	listH := frame.BodyHeight(m.height) - detailTableChromeLines
	if listH < 1 {
		listH = 1
	}
	d.list.Resize(listH)
	parts := []string{
		"",
		"  " + detailTableHeader(d.cols.idW),
		"  " + detailTableSeparator(d.cols.idW),
	}
	parts = append(parts, d.list.VisibleRows()...)
	return frame, strings.Join(parts, "\n")
}

// detailHeader builds the detail view's title line: "Task · <set>  [<status>]"
// plus progress and, when applicable, a yellow "verified @ <shortSHA>" suffix
// inside the status brackets (ADR-0096).
func detailHeader(setID, label, progress, verifiedSHA string) string {
	if verifiedSHA != "" {
		suffix := dashboardVerifiedAtStyle.Render("verified @ " + verifiedSHA)
		label += " · " + suffix
	}
	header := fmt.Sprintf("Task · %s  [%s]", setID, label)
	if progress != "" {
		header += "  " + progress
	}
	return header
}

// detailIDWidth returns the ID-column width: the widest task ID, floored at the
// "ID" header label.
func detailIDWidth(items []tasks.Task) int {
	idW := len("ID")
	for _, t := range items {
		if len(t.ID) > idW {
			idW = len(t.ID)
		}
	}
	return idW
}

// detailTaskLine formats one task row's cells (status / type / id / title /
// blocked-by) over the fixed and idW-derived widths, without the cursor prefix —
// the List owns the leading indicator column.
func detailTaskLine(t tasks.Task, idW int) string {
	title := t.Title
	if len(title) > detailTitleW {
		title = title[:detailTitleW-3] + "..."
	}
	blockedBy := "-"
	if len(t.BlockedBy) > 0 {
		blockedBy = strings.Join(t.BlockedBy, ", ")
	}
	statusCell := string(t.Status)
	if t.Status == tasks.TaskFailed && t.FailedAfter != nil {
		statusCell = fmt.Sprintf("failed(%d)", *t.FailedAfter)
	}
	return fmt.Sprintf("%-*s  %-*s  %-*s  %-*s  %s",
		detailStatusW, statusCell, detailTypeW, t.Type, idW, t.ID, detailTitleW, title, blockedBy)
}

// detailTableHeader is the detail task-table column header, idW-aligned to match
// detailTaskLine.
func detailTableHeader(idW int) string {
	return fmt.Sprintf("%-*s  %-*s  %-*s  %-*s  %s",
		detailStatusW, "STATUS", detailTypeW, "TYPE", idW, "ID", detailTitleW, "TITLE", "BLOCKED-BY")
}

// detailTableSeparator is the dashed rule under detailTableHeader.
func detailTableSeparator(idW int) string {
	return fmt.Sprintf("%-*s  %-*s  %-*s  %-*s  %s",
		detailStatusW, strings.Repeat("-", detailStatusW),
		detailTypeW, strings.Repeat("-", detailTypeW),
		idW, strings.Repeat("-", idW),
		detailTitleW, strings.Repeat("-", detailTitleW),
		strings.Repeat("-", 12))
}

// renderDetailContent renders the detail task list with the action-menu overlay
// spliced next to the cursored task (ADR-0079 bespoke placement; its cursor is
// ported onto List in a later slice). It renders every task — no scroll window —
// and reads the cursor from the List. The non-menu states render via detailFrame.
func renderDetailContent(b *strings.Builder, d *detailView, height, width int, menu *taskMenu) {
	manifest := d.manifest
	taskRow := d.taskRow

	status := tasks.DeriveStatus(manifest)
	label := string(status)
	progress := ""
	verifiedSHA := ""
	if taskRow != nil {
		status = taskRow.Status
		label = tasks.StatusLabel(*taskRow)
		progress = taskRow.Progress
		verifiedSHA = taskRow.VerifiedAtSHA
	}

	header := detailHeader(d.row.SetID, label, progress, verifiedSHA)
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

	idW := d.cols.idW
	fmt.Fprintf(b, "  %s\n", detailTableHeader(idW))
	fmt.Fprintf(b, "  %s\n", detailTableSeparator(idW))

	cursorIdx := d.list.Cursor()
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
		fmt.Fprintf(b, "%s%s\n", prefix, detailTaskLine(t, idW))
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
	cursor := menu.list.Cursor()
	for i, item := range menu.list.Items() {
		marker := "  "
		if i == cursor {
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

// writeModalListRows renders a modal list's scroll window: it sizes the list to
// listH rows (or, when listH is non-positive, to its full length so the caller's
// "don't clamp" mode renders every row) and writes the rows the List returns,
// including its cursor/pad prefix column. Each row is truncated to width so the
// modal never spills past the terminal edge.
func writeModalListRows[T any](w io.Writer, list *ui.List[T], listH, width int) {
	if list == nil {
		return
	}
	if listH < 1 {
		listH = list.Len()
	}
	list.Resize(listH)
	for _, line := range list.VisibleRows() {
		fmt.Fprintln(w, ui.TruncateString(line, width))
	}
}

func renderDashboardBindModal(w io.Writer, modal *dashboardBindModal, avail, width int) {
	if modal == nil {
		return
	}
	fmt.Fprintln(w, ui.TruncateString("Bind worktree", width))
	if modal.loading {
		fmt.Fprintln(w, ui.TruncateString("  loading...", width))
		return
	}
	switch modal.stage {
	case dashboardBindStageWorktree:
		// Chrome above/below the list: the "Bind worktree" title and the hint.
		writeModalListRows(w, modal.list, modalListHeight(avail, 2), width)
		fmt.Fprint(w, ui.HintStyle.Render(ui.TruncateString("enter select · esc cancel", width)))
	case dashboardBindStageBaseRef:
		fmt.Fprintln(w, ui.TruncateString("Base ref", width))
		// Chrome: the title, the "Base ref" caption, and the hint.
		writeModalListRows(w, modal.list, modalListHeight(avail, 3), width)
		fmt.Fprint(w, ui.HintStyle.Render(ui.TruncateString("enter select · esc cancel", width)))
	case dashboardBindStageName:
		fmt.Fprintln(w, ui.TruncateString(fmt.Sprintf("Base: %s", modal.baseRef), width))
		fmt.Fprintln(w, ui.TruncateString(fmt.Sprintf("Name: %s", modal.name), width))
		fmt.Fprint(w, ui.HintStyle.Render(ui.TruncateString("enter create · esc cancel", width)))
	}
}

func renderDashboardDrainModal(w io.Writer, modal *dashboardDrainModal, avail, width int) {
	if modal == nil {
		return
	}
	fmt.Fprintln(w, ui.TruncateString(fmt.Sprintf("Drain target for %s", modal.row.SetID), width))
	if modal.loading {
		fmt.Fprintln(w, ui.TruncateString("  draining...", width))
		return
	}
	// Chrome above/below the list: the title line and the hint.
	writeModalListRows(w, modal.list, modalListHeight(avail, 2), width)
	fmt.Fprint(w, ui.HintStyle.Render(ui.TruncateString("enter drain · esc cancel", width)))
}

// modalListHeight derives a modal list's scroll-window height from the body
// lines left for the modal (avail) minus its chrome lines. A non-positive avail
// (no WindowSizeMsg yet) returns 0, signalling writeModalListRows to render every
// row unclamped; otherwise it floors the window at one row.
func modalListHeight(avail, chrome int) int {
	if avail <= 0 {
		return 0
	}
	h := avail - chrome
	if h < 1 {
		h = 1
	}
	return h
}

func renderDashboardAbandonModal(w io.Writer, modal *dashboardAbandonModal, width int) {
	if modal == nil {
		return
	}
	fmt.Fprintln(w, ui.TruncateString(fmt.Sprintf("Unbind worktree for %s", modal.row.SetID), width))
	if modal.loading {
		fmt.Fprintln(w, ui.TruncateString("  unbinding...", width))
		return
	}
	fmt.Fprintln(w, ui.TruncateString("This releases the binding without integrating. Task statuses are unchanged.", width))
	fmt.Fprint(w, ui.HintStyle.Render(ui.TruncateString("enter/y confirm · n/esc cancel", width)))
}

func renderDashboardTable(w io.Writer, rows []DashboardRow, cursor, width, height int) {
	renderDashboardTableWithMenu(w, rows, cursor, width, height, nil)
}

// renderDashboardTableWithMenu renders the task-set table and, when menu is
// non-nil, splices the action overlay in next to the cursored row: below it by
// default, flipping above when the cursor sits too low for the menu to fit
// beneath it within height (dashboardMenuPlaceBelow).
func renderDashboardTableWithMenu(w io.Writer, rows []DashboardRow, cursor, width, height int, menu *dashboardMenu) {
	if dashboardTwoLineMode(rows, width, height) {
		renderDashboardTableTwoLineWithMenu(w, rows, cursor, width, height, menu)
		return
	}
	headers := dashboardTableHeaders()
	widths := dashboardTableWidthsForRows(rows, width)
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

// renderDashboardTableTwoLineWithMenu renders the two-line task-set table and,
// when menu is non-nil, splices the action overlay next to the cursored row.
// Each row occupies two terminal lines: line 1 holds the live-drain indicator,
// PROJECT, TASK SET (the set id) and WORKTREE; line 2 holds STATUS indented under
// the TASK SET column.
func renderDashboardTableTwoLineWithMenu(w io.Writer, rows []DashboardRow, cursor, width, height int, menu *dashboardMenu) {
	line1Widths := dashboardTwoLineFitWidths(dashboardTwoLineNaturalWidths(rows), dashboardTableBodyBudget(width))
	fmt.Fprintf(w, "%s\n", ui.TruncateString("  "+dashboardTwoLineTableHeader(line1Widths), width))
	fmt.Fprintf(w, "%s\n", ui.TruncateString("  "+dashboardTwoLineStatusHeader(line1Widths), width))
	fmt.Fprintf(w, "%s\n", ui.TruncateString("  "+dashboardTwoLineTableSeparator(line1Widths), width))

	var menuLines []string
	placeBelow := true
	if menu != nil {
		menuLines = dashboardMenuLines(menu, width)
		placeBelow = dashboardMenuPlaceBelowTwoLine(cursor, len(menuLines), height)
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
		line1 := ui.TruncateString(prefix+dashboardTwoLineRowLine1(row, line1Widths), width)
		line2 := ui.TruncateString("  "+dashboardTwoLineRowLine2(row, line1Widths), width)
		fmt.Fprintf(w, "%s\n", line1)
		fmt.Fprintf(w, "%s\n", line2)
		if menu != nil && i == cursor && placeBelow {
			writeMenu()
		}
	}
}

// dashboardMenuPlaceBelowTwoLine is the two-line-mode variant of
// dashboardMenuPlaceBelow. Each row consumes two terminal lines, so the space
// below the cursored row is reduced accordingly.
func dashboardMenuPlaceBelowTwoLine(cursor, menuHeight, height int) bool {
	if height <= 0 {
		return true
	}
	linesBelowCursor := height - (dashboardTableTopOffset + 1) - 2*(cursor+1)
	return linesBelowCursor >= menuHeight
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
	cursor := menu.list.Cursor()
	for i, item := range menu.list.Items() {
		marker := "  "
		if i == cursor {
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

// dashboardRowValues returns a row's rendered column cells, with the STATUS cell
// composed at render time from the row's live fields (styled for display).
func dashboardRowValues(row DashboardRow) []string {
	return []string{
		row.Project,
		row.SetID,
		dashboardStatusCellStyled(row),
		renderDashboardDest(row.destKind, row.Worktree),
		dashboardLiveIndicator(row, true),
	}
}

// dashboardRowNaturalValues returns a row's column cells for width measurement.
// It matches dashboardRowValues but uses the plain, un-styled composed status so
// no ANSI ever reaches column-width math (ADR-0108).
func dashboardRowNaturalValues(row DashboardRow) []string {
	return []string{
		row.Project,
		row.SetID,
		dashboardStatusCell(row),
		renderDashboardDest(row.destKind, row.Worktree),
		dashboardLiveIndicator(row, false),
	}
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
		// The trailing live-drain indicator has no header label, so its rule is
		// blank (spaces) rather than dashes — nothing to underline (ADR-0111).
		if i == len(widths)-1 {
			parts[i] = strings.Repeat(" ", width)
			continue
		}
		parts[i] = strings.Repeat("-", width)
	}
	return strings.Join(parts, "  ")
}

func padDashboardCell(s string, width int) string {
	if width <= 0 {
		return s
	}
	if lipgloss.Width(s) > width {
		s = ui.TruncateString(s, width)
	}
	if pad := width - lipgloss.Width(s); pad > 0 {
		return s + strings.Repeat(" ", pad)
	}
	return s
}

// RunDashboard opens the read-only Queue dashboard TUI. It returns the bound
// checkout path chosen with Ctrl-g on the main list (empty when the dashboard
// quit for any other reason), leaving the workbench-aware open to the command
// layer (task 02).
func RunDashboard(d *Deps, cfg *config.Config) (string, error) {
	snap, err := BuildDashboard(d, cfg)
	if err != nil {
		return "", err
	}
	m := newQueueDashboard(d, cfg, snap)
	program := tea.NewProgram(m)
	final, err := program.Run()
	if err != nil {
		return "", err
	}
	if fm, ok := final.(QueueDashboard); ok {
		return fm.openCheckout, nil
	}
	return "", nil
}
