package wayfinder

import (
	"encoding/json"
	"os"
	"path/filepath"
)

const archiveStateFile = "wayfinder-archive.json"

type archiveState struct {
	Archived []string `json:"archived"`
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
