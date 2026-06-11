package tasks

// SelectionRow is one task offered in a whole-set Multi-task selection. A
// verb's eligibility predicate decides whether the row is checkable or Locked
// (a status indicator, not a removable selection) and which glyph a locked row
// shows in place of a checkbox.
type SelectionRow struct {
	TaskID     string
	File       string
	Title      string
	Status     string
	Locked     bool
	LockedMark string
}

// EligibilityFunc classifies a task status for one verb's Multi-task selection,
// returning whether the row is locked and the glyph to render for a locked row.
// This is the per-verb policy that drives the three-way row split (checkable /
// locked-at-target / inert) on top of the shared multi-select component.
type EligibilityFunc func(status string) (locked bool, lockedMark string)

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
func completeEligibility(status string) (bool, string) {
	if status == "done" {
		return true, "✓"
	}
	return false, ""
}

// openEligibility is the per-verb predicate for `open`: Failed and Skipped
// tasks are checkable, an already-Open task is locked at-target (✓ — already in
// the target state), and a Done task is inert locked context that cannot be
// reopened (·).
func openEligibility(status string) (bool, string) {
	switch status {
	case "failed", "skipped":
		return false, ""
	case "open":
		return true, "✓"
	default:
		return true, "·"
	}
}
