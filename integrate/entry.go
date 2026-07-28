package integrate

import (
	"fmt"
	"io"
	"strings"
)

func installComponentCollectOutcomes(r *run, home, agent string, comp Component) ([]Outcome, error) {
	id := comp.id

	if comp.install != nil {
		probe := newRun(r.deps, Request{Agent: agent, DryRun: true, CoreOnly: true})
		if err := comp.install(probe, home, agent); err != nil {
			return nil, err
		}
		quiet := *r
		quietDeps := *r.deps
		quietDeps.stdout = nil
		quiet.deps = &quietDeps
		if err := comp.install(&quiet, home, agent); err != nil {
			return nil, err
		}
		label := installLabel(!probe.installed, probe.installed && probe.changed)
		return []Outcome{statusWiringOutcome(agent, label)}, nil
	}

	prefix := r.deps.resolveSkillsPrefix()

	installedBefore, err := fileComponentInstalledNames(r.deps, home, id, agent)
	if err != nil {
		return nil, fmt.Errorf("installed check for %s/%s: %w", agent, id, err)
	}
	staleBefore := true
	if len(installedBefore) > 0 {
		if staleBefore, err = fileComponentStaleResolved(r.deps, home, id, agent, installedBefore); err != nil {
			return nil, fmt.Errorf("stale check for %s/%s: %w", agent, id, err)
		}
	}

	installR := *r
	installR.agentName = agent
	if !r.overwriteConflicts {
		quietDeps := *r.deps
		quietDeps.stdout = nil
		installR.deps = &quietDeps
	}
	if err := installFileComponent(&installR, home, id, agent); err != nil {
		return nil, err
	}
	r.overwrotePaths = append(r.overwrotePaths, installR.overwrotePaths...)
	r.prunedStale = append(r.prunedStale, installR.prunedStale...)

	overwritten := overwrittenSkillPaths(prefix, id, agent, installR.overwrotePaths)
	postConflict, err := preInstallSkillConflicts(installR.deps, home, agent, id, prefix)
	if err != nil {
		return nil, fmt.Errorf("conflict check for %s/%s: %w", agent, id, err)
	}

	return fileComponentOutcomesInCatalogOrder(
		agent, id, prefix, installedBefore, staleBefore, installR.prunedStale,
		nil, postConflict, overwritten,
	), nil
}

func conflictSkipLabel(agent, conflictPath string) string {
	return fmt.Sprintf("skipped (conflict at %s; run 'pop integrate %s --overwrite-conflicts' to replace it)", conflictPath, agent)
}

func reportOverwriteDestroyed(out io.Writer, conflictPath string) {
	if out == nil {
		return
	}
	fmt.Fprintf(out, "  OVERWRITE: destroyed %s (not owned by pop — no backup kept)\n", conflictPath)
}

// Install applies integration components for req.Agent according to Request
// intent and returns what changed. Outcome lines are returned on Report for
// the caller to render; Install does not print them.
func Install(d *Deps, req Request) (Report, error) {
	agent := strings.ToLower(req.Agent)
	r := newRun(d, req)
	r.agentName = agent

	core, ok := LookupComponent(ComponentStatusWiring)
	if !ok {
		return Report{}, fmt.Errorf("status-wiring component missing from catalog")
	}
	if !core.supported(agent) {
		return Report{}, unknownAgentError(agent)
	}

	home, err := r.deps.userHomeDir()
	if err != nil {
		return Report{}, fmt.Errorf("failed to get home directory: %w", err)
	}

	if req.CoreOnly {
		if err := core.install(r, home, agent); err != nil {
			return r.toReport(nil), err
		}
		return r.toReport(nil), nil
	}

	installSet := map[ComponentID]bool{ComponentStatusWiring: true}
	for _, id := range req.Components {
		installSet[id] = true
	}

	var outcomes []Outcome
	for _, comp := range catalog {
		if !comp.supported(agent) {
			continue
		}
		if installSet[comp.id] {
			compOutcomes, err := installComponentCollectOutcomes(r, home, agent, comp)
			if err != nil {
				return r.toReport(outcomes), err
			}
			outcomes = append(outcomes, compOutcomes...)
		} else if req.ExplicitOptOuts[comp.id] {
			compOutcomes, err := optOutRemoveOutcomes(r.deps, home, agent, comp.id)
			if err != nil {
				return r.toReport(outcomes), err
			}
			outcomes = append(outcomes, compOutcomes...)
		} else if comp.id != ComponentStatusWiring {
			compOutcomes, err := optOutSkipOutcomes(r.deps, agent, comp.id)
			if err != nil {
				return r.toReport(outcomes), err
			}
			outcomes = append(outcomes, compOutcomes...)
		}
	}

	return r.toReport(outcomes), nil
}

// RemoveOptOutCollectOutcome is retained for tests that call it directly.
func RemoveOptOutCollectOutcome(d *Deps, home, agent string, id ComponentID) ([]Outcome, error) {
	return optOutRemoveOutcomes(d, home, agent, id)
}
