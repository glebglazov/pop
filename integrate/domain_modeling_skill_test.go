package integrate

import (
	"path/filepath"
	"strings"
	"testing"
)

// domainModelingSource is the embedded skill directory the discipline now lives
// in. It is also the canonical home of the two format documents, so several
// tests below read from it rather than from a consumer's installed copy.
const domainModelingSource = "skills/pop/domain-modeling"

// domainModelingBody returns the embedded domain-modeling body.
func domainModelingBody(t *testing.T) string {
	t.Helper()
	body, err := skillFiles.ReadFile(domainModelingSource + "/SKILL.md")
	if err != nil {
		t.Fatalf("read embedded domain-modeling skill: %v", err)
	}
	return string(body)
}

// splitAtOverlayMarker returns a skill body's pinned-upstream region and its pop
// overlay. The provenance header names the marker in its own prose, so the split
// is searched for only after the header comment closes — the same rule
// aboveMarkerRegion applies for the drift diff.
func splitAtOverlayMarker(t *testing.T, body string) (base, overlay string) {
	t.Helper()
	headerClose := strings.Index(body, "-->")
	if headerClose < 0 {
		t.Fatal("domain-modeling body has no provenance header comment")
	}
	marker := strings.Index(body[headerClose:], "POP OVERLAY")
	if marker < 0 {
		t.Fatal("domain-modeling body has no POP OVERLAY marker")
	}
	marker += headerClose
	return body[:marker], body[marker:]
}

// frontmatterOf returns a skill body's frontmatter block.
func frontmatterOf(t *testing.T, body string) string {
	t.Helper()
	if !strings.HasPrefix(body, "---\n") {
		t.Fatal("skill body has no frontmatter")
	}
	end := strings.Index(body[4:], "\n---")
	if end < 0 {
		t.Fatal("skill body has unterminated frontmatter")
	}
	return body[:end+4]
}

// TestDomainModelingIsAgentLoaded pins ADR-0225 decision 2's classification: the
// discipline is a Tool skill any workflow may load and the model may reach on
// its own, so it carries no manual gate. A `disable-model-invocation` here would
// mean a composing workflow could not load it without a human opening it first.
func TestDomainModelingIsAgentLoaded(t *testing.T) {
	t.Parallel()
	front := frontmatterOf(t, domainModelingBody(t))
	if strings.Contains(front, "disable-model-invocation") {
		t.Errorf("domain-modeling is agent-loaded and must carry no disable-model-invocation flag: %q", front)
	}
	if !strings.Contains(front, "\nname: domain-modeling\n") {
		t.Errorf("domain-modeling body does not declare its own frontmatter name: %q", front)
	}
}

// TestDomainModelingOverlayCarriesPopBehaviour asserts the Pop half of the
// overlay is the whole of Pop's repository-fragment and clash-tolerant ADR
// behaviour, and that it sits below the marker rather than being edited into
// the pinned upstream region (TestOverlayBaseMatchesPin guards the other half).
// Used directly, the skill must know where a settled term goes, how the
// glossary is read back, which id an ADR takes, and that it does not commit.
func TestDomainModelingOverlayCarriesPopBehaviour(t *testing.T) {
	t.Parallel()
	base, overlay := splitAtOverlayMarker(t, domainModelingBody(t))

	for _, want := range []string{
		"never write\nthe base `CONTEXT.md`",
		"[CONTEXT-FORMAT.md](./CONTEXT-FORMAT.md)",
		"union of base + fragments",
		"grill-consolidate",
		"clash-tolerant",
		"[ADR-FORMAT.md](./ADR-FORMAT.md)",
		"does not commit",
	} {
		if !strings.Contains(overlay, want) {
			t.Errorf("domain-modeling overlay is missing pop behaviour %q", want)
		}
	}

	// The base region keeps upstream's inline-write instruction byte-intact so
	// drift stays diffable; the override that negates it is the overlay's.
	if !strings.Contains(base, "update `CONTEXT.md` right there") {
		t.Error("domain-modeling base region lost upstream's inline-write instruction")
	}
	if !strings.Contains(overlay, "Override (negates") {
		t.Error("domain-modeling overlay does not state the override of the base's inline-write instruction")
	}
}

// TestDomainModelingOwnsFormatCompanions pins ADR-0225 decision 3: the canonical
// CONTEXT-FORMAT.md and ADR-FORMAT.md are domain-modeling's own directory files,
// and every other consumer's installed copy is served from there. A second
// canonical copy anywhere is exactly the drift the single source prevents.
func TestDomainModelingOwnsFormatCompanions(t *testing.T) {
	t.Parallel()
	for _, name := range []string{"CONTEXT-FORMAT.md", "ADR-FORMAT.md"} {
		got, err := sharedDocSource(name)
		if err != nil {
			t.Errorf("sharedDocSource(%s): %v", name, err)
		} else if got != domainModelingSource+"/"+name {
			t.Errorf("sharedDocSource(%s) = %q, want it under %s", name, got, domainModelingSource)
		}
		if _, err := skillFiles.ReadFile(domainModelingSource + "/" + name); err != nil {
			t.Errorf("canonical %s does not live with domain-modeling: %v", name, err)
		}
		if _, err := skillFiles.ReadFile("skills/pop/_shared/" + name); err == nil {
			t.Errorf("%s still has a copy under the retired _shared/ owner", name)
		}
	}
	if _, ok := sharedSkillDocs["domain-modeling"]; ok {
		t.Error("the owner must not also be listed as a receiver of its own documents")
	}

	// Every receiver's rendered bytes are the owner's rendered bytes.
	tree, err := renderComponent(ComponentTaskSkills, "claude", "pop-")
	if err != nil {
		t.Fatalf("renderComponent: %v", err)
	}
	for base, docs := range sharedSkillDocs {
		for _, name := range docs {
			ownerBase := strings.TrimPrefix(sharedSkillDocOwners[name], "skills/pop/")
			owner := tree["pop-"+ownerBase+"/"+name]
			got := tree["pop-"+base+"/"+name]
			if len(owner) == 0 {
				t.Fatalf("owner %s did not render %s", ownerBase, name)
			}
			if string(got) != string(owner) {
				t.Errorf("pop-%s/%s differs from the canonical copy %s owns", base, name, ownerBase)
			}
		}
	}
}

// TestRenderDomainModelingResolvedNameEveryAgent covers ADR-0063's prefix
// contract for the new skill: under any resolved skills prefix, every supported
// agent gets `<prefix>domain-modeling` with the name injected to match, the
// pop-owned ownership marker, and both format companions beside the body.
func TestRenderDomainModelingResolvedNameEveryAgent(t *testing.T) {
	t.Parallel()
	for _, prefix := range []string{"pop-", "", "x-"} {
		t.Run("prefix="+prefix, func(t *testing.T) {
			t.Parallel()
			for _, agent := range Agents {
				name := prefix + "domain-modeling"
				tree, err := renderComponent(ComponentTaskSkills, agent, prefix)
				if err != nil {
					t.Fatalf("renderComponent(%s): %v", agent, err)
				}
				body, ok := tree[name+"/SKILL.md"]
				if !ok {
					t.Fatalf("%s: missing %s/SKILL.md; tree has %v", agent, name, keysOf(tree))
				}
				if !strings.Contains(string(body), "\nname: "+name+"\n") {
					t.Errorf("%s: %s body does not carry the resolved name", agent, name)
				}
				if !frontmatterHasOwnershipMarker(string(body)) {
					t.Errorf("%s: %s body is not marked pop-owned", agent, name)
				}
				for _, c := range []string{"ADR-FORMAT.md", "CONTEXT-FORMAT.md"} {
					if _, ok := tree[name+"/"+c]; !ok {
						t.Errorf("%s: %s is missing companion %s", agent, name, c)
					}
				}
			}
		})
	}
}

// TestInstallDomainModelingConflictSkipsOnlyIt covers the conflict path for the
// new skill: a user's own skill sitting at its bare name is never overwritten,
// the one skill is skipped, and every other task skill still installs.
func TestInstallDomainModelingConflictSkipsOnlyIt(t *testing.T) {
	t.Parallel()
	fs := newFakeFS()
	d := fakeDeps(installerHome, fs, nil)

	skillsDir := filepath.Join(installerHome, ".claude", "skills")
	bare := filepath.Join(skillsDir, "domain-modeling")
	fs.dirs[bare] = true
	fs.files[filepath.Join(bare, "SKILL.md")] = []byte("mine")

	if err := installFileComponent(fileRun(d, "claude"), installerHome, ComponentTaskSkills, "claude"); err != nil {
		t.Fatalf("installFileComponent: %v", err)
	}

	if _, linked := fs.symlinks[filepath.Join(skillsDir, "pop-domain-modeling")]; linked {
		t.Fatal("pop-domain-modeling was installed despite a user skill at its bare name")
	}
	if got := string(fs.files[filepath.Join(bare, "SKILL.md")]); got != "mine" {
		t.Fatalf("the user's own domain-modeling skill was overwritten: %q", got)
	}
	for _, skill := range taskSkillNames {
		if skill == "pop-domain-modeling" {
			continue
		}
		if _, linked := fs.symlinks[filepath.Join(skillsDir, skill)]; !linked {
			t.Errorf("unrelated skill %s was not installed: %v", skill, fs.symlinks)
		}
	}
}

// TestInstallDomainModelingPrunesStaleEntry covers the set-subtraction prune for
// the new skill's render root: a pop-owned link left by a build that shipped a
// differently-named discipline skill is removed rather than left answering
// invocations with instructions pop no longer ships.
func TestInstallDomainModelingPrunesStaleEntry(t *testing.T) {
	t.Parallel()
	for _, a := range taskAgents() {
		t.Run(a.name, func(t *testing.T) {
			fs := newFakeFS()
			staleLink := filepath.Join(a.skillDir, "pop-domain-model")
			fs.symlinks[staleLink] = filepath.Join(a.renderDir, "pop-domain-model")

			d := fakeDeps(installerHome, fs, nil)
			if err := installFileComponent(fileRun(d, a.name), installerHome, ComponentTaskSkills, a.name); err != nil {
				t.Fatalf("installFileComponent(%s): %v", a.name, err)
			}

			if _, ok := fs.symlinks[staleLink]; ok {
				t.Fatalf("stale pop-domain-model link survived: %v", fs.symlinks)
			}
			live := filepath.Join(a.skillDir, "pop-domain-modeling")
			if fs.symlinks[live] != filepath.Join(a.renderDir, "pop-domain-modeling") {
				t.Fatalf("pop-domain-modeling not linked: %q -> %q", live, fs.symlinks[live])
			}
			if len(fs.symlinks) != len(taskSkillNames) {
				t.Fatalf("expected %d symlinks, got %d: %v", len(taskSkillNames), len(fs.symlinks), fs.symlinks)
			}
		})
	}
}
