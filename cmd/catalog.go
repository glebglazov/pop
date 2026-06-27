package cmd

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
	// cursor) or the status-sync agent extension (pi, opencode). It is
	// plumbing — it makes the agent report pane status to the Monitor without
	// changing how the agent behaves. See ADR 0010.
	ComponentStatusWiring ComponentID = "status-wiring"

	// ComponentPaneSkill is the opt-in pane skill that lets the agent drive
	// tmux panes. Behavior injection, never installed by the bare integrate
	// path; it returns behind an explicit opt-in in a later slice.
	ComponentPaneSkill ComponentID = "pane-skill"

	// ComponentTaskSkills is the opt-in task planning skill set
	// (grill-with-docs, grill-consolidate, to-prd, to-tasks).
	ComponentTaskSkills ComponentID = "task-skills"
)

// integrationComponent is one entry in the component catalog: a stable
// identifier, the set of agents that can host it, the embedded source paths it
// renders from, and (once wired) the installer that applies it for an agent.
//
// A non-nil install applies the component directly (status wiring). File-based
// components leave install nil and go through the link installer, driven by
// their sources. ComponentStatusWiring is core — always installed by the
// integrate verb; the components marked defaultOn install by default too, opt-out
// only (ADR 0064).
type integrationComponent struct {
	id       ComponentID
	supports map[string]bool
	// sources lists embedded source paths (within skillFiles) this component
	// renders from. Empty for components whose sources are not file-based
	// (status wiring) or not yet embedded (task skills).
	sources []string
	install func(d *integrateDeps, home, agent string) error
	// defaultOn marks an opt-in component that `pop integrate <agent>` installs
	// by default, declined per-invocation with the matching `--no-*` flag and,
	// in slice 08, with a persisted opt-out (ADR 0064). Core status wiring is
	// always installed and does not set this.
	defaultOn bool
}

// supported reports whether the component can be hosted by the given agent.
func (c integrationComponent) supported(agent string) bool {
	return c.supports[strings.ToLower(agent)]
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
// other module (the integrate path, refresh, and Doctor) consumes the catalog
// rather than hardcoding component knowledge. Adding a future component means
// adding an entry here; marking it defaultOn makes integrate install it by
// default (ADR 0064).
//
// Support matrix: codex cannot host either skill component; opencode hosts the
// pane skill (in its flat single-file form) but not the task planning
// skills. Unsupported pairs are skipped silently rather than receiving a
// degraded install. See ADR 0010, ADR 0064.
var integrationCatalog = []integrationComponent{
	{
		id:       ComponentStatusWiring,
		supports: agentSet("claude", "codex", "pi", "opencode", "cursor"),
		install:  installStatusWiring,
	},
	{
		id:        ComponentPaneSkill,
		supports:  agentSet("claude", "pi", "cursor", "opencode"),
		sources:   []string{"skills/pop/tmux-pane.md"},
		defaultOn: true,
	},
	{
		id:        ComponentTaskSkills,
		supports:  agentSet("claude", "pi", "cursor"),
		defaultOn: true,
		// Each source is a skill directory (SKILL.md plus any companion
		// documents). grill-with-docs ships two companion format files that
		// must ride alongside its body so its relative references resolve.
		sources: []string{
			"skills/pop/grill-with-docs",
			"skills/pop/grill-consolidate",
			"skills/pop/to-prd",
			"skills/pop/to-tasks",
		},
	},
}

// defaultComponentIDs returns the opt-in components installed by default, in
// catalog order (ADR 0064). The core status wiring is always installed and is
// not part of this set.
func defaultComponentIDs() []ComponentID {
	var ids []ComponentID
	for _, c := range integrationCatalog {
		if c.defaultOn {
			ids = append(ids, c.id)
		}
	}
	return ids
}

// lookupComponent returns the catalog entry for the given identifier.
func lookupComponent(id ComponentID) (integrationComponent, bool) {
	for _, c := range integrationCatalog {
		if c.id == id {
			return c, true
		}
	}
	return integrationComponent{}, false
}

// installStatusWiring applies the status-wiring component for an agent by
// dispatching to that agent's hook merge or extension install. Behavior is
// byte-identical to the previous per-agent integrate functions; only the
// skill installs that used to sit alongside them are gone.
func installStatusWiring(d *integrateDeps, home, agent string) error {
	switch strings.ToLower(agent) {
	case "claude":
		return installClaudeHooks(d, home)
	case "codex":
		return installCodexHooks(d, home)
	case "pi":
		return installPiExtension(d, home)
	case "opencode":
		return installOpencodePlugin(d, home)
	case "cursor":
		return installCursorHooks(d, home)
	default:
		return fmt.Errorf("unknown agent %q (expected: claude, codex, pi, opencode, cursor)", agent)
	}
}
