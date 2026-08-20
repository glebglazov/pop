package conventions

import (
	"bytes"
	"strings"
	"testing"
)

// TestEveryKindHasAShippedAnswer is the enum's whole justification: a kind pop
// admits is a kind pop can answer without anybody having written anything.
func TestEveryKindHasAShippedAnswer(t *testing.T) {
	for _, kind := range Kinds() {
		if strings.TrimSpace(Shipped(kind)) == "" {
			t.Errorf("kind %s has no shipped answer", kind)
		}
	}
}

// TestRenderShippedIsAnAnswer: `default <kind>` and a fallthrough hand over one
// body, and neither of them announces a method for the reader to work
// (ADR-0226 decision 1).
func TestRenderShippedIsAnAnswer(t *testing.T) {
	var out bytes.Buffer
	if err := RenderShipped(&out, KindCommits); err != nil {
		t.Fatal(err)
	}
	got := out.String()
	if !strings.Contains(got, "SHIPPED CONVENTION commits") {
		t.Errorf("shipped output is not labelled:\n%s", got)
	}
	if !strings.Contains(got, shippedLayer(KindCommits).Body) {
		t.Errorf("`default` does not print the body the last rank carries:\n%s", got)
	}
	if strings.Contains(got, "METHOD") {
		t.Errorf("shipped output announces itself as a method:\n%s", got)
	}
}

// TestNoShippedAnswerSendsTheReaderBackToWriteALayer: an answer that closes by
// telling an agent to record a convention is the derivation step ADR-0226
// deleted, wearing an answer's label.
func TestNoShippedAnswerSendsTheReaderBackToWriteALayer(t *testing.T) {
	for _, kind := range Kinds() {
		body := Shipped(kind)
		for _, gone := range []string{"pop memory", "pop conventions set", "Write the result down"} {
			if strings.Contains(body, gone) {
				t.Errorf("the %s shipped answer still instructs a write-back (%q)", kind, gone)
			}
		}
	}
}

// TestCommitsShippedAnswerSamplesTheLog pins the prose that was triplicated
// across skills until ADR-0211 — the guard above all, which is load-bearing in
// any repository pop has drained.
func TestCommitsShippedAnswerSamplesTheLog(t *testing.T) {
	answer := Shipped(KindCommits)
	for what, want := range map[string]string{
		"the five-commit sample":        "last five commits",
		"the discard-pop-commits guard": "Discard pop-generated commits",
		"pop's own subject shape":       "tasks(...)",
		"the walk-further-back rule":    "Walk further back",
		"the no-convention result":      "No discernible convention is a real result",
	} {
		if !strings.Contains(answer, want) {
			t.Errorf("commits shipped answer does not carry %s (%q):\n%s", what, want, answer)
		}
	}
	// Subject grammar and body style are one kind, one document and one sample.
	// An answer that mentions only subjects invites a second, half-read
	// convention for bodies.
	for _, want := range []string{"subject grammar", "body style"} {
		if !strings.Contains(answer, want) {
			t.Errorf("commits shipped answer does not say it covers %q:\n%s", want, answer)
		}
	}
}

// TestCodeReviewIsAKindWithAnAnswer: review is configurable only through this
// stack, so the kind's presence in the enum is what makes a repository able to
// state its own standard at all (ADR-0214).
func TestCodeReviewIsAKindWithAnAnswer(t *testing.T) {
	var found bool
	for _, kind := range Kinds() {
		found = found || kind == KindCodeReview
	}
	if !found {
		t.Fatalf("code-review is not a Convention kind; Kinds() = %v", Kinds())
	}
	if strings.TrimSpace(Shipped(KindCodeReview)) == "" {
		t.Error("the code-review kind has no shipped standard to hold a changeset against")
	}
}

// TestCodeReviewShippedAnswerIsTheSmellBaseline: the baseline is the standards
// content now, not a floor beneath a derivation method, and the two rules that
// keep it from overriding a repository travel with it (ADR-0226 decision 2).
func TestCodeReviewShippedAnswerIsTheSmellBaseline(t *testing.T) {
	answer := Shipped(KindCodeReview)
	for what, want := range map[string]string{
		"the smell baseline's source":    "Fowler",
		"a named smell":                  "Feature Envy",
		"the repository-overrides rule":  "The repository overrides.",
		"the judgement-call rule":        "Always a judgement call.",
		"the repository's own documents": "AGENTS.md",
		"its architectural decisions":    "docs/adr/",
		"its linter configuration":       ".golangci.yml",
		"its formatter and build":        "pre-commit",
		"the idiom of the code itself":   "idiom",
	} {
		if !strings.Contains(answer, want) {
			t.Errorf("code-review shipped answer does not carry %s (%q):\n%s", what, want, answer)
		}
	}
	// All twelve smells are the standard; a baseline missing one holds a
	// changeset against less than it says it does.
	for _, smell := range []string{
		"Mysterious Name", "Duplicated Code", "Feature Envy", "Data Clumps",
		"Primitive Obsession", "Repeated Switches", "Shotgun Surgery",
		"Divergent Change", "Speculative Generality", "Message Chains",
		"Middle Man", "Refused Bequest",
	} {
		if !strings.Contains(answer, smell) {
			t.Errorf("code-review shipped answer is missing the smell %q", smell)
		}
	}
}

// TestIssueTrackerShippedAnswerIsPopsTrackerDoc: the kind resolves to the
// document itself rather than to instructions for finding it, and it is the
// same bytes integration publishes as an asset.
func TestIssueTrackerShippedAnswerIsPopsTrackerDoc(t *testing.T) {
	answer := Shipped(KindIssueTracker)
	for _, want := range []string{"pop Work store", "pop tasks", "docs/agents/issue-tracker.md"} {
		if !strings.Contains(answer, want) {
			t.Errorf("issue-tracker shipped answer does not read as pop's tracker doc (%q missing)", want)
		}
	}
	if strings.Contains(answer, "pop integrate <agent>") {
		t.Errorf("issue-tracker shipped answer still tells the reader to refresh integration:\n%s", answer)
	}
}
