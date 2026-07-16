package tasks

import (
	"github.com/glebglazov/pop/config"
	"github.com/glebglazov/pop/project"
)

// AutoDrainResult is the outcome of toggling one Task set's auto-drain flag.
type AutoDrainResult struct {
	TaskSetID string
	AutoDrain bool
}

// SetAutoDrainResult is the outcome of setting one Task set's auto-drain
// consent bit to an explicit value and refreshing the status table.
type SetAutoDrainResult struct {
	Refresh   *RefreshResult
	TaskSetID string
	AutoDrain bool
}

// SetAutoDrain sets one registered Task set's auto-drain consent bit to the
// requested value and refreshes the table rows.
func SetAutoDrain(input ResolveInput, taskSetID string, enabled bool) (*SetAutoDrainResult, error) {
	return SetAutoDrainWith(defaultDeps, projectDefaultDeps(), config.Load, input, taskSetID, enabled)
}

// SetAutoDrainWith sets the auto-drain bit using injected dependencies. Unlike
// ToggleAutoDrainWith it writes the requested value outright, so it is
// idempotent and safe to run from scripts. It mirrors SetPriorityWith's seam:
// resolve → refresh (auto-registers on-disk sets) → resolve id → reject
// archived → set → refresh.
func SetAutoDrainWith(d *Deps, pd *project.Deps, loadConfig func(string) (*config.Config, error), input ResolveInput, taskSetID string, enabled bool) (*SetAutoDrainResult, error) {
	resolved, err := ResolvePathsWith(d, pd, loadConfig, input)
	if err != nil {
		return nil, err
	}

	statePath := StatePathFor(resolved.DefinitionPath)
	refresh, err := RefreshWith(d, resolved.DefinitionPath, statePath)
	if err != nil {
		return nil, err
	}

	resolvedTaskSetID, err := resolveTaskSetIdentifier(refresh, taskSetID)
	if err != nil {
		return nil, err
	}
	if err := RejectArchivedTaskSet(d, statePath, resolved.DefinitionPath, resolvedTaskSetID); err != nil {
		return nil, err
	}

	if _, err := SetTaskSetAutoDrain(d, resolved.DefinitionPath, resolvedTaskSetID, enabled); err != nil {
		return nil, err
	}

	refresh, err = RefreshWith(d, resolved.DefinitionPath, statePath)
	if err != nil {
		return nil, err
	}

	return &SetAutoDrainResult{
		Refresh:   refresh,
		TaskSetID: resolvedTaskSetID,
		AutoDrain: enabled,
	}, nil
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
