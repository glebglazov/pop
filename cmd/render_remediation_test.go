package cmd

import (
	"strings"
	"testing"
)

// TestRewriteSkillReferencesNoCommonWordCorruption pins remediation for task 03:
// common-word skill names and protected regions must not be rewritten outside
// cross-skill invocation contexts.
func TestRewriteSkillReferencesNoCommonWordCorruption(t *testing.T) {
	tree, err := renderComponent(ComponentTaskSkills, "claude", "pop-")
	if err != nil {
		t.Fatalf("renderComponent: %v", err)
	}

	prototype := string(tree["pop-prototype/SKILL.md"])
	for _, bad := range []string{
		"throwaway pop-prototype",
		"engineering/pop-prototype@",
		"A pop-prototype is",
		"the pop-prototype's README",
	} {
		if strings.Contains(prototype, bad) {
			t.Errorf("pop-prototype/SKILL.md corrupted: contains %q", bad)
		}
	}
	if !strings.Contains(prototype, "Build a throwaway prototype to answer") {
		t.Errorf("pop-prototype description should keep plain English 'prototype'")
	}
	if !strings.Contains(prototype, "engineering/prototype@") {
		t.Errorf("pop-prototype attribution header should keep upstream path")
	}

	logic := string(tree["pop-prototype/LOGIC.md"])
	if strings.Contains(logic, "pop-prototype") {
		t.Errorf("pop-prototype/LOGIC.md should not rewrite plain 'prototype' prose")
	}

	research := string(tree["pop-research/SKILL.md"])
	if strings.Contains(research, "do the pop-research") {
		t.Errorf("pop-research/SKILL.md corrupted background-agent prose")
	}
	if !strings.Contains(research, "engineering/research@") {
		t.Errorf("pop-research attribution header should keep upstream path")
	}

	wayfinder := string(tree["pop-wayfinder/SKILL.md"])
	// The ticket-type vocabulary rides in the verbatim upstream base (the pop
	// overlay's own copy moved to the Work-store doc, ADR-0136). The rewriter
	// must leave the enum and the `research/<name>` branch path untouched.
	for _, want := range []string{
		"one of `research`, `prototype`, `grilling`, `task`",
		"`research/<name>`",
	} {
		if !strings.Contains(wayfinder, want) {
			t.Errorf("wayfinder ticket vocabulary corrupted; missing %q", want)
		}
	}
	for _, bad := range []string{
		"one of `pop-research`",
		"`pop-research/<name>`",
	} {
		if strings.Contains(wayfinder, bad) {
			t.Errorf("wayfinder ticket vocabulary corrupted: contains %q", bad)
		}
	}
	// Real cross-skill references still rewrite: the overlay's handoff/charting
	// skills and the base's `/research` subagent invocation.
	for _, want := range []string{"`pop-grill-with-docs`", "`pop-to-spec`", "`pop-to-tasks`", "`/pop-research`"} {
		if !strings.Contains(wayfinder, want) {
			t.Errorf("wayfinder overlay missing rewritten skill reference %q", want)
		}
	}
}
