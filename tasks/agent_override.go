package tasks

import (
	"strings"
	"sync"

	"github.com/glebglazov/pop/config"
)

// AgentOverrideKey is the cross-surface chord that opens the agent-override
// picker (ADR-0196). It is not a Work-kind Action.Key — kinds bind plain letters
// — and it is inert inside another picker because alt is already quick-access.
const AgentOverrideKey = "alt+a"

// AgentOverrideKeyLabel is how the chord reads in chrome (hints, persistent
// block), matching ui's A- prefix for alt bindings.
const AgentOverrideKeyLabel = "A-a"

// AgentOverrides is the session-lived promotion of one entry to the head of each
// Work agent group (ADR-0196). It is never written to config or the store: one
// OS process owns it, and closing that process ends it.
type AgentOverrides struct {
	mu      sync.Mutex
	byGroup map[string]string // group → entry cmd
}

// NewAgentOverrides returns an empty override bag.
func NewAgentOverrides() *AgentOverrides {
	return &AgentOverrides{byGroup: map[string]string{}}
}

// Promote moves cmd to the head of group for the rest of this process. The
// configured remainder stays behind it — a promotion, not a pin.
func (o *AgentOverrides) Promote(group, cmd string) {
	if o == nil {
		return
	}
	group = strings.TrimSpace(group)
	cmd = strings.TrimSpace(cmd)
	if group == "" || cmd == "" {
		return
	}
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.byGroup == nil {
		o.byGroup = map[string]string{}
	}
	o.byGroup[group] = cmd
}

// Cmd returns the promoted entry cmd for group, or "" when none is set.
func (o *AgentOverrides) Cmd(group string) string {
	if o == nil {
		return ""
	}
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.byGroup[strings.TrimSpace(group)]
}

// AttendedCmd is the attended-group override, empty when the human named none.
func (o *AgentOverrides) AttendedCmd() string {
	return o.Cmd("attended")
}

// FormatAgentEntry is the shared one-line render of an agent entry: its label
// and the model it names (ADR-0196 decision 9). An entry that pins no model
// renders as the label alone — inline surfaces are pane titles and menu rows,
// where naming the absence costs more width than it carries meaning. The agent
// catalog still says so in full, in its own column.
func FormatAgentEntry(e AgentGroupEntry) string {
	if strings.TrimSpace(e.Model) == "" {
		return e.Label()
	}
	return e.Label() + " · " + e.Model
}

// FormatAgentOverrideStatus is the persistent dashboard block naming the entry
// currently in force and the key that changes it.
func FormatAgentOverrideStatus(e AgentGroupEntry) string {
	return "agent " + FormatAgentEntry(e) + " · " + AgentOverrideKeyLabel
}

// EffectiveAgentGroupCatalogs is AgentGroupCatalogs with each group's
// session-lived override promoted to the head and the configured remainder
// behind it. Groups stay in AgentGroupOrder even when empty.
func EffectiveAgentGroupCatalogs(cfg *config.Config, overrides *AgentOverrides) []AgentGroupCatalog {
	catalogs := AgentGroupCatalogs(cfg)
	if overrides == nil {
		return catalogs
	}
	out := make([]AgentGroupCatalog, len(catalogs))
	for i, catalog := range catalogs {
		out[i] = AgentGroupCatalog{
			Group:   catalog.Group,
			Entries: promoteGroupEntries(catalog.Entries, overrides.Cmd(catalog.Group)),
		}
	}
	return out
}

// EffectiveAttendedEntry is the attended entry that will run: the promoted head
// when an override is set, else the configured head, else the built-in default.
func EffectiveAttendedEntry(cfg *config.Config, overrides *AgentOverrides) AgentGroupEntry {
	return EffectiveGroupEntry(cfg, overrides, "attended")
}

// EffectiveGroupEntry is the head entry of group after session promotion, or a
// synthetic default for the attended group when nothing is configured.
func EffectiveGroupEntry(cfg *config.Config, overrides *AgentOverrides, group string) AgentGroupEntry {
	for _, catalog := range EffectiveAgentGroupCatalogs(cfg, overrides) {
		if catalog.Group != group {
			continue
		}
		for _, entry := range catalog.Entries {
			if entry.Problem != "" {
				continue
			}
			return entry
		}
	}
	if group == "attended" {
		return defaultAttendedEntry()
	}
	return AgentGroupEntry{}
}

func defaultAttendedEntry() AgentGroupEntry {
	return AgentGroupEntry{
		Position: 1,
		Cmd:      DefaultAgentPreset,
		Preset:   DefaultAgentPreset,
	}
}

// promoteGroupEntries moves overrideCmd to the head, keeping the configured
// remainder (minus a matching cmd) behind it. An override not present in the
// configured list is still prepended — the human named it for this session.
func promoteGroupEntries(entries []AgentGroupEntry, overrideCmd string) []AgentGroupEntry {
	overrideCmd = strings.TrimSpace(overrideCmd)
	if overrideCmd == "" {
		return entries
	}
	out := make([]AgentGroupEntry, 0, len(entries)+1)
	var promoted *AgentGroupEntry
	for i := range entries {
		if strings.TrimSpace(entries[i].Cmd) == overrideCmd {
			e := entries[i]
			promoted = &e
			continue
		}
		out = append(out, entries[i])
	}
	if promoted == nil {
		promoted = &AgentGroupEntry{
			Cmd:   overrideCmd,
			Model: AgentSpecModel(overrideCmd),
		}
		if preset, err := AgentPresetName(overrideCmd); err == nil {
			promoted.Preset = preset
		}
	}
	promoted.Position = 1
	rest := out
	out = make([]AgentGroupEntry, 0, len(rest)+1)
	out = append(out, *promoted)
	for i, e := range rest {
		e.Position = i + 2
		out = append(out, e)
	}
	return out
}

// sessionAttendedOverride returns the process-lived attended override from d,
// empty when none is set. Explicit call-site overrides still win over this.
func sessionAttendedOverride(d *Deps) string {
	if d == nil || d.AgentOverrides == nil {
		return ""
	}
	return d.AgentOverrides.AttendedCmd()
}
