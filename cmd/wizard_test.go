package cmd

import (
	"bytes"
	"path/filepath"
	"strings"
	"testing"
)

// wizardDeps builds integrate deps wired to a fake FS with scripted stdin and a
// capturing stdout, for driving the interactive wizard.
func wizardDeps(fs *fakeFS, input string, out *bytes.Buffer) *integrateDeps {
	d := fakeDeps(installerHome, fs, out)
	d.stdin = strings.NewReader(input)
	return d
}

// TestWizardCoreInstalledNoPrompt: a bare interactive run installs the core
// status wiring with no prompt (declining everything else), and closes with the
// re-run note. No skills land when every opt-in is declined.
func TestWizardCoreInstalledNoPrompt(t *testing.T) {
	fs := newFakeFS()
	out := &bytes.Buffer{}
	// Decline pane skill and task skills.
	d := wizardDeps(fs, "n\nn\n", out)

	if err := runIntegrateComponents(d, "claude", nil, true); err != nil {
		t.Fatalf("wizard: %v", err)
	}

	settings := filepath.Join(installerHome, ".claude", "settings.json")
	if _, ok := fs.files[settings]; !ok {
		t.Fatalf("core status wiring not installed: %s missing", settings)
	}
	if len(fs.symlinks) != 0 {
		t.Fatalf("no skill should be installed when declined, got %v", fs.symlinks)
	}
	s := out.String()
	if !strings.Contains(s, "status wiring") {
		t.Fatalf("core step not explained, output:\n%s", s)
	}
	if !strings.Contains(s, "Re-run `pop integrate claude`") {
		t.Fatalf("missing closing re-run note, output:\n%s", s)
	}
}

// TestWizardAcceptPaneSkill: answering yes to the pane-skill step installs the
// symlinked pane skill; declining the rest leaves them out.
func TestWizardAcceptPaneSkill(t *testing.T) {
	fs := newFakeFS()
	out := &bytes.Buffer{}
	d := wizardDeps(fs, "y\nn\n", out)

	if err := runIntegrateComponents(d, "claude", nil, true); err != nil {
		t.Fatalf("wizard: %v", err)
	}

	_, linkDest, linkTarget := paneSkillPaths()
	if fs.symlinks[linkDest] != linkTarget {
		t.Fatalf("pane skill not symlinked: %q -> %q", linkDest, fs.symlinks[linkDest])
	}
}

// TestWizardAcceptAll: answering yes to every step installs the pane skill and
// the task skills.
func TestWizardAcceptAll(t *testing.T) {
	fs := newFakeFS()
	out := &bytes.Buffer{}
	d := wizardDeps(fs, "y\ny\n", out)

	if err := runIntegrateComponents(d, "claude", nil, true); err != nil {
		t.Fatalf("wizard: %v", err)
	}

	_, paneDest, paneTarget := paneSkillPaths()
	if fs.symlinks[paneDest] != paneTarget {
		t.Fatalf("pane skill not symlinked: %q -> %q", paneDest, fs.symlinks[paneDest])
	}
	grillDest := filepath.Join(installerHome, ".claude", "skills", "pop-grill-with-docs")
	if _, ok := fs.symlinks[grillDest]; !ok {
		t.Fatalf("task skill not symlinked: %s missing (symlinks=%v)", grillDest, fs.symlinks)
	}
}

// TestWizardDeclineContinues: declining a step does not abort the wizard — the
// flow continues to the next component and completes with the re-run note.
func TestWizardDeclineContinues(t *testing.T) {
	fs := newFakeFS()
	out := &bytes.Buffer{}
	// Decline pane skill, accept task skills.
	d := wizardDeps(fs, "n\ny\n", out)

	if err := runIntegrateComponents(d, "claude", nil, true); err != nil {
		t.Fatalf("wizard: %v", err)
	}

	_, paneDest, _ := paneSkillPaths()
	if _, ok := fs.symlinks[paneDest]; ok {
		t.Fatalf("declined pane skill should not be installed")
	}
	grillDest := filepath.Join(installerHome, ".claude", "skills", "pop-grill-with-docs")
	if _, ok := fs.symlinks[grillDest]; !ok {
		t.Fatalf("accepted task skill should be installed after a prior decline")
	}
	if !strings.Contains(out.String(), "Re-run `pop integrate claude`") {
		t.Fatalf("wizard did not complete with re-run note after a decline")
	}
}

// TestWizardStateDisplayInstalledCurrent: a re-run after a prior install reports
// the component as installed and up to date before prompting.
func TestWizardStateDisplayInstalledCurrent(t *testing.T) {
	fs := newFakeFS()
	// First install the pane skill non-interactively.
	pre := fakeDeps(installerHome, fs, nil)
	if err := installFileComponent(pre, installerHome, ComponentPaneSkill, "claude"); err != nil {
		t.Fatalf("pre-install: %v", err)
	}

	out := &bytes.Buffer{}
	d := wizardDeps(fs, "n\nn\n", out)
	if err := runIntegrateComponents(d, "claude", nil, true); err != nil {
		t.Fatalf("wizard: %v", err)
	}
	if !strings.Contains(out.String(), "installed and up to date") {
		t.Fatalf("expected installed-current state display, output:\n%s", out.String())
	}
}

// TestWizardStateDisplayNotInstalled: a fresh run reports each opt-in as not
// installed before prompting.
func TestWizardStateDisplayNotInstalled(t *testing.T) {
	fs := newFakeFS()
	out := &bytes.Buffer{}
	d := wizardDeps(fs, "n\nn\n", out)
	if err := runIntegrateComponents(d, "claude", nil, true); err != nil {
		t.Fatalf("wizard: %v", err)
	}
	if !strings.Contains(out.String(), "not installed") {
		t.Fatalf("expected not-installed state display, output:\n%s", out.String())
	}
}

// TestWizardConflictReportNoPrompt: a same-named entry pop does not own makes
// the step print the conflict report (path plus remove-and-re-run guidance) and
// install nothing — no prompt is taskd.
func TestWizardConflictReportNoPrompt(t *testing.T) {
	fs := newFakeFS()
	// A hand-written skill under the bare name shadows pop's pane skill.
	conflictPath := filepath.Join(installerHome, ".claude", "skills", "tmux-pane")
	fs.files[conflictPath] = []byte("my own skill")

	out := &bytes.Buffer{}
	// Only the task-skills step should consume input; the pane-skill step
	// is a conflict and must not read a prompt.
	d := wizardDeps(fs, "n\n", out)
	if err := runIntegrateComponents(d, "claude", nil, true); err != nil {
		t.Fatalf("wizard: %v", err)
	}

	s := out.String()
	if !strings.Contains(s, "Conflict") || !strings.Contains(s, conflictPath) {
		t.Fatalf("expected conflict report naming %s, output:\n%s", conflictPath, s)
	}
	if !strings.Contains(s, "re-run") {
		t.Fatalf("conflict report should explain re-run resolution, output:\n%s", s)
	}
	_, paneDest, _ := paneSkillPaths()
	if _, ok := fs.symlinks[paneDest]; ok {
		t.Fatalf("conflicting pane skill must not be installed")
	}
	// The user's own file is left untouched.
	if string(fs.files[conflictPath]) != "my own skill" {
		t.Fatalf("user's own skill was modified")
	}
}

// TestWizardNotSupportedReport: on opencode (which cannot host the task
// planning skills) the task step reports not-supported instead of prompting.
func TestWizardNotSupportedReport(t *testing.T) {
	fs := newFakeFS()
	out := &bytes.Buffer{}
	// opencode supports the pane skill (1 prompt); the task step is
	// not-supported and issues no prompt.
	d := wizardDeps(fs, "n\n", out)
	if err := runIntegrateComponents(d, "opencode", nil, true); err != nil {
		t.Fatalf("wizard: %v", err)
	}
	if !strings.Contains(out.String(), "Not supported for opencode") {
		t.Fatalf("expected not-supported report for opencode task skills, output:\n%s", out.String())
	}
}

// TestWizardNonInteractiveNoFlagsStillFails: the wizard path is gated on
// interactivity — a non-interactive bare run still fails loudly and installs
// nothing (unchanged flag contract).
func TestWizardNonInteractiveNoFlagsStillFails(t *testing.T) {
	fs := newFakeFS()
	d := fakeDeps(installerHome, fs, nil)
	if err := runIntegrateComponents(d, "claude", nil, false); err == nil {
		t.Fatalf("expected non-interactive no-flags run to fail")
	}
	if len(fs.files) != 0 || len(fs.symlinks) != 0 {
		t.Fatalf("nothing should be installed: files=%v symlinks=%v", sortedKeys(fs.files), fs.symlinks)
	}
}
