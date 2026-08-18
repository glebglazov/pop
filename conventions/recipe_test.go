package conventions

import (
	"bytes"
	"strings"
	"testing"
)

// TestEveryKindHasARecipe is the enum's whole justification: a kind pop admits
// is a kind pop knows how to derive.
func TestEveryKindHasARecipe(t *testing.T) {
	for _, kind := range Kinds() {
		if strings.TrimSpace(Recipe(kind)) == "" {
			t.Errorf("kind %s has no recipe", kind)
		}
	}
}

// TestRenderRecipeIsLabelledAsAMethod: a recipe and a convention travel the
// same stream, and one of them is instructions to carry out. The reader has to
// be able to tell which it received without reading the body.
func TestRenderRecipeIsLabelledAsAMethod(t *testing.T) {
	var out bytes.Buffer
	if err := RenderRecipe(&out, KindCommits); err != nil {
		t.Fatal(err)
	}
	got := out.String()
	head := got
	if i := strings.Index(got, "##"); i > 0 {
		head = got[:i]
	}
	for _, want := range []string{"RECIPE commits", "METHOD, not a convention"} {
		if !strings.Contains(head, want) {
			t.Errorf("recipe output is not labelled a method before its body (%q missing):\n%s", want, head)
		}
	}
	if strings.Contains(head, "CONVENTION commits") {
		t.Errorf("recipe output announces itself as a convention:\n%s", head)
	}
}

// TestCommitsRecipeCarriesTheDerivation pins the prose that was triplicated
// across skills until ADR-0211 — the guard above all, which is load-bearing in
// any repository pop has drained.
func TestCommitsRecipeCarriesTheDerivation(t *testing.T) {
	recipe := Recipe(KindCommits)
	for what, want := range map[string]string{
		"the repository's document":     "docs/agents/commits.md",
		"the five-commit sample":        "last five commits",
		"the discard-pop-commits guard": "Discard pop-generated commits",
		"pop's own subject shape":       "tasks(...)",
		"the walk-further-back rule":    "Walk further back",
		"where a derived result goes":   "pop memory",
		"the no-convention result":      "No discernible commits convention",
	} {
		if !strings.Contains(recipe, want) {
			t.Errorf("commits recipe does not carry %s (%q):\n%s", what, want, recipe)
		}
	}
	// Subject grammar and body style are one kind, one document and one sample.
	// A recipe that mentions only subjects invites a second, half-derived
	// convention for bodies.
	for _, want := range []string{"subject grammar", "body style"} {
		if !strings.Contains(recipe, want) {
			t.Errorf("commits recipe does not say it covers %q:\n%s", want, recipe)
		}
	}
}

// TestCodeReviewIsAKindWithARecipe: review is configurable only through this
// stack, so the kind's presence in the enum is what makes a repository able to
// state its own standard at all (ADR-0214).
func TestCodeReviewIsAKindWithARecipe(t *testing.T) {
	var found bool
	for _, kind := range Kinds() {
		found = found || kind == KindCodeReview
	}
	if !found {
		t.Fatalf("code-review is not a Convention kind; Kinds() = %v", Kinds())
	}
	if strings.TrimSpace(Recipe(KindCodeReview)) == "" {
		t.Error("the code-review kind has no recipe to derive a standard with")
	}
}

// TestCodeReviewRecipeDerivesTheStandardFromTheRepository: pop ships no house
// style, so the recipe's whole job is to send the reader to the codebase's own
// evidence and then to a layer that keeps the answer.
func TestCodeReviewRecipeDerivesTheStandardFromTheRepository(t *testing.T) {
	recipe := Recipe(KindCodeReview)
	for what, want := range map[string]string{
		"the repository's own documents": "AGENTS.md",
		"its architectural decisions":    "docs/adr/",
		"its linter configuration":       ".golangci.yml",
		"its formatter and build":        "pre-commit",
		"the idiom of the code itself":   "idiom",
		"the team's own layer":           "docs/agents/code-review.md",
		"the smell baseline's source":    "Fowler",
		"a named smell":                  "Feature Envy",
		"the repository-overrides rule":  "The repository overrides.",
		"the judgement-call rule":        "Always a judgement call.",
	} {
		if !strings.Contains(recipe, want) {
			t.Errorf("code-review recipe does not carry %s (%q):\n%s", what, want, recipe)
		}
	}
	// Pop asserting a house style is the option ADR-0214 rejected; the floor
	// baseline is not that, so the recipe has to keep declining one.
	if !strings.Contains(recipe, "Pop does not ship a house style") {
		t.Errorf("code-review recipe does not decline to assert a house style:\n%s", recipe)
	}
	// ADR-0223 decision 5: pop memory is the wrong artifact for a review
	// standard, and the recipe must not send anyone back to it.
	for _, unwanted := range []string{"pop memory", "pop conventions set code-review", "No discernible code-review convention"} {
		if strings.Contains(recipe, unwanted) {
			t.Errorf("code-review recipe still carries %q, which ADR-0223 retired:\n%s", unwanted, recipe)
		}
	}
}

// TestIssueTrackerRecipeResolvesTheStore: the kind that rarely fires still has
// to answer on the machine where it does — one that never integrated.
func TestIssueTrackerRecipeResolvesTheStore(t *testing.T) {
	recipe := Recipe(KindIssueTracker)
	for _, want := range []string{"~/.agents/docs/issue-tracker.md", "pop integrate", "docs/agents/issue-tracker.md", "pop memory"} {
		if !strings.Contains(recipe, want) {
			t.Errorf("issue-tracker recipe does not name %q:\n%s", want, recipe)
		}
	}
}
