package workload

import (
	"strings"
	"testing"
)

func TestResolveAgentCommandPresets(t *testing.T) {
	presets := []string{"claude", "opencode", "cursor", "codex", "pi"}
	for _, preset := range presets {
		name, args, err := ResolveAgentCommand(preset, "", "prompt text", "/tmp/runtime")
		if err != nil {
			t.Fatalf("%s: %v", preset, err)
		}
		if name == "" || len(args) == 0 {
			t.Fatalf("%s: empty command", preset)
		}
		last := args[len(args)-1]
		if last != "prompt text" {
			t.Fatalf("%s: last arg = %q", preset, last)
		}
	}
}

func TestResolveAgentCommandDefaultClaude(t *testing.T) {
	name, args, err := ResolveAgentCommand("", "", "p", "/tmp/runtime")
	if err != nil {
		t.Fatal(err)
	}
	if name != "claude" {
		t.Fatalf("name = %q", name)
	}
	if args[0] != "--dangerously-skip-permissions" && len(args) < 2 {
		t.Fatalf("args = %v", args)
	}
}

func TestResolveAgentCommandCustom(t *testing.T) {
	name, args, err := ResolveAgentCommand("", "fake-agent --verbose", "prompt", "/tmp/runtime")
	if err != nil {
		t.Fatal(err)
	}
	if name != "sh" {
		t.Fatalf("name = %q", name)
	}
	if !strings.Contains(args[1], "fake-agent") {
		t.Fatalf("args = %v", args)
	}
	if args[len(args)-1] != "prompt" {
		t.Fatalf("prompt arg = %q", args[len(args)-1])
	}
}

func TestResolveAgentCommandUnknownPreset(t *testing.T) {
	_, _, err := ResolveAgentCommand("unknown", "", "p", "/tmp/runtime")
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestBuildAgentPromptAbsolutePaths(t *testing.T) {
	prompt := BuildAgentPrompt("/abs/issues/01-a.md", "/abs/prds/feature.md", "/abs/runtime")
	for _, want := range []string{"/abs/issues/01-a.md", "/abs/prds/feature.md", "/abs/runtime", "index.json", "Do NOT make git commits"} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("missing %q in prompt:\n%s", want, prompt)
		}
	}
}
