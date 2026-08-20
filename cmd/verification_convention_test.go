package cmd

import (
	"strings"
	"testing"
)

// TestVerificationConventionCarriesTheRepositorysOwnDocument: the seam `cmd`
// hands the Verifier resolves the `verification` stack, so a repository that
// commits `docs/agents/verification.md` changes what the Verifier checks with no
// change to pop — and pop's shipped answer is what it displaced.
func TestVerificationConventionCarriesTheRepositorysOwnDocument(t *testing.T) {
	f := newConventionFixture(t)
	shipped, err := verificationConvention(f.deps.tasksDeps())(f.repo)
	if err != nil {
		t.Fatalf("resolve shipped verification convention: %v", err)
	}
	if strings.TrimSpace(shipped) == "" {
		t.Fatal("a kind always answers: pop's shipped verification answer must reach the Verifier")
	}

	f.repoDoc(t, "verification", "# Checking work here\n\nRun `mix test --stale`, then `mix credo --strict`.\n")
	prose, err := verificationConvention(f.deps.tasksDeps())(f.repo)
	if err != nil {
		t.Fatalf("resolve committed verification convention: %v", err)
	}
	if !strings.Contains(prose, "mix credo --strict") {
		t.Fatalf("the repository's own document must be what the Verifier is handed:\n%s", prose)
	}
	if strings.Contains(prose, "pop's own answer") {
		t.Fatalf("the committed document displaces pop's shipped answer whole:\n%s", prose)
	}
}

// TestCodeReviewConventionCarriesTheRepositorysOwnDocument: the Reviewer's body
// resolves the same way (ADR-0227 decision 1), so a repository that commits
// `docs/agents/code-review.md` changes the standard the changeset is held
// against, and pop's two-axis shipped answer is what it displaced.
func TestCodeReviewConventionCarriesTheRepositorysOwnDocument(t *testing.T) {
	f := newConventionFixture(t)
	shipped, err := codeReviewConvention(f.deps.tasksDeps())(f.repo)
	if err != nil {
		t.Fatalf("resolve shipped code-review convention: %v", err)
	}
	for _, want := range []string{"Axis 1", "Axis 2"} {
		if !strings.Contains(shipped, want) {
			t.Fatalf("pop's shipped answer must reach the Reviewer as a two-axis review (%q missing):\n%s", want, shipped)
		}
	}

	f.repoDoc(t, "code-review", "# Our standard\n\nEvery exported function carries a doc comment.\n")
	prose, err := codeReviewConvention(f.deps.tasksDeps())(f.repo)
	if err != nil {
		t.Fatalf("resolve committed code-review convention: %v", err)
	}
	if !strings.Contains(prose, "Every exported function carries a doc comment") {
		t.Fatalf("the repository's own document must be what the Reviewer is handed:\n%s", prose)
	}
	if strings.Contains(prose, "Axis 1") {
		t.Fatalf("the committed document displaces pop's shipped answer whole:\n%s", prose)
	}
}
