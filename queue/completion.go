package queue

import (
	"sort"
	"strings"

	"github.com/glebglazov/pop/tasks/binding"
	"github.com/glebglazov/pop/tasks/integration"
	"github.com/glebglazov/pop/tasks"
)

// CompleteIntegrationSetIDs returns task-set identifiers awaiting queue
// integration, deduplicated and sorted for shell completion.
func CompleteIntegrationSetIDs(d *Deps) ([]string, error) {
	if d == nil {
		d = DefaultDeps()
	}
	td := d.Tasks
	if td == nil {
		td = tasks.DefaultDeps()
	}
	ids, err := integration.SetIDsFromStore(td)
	if err != nil || len(ids) == 0 {
		return nil, err
	}
	return dedupeSortSetIDs(ids), nil
}

// CompleteAbandonSetIDs returns task-set identifiers with a queue worktree
// binding, deduplicated and sorted for shell completion.
func CompleteAbandonSetIDs(d *Deps) ([]string, error) {
	bindings, err := binding.AllBindings(d.Tasks)
	if err != nil || len(bindings) == 0 {
		return nil, err
	}
	ids := make([]string, 0, len(bindings))
	for key := range bindings {
		ids = append(ids, setIDFromScopedKey(key))
	}
	return dedupeSortSetIDs(ids), nil
}

// CompleteBindWorktreeSetIDs returns all task-set identifiers (bound or not)
// for shell completion of `pop tasks bind-worktree`.
func CompleteBindWorktreeSetIDs(d *Deps) ([]string, error) {
	bindings, err := binding.AllBindings(d.Tasks)
	if err != nil {
		return nil, err
	}
	seen := make(map[string]struct{})
	for key := range bindings {
		id := setIDFromScopedKey(key)
		seen[id] = struct{}{}
	}
	mergeIDs, err := integration.SetIDsFromStore(d.Tasks)
	if err != nil {
		return nil, err
	}
	for _, id := range mergeIDs {
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
