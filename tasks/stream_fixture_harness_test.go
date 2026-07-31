package tasks

import (
	"strings"
	"testing"
)

func TestStreamShapeFixtureHarness(t *testing.T) {
	for _, preset := range agentCatalogOrder {
		preset := preset
		t.Run(preset, func(t *testing.T) {
			adapter, err := ResolveAgentAdapter(preset)
			if err != nil {
				t.Fatal(err)
			}

			goldens := streamShapeFixtureGoldens[preset]
			fixtureExists := streamFixtureExists(preset)
			if !fixtureExists {
				return
			}

			events, err := loadStreamFixture(preset)
			if err != nil {
				t.Fatalf("load stream fixture: %v", err)
			}

			for _, cap := range streamShapeCapabilityOrder {
				cap := cap
				t.Run(cap.String(), func(t *testing.T) {
					kind := streamShapeCapabilityKind(adapter, cap)
					if violation := checkStreamShapeFixture(preset, cap, kind, fixtureExists, goldens, events); violation != nil {
						t.Fatal(violation.Description)
					}
				})
			}
		})
	}
}

func TestStreamShapeFixtureHarness_supportedWithoutFixtureFails(t *testing.T) {
	violation := checkStreamShapeFixture(
		"codex", streamShapeToolTimings, CapabilitySupported, false, nil, nil,
	)
	if violation == nil {
		t.Fatal("expected violation")
	}
	if !strings.Contains(violation.Description, "no captured stream fixture") {
		t.Fatalf("description = %q", violation.Description)
	}
}

func TestStreamShapeFixtureHarness_goldenMismatchFails(t *testing.T) {
	events, err := loadStreamFixture("claude")
	if err != nil {
		t.Fatal(err)
	}
	badGoldens := &streamShapeGolden{
		turn: &TurnCount{Count: 0, HasTurn: true},
	}
	violation := checkStreamShapeFixture(
		"claude", streamShapeTurn, CapabilitySupported, true, badGoldens, events,
	)
	if violation == nil {
		t.Fatal("expected violation")
	}
	if !strings.Contains(violation.Description, "turn:") {
		t.Fatalf("description = %q", violation.Description)
	}
}

func TestStreamShapeFixtureHarness_blindWithGoldenFails(t *testing.T) {
	events, err := loadStreamFixture("claude")
	if err != nil {
		t.Fatal(err)
	}
	goldens := &streamShapeGolden{
		cost: &PartialCost{},
	}
	violation := checkStreamShapeFixture(
		"claude", streamShapeCost, CapabilityBlind, true, goldens, events,
	)
	if violation == nil {
		t.Fatal("expected violation")
	}
	if !strings.Contains(violation.Description, "blind capability has a golden fixture entry") {
		t.Fatalf("description = %q", violation.Description)
	}
}
