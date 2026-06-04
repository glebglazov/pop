package cmd

import (
	"path/filepath"
	"testing"
)

// TestRunIntegrateComponentsFlagsInstallExactSet: explicit component flags
// install the core status wiring plus exactly the requested opt-ins, with no
// prompting and regardless of TTY.
func TestRunIntegrateComponentsFlagsInstallExactSet(t *testing.T) {
	fs := newFakeFS()
	d := fakeDeps(installerHome, fs, nil)

	err := runIntegrateComponents(d, "claude", []ComponentID{ComponentPaneSkill}, false)
	if err != nil {
		t.Fatalf("runIntegrateComponents: %v", err)
	}

	settings := filepath.Join(installerHome, ".claude", "settings.json")
	if _, ok := fs.files[settings]; !ok {
		t.Fatalf("status wiring not installed: %s missing", settings)
	}
	_, linkDest, linkTarget := paneSkillPaths()
	if fs.symlinks[linkDest] != linkTarget {
		t.Fatalf("pane skill not symlinked: %q -> %q", linkDest, fs.symlinks[linkDest])
	}
}

// TestRunIntegrateComponentsNonInteractiveNoFlagsFails: a non-interactive run
// without component flags fails loudly and installs nothing.
func TestRunIntegrateComponentsNonInteractiveNoFlagsFails(t *testing.T) {
	fs := newFakeFS()
	d := fakeDeps(installerHome, fs, nil)

	err := runIntegrateComponents(d, "claude", nil, false)
	if err == nil {
		t.Fatalf("expected error for non-interactive run without flags")
	}
	if len(fs.files) != 0 || len(fs.symlinks) != 0 {
		t.Fatalf("nothing should be installed: files=%v symlinks=%v", sortedKeys(fs.files), fs.symlinks)
	}
}

// TestRunIntegrateComponentsInteractiveNoFlagsWiringOnly: a bare interactive run
// installs only the status wiring (slice-01 behavior until the wizard lands) —
// no skill files.
func TestRunIntegrateComponentsInteractiveNoFlagsWiringOnly(t *testing.T) {
	fs := newFakeFS()
	d := fakeDeps(installerHome, fs, nil)

	err := runIntegrateComponents(d, "claude", nil, true)
	if err != nil {
		t.Fatalf("runIntegrateComponents: %v", err)
	}

	settings := filepath.Join(installerHome, ".claude", "settings.json")
	if _, ok := fs.files[settings]; !ok {
		t.Fatalf("status wiring not installed: %s missing", settings)
	}
	if len(fs.symlinks) != 0 {
		t.Fatalf("no skill should be installed on the bare path, got %v", fs.symlinks)
	}
}

// TestRunIntegrateComponentsUnknownAgent: an unknown agent errors.
func TestRunIntegrateComponentsUnknownAgent(t *testing.T) {
	fs := newFakeFS()
	d := fakeDeps(installerHome, fs, nil)
	if err := runIntegrateComponents(d, "bogus", []ComponentID{ComponentPaneSkill}, false); err == nil {
		t.Fatalf("expected error for unknown agent")
	}
}
