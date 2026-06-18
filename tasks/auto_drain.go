package tasks

import "fmt"

// AutoDrainResult is the outcome of toggling one Task set's auto-drain flag.
type AutoDrainResult struct {
	TaskSetID string
	AutoDrain bool
}

// ToggleAutoDrain flips one registered Task set's auto-drain flag in Task state.
func ToggleAutoDrain(defPath, taskSetID string) (*AutoDrainResult, error) {
	return ToggleAutoDrainWith(defaultDeps, defPath, StatePathFor(defPath), taskSetID)
}

// ToggleAutoDrainWith flips one registered Task set's auto-drain flag using
// injected dependencies. It is registration metadata only: no task progress or
// manifest state is written.
func ToggleAutoDrainWith(d *Deps, defPath, statePath, taskSetID string) (*AutoDrainResult, error) {
	canon, err := CanonicalDefinitionPathWith(d, defPath)
	if err != nil {
		return nil, err
	}
	state, err := LoadGlobalStateWith(d, statePath)
	if err != nil {
		return nil, err
	}
	entry := state.Tasks[canon]
	if entry == nil {
		return nil, fmt.Errorf("task set %q is not registered", taskSetID)
	}
	if _, _, err := findRegisteredTaskSet(entry, taskSetID); err != nil {
		return nil, err
	}

	var next bool
	err = UpdateGlobalStateWith(d, statePath, func(state *GlobalState) error {
		entry := state.Tasks[canon]
		idx, _, err := findRegisteredTaskSet(entry, taskSetID)
		if err != nil {
			return err
		}
		next = !entry.TaskSets[idx].AutoDrain
		entry.TaskSets[idx].AutoDrain = next
		return nil
	})
	if err != nil {
		return nil, err
	}
	return &AutoDrainResult{TaskSetID: taskSetID, AutoDrain: next}, nil
}
