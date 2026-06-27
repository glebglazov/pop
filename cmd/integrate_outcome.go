package cmd

import (
	"fmt"
	"io"
	"path/filepath"
	"strings"

	"github.com/glebglazov/pop/config"
)

const statusWiringOutcomeName = "status-wiring"

// integrateOutcome records one integrate stdout line: agent, resolved skill name
// (or status-wiring for hooks), and what happened.
type integrateOutcome struct {
	Agent string
	Skill string
	Label string
}

func (o integrateOutcome) verboseOnly(updateExistingPath bool) bool {
	switch o.Label {
	case "already current":
		return true
	case "skipped (opted out)":
		return updateExistingPath
	default:
		return false
	}
}

func printIntegrateOutcomes(out io.Writer, outcomes []integrateOutcome, verbose, explicitPath bool) {
	if out == nil {
		return
	}
	printed := 0
	for _, o := range outcomes {
		if o.verboseOnly(!explicitPath) && !verbose {
			continue
		}
		fmt.Fprintf(out, "  %s  %s  %s\n", o.Agent, o.Skill, o.Label)
		printed++
	}
	if printed == 0 {
		fmt.Fprintln(out, "nothing to do")
	}
}

func statusWiringOutcome(agent, label string) integrateOutcome {
	return integrateOutcome{Agent: agent, Skill: statusWiringOutcomeName, Label: label}
}

func installLabel(isNew, isUpdate bool) string {
	switch {
	case isNew:
		return "added"
	case isUpdate:
		return "updated"
	default:
		return "already current"
	}
}

func legacyComponentIDs(id ComponentID) []string {
	if id == ComponentPaneSkill {
		return []string{"pane-skill"}
	}
	return nil
}

func fileComponentRenderRoots(dataDir, agent string, id ComponentID) []string {
	roots := []string{filepath.Join(dataDir, "integrations", agent, string(id))}
	for _, legacyID := range legacyComponentIDs(id) {
		roots = append(roots, filepath.Join(dataDir, "integrations", agent, legacyID))
	}
	return roots
}

func symlinkUnderRenderRoot(target string, renderRoots []string) bool {
	target = filepath.Clean(target)
	for _, root := range renderRoots {
		root = filepath.Clean(root)
		if target == root || strings.HasPrefix(target, root+string(filepath.Separator)) {
			return true
		}
	}
	return false
}

func embedBasesForComponent(id ComponentID) ([]string, error) {
	comp, ok := lookupComponent(id)
	if !ok {
		return nil, fmt.Errorf("unknown component %q", id)
	}
	bases := make([]string, 0, len(comp.sources))
	for _, src := range comp.sources {
		bases = append(bases, embedBaseFromSource(src))
	}
	return bases, nil
}

func embedBaseFromSource(src string) string {
	if strings.HasSuffix(src, ".md") {
		return strings.TrimSuffix(filepath.Base(src), ".md")
	}
	return filepath.Base(src)
}

func orderedResolvedSkills(id ComponentID, agent, prefix string) ([]string, error) {
	bases, err := embedBasesForComponent(id)
	if err != nil {
		return nil, err
	}
	names := make([]string, len(bases))
	for i, base := range bases {
		names[i] = prefix + base
	}
	return names, nil
}

func possibleNamesForEmbedBase(base string) []string {
	return []string{config.DefaultSkillsPrefix + base, base}
}

func prunedNamesForEmbedBase(base string, pruned []string) []string {
	possible := map[string]bool{}
	for _, name := range possibleNamesForEmbedBase(base) {
		possible[name] = true
	}
	var out []string
	for _, name := range pruned {
		if possible[name] {
			out = append(out, name)
		}
	}
	// Old prefixed names before bare (pair stale removal with the new line).
	order := map[string]int{}
	for i, name := range possibleNamesForEmbedBase(base) {
		order[name] = i
	}
	for i := 0; i < len(out); i++ {
		for j := i + 1; j < len(out); j++ {
			if order[out[i]] > order[out[j]] {
				out[i], out[j] = out[j], out[i]
			}
		}
	}
	return out
}

func fileComponentOutcomesInCatalogOrder(
	agent string,
	id ComponentID,
	prefix string,
	installedBefore map[string]bool,
	staleBefore bool,
	pruned []string,
	preConflict map[string]string,
	postConflict map[string]string,
	overwritten map[string]string,
) []integrateOutcome {
	bases, err := embedBasesForComponent(id)
	if err != nil {
		return nil
	}
	var outcomes []integrateOutcome
	for _, base := range bases {
		current := prefix + base
		for _, staleName := range prunedNamesForEmbedBase(base, pruned) {
			outcomes = append(outcomes, integrateOutcome{
				Agent: agent, Skill: staleName, Label: "removed (stale)",
			})
		}
		if path, ok := preConflict[current]; ok {
			outcomes = append(outcomes, integrateOutcome{
				Agent: agent, Skill: current, Label: conflictSkipLabel(agent, path),
			})
			continue
		}
		if path, ok := overwritten[current]; ok {
			outcomes = append(outcomes, integrateOutcome{
				Agent: agent, Skill: current,
				Label: "overwritten (not owned by pop at " + path + ")",
			})
			continue
		}
		if path, ok := postConflict[current]; ok {
			outcomes = append(outcomes, integrateOutcome{
				Agent: agent, Skill: current, Label: conflictSkipLabel(agent, path),
			})
			continue
		}
		label := installLabel(!installedBefore[current], installedBefore[current] && staleBefore)
		outcomes = append(outcomes, integrateOutcome{Agent: agent, Skill: current, Label: label})
	}
	return outcomes
}

func optOutSkipOutcomes(d *integrateDeps, agent string, id ComponentID) ([]integrateOutcome, error) {
	names, err := orderedResolvedSkills(id, agent, d.resolveSkillsPrefix())
	if err != nil {
		return nil, err
	}
	outcomes := make([]integrateOutcome, len(names))
	for i, name := range names {
		outcomes[i] = integrateOutcome{Agent: agent, Skill: name, Label: "skipped (opted out)"}
	}
	return outcomes, nil
}

func optOutRemoveOutcomes(d *integrateDeps, home, agent string, id ComponentID) ([]integrateOutcome, error) {
	prefix := d.resolveSkillsPrefix()
	names, err := orderedResolvedSkills(id, agent, prefix)
	if err != nil {
		return nil, err
	}
	installedBefore, err := fileComponentInstalledNames(d, home, id, agent)
	if err != nil {
		return nil, err
	}
	quietD := *d
	quietD.stdout = nil
	if len(installedBefore) > 0 {
		if err := removeComponent(&quietD, home, id, agent); err != nil {
			return nil, err
		}
	}
	outcomes := make([]integrateOutcome, len(names))
	for i, name := range names {
		label := "skipped (opted out)"
		if installedBefore[name] {
			label = "removed (opted out)"
		}
		outcomes[i] = integrateOutcome{Agent: agent, Skill: name, Label: label}
	}
	return outcomes, nil
}

func preInstallSkillConflicts(d *integrateDeps, home, agent string, id ComponentID, prefix string) (map[string]string, error) {
	tree, err := renderComponent(id, agent, prefix)
	if err != nil {
		return nil, err
	}
	dataDir, err := d.dataDir()
	if err != nil {
		return nil, err
	}
	integrationsRoot := filepath.Join(dataDir, "integrations")
	agentDir, err := agentSkillDir(home, agent, id)
	if err != nil {
		return nil, err
	}
	conflicts := map[string]string{}
	seen := map[string]bool{}
	for rel := range tree {
		name := firstSegment(rel)
		if seen[name] {
			continue
		}
		seen[name] = true
		p, conflict, err := skillConflict(d, agentDir, name, integrationsRoot, prefix)
		if err != nil {
			return nil, err
		}
		if conflict {
			conflicts[name] = p
		}
	}
	return conflicts, nil
}

func integrateOutcomeLabel(outcomes []integrateOutcome, skill string) (string, bool) {
	for _, o := range outcomes {
		if o.Skill == skill {
			return o.Label, true
		}
	}
	return "", false
}

func integrateOutcomesInclude(outcomes []integrateOutcome, skill, label string) bool {
	got, ok := integrateOutcomeLabel(outcomes, skill)
	return ok && got == label
}

func overwrittenSkillPaths(prefix string, id ComponentID, agent string, paths []string) map[string]string {
	bases, err := embedBasesForComponent(id)
	if err != nil {
		return nil
	}
	out := map[string]string{}
	for _, p := range paths {
		baseName := filepath.Base(p)
		for _, base := range bases {
			current := prefix + base
			for _, cand := range conflictCandidates(current, prefix) {
				if baseName == cand {
					out[current] = p
				}
			}
		}
	}
	return out
}
