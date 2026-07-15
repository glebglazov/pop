package tasks

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/glebglazov/pop/store"
)

// TestVerifyFailedGateAcceptRecordsHumanPass: choosing Accept (menu "1") at the
// Verify-fail gate records a human-authored PASS at the work SHA carrying the
// typed note — the same store behavior as `pop tasks verify --accept` — and
// returns handled so the drain resumes to a verified terminal (ADR-0103).
func TestVerifyFailedGateAcceptRecordsHumanPass(t *testing.T) {
	d, m := setupDrainVerifyFixture(t, stubGit("shaGATE\n", "", ""), doneAFKSet(), nil)

	var out bytes.Buffer
	in := strings.NewReader("1\nthe retry is intentional\n")
	handled, err := handleInteractiveVerifyFailedGate(gateEnv{d: d, out: &out, in: in, runtimePath: "/rt", taskSetID: "demo"}, "/repo/.git", m, "shaGATE", "the retry looks flaky")
	if err != nil {
		t.Fatalf("handleInteractiveVerifyFailedGate: %v", err)
	}
	if !handled {
		t.Fatalf("Accept must return handled=true so the drain resumes")
	}

	stored := readStoredVerdict(t, d, "/repo/.git", "demo", "shaGATE")
	if stored == nil || stored.Verdict != "PASS" || !stored.HumanAuthored {
		t.Fatalf("stored verdict = %+v, want a human-authored PASS", stored)
	}
	if stored.Note != "the retry is intentional" {
		t.Fatalf("stored note = %q, want the typed accept note", stored.Note)
	}
	if !strings.Contains(out.String(), "Accepted") {
		t.Fatalf("output missing accepted-verdict summary:\n%s", out.String())
	}
}

// TestVerifyFailedGateRemediateSpawnsTask: choosing Remediate (menu "2") at the
// Verify-fail gate spawns a Remediation task carrying the recorded findings and
// the typed note — the same spawn behavior as `pop tasks verify --remediate` —
// and returns handled so the drain picks the new work up (ADR-0103).
func TestVerifyFailedGateRemediateSpawnsTask(t *testing.T) {
	d, m := setupDrainVerifyFixture(t, stubGit("shaGATE\n", "", ""), doneAFKSet(), nil)
	// A NEEDS-HUMAN verdict at the work SHA supplies the findings the spawned task
	// forwards as context.
	seedVerdict(t, d, store.VerifyVerdict{Repo: "/repo/.git", SetID: "demo", WorkSHA: "shaGATE", Verdict: "NEEDS-HUMAN", Findings: "the retry policy needs a human call"})

	var out bytes.Buffer
	in := strings.NewReader("2\ncap the retries at 3\n")
	handled, err := handleInteractiveVerifyFailedGate(gateEnv{d: d, out: &out, in: in, runtimePath: "/rt", taskSetID: "demo"}, "/repo/.git", m, "shaGATE", "the retry policy needs a human call")
	if err != nil {
		t.Fatalf("handleInteractiveVerifyFailedGate: %v", err)
	}
	if !handled {
		t.Fatalf("Remediate must return handled=true so the drain resumes")
	}

	body, err := os.ReadFile(filepath.Join(m.Dir, "02-remediation.md"))
	if err != nil {
		t.Fatalf("read remediation body: %v", err)
	}
	for _, want := range []string{"the retry policy needs a human call", "cap the retries at 3", "## Human note", "## Acceptance criteria"} {
		if !strings.Contains(string(body), want) {
			t.Fatalf("remediation body missing %q:\n%s", want, body)
		}
	}
	if !strings.Contains(out.String(), "Spawned remediation") {
		t.Fatalf("output missing spawned-remediation summary:\n%s", out.String())
	}
	// Spawning invalidated the cached verdict.
	if stored := readStoredVerdict(t, d, "/repo/.git", "demo", "shaGATE"); stored != nil {
		t.Fatalf("cached verdict = %+v, want nil after remediation invalidated the episode", stored)
	}
}

// TestVerifyFailedGateExitFallsThrough: exiting the gate (menu "0") returns
// handled=false so the caller falls back to the static advice and the
// no-runnable exit — and neither disposition action fires.
func TestVerifyFailedGateExitFallsThrough(t *testing.T) {
	d, m := setupDrainVerifyFixture(t, stubGit("shaGATE\n", "", ""), doneAFKSet(), nil)
	seedVerdict(t, d, store.VerifyVerdict{Repo: "/repo/.git", SetID: "demo", WorkSHA: "shaGATE", Verdict: "NEEDS-HUMAN", Findings: "findings"})

	var out bytes.Buffer
	in := strings.NewReader("0\n")
	handled, err := handleInteractiveVerifyFailedGate(gateEnv{d: d, out: &out, in: in, runtimePath: "/rt", taskSetID: "demo"}, "/repo/.git", m, "shaGATE", "findings")
	if err != nil {
		t.Fatalf("handleInteractiveVerifyFailedGate: %v", err)
	}
	if handled {
		t.Fatalf("Exit must return handled=false")
	}
	// Exit disposes of nothing: the seeded NEEDS-HUMAN verdict is untouched.
	if stored := readStoredVerdict(t, d, "/repo/.git", "demo", "shaGATE"); stored == nil || stored.Verdict != "NEEDS-HUMAN" {
		t.Fatalf("exit must not change the verdict, got %+v", stored)
	}
}

// TestVerifyFailedGateYesSkipsPrompt: --yes (and a non-TTY input) no-ops the
// gate — it returns handled=false without consuming input or acting, so
// unattended runs fall straight through to the flag-driven disposition.
func TestVerifyFailedGateYesSkipsPrompt(t *testing.T) {
	d, m := setupDrainVerifyFixture(t, stubGit("shaGATE\n", "", ""), doneAFKSet(), nil)
	seedVerdict(t, d, store.VerifyVerdict{Repo: "/repo/.git", SetID: "demo", WorkSHA: "shaGATE", Verdict: "NEEDS-HUMAN", Findings: "findings"})

	var out bytes.Buffer
	handled, err := handleInteractiveVerifyFailedGate(gateEnv{d: d, out: &out, in: strings.NewReader("1\nnote\n"), yes: true, runtimePath: "/rt", taskSetID: "demo"}, "/repo/.git", m, "shaGATE", "findings")
	if err != nil {
		t.Fatalf("handleInteractiveVerifyFailedGate: %v", err)
	}
	if handled {
		t.Fatalf("--yes must return handled=false (prompt skipped)")
	}
	// The prompt is skipped: no accept fired, so the seeded verdict is unchanged.
	if stored := readStoredVerdict(t, d, "/repo/.git", "demo", "shaGATE"); stored == nil || stored.Verdict != "NEEDS-HUMAN" {
		t.Fatalf("--yes must not act, got stored verdict %+v", stored)
	}
	if strings.Contains(out.String(), "Verify-failed:") {
		t.Fatalf("--yes must not render the gate menu:\n%s", out.String())
	}
}
