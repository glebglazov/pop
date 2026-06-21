package queue

import (
	"io"

	"github.com/glebglazov/pop/tasks/binding"
	"github.com/glebglazov/pop/config"
	"github.com/glebglazov/pop/tasks"
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
		ResolveTeardownBase: func(b binding.Binding) (string, error) {
			scan, err := resolveBindingScan(d, cfg, b)
			if err != nil {
				return "", err
			}
			return scan.RuntimePath, nil
		},
		AfterUnbind: func(key, setID string, b binding.Binding, branch string) error {
			state, err := EnsureDaemonState(d.Tasks)
			if err != nil {
				return err
			}
			if state.Mergeability != nil {
				delete(state.Mergeability, key)
			}
			if err := WriteDaemonState(d.Tasks, state); err != nil {
				return err
			}
			return AppendJournalEntry(d.Tasks, JournalEntry{
				Event:       JournalEventAbandoned,
				Project:     b.Project,
				SetID:       setID,
				RuntimePath: b.RuntimePath,
				SourceRef:   branch,
				Source:      "human",
			})
		},
	}
}

func abandonNeedsConfirm(d *Deps, cfg *config.Config, state *DaemonState, setID string, wt WorktreeBinding) (bool, error) {
	if _, _, ok, err := findIntegrationRecord(state, setID); err != nil {
		return false, err
	} else if ok {
		return true, nil
	}
	if wt.Project == "" {
		return false, nil
	}
	failed, err := setHasStatus(d, cfg, wt, setID, tasks.StatusFailed)
	if err != nil {
		return false, err
	}
	return failed, nil
}

func setHasStatus(d *Deps, cfg *config.Config, binding WorktreeBinding, setID string, status tasks.TaskSetStatus) (bool, error) {
	scan, err := resolveBindingScan(d, cfg, binding)
	if err != nil {
		return false, err
	}
	refresh, err := d.refresh(scan.DefinitionPath)
	if err != nil {
		return false, err
	}
	for _, row := range refresh.Rows {
		if row.ID == setID && row.Status == status {
			return true, nil
		}
	}
	return false, nil
}

func resolveBindingScan(d *Deps, cfg *config.Config, binding WorktreeBinding) (projectScan, error) {
	return resolveIntegrationScan(d, cfg, MergeabilityRecord{
		Project:     binding.Project,
		RuntimePath: binding.RuntimePath,
	})
}
