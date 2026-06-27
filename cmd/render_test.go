package cmd

import (
	"strings"
	"testing"
)

// TestRenderPaneSkillClaude pins the pane skill's rendered tree for claude: a
// single skill-directory entry whose bytes are the embedded source with the
// frontmatter name injected to match the directory.
func TestRenderPaneSkillClaude(t *testing.T) {
	tree, err := renderComponent(ComponentPaneSkill, "claude", "pop-")
	if err != nil {
		t.Fatalf("renderComponent: %v", err)
	}

	if len(tree) != 1 {
		t.Fatalf("expected 1 entry, got %d: %v", len(tree), keysOf(tree))
	}
	got, ok := tree["pop-tmux-pane/SKILL.md"]
	if !ok {
		t.Fatalf("missing pop-tmux-pane/SKILL.md; tree has %v", keysOf(tree))
	}

	src, err := skillFiles.ReadFile("skills/pop/tmux-pane.md")
	if err != nil {
		t.Fatalf("read embedded source: %v", err)
	}
	want := injectOwnershipMarker(injectFrontmatterName(string(src), "pop-tmux-pane"))
	if string(got) != want {
		t.Fatalf("rendered bytes mismatch:\n got: %q\nwant: %q", string(got), want)
	}

	// Sanity: the injected name is present and the directory matches it.
	if !strings.Contains(string(got), "\nname: pop-tmux-pane\n") {
		t.Fatalf("rendered SKILL.md missing injected name: %q", string(got))
	}
	if !strings.Contains(string(got), "\npop-owned: true\n") {
		t.Fatalf("rendered SKILL.md missing ownership marker: %q", string(got))
	}
}

// TestRenderPaneSkillSkillDirAgents pins the pane skill's rendered tree for the
// agents that host skills as directories (codex, pi, cursor) — identical layout
// to claude: a single `pop-tmux-pane/SKILL.md` entry with the frontmatter name injected.
func TestRenderPaneSkillSkillDirAgents(t *testing.T) {
	src, err := skillFiles.ReadFile("skills/pop/tmux-pane.md")
	if err != nil {
		t.Fatalf("read embedded source: %v", err)
	}
	want := injectOwnershipMarker(injectFrontmatterName(string(src), "pop-tmux-pane"))

	for _, agent := range []string{"codex", "pi", "cursor"} {
		t.Run(agent, func(t *testing.T) {
			tree, err := renderComponent(ComponentPaneSkill, agent, "pop-")
			if err != nil {
				t.Fatalf("renderComponent(%s): %v", agent, err)
			}
			if len(tree) != 1 {
				t.Fatalf("expected 1 entry, got %d: %v", len(tree), keysOf(tree))
			}
			got, ok := tree["pop-tmux-pane/SKILL.md"]
			if !ok {
				t.Fatalf("missing pop-tmux-pane/SKILL.md; tree has %v", keysOf(tree))
			}
			if string(got) != want {
				t.Fatalf("rendered bytes mismatch:\n got: %q\nwant: %q", string(got), want)
			}
			if !strings.Contains(string(got), "\nname: pop-tmux-pane\n") {
				t.Fatalf("rendered SKILL.md missing injected name: %q", string(got))
			}
		})
	}
}

// TestRenderPaneSkillOpencode pins the pane skill's rendered tree for opencode:
// a single flat `pop-tmux-pane.md` entry whose bytes are the embedded source
// verbatim — opencode has no skill-directory layout and requires no name
// injection (the file name carries the identity).
func TestRenderPaneSkillOpencode(t *testing.T) {
	tree, err := renderComponent(ComponentPaneSkill, "opencode", "pop-")
	if err != nil {
		t.Fatalf("renderComponent(opencode): %v", err)
	}
	if len(tree) != 1 {
		t.Fatalf("expected 1 entry, got %d: %v", len(tree), keysOf(tree))
	}
	got, ok := tree["pop-tmux-pane.md"]
	if !ok {
		t.Fatalf("missing pop-tmux-pane.md; tree has %v", keysOf(tree))
	}
	src, err := skillFiles.ReadFile("skills/pop/tmux-pane.md")
	if err != nil {
		t.Fatalf("read embedded source: %v", err)
	}
	if string(got) != injectOwnershipMarker(string(src)) {
		t.Fatalf("opencode render should be source with ownership marker:\n got: %q\nwant: %q", string(got), injectOwnershipMarker(string(src)))
	}
}

// TestRenderCaseInsensitiveAgent confirms the agent name is normalized.
func TestRenderCaseInsensitiveAgent(t *testing.T) {
	tree, err := renderComponent(ComponentPaneSkill, "Claude", "pop-")
	if err != nil {
		t.Fatalf("renderComponent(Claude): %v", err)
	}
	if _, ok := tree["pop-tmux-pane/SKILL.md"]; !ok {
		t.Fatalf("expected pop-tmux-pane/SKILL.md, got %v", keysOf(tree))
	}
}

// TestRenderUnsupportedAgent confirms unsupported (agent, component) pairs error
// rather than producing a degraded tree.
func TestRenderUnsupportedAgent(t *testing.T) {
	if _, err := renderComponent(ComponentPaneSkill, "bogus", "pop-"); err == nil {
		t.Fatalf("expected error rendering pane skill for unknown agent")
	}
}

// TestRenderNonFileComponent confirms the status-wiring component has no
// file-based render.
func TestRenderNonFileComponent(t *testing.T) {
	if _, err := renderComponent(ComponentStatusWiring, "claude", "pop-"); err == nil {
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
