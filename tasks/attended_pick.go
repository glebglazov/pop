package tasks

import (
	"errors"
	"fmt"
	"io"

	"github.com/glebglazov/pop/config"
	"github.com/glebglazov/pop/ui"
)

// AttendedAgentsKey is the config key an attended pick states: the whole
// attended list, which is what an Agent override is (ADR-0202 decision 2).
const AttendedAgentsKey = "work.attended.agents"

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
	for _, catalog := range AgentGroupCatalogs(cfg) {
		if catalog.Group != "attended" {
			continue
		}
		for _, entry := range catalog.Entries {
			if entry.Problem != "" {
				continue
			}
			choices = append(choices, ui.AttendedAgentEntry{Cmd: entry.Cmd, Label: FormatAgentEntry(entry)})
		}
	}
	return choices
}

// AttendedPickOffered reports whether a surface should offer the chooser at
// all: only where a choice exists, which is two or more usable entries
// (ADR-0264 decision 4).
func AttendedPickOffered(cfg *config.Config) bool {
	return len(AttendedPickChoices(cfg)) > 1
}

// PickAttendedAgent opens the attended chooser and stores what it returns as an
// Agent override — the whole list, reordered with the picked entry at its head.
// It is the write half of ADR-0264 decisions 1 and 2, shared by the task and
// Routine gate hosts.
//
// It returns the sentence the menu shows next time round, empty when there is
// nothing to say: a human who left the chooser unchanged wrote nothing, and a
// human whose pick the override layer refused must be told there rather than on
// the stdout the menu is drawn over. Either way the caller re-reads its own
// config afterwards — the row must name what a load resolves, never the choice
// (decision 6).
func PickAttendedAgent(d *Deps, cfg *config.Config, in io.Reader, out io.Writer, warn func(string, ...any)) string {
	picked, err := runAttendedPicker(AttendedPickChoices(cfg), in, out, warn)
	if err != nil {
		return fmt.Sprintf("Agent unchanged — the list would not open: %v", err)
	}
	if picked == nil {
		return ""
	}
	if err := PromoteAttendedAgent(d, cfg, picked.Cmd); err != nil {
		return fmt.Sprintf("Agent unchanged — %v", err)
	}
	return ""
}

// PromoteAttendedAgent stores the attended list with cmd at its head. The
// override layer's own schema gate judges the value, so a list pop would later
// complain about is refused here and never reaches the disk.
//
// It is the write on its own, for a host that runs the chooser itself: a
// dashboard hosts the list as a modal inside its own program rather than opening
// one, so it arrives here with a picked entry and no prompt to have run.
func PromoteAttendedAgent(d *Deps, cfg *config.Config, cmd string) error {
	cd := configDeps(d)
	if cd == nil {
		return errors.New("no filesystem to write the Agent override through")
	}
	entries := cfg.AttendedAgentEntries().PromoteToHead(cmd)
	return config.SetOverrideValueWith(cd, AttendedAgentsKey, entries.OverrideValue())
}

// configDeps is this package's door to the config layer: the same filesystem
// the rest of a run reads through, so a test that redirects one redirects both.
// Trunk stays nil, as it is on every production load of a gate's config — the
// Preferred workbench inheritance layer is resolved by the command that needs
// it, not by a config read.
//
// A Deps carrying no filesystem yields none. Falling back to the real one is
// the same mistake the attended launch walk refuses to make: under `go test`
// that points at the machine's own config, and here it would write to it.
func configDeps(d *Deps) *config.Deps {
	if d == nil || d.FS == nil {
		return nil
	}
	return &config.Deps{FS: d.FS}
}
