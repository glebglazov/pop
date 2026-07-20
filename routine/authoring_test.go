package routine

import (
	"bytes"
	"context"
	"io"
	"path/filepath"
	"strings"
	"testing"

	"github.com/glebglazov/pop/tasks"
)

// fakeAttendedRunner records an attended invocation instead of spawning a real
// agent. It satisfies tasks.CommandRunner (so it can sit in tasks.Deps.Runner)
// and tasks.AttendedCommandRunner (so the routine attended path picks it up).
type fakeAttendedRunner struct {
	called   bool
	dir      string
	name     string
	args     []string
	exitCode int
	err      error
}

func (f *fakeAttendedRunner) Run(ctx context.Context, dir string, stdout, stderr io.Writer, name string, args ...string) (int, error) {
	f.called = true
	f.dir = dir
	f.name = name
	f.args = args
	return f.exitCode, f.err
}

func (f *fakeAttendedRunner) Start(ctx context.Context, dir string, stdout, stderr io.Writer, name string, args ...string) (*tasks.ManagedProcess, error) {
	return nil, nil
}

func (f *fakeAttendedRunner) RunAttended(ctx context.Context, dir string, stdin io.Reader, stdout, stderr io.Writer, name string, args ...string) (int, error) {
	f.called = true
	f.dir = dir
	f.name = name
	f.args = args
	return f.exitCode, f.err
}

func TestIsCreateModePrompt(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name  string
		input string
		want  bool
	}{
		{name: "scaffolded stub", input: promptStub, want: true},
		{name: "blank", input: "", want: true},
		{name: "whitespace only", input: "  \n\t\n  ", want: true},
		{name: "authored", input: "# My routine\n\nDo the thing.\n", want: false},
		{name: "stub with trailing space", input: promptStub + " ", want: false},
	} {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := isCreateModePrompt(tc.input); got != tc.want {
				t.Fatalf("isCreateModePrompt(%q) = %v, want %v", tc.input, got, tc.want)
			}
		})
	}
}

func TestBuildAuthoringPromptCreateModeStub(t *testing.T) {
	root := t.TempDir()
	dataHome := filepath.Join(root, "data")
	home := filepath.Join(root, "home")
	d := refineDeps(t, dataHome, "", &bytes.Buffer{})
	addRoutineForGate(t, d, "gate", home)

	r, err := loadManifest(d, "gate")
	if err != nil {
		t.Fatal(err)
	}
	prompt := buildAuthoringPrompt(d, "gate", r)

	for _, want := range []string{
		"interview me and write a good prompt.md",
		"## Interview checklist",
		"Interview me until you can answer each of these",
	} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("create-mode prompt missing %q:\n%s", want, prompt)
		}
	}
	for _, absent := range []string{
		"already exists",
		"## Current prompt.md",
		"## Refinement checklist",
		"work out which of these items it already settles",
	} {
		if strings.Contains(prompt, absent) {
			t.Fatalf("create-mode prompt should not contain %q:\n%s", absent, prompt)
		}
	}
}

func TestBuildAuthoringPromptCreateModeWhitespaceOnly(t *testing.T) {
	root := t.TempDir()
	dataHome := filepath.Join(root, "data")
	home := filepath.Join(root, "home")
	d := refineDeps(t, dataHome, "", &bytes.Buffer{})
	addRoutineForGate(t, d, "gate", home)

	promptPath := filepath.Join(dataHome, "pop", "routines", "gate", "prompt.md")
	if err := d.FS.WriteFile(promptPath, []byte("  \n\t  "), 0o644); err != nil {
		t.Fatal(err)
	}

	r, err := loadManifest(d, "gate")
	if err != nil {
		t.Fatal(err)
	}
	prompt := buildAuthoringPrompt(d, "gate", r)

	if !strings.Contains(prompt, "## Interview checklist") {
		t.Fatalf("whitespace-only prompt should use create mode:\n%s", prompt)
	}
	if strings.Contains(prompt, "already exists") {
		t.Fatalf("whitespace-only prompt should not use revise mode:\n%s", prompt)
	}
}

func TestBuildAuthoringPromptReviseMode(t *testing.T) {
	root := t.TempDir()
	dataHome := filepath.Join(root, "data")
	home := filepath.Join(root, "home")
	d := refineDeps(t, dataHome, "", &bytes.Buffer{})
	addRoutineForGate(t, d, "gate", home)

	authored := "# Daily triage\n\nReview open PRs assigned to me and summarize blockers.\n"
	promptPath := filepath.Join(dataHome, "pop", "routines", "gate", "prompt.md")
	if err := d.FS.WriteFile(promptPath, []byte(authored), 0o644); err != nil {
		t.Fatal(err)
	}

	r, err := loadManifest(d, "gate")
	if err != nil {
		t.Fatal(err)
	}
	prompt := buildAuthoringPrompt(d, "gate", r)

	for _, want := range []string{
		"routine already exists",
		"this session changes its prompt.md",
		"## Current prompt.md",
		authored,
		"## Refinement checklist",
		"work out which of these items it already",
		"settles. Ask me only about what I want changed",
		"Goal",
		"Empty-run behavior",
	} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("revise-mode prompt missing %q:\n%s", want, prompt)
		}
	}
	for _, absent := range []string{
		"## Interview checklist",
		"Interview me until you can answer each of these",
	} {
		if strings.Contains(prompt, absent) {
			t.Fatalf("revise-mode prompt should not contain %q:\n%s", absent, prompt)
		}
	}
}

func TestRefineAgentSessionSpawnsInBoundDirWithFrontLoadedRules(t *testing.T) {
	root := t.TempDir()
	dataHome := filepath.Join(root, "data")
	home := filepath.Join(root, "home")
	var out bytes.Buffer
	d := refineDeps(t, dataHome, "1\n0\n", &out)
	runner := &fakeAttendedRunner{}
	d.Tasks.Runner = runner
	addRoutineForGate(t, d, "gate", home)

	if err := RefineWith(d, "gate", ""); err != nil {
		t.Fatal(err)
	}

	r, err := loadManifest(d, "gate")
	if err != nil {
		t.Fatal(err)
	}
	boundDir := r.Manifest.BoundDirectory

	if !runner.called {
		t.Fatalf("agent session should spawn the attended agent:\n%s", out.String())
	}
	if runner.dir != boundDir {
		t.Fatalf("session should run in the bound directory %q, got %q", boundDir, runner.dir)
	}
	if runner.name != "claude" {
		t.Fatalf("resolved agent = %q, want claude", runner.name)
	}
	if len(runner.args) == 0 {
		t.Fatal("attended invocation carried no prompt")
	}
	prompt := runner.args[len(runner.args)-1]

	promptPath := filepath.Join(dataHome, "pop", "routines", "gate", "prompt.md")
	memoryDir := filepath.Join(dataHome, "pop", "routines", "gate", "memory")
	for _, want := range []string{
		// Framework contract.
		"Before starting, read the routine memory directory",
		"When finished, write your report",
		memoryDir,
		"Schedule grammar",
		"utc",
		// Concrete paths.
		boundDir,
		promptPath,
		"Current schedule: every 6h",
		// Interview checklist.
		"Interview checklist",
		"Goal",
		"Data source",
		"seen/new",
		"Memory format",
		"Report format",
		"Empty-run behavior",
		// Schedule routed through the validated command.
		"pop routine edit gate --schedule",
	} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("front-loaded prompt missing %q:\n%s", want, prompt)
		}
	}

	// Agent exit returns to the gate menu, which then exits on "0".
	text := out.String()
	if !strings.Contains(text, "Authoring session ended") {
		t.Fatalf("expected return-to-menu line:\n%s", text)
	}
	if strings.Count(text, "Choose [1]: ") < 2 {
		t.Fatalf("gate should re-render after the session:\n%s", text)
	}
}

func TestRefineAgentSessionSpawnFailureLoopsBack(t *testing.T) {
	root := t.TempDir()
	dataHome := filepath.Join(root, "data")
	var out bytes.Buffer
	d := refineDeps(t, dataHome, "1\n0\n", &out)
	d.Tasks.Runner = &fakeAttendedRunner{err: io.ErrUnexpectedEOF}
	addRoutineForGate(t, d, "gate", filepath.Join(root, "home"))

	if err := RefineWith(d, "gate", ""); err != nil {
		t.Fatalf("spawn failure must not crash the gate: %v", err)
	}
	text := out.String()
	if !strings.Contains(text, "Could not start the authoring session") {
		t.Fatalf("expected spawn failure report:\n%s", text)
	}
	if strings.Count(text, "Choose [1]: ") < 2 {
		t.Fatalf("gate should loop back after a spawn failure:\n%s", text)
	}
}

func TestRefineAgentSessionNonZeroExitReturnsToMenu(t *testing.T) {
	root := t.TempDir()
	dataHome := filepath.Join(root, "data")
	var out bytes.Buffer
	d := refineDeps(t, dataHome, "1\n0\n", &out)
	d.Tasks.Runner = &fakeAttendedRunner{exitCode: 3}
	addRoutineForGate(t, d, "gate", filepath.Join(root, "home"))

	if err := RefineWith(d, "gate", ""); err != nil {
		t.Fatal(err)
	}
	text := out.String()
	if !strings.Contains(text, "exited with status 3") {
		t.Fatalf("expected non-zero exit notice:\n%s", text)
	}
	if !strings.Contains(text, "Authoring session ended") {
		t.Fatalf("gate should still return to the menu:\n%s", text)
	}
}

func TestRefineAgentOverrideRejectsUnknownPreset(t *testing.T) {
	root := t.TempDir()
	dataHome := filepath.Join(root, "data")
	var out bytes.Buffer
	d := refineDeps(t, dataHome, "0\n", &out)
	addRoutineForGate(t, d, "gate", filepath.Join(root, "home"))

	err := RefineWith(d, "gate", "bogus")
	if err == nil {
		t.Fatal("expected an unknown --agent preset to be rejected")
	}
	if !strings.Contains(err.Error(), "bogus") {
		t.Fatalf("error should name the bad preset, got %v", err)
	}
}

func TestRefineAgentOverrideUsesPreset(t *testing.T) {
	root := t.TempDir()
	dataHome := filepath.Join(root, "data")
	var out bytes.Buffer
	d := refineDeps(t, dataHome, "1\n0\n", &out)
	runner := &fakeAttendedRunner{}
	d.Tasks.Runner = runner
	addRoutineForGate(t, d, "gate", filepath.Join(root, "home"))

	if err := RefineWith(d, "gate", "codex"); err != nil {
		t.Fatal(err)
	}
	if runner.name != "codex" {
		t.Fatalf("override should select codex, got %q", runner.name)
	}
}
