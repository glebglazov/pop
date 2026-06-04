package cmd

import (
	"fmt"
	"strings"
)

// ComponentID is the stable identifier of an Integration component. These
// strings are part of pop's external contract: later slices use them for
// non-interactive component flags, removal targets, and Doctor's per-agent
// state table, so they must not change once shipped. The catalog test pins
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

	// ComponentWorkloadSkills is the opt-in workload planning skill set
	// (grill-with-docs, to-prd, to-issues).
	ComponentWorkloadSkills ComponentID = "workload-skills"

	// ComponentWorkloadGitignore is the opt-in global-gitignore sub-step of
	// the workload component (appends the thoughts/ ignore line).
	ComponentWorkloadGitignore ComponentID = "workload-gitignore"
)

// integrationComponent is one entry in the component catalog: a stable
// identifier, the set of agents that can host it, the embedded source paths it
// renders from, and (once wired) the installer that applies it for an agent.
//
// A non-nil install applies the component directly (status wiring; the
// gitignore step, whose effect is global and agent-independent). File-based
// components leave install nil and go through the link installer, driven by
// their sources. ComponentStatusWiring is the sole component the bare integrate
// path installs; the rest are explicit opt-ins.
type integrationComponent struct {
	id       ComponentID
	supports map[string]bool
	// sources lists embedded source paths (within skillFiles) this component
	// renders from. Empty for components whose sources are not file-based
	// (status wiring, gitignore) or not yet embedded (workload skills).
	sources []string
	install func(d *integrateDeps, home, agent string) error
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
// other module (the integrate path today; the wizard, refresh, and Doctor in
// later slices) consumes the catalog rather than hardcoding component
// knowledge. Adding a future component means adding an entry here.
//
// Support matrix: codex cannot host either skill component; opencode hosts the
// pane skill (in its flat single-file form) but not the workload planning
// skills. Unsupported pairs are reported as not-supported rather than receiving
// a degraded install. See ADR 0010.
var integrationCatalog = []integrationComponent{
	{
		id:       ComponentStatusWiring,
		supports: agentSet("claude", "codex", "pi", "opencode", "cursor"),
		install:  installStatusWiring,
	},
	{
		id:       ComponentPaneSkill,
		supports: agentSet("claude", "pi", "cursor", "opencode"),
		sources:  []string{"skills/pop/pane.md"},
	},
	{
		id:       ComponentWorkloadSkills,
		supports: agentSet("claude", "pi", "cursor"),
		// Each source is a skill directory (SKILL.md plus any companion
		// documents). grill-with-docs ships two companion format files that
		// must ride alongside its body so its relative references resolve.
		sources: []string{
			"skills/pop/grill-with-docs",
			"skills/pop/to-prd",
			"skills/pop/to-issues",
		},
	},
	{
		id:       ComponentWorkloadGitignore,
		supports: agentSet("claude", "codex", "pi", "opencode", "cursor"),
		install:  installGitignore,
	},
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
