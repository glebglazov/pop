package workload

import (
	"fmt"
	"sort"
	"strings"

	"github.com/glebglazov/pop/config"
	"github.com/glebglazov/pop/project"
)

// SetPriorityResult is the outcome of updating one Issue-set priority.
type SetPriorityResult struct {
	Refresh      *RefreshResult
	IssueSetID        string
	OldPriority  int
	NewPriority  int
}

// SetPriority updates one registered Issue-set priority and refreshes the table rows.
func SetPriority(input ResolveInput, issueSetID string, priority int) (*SetPriorityResult, error) {
	return SetPriorityWith(defaultDeps, projectDefaultDeps(), config.Load, input, issueSetID, priority)
}

func projectDefaultDeps() *project.Deps {
	return project.DefaultDeps()
}

// SetPriorityWith updates priority using injected dependencies.
func SetPriorityWith(d *Deps, pd *project.Deps, loadConfig func(string) (*config.Config, error), input ResolveInput, issueSetID string, priority int) (*SetPriorityResult, error) {
	resolved, err := ResolvePathsWith(d, pd, loadConfig, input)
	if err != nil {
		return nil, err
	}

	statePath := DefaultStatePathWith(d)
	refresh, err := RefreshWith(d, resolved.DefinitionPath, statePath)
	if err != nil {
		return nil, err
	}

	resolvedIssueSetID, err := ResolveIssueSetTarget(d, refresh, input.CWD, issueSetID)
	if err != nil {
		return nil, err
	}

	state, err := LoadGlobalStateWith(d, statePath)
	if err != nil {
		return nil, err
	}

	entry := state.Workloads[resolved.DefinitionPath]
	if _, _, err := findRegisteredIssueSet(entry, resolvedIssueSetID); err != nil {
		return nil, err
	}

	var oldPriority int
	err = UpdateGlobalStateWith(d, statePath, func(state *GlobalState) error {
		entry := state.Workloads[resolved.DefinitionPath]
		idx, old, err := findRegisteredIssueSet(entry, resolvedIssueSetID)
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

	refresh, err = RefreshWith(d, resolved.DefinitionPath, statePath)
	if err != nil {
		return nil, err
	}

	return &SetPriorityResult{
		Refresh:     refresh,
		IssueSetID:  resolvedIssueSetID,
		OldPriority: oldPriority,
		NewPriority: priority,
	}, nil
}

func findRegisteredIssueSet(entry *WorkloadEntry, issueSetID string) (int, int, error) {
	if entry == nil || len(entry.IssueSets) == 0 {
		return -1, 0, unknownIssueSetError(issueSetID, nil)
	}

	for i, set := range entry.IssueSets {
		if set.ID == issueSetID {
			return i, set.Priority, nil
		}
	}

	ids := make([]string, len(entry.IssueSets))
	for i, set := range entry.IssueSets {
		ids[i] = set.ID
	}
	return -1, 0, unknownIssueSetError(issueSetID, ids)
}

func unknownIssueSetError(id string, candidates []string) error {
	if len(candidates) == 0 {
		return fmt.Errorf("unknown Issue set %q (no registered Issue sets)", id)
	}
	sort.Strings(candidates)
	return fmt.Errorf("unknown Issue set %q; valid: %s", id, strings.Join(candidates, ", "))
}
