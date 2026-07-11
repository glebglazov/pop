package tasks

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"
)

// Selection is the Task set and task chosen for execution.
type Selection struct {
	TaskSetID string
	TaskID    string
	TaskPath  string
	TaskFile  string
	Manifest  *Manifest
	Task      Task
	TaskIndex int
	// HITLGate marks a selection that opens the named HITL task's gate rather
	// than running an agent. It is set only by an explicit `<set>/<hitl>.md`
	// target naming a ready HITL task (its blockers all satisfied); every AFK
	// selection leaves it false, so the executor keeps running agents as before.
	HITLGate bool
}

// SelectTaskSet chooses the Task set to drain using the same readiness and failed-set
// gates as SelectTask, without selecting an task. The returned bool reports whether
// the selection is a HITL fallback: no Ready Task set existed and the choice is the
// sole attendable Human-blocked Task set, which Drain frames as "No runnable AFK work".
func SelectTaskSet(refresh *RefreshResult, taskSetOverride string) (string, bool, error) {
	if taskSetOverride != "" {
		id, err := selectExplicitTaskSet(refresh, taskSetOverride)
		return id, false, err
	}
	return selectAutomaticTaskSet(refresh)
}

func selectAutomaticTaskSet(refresh *RefreshResult) (string, bool, error) {
	manifests := refresh.Manifests
	if manifests == nil {
		manifests = make(map[string]*Manifest)
	}
	// Intentional divergence from the queue's selectReadySets (supervisor dispatch):
	// this is the interactive local picker for `pop tasks implement`/`drain`. It
	// shares the priority-desc/RegIndex ordering — refresh.Rows arrives already
	// sorted that way (tasks/render.go orderRows) so first-Ready-wins == highest
	// priority — but deliberately drops the dispatch-only gates. It ignores the
	// AutoDrain flag (a human drives this pick, so consent to unattended drain is
	// irrelevant), applies no backoff/parking or quota-recovery waits (those govern
	// the unattended supervisor, not a person at the terminal), and falls back to a
	// HITL gate when no Ready set exists. Do not fold these back into the queue
	// selector: the two answer different questions.
	for _, row := range refresh.Rows {
		if row.Status != StatusReady {
			continue
		}
		m := manifests[row.ID]
		if m == nil || !m.Valid {
			continue
		}
		if _, err := firstEligibleTask(row.ID, m); err != nil {
			continue
		}
		return row.ID, false, nil
	}
	// No Ready Task set. Fall back to the HITL gate only when exactly one Task set
	// is Human-blocked by an open HITL task. When several sets are attendable the
	// choice is ambiguous: refuse to pick by priority and tell the human to target
	// one explicitly. Zero attendable sets stays plain no-runnable.
	attendable := attendableHITLTaskSets(refresh)
	switch len(attendable) {
	case 1:
		return attendable[0], true, nil
	case 0:
		return "", false, exitErr(ExitNoRunnable, "no runnable work")
	default:
		return "", false, exitErr(ExitNoRunnable,
			"no runnable AFK work and multiple Task sets are Human-blocked: %s; "+
				"run `pop tasks implement <task-set>` for the set you want to attend",
			strings.Join(attendable, ", "))
	}
}

// attendableHITLTaskSets returns the IDs of every Task set held at a HITL gate by
// an open HITL task, sorted for stable output.
func attendableHITLTaskSets(refresh *RefreshResult) []string {
	var ids []string
	for _, row := range refresh.Rows {
		if BlockingHITLTask(refresh.Manifests[row.ID]) != nil {
			ids = append(ids, row.ID)
		}
	}
	sort.Strings(ids)
	return ids
}

func selectExplicitTaskSet(refresh *RefreshResult, taskSetID string) (string, error) {
	row := findRow(refresh, taskSetID)
	if row == nil {
		return "", exitErr(ExitNoRunnable, "%s", unknownTaskSetTargetMessage(refresh, taskSetID))
	}

	m := refresh.Manifests[taskSetID]
	if m == nil {
		return "", exitErr(ExitNoRunnable, "Task set %q has no task manifest", taskSetID)
	}

	switch row.Status {
	case StatusFailed:
		// An explicitly targeted Failed set is allowed through so drain can
		// re-enter the Failed gate, mirroring the Human-blocked pass-through
		// below. The loop's StatusFailed branch runs the interactive recovery
		// prompt (re-run / finish by hand / exit) or the static advice fallback.
		return taskSetID, nil
	case StatusMalformed:
		return "", exitErr(ExitNoRunnable, "Task set %q is malformed", taskSetID)
	case StatusMissing:
		return "", exitErr(ExitNoRunnable, "Task set %q is missing", taskSetID)
	case StatusReady, StatusDeferred:
		return taskSetID, nil
	default:
		// An explicitly targeted set that is Human-blocked by an open HITL task
		// is allowed through so drain can enter the HITL gate. Sets blocked only
		// by unresolved AFK dependencies still stop as no-runnable.
		if BlockingHITLTask(m) != nil {
			return taskSetID, nil
		}
		return "", exitErr(ExitNoRunnable, "Task set %q is %s: %s", taskSetID, strings.ToLower(string(row.Status)), row.BlockedReason)
	}
}

// SelectTaskInSet chooses the first eligible AFK task in manifest-array order
// for one Task set. When the set already contains a failed task, selection
// routes to the Failed gate instead of advancing to an open sibling, so a
// set-wide failure is a hard stop until the human disposes of it.
func SelectTaskInSet(refresh *RefreshResult, taskSetID string) (*Selection, error) {
	m := refresh.Manifests[taskSetID]
	if m == nil {
		return nil, exitErr(ExitNoRunnable, "Task set %q has no task manifest", taskSetID)
	}
	if failed := FailedTask(m); failed != nil {
		return nil, exitErr(ExitNoRunnable, "task set %q has failed task %q", taskSetID, failed.ID)
	}
	return firstEligibleTask(taskSetID, m)
}

// FindRow returns the status row for a task set by ID, or nil when none matches.
func FindRow(refresh *RefreshResult, taskSetID string) *Row {
	return findRow(refresh, taskSetID)
}

func findRow(refresh *RefreshResult, taskSetID string) *Row {
	for i := range refresh.Rows {
		if refresh.Rows[i].ID == taskSetID {
			return &refresh.Rows[i]
		}
	}
	return nil
}

// SelectTask chooses the next task to run from a refreshed task.
func SelectTask(refresh *RefreshResult, taskSetOverride, taskOverride string) (*Selection, error) {
	if taskOverride != "" && taskSetOverride == "" {
		return nil, exitErr(ExitSetup, "explicit task requires a task set")
	}

	manifests := refresh.Manifests
	if manifests == nil {
		manifests = make(map[string]*Manifest)
	}

	if taskSetOverride != "" {
		return selectExplicit(refresh, manifests, taskSetOverride, taskOverride)
	}
	return selectAutomatic(refresh, manifests)
}

func selectAutomatic(refresh *RefreshResult, manifests map[string]*Manifest) (*Selection, error) {
	for _, row := range refresh.Rows {
		if row.Status != StatusReady {
			continue
		}
		m := manifests[row.ID]
		if m == nil || !m.Valid {
			continue
		}
		sel, err := firstEligibleTask(row.ID, m)
		if err != nil {
			continue
		}
		return sel, nil
	}
	return nil, exitErr(ExitNoRunnable, "no runnable work")
}

func selectExplicit(refresh *RefreshResult, manifests map[string]*Manifest, taskSetID, taskID string) (*Selection, error) {
	row := findRow(refresh, taskSetID)
	if row == nil {
		return nil, exitErr(ExitNoRunnable, "%s", unknownTaskSetTargetMessage(refresh, taskSetID))
	}

	m := manifests[taskSetID]
	if m == nil {
		return nil, exitErr(ExitNoRunnable, "Task set %q has no task manifest", taskSetID)
	}

	switch row.Status {
	case StatusFailed:
		return nil, exitErr(ExitNoRunnable, "Task set %q has failed tasks; reset required before execution", taskSetID)
	case StatusMalformed:
		return nil, exitErr(ExitNoRunnable, "Task set %q is malformed", taskSetID)
	case StatusMissing:
		return nil, exitErr(ExitNoRunnable, "Task set %q is missing", taskSetID)
	}

	if taskID == "" {
		if row.Status != StatusReady {
			return nil, exitErr(ExitNoRunnable, "Task set %q is %s: %s", taskSetID, strings.ToLower(string(row.Status)), row.BlockedReason)
		}
		return firstEligibleTask(taskSetID, m)
	}

	return selectExplicitTask(taskSetID, m, taskID)
}

func selectExplicitTask(taskSetID string, m *Manifest, taskID string) (*Selection, error) {
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
	switch task.Status {
	case "done":
		return nil, exitErr(ExitNoRunnable, "task %q is already done", taskID)
	case "failed":
		return nil, exitErr(ExitNoRunnable, "task %q failed; reset required", taskID)
	case "skipped":
		return nil, exitErr(ExitNoRunnable, "task %q is skipped", taskID)
	}
	if task.Type == "HITL" {
		// A HITL target is no longer rejected outright: a ready HITL task routes
		// to its own HITL gate (assist / complete / defer / shell / exit), giving
		// explicit disambiguation when a set holds several HITL gates that the
		// scheduler's auto-pick (the single blocking one) would never reach. A
		// target whose dependencies are still open is rejected with the same
		// dependency-specific message an AFK target gets — not the generic non-AFK
		// error — so the human sees *why* the gate is not yet attendable.
		for _, blocker := range task.BlockedBy {
			if !blockerSatisfied(m, blocker) {
				return nil, exitErr(ExitNoRunnable, "task %q blocked by %s", taskID, blocker)
			}
		}
		return &Selection{
			TaskSetID: taskSetID,
			TaskID:    taskID,
			TaskPath:  filepath.Join(m.Dir, task.File),
			TaskFile:  task.File,
			Manifest:  m,
			Task:      task,
			TaskIndex: idx,
			HITLGate:  true,
		}, nil
	}
	if task.Type != "AFK" {
		return nil, exitErr(ExitNoRunnable, "task %q is not AFK", taskID)
	}
	for _, blocker := range task.BlockedBy {
		if !blockerSatisfied(m, blocker) {
			return nil, exitErr(ExitNoRunnable, "task %q blocked by %s", taskID, blocker)
		}
	}

	return &Selection{
		TaskSetID: taskSetID,
		TaskID:    taskID,
		TaskPath:  filepath.Join(m.Dir, task.File),
		TaskFile:  task.File,
		Manifest:  m,
		Task:      task,
		TaskIndex: idx,
	}, nil
}

func firstEligibleTask(taskSetID string, m *Manifest) (*Selection, error) {
	for i, task := range m.Tasks {
		if !isEligible(m, task) {
			continue
		}
		return &Selection{
			TaskSetID: taskSetID,
			TaskID:    task.ID,
			TaskPath:  filepath.Join(m.Dir, task.File),
			TaskFile:  task.File,
			Manifest:  m,
			Task:      task,
			TaskIndex: i,
		}, nil
	}
	return nil, exitErr(ExitNoRunnable, "task set %q has no eligible AFK task", taskSetID)
}

func unknownTaskSetTargetMessage(refresh *RefreshResult, taskSetID string) string {
	var ids []string
	for _, row := range refresh.Rows {
		if row.Status != StatusMissing {
			ids = append(ids, row.ID)
		}
	}
	sort.Strings(ids)
	if len(ids) == 0 {
		return fmt.Sprintf("unknown task set %q", taskSetID)
	}
	return fmt.Sprintf("unknown task set %q; valid: %s", taskSetID, strings.Join(ids, ", "))
}

func unknownTaskMessage(m *Manifest, taskID string) string {
	var ids []string
	for _, task := range m.Tasks {
		ids = append(ids, task.ID)
	}
	sort.Strings(ids)
	return fmt.Sprintf("unknown task %q; valid: %s", taskID, strings.Join(ids, ", "))
}

// MarkNextPick marks the highest-priority runnable Task-set row with NEXT — the
// set a no-argument local `pop tasks implement` would drain next. Non-runnable
// higher-priority rows are skipped. Display-only; unrelated to daemon consent.
func MarkNextPick(rows []Row) {
	for i := range rows {
		if rows[i].Status != StatusReady {
			continue
		}
		rows[i].NextPick = true
		rows[i].PriorityShow = fmt.Sprintf("%d NEXT", rows[i].Priority)
		return
	}
}

// MarkRunTarget marks the selected Task-set row with RUN. A running set reads
// RUN, never NEXT RUN: once it is actually running the run-next badge no longer
// applies.
func MarkRunTarget(rows []Row, taskSetID string) {
	for i := range rows {
		if rows[i].ID != taskSetID {
			continue
		}
		rows[i].RunTarget = true
		rows[i].PriorityShow = fmt.Sprintf("%d RUN", rows[i].Priority)
		return
	}
}
