package queue

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/glebglazov/pop/tasks"
)

// DaemonState is persisted supervisor-owned state. Later queue slices add
// cooldowns, parked sets, and retry timers here.
type DaemonState struct {
	Version   int       `json:"version"`
	UpdatedAt time.Time `json:"updated_at,omitempty"`
}

// DaemonStatePath returns the queue daemon state path.
func DaemonStatePath(d *tasks.Deps) string {
	return filepath.Join(QueueDataDir(d), "state.json")
}

// EnsureDaemonState creates the daemon state file when missing and returns it.
func EnsureDaemonState(d *tasks.Deps) (*DaemonState, error) {
	state, err := ReadDaemonState(d)
	if err == nil {
		return state, nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	state = &DaemonState{Version: 1, UpdatedAt: time.Now().UTC()}
	if err := WriteDaemonState(d, state); err != nil {
		return nil, err
	}
	return state, nil
}

// ReadDaemonState reads the persisted queue daemon state file.
func ReadDaemonState(d *tasks.Deps) (*DaemonState, error) {
	data, err := d.FS.ReadFile(DaemonStatePath(d))
	if err != nil {
		return nil, err
	}
	var state DaemonState
	if err := json.Unmarshal(data, &state); err != nil {
		return nil, fmt.Errorf("parse queue daemon state: %w", err)
	}
	return &state, nil
}

// WriteDaemonState writes the queue daemon state file atomically.
func WriteDaemonState(d *tasks.Deps, state *DaemonState) error {
	if state == nil {
		state = &DaemonState{Version: 1}
	}
	if state.Version == 0 {
		state.Version = 1
	}
	payload, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return err
	}
	return tasks.WriteAtomicWith(d, DaemonStatePath(d), append(payload, '\n'), 0o644)
}
