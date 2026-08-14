package integrate

import (
	"strings"
	"testing"
)

// toTasksSkillBody is the embedded breakdown skill as it ships.
func toTasksSkillBody(t *testing.T) string {
	t.Helper()
	body, err := skillFiles.ReadFile("skills/pop/to-tasks/SKILL.md")
	if err != nil {
		t.Fatalf("read embedded to-tasks skill: %v", err)
	}
	return string(body)
}

// TestToTasksSkill_CommitConventionContract pins ADR-0207's plan-time half: the
// commit grammar is resolved while the work is broken down, so the skill has to
// carry the discovery contract in full — format doc, then the log with pop's own
// commits filtered out, then nothing — and hand the field shape to the guide.
func TestToTasksSkill_CommitConventionContract(t *testing.T) {
	t.Parallel()
	body := toTasksSkillBody(t)

	section := body[strings.Index(body, "## Commit convention"):]
	if idx := strings.Index(section, "\n## No quiz"); idx > 0 {
		section = section[:idx]
	}

	// The three-layer discovery contract, in order.
	for _, want := range []string{
		"docs/commit-format.md",
		"last 5 commits",
		"write neither field",
	} {
		if !strings.Contains(section, want) {
			t.Errorf("to-tasks skill's commit-convention contract missing %q", want)
		}
	}
	// The self-noise guard: sampling the log without it teaches pop its own accent.
	for _, want := range []string{
		"Discard pop-generated\n   commits before sampling",
		"`tasks(...)`",
		"its own accent",
	} {
		if !strings.Contains(section, want) {
			t.Errorf("to-tasks skill does not guard against sampling pop's own commits: missing %q", want)
		}
	}
	// Both keys are named, and their shape is deferred rather than restated.
	for _, want := range []string{
		"`commit_subject`",
		"`commit_convention`",
		"pop tasks\nauthoring-guide",
	} {
		if !strings.Contains(section, want) {
			t.Errorf("to-tasks skill's publish direction missing %q", want)
		}
	}
	// Like every pop-specific rule in this overlaid file, it sits below the marker
	// the drift test compares against upstream.
	marker := strings.Index(body, "POP OVERLAY")
	if marker < 0 {
		t.Fatal("to-tasks skill lost its overlay marker")
	}
	if idx := strings.Index(body, "## Commit convention"); idx < marker {
		t.Error("the commit-convention section must live below the POP OVERLAY marker")
	}
}
