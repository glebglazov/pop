package queue

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/glebglazov/pop/config"
	"github.com/glebglazov/pop/project"
	"github.com/glebglazov/pop/tasks"
	"github.com/glebglazov/pop/tasks/binding"
)

// repoScanReason is the reason emitted when a repository cannot be scheduled
// because no representative checkout could be resolved (ADR-0035): a bare repo
// with no Trunk worktree configured and no per-set Worktree binding. The Queue
// never guesses a checkout, so it refuses and reports.
const repoScanReason = "needs trunk; skipped (set trunk = true in a global [repo.\"<path>\"] block)"

// decideRepoDispatches collapses one repository's in-scope checkouts (its
// worktrees, grouped by Repository identity) into a single scheduling unit and
// returns the Decisions for it. It dispatches at most one drain per idle
// repository per Ready set — never once per worktree (ADR-0035).
//
// The drain routes to a single representative checkout resolved in order:
// per-set Worktree binding → Trunk worktree (explicit trunk override or git main
// worktree for non-bare) → refuse and report.
func decideRepoDispatches(d *Deps, cfg *config.Config, scans []projectScan, now time.Time) []Decision {
	if len(scans) == 0 {
		return nil
	}
	rep, _, err := resolveRepresentative(d, cfg, scans)
	return decideRepoDispatchesWithRep(d, cfg, scans, rep, err, loadRecoveryWaiters(d), now)
}

// decideRepoDispatchesWithRep is the representative-injected variant of
// decideRepoDispatches: the caller supplies the already-resolved representative
// (or nil for a bare repo with no Trunk worktree) and its resolution error. Scan
// uses it to feed a fork-free, marker-derived representative (ADR-0060) into the
// unchanged scheduling logic, so the decisions are identical to the old
// git-resolved path while forking no git for the static resolution. A nil rep
// with a nil error means a bare repo refused for lack of a Trunk worktree.
func decideRepoDispatchesWithRep(d *Deps, cfg *config.Config, scans []projectScan, rep *projectScan, repErr error, recoveryWaiters map[string]tasks.RecoveryWaiter, now time.Time) []Decision {
	if len(scans) == 0 {
		return nil
	}
	name := repoName(scans, rep)
	if repErr != nil {
		return []Decision{{Project: name, Err: repErr, Reason: "repo"}}
	}

	// Crash-retry schedule (its length is the park threshold) drives the per-set
	// abnormal backoff/parking derived from Drain history (ADR-0055). Resolve it
	// once for the group; a config error degrades to no schedule (every abnormal
	// parks immediately), matching the daemon's fail-safe stance.
	var delays []time.Duration
	if qcfg, qerr := resolvedQueueConfig(cfg); qerr == nil {
		delays = qcfg.CrashRetryDelays
	}

	// Live drains in any of the repo's checkouts collapse to the repository:
	// a set already running in one worktree must not be dispatched again into
	// the representative.
	extraBusy, runningElsewhere := repoBusyAcrossCheckouts(d, scans, rep, name)

	if rep == nil {
		// Bare repo with no Trunk worktree configured. A per-set Worktree binding
		// is still a valid drain router; without one the repository is refused and
		// reported.
		return append(extraBusy, decideBareWithoutBase(d, cfg, scans, name, delays, recoveryWaiters, now, runningElsewhere)...)
	}

	decisions := decideProjectDispatches(d, *rep, delays, recoveryWaiters, now)
	decisions = filterRunningElsewhere(decisions, runningElsewhere)
	applyBindingRouting(d, scans, decisions)
	return append(extraBusy, decisions...)
}

// resolveRepresentative resolves the repository's representative base checkout
// (the one the drain routes to when no per-set binding applies):
//
//	Trunk worktree (explicit trunk override or git main worktree) → none (refuse).
//
// A nil scan with bare=true means the repository is bare with no Trunk worktree
// configured: it is refused unless a per-set binding routes the drain elsewhere.
func resolveRepresentative(d *Deps, cfg *config.Config, scans []projectScan) (*projectScan, bool, error) {
	// A renamed execution key (queue_base/execution_base → trunk) is a
	// config-global blocking finding (ADR 0054). The queue consumes execution
	// config, so surface it as fatal up front: ResolveRepoConfig returns the
	// finding (a config.Finding) before touching .pop.toml, while a per-checkout
	// .pop.toml problem comes back as a plain error and is still degraded past in
	// the scan loop below. This keeps the migration tripwire loud for pop queue /
	// pop tasks drain and the queue dashboard.
	if cfg != nil && len(scans) > 0 {
		if _, err := resolveRepoConfigFor(d, cfg, scans[0].ProjectPath); err != nil {
			var f config.Finding
			if errors.As(err, &f) {
				return nil, false, err
			}
		}
	}

	// 1. explicit trunk = true checkout.
	for i := range scans {
		rc, err := resolveRepoConfigFor(d, cfg, scans[i].ProjectPath)
		if err == nil && rc.Trunk {
			return &scans[i], false, nil
		}
	}

	// 2. the repo's git main worktree — derived for any non-bare repo even when
	// it has linked worktrees.
	mainPath, bare, err := binding.GitMainWorktree(d.Tasks, scans[0].ProjectPath)
	if err != nil {
		return nil, false, err
	}
	if bare {
		return nil, true, nil
	}
	if mainPath == "" {
		return nil, false, fmt.Errorf("no git main worktree")
	}
	return scanForCheckout(d, scans, mainPath), false, nil
}

// ResolveTrunkPath resolves the repository's Trunk worktree checkout. It
// delegates to tasks/binding.
func ResolveTrunkPath(d *Deps, cfg *config.Config, checkoutPath string) (path string, bare bool, err error) {
	if d == nil {
		d = DefaultDeps()
	}
	if d.Tasks == nil {
		d.Tasks = tasks.DefaultDeps()
	}
	return binding.ResolveTrunkPath(d.Tasks, cfg, checkoutPath)
}

// MainWorktreeBranch returns the branch currently checked out in the Trunk
// worktree — the merge target for an implement worktree drain (ADR-0036).
// Returns ("", nil) when the repo is bare with no Trunk configured or the
// Trunk worktree is in detached HEAD state.
func MainWorktreeBranch(d *Deps, runtimePath string) (string, error) {
	if d == nil {
		d = DefaultDeps()
	}
	if d.Tasks == nil {
		d.Tasks = tasks.DefaultDeps()
	}
	trunkPath, bare, err := binding.ResolveTrunkPath(d.Tasks, nil, runtimePath)
	if err != nil || bare || trunkPath == "" {
		return "", err
	}
	out, err := d.Tasks.Git.CommandInDir(trunkPath, "branch", "--show-current")
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(out), nil
}

// gitMainWorktree returns the repository's primary working tree by parsing
// `git worktree list --porcelain` run from any of its checkouts. A bare repo
// has no primary working tree: bare is true and the path is empty.
func gitMainWorktree(d *Deps, fromCheckout string) (string, bool, error) {
	out, err := d.Tasks.Git.CommandInDir(fromCheckout, "worktree", "list", "--porcelain")
	if err != nil {
		return "", false, fmt.Errorf("list worktrees: %w", err)
	}
	mainPath, bare := parseGitMainWorktree(out)
	return mainPath, bare, nil
}

// parseGitMainWorktree extracts the primary working tree from porcelain output.
// Git always lists the main worktree first; a bare repo's first entry carries a
// `bare` attribute and names no working tree.
func parseGitMainWorktree(porcelain string) (string, bool) {
	var firstPath string
	started := false
	for _, line := range strings.Split(porcelain, "\n") {
		if strings.HasPrefix(line, "worktree ") {
			if started {
				break // second block reached; the main worktree is the first.
			}
			firstPath = strings.TrimSpace(strings.TrimPrefix(line, "worktree "))
			started = true
			continue
		}
		if !started {
			continue
		}
		switch {
		case line == "bare":
			return "", true
		case strings.TrimSpace(line) == "":
			return firstPath, false
		}
	}
	return firstPath, false
}

// scanForCheckout returns the candidate scan whose checkout canonicalizes to
// checkoutPath, or synthesizes one from the group's shared definition path when
// the representative (e.g. a git main worktree) is not itself a picker Project.
func scanForCheckout(d *Deps, scans []projectScan, checkoutPath string) *projectScan {
	canon, err := canonicalCheckoutPath(d.Tasks, checkoutPath)
	if err != nil {
		canon = checkoutPath
	}
	for i := range scans {
		if c, err := canonicalCheckoutPath(d.Tasks, scans[i].ProjectPath); err == nil && c == canon {
			return &scans[i]
		}
	}
	base := scans[0]
	runtimePath := canon
	if rt, err := canonicalCheckoutPath(d.Tasks, canon); err == nil {
		runtimePath = rt
	}
	return &projectScan{
		Name:           base.Name,
		ProjectPath:    canon,
		DefinitionPath: base.DefinitionPath,
		RuntimePath:    runtimePath,
		SessionName:    project.SessionNameWith(d.Project, canon),
	}
}

// repoBusyAcrossCheckouts reads the runtime execution lock of every checkout in
// the group except the representative's (whose lock decideProjectDispatches
// reads on its own) and returns one busy Decision per live drain plus the set
// of SetIDs already running in another checkout.
func repoBusyAcrossCheckouts(d *Deps, scans []projectScan, rep *projectScan, name string) ([]Decision, map[string]bool) {
	running := map[string]bool{}
	var busy []Decision
	seen := map[string]bool{}
	if rep != nil && rep.RuntimePath != "" {
		seen[rep.RuntimePath] = true
	}
	for i := range scans {
		runtimePath := scans[i].RuntimePath
		if runtimePath == "" || seen[runtimePath] {
			continue
		}
		seen[runtimePath] = true
		lock := d.readLock(runtimePath)
		if lock == nil || !lock.Locked {
			continue
		}
		dec := Decision{Project: name, Busy: true, Reason: "busy", scan: scans[i], lockStatus: lock}
		if lock.Metadata != nil && lock.Metadata.SetID != "" {
			dec.TaskSetID = lock.Metadata.SetID
			running[lock.Metadata.SetID] = true
		}
		busy = append(busy, dec)
	}
	return busy, running
}

// filterRunningElsewhere drops actionable decisions whose set already runs in
// another of the repository's checkouts, preserving busy/idle decisions.
func filterRunningElsewhere(decisions []Decision, runningElsewhere map[string]bool) []Decision {
	if len(runningElsewhere) == 0 {
		return decisions
	}
	out := decisions[:0]
	for _, dec := range decisions {
		if dec.Actionable() && runningElsewhere[dec.TaskSetID] {
			continue
		}
		out = append(out, dec)
	}
	return out
}

// applyBindingRouting routes an actionable drain to its per-set Worktree
// binding when one exists, making the binding the universal drain router
// (ADR-0035) — consulted for any repo. WorktreeReady decisions are left for
// prepareWorktreeDrain, which reuses the binding while preserving the project
// session.
func applyBindingRouting(d *Deps, scans []projectScan, decisions []Decision) {
	bindings, err := binding.AllBindings(d.Tasks)
	if err != nil || len(bindings) == 0 {
		return
	}
	repoKey, err := scanRepoKey(d, scans[0])
	if err != nil {
		return
	}
	for i := range decisions {
		dec := &decisions[i]
		if !dec.Actionable() || dec.WorktreeReady {
			continue
		}
		b, ok := bindings[setScopedKey(repoKey, dec.TaskSetID)]
		if !ok || strings.TrimSpace(b.RuntimePath) == "" {
			continue
		}
		dec.scan.ProjectPath = b.RuntimePath
		dec.scan.RuntimePath = b.RuntimePath
		dec.scan.SessionName = project.SessionNameWith(d.Project, b.RuntimePath)
		if lock := d.readLock(b.RuntimePath); lock != nil && lock.Locked {
			dec.Busy = true
			dec.Reason = "busy"
			dec.lockStatus = lock
		}
	}
}

// decideBareWithoutBase handles a bare repository with no Trunk worktree
// configured: each Ready set with a per-set binding routes to that bound
// checkout; any Ready set without one leaves the repository refused and
// reported (a single skip decision), never scheduled.
func decideBareWithoutBase(d *Deps, cfg *config.Config, scans []projectScan, name string, delays []time.Duration, recoveryWaiters map[string]tasks.RecoveryWaiter, now time.Time, runningElsewhere map[string]bool) []Decision {
	base := scans[0]
	worktreeReady, configErr := readRepoConfig(d, base.ProjectPath)
	skel := Decision{
		Project:            name,
		WorktreeReady:      worktreeReady,
		ProjectConfigError: configErr,
		scan:               projectScan{Name: name, ProjectPath: base.ProjectPath, DefinitionPath: base.DefinitionPath},
	}

	refresh, err := d.refresh(base.DefinitionPath)
	if err != nil {
		skel.Err = err
		skel.Reason = "refresh"
		return []Decision{skel}
	}
	repoKey, err := scanRepoKey(d, base)
	if err != nil {
		skel.Err = err
		skel.Reason = "repo"
		return []Decision{skel}
	}

	backoff := d.setBackoffLookup(scanRepoCommonDir(d, base), delays, now)
	ids, waitUntil, waitReason, blockedID, ok := selectReadySets(refresh, backoff, recoveryWaiters)
	if !ok {
		if !waitUntil.IsZero() {
			skel.Reason = waitReason
			skel.WaitUntil = waitUntil
			skel.BlockedSetID = blockedID
		} else if waitReason != "" {
			skel.Reason = waitReason
			skel.BlockedSetID = blockedID
		} else {
			skel.Reason = "no ready set"
		}
		return []Decision{skel}
	}

	bindings, bindErr := binding.AllBindings(d.Tasks)
	if bindErr != nil {
		bindings = nil
	}

	var decisions []Decision
	var unbound bool
	for _, id := range ids {
		if runningElsewhere[id] {
			continue
		}
		b, has := bindings[setScopedKey(repoKey, id)]
		if !has || strings.TrimSpace(b.RuntimePath) == "" {
			// A `managed` set in a bare repo with no resolvable trunk has nothing to
			// fork from: surface it as a per-set config error (ADR-0059) rather than
			// fold it into the generic repo "needs trunk" skip — the set is named, and
			// no drain is dispatched.
			if msg := d.probeDirective(base.ProjectPath, id); msg != "" {
				decisions = append(decisions, directiveConfigDecision(skel, id, msg))
				continue
			}
			unbound = true
			continue
		}
		action := skel
		action.TaskSetID = id
		action.scan.ProjectPath = b.RuntimePath
		action.scan.RuntimePath = b.RuntimePath
		action.scan.SessionName = project.SessionNameWith(d.Project, b.RuntimePath)
		if lock := d.readLock(b.RuntimePath); lock != nil && lock.Locked {
			busyDec := skel
			busyDec.Busy = true
			busyDec.Reason = "busy"
			busyDec.TaskSetID = id
			busyDec.lockStatus = lock
			busyDec.scan.RuntimePath = b.RuntimePath
			busyDec.scan.ProjectPath = b.RuntimePath
			decisions = append(decisions, busyDec)
			continue
		}
		decisions = append(decisions, action)
	}

	if unbound {
		skip := skel
		skip.Reason = repoScanReason
		decisions = append(decisions, skip)
	}
	if len(decisions) == 0 {
		skel.Reason = "ready set already running"
		return []Decision{skel}
	}
	return decisions
}

// repoName derives a stable label for a repository scheduling unit, preferring
// the representative checkout's picker name.
func repoName(scans []projectScan, rep *projectScan) string {
	if rep != nil && rep.Name != "" {
		return rep.Name
	}
	if len(scans) > 0 {
		return scans[0].Name
	}
	return ""
}

// resolveRepoConfigFor resolves the effective RepoConfig for a checkout, merging
// global [repo."<path>"] overrides over repo-root .pop.toml. trunk is honored
// only for the keyed checkout path.
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
