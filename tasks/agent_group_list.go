package tasks

import "github.com/glebglazov/pop/config"

// ResolveAgentGroupPresets answers one Work group's agent list against the
// implement list — the one rule verify, refine and routines share, so that the
// three cannot drift into three readings of the same config. The group's own
// entries win; a group that states none falls through to [work.implement].agents
// and, failing that, to the built-in default preset; and an override of
// `agents = []` is refused rather than walked on from, because that emptiness is
// a human's instruction and not an absence (ADR-0202 decision 6).
//
// The refusal is an error and not an empty list. Every agent walk substitutes
// the built-in default for a list it is handed empty, so a group that merely
// stopped resolving here would run claude — the fallthrough it disabled, taken
// one rung lower and silently.
func ResolveAgentGroupPresets(list config.AgentList, cfg *config.Config) ([]string, error) {
	if len(list.Commands) > 0 {
		return list.Commands, nil
	}
	if list.EmptyOverride {
		return nil, emptyAgentListOverrideErr(list.Key)
	}
	return ResolveDefaultAgentPresets(nil, "", false, cfg), nil
}

// emptyAgentListOverrideErr is the one sentence all three groups give for an
// explicit empty list. It names the key so the human knows which override to
// remove, the state being one they can only have reached deliberately.
func emptyAgentListOverrideErr(key string) error {
	return exitErr(ExitSetup,
		"%s is overridden to an empty list: no agent is configured and the fallthrough to work.implement.agents is disabled; remove the override or name an agent",
		key)
}
