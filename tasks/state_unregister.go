package tasks

// RemoveRegisteredTaskSets drops the given task sets from registration under
// defPath. It is used to roll back a managed register when provisioning fails
// after the sets were activated (ADR-0147). An empty id slice is a no-op.
func RemoveRegisteredTaskSets(d *Deps, defPath string, taskSetIDs []string) error {
	if len(taskSetIDs) == 0 {
		return nil
	}
	canon, err := CanonicalDefinitionPathWith(d, defPath)
	if err != nil {
		return err
	}
	statePath := StatePathFor(canon)
	remove := make(map[string]struct{}, len(taskSetIDs))
	for _, id := range taskSetIDs {
		remove[id] = struct{}{}
	}
	return UpdateGlobalStateWith(d, statePath, func(state *GlobalState) error {
		entry := state.Tasks[canon]
		if entry == nil {
			return nil
		}
		var kept []RegisteredTaskSet
		for _, reg := range entry.TaskSets {
			if _, drop := remove[reg.ID]; drop {
				continue
			}
			kept = append(kept, reg)
		}
		entry.TaskSets = kept
		return nil
	})
}
