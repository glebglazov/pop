package queue

import (
	"sort"
	"strings"

	"github.com/glebglazov/pop/tasks"
)

// CompleteIntegrationSetIDs returns task-set identifiers awaiting queue
// integration, deduplicated and sorted for shell completion.
func CompleteIntegrationSetIDs(d *Deps) ([]string, error) {
	state, err := readDaemonStateForCompletion(d)
	if err != nil || state == nil || len(state.Mergeability) == 0 {
		return nil, err
	}
	ids := make([]string, 0, len(state.Mergeability))
	for _, rec := range state.Mergeability {
		ids = append(ids, rec.SetID)
	}
	return dedupeSortSetIDs(ids), nil
}

// CompleteAbandonSetIDs returns task-set identifiers with a queue worktree
// binding, deduplicated and sorted for shell completion.
func CompleteAbandonSetIDs(d *Deps) ([]string, error) {
	state, err := readDaemonStateForCompletion(d)
	if err != nil || state == nil || len(state.WorktreeBindings) == 0 {
		return nil, err
	}
	ids := make([]string, 0, len(state.WorktreeBindings))
	for key := range state.WorktreeBindings {
		ids = append(ids, setIDFromScopedKey(key))
	}
	return dedupeSortSetIDs(ids), nil
}

// CompleteBindWorktreeSetIDs returns all task-set identifiers (bound or not)
// for shell completion of `pop tasks bind-worktree`.
func CompleteBindWorktreeSetIDs(d *Deps) ([]string, error) {
	state, err := readDaemonStateForCompletion(d)
	if err != nil || state == nil {
		return nil, err
	}
	// Return all set IDs from bindings and mergeability records
	// (all sets that have ever been associated with queue state)
	seen := make(map[string]struct{})
	for key := range state.WorktreeBindings {
		id := setIDFromScopedKey(key)
		seen[id] = struct{}{}
	}
	for key := range state.Mergeability {
		id := setIDFromScopedKey(key)
		seen[id] = struct{}{}
	}
	ids := make([]string, 0, len(seen))
	for id := range seen {
		ids = append(ids, id)
	}
	return dedupeSortSetIDs(ids), nil
}

func readDaemonStateForCompletion(d *Deps) (*DaemonState, error) {
	if d == nil {
		d = DefaultDeps()
	}
	td := d.Tasks
	if td == nil {
		td = tasks.DefaultDeps()
	}
	return EnsureDaemonState(td)
}

func dedupeSortSetIDs(ids []string) []string {
	seen := make(map[string]struct{}, len(ids))
	for _, id := range ids {
		id = strings.TrimSpace(id)
		if id != "" {
			seen[id] = struct{}{}
		}
	}
	out := make([]string, 0, len(seen))
	for id := range seen {
		out = append(out, id)
	}
	sort.Strings(out)
	return out
}
