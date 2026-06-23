package tasks

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
	next, err := ToggleTaskSetAutoDrain(d, defPath, taskSetID)
	if err != nil {
		return nil, err
	}
	return &AutoDrainResult{TaskSetID: taskSetID, AutoDrain: next}, nil
}
