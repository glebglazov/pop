package queue

import (
	"fmt"
	"strings"

	"github.com/glebglazov/pop/tasks"
)

// applyDashboardStatusVerb applies a Work-dashboard status-submenu verb
// in-process (ADR-0158): no subprocess, no TUI suspend. complete/open/skip
// write every unlocked task in the set; archive/unarchive flip the set flag.
func applyDashboardStatusVerb(d *Deps, row DashboardRow, verb string) error {
	d = ensureQueueDeps(d)
	switch verb {
	case "archive":
		return d.archiveSet(row.DefPath, row.ID)
	case "unarchive":
		return d.unarchiveSet(row.DefPath, row.ID)
	case "complete", "open", "skip":
		return applyDashboardStatusBatch(d, row, verb)
	default:
		return fmt.Errorf("unknown status verb %q", verb)
	}
}

func applyDashboardStatusBatch(d *Deps, row DashboardRow, verb string) error {
	in := dashboardStatusResolveInput(row)
	loadConfig := d.loadConfig()
	pd := d.projectDeps()
	td := d.Tasks

	var (
		ids []string
		err error
	)
	switch verb {
	case "complete":
		ctx, loadErr := tasks.LoadCompleteSelectionWith(td, pd, loadConfig, in, row.ID)
		if loadErr != nil {
			return loadErr
		}
		ids = unlockedSelectionIDs(ctx.Rows)
		_, err = tasks.CompleteTasksWith(td, pd, loadConfig, tasks.CompleteTasksOptions{
			ResolveInput:    in,
			TaskSetTarget:   row.ID,
			SelectedTaskIDs: ids,
		})
	case "open":
		ctx, loadErr := tasks.LoadOpenSelectionWith(td, pd, loadConfig, in, row.ID)
		if loadErr != nil {
			return loadErr
		}
		ids = unlockedSelectionIDs(ctx.Rows)
		_, err = tasks.OpenTasksWith(td, pd, loadConfig, tasks.OpenTasksOptions{
			ResolveInput:    in,
			TaskSetTarget:   row.ID,
			SelectedTaskIDs: ids,
		})
	case "skip":
		ctx, loadErr := tasks.LoadSkipSelectionWith(td, pd, loadConfig, in, row.ID)
		if loadErr != nil {
			return loadErr
		}
		ids = unlockedSelectionIDs(ctx.Rows)
		_, err = tasks.SkipTasksWith(td, pd, loadConfig, tasks.SkipTasksOptions{
			ResolveInput:    in,
			TaskSetTarget:   row.ID,
			SelectedTaskIDs: ids,
		})
	default:
		return fmt.Errorf("unknown status batch verb %q", verb)
	}
	return err
}

func unlockedSelectionIDs(rows []tasks.SelectionRow) []string {
	var ids []string
	for _, r := range rows {
		if !r.Locked {
			ids = append(ids, r.TaskID)
		}
	}
	return ids
}

func dashboardStatusResolveInput(row DashboardRow) tasks.ResolveInput {
	cwd := strings.TrimSpace(row.ProjectPath)
	if cwd == "" {
		cwd = strings.TrimSpace(row.RuntimePath)
	}
	if cwd == "" {
		cwd = strings.TrimSpace(row.DefPath)
	}
	return tasks.ResolveInput{DefinitionOverride: row.DefPath, CWD: cwd}
}
