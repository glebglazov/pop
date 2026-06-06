package cmd

import (
	"bytes"
	"path/filepath"
	"strings"
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

// TestConflictCandidates pins the prefix-insensitive candidate expansion: a
// render-tree name yields both its canonical `pop-` form and the bare form, so
// a hand-written skill under either name is caught.
func TestConflictCandidates(t *testing.T) {
	tests := []struct {
		name string
		want []string
	}{
		{"pop-pane", []string{"pop-pane", "pane"}},
		{"pop-pane.md", []string{"pop-pane.md", "pane.md"}},
		{"pop-grill-with-docs", []string{"pop-grill-with-docs", "grill-with-docs"}},
		{"no-prefix", []string{"no-prefix"}},
	}
	for _, tt := range tests {
		got := conflictCandidates(tt.name)
		if len(got) != len(tt.want) {
			t.Fatalf("conflictCandidates(%q) = %v, want %v", tt.name, got, tt.want)
		}
		for i := range got {
			if got[i] != tt.want[i] {
				t.Fatalf("conflictCandidates(%q) = %v, want %v", tt.name, got, tt.want)
			}
		}
	}
}

// TestInstallFileComponentConflictReportsPathAndResolution covers a same-named
// non-pop-owned skill (a user's real directory under the bare name): the skill
// is skipped — never overwritten, never removed — and the report names the
// conflicting path and states the resolution step.
func TestInstallFileComponentConflictReportsPathAndResolution(t *testing.T) {
	fs := newFakeFS()
	out := &bytes.Buffer{}
	d := fakeDeps(installerHome, fs, out)

	_, linkDest, _ := paneSkillPaths()
	// A user's own skill, a real directory pop does not own, under the bare
	// name (the prefix-stripped form of pop's pop-pane).
	bareDest := filepath.Join(filepath.Dir(linkDest), "pane")
	fs.dirs[bareDest] = true
	userFile := filepath.Join(bareDest, "SKILL.md")
	fs.files[userFile] = []byte("hand-written skill")

	if err := installFileComponent(d, installerHome, ComponentPaneSkill, "claude"); err != nil {
		t.Fatalf("installFileComponent: %v", err)
	}

	// Never overwritten, never removed.
	if string(fs.files[userFile]) != "hand-written skill" {
		t.Fatalf("user skill was modified: %q", fs.files[userFile])
	}
	if !fs.dirs[bareDest] {
		t.Fatalf("user skill directory was removed: %s", bareDest)
	}
	// Skipped: pop must not link its version into the agent dir.
	if _, linked := fs.symlinks[linkDest]; linked {
		t.Fatalf("conflict skill was installed anyway: %q", fs.symlinks[linkDest])
	}

	report := out.String()
	if !strings.Contains(report, bareDest) {
		t.Fatalf("report does not name the conflicting path %q: %q", bareDest, report)
	}
	if !strings.Contains(report, "not owned by pop") || !strings.Contains(report, "re-run integrate") {
		t.Fatalf("report does not state the resolution step: %q", report)
	}
}

// TestInstallFileComponentConflictPrefixInsensitive covers the prefix-insensitive
// match: a non-pop entry under the bare name (`pane`) OR the canonical prefixed
// name (`pop-pane`) both block the install. The bare-name conflict is a user's
// real directory; the prefixed-name conflict is a foreign symlink (a real
// pop- entry is a pop-owned copy-mode install, eligible for migration, not a
// conflict).
func TestInstallFileComponentConflictPrefixInsensitive(t *testing.T) {
	_, linkDest, _ := paneSkillPaths()
	agentDir := filepath.Dir(linkDest)
	bareDest := filepath.Join(agentDir, "pane")

	cases := []struct {
		name  string
		setup func(fs *fakeFS) string // returns the conflicting path
	}{
		{"bare form", func(fs *fakeFS) string {
			fs.dirs[bareDest] = true
			fs.files[filepath.Join(bareDest, "SKILL.md")] = []byte("mine")
			return bareDest
		}},
		{"prefixed form", func(fs *fakeFS) string {
			fs.symlinks[linkDest] = "/somewhere/else/pop-pane"
			return linkDest
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			fs := newFakeFS()
			out := &bytes.Buffer{}
			d := fakeDeps(installerHome, fs, out)

			conflict := tc.setup(fs)

			if err := installFileComponent(d, installerHome, ComponentPaneSkill, "claude"); err != nil {
				t.Fatalf("installFileComponent: %v", err)
			}

			// The agent-location link must not point into pop's render tree.
			if target := fs.symlinks[linkDest]; strings.Contains(target, "integrations") {
				t.Fatalf("install proceeded despite %s conflict at %s (linked %q)", tc.name, conflict, target)
			}
			if !strings.Contains(out.String(), conflict) {
				t.Fatalf("report does not name conflict %q: %q", conflict, out.String())
			}
		})
	}
}

// TestInstallFileComponentPartialSetInstall covers a conflict on one skill of a
// multi-skill component skipping only that skill while the rest install. The
// task-skills component renders three skill directories for claude; a
// non-pop entry at one of them must not block the other two.
func TestInstallFileComponentPartialSetInstall(t *testing.T) {
	fs := newFakeFS()
	out := &bytes.Buffer{}
	d := fakeDeps(installerHome, fs, out)

	skillsDir := filepath.Join(installerHome, ".claude", "skills")
	conflictDest := filepath.Join(skillsDir, "pop-to-prd")
	// User's own skill at the bare name of one task skill.
	bareConflict := filepath.Join(skillsDir, "to-tasks")
	fs.dirs[bareConflict] = true
	fs.files[filepath.Join(bareConflict, "SKILL.md")] = []byte("mine")

	if err := installFileComponent(d, installerHome, ComponentTaskSkills, "claude"); err != nil {
		t.Fatalf("installFileComponent: %v", err)
	}

	// The conflicting skill (to-tasks, via its bare form) is skipped.
	if _, linked := fs.symlinks[filepath.Join(skillsDir, "pop-to-tasks")]; linked {
		t.Fatalf("conflicting skill pop-to-tasks was installed despite bare conflict")
	}
	// The other two skills install normally.
	for _, name := range []string{"pop-grill-with-docs", "pop-to-prd"} {
		dest := filepath.Join(skillsDir, name)
		if _, linked := fs.symlinks[dest]; !linked {
			t.Fatalf("non-conflicting skill %s was not installed: %v", name, fs.symlinks)
		}
	}
	_ = conflictDest
}

// TestInstallFileComponentOwnershipExemption covers that a pop-owned symlink is
// never treated as a conflict: re-install rewrites it and refresh proceeds,
// even with the bare-name location also pointing into pop's render tree.
func TestInstallFileComponentOwnershipExemption(t *testing.T) {
	fs := newFakeFS()
	out := &bytes.Buffer{}
	d := fakeDeps(installerHome, fs, out)

	_, linkDest, linkTarget := paneSkillPaths()
	// Pre-existing pop-owned symlink at the canonical location.
	fs.symlinks[linkDest] = linkTarget

	if err := installFileComponent(d, installerHome, ComponentPaneSkill, "claude"); err != nil {
		t.Fatalf("installFileComponent: %v", err)
	}

	if fs.symlinks[linkDest] != linkTarget {
		t.Fatalf("pop-owned symlink not preserved/rewritten: %q", fs.symlinks[linkDest])
	}
	if strings.Contains(out.String(), "not owned by pop") {
		t.Fatalf("pop-owned symlink reported as conflict: %q", out.String())
	}
}
