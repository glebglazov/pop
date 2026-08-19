package integrate

import (
	"fmt"
	"path/filepath"
	"testing"
)

// taskAgent describes where the task-skills install lands for one
// directory-hosting agent: the render-tree root under pop's data dir and the
// agent's skill directory where the skill symlinks are created.
type taskAgent struct {
	name      string
	renderDir string
	skillDir  string
}

// taskAgents returns the install layout for every agent the task-skills
// component supports (claude, codex, pi, cursor, opencode), derived from
// installerHome.
func taskAgents() []taskAgent {
	dataRoot := filepath.Join(installerHome, ".local", "share", "pop", "integrations")
	return []taskAgent{
		{
			name:      "claude",
			renderDir: filepath.Join(dataRoot, "claude", "task-skills"),
			skillDir:  filepath.Join(installerHome, ".claude", "skills"),
		},
		{
			name:      "codex",
			renderDir: filepath.Join(dataRoot, "codex", "task-skills"),
			skillDir:  filepath.Join(installerHome, ".codex", "skills"),
		},
		{
			name:      "pi",
			renderDir: filepath.Join(dataRoot, "pi", "task-skills"),
			skillDir:  filepath.Join(installerHome, ".pi", "agent", "skills"),
		},
		{
			name:      "cursor",
			renderDir: filepath.Join(dataRoot, "cursor", "task-skills"),
			skillDir:  filepath.Join(installerHome, ".cursor", "skills"),
		},
		{
			name:      "opencode",
			renderDir: filepath.Join(dataRoot, "opencode", "task-skills"),
			skillDir:  filepath.Join(installerHome, ".config", "opencode", "skills"),
		},
	}
}

// taskSkillNames is the set of skill directory names the task-skills
// component installs.
var taskSkillNames = []string{"pop-grilling", "pop-grill-with-docs", "pop-grill-with-map", "pop-grill-consolidate", "pop-domain-modeling", "pop-to-spec", "pop-to-tasks", "pop-wayfinder", "pop-prototype", "pop-research", "pop-setup-matt-pocock-skills", "pop-spend-audit"}

// TestInstallTaskSkillsAllAgents covers the clean install for claude, codex,
// pi, and cursor: every planning skill lands as a render tree under the data
// dir and the agent location receives a symlink per skill. Each skill that has
// companion documents keeps them so its internal references resolve.
func TestInstallTaskSkillsAllAgents(t *testing.T) {
	t.Parallel()
	for _, a := range taskAgents() {
		t.Run(a.name, func(t *testing.T) {
			fs := newFakeFS()
			d := fakeDeps(installerHome, fs, nil)

			if err := installFileComponent(fileRun(d, a.name), installerHome, ComponentTaskSkills, a.name); err != nil {
				t.Fatalf("installFileComponent(%s): %v", a.name, err)
			}

			// One symlink per skill, each resolving into the render tree.
			if len(fs.symlinks) != len(taskSkillNames) {
				t.Fatalf("expected %d symlinks, got %d: %v", len(taskSkillNames), len(fs.symlinks), fs.symlinks)
			}
			for _, skill := range taskSkillNames {
				dest := filepath.Join(a.skillDir, skill)
				wantTarget := filepath.Join(a.renderDir, skill)
				if fs.symlinks[dest] != wantTarget {
					t.Fatalf("symlink %q = %q, want %q", dest, fs.symlinks[dest], wantTarget)
				}
				// Body lands under the render tree.
				body := filepath.Join(a.renderDir, skill, "SKILL.md")
				if _, ok := fs.files[body]; !ok {
					t.Fatalf("skill body not written: %s (have %v)", body, sortedKeys(fs.files))
				}
			}

			// domain-modeling, grill-with-map and prototype companions ride
			// alongside their bodies in the render tree so their relative
			// references resolve.
			for skill, companions := range map[string][]string{
				"pop-grill-with-map":           {"ADR-FORMAT.md", "CONTEXT-FORMAT.md"},
				"pop-domain-modeling":          {"ADR-FORMAT.md", "CONTEXT-FORMAT.md"},
				"pop-prototype":                {"LOGIC.md", "UI.md"},
				"pop-setup-matt-pocock-skills": {"domain.md", "issue-tracker-github.md", "issue-tracker-gitlab.md", "issue-tracker-local.md"},
			} {
				for _, c := range companions {
					p := filepath.Join(a.renderDir, skill, c)
					if _, ok := fs.files[p]; !ok {
						t.Fatalf("companion not written: %s (have %v)", p, sortedKeys(fs.files))
					}
				}
			}
		})
	}
}

// TestInstallTaskSkillsIdempotent covers re-running: the same seven symlinks
// to the same targets, nothing duplicated.
func TestInstallTaskSkillsIdempotent(t *testing.T) {
	t.Parallel()
	for _, a := range taskAgents() {
		t.Run(a.name, func(t *testing.T) {
			fs := newFakeFS()
			d := fakeDeps(installerHome, fs, nil)

			for i := 0; i < 2; i++ {
				if err := installFileComponent(fileRun(d, a.name), installerHome, ComponentTaskSkills, a.name); err != nil {
					t.Fatalf("install pass %d (%s): %v", i, a.name, err)
				}
			}
			if len(fs.symlinks) != len(taskSkillNames) {
				t.Fatalf("expected %d symlinks after re-run, got %d: %v", len(taskSkillNames), len(fs.symlinks), fs.symlinks)
			}
			for _, skill := range taskSkillNames {
				dest := filepath.Join(a.skillDir, skill)
				wantTarget := filepath.Join(a.renderDir, skill)
				if fs.symlinks[dest] != wantTarget {
					t.Fatalf("symlink %q = %q, want %q", dest, fs.symlinks[dest], wantTarget)
				}
			}
		})
	}
}

// TestRunIntegrateTaskSkillsInstallsExactSet covers the command-level path:
// `pop integrate <agent> --task-skills` installs the core status wiring plus
// the seven symlinked planning skills, with no prompting.
func TestRunIntegrateTaskSkillsInstallsExactSet(t *testing.T) {
	t.Parallel()
	for _, a := range taskAgents() {
		t.Run(a.name, func(t *testing.T) {
			fs := newFakeFS()
			d := fakeDeps(installerHome, fs, nil)

			if _, err := installReq(d, fullReq(a.name, []ComponentID{ComponentTaskSkills}, nil, false, false, false)); err != nil {
				t.Fatalf("RunComponents(%s): %v", a.name, err)
			}

			// Core status wiring landed.
			if len(fs.files) == 0 {
				t.Fatalf("status wiring not installed for %s", a.name)
			}
			// All skill symlinks.
			if len(fs.symlinks) != len(taskSkillNames) {
				t.Fatalf("expected %d skill symlinks, got %d: %v", len(taskSkillNames), len(fs.symlinks), fs.symlinks)
			}
			for _, skill := range taskSkillNames {
				dest := filepath.Join(a.skillDir, skill)
				if fs.symlinks[dest] == "" {
					t.Fatalf("missing symlink for %s at %s", skill, dest)
				}
			}
		})
	}
}

// TestInstallTaskSkillsLeftoverOldNameNotBlocking covers the rename seam: a
// pre-existing skill under the old `to-issues` name (bare or `pop-` prefixed),
// which pop does not own, neither blocks the `to-tasks` install nor is deleted.
// The old-name vocabulary differs from the new skill name, so it is not even a
// conflict candidate — all seven current skills install and the leftover stays.
func TestInstallTaskSkillsLeftoverOldNameNotBlocking(t *testing.T) {
	t.Parallel()
	for _, leftover := range []string{"to-issues", "pop-to-issues"} {
		t.Run(leftover, func(t *testing.T) {
			fs := newFakeFS()
			d := fakeDeps(installerHome, fs, nil)

			skillsDir := filepath.Join(installerHome, ".claude", "skills")
			oldDir := filepath.Join(skillsDir, leftover)
			oldBody := filepath.Join(oldDir, "SKILL.md")
			fs.dirs[oldDir] = true
			fs.files[oldBody] = []byte("legacy user skill")

			if err := installFileComponent(fileRun(d, "claude"), installerHome, ComponentTaskSkills, "claude"); err != nil {
				t.Fatalf("installFileComponent: %v", err)
			}

			// All seven current skills install — the leftover blocks nothing.
			if len(fs.symlinks) != len(taskSkillNames) {
				t.Fatalf("expected %d symlinks, got %d: %v", len(taskSkillNames), len(fs.symlinks), fs.symlinks)
			}
			for _, skill := range taskSkillNames {
				if _, linked := fs.symlinks[filepath.Join(skillsDir, skill)]; !linked {
					t.Fatalf("skill %s was not installed: %v", skill, fs.symlinks)
				}
			}

			// The old-name skill pop does not own is left untouched.
			if _, ok := fs.files[oldBody]; !ok {
				t.Fatalf("leftover old-name skill %s was deleted", leftover)
			}
		})
	}
}

// TestInstallTaskSkillsPrunesStaleToPRD covers the to-prd → to-spec rename
// (ADR-0136) as a base-name change within the task-skills component: a machine
// that previously installed `pop-to-prd` (a symlink into this component's render
// root) must, on the next refresh, end up with `pop-to-spec` linked and the
// stale `pop-to-prd` pruned — not both names live. This is the same
// set-subtraction prune path ADR-0063 built for the pane → tmux-pane rename,
// exercised here for the skill this slice renames.
func TestInstallTaskSkillsPrunesStaleToPRD(t *testing.T) {
	t.Parallel()
	for _, a := range taskAgents() {
		t.Run(a.name, func(t *testing.T) {
			fs := newFakeFS()

			// Simulate the prior-binary install: a pop-owned `pop-to-prd`
			// symlink pointing into this component's render root, exactly as an
			// older pop would have created it.
			staleName := "pop-to-prd"
			staleLink := filepath.Join(a.skillDir, staleName)
			staleTarget := filepath.Join(a.renderDir, staleName)
			fs.symlinks[staleLink] = staleTarget

			var logs []string
			d := fakeDeps(installerHome, fs, nil)
			d.logf = func(format string, args ...any) {
				logs = append(logs, fmt.Sprintf(format, args...))
			}

			if err := installFileComponent(fileRun(d, a.name), installerHome, ComponentTaskSkills, a.name); err != nil {
				t.Fatalf("installFileComponent(%s): %v", a.name, err)
			}

			// The renamed skill is linked...
			specLink := filepath.Join(a.skillDir, "pop-to-spec")
			if fs.symlinks[specLink] != filepath.Join(a.renderDir, "pop-to-spec") {
				t.Fatalf("pop-to-spec not linked: %q -> %q", specLink, fs.symlinks[specLink])
			}
			// ...and the stale pop-to-prd is pruned — no duplicate left behind.
			if _, ok := fs.symlinks[staleLink]; ok {
				t.Fatalf("stale pop-to-prd not pruned: %s still -> %q", staleLink, fs.symlinks[staleLink])
			}
			// Exactly the current set survives, no stale extra.
			if len(fs.symlinks) != len(taskSkillNames) {
				t.Fatalf("expected %d symlinks after rename refresh, got %d: %v", len(taskSkillNames), len(fs.symlinks), fs.symlinks)
			}
			if !containsSubstr(logs, "pruning stale "+staleLink) {
				t.Fatalf("expected a prune log line for %s, got: %v", staleLink, logs)
			}
		})
	}
}

// TestTaskSkillsDoctorSeesMissingSkillOrSharedDoc drives the state Doctor
// reports for the task-skills component after the install is damaged five
// ways: the interview primitive's body deleted, the one receiver's copy of the
// shared CONTEXT-FORMAT.md deleted, that same receiver's copy hand-edited, and
// the equivalent two damages done to domain-modeling, which owns the canonical
// documents. Each is a finding — an installed skill missing the format document
// writes drafts by guesswork, and one carrying a drifted copy writes them by
// stale rules. The owner is no more exempt than a receiver.
func TestTaskSkillsDoctorSeesMissingSkillOrSharedDoc(t *testing.T) {
	t.Parallel()
	renderDir := filepath.Join(installerHome, ".local", "share", "pop", "integrations", "claude", "task-skills")
	for _, tc := range []struct {
		name   string
		damage func(fs *fakeFS)
	}{
		{
			name:   "skill body absent",
			damage: func(fs *fakeFS) { delete(fs.files, filepath.Join(renderDir, "pop-grilling", "SKILL.md")) },
		},
		{
			name: "shared doc absent",
			damage: func(fs *fakeFS) {
				delete(fs.files, filepath.Join(renderDir, "pop-grill-with-map", "CONTEXT-FORMAT.md"))
			},
		},
		{
			name: "shared doc drifted",
			damage: func(fs *fakeFS) {
				fs.files[filepath.Join(renderDir, "pop-grill-with-map", "ADR-FORMAT.md")] = []byte("hand-edited format rules")
			},
		},
		{
			name:   "discipline body absent",
			damage: func(fs *fakeFS) { delete(fs.files, filepath.Join(renderDir, "pop-domain-modeling", "SKILL.md")) },
		},
		{
			name: "canonical doc drifted at its owner",
			damage: func(fs *fakeFS) {
				fs.files[filepath.Join(renderDir, "pop-domain-modeling", "ADR-FORMAT.md")] = []byte("hand-edited ADR rules")
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fs := newFakeFS()
			d := fakeDeps(installerHome, fs, nil)
			if err := installFileComponent(fileRun(d, "claude"), installerHome, ComponentTaskSkills, "claude"); err != nil {
				t.Fatalf("installFileComponent: %v", err)
			}
			state, err := ComponentState(d, installerHome, ComponentTaskSkills, "claude")
			if err != nil {
				t.Fatalf("ComponentState (fresh): %v", err)
			}
			if state.Kind != StateInstalledCurrent {
				t.Fatalf("fresh install state = %v, want installed-current", state.Kind)
			}

			tc.damage(fs)

			state, err = ComponentState(d, installerHome, ComponentTaskSkills, "claude")
			if err != nil {
				t.Fatalf("ComponentState (damaged): %v", err)
			}
			if state.Kind != StateStale {
				t.Fatalf("state after %s = %v, want stale", tc.name, state.Kind)
			}
		})
	}
}
