package queue

import (
	"io"

	"github.com/glebglazov/pop/config"
	"github.com/glebglazov/pop/tasks/binding"
)

// FoldOptions controls confirmation for managed-worktree teardown after fold.
type FoldOptions = binding.FoldOptions

// FoldResult describes a successful fold.
type FoldResult = binding.FoldResult

// FoldSet folds a task set through the same implementation as `pop tasks fold`.
func FoldSet(d *Deps, cfg *config.Config, ref SetRef, out io.Writer, opts FoldOptions) (FoldResult, error) {
	d = ensureQueueDeps(d)
	return binding.Fold(d.Tasks, d.Project, cfg, ref.SetID, opts, queueFoldHooks(d), out)
}

// PreflightFold applies fold precondition checks for a dashboard row without
// rebasing or releasing its binding. A fold rebase left in progress is allowed
// through so the dashboard can hand the operator to the fold pane's conflict
// prompt.
func PreflightFold(d *Deps, cfg *config.Config, ref SetRef) error {
	d = ensureQueueDeps(d)
	return binding.PreflightFold(d.Tasks, cfg, ref.SetID)
}

func queueFoldHooks(d *Deps) binding.LifecycleHooks {
	return binding.LifecycleHooks{ReadLock: d.readLock}
}
