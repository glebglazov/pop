package workload

import (
	"fmt"
	"regexp"
	"strings"
)

var (
	summaryStartRE = regexp.MustCompile(`(?m)^SUMMARY_START\s*$`)
	summaryEndRE   = regexp.MustCompile(`(?m)^SUMMARY_END\s*$`)
)

// Assessment holds the outcome of verifying agent output and issue markdown.
type Assessment struct {
	Summary      string
	Complete     bool
	FailedReason string
	AllChecked   bool
}

// AssessCompletion parses captured agent output and verifies acceptance checkboxes.
func AssessCompletion(output string, issueMarkdown []byte) Assessment {
	a := Assessment{}
	trimmed := strings.TrimRight(output, " \t\r\n")
	if trimmed == "" {
		a.FailedReason = "empty agent output"
		return a
	}

	lines := splitNonEmptyLines(trimmed)
	lastLine := lines[len(lines)-1]

	if strings.HasPrefix(lastLine, "TASK_FAILED:") {
		a.FailedReason = strings.TrimSpace(strings.TrimPrefix(lastLine, "TASK_FAILED:"))
		if a.FailedReason == "" {
			a.FailedReason = "agent reported failure"
		}
		return a
	}

	if lastLine != "TASK_COMPLETE" {
		a.FailedReason = "missing TASK_COMPLETE sentinel"
		return a
	}

	summary, ok := extractSummary(trimmed)
	if !ok || strings.TrimSpace(summary) == "" {
		a.FailedReason = "missing or empty summary block"
		return a
	}
	a.Summary = strings.TrimSpace(summary)
	a.Complete = true
	a.AllChecked = allAcceptanceChecked(issueMarkdown)
	if !a.AllChecked {
		a.Complete = false
		a.FailedReason = "acceptance criteria not all checked"
	}
	return a
}

func splitNonEmptyLines(s string) []string {
	raw := strings.Split(s, "\n")
	var lines []string
	for _, line := range raw {
		if strings.TrimSpace(line) != "" {
			lines = append(lines, strings.TrimSpace(line))
		}
	}
	return lines
}

func extractSummary(output string) (string, bool) {
	start := summaryStartRE.FindStringIndex(output)
	end := summaryEndRE.FindStringIndex(output)
	if start == nil || end == nil || end[0] <= start[1] {
		return "", false
	}
	body := output[start[1]:end[0]]
	return strings.TrimSpace(body), true
}

func allAcceptanceChecked(data []byte) bool {
	lines := strings.Split(string(data), "\n")
	inSection := false
	foundCheckbox := false

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if acHeaderPattern.MatchString(trimmed) {
			inSection = true
			continue
		}
		if inSection && strings.HasPrefix(trimmed, "## ") {
			break
		}
		if inSection && checkboxPattern.MatchString(trimmed) {
			foundCheckbox = true
			if !strings.Contains(trimmed, "[x]") && !strings.Contains(trimmed, "[X]") {
				return false
			}
		}
	}
	return foundCheckbox
}

// CommitSubject returns the implementation commit subject for an issue.
func CommitSubject(prdID, issueID string) string {
	return fmt.Sprintf("workload(%s): %s", prdID, issueID)
}

// DirtyCheckpointSubject returns the checkpoint commit subject for dirty runtime state.
func DirtyCheckpointSubject(prdID, issueID string) string {
	return fmt.Sprintf("workload(%s): %s capturing dirty state", prdID, issueID)
}
