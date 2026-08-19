package integrate

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// setupDomainSeed is the path of the Domain-doc template the setup skill copies
// into a configured repository's docs/agents/domain.md.
const setupDomainSeed = "skills/pop/setup-matt-pocock-skills/domain.md"

// effectiveGlossaryReadRule is what a passive consumer of the glossary has to be
// told: where fragments live now, where the legacy ones live, that a dotdir needs
// a hidden glob, and how the overlay resolves. Both carriers of the rule — the
// setup seed and pop's own docs/agents/domain.md — are checked against it.
var effectiveGlossaryReadRule = []string{
	".grill-context/",
	"effective glossary",
	"legacy",
	"--hidden",
	"contested",
	"generation beats a lower one",
}

// writeAlgorithmMarkers are lines that belong only to domain-modeling and its
// CONTEXT-FORMAT.md. Turn-one repository instructions are read by every agent in
// the repo, so the fragment *write* algorithm must not leak into either carrier
// of the read rule — ADR-0225 decision 6 gives the scheme one owner.
var writeAlgorithmMarkers = []string{
	"uuidgen",
	"max(counter) + 1",
	"fragment:",
	"under:",
	"was:",
}

// TestSetupDomainSeedDefinesEffectiveGlossary asserts the pop overlay on the
// Domain-doc seed carries the consumer rule and only the consumer rule.
func TestSetupDomainSeedDefinesEffectiveGlossary(t *testing.T) {
	t.Parallel()
	src, err := skillFiles.ReadFile(setupDomainSeed)
	if err != nil {
		t.Fatalf("read %s: %v", setupDomainSeed, err)
	}
	body := string(src)

	markerIdx := strings.Index(body, "POP OVERLAY")
	if markerIdx == -1 {
		t.Fatalf("%s has no POP OVERLAY marker: the seed must carry pop's fragment read rule", setupDomainSeed)
	}
	overlay := body[markerIdx:]

	for _, want := range effectiveGlossaryReadRule {
		if !strings.Contains(overlay, want) {
			t.Fatalf("%s overlay missing read rule %q:\n%s", setupDomainSeed, want, overlay)
		}
	}
	for _, unwanted := range writeAlgorithmMarkers {
		if strings.Contains(overlay, unwanted) {
			t.Fatalf("%s overlay carries write-algorithm detail %q; that belongs to domain-modeling and its CONTEXT-FORMAT.md", setupDomainSeed, unwanted)
		}
	}
	// The rule has to hand a writing session off to the owner rather than
	// letting a passive reader improvise a fragment.
	for _, want := range []string{"`domain-modeling`", "`grill-consolidate`"} {
		if !strings.Contains(overlay, want) {
			t.Fatalf("%s overlay does not name %s as the owner of writes", setupDomainSeed, want)
		}
	}
	if !strings.Contains(overlay, "@8b78b53") && !strings.Contains(body, "@8b78b53") {
		t.Fatalf("%s no longer records its upstream pin", setupDomainSeed)
	}
}

// TestDomainModelingKeepsFragmentWriteAlgorithm is the other half: the write
// rules the seed must not repeat have to still exist in full where they belong.
func TestDomainModelingKeepsFragmentWriteAlgorithm(t *testing.T) {
	t.Parallel()
	src, err := skillFiles.ReadFile("skills/pop/domain-modeling/CONTEXT-FORMAT.md")
	if err != nil {
		t.Fatalf("read CONTEXT-FORMAT.md: %v", err)
	}
	for _, want := range writeAlgorithmMarkers {
		if !strings.Contains(string(src), want) {
			t.Fatalf("domain-modeling/CONTEXT-FORMAT.md lost write-algorithm detail %q — the seed defers to it", want)
		}
	}
}

// TestRepoDomainDocCarriesReadRule asserts pop's own repository instructions say
// the same thing they tell configured repositories to say. Pop is a
// Pop-configured repo; an agent starting here reads the glossary the same way.
func TestRepoDomainDocCarriesReadRule(t *testing.T) {
	t.Parallel()
	raw, err := os.ReadFile(filepath.Join("..", "docs", "agents", "domain.md"))
	if err != nil {
		t.Fatalf("read docs/agents/domain.md: %v", err)
	}
	doc := string(raw)
	for _, want := range effectiveGlossaryReadRule {
		if !strings.Contains(doc, want) {
			t.Fatalf("docs/agents/domain.md missing read rule %q", want)
		}
	}
	for _, unwanted := range writeAlgorithmMarkers {
		if strings.Contains(doc, unwanted) {
			t.Fatalf("docs/agents/domain.md carries write-algorithm detail %q; keep it in domain-modeling", unwanted)
		}
	}

	// The pointer path: AGENTS.md is the real file, CLAUDE.md its symlink, and
	// the Domain docs entry is how any agent CLI reaches the rule on turn one.
	agents, err := os.ReadFile(filepath.Join("..", "AGENTS.md"))
	if err != nil {
		t.Fatalf("read AGENTS.md: %v", err)
	}
	if !strings.Contains(string(agents), "docs/agents/domain.md") {
		t.Fatalf("AGENTS.md no longer points at docs/agents/domain.md:\n%s", string(agents))
	}
	link, err := os.Lstat(filepath.Join("..", "CLAUDE.md"))
	if err != nil {
		t.Fatalf("lstat CLAUDE.md: %v", err)
	}
	if link.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("CLAUDE.md is not a symlink to AGENTS.md; claude would stop seeing the pointer")
	}
}

// TestInstalledDomainSeedCarriesReadRule drives the rule through render and
// install for every directory-hosting agent: the seed the setup session copies
// from has to arrive intact, with cross-skill names resolved to the installed
// prefix so a user's docs/agents/domain.md names a skill that exists.
func TestInstalledDomainSeedCarriesReadRule(t *testing.T) {
	t.Parallel()
	for _, a := range taskAgents() {
		t.Run(a.name, func(t *testing.T) {
			fs := newFakeFS()
			d := fakeDeps(installerHome, fs, nil)
			if err := installFileComponent(fileRun(d, a.name), installerHome, ComponentTaskSkills, a.name); err != nil {
				t.Fatalf("installFileComponent(%s): %v", a.name, err)
			}

			p := filepath.Join(a.renderDir, "pop-setup-matt-pocock-skills", "domain.md")
			got, ok := fs.files[p]
			if !ok {
				t.Fatalf("domain seed not installed at %s (have %v)", p, sortedKeys(fs.files))
			}
			for _, want := range append(effectiveGlossaryReadRule, "`pop-domain-modeling`", "`pop-grill-consolidate`") {
				if !strings.Contains(string(got), want) {
					t.Fatalf("installed %s missing %q", p, want)
				}
			}
		})
	}
}
