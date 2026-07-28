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

// ApplyRuntimeConfig mutates config.runtime.toml once per integrate
// invocation. Bare integrate clears runtime [integrations] overrides; --no-*
// removes the corresponding skill aliases from the runtime layer (ADR 0065).
func ApplyRuntimeConfig(cd *config.Deps, bareIntegrate bool, explicitOptOuts map[ComponentID]bool) error {
	if bareIntegrate {
		return config.ClearRuntimeIntegrationsWith(cd)
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
	return config.RemoveRuntimeIntegrationSkillsWith(cd, aliases...)
}

