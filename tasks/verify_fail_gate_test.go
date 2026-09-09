package tasks

import (
	"bytes"
	"io"
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

// TestVerifyFailedGateAgentAssistanceAdvisory: choosing Agent assistance (menu "3")
// launches attended assistance, then re-shows the gate menu without changing the
// stored verdict or manifest — advisory only, matching the interrupt gate shape.
func TestVerifyFailedGateAgentAssistanceAdvisory(t *testing.T) {
	d, m := setupDrainVerifyFixture(t, stubGit("shaGATE\n", "", ""), doneAFKSet(), nil)
	seedVerdict(t, d, store.VerifyVerdict{Repo: "/repo/.git", SetID: "demo", WorkSHA: "shaGATE", Verdict: "NEEDS-HUMAN", Findings: "the retry looks flaky"})

	runner := &configurableHITLAssistanceRunner{t: t}
	d.Runner = runner

	var out bytes.Buffer
	in := strings.NewReader("3\n0\n")
	handled, err := handleInteractiveVerifyFailedGate(gateEnv{d: d, out: &out, in: in, cfg: NewAttendedSession(nil, "claude"), runtimePath: "/rt", taskSetID: "demo"}, "/repo/.git", m, "shaGATE", "the retry looks flaky")
	if err != nil {
		t.Fatalf("handleInteractiveVerifyFailedGate: %v", err)
	}
	if handled {
		t.Fatalf("advisory assistance must return handled=false so the set stays Verify-failed")
	}

	outStr := out.String()
	for _, want := range []string{
		"1. Accept (record a human-authored PASS)",
		"2. Remediate (spawn a fix task)",
		"3. Agent assistance",
		"4. Open a shell in the checkout",
		"0. Exit",
		"Starting Verify-failed assistance:",
	} {
		if !strings.Contains(outStr, want) {
			t.Fatalf("gate menu missing %q:\n%s", want, outStr)
		}
	}
	if strings.Contains(outStr, "Re-verify") {
		t.Fatalf("verify-fail gate must not offer re-verify:\n%s", outStr)
	}
	if runner.attendedCalls != 1 || runner.runCalls != 0 {
		t.Fatalf("runner calls: attended=%d run=%d, want attended only", runner.attendedCalls, runner.runCalls)
	}
	if len(runner.args) != 3 || runner.args[0] != "--permission-mode" || runner.args[1] != "auto" || !strings.Contains(runner.args[2], "You are assisting a human at a Verify-failed gate") {
		t.Fatalf("assistance prompt = %v", runner.args)
	}
	if strings.Count(outStr, "Verify-failed:") < 2 {
		t.Fatalf("assistance did not re-show the gate menu:\n%s", outStr)
	}
	// Verdict untouched — assistance is advisory.
	if stored := readStoredVerdict(t, d, "/repo/.git", "demo", "shaGATE"); stored == nil || stored.Verdict != "NEEDS-HUMAN" {
		t.Fatalf("assistance must not change verdict, got %+v", stored)
	}
}

// TestVerifyFailedGateEnterDefaultsToAgentAssistance: an empty line at the gate
// takes the Enter default, which is assistance rather than exit — so the cheapest
// keystroke reads the findings for the human instead of walking away from them.
func TestVerifyFailedGateEnterDefaultsToAgentAssistance(t *testing.T) {
	d, m := setupDrainVerifyFixture(t, stubGit("shaGATE\n", "", ""), doneAFKSet(), nil)
	runner := &configurableHITLAssistanceRunner{t: t}
	d.Runner = runner

	var out bytes.Buffer
	in := strings.NewReader("\n0\n")
	handled, err := handleInteractiveVerifyFailedGate(gateEnv{d: d, out: &out, in: in, cfg: NewAttendedSession(nil, "claude"), runtimePath: "/rt", taskSetID: "demo"}, "/repo/.git", m, "shaGATE", "the retry looks flaky")
	if err != nil {
		t.Fatalf("handleInteractiveVerifyFailedGate: %v", err)
	}
	if handled {
		t.Fatalf("advisory assistance must return handled=false so the set stays Verify-failed")
	}
	if runner.attendedCalls != 1 {
		t.Fatalf("Enter at the gate ran %d attended assistance sessions, want 1:\n%s", runner.attendedCalls, out.String())
	}
}

// standaloneVerifyGateReader is the input a standalone Forced verification
// reads its gate answer from. Its check fires on the first read — the moment
// the gate opens — which is where the hold discipline is observable: the
// Verifier's claim is gone and only the non-claiming gate hold remains.
func standaloneVerifyGateReader(t *testing.T, d *Deps, response string) io.Reader {
	t.Helper()
	return &checkingPromptReader{
		t:        t,
		response: response,
		check: func(t *testing.T) {
			claim, err := ReadCheckoutClaim(d, "/rt")
			if err != nil {
				t.Fatalf("ReadCheckoutClaim at gate: %v", err)
			}
			if claim != nil {
				t.Fatalf("Verifier checkout claim still active at gate: %+v", claim)
			}
			hold, err := GetCheckoutGateHold(d, "/rt")
			if err != nil {
				t.Fatalf("GetCheckoutGateHold at gate: %v", err)
			}
			if hold == nil || hold.SetID != "demo" || hold.Claim {
				t.Fatalf("gate hold = %+v, want demo's non-claiming hold", hold)
			}
		},
	}
}

func assertStandaloneVerifyGateReleased(t *testing.T, d *Deps) {
	t.Helper()
	hold, err := GetCheckoutGateHold(d, "/rt")
	if err != nil {
		t.Fatalf("GetCheckoutGateHold after gate: %v", err)
	}
	if hold != nil {
		t.Fatalf("gate hold leaked after gate closed: %+v", hold)
	}
}

// TestStandaloneVerifyGateAcceptsVerdict: Accept at the gate a forced non-PASS
// opened records the same noted human-authored PASS as `--accept` and prints
// that flag's block, so a human never sees the tail they have just acted on.
func TestStandaloneVerifyGateAcceptsVerdict(t *testing.T) {
	d, defPath := setupVerifyFixture(t, stubGit("shaGATE\n", "", ""))
	var out bytes.Buffer
	_, err := verifyResolvedSet(d, nil, verifyCoreOptions{
		Repo: "/repo/.git", DefPath: defPath, RuntimePath: "/rt", SetID: "demo", Output: &out,
		confirmIn: standaloneVerifyGateReader(t, d, "1\nreviewed by hand\n"),
		runVerifier: func(string) (string, error) {
			return "VERDICT: FIXABLE\nFINDINGS: retry is unstable\n", nil
		},
	})
	if err != nil {
		t.Fatalf("verifyResolvedSet: %v", err)
	}
	stored := readStoredVerdict(t, d, "/repo/.git", "demo", "shaGATE")
	if stored == nil || stored.Verdict != "PASS" || !stored.HumanAuthored || stored.Note != "reviewed by hand" {
		t.Fatalf("stored verdict = %+v, want noted human-authored PASS", stored)
	}
	if !strings.Contains(out.String(), "━━ Accepted verify verdict for demo") || strings.Contains(out.String(), "━━ Disposition for demo") {
		t.Fatalf("Accept output did not match the flag disposition block:\n%s", out.String())
	}
	assertStandaloneVerifyGateReleased(t, d)
}

// TestStandaloneVerifyGateSpawnsRemediation: Remediate at that gate spawns the
// same Remediation task as `--remediate`, carrying the fresh findings and the
// typed note.
func TestStandaloneVerifyGateSpawnsRemediation(t *testing.T) {
	d, defPath := setupVerifyFixture(t, stubGit("shaGATE\n", "", ""))
	var out bytes.Buffer
	_, err := verifyResolvedSet(d, nil, verifyCoreOptions{
		Repo: "/repo/.git", DefPath: defPath, RuntimePath: "/rt", SetID: "demo", Output: &out,
		confirmIn: standaloneVerifyGateReader(t, d, "2\ncap retries at three\n"),
		runVerifier: func(string) (string, error) {
			return "VERDICT: NEEDS-HUMAN\nFINDINGS: retry policy is unspecified\n", nil
		},
	})
	if err != nil {
		t.Fatalf("verifyResolvedSet: %v", err)
	}
	body, err := os.ReadFile(filepath.Join(defPath, "demo", "02-remediation.md"))
	if err != nil {
		t.Fatalf("read remediation task: %v", err)
	}
	if !strings.Contains(string(body), "retry policy is unspecified") || !strings.Contains(string(body), "cap retries at three") {
		t.Fatalf("remediation task lacks verdict or note:\n%s", body)
	}
	if !strings.Contains(out.String(), "━━ Spawned remediation task for demo") || strings.Contains(out.String(), "━━ Disposition for demo") {
		t.Fatalf("Remediate output did not match the flag disposition block:\n%s", out.String())
	}
	assertStandaloneVerifyGateReleased(t, d)
}

// TestStandaloneVerifyGateExitKeepsCachedVerdict: exit dispositions nothing —
// the verdict stays cached for the next drain, and the tail says what that
// drain will do with it.
func TestStandaloneVerifyGateExitKeepsCachedVerdict(t *testing.T) {
	d, defPath := setupVerifyFixture(t, stubGit("shaGATE\n", "", ""))
	var out bytes.Buffer
	_, err := verifyResolvedSet(d, nil, verifyCoreOptions{
		Repo: "/repo/.git", DefPath: defPath, RuntimePath: "/rt", SetID: "demo", Output: &out,
		confirmIn: standaloneVerifyGateReader(t, d, "0\n"),
		runVerifier: func(string) (string, error) {
			return "VERDICT: FIXABLE\nFINDINGS: retry is unstable\n", nil
		},
	})
	if err != nil {
		t.Fatalf("verifyResolvedSet: %v", err)
	}
	if stored := readStoredVerdict(t, d, "/repo/.git", "demo", "shaGATE"); stored == nil || stored.Verdict != "FIXABLE" {
		t.Fatalf("exit changed cached verdict: %+v", stored)
	}
	if !strings.Contains(out.String(), "Verify-failed:") || !strings.Contains(out.String(), "━━ Disposition for demo") {
		t.Fatalf("exit did not print the gate and disposition tail:\n%s", out.String())
	}
	assertStandaloneVerifyGateReleased(t, d)
}

// TestStandaloneVerifySkipsGateHeadless: with no terminal to prompt there is
// nobody to disposition the verdict, so the tail is the whole answer.
func TestStandaloneVerifySkipsGateHeadless(t *testing.T) {
	d, defPath := setupVerifyFixture(t, stubGit("shaHEADLESS\n", "", ""))
	var out bytes.Buffer
	_, err := verifyResolvedSet(d, nil, verifyCoreOptions{
		Repo: "/repo/.git", DefPath: defPath, RuntimePath: "/rt", SetID: "demo", Output: &out,
		confirmIn: NonInteractiveReader{},
		runVerifier: func(string) (string, error) {
			return "VERDICT: NEEDS-HUMAN\nFINDINGS: needs a decision\n", nil
		},
	})
	if err != nil {
		t.Fatalf("verifyResolvedSet: %v", err)
	}
	if strings.Contains(out.String(), "Verify-failed:") || !strings.Contains(out.String(), "━━ Disposition for demo") {
		t.Fatalf("headless output opened a gate or omitted the tail:\n%s", out.String())
	}
	assertStandaloneVerifyGateReleased(t, d)
}

// TestStandaloneVerifySkipsGateForHumanCompletion: a Human completion has
// nothing to disposition — the verdict lands as a mark — so it gets its own
// tail and no gate.
func TestStandaloneVerifySkipsGateForHumanCompletion(t *testing.T) {
	d, defPath := setupVerifyFixture(t, stubGit("shaHUMAN\n", "", ""))
	m := LoadManifest(d, "demo", filepath.Join(defPath, "demo", "index.json"))
	m.HumanCompleted = true
	if err := WriteManifestAtomic(d, m); err != nil {
		t.Fatalf("write Human completion: %v", err)
	}
	var out bytes.Buffer
	_, err := verifyResolvedSet(d, nil, verifyCoreOptions{
		Repo: "/repo/.git", DefPath: defPath, RuntimePath: "/rt", SetID: "demo", Output: &out,
		confirmIn: strings.NewReader("1\nshould not be read\n"),
		runVerifier: func(string) (string, error) {
			return "VERDICT: FIXABLE\nFINDINGS: advisory only\n", nil
		},
	})
	if err != nil {
		t.Fatalf("verifyResolvedSet: %v", err)
	}
	if strings.Contains(out.String(), "Verify-failed:") || !strings.Contains(out.String(), "This Human completion stays complete") {
		t.Fatalf("Human completion output opened a gate or omitted its tail:\n%s", out.String())
	}
	if stored := readStoredVerdict(t, d, "/repo/.git", "demo", "shaHUMAN"); stored == nil || stored.Verdict != "FIXABLE" {
		t.Fatalf("Human completion verdict = %+v, want cached FIXABLE mark", stored)
	}
	assertStandaloneVerifyGateReleased(t, d)
}

// TestStandaloneVerifyExplicitDispositionsSkipGate: `--accept` / `--remediate`
// are the human's decision already made, so neither reads gate input.
func TestStandaloneVerifyExplicitDispositionsSkipGate(t *testing.T) {
	tests := []struct {
		name      string
		accept    bool
		remediate bool
		want      string
	}{
		{name: "accept", accept: true, want: "━━ Accepted verify verdict for demo"},
		{name: "remediate", remediate: true, want: "━━ Spawned remediation task for demo"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			d, defPath := setupVerifyFixture(t, stubGit("shaFLAG\n", "", ""))
			seedVerdict(t, d, store.VerifyVerdict{
				Repo: "/repo/.git", SetID: "demo", WorkSHA: "shaFLAG", Verdict: "NEEDS-HUMAN", Findings: "needs a decision",
			})
			in := &checkingPromptReader{
				t: t,
				check: func(t *testing.T) {
					t.Fatal("an explicit disposition must not read gate input")
				},
				response: "0\n",
			}
			var out bytes.Buffer
			_, err := verifyResolvedSet(d, nil, verifyCoreOptions{
				Repo: "/repo/.git", DefPath: defPath, RuntimePath: "/rt", SetID: "demo", Output: &out,
				Accept: tc.accept, AcceptNote: "reviewed", Remediate: tc.remediate, RemediateNote: "fix it", confirmIn: in,
			})
			if err != nil {
				t.Fatalf("verifyResolvedSet: %v", err)
			}
			if strings.Contains(out.String(), "Verify-failed:") || !strings.Contains(out.String(), tc.want) {
				t.Fatalf("explicit disposition opened a gate or omitted its output block:\n%s", out.String())
			}
		})
	}
}
