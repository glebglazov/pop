package tasks

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

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

// TestAssistOpensWhileTheSetIsHeld: a session opens on a set another drain is
// running and on a set parked at one of its gates. Assist reads Task storage and
// claims nothing, so there is nothing for either occupant to refuse it over.
func TestAssistOpensWhileTheSetIsHeld(t *testing.T) {
	for _, tc := range []struct {
		name string
		hold func(t *testing.T, d *Deps, runtime string)
	}{
		{
			name: "live drain on the same set",
			hold: func(t *testing.T, d *Deps, runtime string) {
				seedLiveDrain(t, d, runtime, "demo", 4242, "live-tok")
			},
		},
		{
			name: "drain parked at a gate",
			hold: func(t *testing.T, d *Deps, runtime string) {
				seedGateHold(t, d, runtime, "demo", 4242, "live-tok")
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d, defPath, root := setupAssistFixture(t, doneAFKSet())
			runtime, err := ResolveRuntimePathWith(d, root, root)
			if err != nil {
				t.Fatalf("ResolveRuntimePathWith: %v", err)
			}
			tc.hold(t, d, runtime)

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
			if !strings.Contains(out.String(), "Assist session: demo") {
				t.Fatalf("assist must open over a held set:\n%s", out.String())
			}
		})
	}
}

// TestAssistOpensOnArchivedSet: an archived set opens and says it is archived.
func TestAssistOpensOnArchivedSet(t *testing.T) {
	d, defPath, root := setupAssistFixture(t, doneAFKSet())
	runtime, err := ResolveRuntimePathWith(d, root, root)
	if err != nil {
		t.Fatalf("ResolveRuntimePathWith: %v", err)
	}
	pd := &project.Deps{Git: d.Git, FS: d.FS}
	// Archiving writes against registered state, so the set is registered first.
	if _, err := RegisterWith(d, defPath, StatePathFor(defPath)); err != nil {
		t.Fatalf("RegisterWith: %v", err)
	}
	if _, err := ArchiveTaskSetWith(d, pd, loadConfigVerifyEnabled, ResolveInput{CWD: root, DefinitionOverride: defPath}, "demo"); err != nil {
		t.Fatalf("ArchiveTaskSetWith: %v", err)
	}

	var out bytes.Buffer
	err = AssistTaskSetWith(d, pd, loadConfigVerifyEnabled, AssistOptions{
		ResolveInput: ResolveInput{CWD: root, DefinitionOverride: defPath, RuntimeOverride: runtime},
		TaskSetID:    "demo",
		Output:       &out,
		Input:        strings.NewReader("0\n"),
	})
	if err != nil {
		t.Fatalf("AssistTaskSetWith on an archived set: %v", err)
	}
	if !strings.Contains(out.String(), "Archived:") {
		t.Fatalf("an archived set must say so:\n%s", out.String())
	}
}

// TestAssistOpensOnMalformedManifest: a manifest that will not parse is what a
// human opens Assist to look at, so the session shows the parse errors and opens
// the menu on them.
func TestAssistOpensOnMalformedManifest(t *testing.T) {
	d, defPath, root := setupAssistFixture(t, doneAFKSet())
	runtime, err := ResolveRuntimePathWith(d, root, root)
	if err != nil {
		t.Fatalf("ResolveRuntimePathWith: %v", err)
	}
	if err := os.WriteFile(filepath.Join(defPath, "demo", "index.json"), []byte("{ not json"), 0o644); err != nil {
		t.Fatal(err)
	}

	var out bytes.Buffer
	err = AssistTaskSetWith(d, &project.Deps{Git: d.Git, FS: d.FS}, loadConfigVerifyEnabled, AssistOptions{
		ResolveInput: ResolveInput{CWD: root, DefinitionOverride: defPath, RuntimeOverride: runtime},
		TaskSetID:    "demo",
		Output:       &out,
		Input:        strings.NewReader("0\n"),
	})
	if err != nil {
		t.Fatalf("AssistTaskSetWith on a malformed set: %v", err)
	}
	outStr := out.String()
	if !strings.Contains(outStr, "Manifest is malformed:") {
		t.Fatalf("assist must show the manifest diagnostics:\n%s", outStr)
	}
	if !strings.Contains(outStr, "0. Exit") {
		t.Fatalf("assist must still open the menu over a malformed manifest:\n%s", outStr)
	}
}

// TestAssistAddressesTheSetsOwnCheckout: run from a checkout of another
// repository, the session addresses the set's own bound checkout instead of
// refusing over where the human was standing.
func TestAssistAddressesTheSetsOwnCheckout(t *testing.T) {
	d, defPath, root := setupAssistFixture(t, doneAFKSet())
	elsewhere := t.TempDir()
	// Two repositories: the set's checkout answers /repo/.git, the checkout the
	// human is standing in answers /elsewhere/.git.
	d.Git = &deps.MockGit{CommandInDirFunc: func(dir string, args ...string) (string, error) {
		inElsewhere := strings.HasPrefix(dir, elsewhere)
		switch {
		case len(args) >= 2 && args[0] == "rev-parse" && args[1] == "HEAD":
			return "shaASSIST\n", nil
		case len(args) >= 2 && args[0] == "rev-parse" && args[1] == "--git-common-dir":
			if inElsewhere {
				return "/elsewhere/.git\n", nil
			}
			return "/repo/.git\n", nil
		case len(args) >= 2 && args[0] == "rev-parse" && args[1] == "--show-toplevel":
			if inElsewhere {
				return elsewhere + "\n", nil
			}
			return root + "\n", nil
		}
		return "", nil
	}}

	var out bytes.Buffer
	err := AssistTaskSetWith(d, &project.Deps{Git: d.Git, FS: d.FS}, loadConfigVerifyEnabled, AssistOptions{
		ResolveInput: ResolveInput{CWD: elsewhere, DefinitionOverride: defPath, RuntimeOverride: root},
		TaskSetID:    "demo",
		Output:       &out,
		Input:        strings.NewReader("0\n"),
	})
	if err != nil {
		t.Fatalf("AssistTaskSetWith from another repository: %v", err)
	}
	if !strings.Contains(out.String(), "Runtime: "+root) {
		t.Fatalf("session must address the set's own checkout:\n%s", out.String())
	}
}

// TestAssistOpensWithNoUsableAgent: with every attended agent unusable the
// session still opens and shows the set; the walk runs when assistance is
// chosen, and its refusal prints in the menu.
func TestAssistOpensWithNoUsableAgent(t *testing.T) {
	d, defPath, root := setupAssistFixture(t, doneAFKSet())
	d.LookPath = func(string) (string, error) { return "", errors.New("not on PATH") }
	runtime, err := ResolveRuntimePathWith(d, root, root)
	if err != nil {
		t.Fatalf("ResolveRuntimePathWith: %v", err)
	}

	var out bytes.Buffer
	// Choose agent assistance, read the refusal, then exit from the re-shown menu.
	err = AssistTaskSetWith(d, &project.Deps{Git: d.Git, FS: d.FS}, loadConfigVerifyEnabled, AssistOptions{
		ResolveInput: ResolveInput{CWD: root, DefinitionOverride: defPath, RuntimeOverride: runtime},
		TaskSetID:    "demo",
		Output:       &out,
		Input:        strings.NewReader("1\n0\n"),
	})
	if err != nil {
		t.Fatalf("AssistTaskSetWith with no usable agent: %v", err)
	}
	outStr := out.String()
	if !strings.Contains(outStr, "Assist session: demo") {
		t.Fatalf("session must open with no usable agent:\n%s", outStr)
	}
	if !strings.Contains(outStr, "Could not start Assist assistance: no usable attended agent") {
		t.Fatalf("the agent refusal must print in the menu:\n%s", outStr)
	}
}

// TestAssistAcceptWaitsForTheCheckoutThenRuns: Accept takes the checkout when it
// is chosen. Held by another set, it waits in the Admission queue — naming what
// it waits on — and runs once the holder lets go.
func TestAssistAcceptWaitsForTheCheckoutThenRuns(t *testing.T) {
	d, defPath, root, runtime := setupAssistVerifyFailedFixture(t)
	rival, err := BeginDrain(d, runtime, "rival", io.Discard)
	if err != nil {
		t.Fatalf("rival drain: %v", err)
	}

	out := &liveBuffer{}
	done := make(chan error, 1)
	go func() {
		done <- AssistTaskSetWith(d, &project.Deps{Git: d.Git, FS: d.FS}, loadConfigVerifyEnabled, AssistOptions{
			ResolveInput: ResolveInput{CWD: root, DefinitionOverride: defPath, RuntimeOverride: runtime},
			TaskSetID:    "demo",
			Output:       out,
			Input:        strings.NewReader("1\nsigning off\n0\n"),
		})
	}()
	waitForOutput(t, out, "Waiting for checkout")
	if err := rival.Finish(store.DrainEnding{State: store.StateFinished}); err != nil {
		t.Fatalf("release the rival claim: %v", err)
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("AssistTaskSetWith: %v", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("Accept never took the checkout after the holder let go")
	}

	if !strings.Contains(out.String(), "held by set rival") {
		t.Fatalf("the wait must name who holds the checkout:\n%s", out.String())
	}
	stored := readStoredVerdict(t, d, "/repo/.git", "demo", "shaASSIST")
	if stored == nil || stored.Verdict != "PASS" || !stored.HumanAuthored {
		t.Fatalf("stored verdict = %+v, want human-authored PASS", stored)
	}
	claim, err := ReadCheckoutClaim(d, runtime)
	if err != nil {
		t.Fatalf("ReadCheckoutClaim: %v", err)
	}
	if claim != nil {
		t.Fatalf("Accept must give the checkout back: %+v", claim)
	}
}

// TestAssistAcceptInterruptedWaitReturnsToTheMenu: an interrupt while a verb
// waits for the checkout ends the wait, not the session — the menu comes back
// and the set is untouched.
func TestAssistAcceptInterruptedWaitReturnsToTheMenu(t *testing.T) {
	// Keep the process alive through the SIGINT this test raises, whatever order
	// the wait loop's own Notify lands in.
	guard := make(chan os.Signal, 1)
	signal.Notify(guard, syscall.SIGINT)
	defer signal.Stop(guard)

	d, defPath, root, runtime := setupAssistVerifyFailedFixture(t)
	rival, err := BeginDrain(d, runtime, "rival", io.Discard)
	if err != nil {
		t.Fatalf("rival drain: %v", err)
	}
	t.Cleanup(func() { _ = rival.Finish(store.DrainEnding{State: store.StateFinished}) })

	out := &liveBuffer{}
	done := make(chan error, 1)
	go func() {
		done <- AssistTaskSetWith(d, &project.Deps{Git: d.Git, FS: d.FS}, loadConfigVerifyEnabled, AssistOptions{
			ResolveInput: ResolveInput{CWD: root, DefinitionOverride: defPath, RuntimeOverride: runtime},
			TaskSetID:    "demo",
			Output:       out,
			Input:        strings.NewReader("1\nsigning off\n0\n"),
		})
	}()
	waitForOutput(t, out, "Waiting for checkout")
	if err := syscall.Kill(os.Getpid(), syscall.SIGINT); err != nil {
		t.Fatalf("raise SIGINT: %v", err)
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("an interrupted wait must not end the session: %v", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("SIGINT did not end the wait")
	}

	outStr := out.String()
	if !strings.Contains(outStr, "Accept cancelled") {
		t.Fatalf("the menu must say the verb was cancelled:\n%s", outStr)
	}
	if strings.Count(outStr, "Verify-failed:") < 2 {
		t.Fatalf("the session must come back to its menu:\n%s", outStr)
	}
	if stored := readStoredVerdict(t, d, "/repo/.git", "demo", "shaASSIST"); stored == nil || stored.Verdict != "NEEDS-HUMAN" {
		t.Fatalf("an abandoned Accept must leave the verdict alone, got %+v", stored)
	}
}

// TestAssistRemediateWaitsForTheCheckoutThenRuns: Remediate takes the checkout
// the same way Accept does — it appends to the manifest, so the tree must hold
// still for it — and spawns its task once the holder lets go.
func TestAssistRemediateWaitsForTheCheckoutThenRuns(t *testing.T) {
	d, defPath, root, runtime := setupAssistVerifyFailedFixture(t)
	rival, err := BeginDrain(d, runtime, "rival", io.Discard)
	if err != nil {
		t.Fatalf("rival drain: %v", err)
	}

	out := &liveBuffer{}
	done := make(chan error, 1)
	go func() {
		done <- AssistTaskSetWith(d, &project.Deps{Git: d.Git, FS: d.FS}, loadConfigVerifyEnabled, AssistOptions{
			ResolveInput: ResolveInput{CWD: root, DefinitionOverride: defPath, RuntimeOverride: runtime},
			TaskSetID:    "demo",
			Output:       out,
			Input:        strings.NewReader("2\nfix the retry\n0\n"),
		})
	}()
	waitForOutput(t, out, "Waiting for checkout")
	if err := rival.Finish(store.DrainEnding{State: store.StateFinished}); err != nil {
		t.Fatalf("release the rival claim: %v", err)
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("AssistTaskSetWith: %v", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("Remediate never took the checkout after the holder let go")
	}

	m := LoadManifest(d, "demo", filepath.Join(defPath, "demo", "index.json"))
	if len(m.Tasks) != 2 {
		t.Fatalf("remediation task was not appended: %+v", m.Tasks)
	}
	claim, err := ReadCheckoutClaim(d, runtime)
	if err != nil {
		t.Fatalf("ReadCheckoutClaim: %v", err)
	}
	if claim != nil {
		t.Fatalf("Remediate must give the checkout back: %+v", claim)
	}
}

// TestAssistFoldTakesTheCheckoutForItsOwnAct: Fold runs under the Checkout claim
// and gives it back when it is done.
func TestAssistFoldTakesTheCheckoutForItsOwnAct(t *testing.T) {
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
		Provisioned: true,
	}); err != nil {
		t.Fatalf("PutBinding: %v", err)
	}
	runtime, err := ResolveRuntimePathWith(d, root, root)
	if err != nil {
		t.Fatalf("ResolveRuntimePathWith: %v", err)
	}

	heldBy := ""
	var out bytes.Buffer
	err = AssistTaskSetWith(d, &project.Deps{Git: d.Git, FS: d.FS}, func(string) (*config.Config, error) {
		return &config.Config{}, nil
	}, AssistOptions{
		ResolveInput: ResolveInput{CWD: root, DefinitionOverride: defPath, RuntimeOverride: runtime},
		TaskSetID:    "demo",
		Output:       &out,
		Input:        strings.NewReader("3\n"),
		Fold: func(string, io.Reader, io.Writer) error {
			if claim, cerr := ReadCheckoutClaim(d, runtime); cerr == nil && claim != nil {
				heldBy = claim.Holder.ContainerID
			}
			return nil
		},
	})
	if err != nil {
		t.Fatalf("AssistTaskSetWith: %v", err)
	}
	if heldBy != "demo" {
		t.Fatalf("fold ran under claim %q, want the set's own", heldBy)
	}
	claim, err := ReadCheckoutClaim(d, runtime)
	if err != nil {
		t.Fatalf("ReadCheckoutClaim: %v", err)
	}
	if claim != nil {
		t.Fatalf("fold must give the checkout back: %+v", claim)
	}
}

// setupAssistVerifyFailedFixture is a demo set sitting at VERIFY-FAILED, with the
// runtime checkout resolved — the state the Accept and Remediate verbs act from.
func setupAssistVerifyFailedFixture(t *testing.T) (*Deps, string, string, string) {
	t.Helper()
	d, defPath, root := setupAssistFixture(t, doneAFKSet())
	seedVerdict(t, d, store.VerifyVerdict{
		Repo: "/repo/.git", SetID: "demo", WorkSHA: "shaASSIST",
		Verdict: "NEEDS-HUMAN", Findings: "the retry looks flaky",
	})
	runtime, err := ResolveRuntimePathWith(d, root, root)
	if err != nil {
		t.Fatalf("ResolveRuntimePathWith: %v", err)
	}
	return d, defPath, root, runtime
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
		Provisioned: true,
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
		t.Fatalf("DONE managed menu missing fold:\n%s", out.String())
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
		Provisioned: true,
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

// TestAssistSessionHoldsNothing: the session itself takes no Checkout gate hold
// and no Checkout claim. Exclusivity belongs to the verbs inside the menu, so an
// open menu never stands between another command and the checkout.
func TestAssistSessionHoldsNothing(t *testing.T) {
	d, defPath, root := setupAssistFixture(t, doneAFKSet())
	runtime, err := ResolveRuntimePathWith(d, root, root)
	if err != nil {
		t.Fatalf("ResolveRuntimePathWith: %v", err)
	}

	checked := false
	in := &checkingPromptReader{
		t: t,
		check: func(t *testing.T) {
			checked = true
			if hold, err := GetCheckoutGateHold(d, runtime); err != nil || hold != nil {
				t.Errorf("assist session registered a gate hold: %+v (err %v)", hold, err)
			}
			if claim, err := ReadCheckoutClaim(d, runtime); err != nil || claim != nil {
				t.Errorf("assist session claimed the checkout: %+v (err %v)", claim, err)
			}
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
	if !checked {
		t.Fatal("the menu never prompted, so nothing was observed")
	}
}

// TestRuntimeShellSaysTheCheckoutIsUnclaimed: the shell is the deliberate
// exception to Tree-stable admission, and its banner says so.
func TestRuntimeShellSaysTheCheckoutIsUnclaimed(t *testing.T) {
	d := newTestDeps(t)
	d.Runner = fakeShellRunner{}
	var out bytes.Buffer
	if err := spawnRuntimeShell(d, strings.NewReader(""), "/rt", &out); err != nil {
		t.Fatalf("spawnRuntimeShell: %v", err)
	}
	if !strings.Contains(out.String(), "the checkout is not claimed while it is open") {
		t.Fatalf("shell banner must say the checkout stays unclaimed:\n%s", out.String())
	}
}

type fakeShellRunner struct{}

func (fakeShellRunner) Run(context.Context, string, io.Writer, io.Writer, string, ...string) (int, error) {
	return 0, nil
}

func (fakeShellRunner) Start(context.Context, string, io.Writer, io.Writer, string, ...string) (*ManagedProcess, error) {
	return nil, errors.New("not used")
}

func TestBuildAssistPromptOmitsTaskBodies(t *testing.T) {
	d, m := setupDrainVerifyFixture(t, stubGit("sha1\n", "", ""), []Task{
		{ID: "01-a", File: "01-a.md", Title: "A", Type: "AFK", Status: "open", Effort: "heavy"},
	}, nil)
	writePromptTestFile(t, filepath.Join(m.Dir, "01-a.md"), "## Secret body\n\nDo not inline me.\n\n## Acceptance criteria\n\n- [ ] ok\n")

	prompt := BuildAssistPrompt(d, nil, "demo", m, StatusReady, "/rt", "cached findings")
	for _, want := range []string{
		"You are assisting a human in an Assist session",
		"Task set: demo",
		"Derived status: READY",
		"01-a [AFK open effort=heavy]",
		"cached findings",
		"Task contract to respect",
		"Operations you may perform",
		"Do not start a Drain",
		// Task-appending is granted once, via the shared authoring grant
		// (ADR-0255), not restated in this Operations list.
		"You may create a new Task set, or append a task to this one",
		"You do not effect a disposition",
		"an edit to the task manifest",
	} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("prompt missing %q:\n%s", want, prompt)
		}
	}
	if strings.Contains(prompt, "Secret body") || strings.Contains(prompt, "Do not inline me") {
		t.Fatalf("Assist prompt must not inline task bodies:\n%s", prompt)
	}
}

// liveBuffer is an output a test can read while the session under test is still
// writing to it — the only way to act on a wait exactly when the wait is really
// on screen, rather than on a queue row that may already have been granted.
type liveBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *liveBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *liveBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

func waitForOutput(t *testing.T, b *liveBuffer, want string) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if strings.Contains(b.String(), want) {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("output never showed %q:\n%s", want, b.String())
}
