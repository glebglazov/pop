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
	MalformedSummary string
	DetailErrors     []string
	RegIndex         int
	AutoPick         bool
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
	return StatusBlocked
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
		if !issueDone(m, blocker) {
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

	done, open, failed, hitl := 0, 0, 0, 0
	for _, issue := range m.Issues {
		switch issue.Status {
		case "done":
			done++
		case "failed":
			failed++
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
			if !issueDone(m, blocker) {
				return fmt.Sprintf("blocked by %s", blocker)
			}
		}
	}
	return "no eligible AFK issue"
}

func blockersResolved(m *Manifest, issue Issue) bool {
	for _, blocker := range issue.BlockedBy {
		if !issueDone(m, blocker) {
			return false
		}
	}
	return true
}

// BuildFailedInfo returns failed issue IDs and reset command hints.
func BuildFailedInfo(stem string, m *Manifest) (ids []string, hints []string) {
	if m == nil {
		return nil, nil
	}
	for _, issue := range m.Issues {
		if issue.Status == "failed" {
			ids = append(ids, issue.ID)
			hints = append(hints, fmt.Sprintf("pop workload reset-issue %s", path.Join("thoughts/issues", stem, issue.File)))
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
