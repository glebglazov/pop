package tasks

import (
	"os/exec"
	"sort"
	"strings"
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
	// entries the ladder is currently walking past. A zero Until is a permanent
	// skip; an absent key is runnable. This is the store's own encoding
	// (store.AgentModelCooldown).
	ModelSkips map[string]ModelSkipHorizon
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
func catalogModelSkips(d *Deps, now time.Time) map[string]map[string]ModelSkipHorizon {
	if d == nil {
		return nil
	}
	rows, err := ActiveAgentModelCooldownsWith(d, now)
	if err != nil {
		return nil
	}
	skips := map[string]map[string]ModelSkipHorizon{}
	for _, row := range rows {
		if skips[row.Preset] == nil {
			skips[row.Preset] = map[string]ModelSkipHorizon{}
		}
		skips[row.Preset][row.Model] = ModelSkipHorizon{Until: row.Until, StatedUntil: row.StatedUntil}
	}
	return skips
}

// AgentGroupOrder lists the Work groups the catalog renders, in display order.
var AgentGroupOrder = []string{"implement", "verify", "review", "routine", "attended"}

// AgentGroupCatalog is one Work group's agent list, resolved for display.
type AgentGroupCatalog struct {
	Group   string
	Entries []AgentGroupEntry
}

// AgentGroupEntry is one agent list entry resolved for display: what to call
// it, which preset it selects, and which model its command names.
type AgentGroupEntry struct {
	// Position is the entry's 1-based place in the configured list.
	Position int
	// DisplayName is the configured display_name, empty when none was given.
	DisplayName string
	// Cmd is the entry's command as configured.
	Cmd string
	// Preset is the agent preset the command's first token selects.
	Preset string
	// Model is the model the command names through `--model`. Empty means the
	// entry names no model, and the agent's own configuration decides — pop
	// never guesses one (CONTEXT.md, Model source).
	Model string
	// Problem is non-empty for a malformed entry, carrying why it was skipped.
	Problem string
}

// Label is what a human-facing surface calls this entry: its display name where
// one is given, otherwise the command itself.
func (e AgentGroupEntry) Label() string {
	if strings.TrimSpace(e.DisplayName) != "" {
		return e.DisplayName
	}
	if strings.TrimSpace(e.Cmd) != "" {
		return e.Cmd
	}
	return "(malformed entry)"
}

// AgentEntryNoModelLabel is how an entry that names no model reads. Pop has no
// catalog of what an agent defaults to, so it says who decides instead of
// guessing a name.
const AgentEntryNoModelLabel = "agent decides"

// ModelLabel names the model this entry resolves to, deferring honestly when
// the command names none.
func (e AgentGroupEntry) ModelLabel() string {
	if strings.TrimSpace(e.Model) == "" {
		return AgentEntryNoModelLabel
	}
	return e.Model
}

// AgentGroupCatalogs resolves every Work group's configured agent list for
// display, in configured order. Groups with no configured list render empty
// rather than being dropped, so the catalog shows the whole shape of [work].
func AgentGroupCatalogs(cfg *config.Config) []AgentGroupCatalog {
	lists := map[string]config.AgentEntries{
		"implement": cfg.ImplementAgentEntries(),
		"verify":    cfg.VerifyAgentEntries(),
		"review":    cfg.ReviewAgentEntries(),
		"routine":   cfg.RoutineAgentEntries(),
		"attended":  cfg.AttendedAgentEntries(),
	}
	catalogs := make([]AgentGroupCatalog, 0, len(AgentGroupOrder))
	for _, group := range AgentGroupOrder {
		entries := lists[group]
		rows := make([]AgentGroupEntry, 0, len(entries))
		for i, entry := range entries {
			row := AgentGroupEntry{
				Position:    i + 1,
				DisplayName: entry.DisplayName,
				Cmd:         entry.Cmd,
				Problem:     entry.Problem(),
			}
			if row.Problem == "" {
				if preset, err := AgentPresetName(entry.Cmd); err == nil {
					row.Preset = preset
				}
				row.Model = AgentSpecModel(entry.Cmd)
			}
			rows = append(rows, row)
		}
		catalogs = append(catalogs, AgentGroupCatalog{Group: group, Entries: rows})
	}
	return catalogs
}

// AgentSpecModel returns the model an agent command names through `--model`,
// in either the separate-argument or `--model=` form. Every other argument is
// left alone: this reads the spec, it never rewrites it. An empty result means
// the command names no model.
func AgentSpecModel(spec string) string {
	_, args, err := parseAgentPresetSpec(spec)
	if err != nil {
		return ""
	}
	for i, arg := range args {
		if arg == "--model" {
			if i+1 < len(args) {
				return args[i+1]
			}
			return ""
		}
		if value, ok := strings.CutPrefix(arg, "--model="); ok {
			return value
		}
	}
	return ""
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
