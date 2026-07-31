package integrate

import (
	"fmt"
	"path/filepath"
	"strings"
)

// SkillLayout is how an agent hosts a single-file skill on disk. Multi-file
// skills always use a skill directory for every registered agent.
type SkillLayout int

const (
	// SkillLayoutDirectory hosts a skill as <name>/SKILL.md.
	SkillLayoutDirectory SkillLayout = iota
	// SkillLayoutFlatFile hosts a skill as a single <name>.md file.
	SkillLayoutFlatFile
)

// HookDialect is profile data describing one JSON-hook entry shape. Shared
// install/remove/detect helpers consume a dialect without branching on agent
// name — nested (claude/codex) versus flat (cursor) differ only here.
type HookDialect struct {
	// Wrap builds one hook entry for a command string.
	Wrap func(command string) map[string]interface{}
	// IsPop reports whether an existing entry is a pop-owned hook.
	IsPop func(entry interface{}) bool
	// EnsureVersion, when true, writes "version": 1 if the key is absent.
	EnsureVersion bool
}

var (
	nestedHookDialect = HookDialect{
		Wrap: func(command string) map[string]interface{} {
			return map[string]interface{}{
				"hooks": []interface{}{
					map[string]interface{}{
						"type":    "command",
						"command": command,
					},
				},
			}
		},
		IsPop: isPopHook,
	}
	flatHookDialect = HookDialect{
		Wrap: func(command string) map[string]interface{} {
			return map[string]interface{}{"command": command}
		},
		IsPop:         isCursorPopHook,
		EnsureVersion: true,
	}
)

// AgentProfile is the per-agent record of how each Integration component is
// wired for one agent: its status-wiring install, remove, and detect
// behaviour, its Agent install path roots for file-based components, and the
// legacy artifacts to prune. One profile per supported agent; the profile is
// what makes a JSON-hook agent and a file-drop extension agent interchangeable
// to the rest of integrate. It is a struct with function fields, not a Go
// interface — the agents are static and known at compile time.
// Path resolution takes Deps as well as the user's home directory because an
// agent may relocate its own root through the environment (kimi's
// KIMI_CODE_HOME); the env read goes through the same seam as every other one.
type AgentProfile struct {
	Name string

	InstallStatusWiring func(r *run, home string) error
	RemoveStatusWiring  func(d *Deps, home string) error
	DetectStatusWiring  func(d *Deps, home string) (bool, error)

	// SkillDir returns the Agent install path root for a file-based component.
	SkillDir func(d *Deps, home string, id ComponentID) string

	// LegacyArtifacts returns paths to prune when installing a component.
	LegacyArtifacts func(d *Deps, home string, id ComponentID) []string

	// SkillLayout selects how single-file skills are rendered for this agent.
	SkillLayout SkillLayout
}

// profiles is the agent integration profile registry: exactly one entry per
// supported agent. Adding an agent is one new entry here and no other edits
// in the dispatch paths — call sites resolve through LookupProfile.
var profiles = []AgentProfile{
	jsonHookAgentProfile(
		"claude",
		".claude/settings.json",
		popHooks,
		nestedHookDialect,
		func(_ *Deps, home string, _ ComponentID) string {
			return filepath.Join(home, ".claude", "skills")
		},
		func(_ *Deps, home string, id ComponentID) []string {
			if id == ComponentPaneSkill {
				return []string{filepath.Join(home, ".claude", "commands", "pop", "pane.md")}
			}
			return nil
		},
		SkillLayoutDirectory,
	),
	jsonHookAgentProfile(
		"codex",
		".codex/hooks.json",
		codexPopHooks,
		nestedHookDialect,
		func(_ *Deps, home string, _ ComponentID) string {
			return filepath.Join(home, ".codex", "skills")
		},
		nil,
		SkillLayoutDirectory,
	),
	extensionAgentProfile(
		"pi",
		".pi/agent/extensions/pop-status-sync.ts",
		piExtensionFile,
		"pi extension",
		func(_ *Deps, home string, _ ComponentID) string {
			return filepath.Join(home, ".pi", "agent", "skills")
		},
		nil,
		SkillLayoutDirectory,
	),
	extensionAgentProfile(
		"opencode",
		".config/opencode/plugins/pop-status-sync.ts",
		opencodeExtensionFile,
		"opencode plugin",
		func(_ *Deps, home string, id ComponentID) string {
			if id == ComponentPaneSkill {
				return filepath.Join(home, ".config", "opencode", "agent")
			}
			return filepath.Join(home, ".config", "opencode", "skills")
		},
		nil,
		SkillLayoutFlatFile,
	),
	jsonHookAgentProfile(
		"cursor",
		".cursor/hooks.json",
		cursorPopHooks,
		flatHookDialect,
		func(_ *Deps, home string, _ ComponentID) string {
			return filepath.Join(home, ".cursor", "skills")
		},
		nil,
		SkillLayoutDirectory,
	),
	tomlHookAgentProfile(
		"kimi",
		kimiPopHooks,
		func(d *Deps, home string) string {
			return filepath.Join(kimiHome(d, home), "config.toml")
		},
		func(d *Deps, home string, _ ComponentID) string {
			return filepath.Join(kimiHome(d, home), "skills")
		},
		nil,
		SkillLayoutDirectory,
	),
}

// Agents is the ordered list of agents Integration refresh and Doctor iterate.
// Derived from the profile registry so the two cannot drift.
var Agents = registeredAgentNames()

func registeredAgentNames() []string {
	names := make([]string, len(profiles))
	for i, p := range profiles {
		names[i] = p.Name
	}
	return names
}

func allRegisteredAgents() map[string]bool {
	return agentSet(registeredAgentNames()...)
}

// LookupProfile returns the Agent integration profile for the given agent name.
func LookupProfile(agent string) (AgentProfile, bool) {
	agent = strings.ToLower(agent)
	for _, p := range profiles {
		if p.Name == agent {
			return p, true
		}
	}
	return AgentProfile{}, false
}

func unknownAgentError(agent string) error {
	return fmt.Errorf("unknown agent %q (expected: %s)", agent, strings.Join(Agents, ", "))
}

func jsonHookAgentProfile(
	name, relSettingsPath string,
	hooks []hookSpec,
	dialect HookDialect,
	skillDir func(d *Deps, home string, id ComponentID) string,
	legacy func(d *Deps, home string, id ComponentID) []string,
	layout SkillLayout,
) AgentProfile {
	legacy = orNoLegacyArtifacts(legacy)
	settingsPath := func(home string) string {
		return filepath.Join(home, filepath.FromSlash(relSettingsPath))
	}
	return AgentProfile{
		Name: name,
		InstallStatusWiring: func(r *run, home string) error {
			return installJSONHooks(r, settingsPath(home), hooks, dialect)
		},
		RemoveStatusWiring: func(d *Deps, home string) error {
			return stripJSONHooks(d, settingsPath(home), dialect)
		},
		DetectStatusWiring: func(d *Deps, home string) (bool, error) {
			return jsonHasPopHooks(d, settingsPath(home), dialect.IsPop)
		},
		SkillDir:        skillDir,
		LegacyArtifacts: legacy,
		SkillLayout:     layout,
	}
}

func extensionAgentProfile(
	name, relPath string,
	content []byte,
	installedLabel string,
	skillDir func(d *Deps, home string, id ComponentID) string,
	legacy func(d *Deps, home string, id ComponentID) []string,
	layout SkillLayout,
) AgentProfile {
	legacy = orNoLegacyArtifacts(legacy)
	extPath := func(home string) string {
		return filepath.Join(home, filepath.FromSlash(relPath))
	}
	return AgentProfile{
		Name: name,
		InstallStatusWiring: func(r *run, home string) error {
			return installExtensionFile(r, extPath(home), content, installedLabel)
		},
		RemoveStatusWiring: func(d *Deps, home string) error {
			return removeExtensionFile(d, extPath(home))
		},
		DetectStatusWiring: func(d *Deps, home string) (bool, error) {
			return fileExists(d, extPath(home))
		},
		SkillDir:        skillDir,
		LegacyArtifacts: legacy,
		SkillLayout:     layout,
	}
}

// tomlHookAgentProfile builds the profile for an agent whose status wiring is a
// run of [[hooks]] blocks inside a hand-authored TOML config (kimi). The config
// path is a func of Deps because the agent's root is env-relocatable, unlike the
// JSON-hook and extension agents whose paths are fixed under the user's home.
func tomlHookAgentProfile(
	name string,
	hooks []hookSpec,
	configPath func(d *Deps, home string) string,
	skillDir func(d *Deps, home string, id ComponentID) string,
	legacy func(d *Deps, home string, id ComponentID) []string,
	layout SkillLayout,
) AgentProfile {
	legacy = orNoLegacyArtifacts(legacy)
	return AgentProfile{
		Name: name,
		InstallStatusWiring: func(r *run, home string) error {
			return installTOMLHooks(r, configPath(r.deps, home), hooks)
		},
		RemoveStatusWiring: func(d *Deps, home string) error {
			return stripTOMLHooks(d, configPath(d, home))
		},
		DetectStatusWiring: func(d *Deps, home string) (bool, error) {
			return tomlHasPopHooks(d, configPath(d, home))
		},
		SkillDir:        skillDir,
		LegacyArtifacts: legacy,
		SkillLayout:     layout,
	}
}

func orNoLegacyArtifacts(legacy func(d *Deps, home string, id ComponentID) []string) func(*Deps, string, ComponentID) []string {
	if legacy != nil {
		return legacy
	}
	return func(*Deps, string, ComponentID) []string { return nil }
}
