package tasks

import (
	"fmt"

	"github.com/glebglazov/pop/config"
	"github.com/glebglazov/pop/project"
)

// ResetTaskOptions configures reopening one failed, skipped, or done task.
type ResetTaskOptions struct {
	ResolveInput
	TaskPath string
}

// ResetTaskResult is the outcome of resetting a failed task.
type ResetTaskResult struct {
	TaskSetID string
	TaskID    string
	Refresh   *RefreshResult
}

// ResetTask returns one failed, skipped, or done task to open status.
func ResetTask(opts ResetTaskOptions) (*ResetTaskResult, error) {
	return ResetTaskWith(defaultDeps, project.DefaultDeps(), config.Load, opts)
}

// ResetTaskWith resets a failed task using injected dependencies.
func ResetTaskWith(d *Deps, pd *project.Deps, loadConfig func(string) (*config.Config, error), opts ResetTaskOptions) (*ResetTaskResult, error) {
	resolved, err := ResolvePathsWith(d, pd, loadConfig, opts.ResolveInput)
	if err != nil {
		return nil, exitErr(ExitSetup, "%v", err)
	}

	statePath := StatePathFor(resolved.DefinitionPath)
	refresh, err := RefreshWith(d, resolved.DefinitionPath, statePath)
	if err != nil {
		return nil, exitErr(ExitSetup, "%v", err)
	}

	taskSetID, taskID, err := ResolveTaskFileTarget(refresh, opts.TaskPath)
	if err != nil {
		return nil, err
	}
	if err := RejectArchivedTaskSet(d, statePath, resolved.DefinitionPath, taskSetID); err != nil {
		return nil, err
	}
	if taskSetID == "" || taskID == "" {
		return nil, exitErr(ExitSetup, "open requires a task path")
	}

	m := refresh.Manifests[taskSetID]
	if m == nil {
		return nil, exitErr(ExitNoRunnable, "task set %q has no task manifest", taskSetID)
	}
	if !m.Valid {
		return nil, exitErr(ExitNoRunnable, "task set %q is malformed", taskSetID)
	}

	idx := -1
	for i, task := range m.Tasks {
		if task.ID == taskID {
			idx = i
			break
		}
	}
	if idx < 0 {
		return nil, exitErr(ExitNoRunnable, "%s", unknownTaskMessage(m, taskID))
	}

	task := m.Tasks[idx]
	// Any non-Open status reopens: Failed/Skipped (retry) and Done (undo a
	// completion, ADR-0053). Only an already-Open task is rejected.
	if task.Status == "open" {
		return nil, exitErr(ExitNoRunnable, "task %q is already open", taskID)
	}

	priorStatus := task.Status
	summary := fmt.Sprintf("reset %s/%s to open (was %s)", taskSetID, taskID, priorStatus)
	if err := AppendProgress(d, m.Dir, task.File, "RESET", summary); err != nil {
		return nil, manualRepairErr(err)
	}

	m.Tasks[idx].Status = "open"
	m.Tasks[idx].FailedAfter = nil
	if err := WriteManifestAtomic(d, m); err != nil {
		return nil, manualRepairErr(fmt.Errorf("update manifest after reset progress: %w", err))
	}

	afterRefresh, err := RefreshWith(d, resolved.DefinitionPath, statePath)
	if err != nil {
		return nil, exitErr(ExitOperational, "refresh after reset: %v", err)
	}

	return &ResetTaskResult{TaskSetID: taskSetID, TaskID: taskID, Refresh: afterRefresh}, nil
}
