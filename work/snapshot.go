package work

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/glebglazov/pop/config"
	"github.com/glebglazov/pop/project"
	"github.com/glebglazov/pop/tasks"
	"github.com/glebglazov/pop/tasks/binding"
	"github.com/glebglazov/pop/wayfinder"
)

// Deps holds the Work data core's external dependencies. It borrows the
// process-cached Execution-state store handle through Tasks (ADR-0140): every
// store touch goes through a tasks/binding accessor in if-exists mode, so a pure
// read never materialises a database and the handle is never closed here. It
// cannot take queue.Deps — queue imports work, not the reverse (ADR-0143). The
// seams mirror the ones the queue builder carried so injected test doubles keep
// working after the move.
type Deps struct {
	Tasks      *tasks.Deps
	Project    *project.Deps
	LoadConfig func(string) (*config.Config, error)

	// IncludeDone is the Done-inclusion view flag (ADR-0121): DONE sets are hidden
	// unless this is true.
	IncludeDone bool

	// Refresh returns the Task-set rows registered under a definition path.
	// Defaults to tasks.RefreshWith.
	Refresh func(defPath string) (*tasks.RefreshResult, error)
	// LiveDrains returns every running Drain whose owning process is still alive.
	// The build reads it once per build into its snapshot. Defaults to
	// tasks.LiveRunningDrains.
	LiveDrains func() ([]tasks.RunningDrain, error)
	// Reconcile runs the opportunistic crash-detection pass before a read,
	// transitioning dead-PID running Drains to crashed. Defaults to
	// tasks.ReconcileDrains.
	Reconcile func() (int, error)
	// ReconcileOut receives a message when the opportunistic reconcile pass fails.
	// Defaults to os.Stderr.
	ReconcileOut io.Writer
	// Now returns the current time. Defaults to time.Now.
	Now func() time.Time
	// ProbeDirective reports a config/registration-class error message when a
	// Ready set's worktree directive cannot be satisfied (ADR-0059) — read-only.
	// Empty means satisfiable or no directive. Defaults to a
	// binding.ProbeWorktreeDirective probe surfacing only the two directive
	// sentinels.
	ProbeDirective func(checkout, setID string) string
}

// DefaultDeps returns Work data-core dependencies backed by real implementations.
func DefaultDeps() *Deps {
	return &Deps{
		Tasks:      tasks.DefaultDeps(),
		Project:    project.DefaultDeps(),
		LoadConfig: config.Load,
	}
}

func (d *Deps) now() time.Time {
	if d != nil && d.Now != nil {
		return d.Now()
	}
	return time.Now()
}

// refresh resolves the Refresh seam, defaulting to tasks.RefreshWith.
func (d *Deps) refresh(defPath string) (*tasks.RefreshResult, error) {
	if d.Refresh != nil {
		return d.Refresh(defPath)
	}
	return tasks.RefreshWith(d.Tasks, defPath, tasks.StatePathFor(defPath))
}

// liveDrains resolves the LiveDrains seam, defaulting to tasks.LiveRunningDrains.
func (d *Deps) liveDrains() ([]tasks.RunningDrain, error) {
	if d.LiveDrains != nil {
		return d.LiveDrains()
	}
	return tasks.LiveRunningDrains(d.Tasks)
}

// reconcile runs the opportunistic crash-detection pass before a read pass,
// healing dead-PID running Drains into crashed (ADR-0055). Reconciliation never
// blocks a read: an error is logged to ReconcileOut (default os.Stderr) instead
// of failing the build, so a human notices a transient store failure while the
// read still reflects the pre-reconcile truth.
func (d *Deps) reconcile() {
	var err error
	if d.Reconcile != nil {
		_, err = d.Reconcile()
	} else {
		_, err = tasks.ReconcileDrains(d.Tasks)
	}
	if err == nil {
		return
	}
	out := d.ReconcileOut
	if out == nil {
		out = os.Stderr
	}
	fmt.Fprintf(out, "work: reconcile: %v (continuing with pre-reconcile snapshot)\n", err)
}

// probeDirective resolves the ProbeDirective seam, defaulting to a read-only
// binding.ProbeWorktreeDirective probe. It returns a config-class error message
// only for the two unsatisfiable-directive sentinels (ADR-0059); any other probe
// error yields "".
func (d *Deps) probeDirective(checkout, setID string) string {
	if d.ProbeDirective != nil {
		return d.ProbeDirective(checkout, setID)
	}
	var cfg *config.Config
	if d.LoadConfig != nil {
		cfg, _ = d.LoadConfig(config.DefaultConfigPath())
	}
	err := binding.ProbeWorktreeDirective(d.Tasks, d.Project, cfg, checkout, setID)
	if errors.Is(err, binding.ErrNoResolvableTrunk) || errors.Is(err, binding.ErrNamedWorktreeNotFound) {
		return err.Error()
	}
	return ""
}

// setBackoffLookup builds the per-set abnormal-backoff probe used to derive the
// parked fact. It reads each set's Drain history under the repository's common
// dir and applies the configured escalation schedule. Read errors and a missing
// store degrade to "not parked", never blocking the build.
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

// repoScan holds the minimal per-checkout coordinates the fork-free static
// resolution needs: the picker labels and canonical checkout path (ADR-0060). It
// is the Work data core's slice of the queue scheduler's richer scan record.
type repoScan struct {
	Name         string
	ProjectLabel string
	ProjectPath  string
	RuntimePath  string
}

// repoStatic holds one repo group's static resolution: the repository
// coordinates and integration target, all derived fork-free from the repo.json
// marker's common directory and config (ADR-0060). The build recomputes only the
// volatile overlay (task statuses, locks, daemon state) per poll.
type repoStatic struct {
	defPath       string
	statePath     string
	storageDir    string
	repoKey       string
	repoCommonDir string
	projectName   string
	rep           *repoScan
	repBranch     string
	bare          bool
	// configErr is non-empty when the repository cannot resolve an integration
	// target from config — a bare repo with no declared trunk (ADR-0060/0059). Its
	// sets render this as a config-class error rather than forking git.
	configErr string
}

// repoScanReason is the config-class message a bare repo with no declared trunk
// carries on its sets (ADR-0059/0060).
const repoScanReason = "needs trunk; skipped (set trunk = true in a global [repo.\"<path>\"] block)"

// snapshot is a single point-in-time read of pop.db taken once per build. Each
// rendered row's binding, live-drain, and drain-pane lookup is served from these
// in-memory maps instead of reopening the store per row, so the whole view is one
// consistent snapshot and the store is opened a bounded number of times per
// build.
type snapshot struct {
	bindings   map[string]binding.Binding
	liveDrains map[string]tasks.RunningDrain
	drainPanes map[string]tasks.DrainPane
}

// newSnapshot reads the volatile per-build store state once: AllBindings, the
// live running drains (filtered to PID-alive), and the recorded drain panes. It
// is the single point at which a build touches pop.db for the overlay.
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

// bindingFor returns the snapshot binding for (repoKey, setID).
func (s *snapshot) bindingFor(repoKey, setID string) (binding.Binding, bool) {
	b, ok := s.bindings[binding.ScopedKey(repoKey, setID)]
	return b, ok
}

// BuildSnapshot derives the full Work dashboard snapshot — Task-set rows,
// Wayfinder Map rows, and the live-drain/orphan/park/config-error facts — from
// registered projects and on-disk task/queue state (ADR-0143). It is read-only
// except for the same opportunistic reconcile `pop queue status` runs, and it
// forks no git for the static side (identity, integration target, and branch all
// derive from each repo's repo.json marker plus config, ADR-0060).
func BuildSnapshot(d *Deps, cfg *config.Config) (Snapshot, error) {
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
	// volatile overlay below reads locks from them (ADR-0055).
	d.reconcile()
	projects, err := tasks.ListPickerProjectsWith(d.Project, cfg)
	if err != nil {
		return Snapshot{}, err
	}

	statics, err := repoStatics(d, cfg, projects)
	if err != nil {
		return Snapshot{}, err
	}
	if len(statics) == 0 {
		return Snapshot{}, nil
	}

	var delays []time.Duration
	if qcfg, qerr := cfg.ResolveQueue(); qerr == nil {
		delays = qcfg.CrashRetryDelays
	}
	now := d.now().UTC()
	snap, err := newSnapshot(d)
	if err != nil {
		return Snapshot{}, err
	}
	var rows []Row
	for _, st := range statics {
		groupRows, err := rowsFromStatic(d, cfg, snap, delays, now, st)
		if err != nil {
			return Snapshot{}, err
		}
		rows = append(rows, groupRows...)
	}
	SortRows(rows)
	return Snapshot{Rows: rows}, nil
}

// repoStatics resolves every renderable repo group's static coordinates fork-free
// (ADR-0060). It iterates the repositories that have a Task storage marker on disk
// — the only repos that can contribute rows — and pairs each with the config
// projects whose checkout nests under (or contains) its working-tree root. A
// repository with no matching config project is dropped (ADR-0042).
func repoStatics(d *Deps, cfg *config.Config, projects []project.ExpandedProject) ([]repoStatic, error) {
	// Work dashboard discovery includes wayfinder-only storage (ADR-0130).
	repos, err := tasks.ListWorkStorageRepos(d.Tasks)
	if err != nil {
		return nil, err
	}
	if len(repos) == 0 {
		return nil, nil
	}

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

	statics := make([]repoStatic, 0, len(repos))
	for _, repo := range repos {
		root := storageRepoRoot(d.Tasks, repo.RepositoryPath)
		var scans []repoScan
		for _, c := range cands {
			if pathWithinOrEqual(c.canon, root) || pathWithinOrEqual(root, c.canon) {
				scans = append(scans, repoScan{
					Name:         c.p.Name,
					ProjectLabel: c.p.ProjectLabel,
					ProjectPath:  c.canon,
					RuntimePath:  c.canon,
				})
			}
		}
		if len(scans) == 0 {
			continue // registered storage but not in config: dropped by the intersection.
		}
		st, err := repoStaticFromMarker(d, cfg, repo.RepositoryPath, scans)
		if err != nil {
			return nil, err
		}
		statics = append(statics, st)
	}
	return statics, nil
}

// repoStaticFromMarker derives one repo group's static coordinates from its
// marker's common directory and config, forking no git (ADR-0060): identity and
// paths come from IdentityFromCommonDir, the integration target from
// representative, and the branch from a HEAD file read. A bare repo with no
// declared trunk carries a config-class error on configErr instead.
func repoStaticFromMarker(d *Deps, cfg *config.Config, commonDir string, scans []repoScan) (repoStatic, error) {
	id, err := tasks.IdentityFromCommonDir(d.Tasks, commonDir)
	if err != nil {
		return repoStatic{}, err
	}
	defPath, err := tasks.CanonicalDefinitionPathWith(d.Tasks, id.TasksDir)
	if err != nil {
		return repoStatic{}, err
	}

	rep, bare, err := representative(d, cfg, id.CommonDir, scans)
	if err != nil {
		return repoStatic{}, err
	}
	repBranch := ""
	configErr := ""
	switch {
	case rep != nil:
		repBranch = headBranchFromCheckout(d.Tasks, rep.ProjectPath, id.CommonDir)
	case bare:
		// Bare repo with no declared trunk: no integration target to fork for.
		configErr = repoScanReason
	}

	return repoStatic{
		defPath:       defPath,
		statePath:     tasks.StatePathFor(defPath),
		storageDir:    id.StorageDir,
		repoKey:       binding.RepoKey(id),
		repoCommonDir: id.CommonDir,
		projectName:   repoName(scans, rep),
		rep:           rep,
		repBranch:     repBranch,
		bare:          bare,
		configErr:     configErr,
	}, nil
}

// representative resolves a repo group's integration target without forking git
// (ADR-0060): a per-checkout `trunk = true` override wins (bare or not), else a
// non-bare repo's target is the main worktree — the parent of the common
// directory — and a bare repo with no declared trunk has none (bare=true,
// rep=nil). A renamed execution key surfaces as a fatal config finding.
func representative(d *Deps, cfg *config.Config, commonDir string, scans []repoScan) (*repoScan, bool, error) {
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

	// 2. non-bare repo → main worktree = parent of the common directory.
	if filepath.Base(commonDir) == ".git" {
		return scanForCheckout(d, scans, filepath.Dir(commonDir)), false, nil
	}

	// 3. bare repo with no declared trunk → no integration target.
	return nil, true, nil
}

// scanForCheckout returns the scan whose checkout canonicalizes to checkoutPath,
// or synthesizes one (fork-free) when the target — e.g. a main worktree that is
// not itself a picker Project — is not among the group's scans.
func scanForCheckout(d *Deps, scans []repoScan, checkoutPath string) *repoScan {
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
	return &repoScan{
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

// storageRepoRoot derives a repository's working-tree root from the canonical git
// common directory recorded in its marker: a normal repo's common dir is
// `<root>/.git` and a bare-with-worktrees layout's is `<root>/.bare`, so the root
// is the parent; a top-level bare repo's common dir is the repo dir itself.
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

// canonicalCheckoutPath resolves an absolute, symlink-evaluated checkout path.
func canonicalCheckoutPath(d *tasks.Deps, path string) (string, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	return d.FS.EvalSymlinks(abs)
}

// resolveRepoConfigFor resolves the effective RepoConfig for a checkout, merging
// global [repo."<path>"] overrides over repo-root .pop/config.toml. trunk is
// honored only for the keyed checkout path.
func resolveRepoConfigFor(d *Deps, cfg *config.Config, checkoutPath string) (config.RepoConfig, error) {
	pd := d.Project
	if pd == nil || pd.FS == nil {
		pd = project.DefaultDeps()
	}
	cd := &config.Deps{FS: pd.FS}
	if cfg == nil {
		return config.LoadRepoConfigWith(cd, checkoutPath)
	}
	return cfg.ResolveRepoConfig(cd, checkoutPath)
}

// repoName derives a stable label for a repository unit — the repository display
// label (ProjectLabel), so a bare repo's representative shows "game server"
// rather than "game server/main". It falls back to the full picker Name when no
// ProjectLabel is carried (e.g. synthesized scans).
func repoName(scans []repoScan, rep *repoScan) string {
	if rep != nil {
		if rep.ProjectLabel != "" {
			return rep.ProjectLabel
		}
		if rep.Name != "" {
			return rep.Name
		}
	}
	if len(scans) > 0 {
		if scans[0].ProjectLabel != "" {
			return scans[0].ProjectLabel
		}
		return scans[0].Name
	}
	return ""
}

// rowsForStatic renders one repo group's rows from a fully resolved static plus
// the current volatile overlay, taking the single per-build store snapshot. It is
// the unexported seam tests drive with a hand-built static, mirroring what
// BuildSnapshot does per group after deriving statics fork-free from markers
// (ADR-0143). The public surface is snapshot-in, rows-out.
func rowsForStatic(d *Deps, cfg *config.Config, st repoStatic) ([]Row, error) {
	var delays []time.Duration
	if qcfg, qerr := cfg.ResolveQueue(); qerr == nil {
		delays = qcfg.CrashRetryDelays
	}
	snap, err := newSnapshot(d)
	if err != nil {
		return nil, err
	}
	return rowsFromStatic(d, cfg, snap, delays, d.now().UTC(), st)
}

// rowsFromStatic builds a repo group's rows from its static resolution plus the
// current volatile state: task statuses (refresh), runtime locks, and
// daemon-state columns. It forks no git — the static side is marker/config
// derived (ADR-0060) and this overlay is cheap file/store reads.
func rowsFromStatic(d *Deps, cfg *config.Config, snap *snapshot, delays []time.Duration, now time.Time, st repoStatic) ([]Row, error) {
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
	intents := worktreeIntents(d, st.defPath)
	backoff := d.setBackoffLookup(st.repoCommonDir, delays, now)
	var rows []Row
	for _, taskRow := range refresh.Rows {
		bnd, hasBinding := snap.bindingFor(st.repoKey, taskRow.ID)
		bound := hasBinding && strings.TrimSpace(bnd.RuntimePath) != ""
		doneStillManagedBound := taskRow.Status == tasks.StatusDone && bound && bnd.Provisioned
		orphanedSet := orphaned(d, bnd, hasBinding)
		if !ShowRow(taskRow, d.IncludeDone) {
			continue
		}
		wt := worktree(d, snap, intents, st.repoKey, taskRow.ID, taskRow.Status, bnd, bound)
		parked := false
		if backoff != nil {
			parked = backoff(taskRow.ID)
		}
		liveDrainSet := liveDrain(snap, st.repoKey, taskRow.ID, wt.runtimePath)
		// A live drain lights the trailing ● indicator (ADR-0111); parked and
		// config-error ride the STATUS cell. The mutual exclusion the retired
		// single-string DRAIN cell enforced is preserved by gating the config-error
		// probe on a set that is neither live-drained nor parked.
		configErr := ""
		if !liveDrainSet && !parked {
			if st.configErr != "" && !hasBinding {
				// Bare repo with no declared trunk: an unbound set has no integration
				// target to route to (ADR-0060). A bound set is still drainable via its
				// binding, so it is left untouched.
				configErr = st.configErr
			} else if taskRow.Status == tasks.StatusReady {
				// An unsatisfiable worktree directive is a static config defect
				// (ADR-0059). Read the registration intent first (a store read, no git);
				// only a set that carries a directive pays the read-only probe.
				if intent, _ := tasks.RegisteredWorktreeIntent(d.Tasks, st.defPath, taskRow.ID); intent != nil {
					if msg := d.probeDirective(staticProjectPath(st), taskRow.ID); msg != "" {
						configErr = msg
					}
				}
			}
		}
		rows = append(rows, Row{
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
				Orphaned:              orphanedSet,
				Bound:                 bound,
				PaneID:                paneID(snap, st.repoKey, taskRow.ID),
				LiveDrain:             liveDrainSet,
			},
			Project:       st.projectName,
			Started:       taskRow.Started,
			VerifiedAtSHA: taskRow.VerifiedAtSHA,
			Worktree:      wt.label,
			CursorKey:     st.projectName + "\x00" + taskRow.ID,
			DestKind:      wt.DestKind,
		})
	}
	mapRows, err := mapRowsFromStatic(d, st)
	if err != nil {
		return nil, err
	}
	rows = append(rows, mapRows...)
	return rows, nil
}

// mapRowsFromStatic walks one repo's wayfinder/ maps and returns Work dashboard
// rows for active, non-archived maps (ADR-0130). Done, abandoned, archived, and
// malformed maps are hidden.
func mapRowsFromStatic(d *Deps, st repoStatic) ([]Row, error) {
	storageDir := st.storageDir
	if storageDir == "" && st.defPath != "" {
		storageDir = filepath.Dir(st.defPath)
	}
	if storageDir == "" {
		return nil, nil
	}
	wd := &wayfinder.Deps{FS: d.Tasks.FS, Tasks: d.Tasks}
	maps, err := wayfinder.ScanMapsInStorage(wd, storageDir)
	if err != nil {
		return nil, err
	}
	var rows []Row
	for _, m := range maps {
		if !mapVisible(m) {
			continue
		}
		counts := wayfinder.CountTickets(m.Tickets)
		frontier := len(wayfinder.Frontier(m.Tickets))
		rows = append(rows, Row{
			SetRef: SetRef{
				SetID:         m.ID,
				DefPath:       st.defPath,
				StatePath:     st.statePath,
				RepoKey:       st.repoKey,
				RepoCommonDir: st.repoCommonDir,
				ProjectPath:   staticProjectPath(st),
				ProjectName:   st.projectName,
			},
			Project:     st.projectName,
			IsMap:       true,
			MapOpen:     counts.Open,
			MapFrontier: frontier,
			CursorKey:   st.projectName + "\x00map\x00" + m.ID,
		})
	}
	return rows, nil
}

// mapVisible reports whether a Map should appear on the Work dashboard: active
// and not archived. Done, abandoned, archived, and malformed maps are hidden
// (ADR-0130).
func mapVisible(m wayfinder.Map) bool {
	if m.Archived || m.Malformed {
		return false
	}
	return m.Status == wayfinder.MapActive
}

// staticProjectPath returns the repo group's representative checkout, the path
// every bind/drain sub-action runs git against. It is empty only for a bare repo
// with no resolvable representative.
func staticProjectPath(st repoStatic) string {
	if st.rep == nil {
		return ""
	}
	return st.rep.ProjectPath
}

// worktreeView is the resolved destination column for one row.
type worktreeView struct {
	label       string
	runtimePath string
	DestKind    DestKind
}

// worktreeIntents loads seeded worktree directives for one definition path in a
// single store read, keyed by set ID. The per-row destination column consults
// this map instead of reopening the store for each unbound row.
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
			branch = headBranchFromCheckout(d.Tasks, bnd.RuntimePath, "")
		}
		branch = formatBranch(branch)
		kind := DestBound
		if status == tasks.StatusDone && bnd.Provisioned {
			kind = DestDoneManagedBound
		}
		return worktreeView{label: branch, runtimePath: bnd.RuntimePath, DestKind: kind}
	}
	intent := intents[setID]
	if intent != nil && intent.Managed {
		return worktreeView{label: DestLabelManagedWt, DestKind: DestManagedDirective}
	}
	return worktreeView{label: DestLabelNeedsBind, DestKind: DestNeedsBind}
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
// reading the per-build snapshot's live-drain map instead of reopening the
// runtime lock per row. It is the structured boolean the sort tier, header count,
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
