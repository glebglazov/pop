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
