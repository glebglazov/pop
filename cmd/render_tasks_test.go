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
	"pop-wayfinder":         {},
	"pop-prototype":         {"LOGIC.md", "UI.md"},
	"pop-research":          {},
}

// TestRenderTaskSkillsDirAgents pins the task-skills rendered tree for
// each agent that hosts skills as directories (claude, codex, pi, cursor,
// opencode): seven skill directories, each with a name-injected SKILL.md, and
// grill-with-docs and prototype carrying companion documents with cross-skill
// references rewritten alongside the body.
func TestRenderTaskSkillsDirAgents(t *testing.T) {
	baseNames := fileBasedSkillBaseNames()
	for _, agent := range []string{"claude", "codex", "pi", "cursor", "opencode"} {
		t.Run(agent, func(t *testing.T) {
			tree, err := renderComponent(ComponentTaskSkills, agent, "pop-")
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

			// Companion files: ride alongside the body with cross-skill refs rewritten.
			for skill, companions := range taskSkillDirs {
				for _, c := range companions {
					key := skill + "/" + c
					got, ok := tree[key]
					if !ok {
						t.Fatalf("missing companion %s; tree has %v", key, keysOf(tree))
					}
					srcPath := "skills/pop/" + strings.TrimPrefix(skill, "pop-") + "/" + c
					raw, err := skillFiles.ReadFile(srcPath)
					if err != nil {
						t.Fatalf("read embedded companion %s: %v", srcPath, err)
					}
					want := rewriteSkillReferences(string(raw), "pop-", baseNames)
					if string(got) != want {
						t.Fatalf("companion %s mismatch:\n got: %q\nwant: %q", key, string(got), want)
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
// body is the embedded source with cross-skill references rewritten and the
// name injected — the body is not emitted verbatim like a companion file.
func TestRenderTaskSkillsBodyMatchesInjectedSource(t *testing.T) {
	tree, err := renderComponent(ComponentTaskSkills, "claude", "pop-")
	if err != nil {
		t.Fatalf("renderComponent: %v", err)
	}
	src, err := skillFiles.ReadFile("skills/pop/grill-with-docs/SKILL.md")
	if err != nil {
		t.Fatalf("read embedded source: %v", err)
	}
	rewritten := rewriteSkillReferences(string(src), "pop-", fileBasedSkillBaseNames())
	want := injectOwnershipMarker(injectFrontmatterName(rewritten, "pop-grill-with-docs"))
	got := tree["pop-grill-with-docs/SKILL.md"]
	if string(got) != want {
		t.Fatalf("grill-with-docs body mismatch:\n got: %q\nwant: %q", string(got), want)
	}
}

// TestRenderTaskSkillsContentUsesShowPath confirms the rendered planning
// skills no longer assume an in-tree thoughts/ location and instead resolve
// their write location via `pop tasks show-path` (ADR 0039, ADR 0013).
func TestRenderTaskSkillsContentUsesShowPath(t *testing.T) {
	tree, err := renderComponent(ComponentTaskSkills, "claude", "pop-")
	if err != nil {
		t.Fatalf("renderComponent: %v", err)
	}

	for _, skill := range []string{"pop-to-prd", "pop-to-tasks", "pop-wayfinder"} {
		body := string(tree[skill+"/SKILL.md"])
		if strings.Contains(body, "thoughts/") {
			t.Errorf("%s SKILL.md still mentions thoughts/: %q", skill, body)
		}
		if !strings.Contains(body, "pop tasks show-path") && !strings.Contains(body, "pop work show-path") {
			t.Errorf("%s SKILL.md does not route through pop storage resolver", skill)
		}
		// No leftover issue/workload vocabulary.
		if strings.Contains(body, "workload") || strings.Contains(body, "to-issues") {
			t.Errorf("%s SKILL.md still mentions issue/workload vocabulary: %q", skill, body)
		}
	}
}

// TestRenderTaskSkillsBodyRewritesCrossSkillReferences pins that rendered
// bodies rewrite embedded base names to their resolved installed names.
func TestRenderTaskSkillsBodyRewritesCrossSkillReferences(t *testing.T) {
	tree, err := renderComponent(ComponentTaskSkills, "claude", "pop-")
	if err != nil {
		t.Fatalf("renderComponent: %v", err)
	}

	wayfinder := string(tree["pop-wayfinder/SKILL.md"])
	for _, want := range []string{"pop-grill-with-docs", "pop-to-prd", "pop-to-tasks"} {
		if !strings.Contains(wayfinder, want) {
			t.Errorf("wayfinder body missing rewritten reference %q", want)
		}
	}
	for _, bare := range []string{"`grill-with-docs`", "`to-prd`", "`to-tasks`"} {
		if strings.Contains(wayfinder, bare) {
			t.Errorf("wayfinder body still has bare reference %q", bare)
		}
	}

	grill := string(tree["pop-grill-with-docs/SKILL.md"])
	if !strings.Contains(grill, "pop-grill-consolidate") {
		t.Errorf("grill-with-docs body missing rewritten pop-grill-consolidate reference")
	}
	if strings.Contains(grill, "`grill-consolidate`") {
		t.Errorf("grill-with-docs body still has bare `grill-consolidate` reference")
	}
}

// TestRenderTaskSkillsBarePrefixNoBodyRewrite confirms an empty prefix leaves
// bodies byte-identical to embedded sources apart from frontmatter injection
// and the ownership marker.
func TestRenderTaskSkillsBarePrefixNoBodyRewrite(t *testing.T) {
	tree, err := renderComponent(ComponentTaskSkills, "claude", "")
	if err != nil {
		t.Fatalf("renderComponent: %v", err)
	}

	for skill, companions := range taskSkillDirs {
		base := strings.TrimPrefix(skill, "pop-")
		src, err := skillFiles.ReadFile("skills/pop/" + base + "/SKILL.md")
		if err != nil {
			t.Fatalf("read embedded source %s: %v", base, err)
		}
		want := injectOwnershipMarker(injectFrontmatterName(string(src), base))
		got := tree[base+"/SKILL.md"]
		if string(got) != want {
			t.Fatalf("%s/SKILL.md bare-prefix mismatch:\n got: %q\nwant: %q", base, string(got), want)
		}
		for _, c := range companions {
			raw, err := skillFiles.ReadFile("skills/pop/" + base + "/" + c)
			if err != nil {
				t.Fatalf("read companion %s/%s: %v", base, c, err)
			}
			got := tree[base+"/"+c]
			if string(got) != string(raw) {
				t.Fatalf("%s/%s bare-prefix companion mismatch:\n got: %q\nwant: %q", base, c, string(got), string(raw))
			}
		}
	}
}

// TestRenderTaskSkillsCustomPrefixRewritesReferences confirms a custom prefix
// is applied consistently to frontmatter names and body references.
func TestRenderTaskSkillsCustomPrefixRewritesReferences(t *testing.T) {
	const prefix = "x-"
	tree, err := renderComponent(ComponentTaskSkills, "claude", prefix)
	if err != nil {
		t.Fatalf("renderComponent: %v", err)
	}

	wayfinder := string(tree["x-wayfinder/SKILL.md"])
	if !strings.Contains(wayfinder, "\nname: x-wayfinder\n") {
		t.Fatalf("wayfinder missing injected x- name")
	}
	for _, want := range []string{"x-grill-with-docs", "x-to-prd", "x-to-tasks"} {
		if !strings.Contains(wayfinder, want) {
			t.Errorf("wayfinder body missing rewritten reference %q", want)
		}
	}

	grill := string(tree["x-grill-with-docs/SKILL.md"])
	if !strings.Contains(grill, "x-grill-consolidate") {
		t.Errorf("grill-with-docs body missing rewritten x-grill-consolidate reference")
	}
}
