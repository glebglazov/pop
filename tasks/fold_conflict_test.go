package tasks

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/glebglazov/pop/config"
)

func TestBuildFoldConflictPromptCarriesContextAndBoundaries(t *testing.T) {
	d := newTestDeps(t)
	dir := t.TempDir()
	taskDir := filepath.Join(dir, "demo-set")
	if err := os.MkdirAll(taskDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(taskDir, "01-work.md"), []byte("## What to build\n\nShip the widget.\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	m := &Manifest{
		Valid: true,
		Dir:   taskDir,
		Tasks: []Task{{
			ID: "01-work", File: "01-work.md", Title: "Do work", Type: "AFK", Status: "done",
		}},
	}

	prompt := BuildFoldConflictPrompt(d, FoldConflictContext{
		SetID:       "demo-set",
		Manifest:    m,
		RuntimePath: "/wt/demo-set",
		SetBranch:   "pop/demo-set",
		TrunkBranch: "main",
		TrunkPath:   "/repo/trunk",
	}, []string{"clash.txt", "pkg/a.go"})

	for _, want := range []string{
		"Task set: demo-set",
		"Task set path: " + taskDir,
		"Set checkout (resolve here): /wt/demo-set",
		"Set branch: pop/demo-set",
		"Trunk branch rebasing onto: main",
		"Trunk worktree (read-only boundary): /repo/trunk",
		"- clash.txt",
		"- pkg/a.go",
		"01-work.md",
		"Ship the widget.",
		"Never touch the Trunk worktree",
		"Never push",
		"resolve inside the set checkout only",
		"git rebase --continue",
		"fold rebase conflict",
	} {
		if !strings.Contains(prompt, want) {
			t.Errorf("prompt missing %q\n%s", want, prompt)
		}
	}
}

func TestHandleFoldConflictRefusesWithoutTTY(t *testing.T) {
	d := newTestDeps(t)
	err := HandleFoldConflict(d, nil, FoldConflictContext{
		SetID:       "demo",
		RuntimePath: "/wt/demo",
		TrunkBranch: "main",
	}, FoldConflictAssistanceOptions{In: NonInteractiveReader{}})
	if err == nil || !strings.Contains(err.Error(), "conflict") {
		t.Fatalf("err = %v, want conflict refusal", err)
	}
	if !strings.Contains(err.Error(), "rebasing") {
		t.Fatalf("err = %v, want rebase wording", err)
	}
}

func TestPromptFoldConflictActionMenuOptions(t *testing.T) {
	var out bytes.Buffer
	in := strings.NewReader("0\n")
	reader := newPromptReader(in)
	action, err := promptFoldConflictAction(&out, in, reader, nil, nil, "demo", VerifiedAtBadge{
		State: VerifiedAtAtHead,
		SHA:   "abc123def456",
	}, nil)
	if err != nil {
		t.Fatalf("prompt: %v", err)
	}
	if action != foldConflictExit {
		t.Fatalf("action = %v, want exit", action)
	}
	got := out.String()
	for _, want := range []string{
		"Fold conflict: demo",
		"verified @ abc123def456",
		"1. Agent assistance (default)",
		"2. Resume fold",
		"3. Retry fold from scratch",
		"4. Verify set",
		"5. Abandon fold",
		"0. Exit",
		// Abandon and exit are different intentions, so the menu says which is which.
		"abort the rebase",
		"leave the rebase parked",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("menu missing %q\n%s", want, got)
		}
	}
}

func TestPromptFoldConflictActionDefaultsToAgent(t *testing.T) {
	var out bytes.Buffer
	in := strings.NewReader("\n")
	reader := newPromptReader(in)
	action, err := promptFoldConflictAction(&out, in, reader, nil, nil, "demo", VerifiedAtBadge{}, nil)
	if err != nil {
		t.Fatalf("prompt: %v", err)
	}
	if action != foldConflictAgent {
		t.Fatalf("action = %v, want agent", action)
	}
}

func TestPromptFoldConflictActionSelectsResumeRetryVerifyAbandon(t *testing.T) {
	cases := []struct {
		in   string
		want foldConflictAction
	}{
		{"2\n", foldConflictResume},
		{"3\n", foldConflictRetry},
		{"4\n", foldConflictVerify},
		{"5\n", foldConflictAbandon},
	}
	for _, tc := range cases {
		var out bytes.Buffer
		in := strings.NewReader(tc.in)
		reader := newPromptReader(in)
		got, err := promptFoldConflictAction(&out, in, reader, nil, nil, "demo", VerifiedAtBadge{}, nil)
		if err != nil {
			t.Fatalf("input %q: %v", tc.in, err)
		}
		if got != tc.want {
			t.Fatalf("input %q: got %v, want %v", tc.in, got, tc.want)
		}
	}
}

func TestOfferFoldPostResolveVerifyDeclineProceeds(t *testing.T) {
	d := newTestDeps(t)
	var out bytes.Buffer
	reader := newPromptReader(strings.NewReader("\n"))
	err := offerFoldPostResolveVerify(d, nil, FoldConflictContext{SetID: "demo"}, FoldConflictAssistanceOptions{}, &out, reader)
	if err != nil {
		t.Fatalf("decline verify: %v", err)
	}
	if !strings.Contains(out.String(), "Verify set? [y/N]:") {
		t.Fatalf("missing verify offer:\n%s", out.String())
	}
}

func TestOfferFoldPostResolveVerifyFailStops(t *testing.T) {
	d := newTestDeps(t)
	var out bytes.Buffer
	reader := newPromptReader(strings.NewReader("y\n"))
	err := offerFoldPostResolveVerify(d, &config.Config{}, FoldConflictContext{
		SetID:       "missing-set",
		RuntimePath: t.TempDir(),
	}, FoldConflictAssistanceOptions{
		RunVerifier: func(string) (string, error) {
			return "VERDICT: FIXABLE\nFINDINGS: still broken\n", nil
		},
	}, &out, reader)
	if err == nil {
		t.Fatal("expected verify failure to stop fold")
	}
	if !strings.Contains(err.Error(), "fold refused") {
		t.Fatalf("err = %v, want fold refused", err)
	}
}
