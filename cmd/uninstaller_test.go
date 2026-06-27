package cmd

import (
	"bytes"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
)

// TestRemoveFileComponentOwnedOnly: removing a file-based component deletes the
// pop-owned symlink and its render-tree entries, and leaves nothing behind.
func TestRemoveFileComponentOwnedOnly(t *testing.T) {
	fs := newFakeFS()
	out := &bytes.Buffer{}
	d := fakeDeps(installerHome, fs, out)

	// Install first, then remove.
	if err := installFileComponent(d, installerHome, ComponentPaneSkill, "claude"); err != nil {
		t.Fatalf("install: %v", err)
	}
	renderFile, linkDest, _ := paneSkillPaths()
	if fs.symlinks[linkDest] == "" || fs.files[renderFile] == nil {
		t.Fatalf("precondition: install did not land artifacts")
	}

	if err := removeFileComponent(d, installerHome, ComponentPaneSkill, "claude"); err != nil {
		t.Fatalf("removeFileComponent: %v", err)
	}

	if _, ok := fs.symlinks[linkDest]; ok {
		t.Fatalf("symlink not removed: %s", linkDest)
	}
	if _, ok := fs.files[renderFile]; ok {
		t.Fatalf("render-tree entry not removed: %s", renderFile)
	}
	if !strings.Contains(out.String(), "removed") {
		t.Fatalf("removal not reported: %q", out.String())
	}
}

// TestRemoveFileComponentLeavesNonOwned: a same-named entry pop does not own
// (here a foreign symlink at the canonical location) is left untouched and
// reported, not deleted.
func TestRemoveFileComponentLeavesNonOwned(t *testing.T) {
	fs := newFakeFS()
	out := &bytes.Buffer{}
	d := fakeDeps(installerHome, fs, out)

	_, linkDest, _ := paneSkillPaths()
	foreign := "/somewhere/else/pop-pane"
	fs.symlinks[linkDest] = foreign

	if err := removeFileComponent(d, installerHome, ComponentPaneSkill, "claude"); err != nil {
		t.Fatalf("removeFileComponent: %v", err)
	}

	if fs.symlinks[linkDest] != foreign {
		t.Fatalf("non-owned entry was modified: %q", fs.symlinks[linkDest])
	}
	if !strings.Contains(out.String(), "not owned by pop") || !strings.Contains(out.String(), linkDest) {
		t.Fatalf("non-owned entry not reported: %q", out.String())
	}
}

// TestRemoveStatusWiringStripsPopHooksPreservesOthers covers the claude, codex,
// and cursor JSON formats: pop's hook entries are stripped while an unrelated
// hook is preserved.
func TestRemoveStatusWiringStripsPopHooksPreservesOthers(t *testing.T) {
	cases := []struct {
		agent    string
		path     string
		settings string
		// unrelatedCmd is the command of the hook that must survive.
		unrelatedCmd string
	}{
		{
			agent: "claude",
			path:  filepath.Join(installerHome, ".claude", "settings.json"),
			settings: `{"hooks":{"Stop":[` +
				`{"hooks":[{"type":"command","command":"pop pane set-status unread 2>/dev/null || true"}]},` +
				`{"hooks":[{"type":"command","command":"echo keep-me"}]}` +
				`]}}`,
			unrelatedCmd: "echo keep-me",
		},
		{
			agent: "codex",
			path:  filepath.Join(installerHome, ".codex", "hooks.json"),
			settings: `{"hooks":{"Stop":[` +
				`{"hooks":[{"type":"command","command":"pop pane set-status unread 2>/dev/null || true"}]},` +
				`{"hooks":[{"type":"command","command":"echo keep-me"}]}` +
				`]}}`,
			unrelatedCmd: "echo keep-me",
		},
		{
			agent: "cursor",
			path:  filepath.Join(installerHome, ".cursor", "hooks.json"),
			settings: `{"version":1,"hooks":{"stop":[` +
				`{"command":"pop pane set-status unread --label cursor 2>/dev/null || true"},` +
				`{"command":"echo keep-me"}` +
				`]}}`,
			unrelatedCmd: "echo keep-me",
		},
	}

	for _, tc := range cases {
		t.Run(tc.agent, func(t *testing.T) {
			fs := newFakeFS()
			out := &bytes.Buffer{}
			d := fakeDeps(installerHome, fs, out)
			fs.files[tc.path] = []byte(tc.settings)

			if err := removeStatusWiring(d, installerHome, tc.agent); err != nil {
				t.Fatalf("removeStatusWiring(%s): %v", tc.agent, err)
			}

			body := string(fs.files[tc.path])
			if strings.Contains(body, "pop pane set-status") {
				t.Fatalf("pop hooks not stripped for %s: %q", tc.agent, body)
			}
			if !strings.Contains(body, tc.unrelatedCmd) {
				t.Fatalf("unrelated hook not preserved for %s: %q", tc.agent, body)
			}
			// The stripped file must still be valid JSON.
			var probe map[string]interface{}
			if err := json.Unmarshal(fs.files[tc.path], &probe); err != nil {
				t.Fatalf("stripped %s is not valid JSON: %v", tc.agent, err)
			}
		})
	}
}

// TestRemoveStatusWiringDeletesExtensionFile covers pi and opencode: the
// pop-owned status-sync extension file is removed.
func TestRemoveStatusWiringDeletesExtensionFile(t *testing.T) {
	cases := []struct {
		agent string
		path  string
	}{
		{"pi", filepath.Join(installerHome, ".pi", "agent", "extensions", "pop-status-sync.ts")},
		{"opencode", filepath.Join(installerHome, ".config", "opencode", "plugins", "pop-status-sync.ts")},
	}
	for _, tc := range cases {
		t.Run(tc.agent, func(t *testing.T) {
			fs := newFakeFS()
			d := fakeDeps(installerHome, fs, nil)
			fs.files[tc.path] = []byte("// pop extension")

			if err := removeStatusWiring(d, installerHome, tc.agent); err != nil {
				t.Fatalf("removeStatusWiring(%s): %v", tc.agent, err)
			}
			if _, ok := fs.files[tc.path]; ok {
				t.Fatalf("extension file not removed for %s: %s", tc.agent, tc.path)
			}
		})
	}
}

// TestRemoveStatusWiringNoHooksReportsNothing: stripping a settings file with no
// pop hooks leaves it byte-identical and reports nothing-to-remove.
func TestRemoveStatusWiringNoHooksReportsNothing(t *testing.T) {
	fs := newFakeFS()
	out := &bytes.Buffer{}
	d := fakeDeps(installerHome, fs, out)
	path := filepath.Join(installerHome, ".claude", "settings.json")
	body := `{"hooks":{"Stop":[{"hooks":[{"type":"command","command":"echo keep-me"}]}]}}`
	fs.files[path] = []byte(body)

	if err := removeStatusWiring(d, installerHome, "claude"); err != nil {
		t.Fatalf("removeStatusWiring: %v", err)
	}
	if string(fs.files[path]) != body {
		t.Fatalf("file changed despite no pop hooks: %q", fs.files[path])
	}
	if !strings.Contains(out.String(), "nothing to remove") {
		t.Fatalf("nothing-to-remove not reported: %q", out.String())
	}
}

// TestRunIntegrateRemoveSingleComponent: removing one named component touches
// only that component and leaves the others installed.
func TestRunIntegrateRemoveSingleComponent(t *testing.T) {
	fs := newFakeFS()
	d := fakeDeps(installerHome, fs, nil)

	// Install status wiring + pane skill.
	if err := installComponentSet(d, "claude", []ComponentID{ComponentPaneSkill}); err != nil {
		t.Fatalf("install: %v", err)
	}
	settings := filepath.Join(installerHome, ".claude", "settings.json")
	_, linkDest, _ := paneSkillPaths()

	// Remove only the pane skill.
	if err := runIntegrateRemoveComponents(d, "claude", []ComponentID{ComponentPaneSkill}); err != nil {
		t.Fatalf("remove pane-skill: %v", err)
	}

	if _, ok := fs.symlinks[linkDest]; ok {
		t.Fatalf("pane skill symlink not removed")
	}
	// Status wiring untouched.
	installed, err := statusWiringInstalled(d, installerHome, "claude")
	if err != nil || !installed {
		t.Fatalf("status wiring should remain: installed=%v err=%v", installed, err)
	}
	_ = settings
}

// TestRunIntegrateRemoveSeveralComponents: a multi-identifier remove removes
// exactly the requested set.
func TestRunIntegrateRemoveSeveralComponents(t *testing.T) {
	fs := newFakeFS()
	d := fakeDeps(installerHome, fs, nil)

	if err := installComponentSet(d, "claude",
		[]ComponentID{ComponentPaneSkill, ComponentTaskSkills}); err != nil {
		t.Fatalf("install: %v", err)
	}

	if err := runIntegrateRemoveComponents(d, "claude",
		[]ComponentID{ComponentStatusWiring, ComponentPaneSkill}); err != nil {
		t.Fatalf("remove: %v", err)
	}

	// Status wiring and pane skill gone.
	if inst, _ := statusWiringInstalled(d, installerHome, "claude"); inst {
		t.Fatalf("status wiring should be removed")
	}
	if inst, _ := fileComponentInstalled(d, installerHome, ComponentPaneSkill, "claude"); inst {
		t.Fatalf("pane skill should be removed")
	}
	// Task skills remain (not requested).
	if inst, _ := fileComponentInstalled(d, installerHome, ComponentTaskSkills, "claude"); !inst {
		t.Fatalf("task skills should remain")
	}
}

// TestRunIntegrateRemoveDefaultSet: with no identifiers, the full installed set
// is removed.
func TestRunIntegrateRemoveDefaultSet(t *testing.T) {
	fs := newFakeFS()
	d := fakeDeps(installerHome, fs, nil)

	if err := installComponentSet(d, "claude",
		[]ComponentID{ComponentPaneSkill, ComponentTaskSkills}); err != nil {
		t.Fatalf("install: %v", err)
	}

	if err := runIntegrateRemoveComponents(d, "claude", nil); err != nil {
		t.Fatalf("remove default set: %v", err)
	}

	for _, id := range []ComponentID{ComponentPaneSkill, ComponentTaskSkills} {
		if inst, _ := fileComponentInstalled(d, installerHome, id, "claude"); inst {
			t.Fatalf("component %s should be removed in default set", id)
		}
	}
	if inst, _ := statusWiringInstalled(d, installerHome, "claude"); inst {
		t.Fatalf("status wiring should be removed in default set")
	}
}

// TestRunIntegrateRemoveNothingInstalled: a default-set remove with nothing
// installed reports nothing-to-remove and makes no change.
func TestRunIntegrateRemoveNothingInstalled(t *testing.T) {
	fs := newFakeFS()
	out := &bytes.Buffer{}
	d := fakeDeps(installerHome, fs, out)

	if err := runIntegrateRemoveComponents(d, "claude", nil); err != nil {
		t.Fatalf("remove: %v", err)
	}
	if len(fs.files) != 0 || len(fs.symlinks) != 0 {
		t.Fatalf("nothing should be touched: files=%v symlinks=%v", sortedKeys(fs.files), fs.symlinks)
	}
	if !strings.Contains(out.String(), "nothing to remove") {
		t.Fatalf("nothing-to-remove not reported: %q", out.String())
	}
}

// TestRunIntegrateRemoveUnsupportedComponent: an explicit component the agent
// cannot host errors and removes nothing.
func TestRunIntegrateRemoveUnsupportedComponent(t *testing.T) {
	fs := newFakeFS()
	d := fakeDeps(installerHome, fs, nil)

	err := runIntegrateRemoveComponents(d, "codex", []ComponentID{ComponentPaneSkill})
	if err == nil || !strings.Contains(err.Error(), "not supported") {
		t.Fatalf("expected not-supported error, got: %v", err)
	}
}
