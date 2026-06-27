package cmd

import (
	"bytes"
	"path/filepath"
	"strings"
	"testing"
)

// grillSkillDest is the agent-location symlink a task-skills install lands for
// claude — used as a stand-in for "the task planning skills are installed".
func grillSkillDest() string {
	return filepath.Join(installerHome, ".claude", "skills", "pop-grill-with-docs")
}

// TestRunIntegrateDefaultSetInstallsEverything: a bare `pop integrate <agent>`
// installs the full default set — status wiring, pane skill, and task planning
// skills — with no prompting (ADR 0064).
func TestRunIntegrateDefaultSetInstallsEverything(t *testing.T) {
	fs := newFakeFS()
	d := fakeDeps(installerHome, fs, nil)

	if err := runIntegrateComponents(d, "claude", nil); err != nil {
		t.Fatalf("runIntegrateComponents: %v", err)
	}

	settings := filepath.Join(installerHome, ".claude", "settings.json")
	if _, ok := fs.files[settings]; !ok {
		t.Fatalf("status wiring not installed: %s missing", settings)
	}
	_, paneDest, paneTarget := paneSkillPaths()
	if fs.symlinks[paneDest] != paneTarget {
		t.Fatalf("pane skill not symlinked: %q -> %q", paneDest, fs.symlinks[paneDest])
	}
	if _, ok := fs.symlinks[grillSkillDest()]; !ok {
		t.Fatalf("task planning skills not installed: %s missing (symlinks=%v)", grillSkillDest(), fs.symlinks)
	}
}

// TestRunIntegrateNoPaneSkillExcludesPaneSkill: --no-pane-skill drops the pane
// skill while still installing the status wiring and the task planning skills.
func TestRunIntegrateNoPaneSkillExcludesPaneSkill(t *testing.T) {
	fs := newFakeFS()
	d := fakeDeps(installerHome, fs, nil)

	if err := runIntegrateComponents(d, "claude", []ComponentID{ComponentPaneSkill}); err != nil {
		t.Fatalf("runIntegrateComponents: %v", err)
	}

	settings := filepath.Join(installerHome, ".claude", "settings.json")
	if _, ok := fs.files[settings]; !ok {
		t.Fatalf("status wiring not installed: %s missing", settings)
	}
	_, paneDest, _ := paneSkillPaths()
	if _, ok := fs.symlinks[paneDest]; ok {
		t.Fatalf("pane skill should be excluded by --no-pane-skill, got %q", fs.symlinks[paneDest])
	}
	if _, ok := fs.symlinks[grillSkillDest()]; !ok {
		t.Fatalf("task planning skills should still install: %s missing", grillSkillDest())
	}
}

// TestRunIntegrateNoTaskSkillsExcludesTaskSkills: --no-task-skills drops the
// task planning skills while still installing the status wiring and pane skill.
func TestRunIntegrateNoTaskSkillsExcludesTaskSkills(t *testing.T) {
	fs := newFakeFS()
	d := fakeDeps(installerHome, fs, nil)

	if err := runIntegrateComponents(d, "claude", []ComponentID{ComponentTaskSkills}); err != nil {
		t.Fatalf("runIntegrateComponents: %v", err)
	}

	_, paneDest, paneTarget := paneSkillPaths()
	if fs.symlinks[paneDest] != paneTarget {
		t.Fatalf("pane skill not installed: %q -> %q", paneDest, fs.symlinks[paneDest])
	}
	if _, ok := fs.symlinks[grillSkillDest()]; ok {
		t.Fatalf("task planning skills should be excluded by --no-task-skills, got %q", fs.symlinks[grillSkillDest()])
	}
}

// TestRunIntegrateNoBothExcludesBothSkills: opting out of both skills leaves
// only the core status wiring.
func TestRunIntegrateNoBothExcludesBothSkills(t *testing.T) {
	fs := newFakeFS()
	d := fakeDeps(installerHome, fs, nil)

	if err := runIntegrateComponents(d, "claude",
		[]ComponentID{ComponentPaneSkill, ComponentTaskSkills}); err != nil {
		t.Fatalf("runIntegrateComponents: %v", err)
	}

	settings := filepath.Join(installerHome, ".claude", "settings.json")
	if _, ok := fs.files[settings]; !ok {
		t.Fatalf("status wiring not installed: %s missing", settings)
	}
	if len(fs.symlinks) != 0 {
		t.Fatalf("no skills should be installed when both are opted out, got %v", fs.symlinks)
	}
}

// TestRunIntegrateDefaultSetForNewAgents: the default set installs the pane
// skill (and, where supported, the task planning skills) for pi and cursor
// through the same symlink path.
func TestRunIntegrateDefaultSetForNewAgents(t *testing.T) {
	for _, a := range paneSkillAgents() {
		t.Run(a.name, func(t *testing.T) {
			fs := newFakeFS()
			d := fakeDeps(installerHome, fs, nil)

			if err := runIntegrateComponents(d, a.name, nil); err != nil {
				t.Fatalf("runIntegrateComponents(%s): %v", a.name, err)
			}
			if fs.symlinks[a.linkDest] != a.linkTarget {
				t.Fatalf("pane skill not symlinked for %s: %q -> %q", a.name, a.linkDest, fs.symlinks[a.linkDest])
			}
		})
	}
}

// TestRunIntegrateCodexSkipsUnsupportedSkills: `pop integrate codex` installs
// the status wiring and silently skips both skills (codex hosts neither) — no
// error, no degraded install.
func TestRunIntegrateCodexSkipsUnsupportedSkills(t *testing.T) {
	fs := newFakeFS()
	d := fakeDeps(installerHome, fs, nil)

	if err := runIntegrateComponents(d, "codex", nil); err != nil {
		t.Fatalf("runIntegrateComponents(codex): %v", err)
	}

	hooks := filepath.Join(installerHome, ".codex", "hooks.json")
	if _, ok := fs.files[hooks]; !ok {
		t.Fatalf("status wiring not installed for codex: %s missing", hooks)
	}
	if len(fs.symlinks) != 0 {
		t.Fatalf("no skills should be installed for codex, got %v", fs.symlinks)
	}
}

// TestRunIntegrateConflictSkippedNeverOverwritten: a same-named entry pop does
// not own is skipped, never overwritten — and the rest of the default set still
// installs.
func TestRunIntegrateConflictSkippedNeverOverwritten(t *testing.T) {
	fs := newFakeFS()
	out := &bytes.Buffer{}
	d := fakeDeps(installerHome, fs, out)

	// A hand-written skill under the bare pane name shadows pop's pane skill.
	conflictPath := filepath.Join(installerHome, ".claude", "skills", "tmux-pane")
	fs.files[conflictPath] = []byte("my own skill")

	if err := runIntegrateComponents(d, "claude", nil); err != nil {
		t.Fatalf("runIntegrateComponents: %v", err)
	}

	// The conflicting pane skill is not installed, and the user's file is intact.
	_, paneDest, _ := paneSkillPaths()
	if _, ok := fs.symlinks[paneDest]; ok {
		t.Fatalf("conflicting pane skill must not be installed")
	}
	if string(fs.files[conflictPath]) != "my own skill" {
		t.Fatalf("user's own skill was overwritten: %q", fs.files[conflictPath])
	}
	if !strings.Contains(out.String(), "not owned by pop") {
		t.Fatalf("conflict not reported: %q", out.String())
	}
	// Non-conflicting components still install.
	settings := filepath.Join(installerHome, ".claude", "settings.json")
	if _, ok := fs.files[settings]; !ok {
		t.Fatalf("status wiring not installed despite conflict on pane skill")
	}
	if _, ok := fs.symlinks[grillSkillDest()]; !ok {
		t.Fatalf("task planning skills should install despite pane-skill conflict")
	}
}

// TestRunIntegrateUnknownAgent: an unknown agent errors.
func TestRunIntegrateUnknownAgent(t *testing.T) {
	fs := newFakeFS()
	d := fakeDeps(installerHome, fs, nil)
	if err := runIntegrateComponents(d, "bogus", nil); err == nil {
		t.Fatalf("expected error for unknown agent")
	}
}

// TestRunIntegrateInstallDeprecatedPaneSkillFlag: the positive --pane-skill flag
// is a no-op that prints a deprecation notice — the component installs anyway
// (it is on by default).
func TestRunIntegrateInstallDeprecatedPaneSkillFlag(t *testing.T) {
	fs := newFakeFS()
	out := &bytes.Buffer{}
	d := fakeDeps(installerHome, fs, out)

	if err := runIntegrateInstall(d, "claude", true /*paneSkill*/, false, false, false); err != nil {
		t.Fatalf("runIntegrateInstall: %v", err)
	}
	if !strings.Contains(out.String(), "--pane-skill is deprecated") {
		t.Fatalf("expected pane-skill deprecation notice, output:\n%s", out.String())
	}
	if strings.Contains(out.String(), "--task-skills is deprecated") {
		t.Fatalf("unexpected task-skills deprecation notice, output:\n%s", out.String())
	}
	// No-op: the pane skill still installs (it is on by default).
	if inst, _ := fileComponentInstalled(d, installerHome, ComponentPaneSkill, "claude"); !inst {
		t.Fatalf("pane skill should still install — the flag is a no-op, not a removal")
	}
}

// TestRunIntegrateInstallDeprecatedTaskSkillsFlag: the positive --task-skills
// flag is a no-op that prints a deprecation notice.
func TestRunIntegrateInstallDeprecatedTaskSkillsFlag(t *testing.T) {
	fs := newFakeFS()
	out := &bytes.Buffer{}
	d := fakeDeps(installerHome, fs, out)

	if err := runIntegrateInstall(d, "claude", false, true /*taskSkills*/, false, false); err != nil {
		t.Fatalf("runIntegrateInstall: %v", err)
	}
	if !strings.Contains(out.String(), "--task-skills is deprecated") {
		t.Fatalf("expected task-skills deprecation notice, output:\n%s", out.String())
	}
	if inst, _ := fileComponentInstalled(d, installerHome, ComponentTaskSkills, "claude"); !inst {
		t.Fatalf("task planning skills should still install — the flag is a no-op")
	}
}

// TestRunIntegrateInstallNoFlagsNoDeprecationNotice: a bare run prints no
// deprecation notice.
func TestRunIntegrateInstallNoFlagsNoDeprecationNotice(t *testing.T) {
	fs := newFakeFS()
	out := &bytes.Buffer{}
	d := fakeDeps(installerHome, fs, out)

	if err := runIntegrateInstall(d, "claude", false, false, false, false); err != nil {
		t.Fatalf("runIntegrateInstall: %v", err)
	}
	if strings.Contains(out.String(), "deprecated") {
		t.Fatalf("no deprecation notice expected on a bare run, output:\n%s", out.String())
	}
}

// TestRunIntegrateInstallNoFlagOptsOut: the --no-task-skills opt-out flows
// through runIntegrateInstall to exclude the task planning skills.
func TestRunIntegrateInstallNoFlagOptsOut(t *testing.T) {
	fs := newFakeFS()
	d := fakeDeps(installerHome, fs, nil)

	if err := runIntegrateInstall(d, "claude", false, false, false, true /*noTaskSkills*/); err != nil {
		t.Fatalf("runIntegrateInstall: %v", err)
	}
	if inst, _ := fileComponentInstalled(d, installerHome, ComponentTaskSkills, "claude"); inst {
		t.Fatalf("task planning skills should be excluded by --no-task-skills")
	}
	if inst, _ := fileComponentInstalled(d, installerHome, ComponentPaneSkill, "claude"); !inst {
		t.Fatalf("pane skill should still install when only task skills are opted out")
	}
}
