package tasks

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// repoMarkerFile is the reverse-lookup marker written into each Task storage directory.
const repoMarkerFile = "repo.json"

// ShortHashLen is the number of hex characters retained from the identity hash for directory naming.
const ShortHashLen = 12

// RepoMarker is persisted as repo.json inside a Task storage directory.
type RepoMarker struct {
	RepositoryPath string    `json:"repository_path"`
	CreatedAt      time.Time `json:"created_at"`
}

// RepositoryIdentity locates the per-repository Task storage derived from a checkout.
type RepositoryIdentity struct {
	// CommonDir is the canonical git common directory path; identity derives from it.
	CommonDir string
	// Basename is the human-readable repository name used in the storage directory name.
	Basename string
	// ShortHash is the truncated hash of CommonDir used in the storage directory name.
	ShortHash string
	// StorageDir is the per-repository Task storage directory.
	StorageDir string
	// TasksDir is the Task-set container inside StorageDir.
	TasksDir string
}

// popDataDirWith returns pop's base data directory, respecting XDG_DATA_HOME with the
// ~/.local/share/pop fallback, consistent with state and runtime-lock paths.
func popDataDirWith(d *Deps) string {
	if xdgData := d.FS.Getenv("XDG_DATA_HOME"); xdgData != "" {
		return filepath.Join(xdgData, "pop")
	}
	home, err := d.FS.UserHomeDir()
	if err != nil {
		return filepath.Join("/tmp", "pop")
	}
	return filepath.Join(home, ".local", "share", "pop")
}

// ResolveRepositoryIdentity resolves the Task storage location for the git repository
// containing cwd. When cwd is empty the current working directory is used. The returned
// paths are derived only; nothing is written.
func ResolveRepositoryIdentity(d *Deps, cwd string) (*RepositoryIdentity, error) {
	if cwd == "" {
		var err error
		cwd, err = d.FS.Getwd()
		if err != nil {
			return nil, exitErr(ExitSetup, "determine working directory: %v", err)
		}
	}

	commonDir, err := d.Git.CommandInDir(cwd, "rev-parse", "--git-common-dir")
	if err != nil {
		return nil, exitErr(ExitSetup, "not inside a git repository (run from a worktree of the target repo)")
	}
	return identityFromCommonDir(d, cwd, commonDir)
}

// IdentityFromCommonDir derives a repository identity from a canonical git
// common directory already recorded on disk (a repo.json marker's
// repository_path), performing no git invocation (ADR-0060). The common
// directory is expected to be absolute, as markers record it; derivation is a
// sha256 plus path operations.
func IdentityFromCommonDir(d *Deps, commonDir string) (*RepositoryIdentity, error) {
	return identityFromCommonDir(d, "", commonDir)
}

// identityFromCommonDir derives the repository identity from a git common
// directory already obtained by the caller (relative to gitCwd, as git reports
// it). It performs no git invocation, letting bulk resolvers fetch the common
// directory once and reuse it. An empty commonDir means the path is not inside a
// git repository.
func identityFromCommonDir(d *Deps, gitCwd, commonDir string) (*RepositoryIdentity, error) {
	commonDir = strings.TrimSpace(commonDir)
	if commonDir == "" {
		return nil, exitErr(ExitSetup, "not inside a git repository (run from a worktree of the target repo)")
	}

	if !filepath.IsAbs(commonDir) {
		commonDir = filepath.Join(gitCwd, commonDir)
	}
	canon, err := canonicalAbsPath(d, commonDir)
	if err != nil {
		return nil, exitErr(ExitSetup, "canonicalize git common directory: %v", err)
	}

	sum := sha256.Sum256([]byte(canon))
	shortHash := fmt.Sprintf("%x", sum)[:ShortHashLen]
	basename := RepoBasename(canon)

	storageDir := filepath.Join(popDataDirWith(d), "repos", fmt.Sprintf("%s-%s", basename, shortHash))
	return &RepositoryIdentity{
		CommonDir:  canon,
		Basename:   basename,
		ShortHash:  shortHash,
		StorageDir: storageDir,
		TasksDir:   filepath.Join(storageDir, "tasks"),
	}, nil
}

// RepoBasename derives a human-readable repository name from a canonical git common directory.
func RepoBasename(commonDir string) string {
	base := filepath.Base(commonDir)
	switch base {
	case ".git", ".bare":
		return filepath.Base(filepath.Dir(commonDir))
	}
	return strings.TrimSuffix(base, ".git")
}

// EnsureStorage creates the Task storage directory tree and repo.json marker on demand.
// It is idempotent: an existing marker is left untouched so created_at is preserved.
// Before creating anything it migrates a pre-rename storage layout into place (see
// MigrateStorageLayout) so first touch lands on the current layout.
func EnsureStorage(d *Deps, id *RepositoryIdentity) error {
	if _, err := MigrateStorageLayout(d, id.TasksDir); err != nil {
		return err
	}
	if err := d.FS.MkdirAll(id.TasksDir, 0o755); err != nil {
		return exitErr(ExitOperational, "create task storage: %v", err)
	}
	// Co-locate any PRDs authored under the retired sibling prds/ layout into
	// their matching set folders (ADR-0088). Idempotent once drained.
	if _, err := MigratePRDColocation(d, id.TasksDir); err != nil {
		return err
	}

	markerPath := filepath.Join(id.StorageDir, repoMarkerFile)
	if _, err := d.FS.Stat(markerPath); err == nil {
		return nil
	} else if !os.IsNotExist(err) {
		return exitErr(ExitOperational, "inspect task storage marker: %v", err)
	}

	marker := RepoMarker{
		RepositoryPath: id.CommonDir,
		CreatedAt:      time.Now().UTC(),
	}
	data, err := json.MarshalIndent(marker, "", "  ")
	if err != nil {
		return exitErr(ExitOperational, "encode task storage marker: %v", err)
	}
	if err := WriteAtomicWith(d, markerPath, data, 0o644); err != nil {
		return exitErr(ExitOperational, "write task storage marker: %v", err)
	}
	return nil
}

// ListStoredTaskSetIDs returns the Task-set identifiers present in Task storage,
// sorted. It is read-only: missing storage yields an empty list rather than an error.
func ListStoredTaskSetIDs(d *Deps, cwd string) ([]string, error) {
	id, err := ResolveRepositoryIdentity(d, cwd)
	if err != nil {
		return nil, err
	}
	return readTaskSetIDs(d, id.TasksDir), nil
}

func readTaskSetIDs(d *Deps, tasksDir string) []string {
	entries, err := d.FS.ReadDir(tasksDir)
	if err != nil {
		return nil
	}
	var ids []string
	for _, e := range entries {
		if e.IsDir() {
			ids = append(ids, e.Name())
		}
	}
	sort.Strings(ids)
	return ids
}

// ShowPathResult is the resolved absolute path printed by show-path commands.
type ShowPathResult struct {
	Path string
}

// ShowStorageRoot resolves the Task-storage root for the repository containing cwd,
// creating the storage directory tree and repo.json marker on demand. This is the
// single storage-root resolver shared by pop work show-path and pop tasks show-path.
func ShowStorageRoot(d *Deps, cwd string) (*ShowPathResult, error) {
	id, err := ResolveRepositoryIdentity(d, cwd)
	if err != nil {
		return nil, err
	}
	if err := EnsureStorage(d, id); err != nil {
		return nil, err
	}
	return &ShowPathResult{Path: id.StorageDir}, nil
}

// ShowPath resolves the Task storage path for the repository containing cwd, creating
// the storage and marker on demand. With a bare Task-set identifier it resolves that set's
// directory instead; an unknown identifier fails listing the valid ones.
func ShowPath(d *Deps, cwd, taskSetID string) (*ShowPathResult, error) {
	root, err := ShowStorageRoot(d, cwd)
	if err != nil {
		return nil, err
	}
	tasksDir := filepath.Join(root.Path, "tasks")

	if taskSetID == "" {
		return &ShowPathResult{Path: tasksDir}, nil
	}

	setDir := filepath.Join(tasksDir, taskSetID)
	info, err := d.FS.Stat(setDir)
	if err == nil && info.IsDir() {
		return &ShowPathResult{Path: setDir}, nil
	}
	if err != nil && !os.IsNotExist(err) {
		return nil, exitErr(ExitOperational, "inspect Task set %q: %v", taskSetID, err)
	}

	valid := readTaskSetIDs(d, tasksDir)
	if len(valid) == 0 {
		return nil, exitErr(ExitSetup, "unknown Task set %q (no Task sets in storage)", taskSetID)
	}
	return nil, exitErr(ExitSetup, "unknown Task set %q; valid identifiers: %s", taskSetID, strings.Join(valid, ", "))
}
