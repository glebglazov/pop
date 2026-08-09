package tasks

import (
	"testing"

	"github.com/glebglazov/pop/config"
)

// TestTurnCapReachComesFromEnforcementStances pins ADR-0198 decision 3: turn_cap
// reach is the adapter capabilities flattened — claude's argv with N, and each
// Blind preset's own Reason, with no second copy of those sentences here.
func TestTurnCapReachComesFromEnforcementStances(t *testing.T) {
	reach, ok := config.ConfigKeyReachFor("turn_cap")
	if !ok {
		t.Fatal("turn_cap did not register a reach")
	}
	byActor := make(map[string]string, len(reach.Lines))
	for _, line := range reach.Lines {
		byActor[line.Actor] = line.Detail
	}
	if len(byActor) != len(agentCatalogOrder) {
		t.Fatalf("reach has %d actors, want the %d built-in presets", len(byActor), len(agentCatalogOrder))
	}

	if got := byActor["claude"]; got != "--max-turns N" {
		t.Errorf("claude detail = %q, want --max-turns N", got)
	}

	for _, preset := range agentCatalogOrder {
		if preset == "claude" {
			continue
		}
		want := agentAdapters[preset].TurnCapEnforcementCapability().Reason
		if want == "" {
			t.Fatalf("%s Blind reason is empty; capability declaration is incomplete", preset)
		}
		if byActor[preset] != want {
			t.Errorf("%s detail = %q, want the adapter Reason %q", preset, byActor[preset], want)
		}
	}
}
