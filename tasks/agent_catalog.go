package tasks

import (
	"os/exec"
	"sort"
	"time"

	"github.com/glebglazov/pop/config"
)

const DefaultAgentPreset = "claude"

var agentCatalogOrder = []string{"claude", "opencode", "cursor", "codex", "pi", "kimi"}

// AgentCatalogRow describes Pop's recognition, PATH availability, attended
// assistance, curated models, and resolved effort ladder for one agent row.
type AgentCatalogRow struct {
	Agent        string
	Binary       string
	Found        bool
	Assistance   bool
	EffortLadder []AgentCatalogEffortTier
	// Models is the preset's curated, recommended-first alias list — advisory
	// only, never a validation gate (ADR-0019).
	Models []string
	// ModelsInstallDependent reports that those aliases are whatever the local
	// install's provider config names them, not stable account-independent
	// names, so a planner knows they may need overriding.
	ModelsInstallDependent bool
	// ModelSkips carries the Effort model skips still in force for this preset
	// (ADR-0168), keyed by the ladder `--model` token, so a reader can see which
	// entries the ladder is currently walking past. A zero instant is a permanent
	// skip; an absent key is runnable. This is the store's own encoding
	// (store.AgentModelCooldown.Until).
	ModelSkips map[string]time.Time
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
// It performs PATH lookup and a read-only Effort model skip lookup; it does not
// invoke agent binaries.
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
	skips := catalogModelSkips(d, time.Now())

	rows := make([]AgentCatalogRow, 0, len(agentCatalogOrder))
	seen := make(map[string]bool, len(agentCatalogOrder))
	for _, preset := range agentCatalogOrder {
		adapter := agentAdapters[preset]
		binary := AgentBinary(adapter)
		_, err := lookPath(binary)
		rows = append(rows, AgentCatalogRow{
			Agent:                  preset,
			Binary:                 binary,
			Found:                  err == nil,
			Assistance:             adapter.AssistanceCapability().Available(),
			EffortLadder:           effortLadderForCatalog(cfg, preset),
			Models:                 adapter.Models(),
			ModelsInstallDependent: modelsInstallDependent(adapter),
			ModelSkips:             skips[preset],
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
			ModelSkips:   skips[agent],
		})
	}
	return rows
}

// catalogModelSkips reads the Effort model skips in force at now, grouped by
// preset. The catalog is a display surface with no error channel, so a store it
// cannot read yields no annotations: the ladder still renders, unmarked, which
// is exactly what a machine that has never skipped a model shows.
func catalogModelSkips(d *Deps, now time.Time) map[string]map[string]time.Time {
	if d == nil {
		return nil
	}
	rows, err := ActiveAgentModelCooldownsWith(d, now)
	if err != nil {
		return nil
	}
	skips := map[string]map[string]time.Time{}
	for _, row := range rows {
		if skips[row.Preset] == nil {
			skips[row.Preset] = map[string]time.Time{}
		}
		skips[row.Preset][row.Model] = row.Until
	}
	return skips
}

func AgentBinary(adapter AgentAdapter) string {
	name := adapter.ExecutableCapability().executableName()
	if name != "" {
		return name
	}
	return adapter.Preset()
}

func modelsInstallDependent(adapter AgentAdapter) bool {
	preset, ok := adapter.(*presetAgentAdapter)
	return ok && preset.modelsInstallDependent
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
	adapter, err := ResolveAgentAdapter(agent)
	if err != nil {
		return nil
	}
	cap := adapter.EffortLadderCapability()
	if cap.Kind != CapabilitySupported || len(cap.Ladder) == 0 {
		return nil
	}
	return []AgentCatalogEffortTier{
		{Tier: "heavy", Entries: append([]config.EffortModel(nil), cap.Ladder["heavy"]...), Source: "built-in"},
		{Tier: "standard", Entries: append([]config.EffortModel(nil), cap.Ladder["standard"]...), Source: "built-in"},
		{Tier: "light", Entries: append([]config.EffortModel(nil), cap.Ladder["light"]...), Source: "built-in"},
	}
}

func effortLadderTiers(ladder config.EffortConfig, source string) []AgentCatalogEffortTier {
	return []AgentCatalogEffortTier{
		{Tier: "heavy", Entries: append([]config.EffortModel(nil), ladder.Heavy...), Source: source},
		{Tier: "standard", Entries: append([]config.EffortModel(nil), ladder.Standard...), Source: source},
		{Tier: "light", Entries: append([]config.EffortModel(nil), ladder.Light...), Source: source},
	}
}
