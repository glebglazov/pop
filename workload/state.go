package workload

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

const StateVersion = 1

// stateFileName is the per-repository Task state file, stored inside Task storage
// beside the tasks/ directory.
const stateFileName = "state.json"

// StatePathFor returns the per-repository Task state file path for a definition
// (tasks) directory: <storage>/state.json beside the tasks/ directory. The state
// lives in Task storage, not in a single global file.
func StatePathFor(defPath string) string {
	return filepath.Join(filepath.Dir(defPath), stateFileName)
}

// RegisteredIssueSet is persisted registration metadata for an Issue set.
type RegisteredIssueSet struct {
	ID       string `json:"id"`
	Priority int    `json:"priority"`
}

// WorkloadEntry holds registered Issue sets for one definition path.
type WorkloadEntry struct {
	IssueSets []RegisteredIssueSet `json:"issue_sets"`
}

// GlobalState is the machine-local workload state file.
type GlobalState struct {
	Version   int                       `json:"version"`
	Workloads map[string]*WorkloadEntry `json:"workloads"`
	path      string
}

// DefaultStatePath returns the legacy global workload state file path.
//
// Deprecated: Task state now lives per-repository inside Task storage (see
// StatePathFor). This path is retained only as the migration source read by
// MigrateStorageLayout; no normal command reads or writes it.
func DefaultStatePath() string {
	return DefaultStatePathWith(defaultDeps)
}

// DefaultStatePathWith returns the legacy global workload state file path using
// provided dependencies. See DefaultStatePath for why it survives.
func DefaultStatePathWith(d *Deps) string {
	if xdgData := d.FS.Getenv("XDG_DATA_HOME"); xdgData != "" {
		return filepath.Join(xdgData, "pop", "workloads-state.json")
	}
	home, err := d.FS.UserHomeDir()
	if err != nil {
		return filepath.Join("/tmp", "pop", "workloads-state.json")
	}
	return filepath.Join(home, ".local", "share", "pop", "workloads-state.json")
}

// LoadGlobalState reads workload state from disk.
func LoadGlobalState(path string) (*GlobalState, error) {
	return LoadGlobalStateWith(defaultDeps, path)
}

// LoadGlobalStateWith reads workload state using provided dependencies.
func LoadGlobalStateWith(d *Deps, path string) (*GlobalState, error) {
	data, err := d.FS.ReadFile(path)
	if os.IsNotExist(err) {
		return &GlobalState{
			Version:   StateVersion,
			Workloads: make(map[string]*WorkloadEntry),
			path:      path,
		}, nil
	}
	if err != nil {
		return nil, err
	}

	var s GlobalState
	if err := json.Unmarshal(data, &s); err != nil {
		return nil, fmt.Errorf("workload state: parse %s: %w", path, err)
	}
	if s.Version != StateVersion {
		return nil, fmt.Errorf("workload state: unsupported version %d in %s", s.Version, path)
	}
	if s.Workloads == nil {
		s.Workloads = make(map[string]*WorkloadEntry)
	}
	s.path = path
	return &s, nil
}

// Save persists workload state atomically.
func (s *GlobalState) Save() error {
	return s.SaveWith(defaultDeps)
}

// SaveWith persists workload state using provided dependencies.
func (s *GlobalState) SaveWith(d *Deps) error {
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	return WriteAtomicWith(d, s.path, data, 0o644)
}

// Entry returns the workload entry for a definition path, creating it if needed.
func (s *GlobalState) Entry(defPath string) *WorkloadEntry {
	if s.Workloads[defPath] == nil {
		s.Workloads[defPath] = &WorkloadEntry{IssueSets: nil}
	}
	return s.Workloads[defPath]
}

// RegisteredIDs returns a set of registered Issue-set IDs for a definition path.
func (s *GlobalState) RegisteredIDs(defPath string) map[string]int {
	entry := s.Workloads[defPath]
	result := make(map[string]int)
	if entry == nil {
		return result
	}
	for i, set := range entry.IssueSets {
		result[set.ID] = i
	}
	return result
}
