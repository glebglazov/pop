package tasks

import (
	"fmt"
	"regexp"
	"strings"
)

var (
	summaryStartRE = regexp.MustCompile(`(?m)^SUMMARY_START\s*$`)
	summaryEndRE   = regexp.MustCompile(`(?m)^SUMMARY_END\s*$`)
)

// The completion sentinels open the line a run closes out on. Requiring the
// line to *open* on the sentinel rejects one an agent buried mid-sentence
// while narrating the contract ("…then print TASK_COMPLETE if the suite is
// green"), which is the shape that would otherwise pass off unfinished work as
// done. That anchor is the guard; it is what makes a narrated mention
// unreadable as a close-out.
//
// What the reading deliberately tolerates is the model's own sign-off after
// the sentinel — several models close out "TASK_COMPLETEThe work landed: …"
// from a single message, and that sign-off runs to several lines as often as
// one. So the close-out is the *last* sentinel-opening line rather than the
// last line of the transcript: a sign-off that wraps into a second paragraph
// no longer buries a sentinel the agent did emit. Failure has always been read
// this way; success now matches.
const (
	completeSentinel = "TASK_COMPLETE"
	failedSentinel   = "TASK_FAILED:"
)

// The completion-contract failure reasons the harness itself records, as
// opposed to the agent's own TASK_FAILED text. The retry digest reads these
// back to pick a lesson (digest.go), so they are named rather than spelled
// twice.
const (
	reasonEmptyOutput     = "empty agent output"
	reasonMissingSentinel = "missing TASK_COMPLETE sentinel"
	reasonMissingSummary  = "missing or empty summary block"
	reasonUncheckedBoxes  = "acceptance criteria not all checked"
	reasonContractUnmet   = "agent output did not satisfy completion contract"
)

// Assessment holds the outcome of verifying agent output and task markdown.
type Assessment struct {
	Summary      string
	Complete     bool
	FailedReason string
	AllChecked   bool
}

// AssessCompletion parses captured agent output and verifies acceptance checkboxes.
func AssessCompletion(output string, taskMarkdown []byte) Assessment {
	a := Assessment{}
	trimmed := strings.TrimRight(output, " \t\r\n")
	if trimmed == "" {
		a.FailedReason = reasonEmptyOutput
		return a
	}

	closeOut, ok := closeOutLine(splitNonEmptyLines(trimmed))
	if !ok {
		a.FailedReason = reasonMissingSentinel
		return a
	}

	if strings.HasPrefix(closeOut, failedSentinel) {
		a.FailedReason = strings.TrimSpace(strings.TrimPrefix(closeOut, failedSentinel))
		if a.FailedReason == "" {
			a.FailedReason = "agent reported failure"
		}
		return a
	}

	summary, ok := extractSummary(trimmed)
	if !ok || strings.TrimSpace(summary) == "" {
		a.FailedReason = reasonMissingSummary
		return a
	}
	a.Summary = strings.TrimSpace(summary)
	a.Complete = true
	a.AllChecked = allAcceptanceChecked(taskMarkdown)
	if !a.AllChecked {
		a.Complete = false
		a.FailedReason = reasonUncheckedBoxes
	}
	return a
}

// closeOutLine picks the line the run closes out on: the last one opening on
// either sentinel. Reading from the end is what lets an agent's own sign-off
// follow the sentinel across a paragraph break — the two sentinels are read
// together so a TASK_FAILED written under an earlier TASK_COMPLETE is still
// the ending that counts.
func closeOutLine(lines []string) (string, bool) {
	for i := len(lines) - 1; i >= 0; i-- {
		if strings.HasPrefix(lines[i], completeSentinel) || strings.HasPrefix(lines[i], failedSentinel) {
			return lines[i], true
		}
	}
	return "", false
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

// timestampPrefixPattern matches the chronological prefix of a Task set
// identifier (YYYY-MM-DD or YYYY-MM-DD-HHMM followed by a hyphen).
var timestampPrefixPattern = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}(-\d{4})?-`)

// taskSetSlug returns the Task set identifier without its timestamp prefix;
// the commit carries its own date, so the prefix is noise in subjects.
func taskSetSlug(taskSetID string) string {
	if slug := timestampPrefixPattern.ReplaceAllString(taskSetID, ""); slug != "" {
		return slug
	}
	return taskSetID
}

// CommitSubject returns the implementation commit subject for a task.
func CommitSubject(taskSetID, taskID string) string {
	return fmt.Sprintf("tasks(%s): %s", taskSetSlug(taskSetID), taskID)
}

// TaskTrailerKey is the git trailer key naming the task an implementation commit
// came from (ADR-0216).
const TaskTrailerKey = "Pop-Task"

// TaskTrailer returns the trailer line an implementation commit ends with. The
// Task set identifier keeps its timestamp prefix — unlike a subject, which
// carries its own date, the trailer is an identifier readers match against the
// store's own directory names, so stripping would force a reverse mapping.
func TaskTrailer(taskSetID, taskID string) string {
	return fmt.Sprintf("%s: %s/%s", TaskTrailerKey, taskSetID, taskID)
}

// RefineCommitSubject returns pop's default subject for a Refine commit — the
// refine variant of the implementation format, used when the Refiner rendered
// no subject of its own or the set records no Commit convention to render one
// under (ADR-0252). The set is named without its timestamp prefix for the same
// reason a task's subject is: the commit carries its own date.
func RefineCommitSubject(taskSetID string) string {
	return fmt.Sprintf("tasks(%s): refine", taskSetSlug(taskSetID))
}

// RefineTrailerKey is the git trailer key naming the Task set a Refine commit
// belongs to (ADR-0252). It is its own key rather than Pop-Task's value with the
// task half left off: a refine pass belongs to no task, and a distinct key lets
// history be filtered for refine work without parsing subjects.
const RefineTrailerKey = "Pop-Refine"

// RefineTrailer returns the trailer line a Refine commit ends with. Like the
// Task trailer, the identifier keeps its timestamp prefix so a reader matches it
// against the Work store's own directory names.
func RefineTrailer(taskSetID string) string {
	return fmt.Sprintf("%s: %s", RefineTrailerKey, taskSetID)
}

// DirtyCheckpointSubject returns the checkpoint commit subject for dirty runtime state.
func DirtyCheckpointSubject(taskSetID, taskID string) string {
	return fmt.Sprintf("tasks(%s): %s capturing dirty state", taskSetSlug(taskSetID), taskID)
}
