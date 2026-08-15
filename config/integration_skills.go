package config

import "fmt"

// This file is the integrate opt-out as a stated value: `pop integrate
// --no-<component>` declining an optional component, and bare `pop integrate`
// taking that decline back.
//
// Declining a component is a human stating intent, so it lands in the override
// layer rather than in a gap-filler pop wrote for itself (ADR-0212 decision 5).
// That is also what makes the flag mean what it says: recorded below config.toml
// (ADR-0065's runtime tier), an opt-out lost to any skills list the user had
// hand-authored, so declining a component that list named changed nothing.
//
// The unit is the whole list, because that is the unit of an override
// (ADR-0202 decision 2): the opt-out states the list it wants, computed from the
// list in force, rather than a subtraction the layer would have to remember how
// to apply.

// integrationSkillsKey is the dotted config key the two entry points state and
// clear — the same spelling `pop config keys` prints and the Config dashboard
// edits, so a scripted decline and an edited one are one entry in one file.
const integrationSkillsKey = "integrations.skills"

// DeclineIntegrationSkills states the Integration skills list without the given
// aliases, so the declined components stay uninstalled however the layers below
// are later edited. Declining an alias no list holds is a no-op — the resulting
// list is the one already in force.
func DeclineIntegrationSkills(aliases ...string) error {
	return DeclineIntegrationSkillsWith(defaultDeps, aliases...)
}

// DeclineIntegrationSkillsWith is the injectable variant. The list it states is
// the merged one minus the aliases: an opt-out is a subtraction from what is in
// force today, which is what makes declining one component leave the others
// exactly as the human had them.
func DeclineIntegrationSkillsWith(d *Deps, aliases ...string) error {
	if len(aliases) == 0 {
		return nil
	}
	cfg, err := LoadWith(d, DefaultConfigPathWith(d))
	if err != nil {
		return fmt.Errorf("config: %w", err)
	}
	skills, err := cfg.IntegrationsSkills()
	if err != nil {
		return err
	}
	kept := removeIntegrationSkillAliases(skills, aliases)
	value := make([]any, 0, len(kept))
	for _, skill := range kept {
		value = append(value, skill)
	}
	return SetOverrideValueWith(d, integrationSkillsKey, value)
}

// ClearIntegrationSkillsDecline removes the stated skills list, so the merged
// baseline — the embedded defaults, or whatever the user's config.toml says —
// is in force again. It is what bare `pop integrate` runs: re-asserting the full
// baseline means taking back every decline, not just installing what is left.
func ClearIntegrationSkillsDecline() error {
	return ClearIntegrationSkillsDeclineWith(defaultDeps)
}

// ClearIntegrationSkillsDeclineWith is the injectable variant.
func ClearIntegrationSkillsDeclineWith(d *Deps) error {
	return DeleteOverrideValueWith(d, integrationSkillsKey)
}
