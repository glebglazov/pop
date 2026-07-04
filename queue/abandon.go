package queue

import (
	"io"

	"github.com/glebglazov/pop/config"
	"github.com/glebglazov/pop/tasks"
	"github.com/glebglazov/pop/tasks/binding"
)

// AbandonResult describes the outcome of releasing a worktree binding.
type AbandonResult = binding.UnbindWorktreeResult

// AbandonOptions controls confirmation for abandon.
type AbandonOptions = binding.UnbindWorktreeOptions

// Abandon releases a set's worktree binding without integrating.
func Abandon(d *Deps, cfg *config.Config, setID string, out io.Writer) (AbandonResult, error) {
	return AbandonWithOptions(d, cfg, setID, out, AbandonOptions{In: tasks.NonInteractiveReader{}})
}

// AbandonWithOptions releases a set's worktree binding without integrating.
func AbandonWithOptions(d *Deps, cfg *config.Config, setID string, out io.Writer, opts AbandonOptions) (AbandonResult, error) {
	d = ensureQueueDeps(d)
	return binding.UnbindWorktree(d.Tasks, d.Project, cfg, setID, opts, queueUnbindHooks(d, cfg), out)
}

// AbandonBindingWithOptions releases the binding stored at bindingKey using
// the same implementation as AbandonWithOptions. It is for callers, such as the
// dashboard, that already resolved a highlighted row to a repository-scoped key.
func AbandonBindingWithOptions(d *Deps, cfg *config.Config, bindingKey, setID string, out io.Writer, opts AbandonOptions) (AbandonResult, error) {
	d = ensureQueueDeps(d)
	return binding.UnbindBindingKey(d.Tasks, d.Project, cfg, bindingKey, setID, opts, queueUnbindHooks(d, cfg), out)
}

func queueUnbindHooks(d *Deps, cfg *config.Config) binding.LifecycleHooks {
	return binding.LifecycleHooks{
		ReadLock: d.readLock,
		NeedsConfirm: func(setID string, b binding.Binding) (bool, error) {
			state, err := EnsureDaemonState(d.Tasks)
			if err != nil {
				return false, err
			}
			return abandonNeedsConfirm(d, cfg, state, setID, b)
		},
	}
}

// abandonNeedsConfirm reports whether unbinding setID should prompt first. With
// integration removed (ADR-0070), the only confirm trigger is a terminal set
// state whose work would be quietly forgotten — Done or Failed.
func abandonNeedsConfirm(d *Deps, cfg *config.Config, state *DaemonState, setID string, wt WorktreeBinding) (bool, error) {
	_ = state
	return setHasStatus(d, wt, setID, tasks.StatusDone, tasks.StatusFailed)
}

func setHasStatus(d *Deps, wt WorktreeBinding, setID string, statuses ...tasks.TaskSetStatus) (bool, error) {
	defPath, err := bindingDefinitionPath(d, wt)
	if err != nil || defPath == "" {
		return false, err
	}
	refresh, err := d.refresh(defPath)
	if err != nil {
		return false, err
	}
	for _, row := range refresh.Rows {
		if row.ID != setID {
			continue
		}
		for _, status := range statuses {
			if row.Status == status {
				return true, nil
			}
		}
	}
	return false, nil
}

// bindingDefinitionPath resolves the canonical Task-set definition path for a
// binding from its runtime checkout, so the set's manifest can be refreshed.
func bindingDefinitionPath(d *Deps, wt WorktreeBinding) (string, error) {
	if d == nil || d.Tasks == nil || wt.RuntimePath == "" {
		return "", nil
	}
	id, err := tasks.ResolveRepositoryIdentity(d.Tasks, wt.RuntimePath)
	if err != nil {
		return "", err
	}
	return tasks.CanonicalDefinitionPathWith(d.Tasks, id.TasksDir)
}
