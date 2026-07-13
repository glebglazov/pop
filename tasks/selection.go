package tasks

// SelectionRow is one task offered in a whole-set Multi-task selection. A
// verb's eligibility predicate decides whether the row is checkable or Locked
// (a status indicator, not a removable selection) and which glyph a locked row
// shows in place of a checkbox.
type SelectionRow struct {
	TaskID     string
	File       string
	Title      string
	Status     TaskStatus
	Locked     bool
	LockedMark string
}

// afkOrdinal returns the 1-based position of taskID among AFK-typed tasks in
// manifest order and the total count of AFK tasks in the set. The position is a
// fixed property of the task (all statuses counted), so it is stable across
// skips, retries, and re-drains. Returns (0, 0) when the manifest is nil or
// taskID is not an AFK task in it, and the caller then omits the counter.
func afkOrdinal(m *Manifest, taskID string) (pos, total int) {
	if m == nil {
		return 0, 0
	}
	for _, task := range m.Tasks {
		if task.Type != "AFK" {
			continue
		}
		total++
		if task.ID == taskID {
			pos = total
		}
	}
	return pos, total
}

// EligibilityFunc classifies a task status for one verb's Multi-task selection,
// returning whether the row is locked and the glyph to render for a locked row.
// This is the per-verb policy that drives the three-way row split (checkable /
// locked-at-target / inert) on top of the shared multi-select component.
type EligibilityFunc func(status TaskStatus) (locked bool, lockedMark string)

// BuildSelection lists every task in manifest order, classifying each row with
// the verb's eligibility predicate. Returns nil for a nil manifest.
func BuildSelection(m *Manifest, eligible EligibilityFunc) []SelectionRow {
	if m == nil {
		return nil
	}
	rows := make([]SelectionRow, 0, len(m.Tasks))
	for _, task := range m.Tasks {
		locked, mark := eligible(task.Status)
		rows = append(rows, SelectionRow{
			TaskID:     task.ID,
			File:       task.File,
			Title:      task.Title,
			Status:     task.Status,
			Locked:     locked,
			LockedMark: mark,
		})
	}
	return rows
}

// completeEligibility is the per-verb predicate for `complete`: Done tasks are
// locked (already at the target), every other status is checkable.
func completeEligibility(status TaskStatus) (bool, string) {
	if status == TaskDone {
		return true, "✓"
	}
	return false, ""
}

// skipEligibility is the per-verb predicate for `skip`: an Open task is
// checkable, an already-Skipped task is locked at-target (✓ — already in the
// target state), and Done/Failed tasks are inert locked context that cannot be
// skipped (·).
func skipEligibility(status TaskStatus) (bool, string) {
	switch status {
	case TaskOpen:
		return false, ""
	case TaskSkipped:
		return true, "✓"
	default:
		return true, "·"
	}
}

// CanReopen reports whether `open` can return a task in this status to open.
// Every non-open status — failed, skipped, done — reopens: failed/skipped are
// retries, done is undoing a completion (ADR-0053). Only an already-open task
// cannot. This is the single source of truth shared by the single-task, batch,
// picker, and queue-dashboard open paths.
func CanReopen(status TaskStatus) bool {
	return status != TaskOpen
}

// openEligibility is the per-verb predicate for `open`: every CanReopen status
// is checkable (no row pre-checked; the human selects each), and an already-Open
// task is locked at-target (✓ — already in the target state).
func openEligibility(status TaskStatus) (bool, string) {
	if !CanReopen(status) {
		return true, "✓"
	}
	return false, ""
}
