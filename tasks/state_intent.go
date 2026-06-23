package tasks

// This file holds the task-state mutation primitives that own the full
// locked load -> find-by-id -> mutate-by-index -> save choreography for a
// single registered task set's fields. Each function canonicalizes the
// definition path, derives its state path internally, and writes through
// UpdateGlobalStateWith. Callers pass an already-canonical task-set id (raw-id
// resolution stays with callers); an in-lock find-miss is treated as a
// last-resort invariant error via findRegisteredTaskSet.

// SetTaskSetPriority sets one registered task set's priority and returns the
// prior value. An in-lock find-miss is a race guard reported as "unknown task
// set".
func SetTaskSetPriority(d *Deps, defPath, taskSetID string, priority int) (old int, err error) {
	canon, err := CanonicalDefinitionPathWith(d, defPath)
	if err != nil {
		return 0, err
	}
	statePath := StatePathFor(canon)

	err = UpdateGlobalStateWith(d, statePath, func(state *GlobalState) error {
		entry := state.Tasks[canon]
		idx, prior, err := findRegisteredTaskSet(entry, taskSetID)
		if err != nil {
			return err
		}
		old = prior
		entry.TaskSets[idx].Priority = priority
		return nil
	})
	if err != nil {
		return 0, err
	}
	return old, nil
}

// SetTaskSetArchived sets the archived flag on several registered task sets in
// one lock acquisition. It is all-or-nothing: an unknown id fails the whole
// batch and writes nothing. An empty id slice is a clean no-op that writes
// nothing.
func SetTaskSetArchived(d *Deps, defPath string, taskSetIDs []string, archived bool) error {
	if len(taskSetIDs) == 0 {
		return nil
	}
	canon, err := CanonicalDefinitionPathWith(d, defPath)
	if err != nil {
		return err
	}
	statePath := StatePathFor(canon)

	return UpdateGlobalStateWith(d, statePath, func(state *GlobalState) error {
		entry := state.Tasks[canon]
		for _, id := range taskSetIDs {
			idx, _, err := findRegisteredTaskSet(entry, id)
			if err != nil {
				return err
			}
			entry.TaskSets[idx].Archived = archived
		}
		return nil
	})
}

// ToggleTaskSetAutoDrain flips one registered task set's auto-drain flag and
// returns the new value. An in-lock find-miss is a race guard reported as
// "unknown task set".
func ToggleTaskSetAutoDrain(d *Deps, defPath, taskSetID string) (next bool, err error) {
	canon, err := CanonicalDefinitionPathWith(d, defPath)
	if err != nil {
		return false, err
	}
	statePath := StatePathFor(canon)

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
		return false, err
	}
	return next, nil
}
