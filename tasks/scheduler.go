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
	// is Human-blocked by an open HITL task; ambiguity stays no-runnable.
	if id, ok := singleAttendableHITLTaskSet(refresh); ok {
		return id, true, nil
	}
	return "", false, exitErr(ExitNoRunnable, "no runnable work")
}

// singleAttendableHITLTaskSet returns the only Task set held at a HITL gate by an
// open HITL task, reporting false when zero or more than one such set exists.
func singleAttendableHITLTaskSet(refresh *RefreshResult) (string, bool) {
	found := ""
	count := 0
	for _, row := range refresh.Rows {
		if BlockingHITLTask(refresh.Manifests[row.ID]) != nil {
			found = row.ID
			count++
		}
	}
	if count == 1 {
		return found, true
	}
	return "", false
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
		return "", exitErr(ExitNoRunnable, "Task set %q has failed tasks; reset required before execution", taskSetID)
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
// for one Task set.
func SelectTaskInSet(refresh *RefreshResult, taskSetID string) (*Selection, error) {
	m := refresh.Manifests[taskSetID]
	if m == nil {
		return nil, exitErr(ExitNoRunnable, "Task set %q has no task manifest", taskSetID)
	}
	return firstEligibleTask(taskSetID, m)
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
		return nil, exitErr(ExitNoRunnable, "task %q is HITL", taskID)
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

// MarkAutoPick marks the highest-priority runnable Task-set row with AUTO.
// Non-runnable higher-priority rows are skipped.
func MarkAutoPick(rows []Row) {
	for i := range rows {
		if rows[i].Status != StatusReady {
			continue
		}
		rows[i].AutoPick = true
		rows[i].PriorityShow = fmt.Sprintf("%d AUTO", rows[i].Priority)
		return
	}
}

// MarkRunTarget marks the selected Task-set row with RUN, combining with AUTO when applicable.
func MarkRunTarget(rows []Row, taskSetID string) {
	for i := range rows {
		if rows[i].ID != taskSetID {
			continue
		}
		rows[i].RunTarget = true
		if rows[i].AutoPick {
			rows[i].PriorityShow = fmt.Sprintf("%d AUTO RUN", rows[i].Priority)
		} else {
			rows[i].PriorityShow = fmt.Sprintf("%d RUN", rows[i].Priority)
		}
		return
	}
}
