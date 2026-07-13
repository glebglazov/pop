package tasks

import (
	"fmt"

	"github.com/glebglazov/pop/config"
	"github.com/glebglazov/pop/project"
)

// BuildOpenSelection lists every task in manifest order shaped by the `open`
// eligibility predicate: Failed/Skipped checkable, already-Open locked
// at-target, Done inert locked. Returns nil for a nil/invalid manifest.
func BuildOpenSelection(m *Manifest) []SelectionRow {
	return BuildSelection(m, openEligibility)
}

// OpenSelectionContext carries the resolved whole-set Multi-task selection for
// `open` — the canonical set ID and the rows to offer — so a caller can run the
// interactive picker before applying a batch.
type OpenSelectionContext struct {
	TaskSetID string
	Rows      []SelectionRow
}

// OpenTasksOptions configures the atomic whole-set batch reopen.
type OpenTasksOptions struct {
	ResolveInput
	// TaskSetTarget is a whole-set target reference (<task-set> or <task-set>/).
	TaskSetTarget string
	// SelectedTaskIDs are the tasks chosen in the Multi-task selection.
	SelectedTaskIDs []string
}

// OpenTransition records one task's status change in a batch reopen.
type OpenTransition struct {
	TaskID string
	File   string
	Prior  string
}

// OpenTasksResult is the outcome of a whole-set batch reopen.
type OpenTasksResult struct {
	TaskSetID   string
	Transitions []OpenTransition
	Refresh     *RefreshResult
}

// LoadOpenSelectionWith resolves a whole-set target and builds the rows for the
// Multi-task selection without writing anything.
func LoadOpenSelectionWith(d *Deps, pd *project.Deps, loadConfig func(string) (*config.Config, error), in ResolveInput, target string) (*OpenSelectionContext, error) {
	_, _, taskSetID, m, err := resolveTaskSetForBatch(d, pd, loadConfig, in, target, "open")
	if err != nil {
		return nil, err
	}
	return &OpenSelectionContext{TaskSetID: taskSetID, Rows: BuildOpenSelection(m)}, nil
}

// OpenTasks manually reopens several non-open tasks (failed, skipped, or done)
// back to open in one atomic batch.
func OpenTasks(opts OpenTasksOptions) (*OpenTasksResult, error) {
	return OpenTasksWith(defaultDeps, project.DefaultDeps(), config.Load, opts)
}

// OpenTasksWith applies a whole-set batch reopen using injected dependencies.
// Each reopen is independent — there is no blocked_by check on reset — so the
// checked rows apply unordered (deterministically in manifest order) as one
// manifest write plus one RESET progress record per task. An empty selection is
// a clean no-op.
func OpenTasksWith(d *Deps, pd *project.Deps, loadConfig func(string) (*config.Config, error), opts OpenTasksOptions) (*OpenTasksResult, error) {
	resolved, refresh, taskSetID, m, err := resolveTaskSetForBatch(d, pd, loadConfig, opts.ResolveInput, opts.TaskSetTarget, "open")
	if err != nil {
		return nil, err
	}
	statePath := StatePathFor(resolved.DefinitionPath)

	// Empty selection: a clean no-op exit, no writes.
	if len(opts.SelectedTaskIDs) == 0 {
		return &OpenTasksResult{TaskSetID: taskSetID, Refresh: refresh}, nil
	}

	indexByID := make(map[string]int, len(m.Tasks))
	for i, task := range m.Tasks {
		indexByID[task.ID] = i
	}

	// Validate the whole batch before any write: reject (no writes) if any
	// selection cannot reopen, naming it. CanReopen is the shared policy.
	selected := make(map[string]bool, len(opts.SelectedTaskIDs))
	for _, id := range opts.SelectedTaskIDs {
		idx, ok := indexByID[id]
		if !ok {
			return nil, exitErr(ExitNoRunnable, "%s", unknownTaskMessage(m, id))
		}
		if !CanReopen(m.Tasks[idx].Status) {
			return nil, exitErr(ExitNoRunnable, "task %q is already open", id)
		}
		selected[id] = true
	}

	// Build one transition op per selected task in manifest order (each reopen is
	// independent) and route the whole batch through the Task-transition
	// chokepoint as Human — one atomic manifest write plus one RESET progress
	// record per task.
	ops := make([]TransitionOp, 0, len(selected))
	transitions := make([]OpenTransition, 0, len(selected))
	for _, task := range m.Tasks {
		if !selected[task.ID] {
			continue
		}
		summary := fmt.Sprintf("reset %s/%s to open (was %s)", taskSetID, task.ID, task.Status)
		ops = append(ops, TransitionOp{TaskID: task.ID, To: TaskOpen, Actor: ActorHuman, Marker: "RESET", Summary: summary})
		transitions = append(transitions, OpenTransition{TaskID: task.ID, File: task.File, Prior: task.Status})
	}
	if err := ApplyTransitions(d, m, ops); err != nil {
		return nil, err
	}
	// Leaving the terminal zone ends the verification episode: drop any cached
	// verdict so the set must re-verify after the batch reopen (ADR-0096).
	if id, idErr := ResolveRepositoryIdentity(d, resolved.ProjectPath); idErr == nil {
		invalidateVerifyVerdicts(d, id.CommonDir, taskSetID)
	}

	afterRefresh, err := RefreshWith(d, resolved.DefinitionPath, statePath)
	if err != nil {
		return nil, exitErr(ExitOperational, "refresh after open: %v", err)
	}

	return &OpenTasksResult{TaskSetID: taskSetID, Transitions: transitions, Refresh: afterRefresh}, nil
}
