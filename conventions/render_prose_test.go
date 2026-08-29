package conventions

import (
	"strings"
	"testing"
)

// TestStackProse: the prose an agent is handed inside a larger prompt carries
// the one answer in force and the human's overlay, under no composition rule
// and no layer numbering. A kind nobody has written an answer to is handed
// pop's own answer, labelled an answer like any other, rather than silence.
func TestStackProse(t *testing.T) {
	unwritten := StackProse(Stack{Kind: KindRefine, Layers: []Layer{
		{Origin: OriginRepository, Path: "/repo/docs/agents/refine.md"},
	}})
	for _, want := range []string{"ANSWER: SHIPPED", Shipped(KindRefine)} {
		if !strings.Contains(unwritten, want) {
			t.Fatalf("prose for an unwritten kind is missing %q:\n%s", want, unwritten)
		}
	}

	spoken := Stack{Kind: KindRefine, Layers: []Layer{
		{Origin: OriginProject, Path: "/home/docs/projects/github.com-tripledot-pop/refine.md", Present: true, Body: "prefer small functions"},
		{Origin: OriginRepository, Path: "/repo/docs/agents/refine.md", Present: true, Body: "table-driven tests only"},
		{Origin: OriginOverlay, Path: "/home/docs/refine.overlay.md", Present: true, Body: "never approve a TODO"},
	}}
	got := StackProse(spoken)
	for _, want := range []string{
		"ANSWER: USER PROJECT",
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
	// A written answer displaces the shipped rank as it displaces any lower rank.
	for _, absent := range []string{"table-driven tests only", "LAYER 1 OF", "compose", "contradict", "METHOD", "SHIPPED"} {
		if strings.Contains(got, absent) {
			t.Fatalf("prose still carries %q:\n%s", absent, got)
		}
	}
	// A prompt embeds this; the editing surface's overlay note is not its business.
	if strings.Contains(got, "edited here") {
		t.Fatalf("prose must not name an editing surface:\n%s", got)
	}
}
