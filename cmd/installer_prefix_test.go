package cmd

import (
	"bytes"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
)

// withPrefix returns deps that resolve skill names under the given prefix. An
// empty string is a valid, explicit "bare names" choice — distinct from a nil
// pointer (the unset default `pop-`).
func withPrefix(d *integrateDeps, prefix string) *integrateDeps {
	d.skillsPrefix = &prefix
	return d
}

// paneRenderRoot / paneLink helpers, parameterized by the resolved skill name.
func paneRenderRoot() string {
	return filepath.Join(installerHome, ".local", "share", "pop", "integrations", "claude", "pane-skill")
}

// TestInstallFileComponentDefaultPrefixParity pins that a deps with no
// configured prefix (nil pointer) installs under the canonical `pop-` names —
// byte-identical to pop's original behaviour (ADR 0063 criterion).
func TestInstallFileComponentDefaultPrefixParity(t *testing.T) {
	fs := newFakeFS()
	d := fakeDeps(installerHome, fs, nil) // skillPrefix nil → default "pop-"

	if err := installFileComponent(d, installerHome, ComponentPaneSkill, "claude"); err != nil {
		t.Fatalf("installFileComponent: %v", err)
	}

	renderFile, linkDest, linkTarget := paneSkillPaths() // the pop- canonical paths
	if _, ok := fs.files[renderFile]; !ok {
		t.Fatalf("default prefix did not render canonical file %s (have %v)", renderFile, sortedKeys(fs.files))
	}
	if fs.symlinks[linkDest] != linkTarget {
		t.Fatalf("default prefix symlink = %q -> %q, want -> %q", linkDest, fs.symlinks[linkDest], linkTarget)
	}
	// The rendered body must carry the injected `pop-` name and the marker.
	src, _ := skillFiles.ReadFile("skills/pop/tmux-pane.md")
	want := injectOwnershipMarker(injectFrontmatterName(string(src), "pop-tmux-pane"))
	if string(fs.files[renderFile]) != want {
		t.Fatalf("default prefix render bytes mismatch")
	}
}

// TestInstallFileComponentBarePrefix covers skill_prefix = "": the skill lands
// under its bare base name in the render tree and at the agent location, and
// the injected frontmatter name is bare too. There is no `pop-` form anywhere.
func TestInstallFileComponentBarePrefix(t *testing.T) {
	fs := newFakeFS()
	d := withPrefix(fakeDeps(installerHome, fs, nil), "")

	if err := installFileComponent(d, installerHome, ComponentPaneSkill, "claude"); err != nil {
		t.Fatalf("installFileComponent: %v", err)
	}

	bareRender := filepath.Join(paneRenderRoot(), "tmux-pane", "SKILL.md")
	bareLink := filepath.Join(installerHome, ".claude", "skills", "tmux-pane")
	bareTarget := filepath.Join(paneRenderRoot(), "tmux-pane")

	if _, ok := fs.files[bareRender]; !ok {
		t.Fatalf("bare render file not written: %s (have %v)", bareRender, sortedKeys(fs.files))
	}
	// No pop- form in the render tree.
	popRender := filepath.Join(paneRenderRoot(), "pop-tmux-pane", "SKILL.md")
	if _, ok := fs.files[popRender]; ok {
		t.Fatalf("bare install must not render the pop- form %s", popRender)
	}
	if fs.symlinks[bareLink] != bareTarget {
		t.Fatalf("bare symlink = %q -> %q, want -> %q", bareLink, fs.symlinks[bareLink], bareTarget)
	}
	if len(fs.symlinks) != 1 {
		t.Fatalf("expected exactly 1 symlink, got %d: %v", len(fs.symlinks), fs.symlinks)
	}
	// The injected frontmatter name is bare — the file name carries no prefix.
	src, _ := skillFiles.ReadFile("skills/pop/tmux-pane.md")
	want := injectOwnershipMarker(injectFrontmatterName(string(src), "tmux-pane"))
	if string(fs.files[bareRender]) != want {
		t.Fatalf("bare render bytes mismatch:\n got %q\nwant %q", fs.files[bareRender], want)
	}
}

// TestInstallFileComponentPrefixChangeMigration is the core stale-name cleanup
// case (ADR 0063): a skill installed under `pop-` is re-integrated with an
// empty prefix. The new bare name is linked, and the old `pop-tmux-pane` pop-owned
// symlink and its render-tree directory are pruned — no duplicate left behind.
func TestInstallFileComponentPrefixChangeMigration(t *testing.T) {
	fs := newFakeFS()
	var logs []string
	logfDeps := func() *integrateDeps {
		d := fakeDeps(installerHome, fs, nil)
		d.logf = func(format string, args ...any) { logs = append(logs, fmt.Sprintf(format, args...)) }
		return d
	}

	// First install under the default pop- prefix.
	if err := installFileComponent(logfDeps(), installerHome, ComponentPaneSkill, "claude"); err != nil {
		t.Fatalf("first install: %v", err)
	}
	_, popLink, popTarget := paneSkillPaths()
	if fs.symlinks[popLink] != popTarget {
		t.Fatalf("setup: expected pop- symlink %q -> %q, got %q", popLink, popTarget, fs.symlinks[popLink])
	}

	// Re-integrate with an empty prefix.
	logs = nil
	if err := installFileComponent(withPrefix(logfDeps(), ""), installerHome, ComponentPaneSkill, "claude"); err != nil {
		t.Fatalf("re-integrate bare: %v", err)
	}

	bareLink := filepath.Join(installerHome, ".claude", "skills", "tmux-pane")
	bareTarget := filepath.Join(paneRenderRoot(), "tmux-pane")

	// New bare name linked.
	if fs.symlinks[bareLink] != bareTarget {
		t.Fatalf("bare symlink = %q -> %q, want -> %q", bareLink, fs.symlinks[bareLink], bareTarget)
	}
	// Old pop- agent-location symlink pruned — no duplicate.
	if _, ok := fs.symlinks[popLink]; ok {
		t.Fatalf("stale pop- symlink not pruned: %s still -> %q", popLink, fs.symlinks[popLink])
	}
	if len(fs.symlinks) != 1 {
		t.Fatalf("expected exactly 1 symlink after migration, got %d: %v", len(fs.symlinks), fs.symlinks)
	}
	// Old render-tree directory pruned (removeAll(renderRoot) before re-render).
	popRender := filepath.Join(paneRenderRoot(), "pop-tmux-pane", "SKILL.md")
	if _, ok := fs.files[popRender]; ok {
		t.Fatalf("stale pop- render file not pruned: %s", popRender)
	}
	bareRender := filepath.Join(paneRenderRoot(), "tmux-pane", "SKILL.md")
	if _, ok := fs.files[bareRender]; !ok {
		t.Fatalf("new bare render file missing: %s", bareRender)
	}
	// The prune logs per slice 01's convention.
	if !containsSubstr(logs, "pruning stale "+popLink) {
		t.Fatalf("expected a prune log line for %s, got: %v", popLink, logs)
	}
}

// TestInstallFileComponentPruneNeverRemovesUnowned guards criterion 4: a prefix
// change must never remove an entry pop does not own, even when that entry's
// name is absent from the freshly rendered set.
func TestInstallFileComponentPruneNeverRemovesUnowned(t *testing.T) {
	fs := newFakeFS()

	// Prior pop install under pop-.
	if err := installFileComponent(fakeDeps(installerHome, fs, nil), installerHome, ComponentPaneSkill, "claude"); err != nil {
		t.Fatalf("first install: %v", err)
	}

	// A user's own skill (real dir, no marker) and a foreign symlink pointing
	// outside pop's tree — neither is a freshly rendered name under the new
	// empty prefix.
	foreignDir := filepath.Join(installerHome, ".claude", "skills", "my-notes")
	fs.dirs[foreignDir] = true
	fs.files[filepath.Join(foreignDir, "SKILL.md")] = []byte("---\nname: my-notes\n---\nmine")
	foreignLink := filepath.Join(installerHome, ".claude", "skills", "other")
	fs.symlinks[foreignLink] = "/elsewhere/other"

	if err := installFileComponent(withPrefix(fakeDeps(installerHome, fs, nil), ""), installerHome, ComponentPaneSkill, "claude"); err != nil {
		t.Fatalf("re-integrate bare: %v", err)
	}

	if !fs.dirs[foreignDir] {
		t.Fatalf("unowned skill dir was removed by prune: %s", foreignDir)
	}
	if _, ok := fs.files[filepath.Join(foreignDir, "SKILL.md")]; !ok {
		t.Fatalf("unowned skill SKILL.md was removed by prune")
	}
	if fs.symlinks[foreignLink] != "/elsewhere/other" {
		t.Fatalf("foreign symlink was removed/altered by prune: %q", fs.symlinks[foreignLink])
	}
	// The pop-owned stale link is still pruned though.
	_, popLink, _ := paneSkillPaths()
	if _, ok := fs.symlinks[popLink]; ok {
		t.Fatalf("stale pop- symlink not pruned: %s", popLink)
	}
}

// TestInstallFileComponentPruneIsComponentScoped guards that pruning one
// component's stale names never removes another component's valid links: the
// scope is THIS component's render root, not the whole agent location.
func TestInstallFileComponentPruneIsComponentScoped(t *testing.T) {
	fs := newFakeFS()

	// Install both skill components under pop-.
	if err := installFileComponent(fakeDeps(installerHome, fs, nil), installerHome, ComponentPaneSkill, "claude"); err != nil {
		t.Fatalf("install pane-skill: %v", err)
	}
	if err := installFileComponent(fakeDeps(installerHome, fs, nil), installerHome, ComponentTaskSkills, "claude"); err != nil {
		t.Fatalf("install task-skills: %v", err)
	}
	grillLink := filepath.Join(installerHome, ".claude", "skills", "pop-grill-with-docs")
	if _, ok := fs.symlinks[grillLink]; !ok {
		t.Fatalf("setup: expected task-skills link %s", grillLink)
	}

	// Re-integrate ONLY pane-skill with an empty prefix.
	if err := installFileComponent(withPrefix(fakeDeps(installerHome, fs, nil), ""), installerHome, ComponentPaneSkill, "claude"); err != nil {
		t.Fatalf("re-integrate pane-skill bare: %v", err)
	}

	// pane-skill's old pop- link is pruned...
	_, popPaneLink, _ := paneSkillPaths()
	if _, ok := fs.symlinks[popPaneLink]; ok {
		t.Fatalf("stale pop-tmux-pane link not pruned: %s", popPaneLink)
	}
	// ...but task-skills' link (a different component's render root) is untouched.
	if _, ok := fs.symlinks[grillLink]; !ok {
		t.Fatalf("task-skills link wrongly pruned during pane-skill re-integrate: %s", grillLink)
	}
}

// TestInstallFileComponentPrunesStaleMarkerOwned covers the marker-owned half
// of stale-name cleanup: a real, marker-bearing copy-mode entry left under the
// old `pop-` name is pruned when the component is re-rendered under an empty
// prefix, just like a stale symlink.
func TestInstallFileComponentPrunesStaleMarkerOwned(t *testing.T) {
	fs := newFakeFS()

	// A leftover copy-mode install under the pop- name: a real directory whose
	// SKILL.md carries the pop-owned marker.
	staleDir := filepath.Join(installerHome, ".claude", "skills", "pop-tmux-pane")
	fs.dirs[staleDir] = true
	fs.files[filepath.Join(staleDir, "SKILL.md")] = []byte("---\npop-owned: true\nname: pop-tmux-pane\n---\nold copy body")

	if err := installFileComponent(withPrefix(fakeDeps(installerHome, fs, nil), ""), installerHome, ComponentPaneSkill, "claude"); err != nil {
		t.Fatalf("installFileComponent: %v", err)
	}

	// The bare name is linked...
	bareLink := filepath.Join(installerHome, ".claude", "skills", "tmux-pane")
	if _, ok := fs.symlinks[bareLink]; !ok {
		t.Fatalf("bare name not linked: %s", bareLink)
	}
	// ...and the stale marker-owned copy-mode dir is gone.
	if fs.dirs[staleDir] {
		t.Fatalf("stale marker-owned dir not pruned: %s", staleDir)
	}
	if _, ok := fs.files[filepath.Join(staleDir, "SKILL.md")]; ok {
		t.Fatalf("stale marker-owned SKILL.md not pruned")
	}
}

// TestInstallFileComponentBarePrefixConflict covers criterion 5 under an empty
// prefix: the resolved install name is the bare base, so an unowned entry at
// that bare name is the conflict — reported at the resolved name and skipped,
// never overwritten. There is no pop- candidate to fall back on.
func TestInstallFileComponentBarePrefixConflict(t *testing.T) {
	fs := newFakeFS()
	out := &bytes.Buffer{}
	d := withPrefix(fakeDeps(installerHome, fs, out), "")

	bareDest := filepath.Join(installerHome, ".claude", "skills", "tmux-pane")
	fs.dirs[bareDest] = true
	userFile := filepath.Join(bareDest, "SKILL.md")
	fs.files[userFile] = []byte("hand-written skill")

	if err := installFileComponent(d, installerHome, ComponentPaneSkill, "claude"); err != nil {
		t.Fatalf("installFileComponent: %v", err)
	}

	// The unowned entry at the resolved name is never touched.
	if string(fs.files[userFile]) != "hand-written skill" {
		t.Fatalf("user skill at resolved name was modified: %q", fs.files[userFile])
	}
	if len(fs.symlinks) != 0 {
		t.Fatalf("conflict must not install a symlink, got %v", fs.symlinks)
	}
	// The report names the conflicting resolved path.
	if report := out.String(); !strings.Contains(report, bareDest) || !strings.Contains(report, "not owned by pop") {
		t.Fatalf("report does not name the conflict at the resolved name %q: %q", bareDest, report)
	}
}

func containsSubstr(lines []string, sub string) bool {
	for _, l := range lines {
		if strings.Contains(l, sub) {
			return true
		}
	}
	return false
}
