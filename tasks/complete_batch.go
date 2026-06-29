package tasks

import (
	"fmt"

	"github.com/glebglazov/pop/config"
	"github.com/glebglazov/pop/project"
)

// CompleteSelectionRow is one task offered in the whole-set Multi-task
// selection. It is the shared SelectionRow shaped by the `complete` eligibility
// predicate: Done tasks are Locked, every other task is checkable.
type CompleteSelectionRow = SelectionRow

// BuildCompleteSelection lists every task in manifest order, marking already
// Done tasks Locked. Returns nil for a nil/invalid manifest.
func BuildCompleteSelection(m *Manifest) []CompleteSelectionRow {
	return BuildSelection(m, completeEligibility)
}

// CompleteTasksOptions configures the atomic whole-set batch completion.
type CompleteTasksOptions struct {
	ResolveInput
	// TaskSetTarget is a whole-set target reference (<task-set> or <task-set>/).
	TaskSetTarget string
	// SelectedTaskIDs are the tasks chosen in the Multi-task selection.
	SelectedTaskIDs []string
}

// CompleteTransition records one task's status change in a batch completion.
type CompleteTransition struct {
	TaskID string
	File   string
	Prior  string
}

// CompleteTasksResult is the outcome of a whole-set batch completion.
type CompleteTasksResult struct {
	TaskSetID   string
	Transitions []CompleteTransition
	// ProjectPath is the resolved checkout the batch ran against.
	ProjectPath string
	Refresh     *RefreshResult
}

// CompleteSelectionContext carries the resolved whole-set Multi-task selection
// — the canonical set ID and the rows to offer — so a caller can run the
// interactive picker before applying a batch.
type CompleteSelectionContext struct {
	TaskSetID string
	Rows      []CompleteSelectionRow
}

// resolveTaskSetForBatch resolves a whole-set target to its canonical ID and
// validated manifest, shared by every verb's selection loader and batch
// applier. verb names the operation for error messages (e.g. "complete").
func resolveTaskSetForBatch(d *Deps, pd *project.Deps, loadConfig func(string) (*config.Config, error), in ResolveInput, target, verb string) (*ResolvedPaths, *RefreshResult, string, *Manifest, error) {
	resolved, err := ResolvePathsWith(d, pd, loadConfig, in)
	if err != nil {
		return nil, nil, "", nil, exitErr(ExitSetup, "%v", err)
	}

	statePath := StatePathFor(resolved.DefinitionPath)
	refresh, err := RefreshWith(d, resolved.DefinitionPath, statePath)
	if err != nil {
		return nil, nil, "", nil, exitErr(ExitSetup, "%v", err)
	}

	taskSetID, err := ResolveTaskSetTarget(refresh, target)
	if err != nil {
		return nil, nil, "", nil, err
	}
	if err := RejectArchivedTaskSet(d, statePath, resolved.DefinitionPath, taskSetID); err != nil {
		return nil, nil, "", nil, err
	}
	if taskSetID == "" {
		return nil, nil, "", nil, exitErr(ExitSetup, "%s requires a task set target", verb)
	}

	m := refresh.Manifests[taskSetID]
	if m == nil {
		return nil, nil, "", nil, exitErr(ExitNoRunnable, "task set %q has no task manifest", taskSetID)
	}
	if !m.Valid {
		return nil, nil, "", nil, exitErr(ExitNoRunnable, "task set %q is malformed", taskSetID)
	}
	return resolved, refresh, taskSetID, m, nil
}

// LoadCompleteSelectionWith resolves a whole-set target and builds the rows for
// the Multi-task selection without writing anything.
func LoadCompleteSelectionWith(d *Deps, pd *project.Deps, loadConfig func(string) (*config.Config, error), in ResolveInput, target string) (*CompleteSelectionContext, error) {
	_, _, taskSetID, m, err := resolveTaskSetForBatch(d, pd, loadConfig, in, target, "complete")
	if err != nil {
		return nil, err
	}
	return &CompleteSelectionContext{TaskSetID: taskSetID, Rows: BuildCompleteSelection(m)}, nil
}

// CompleteTasks manually marks several tasks Done in one atomic batch.
func CompleteTasks(opts CompleteTasksOptions) (*CompleteTasksResult, error) {
	return CompleteTasksWith(defaultDeps, project.DefaultDeps(), config.Load, opts)
}

// CompleteTasksWith applies a whole-set batch completion using injected
// dependencies. The selected rows are validated as one batch, applied in
// blocked_by topological order, and persisted as one manifest write plus one
// COMPLETE progress record per task. An empty selection is a clean no-op.
func CompleteTasksWith(d *Deps, pd *project.Deps, loadConfig func(string) (*config.Config, error), opts CompleteTasksOptions) (*CompleteTasksResult, error) {
	resolved, refresh, taskSetID, m, err := resolveTaskSetForBatch(d, pd, loadConfig, opts.ResolveInput, opts.TaskSetTarget, "complete")
	if err != nil {
		return nil, err
	}
	statePath := StatePathFor(resolved.DefinitionPath)

	// Empty selection: a clean no-op exit, no writes.
	if len(opts.SelectedTaskIDs) == 0 {
		return &CompleteTasksResult{TaskSetID: taskSetID, ProjectPath: resolved.ProjectPath, Refresh: refresh}, nil
	}

	indexByID := make(map[string]int, len(m.Tasks))
	for i, task := range m.Tasks {
		indexByID[task.ID] = i
	}

	selected := make(map[string]bool, len(opts.SelectedTaskIDs))
	for _, id := range opts.SelectedTaskIDs {
		idx, ok := indexByID[id]
		if !ok {
			return nil, exitErr(ExitNoRunnable, "%s", unknownTaskMessage(m, id))
		}
		if m.Tasks[idx].Status == "done" {
			return nil, exitErr(ExitNoRunnable, "task %q is already done", id)
		}
		selected[id] = true
	}

	// Pre-validate the whole batch before any write: a selected task's blocker
	// is satisfied if already Done/Skipped or also selected in this batch.
	// Reject the entire batch on the first unsatisfied, unselected blocker.
	for _, task := range m.Tasks {
		if !selected[task.ID] {
			continue
		}
		for _, blocker := range task.BlockedBy {
			if blockerSatisfied(m, blocker) || selected[blocker] {
				continue
			}
			return nil, exitErr(ExitNoRunnable, "task %q blocked by %s; complete it first or select it too", task.ID, blocker)
		}
	}

	order, err := topoOrderSelected(m, selected)
	if err != nil {
		return nil, exitErr(ExitNoRunnable, "%v", err)
	}

	// Append progress records first, then one manifest write — matching the
	// single-task ordering so a crash leaves a recoverable trail.
	transitions := make([]CompleteTransition, 0, len(order))
	for _, id := range order {
		idx := indexByID[id]
		task := m.Tasks[idx]
		summary := fmt.Sprintf("manually completed %s/%s (was %s)", taskSetID, task.ID, task.Status)
		if err := AppendProgress(d, m.Dir, task.File, "COMPLETE", summary); err != nil {
			return nil, manualRepairErr(err)
		}
		transitions = append(transitions, CompleteTransition{TaskID: task.ID, File: task.File, Prior: task.Status})
	}

	for _, id := range order {
		idx := indexByID[id]
		m.Tasks[idx].Status = "done"
		m.Tasks[idx].FailedAfter = nil
	}
	if err := WriteManifestAtomic(d, m); err != nil {
		return nil, manualRepairErr(fmt.Errorf("update manifest after complete progress: %w", err))
	}

	afterRefresh, err := RefreshWith(d, resolved.DefinitionPath, statePath)
	if err != nil {
		return nil, exitErr(ExitOperational, "refresh after complete: %v", err)
	}

	return &CompleteTasksResult{TaskSetID: taskSetID, Transitions: transitions, ProjectPath: resolved.ProjectPath, Refresh: afterRefresh}, nil
}

// topoOrderSelected orders the selected task IDs so each task follows its
// selected blockers (blocked_by topological order), preserving manifest order
// among independent tasks. A dependency cycle among the selection is an error.
func topoOrderSelected(m *Manifest, selected map[string]bool) ([]string, error) {
	emitted := make(map[string]bool, len(selected))
	var order []string

	for len(order) < len(selected) {
		progress := false
		for _, task := range m.Tasks {
			if !selected[task.ID] || emitted[task.ID] {
				continue
			}
			ready := true
			for _, blocker := range task.BlockedBy {
				if selected[blocker] && !emitted[blocker] {
					ready = false
					break
				}
			}
			if !ready {
				continue
			}
			emitted[task.ID] = true
			order = append(order, task.ID)
			progress = true
		}
		if !progress {
			return nil, fmt.Errorf("selected tasks have a blocked_by cycle")
		}
	}
	return order, nil
}
