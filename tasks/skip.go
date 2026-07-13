package tasks

import (
	"fmt"

	"github.com/glebglazov/pop/config"
	"github.com/glebglazov/pop/project"
)

// SkipTaskOptions configures deferring one open task.
type SkipTaskOptions struct {
	ResolveInput
	TaskPath string
}

// SkipTaskResult is the outcome of skipping an task.
type SkipTaskResult struct {
	TaskSetID string
	TaskID    string
	Refresh   *RefreshResult
}

// SkipTask defers one open task to skipped status.
func SkipTask(opts SkipTaskOptions) (*SkipTaskResult, error) {
	return SkipTaskWith(defaultDeps, project.DefaultDeps(), config.Load, opts)
}

// SkipTaskWith defers an open task using injected dependencies.
func SkipTaskWith(d *Deps, pd *project.Deps, loadConfig func(string) (*config.Config, error), opts SkipTaskOptions) (*SkipTaskResult, error) {
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
		return nil, exitErr(ExitSetup, "skip requires a task path")
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
	if task.Status != "open" {
		return nil, exitErr(ExitNoRunnable, "task %q is %s; skip requires an open task", taskID, task.Status)
	}

	summary := fmt.Sprintf("skipped %s/%s", taskSetID, taskID)
	// Route the status write through the Task-transition chokepoint as Human;
	// the verb keeps its own open-only precondition above.
	if err := ApplyTransitions(d, m, resolved.ProjectPath, []TransitionOp{{
		TaskID:  taskID,
		To:      TaskSkipped,
		Actor:   ActorHuman,
		Marker:  "SKIP",
		Summary: summary,
	}}); err != nil {
		return nil, err
	}

	afterRefresh, err := RefreshWith(d, resolved.DefinitionPath, statePath)
	if err != nil {
		return nil, exitErr(ExitOperational, "refresh after skip: %v", err)
	}

	return &SkipTaskResult{TaskSetID: taskSetID, TaskID: taskID, Refresh: afterRefresh}, nil
}
