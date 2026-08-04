package setkind

import (
	"fmt"

	"github.com/glebglazov/pop/config"
	"github.com/glebglazov/pop/tasks"
	"github.com/glebglazov/pop/work"
)

// The Task set's status writes as its own kind performs them (ADR-0186). They ran
// on the Work dashboard before, behind a switch over five verb strings the
// dashboard also authored; the behaviour is unchanged and deliberately so — every
// one still writes in-process (ADR-0158), with no subprocess and no TUI suspend —
// but the vocabulary is now this kind's, so a surface offering the submenu names
// no kind to fill it.

// applySetVerb writes one status over every unlocked task in the set: the
// whole-set half of complete/open/skip. Locked tasks are skipped rather than
// refused, because a set holding one is still a set the operator meant to mark
// done — the lock exists to protect the running task, not to veto the others.
func (k *Kind) applySetVerb(c work.Container, verb work.Verb) error {
	in := resolveInput(c)
	loadConfig := k.loadConfig()
	td, pd := k.d.Tasks, k.d.Project

	switch verb {
	case VerbComplete:
		ctx, err := tasks.LoadCompleteSelectionWith(td, pd, loadConfig, in, c.ID)
		if err != nil {
			return err
		}
		_, err = tasks.CompleteTasksWith(td, pd, loadConfig, tasks.CompleteTasksOptions{
			ResolveInput:    in,
			TaskSetTarget:   c.ID,
			SelectedTaskIDs: unlockedSelectionIDs(ctx.Rows),
		})
		return err
	case VerbOpen:
		ctx, err := tasks.LoadOpenSelectionWith(td, pd, loadConfig, in, c.ID)
		if err != nil {
			return err
		}
		_, err = tasks.OpenTasksWith(td, pd, loadConfig, tasks.OpenTasksOptions{
			ResolveInput:    in,
			TaskSetTarget:   c.ID,
			SelectedTaskIDs: unlockedSelectionIDs(ctx.Rows),
		})
		return err
	case VerbSkip:
		ctx, err := tasks.LoadSkipSelectionWith(td, pd, loadConfig, in, c.ID)
		if err != nil {
			return err
		}
		_, err = tasks.SkipTasksWith(td, pd, loadConfig, tasks.SkipTasksOptions{
			ResolveInput:    in,
			TaskSetTarget:   c.ID,
			SelectedTaskIDs: unlockedSelectionIDs(ctx.Rows),
		})
		return err
	default:
		return work.UnknownVerb(k.ID(), verb)
	}
}

// setArchived flips the set's reversible archived flag. It touches only Task
// state, leaving any Worktree binding intact: archiving is a view decision, and
// an archived set that still holds a managed checkout is exactly the row the
// clean-up reminder exists for (ADR-0070).
func (k *Kind) setArchived(c work.Container, archived bool) error {
	if c.DefPath == "" {
		return fmt.Errorf("setkind: %s carries no definition path to archive against", c.ID)
	}
	if k.d.SetArchived != nil {
		return k.d.SetArchived(c.DefPath, c.ID, archived)
	}
	return tasks.SetTaskSetArchived(k.d.Tasks, c.DefPath, []string{c.ID}, archived)
}

// unlockedSelectionIDs is every task in a selection a status write may touch.
func unlockedSelectionIDs(rows []tasks.SelectionRow) []string {
	var ids []string
	for _, r := range rows {
		if !r.Locked {
			ids = append(ids, r.TaskID)
		}
	}
	return ids
}

// loadConfig is the kind's config loader, defaulting to the real one.
func (k *Kind) loadConfig() func(string) (*config.Config, error) {
	if k.d.LoadConfig != nil {
		return k.d.LoadConfig
	}
	return config.Load
}
