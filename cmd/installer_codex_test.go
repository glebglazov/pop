package cmd

import (
	"bytes"
	"path/filepath"
	"strings"
	"testing"
)

func codexPaneSkillPaths() (renderFile, linkDest, linkTarget string) {
	renderRoot := filepath.Join(installerHome, ".local", "share", "pop", "integrations", "codex", "pane-skill")
	renderFile = filepath.Join(renderRoot, "pop-tmux-pane", "SKILL.md")
	linkDest = filepath.Join(installerHome, ".codex", "skills", "pop-tmux-pane")
	linkTarget = filepath.Join(renderRoot, "pop-tmux-pane")
	return
}

// TestInstallCodexPaneSkill covers the clean codex pane-skill install: render
// tree under the data dir and a symlink at ~/.codex/skills/pop-tmux-pane.
func TestInstallCodexPaneSkill(t *testing.T) {
	fs := newFakeFS()
	d := fakeDeps(installerHome, fs, nil)

	if err := installFileComponent(d, installerHome, ComponentPaneSkill, "codex"); err != nil {
		t.Fatalf("installFileComponent: %v", err)
	}

	renderFile, linkDest, linkTarget := codexPaneSkillPaths()
	if _, ok := fs.files[renderFile]; !ok {
		t.Fatalf("render file not written: %s", renderFile)
	}
	if fs.symlinks[linkDest] != linkTarget {
		t.Fatalf("symlink %q = %q, want %q", linkDest, fs.symlinks[linkDest], linkTarget)
	}
}

// TestRefreshCodexPaneSkillStale updates codex skill artifacts when the render
// tree drifts from the embedded source.
func TestRefreshCodexPaneSkillStale(t *testing.T) {
	fs := newFakeFS()
	realDeps := fakeDeps(installerHome, fs, nil)
	if err := installFileComponent(realDeps, installerHome, ComponentPaneSkill, "codex"); err != nil {
		t.Fatalf("setup install: %v", err)
	}

	renderFile, linkDest, linkTarget := codexPaneSkillPaths()
	fs.files[renderFile] = []byte("stale skill body")

	dry := func() *integrateDeps { return withDryRun(fakeDeps(installerHome, fs, nil)) }
	real := func() *integrateDeps { return fakeDeps(installerHome, fs, nil) }

	outcome, warning := refreshFileComponent(dry, real, "codex", ComponentPaneSkill)
	if outcome == nil || outcome.Label != "updated" {
		t.Fatalf("expected updated outcome, got outcome=%v warning=%q", outcome, warning)
	}
	if warning != "" {
		t.Fatalf("unexpected warning: %q", warning)
	}
	src, _ := skillFiles.ReadFile("skills/pop/tmux-pane.md")
	want := injectOwnershipMarker(injectFrontmatterName(string(src), "pop-tmux-pane"))
	if string(fs.files[renderFile]) != want {
		t.Fatalf("render tree not refreshed: got %q", fs.files[renderFile])
	}
	if fs.symlinks[linkDest] != linkTarget {
		t.Fatalf("symlink not preserved after refresh: %q", fs.symlinks[linkDest])
	}
}

// TestInstallCodexPaneSkillConflictSkipWithOverwriteHint covers an unowned
// entry blocking pop's pane skill: integrate skips it and names
// --overwrite-conflicts on the explicit path.
func TestInstallCodexPaneSkillConflictSkipWithOverwriteHint(t *testing.T) {
	fs := newFakeFS()
	out := &bytes.Buffer{}
	d := fakeDeps(installerHome, fs, out)
	d.agentName = "codex"

	skillsDir := filepath.Join(installerHome, ".codex", "skills")
	conflictPath := filepath.Join(skillsDir, "tmux-pane")
	fs.dirs[conflictPath] = true
	fs.files[filepath.Join(conflictPath, "SKILL.md")] = []byte("mine")

	if err := installFileComponent(d, installerHome, ComponentPaneSkill, "codex"); err != nil {
		t.Fatalf("installFileComponent: %v", err)
	}

	if _, linked := fs.symlinks[filepath.Join(skillsDir, "pop-tmux-pane")]; linked {
		t.Fatal("conflicting pane skill was installed despite unowned entry")
	}
	got := out.String()
	if !strings.Contains(got, "not owned by pop") {
		t.Fatalf("expected ownership skip message, got %q", got)
	}
	if !strings.Contains(got, "pop integrate codex --overwrite-conflicts") {
		t.Fatalf("expected overwrite-conflicts hint, got %q", got)
	}
}
