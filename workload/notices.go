package workload

import (
	"io"
	"strings"
)

// RenderMigrate writes the user-facing summary of a Workload migration.
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
func RenderPriorityUpdate(w io.Writer, issueSetID string, oldPriority, newPriority int) {
	outputFor(w).line(ansiCyan, "Updated priority for %s: %d -> %d", issueSetID, oldPriority, newPriority)
}

// RenderIssueReset writes the user-facing open result.
func RenderIssueReset(w io.Writer, issueSetID, issueID string) {
	outputFor(w).line(ansiCyan, "Reset task %s/%s to open", issueSetID, issueID)
}

// RenderIssueComplete writes the user-facing complete result.
func RenderIssueComplete(w io.Writer, issueSetID, issueID string) {
	outputFor(w).line(ansiGreen, "✓ Completed task %s/%s", issueSetID, issueID)
}

// RenderIssueSkip writes the user-facing skip result.
func RenderIssueSkip(w io.Writer, issueSetID, issueID string) {
	outputFor(w).line(ansiYellow, "Skipped task %s/%s", issueSetID, issueID)
}
