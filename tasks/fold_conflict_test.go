package tasks

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
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
		"Trunk branch merging in: main",
		"Trunk worktree (read-only boundary): /repo/trunk",
		"- clash.txt",
		"- pkg/a.go",
		"01-work.md",
		"Ship the widget.",
		"Never touch the Trunk worktree",
		"Never push",
		"resolve inside the set checkout only",
	} {
		if !strings.Contains(prompt, want) {
			t.Errorf("prompt missing %q\n%s", want, prompt)
		}
	}
}

func TestHandleFoldMergeConflictRefusesWithoutTTY(t *testing.T) {
	d := newTestDeps(t)
	err := HandleFoldMergeConflict(d, nil, FoldConflictContext{
		SetID:       "demo",
		RuntimePath: "/wt/demo",
		TrunkBranch: "main",
	}, FoldConflictAssistanceOptions{In: NonInteractiveReader{}})
	if err == nil || !strings.Contains(err.Error(), "conflict") {
		t.Fatalf("err = %v, want conflict refusal", err)
	}
}
