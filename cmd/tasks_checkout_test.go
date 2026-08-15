package cmd

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/glebglazov/pop/config"
	"github.com/glebglazov/pop/tasks"
	"github.com/glebglazov/pop/tasks/binding"
)

// TestTaskCheckoutReportsLocalityAcrossCheckouts pins both output modes over the
// five checkout shapes the routing rule has to tell apart. It uses real git
// repositories rather than a mock: locality is defined as "whatever the drain's
// linked-worktree predicate says", so a fake git would pin the test's opinion
// instead of git's.
func TestTaskCheckoutReportsLocalityAcrossCheckouts(t *testing.T) {
	root := t.TempDir()
	dataHome := filepath.Join(root, "xdg")

	trunk := filepath.Join(root, "trunk")
	if err := os.MkdirAll(trunk, 0o755); err != nil {
		t.Fatal(err)
	}
	initGitRepoWithCommitCmd(t, trunk)

	handmade := filepath.Join(root, "handmade")
	runGitCheckout(t, trunk, "worktree", "add", "-b", "feature", handmade)

	// A managed worktree is just a worktree that lives under pop's managed root,
	// so the fixture provisions it exactly where ManagedWorktreesRoot points for
	// this test's isolated XDG_DATA_HOME.
	managedRoot := binding.ManagedWorktreesRoot(newTestCmdDeps(t, trunk, dataHome, "").tasksDeps())
	if err := os.MkdirAll(managedRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	managed := filepath.Join(managedRoot, "managed-wt")
	runGitCheckout(t, trunk, "worktree", "add", "-b", "managed-branch", managed)

	bareDir := filepath.Join(root, "bare.git")
	runGitCheckout(t, root, "clone", "--bare", trunk, bareDir)
	bareWorktree := filepath.Join(root, "bare-wt")
	runGitCheckout(t, bareDir, "worktree", "add", "-b", "bare-branch", bareWorktree)

	// A config block declaring a checkout as Trunk. Locality must ignore it in
	// both the bare and the non-bare case, because the drain does.
	declared := func(path string) *config.Config {
		return &config.Config{Repo: map[string]config.RepoOverrideConfig{
			realPath(t, path): {Trunk: trunkPtr(realPath(t, path))},
		}}
	}

	tests := []struct {
		name         string
		dir          string
		cfg          *config.Config
		wantLocality string
		wantBare     bool
		wantManaged  bool
		wantTrunk    string // "" means the key must be absent
		wantBranch   string
	}{
		{
			name:         "trunk",
			dir:          trunk,
			wantLocality: "trunk",
			wantTrunk:    realPath(t, trunk),
			wantBranch:   currentBranchOf(t, trunk),
		},
		{
			name:         "hand-made worktree",
			dir:          handmade,
			wantLocality: "worktree",
			wantTrunk:    realPath(t, trunk),
			wantBranch:   "feature",
		},
		{
			name:         "managed worktree",
			dir:          managed,
			wantLocality: "worktree",
			wantManaged:  true,
			wantTrunk:    realPath(t, trunk),
			wantBranch:   "managed-branch",
		},
		{
			name:         "bare repository directory",
			dir:          bareDir,
			wantLocality: "worktree",
			wantBare:     true,
			wantBranch:   currentBranchOf(t, trunk),
		},
		{
			name:         "bare repository worktree declared trunk in config",
			dir:          bareWorktree,
			cfg:          declared(bareWorktree),
			wantLocality: "worktree",
			wantBare:     true,
			wantTrunk:    realPath(t, bareWorktree),
			wantBranch:   "bare-branch",
		},
		{
			name:         "linked worktree declared trunk in config",
			dir:          handmade,
			cfg:          declared(handmade),
			wantLocality: "worktree",
			wantTrunk:    realPath(t, handmade),
			wantBranch:   "feature",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			deps := newTestCmdDeps(t, tt.dir, dataHome, "")

			var scalar bytes.Buffer
			if err := runTaskCheckoutWith(deps.configDeps(), deps.tasksDeps(), tt.cfg, &scalar, tt.dir, false); err != nil {
				t.Fatalf("scalar mode: %v", err)
			}
			if got := scalar.String(); got != tt.wantLocality+"\n" {
				t.Fatalf("scalar output = %q, want %q", got, tt.wantLocality+"\n")
			}

			var raw bytes.Buffer
			if err := runTaskCheckoutWith(deps.configDeps(), deps.tasksDeps(), tt.cfg, &raw, tt.dir, true); err != nil {
				t.Fatalf("json mode: %v", err)
			}
			var got map[string]any
			if err := json.Unmarshal(raw.Bytes(), &got); err != nil {
				t.Fatalf("json output %q: %v", raw.String(), err)
			}

			wantKeys := []string{"path", "locality", "branch", "bare", "managed"}
			if tt.wantTrunk != "" {
				wantKeys = append(wantKeys, "trunk_path")
			}
			if len(got) != len(wantKeys) {
				t.Fatalf("json keys = %v, want exactly %v", keysOf(got), wantKeys)
			}
			for _, k := range wantKeys {
				if _, ok := got[k]; !ok {
					t.Fatalf("json missing %q: %v", k, got)
				}
			}
			if got["locality"] != tt.wantLocality {
				t.Errorf("locality = %v, want %s", got["locality"], tt.wantLocality)
			}
			if got["bare"] != tt.wantBare {
				t.Errorf("bare = %v, want %v", got["bare"], tt.wantBare)
			}
			if got["managed"] != tt.wantManaged {
				t.Errorf("managed = %v, want %v", got["managed"], tt.wantManaged)
			}
			if got["path"] != realPath(t, tt.dir) {
				t.Errorf("path = %v, want %s", got["path"], realPath(t, tt.dir))
			}
			if got["branch"] != tt.wantBranch {
				t.Errorf("branch = %v, want %s", got["branch"], tt.wantBranch)
			}
			if tt.wantTrunk != "" && got["trunk_path"] != tt.wantTrunk {
				t.Errorf("trunk_path = %v, want %s", got["trunk_path"], tt.wantTrunk)
			}
		})
	}
}

// TestTaskCheckoutWorksWithoutRegisteredSets is the precondition the verb exists
// for: a locality probe runs before anything is registered, so it must answer in
// a repository whose task storage has never been written.
func TestTaskCheckoutWorksWithoutRegisteredSets(t *testing.T) {
	root := t.TempDir()
	repo := filepath.Join(root, "virgin")
	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatal(err)
	}
	initGitRepoWithCommitCmd(t, repo)

	deps := newTestCmdDeps(t, repo, filepath.Join(root, "xdg"), "")
	var out bytes.Buffer
	if err := runTaskCheckoutWith(deps.configDeps(), deps.tasksDeps(), nil, &out, repo, false); err != nil {
		t.Fatalf("virgin repo: %v", err)
	}
	if out.String() != "trunk\n" {
		t.Fatalf("output = %q, want %q", out.String(), "trunk\n")
	}
}

func TestTaskCheckoutOutsideGitRepoRefuses(t *testing.T) {
	root := t.TempDir()
	deps := newTestCmdDeps(t, root, filepath.Join(root, "xdg"), "")
	err := runTaskCheckoutWith(deps.configDeps(), deps.tasksDeps(), nil, &bytes.Buffer{}, root, false)
	if err == nil || !strings.Contains(err.Error(), "not inside a git repository") {
		t.Fatalf("error = %v, want a not-a-repository refusal", err)
	}
}

func runGitCheckout(t *testing.T, dir string, args ...string) {
	t.Helper()
	c := exec.Command("git", args...)
	c.Dir = dir
	if out, err := c.CombinedOutput(); err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
	}
}

func currentBranchOf(t *testing.T, dir string) string {
	t.Helper()
	c := exec.Command("git", "branch", "--show-current")
	c.Dir = dir
	out, err := c.CombinedOutput()
	if err != nil {
		t.Fatalf("git branch --show-current: %v\n%s", err, out)
	}
	return strings.TrimSpace(string(out))
}

func keysOf(m map[string]any) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

// TestTaskRegisterFromWorktreeBindsInPlaceAndAutoDrains pins the worktree branch
// of the registration rule end to end: from a non-trunk checkout, --auto-drain
// with no --managed binds the set to that very checkout, provisions nothing, and
// still leaves the set consenting to an unattended drain. The two flags have no
// binary-level tie, and this is the test that keeps it that way.
func TestTaskRegisterFromWorktreeBindsInPlaceAndAutoDrains(t *testing.T) {
	base := t.TempDir()
	trunk := filepath.Join(base, "trunk")
	if err := os.MkdirAll(trunk, 0o755); err != nil {
		t.Fatal(err)
	}
	initGitRepoWithCommitCmd(t, trunk)
	worktree := filepath.Join(base, "feature-wt")
	runGitCheckout(t, trunk, "worktree", "add", "-b", "feature", worktree)

	deps := newTestCmdDeps(t, worktree, filepath.Join(base, ".xdg"), "")
	setCmdLayerDeps(t, deps)
	td := deps.tasksDeps()

	resetTaskFlags()
	t.Cleanup(resetTaskFlags)
	taskRegisterAutoDrain = true

	tasksDir := cmdTasksDir(t, td, worktree)
	writeTaskThoughts(t, tasksDir, "draft")

	origLoad := taskConfigLoad
	taskConfigLoad = func(string) (*config.Config, error) {
		return &config.Config{Projects: []config.ProjectEntry{{Path: trunk}}}, nil
	}
	t.Cleanup(func() { taskConfigLoad = origLoad })

	var out bytes.Buffer
	if err := runTaskRegisterWith(td, &out, ""); err != nil {
		t.Fatalf("register failed: %v", err)
	}

	// The trunk auto-drain warning (ADR-0192) has no business here: this
	// registration drains unattended in a checkout the human is not standing on.
	if warnings := trunkAutoDrainWarnings(out.String()); len(warnings) != 0 {
		t.Fatalf("registering from a linked worktree must be quiet, got:\n%s", strings.Join(warnings, "\n"))
	}

	wantPath, err := tasks.ResolveRuntimePathWith(td, worktree, "")
	if err != nil {
		t.Fatalf("resolve runtime path: %v", err)
	}
	_, b, bound, err := binding.FindBySetID(td, "draft")
	if err != nil {
		t.Fatalf("find binding: %v", err)
	}
	if !bound {
		t.Fatalf("register did not bind the set:\n%s", out.String())
	}
	if b.RuntimePath != wantPath {
		t.Fatalf("bound to %q, want the worktree it was run from %q", b.RuntimePath, wantPath)
	}
	if b.Provisioned {
		t.Fatalf("binding must be adopted, not provisioned: %+v", b)
	}

	// Nothing was forked from trunk: the managed root holds no checkout at all.
	if entries, err := os.ReadDir(binding.ManagedWorktreesRoot(td)); err == nil && len(entries) > 0 {
		t.Fatalf("register provisioned a worktree under the managed root: %v", entries)
	}

	// Reading the consent bit through its own writer: setting it to the value it
	// already holds reports no change.
	changed, err := tasks.SetTaskSetAutoDrain(td, cmdTasksDir(t, td, worktree), "draft", true)
	if err != nil {
		t.Fatalf("read auto-drain bit: %v", err)
	}
	if changed {
		t.Fatal("register --auto-drain without --managed left the consent bit off")
	}
}
