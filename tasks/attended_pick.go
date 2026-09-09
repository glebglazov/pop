package tasks

import (
	"fmt"
	"io"

	"github.com/glebglazov/pop/config"
	"github.com/glebglazov/pop/ui"
)

// runAttendedPicker is the seam a gate opens the attended chooser through.
// Production points at ui.RunAttendedAgentPicker; tests may swap it, as they do
// runGateMenu beside it.
var runAttendedPicker = ui.RunAttendedAgentPicker

// AttendedPickChoices renders the attended entries a human may pick between:
// the usable ones, in configured order, each by the shared Attended entry
// render. A malformed entry is left out — it launches nothing, so offering it
// would offer a choice that cannot be taken.
func AttendedPickChoices(cfg *config.Config) []ui.AttendedAgentEntry {
	var choices []ui.AttendedAgentEntry
	for _, entry := range usableGroupEntries(cfg, "attended") {
		choices = append(choices, ui.AttendedAgentEntry{Cmd: entry.Cmd, Label: FormatAgentEntry(entry)})
	}
	return choices
}

// AttendedPickOffered reports whether a surface should offer the chooser at
// all: only where a choice exists, which is two or more usable entries
// (ADR-0264 decision 4).
func AttendedPickOffered(cfg *config.Config) bool {
	return len(AttendedPickChoices(cfg)) > 1
}

// PickAttendedAgent returns the whole entry selected by the chooser. It does
// not write the Agent override or any other saved state (ADR-0266).
func PickAttendedAgent(cfg *config.Config, in io.Reader, out io.Writer, warn func(string, ...any)) (*AgentGroupEntry, string) {
	return pickAgentEntry(usableGroupEntries(cfg, "attended"), in, out, warn)
}

func pickAgentEntry(entries []AgentGroupEntry, in io.Reader, out io.Writer, warn func(string, ...any)) (*AgentGroupEntry, string) {
	var choices []ui.AttendedAgentEntry
	for _, entry := range entries {
		choices = append(choices, ui.AttendedAgentEntry{Cmd: entry.Cmd, Label: FormatAgentEntry(entry)})
	}
	picked, err := runAttendedPicker(choices, in, out, warn)
	if err != nil {
		return nil, fmt.Sprintf("Agent unchanged — the list would not open: %v", err)
	}
	if picked == nil {
		return nil, ""
	}
	for _, entry := range entries {
		if entry.Cmd == picked.Cmd && FormatAgentEntry(entry) == picked.Label {
			chosen := entry
			return &chosen, ""
		}
	}
	return nil, "Agent unchanged — the list returned an unknown entry"
}
