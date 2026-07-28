package integrate

import (
	"fmt"
	"io"
	"strings"
)

func installComponentCollectOutcomes(d *Deps, home, agent string, comp Component) ([]Outcome, error) {
	id := comp.id

	if comp.install != nil {
		dryD := WithDryRun(d)
		if err := comp.install(dryD, home, agent); err != nil {
			return nil, err
		}
		quietD := *d
		quietD.stdout = nil
		if err := comp.install(&quietD, home, agent); err != nil {
			return nil, err
		}
		label := installLabel(!dryD.installed, dryD.installed && dryD.changed)
		return []Outcome{statusWiringOutcome(agent, label)}, nil
	}

	prefix := d.resolveSkillsPrefix()

	installedBefore, err := fileComponentInstalledNames(d, home, id, agent)
	if err != nil {
		return nil, fmt.Errorf("installed check for %s/%s: %w", agent, id, err)
	}
	staleBefore := true
	if len(installedBefore) > 0 {
		if staleBefore, err = fileComponentStaleResolved(d, home, id, agent, installedBefore); err != nil {
			return nil, fmt.Errorf("stale check for %s/%s: %w", agent, id, err)
		}
	}

	installD := *d
	installD.agentName = agent
	if !d.overwriteConflicts {
		installD.stdout = nil
	}
	if err := installFileComponent(&installD, home, id, agent); err != nil {
		return nil, err
	}

	overwritten := overwrittenSkillPaths(prefix, id, agent, installD.overwrotePaths)
	postConflict, err := preInstallSkillConflicts(&installD, home, agent, id, prefix)
	if err != nil {
		return nil, fmt.Errorf("conflict check for %s/%s: %w", agent, id, err)
	}

	return fileComponentOutcomesInCatalogOrder(
		agent, id, prefix, installedBefore, staleBefore, installD.prunedStale,
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

// RunComponents is the entry point for `pop integrate <agent>`.
func RunComponents(d *Deps, agent string, baseline []ComponentID, interactive bool, verbose bool, explicitOptOuts map[ComponentID]bool, overwriteConflicts, assumeYes bool) error {
	agent = strings.ToLower(agent)
	d.overwriteConflicts = overwriteConflicts
	d.assumeYes = assumeYes
	d.interactive = interactive
	d.agentName = agent

	core, ok := LookupComponent(ComponentStatusWiring)
	if !ok {
		return fmt.Errorf("status-wiring component missing from catalog")
	}
	if !core.supported(agent) {
		return fmt.Errorf("unknown agent %q (expected: claude, codex, pi, opencode, cursor)", agent)
	}

	home, err := d.userHomeDir()
	if err != nil {
		return fmt.Errorf("failed to get home directory: %w", err)
	}

	installSet := map[ComponentID]bool{ComponentStatusWiring: true}
	for _, id := range baseline {
		installSet[id] = true
	}

	var outcomes []Outcome
	for _, comp := range catalog {
		if !comp.supported(agent) {
			continue
		}
		if installSet[comp.id] {
			compOutcomes, err := installComponentCollectOutcomes(d, home, agent, comp)
			if err != nil {
				return err
			}
			outcomes = append(outcomes, compOutcomes...)
		} else if explicitOptOuts[comp.id] {
			compOutcomes, err := optOutRemoveOutcomes(d, home, agent, comp.id)
			if err != nil {
				return err
			}
			outcomes = append(outcomes, compOutcomes...)
		} else if comp.id != ComponentStatusWiring {
			compOutcomes, err := optOutSkipOutcomes(d, agent, comp.id)
			if err != nil {
				return err
			}
			outcomes = append(outcomes, compOutcomes...)
		}
	}

	PrintOutcomes(d.stdout, outcomes, verbose, true)
	return nil
}

// RemoveOptOutCollectOutcome is retained for tests that call it directly.
func RemoveOptOutCollectOutcome(d *Deps, home, agent string, id ComponentID) ([]Outcome, error) {
	return optOutRemoveOutcomes(d, home, agent, id)
}

// RunWith installs the status-wiring component for the given agent.
func RunWith(d *Deps, agent string) error {
	agent = strings.ToLower(agent)

	comp, ok := LookupComponent(ComponentStatusWiring)
	if !ok {
		return fmt.Errorf("status-wiring component missing from catalog")
	}
	if !comp.supported(agent) {
		return fmt.Errorf("unknown agent %q (expected: claude, codex, pi, opencode, cursor)", agent)
	}

	home, err := d.userHomeDir()
	if err != nil {
		return fmt.Errorf("failed to get home directory: %w", err)
	}
	return comp.install(d, home, agent)
}
