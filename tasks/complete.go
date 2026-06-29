package tasks

import (
	"fmt"

	"github.com/glebglazov/pop/config"
	"github.com/glebglazov/pop/project"
)

// CompleteTaskOptions configures manually completing one task.
type CompleteTaskOptions struct {
	ResolveInput
	TaskPath string
}

// CompleteTaskResult is the outcome of manually completing an task.
type CompleteTaskResult struct {
	TaskSetID string
	TaskID    string
	// ProjectPath is the resolved checkout the completion ran against.
	ProjectPath string
	Refresh     *RefreshResult
}

// CompleteTask manually marks one task Done without running an agent.
func CompleteTask(opts CompleteTaskOptions) (*CompleteTaskResult, error) {
	return CompleteTaskWith(defaultDeps, project.DefaultDeps(), config.Load, opts)
}

// CompleteTaskWith manually completes an task using injected dependencies.
func CompleteTaskWith(d *Deps, pd *project.Deps, loadConfig func(string) (*config.Config, error), opts CompleteTaskOptions) (*CompleteTaskResult, error) {
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
		return nil, exitErr(ExitSetup, "complete requires a task path")
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
	if task.Status == "done" {
		return nil, exitErr(ExitNoRunnable, "task %q is already done", taskID)
	}

	for _, blocker := range task.BlockedBy {
		if !blockerSatisfied(m, blocker) {
			return nil, exitErr(ExitNoRunnable, "task %q blocked by %s; complete it first", taskID, blocker)
		}
	}

	priorStatus := task.Status
	summary := fmt.Sprintf("manually completed %s/%s (was %s)", taskSetID, taskID, priorStatus)
	if err := AppendProgress(d, m.Dir, task.File, "COMPLETE", summary); err != nil {
		return nil, manualRepairErr(err)
	}

	m.Tasks[idx].Status = "done"
	m.Tasks[idx].FailedAfter = nil
	if err := WriteManifestAtomic(d, m); err != nil {
		return nil, manualRepairErr(fmt.Errorf("update manifest after complete progress: %w", err))
	}

	afterRefresh, err := RefreshWith(d, resolved.DefinitionPath, statePath)
	if err != nil {
		return nil, exitErr(ExitOperational, "refresh after complete: %v", err)
	}

	return &CompleteTaskResult{TaskSetID: taskSetID, TaskID: taskID, ProjectPath: resolved.ProjectPath, Refresh: afterRefresh}, nil
}
