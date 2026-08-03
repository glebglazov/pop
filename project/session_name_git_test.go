package project

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// These fixtures are real repositories on purpose. The prefix-loss paths they
// cover — a trunk whose git common directory is not named ".git", a submodule
// trunk, a worktree whose administrative directory has been deleted — are
// properties of what git reports for such a layout, so a faked git would only
// re-assert the assumption under test.

func gitBin(t *testing.T) string {
	t.Helper()
	bin, err := exec.LookPath("git")
	if err != nil {
		t.Skip("git not installed")
	}
	return bin
}

// git runs a git command with identity and branch defaults supplied inline, so the
// fixtures never depend on the machine's global git config.
func git(t *testing.T, dir string, args ...string) string {
	t.Helper()
	full := append([]string{
		"-c", "user.name=pop test",
		"-c", "user.email=pop@test",
		"-c", "init.defaultBranch=main",
		"-c", "protocol.file.allow=always",
	}, args...)
	cmd := exec.Command(gitBin(t), full...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v in %s: %v\n%s", args, dir, err, out)
	}
	return string(out)
}

// fixtureRoot returns a symlink-free temp directory: git reports resolved paths,
// and on macOS t.TempDir() sits under a symlinked /var.
func fixtureRoot(t *testing.T) string {
	t.Helper()
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatalf("resolve temp dir: %v", err)
	}
	return root
}

// initRepo creates a repository at path with one commit. extraInitArgs are passed
// to `git init` (used for --separate-git-dir).
func initRepo(t *testing.T, path string, extraInitArgs ...string) string {
	t.Helper()
	if err := os.MkdirAll(path, 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", path, err)
	}
	git(t, path, append(append([]string{"init"}, extraInitArgs...), path)...)
	if err := os.WriteFile(filepath.Join(path, "README"), []byte("fixture\n"), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}
	git(t, path, "add", "README")
	git(t, path, "commit", "-m", "initial")
	return path
}

func sessionNameOf(t *testing.T, path string) (string, error) {
	t.Helper()
	return SessionNameForWith(DefaultDeps(), path)
}

func assertName(t *testing.T, path, want string) {
	t.Helper()
	got, err := sessionNameOf(t, path)
	if err != nil {
		t.Fatalf("SessionNameFor(%s): unexpected error %v", path, err)
	}
	if got != want {
		t.Errorf("SessionNameFor(%s) = %q, want %q", path, got, want)
	}
}

// TestSessionNameSeparateGitDirTrunk covers the prefix-loss path where the common
// directory is not named ".git": nothing in the layout can be compared against a
// ".git"-derived main worktree path, so the worktree used to come out bare.
func TestSessionNameSeparateGitDirTrunk(t *testing.T) {
	t.Parallel()
	root := fixtureRoot(t)
	trunk := filepath.Join(root, "trunk")
	gitDir := filepath.Join(root, "gitdirs", "trunk.git")
	if err := os.MkdirAll(filepath.Dir(gitDir), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	initRepo(t, trunk, "--separate-git-dir="+gitDir)
	worktree := filepath.Join(root, "checkouts", "s4")
	git(t, trunk, "worktree", "add", worktree)

	assertName(t, trunk, "trunk")
	assertName(t, worktree, "trunk/s4")
}

// TestSessionNameSubmoduleTrunk covers the same prefix-loss path for a submodule,
// whose common directory is <super>/.git/modules/<sub>.
func TestSessionNameSubmoduleTrunk(t *testing.T) {
	t.Parallel()
	root := fixtureRoot(t)
	upstream := initRepo(t, filepath.Join(root, "upstream"))
	super := initRepo(t, filepath.Join(root, "super"))
	git(t, super, "submodule", "add", upstream, "sub")
	git(t, super, "commit", "-m", "add submodule")

	sub := filepath.Join(super, "sub")
	worktree := filepath.Join(root, "checkouts", "s6")
	git(t, sub, "worktree", "add", worktree)

	assertName(t, sub, "sub")
	assertName(t, worktree, "sub/s6")
}

// TestSessionNameBareRepoWithDotBare covers the <repo>/.bare layout: the repository
// is named after the directory holding .bare, never ".bare" itself.
func TestSessionNameBareRepoWithDotBare(t *testing.T) {
	t.Parallel()
	root := fixtureRoot(t)
	upstream := initRepo(t, filepath.Join(root, "upstream"))
	repo := filepath.Join(root, "annual_calendar")
	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	git(t, repo, "clone", "--bare", upstream, filepath.Join(repo, ".bare"))
	if err := os.WriteFile(filepath.Join(repo, ".git"), []byte("gitdir: ./.bare\n"), 0o644); err != nil {
		t.Fatalf("write gitdir pointer: %v", err)
	}
	worktree := filepath.Join(repo, "alfa")
	git(t, repo, "worktree", "add", worktree)

	assertName(t, worktree, "annual_calendar/alfa")
}

// TestSessionNameManagedWorktreeWithMissingAdminDir is the reported bug: a managed
// worktree lives outside its repository's tree, so when its administrative directory
// goes (a prune, a moved trunk) git can say nothing about it. The name must not
// collapse to the bare Task-set id — that is how one checkout became reachable under
// two session names — and the breakage must be reported rather than swallowed.
func TestSessionNameManagedWorktreeWithMissingAdminDir(t *testing.T) {
	t.Parallel()
	root := fixtureRoot(t)
	trunk := initRepo(t, filepath.Join(root, "myrepo"))
	const setID = "2026-08-03-worktree-session-locality"
	managed := filepath.Join(root, "work", "worktrees", "myrepo-0123456789ab", setID)
	git(t, trunk, "worktree", "add", managed)

	assertName(t, managed, "myrepo/"+setID)

	if err := os.RemoveAll(filepath.Join(trunk, ".git", "worktrees", setID)); err != nil {
		t.Fatalf("remove worktree admin dir: %v", err)
	}
	if _, err := git2(trunk, "rev-parse", "--git-dir"); err != nil {
		t.Fatalf("trunk itself broke: %v", err)
	}

	got, err := sessionNameOf(t, managed)
	if got != "myrepo/"+setID {
		t.Errorf("degraded SessionNameFor = %q, want the prefixed name %q", got, "myrepo/"+setID)
	}
	if err == nil {
		t.Error("a checkout git cannot answer for reported no failure")
	}
}

// TestSessionNameHandMadeWorktreeWithMissingAdminDir is the same breakage outside the
// managed root: the `.git` pointer file still names the repository, so the prefix is
// recovered from it rather than lost.
func TestSessionNameHandMadeWorktreeWithMissingAdminDir(t *testing.T) {
	t.Parallel()
	root := fixtureRoot(t)
	trunk := initRepo(t, filepath.Join(root, "game_server"))
	worktree := filepath.Join(root, "game-server-apple-arcade")
	git(t, trunk, "worktree", "add", worktree)

	if err := os.RemoveAll(filepath.Join(trunk, ".git", "worktrees", "game-server-apple-arcade")); err != nil {
		t.Fatalf("remove worktree admin dir: %v", err)
	}

	got, err := sessionNameOf(t, worktree)
	if want := "game_server/game-server-apple-arcade"; got != want {
		t.Errorf("SessionNameFor = %q, want %q", got, want)
	}
	if err == nil {
		t.Error("a checkout git cannot answer for reported no failure")
	}
}

// git2 runs git without failing the test, for assertions about git's own state.
func git2(dir string, args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	return string(out), err
}
