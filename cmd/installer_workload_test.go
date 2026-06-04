package cmd

import (
	"path/filepath"
	"testing"
)

// workloadAgent describes where the workload-skills install lands for one
// directory-hosting agent: the render-tree root under pop's data dir and the
// agent's skill directory where the three skill symlinks are created.
type workloadAgent struct {
	name      string
	renderDir string
	skillDir  string
}

// workloadAgents returns the install layout for every agent the workload-skills
// component supports (claude, pi, cursor), derived from installerHome.
func workloadAgents() []workloadAgent {
	dataRoot := filepath.Join(installerHome, ".local", "share", "pop", "integrations")
	return []workloadAgent{
		{
			name:      "claude",
			renderDir: filepath.Join(dataRoot, "claude", "workload-skills"),
			skillDir:  filepath.Join(installerHome, ".claude", "skills"),
		},
		{
			name:      "pi",
			renderDir: filepath.Join(dataRoot, "pi", "workload-skills"),
			skillDir:  filepath.Join(installerHome, ".pi", "agent", "skills"),
		},
		{
			name:      "cursor",
			renderDir: filepath.Join(dataRoot, "cursor", "workload-skills"),
			skillDir:  filepath.Join(installerHome, ".cursor", "skills"),
		},
	}
}

// workloadSkillNames is the set of skill directory names the workload-skills
// component installs.
var workloadSkillNames = []string{"pop-grill-with-docs", "pop-to-prd", "pop-to-issues"}

// TestInstallWorkloadSkillsAllAgents covers the clean install for claude, pi,
// and cursor: all three planning skills land as render trees under the data dir
// and the agent location receives a symlink per skill. grill-with-docs keeps its
// companion documents so its internal references resolve.
func TestInstallWorkloadSkillsAllAgents(t *testing.T) {
	for _, a := range workloadAgents() {
		t.Run(a.name, func(t *testing.T) {
			fs := newFakeFS()
			d := fakeDeps(installerHome, fs, nil)

			if err := installFileComponent(d, installerHome, ComponentWorkloadSkills, a.name); err != nil {
				t.Fatalf("installFileComponent(%s): %v", a.name, err)
			}

			// One symlink per skill, each resolving into the render tree.
			if len(fs.symlinks) != len(workloadSkillNames) {
				t.Fatalf("expected %d symlinks, got %d: %v", len(workloadSkillNames), len(fs.symlinks), fs.symlinks)
			}
			for _, skill := range workloadSkillNames {
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

// TestInstallWorkloadSkillsIdempotent covers re-running: the same three symlinks
// to the same targets, nothing duplicated.
func TestInstallWorkloadSkillsIdempotent(t *testing.T) {
	for _, a := range workloadAgents() {
		t.Run(a.name, func(t *testing.T) {
			fs := newFakeFS()
			d := fakeDeps(installerHome, fs, nil)

			for i := 0; i < 2; i++ {
				if err := installFileComponent(d, installerHome, ComponentWorkloadSkills, a.name); err != nil {
					t.Fatalf("install pass %d (%s): %v", i, a.name, err)
				}
			}
			if len(fs.symlinks) != len(workloadSkillNames) {
				t.Fatalf("expected %d symlinks after re-run, got %d: %v", len(workloadSkillNames), len(fs.symlinks), fs.symlinks)
			}
			for _, skill := range workloadSkillNames {
				dest := filepath.Join(a.skillDir, skill)
				wantTarget := filepath.Join(a.renderDir, skill)
				if fs.symlinks[dest] != wantTarget {
					t.Fatalf("symlink %q = %q, want %q", dest, fs.symlinks[dest], wantTarget)
				}
			}
		})
	}
}

// TestRunIntegrateWorkloadSkillsInstallsExactSet covers the command-level path:
// `pop integrate <agent> --workload-skills` installs the core status wiring plus
// the three symlinked planning skills, with no prompting.
func TestRunIntegrateWorkloadSkillsInstallsExactSet(t *testing.T) {
	for _, a := range workloadAgents() {
		t.Run(a.name, func(t *testing.T) {
			fs := newFakeFS()
			d := fakeDeps(installerHome, fs, nil)

			if err := runIntegrateComponents(d, a.name, []ComponentID{ComponentWorkloadSkills}, false); err != nil {
				t.Fatalf("runIntegrateComponents(%s): %v", a.name, err)
			}

			// Core status wiring landed.
			if len(fs.files) == 0 {
				t.Fatalf("status wiring not installed for %s", a.name)
			}
			// Three skill symlinks.
			if len(fs.symlinks) != len(workloadSkillNames) {
				t.Fatalf("expected %d skill symlinks, got %d: %v", len(workloadSkillNames), len(fs.symlinks), fs.symlinks)
			}
			for _, skill := range workloadSkillNames {
				dest := filepath.Join(a.skillDir, skill)
				if fs.symlinks[dest] == "" {
					t.Fatalf("missing symlink for %s at %s", skill, dest)
				}
			}
		})
	}
}

// TestRunIntegrateWorkloadSkillsUnsupported covers opencode and codex: the
// component is reported as not supported and nothing is installed — not even
// the core status wiring.
func TestRunIntegrateWorkloadSkillsUnsupported(t *testing.T) {
	for _, agent := range []string{"opencode", "codex"} {
		t.Run(agent, func(t *testing.T) {
			fs := newFakeFS()
			d := fakeDeps(installerHome, fs, nil)

			err := runIntegrateComponents(d, agent, []ComponentID{ComponentWorkloadSkills}, false)
			if err == nil {
				t.Fatalf("expected not-supported error for %s --workload-skills", agent)
			}
			if len(fs.files) != 0 || len(fs.symlinks) != 0 {
				t.Fatalf("nothing should be installed for %s: files=%v symlinks=%v", agent, sortedKeys(fs.files), fs.symlinks)
			}
		})
	}
}
