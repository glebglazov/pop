package integrate

import (
	"strings"
	"testing"
)

// TestRenderPaneSkillClaude pins the pane skills' rendered tree for claude: two
// skill-directory entries whose bytes are the embedded sources with the
// frontmatter name injected to match each directory.
func TestRenderPaneSkillClaude(t *testing.T) {
	t.Parallel()
	tree, err := renderComponent(ComponentPaneSkill, "claude", "pop-")
	if err != nil {
		t.Fatalf("renderComponent: %v", err)
	}

	if len(tree) != paneSkillCount {
		t.Fatalf("expected %d entries, got %d: %v", paneSkillCount, len(tree), keysOf(tree))
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

	spawnGot, ok := tree["pop-spawn-agent/SKILL.md"]
	if !ok {
		t.Fatalf("missing pop-spawn-agent/SKILL.md; tree has %v", keysOf(tree))
	}
	spawnSrc, err := skillFiles.ReadFile("skills/pop/spawn-agent.md")
	if err != nil {
		t.Fatalf("read embedded spawn-agent source: %v", err)
	}
	spawnContent := rewriteSkillReferences(string(spawnSrc), "pop-", fileBasedSkillBaseNames())
	_, spawnWant, err := renderSkillFile("claude", "pop-spawn-agent", spawnContent)
	if err != nil {
		t.Fatalf("renderSkillFile(spawn-agent): %v", err)
	}
	if string(spawnGot) != spawnWant {
		t.Fatalf("spawn-agent render bytes mismatch:\n got: %q\nwant: %q", string(spawnGot), spawnWant)
	}
}

// TestRenderPaneSkillSkillDirAgents pins the pane skills' rendered tree for the
// agents that host skills as directories (codex, pi, cursor) — identical layout
// to claude: `pop-tmux-pane/SKILL.md` and `pop-spawn-agent/SKILL.md` with the
// frontmatter name injected.
func TestRenderPaneSkillSkillDirAgents(t *testing.T) {
	t.Parallel()
	src, err := skillFiles.ReadFile("skills/pop/tmux-pane.md")
	if err != nil {
		t.Fatalf("read embedded source: %v", err)
	}
	want := injectOwnershipMarker(injectFrontmatterName(string(src), "pop-tmux-pane"))
	spawnSrc, err := skillFiles.ReadFile("skills/pop/spawn-agent.md")
	if err != nil {
		t.Fatalf("read embedded spawn-agent source: %v", err)
	}
	spawnContent := rewriteSkillReferences(string(spawnSrc), "pop-", fileBasedSkillBaseNames())

	for _, agent := range []string{"codex", "pi", "cursor"} {
		t.Run(agent, func(t *testing.T) {
			_, spawnWant, err := renderSkillFile(agent, "pop-spawn-agent", spawnContent)
			if err != nil {
				t.Fatalf("renderSkillFile(spawn-agent): %v", err)
			}
			tree, err := renderComponent(ComponentPaneSkill, agent, "pop-")
			if err != nil {
				t.Fatalf("renderComponent(%s): %v", agent, err)
			}
			if len(tree) != paneSkillCount {
				t.Fatalf("expected %d entries, got %d: %v", paneSkillCount, len(tree), keysOf(tree))
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
			spawnGot, ok := tree["pop-spawn-agent/SKILL.md"]
			if !ok {
				t.Fatalf("missing pop-spawn-agent/SKILL.md; tree has %v", keysOf(tree))
			}
			if string(spawnGot) != spawnWant {
				t.Fatalf("spawn-agent render bytes mismatch:\n got: %q\nwant: %q", string(spawnGot), spawnWant)
			}
		})
	}
}

// TestRenderPaneSkillOpencode pins the pane skills' rendered tree for opencode:
// two flat entries (`pop-tmux-pane.md`, `pop-spawn-agent.md`) whose bytes are
// the embedded sources verbatim — opencode has no skill-directory layout and
// requires no name injection (the file name carries the identity).
func TestRenderPaneSkillOpencode(t *testing.T) {
	t.Parallel()
	tree, err := renderComponent(ComponentPaneSkill, "opencode", "pop-")
	if err != nil {
		t.Fatalf("renderComponent(opencode): %v", err)
	}
	if len(tree) != paneSkillCount {
		t.Fatalf("expected %d entries, got %d: %v", paneSkillCount, len(tree), keysOf(tree))
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
	spawnGot, ok := tree["pop-spawn-agent.md"]
	if !ok {
		t.Fatalf("missing pop-spawn-agent.md; tree has %v", keysOf(tree))
	}
	spawnSrc, err := skillFiles.ReadFile("skills/pop/spawn-agent.md")
	if err != nil {
		t.Fatalf("read embedded spawn-agent source: %v", err)
	}
	spawnContent := rewriteSkillReferences(string(spawnSrc), "pop-", fileBasedSkillBaseNames())
	_, spawnWant, err := renderSkillFile("opencode", "pop-spawn-agent", spawnContent)
	if err != nil {
		t.Fatalf("renderSkillFile(spawn-agent): %v", err)
	}
	if string(spawnGot) != spawnWant {
		t.Fatalf("opencode spawn-agent render mismatch:\n got: %q\nwant: %q", string(spawnGot), spawnWant)
	}
}

// TestRenderCaseInsensitiveAgent confirms the agent name is normalized.
func TestRenderCaseInsensitiveAgent(t *testing.T) {
	t.Parallel()
	tree, err := renderComponent(ComponentPaneSkill, "Claude", "pop-")
	if err != nil {
		t.Fatalf("renderComponent(Claude): %v", err)
	}
	if _, ok := tree["pop-tmux-pane/SKILL.md"]; !ok {
		t.Fatalf("expected pop-tmux-pane/SKILL.md, got %v", keysOf(tree))
	}
	if _, ok := tree["pop-spawn-agent/SKILL.md"]; !ok {
		t.Fatalf("expected pop-spawn-agent/SKILL.md, got %v", keysOf(tree))
	}
}

// TestRenderUnsupportedAgent confirms unsupported (agent, component) pairs error
// rather than producing a degraded tree.
func TestRenderUnsupportedAgent(t *testing.T) {
	t.Parallel()
	if _, err := renderComponent(ComponentPaneSkill, "bogus", "pop-"); err == nil {
		t.Fatalf("expected error rendering pane skill for unknown agent")
	}
}

// TestRenderNonFileComponent confirms the status-wiring component has no
// file-based render.
func TestRenderNonFileComponent(t *testing.T) {
	t.Parallel()
	if _, err := renderComponent(ComponentStatusWiring, "claude", "pop-"); err == nil {
		t.Fatalf("expected error: status-wiring has no file-based render")
	}
}

// TestRenderWayfinderInvocationFollowsPrefix pins that the wayfinder skill body
// names its own slash invocation through the resolved skills_prefix. A hardcoded
// `/pop-wayfinder` in the source would hand a bare-prefix user a command that
// does not exist on their machine.
func TestRenderWayfinderInvocationFollowsPrefix(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		prefix string
		want   string
	}{
		{prefix: "pop-", want: "`/pop-wayfinder`"},
		{prefix: "", want: "`/wayfinder`"},
	} {
		tree, err := renderComponent(ComponentTaskSkills, "claude", tc.prefix)
		if err != nil {
			t.Fatalf("renderComponent(prefix=%q): %v", tc.prefix, err)
		}
		body := string(tree[tc.prefix+"wayfinder/SKILL.md"])
		if body == "" {
			t.Fatalf("prefix=%q: no wayfinder SKILL.md in %v", tc.prefix, keysOf(tree))
		}
		if !strings.Contains(body, "- **Invocation form.** "+tc.want) {
			t.Fatalf("prefix=%q: invocation form does not name %s", tc.prefix, tc.want)
		}
	}
}

func keysOf(m map[string][]byte) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

// TestRewriteSkillReferencesWordBoundary pins word-boundary safety and
// idempotency: hyphenated names match whole identifiers only and re-render
// does not double-prefix.
func TestRewriteSkillReferencesWordBoundary(t *testing.T) {
	t.Parallel()
	baseNames := fileBasedSkillBaseNames()
	const prefix = "pop-"

	cases := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "bare backtick name",
			input: "run the `grill-with-docs` skill",
			want:  "run the `pop-grill-with-docs` skill",
		},
		{
			name:  "already prefixed is unchanged",
			input: "run the `pop-grill-with-docs` skill",
			want:  "run the `pop-grill-with-docs` skill",
		},
		{
			name:  "sentence-initial invocation of a common-word name",
			input: "Run the `grilling` skill for the interview.",
			want:  "Run the `pop-grilling` skill for the interview.",
		},
		{
			name:  "common-word name outside an invocation is left alone",
			input: "a grilling ticket resolves in conversation",
			want:  "a grilling ticket resolves in conversation",
		},
		{
			name:  "hyphenated partial word not matched",
			input: "not-pop-to-spec-here",
			want:  "not-pop-to-spec-here",
		},
		{
			name:  "to-spec standalone",
			input: "suggest to-spec and to-tasks",
			want:  "suggest pop-to-spec and pop-to-tasks",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := rewriteSkillReferences(tc.input, prefix, baseNames)
			if got != tc.want {
				t.Fatalf("rewriteSkillReferences() = %q, want %q", got, tc.want)
			}
			// Idempotent on second pass.
			if again := rewriteSkillReferences(got, prefix, baseNames); again != tc.want {
				t.Fatalf("second rewrite = %q, want %q", again, tc.want)
			}
		})
	}
}
