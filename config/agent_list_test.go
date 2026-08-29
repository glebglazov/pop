package config

import (
	"fmt"
	"strings"
	"testing"
)

// implementOnlyConfig leaves every falling-through group absent, so each case
// below decides on its own what the group says.
const implementOnlyConfig = `
[work.implement]
agents = ["claude"]
`

// TestAgentListEmptinessAgreesWithTheDashboard drives one fixture through both
// readers of the same emptiness — the merged config every resolver holds, and
// the preview the Config dashboard shows — for each list that falls through to
// implement's. The two must never disagree about which empty list walks on
// (ADR-0202 decision 6); that they were read from one file and answered by two
// packages is the whole point of the pairing.
func TestAgentListEmptinessAgreesWithTheDashboard(t *testing.T) {
	groups := []struct{ key, table string }{
		{KeyVerifyAgents, "work.verify"},
		{KeyRefineAgents, "work.refine"},
		{KeyRoutineAgents, "work.routine"},
	}
	fallsThroughNote := fmt.Sprintf(overrideNoteFallsThrough, "the implement list")

	for _, g := range groups {
		t.Run(g.key, func(t *testing.T) {
			t.Run("explicit empty override", func(t *testing.T) {
				f := newOverrideFixture(t)
				writeConfigFile(t, f.userPath, implementOnlyConfig)
				writeConfigFile(t, f.overridePath, fmt.Sprintf("[%s]\nagents = []\n", g.table))

				list := f.load(t).agentList(g.key)
				if !list.EmptyOverride {
					t.Errorf("EmptyOverride = false for an override of %s = []", g.key)
				}
				if list.FallsThrough() {
					t.Error("FallsThrough() = true; an override wrote this emptiness")
				}
				if note := viewFor(t, f, g.key).Note; note != overrideNoteEmptyOverride {
					t.Errorf("dashboard note = %q, want %q", note, overrideNoteEmptyOverride)
				}
			})

			t.Run("absent list", func(t *testing.T) {
				f := newOverrideFixture(t)
				writeConfigFile(t, f.userPath, implementOnlyConfig)

				list := f.load(t).agentList(g.key)
				if list.EmptyOverride {
					t.Errorf("EmptyOverride = true with no override file")
				}
				if !list.FallsThrough() {
					t.Error("FallsThrough() = false; nothing states this key")
				}
				if note := viewFor(t, f, g.key).Note; note != fallsThroughNote {
					t.Errorf("dashboard note = %q, want %q", note, fallsThroughNote)
				}
			})

			// An empty list a human typed into config.toml is not the override
			// layer speaking, and the preview says so in the same words it uses
			// for an absent key. Execution reads it the same way.
			t.Run("empty list in config.toml", func(t *testing.T) {
				f := newOverrideFixture(t)
				writeConfigFile(t, f.userPath, implementOnlyConfig+fmt.Sprintf("\n[%s]\nagents = []\n", g.table))

				list := f.load(t).agentList(g.key)
				if list.EmptyOverride {
					t.Error("EmptyOverride = true for an empty list in config.toml")
				}
				if !list.FallsThrough() {
					t.Error("FallsThrough() = false; no override wrote this emptiness")
				}
				if note := viewFor(t, f, g.key).Note; note != fallsThroughNote {
					t.Errorf("dashboard note = %q, want %q", note, fallsThroughNote)
				}
			})

			t.Run("override names agents", func(t *testing.T) {
				f := newOverrideFixture(t)
				writeConfigFile(t, f.userPath, implementOnlyConfig)
				writeConfigFile(t, f.overridePath, fmt.Sprintf("[%s]\nagents = [\"codex\"]\n", g.table))

				list := f.load(t).agentList(g.key)
				if list.EmptyOverride || list.FallsThrough() {
					t.Errorf("list = %+v, want the stated agents in force", list)
				}
				if strings.Join(list.Commands, ",") != "codex" {
					t.Errorf("Commands = %v, want [codex]", list.Commands)
				}
				if note := viewFor(t, f, g.key).Note; note != "" {
					t.Errorf("dashboard note = %q, want none for a stated value", note)
				}
			})
		})
	}
}

// TestEmptyAgentOverridesRecordsOnlyStatedEmptyLists pins what the merged config
// carries: the override layer's own empty lists, and nothing a lower layer or a
// malformed entry produced.
func TestEmptyAgentOverridesRecordsOnlyStatedEmptyLists(t *testing.T) {
	f := newOverrideFixture(t)
	writeConfigFile(t, f.userPath, implementOnlyConfig+`
[work.routine]
agents = []
`)
	writeConfigFile(t, f.overridePath, `
[work.verify]
agents = []

[work.refine]
agents = [{ display_name = "no cmd" }]
`)

	cfg := f.load(t)
	if got := strings.Join(cfg.EmptyAgentOverrides, ","); got != KeyVerifyAgents {
		t.Fatalf("EmptyAgentOverrides = %v, want only %s", cfg.EmptyAgentOverrides, KeyVerifyAgents)
	}
	// A malformed entry yields no command, but the list is not empty: the
	// preview shows the entry, so resolution must still walk on.
	refine := cfg.RefineAgentList()
	if len(refine.Commands) != 0 || refine.EmptyOverride || !refine.FallsThrough() {
		t.Fatalf("refine list = %+v, want an empty-of-commands list that falls through", refine)
	}
}
