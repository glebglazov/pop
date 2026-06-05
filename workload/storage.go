package workload

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

// repoMarkerFile is the reverse-lookup marker written into each Workload storage directory.
const repoMarkerFile = "repo.json"

// shortHashLen is the number of hex characters retained from the identity hash for directory naming.
const shortHashLen = 12

// RepoMarker is persisted as repo.json inside a Workload storage directory.
type RepoMarker struct {
	RepositoryPath string    `json:"repository_path"`
	CreatedAt      time.Time `json:"created_at"`
}

// RepositoryIdentity locates the per-repository Workload storage derived from a checkout.
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

// ResolveRepositoryIdentity resolves the Workload storage location for the git repository
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
	commonDir = strings.TrimSpace(commonDir)
	if err != nil || commonDir == "" {
		return nil, exitErr(ExitSetup, "not inside a git repository (run from a worktree of the target repo)")
	}

	if !filepath.IsAbs(commonDir) {
		commonDir = filepath.Join(cwd, commonDir)
	}
	canon, err := canonicalAbsPath(d, commonDir)
	if err != nil {
		return nil, exitErr(ExitSetup, "canonicalize git common directory: %v", err)
	}

	sum := sha256.Sum256([]byte(canon))
	shortHash := fmt.Sprintf("%x", sum)[:shortHashLen]
	basename := repoBasename(canon)

	storageDir := filepath.Join(popDataDirWith(d), "repos", fmt.Sprintf("%s-%s", basename, shortHash))
	return &RepositoryIdentity{
		CommonDir:  canon,
		Basename:   basename,
		ShortHash:  shortHash,
		StorageDir: storageDir,
		TasksDir:   filepath.Join(storageDir, "tasks"),
	}, nil
}

// repoBasename derives a human-readable repository name from a canonical git common directory.
func repoBasename(commonDir string) string {
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

// ListStoredIssueSetIDs returns the Issue-set identifiers present in Workload storage,
// sorted. It is read-only: missing storage yields an empty list rather than an error.
func ListStoredIssueSetIDs(d *Deps, cwd string) ([]string, error) {
	id, err := ResolveRepositoryIdentity(d, cwd)
	if err != nil {
		return nil, err
	}
	return readIssueSetIDs(d, id.TasksDir), nil
}

func readIssueSetIDs(d *Deps, issuesDir string) []string {
	entries, err := d.FS.ReadDir(issuesDir)
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

// ShowPathResult is the resolved absolute path printed by `workload show-path`.
type ShowPathResult struct {
	Path string
}

// ShowPath resolves the Workload storage path for the repository containing cwd, creating
// the storage and marker on demand. With a bare Issue-set identifier it resolves that set's
// directory instead; an unknown identifier fails listing the valid ones.
func ShowPath(d *Deps, cwd, issueSetID string) (*ShowPathResult, error) {
	id, err := ResolveRepositoryIdentity(d, cwd)
	if err != nil {
		return nil, err
	}
	if err := EnsureStorage(d, id); err != nil {
		return nil, err
	}

	if issueSetID == "" {
		return &ShowPathResult{Path: id.TasksDir}, nil
	}

	setDir := filepath.Join(id.TasksDir, issueSetID)
	info, err := d.FS.Stat(setDir)
	if err == nil && info.IsDir() {
		return &ShowPathResult{Path: setDir}, nil
	}
	if err != nil && !os.IsNotExist(err) {
		return nil, exitErr(ExitOperational, "inspect Issue set %q: %v", issueSetID, err)
	}

	valid := readIssueSetIDs(d, id.TasksDir)
	if len(valid) == 0 {
		return nil, exitErr(ExitSetup, "unknown Issue set %q (no Issue sets in storage)", issueSetID)
	}
	return nil, exitErr(ExitSetup, "unknown Issue set %q; valid identifiers: %s", issueSetID, strings.Join(valid, ", "))
}
