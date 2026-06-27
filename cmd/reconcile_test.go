package cmd

import (
	"bytes"
	"fmt"
	"io"
	"path/filepath"
	"testing"
)

// reconcileFactories builds dry/real deps factories over a shared fake FS for
// the reconcile refresh paths, optionally pinning a resolved skill_prefix and
// capturing debug logs. A nil prefix uses the default (`pop-`).
func reconcileFactories(home string, fs *fakeFS, prefix *string, logs *[]string) (dry, real func() *integrateDeps) {
	mk := func() *integrateDeps {
		d := fakeDeps(home, fs, io.Discard)
		if prefix != nil {
			d.skillsPrefix = prefix
		}
		if logs != nil {
			d.logf = func(f string, a ...any) { *logs = append(*logs, fmt.Sprintf(f, a...)) }
		}
		return d
	}
	dry = func() *integrateDeps { return withDryRun(mk()) }
	real = func() *integrateDeps { return mk() }
	return dry, real
}

// seedOldNamePaneSkill installs a pane skill under an arbitrary old name
// (`pop-pane`) directly into the fake FS: the render-tree directory under the
// per-component render root plus the agent-location symlink into it. This is the
// on-disk shape left behind by a prior binary before the base rename to
// `tmux-pane`, which the current sources can no longer render.
func seedOldNamePaneSkill(fs *fakeFS, home, oldName string) (link string) {
	renderRoot := filepath.Join(home, ".local", "share", "pop", "integrations", "claude", "pane-skill")
	renderDir := filepath.Join(renderRoot, oldName)
	fs.dirs[renderDir] = true
	fs.files[filepath.Join(renderDir, "SKILL.md")] =
		[]byte("---\npop-owned: true\nname: " + oldName + "\n---\nold pane body")
	link = filepath.Join(home, ".claude", "skills", oldName)
	fs.symlinks[link] = renderDir
	return link
}

// --- fileComponentStaleResolved unit tests (the three divergence kinds) -------

func TestStaleResolved_NameOnlyDivergence(t *testing.T) {
	// Installed under an old name; the current render resolves to a different
	// name. Stale by resolved-name divergence — content is never consulted.
	fs := newFakeFS()
	d := fakeDeps("/h", fs, io.Discard) // default pop- → renders pop-tmux-pane
	installed := map[string]bool{"pop-pane": true}

	stale, err := fileComponentStaleResolved(d, "/h", ComponentPaneSkill, "claude", installed)
	if err != nil {
		t.Fatalf("fileComponentStaleResolved: %v", err)
	}
	if !stale {
		t.Fatal("expected name-only divergence (pop-pane vs pop-tmux-pane) to read stale")
	}
}

func TestStaleResolved_ContentOnlyDivergence(t *testing.T) {
	// Names match; only the rendered bytes differ — the original staleness,
	// preserved (criterion 3).
	fs := newFakeFS()
	seedFileComponent(t, fs, "/h", ComponentPaneSkill, "claude")
	d := fakeDeps("/h", fs, io.Discard)
	installed := map[string]bool{"pop-tmux-pane": true}

	// Current state: not stale.
	stale, err := fileComponentStaleResolved(d, "/h", ComponentPaneSkill, "claude", installed)
	if err != nil {
		t.Fatalf("fileComponentStaleResolved: %v", err)
	}
	if stale {
		t.Fatal("freshly seeded component must not read stale")
	}

	// Corrupt the rendered bytes — now content-stale, names still match.
	fs.files[claudePaneRenderFile("/h")] = []byte("stale skill body")
	stale, err = fileComponentStaleResolved(d, "/h", ComponentPaneSkill, "claude", installed)
	if err != nil {
		t.Fatalf("fileComponentStaleResolved (corrupted): %v", err)
	}
	if !stale {
		t.Fatal("expected content divergence to read stale")
	}
}

func TestStaleResolved_CombinedDivergence(t *testing.T) {
	// Both the resolved name and the content differ — stale.
	fs := newFakeFS()
	seedOldNamePaneSkill(fs, "/h", "pop-pane")
	d := fakeDeps("/h", fs, io.Discard)
	installed := map[string]bool{"pop-pane": true}

	stale, err := fileComponentStaleResolved(d, "/h", ComponentPaneSkill, "claude", installed)
	if err != nil {
		t.Fatalf("fileComponentStaleResolved: %v", err)
	}
	if !stale {
		t.Fatal("expected combined name+content divergence to read stale")
	}
}

// --- installed-names probe is name-agnostic -----------------------------------

func TestInstalledNames_FindsRenamedEntry(t *testing.T) {
	fs := newFakeFS()
	seedOldNamePaneSkill(fs, "/h", "pop-pane")
	d := fakeDeps("/h", fs, io.Discard)

	names, err := fileComponentInstalledNames(d, "/h", ComponentPaneSkill, "claude")
	if err != nil {
		t.Fatalf("fileComponentInstalledNames: %v", err)
	}
	if !names["pop-pane"] {
		t.Fatalf("expected pop-pane recognised as installed via render-root attribution, got %v", sortedSet(names))
	}
}

// --- end-to-end refresh paths -------------------------------------------------

func TestUpdateExisting_AppliesSkillPrefixChange(t *testing.T) {
	// Criterion 1: a config-only skill_prefix change (pop- → bare) is applied by
	// `pop integrate --update-existing` — the new bare name is linked and the old
	// pop- entry pruned — even though no embedded source byte changed.
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	fs := newFakeFS()
	installViaFake(t, fs, "/h", "claude")
	seedFileComponent(t, fs, "/h", ComponentPaneSkill, "claude") // installs pop-tmux-pane

	popLink := claudePaneLink("/h") // pop-tmux-pane (the current default name)
	if _, ok := fs.symlinks[popLink]; !ok {
		t.Fatalf("setup: expected seeded pop- link %s", popLink)
	}

	bare := ""
	var logs []string
	dry, real := reconcileFactories("/h", fs, &bare, &logs)

	var out, errb bytes.Buffer
	if err := runIntegrateUpdateExistingWith("rev-prefix", dry, real, &out, &errb, false); err != nil {
		t.Fatalf("runIntegrateUpdateExistingWith: %v", err)
	}

	bareLink := filepath.Join("/h", ".claude", "skills", "tmux-pane")
	bareTarget := filepath.Join("/h", ".local", "share", "pop", "integrations", "claude", "pane-skill", "tmux-pane")
	if fs.symlinks[bareLink] != bareTarget {
		t.Fatalf("bare name not linked: %q -> %q (want -> %q)", bareLink, fs.symlinks[bareLink], bareTarget)
	}
	if _, ok := fs.symlinks[popLink]; ok {
		t.Fatalf("old pop- link not pruned: %s still -> %q", popLink, fs.symlinks[popLink])
	}
	if !bytes.Contains(out.Bytes(), []byte("updated")) {
		t.Fatalf("expected update outcome for claude, got %q", out.String())
	}
	// New paths log per slice 01: the resolved-name divergence and the prune.
	if !containsSubstr(logs, "resolved-name divergence") {
		t.Fatalf("expected a resolved-name divergence log line, got: %v", logs)
	}
	if !containsSubstr(logs, "pruning stale "+popLink) {
		t.Fatalf("expected a prune log line for %s, got: %v", popLink, logs)
	}
}

func TestEnsureIntegrations_MigratesBaseRename(t *testing.T) {
	// Criterion 2: a binary release that renames the base (pane → tmux-pane)
	// auto-migrates the installed entry on the next picker launch (the
	// binary-revision-gated ensure path).
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	fs := newFakeFS()
	installViaFake(t, fs, "/h", "claude")
	oldLink := seedOldNamePaneSkill(fs, "/h", "pop-pane")

	var logs []string
	dry, real := reconcileFactories("/h", fs, nil, &logs) // default pop- → renders pop-tmux-pane

	warnings := ensureIntegrationsForRevisionWith("rev-rename", dry, real)
	if warnings != nil {
		t.Fatalf("expected no warnings, got %v", warnings)
	}

	newLink := claudePaneLink("/h") // pop-tmux-pane
	newTarget := filepath.Join("/h", ".local", "share", "pop", "integrations", "claude", "pane-skill", "pop-tmux-pane")
	if fs.symlinks[newLink] != newTarget {
		t.Fatalf("renamed name not linked: %q -> %q (want -> %q)", newLink, fs.symlinks[newLink], newTarget)
	}
	if _, ok := fs.symlinks[oldLink]; ok {
		t.Fatalf("old pop-pane link not pruned: %s still -> %q", oldLink, fs.symlinks[oldLink])
	}
	if _, ok := fs.files[claudePaneRenderFile("/h")]; !ok {
		t.Fatalf("renamed render file missing: %s", claudePaneRenderFile("/h"))
	}
	if got := readStateRevision(t); got != "rev-rename" {
		t.Fatalf("state.json revision = %q, want rev-rename", got)
	}
}

func TestUpdateStale_ContentOnlyChangeStillRefreshes(t *testing.T) {
	// Criterion 3: a content-only change (names unchanged) still re-renders.
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	fs := newFakeFS()
	installViaFake(t, fs, "/h", "claude")
	seedFileComponent(t, fs, "/h", ComponentPaneSkill, "claude")

	renderFile := claudePaneRenderFile("/h")
	want := append([]byte{}, fs.files[renderFile]...)
	fs.files[renderFile] = []byte("stale skill body")

	dry, real := reconcileFactories("/h", fs, nil, nil)
	result := updateStaleIntegrations(dry, real)
	if len(result.Warnings) != 0 {
		t.Fatalf("expected no warnings, got %v", result.Warnings)
	}
	if !bytes.Equal(fs.files[renderFile], want) {
		t.Fatalf("content-only stale render not refreshed:\n got %q\nwant %q", fs.files[renderFile], want)
	}
}

func TestReconcile_SkipsUnownedConflictOnPrefixChange(t *testing.T) {
	// Criterion 4: reconcile never removes an unowned entry. A prefix change to
	// bare would resolve to `tmux-pane`, but a hand-written skill sits there —
	// an unowned conflict. Refresh skips entirely: the user's skill is untouched
	// and the old pop- entry is left in place (no link created, nothing pruned).
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	fs := newFakeFS()
	installViaFake(t, fs, "/h", "claude")
	seedFileComponent(t, fs, "/h", ComponentPaneSkill, "claude") // pop-tmux-pane
	seedFileComponent(t, fs, "/h", ComponentTaskSkills, "claude")
	popLink := claudePaneLink("/h")
	popTarget := filepath.Join("/h", ".local", "share", "pop", "integrations", "claude", "pane-skill", "pop-tmux-pane")

	// A user's own skill at the resolved bare name.
	bareDir := filepath.Join("/h", ".claude", "skills", "tmux-pane")
	fs.dirs[bareDir] = true
	userFile := filepath.Join(bareDir, "SKILL.md")
	fs.files[userFile] = []byte("hand-written skill")

	bare := ""
	dry, real := reconcileFactories("/h", fs, &bare, nil)
	result := updateStaleIntegrations(dry, real)

	// The unowned entry is never touched.
	if string(fs.files[userFile]) != "hand-written skill" {
		t.Fatalf("unowned skill modified by reconcile: %q", fs.files[userFile])
	}
	// No new bare symlink installed (the conflict blocked it).
	if tgt, ok := fs.symlinks[bareDir]; ok {
		t.Fatalf("conflict must not install a symlink at the unowned name, got -> %q", tgt)
	}
	// The pop- owned entry is left in place — reconcile took no destructive action.
	if fs.symlinks[popLink] != popTarget {
		t.Fatalf("pop- link wrongly removed on a skipped conflict: %q -> %q", popLink, fs.symlinks[popLink])
	}
	// pane-skill not reported as updated/added (conflict-blocked).
	for _, o := range result.Outcomes {
		if o.Component == ComponentPaneSkill && (o.Label == "updated" || o.Label == "added") {
			t.Fatalf("pane-skill must not be updated when blocked by unowned bare-name conflict, got %+v", o)
		}
	}
}
