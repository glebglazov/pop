package cmd

import (
	"bytes"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
)

const installerHome = "/home/u"

// paneSkillPaths returns the canonical paths the pane-skill install touches for
// claude, derived from installerHome.
func paneSkillPaths() (renderFile, linkDest, linkTarget string) {
	renderRoot := filepath.Join(installerHome, ".local", "share", "pop", "integrations", "claude", "pane-skill")
	renderFile = filepath.Join(renderRoot, "pop-tmux-pane", "SKILL.md")
	linkDest = filepath.Join(installerHome, ".claude", "skills", "pop-tmux-pane")
	linkTarget = filepath.Join(renderRoot, "pop-tmux-pane")
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
	src, _ := skillFiles.ReadFile("skills/pop/tmux-pane.md")
	want := injectOwnershipMarker(injectFrontmatterName(string(src), "pop-tmux-pane"))
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
	fs.files[copyFile] = []byte(injectOwnershipMarker("---\nname: pop-tmux-pane\n---\nold copy-mode body"))

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
// this slice (codex, pi, cursor, opencode), derived from installerHome.
func paneSkillAgents() []paneSkillAgent {
	src, _ := skillFiles.ReadFile("skills/pop/tmux-pane.md")
	skillDirBytes := []byte(injectOwnershipMarker(injectFrontmatterName(string(src), "pop-tmux-pane")))

	dataRoot := filepath.Join(installerHome, ".local", "share", "pop", "integrations")

	codexRoot := filepath.Join(dataRoot, "codex", "pane-skill")
	piRoot := filepath.Join(dataRoot, "pi", "pane-skill")
	curRoot := filepath.Join(dataRoot, "cursor", "pane-skill")
	ocRoot := filepath.Join(dataRoot, "opencode", "pane-skill")

	codexDest := filepath.Join(installerHome, ".codex", "skills", "pop-tmux-pane")
	piDest := filepath.Join(installerHome, ".pi", "agent", "skills", "pop-tmux-pane")
	curDest := filepath.Join(installerHome, ".cursor", "skills", "pop-tmux-pane")
	ocDest := filepath.Join(installerHome, ".config", "opencode", "agent", "pop-tmux-pane.md")

	return []paneSkillAgent{
		{
			name:       "codex",
			renderFile: filepath.Join(codexRoot, "pop-tmux-pane", "SKILL.md"),
			linkDest:   codexDest,
			linkTarget: filepath.Join(codexRoot, "pop-tmux-pane"),
			wantBytes:  skillDirBytes,
			legacyDir:  codexDest,
		},
		{
			name:       "pi",
			renderFile: filepath.Join(piRoot, "pop-tmux-pane", "SKILL.md"),
			linkDest:   piDest,
			linkTarget: filepath.Join(piRoot, "pop-tmux-pane"),
			wantBytes:  skillDirBytes,
			legacyDir:  piDest,
		},
		{
			name:       "cursor",
			renderFile: filepath.Join(curRoot, "pop-tmux-pane", "SKILL.md"),
			linkDest:   curDest,
			linkTarget: filepath.Join(curRoot, "pop-tmux-pane"),
			wantBytes:  skillDirBytes,
			legacyDir:  curDest,
		},
		{
			name:       "opencode",
			renderFile: filepath.Join(ocRoot, "pop-tmux-pane.md"),
			linkDest:   ocDest,
			linkTarget: filepath.Join(ocRoot, "pop-tmux-pane.md"),
			wantBytes:  []byte(injectOwnershipMarker(string(src))),
			legacyFile: ocDest,
		},
	}
}

// TestInstallFileComponentInstallNewAgents covers the clean install for codex,
// pi, cursor, and opencode: the rendered tree lands under the data dir and the
// agent location is a symlink into it (a skill directory for codex/pi/cursor, a
// flat file for opencode).
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

// TestInstallFileComponentMigrationNewAgents covers a pre-existing marker-owned
// copy-mode install being replaced by a symlink.
func TestInstallFileComponentMigrationNewAgents(t *testing.T) {
	for _, a := range paneSkillAgents() {
		t.Run(a.name, func(t *testing.T) {
			fs := newFakeFS()
			d := fakeDeps(installerHome, fs, nil)
			markerBody := injectOwnershipMarker("---\nname: pop-tmux-pane\n---\nold copy-mode body")

			if a.legacyDir != "" {
				fs.dirs[a.legacyDir] = true
				fs.files[filepath.Join(a.legacyDir, "SKILL.md")] = []byte(markerBody)
			}
			if a.legacyFile != "" {
				fs.files[a.legacyFile] = []byte(markerBody)
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
	foreign := "/somewhere/else/pop-tmux-pane"
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
	dest := filepath.Join(installerHome, ".claude", "skills", "pop-tmux-pane")

	// Missing entry.
	if exists, owned, err := ownership(d, dest, integrationsRoot); err != nil || exists || owned {
		t.Fatalf("missing: exists=%v owned=%v err=%v", exists, owned, err)
	}

	// Symlink into pop's tree → owned.
	fs.symlinks[dest] = filepath.Join(integrationsRoot, "claude", "pane-skill", "pop-tmux-pane")
	if exists, owned, err := ownership(d, dest, integrationsRoot); err != nil || !exists || !owned {
		t.Fatalf("in-tree symlink: exists=%v owned=%v err=%v", exists, owned, err)
	}

	// Symlink outside pop's tree → not owned.
	fs.symlinks[dest] = "/elsewhere/pop-tmux-pane"
	if exists, owned, err := ownership(d, dest, integrationsRoot); err != nil || !exists || owned {
		t.Fatalf("foreign symlink: exists=%v owned=%v err=%v", exists, owned, err)
	}
	delete(fs.symlinks, dest)

	// Real marker-owned copy-mode directory → owned.
	fs.dirs[dest] = true
	fs.files[filepath.Join(dest, "SKILL.md")] = []byte(injectOwnershipMarker("---\nname: pop-tmux-pane\n---\nbody"))
	if exists, owned, err := ownership(d, dest, integrationsRoot); err != nil || !exists || !owned {
		t.Fatalf("marker copy dir: exists=%v owned=%v err=%v", exists, owned, err)
	}
	delete(fs.dirs, dest)
	delete(fs.files, filepath.Join(dest, "SKILL.md"))

	// Real pop- prefixed directory without marker → not owned.
	fs.dirs[dest] = true
	if exists, owned, err := ownership(d, dest, integrationsRoot); err != nil || !exists || owned {
		t.Fatalf("legacy pop- copy dir without marker: exists=%v owned=%v err=%v", exists, owned, err)
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
		name   string
		prefix string
		want   []string
	}{
		{"pop-tmux-pane", "pop-", []string{"pop-tmux-pane", "tmux-pane"}},
		{"pop-tmux-pane.md", "pop-", []string{"pop-tmux-pane.md", "tmux-pane.md"}},
		{"pop-grill-with-docs", "pop-", []string{"pop-grill-with-docs", "grill-with-docs"}},
		{"no-prefix", "pop-", []string{"no-prefix"}},
		{"tmux-pane", "", []string{"tmux-pane"}},
	}
	for _, tt := range tests {
		got := conflictCandidates(tt.name, tt.prefix)
		if len(got) != len(tt.want) {
			t.Fatalf("conflictCandidates(%q, %q) = %v, want %v", tt.name, tt.prefix, got, tt.want)
		}
		for i := range got {
			if got[i] != tt.want[i] {
				t.Fatalf("conflictCandidates(%q, %q) = %v, want %v", tt.name, tt.prefix, got, tt.want)
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
	// name (the prefix-stripped form of pop's pop-tmux-pane).
	bareDest := filepath.Join(filepath.Dir(linkDest), "tmux-pane")
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
// name (`pop-tmux-pane`) both block the install. The bare-name conflict is a user's
// real directory; the prefixed-name conflict is a foreign symlink (a real
// pop- entry is a pop-owned copy-mode install, eligible for migration, not a
// conflict).
func TestInstallFileComponentConflictPrefixInsensitive(t *testing.T) {
	_, linkDest, _ := paneSkillPaths()
	agentDir := filepath.Dir(linkDest)
	bareDest := filepath.Join(agentDir, "tmux-pane")

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
			fs.symlinks[linkDest] = "/somewhere/else/pop-tmux-pane"
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

// captureLogf returns a logf func and a getter for the accumulated lines.
func captureLogf() (logf func(string, ...any), lines func() []string) {
	var captured []string
	logf = func(format string, args ...any) {
		captured = append(captured, fmt.Sprintf(format, args...))
	}
	lines = func() []string { return captured }
	return logf, lines
}

// hasLog reports whether any captured line contains substr.
func hasLog(lines []string, substr string) bool {
	for _, l := range lines {
		if strings.Contains(l, substr) {
			return true
		}
	}
	return false
}

// TestInstallFileComponentDebugInstall asserts that a clean install emits debug
// lines naming the component/agent and the link action taken.
func TestInstallFileComponentDebugInstall(t *testing.T) {
	fs := newFakeFS()
	d := fakeDeps(installerHome, fs, nil)
	logf, lines := captureLogf()
	d.logf = logf

	if err := installFileComponent(d, installerHome, ComponentPaneSkill, "claude"); err != nil {
		t.Fatalf("installFileComponent: %v", err)
	}

	got := lines()
	if !hasLog(got, "agent=claude") {
		t.Errorf("expected agent=claude in debug lines, got %v", got)
	}
	if !hasLog(got, string(ComponentPaneSkill)) {
		t.Errorf("expected component id %q in debug lines, got %v", ComponentPaneSkill, got)
	}
	_, linkDest, linkTarget := paneSkillPaths()
	wantLink := fmt.Sprintf("linked %s -> %s", linkDest, linkTarget)
	if !hasLog(got, wantLink) {
		t.Errorf("expected link action %q in debug lines, got %v", wantLink, got)
	}
}

// TestInstallFileComponentDebugConflict asserts that a conflict emits a debug
// line naming the conflicting path and stating it is not owned by pop.
func TestInstallFileComponentDebugConflict(t *testing.T) {
	fs := newFakeFS()
	d := fakeDeps(installerHome, fs, nil)
	logf, lines := captureLogf()
	d.logf = logf

	_, linkDest, _ := paneSkillPaths()
	// User's own skill at the bare name — not owned by pop.
	bareDest := filepath.Join(filepath.Dir(linkDest), "tmux-pane")
	fs.dirs[bareDest] = true
	fs.files[filepath.Join(bareDest, "SKILL.md")] = []byte("mine")

	if err := installFileComponent(d, installerHome, ComponentPaneSkill, "claude"); err != nil {
		t.Fatalf("installFileComponent: %v", err)
	}

	got := lines()
	if !hasLog(got, bareDest) {
		t.Errorf("expected conflicting path %q in debug lines, got %v", bareDest, got)
	}
	if !hasLog(got, "not owned by pop") {
		t.Errorf("expected 'not owned by pop' reason in debug lines, got %v", got)
	}
}

// TestRefreshFileComponentDebugNotInstalled asserts that refresh logs a
// "not installed — adding" line and installs when the component is absent.
func TestRefreshFileComponentDebugNotInstalled(t *testing.T) {
	fs := newFakeFS()
	logf, lines := captureLogf()
	dry := func() *integrateDeps {
		d := withDryRun(fakeDeps("/h", fs, nil))
		d.logf = logf
		return d
	}
	real := func() *integrateDeps { return fakeDeps("/h", fs, nil) }

	outcome, warning := refreshFileComponent(dry, real, "claude", ComponentPaneSkill)
	if outcome == nil || outcome.Label != "added" {
		t.Errorf("expected added outcome, got outcome=%v warning=%q", outcome, warning)
	}
	if warning != "" {
		t.Errorf("unexpected warning: %q", warning)
	}
	got := lines()
	if !hasLog(got, "not installed") {
		t.Errorf("expected 'not installed' in debug lines, got %v", got)
	}
	if !hasLog(got, "adding") {
		t.Errorf("expected 'adding' in debug lines, got %v", got)
	}
}

// TestRefreshFileComponentDebugCurrent asserts that refresh logs a
// "current — no-op" line when the component is installed and up to date.
func TestRefreshFileComponentDebugCurrent(t *testing.T) {
	// First install the component for real so the render tree matches.
	fs := newFakeFS()
	realDeps := fakeDeps("/h", fs, nil)
	if err := installFileComponent(realDeps, "/h", ComponentPaneSkill, "claude"); err != nil {
		t.Fatalf("setup install: %v", err)
	}

	logf, lines := captureLogf()
	dry := func() *integrateDeps {
		d := withDryRun(fakeDeps("/h", fs, nil))
		d.logf = logf
		return d
	}
	real := func() *integrateDeps { return fakeDeps("/h", fs, nil) }

	outcome, warning := refreshFileComponent(dry, real, "claude", ComponentPaneSkill)
	updated := outcome != nil && (outcome.Label == "updated" || outcome.Label == "added")
	if updated || warning != "" {
		t.Errorf("expected no update/warning, got outcome=%v warning=%q", outcome, warning)
	}
	got := lines()
	if !hasLog(got, "current") {
		t.Errorf("expected 'current' in debug lines, got %v", got)
	}
}

// TestRefreshFileComponentDebugStale asserts that refresh logs "stale —
// refreshing" and "refreshed" when the component is installed but outdated.
func TestRefreshFileComponentDebugStale(t *testing.T) {
	fs := newFakeFS()
	// Install the component so the symlink is present (marks it as installed),
	// then corrupt the render tree to make it stale.
	realDeps := fakeDeps("/h", fs, nil)
	if err := installFileComponent(realDeps, "/h", ComponentPaneSkill, "claude"); err != nil {
		t.Fatalf("setup install: %v", err)
	}
	renderRoot := filepath.Join("/h", ".local", "share", "pop", "integrations", "claude", "pane-skill")
	for k := range fs.files {
		if strings.HasPrefix(k, renderRoot) {
			fs.files[k] = []byte("stale content")
		}
	}

	logf, lines := captureLogf()
	dry := func() *integrateDeps {
		d := withDryRun(fakeDeps("/h", fs, nil))
		d.logf = logf
		return d
	}
	real := func() *integrateDeps { return fakeDeps("/h", fs, nil) }

	outcome, warning := refreshFileComponent(dry, real, "claude", ComponentPaneSkill)
	if outcome == nil || outcome.Label != "updated" {
		t.Errorf("expected component to be refreshed, got outcome=%v", outcome)
	}
	if warning != "" {
		t.Errorf("unexpected warning: %q", warning)
	}
	got := lines()
	if !hasLog(got, "stale") {
		t.Errorf("expected 'stale' in debug lines, got %v", got)
	}
	if !hasLog(got, "refreshed") {
		t.Errorf("expected 'refreshed' in debug lines, got %v", got)
	}
}

// TestRefreshFileComponentDebugConflict asserts that refresh logs the conflict
// path and reason when an unowned entry shadows pop's skill.
func TestRefreshFileComponentDebugConflict(t *testing.T) {
	fs := newFakeFS()
	// Place a non-pop entry at the bare name to create a conflict.
	conflict := filepath.Join("/h", ".claude", "skills", "tmux-pane")
	fs.dirs[conflict] = true

	logf, lines := captureLogf()
	dry := func() *integrateDeps {
		d := withDryRun(fakeDeps("/h", fs, nil))
		d.logf = logf
		return d
	}
	real := func() *integrateDeps { return fakeDeps("/h", fs, nil) }

	outcome, warning := refreshFileComponent(dry, real, "claude", ComponentPaneSkill)
	updated := outcome != nil && (outcome.Label == "updated" || outcome.Label == "added")
	if updated || warning != "" {
		t.Errorf("expected no update/warning on conflict, got outcome=%v warning=%q", outcome, warning)
	}
	got := lines()
	if !hasLog(got, conflict) {
		t.Errorf("expected conflict path %q in debug lines, got %v", conflict, got)
	}
	if !hasLog(got, "not owned by pop") {
		t.Errorf("expected 'not owned by pop' in debug lines, got %v", got)
	}
}
