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

// embeddedSharedDoc returns one embedded companion of a skill directory.
func embeddedSharedDoc(t *testing.T, base, name string) string {
	t.Helper()
	body, err := skillFiles.ReadFile("skills/pop/" + base + "/" + name)
	if err != nil {
		t.Fatalf("read embedded %s/%s: %v", base, name, err)
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
		t.Error("grill-with-docs receives no document: it owns GRILL-SESSION.md and holds no format-document copy")
	}
	if owner := sharedSkillDocOwners["GRILL-SESSION.md"]; owner != "skills/pop/grill-with-docs" {
		t.Errorf("GRILL-SESSION.md owner = %q, want grill-with-docs", owner)
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
	// a commit of exactly this session's paths. All three live in the document
	// this skill owns rather than in its body (ADR-0253 decision 6), so the fast
	// sibling follows the same words instead of a second copy of them.
	session := embeddedSharedDoc(t, "grill-with-docs", "GRILL-SESSION.md")
	for _, want := range []string{
		"Write once a round, not per term",
		"skip rounds that settled nothing",
		"the same\nactivity",
		"never `git add -A`",
		"Stage exactly those paths",
		"Ask what happens next, and wait",
	} {
		if !strings.Contains(session, want) {
			t.Errorf("GRILL-SESSION.md lost the shared behaviour %q", want)
		}
		if strings.Contains(body, want) {
			t.Errorf("grill-with-docs body keeps a second copy of %q", want)
		}
	}
	if !strings.Contains(body, "(./GRILL-SESSION.md)") {
		t.Error("grill-with-docs does not send the session to the document it owns")
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
			if !strings.HasPrefix(key, prefix+"grill-with-docs/") {
				continue
			}
			switch key {
			case prefix + "grill-with-docs/SKILL.md", prefix + "grill-with-docs/GRILL-SESSION.md":
			default:
				t.Errorf("grill-with-docs installs an unexpected companion %s", key)
			}
		}
	}
}

// TestGrillWithDocsFastIsTheDeltaOnly pins ADR-0253. The fast pass is its
// sibling's session conducted differently, so its body may carry only what
// differs — the marked override, the two-part ask test, the round budget, the
// ledger — and must reach every shared rule through the document it receives
// rather than a copy of its own.
func TestGrillWithDocsFastIsTheDeltaOnly(t *testing.T) {
	t.Parallel()
	body := embeddedSkillBody(t, "grill-with-docs-fast")

	if !strings.Contains(frontmatterOf(t, body), "disable-model-invocation: true") {
		t.Error("grill-with-docs-fast commits to the repository and must stay human-opened")
	}
	if _, pinned := overlayPinnedFiles["skills/pop/grill-with-docs-fast/SKILL.md"]; pinned {
		t.Error("a composer inlines no upstream region, so it has no drift pin")
	}
	if got := sharedSkillDocs["grill-with-docs-fast"]; len(got) != 1 || got[0] != "GRILL-SESSION.md" {
		t.Errorf("grill-with-docs-fast receives %v, want just GRILL-SESSION.md", got)
	}

	// It composes rather than inlines, exactly as its sibling does.
	for _, inlined := range []string{"### Update CONTEXT.md inline", "### Offer ADRs sparingly", "POP OVERLAY"} {
		if strings.Contains(body, inlined) {
			t.Errorf("grill-with-docs-fast inlines %q instead of loading the skill that owns it", inlined)
		}
	}
	// The shared rules reach it as a document, never as a second copy.
	for _, copied := range []string{
		"Write once a round, not per term",
		"never `git add -A`",
		"Stage exactly those paths",
	} {
		if strings.Contains(body, copied) {
			t.Errorf("grill-with-docs-fast carries its own copy of the shared rule %q", copied)
		}
	}
	if !strings.Contains(body, "(./GRILL-SESSION.md)") {
		t.Error("grill-with-docs-fast does not reach the shared rules it receives")
	}

	// Its own delta, including the marked override of the primitive it loads.
	for _, want := range []string{
		"Override (negates",
		"Reversibility is deliberately **not** a criterion",
		"never a refusal",
		"Decided without asking:",
		// ADR-0257 decisions 1-3: the negation is bounded, beside the override
		// it bounds, and its sibling needs no copy of the bound.
		"never whether the work starts",
		"not the\nlicence to act on it",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("grill-with-docs-fast lost its own behaviour %q", want)
		}
	}

	for _, prefix := range []string{"pop-", "", "x-"} {
		tree := renderedTaskSkills(t, "claude", prefix)
		name := prefix + "grill-with-docs-fast"
		rendered := string(tree[name+"/SKILL.md"])
		for _, base := range []string{"grilling", "domain-modeling"} {
			if !strings.Contains(rendered, "the `"+prefix+base+"` skill") {
				t.Errorf("at prefix %q grill-with-docs-fast does not load the resolved `%s` skill", prefix, prefix+base)
			}
		}
		// The installed copy is the owner's bytes, so the two composers cannot
		// drift apart on a machine.
		got, ok := tree[name+"/GRILL-SESSION.md"]
		if !ok {
			t.Fatalf("%s is missing GRILL-SESSION.md", name)
		}
		if owner := tree[prefix+"grill-with-docs/GRILL-SESSION.md"]; string(got) != string(owner) {
			t.Errorf("%s's GRILL-SESSION.md is not the copy grill-with-docs owns", name)
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
