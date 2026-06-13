package tasks

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

// RegisteredTaskSet is persisted registration metadata for an Task set.
type RegisteredTaskSet struct {
	ID       string `json:"id"`
	Priority int    `json:"priority"`
	Archived bool   `json:"archived"`
}

// TaskEntry holds registered Task sets for one definition path.
type TaskEntry struct {
	TaskSets []RegisteredTaskSet `json:"issue_sets"`
}

// GlobalState is the machine-local task state file.
type GlobalState struct {
	Version int                   `json:"version"`
	Tasks   map[string]*TaskEntry `json:"workloads"`
	path    string
}

// DefaultStatePath returns the legacy global task state file path.
//
// Deprecated: Task state now lives per-repository inside Task storage (see
// StatePathFor). This path is retained only as the migration source read by
// MigrateStorageLayout; no normal command reads or writes it.
func DefaultStatePath() string {
	return DefaultStatePathWith(defaultDeps)
}

// DefaultStatePathWith returns the legacy global task state file path using
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

// LoadGlobalState reads task state from disk.
func LoadGlobalState(path string) (*GlobalState, error) {
	return LoadGlobalStateWith(defaultDeps, path)
}

// LoadGlobalStateWith reads task state using provided dependencies.
func LoadGlobalStateWith(d *Deps, path string) (*GlobalState, error) {
	data, err := d.FS.ReadFile(path)
	if os.IsNotExist(err) {
		return &GlobalState{
			Version: StateVersion,
			Tasks:   make(map[string]*TaskEntry),
			path:    path,
		}, nil
	}
	if err != nil {
		return nil, err
	}

	var s GlobalState
	if err := json.Unmarshal(data, &s); err != nil {
		return nil, fmt.Errorf("task state: parse %s: %w", path, err)
	}
	if s.Version != StateVersion {
		return nil, fmt.Errorf("task state: unsupported version %d in %s", s.Version, path)
	}
	if s.Tasks == nil {
		s.Tasks = make(map[string]*TaskEntry)
	}
	s.path = path
	return &s, nil
}

// Save persists task state atomically.
func (s *GlobalState) Save() error {
	return s.SaveWith(defaultDeps)
}

// SaveWith persists task state using provided dependencies.
func (s *GlobalState) SaveWith(d *Deps) error {
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	return WriteAtomicWith(d, s.path, data, 0o644)
}

// Entry returns the task entry for a definition path, creating it if needed.
func (s *GlobalState) Entry(defPath string) *TaskEntry {
	if s.Tasks[defPath] == nil {
		s.Tasks[defPath] = &TaskEntry{TaskSets: nil}
	}
	return s.Tasks[defPath]
}

// RegisteredIDs returns a set of registered Task-set IDs for a definition path.
func (s *GlobalState) RegisteredIDs(defPath string) map[string]int {
	entry := s.Tasks[defPath]
	result := make(map[string]int)
	if entry == nil {
		return result
	}
	for i, set := range entry.TaskSets {
		result[set.ID] = i
	}
	return result
}
