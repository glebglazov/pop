package workload

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/glebglazov/pop/internal/deps"
)

// storageDeps builds Deps backed by the real filesystem with a mocked git that
// reports a fixed common directory regardless of the directory queried.
func storageDeps(t *testing.T, dataHome, commonDir string) *Deps {
	t.Helper()
	t.Setenv("XDG_DATA_HOME", dataHome)
	return &Deps{
		FS: deps.NewRealFileSystem(),
		Git: &deps.MockGit{
			CommandInDirFunc: func(dir string, args ...string) (string, error) {
				if len(args) >= 2 && args[0] == "rev-parse" && args[1] == "--git-common-dir" {
					return commonDir, nil
				}
				return "", nil
			},
		},
		Runner: RealCommandRunner{},
	}
}

func canonical(t *testing.T, path string) string {
	t.Helper()
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
		return filepath.Clean(path)
	}
	return resolved
}

func TestShowPathCreatesStorageAndMarker(t *testing.T) {
	root := t.TempDir()
	dataHome := filepath.Join(root, "data")
	commonDir := filepath.Join(root, "repo", ".git")
	if err := os.MkdirAll(commonDir, 0o755); err != nil {
		t.Fatal(err)
	}
	d := storageDeps(t, dataHome, commonDir)

	res, err := ShowPath(d, filepath.Join(root, "repo"), "")
	if err != nil {
		t.Fatal(err)
	}

	wantPrefix := filepath.Join(dataHome, "pop", "repos")
	if !strings.HasPrefix(res.Path, wantPrefix) {
		t.Fatalf("path %q not under %q", res.Path, wantPrefix)
	}
	if filepath.Base(res.Path) != "tasks" {
		t.Fatalf("path %q does not end in tasks", res.Path)
	}
	storageDir := filepath.Dir(res.Path)
	if !strings.HasPrefix(filepath.Base(storageDir), "repo-") {
		t.Fatalf("storage dir %q not named repo-<hash>", storageDir)
	}

	if info, err := os.Stat(res.Path); err != nil || !info.IsDir() {
		t.Fatalf("issues dir not created: %v", err)
	}

	markerPath := filepath.Join(storageDir, "repo.json")
	data, err := os.ReadFile(markerPath)
	if err != nil {
		t.Fatalf("repo.json not created: %v", err)
	}
	var marker RepoMarker
	if err := json.Unmarshal(data, &marker); err != nil {
		t.Fatal(err)
	}
	if marker.RepositoryPath != canonical(t, commonDir) {
		t.Fatalf("marker repository_path = %q, want %q", marker.RepositoryPath, canonical(t, commonDir))
	}
	if marker.CreatedAt.IsZero() {
		t.Fatal("marker created_at is zero")
	}
}

func TestShowPathIdempotent(t *testing.T) {
	root := t.TempDir()
	dataHome := filepath.Join(root, "data")
	commonDir := filepath.Join(root, "repo", ".git")
	if err := os.MkdirAll(commonDir, 0o755); err != nil {
		t.Fatal(err)
	}
	d := storageDeps(t, dataHome, commonDir)

	first, err := ShowPath(d, "", "")
	if err != nil {
		t.Fatal(err)
	}
	markerPath := filepath.Join(filepath.Dir(first.Path), "repo.json")
	before, err := os.ReadFile(markerPath)
	if err != nil {
		t.Fatal(err)
	}

	second, err := ShowPath(d, "", "")
	if err != nil {
		t.Fatal(err)
	}
	if first.Path != second.Path {
		t.Fatalf("path differs across calls: %q vs %q", first.Path, second.Path)
	}
	after, err := os.ReadFile(markerPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(before) != string(after) {
		t.Fatal("repo.json marker changed on second call; created_at not preserved")
	}
}

func TestAllWorktreesResolveSameStorage(t *testing.T) {
	root := t.TempDir()
	dataHome := filepath.Join(root, "data")
	commonDir := filepath.Join(root, "repo", ".bare")
	if err := os.MkdirAll(commonDir, 0o755); err != nil {
		t.Fatal(err)
	}
	// Two distinct worktrees both report the same common directory.
	wtA := filepath.Join(root, "repo", "wt-a")
	wtB := filepath.Join(root, "repo", "wt-b")
	for _, wt := range []string{wtA, wtB} {
		if err := os.MkdirAll(wt, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	d := storageDeps(t, dataHome, commonDir)

	a, err := ShowPath(d, wtA, "")
	if err != nil {
		t.Fatal(err)
	}
	b, err := ShowPath(d, wtB, "")
	if err != nil {
		t.Fatal(err)
	}
	if a.Path != b.Path {
		t.Fatalf("worktrees resolved to different storage: %q vs %q", a.Path, b.Path)
	}
}

func TestShowPathBareIssueSet(t *testing.T) {
	root := t.TempDir()
	dataHome := filepath.Join(root, "data")
	commonDir := filepath.Join(root, "repo", ".git")
	if err := os.MkdirAll(commonDir, 0o755); err != nil {
		t.Fatal(err)
	}
	d := storageDeps(t, dataHome, commonDir)

	base, err := ShowPath(d, "", "")
	if err != nil {
		t.Fatal(err)
	}
	setID := "2026-06-05-some-set"
	setDir := filepath.Join(base.Path, setID)
	if err := os.MkdirAll(setDir, 0o755); err != nil {
		t.Fatal(err)
	}

	res, err := ShowPath(d, "", setID)
	if err != nil {
		t.Fatal(err)
	}
	if res.Path != setDir {
		t.Fatalf("path = %q, want %q", res.Path, setDir)
	}
}

func TestShowPathUnknownIssueSetListsValid(t *testing.T) {
	root := t.TempDir()
	dataHome := filepath.Join(root, "data")
	commonDir := filepath.Join(root, "repo", ".git")
	if err := os.MkdirAll(commonDir, 0o755); err != nil {
		t.Fatal(err)
	}
	d := storageDeps(t, dataHome, commonDir)

	base, err := ShowPath(d, "", "")
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"set-b", "set-a"} {
		if err := os.MkdirAll(filepath.Join(base.Path, id), 0o755); err != nil {
			t.Fatal(err)
		}
	}

	_, err = ShowPath(d, "", "missing")
	if err == nil {
		t.Fatal("expected error for unknown Issue set")
	}
	var exit *ExitError
	if !errors.As(err, &exit) || exit.Code == 0 {
		t.Fatalf("expected non-zero ExitError, got %v", err)
	}
	if !strings.Contains(err.Error(), "set-a") || !strings.Contains(err.Error(), "set-b") {
		t.Fatalf("error does not list valid identifiers: %v", err)
	}
}

func TestShowPathUnknownIssueSetEmptyStorage(t *testing.T) {
	root := t.TempDir()
	dataHome := filepath.Join(root, "data")
	commonDir := filepath.Join(root, "repo", ".git")
	if err := os.MkdirAll(commonDir, 0o755); err != nil {
		t.Fatal(err)
	}
	d := storageDeps(t, dataHome, commonDir)

	_, err := ShowPath(d, "", "anything")
	if err == nil {
		t.Fatal("expected error for unknown Issue set in empty storage")
	}
	if !strings.Contains(err.Error(), "no Issue sets") {
		t.Fatalf("error = %v", err)
	}
}

func TestShowPathOutsideGitRepo(t *testing.T) {
	root := t.TempDir()
	dataHome := filepath.Join(root, "data")
	t.Setenv("XDG_DATA_HOME", dataHome)
	d := &Deps{
		FS: deps.NewRealFileSystem(),
		Git: &deps.MockGit{
			CommandInDirFunc: func(dir string, args ...string) (string, error) {
				return "", errors.New("fatal: not a git repository")
			},
		},
		Runner: RealCommandRunner{},
	}

	_, err := ShowPath(d, root, "")
	if err == nil {
		t.Fatal("expected error outside git repository")
	}
	var exit *ExitError
	if !errors.As(err, &exit) || exit.Code == 0 {
		t.Fatalf("expected non-zero ExitError, got %v", err)
	}
}

func TestListStoredIssueSetIDsReadOnly(t *testing.T) {
	root := t.TempDir()
	dataHome := filepath.Join(root, "data")
	commonDir := filepath.Join(root, "repo", ".git")
	if err := os.MkdirAll(commonDir, 0o755); err != nil {
		t.Fatal(err)
	}
	d := storageDeps(t, dataHome, commonDir)

	// No storage yet: read-only listing must not create anything and must not error.
	ids, err := ListStoredIssueSetIDs(d, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(ids) != 0 {
		t.Fatalf("expected no ids, got %v", ids)
	}
	id, err := ResolveRepositoryIdentity(d, "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(id.StorageDir); !os.IsNotExist(err) {
		t.Fatalf("listing created storage dir: %v", err)
	}

	// With sets present, listing returns them sorted.
	if err := EnsureStorage(d, id); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"zeta", "alpha"} {
		if err := os.MkdirAll(filepath.Join(id.TasksDir, name), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	// A stray file must be ignored.
	if err := os.WriteFile(filepath.Join(id.TasksDir, "note.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	ids, err = ListStoredIssueSetIDs(d, "")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(ids, ",") != "alpha,zeta" {
		t.Fatalf("ids = %v, want [alpha zeta]", ids)
	}
}

func TestRespectsHomeFallbackWithoutXDG(t *testing.T) {
	root := t.TempDir()
	commonDir := filepath.Join(root, "repo", ".git")
	if err := os.MkdirAll(commonDir, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("XDG_DATA_HOME", "")
	home := filepath.Join(root, "home")
	if err := os.MkdirAll(home, 0o755); err != nil {
		t.Fatal(err)
	}
	d := &Deps{
		FS:  fixedHomeFS{RealFileSystem: deps.NewRealFileSystem(), home: home},
		Git: &deps.MockGit{CommandInDirFunc: func(dir string, args ...string) (string, error) { return commonDir, nil }},
	}

	id, err := ResolveRepositoryIdentity(d, root)
	if err != nil {
		t.Fatal(err)
	}
	wantPrefix := filepath.Join(home, ".local", "share", "pop", "repos")
	if !strings.HasPrefix(id.StorageDir, wantPrefix) {
		t.Fatalf("storage dir %q not under %q", id.StorageDir, wantPrefix)
	}
}

// fixedHomeFS is a real filesystem with a controlled home directory and empty env.
type fixedHomeFS struct {
	*deps.RealFileSystem
	home string
}

func (f fixedHomeFS) UserHomeDir() (string, error) { return f.home, nil }
func (f fixedHomeFS) Getenv(string) string         { return "" }
