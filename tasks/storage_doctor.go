package tasks

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
)

// doctorWriteProbeFile is the transient probe Doctor writes and removes to prove
// pop can write beneath the Task storage data dir. It lives at the repos-dir
// root, never inside a Task-set storage directory.
const doctorWriteProbeFile = ".pop-doctor-write-probe"

// TaskStorageRoot returns pop's per-repository Task storage parent directory:
// <pop data dir>/repos. It is derived only; nothing is written.
func TaskStorageRoot(d *Deps) string {
	return filepath.Join(popDataDirWith(d), "repos")
}

// ProbeStorageWritable verifies pop can create and write beneath its Task storage
// data dir. It creates the directory on demand (harmless and idempotent), writes
// a probe file, then removes it, and returns the repos directory path. It never
// touches Task-set storage directories.
func ProbeStorageWritable(d *Deps) (string, error) {
	dir := TaskStorageRoot(d)
	if err := d.FS.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("create task storage data dir %s: %w", dir, err)
	}
	probe := filepath.Join(dir, doctorWriteProbeFile)
	if err := d.FS.WriteFile(probe, []byte("pop doctor write probe\n"), 0o644); err != nil {
		return "", fmt.Errorf("write beneath task storage data dir %s: %w", dir, err)
	}
	if err := d.FS.RemoveAll(probe); err != nil {
		return dir, fmt.Errorf("remove task storage data dir probe %s: %w", probe, err)
	}
	return dir, nil
}

// OrphanedStorage describes a Task storage directory whose recorded
// repository path no longer exists on disk.
type OrphanedStorage struct {
	// StorageDir is the per-repository Task storage directory.
	StorageDir string
	// RepositoryPath is the repository path recorded in repo.json.
	RepositoryPath string
}

// FindOrphanedStorage walks the Task storage data dir and returns storage
// directories whose repo.json repository_path no longer exists. It is strictly
// read-only: it never creates, modifies, or deletes storage. A missing repos
// dir yields no orphans. Directories without a readable, parseable repo.json are
// skipped rather than reported.
func FindOrphanedStorage(d *Deps) ([]OrphanedStorage, error) {
	dir := TaskStorageRoot(d)
	entries, err := d.FS.ReadDir(dir)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read task storage data dir %s: %w", dir, err)
	}

	var orphans []OrphanedStorage
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		storageDir := filepath.Join(dir, e.Name())
		data, err := d.FS.ReadFile(filepath.Join(storageDir, repoMarkerFile))
		if err != nil {
			continue
		}
		var marker RepoMarker
		if err := json.Unmarshal(data, &marker); err != nil || marker.RepositoryPath == "" {
			continue
		}
		if _, err := d.FS.Stat(marker.RepositoryPath); os.IsNotExist(err) {
			orphans = append(orphans, OrphanedStorage{
				StorageDir:     storageDir,
				RepositoryPath: marker.RepositoryPath,
			})
		}
	}
	sort.Slice(orphans, func(i, j int) bool { return orphans[i].StorageDir < orphans[j].StorageDir })
	return orphans, nil
}

// TaskStorageRepo names one repository that has Task storage on disk: the git
// common directory recorded in its repo.json marker, alongside the storage
// directory that holds its Task sets.
type TaskStorageRepo struct {
	// StorageDir is the per-repository Task storage directory.
	StorageDir string
	// RepositoryPath is the canonical git common directory recorded in repo.json.
	RepositoryPath string
}

// ListTaskStorageRepos returns every repository that has a Task storage
// directory containing at least one Task set, reading each repo.json marker for
// the recorded git common directory. It lets a bulk reader (the Work dashboard)
// discover the small set of repositories that can actually contribute rows
// without resolving git coordinates for every registered project. It is strictly
// read-only: a missing repos dir yields nothing, and directories without a
// readable, parseable marker or with no Task sets are skipped.
func ListTaskStorageRepos(d *Deps) ([]TaskStorageRepo, error) {
	dir := TaskStorageRoot(d)
	entries, err := d.FS.ReadDir(dir)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read task storage data dir %s: %w", dir, err)
	}

	var repos []TaskStorageRepo
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		storageDir := filepath.Join(dir, e.Name())
		sets, err := d.FS.ReadDir(filepath.Join(storageDir, "tasks"))
		if err != nil || len(sets) == 0 {
			continue
		}
		data, err := d.FS.ReadFile(filepath.Join(storageDir, repoMarkerFile))
		if err != nil {
			continue
		}
		var marker RepoMarker
		if err := json.Unmarshal(data, &marker); err != nil || marker.RepositoryPath == "" {
			continue
		}
		repos = append(repos, TaskStorageRepo{StorageDir: storageDir, RepositoryPath: marker.RepositoryPath})
	}
	sort.Slice(repos, func(i, j int) bool { return repos[i].StorageDir < repos[j].StorageDir })
	return repos, nil
}

// LegacyTaskSetIDs returns the legacy in-tree Task-set identifiers
// (thoughts/issues/<id>/index.json) present in the worktree containing cwd,
// sorted. When cwd is empty the current working directory is used. It is
// read-only and yields an empty list when no legacy location exists.
func LegacyTaskSetIDs(d *Deps, cwd string) ([]string, error) {
	if cwd == "" {
		var err error
		cwd, err = d.FS.Getwd()
		if err != nil {
			return nil, exitErr(ExitSetup, "determine working directory: %v", err)
		}
	}
	root, err := NormalizeProjectPathWith(d, cwd)
	if err != nil {
		return nil, exitErr(ExitSetup, "resolve worktree root: %v", err)
	}

	disc, err := DiscoverWith(d, filepath.Join(root, legacyThoughtsSubdir))
	if err != nil {
		return nil, err
	}
	ids := make([]string, 0, len(disc.Manifests))
	for id := range disc.Manifests {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids, nil
}
