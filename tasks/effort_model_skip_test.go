package tasks

import (
	"testing"
	"time"

	"github.com/glebglazov/pop/config"
)

func kimiLightLadder(models ...string) *config.Config {
	entries := make([]config.EffortModel, 0, len(models))
	for _, model := range models {
		entries = append(entries, config.EffortModel{Model: model})
	}
	return &config.Config{Effort: map[string]config.EffortConfig{"kimi": {Light: entries}}}
}

// Resolution reads the tier's first entry that is not recorded as skipped, so
// each recorded skip strictly shortens the candidate list — which is what makes
// the list itself the loop guard (ADR-0168).
func TestResolveEffortModelWalksPastRecordedSkips(t *testing.T) {
	t.Parallel()
	cfg := kimiLightLadder("head", "middle", "tail")
	skips := effortModelSkips{}

	for _, want := range []string{"head", "middle", "tail"} {
		resolution := resolveEffortModel("kimi", "light", true, cfg, skips)
		if resolution.Exhausted {
			t.Fatalf("tier exhausted before %q ran", want)
		}
		if resolution.Model != want {
			t.Fatalf("model = %q, want %q", resolution.Model, want)
		}
		if resolution.Spec != "kimi --model "+want {
			t.Fatalf("spec = %q, want the tier entry pinned", resolution.Spec)
		}
		skips.add("kimi", want)
	}

	if resolution := resolveEffortModel("kimi", "light", true, cfg, skips); !resolution.Exhausted {
		t.Fatalf("resolution = %#v, want an exhausted tier once every entry is skipped", resolution)
	}
}

// The skip list is keyed by preset: one preset's refused model says nothing
// about another preset that happens to broker the same name.
func TestResolveEffortModelIgnoresAnotherPresetsSkips(t *testing.T) {
	t.Parallel()
	skips := effortModelSkips{}
	skips.add("cursor", "head")

	resolution := resolveEffortModel("kimi", "light", true, kimiLightLadder("head", "tail"), skips)
	if resolution.Model != "head" {
		t.Fatalf("model = %q, want head — the skip belongs to cursor", resolution.Model)
	}
}

// A hand-pinned model steps outside the ladder, so no skip filters it and no
// tier entry is resolved for the walk to retire.
func TestResolveEffortModelLeavesAPinnedModelAlone(t *testing.T) {
	t.Parallel()
	skips := effortModelSkips{}
	skips.add("kimi", "head")

	resolution := resolveEffortModel("kimi --model head", "light", true, kimiLightLadder("head", "tail"), skips)
	if resolution.Spec != "kimi --model head" || resolution.Model != "" || resolution.Exhausted {
		t.Fatalf("resolution = %#v, want the pinned spec untouched and no ladder entry", resolution)
	}
}

// The walk seeds itself from the durable store, so a model refused by an earlier
// run is filtered out before this one spawns anything.
func TestLoadEffortModelSkipsReadsTheDurableStore(t *testing.T) {
	t.Parallel()
	d := newTestDeps(t)
	if err := updateAgentModelCooldown(d, "kimi", "head", time.Time{}, true); err != nil {
		t.Fatalf("updateAgentModelCooldown: %v", err)
	}

	skips, err := loadEffortModelSkips(d, time.Now())
	if err != nil {
		t.Fatalf("loadEffortModelSkips: %v", err)
	}
	if !skips.blocked("kimi", "head") {
		t.Fatalf("skips = %#v, want the recorded kimi entry blocked", skips)
	}
	resolution := resolveEffortModel("kimi", "light", true, kimiLightLadder("head", "tail"), skips)
	if resolution.Model != "tail" {
		t.Fatalf("model = %q, want tail", resolution.Model)
	}
}

// A timed skip lifts on its own: the model is a candidate again once its expiry
// has passed, with no operator action and no manual clear verb.
func TestLoadEffortModelSkipsDropsElapsedEntries(t *testing.T) {
	t.Parallel()
	d := newTestDeps(t)
	if err := updateAgentModelCooldown(d, "kimi", "head", time.Now().Add(30*time.Minute), false); err != nil {
		t.Fatalf("updateAgentModelCooldown: %v", err)
	}

	skips, err := loadEffortModelSkips(d, time.Now().Add(2*time.Hour))
	if err != nil {
		t.Fatalf("loadEffortModelSkips: %v", err)
	}
	if skips.blocked("kimi", "head") {
		t.Fatalf("skips = %#v, want the elapsed entry dropped", skips)
	}
}
