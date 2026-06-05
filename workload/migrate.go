package workload

import (
	"os"
	"path/filepath"
	"sort"
)

// legacyIssuesSubdir is the retired in-tree Issue-set location, relative to a worktree root.
const legacyIssuesSubdir = "thoughts/issues"

// MigrateResult summarizes a one-shot Workload migration of the current worktree.
type MigrateResult struct {
	// StorageDir is the repository's Workload storage issues directory sets moved into.
	StorageDir string
	// Migrated lists the Issue-set identifiers moved into storage, sorted.
	Migrated []string
	// Skipped lists identifiers left in place due to a storage collision, sorted.
	Skipped []string
	// ThoughtsRemoved reports whether thoughts/ was deleted because it was left empty.
	ThoughtsRemoved bool
}

// Migrate performs the current-worktree Workload migration: every legacy
// thoughts/issues/<id>/ Issue set is moved into the repository's Workload storage
// issues directory, and the matching Workload state entries are rekeyed to the
// storage path preserving priority and registration order. An identifier that
// already exists in storage is reported and skipped, never merged. thoughts/ is
// removed only when migration leaves it empty. Git configuration and ignore files
// are never read or modified. It operates on the current worktree only.
func Migrate(d *Deps, cwd string) (*MigrateResult, error) {
	if cwd == "" {
		var err error
		cwd, err = d.FS.Getwd()
		if err != nil {
			return nil, exitErr(ExitSetup, "determine working directory: %v", err)
		}
	}

	// Legacy state was keyed by the canonical worktree root; legacy sets live under it.
	legacyDefPath, err := NormalizeProjectPathWith(d, cwd)
	if err != nil {
		return nil, exitErr(ExitSetup, "resolve worktree root: %v", err)
	}
	legacyIssuesDir := filepath.Join(legacyDefPath, legacyIssuesSubdir)

	id, err := ResolveRepositoryIdentity(d, cwd)
	if err != nil {
		return nil, err
	}
	if err := EnsureStorage(d, id); err != nil {
		return nil, err
	}
	// Canonicalize the storage issues directory so the rekeyed state key matches the
	// key discovery and status derive (resolveDefinitionPath canonicalizes it too).
	newDefPath, err := CanonicalDefinitionPathWith(d, id.IssuesDir)
	if err != nil {
		return nil, exitErr(ExitSetup, "canonicalize storage issues directory: %v", err)
	}

	result := &MigrateResult{StorageDir: id.IssuesDir}

	legacyIDs := readIssueSetIDs(d, legacyIssuesDir)
	for _, setID := range legacyIDs {
		dst := filepath.Join(id.IssuesDir, setID)
		if _, statErr := d.FS.Stat(dst); statErr == nil {
			result.Skipped = append(result.Skipped, setID)
			continue
		} else if !os.IsNotExist(statErr) {
			return nil, exitErr(ExitOperational, "inspect storage for Issue set %q: %v", setID, statErr)
		}

		src := filepath.Join(legacyIssuesDir, setID)
		if err := moveTree(d, src, dst); err != nil {
			return nil, exitErr(ExitOperational, "move Issue set %q into storage: %v", setID, err)
		}
		result.Migrated = append(result.Migrated, setID)
	}

	sort.Strings(result.Migrated)
	sort.Strings(result.Skipped)

	if len(result.Migrated) > 0 {
		if err := rekeyState(d, legacyDefPath, newDefPath, result.Migrated); err != nil {
			return nil, err
		}
	}

	removed, err := pruneEmptyThoughts(d, legacyDefPath)
	if err != nil {
		return nil, err
	}
	result.ThoughtsRemoved = removed

	return result, nil
}

// rekeyState moves the registration entries for migrated Issue sets from the legacy
// definition-path key to the storage key, preserving priority and relative order.
// Entries already registered under the storage key are left untouched (never merged).
func rekeyState(d *Deps, legacyDefPath, newDefPath string, migrated []string) error {
	migratedSet := make(map[string]bool, len(migrated))
	for _, setID := range migrated {
		migratedSet[setID] = true
	}

	statePath := DefaultStatePathWith(d)
	return UpdateGlobalStateWith(d, statePath, func(state *GlobalState) error {
		oldEntry := state.Workloads[legacyDefPath]
		if oldEntry == nil {
			return nil
		}

		newEntry := state.Entry(newDefPath)
		registered := state.RegisteredIDs(newDefPath)

		var remaining []RegisteredIssueSet
		for _, set := range oldEntry.IssueSets {
			if !migratedSet[set.ID] {
				remaining = append(remaining, set)
				continue
			}
			if _, exists := registered[set.ID]; exists {
				continue
			}
			newEntry.IssueSets = append(newEntry.IssueSets, set)
			registered[set.ID] = len(newEntry.IssueSets) - 1
		}

		if len(remaining) == 0 {
			delete(state.Workloads, legacyDefPath)
		} else {
			oldEntry.IssueSets = remaining
		}
		return nil
	})
}

// pruneEmptyThoughts removes thoughts/issues and then thoughts under the worktree root,
// each only when it has been left empty. Non-empty directories are left untouched.
func pruneEmptyThoughts(d *Deps, worktreeRoot string) (bool, error) {
	issuesDir := filepath.Join(worktreeRoot, legacyIssuesSubdir)
	if _, err := removeIfEmpty(d, issuesDir); err != nil {
		return false, err
	}
	thoughtsDir := filepath.Join(worktreeRoot, "thoughts")
	return removeIfEmpty(d, thoughtsDir)
}

// removeIfEmpty deletes dir when it exists and contains no entries. A missing
// directory is a no-op. It reports whether the directory was removed.
func removeIfEmpty(d *Deps, dir string) (bool, error) {
	entries, err := d.FS.ReadDir(dir)
	if os.IsNotExist(err) {
		return false, nil
	}
	if err != nil {
		return false, exitErr(ExitOperational, "inspect %s: %v", dir, err)
	}
	if len(entries) > 0 {
		return false, nil
	}
	if err := d.FS.RemoveAll(dir); err != nil {
		return false, exitErr(ExitOperational, "remove %s: %v", dir, err)
	}
	return true, nil
}

// moveTree relocates src to dst, falling back to a recursive copy-and-remove when a
// plain rename fails (for example across filesystem boundaries).
func moveTree(d *Deps, src, dst string) error {
	if err := d.FS.Rename(src, dst); err == nil {
		return nil
	}
	if err := copyTree(d, src, dst); err != nil {
		return err
	}
	return d.FS.RemoveAll(src)
}

// copyTree recursively copies the directory tree at src to dst.
func copyTree(d *Deps, src, dst string) error {
	info, err := d.FS.Stat(src)
	if err != nil {
		return err
	}
	if !info.IsDir() {
		data, err := d.FS.ReadFile(src)
		if err != nil {
			return err
		}
		if err := d.FS.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
			return err
		}
		return d.FS.WriteFile(dst, data, fileMode(info, 0o644))
	}

	if err := d.FS.MkdirAll(dst, fileMode(info, 0o755)); err != nil {
		return err
	}
	entries, err := d.FS.ReadDir(src)
	if err != nil {
		return err
	}
	for _, ent := range entries {
		if err := copyTree(d, filepath.Join(src, ent.Name()), filepath.Join(dst, ent.Name())); err != nil {
			return err
		}
	}
	return nil
}

// fileMode returns the file's permission bits, or fallback when info is nil.
func fileMode(info os.FileInfo, fallback os.FileMode) os.FileMode {
	if info == nil {
		return fallback
	}
	if perm := info.Mode().Perm(); perm != 0 {
		return perm
	}
	return fallback
}
