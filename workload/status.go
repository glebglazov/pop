package workload

import (
	"fmt"
	"path"
	"strings"
)

// IssueSetStatus is a derived workload status for one Issue-set row.
type IssueSetStatus string

const (
	StatusMissing   IssueSetStatus = "MISSING"
	StatusDone      IssueSetStatus = "DONE"
	StatusMalformed IssueSetStatus = "MALFORMED"
	StatusFailed    IssueSetStatus = "FAILED"
	StatusReady     IssueSetStatus = "READY"
	StatusDeferred  IssueSetStatus = "DEFERRED"
	StatusBlocked   IssueSetStatus = "BLOCKED"
)

// Row is one line in the workload status table.
type Row struct {
	ID               string
	Status           IssueSetStatus
	Priority         int
	PriorityShow     string
	Progress         string
	BlockedReason    string
	FailedIssues     []string
	ResetHints       []string
	CompleteHint     string
	MalformedSummary string
	DetailErrors     []string
	RegIndex         int
	AutoPick         bool
	RunTarget        bool
}

// DeriveStatus computes Issue-set status from manifest validation.
func DeriveStatus(m *Manifest) IssueSetStatus {
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
	if hasEligibleIssue(m) {
		return StatusReady
	}
	if isDeferred(m) {
		return StatusDeferred
	}
	return StatusBlocked
}

// isDeferred reports whether every issue is Done or Skipped with at least one
// Skipped. A still-Open issue (AFK or HITL) leaves the set Ready or Blocked,
// never Deferred (see ADR 0006).
func isDeferred(m *Manifest) bool {
	if len(m.Issues) == 0 {
		return false
	}
	anySkipped := false
	for _, issue := range m.Issues {
		switch issue.Status {
		case "skipped":
			anySkipped = true
		case "done":
		default:
			return false
		}
	}
	return anySkipped
}

// SkippedIssueIDs returns the IDs of skipped issues in manifest-array order.
func SkippedIssueIDs(m *Manifest) []string {
	if m == nil {
		return nil
	}
	var ids []string
	for _, issue := range m.Issues {
		if issue.Status == "skipped" {
			ids = append(ids, issue.ID)
		}
	}
	return ids
}

func allDone(m *Manifest) bool {
	if len(m.Issues) == 0 {
		return false
	}
	for _, issue := range m.Issues {
		if issue.Status != "done" {
			return false
		}
	}
	return true
}

func anyFailed(m *Manifest) bool {
	for _, issue := range m.Issues {
		if issue.Status == "failed" {
			return true
		}
	}
	return false
}

func issueDone(m *Manifest, id string) bool {
	for _, issue := range m.Issues {
		if issue.ID == id {
			return issue.Status == "done"
		}
	}
	return false
}

// blockerSatisfied reports whether a blocked_by prerequisite is satisfied.
// A Skipped issue is deliberately set aside, not completed, yet it satisfies
// blocked_by so its dependents can proceed (see ADR 0006).
func blockerSatisfied(m *Manifest, id string) bool {
	for _, issue := range m.Issues {
		if issue.ID == id {
			return issue.Status == "done" || issue.Status == "skipped"
		}
	}
	return false
}

func hasEligibleIssue(m *Manifest) bool {
	for _, issue := range m.Issues {
		if isEligible(m, issue) {
			return true
		}
	}
	return false
}

func isEligible(m *Manifest, issue Issue) bool {
	if issue.Status != "open" || issue.Type != "AFK" {
		return false
	}
	for _, blocker := range issue.BlockedBy {
		if !blockerSatisfied(m, blocker) {
			return false
		}
	}
	return true
}

// BuildProgress returns compact progress text for a row.
func BuildProgress(m *Manifest, status IssueSetStatus) string {
	if status == StatusMissing {
		return ""
	}

	if m == nil {
		return ""
	}

	done, open, failed, hitl, skipped := 0, 0, 0, 0, 0
	for _, issue := range m.Issues {
		switch issue.Status {
		case "done":
			done++
		case "failed":
			failed++
		case "skipped":
			skipped++
		case "open":
			if issue.Type == "HITL" {
				hitl++
			} else {
				open++
			}
		}
	}
	total := len(m.Issues)
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

// BuildBlockedReason explains why an Issue set is blocked.
func BuildBlockedReason(m *Manifest) string {
	if m == nil || !m.Valid {
		return ""
	}

	for _, issue := range m.Issues {
		if issue.Status != "open" {
			continue
		}
		if issue.Type == "HITL" && blockersResolved(m, issue) {
			return fmt.Sprintf("HITL: %s", issue.ID)
		}
	}

	for _, issue := range m.Issues {
		if issue.Status != "open" || issue.Type != "AFK" {
			continue
		}
		for _, blocker := range issue.BlockedBy {
			if !blockerSatisfied(m, blocker) {
				return fmt.Sprintf("blocked by %s", blocker)
			}
		}
	}
	return "no eligible AFK issue"
}

// BlockingHITLIssue returns the open HITL issue whose blockers are all resolved
// — the issue that holds a Human-blocked Issue set at a human-in-the-loop gate.
// Returns nil when no such issue exists.
func BlockingHITLIssue(m *Manifest) *Issue {
	if m == nil || !m.Valid {
		return nil
	}
	for i := range m.Issues {
		issue := m.Issues[i]
		if issue.Status == "open" && issue.Type == "HITL" && blockersResolved(m, issue) {
			return &m.Issues[i]
		}
	}
	return nil
}

func blockersResolved(m *Manifest, issue Issue) bool {
	for _, blocker := range issue.BlockedBy {
		if !blockerSatisfied(m, blocker) {
			return false
		}
	}
	return true
}

// issuePathHint returns the canonical copy-paste Workload target reference for an
// issue file: the <issue-set>/<file>.md form (see ADR 0012).
func issuePathHint(stem, file string) string {
	return path.Join(stem, file)
}

// resetIssueHint returns the copy-paste reset-issue command for an issue file.
func resetIssueHint(stem, file string) string {
	return fmt.Sprintf("pop workload reset-issue %s", issuePathHint(stem, file))
}

// completeIssueHint returns the copy-paste complete-issue command for an issue file.
func completeIssueHint(stem, file string) string {
	return fmt.Sprintf("pop workload complete-issue %s", issuePathHint(stem, file))
}

// skipIssueHint returns the copy-paste skip-issue command for an issue file.
func skipIssueHint(stem, file string) string {
	return fmt.Sprintf("pop workload skip-issue %s", issuePathHint(stem, file))
}

// BuildFailedInfo returns failed issue IDs and reset command hints.
func BuildFailedInfo(stem string, m *Manifest) (ids []string, hints []string) {
	if m == nil {
		return nil, nil
	}
	for _, issue := range m.Issues {
		if issue.Status == "failed" {
			ids = append(ids, issue.ID)
			hints = append(hints, resetIssueHint(stem, issue.File))
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
