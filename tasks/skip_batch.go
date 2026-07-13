package tasks

import (
	"fmt"

	"github.com/glebglazov/pop/config"
	"github.com/glebglazov/pop/project"
)

// BuildSkipSelection lists every task in manifest order shaped by the `skip`
// eligibility predicate: Open checkable, already-Skipped locked at-target,
// Done/Failed inert locked. Returns nil for a nil/invalid manifest.
func BuildSkipSelection(m *Manifest) []SelectionRow {
	return BuildSelection(m, skipEligibility)
}

// SkipSelectionContext carries the resolved whole-set Multi-task selection for
// `skip` — the canonical set ID and the rows to offer — so a caller can run the
// interactive picker before applying a batch.
type SkipSelectionContext struct {
	TaskSetID string
	Rows      []SelectionRow
}

// SkipTasksOptions configures the atomic whole-set batch skip.
type SkipTasksOptions struct {
	ResolveInput
	// TaskSetTarget is a whole-set target reference (<task-set> or <task-set>/).
	TaskSetTarget string
	// SelectedTaskIDs are the tasks chosen in the Multi-task selection.
	SelectedTaskIDs []string
}

// SkipTransition records one task's status change in a batch skip.
type SkipTransition struct {
	TaskID string
	File   string
	Prior  TaskStatus
}

// SkipTasksResult is the outcome of a whole-set batch skip.
type SkipTasksResult struct {
	TaskSetID   string
	Transitions []SkipTransition
	Refresh     *RefreshResult
}

// LoadSkipSelectionWith resolves a whole-set target and builds the rows for the
// Multi-task selection without writing anything.
func LoadSkipSelectionWith(d *Deps, pd *project.Deps, loadConfig func(string) (*config.Config, error), in ResolveInput, target string) (*SkipSelectionContext, error) {
	_, _, taskSetID, m, err := resolveTaskSetForBatch(d, pd, loadConfig, in, target, "skip")
	if err != nil {
		return nil, err
	}
	return &SkipSelectionContext{TaskSetID: taskSetID, Rows: BuildSkipSelection(m)}, nil
}

// SkipTasks manually defers several open tasks to skipped in one atomic batch.
func SkipTasks(opts SkipTasksOptions) (*SkipTasksResult, error) {
	return SkipTasksWith(defaultDeps, project.DefaultDeps(), config.Load, opts)
}

// SkipTasksWith applies a whole-set batch skip using injected dependencies.
// Each skip is independent — no ordering needed — so the checked rows apply
// unordered (deterministically in manifest order) as one manifest write plus
// one SKIP progress record per task. An empty selection is a clean no-op.
func SkipTasksWith(d *Deps, pd *project.Deps, loadConfig func(string) (*config.Config, error), opts SkipTasksOptions) (*SkipTasksResult, error) {
	resolved, refresh, taskSetID, m, err := resolveTaskSetForBatch(d, pd, loadConfig, opts.ResolveInput, opts.TaskSetTarget, "skip")
	if err != nil {
		return nil, err
	}
	statePath := StatePathFor(resolved.DefinitionPath)

	// Empty selection: a clean no-op exit, no writes.
	if len(opts.SelectedTaskIDs) == 0 {
		return &SkipTasksResult{TaskSetID: taskSetID, Refresh: refresh}, nil
	}

	indexByID := make(map[string]int, len(m.Tasks))
	for i, task := range m.Tasks {
		indexByID[task.ID] = i
	}

	// Validate the whole batch before any write: only Open tasks can be
	// skipped. Reject the entire batch (no writes) if any selection is not
	// eligible, naming the offender.
	selected := make(map[string]bool, len(opts.SelectedTaskIDs))
	for _, id := range opts.SelectedTaskIDs {
		idx, ok := indexByID[id]
		if !ok {
			return nil, exitErr(ExitNoRunnable, "%s", unknownTaskMessage(m, id))
		}
		status := m.Tasks[idx].Status
		if status != TaskOpen {
			if status == TaskSkipped {
				return nil, exitErr(ExitNoRunnable, "task %q is already skipped", id)
			}
			return nil, exitErr(ExitNoRunnable, "task %q is %s; skip requires an open task", id, status)
		}
		selected[id] = true
	}

	// Build one transition op per selected task in manifest order (each skip is
	// independent) and route the whole batch through the Task-transition
	// chokepoint as Human — one atomic manifest write plus one SKIP progress
	// record per task.
	ops := make([]TransitionOp, 0, len(selected))
	transitions := make([]SkipTransition, 0, len(selected))
	for _, task := range m.Tasks {
		if !selected[task.ID] {
			continue
		}
		summary := fmt.Sprintf("skipped %s/%s (was %s)", taskSetID, task.ID, task.Status)
		ops = append(ops, TransitionOp{TaskID: task.ID, To: TaskSkipped, Actor: ActorHuman, Marker: "SKIP", Summary: summary})
		transitions = append(transitions, SkipTransition{TaskID: task.ID, File: task.File, Prior: task.Status})
	}
	if err := ApplyTransitions(d, m, resolved.ProjectPath, ops); err != nil {
		return nil, err
	}

	afterRefresh, err := RefreshWith(d, resolved.DefinitionPath, statePath)
	if err != nil {
		return nil, exitErr(ExitOperational, "refresh after skip: %v", err)
	}

	return &SkipTasksResult{TaskSetID: taskSetID, Transitions: transitions, Refresh: afterRefresh}, nil
}
