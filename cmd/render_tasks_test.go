package cmd

import (
	"strings"
	"testing"
)

// taskSkillDirs is the set of skill names the task-skills component
// renders, with the companion files each one is expected to carry alongside its
// SKILL.md body.
var taskSkillDirs = map[string][]string{
	"pop-grill-with-docs":   {"ADR-FORMAT.md", "CONTEXT-FORMAT.md"},
	"pop-grill-consolidate": {},
	"pop-to-prd":            {},
	"pop-to-tasks":          {},
}

// TestRenderTaskSkillsDirAgents pins the task-skills rendered tree for
// each agent that hosts skills as directories (claude, pi, cursor): four skill
// directories, each with a name-injected SKILL.md, and grill-with-docs carrying
// its two companion format documents verbatim alongside the body.
func TestRenderTaskSkillsDirAgents(t *testing.T) {
	for _, agent := range []string{"claude", "pi", "cursor"} {
		t.Run(agent, func(t *testing.T) {
			tree, err := renderComponent(ComponentTaskSkills, agent)
			if err != nil {
				t.Fatalf("renderComponent(%s): %v", agent, err)
			}

			// Body files: one SKILL.md per skill, frontmatter name injected to
			// match the directory.
			for skill := range taskSkillDirs {
				key := skill + "/SKILL.md"
				body, ok := tree[key]
				if !ok {
					t.Fatalf("missing %s; tree has %v", key, keysOf(tree))
				}
				if !strings.Contains(string(body), "\nname: "+skill+"\n") {
					t.Fatalf("%s missing injected name %q: %q", key, skill, string(body))
				}
			}

			// Companion files: ride alongside the body, byte-for-byte from the
			// embedded source.
			for skill, companions := range taskSkillDirs {
				for _, c := range companions {
					key := skill + "/" + c
					got, ok := tree[key]
					if !ok {
						t.Fatalf("missing companion %s; tree has %v", key, keysOf(tree))
					}
					srcPath := "skills/pop/" + strings.TrimPrefix(skill, "pop-") + "/" + c
					want, err := skillFiles.ReadFile(srcPath)
					if err != nil {
						t.Fatalf("read embedded companion %s: %v", srcPath, err)
					}
					if string(got) != string(want) {
						t.Fatalf("companion %s should be verbatim source:\n got: %q\nwant: %q", key, string(got), string(want))
					}
				}
			}

			// Exactly the expected entry count: one SKILL.md per skill plus the
			// companion files, nothing extra.
			wantCount := 0
			for _, companions := range taskSkillDirs {
				wantCount += 1 + len(companions)
			}
			if len(tree) != wantCount {
				t.Fatalf("expected %d entries, got %d: %v", wantCount, len(tree), keysOf(tree))
			}
		})
	}
}

// TestRenderTaskSkillsBodyMatchesInjectedSource confirms the grill-with-docs
// body is the embedded source with the name injected — the body is not emitted
// verbatim like a companion file.
func TestRenderTaskSkillsBodyMatchesInjectedSource(t *testing.T) {
	tree, err := renderComponent(ComponentTaskSkills, "claude")
	if err != nil {
		t.Fatalf("renderComponent: %v", err)
	}
	src, err := skillFiles.ReadFile("skills/pop/grill-with-docs/SKILL.md")
	if err != nil {
		t.Fatalf("read embedded source: %v", err)
	}
	want := injectOwnershipMarker(injectFrontmatterName(string(src), "pop-grill-with-docs"))
	got := tree["pop-grill-with-docs/SKILL.md"]
	if string(got) != want {
		t.Fatalf("grill-with-docs body mismatch:\n got: %q\nwant: %q", string(got), want)
	}
}

// TestRenderTaskSkillsContentUsesShowPath confirms the rendered planning
// skills no longer assume an in-tree thoughts/ location and instead resolve
// their write location via `pop tasks show-path` (ADR 0039, ADR 0013).
func TestRenderTaskSkillsContentUsesShowPath(t *testing.T) {
	tree, err := renderComponent(ComponentTaskSkills, "claude")
	if err != nil {
		t.Fatalf("renderComponent: %v", err)
	}

	for _, skill := range []string{"pop-to-prd", "pop-to-tasks"} {
		body := string(tree[skill+"/SKILL.md"])
		if strings.Contains(body, "thoughts/") {
			t.Errorf("%s SKILL.md still mentions thoughts/: %q", skill, body)
		}
		if !strings.Contains(body, "pop tasks show-path") {
			t.Errorf("%s SKILL.md does not route through `pop tasks show-path`", skill)
		}
		// No leftover issue/workload vocabulary.
		if strings.Contains(body, "workload") || strings.Contains(body, "to-issues") {
			t.Errorf("%s SKILL.md still mentions issue/workload vocabulary: %q", skill, body)
		}
	}
}

// TestRenderTaskSkillsUnsupportedAgents confirms opencode and codex error
// rather than producing a degraded (flat, companion-less) tree.
func TestRenderTaskSkillsUnsupportedAgents(t *testing.T) {
	for _, agent := range []string{"opencode", "codex"} {
		if _, err := renderComponent(ComponentTaskSkills, agent); err == nil {
			t.Fatalf("expected error rendering task skills for %s", agent)
		}
	}
}
