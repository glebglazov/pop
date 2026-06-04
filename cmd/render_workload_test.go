package cmd

import (
	"strings"
	"testing"
)

// workloadSkillDirs is the set of skill names the workload-skills component
// renders, with the companion files each one is expected to carry alongside its
// SKILL.md body.
var workloadSkillDirs = map[string][]string{
	"pop-grill-with-docs": {"ADR-FORMAT.md", "CONTEXT-FORMAT.md"},
	"pop-to-prd":          {},
	"pop-to-issues":       {},
}

// TestRenderWorkloadSkillsDirAgents pins the workload-skills rendered tree for
// each agent that hosts skills as directories (claude, pi, cursor): three skill
// directories, each with a name-injected SKILL.md, and grill-with-docs carrying
// its two companion format documents verbatim alongside the body.
func TestRenderWorkloadSkillsDirAgents(t *testing.T) {
	for _, agent := range []string{"claude", "pi", "cursor"} {
		t.Run(agent, func(t *testing.T) {
			tree, err := renderComponent(ComponentWorkloadSkills, agent)
			if err != nil {
				t.Fatalf("renderComponent(%s): %v", agent, err)
			}

			// Body files: one SKILL.md per skill, frontmatter name injected to
			// match the directory.
			for skill := range workloadSkillDirs {
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
			for skill, companions := range workloadSkillDirs {
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
			for _, companions := range workloadSkillDirs {
				wantCount += 1 + len(companions)
			}
			if len(tree) != wantCount {
				t.Fatalf("expected %d entries, got %d: %v", wantCount, len(tree), keysOf(tree))
			}
		})
	}
}

// TestRenderWorkloadSkillsBodyMatchesInjectedSource confirms the grill-with-docs
// body is the embedded source with the name injected — the body is not emitted
// verbatim like a companion file.
func TestRenderWorkloadSkillsBodyMatchesInjectedSource(t *testing.T) {
	tree, err := renderComponent(ComponentWorkloadSkills, "claude")
	if err != nil {
		t.Fatalf("renderComponent: %v", err)
	}
	src, err := skillFiles.ReadFile("skills/pop/grill-with-docs/SKILL.md")
	if err != nil {
		t.Fatalf("read embedded source: %v", err)
	}
	want := injectFrontmatterName(string(src), "pop-grill-with-docs")
	got := tree["pop-grill-with-docs/SKILL.md"]
	if string(got) != want {
		t.Fatalf("grill-with-docs body mismatch:\n got: %q\nwant: %q", string(got), want)
	}
}

// TestRenderWorkloadSkillsUnsupportedAgents confirms opencode and codex error
// rather than producing a degraded (flat, companion-less) tree.
func TestRenderWorkloadSkillsUnsupportedAgents(t *testing.T) {
	for _, agent := range []string{"opencode", "codex"} {
		if _, err := renderComponent(ComponentWorkloadSkills, agent); err == nil {
			t.Fatalf("expected error rendering workload skills for %s", agent)
		}
	}
}
