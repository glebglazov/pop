package workload

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
)

// legacyStorageParent is the retired data-dir parent for per-repository storage,
// replaced by "repos". A directory under it is a pre-rename storage layout.
const legacyStorageParent = "workloads"

// legacyTasksSubdir is the retired Task-set container name inside a storage
// directory, replaced by "tasks".
const legacyTasksSubdir = "issues"

// StorageLayoutMigration summarizes a one-shot move of a pre-rename storage layout.
type StorageLayoutMigration struct {
	// OldStorageDir is the retired workloads/<repo>-<hash> directory that was moved.
	OldStorageDir string
	// NewStorageDir is the repos/<repo>-<hash> directory it became.
	NewStorageDir string
	// MovedSets lists the Task-set identifiers relocated, sorted.
	MovedSets []string
}

// MigrateStorageLayout moves a pre-rename storage layout into the current one on
// first touch. Given the current tasks directory (repos/<repo>-<hash>/tasks), it
// looks for the retired layout (workloads/<repo>-<hash>/issues, issue-keyed
// manifests, the global workloads-state.json). When the old layout exists and the
// new storage directory does not, it relocates the whole tree, renames issues/ to
// tasks/, rewrites each manifest's "issues" key to "tasks", and moves the
// repository's state entry into a per-repository state.json. It is idempotent: an
// already-migrated repository (new storage present) is left untouched, and
// colliding identifiers are never merged. It reports what moved via a notice and
// the returned summary; a nil summary means nothing was migrated.
func MigrateStorageLayout(d *Deps, tasksDir string) (*StorageLayoutMigration, error) {
	newStorageDir := filepath.Dir(tasksDir)
	base := filepath.Base(newStorageDir)
	oldStorageDir := filepath.Join(popDataDirWith(d), legacyStorageParent, base)

	// Idempotent: a present new-layout storage means migration already happened.
	if _, err := d.FS.Stat(newStorageDir); err == nil {
		return nil, nil
	} else if !os.IsNotExist(err) {
		return nil, exitErr(ExitOperational, "inspect task storage %s: %v", newStorageDir, err)
	}

	// Nothing to migrate when the retired layout is absent.
	if _, err := d.FS.Stat(oldStorageDir); err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, exitErr(ExitOperational, "inspect legacy storage %s: %v", oldStorageDir, err)
	}

	// Capture the legacy state key while the old issues directory still exists, so
	// the rekey matches the canonical path discovery registered under.
	oldIssuesDir := filepath.Join(oldStorageDir, legacyTasksSubdir)
	oldKey, err := CanonicalDefinitionPathWith(d, oldIssuesDir)
	if err != nil {
		return nil, exitErr(ExitSetup, "canonicalize legacy issues directory: %v", err)
	}

	if err := d.FS.MkdirAll(filepath.Dir(newStorageDir), 0o755); err != nil {
		return nil, exitErr(ExitOperational, "create task storage parent: %v", err)
	}
	if err := moveTree(d, oldStorageDir, newStorageDir); err != nil {
		return nil, exitErr(ExitOperational, "move storage %s into %s: %v", oldStorageDir, newStorageDir, err)
	}

	movedIssuesDir := filepath.Join(newStorageDir, legacyTasksSubdir)
	if _, err := d.FS.Stat(movedIssuesDir); err == nil {
		if err := d.FS.Rename(movedIssuesDir, tasksDir); err != nil {
			return nil, exitErr(ExitOperational, "rename issues directory to tasks: %v", err)
		}
	}

	moved := readIssueSetIDs(d, tasksDir)
	for _, setID := range moved {
		manifestPath := filepath.Join(tasksDir, setID, "index.json")
		if err := rewriteManifestTaskKey(d, manifestPath); err != nil {
			return nil, exitErr(ExitOperational, "rewrite manifest for task set %q: %v", setID, err)
		}
	}

	newKey, err := CanonicalDefinitionPathWith(d, tasksDir)
	if err != nil {
		return nil, exitErr(ExitSetup, "canonicalize tasks directory: %v", err)
	}
	if err := migrateStateEntry(d, oldKey, newKey, StatePathFor(tasksDir)); err != nil {
		return nil, err
	}

	noticeOut := outputFor(noticeWriter(d))
	noticeOut.line(ansiCyan, "Migrated task storage layout: %s -> %s", oldStorageDir, newStorageDir)

	return &StorageLayoutMigration{
		OldStorageDir: oldStorageDir,
		NewStorageDir: newStorageDir,
		MovedSets:     moved,
	}, nil
}

// rewriteManifestTaskKey rewrites a manifest's top-level "issues" array key to
// "tasks", preserving every other field. A manifest already using "tasks", one
// without an "issues" key, or an unparseable one is left untouched.
func rewriteManifestTaskKey(d *Deps, manifestPath string) error {
	data, err := d.FS.ReadFile(manifestPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil
	}
	if _, ok := raw["tasks"]; ok {
		return nil
	}
	issues, ok := raw["issues"]
	if !ok {
		return nil
	}
	raw["tasks"] = issues
	delete(raw, "issues")

	out, err := json.MarshalIndent(raw, "", "  ")
	if err != nil {
		return err
	}
	return WriteAtomicWith(d, manifestPath, out, 0o644)
}

// migrateStateEntry moves the registration entry keyed by the legacy issues path
// from the global workloads-state.json into a per-repository state.json keyed by
// the tasks path, preserving priority and registration order. The global entry is
// then removed; an emptied global file is deleted. A missing global file or entry
// is a no-op.
func migrateStateEntry(d *Deps, oldKey, newKey, newStatePath string) error {
	legacyPath := DefaultStatePathWith(d)
	state, err := LoadGlobalStateWith(d, legacyPath)
	if err != nil {
		return err
	}
	entry := state.Workloads[oldKey]
	if entry == nil {
		return nil
	}

	if err := UpdateGlobalStateWith(d, newStatePath, func(s *GlobalState) error {
		if _, exists := s.Workloads[newKey]; exists {
			return nil
		}
		s.Workloads[newKey] = &WorkloadEntry{IssueSets: append([]RegisteredIssueSet(nil), entry.IssueSets...)}
		return nil
	}); err != nil {
		return err
	}

	return UpdateGlobalStateWith(d, legacyPath, func(s *GlobalState) error {
		delete(s.Workloads, oldKey)
		return nil
	})
}

// LegacyLayoutStorageDirs returns the names of pre-rename storage directories that
// still exist under the retired workloads/ parent, sorted. Doctor surfaces these
// as a finding so an un-migrated layout is visible without failing readiness. It
// is strictly read-only; a missing workloads/ parent yields no entries.
func LegacyLayoutStorageDirs(d *Deps) ([]string, error) {
	parent := filepath.Join(popDataDirWith(d), legacyStorageParent)
	entries, err := d.FS.ReadDir(parent)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var dirs []string
	for _, e := range entries {
		if e.IsDir() {
			dirs = append(dirs, filepath.Join(parent, e.Name()))
		}
	}
	sort.Strings(dirs)
	return dirs, nil
}
