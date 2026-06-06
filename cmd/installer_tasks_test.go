package cmd

import (
	"path/filepath"
	"testing"
)

// taskAgent describes where the task-skills install lands for one
// directory-hosting agent: the render-tree root under pop's data dir and the
// agent's skill directory where the three skill symlinks are created.
type taskAgent struct {
	name      string
	renderDir string
	skillDir  string
}

// taskAgents returns the install layout for every agent the task-skills
// component supports (claude, pi, cursor), derived from installerHome.
func taskAgents() []taskAgent {
	dataRoot := filepath.Join(installerHome, ".local", "share", "pop", "integrations")
	return []taskAgent{
		{
			name:      "claude",
			renderDir: filepath.Join(dataRoot, "claude", "task-skills"),
			skillDir:  filepath.Join(installerHome, ".claude", "skills"),
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
	}
}

// taskSkillNames is the set of skill directory names the task-skills
// component installs.
var taskSkillNames = []string{"pop-grill-with-docs", "pop-to-prd", "pop-to-issues"}

// TestInstallTaskSkillsAllAgents covers the clean install for claude, pi,
// and cursor: all three planning skills land as render trees under the data dir
// and the agent location receives a symlink per skill. grill-with-docs keeps its
// companion documents so its internal references resolve.
func TestInstallTaskSkillsAllAgents(t *testing.T) {
	for _, a := range taskAgents() {
		t.Run(a.name, func(t *testing.T) {
			fs := newFakeFS()
			d := fakeDeps(installerHome, fs, nil)

			if err := installFileComponent(d, installerHome, ComponentTaskSkills, a.name); err != nil {
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

			// grill-with-docs companions ride alongside its body in the render
			// tree so its relative references resolve.
			for _, c := range []string{"ADR-FORMAT.md", "CONTEXT-FORMAT.md"} {
				p := filepath.Join(a.renderDir, "pop-grill-with-docs", c)
				if _, ok := fs.files[p]; !ok {
					t.Fatalf("companion not written: %s (have %v)", p, sortedKeys(fs.files))
				}
			}
		})
	}
}

// TestInstallTaskSkillsIdempotent covers re-running: the same three symlinks
// to the same targets, nothing duplicated.
func TestInstallTaskSkillsIdempotent(t *testing.T) {
	for _, a := range taskAgents() {
		t.Run(a.name, func(t *testing.T) {
			fs := newFakeFS()
			d := fakeDeps(installerHome, fs, nil)

			for i := 0; i < 2; i++ {
				if err := installFileComponent(d, installerHome, ComponentTaskSkills, a.name); err != nil {
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
// the three symlinked planning skills, with no prompting.
func TestRunIntegrateTaskSkillsInstallsExactSet(t *testing.T) {
	for _, a := range taskAgents() {
		t.Run(a.name, func(t *testing.T) {
			fs := newFakeFS()
			d := fakeDeps(installerHome, fs, nil)

			if err := runIntegrateComponents(d, a.name, []ComponentID{ComponentTaskSkills}, false); err != nil {
				t.Fatalf("runIntegrateComponents(%s): %v", a.name, err)
			}

			// Core status wiring landed.
			if len(fs.files) == 0 {
				t.Fatalf("status wiring not installed for %s", a.name)
			}
			// Three skill symlinks.
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

// TestRunIntegrateTaskSkillsUnsupported covers opencode and codex: the
// component is reported as not supported and nothing is installed — not even
// the core status wiring.
func TestRunIntegrateTaskSkillsUnsupported(t *testing.T) {
	for _, agent := range []string{"opencode", "codex"} {
		t.Run(agent, func(t *testing.T) {
			fs := newFakeFS()
			d := fakeDeps(installerHome, fs, nil)

			err := runIntegrateComponents(d, agent, []ComponentID{ComponentTaskSkills}, false)
			if err == nil {
				t.Fatalf("expected not-supported error for %s --task-skills", agent)
			}
			if len(fs.files) != 0 || len(fs.symlinks) != 0 {
				t.Fatalf("nothing should be installed for %s: files=%v symlinks=%v", agent, sortedKeys(fs.files), fs.symlinks)
			}
		})
	}
}
