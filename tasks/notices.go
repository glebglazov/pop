package tasks

import (
	"io"
	"strings"
)

// RenderMigrate writes the user-facing summary of a Task migration.
func RenderMigrate(w io.Writer, result *MigrateResult) {
	out := outputFor(w)

	if len(result.Migrated) == 0 && len(result.Skipped) == 0 {
		out.line(ansiDim, "Nothing to migrate; no legacy thoughts/issues sets found")
		return
	}

	for _, setID := range result.Migrated {
		out.line(ansiGreen, "✓ Migrated %s into task storage", setID)
	}
	if len(result.Skipped) > 0 {
		out.line(ansiYellow, "Skipped %d set(s) already present in storage: %s",
			len(result.Skipped), strings.Join(result.Skipped, ", "))
	}

	out.line(ansiCyan, "Storage: %s", result.StorageDir)
	if result.ThoughtsRemoved {
		out.line(ansiDim, "Removed empty thoughts/ directory")
	} else if len(result.Skipped) > 0 {
		out.line(ansiDim, "Left thoughts/ in place (still contains skipped sets)")
	}
}

// RenderPriorityUpdate writes the user-facing set-priority result.
func RenderPriorityUpdate(w io.Writer, taskSetID string, oldPriority, newPriority int) {
	outputFor(w).line(ansiCyan, "Updated priority for %s: %d -> %d", taskSetID, oldPriority, newPriority)
}

// RenderAutoDrainUpdate writes the user-facing set-value auto-drain result.
func RenderAutoDrainUpdate(w io.Writer, taskSetID string, enabled bool) {
	if enabled {
		outputFor(w).line(ansiCyan, "Auto-drain enabled: %s", taskSetID)
		return
	}
	outputFor(w).line(ansiCyan, "Auto-drain disabled: %s", taskSetID)
}

// RenderTaskReset writes the user-facing open result.
func RenderTaskReset(w io.Writer, taskSetID, taskID string) {
	outputFor(w).line(ansiCyan, "Reset task %s/%s to open", taskSetID, taskID)
}

// RenderTaskComplete writes the user-facing complete result.
func RenderTaskComplete(w io.Writer, taskSetID, taskID string) {
	outputFor(w).line(ansiGreen, "✓ Completed task %s/%s", taskSetID, taskID)
}

// RenderTaskSkip writes the user-facing skip result.
func RenderTaskSkip(w io.Writer, taskSetID, taskID string) {
	outputFor(w).line(ansiYellow, "Skipped task %s/%s", taskSetID, taskID)
}

// RenderTaskCompleteBatch writes one line per batch transition in the form
// <set>/<file>: <prior>→done.
func RenderTaskCompleteBatch(w io.Writer, taskSetID string, transitions []CompleteTransition) {
	out := outputFor(w)
	for _, t := range transitions {
		out.line(ansiGreen, "✓ %s/%s: %s→done", taskSetID, t.File, t.Prior)
	}
}

// RenderTaskOpenBatch writes one line per batch transition in the form
// <set>/<file>: <prior>→open.
func RenderTaskOpenBatch(w io.Writer, taskSetID string, transitions []OpenTransition) {
	out := outputFor(w)
	for _, t := range transitions {
		out.line(ansiCyan, "%s/%s: %s→open", taskSetID, t.File, t.Prior)
	}
}

// RenderTaskSkipBatch writes one line per batch transition in the form
// <set>/<file>: <prior>→skipped.
func RenderTaskSkipBatch(w io.Writer, taskSetID string, transitions []SkipTransition) {
	out := outputFor(w)
	for _, t := range transitions {
		out.line(ansiYellow, "%s/%s: %s→skipped", taskSetID, t.File, t.Prior)
	}
}
