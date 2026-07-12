package tasks

import (
	"bytes"
	"strings"
	"testing"

	"github.com/glebglazov/pop/store"
)

// TestAutoRemediationSpawnForwardFeedsAcceptedNote: a human Accept note recorded
// before an auto-origin Remediation task is spawned survives the spawn's verdict
// invalidation and feeds forward into the next Verifier run — the remediation
// path now preserves the note exactly like the scope-growth path (ADR-0103 /
// ADR-0105), where previously it was silently dropped.
func TestAutoRemediationSpawnForwardFeedsAcceptedNote(t *testing.T) {
	d, m := setupDrainVerifyFixture(t, stubGit("shaB\n", "", ""), doneAFKSet(), nil)
	repo := "/repo/.git"
	seedVerdict(t, d, store.VerifyVerdict{
		Repo: repo, SetID: "demo", WorkSHA: "shaB", Verdict: "PASS",
		HumanAuthored: true, Note: "the widget gap is deliberate",
	})

	if _, err := spawnRemediationTask(d, m, repo, "shaB", "criterion 2 unmet", "", RemediationOriginAuto); err != nil {
		t.Fatalf("spawnRemediationTask: %v", err)
	}

	// The accepted PASS itself no longer immunizes the set.
	if got := readStoredVerdict(t, d, repo, "demo", "shaB"); got != nil {
		t.Fatalf("accepted PASS survived the auto remediation invalidation: %+v", got)
	}

	// The set's next Verifier run (once the Remediation task drains) still hears
	// about the human's earlier note.
	var gotPrompt string
	if _, err := ensureVerifyVerdict(d, nil, verifyCoreOptions{
		Repo: repo, RuntimePath: "/rt", SetID: "demo", Output: &bytes.Buffer{},
		runVerifier: func(prompt string) (string, error) { gotPrompt = prompt; return "VERDICT: PASS\n", nil },
	}, m, "shaB"); err != nil {
		t.Fatalf("ensureVerifyVerdict: %v", err)
	}
	if !strings.Contains(gotPrompt, "the widget gap is deliberate") {
		t.Fatalf("re-verify prompt must forward-feed the accepted note surviving the auto remediation spawn:\n%s", gotPrompt)
	}
	if !strings.Contains(gotPrompt, "Prior human note") {
		t.Fatalf("re-verify prompt must frame the note as a prior-human-note context section:\n%s", gotPrompt)
	}
}

// TestHumanRemediateForwardFeedsAcceptedNote: the same preservation holds for a
// human-origin Remediate (`pop tasks verify --remediate`) — a separately
// recorded Accept note must not be lost when the human later remediates a
// different finding on the same set.
func TestHumanRemediateForwardFeedsAcceptedNote(t *testing.T) {
	d, defPath := setupVerifyFixture(t, stubGit("shaA\n", "", ""))
	seedVerifyVerdict(t, d, store.VerifyVerdict{
		Repo: "/repo/.git", SetID: "demo", WorkSHA: "shaA", Verdict: "PASS",
		HumanAuthored: true, Note: "the extra allocation is deliberate",
	})

	if _, err := verifyResolvedSet(d, nil, verifyCoreOptions{
		Repo: "/repo/.git", DefPath: defPath, RuntimePath: "/rt", SetID: "demo",
		Output: &bytes.Buffer{}, Remediate: true, RemediateNote: "please also fix Y",
	}); err != nil {
		t.Fatalf("verifyResolvedSet remediate: %v", err)
	}

	// The prior accept no longer immunizes the set.
	if got := readStoredVerdict(t, d, "/repo/.git", "demo", "shaA"); got != nil {
		t.Fatalf("accepted PASS survived the human remediate invalidation: %+v", got)
	}

	var gotPrompt string
	if _, err := verifyResolvedSet(d, nil, verifyCoreOptions{
		Repo: "/repo/.git", DefPath: defPath, RuntimePath: "/rt", SetID: "demo",
		Output: &bytes.Buffer{},
		runVerifier: func(prompt string) (string, error) {
			gotPrompt = prompt
			return "VERDICT: PASS\n", nil
		},
	}); err != nil {
		t.Fatalf("verifyResolvedSet re-verify: %v", err)
	}
	if !strings.Contains(gotPrompt, "the extra allocation is deliberate") {
		t.Fatalf("re-verify prompt must forward-feed the accepted note surviving the human remediate:\n%s", gotPrompt)
	}
}
