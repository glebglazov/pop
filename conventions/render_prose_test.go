package conventions

import (
	"strings"
	"testing"
)

// TestStackProse: the prose an agent is handed inside a larger prompt carries
// the one answer in force and the human's overlay, under no composition rule
// and no layer numbering, and a kind nothing answers yields nothing at all
// rather than a list of paths an agent cannot act on.
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
		{Origin: OriginOverlay, Path: "/home/docs/code-review.overlay.md", Present: true, Body: "never approve a TODO"},
	}}
	got, ok := StackProse(spoken)
	if !ok {
		t.Fatal("StackProse must speak for a kind that answers")
	}
	for _, want := range []string{
		"ANSWER: USER DEFAULTS",
		"prefer small functions",
		"APPENDED: USER OVERLAY",
		"never approve a TODO",
		"Provenance:",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("prose missing %q:\n%s", want, got)
		}
	}
	// The rank the answer stood down is not handed to the agent at all, and
	// there is no rule left telling it to reconcile anything.
	for _, absent := range []string{"table-driven tests only", "LAYER 1 OF", "compose", "contradict"} {
		if strings.Contains(got, absent) {
			t.Fatalf("prose still carries %q:\n%s", absent, got)
		}
	}
	// A prompt embeds this; the editing surface's overlay note is not its business.
	if strings.Contains(got, "edited here") {
		t.Fatalf("prose must not name an editing surface:\n%s", got)
	}
}
