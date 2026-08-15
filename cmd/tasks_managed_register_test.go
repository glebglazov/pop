package cmd

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/glebglazov/pop/config"
)

// Managed provisioning and the Map's session root both resolve their Trunk
// worktree through resolveManagedTrunk, and both `--trunk` flags — on `tasks
// register --managed` and on `tasks bind-worktree --managed` — write through it
// too. This pins the whole of it on the path form of ADR-0212 decision 3: a
// declaration keyed by *another* worktree is honoured, the flag states a path
// rather than a flag on a checkout, and the Map reads back what the flag stated.
func TestManagedTrunkResolvesAndStatesThePathForm(t *testing.T) {
	seed := t.TempDir()
	initGitRepoWithCommitCmd(t, seed)
	bare := filepath.Join(t.TempDir(), "bare.git")
	if out, err := exec.Command("git", "clone", "--bare", seed, bare).CombinedOutput(); err != nil {
		t.Fatalf("clone --bare: %v\n%s", err, out)
	}
	root := t.TempDir()
	trunk := filepath.Join(root, "trunk")
	other := filepath.Join(root, "other")
	for _, wt := range []struct{ branch, path string }{{"trunk-branch", trunk}, {"side-branch", other}} {
		if out, err := exec.Command("git", "-C", bare, "worktree", "add", "-b", wt.branch, wt.path).CombinedOutput(); err != nil {
			t.Fatalf("worktree add %s: %v\n%s", wt.branch, err, out)
		}
	}

	xdg := filepath.Join(t.TempDir(), "xdg")
	cd := newTestCmdDeps(t, other, xdg, xdg)
	setCmdLayerDeps(t, cd)
	td := cd.tasksDeps()
	resetTaskFlags()
	t.Cleanup(resetTaskFlags)

	// A block keyed by the worktree we are standing in states that another
	// checkout is the trunk — which the retired boolean could never say.
	declared := &config.Config{Repo: map[string]config.RepoOverrideConfig{
		other: {Trunk: trunkPtr(trunk)},
	}}
	got, err := resolveManagedTrunk(td, declared, other, "")
	if err != nil {
		t.Fatalf("resolve declared trunk: %v", err)
	}
	if realPath(t, got) != realPath(t, trunk) {
		t.Fatalf("declared trunk = %q, want %s", got, trunk)
	}

	// --trunk states the same fact for a repository that declares nothing.
	got, err = resolveManagedTrunk(td, nil, other, trunk)
	if err != nil {
		t.Fatalf("--trunk: %v", err)
	}
	if realPath(t, got) != realPath(t, trunk) {
		t.Fatalf("--trunk resolved %q, want %s", got, trunk)
	}
	stated, err := os.ReadFile(filepath.Join(xdg, "pop", "config.override.toml"))
	if err != nil {
		t.Fatalf("read override config: %v", err)
	}
	if !strings.Contains(string(stated), "trunk = ") || !strings.Contains(string(stated), realPath(t, trunk)) {
		t.Fatalf("--trunk did not state the trunk path %q:\n%s", trunk, stated)
	}
	if strings.Contains(string(stated), "trunk = true") {
		t.Fatalf("--trunk wrote the retired boolean spelling:\n%s", stated)
	}

	// The Map roots its session at the same answer, with no flag of its own.
	rooted, err := resolveMapTrunk()
	if err != nil {
		t.Fatalf("map trunk: %v", err)
	}
	if realPath(t, rooted) != realPath(t, trunk) {
		t.Fatalf("map session root = %q, want %s", rooted, trunk)
	}
}
