package workload

import (
	"fmt"
	"sort"
	"strings"

	"github.com/glebglazov/pop/config"
	"github.com/glebglazov/pop/project"
)

// SetPriorityResult is the outcome of updating one PRD priority.
type SetPriorityResult struct {
	Refresh      *RefreshResult
	PRDID        string
	OldPriority  int
	NewPriority  int
}

// SetPriority updates one registered PRD priority and refreshes the table rows.
func SetPriority(input ResolveInput, prdID string, priority int) (*SetPriorityResult, error) {
	return SetPriorityWith(defaultDeps, projectDefaultDeps(), config.Load, input, prdID, priority)
}

func projectDefaultDeps() *project.Deps {
	return project.DefaultDeps()
}

// SetPriorityWith updates priority using injected dependencies.
func SetPriorityWith(d *Deps, pd *project.Deps, loadConfig func(string) (*config.Config, error), input ResolveInput, prdID string, priority int) (*SetPriorityResult, error) {
	resolved, err := ResolvePathsWith(d, pd, loadConfig, input)
	if err != nil {
		return nil, err
	}

	statePath := DefaultStatePathWith(d)
	state, err := LoadGlobalStateWith(d, statePath)
	if err != nil {
		return nil, err
	}

	entry := state.Workloads[resolved.DefinitionPath]
	if _, _, err := findRegisteredPRD(entry, prdID); err != nil {
		return nil, err
	}

	var oldPriority int
	err = UpdateGlobalStateWith(d, statePath, func(state *GlobalState) error {
		entry := state.Workloads[resolved.DefinitionPath]
		idx, old, err := findRegisteredPRD(entry, prdID)
		if err != nil {
			return err
		}
		oldPriority = old
		entry.IssueSets[idx].Priority = priority
		return nil
	})
	if err != nil {
		return nil, err
	}

	refresh, err := RefreshWith(d, resolved.DefinitionPath, statePath)
	if err != nil {
		return nil, err
	}

	return &SetPriorityResult{
		Refresh:     refresh,
		PRDID:       prdID,
		OldPriority: oldPriority,
		NewPriority: priority,
	}, nil
}

func findRegisteredPRD(entry *WorkloadEntry, prdID string) (int, int, error) {
	if entry == nil || len(entry.IssueSets) == 0 {
		return -1, 0, unknownPRDError(prdID, nil)
	}

	for i, set := range entry.IssueSets {
		if set.ID == prdID {
			return i, set.Priority, nil
		}
	}

	ids := make([]string, len(entry.IssueSets))
	for i, set := range entry.IssueSets {
		ids[i] = set.ID
	}
	return -1, 0, unknownPRDError(prdID, ids)
}

func unknownPRDError(id string, candidates []string) error {
	if len(candidates) == 0 {
		return fmt.Errorf("unknown PRD %q (no registered PRDs)", id)
	}
	sort.Strings(candidates)
	return fmt.Errorf("unknown PRD %q; valid: %s", id, strings.Join(candidates, ", "))
}
