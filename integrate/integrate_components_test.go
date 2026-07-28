package integrate

import (
	"io"
	"path/filepath"
	"testing"
)

// defaultIntegrationBaseline returns the embedded pop default optional components.
func defaultIntegrationBaseline() []ComponentID {
	return []ComponentID{ComponentTaskSkills, ComponentPaneSkill}
}

// TestRunIntegrateComponentsBareInstallsMergedBaseline: bare integrate installs
// the core status wiring plus every component in the merged baseline.
func TestRunIntegrateComponentsBareInstallsMergedBaseline(t *testing.T) {
	t.Parallel()
	fs := newFakeFS()
	d := fakeDeps(installerHome, fs, nil)

	_, err := installReq(d, fullReq("claude", defaultIntegrationBaseline(), nil, false, false, false))
	if err != nil {
		t.Fatalf("RunComponents: %v", err)
	}

	settings := filepath.Join(installerHome, ".claude", "settings.json")
	if _, ok := fs.files[settings]; !ok {
		t.Fatalf("status wiring not installed: %s missing", settings)
	}
	_, linkDest, linkTarget := paneSkillPaths()
	if fs.symlinks[linkDest] != linkTarget {
		t.Fatalf("pane skill not symlinked: %q -> %q", linkDest, fs.symlinks[linkDest])
	}
	grillDest := filepath.Join(installerHome, ".claude", "skills", "pop-grill-with-docs")
	if _, ok := fs.symlinks[grillDest]; !ok {
		t.Fatalf("task skill not symlinked: %s missing", grillDest)
	}
}

// TestRunIntegrateComponentsNonInteractiveBareSucceeds: a non-interactive bare
// run installs the merged baseline without prompting.
func TestRunIntegrateComponentsNonInteractiveBareSucceeds(t *testing.T) {
	t.Parallel()
	fs := newFakeFS()
	d := fakeDeps(installerHome, fs, nil)

	_, err := installReq(d, fullReq("claude", defaultIntegrationBaseline(), nil, false, false, false))
	if err != nil {
		t.Fatalf("expected non-interactive bare integrate to succeed, got: %v", err)
	}

	settings := filepath.Join(installerHome, ".claude", "settings.json")
	if _, ok := fs.files[settings]; !ok {
		t.Fatalf("status wiring not installed: %s missing", settings)
	}
	if len(fs.symlinks) == 0 {
		t.Fatal("expected baseline skill symlinks to be installed")
	}
}

// TestRunIntegrateComponentsInteractiveBareNoWizard: bare interactive integrate
// never runs the wizard — it installs the merged baseline with no prompts.
func TestRunIntegrateComponentsInteractiveBareNoWizard(t *testing.T) {
	t.Parallel()
	fs := newFakeFS()
	d := fakeDeps(installerHome, fs, nil)
	// ConfirmOverwrite left nil — bare integrate must not prompt.

	_, err := installReq(d, fullReq("claude", defaultIntegrationBaseline(), nil, false, false, false))
	if err != nil {
		t.Fatalf("RunComponents: %v", err)
	}

	settings := filepath.Join(installerHome, ".claude", "settings.json")
	if _, ok := fs.files[settings]; !ok {
		t.Fatalf("status wiring not installed: %s missing", settings)
	}
	if len(fs.symlinks) == 0 {
		t.Fatal("expected baseline skill symlinks on interactive bare path")
	}
}

// TestRunIntegrateVariadicAgentsSameBaseline: variadic agents each receive the
// same merged baseline in order.
func TestRunIntegrateVariadicAgentsSameBaseline(t *testing.T) {
	t.Parallel()
	fs := newFakeFS()
	home := "/h"
	baseline := defaultIntegrationBaseline()

	for _, agent := range []string{"claude", "pi"} {
		if _, err := installReq(fakeDeps(home, fs, io.Discard), fullReq(agent, baseline, nil, false, false, false)); err != nil {
			t.Fatalf("RunComponents(%s): %v", agent, err)
		}
	}

	claudeSettings := filepath.Join(home, ".claude", "settings.json")
	if _, ok := fs.files[claudeSettings]; !ok {
		t.Error("claude status wiring not installed")
	}
	piExt := filepath.Join(home, ".pi", "agent", "extensions", "pop-status-sync.ts")
	if _, ok := fs.files[piExt]; !ok {
		t.Error("pi status wiring not installed")
	}
	claudePane := filepath.Join(home, ".claude", "skills", "pop-tmux-pane")
	if _, ok := fs.symlinks[claudePane]; !ok {
		t.Error("claude pane-skill not installed")
	}
}

// TestRunIntegrateComponentsPaneSkillNewAgents: baseline pane-skill installs the
// symlinked pane-skill artifact for pi, cursor, and opencode.
func TestRunIntegrateComponentsPaneSkillNewAgents(t *testing.T) {
	t.Parallel()
	for _, a := range paneSkillAgents() {
		t.Run(a.name, func(t *testing.T) {
			fs := newFakeFS()
			d := fakeDeps(installerHome, fs, nil)

			if _, err := installReq(d, fullReq(a.name, []ComponentID{ComponentPaneSkill}, nil, false, false, false)); err != nil {
				t.Fatalf("RunComponents(%s): %v", a.name, err)
			}
			if fs.symlinks[a.linkDest] != a.linkTarget {
				t.Fatalf("pane skill not symlinked for %s: %q -> %q", a.name, a.linkDest, fs.symlinks[a.linkDest])
			}
		})
	}
}

// TestRunIntegrateComponentsCodexInstallsMergedBaseline: codex hosts every
// baseline skill component — status wiring plus pane and task skills.
func TestRunIntegrateComponentsCodexInstallsMergedBaseline(t *testing.T) {
	t.Parallel()
	fs := newFakeFS()
	d := fakeDeps(installerHome, fs, nil)

	_, err := installReq(d, fullReq("codex", defaultIntegrationBaseline(), nil, false, false, false))
	if err != nil {
		t.Fatalf("RunComponents: %v", err)
	}
	hooksPath := filepath.Join(installerHome, ".codex", "hooks.json")
	if _, ok := fs.files[hooksPath]; !ok {
		t.Fatalf("codex status wiring not installed")
	}
	paneDest := filepath.Join(installerHome, ".codex", "skills", "pop-tmux-pane")
	if _, ok := fs.symlinks[paneDest]; !ok {
		t.Fatalf("codex pane skill not symlinked: %v", fs.symlinks)
	}
	grillDest := filepath.Join(installerHome, ".codex", "skills", "pop-grill-with-docs")
	if _, ok := fs.symlinks[grillDest]; !ok {
		t.Fatalf("codex task skill not symlinked: %v", fs.symlinks)
	}
	if len(fs.symlinks) != 9 { // pane + 8 task skills
		t.Fatalf("expected 9 skill symlinks, got %d: %v", len(fs.symlinks), fs.symlinks)
	}
}

// TestRunIntegrateComponentsOpencodeInstallsMergedBaseline: opencode hosts every
// baseline skill component — status wiring plus pane and task skills.
func TestRunIntegrateComponentsOpencodeInstallsMergedBaseline(t *testing.T) {
	t.Parallel()
	fs := newFakeFS()
	d := fakeDeps(installerHome, fs, nil)

	_, err := installReq(d, fullReq("opencode", defaultIntegrationBaseline(), nil, false, false, false))
	if err != nil {
		t.Fatalf("RunComponents: %v", err)
	}
	pluginPath := filepath.Join(installerHome, ".config", "opencode", "plugins", "pop-status-sync.ts")
	if _, ok := fs.files[pluginPath]; !ok {
		t.Fatalf("opencode status wiring not installed")
	}
	paneDest := filepath.Join(installerHome, ".config", "opencode", "agent", "pop-tmux-pane.md")
	if _, ok := fs.symlinks[paneDest]; !ok {
		t.Fatalf("opencode pane skill not symlinked: %v", fs.symlinks)
	}
	grillDest := filepath.Join(installerHome, ".config", "opencode", "skills", "pop-grill-with-docs")
	if _, ok := fs.symlinks[grillDest]; !ok {
		t.Fatalf("opencode task skill not symlinked: %v", fs.symlinks)
	}
	if len(fs.symlinks) != 9 { // pane + 8 task skills
		t.Fatalf("expected 9 skill symlinks, got %d: %v", len(fs.symlinks), fs.symlinks)
	}
}

// TestRunIntegrateComponentsUnknownAgent: an unknown agent errors.
func TestRunIntegrateComponentsUnknownAgent(t *testing.T) {
	t.Parallel()
	fs := newFakeFS()
	d := fakeDeps(installerHome, fs, nil)
	if _, err := installReq(d, fullReq("bogus", defaultIntegrationBaseline(), nil, false, false, false)); err == nil {
		t.Fatalf("expected error for unknown agent")
	}
}
