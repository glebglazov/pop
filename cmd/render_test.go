package cmd

import (
	"strings"
	"testing"
)

// TestRenderPaneSkillClaude pins the pane skill's rendered tree for claude: a
// single skill-directory entry whose bytes are the embedded source with the
// frontmatter name injected to match the directory.
func TestRenderPaneSkillClaude(t *testing.T) {
	tree, err := renderComponent(ComponentPaneSkill, "claude")
	if err != nil {
		t.Fatalf("renderComponent: %v", err)
	}

	if len(tree) != 1 {
		t.Fatalf("expected 1 entry, got %d: %v", len(tree), keysOf(tree))
	}
	got, ok := tree["pop-pane/SKILL.md"]
	if !ok {
		t.Fatalf("missing pop-pane/SKILL.md; tree has %v", keysOf(tree))
	}

	src, err := skillFiles.ReadFile("skills/pop/pane.md")
	if err != nil {
		t.Fatalf("read embedded source: %v", err)
	}
	want := injectFrontmatterName(string(src), "pop-pane")
	if string(got) != want {
		t.Fatalf("rendered bytes mismatch:\n got: %q\nwant: %q", string(got), want)
	}

	// Sanity: the injected name is present and the directory matches it.
	if !strings.Contains(string(got), "\nname: pop-pane\n") {
		t.Fatalf("rendered SKILL.md missing injected name: %q", string(got))
	}
}

// TestRenderCaseInsensitiveAgent confirms the agent name is normalized.
func TestRenderCaseInsensitiveAgent(t *testing.T) {
	tree, err := renderComponent(ComponentPaneSkill, "Claude")
	if err != nil {
		t.Fatalf("renderComponent(Claude): %v", err)
	}
	if _, ok := tree["pop-pane/SKILL.md"]; !ok {
		t.Fatalf("expected pop-pane/SKILL.md, got %v", keysOf(tree))
	}
}

// TestRenderUnsupportedAgent confirms unsupported (agent, component) pairs error
// rather than producing a degraded tree.
func TestRenderUnsupportedAgent(t *testing.T) {
	for _, agent := range []string{"opencode", "codex"} {
		if _, err := renderComponent(ComponentPaneSkill, agent); err == nil {
			t.Fatalf("expected error rendering pane skill for %q", agent)
		}
	}
}

// TestRenderNonFileComponent confirms the status-wiring component has no
// file-based render.
func TestRenderNonFileComponent(t *testing.T) {
	if _, err := renderComponent(ComponentStatusWiring, "claude"); err == nil {
		t.Fatalf("expected error: status-wiring has no file-based render")
	}
}

func keysOf(m map[string][]byte) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
