package workload

import "io"

// RenderPriorityUpdate writes the user-facing set-priority result.
func RenderPriorityUpdate(w io.Writer, issueSetID string, oldPriority, newPriority int) {
	outputFor(w).line(ansiCyan, "Updated priority for %s: %d -> %d", issueSetID, oldPriority, newPriority)
}

// RenderIssueReset writes the user-facing reset-issue result.
func RenderIssueReset(w io.Writer, issueSetID, issueID string) {
	outputFor(w).line(ansiCyan, "Reset issue %s/%s to open", issueSetID, issueID)
}

// RenderIssueComplete writes the user-facing complete-issue result.
func RenderIssueComplete(w io.Writer, issueSetID, issueID string) {
	outputFor(w).line(ansiGreen, "✓ Completed issue %s/%s", issueSetID, issueID)
}

// RenderIssueSkip writes the user-facing skip-issue result.
func RenderIssueSkip(w io.Writer, issueSetID, issueID string) {
	outputFor(w).line(ansiYellow, "Skipped issue %s/%s", issueSetID, issueID)
}
