package queue

import (
	"fmt"
	"strings"
	"time"

	"github.com/glebglazov/pop/config"
	"github.com/glebglazov/pop/project"
)

// repoScanReason is the reason emitted when a repository cannot be scheduled
// because no representative checkout could be resolved (ADR-0035): a bare repo
// with no `queue_base` override and no per-set Worktree binding. The Queue
// never guesses a checkout, so it refuses and reports.
const repoScanReason = "needs queue_base; skipped"

// decideRepoDispatches collapses one repository's in-scope checkouts (its
// worktrees, grouped by Repository identity) into a single scheduling unit and
// returns the Decisions for it. It dispatches at most one drain per idle
// repository per Ready set — never once per worktree (ADR-0035).
//
// The drain routes to a single representative checkout resolved in order:
// per-set Worktree binding → explicit queue_base checkout → the repo's git main
// worktree (non-bare) → refuse and report.
func decideRepoDispatches(d *Deps, cfg *config.Config, scans []projectScan, agents []string, state *DaemonState, now time.Time) []Decision {
	if len(scans) == 0 {
		return nil
	}

	rep, _, err := resolveRepresentative(d, cfg, scans)
	name := repoName(scans, rep)
	if err != nil {
		return []Decision{{Project: name, Err: err, Reason: "repo"}}
	}

	// Live drains in any of the repo's checkouts collapse to the repository:
	// a set already running in one worktree must not be dispatched again into
	// the representative.
	extraBusy, runningElsewhere := repoBusyAcrossCheckouts(d, scans, rep, name)

	if rep == nil {
		// Bare repo with no queue_base. A per-set Worktree binding is still a
		// valid drain router (decoupled from worktree_ready); without one the
		// repository is refused and reported.
		return append(extraBusy, decideBareWithoutBase(d, cfg, scans, name, agents, state, now, runningElsewhere)...)
	}

	decisions := decideProjectDispatches(d, *rep, agents, state, now)
	decisions = filterRunningElsewhere(decisions, runningElsewhere)
	applyBindingRouting(d, scans, state, decisions)
	return append(extraBusy, decisions...)
}

// resolveRepresentative resolves the repository's representative base checkout
// (the one the drain routes to when no per-set binding applies):
//
//	explicit queue_base checkout → git main worktree (non-bare) → none (refuse).
//
// A nil scan with bare=true means the repository is bare and has no queue_base
// override: it has no git main worktree, so it is refused unless a per-set
// binding routes the drain elsewhere.
func resolveRepresentative(d *Deps, cfg *config.Config, scans []projectScan) (*projectScan, bool, error) {
	// 1. explicit queue_base checkout (task 01's resolved config).
	for i := range scans {
		rc, err := resolveRepoConfigFor(d, cfg, scans[i].ProjectPath)
		if err == nil && rc.QueueBase {
			return &scans[i], false, nil
		}
	}

	// 2. the repo's git main worktree — derived for any non-bare repo even when
	// it has linked worktrees.
	mainPath, bare, err := gitMainWorktree(d, scans[0].ProjectPath)
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

// applyBindingRouting routes a non-worktree-ready actionable drain to its per-set
// Worktree binding when one exists, making the binding the universal drain
// router (ADR-0035) — consulted for any repo, not only worktree-ready ones.
// Worktree-ready decisions are left for prepareWorktreeDrain, which provisions
// or reuses the binding while preserving the project session.
func applyBindingRouting(d *Deps, scans []projectScan, state *DaemonState, decisions []Decision) {
	if state == nil || len(state.WorktreeBindings) == 0 {
		return
	}
	repoKey, err := resolveRepoKey(d, scans[0].ProjectPath)
	if err != nil {
		return
	}
	for i := range decisions {
		dec := &decisions[i]
		if !dec.Actionable() || dec.WorktreeReady {
			continue
		}
		binding, ok := state.WorktreeBindings[setScopedKey(repoKey, dec.TaskSetID)]
		if !ok || strings.TrimSpace(binding.RuntimePath) == "" {
			continue
		}
		dec.scan.ProjectPath = binding.RuntimePath
		dec.scan.RuntimePath = binding.RuntimePath
		dec.scan.SessionName = project.SessionNameWith(d.Project, binding.RuntimePath)
		if lock := d.readLock(binding.RuntimePath); lock != nil && lock.Locked {
			dec.Busy = true
			dec.Reason = "busy"
			dec.lockStatus = lock
		}
	}
}

// decideBareWithoutBase handles a bare repository with no queue_base: each Ready
// set with a per-set binding routes to that bound checkout; any Ready set
// without one leaves the repository refused and reported (a single skip
// decision), never scheduled.
func decideBareWithoutBase(d *Deps, cfg *config.Config, scans []projectScan, name string, agents []string, state *DaemonState, now time.Time, runningElsewhere map[string]bool) []Decision {
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
	repoKey, err := resolveRepoKey(d, base.ProjectPath)
	if err != nil {
		skel.Err = err
		skel.Reason = "repo"
		return []Decision{skel}
	}

	ids, waitUntil, waitReason, ok := selectReadySets(refresh, repoKey, state, now)
	if !ok {
		if !waitUntil.IsZero() {
			skel.Reason = waitReason
			skel.WaitUntil = waitUntil
		} else if waitReason != "" {
			skel.Reason = waitReason
		} else {
			skel.Reason = "no ready set"
		}
		return []Decision{skel}
	}

	var decisions []Decision
	var unbound bool
	var defaultAgent string
	var agentResolved bool
	for _, id := range ids {
		if runningElsewhere[id] {
			continue
		}
		binding, has := state.WorktreeBindings[setScopedKey(repoKey, id)]
		if !has || strings.TrimSpace(binding.RuntimePath) == "" {
			unbound = true
			continue
		}
		if !agentResolved {
			agent, until, notes, agentOK := selectDefaultAgent(d, agents, state, now)
			skel.AgentNotes = notes
			if !agentOK {
				if until.IsZero() {
					skel.Reason = "no available agent"
				} else {
					skel.Reason = "all agents cooling"
				}
				skel.WaitUntil = until
				return []Decision{skel}
			}
			defaultAgent = agent
			agentResolved = true
		}
		action := skel
		action.TaskSetID = id
		action.DefaultAgent = defaultAgent
		action.scan.ProjectPath = binding.RuntimePath
		action.scan.RuntimePath = binding.RuntimePath
		action.scan.SessionName = project.SessionNameWith(d.Project, binding.RuntimePath)
		if lock := d.readLock(binding.RuntimePath); lock != nil && lock.Locked {
			busyDec := skel
			busyDec.Busy = true
			busyDec.Reason = "busy"
			busyDec.TaskSetID = id
			busyDec.lockStatus = lock
			busyDec.scan.RuntimePath = binding.RuntimePath
			busyDec.scan.ProjectPath = binding.RuntimePath
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
// global [repo."<path>"] overrides (task 01) over repo-root .pop.toml. queue_base
// is honored only for the keyed checkout path.
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
