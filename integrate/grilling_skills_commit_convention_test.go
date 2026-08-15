package integrate

import (
	"io/fs"
	"strings"
	"testing"
)

// grillWithDocsCommitSection is grill-with-docs' closing-commit subject step.
func grillWithDocsCommitSection(t *testing.T) string {
	t.Helper()
	body, err := skillFiles.ReadFile("skills/pop/grill-with-docs/SKILL.md")
	if err != nil {
		t.Fatalf("read embedded grill-with-docs skill: %v", err)
	}
	section := string(body)
	idx := strings.Index(section, "Before writing the subject")
	if idx < 0 {
		t.Fatal("grill-with-docs skill lost its closing-commit subject step")
	}
	return section[idx:]
}

// grillConsolidateCommitSection is grill-consolidate's commit-immediately step.
func grillConsolidateCommitSection(t *testing.T) string {
	t.Helper()
	body, err := skillFiles.ReadFile("skills/pop/grill-consolidate/SKILL.md")
	if err != nil {
		t.Fatalf("read embedded grill-consolidate skill: %v", err)
	}
	section := string(body)
	idx := strings.Index(section, "Commit immediately")
	if idx < 0 {
		t.Fatal("grill-consolidate skill lost its commit-immediately step")
	}
	return section[idx:]
}

// TestGrillingSkills_CommitConventionContract pins ADR-0211's move for the two
// grilling skills: both resolve the closing commit's subject through pop
// rather than sampling the log themselves.
func TestGrillingSkills_CommitConventionContract(t *testing.T) {
	t.Parallel()

	for _, want := range []string{"pop repo conventions get commits", "Exit 0", "Exit 1", "recipe"} {
		if !strings.Contains(grillWithDocsCommitSection(t), want) {
			t.Errorf("grill-with-docs skill does not resolve the commit convention through pop: missing %q", want)
		}
		if !strings.Contains(grillConsolidateCommitSection(t), want) {
			t.Errorf("grill-consolidate skill does not resolve the commit convention through pop: missing %q", want)
		}
	}

	// grill-consolidate keeps its own concrete fallback subjects — those are
	// its defaults, not a derivation rule this move retires.
	for _, want := range []string{
		"docs(context): consolidate glossary fragments",
		"Consolidate context fragments",
	} {
		if !strings.Contains(grillConsolidateCommitSection(t), want) {
			t.Errorf("grill-consolidate skill lost its own fallback subject %q", want)
		}
	}
}

// TestGrillingSkills_NoOwnLogSamplingRecipe is the un-drift guard: no embedded
// skill body may carry its own git-log sampling derivation for the commit
// convention. That derivation lives in the `commits` recipe now — a fourth
// copy anywhere in the shipped skills is exactly the drift ADR-0211 retires.
func TestGrillingSkills_NoOwnLogSamplingRecipe(t *testing.T) {
	t.Parallel()

	err := fs.WalkDir(skillFiles, "skills/pop", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !strings.HasSuffix(path, ".md") {
			return nil
		}
		body, err := skillFiles.ReadFile(path)
		if err != nil {
			t.Fatalf("read embedded %s: %v", path, err)
		}
		text := string(body)
		for _, gone := range []string{
			"git log -5 --format",
			"house style",
		} {
			if strings.Contains(text, gone) {
				t.Errorf("%s still carries its own log-sampling recipe: %q", path, gone)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk embedded skills: %v", err)
	}
}
