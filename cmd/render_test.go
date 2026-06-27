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
	want := injectOwnershipMarker(injectFrontmatterName(string(src), "pop-pane"))
	if string(got) != want {
		t.Fatalf("rendered bytes mismatch:\n got: %q\nwant: %q", string(got), want)
	}

	// Sanity: the injected name is present and the directory matches it.
	if !strings.Contains(string(got), "\nname: pop-pane\n") {
		t.Fatalf("rendered SKILL.md missing injected name: %q", string(got))
	}
}

// TestRenderPaneSkillSkillDirAgents pins the pane skill's rendered tree for the
// agents that host skills as directories (pi, cursor) — identical layout to
// claude: a single `pop-pane/SKILL.md` entry with the frontmatter name injected.
func TestRenderPaneSkillSkillDirAgents(t *testing.T) {
	src, err := skillFiles.ReadFile("skills/pop/pane.md")
	if err != nil {
		t.Fatalf("read embedded source: %v", err)
	}
	want := injectOwnershipMarker(injectFrontmatterName(string(src), "pop-pane"))

	for _, agent := range []string{"pi", "cursor"} {
		t.Run(agent, func(t *testing.T) {
			tree, err := renderComponent(ComponentPaneSkill, agent)
			if err != nil {
				t.Fatalf("renderComponent(%s): %v", agent, err)
			}
			if len(tree) != 1 {
				t.Fatalf("expected 1 entry, got %d: %v", len(tree), keysOf(tree))
			}
			got, ok := tree["pop-pane/SKILL.md"]
			if !ok {
				t.Fatalf("missing pop-pane/SKILL.md; tree has %v", keysOf(tree))
			}
			if string(got) != want {
				t.Fatalf("rendered bytes mismatch:\n got: %q\nwant: %q", string(got), want)
			}
			if !strings.Contains(string(got), "\nname: pop-pane\n") {
				t.Fatalf("rendered SKILL.md missing injected name: %q", string(got))
			}
		})
	}
}

// TestRenderPaneSkillOpencode pins the pane skill's rendered tree for opencode:
// a single flat `pop-pane.md` entry whose bytes are the embedded source with no
// name injected (opencode has no skill-directory layout; the file name carries
// the identity) but with the name-independent pop-owned marker added.
func TestRenderPaneSkillOpencode(t *testing.T) {
	tree, err := renderComponent(ComponentPaneSkill, "opencode")
	if err != nil {
		t.Fatalf("renderComponent(opencode): %v", err)
	}
	if len(tree) != 1 {
		t.Fatalf("expected 1 entry, got %d: %v", len(tree), keysOf(tree))
	}
	got, ok := tree["pop-pane.md"]
	if !ok {
		t.Fatalf("missing pop-pane.md; tree has %v", keysOf(tree))
	}
	src, err := skillFiles.ReadFile("skills/pop/pane.md")
	if err != nil {
		t.Fatalf("read embedded source: %v", err)
	}
	// opencode injects no name, but every rendered skill carries the
	// name-independent pop-owned marker (skill-prefix slice 02).
	want := injectOwnershipMarker(string(src))
	if string(got) != want {
		t.Fatalf("opencode render mismatch:\n got: %q\nwant: %q", string(got), want)
	}
	if !frontmatterHasOwnershipMarker(string(got)) {
		t.Fatalf("opencode render missing pop-owned marker: %q", string(got))
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
	if _, err := renderComponent(ComponentPaneSkill, "codex"); err == nil {
		t.Fatalf("expected error rendering pane skill for codex")
	}
	// Task skills remain unsupported for opencode even though the pane
	// skill is now supported there.
	if _, err := renderComponent(ComponentTaskSkills, "opencode"); err == nil {
		t.Fatalf("expected error rendering task skills for opencode")
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
