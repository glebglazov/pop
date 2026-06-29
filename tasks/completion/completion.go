// Package completion provides shell tab-completion candidate lists for the
// `pop tasks` binding verbs. It reads the binding store (owned under tasks/) and
// returns bare Task set identifiers for the shell. It lives beside those stores
// rather than in queue/ because the verbs it completes (bind-worktree,
// unbind-worktree) moved out of queue into tasks (ADR-0038); their completion
// follows them home.
package completion

import (
	"sort"
	"strings"

	"github.com/glebglazov/pop/tasks"
	"github.com/glebglazov/pop/tasks/binding"
)

// AbandonSetIDs returns task-set identifiers that currently hold a worktree
// binding, deduplicated and sorted, for completing `pop tasks unbind-worktree`.
func AbandonSetIDs(td *tasks.Deps) ([]string, error) {
	if td == nil {
		td = tasks.DefaultDeps()
	}
	bindings, err := binding.AllBindings(td)
	if err != nil || len(bindings) == 0 {
		return nil, err
	}
	ids := make([]string, 0, len(bindings))
	for key := range bindings {
		ids = append(ids, binding.SetIDFromKey(key))
	}
	return dedupeSortSetIDs(ids), nil
}

// BindWorktreeSetIDs returns every bound task-set identifier, deduplicated and
// sorted, for completing `pop tasks bind-worktree`.
func BindWorktreeSetIDs(td *tasks.Deps) ([]string, error) {
	if td == nil {
		td = tasks.DefaultDeps()
	}
	bindings, err := binding.AllBindings(td)
	if err != nil {
		return nil, err
	}
	seen := make(map[string]struct{})
	for key := range bindings {
		seen[binding.SetIDFromKey(key)] = struct{}{}
	}
	ids := make([]string, 0, len(seen))
	for id := range seen {
		ids = append(ids, id)
	}
	return dedupeSortSetIDs(ids), nil
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
