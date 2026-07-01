package queue

import (
	"bytes"
	"fmt"
	"io"
	"path/filepath"
	"strings"

	"github.com/glebglazov/pop/config"
	"github.com/glebglazov/pop/internal/deps"
	"github.com/glebglazov/pop/project"
	"github.com/glebglazov/pop/tasks"
	"github.com/glebglazov/pop/tasks/binding"
)

// UnparkSet clears the park on a dashboard row's Task set by appending a
// park-clear event keyed by the repository's common dir and set id. The row must
// carry a resolved common dir (parked rows always do).
func UnparkSet(d *Deps, ref SetRef) error {
	if d == nil || d.Tasks == nil {
		return fmt.Errorf("missing task dependencies")
	}
	commonDir := ref.RepoCommonDir
	if strings.TrimSpace(commonDir) == "" {
		id, err := tasks.ResolveRepositoryIdentity(d.Tasks, ref.RuntimePath)
		if err != nil {
			return err
		}
		commonDir = id.CommonDir
	}
	return tasks.RecordParkClear(d.Tasks, commonDir, ref.SetID)
}

// StatusDetailLines renders the same per-set task status detail as
// `pop tasks status <set>` for a dashboard row.
func StatusDetailLines(d *Deps, ref SetRef) ([]string, error) {
	if d == nil {
		d = DefaultDeps()
	}
	if d.Tasks == nil {
		d.Tasks = tasks.DefaultDeps()
	}
	refresh, err := d.refresh(ref.DefPath)
	if err != nil {
		return nil, err
	}
	detailRow := tasks.FindRow(refresh, ref.SetID)
	var buf bytes.Buffer
	tasks.RenderTaskSetDetail(&buf, ref.SetID, detailRow, refresh.Manifests[ref.SetID])
	text := strings.TrimRight(buf.String(), "\n")
	if text == "" {
		text = fmt.Sprintf("%s: no status detail available", ref.SetID)
	}
	lines := strings.Split(text, "\n")
	if strings.TrimSpace(ref.RuntimePath) != "" {
		lines = append([]string{"checkout: " + ref.RuntimePath, ""}, lines...)
	}
	return lines, nil
}

type DashboardDrainResult struct {
	PaneID      string
	RuntimePath string
}

// LaunchDrain manually launches the highlighted dashboard row through
// the same Queue provisioning and tmux spawn path used by the supervisor.
func LaunchDrain(d *Deps, cfg *config.Config, ref SetRef) (DashboardDrainResult, error) {
	if d == nil {
		d = DefaultDeps()
	}
	if d.Tasks == nil {
		d.Tasks = tasks.DefaultDeps()
	}
	if d.Project == nil {
		d.Project = project.DefaultDeps()
	}
	scans, err := dashboardScansForDefinition(d, cfg, ref.DefPath)
	if err != nil {
		return DashboardDrainResult{}, err
	}
	if len(scans) == 0 {
		return DashboardDrainResult{}, fmt.Errorf("task set %s is no longer in a registered queue project", ref.SetID)
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
		TaskSetID: ref.SetID,
	}
	if b, ok := bindingForSet(d.Tasks, repoKey, ref.SetID); ok && strings.TrimSpace(b.RuntimePath) != "" {
		if err := validateBoundWorktree(d, scans[0].ProjectPath, b); err != nil {
			return DashboardDrainResult{}, fmt.Errorf("bound worktree for %s is invalid (%v); repair git state or run `pop tasks unbind-worktree`", ref.SetID, err)
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

// PreviewDrain switches the active tmux client to the pane associated
// with the highlighted row. Rows without a recorded pane intentionally no-op.
func PreviewDrain(d *Deps, ref SetRef) error {
	if strings.TrimSpace(ref.PaneID) == "" {
		return nil
	}
	if d == nil {
		d = DefaultDeps()
	}
	if d.Tmux == nil {
		d.Tmux = deps.NewRealTmux()
	}
	if _, err := d.Tmux.Command("select-pane", "-t", ref.PaneID); err != nil {
		return err
	}
	_, err := d.Tmux.Command("switch-client", "-t", ref.PaneID)
	return err
}

// UnbindWorktree releases the highlighted set's worktree binding
// through the same unbind implementation used by `pop tasks unbind-worktree`.
// The dashboard supplies its own inline confirmation, so the command-level
// prompt is skipped here.
func UnbindWorktree(d *Deps, cfg *config.Config, ref SetRef) (AbandonResult, error) {
	key := ""
	if strings.TrimSpace(ref.RepoKey) != "" {
		key = setScopedKey(ref.RepoKey, ref.SetID)
	}
	return AbandonBindingWithOptions(d, cfg, key, ref.SetID, io.Discard, AbandonOptions{Yes: true, In: tasks.NonInteractiveReader{}})
}

// BindWorktreeEntries returns the inline bind picker entries for the
// highlighted dashboard row: every existing worktree in the row's repository,
// followed by the pop-native creation entry.
func BindWorktreeEntries(d *Deps, cfg *config.Config, ref SetRef) ([]dashboardBindEntry, error) {
	scans, _, err := dashboardBindContext(d, cfg, ref)
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

// BindBaseRefs lists local and remote branch refs for the create-new
// flow, with main/master variants first.
func BindBaseRefs(d *Deps, cfg *config.Config, ref SetRef) ([]string, error) {
	scans, _, err := dashboardBindContext(d, cfg, ref)
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

// AdoptWorktree binds ref.SetID to an existing checkout. The dashboard
// action is deliberate, so idle re-pointing uses Force without a second prompt.
func AdoptWorktree(d *Deps, cfg *config.Config, ref SetRef, checkoutPath string) (BindWorktreeResult, error) {
	if err := refuseDashboardBindWhileLocked(d, ref); err != nil {
		return BindWorktreeResult{}, err
	}
	return BindWorktree(d, cfg, ref.SetID, checkoutPath, BindWorktreeOptions{Force: true}, io.Discard)
}

type DashboardCreateWorktreeResult struct {
	SetID       string
	RuntimePath string
	Branch      string
	BaseRef     string
}

// CreateWorktree creates a pop-managed worktree on a fresh branch and
// records a provisioned binding. It never opens or attaches a tmux session.
func CreateWorktree(d *Deps, cfg *config.Config, ref SetRef, baseRef, name string) (DashboardCreateWorktreeResult, error) {
	baseRef = strings.TrimSpace(baseRef)
	name = strings.TrimSpace(name)
	if baseRef == "" {
		return DashboardCreateWorktreeResult{}, fmt.Errorf("base ref is required")
	}
	if name == "" {
		return DashboardCreateWorktreeResult{}, fmt.Errorf("worktree name is required")
	}
	scans, repoKey, err := dashboardBindContext(d, cfg, ref)
	if err != nil {
		return DashboardCreateWorktreeResult{}, err
	}
	if err := refuseDashboardBindWhileLocked(d, ref); err != nil {
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
	key := setScopedKey(repoKey, ref.SetID)
	if err := binding.Put(d.Tasks, key, binding.Binding{RuntimePath: path, Branch: branch, Project: proj, Provisioned: true}); err != nil {
		return DashboardCreateWorktreeResult{}, err
	}
	return DashboardCreateWorktreeResult{SetID: ref.SetID, RuntimePath: path, Branch: branch, BaseRef: baseRef}, nil
}

// DrainTargetEntries builds the Drain target picker options for an
// unbound set (ADR-0052), in order: the repo's existing non-managed, unbound
// worktrees (adopt), "new managed worktree" (provision off the trunk), then the
// trunk itself (drain inline). The trunk-dependent options are omitted when no
// trunk resolves (an unconfigured bare repo). Managed worktrees, the trunk, and
// any worktree already bound to another set are excluded from the adopt list to
// preserve the 1:1 checkout↔set mapping.
func DrainTargetEntries(d *Deps, cfg *config.Config, ref SetRef) ([]dashboardDrainEntry, error) {
	scans, _, err := dashboardBindContext(d, cfg, ref)
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

// LaunchDrainTarget binds the chosen Drain target picker option and
// drains in one action (ADR-0052): an existing worktree is adopted, "new managed
// worktree" provisions a managed checkout forked from the trunk, and trunk leaves
// the set unbound so LaunchDrain routes it to the trunk. Once bound (or
// for trunk, immediately), it reuses LaunchDrain to spawn the drain.
func LaunchDrainTarget(d *Deps, cfg *config.Config, ref SetRef, target dashboardDrainEntry) (DashboardDrainResult, error) {
	switch target.Kind {
	case drainTargetWorktree:
		if _, err := AdoptWorktree(d, cfg, ref, target.Path); err != nil {
			return DashboardDrainResult{}, err
		}
	case drainTargetNewManaged:
		if _, err := ProvisionManagedWorktree(d, cfg, ref); err != nil {
			return DashboardDrainResult{}, err
		}
	case drainTargetTrunk:
		// Leave the set unbound: LaunchDrain routes to the representative
		// checkout (the trunk) and records no binding — a trunk drain is inline.
	default:
		return DashboardDrainResult{}, fmt.Errorf("unknown drain target")
	}
	return LaunchDrain(d, cfg, ref)
}

// ProvisionManagedWorktree provisions a pop-managed worktree forked
// from the Trunk worktree's HEAD and records a provisioned binding, reusing the
// shared provisioning path (ADR-0052). It refuses a repo with no resolvable
// trunk and never opens or attaches a tmux session.
func ProvisionManagedWorktree(d *Deps, cfg *config.Config, ref SetRef) (DashboardCreateWorktreeResult, error) {
	scans, repoKey, err := dashboardBindContext(d, cfg, ref)
	if err != nil {
		return DashboardCreateWorktreeResult{}, err
	}
	if err := refuseDashboardBindWhileLocked(d, ref); err != nil {
		return DashboardCreateWorktreeResult{}, err
	}
	if b, ok := bindingForSet(d.Tasks, repoKey, ref.SetID); ok && strings.TrimSpace(b.RuntimePath) != "" {
		return DashboardCreateWorktreeResult{}, fmt.Errorf("task set %s is already bound; unbind first to retarget", ref.SetID)
	}
	trunkPath, bare, err := binding.ResolveTrunkPath(d.Tasks, cfg, scans[0].ProjectPath)
	if err != nil {
		return DashboardCreateWorktreeResult{}, err
	}
	if bare || strings.TrimSpace(trunkPath) == "" {
		return DashboardCreateWorktreeResult{}, fmt.Errorf("no Trunk worktree configured; set trunk = true in a global [repo.\"<path>\"] block")
	}
	b, err := binding.ProvisionWorktree(d.Tasks, binding.ManagedWorktreesRoot(d.Tasks), trunkPath, ref.SetID, d.now())
	if err != nil {
		return DashboardCreateWorktreeResult{}, err
	}
	proj := repoName(scans, nil)
	if rep, _, repErr := resolveRepresentative(d, cfg, scans); repErr == nil {
		proj = repoName(scans, rep)
	}
	b.Project = proj
	key := setScopedKey(repoKey, ref.SetID)
	if err := binding.Put(d.Tasks, key, b); err != nil {
		return DashboardCreateWorktreeResult{}, err
	}
	return DashboardCreateWorktreeResult{SetID: ref.SetID, RuntimePath: b.RuntimePath, Branch: b.Branch}, nil
}

func dashboardBindContext(d *Deps, cfg *config.Config, ref SetRef) ([]projectScan, string, error) {
	if d == nil {
		d = DefaultDeps()
	}
	if d.Tasks == nil {
		d.Tasks = tasks.DefaultDeps()
	}
	if d.Project == nil {
		d.Project = project.DefaultDeps()
	}
	// Fast path: a SetRef built by the live dashboard already carries its repo
	// group's resolved coordinates (the integration target checkout and repo
	// key), derived fork-free at build time (ADR-0060). Every bind/drain
	// sub-action consumes only scans[0].ProjectPath and the repo key, so reuse
	// them directly instead of re-forking `git rev-parse` across every registered
	// project — the sequential rescan that left the inline bind picker stuck on
	// "loading...".
	if ref.ProjectPath != "" && ref.RepoKey != "" {
		scan := projectScan{
			ProjectPath:    ref.ProjectPath,
			DefinitionPath: ref.DefPath,
			RuntimePath:    ref.ProjectPath,
			SessionName:    project.SessionNameWith(d.Project, ref.ProjectPath),
			RepoKey:        ref.RepoKey,
		}
		return []projectScan{scan}, ref.RepoKey, nil
	}
	scans, err := dashboardScansForDefinition(d, cfg, ref.DefPath)
	if err != nil {
		return nil, "", err
	}
	if len(scans) == 0 {
		return nil, "", fmt.Errorf("task set %s is no longer in a registered queue project", ref.SetID)
	}
	repoKey := ref.RepoKey
	if repoKey == "" {
		repoKey, err = scanRepoKey(d, scans[0])
		if err != nil {
			return nil, "", err
		}
	}
	return scans, repoKey, nil
}

func refuseDashboardBindWhileLocked(d *Deps, ref SetRef) error {
	if d == nil {
		d = DefaultDeps()
	}
	if d.Tasks == nil {
		d.Tasks = tasks.DefaultDeps()
	}
	paths := map[string]bool{}
	if strings.TrimSpace(ref.RuntimePath) != "" {
		paths[ref.RuntimePath] = true
	}
	if ref.RepoKey != "" {
		if b, ok := bindingForSet(d.Tasks, ref.RepoKey, ref.SetID); ok && b.RuntimePath != "" {
			paths[b.RuntimePath] = true
		}
	}
	for path := range paths {
		lock := d.readLock(path)
		if lock == nil || !lock.Locked {
			continue
		}
		if lock.Metadata == nil || lock.Metadata.SetID == "" || lock.Metadata.SetID == ref.SetID {
			return fmt.Errorf("refusing bind-worktree: %s is currently executing", ref.SetID)
		}
	}
	return nil
}
