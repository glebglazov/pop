package conventions

import (
	"strings"
	"testing"
)

// TestStackProse: the prose an agent is handed inside a larger prompt carries
// every layer that speaks under one composition rule, and a silent stack yields
// nothing at all rather than a list of paths an agent cannot act on.
func TestStackProse(t *testing.T) {
	silent := Stack{Kind: KindCodeReview, Layers: []Layer{
		{Origin: OriginRepository, Path: "/repo/docs/agents/code-review.md"},
	}}
	if got, ok := StackProse(silent); ok || got != "" {
		t.Fatalf("StackProse(silent) = %q, %v; want empty and false", got, ok)
	}

	spoken := Stack{Kind: KindCodeReview, Layers: []Layer{
		{Origin: OriginUserDefaults, Path: "/home/docs/code-review.md", Present: true, Body: "prefer small functions"},
		{Origin: OriginRepository, Path: "/repo/docs/agents/code-review.md", Present: true, Body: "table-driven tests only"},
	}}
	got, ok := StackProse(spoken)
	if !ok {
		t.Fatal("StackProse must speak for a stack with layers")
	}
	for _, want := range []string{
		"where two of them directly contradict, the later one wins",
		"LAYER 1 OF 2: USER DEFAULTS",
		"prefer small functions",
		"LAYER 2 OF 2: REPOSITORY",
		"table-driven tests only",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("prose missing %q:\n%s", want, got)
		}
	}
	// A prompt embeds this; the editing surface's overlay note is not its business.
	if strings.Contains(got, "edited here") {
		t.Fatalf("prose must not name an editing surface:\n%s", got)
	}
}
