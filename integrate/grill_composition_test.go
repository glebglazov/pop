package integrate

import (
	"path/filepath"
	"strings"
	"testing"
)

// embeddedSkillBody returns one embedded skill body by base name.
func embeddedSkillBody(t *testing.T, base string) string {
	t.Helper()
	body, err := skillFiles.ReadFile("skills/pop/" + base + "/SKILL.md")
	if err != nil {
		t.Fatalf("read embedded %s skill: %v", base, err)
	}
	return string(body)
}

// renderedTaskSkills renders the task-skills component for one agent at one
// prefix, keyed by rendered path.
func renderedTaskSkills(t *testing.T, agent, prefix string) map[string][]byte {
	t.Helper()
	tree, err := renderComponent(ComponentTaskSkills, agent, prefix)
	if err != nil {
		t.Fatalf("renderComponent(%s, %q): %v", agent, prefix, err)
	}
	return tree
}

// TestGrillingIsInterviewOnly pins ADR-0225 decision 1. The interview primitive
// carries no pop rule and no format companion: every glossary sentence pop used
// to keep here now belongs to the skill that owns the destination, and a rule
// that is absent cannot be disobeyed by a session composing this one for the
// interview alone. TestVerbatimPinMatchesUpstream guards the body's bytes; this
// guards what pop is allowed to put around them.
func TestGrillingIsInterviewOnly(t *testing.T) {
	t.Parallel()
	body := embeddedSkillBody(t, "grilling")

	if strings.Contains(frontmatterOf(t, body), "disable-model-invocation") {
		t.Error("grilling is agent-loaded and must carry no manual gate")
	}
	if _, ok := sharedSkillDocs["grilling"]; ok {
		t.Error("grilling must receive no shared format document")
	}
	// Prose below the provenance header is upstream's, so a pop glossary rule
	// would show up as one of these.
	for _, gone := range []string{"CONTEXT-FORMAT.md", ".grill-context", "union of the base"} {
		if strings.Contains(belowHeaderRegion(t, body), gone) {
			t.Errorf("grilling body still carries the pop glossary rule %q", gone)
		}
	}

	// The interview itself survives, under every prefix and agent, with no
	// companion beside it.
	for _, prefix := range []string{"pop-", "", "x-"} {
		for _, agent := range Agents {
			name := prefix + "grilling"
			tree := renderedTaskSkills(t, agent, prefix)
			rendered, ok := tree[name+"/SKILL.md"]
			if !ok {
				t.Fatalf("%s: missing %s/SKILL.md; tree has %v", agent, name, keysOf(tree))
			}
			for _, want := range []string{"design tree", "frontier", "❓ **Q1**", "Finding _facts_ is your job"} {
				if !strings.Contains(string(rendered), want) {
					t.Errorf("%s at prefix %q lost upstream interview text %q", name, prefix, want)
				}
			}
			for key := range tree {
				if strings.HasPrefix(key, name+"/") && key != name+"/SKILL.md" {
					t.Errorf("%s installs an unexpected companion %s", name, key)
				}
			}
		}
	}
}

// TestGrillWithDocsComposesGrillingAndDomainModeling pins ADR-0225 decision 4:
// the session is a composer. It names both reusable skills at their resolved
// installed names — a bare name would ask a prefixed machine for a skill it does
// not have — inlines neither, and keeps the three behaviours that are its own.
func TestGrillWithDocsComposesGrillingAndDomainModeling(t *testing.T) {
	t.Parallel()
	body := embeddedSkillBody(t, "grill-with-docs")

	if !strings.Contains(frontmatterOf(t, body), "disable-model-invocation: true") {
		t.Error("grill-with-docs owns the commit and must stay human-opened")
	}
	if _, ok := sharedSkillDocs["grill-with-docs"]; ok {
		t.Error("grill-with-docs must own no copy of the format documents")
	}
	if _, pinned := overlayPinnedFiles["skills/pop/grill-with-docs/SKILL.md"]; pinned {
		t.Error("a composer inlines no upstream region, so it has no drift pin")
	}
	// Inlining would reproduce the discipline's own section headings.
	for _, inlined := range []string{"### Update CONTEXT.md inline", "### Offer ADRs sparingly", "POP OVERLAY"} {
		if strings.Contains(body, inlined) {
			t.Errorf("grill-with-docs inlines %q instead of loading the skill that owns it", inlined)
		}
	}
	// A link to a document it no longer installs would dangle.
	for _, dangling := range []string{"(./CONTEXT-FORMAT.md)", "(./ADR-FORMAT.md)"} {
		if strings.Contains(body, dangling) {
			t.Errorf("grill-with-docs links %q but installs no such companion", dangling)
		}
	}
	// Pop's own behaviour: the round-close beat, one fact-finding activity, and
	// a commit of exactly this session's paths.
	for _, want := range []string{
		"Write once a round, not per term",
		"skip rounds that settled nothing",
		"the same\nactivity",
		"never `git add -A`",
		"Stage exactly those paths",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("grill-with-docs lost its own behaviour %q", want)
		}
	}

	for _, prefix := range []string{"pop-", "", "x-"} {
		tree := renderedTaskSkills(t, "claude", prefix)
		rendered := string(tree[prefix+"grill-with-docs/SKILL.md"])
		for _, base := range []string{"grilling", "domain-modeling"} {
			if !strings.Contains(rendered, "the `"+prefix+base+"` skill") {
				t.Errorf("at prefix %q grill-with-docs does not load the resolved `%s` skill", prefix, prefix+base)
			}
		}
		for key := range tree {
			if strings.HasPrefix(key, prefix+"grill-with-docs/") && key != prefix+"grill-with-docs/SKILL.md" {
				t.Errorf("grill-with-docs installs an unexpected companion %s", key)
			}
		}
	}
}

// TestGrillWithMapKeepsMapOnlyDestination pins ADR-0225 decision 5: the
// wayfinding session composes the interview and nothing that writes into the
// repository. It restates the drafting rules itself and receives both canonical
// format documents from domain-modeling, so a Map draft stays compatible with the
// slice that later mints it, without loading the discipline that would commit.
func TestGrillWithMapKeepsMapOnlyDestination(t *testing.T) {
	t.Parallel()
	body := embeddedSkillBody(t, "grill-with-map")

	if got := sharedSkillDocs["grill-with-map"]; len(got) != 2 {
		t.Errorf("grill-with-map receives %v, want both format documents", got)
	}
	for _, load := range []string{"run the `domain-modeling` skill", "load the `domain-modeling` skill"} {
		if strings.Contains(strings.ToLower(body), load) {
			t.Errorf("grill-with-map must not load the repository-writing discipline: %q", load)
		}
	}
	for _, want := range []string{
		"never loads\n`domain-modeling`",
		"no `.grill-context/` fragment",
		"**no commit**",
		"the ops go into\nthe Map",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("grill-with-map lost its Map-only rule %q", want)
		}
	}

	// The installed copies are the owner's bytes, under every prefix.
	for _, prefix := range []string{"pop-", "", "x-"} {
		tree := renderedTaskSkills(t, "claude", prefix)
		for _, name := range []string{"ADR-FORMAT.md", "CONTEXT-FORMAT.md"} {
			got, ok := tree[prefix+"grill-with-map/"+name]
			if !ok {
				t.Fatalf("grill-with-map is missing %s at prefix %q", name, prefix)
			}
			if owner := tree[prefix+"domain-modeling/"+name]; string(got) != string(owner) {
				t.Errorf("grill-with-map's %s at prefix %q is not the copy domain-modeling owns", name, prefix)
			}
		}
	}
}

// TestInstallTaskSkillsClearsRetiredCompanions covers the upgrade this slice
// hands a machine that already has the old shape installed: the format documents
// grilling and grill-with-docs used to receive are still sitting in the render
// tree. The install rebuilds that tree from the render, so the retired copies go
// and no skill is left pointing at a document pop no longer gives it — while the
// receiver that still gets one keeps it.
func TestInstallTaskSkillsClearsRetiredCompanions(t *testing.T) {
	t.Parallel()
	for _, a := range taskAgents() {
		t.Run(a.name, func(t *testing.T) {
			fs := newFakeFS()
			d := fakeDeps(installerHome, fs, nil)

			retired := []string{
				filepath.Join(a.renderDir, "pop-grilling", "CONTEXT-FORMAT.md"),
				filepath.Join(a.renderDir, "pop-grill-with-docs", "CONTEXT-FORMAT.md"),
				filepath.Join(a.renderDir, "pop-grill-with-docs", "ADR-FORMAT.md"),
			}
			for _, p := range retired {
				fs.dirs[filepath.Dir(p)] = true
				fs.files[p] = []byte("copy from the pre-composition install")
			}

			if err := installFileComponent(fileRun(d, a.name), installerHome, ComponentTaskSkills, a.name); err != nil {
				t.Fatalf("installFileComponent(%s): %v", a.name, err)
			}

			for _, p := range retired {
				if _, ok := fs.files[p]; ok {
					t.Errorf("retired companion survived the install: %s", p)
				}
			}
			kept := filepath.Join(a.renderDir, "pop-grill-with-map", "CONTEXT-FORMAT.md")
			if _, ok := fs.files[kept]; !ok {
				t.Errorf("the remaining receiver lost its companion: %s", kept)
			}

			state, err := ComponentState(d, installerHome, ComponentTaskSkills, a.name)
			if err != nil {
				t.Fatalf("ComponentState: %v", err)
			}
			if state.Kind != StateInstalledCurrent {
				t.Fatalf("state after the upgrade install = %v, want installed-current", state.Kind)
			}
		})
	}
}
