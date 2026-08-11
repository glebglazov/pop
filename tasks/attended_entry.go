package tasks

import (
	"strings"

	"github.com/glebglazov/pop/config"
	"github.com/glebglazov/pop/ui"
)

// FormatAgentEntry is the shared one-line render of an agent entry: its label
// and the model it names (ADR-0196 decision 9, kept by ADR-0202 decision 5). An
// entry that pins no model renders as the label alone — inline surfaces are pane
// titles and menu rows, where naming the absence costs more width than it
// carries meaning. The agent catalog still says so in full, in its own column.
func FormatAgentEntry(e AgentGroupEntry) string {
	if strings.TrimSpace(e.Model) == "" {
		return e.Label()
	}
	return e.Label() + " · " + e.Model
}

// FormatAttendedAgentStatus is the persistent dashboard subheader: the attended
// entry the merged config resolves to, and the key that opens the surface which
// changes it. That surface is the Config dashboard now — a setting in force can
// come from the override layer, which no hand-authored file mentions, so the
// subheader has to say where it is edited (ADR-0202 decision 5).
func FormatAttendedAgentStatus(e AgentGroupEntry) string {
	return "agent " + FormatAgentEntry(e) + " · " + ui.ConfigDashboardKeyLabel
}

// EffectiveAttendedEntry is the attended entry that will run: the head of the
// merged attended list, else the built-in default. The merge is where an
// override lives, so every render that calls this reports one.
func EffectiveAttendedEntry(cfg *config.Config) AgentGroupEntry {
	return EffectiveGroupEntry(cfg, "attended")
}

// EffectiveGroupEntry is the head entry of group as the merged config resolves
// it, or a synthetic default for the attended group when nothing is configured.
func EffectiveGroupEntry(cfg *config.Config, group string) AgentGroupEntry {
	for _, catalog := range AgentGroupCatalogs(cfg) {
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
