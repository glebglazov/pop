package tasks

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/glebglazov/pop/config"
	"github.com/glebglazov/pop/internal/deps"
	"github.com/glebglazov/pop/project"
	"github.com/glebglazov/pop/store"
)

// setupAssistFixture stands up a discoverable "demo" set under an isolated data
// dir with stub git answering HEAD + common-dir, so AssistTaskSetWith can resolve
// repository identity and derive Verify-failed status from a cached verdict.
func setupAssistFixture(t *testing.T, tasks []Task) (*Deps, string, string) {
	t.Helper()
	d := newTestDeps(t)
	root := t.TempDir()
	defPath := filepath.Join(root, "tasks")
	setupManifest(t, defPath, "demo", tasks)
	d.Git = assistStubGit(root, "shaASSIST", "/repo/.git")
	d.ProcessAlive = func(pid int) bool { return pid == os.Getpid() || pid == 4242 }
	d.ProcessStartToken = func(pid int) (string, bool) {
		if pid == 4242 {
			return "live-tok", true
		}
		return "self-tok", true
	}
	return d, defPath, root
}

// assistStubGit answers the git probes Assist needs: show-toplevel, common-dir,
// HEAD, and empty log/diff.
func assistStubGit(toplevel, head, commonDir string) *deps.MockGit {
	return &deps.MockGit{CommandInDirFunc: func(dir string, args ...string) (string, error) {
		switch {
		case len(args) >= 2 && args[0] == "rev-parse" && args[1] == "HEAD":
			return head + "\n", nil
		case len(args) >= 2 && args[0] == "rev-parse" && args[1] == "--git-common-dir":
			return commonDir + "\n", nil
		case len(args) >= 2 && args[0] == "rev-parse" && args[1] == "--show-toplevel":
			return toplevel + "\n", nil
		case len(args) >= 1 && args[0] == "log":
			return "", nil
		case len(args) >= 1 && args[0] == "diff":
			return "", nil
		}
		return "", nil
	}}
}

func loadConfigVerifyEnabled(string) (*config.Config, error) {
	return verifyEnabledConfig(), nil
}

// TestAssistSessionVerifyFailedMenuRouting: opening assist on a Verify-failed set
// presents the verify-fail gate menu (Accept / Remediate / assistance / shell /
// exit), and exiting leaves without invoking the Verifier.
func TestAssistSessionVerifyFailedMenuRouting(t *testing.T) {
	d, defPath, root := setupAssistFixture(t, doneAFKSet())
	seedVerdict(t, d, store.VerifyVerdict{
		Repo: "/repo/.git", SetID: "demo", WorkSHA: "shaASSIST",
		Verdict: "NEEDS-HUMAN", Findings: "the retry looks flaky",
	})

	runtime, err := ResolveRuntimePathWith(d, root, root)
	if err != nil {
		t.Fatalf("ResolveRuntimePathWith: %v", err)
	}

	var out bytes.Buffer
	err = AssistTaskSetWith(d, &project.Deps{Git: d.Git, FS: d.FS}, loadConfigVerifyEnabled, AssistOptions{
		ResolveInput: ResolveInput{CWD: root, DefinitionOverride: defPath, RuntimeOverride: runtime},
		TaskSetID:    "demo",
		AgentPreset:  "claude",
		Output:       &out,
		Input:        strings.NewReader("0\n"),
	})
	if err != nil {
		t.Fatalf("AssistTaskSetWith: %v", err)
	}
	outStr := out.String()
	for _, want := range []string{
		"Assist session: demo [VERIFY-FAILED]",
		"1. Accept (record a human-authored PASS)",
		"2. Remediate (spawn a fix task)",
		"3. Agent assistance",
		"4. Open a shell in the checkout",
		"0. Exit",
		"the retry looks flaky",
	} {
		if !strings.Contains(outStr, want) {
			t.Fatalf("assist output missing %q:\n%s", want, outStr)
		}
	}
	if strings.Contains(outStr, "Re-verify") {
		t.Fatalf("assist verify-fail menu must not offer re-verify:\n%s", outStr)
	}
	// Cached verdict untouched — Exit, and no Verifier run.
	if stored := readStoredVerdict(t, d, "/repo/.git", "demo", "shaASSIST"); stored == nil || stored.Verdict != "NEEDS-HUMAN" {
		t.Fatalf("exit must leave cached verdict, got %+v", stored)
	}
}

// TestAssistSessionVerifyFailedAcceptReentersSignOff: Accept at the assist
// verify-fail menu re-derives status and lands on the Awaiting-approval HITL gate.
func TestAssistSessionVerifyFailedAcceptReentersSignOff(t *testing.T) {
	d, defPath, root := setupAssistFixture(t, terminalHITLSet())
	seedVerdict(t, d, store.VerifyVerdict{
		Repo: "/repo/.git", SetID: "demo", WorkSHA: "shaASSIST",
		Verdict: "NEEDS-HUMAN", Findings: "needs a look",
	})
	runtime, err := ResolveRuntimePathWith(d, root, root)
	if err != nil {
		t.Fatalf("ResolveRuntimePathWith: %v", err)
	}

	var out bytes.Buffer
	// Accept with a note, then exit the ensuing HITL / awaiting-approval menu.
	err = AssistTaskSetWith(d, &project.Deps{Git: d.Git, FS: d.FS}, loadConfigVerifyEnabled, AssistOptions{
		ResolveInput: ResolveInput{CWD: root, DefinitionOverride: defPath, RuntimeOverride: runtime},
		TaskSetID:    "demo",
		AgentPreset:  "claude",
		Output:       &out,
		Input:        strings.NewReader("1\nsigning off\n0\n"),
	})
	if err != nil {
		t.Fatalf("AssistTaskSetWith: %v", err)
	}
	outStr := out.String()
	if !strings.Contains(outStr, "VERIFY-FAILED") {
		t.Fatalf("expected initial verify-fail menu:\n%s", outStr)
	}
	if !strings.Contains(outStr, "AWAITING-APPROVAL") && !strings.Contains(outStr, "Human-blocked") {
		t.Fatalf("after Accept, expected sign-off / HITL gate:\n%s", outStr)
	}
	stored := readStoredVerdict(t, d, "/repo/.git", "demo", "shaASSIST")
	if stored == nil || stored.Verdict != "PASS" || !stored.HumanAuthored {
		t.Fatalf("stored verdict = %+v, want human-authored PASS", stored)
	}
}

// TestAssistSessionRefusesLiveDrain: assist refuses while the set's own drain is
// live, naming the occupant.
func TestAssistSessionRefusesLiveDrain(t *testing.T) {
	d, defPath, root := setupAssistFixture(t, doneAFKSet())
	runtime, err := ResolveRuntimePathWith(d, root, root)
	if err != nil {
		t.Fatalf("ResolveRuntimePathWith: %v", err)
	}
	seedLiveDrain(t, d, runtime, "demo", 4242, "live-tok")

	err = AssistTaskSetWith(d, &project.Deps{Git: d.Git, FS: d.FS}, loadConfigVerifyEnabled, AssistOptions{
		ResolveInput: ResolveInput{CWD: root, DefinitionOverride: defPath, RuntimeOverride: runtime},
		TaskSetID:    "demo",
		Output:       &bytes.Buffer{},
		Input:        strings.NewReader("0\n"),
	})
	if err == nil {
		t.Fatal("assist must refuse while the set's drain is live")
	}
	if !strings.Contains(err.Error(), "live drain") {
		t.Fatalf("error must name the live drain: %v", err)
	}
}

// TestAssistGenericMenuOffersFoldForDoneBoundSet: the generic assist menu offers
// Fold when the set is DONE and still holds a Worktree binding.
func TestAssistGenericMenuOffersFoldForDoneBoundSet(t *testing.T) {
	d, defPath, root := setupAssistFixture(t, doneAFKSet())
	s, _, err := d.Store(true)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.PutBinding(store.Binding{
		ScopedKey:   "repo\x00demo",
		RuntimePath: root,
		Branch:      "demo",
		Project:     "demo",
	}); err != nil {
		t.Fatalf("PutBinding: %v", err)
	}
	runtime, err := ResolveRuntimePathWith(d, root, root)
	if err != nil {
		t.Fatalf("ResolveRuntimePathWith: %v", err)
	}

	var out bytes.Buffer
	err = AssistTaskSetWith(d, &project.Deps{Git: d.Git, FS: d.FS}, func(string) (*config.Config, error) {
		return &config.Config{}, nil
	}, AssistOptions{
		ResolveInput: ResolveInput{CWD: root, DefinitionOverride: defPath, RuntimeOverride: runtime},
		TaskSetID:    "demo",
		AgentPreset:  "claude",
		Output:       &out,
		Input:        strings.NewReader("0\n"),
		Fold:         func(string, io.Reader, io.Writer) error { return nil },
	})
	if err != nil {
		t.Fatalf("AssistTaskSetWith: %v", err)
	}
	if !strings.Contains(out.String(), "Assist session: demo [DONE]") {
		t.Fatalf("expected generic DONE menu:\n%s", out.String())
	}
	if !strings.Contains(out.String(), "3. Fold branch back into Trunk and release checkout") {
		t.Fatalf("DONE bound menu missing fold:\n%s", out.String())
	}
}

// TestAssistFoldRefusalReasonReachesTheMenu: a refused fold prints the refusal
// itself, not just a status. The seam exists because assist used to re-exec `pop
// tasks fold`, whose refusal never made it back across the process boundary.
func TestAssistFoldRefusalReasonReachesTheMenu(t *testing.T) {
	d, defPath, root := setupAssistFixture(t, doneAFKSet())
	s, _, err := d.Store(true)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.PutBinding(store.Binding{
		ScopedKey:   "repo\x00demo",
		RuntimePath: root,
		Branch:      "demo",
		Project:     "demo",
	}); err != nil {
		t.Fatalf("PutBinding: %v", err)
	}
	runtime, err := ResolveRuntimePathWith(d, root, root)
	if err != nil {
		t.Fatalf("ResolveRuntimePathWith: %v", err)
	}

	var out bytes.Buffer
	folds := 0
	err = AssistTaskSetWith(d, &project.Deps{Git: d.Git, FS: d.FS}, func(string) (*config.Config, error) {
		return &config.Config{}, nil
	}, AssistOptions{
		ResolveInput: ResolveInput{CWD: root, DefinitionOverride: defPath, RuntimeOverride: runtime},
		TaskSetID:    "demo",
		AgentPreset:  "claude",
		Output:       &out,
		// Choose fold, see the refusal, then exit from the re-shown menu.
		Input: strings.NewReader("3\n0\n"),
		Fold: func(setID string, _ io.Reader, _ io.Writer) error {
			folds++
			if setID != "demo" {
				t.Fatalf("fold set = %q, want demo", setID)
			}
			return fmt.Errorf("fold refused: set worktree is dirty (%s)", runtime)
		},
	})
	if err != nil {
		t.Fatalf("AssistTaskSetWith: %v", err)
	}
	if folds != 1 {
		t.Fatalf("fold seam called %d times, want 1", folds)
	}
	if !strings.Contains(out.String(), "Fold failed: fold refused: set worktree is dirty") {
		t.Fatalf("menu must print the refusal reason:\n%s", out.String())
	}
}

// TestAssistGenericMenuOmitsFoldWhenUnbound: Fold is not offered without a binding.
func TestAssistGenericMenuOmitsFoldWhenUnbound(t *testing.T) {
	d, defPath, root := setupAssistFixture(t, doneAFKSet())
	runtime, err := ResolveRuntimePathWith(d, root, root)
	if err != nil {
		t.Fatalf("ResolveRuntimePathWith: %v", err)
	}

	var out bytes.Buffer
	err = AssistTaskSetWith(d, &project.Deps{Git: d.Git, FS: d.FS}, loadConfigVerifyEnabled, AssistOptions{
		ResolveInput: ResolveInput{CWD: root, DefinitionOverride: defPath, RuntimeOverride: runtime},
		TaskSetID:    "demo",
		Output:       &out,
		Input:        strings.NewReader("0\n"),
	})
	if err != nil {
		t.Fatalf("AssistTaskSetWith: %v", err)
	}
	if strings.Contains(out.String(), "Fold branch back into Trunk") {
		t.Fatalf("unbound DONE menu should not offer fold:\n%s", out.String())
	}
}

// TestAssistSessionRefusesNonInteractive: assist refuses without a TTY and names
// the headless equivalents.
func TestAssistSessionRefusesNonInteractive(t *testing.T) {
	d, defPath, root := setupAssistFixture(t, doneAFKSet())
	runtime, err := ResolveRuntimePathWith(d, root, root)
	if err != nil {
		t.Fatalf("ResolveRuntimePathWith: %v", err)
	}
	err = AssistTaskSetWith(d, &project.Deps{Git: d.Git, FS: d.FS}, loadConfigVerifyEnabled, AssistOptions{
		ResolveInput: ResolveInput{CWD: root, DefinitionOverride: defPath, RuntimeOverride: runtime},
		TaskSetID:    "demo",
		Output:       &bytes.Buffer{},
		Input:        NonInteractiveReader{},
	})
	if err == nil {
		t.Fatal("assist must refuse without a TTY")
	}
	msg := err.Error()
	for _, want := range []string{"interactive terminal", "--accept", "--remediate"} {
		if !strings.Contains(msg, want) {
			t.Fatalf("refusal must name headless equivalents, missing %q in: %v", want, err)
		}
	}
}

// TestAssistSessionRegistersNonClaimingGateHold: while the menu is up the session
// holds a non-claiming Checkout gate hold, released on exit.
func TestAssistSessionRegistersNonClaimingGateHold(t *testing.T) {
	d, defPath, root := setupAssistFixture(t, doneAFKSet())
	runtime, err := ResolveRuntimePathWith(d, root, root)
	if err != nil {
		t.Fatalf("ResolveRuntimePathWith: %v", err)
	}
	seedVerdict(t, d, store.VerifyVerdict{
		Repo: "/repo/.git", SetID: "demo", WorkSHA: "shaASSIST",
		Verdict: "NEEDS-HUMAN", Findings: "findings",
	})

	holdSeen := false
	in := &checkingPromptReader{
		t: t,
		check: func(*testing.T) {
			hold, err := GetCheckoutGateHold(d, runtime)
			if err != nil {
				t.Fatalf("GetCheckoutGateHold: %v", err)
			}
			if hold == nil {
				t.Fatal("checkout gate hold missing during assist session")
			}
			if hold.SetID != "demo" {
				t.Fatalf("hold set = %q, want demo", hold.SetID)
			}
			if hold.Claim {
				t.Fatal("assist session must register a non-claiming hold")
			}
			holdSeen = true
		},
		response: "0\n",
	}

	err = AssistTaskSetWith(d, &project.Deps{Git: d.Git, FS: d.FS}, loadConfigVerifyEnabled, AssistOptions{
		ResolveInput: ResolveInput{CWD: root, DefinitionOverride: defPath, RuntimeOverride: runtime},
		TaskSetID:    "demo",
		AgentPreset:  "claude",
		Output:       &bytes.Buffer{},
		Input:        in,
	})
	if err != nil {
		t.Fatalf("AssistTaskSetWith: %v", err)
	}
	if !holdSeen {
		t.Fatal("gate hold was not observed during the menu")
	}
	hold, err := GetCheckoutGateHold(d, runtime)
	if err != nil {
		t.Fatalf("GetCheckoutGateHold after exit: %v", err)
	}
	if hold != nil {
		t.Fatalf("checkout gate hold leaked after assist exit: %#v", hold)
	}
}

func TestBuildAssistPromptOmitsTaskBodies(t *testing.T) {
	d, m := setupDrainVerifyFixture(t, stubGit("sha1\n", "", ""), []Task{
		{ID: "01-a", File: "01-a.md", Title: "A", Type: "AFK", Status: "open", Effort: "heavy"},
	}, nil)
	writePromptTestFile(t, filepath.Join(m.Dir, "01-a.md"), "## Secret body\n\nDo not inline me.\n\n## Acceptance criteria\n\n- [ ] ok\n")

	prompt := BuildAssistPrompt(d, "demo", m, StatusReady, "/rt", "cached findings")
	for _, want := range []string{
		"You are assisting a human in an Assist session",
		"Task set: demo",
		"Derived status: READY",
		"01-a [AFK open effort=heavy]",
		"cached findings",
		"Task contract to respect",
		"Operations you may perform",
		"Do not start a Drain",
	} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("prompt missing %q:\n%s", want, prompt)
		}
	}
	if strings.Contains(prompt, "Secret body") || strings.Contains(prompt, "Do not inline me") {
		t.Fatalf("Assist prompt must not inline task bodies:\n%s", prompt)
	}
}
