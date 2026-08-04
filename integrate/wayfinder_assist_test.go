package integrate

import (
	"strings"
	"testing"
)

// wayfinderSkillBody is the embedded wayfinding skill as it ships.
func wayfinderSkillBody(t *testing.T) string {
	t.Helper()
	body, err := skillFiles.ReadFile("skills/pop/wayfinder/SKILL.md")
	if err != nil {
		t.Fatalf("read embedded wayfinder skill: %v", err)
	}
	return string(body)
}

// TestWayfinderSkill_AssistModeFencesResolve pins the third mode and the fence it
// costs (ADR-0184): the modes share one file, so an assist session must be told in
// so many words that work mode's resolve flow is not its own.
func TestWayfinderSkill_AssistModeFencesResolve(t *testing.T) {
	t.Parallel()
	body := wayfinderSkillBody(t)

	for _, want := range []string{
		"## Assist the map",
		"assist <map-id>",
		"holds **no ticket**",
		"pop map authoring-guide",
		"pop map register <map-id>",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("wayfinder skill's assist mode missing %q", want)
		}
	}
	// The fence itself: naming the mode is not enough, the resolve flow has to be
	// ruled out of it explicitly, and the human handed the verb that does resolve.
	for _, want := range []string{
		"**Assist resolves nothing.**",
		"fenced off from this mode",
		"pop map next <map-id> <NN>",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("wayfinder skill does not fence resolve off from assist: missing %q", want)
		}
	}
	// The assist section sits in the pop overlay, not in the byte-verbatim upstream
	// region the drift test compares — upstream has two modes.
	marker := strings.Index(body, "POP OVERLAY")
	if marker < 0 {
		t.Fatal("wayfinder skill lost its overlay marker")
	}
	if idx := strings.Index(body, "## Assist the map"); idx < marker {
		t.Error("the assist mode must live below the POP OVERLAY marker")
	}
}

// TestIssueTrackerDoc_AssistWorkflowRules pins the workflow half of ADR-0184's
// split: the doc — not the authoring guide — carries never-resolves and
// re-validate-on-close, beside the one-non-research-ticket-per-session rule they
// protect.
func TestIssueTrackerDoc_AssistWorkflowRules(t *testing.T) {
	t.Parallel()
	body := string(issueTrackerDoc)

	resolution := body[strings.Index(body, "### Resolution"):]
	if idx := strings.Index(resolution, "### Handoff to implementation"); idx > 0 {
		resolution = resolution[:idx]
	}
	for _, want := range []string{
		"pop map assist",
		"it must not run `pop map resolve`",
		"one-non-research-ticket-per-session",
		"pop map next <map-id> <NN>",
		"pop map register <map-id>",
		"pop map out-of-scope",
		"One pane per map, reused",
	} {
		if !strings.Contains(resolution, want) {
			t.Errorf("the doc's Resolution section missing the assist rule %q", want)
		}
	}
	// The artifact half stays in the binary: the doc points at the guide rather
	// than restating which files a session may write (ADR-0183).
	if !strings.Contains(resolution, "pop map authoring-guide") {
		t.Error("the doc must defer the writable-files question to the authoring guide")
	}
}
