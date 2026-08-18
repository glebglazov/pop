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

// toTasksCommitConventionSection is the skill's own commit-convention section.
func toTasksCommitConventionSection(t *testing.T) string {
	t.Helper()
	body := toTasksSkillBody(t)
	section := body[strings.Index(body, "## Commit convention"):]
	if idx := strings.Index(section, "\n## No quiz"); idx > 0 {
		section = section[:idx]
	}
	return section
}

// TestToTasksSkill_CommitConventionContract pins what survives ADR-0211: the
// commit grammar is still resolved while the work is broken down, but the skill
// resolves it by asking pop rather than deriving it, and still owns the two
// manifest fields while deferring their shape to the guide.
func TestToTasksSkill_CommitConventionContract(t *testing.T) {
	t.Parallel()
	section := toTasksCommitConventionSection(t)

	// Resolution is a command call, and both of its outcomes are handled.
	for _, want := range []string{
		"pop conventions get commits",
		"always exits 0",
		"ANSWER",
		"METHOD",
		"recipe",
	} {
		if !strings.Contains(section, want) {
			t.Errorf("to-tasks skill does not resolve the commit convention through pop: missing %q", want)
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
	// A non-pop Work store has no pop manifest to write these keys into.
	if !strings.Contains(section, "store is not pop") {
		t.Error("to-tasks skill no longer skips the step when the Work store is not pop")
	}
	// Like every pop-specific rule in this overlaid file, it sits below the marker
	// the drift test compares against upstream.
	body := toTasksSkillBody(t)
	marker := strings.Index(body, "POP OVERLAY")
	if marker < 0 {
		t.Fatal("to-tasks skill lost its overlay marker")
	}
	if idx := strings.Index(body, "## Commit convention"); idx < marker {
		t.Error("the commit-convention section must live below the POP OVERLAY marker")
	}
}

// TestToTasksSkill_CommitConventionRecipeMoved guards the direction of the move:
// the derivation ladder lives in the `commits` recipe now, so the skill must not
// carry a second copy that can drift from it, and the retired document name must
// not survive as an alias.
func TestToTasksSkill_CommitConventionRecipeMoved(t *testing.T) {
	t.Parallel()
	body := toTasksSkillBody(t)

	for _, gone := range []string{
		"docs/commit-format.md",
		"last 5 commits",
		"last five commits",
		"tasks(...)",
		"own accent",
	} {
		if strings.Contains(body, gone) {
			t.Errorf("to-tasks skill still carries the retired derivation ladder: %q", gone)
		}
	}
	if !strings.Contains(toTasksCommitConventionSection(t), "docs/agents/commits.md") {
		t.Error("to-tasks skill does not name docs/agents/commits.md as the repository's commit document")
	}
}
