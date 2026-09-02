package integrate

import (
	"fmt"
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
	// cursor, kimi) or the status-sync agent extension (pi, opencode). It is
	// plumbing — it makes the agent report pane status to the Monitor without
	// changing how the agent behaves. See ADR 0010.
	ComponentStatusWiring ComponentID = "status-wiring"

	// ComponentPaneSkill is the opt-in pane skills that let the agent drive
	// tmux panes and spawn another agent CLI into one. Behavior injection,
	// never installed by the bare integrate path; it returns behind an explicit
	// opt-in in a later slice.
	ComponentPaneSkill ComponentID = "pane-skills"

	// ComponentTaskSkills is the opt-in task planning skill set
	// (grilling, grill-with-docs, grill-with-docs-fast, grill-with-map,
	// grill-consolidate, domain-modeling, to-spec, to-tasks,
	// wayfinder, prototype, research, setup-matt-pocock-skills, spend-audit).
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
		sources: []string{
			"skills/pop/tmux-pane.md",
			"skills/pop/spawn-agent.md",
		},
	},
	{
		id:       ComponentTaskSkills,
		supports: allRegisteredAgents(),
		// Each source is a skill directory (SKILL.md plus any companion
		// documents). Companion files must ride alongside a body so its
		// relative references resolve; sharedSkillDocs below hands the two
		// format documents domain-modeling owns to the one other skill that
		// writes compatible glossary and ADR documents without loading the
		// discipline.
		sources: []string{
			"skills/pop/grilling",
			"skills/pop/grill-with-docs",
			"skills/pop/grill-with-docs-fast",
			"skills/pop/grill-with-map",
			"skills/pop/grill-consolidate",
			"skills/pop/domain-modeling",
			"skills/pop/to-spec",
			"skills/pop/to-tasks",
			"skills/pop/wayfinder",
			"skills/pop/prototype",
			"skills/pop/research",
			"skills/pop/setup-matt-pocock-skills",
			"skills/pop/spend-audit",
		},
	},
}

// sharedSkillDocOwners maps a shared document to the skill source holding its
// canonical copy. Ownership is per document, not one owner for all of them
// (ADR-0253 decision 6): each document belongs to the skill that owns its
// meaning. domain-modeling owns the glossary and ADR disciplines, so it owns the
// two documents that spell those disciplines out — the same ownership upstream
// has. grill-with-docs owns the session rules both grilling composers follow.
// A document renders as an ordinary companion of its owner; renderSharedSkillDocs
// copies it into each other consumer's directory as well.
var sharedSkillDocOwners = map[string]string{
	"ADR-FORMAT.md":     "skills/pop/domain-modeling",
	"CONTEXT-FORMAT.md": "skills/pop/domain-modeling",
	"GRILL-SESSION.md":  "skills/pop/grill-with-docs",
}

// sharedSkillDocs maps a skill's base name to the documents it receives a copy
// of from that document's owner. Two receivers, each for the same reason — the
// rules are identical and the destination is not:
//
//   - grill-with-map drafts the same `+`/`~`/`-` glossary ops and the same ADR
//     body as domain-modeling does, but into a Map rather than the repository, so
//     it must read the identical rules while deliberately not loading the
//     discipline that writes them (ADR-0225 decision 3).
//   - grill-with-docs-fast runs the same session as grill-with-docs and closes it
//     the same way, but cannot load its sibling to learn how: a human-opened
//     skill is not loadable by another skill, so the shared rules travel as a
//     document (ADR-0253 decision 6).
//
// Every other skill that needs those rules loads the skill that owns them. The
// copy lands beside the receiving body, so a `./CONTEXT-FORMAT.md` link resolves
// wherever the skill is installed. An owner is absent from this map for its own
// documents: those copies are already its own directory's files.
var sharedSkillDocs = map[string][]string{
	"grill-with-map":       {"ADR-FORMAT.md", "CONTEXT-FORMAT.md"},
	"grill-with-docs-fast": {"GRILL-SESSION.md"},
}

// sharedDocSource returns the embedded path of a shared document, resolved
// through its own owner.
func sharedDocSource(name string) (string, error) {
	owner, ok := sharedSkillDocOwners[name]
	if !ok {
		return "", fmt.Errorf("shared skill doc %q has no declared owner", name)
	}
	return owner + "/" + name, nil
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
