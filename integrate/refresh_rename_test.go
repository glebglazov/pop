package integrate

import (
	"bytes"
	"io"
	"path/filepath"
	"strings"
	"testing"
)

// renameInstalledSkill rewrites a freshly installed skill's artifacts back to
// the name an earlier pop release rendered it under: every render-tree path, the
// agent-location entry that links into it, and the `name:` the body carries.
//
// This is the on-disk shape an upgrade that renames a shipped skill inherits —
// the render root still holds the old name and the agent still hosts it — which
// is what refresh has to reconcile. Segment-wise matching keeps it layout-
// agnostic: it hits `<name>` for a skill directory and `<name>.md` for a
// flat-file one, and never a longer name that merely starts with the same text.
func renameInstalledSkill(fs *fakeFS, currentName, priorName string) {
	rewrite := func(p string) string {
		segs := strings.Split(p, string(filepath.Separator))
		for i, seg := range segs {
			switch seg {
			case currentName:
				segs[i] = priorName
			case currentName + ".md":
				segs[i] = priorName + ".md"
			}
		}
		return strings.Join(segs, string(filepath.Separator))
	}
	for path, data := range fs.files {
		moved := rewrite(path)
		if moved == path {
			continue
		}
		delete(fs.files, path)
		fs.files[moved] = bytes.Replace(data, []byte("name: "+currentName), []byte("name: "+priorName), 1)
	}
	for path := range fs.dirs {
		if moved := rewrite(path); moved != path {
			delete(fs.dirs, path)
			fs.dirs[moved] = true
		}
	}
	for link, target := range fs.symlinks {
		if moved := rewrite(link); moved != link {
			delete(fs.symlinks, link)
			fs.symlinks[moved] = rewrite(target)
		}
	}
}

// pathsContaining lists every fake-FS entry whose path mentions name, across
// render tree, directories, and agent-location links — the whole surface an
// orphaned skill could survive on.
func pathsContaining(fs *fakeFS, name string) []string {
	var found []string
	for p := range fs.dirs {
		if strings.Contains(p, name) {
			found = append(found, p)
		}
	}
	for p := range fs.files {
		if strings.Contains(p, name) {
			found = append(found, p)
		}
	}
	for p := range fs.symlinks {
		if strings.Contains(p, name) {
			found = append(found, p)
		}
	}
	return found
}

// TestRefresh_PrunesRenamedSkill covers the batch-grill-me → grilling rename
// (ADR-0141's amendment) as an upgrade a machine walks into: the previous binary
// installed the interview primitive as `pop-batch-grill-me`, the new one renders
// it as `pop-grilling`, and the revision-gated refresh must be the whole
// migration — new name linked, old name gone from render tree and agent
// location alike, and Doctor reporting installed-current with nothing left to do.
//
// An orphan here is worse than a missing skill: a stale pop-owned entry keeps
// answering invocations with the instructions of a skill pop no longer ships.
//
// Both layouts are exercised. claude hosts skills in its own directory;
// opencode is the non-directory-hosting agent (SkillLayoutFlatFile), so the
// prune must recognise its entries too.
//
// Nothing in integrate/ needed changing for this: the set-subtraction prune
// ADR-0063 built for the pane → tmux-pane rename already covers a rename, since
// ownership is decided by "symlink resolving into this component's render root",
// never by the skill's name. This test names the rename case so a future
// rename inherits the guarantee.
func TestRefresh_PrunesRenamedSkill(t *testing.T) {
	t.Parallel()
	const (
		home        = "/h"
		priorName   = "pop-batch-grill-me"
		currentName = "pop-grilling"
	)
	for _, agent := range []string{"claude", "opencode"} {
		t.Run(agent, func(t *testing.T) {
			t.Parallel()
			setupIntegrateConfigLayer(t)

			fs := newFakeFS()
			installViaFake(t, fs, home, agent)
			seedBaselineComponents(t, fs, home, agent)
			renameInstalledSkill(fs, currentName, priorName)

			profile, ok := LookupProfile(agent)
			if !ok {
				t.Fatalf("no profile for %s", agent)
			}
			probe := fakeDeps(home, fs, io.Discard)
			skillDir := profile.SkillDir(probe, home, ComponentTaskSkills)
			renderRoot := filepath.Join(home, ".local", "share", "pop", "integrations", agent, "task-skills")

			priorLink := filepath.Join(skillDir, priorName)
			if _, ok := fs.symlinks[priorLink]; !ok {
				t.Fatalf("setup: expected the prior-name link %s, have %v", priorLink, fs.symlinks)
			}

			var logs []string
			_, real := reconcileFactories(home, fs, nil, &logs)
			result := updateStaleIntegrations(testConfigDeps(t), real)
			if len(result.Warnings) != 0 {
				t.Fatalf("expected no warnings, got %v", result.Warnings)
			}

			// The renamed skill is installed under its new name...
			currentLink := filepath.Join(skillDir, currentName)
			wantTarget := filepath.Join(renderRoot, currentName)
			if fs.symlinks[currentLink] != wantTarget {
				t.Fatalf("%s not linked: %q -> %q (want -> %q)", currentName, currentLink, fs.symlinks[currentLink], wantTarget)
			}
			if _, ok := fs.files[filepath.Join(wantTarget, "SKILL.md")]; !ok {
				t.Fatalf("renamed skill missing SKILL.md under %s", wantTarget)
			}

			// ...and the old name survives nowhere — no orphan link, no orphan
			// render tree, no manual cleanup left for the user.
			if orphans := pathsContaining(fs, priorName); len(orphans) != 0 {
				t.Fatalf("prior name %s not fully pruned, still at: %v", priorName, orphans)
			}
			if !containsSubstr(logs, "pruning stale "+priorLink) {
				t.Fatalf("expected a prune log line for %s, got: %v", priorLink, logs)
			}

			// Doctor has nothing to report afterwards.
			state, err := ComponentState(fakeDeps(home, fs, io.Discard), home, ComponentTaskSkills, agent)
			if err != nil {
				t.Fatalf("ComponentState: %v", err)
			}
			if state.Kind != StateInstalledCurrent {
				t.Fatalf("state after rename refresh = %v (conflict at %q), want installed-current", state.Kind, state.ConflictPath)
			}
		})
	}
}
