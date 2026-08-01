package integrate

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/glebglazov/pop/config"
	"github.com/glebglazov/pop/debug"
)

// ----- App state (state.json) ------------------------------------------------

// appState holds cross-run markers persisted at ~/.local/share/pop/state.json.
// Currently used only as a staleness marker for auto-updating integrations.
type appState struct {
	// BuildRevision is the vcs.revision of the binary that last successfully
	// ran EnsureForRevision. An empty value means no check has run yet.
	BuildRevision string `json:"build_revision"`
}

// appStatePath returns the path to state.json, respecting XDG_DATA_HOME through
// the cmd-layer FS seam. Mirrors the pattern used by history.DefaultHistoryPath
// and monitor.DefaultStatePath.
func appStatePath(d *Deps) string {
	if d != nil && d.getenv != nil {
		if xdg := d.getenv("XDG_DATA_HOME"); xdg != "" {
			return filepath.Join(xdg, "pop", "state.json")
		}
	}
	if d != nil && d.userHomeDir != nil {
		home, err := d.userHomeDir()
		if err == nil {
			return filepath.Join(home, ".local", "share", "pop", "state.json")
		}
	}
	if xdg := os.Getenv("XDG_DATA_HOME"); xdg != "" {
		return filepath.Join(xdg, "pop", "state.json")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		debug.Error("appStatePath: UserHomeDir: %v", err)
		return filepath.Join(".local", "share", "pop", "state.json")
	}
	return filepath.Join(home, ".local", "share", "pop", "state.json")
}

// loadAppState reads state.json. A missing or corrupt file is treated as an
// empty state, so the auto-updater re-checks everything on the next launch.
func loadAppState(d *Deps) *appState {
	path := appStatePath(d)
	data, err := os.ReadFile(path)
	if err != nil {
		if !os.IsNotExist(err) {
			debug.Error("loadAppState: %v", err)
		}
		return &appState{}
	}
	var s appState
	if err := json.Unmarshal(data, &s); err != nil {
		debug.Error("loadAppState: unmarshal: %v", err)
		return &appState{}
	}
	return &s
}

// saveAppState writes state.json, creating parent directories as needed.
func saveAppState(d *Deps, s *appState) error {
	path := appStatePath(d)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := json.Marshal(s)
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}

// ----- Auto-update integrations ---------------------------------------------

// EnsureForRevision is the revision-gated Integration refresh entry point.
// The caller supplies the binary revision (cmd injects buildRevision());
// integrate never derives it. When the recorded revision differs, installed
// integration components are reconciled to the merged baseline and the Issue
// tracker doc is seeded; when it matches, refresh is skipped. Returns warnings
// to surface in the picker for any failures.
func EnsureForRevision(rev string, cd *config.Deps) []string {
	return ensureForRevisionWith(rev, cd, DefaultDeps)
}

// integrationUpdateResult reports what updateStaleIntegrations did during
// one pass: per-component outcomes for CLI display and warnings to surface.
type integrationUpdateResult struct {
	Outcomes []Outcome
	Warnings []string
}

// updateStaleIntegrations is the pure core of the per-component refresh flow.
// For each integrated agent it reconciles every supported catalog component
// against the merged Integration baseline: re-render installed pop-owned
// artifacts, install missing baseline-listed components, skip baseline
// omissions and conflicts without overwriting. Non-integrated agents are left
// alone. Each pair with a reportable state produces one outcome.
//
// The function does not read or write state.json, does not gate on the
// binary revision, and does not emit output. Callers layer those behaviors
// on top (see ensureForRevisionWith and RunUpdateExistingWith).
func updateStaleIntegrations(cd *config.Deps, newDeps func() *Deps) integrationUpdateResult {
	d := newDeps()
	if err := seedIssueTrackerDoc(d); err != nil {
		debug.Error("updateStaleIntegrations: seed issue tracker doc: %v", err)
	}

	var result integrationUpdateResult
	if o := linkUserIssueTrackerDoc(d); o != nil {
		result.Outcomes = append(result.Outcomes, *o)
	}
	if o := removeStaleDataDirWorkStoreDoc(d); o != nil {
		result.Outcomes = append(result.Outcomes, *o)
	}
	if o := removeLegacyWorkStoreDoc(d); o != nil {
		result.Outcomes = append(result.Outcomes, *o)
	}

	baseline, err := BaselineLoader(cd)
	if err != nil {
		debug.Error("updateStaleIntegrations: baseline: %v", err)
		return integrationUpdateResult{
			Warnings: []string{fmt.Sprintf("failed to load integration config: %v", err)},
		}
	}
	baselineSet := baselineComponentSet(baseline)

	for _, agent := range Agents {
		integrated, err := agentIntegratedViaStatusWiring(newDeps, agent)
		if err != nil {
			debug.Error("updateStaleIntegrations: integrated check %s: %v", agent, err)
			continue
		}
		if !integrated {
			continue
		}

		agentUpdated := false
		for _, comp := range catalog {
			compOutcomes, warning := refreshComponent(newDeps, agent, comp.id, baselineSet)
			if warning != "" {
				result.Warnings = append(result.Warnings, warning)
			}
			if len(compOutcomes) > 0 {
				result.Outcomes = append(result.Outcomes, compOutcomes...)
				for _, o := range compOutcomes {
					if o.Label == "updated" || o.Label == "added" {
						agentUpdated = true
					}
				}
			}
		}
		if agentUpdated {
			debug.Log("updateStaleIntegrations: updated %s integration", agent)
		}
	}

	return result
}

func baselineComponentSet(baseline []ComponentID) map[ComponentID]bool {
	set := make(map[ComponentID]bool, len(baseline))
	for _, id := range baseline {
		set[id] = true
	}
	return set
}

// agentIntegratedViaStatusWiring reports whether an agent has pop status wiring
// installed. Refresh only reconciles agents that are already integrated.
func agentIntegratedViaStatusWiring(newDeps func() *Deps, agent string) (bool, error) {
	report, err := Install(newDeps(), Request{Agent: agent, DryRun: true, CoreOnly: true})
	if err != nil {
		return false, err
	}
	return report.Installed, nil
}

// refreshComponent reconciles a single (agent, component) pair against the
// merged Integration baseline, returning an outcome and any warning to surface.
// A component not supported by the agent is skipped silently (nil outcome, no
// warning). Callers must only invoke this for agents already integrated via
// status wiring.
func refreshComponent(newDeps func() *Deps, agent string, id ComponentID, baselineSet map[ComponentID]bool) ([]Outcome, string) {
	comp, ok := LookupComponent(id)
	if !ok {
		return nil, ""
	}
	if !comp.supported(agent) {
		return nil, ""
	}
	switch id {
	case ComponentStatusWiring:
		outcome, warning := refreshStatusWiring(newDeps, agent)
		if outcome == nil {
			return nil, warning
		}
		return []Outcome{*outcome}, warning
	default:
		if !baselineSet[id] {
			outcomes, err := optOutSkipOutcomes(newDeps(), agent, id)
			if err != nil {
				debug.Error("refreshComponent: opt-out outcomes %s/%s: %v", agent, id, err)
				return nil, ""
			}
			return outcomes, ""
		}
		return refreshFileComponent(newDeps, agent, id)
	}
}

// refreshStatusWiring refreshes the status-wiring component for an integrated
// agent. It dry-runs the install to learn changed state and, only when stale,
// performs the real install. Warnings are returned solely for an agent
// demonstrably installed but failing to check or update.
func refreshStatusWiring(newDeps func() *Deps, agent string) (*Outcome, string) {
	report, err := Install(newDeps(), Request{Agent: agent, DryRun: true, CoreOnly: true})
	if err != nil {
		debug.Error("refreshStatusWiring: dry-run %s: %v", agent, err)
		if report.Installed {
			return nil, fmt.Sprintf("failed to check %s integration: %v", agent, err)
		}
		return nil, ""
	}
	if !report.Installed {
		return nil, ""
	}
	if !report.Changed {
		o := statusWiringOutcome(agent, "already current")
		return &o, ""
	}

	realDeps := newDeps()
	realDeps.stdout = nil
	if _, err := Install(realDeps, Request{Agent: agent, CoreOnly: true}); err != nil {
		debug.Error("refreshStatusWiring: update %s: %v", agent, err)
		return nil, fmt.Sprintf("failed to update %s integration (see pop.log)", agent)
	}
	o := statusWiringOutcome(agent, "updated")
	return &o, ""
}

// refreshFileComponent reconciles a baseline-listed file-based skill component
// for an integrated agent. It inspects the link installer's render tree and
// the agent-location symlinks to decide:
//
//   - conflict (an unowned entry shadows pop's) → "skipped (conflict)";
//   - not installed → install and report "added";
//   - installed but current → "already current";
//   - installed and stale → re-render and re-link via installFileComponent,
//     which also migrates any lingering copy-mode artifact to a symlink.
//
// Warnings follow the status-wiring contract: only an installed component that
// fails its staleness check or its re-install warns; everything else is silent.
func refreshFileComponent(newDeps func() *Deps, agent string, id ComponentID) ([]Outcome, string) {
	checkDeps := newDeps()
	home, err := checkDeps.userHomeDir()
	if err != nil {
		debug.Error("refreshFileComponent: home %s/%s: %v", agent, id, err)
		return nil, ""
	}

	prefix := checkDeps.resolveSkillsPrefix()
	preConflict, err := preInstallSkillConflicts(checkDeps, home, agent, id, prefix)
	if err != nil {
		debug.Error("refreshFileComponent: conflict check %s/%s: %v", agent, id, err)
		return nil, ""
	}

	installedBefore, err := fileComponentInstalledNames(checkDeps, home, id, agent)
	if err != nil {
		debug.Error("refreshFileComponent: installed check %s/%s: %v", agent, id, err)
		return nil, ""
	}
	if len(preConflict) > 0 {
		return fileComponentOutcomesInCatalogOrder(
			agent, id, prefix, installedBefore, false, nil, preConflict, nil, nil,
		), ""
	}
	if len(installedBefore) == 0 {
		if checkDeps.logf != nil {
			checkDeps.logf("refreshFileComponent: %s/%s not installed — adding", agent, id)
		}
		realDeps := newDeps()
		realDeps.stdout = nil
		r := newRun(realDeps, Request{Agent: agent})
		if err := installFileComponent(r, home, id, agent); err != nil {
			debug.Error("refreshFileComponent: add %s/%s: %v", agent, id, err)
			return nil, fmt.Sprintf("failed to add %s %s integration (see pop.log)", agent, id)
		}
		return fileComponentOutcomesInCatalogOrder(
			agent, id, prefix, nil, true, r.prunedStale, preConflict, nil, nil,
		), ""
	}

	staleBefore, err := fileComponentStaleResolved(checkDeps, home, id, agent, installedBefore)
	if err != nil {
		debug.Error("refreshFileComponent: stale check %s/%s: %v", agent, id, err)
		return nil, fmt.Sprintf("failed to check %s %s integration: %v", agent, id, err)
	}
	if !staleBefore {
		if checkDeps.logf != nil {
			checkDeps.logf("refreshFileComponent: %s/%s installed and current — no-op", agent, id)
		}
		return fileComponentOutcomesInCatalogOrder(
			agent, id, prefix, installedBefore, false, nil, preConflict, nil, nil,
		), ""
	}

	if checkDeps.logf != nil {
		checkDeps.logf("refreshFileComponent: %s/%s stale — refreshing", agent, id)
	}
	realDeps := newDeps()
	realDeps.stdout = nil
	r := newRun(realDeps, Request{Agent: agent})
	if err := installFileComponent(r, home, id, agent); err != nil {
		debug.Error("refreshFileComponent: update %s/%s: %v", agent, id, err)
		return nil, fmt.Sprintf("failed to update %s %s integration (see pop.log)", agent, id)
	}
	if checkDeps.logf != nil {
		checkDeps.logf("refreshFileComponent: %s/%s refreshed", agent, id)
	}
	postConflict, err := preInstallSkillConflicts(realDeps, home, agent, id, prefix)
	if err != nil {
		debug.Error("refreshFileComponent: post conflict check %s/%s: %v", agent, id, err)
	}
	return fileComponentOutcomesInCatalogOrder(
		agent, id, prefix, installedBefore, true, r.prunedStale,
		nil, postConflict, nil,
	), ""
}

// stampRevisionIfSuccess writes the given revision to state.json, but only
// when no warnings were produced. Partial failures deliberately leave the
// previous revision in place so the next launch retries. A "dev" revision
// is never stamped — dev builds have no stable staleness marker.
func stampRevisionIfSuccess(rev string, d *Deps, result integrationUpdateResult) {
	if len(result.Warnings) > 0 || rev == "dev" {
		return
	}
	state := loadAppState(d)
	if state.BuildRevision == rev {
		return
	}
	state.BuildRevision = rev
	if err := saveAppState(d, state); err != nil {
		debug.Error("stampRevisionIfSuccess: save state: %v", err)
	}
}

func stateDepsFromConfig(cd *config.Deps, base *Deps) *Deps {
	if cd == nil || cd.FS == nil || base == nil {
		return base
	}
	d := *base
	xdg := cd.FS.Getenv("XDG_DATA_HOME")
	cfg := cd.FS.Getenv("XDG_CONFIG_HOME")
	if xdg == "" && cfg == "" {
		return base
	}
	d.getenv = func(key string) string {
		switch key {
		case "XDG_DATA_HOME":
			return xdg
		case "XDG_CONFIG_HOME":
			return cfg
		}
		return ""
	}
	return &d
}

func ensureForRevisionWith(rev string, cd *config.Deps, newDeps func() *Deps) []string {
	if rev == "dev" {
		return nil
	}
	stateDeps := stateDepsFromConfig(cd, DefaultDeps())
	state := loadAppState(stateDeps)
	if state.BuildRevision == rev {
		return nil
	}

	result := updateStaleIntegrations(cd, newDeps)
	stampRevisionIfSuccess(rev, stateDeps, result)
	return result.Warnings
}
