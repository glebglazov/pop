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

	// Resolution is one command call with one outcome to handle.
	for _, want := range []string{
		"pop conventions get commits",
		"always exits 0",
		"prints rules to follow",
	} {
		if !strings.Contains(section, want) {
			t.Errorf("to-tasks skill does not resolve the commit convention through pop: missing %q", want)
		}
	}
	// The subject is the agent's to write, and its shape is deferred rather than
	// restated.
	for _, want := range []string{
		"`commit_subject`",
		"pop tasks\nauthoring-guide",
	} {
		if !strings.Contains(section, want) {
			t.Errorf("to-tasks skill's publish direction missing %q", want)
		}
	}
	// ADR-0228: the set-level key is pop's own projection of the stack, so the
	// skill names it only to say not to write it. An instruction to supply it is
	// exactly the hand-copy that ADR retires.
	for _, want := range []string{
		"Do **not** write the set-level **`commit_convention`**",
		"pop writes that key",
	} {
		if !strings.Contains(section, want) {
			t.Errorf("to-tasks skill still asks an agent for the commit convention: missing %q", want)
		}
	}
	for _, gone := range []string{
		"write both fields",
		"the convention text itself",
	} {
		if strings.Contains(section, gone) {
			t.Errorf("to-tasks skill still instructs writing `commit_convention`: %q", gone)
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

// TestToTasksSkill_CommitConventionDerivationMoved guards the direction of the
// move: the log-sampling ladder lives in pop's shipped `commits` answer now, so
// the skill must not carry a second copy that can drift from it, and the retired
// document name must not survive as an alias.
func TestToTasksSkill_CommitConventionDerivationMoved(t *testing.T) {
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
