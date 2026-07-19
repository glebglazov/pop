package wayfinder

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"github.com/glebglazov/pop/tasks"
)

const archiveStateFile = "wayfinder-archive.json"

type archiveState struct {
	Archived []string `json:"archived"`
}

// ArchiveResult is the outcome of toggling one map's archived flag.
type ArchiveResult struct {
	MapID    string
	Archived bool
}

// LoadArchivedMapIDs reads archived map ids from pop-owned per-repository state.
// A missing state file yields an empty set.
func LoadArchivedMapIDs(d *Deps, storageDir string) (map[string]bool, error) {
	path := filepath.Join(storageDir, archiveStateFile)
	data, err := d.FS.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return map[string]bool{}, nil
		}
		return nil, err
	}
	var state archiveState
	if err := json.Unmarshal(data, &state); err != nil {
		return nil, err
	}
	out := make(map[string]bool, len(state.Archived))
	for _, id := range state.Archived {
		if id != "" {
			out[id] = true
		}
	}
	return out, nil
}

// SaveArchivedMapIDs writes archived map ids to pop-owned per-repository state.
func SaveArchivedMapIDs(d *Deps, storageDir string, archived map[string]bool) error {
	ids := make([]string, 0, len(archived))
	for id, on := range archived {
		if on {
			ids = append(ids, id)
		}
	}
	sort.Strings(ids)
	data, err := json.Marshal(archiveState{Archived: ids})
	if err != nil {
		return err
	}
	path := filepath.Join(storageDir, archiveStateFile)
	return d.FS.WriteFile(path, data, 0o644)
}

// ArchiveMap marks one map as archived. The operation is idempotent.
func ArchiveMap(d *Deps, cwd, mapID string) (*ArchiveResult, error) {
	return setMapArchived(d, cwd, mapID, true)
}

// UnarchiveMap clears one map's archived flag.
func UnarchiveMap(d *Deps, cwd, mapID string) (*ArchiveResult, error) {
	return setMapArchived(d, cwd, mapID, false)
}

func setMapArchived(d *Deps, cwd, mapID string, archived bool) (*ArchiveResult, error) {
	m, err := FindMap(d, cwd, mapID)
	if err != nil {
		return nil, err
	}
	id, err := tasks.ResolveRepositoryIdentity(d.taskDeps(), cwd)
	if err != nil {
		return nil, err
	}
	state, err := LoadArchivedMapIDs(d, id.StorageDir)
	if err != nil {
		return nil, err
	}
	already := state[m.ID]
	if archived {
		if already {
			return &ArchiveResult{MapID: m.ID, Archived: true}, nil
		}
		state[m.ID] = true
	} else {
		if !already {
			return nil, fmt.Errorf("wayfinder map %q is not archived", m.ID)
		}
		delete(state, m.ID)
	}
	if err := SaveArchivedMapIDs(d, id.StorageDir, state); err != nil {
		return nil, err
	}
	return &ArchiveResult{MapID: m.ID, Archived: archived}, nil
}
