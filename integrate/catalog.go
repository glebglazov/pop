package integrate

import (
	"strings"
)

// ComponentID is the stable identifier of an Integration component. These
// strings are part of pop's external contract: later slices use them for
// non-interactive component flags, removal targets, and Doctor's supporting
// evidence reads, so they must not change once shipped. The catalog test pins
// the exact values.
type ComponentID string

const (
	// ComponentStatusWiring is the core component implied by running
	// `pop integrate <agent>` at all: the pane-status hooks (claude, codex,
	// cursor) or the status-sync agent extension (pi, opencode). It is
	// plumbing — it makes the agent report pane status to the Monitor without
	// changing how the agent behaves. See ADR 0010.
	ComponentStatusWiring ComponentID = "status-wiring"

	// ComponentPaneSkill is the opt-in pane skill that lets the agent drive
	// tmux panes. Behavior injection, never installed by the bare integrate
	// path; it returns behind an explicit opt-in in a later slice.
	ComponentPaneSkill ComponentID = "pane-skills"

	// ComponentTaskSkills is the opt-in task planning skill set
	// (grill-with-docs, grill-consolidate, to-spec, to-tasks, wayfinder,
	// prototype, research, setup-matt-pocock-skills).
	ComponentTaskSkills ComponentID = "task-skills"
)

// integrationComponent is one entry in the component catalog: a stable
// identifier, the set of agents that can host it, the embedded source paths it
// renders from, and (once wired) the installer that applies it for an agent.
//
// A non-nil install applies the component directly (status wiring). File-based
// components leave install nil and go through the link installer, driven by
// their sources. ComponentStatusWiring is the sole component the bare integrate
// path installs; the rest are explicit opt-ins.
type Component struct {
	id       ComponentID
	supports map[string]bool
	sources  []string
	install  func(r *run, home, agent string) error
}

func (c Component) supported(agent string) bool {
	return c.supports[strings.ToLower(agent)]
}

// AgentSupported reports whether the component can be hosted by the given agent.
func (c Component) AgentSupported(agent string) bool {
	return c.supported(agent)
}

// agentSet builds a support-matrix set from a list of agent names.
func agentSet(agents ...string) map[string]bool {
	m := make(map[string]bool, len(agents))
	for _, a := range agents {
		m[a] = true
	}
	return m
}

// integrationCatalog is the single registry of Integration components. Every
// other module (the integrate path today; the wizard, refresh, and Doctor in
// later slices) consumes the catalog rather than hardcoding component
// knowledge. Adding a future component means adding an entry here.
//
// Support matrix: opencode hosts the pane skill as a flat agent file and the
// task planning skills as skill directories under ~/.config/opencode/skills/.
// Unsupported pairs are reported as not-supported rather than receiving a
// degraded install. See ADR 0010.
var catalog = []Component{
	{
		id:       ComponentStatusWiring,
		supports: allRegisteredAgents(),
		install:  installStatusWiring,
	},
	{
		id:       ComponentPaneSkill,
		supports: allRegisteredAgents(),
		sources:  []string{"skills/pop/tmux-pane.md"},
	},
	{
		id:       ComponentTaskSkills,
		supports: allRegisteredAgents(),
		// Each source is a skill directory (SKILL.md plus any companion
		// documents). grill-with-docs ships two companion format files that
		// must ride alongside its body so its relative references resolve.
		sources: []string{
			"skills/pop/grill-with-docs",
			"skills/pop/grill-consolidate",
			"skills/pop/to-spec",
			"skills/pop/to-tasks",
			"skills/pop/wayfinder",
			"skills/pop/prototype",
			"skills/pop/research",
			"skills/pop/setup-matt-pocock-skills",
		},
	},
}

// LookupComponent returns the catalog entry for the given identifier.
func LookupComponent(id ComponentID) (Component, bool) {
	for _, c := range catalog {
		if c.id == id {
			return c, true
		}
	}
	return Component{}, false
}

// Components returns the supported integration component IDs in catalog order.
func Components() []ComponentID {
	ids := make([]ComponentID, len(catalog))
	for i, c := range catalog {
		ids[i] = c.id
	}
	return ids
}

// installStatusWiring applies the status-wiring component for an agent via
// that agent's integration profile. Behavior is byte-identical to the previous
// per-agent integrate functions; only the skill installs that used to sit
// alongside them are gone.
func installStatusWiring(r *run, home, agent string) error {
	p, ok := LookupProfile(agent)
	if !ok {
		return unknownAgentError(agent)
	}
	return p.InstallStatusWiring(r, home)
}
