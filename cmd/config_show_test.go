package cmd

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/glebglazov/pop/config"
	"github.com/glebglazov/pop/internal/deps"
	"github.com/glebglazov/pop/tasks"
	"github.com/spf13/cobra"
)

// mockTrunkDeps builds task deps whose git and filesystem are fully mocked, so
// resolveCurrentRepoTrunk exercises no real git or filesystem. gitInDir routes
// `git ... ` invocations by their joined args; fsWrite, when non-nil, is
// installed as a WriteFile/MkdirAll trap that fails the test if the resolver
// ever tries to write (it must never touch the task-binding store).
func mockTrunkDeps(t *testing.T, gitInDir func(dir string, args ...string) (string, error)) *tasks.Deps {
	t.Helper()
	failWrite := func(where string) {
		t.Helper()
		t.Fatalf("resolver wrote to the filesystem (%s); it must never touch the task-binding store", where)
	}
	return &tasks.Deps{
		FS: &deps.MockFileSystem{
			WriteFileFunc: func(string, []byte, os.FileMode) error { failWrite("WriteFile"); return nil },
			MkdirAllFunc:  func(string, os.FileMode) error { failWrite("MkdirAll"); return nil },
			RenameFunc:    func(string, string) error { failWrite("Rename"); return nil },
		},
		Git: &deps.MockGit{CommandInDirFunc: gitInDir},
	}
}

func runGitShow(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

func realPath(t *testing.T, path string) string {
	t.Helper()
	r, err := filepath.EvalSymlinks(path)
	if err != nil {
		return path
	}
	return r
}

// TestResolveCurrentRepoTrunkNonBareDerived covers a plain (non-bare) repo with
// no trunk config: the resolver reports the git-derived main worktree as the
// trunk and bare = false. Fully mocked git — no real repo.
func TestResolveCurrentRepoTrunkNonBareDerived(t *testing.T) {
	const repo = "/repo"
	td := mockTrunkDeps(t, func(dir string, args ...string) (string, error) {
		switch strings.Join(args, " ") {
		case "worktree list --porcelain":
			// A non-bare repo lists its primary working tree first, no `bare`.
			return "worktree " + repo + "\nHEAD abc123\nbranch refs/heads/main\n", nil
		case "rev-parse --git-common-dir":
			return repo + "/.git", nil
		}
		return "", fmt.Errorf("unexpected git %v in %q", args, dir)
	})

	got, err := resolveCurrentRepoTrunk(td, &config.Config{}, repo)
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if got == nil {
		t.Fatal("expected a resolved trunk inside a git repo, got nil")
	}
	if got.Bare {
		t.Errorf("bare = true, want false for a non-bare repo")
	}
	if got.Path != repo {
		t.Errorf("trunk = %q, want the main worktree %q", got.Path, repo)
	}
}

// TestResolveCurrentRepoTrunkBareConfigOverride covers a bare repo whose
// config-declared trunk = true names one worktree: the resolver reports that
// worktree as the trunk and bare = true. Fully mocked git — no real repo.
func TestResolveCurrentRepoTrunkBareConfigOverride(t *testing.T) {
	const (
		bareDir = "/base/bare.git"
		wt      = "/base/trunkwt"
	)
	td := mockTrunkDeps(t, func(dir string, args ...string) (string, error) {
		switch strings.Join(args, " ") {
		case "worktree list --porcelain":
			// A bare repo lists its bare entry first, so bareness is reported
			// even when the current directory is a linked worktree.
			return "worktree " + bareDir + "\nbare\n\nworktree " + wt + "\nHEAD abc123\nbranch refs/heads/trunk\n", nil
		case "rev-parse --git-common-dir":
			// Both the checkout and the config-named candidate share the bare
			// repo's common dir, so their repo keys match.
			return bareDir, nil
		case "rev-parse --show-toplevel":
			return wt, nil
		}
		return "", fmt.Errorf("unexpected git %v in %q", args, dir)
	})

	cfg := &config.Config{
		Repo: map[string]config.RepoOverrideConfig{
			wt: {Trunk: boolPtrShow(true)},
		},
	}

	got, err := resolveCurrentRepoTrunk(td, cfg, wt)
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if got == nil {
		t.Fatal("expected a resolved trunk inside a bare repo worktree, got nil")
	}
	if !got.Bare {
		t.Errorf("bare = false, want true for a bare repo")
	}
	if got.Path != wt {
		t.Errorf("trunk = %q, want the config-declared worktree %q", got.Path, wt)
	}
}

// TestResolveCurrentRepoTrunkOutsideRepo covers running outside any git repo:
// git fails the in-repo probe, so the resolver returns nil and the current-repo
// section is omitted. Fully mocked git — no real repo.
func TestResolveCurrentRepoTrunkOutsideRepo(t *testing.T) {
	const dir = "/not/a/repo"
	td := mockTrunkDeps(t, func(_ string, _ ...string) (string, error) {
		// Outside a git repo every git invocation fails.
		return "", fmt.Errorf("fatal: not a git repository")
	})

	got, err := resolveCurrentRepoTrunk(td, &config.Config{}, dir)
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if got != nil {
		t.Errorf("expected nil trunk outside a git repo, got %+v", got)
	}
}

func boolPtrShow(b bool) *bool { return &b }

// TestRunConfigShowJSONEmitsValidJSON verifies --json produces valid JSON
// carrying the current-repo trunk/bare as cleanly extractable fields, while
// the default (no flag) invocation still prints TOML. It runs inside a real
// temp git repo so currentRepoTrunk resolves a genuine trunk, exercising the
// same path pop config show --json takes in practice.
func TestRunConfigShowJSONEmitsValidJSON(t *testing.T) {
	xdgConfig := filepath.Join(t.TempDir(), "xdgconfig")
	if err := os.MkdirAll(filepath.Join(xdgConfig, "pop"), 0o755); err != nil {
		t.Fatalf("mkdir config dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(xdgConfig, "pop", "config.toml"), nil, 0o644); err != nil {
		t.Fatalf("write config.toml: %v", err)
	}
	t.Setenv("XDG_CONFIG_HOME", xdgConfig)
	t.Setenv("XDG_DATA_HOME", filepath.Join(t.TempDir(), "xdgdata"))

	repo := t.TempDir()
	runGitShow(t, repo, "init")
	runGitShow(t, repo, "config", "user.email", "a@b.c")
	runGitShow(t, repo, "config", "user.name", "x")
	runGitShow(t, repo, "commit", "--allow-empty", "-m", "init")

	oldWd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	if err := os.Chdir(repo); err != nil {
		t.Fatalf("chdir: %v", err)
	}
	defer func() { _ = os.Chdir(oldWd) }()

	oldJSON := configShowJSON
	configShowJSON = true
	defer func() { configShowJSON = oldJSON }()

	var out bytes.Buffer
	cmd := &cobra.Command{}
	cmd.SetOut(&out)
	if err := runConfigShow(cmd, nil); err != nil {
		t.Fatalf("runConfigShow --json: %v", err)
	}

	var got map[string]interface{}
	if err := json.Unmarshal(out.Bytes(), &got); err != nil {
		t.Fatalf("--json output is not valid JSON: %v\n%s", err, out.String())
	}
	currentRepo, ok := got["current_repo"].(map[string]interface{})
	if !ok {
		t.Fatalf("current_repo missing or wrong shape, got: %v", got)
	}
	if currentRepo["trunk"] != realPath(t, repo) {
		t.Errorf("current_repo.trunk = %v, want %v", currentRepo["trunk"], realPath(t, repo))
	}
	if currentRepo["bare"] != false {
		t.Errorf("current_repo.bare = %v, want false", currentRepo["bare"])
	}

	// Default (no flag) invocation still emits TOML.
	configShowJSON = false
	var tomlOut bytes.Buffer
	cmd2 := &cobra.Command{}
	cmd2.SetOut(&tomlOut)
	if err := runConfigShow(cmd2, nil); err != nil {
		t.Fatalf("runConfigShow (TOML): %v", err)
	}
	if !strings.Contains(tomlOut.String(), "[current_repo]") {
		t.Errorf("expected TOML output to contain [current_repo], got:\n%s", tomlOut.String())
	}
	if strings.HasPrefix(strings.TrimSpace(tomlOut.String()), "{") {
		t.Errorf("default output should be TOML, not JSON, got:\n%s", tomlOut.String())
	}
}

// TestConfigShowJSONFlagInHelp verifies --json is documented in `pop config
// show --help`.
func TestConfigShowJSONFlagInHelp(t *testing.T) {
	f := configShowCmd.Flags().Lookup("json")
	if f == nil {
		t.Fatal("expected --json flag registered on config show")
	}
}
