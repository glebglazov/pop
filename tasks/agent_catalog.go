package tasks

import (
	"os/exec"
	"sort"

	"github.com/glebglazov/pop/config"
)

const DefaultAgentPreset = "claude"

var agentCatalogOrder = []string{"claude", "opencode", "cursor", "codex", "pi"}

// AgentCatalogRow describes Pop's recognition, PATH availability, and resolved
// effort ladder for one agent row.
type AgentCatalogRow struct {
	Agent        string
	Binary       string
	Found        bool
	EffortLadder []AgentCatalogEffortTier
}

// AgentCatalogEffortTier describes one resolved effort tier for display.
// Source is "built-in" for Pop's default opinion and "configured" for user
// config. An empty Entries slice means the tier has no configured models.
type AgentCatalogEffortTier struct {
	Tier    string
	Entries []config.EffortModel
	Source  string
}

// AgentCatalog returns stable rows for every recognized built-in agent preset.
// It performs PATH lookup only; it does not invoke agent binaries.
func AgentCatalog(d *Deps) []AgentCatalogRow {
	return AgentCatalogWithConfig(d, nil)
}

// AgentCatalogWithConfig returns stable rows for built-in agent presets plus
// any config-only effort agents. Configured ladders fully replace built-ins.
func AgentCatalogWithConfig(d *Deps, cfg *config.Config) []AgentCatalogRow {
	lookPath := exec.LookPath
	if d != nil && d.LookPath != nil {
		lookPath = d.LookPath
	}

	rows := make([]AgentCatalogRow, 0, len(agentCatalogOrder))
	seen := make(map[string]bool, len(agentCatalogOrder))
	for _, preset := range agentCatalogOrder {
		adapter := agentAdapters[preset]
		binary := AgentBinary(adapter)
		_, err := lookPath(binary)
		rows = append(rows, AgentCatalogRow{
			Agent:        preset,
			Binary:       binary,
			Found:        err == nil,
			EffortLadder: effortLadderForCatalog(cfg, preset),
		})
		seen[preset] = true
	}

	for _, agent := range configuredEffortAgents(cfg, seen) {
		_, err := lookPath(agent)
		rows = append(rows, AgentCatalogRow{
			Agent:        agent,
			Binary:       agent,
			Found:        err == nil,
			EffortLadder: effortLadderForCatalog(cfg, agent),
		})
	}
	return rows
}

func AgentBinary(adapter AgentAdapter) string {
	if preset, ok := adapter.(*presetAgentAdapter); ok && len(preset.headlessPrefix) > 0 {
		return preset.headlessPrefix[0]
	}
	return adapter.Preset()
}

func configuredEffortAgents(cfg *config.Config, seen map[string]bool) []string {
	if cfg == nil || len(cfg.Effort) == 0 {
		return nil
	}
	agents := make([]string, 0, len(cfg.Effort))
	for agent := range cfg.Effort {
		if !seen[agent] {
			agents = append(agents, agent)
		}
	}
	sort.Strings(agents)
	return agents
}

func effortLadderForCatalog(cfg *config.Config, agent string) []AgentCatalogEffortTier {
	if cfg != nil && cfg.Effort != nil {
		if ladder, ok := cfg.Effort[agent]; ok {
			return effortLadderTiers(ladder, "configured")
		}
	}
	if ladder, ok := builtInEffortModels[agent]; ok {
		return []AgentCatalogEffortTier{
			{Tier: "heavy", Entries: append([]config.EffortModel(nil), ladder["heavy"]...), Source: "built-in"},
			{Tier: "standard", Entries: append([]config.EffortModel(nil), ladder["standard"]...), Source: "built-in"},
			{Tier: "light", Entries: append([]config.EffortModel(nil), ladder["light"]...), Source: "built-in"},
		}
	}
	return nil
}

func effortLadderTiers(ladder config.EffortConfig, source string) []AgentCatalogEffortTier {
	return []AgentCatalogEffortTier{
		{Tier: "heavy", Entries: append([]config.EffortModel(nil), ladder.Heavy...), Source: source},
		{Tier: "standard", Entries: append([]config.EffortModel(nil), ladder.Standard...), Source: source},
		{Tier: "light", Entries: append([]config.EffortModel(nil), ladder.Light...), Source: source},
	}
}
