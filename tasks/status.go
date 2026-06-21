package tasks

import (
	"fmt"
	"path"
	"strings"
)

// TaskSetStatus is a derived task status for one Task-set row.
type TaskSetStatus string

const (
	StatusMissing    TaskSetStatus = "MISSING"
	StatusDone       TaskSetStatus = "DONE"
	StatusMalformed  TaskSetStatus = "MALFORMED"
	StatusFailed     TaskSetStatus = "FAILED"
	StatusReady      TaskSetStatus = "READY"
	StatusDeferred   TaskSetStatus = "DEFERRED"
	StatusBlocked    TaskSetStatus = "BLOCKED"
	StatusUnverified TaskSetStatus = "UNVERIFIED"
)

// Row is one line in the task status table.
type Row struct {
	ID               string
	Status           TaskSetStatus
	Priority         int
	PriorityShow     string
	AutoDrain        bool
	Progress         string
	BlockedReason    string
	FailedTasks      []string
	ResetHints       []string
	CompleteHint     string
	MalformedSummary string
	DetailErrors     []string
	RegIndex         int
	AutoPick         bool
	RunTarget        bool
}

// DeriveStatus computes Task-set status from manifest validation.
func DeriveStatus(m *Manifest) TaskSetStatus {
	if m == nil {
		return StatusMalformed
	}
	if !m.Valid {
		return StatusMalformed
	}
	if allDone(m) {
		return StatusDone
	}
	if anyFailed(m) {
		return StatusFailed
	}
	if hasEligibleTask(m) {
		return StatusReady
	}
	if isDeferred(m) {
		return StatusDeferred
	}
	// Terminal HITL: all AFK work is done/skipped, only the human-gate remains.
	// Gating HITL: at least one open AFK task is still waiting behind the gate.
	if hasOpenAFKTask(m) {
		return StatusBlocked
	}
	return StatusUnverified
}

// isDeferred reports whether every task is Done or Skipped with at least one
// Skipped. A still-Open task (AFK or HITL) leaves the set Ready or Blocked,
// never Deferred (see ADR 0006).
func isDeferred(m *Manifest) bool {
	if len(m.Tasks) == 0 {
		return false
	}
	anySkipped := false
	for _, task := range m.Tasks {
		switch task.Status {
		case "skipped":
			anySkipped = true
		case "done":
		default:
			return false
		}
	}
	return anySkipped
}

// SkippedTaskIDs returns the IDs of skipped tasks in manifest-array order.
func SkippedTaskIDs(m *Manifest) []string {
	if m == nil {
		return nil
	}
	var ids []string
	for _, task := range m.Tasks {
		if task.Status == "skipped" {
			ids = append(ids, task.ID)
		}
	}
	return ids
}

func allDone(m *Manifest) bool {
	if len(m.Tasks) == 0 {
		return false
	}
	for _, task := range m.Tasks {
		if task.Status != "done" {
			return false
		}
	}
	return true
}

func anyFailed(m *Manifest) bool {
	for _, task := range m.Tasks {
		if task.Status == "failed" {
			return true
		}
	}
	return false
}

func taskDone(m *Manifest, id string) bool {
	for _, task := range m.Tasks {
		if task.ID == id {
			return task.Status == "done"
		}
	}
	return false
}

// blockerSatisfied reports whether a blocked_by prerequisite is satisfied.
// A Skipped task is deliberately set aside, not completed, yet it satisfies
// blocked_by so its dependents can proceed (see ADR 0006).
func blockerSatisfied(m *Manifest, id string) bool {
	for _, task := range m.Tasks {
		if task.ID == id {
			return task.Status == "done" || task.Status == "skipped"
		}
	}
	return false
}

func hasEligibleTask(m *Manifest) bool {
	for _, task := range m.Tasks {
		if isEligible(m, task) {
			return true
		}
	}
	return false
}

func isEligible(m *Manifest, task Task) bool {
	if task.Status != "open" || task.Type != "AFK" {
		return false
	}
	for _, blocker := range task.BlockedBy {
		if !blockerSatisfied(m, blocker) {
			return false
		}
	}
	return true
}

// hasOpenAFKTask reports whether any AFK task is still open (eligible or not).
func hasOpenAFKTask(m *Manifest) bool {
	for _, task := range m.Tasks {
		if task.Status == "open" && task.Type == "AFK" {
			return true
		}
	}
	return false
}

// BuildProgress returns compact progress text for a row.
func BuildProgress(m *Manifest, status TaskSetStatus) string {
	if status == StatusMissing {
		return ""
	}

	if m == nil {
		return ""
	}

	done, open, failed, hitl, skipped := 0, 0, 0, 0, 0
	for _, task := range m.Tasks {
		switch task.Status {
		case "done":
			done++
		case "failed":
			failed++
		case "skipped":
			skipped++
		case "open":
			if task.Type == "HITL" {
				hitl++
			} else {
				open++
			}
		}
	}
	total := len(m.Tasks)
	parts := []string{fmt.Sprintf("%d/%d done", done, total)}
	if open > 0 {
		parts = append(parts, fmt.Sprintf("%d open", open))
	}
	if failed > 0 {
		parts = append(parts, fmt.Sprintf("%d failed", failed))
	}
	if hitl > 0 {
		parts = append(parts, fmt.Sprintf("%d HITL", hitl))
	}
	if skipped > 0 {
		parts = append(parts, fmt.Sprintf("%d skipped", skipped))
	}
	return strings.Join(parts, ", ")
}

// BuildBlockedReason explains why an Task set is blocked.
func BuildBlockedReason(m *Manifest) string {
	if m == nil || !m.Valid {
		return ""
	}

	for _, task := range m.Tasks {
		if task.Status != "open" {
			continue
		}
		if task.Type == "HITL" && blockersResolved(m, task) {
			return fmt.Sprintf("HITL: %s", task.ID)
		}
	}

	for _, task := range m.Tasks {
		if task.Status != "open" || task.Type != "AFK" {
			continue
		}
		for _, blocker := range task.BlockedBy {
			if !blockerSatisfied(m, blocker) {
				return fmt.Sprintf("blocked by %s", blocker)
			}
		}
	}
	return "no eligible AFK task"
}

// BlockingHITLTask returns the open HITL task whose blockers are all resolved
// — the task that holds a Human-blocked Task set at a human-in-the-loop gate.
// Returns nil when no such task exists.
func BlockingHITLTask(m *Manifest) *Task {
	if m == nil || !m.Valid {
		return nil
	}
	for i := range m.Tasks {
		task := m.Tasks[i]
		if task.Status == "open" && task.Type == "HITL" && blockersResolved(m, task) {
			return &m.Tasks[i]
		}
	}
	return nil
}

// FailedTask returns the first failed task in a manifest, or nil when none has
// failed. A set goes Failed on its first failure and selection halts before
// another task can fail, so this is the single task the Failed gate targets.
func FailedTask(m *Manifest) *Task {
	if m == nil {
		return nil
	}
	for i := range m.Tasks {
		if m.Tasks[i].Status == "failed" {
			return &m.Tasks[i]
		}
	}
	return nil
}

func blockersResolved(m *Manifest, task Task) bool {
	for _, blocker := range task.BlockedBy {
		if !blockerSatisfied(m, blocker) {
			return false
		}
	}
	return true
}

// taskPathHint returns the canonical copy-paste Task target reference for an
// task file: the <task-set>/<file>.md form (see ADR 0012).
func taskPathHint(stem, file string) string {
	return path.Join(stem, file)
}

// resetTaskHint returns the copy-paste open command for a task file.
func resetTaskHint(stem, file string) string {
	return fmt.Sprintf("pop tasks open %s", taskPathHint(stem, file))
}

// completeTaskHint returns the copy-paste complete command for a task file.
func completeTaskHint(stem, file string) string {
	return fmt.Sprintf("pop tasks complete %s", taskPathHint(stem, file))
}

// skipTaskHint returns the copy-paste skip command for a task file.
func skipTaskHint(stem, file string) string {
	return fmt.Sprintf("pop tasks skip %s", taskPathHint(stem, file))
}

// BuildFailedInfo returns failed task IDs and reset command hints.
func BuildFailedInfo(stem string, m *Manifest) (ids []string, hints []string) {
	if m == nil {
		return nil, nil
	}
	for _, task := range m.Tasks {
		if task.Status == "failed" {
			ids = append(ids, task.ID)
			hints = append(hints, resetTaskHint(stem, task.File))
		}
	}
	return ids, hints
}

// MalformedSummary returns a compact malformed summary for table display.
func MalformedSummary(m *Manifest) string {
	if m == nil {
		return "malformed"
	}
	if len(m.Errors) == 1 {
		return m.Errors[0]
	}
	if len(m.Errors) > 1 {
		return fmt.Sprintf("%d validation errors", len(m.Errors))
	}
	return "malformed"
}
