package queue

import (
	"github.com/glebglazov/pop/binding"
	"github.com/glebglazov/pop/tasks"
)

// RecordImplementMergeability computes and records Mergeability for a task set
// drained by `pop tasks implement` to Done in a linked worktree. It is the
// implement-path complement of the queue supervisor's Done-outcome mergeability
// computation and writes into the same state.Mergeability map so the
// Integration backlog view is trigger-agnostic (ADR-0036).
//
// It is a no-op when no worktree binding exists for (setID, runtimePath) —
// which covers trunk drains, since AdoptCurrentCheckout records no binding for
// the repository's main working tree — and when the repository is bare with no
// main working tree to merge into.
func RecordImplementMergeability(d *Deps, projectPath, runtimePath, setID, project string) error {
	if d == nil {
		d = DefaultDeps()
	}
	if d.Tasks == nil {
		d.Tasks = tasks.DefaultDeps()
	}

	// Only compute mergeability when a worktree binding exists for this set.
	// Trunk drains have no binding; they're never integrateable.
	store, err := binding.Load(d.Tasks)
	if err != nil {
		return err
	}
	id, err := tasks.ResolveRepositoryIdentity(d.Tasks, runtimePath)
	if err != nil {
		return err
	}
	b, ok := store.Get(binding.Key(id, setID))
	if !ok {
		return nil // trunk drain or unbound: not integrateable
	}

	// Resolve the main working tree as the merge target.
	mainPath, bare, err := gitMainWorktree(d, runtimePath)
	if err != nil {
		return err
	}
	if bare || mainPath == "" {
		return nil // bare repo: no trunk checkout to merge into
	}

	proj := project
	if proj == "" {
		proj = b.Project
	}

	merge, err := d.computeMergeability(mainPath, runtimePath)
	if err != nil {
		return err
	}
	merge.Project = proj
	merge.RuntimePath = runtimePath
	merge.SetID = setID

	state, err := EnsureDaemonState(d.Tasks)
	if err != nil {
		return err
	}
	return recordMergeability(d, state, projectPath, merge)
}
