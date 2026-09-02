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

// TestImplementationConventionCarriesTheRepositorysOwnDocument: the Refiner's
// labelled block resolves the `implementation` stack (ADR-0246), so a repository
// that commits `docs/agents/implementation.md` changes the standard the
// changeset is held against, and pop's shipped rule list is what it displaced.
func TestImplementationConventionCarriesTheRepositorysOwnDocument(t *testing.T) {
	f := newConventionFixture(t)
	shipped, err := implementationConvention(f.deps.tasksDeps())(f.repo)
	if err != nil {
		t.Fatalf("resolve shipped implementation convention: %v", err)
	}
	if !strings.Contains(shipped, "## What good code looks like here") {
		t.Fatalf("pop's shipped answer must reach the Refiner as a rule list:\n%s", shipped)
	}
	if strings.Contains(shipped, "## What a pass may fix") {
		t.Fatalf("the fix licence belongs in the Refiner's prompt, not the shipped answer:\n%s", shipped)
	}

	f.repoDoc(t, "implementation", "# Our standard\n\nEvery exported function carries a doc comment.\n")
	prose, err := implementationConvention(f.deps.tasksDeps())(f.repo)
	if err != nil {
		t.Fatalf("resolve committed implementation convention: %v", err)
	}
	if !strings.Contains(prose, "Every exported function carries a doc comment") {
		t.Fatalf("the repository's own document must be what the Refiner is handed:\n%s", prose)
	}
	if strings.Contains(prose, "What good code looks like here") {
		t.Fatalf("the committed document displaces pop's shipped answer whole:\n%s", prose)
	}
}
