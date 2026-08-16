package drain

import (
	"bytes"
	"fmt"
	"io"
	"path/filepath"
	"strings"
	"sync"

	"github.com/glebglazov/pop/config"
	tmuxmod "github.com/glebglazov/pop/internal/tmux"
	"github.com/glebglazov/pop/project"
	"github.com/glebglazov/pop/tasks"
	"github.com/glebglazov/pop/tasks/binding"
)

// UnparkSet clears the park on a dashboard row's Task set by appending a
// park-clear event keyed by the repository's common dir and set id. The row must
// carry a resolved common dir (parked rows always do).
func UnparkSet(d *Deps, row DashboardRow) error {
	if d == nil || d.Tasks == nil {
		return fmt.Errorf("missing task dependencies")
	}
	commonDir := row.RepoCommonDir
	if strings.TrimSpace(commonDir) == "" {
		id, err := tasks.ResolveRepositoryIdentity(d.Tasks, row.RuntimePath)
		if err != nil {
			return err
		}
		commonDir = id.CommonDir
	}
	return tasks.RecordParkClear(d.Tasks, commonDir, row.ID)
}

// StatusDetailLines renders the same per-set task status detail as
// `pop tasks status <set>` for a dashboard row.
func StatusDetailLines(d *Deps, row DashboardRow) ([]string, error) {
	if d == nil {
		d = DefaultDeps()
	}
	if d.Tasks == nil {
		d.Tasks = tasks.DefaultDeps()
	}
	refresh, err := d.refresh(row.DefPath)
	if err != nil {
		return nil, err
	}
	detailRow := tasks.FindRow(refresh, row.ID)
	var buf bytes.Buffer
	tasks.RenderTaskSetDetail(d.Tasks, &buf, row.ID, detailRow, refresh.Manifests[row.ID])
	text := strings.TrimRight(buf.String(), "\n")
	if text == "" {
		text = fmt.Sprintf("%s: no status detail available", row.ID)
	}
	lines := strings.Split(text, "\n")
	if strings.TrimSpace(row.RuntimePath) != "" {
		lines = append([]string{"checkout: " + row.RuntimePath, ""}, lines...)
	}
	return lines, nil
}

type DashboardDrainResult struct {
	PaneID      string
	Session     string
	RuntimePath string
}

// dashboardSetPaneCoords resolves the tmux session and working directory for a
// Task-set pane opened from the dashboard. Both come from one place — the checkout
// the set is bound to — so the pane lives in that checkout's session rather than in
// the session of the project the operator invoked the verb from (ADR-0180). An
// unbound set has no binding to follow and falls back to the repository's own
// checkout, which is where its drain would run inline.
//
// It resolves no project path and forks no git: deriving the session from the
// checkout is what let the resolveRepresentative fan-out be deleted from in front
// of a verb the operator is waiting on.
func dashboardSetPaneCoords(d *Deps, scans []projectScan, row DashboardRow, checkout string) (session, dir string, err error) {
	if d == nil {
		d = DefaultDeps()
	}
	if d.Project == nil {
		d.Project = project.DefaultDeps()
	}
	dir = strings.TrimSpace(checkout)
	if dir == "" {
		dir = strings.TrimSpace(row.RuntimePath)
	}
	if dir == "" {
		dir = strings.TrimSpace(row.ProjectPath)
	}
	if dir == "" && len(scans) > 0 {
		dir = strings.TrimSpace(scans[0].ProjectPath)
	}
	session, err = project.CheckoutSessionWith(d.Project, dir)
	if err != nil {
		return "", "", err
	}
	return session, dir, nil
}

// refuseUnusableBoundCheckout refuses a handoff verb whose set is bound to a
// checkout that is missing or that git no longer registers. Every verb calls it,
// not just drain: now that the pane's session is the bound checkout's, a verb that
// carried on would either name a session after a directory that is gone or fall
// back to the trunk — the silent mislocation ADR-0180 removes, arriving at the
// moment the operator is least able to notice it. An unbound set has nothing to
// validate and passes.
func refuseUnusableBoundCheckout(d *Deps, scans []projectScan, repoKey string, row DashboardRow) error {
	if strings.TrimSpace(repoKey) == "" || len(scans) == 0 {
		return nil
	}
	b, ok := BindingForSet(d.Tasks, repoKey, row.ID)
	if !ok || strings.TrimSpace(b.RuntimePath) == "" {
		return nil
	}
	if err := validateBoundWorktree(d, scans[0].ProjectPath, b); err != nil {
		return fmt.Errorf("bound worktree for %s is invalid (%v); repair git state or run `pop tasks unbind-worktree`", row.ID, err)
	}
	return nil
}

// LaunchDrain manually launches the highlighted dashboard row through
// the same Queue provisioning and tmux spawn path used by the supervisor.
func LaunchDrain(d *Deps, cfg *config.Config, row DashboardRow) (DashboardDrainResult, error) {
	if d == nil {
		d = DefaultDeps()
	}
	if d.Tasks == nil {
		d.Tasks = tasks.DefaultDeps()
	}
	if d.Project == nil {
		d.Project = project.DefaultDeps()
	}
	// Bound-set drains — every resume of a live or parked set — resolve entirely
	// from the row's carried coordinates, forking no git (ADR-0167). Only the
	// unbound branch below, which must choose a trunk among the repo's checkouts,
	// earns the project scan fan-out.
	scans, repoKey, err := dashboardBindContext(d, cfg, row)
	if err != nil {
		return DashboardDrainResult{}, err
	}
	dec := Decision{TaskSetID: row.ID}
	if b, ok := BindingForSet(d.Tasks, repoKey, row.ID); ok && strings.TrimSpace(b.RuntimePath) != "" {
		dec.Project = repoName(scans, nil)
		if err := refuseUnusableBoundCheckout(d, scans, repoKey, row); err != nil {
			return DashboardDrainResult{}, err
		}
		session, checkout, err := dashboardSetPaneCoords(d, scans, row, b.RuntimePath)
		if err != nil {
			return DashboardDrainResult{}, err
		}
		dec.scan = projectScan{
			Name:           dec.Project,
			ProjectPath:    checkout,
			DefinitionPath: scans[0].DefinitionPath,
			RuntimePath:    b.RuntimePath,
			SessionName:    session,
			RepoKey:        repoKey,
		}
	} else {
		// An unbound row drains inline at the representative (the trunk), which is
		// chosen by comparing every checkout registered for this definition path —
		// the one question a single carried scan cannot answer. Provisioning a
		// managed worktree or adopting an existing one is the Drain target picker's
		// job (LaunchDrainTarget), which binds before this runs.
		allScans, err := dashboardScansForDefinition(d, cfg, row.DefPath)
		if err != nil {
			return DashboardDrainResult{}, err
		}
		if len(allScans) == 0 {
			return DashboardDrainResult{}, fmt.Errorf("task set %s is no longer in a registered project", row.ID)
		}
		rep, bare, err := resolveRepresentative(d, cfg, allScans)
		if err != nil {
			return DashboardDrainResult{}, err
		}
		if rep == nil {
			if bare {
				return DashboardDrainResult{}, fmt.Errorf("%s", RepoScanReason)
			}
			return DashboardDrainResult{}, fmt.Errorf("no Trunk worktree configured; set trunk = true in a global [repo.\"<path>\"] block")
		}
		dec.Project = repoName(allScans, rep)
		dec.scan = *rep
	}

	// Pin implement to the resolved checkout so a reused pane cannot re-resolve
	// to its stale cwd — the same contract as supervisor-spawned drains.
	dec.pinRuntimePath = true

	if d.Tmux == nil {
		d.Tmux = tmuxmod.New(config.ConfiguredTmuxSocket(), config.ConfiguredTmuxInclude())
	}
	// An already-running drain pane for this set is a jump target: focus it
	// rather than re-sending implement into the live process (ADR-0158). An
	// idle tagged pane (bare shell) falls through so EnsureTaggedPane respawns.
	paneID, err := runningTaggedPane(d.Tmux, dec.scan.SessionName, tmuxmod.TagSet, row.ID)
	if err != nil {
		return DashboardDrainResult{}, err
	} else if paneID != "" {
		return DashboardDrainResult{PaneID: paneID, Session: dec.scan.SessionName, RuntimePath: dec.scan.RuntimePath}, nil
	}

	spawn, err := SpawnWithResult(d, dec)
	if err != nil {
		return DashboardDrainResult{}, err
	}
	if err := recordDrainPane(d, dec, spawn.PaneID, "dashboard"); err != nil {
		return DashboardDrainResult{}, err
	}
	return DashboardDrainResult{PaneID: spawn.PaneID, Session: dec.scan.SessionName, RuntimePath: dec.scan.RuntimePath}, nil
}

// LaunchVerify spawns a Verifier pane on the dashboard row's set (ADR-0123). It
// is the lighter counterpart to LaunchDrain: it runs `pop tasks verify <set>`
// pinned to the row's runtime path through EnsureTaggedPane with TagVerify,
// but records neither a Runtime execution lock, a spawn intent, nor a DrainPane —
// verify is not a drain, so the `●` live-drain indicator must stay dark. An
// empty runtime path omits the flag and lets `pop tasks verify` default to the
// project root, matching the drain when no worktree is ready.
func LaunchVerify(d *Deps, cfg *config.Config, row DashboardRow) (DashboardDrainResult, error) {
	if d == nil {
		d = DefaultDeps()
	}
	if d.Tasks == nil {
		d.Tasks = tasks.DefaultDeps()
	}
	if d.Project == nil {
		d.Project = project.DefaultDeps()
	}
	scans, repoKey, err := dashboardBindContext(d, cfg, row)
	if err != nil {
		return DashboardDrainResult{}, err
	}
	if err := refuseUnusableBoundCheckout(d, scans, repoKey, row); err != nil {
		return DashboardDrainResult{}, err
	}
	// The pane spawns into the checkout the verdict must judge: the row's runtime
	// path when it resolves to one (a bound worktree or trunk), else the project
	// root. EnsureTaggedPane reuses this set's existing tagged pane, so verify
	// lands in the same session the set's drain would.
	session, checkout, err := dashboardSetPaneCoords(d, scans, row, row.RuntimePath)
	if err != nil {
		return DashboardDrainResult{}, err
	}
	if d.Tmux == nil {
		d.Tmux = tmuxmod.New(config.ConfiguredTmuxSocket(), config.ConfiguredTmuxInclude())
	}
	// An already-running verify pane for this set is a jump target: focus it
	// rather than re-sending verify into the live process (ADR-0158). An idle
	// tagged pane (bare shell) falls through so EnsureTaggedPane respawns.
	if paneID, err := runningTaggedPane(d.Tmux, session, tmuxmod.TagVerify, row.ID); err != nil {
		return DashboardDrainResult{}, err
	} else if paneID != "" {
		return DashboardDrainResult{PaneID: paneID, Session: session, RuntimePath: row.RuntimePath}, nil
	}
	command := fmt.Sprintf("pop tasks verify %s", shellQuote(row.ID))
	if strings.TrimSpace(row.RuntimePath) != "" {
		command += " --task-runtime-path " + shellQuote(row.RuntimePath)
	}
	paneID, err := tmuxmod.EnsureTaggedPane(d.Tmux, tmuxmod.TagVerify, session, tmuxmod.DrainWindow, checkout, row.ID, command)
	if err != nil {
		return DashboardDrainResult{}, err
	}
	if err := d.Tmux.SetPaneTitle(paneID, VerifyPaneTitle(row.ID)); err != nil {
		return DashboardDrainResult{}, err
	}
	return DashboardDrainResult{PaneID: paneID, Session: session, RuntimePath: row.RuntimePath}, nil
}

func dashboardScansForDefinition(d *Deps, cfg *config.Config, defPath string) ([]projectScan, error) {
	projects, err := tasks.ListPickerProjectsWith(d.Project, cfg)
	if err != nil {
		return nil, err
	}
	// Each resolveScan forks git, so a serial loop costs one process per
	// registered project — 2.7s across 55 projects, all of it in front of a verb
	// the operator is waiting on (ADR-0167). The scans are independent, so fan
	// out and keep the results in project order.
	type scanResult struct {
		scan projectScan
		err  error
	}
	results := make([]scanResult, len(projects))
	var wg sync.WaitGroup
	for i, p := range projects {
		wg.Add(1)
		go func(idx int, ep project.ExpandedProject) {
			defer wg.Done()
			scan, err := resolveScan(d, ep)
			results[idx] = scanResult{scan: scan, err: err}
		}(i, p)
	}
	wg.Wait()
	var scans []projectScan
	for _, r := range results {
		if r.err != nil {
			if outsideQueueScopeResolveError(r.err) {
				continue
			}
			return nil, r.err
		}
		if r.scan.DefinitionPath == defPath {
			scans = append(scans, r.scan)
		}
	}
	return scans, nil
}

// LaunchAssist opens or reuses an Assist session pane for the dashboard row's
// set in the project's pop-work window. A pane already tagged for the set whose
// command is still running is returned without spawning a twin or re-sending;
// an idle tagged pane (bare shell) is respawned. Otherwise a fresh pane runs
// `pop tasks assist` pinned to the row's binding-first runtime checkout. Focus
// and quit belong to the dashboard handoff path (ADR-0158).
func LaunchAssist(d *Deps, cfg *config.Config, row DashboardRow) (DashboardDrainResult, error) {
	if d == nil {
		d = DefaultDeps()
	}
	if d.Tasks == nil {
		d.Tasks = tasks.DefaultDeps()
	}
	if d.Project == nil {
		d.Project = project.DefaultDeps()
	}
	if d.Tmux == nil {
		d.Tmux = tmuxmod.New(config.ConfiguredTmuxSocket(), config.ConfiguredTmuxInclude())
	}
	scans, repoKey, err := dashboardBindContext(d, cfg, row)
	if err != nil {
		return DashboardDrainResult{}, err
	}
	if err := refuseUnusableBoundCheckout(d, scans, repoKey, row); err != nil {
		return DashboardDrainResult{}, err
	}
	projectPath := scans[0].ProjectPath
	if strings.TrimSpace(row.ProjectPath) != "" {
		projectPath = row.ProjectPath
	}
	runtimeOverride := strings.TrimSpace(row.RuntimePath)
	if runtimeOverride == "" {
		var resolveErr error
		runtimeOverride, resolveErr = binding.ResolveCommandRuntime(d.Tasks, projectPath, row.ID, "")
		if resolveErr != nil {
			return DashboardDrainResult{}, resolveErr
		}
	}
	loadConfig := config.Load
	if d.LoadConfig != nil {
		loadConfig = d.LoadConfig
	}
	runtimePath, _, err := tasks.ValidateAssistLaunch(d.Tasks, d.Project, loadConfig, tasks.AssistOptions{
		ResolveInput: tasks.ResolveInput{
			CWD:             projectPath,
			RuntimeOverride: runtimeOverride,
		},
		TaskSetID: row.ID,
	})
	if err != nil {
		return DashboardDrainResult{}, err
	}

	session, checkout, err := dashboardSetPaneCoords(d, scans, row, runtimePath)
	if err != nil {
		return DashboardDrainResult{}, err
	}

	if paneID, err := runningTaggedPane(d.Tmux, session, tmuxmod.TagAssist, row.ID); err != nil {
		return DashboardDrainResult{}, err
	} else if paneID != "" {
		return DashboardDrainResult{PaneID: paneID, Session: session, RuntimePath: runtimePath}, nil
	}

	command := fmt.Sprintf("pop tasks assist %s", shellQuote(row.ID))
	if strings.TrimSpace(runtimePath) != "" {
		command += " --task-runtime-path " + shellQuote(runtimePath)
	}
	paneID, err := tmuxmod.EnsureTaggedPane(d.Tmux, tmuxmod.TagAssist, session, tmuxmod.DrainWindow, checkout, row.ID, command)
	if err != nil {
		return DashboardDrainResult{}, err
	}
	if err := d.Tmux.SetPaneTitle(paneID, AssistPaneTitle(row.ID, attendedEntryLabel(cfg))); err != nil {
		return DashboardDrainResult{}, err
	}
	return DashboardDrainResult{PaneID: paneID, Session: session, RuntimePath: runtimePath}, nil
}

func activityPaneTitle(setID, activity string) string {
	return setID + "-" + activity
}

func DrainPaneTitle(setID string) string {
	return activityPaneTitle(setID, "drain")
}

func VerifyPaneTitle(setID string) string {
	return activityPaneTitle(setID, "verify")
}

func FoldPaneTitle(setID string) string {
	return activityPaneTitle(setID, "fold")
}

func AssistPaneTitle(setID string, entryLabel string) string {
	base := activityPaneTitle(setID, "assist")
	entryLabel = strings.TrimSpace(entryLabel)
	if entryLabel == "" {
		return base
	}
	return base + " · " + entryLabel
}

// attendedEntryLabel is the shared one-line render of the attended entry the
// merged config resolves to (ADR-0196 decision 9). The spawned session resolves
// the same merged config for itself, so the title and the launch agree without
// this process passing anything across the boundary.
func attendedEntryLabel(cfg *config.Config) string {
	return tasks.FormatAgentEntry(tasks.EffectiveAttendedEntry(cfg))
}

// LaunchFold spawns `pop tasks fold <set>` under TagFold in the project's
// pop-work window (ADR-0158). The Fold conflict prompt lives in that pane so
// it outlives the dashboard. An already-running fold pane for the set is a
// jump target — focus it rather than re-sending fold into the live process.
// An idle tagged pane (bare shell) is respawned. Dashboard-side PreflightFold
// still refuses ineligible rows before this runs.
func LaunchFold(d *Deps, cfg *config.Config, row DashboardRow) (DashboardDrainResult, error) {
	if d == nil {
		d = DefaultDeps()
	}
	if d.Tasks == nil {
		d.Tasks = tasks.DefaultDeps()
	}
	if d.Project == nil {
		d.Project = project.DefaultDeps()
	}
	if d.Tmux == nil {
		d.Tmux = tmuxmod.New(config.ConfiguredTmuxSocket(), config.ConfiguredTmuxInclude())
	}
	scans, repoKey, err := dashboardBindContext(d, cfg, row)
	if err != nil {
		return DashboardDrainResult{}, err
	}
	if err := refuseUnusableBoundCheckout(d, scans, repoKey, row); err != nil {
		return DashboardDrainResult{}, err
	}
	session, checkout, err := dashboardSetPaneCoords(d, scans, row, row.RuntimePath)
	if err != nil {
		return DashboardDrainResult{}, err
	}
	if paneID, err := runningTaggedPane(d.Tmux, session, tmuxmod.TagFold, row.ID); err != nil {
		return DashboardDrainResult{}, err
	} else if paneID != "" {
		return DashboardDrainResult{PaneID: paneID, Session: session, RuntimePath: row.RuntimePath}, nil
	}
	command := fmt.Sprintf("pop tasks fold %s", shellQuote(row.ID))
	paneID, err := tmuxmod.EnsureTaggedPane(d.Tmux, tmuxmod.TagFold, session, tmuxmod.DrainWindow, checkout, row.ID, command)
	if err != nil {
		return DashboardDrainResult{}, err
	}
	if err := d.Tmux.SetPaneTitle(paneID, FoldPaneTitle(row.ID)); err != nil {
		return DashboardDrainResult{}, err
	}
	return DashboardDrainResult{PaneID: paneID, Session: session, RuntimePath: row.RuntimePath}, nil
}

// LaunchShell opens a fresh untagged Runtime shell pane in the set's checkout
// (ADR-0158). Every press yields a new pane — shells are never tagged or
// reused — so the operator's process outlives the dashboard exiting.
func LaunchShell(d *Deps, cfg *config.Config, row DashboardRow) (DashboardDrainResult, error) {
	return LaunchShellIn(d, cfg, row, row.RuntimePath)
}

// LaunchShellIn is the same spawn into an explicitly named directory: the shell
// verb's working directory is the Work kind's answer (a task set's bound
// checkout, a Map's repository), not something the launcher re-derives from the
// Task-set binding it was handed.
func LaunchShellIn(d *Deps, cfg *config.Config, row DashboardRow, dir string) (DashboardDrainResult, error) {
	if d == nil {
		d = DefaultDeps()
	}
	if d.Project == nil {
		d.Project = project.DefaultDeps()
	}
	if d.Tmux == nil {
		d.Tmux = tmuxmod.New(config.ConfiguredTmuxSocket(), config.ConfiguredTmuxInclude())
	}
	checkout := strings.TrimSpace(dir)
	if checkout == "" {
		return DashboardDrainResult{}, fmt.Errorf("no checkout bound to this task set")
	}
	scans, repoKey, err := dashboardBindContext(d, cfg, row)
	if err != nil {
		return DashboardDrainResult{}, err
	}
	if err := refuseUnusableBoundCheckout(d, scans, repoKey, row); err != nil {
		return DashboardDrainResult{}, err
	}
	session, dir, err := dashboardSetPaneCoords(d, scans, row, checkout)
	if err != nil {
		return DashboardDrainResult{}, err
	}
	paneID, err := tmuxmod.SpawnFreshPane(d.Tmux, session, dir, "")
	if err != nil {
		return DashboardDrainResult{}, err
	}
	return DashboardDrainResult{PaneID: paneID, Session: session, RuntimePath: checkout}, nil
}

// UnbindWorktree releases the highlighted set's worktree binding
// through the same unbind implementation used by `pop tasks unbind-worktree`.
// The dashboard supplies its own inline confirmation, so the command-level
// prompt is skipped here.
func UnbindWorktree(d *Deps, cfg *config.Config, row DashboardRow) (AbandonResult, error) {
	key := ""
	if strings.TrimSpace(row.RepoKey) != "" {
		key = SetScopedKey(row.RepoKey, row.ID)
	}
	return AbandonBindingWithOptions(d, cfg, key, row.ID, io.Discard, AbandonOptions{Yes: true, In: tasks.NonInteractiveReader{}})
}

// BindWorktreeEntries returns the inline bind picker entries for the
// highlighted dashboard row: every existing worktree in the row's repository,
// followed by the managed-intent entry and the pop-native creation entry. The
// managed entry forks a new worktree from the Trunk worktree and binds it
// immediately (ADR-0147), with no drain required.
func BindWorktreeEntries(d *Deps, cfg *config.Config, row DashboardRow) ([]BindEntry, error) {
	scans, _, err := dashboardBindContext(d, cfg, row)
	if err != nil {
		return nil, err
	}
	out, err := d.Tasks.Git.CommandInDir(scans[0].ProjectPath, "worktree", "list", "--porcelain")
	if err != nil {
		return nil, fmt.Errorf("list worktrees: %w", err)
	}
	worktrees := parseDashboardWorktrees(out)
	entries := make([]BindEntry, 0, len(worktrees)+1)
	for _, wt := range worktrees {
		label := wt.Name
		if wt.Branch != "" {
			label = fmt.Sprintf("%s (%s)", wt.Name, wt.Branch)
		}
		entries = append(entries, BindEntry{Label: label, Path: wt.Path, Branch: wt.Branch})
	}
	entries = append(entries, BindEntry{Label: "＋ Managed worktree (fork from trunk)", Managed: true})
	entries = append(entries, BindEntry{Label: "＋ Create new worktree", Create: true})
	return entries, nil
}

// BindBaseRefs lists local and remote branch refs for the create-new
// flow, with main/master variants first.
func BindBaseRefs(d *Deps, cfg *config.Config, row DashboardRow) ([]string, error) {
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

// AdoptWorktree binds the row's set to an existing checkout. The dashboard
// action is deliberate, so idle re-pointing uses Force without a second prompt.
func AdoptWorktree(d *Deps, cfg *config.Config, row DashboardRow, checkoutPath string) (BindWorktreeResult, error) {
	if err := refuseDashboardBindWhileLocked(d, row); err != nil {
		return BindWorktreeResult{}, err
	}
	return BindWorktree(d, cfg, row.ID, checkoutPath, BindWorktreeOptions{Force: true, ProjectName: row.Project}, io.Discard)
}

// BindManagedWorktree provisions a managed worktree for the row's set eagerly —
// the interactive twin of `bind-worktree --managed` (ADR-0147). The dashboard
// action is deliberate, so a set already bound elsewhere is re-pointed without
// a second prompt (Force), dropping the old binding forget-only before
// provisioning the new checkout — exactly like AdoptWorktree.
func BindManagedWorktree(d *Deps, cfg *config.Config, row DashboardRow) (BindWorktreeResult, error) {
	if err := refuseDashboardBindWhileLocked(d, row); err != nil {
		return BindWorktreeResult{}, err
	}
	scans, _, err := dashboardBindContext(d, cfg, row)
	if err != nil {
		return BindWorktreeResult{}, err
	}
	return BindWorktree(d, cfg, row.ID, scans[0].ProjectPath, BindWorktreeOptions{Managed: true, Force: true, ProjectName: row.Project}, io.Discard)
}

type DashboardCreateWorktreeResult struct {
	SetID       string
	RuntimePath string
	Branch      string
	BaseRef     string
}

// CreateWorktree creates a pop-managed worktree on a fresh branch and records
// a binding whose Provisioned bit is derived from its location under the
// managed-worktree root. It never opens or attaches a tmux session.
func CreateWorktree(d *Deps, cfg *config.Config, row DashboardRow, baseRef, name string) (DashboardCreateWorktreeResult, error) {
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
	path := filepath.Join(binding.ManagedWorktreesRoot(d.Tasks), repoKey, binding.SafeComponent(name))
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
	key := SetScopedKey(repoKey, row.ID)
	if err := binding.Put(d.Tasks, key, binding.Adopt(d.Tasks, path, branch, proj)); err != nil {
		return DashboardCreateWorktreeResult{}, err
	}
	return DashboardCreateWorktreeResult{SetID: row.ID, RuntimePath: path, Branch: branch, BaseRef: baseRef}, nil
}

// DrainTargetEntries builds the Drain target picker options for an
// unbound set (ADR-0052), in order: the repo's existing non-managed, unbound
// worktrees (adopt), "new managed worktree" (provision off the trunk), then the
// trunk itself (drain inline). The trunk-dependent options are omitted when no
// trunk resolves (an unconfigured bare repo). Managed worktrees, the trunk, and
// any worktree already bound to another set are excluded from the adopt list to
// preserve the 1:1 checkout↔set mapping.
func DrainTargetEntries(d *Deps, cfg *config.Config, row DashboardRow) ([]DrainEntry, error) {
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
	// Every managed root, not just the current one: a worktree still waiting for
	// the gated root move is pop-managed too, and offering it here would let a
	// human adopt a checkout pop is about to relocate.
	var managedRoots []string
	for _, root := range binding.ManagedWorktreeRoots(d.Tasks) {
		managedRoots = append(managedRoots, bestEffortCanon(d, root))
	}

	var entries []DrainEntry
	for _, wt := range worktrees {
		canon := bestEffortCanon(d, wt.Path)
		if hasTrunk && canon == canonTrunk {
			continue // the trunk is offered as its own option
		}
		if pathUnderAny(canon, managedRoots) {
			continue // a pop-managed worktree
		}
		if bound[canon] {
			continue // already bound to another set (1:1 checkout↔set)
		}
		label := wt.Name
		if wt.Branch != "" {
			label = fmt.Sprintf("%s (%s)", wt.Name, wt.Branch)
		}
		entries = append(entries, DrainEntry{Label: label, Kind: DrainTargetWorktree, Path: wt.Path, Branch: wt.Branch})
	}
	if hasTrunk {
		entries = append(entries, DrainEntry{Label: "＋ New managed worktree (fork from trunk)", Kind: DrainTargetNewManaged})
		entries = append(entries, DrainEntry{Label: "Trunk worktree (drain inline)", Kind: DrainTargetTrunk, Path: trunkPath})
	}
	return entries, nil
}

// LaunchDrainTarget binds the chosen Drain target picker option and
// drains in one action (ADR-0052): an existing worktree is adopted, "new managed
// worktree" provisions a managed checkout forked from the trunk, and trunk leaves
// the set unbound so LaunchDrain routes it to the trunk. Once bound (or
// for trunk, immediately), it reuses LaunchDrain to spawn the drain.
func LaunchDrainTarget(d *Deps, cfg *config.Config, row DashboardRow, target DrainEntry) (DashboardDrainResult, error) {
	switch target.Kind {
	case DrainTargetWorktree:
		if _, err := AdoptWorktree(d, cfg, row, target.Path); err != nil {
			return DashboardDrainResult{}, err
		}
	case DrainTargetNewManaged:
		if _, err := ProvisionManagedWorktree(d, cfg, row); err != nil {
			return DashboardDrainResult{}, err
		}
	case DrainTargetTrunk:
		// Leave the set unbound: LaunchDrain routes to the representative
		// checkout (the trunk) and records no binding — a trunk drain is inline.
	default:
		return DashboardDrainResult{}, fmt.Errorf("unknown drain target")
	}
	return LaunchDrain(d, cfg, row)
}

// ProvisionManagedWorktree provisions a pop-managed worktree forked
// from the Trunk worktree's HEAD and records a provisioned binding, reusing the
// shared provisioning path (ADR-0052). It refuses a repo with no resolvable
// trunk and never opens or attaches a tmux session.
func ProvisionManagedWorktree(d *Deps, cfg *config.Config, row DashboardRow) (DashboardCreateWorktreeResult, error) {
	scans, repoKey, err := dashboardBindContext(d, cfg, row)
	if err != nil {
		return DashboardCreateWorktreeResult{}, err
	}
	if err := refuseDashboardBindWhileLocked(d, row); err != nil {
		return DashboardCreateWorktreeResult{}, err
	}
	if b, ok := BindingForSet(d.Tasks, repoKey, row.ID); ok && strings.TrimSpace(b.RuntimePath) != "" {
		return DashboardCreateWorktreeResult{}, fmt.Errorf("task set %s is already bound; unbind first to retarget", row.ID)
	}
	trunkPath, bare, err := binding.ResolveTrunkPath(d.Tasks, cfg, scans[0].ProjectPath)
	if err != nil {
		return DashboardCreateWorktreeResult{}, err
	}
	if bare || strings.TrimSpace(trunkPath) == "" {
		return DashboardCreateWorktreeResult{}, fmt.Errorf("no Trunk worktree configured; set trunk = true in a global [repo.\"<path>\"] block")
	}
	b, err := binding.ProvisionWorktree(d.Tasks, binding.ManagedWorktreesRoot(d.Tasks), trunkPath, row.ID, "HEAD", d.now())
	if err != nil {
		return DashboardCreateWorktreeResult{}, err
	}
	proj := repoName(scans, nil)
	if rep, _, repErr := resolveRepresentative(d, cfg, scans); repErr == nil {
		proj = repoName(scans, rep)
	}
	b.Project = proj
	key := SetScopedKey(repoKey, row.ID)
	if err := binding.Put(d.Tasks, key, b); err != nil {
		return DashboardCreateWorktreeResult{}, err
	}
	return DashboardCreateWorktreeResult{SetID: row.ID, RuntimePath: b.RuntimePath, Branch: b.Branch}, nil
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
	if row.ProjectPath != "" && row.RepoKey != "" {
		scan := projectScan{
			Name:           row.Project,
			ProjectPath:    row.ProjectPath,
			DefinitionPath: row.DefPath,
			RuntimePath:    row.ProjectPath,
			SessionName:    project.CheckoutSessionNameWith(d.Project, row.ProjectPath),
			RepoKey:        row.RepoKey,
		}
		return []projectScan{scan}, row.RepoKey, nil
	}
	scans, err := dashboardScansForDefinition(d, cfg, row.DefPath)
	if err != nil {
		return nil, "", err
	}
	if len(scans) == 0 {
		return nil, "", fmt.Errorf("task set %s is no longer in a registered project", row.ID)
	}
	repoKey := row.RepoKey
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
	if strings.TrimSpace(row.RuntimePath) != "" {
		paths[row.RuntimePath] = true
	}
	if row.RepoKey != "" {
		if b, ok := BindingForSet(d.Tasks, row.RepoKey, row.ID); ok && b.RuntimePath != "" {
			paths[b.RuntimePath] = true
		}
	}
	for path := range paths {
		lock := d.readLock(path)
		if lock == nil || !lock.Locked {
			continue
		}
		if lock.Metadata == nil || lock.Metadata.SetID == "" || lock.Metadata.SetID == row.ID {
			return fmt.Errorf("refusing bind-worktree: %s is currently executing", row.ID)
		}
	}
	return nil
}

// RepoKeyForRow resolves the repository key a row's Task set binds under. It is
// the one half of the bind context a read surface needs; the scans beside it
// belong to the drain pipeline that resolved them.
func RepoKeyForRow(d *Deps, cfg *config.Config, row DashboardRow) (string, error) {
	_, repoKey, err := dashboardBindContext(d, cfg, row)
	return repoKey, err
}
