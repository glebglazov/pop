package tasks

import (
	"fmt"
	"sort"
	"strings"

	"github.com/glebglazov/pop/config"
	"github.com/glebglazov/pop/project"
)

// SetPriorityResult is the outcome of updating one Task-set priority.
type SetPriorityResult struct {
	Refresh     *RefreshResult
	TaskSetID   string
	OldPriority int
	NewPriority int
}

// SetPriority updates one registered Task-set priority and refreshes the table rows.
func SetPriority(input ResolveInput, taskSetID string, priority int) (*SetPriorityResult, error) {
	return SetPriorityWith(defaultDeps, projectDefaultDeps(), config.Load, input, taskSetID, priority)
}

func projectDefaultDeps() *project.Deps {
	return project.DefaultDeps()
}

// SetPriorityWith updates priority using injected dependencies.
func SetPriorityWith(d *Deps, pd *project.Deps, loadConfig func(string) (*config.Config, error), input ResolveInput, taskSetID string, priority int) (*SetPriorityResult, error) {
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

	state, err := LoadGlobalStateWith(d, statePath)
	if err != nil {
		return nil, err
	}

	entry := state.Tasks[resolved.DefinitionPath]
	if _, _, err := findRegisteredTaskSet(entry, resolvedTaskSetID); err != nil {
		return nil, err
	}

	var oldPriority int
	err = UpdateGlobalStateWith(d, statePath, func(state *GlobalState) error {
		entry := state.Tasks[resolved.DefinitionPath]
		idx, old, err := findRegisteredTaskSet(entry, resolvedTaskSetID)
		if err != nil {
			return err
		}
		oldPriority = old
		entry.TaskSets[idx].Priority = priority
		return nil
	})
	if err != nil {
		return nil, err
	}

	refresh, err = RefreshWith(d, resolved.DefinitionPath, statePath)
	if err != nil {
		return nil, err
	}

	return &SetPriorityResult{
		Refresh:     refresh,
		TaskSetID:   resolvedTaskSetID,
		OldPriority: oldPriority,
		NewPriority: priority,
	}, nil
}

func findRegisteredTaskSet(entry *TaskEntry, taskSetID string) (int, int, error) {
	if entry == nil || len(entry.TaskSets) == 0 {
		return -1, 0, unknownTaskSetError(taskSetID, nil)
	}

	for i, set := range entry.TaskSets {
		if set.ID == taskSetID {
			return i, set.Priority, nil
		}
	}

	ids := registeredIDsFromEntry(entry)
	return -1, 0, unknownTaskSetError(taskSetID, ids)
}

func registeredIdentifierList(state *GlobalState, defPath string) []string {
	if state == nil {
		return nil
	}
	return registeredIDsFromEntry(state.Tasks[defPath])
}

func registeredIDsFromEntry(entry *TaskEntry) []string {
	if entry == nil {
		return nil
	}
	ids := make([]string, len(entry.TaskSets))
	for i, set := range entry.TaskSets {
		ids[i] = set.ID
	}
	sort.Strings(ids)
	return ids
}

func unknownTaskSetError(id string, candidates []string) error {
	if len(candidates) == 0 {
		return fmt.Errorf("unknown task set %q (no registered task sets)", id)
	}
	return fmt.Errorf("unknown task set %q; valid: %s", id, strings.Join(candidates, ", "))
}
