package cmd

import (
	"path/filepath"
	"testing"
)

const installerHome = "/home/u"

// paneSkillPaths returns the canonical paths the pane-skill install touches for
// claude, derived from installerHome.
func paneSkillPaths() (renderFile, linkDest, linkTarget string) {
	renderRoot := filepath.Join(installerHome, ".local", "share", "pop", "integrations", "claude", "pane-skill")
	renderFile = filepath.Join(renderRoot, "pop-pane", "SKILL.md")
	linkDest = filepath.Join(installerHome, ".claude", "skills", "pop-pane")
	linkTarget = filepath.Join(renderRoot, "pop-pane")
	return
}

// TestInstallFileComponentInstall covers the clean install: the rendered tree
// lands under the data dir and the agent location is a symlink into it.
func TestInstallFileComponentInstall(t *testing.T) {
	fs := newFakeFS()
	d := fakeDeps(installerHome, fs, nil)

	if err := installFileComponent(d, installerHome, ComponentPaneSkill, "claude"); err != nil {
		t.Fatalf("installFileComponent: %v", err)
	}

	renderFile, linkDest, linkTarget := paneSkillPaths()

	data, ok := fs.files[renderFile]
	if !ok {
		t.Fatalf("render file not written: %s (have %v)", renderFile, sortedKeys(fs.files))
	}
	src, _ := skillFiles.ReadFile("skills/pop/pane.md")
	want := injectFrontmatterName(string(src), "pop-pane")
	if string(data) != want {
		t.Fatalf("render bytes mismatch")
	}

	target, ok := fs.symlinks[linkDest]
	if !ok {
		t.Fatalf("no symlink at %s", linkDest)
	}
	if target != linkTarget {
		t.Fatalf("symlink target = %q, want %q", target, linkTarget)
	}
}

// TestInstallFileComponentIdempotent covers re-running: the symlink is rewritten
// to the same target and nothing duplicates.
func TestInstallFileComponentIdempotent(t *testing.T) {
	fs := newFakeFS()
	d := fakeDeps(installerHome, fs, nil)

	for i := 0; i < 2; i++ {
		if err := installFileComponent(d, installerHome, ComponentPaneSkill, "claude"); err != nil {
			t.Fatalf("install pass %d: %v", i, err)
		}
	}

	_, linkDest, linkTarget := paneSkillPaths()
	if len(fs.symlinks) != 1 {
		t.Fatalf("expected exactly 1 symlink, got %d: %v", len(fs.symlinks), fs.symlinks)
	}
	if fs.symlinks[linkDest] != linkTarget {
		t.Fatalf("symlink target = %q, want %q", fs.symlinks[linkDest], linkTarget)
	}
}

// TestInstallFileComponentCopyToSymlinkMigration covers a pre-existing copy-mode
// install (a real directory under the pop- name prefix) being replaced by a
// symlink.
func TestInstallFileComponentCopyToSymlinkMigration(t *testing.T) {
	fs := newFakeFS()
	d := fakeDeps(installerHome, fs, nil)

	_, linkDest, linkTarget := paneSkillPaths()
	// Simulate an older copy-mode install: a real directory + file at the
	// agent's skill location.
	fs.dirs[linkDest] = true
	copyFile := filepath.Join(linkDest, "SKILL.md")
	fs.files[copyFile] = []byte("old copy-mode body")

	if err := installFileComponent(d, installerHome, ComponentPaneSkill, "claude"); err != nil {
		t.Fatalf("installFileComponent: %v", err)
	}

	if _, ok := fs.files[copyFile]; ok {
		t.Fatalf("copy-mode file not removed: %s", copyFile)
	}
	if fs.dirs[linkDest] {
		t.Fatalf("copy-mode directory not removed: %s", linkDest)
	}
	if fs.symlinks[linkDest] != linkTarget {
		t.Fatalf("expected migration to symlink %q -> %q, got %q", linkDest, linkTarget, fs.symlinks[linkDest])
	}
}

// TestInstallFileComponentLegacyClaudeCommandRemoved covers the old slash
// command being cleaned up by the new skill install path.
func TestInstallFileComponentLegacyClaudeCommandRemoved(t *testing.T) {
	fs := newFakeFS()
	d := fakeDeps(installerHome, fs, nil)

	legacy := filepath.Join(installerHome, ".claude", "commands", "pop", "pane.md")
	fs.files[legacy] = []byte("legacy command body")

	if err := installFileComponent(d, installerHome, ComponentPaneSkill, "claude"); err != nil {
		t.Fatalf("installFileComponent: %v", err)
	}

	if _, ok := fs.files[legacy]; ok {
		t.Fatalf("legacy claude command not removed: %s", legacy)
	}
}

// paneSkillAgent describes the canonical pane-skill install layout for one
// agent: where the rendered file lands under the data dir, the agent-location
// symlink and its target, the legacy copy-mode entry to migrate, and the
// expected rendered bytes.
type paneSkillAgent struct {
	name       string
	renderFile string
	linkDest   string
	linkTarget string
	wantBytes  []byte
	// legacyDir/legacyFile model a pre-symlink copy-mode install at the agent
	// location. Skill-directory agents (pi, cursor) had a real directory with a
	// SKILL.md inside; opencode had a single flat file. Exactly one shape is
	// set per agent.
	legacyDir  string
	legacyFile string
}

// paneSkillAgents returns the install layout for every agent newly wired in
// this slice (pi, cursor, opencode), derived from installerHome.
func paneSkillAgents() []paneSkillAgent {
	src, _ := skillFiles.ReadFile("skills/pop/pane.md")
	skillDirBytes := []byte(injectFrontmatterName(string(src), "pop-pane"))

	dataRoot := filepath.Join(installerHome, ".local", "share", "pop", "integrations")

	piRoot := filepath.Join(dataRoot, "pi", "pane-skill")
	curRoot := filepath.Join(dataRoot, "cursor", "pane-skill")
	ocRoot := filepath.Join(dataRoot, "opencode", "pane-skill")

	piDest := filepath.Join(installerHome, ".pi", "agent", "skills", "pop-pane")
	curDest := filepath.Join(installerHome, ".cursor", "skills", "pop-pane")
	ocDest := filepath.Join(installerHome, ".config", "opencode", "agent", "pop-pane.md")

	return []paneSkillAgent{
		{
			name:       "pi",
			renderFile: filepath.Join(piRoot, "pop-pane", "SKILL.md"),
			linkDest:   piDest,
			linkTarget: filepath.Join(piRoot, "pop-pane"),
			wantBytes:  skillDirBytes,
			legacyDir:  piDest,
		},
		{
			name:       "cursor",
			renderFile: filepath.Join(curRoot, "pop-pane", "SKILL.md"),
			linkDest:   curDest,
			linkTarget: filepath.Join(curRoot, "pop-pane"),
			wantBytes:  skillDirBytes,
			legacyDir:  curDest,
		},
		{
			name:       "opencode",
			renderFile: filepath.Join(ocRoot, "pop-pane.md"),
			linkDest:   ocDest,
			linkTarget: filepath.Join(ocRoot, "pop-pane.md"),
			wantBytes:  src,
			legacyFile: ocDest,
		},
	}
}

// TestInstallFileComponentInstallNewAgents covers the clean install for pi,
// cursor, and opencode: the rendered tree lands under the data dir and the
// agent location is a symlink into it (a skill directory for pi/cursor, a flat
// file for opencode).
func TestInstallFileComponentInstallNewAgents(t *testing.T) {
	for _, a := range paneSkillAgents() {
		t.Run(a.name, func(t *testing.T) {
			fs := newFakeFS()
			d := fakeDeps(installerHome, fs, nil)

			if err := installFileComponent(d, installerHome, ComponentPaneSkill, a.name); err != nil {
				t.Fatalf("installFileComponent(%s): %v", a.name, err)
			}

			data, ok := fs.files[a.renderFile]
			if !ok {
				t.Fatalf("render file not written: %s (have %v)", a.renderFile, sortedKeys(fs.files))
			}
			if string(data) != string(a.wantBytes) {
				t.Fatalf("render bytes mismatch for %s:\n got: %q\nwant: %q", a.name, string(data), string(a.wantBytes))
			}
			if fs.symlinks[a.linkDest] != a.linkTarget {
				t.Fatalf("symlink %q = %q, want %q", a.linkDest, fs.symlinks[a.linkDest], a.linkTarget)
			}
		})
	}
}

// TestInstallFileComponentIdempotentNewAgents covers re-running for pi, cursor,
// and opencode: a single symlink to the same target, nothing duplicated.
func TestInstallFileComponentIdempotentNewAgents(t *testing.T) {
	for _, a := range paneSkillAgents() {
		t.Run(a.name, func(t *testing.T) {
			fs := newFakeFS()
			d := fakeDeps(installerHome, fs, nil)

			for i := 0; i < 2; i++ {
				if err := installFileComponent(d, installerHome, ComponentPaneSkill, a.name); err != nil {
					t.Fatalf("install pass %d (%s): %v", i, a.name, err)
				}
			}
			if len(fs.symlinks) != 1 {
				t.Fatalf("expected exactly 1 symlink, got %d: %v", len(fs.symlinks), fs.symlinks)
			}
			if fs.symlinks[a.linkDest] != a.linkTarget {
				t.Fatalf("symlink %q = %q, want %q", a.linkDest, fs.symlinks[a.linkDest], a.linkTarget)
			}
		})
	}
}

// TestInstallFileComponentMigrationNewAgents covers a pre-existing copy-mode
// install being replaced by a symlink: a real skill directory for pi/cursor, a
// real flat file for opencode. Both live under the pop- name prefix at the
// agent location, so ownership recognizes them as pop-owned and migrates them.
func TestInstallFileComponentMigrationNewAgents(t *testing.T) {
	for _, a := range paneSkillAgents() {
		t.Run(a.name, func(t *testing.T) {
			fs := newFakeFS()
			d := fakeDeps(installerHome, fs, nil)

			if a.legacyDir != "" {
				fs.dirs[a.legacyDir] = true
				fs.files[filepath.Join(a.legacyDir, "SKILL.md")] = []byte("old copy-mode body")
			}
			if a.legacyFile != "" {
				fs.files[a.legacyFile] = []byte("old copy-mode body")
			}

			if err := installFileComponent(d, installerHome, ComponentPaneSkill, a.name); err != nil {
				t.Fatalf("installFileComponent(%s): %v", a.name, err)
			}

			if a.legacyDir != "" {
				if fs.dirs[a.legacyDir] {
					t.Fatalf("copy-mode directory not removed: %s", a.legacyDir)
				}
				if _, ok := fs.files[filepath.Join(a.legacyDir, "SKILL.md")]; ok {
					t.Fatalf("copy-mode file not removed under %s", a.legacyDir)
				}
			}
			if a.legacyFile != "" {
				// The flat copy-mode file is replaced by a symlink at the same path.
				if _, ok := fs.files[a.legacyFile]; ok {
					t.Fatalf("copy-mode file not removed: %s", a.legacyFile)
				}
			}
			if fs.symlinks[a.linkDest] != a.linkTarget {
				t.Fatalf("expected migration to symlink %q -> %q, got %q", a.linkDest, a.linkTarget, fs.symlinks[a.linkDest])
			}
		})
	}
}

// TestInstallFileComponentConflictSkipped covers ownership detection via the
// symlink target: an entry that is a symlink pointing OUTSIDE pop's render tree
// is not owned by pop and is left untouched.
func TestInstallFileComponentConflictSkipped(t *testing.T) {
	fs := newFakeFS()
	d := fakeDeps(installerHome, fs, nil)

	_, linkDest, _ := paneSkillPaths()
	foreign := "/somewhere/else/pop-pane"
	fs.symlinks[linkDest] = foreign

	if err := installFileComponent(d, installerHome, ComponentPaneSkill, "claude"); err != nil {
		t.Fatalf("installFileComponent: %v", err)
	}

	if fs.symlinks[linkDest] != foreign {
		t.Fatalf("non-owned symlink was overwritten: now %q", fs.symlinks[linkDest])
	}
}

// TestOwnership exercises the ownership predicate directly across its cases.
func TestOwnership(t *testing.T) {
	fs := newFakeFS()
	d := fakeDeps(installerHome, fs, nil)
	integrationsRoot := filepath.Join(installerHome, ".local", "share", "pop", "integrations")
	dest := filepath.Join(installerHome, ".claude", "skills", "pop-pane")

	// Missing entry.
	if exists, owned, err := ownership(d, dest, integrationsRoot); err != nil || exists || owned {
		t.Fatalf("missing: exists=%v owned=%v err=%v", exists, owned, err)
	}

	// Symlink into pop's tree → owned.
	fs.symlinks[dest] = filepath.Join(integrationsRoot, "claude", "pane-skill", "pop-pane")
	if exists, owned, err := ownership(d, dest, integrationsRoot); err != nil || !exists || !owned {
		t.Fatalf("in-tree symlink: exists=%v owned=%v err=%v", exists, owned, err)
	}

	// Symlink outside pop's tree → not owned.
	fs.symlinks[dest] = "/elsewhere/pop-pane"
	if exists, owned, err := ownership(d, dest, integrationsRoot); err != nil || !exists || owned {
		t.Fatalf("foreign symlink: exists=%v owned=%v err=%v", exists, owned, err)
	}
	delete(fs.symlinks, dest)

	// Real pop- prefixed directory → owned (copy-mode).
	fs.dirs[dest] = true
	if exists, owned, err := ownership(d, dest, integrationsRoot); err != nil || !exists || !owned {
		t.Fatalf("pop- copy dir: exists=%v owned=%v err=%v", exists, owned, err)
	}
	delete(fs.dirs, dest)

	// Real directory without the pop- prefix → not owned.
	other := filepath.Join(installerHome, ".claude", "skills", "my-skill")
	fs.dirs[other] = true
	if exists, owned, err := ownership(d, other, integrationsRoot); err != nil || !exists || owned {
		t.Fatalf("foreign dir: exists=%v owned=%v err=%v", exists, owned, err)
	}
}
