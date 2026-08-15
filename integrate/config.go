package integrate

import (
	"fmt"

	"github.com/glebglazov/pop/config"
)

// baselineLoader loads pop config and returns optional skill
// components from the merged [integrations] skills list. Status wiring is
// not included — callers always install it separately.
func BaselineLoader(cd *config.Deps) ([]ComponentID, error) {
	cfg, err := config.LoadWith(cd, config.DefaultConfigPathWith(cd))
	if err != nil {
		return nil, fmt.Errorf("config: %w", err)
	}
	skills, err := cfg.IntegrationsSkills()
	if err != nil {
		return nil, err
	}
	seen := map[ComponentID]bool{}
	var baseline []ComponentID
	for _, alias := range skills {
		id, ok := ComponentForSkillAlias(alias)
		if !ok || seen[id] {
			continue
		}
		seen[id] = true
		baseline = append(baseline, id)
	}
	return baseline, nil
}

func integrationSkillAliasForOptOut(id ComponentID) (string, bool) {
	switch id {
	case ComponentPaneSkill:
		return config.IntegrationSkillPane, true
	case ComponentTaskSkills:
		return config.IntegrationSkillTasks, true
	default:
		return "", false
	}
}

func ComponentForSkillAlias(alias string) (ComponentID, bool) {
	switch alias {
	case config.IntegrationSkillPane:
		return ComponentPaneSkill, true
	case config.IntegrationSkillTasks:
		return ComponentTaskSkills, true
	default:
		return "", false
	}
}

// ApplyComponentOptOuts records the invocation's component preferences once per
// integrate run. Bare integrate takes back every decline; --no-* declines the
// corresponding skills, which states the reduced list in the override layer so
// the decline outlives the run and beats a hand-authored list naming the same
// component (ADR-0065's three-layer merge, retargeted by ADR-0212 decision 5).
func ApplyComponentOptOuts(cd *config.Deps, bareIntegrate bool, explicitOptOuts map[ComponentID]bool) error {
	if bareIntegrate {
		return config.ClearIntegrationSkillsDeclineWith(cd)
	}
	if len(explicitOptOuts) == 0 {
		return nil
	}
	var aliases []string
	for id := range explicitOptOuts {
		alias, ok := integrationSkillAliasForOptOut(id)
		if !ok {
			continue
		}
		aliases = append(aliases, alias)
	}
	return config.DeclineIntegrationSkillsWith(cd, aliases...)
}
